package com.eschgi.share.transfer

import org.json.JSONException
import org.json.JSONObject
import java.io.File
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.util.Base64

/** Where an upload's bytes come from: a picked file, or in tests a plain one. */
interface UploadSource {
    /** The bytes from [offset] on. Throws if the file can't be read any more. */
    fun openAt(offset: Long): InputStream
}

class FileSource(private val file: File) : UploadSource {
    override fun openAt(offset: Long): InputStream =
        FileInputStream(file).also { it.channel.position(offset) }
}

/**
 * One file's upload with tus 1.0, the way the server's /tus/ speaks it. Between tries only
 * the upload id is kept: where to go on always comes from the server, so any failure, a
 * switch between the local and the public address, or a restart continues where the server
 * has the file.
 */
class Uploader(private val bufferSize: Int = 256 * 1024) {

    sealed interface Outcome {
        /** All of it is on the server; [id] is the file's id in the library. */
        data class Done(val id: String) : Outcome

        /** 401 for a phone: its key doesn't work any more. */
        data object SignedOut : Outcome

        /** 401 for a PIN: it ended. Unlocking again moves the upload to the new PIN. */
        data object PinEnded : Outcome

        /** The picked file can't be read any more: it has to be picked again. */
        data object Lost : Outcome

        data object Stopped : Outcome

        /** An answer that trying again won't change: too big, not allowed, not ours. */
        data class Failed(val status: Int, val code: String?) : Outcome

        data class Retry(
            val cause: Throwable?,
            val status: Int? = null,
            val retryAfterMs: Long? = null,
            val progressed: Boolean = false,
        ) : Outcome
    }

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
    ): Outcome {
        var id = uploadId
        var offset: Long
        var started = -1L
        var recreated = 0
        while (true) {
            if (abort.stopped) return Outcome.Stopped
            if (id == null) {
                when (val created = create(name, mime, size, folder, open, abort)) {
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
                        if (head.outcome !is Outcome.Failed || head.outcome.status !in GONE || ++recreated > 1) return head.outcome
                        id = null // it expired on the server: start this file again
                        continue
                    }
                }
            }
            if (started < 0) started = offset
            onBytes(offset)

            // PATCH until done; a 409 or 404 on the way sends us back to HEAD or POST.
            while (offset < size) {
                if (abort.stopped) return Outcome.Stopped
                val length = minOf(chunkSize, size - offset)
                when (val sent = patch(id!!, source, offset, length, open, abort, onBytes)) {
                    is Step.Ok -> offset = sent.value
                    is Step.End -> {
                        val o = sent.outcome
                        if (o is Outcome.Retry) return o.copy(progressed = offset > started)
                        if (o is Outcome.Failed && o.status == 409) break // somewhere else: HEAD again
                        if (o is Outcome.Failed && o.status in GONE && ++recreated <= 1) {
                            id = null
                            break
                        }
                        return o
                    }
                }
            }
            if (id != null && offset >= size) return Outcome.Done(id)
        }
    }

    private sealed interface Step<out T> {
        data class Ok<T>(val value: T) : Step<T>
        data class End(val outcome: Outcome) : Step<Nothing>
    }

    private fun create(name: String, mime: String, size: Long, folder: String?, open: (String, String) -> HttpURLConnection, abort: Abort): Step<String> =
        request(open, "POST", "/tus/", abort) { conn ->
            conn.setRequestProperty("Upload-Length", size.toString())
            conn.setRequestProperty("Upload-Metadata", "filename ${b64(name)},filetype ${b64(mime)}" + (folder?.let { ",folder ${b64(it)}" } ?: ""))
            // No body, so no streaming mode: in it the JDK hides the body of a 401.
            conn.doOutput = true
            conn.outputStream.close()
            if (conn.responseCode != 201) return@request Step.End(outcomeOf(conn))
            val location = conn.getHeaderField("Location") ?: return@request Step.End(Outcome.Retry(IOException("no Location")))
            Step.Ok(location.trimEnd('/').substringAfterLast('/'))
        }

    private fun head(id: String, open: (String, String) -> HttpURLConnection, abort: Abort): Step<Long> =
        request(open, "HEAD", "/tus/$id", abort) { conn ->
            if (conn.responseCode != 200) return@request Step.End(outcomeOf(conn))
            val offset = conn.getHeaderField("Upload-Offset")?.toLongOrNull()
                ?: return@request Step.End(Outcome.Retry(IOException("no Upload-Offset")))
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
            return Step.End(Outcome.Lost)
        } catch (e: SecurityException) {
            return Step.End(Outcome.Lost) // the permission to read it is gone
        }
        return input.use { data ->
            request(open, "PATCH", "/tus/$id", abort) { conn ->
                conn.setRequestProperty("Upload-Offset", offset.toString())
                conn.setRequestProperty("Content-Type", "application/offset+octet-stream")
                conn.setFixedLengthStreamingMode(length)
                conn.doOutput = true
                conn.outputStream.use { out -> copy(data, out, length, abort) { onBytes(offset + it) } }
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
            return Step.End(Outcome.Retry(e))
        }
        abort.connection = conn
        return try {
            conn.setRequestProperty("Tus-Resumable", "1.0.0")
            body(conn)
        } catch (e: SourceError) {
            Step.End(Outcome.Lost)
        } catch (e: IOException) {
            Step.End(if (abort.stopped) Outcome.Stopped else Outcome.Retry(e))
        } finally {
            abort.connection = null
            conn.disconnect()
        }
    }

    /** The picked file failed while it was being read. */
    private class SourceError(cause: Throwable) : IOException(cause)

    private fun copy(input: InputStream, out: OutputStream, length: Long, abort: Abort, onBytes: (Long) -> Unit) {
        val buffer = ByteArray(bufferSize)
        var sent = 0L
        while (sent < length) {
            if (abort.stopped) throw IOException("stopped")
            val n = try {
                input.read(buffer, 0, minOf(buffer.size.toLong(), length - sent).toInt())
            } catch (e: IOException) {
                throw SourceError(e)
            } catch (e: SecurityException) {
                throw SourceError(e)
            }
            if (n < 0) throw SourceError(IOException("the file ended at ${sent + 0} of $length"))
            out.write(buffer, 0, n)
            sent += n
            onBytes(sent)
        }
    }

    companion object {
        private val GONE = setOf(404, 410)

        private fun b64(s: String) = Base64.getEncoder().encodeToString(s.toByteArray())

        /** What an error answer means for the upload. */
        internal fun outcomeOf(conn: HttpURLConnection): Outcome {
            val status = conn.responseCode
            val code = errorCode(conn)
            return when {
                status == 401 && code == "session_ended" -> Outcome.PinEnded
                status == 401 -> Outcome.SignedOut
                status == 408 || status == 429 || status >= 500 ->
                    Outcome.Retry(null, status, Downloader.retryAfter(conn.getHeaderField("Retry-After")))
                else -> Outcome.Failed(status, code)
            }
        }

        /** The code of the server's JSON error, if the answer is one (tusd's own are text). */
        private fun errorCode(conn: HttpURLConnection): String? = try {
            val body = conn.errorStream?.use { String(it.readBytes(), Charsets.UTF_8) } ?: return null
            JSONObject(body).optJSONObject("error")?.optString("code")?.ifEmpty { null }
        } catch (e: IOException) {
            null
        } catch (e: JSONException) {
            null
        }
    }
}
