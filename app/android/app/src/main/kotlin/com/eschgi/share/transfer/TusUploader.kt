package com.eschgi.share.transfer

import java.io.IOException
import java.net.HttpURLConnection
import java.util.Base64

/**
 * One file's upload with tus 1.0, the way the server's /tus/ speaks it. Between tries only
 * the upload id is kept: where to go on always comes from the server, so any failure, a
 * switch between the local and the public address, or a restart continues where the server
 * has the file.
 */
class TusUploader(private val bufferSize: Int = 256 * 1024) {

    /**
     * Uploads [size] bytes of [source] as [name], signed in into [folder]. [open] makes a request
     * with the given method and path, on the current route and with the right key. [onCreated]
     * hears the id of a new upload (keep it: it's what a later try continues), [onBytes] how far
     * it is.
     */
    fun upload(
        name: String,
        mime: String,
        size: Long,
        source: UploadSource,
        uploadId: String?,
        chunkSize: Long,
        open: (method: String, path: String) -> HttpURLConnection,
        abort: Abort = Abort(),
        onCreated: (String) -> Unit = {},
        onBytes: (Long) -> Unit = {},
        folder: String? = null,
        lastModified: Long? = null,
    ): UploadOutcome {
        var id = uploadId
        var offset: Long
        var started = -1L
        var recreated = 0
        while (true) {
            if (abort.stopped) return UploadOutcome.Stopped
            if (id == null) {
                when (val created = create(name, mime, size, folder, lastModified, open, abort)) {
                    is Step.Ok -> {
                        id = created.value
                        onCreated(id)
                        offset = 0
                    }
                    is Step.End -> return created.outcome
                }
            } else {
                when (val head = head(id, open, abort)) {
                    is Step.Ok -> offset = head.value
                    is Step.End -> {
                        if (head.outcome !is UploadOutcome.Failed || head.outcome.status !in GONE || ++recreated > 1) return head.outcome
                        id = null // it expired on the server: start this file again
                        continue
                    }
                }
            }
            if (started < 0) started = offset
            onBytes(offset)

            // PATCH until done; a 409 or 404 on the way sends us back to HEAD or POST.
            while (offset < size) {
                if (abort.stopped) return UploadOutcome.Stopped
                val length = minOf(chunkSize, size - offset)
                when (val sent = patch(id!!, source, offset, length, open, abort, onBytes)) {
                    is Step.Ok -> offset = sent.value
                    is Step.End -> {
                        val o = sent.outcome
                        if (o is UploadOutcome.Retry) return o.copy(progressed = offset > started)
                        if (o is UploadOutcome.Failed && o.status == 409) break // somewhere else: HEAD again
                        if (o is UploadOutcome.Failed && o.status in GONE && ++recreated <= 1) {
                            id = null
                            break
                        }
                        return o
                    }
                }
            }
            if (id != null && offset >= size) return UploadOutcome.Done(id)
        }
    }

    private sealed interface Step<out T> {
        data class Ok<T>(val value: T) : Step<T>
        data class End(val outcome: UploadOutcome) : Step<Nothing>
    }

    private fun create(name: String, mime: String, size: Long, folder: String?, lastModified: Long?, open: (String, String) -> HttpURLConnection, abort: Abort): Step<String> =
        request(open, "POST", "/tus/", abort) { conn ->
            conn.setRequestProperty("Upload-Length", size.toString())
            conn.setRequestProperty(
                "Upload-Metadata",
                "filename ${b64(name)},filetype ${b64(mime)}" + (folder?.let { ",folder ${b64(it)}" } ?: "") + (lastModified?.let { ",lastModified ${b64(it.toString())}" } ?: ""),
            )
            // No body, so no streaming mode: in it the JDK hides the body of a 401.
            conn.doOutput = true
            conn.outputStream.close()
            if (conn.responseCode != 201) return@request Step.End(outcomeOf(conn))
            val location = conn.getHeaderField("Location") ?: return@request Step.End(UploadOutcome.Retry(IOException("no Location")))
            Step.Ok(location.trimEnd('/').substringAfterLast('/'))
        }

    private fun head(id: String, open: (String, String) -> HttpURLConnection, abort: Abort): Step<Long> =
        request(open, "HEAD", "/tus/$id", abort) { conn ->
            if (conn.responseCode != 200) return@request Step.End(outcomeOf(conn))
            val offset = conn.getHeaderField("Upload-Offset")?.toLongOrNull()
                ?: return@request Step.End(UploadOutcome.Retry(IOException("no Upload-Offset")))
            Step.Ok(offset)
        }

    private fun patch(
        id: String,
        source: UploadSource,
        offset: Long,
        length: Long,
        open: (String, String) -> HttpURLConnection,
        abort: Abort,
        onBytes: (Long) -> Unit,
    ): Step<Long> {
        val input = try {
            source.openAt(offset)
        } catch (e: IOException) {
            return Step.End(UploadOutcome.Lost)
        } catch (e: SecurityException) {
            return Step.End(UploadOutcome.Lost) // the permission to read it is gone
        }
        return input.use { data ->
            request(open, "PATCH", "/tus/$id", abort) { conn ->
                conn.setRequestProperty("Upload-Offset", offset.toString())
                conn.setRequestProperty("Content-Type", "application/offset+octet-stream")
                conn.setFixedLengthStreamingMode(length)
                conn.doOutput = true
                conn.outputStream.use { out -> copySource(data, out, length, abort, bufferSize) { onBytes(offset + it) } }
                if (conn.responseCode != 204) return@request Step.End(outcomeOf(conn))
                val next = conn.getHeaderField("Upload-Offset")?.toLongOrNull() ?: (offset + length)
                Step.Ok(next)
            }
        }
    }

    /** Runs one request; a broken connection is a Retry, and a stop wins over both. */
    private fun <T> request(
        open: (String, String) -> HttpURLConnection,
        method: String,
        path: String,
        abort: Abort,
        body: (HttpURLConnection) -> Step<T>,
    ): Step<T> {
        val conn = try {
            open(method, path)
        } catch (e: IOException) {
            return Step.End(UploadOutcome.Retry(e))
        }
        abort.connection = conn
        return try {
            conn.setRequestProperty("Tus-Resumable", "1.0.0")
            body(conn)
        } catch (e: SourceError) {
            Step.End(UploadOutcome.Lost)
        } catch (e: IOException) {
            Step.End(if (abort.stopped) UploadOutcome.Stopped else UploadOutcome.Retry(e))
        } finally {
            abort.connection = null
            conn.disconnect()
        }
    }

    companion object {
        private val GONE = setOf(404, 410)

        private fun b64(s: String) = Base64.getEncoder().encodeToString(s.toByteArray())
    }
}
