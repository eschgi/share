package com.eschgi.share.transfer

import android.os.SystemClock
import com.eschgi.share.net.S3Connection
import com.eschgi.share.net.readLimited
import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection

/**
 * One file's upload into a bucket, for a server whose files are there (storage s3). The server
 * cuts the file into parts and hands out a link for each; the parts go straight to the bucket,
 * one after another, and the server puts them together. Between tries only the upload id is
 * kept: which parts the bucket has always comes from the server, so any failure, a switch
 * between addresses or a restart goes on where the bucket is. The phone's key goes only to the
 * server; the bucket gets nothing but the links.
 */
class S3Uploader(
    private val bufferSize: Int = 256 * 1024,
    private val bucket: (url: String, method: String) -> HttpURLConnection = { url, method -> S3Connection.open(url, method) },
    /** A clock that also counts deep sleep, for how old links are. */
    private val now: () -> Long = SystemClock::elapsedRealtimeNanos,
) {
    private class Plan(val id: String, val partSize: Long, val parts: Int, val state: String, val done: Set<Int>)

    private class Link(val url: String, val size: Long)

    /**
     * Uploads [size] bytes of [source] as [name], signed in into [folder]. [open] makes a request
     * to the server with the given method and path, on the current route and with the right
     * key. [onCreated] hears the id of a new upload (keep it: it's what a later try continues),
     * [onBytes] how far it is. Into an encrypted folder, [source] gives the encrypted stream,
     * [size] is its size, and [enc] goes along (UploadSeal.enc).
     */
    fun upload(
        name: String,
        size: Long,
        source: UploadSource,
        uploadId: String?,
        open: (method: String, path: String) -> HttpURLConnection,
        abort: Abort = Abort(),
        onCreated: (String) -> Unit = {},
        onBytes: (Long) -> Unit = {},
        folder: String? = null,
        lastModified: Long? = null,
        enc: JSONObject? = null,
    ): UploadOutcome {
        var id = uploadId
        var restarted = false
        var resyncs = 0
        var lostParts = 0
        var progressed = false
        var links = HashMap<Int, Link>()
        var linkedAt = 0L
        fun ended(o: UploadOutcome) = if (o is UploadOutcome.Retry) o.copy(progressed = o.progressed || progressed) else o
        outer@ while (true) {
            if (abort.stopped) return UploadOutcome.Stopped
            val plan = if (id == null) {
                val body = JSONObject().put("name", name).put("size", size)
                lastModified?.let { body.put("last_modified_ms", it) }
                folder?.let { body.put("folder", it) }
                enc?.let { body.put("enc", it) }
                when (val created = call(open, "POST", "/api/s3/uploads", body, abort)) {
                    is Step.End -> return ended(created.outcome)
                    is Step.Ok -> {
                        val j = created.value
                        val plan = Plan(j.getString("id"), j.getLong("part_size"), j.getInt("parts"), "receiving", emptySet())
                        id = plan.id
                        onCreated(plan.id)
                        links = linksOf(j.getJSONArray("urls"))
                        linkedAt = now()
                        plan
                    }
                }
            } else {
                when (val status = call(open, "GET", "/api/s3/uploads/$id", null, abort)) {
                    is Step.Ok -> {
                        val j = status.value
                        val done = j.getJSONArray("done_parts").let { a -> (0 until a.length()).map { a.getInt(it) }.toSet() }
                        Plan(j.getString("id"), j.getLong("part_size"), j.getInt("parts"), j.getString("state"), done)
                    }
                    is Step.End -> {
                        if (gone(status.outcome) && !restarted) {
                            restarted = true // the server or the bucket lost it: once more from the start
                            id = null
                            links.clear()
                            continue@outer
                        }
                        return ended(status.outcome)
                    }
                }
            }
            if (plan.parts != partsOf(size, plan.partSize) || links.any { (n, l) -> l.size != partLen(n, size, plan.partSize) }) {
                return UploadOutcome.Failed(0, "bad_plan")
            }
            if (plan.state == "complete") return UploadOutcome.Done(plan.id)

            val done = plan.done.toMutableSet()
            var doneBytes = done.sumOf { partLen(it, size, plan.partSize) }
            onBytes(doneBytes)
            if (plan.state != "finishing") {
                val missing = (1..plan.parts).filter { it !in done }
                var refusedPart = -1
                var i = 0
                while (i < missing.size) {
                    if (abort.stopped) return UploadOutcome.Stopped
                    val n = missing[i]
                    var link = links[n]
                    if (link == null || now() - linkedAt > LINK_LIFE_NANOS) {
                        val wanted = missing.drop(i).take(100)
                        val body = JSONObject().put("parts", JSONArray(wanted))
                        when (val fresh = call(open, "POST", "/api/s3/uploads/${plan.id}/parts", body, abort)) {
                            is Step.Ok -> {
                                links = linksOf(fresh.value.getJSONArray("urls"))
                                linkedAt = now()
                                link = links[n] ?: return ended(UploadOutcome.Retry(IOException("no link for part $n")))
                                if (link.size != partLen(n, size, plan.partSize)) return UploadOutcome.Failed(0, "bad_plan")
                            }
                            is Step.End -> {
                                val o = fresh.outcome
                                if (o is UploadOutcome.Failed && o.code == "s3_upload_finished") continue@outer // finished meanwhile
                                if (gone(o) && !restarted) {
                                    restarted = true
                                    id = null
                                    links.clear()
                                    continue@outer
                                }
                                return ended(o)
                            }
                        }
                    }
                    val before = doneBytes
                    when (val sent = put(link, source, (n - 1L) * plan.partSize, abort) { onBytes(before + it) }) {
                        Put.Sent -> {
                            done += n
                            doneBytes += link.size
                            progressed = true
                            refusedPart = -1
                            i++
                        }
                        is Put.Refused -> {
                            // An expired link, or a clock that is off: fresh links, once for this part.
                            onBytes(doneBytes)
                            if (refusedPart == n) return ended(UploadOutcome.Retry(null, sent.status))
                            refusedPart = n
                            links.clear()
                        }
                        Put.NoUpload -> {
                            onBytes(doneBytes)
                            if (++lostParts > 2) return ended(UploadOutcome.Retry(null, 404))
                            continue@outer // the server knows whether it is gone
                        }
                        is Put.End -> {
                            onBytes(doneBytes)
                            return ended(sent.outcome)
                        }
                    }
                }
            }

            // Putting a big file together may take a while.
            when (val completed = call(open, "POST", "/api/s3/uploads/${plan.id}/complete", JSONObject(), abort, readTimeoutMs = 180_000)) {
                is Step.Ok -> return UploadOutcome.Done(completed.value.optString("id").ifEmpty { plan.id })
                is Step.End -> {
                    val o = completed.outcome
                    if (o is UploadOutcome.Failed && o.code == "s3_parts_missing") {
                        if (++resyncs <= 2) continue@outer // a part sent again or lost: which ones now?
                        return ended(UploadOutcome.Retry(null, 409))
                    }
                    if (gone(o) && !restarted) {
                        restarted = true
                        id = null
                        links.clear()
                        continue@outer
                    }
                    // While the server is still putting it together, waiting is progress too.
                    if (o is UploadOutcome.Retry && plan.state == "finishing") return o.copy(progressed = true)
                    return ended(o)
                }
            }
        }
    }

    private sealed interface Step<out T> {
        data class Ok<T>(val value: T) : Step<T>
        data class End(val outcome: UploadOutcome) : Step<Nothing>
    }

    /** A JSON request to the server. The body is sent buffered, so the answer to a refusal stays readable. */
    private fun call(
        open: (String, String) -> HttpURLConnection,
        method: String,
        path: String,
        body: JSONObject?,
        abort: Abort,
        readTimeoutMs: Int? = null,
    ): Step<JSONObject> {
        val conn = try {
            open(method, path)
        } catch (e: IOException) {
            return Step.End(UploadOutcome.Retry(e))
        }
        abort.connection = conn
        return try {
            readTimeoutMs?.let { conn.readTimeout = it }
            if (body != null) {
                val bytes = body.toString().toByteArray()
                conn.setRequestProperty("Content-Type", "application/json")
                conn.doOutput = true
                conn.outputStream.use { it.write(bytes) }
            }
            val status = conn.responseCode
            if (status !in 200..299) return Step.End(outcomeOf(conn))
            val text = if (status == 204) "" else String(readLimited(conn.inputStream, 4 * 1024 * 1024))
            Step.Ok(if (text.isBlank()) JSONObject() else JSONObject(text))
        } catch (e: JSONException) {
            Step.End(UploadOutcome.Retry(IOException("the server's answer to $method $path can't be read")))
        } catch (e: IOException) {
            Step.End(if (abort.stopped) UploadOutcome.Stopped else UploadOutcome.Retry(e))
        } finally {
            abort.connection = null
            conn.disconnect()
        }
    }

    private sealed interface Put {
        data object Sent : Put

        /** 401 or 403: the link expired, or a clock is off. */
        data class Refused(val status: Int) : Put

        /** 404: the bucket doesn't have the upload (any more). */
        data object NoUpload : Put

        data class End(val outcome: UploadOutcome) : Put
    }

    /** One part to the bucket, exactly its length and nothing else: only the length is signed. */
    private fun put(link: Link, source: UploadSource, offset: Long, abort: Abort, onBytes: (Long) -> Unit): Put {
        val input = try {
            source.openAt(offset)
        } catch (e: IOException) {
            return Put.End(UploadOutcome.Lost)
        } catch (e: SecurityException) {
            return Put.End(UploadOutcome.Lost) // the permission to read it is gone
        }
        return input.use { data ->
            val conn = try {
                bucket(link.url, "PUT")
            } catch (e: S3Connection.InsecureLink) {
                return Put.End(UploadOutcome.Failed(0, "insecure_link"))
            } catch (e: IOException) {
                return Put.End(UploadOutcome.Retry(S3Connection.scrub(e)))
            }
            abort.connection = conn
            try {
                conn.setFixedLengthStreamingMode(link.size)
                conn.doOutput = true
                conn.outputStream.use { out -> copySource(data, out, link.size, abort, bufferSize, onBytes) }
                when (val status = conn.responseCode) {
                    in 200..299 -> Put.Sent
                    401, 403 -> Put.Refused(status)
                    404 -> Put.NoUpload
                    408, 429, in 500..599 -> Put.End(UploadOutcome.Retry(null, status, Downloader.retryAfter(conn.getHeaderField("Retry-After"))))
                    else -> Put.End(UploadOutcome.Failed(status, bucketCode(conn)))
                }
            } catch (e: SourceError) {
                Put.End(UploadOutcome.Lost)
            } catch (e: IOException) {
                Put.End(if (abort.stopped) UploadOutcome.Stopped else UploadOutcome.Retry(S3Connection.scrub(e)))
            } finally {
                abort.connection = null
                conn.disconnect()
            }
        }
    }

    companion object {
        /** Links work for an hour; after 45 minutes the next part asks for fresh ones. */
        private const val LINK_LIFE_NANOS = 45L * 60 * 1_000_000_000

        private fun gone(o: UploadOutcome) = o is UploadOutcome.Failed && o.status == 404 && o.code != "folder_gone"

        internal fun partsOf(size: Long, partSize: Long): Int = if (size == 0L) 0 else ((size + partSize - 1) / partSize).toInt()

        internal fun partLen(n: Int, size: Long, partSize: Long): Long = minOf(partSize, size - (n - 1L) * partSize)

        private fun linksOf(urls: JSONArray): HashMap<Int, Link> {
            val out = HashMap<Int, Link>()
            for (i in 0 until urls.length()) {
                val u = urls.getJSONObject(i)
                out[u.getInt("number")] = Link(u.getString("url"), u.getLong("size"))
            }
            return out
        }

        /** The code of the bucket's XML error, such as EntityTooLarge. */
        private fun bucketCode(conn: HttpURLConnection): String? = try {
            val body = conn.errorStream?.use { String(readLimited(it, 64 * 1024)) } ?: ""
            Regex("<Code>([^<]+)</Code>").find(body)?.groupValues?.get(1)
        } catch (e: IOException) {
            null
        }
    }
}
