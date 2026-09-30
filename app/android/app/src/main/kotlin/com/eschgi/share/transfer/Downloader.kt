package com.eschgi.share.transfer

import java.io.EOFException
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection

/** Where a download is written. What's already there is kept, and a resume appends to it. */
interface DownloadSink {
    /** How many bytes are there already. */
    fun length(): Long

    /** A stream that writes at [length]. */
    fun append(): OutputStream

    fun truncate()

    /** Makes the file visible where it belongs, and says where that is (a URI or a path). */
    fun commit(): String

    /** Removes what was written. */
    fun discard()
}

/** A plain file, written as `<name>.part` and renamed at the end: the cache for sharing and playing. */
class FileSink(private val target: File) : DownloadSink {
    private val partial = File(target.path + ".part")

    override fun length() = if (partial.exists()) partial.length() else 0L

    override fun append(): OutputStream {
        partial.parentFile?.mkdirs()
        return FileOutputStream(partial, true)
    }

    override fun truncate() {
        partial.delete()
    }

    override fun commit(): String {
        if (!partial.exists()) partial.createNewFile()
        target.delete()
        if (!partial.renameTo(target)) throw IOException("can't move $partial to $target")
        return target.path
    }

    override fun discard() {
        partial.delete()
    }
}

/** Stops a running download from another thread, without waiting for its next read. */
class Abort {
    @Volatile var stopped = false
        private set

    @Volatile internal var connection: HttpURLConnection? = null

    fun stop() {
        stopped = true
        connection?.disconnect()
    }
}

/**
 * One file's download from GET /api/files/{id}/content. What the sink already has is kept
 * and the rest asked for with Range + If-Range; the file's id is its ETag, and a file's
 * content never changes, so a resume is always safe.
 */
class Downloader(private val bufferSize: Int = 256 * 1024) {

    sealed interface Outcome {
        /** Everything is in the sink; it still has to be committed. */
        data object Done : Outcome

        /** 401: the phone's key doesn't work any more. */
        data object SignedOut : Outcome

        /** 404: deleted on the server since. */
        data object Gone : Outcome

        data object Stopped : Outcome

        /** An answer that trying again won't change. */
        data class Failed(val status: Int, val message: String) : Outcome

        /** Try again later; [progressed] if some bytes arrived first. */
        data class Retry(
            val cause: Throwable?,
            val status: Int? = null,
            val retryAfterMs: Long? = null,
            val progressed: Boolean = false,
        ) : Outcome

        /** Writing on the phone failed: full, or the target went away. */
        data class WriteFailed(val cause: IOException) : Outcome
    }

    /**
     * Downloads file [id] of [size] bytes into [sink]. [open] makes the request for a path
     * (on the current route, with the phone's key); [onBytes] hears how many bytes the sink
     * holds as they arrive.
     */
    fun fetch(
        id: String,
        size: Long,
        sink: DownloadSink,
        open: (path: String) -> HttpURLConnection,
        abort: Abort = Abort(),
        onBytes: (Long) -> Unit = {},
    ): Outcome {
        var offset = try {
            sink.length()
        } catch (e: IOException) {
            return Outcome.WriteFailed(e)
        }
        if (offset > size) {
            sink.truncate()
            offset = 0
        }
        if (offset == size) return Outcome.Done // everything arrived last time
        if (abort.stopped) return Outcome.Stopped

        val conn = try {
            open("/api/files/$id/content")
        } catch (e: IOException) {
            return Outcome.Retry(e)
        }
        abort.connection = conn
        try {
            conn.setRequestProperty("Accept-Encoding", "identity")
            if (offset > 0) {
                conn.setRequestProperty("Range", "bytes=$offset-")
                conn.setRequestProperty("If-Range", "\"$id\"")
            }
            val status = try {
                conn.responseCode
            } catch (e: IOException) {
                return if (abort.stopped) Outcome.Stopped else Outcome.Retry(e)
            }
            when (status) {
                200 -> if (offset > 0) {
                    // The server sent all of it after all (If-Range didn't match).
                    sink.truncate()
                    offset = 0
                    onBytes(0)
                }
                206 -> {
                    val range = contentRange(conn.getHeaderField("Content-Range"))
                    if (range == null || range.first != offset || (range.second != null && range.second != size)) {
                        sink.truncate()
                        onBytes(0)
                        return Outcome.Retry(IOException("unexpected range ${conn.getHeaderField("Content-Range")}"), status)
                    }
                }
                401 -> return Outcome.SignedOut
                404, 410 -> return Outcome.Gone
                416 -> {
                    // Asked past the end: what we have doesn't fit this file. Start over.
                    sink.truncate()
                    onBytes(0)
                    return Outcome.Retry(null, status)
                }
                408, 429, 500, 502, 503, 504, in 520..530 ->
                    return Outcome.Retry(null, status, retryAfter(conn.getHeaderField("Retry-After")))
                else -> return Outcome.Failed(status, conn.responseMessage ?: "")
            }

            val input = try {
                conn.inputStream
            } catch (e: IOException) {
                return if (abort.stopped) Outcome.Stopped else Outcome.Retry(e, status)
            }
            val out = try {
                sink.append()
            } catch (e: IOException) {
                return Outcome.WriteFailed(e)
            }
            val start = offset
            val copied = copy(input, out, size, offset, abort, onBytes)
            try {
                out.close()
            } catch (e: IOException) {
                return Outcome.WriteFailed(e)
            }
            return when (copied) {
                is Copied.Complete -> Outcome.Done
                is Copied.ReadError ->
                    if (abort.stopped) Outcome.Stopped else Outcome.Retry(copied.cause, status, progressed = copied.offset > start)
                is Copied.WriteError -> Outcome.WriteFailed(copied.cause)
                is Copied.TooLong -> {
                    sink.truncate()
                    Outcome.Failed(status, "longer than $size bytes")
                }
                Copied.Stopped -> Outcome.Stopped
            }
        } finally {
            abort.connection = null
            conn.disconnect()
        }
    }

    private sealed interface Copied {
        data object Complete : Copied
        data object Stopped : Copied
        data object TooLong : Copied
        data class ReadError(val cause: IOException, val offset: Long) : Copied
        data class WriteError(val cause: IOException) : Copied
    }

    private fun copy(input: InputStream, out: OutputStream, size: Long, from: Long, abort: Abort, onBytes: (Long) -> Unit): Copied {
        val buffer = ByteArray(bufferSize)
        var offset = from
        while (true) {
            if (abort.stopped) return Copied.Stopped
            val n = try {
                input.read(buffer)
            } catch (e: IOException) {
                return Copied.ReadError(e, offset)
            }
            if (n < 0) break
            if (offset + n > size) return Copied.TooLong
            try {
                out.write(buffer, 0, n)
            } catch (e: IOException) {
                return Copied.WriteError(e)
            }
            offset += n
            onBytes(offset)
        }
        return if (offset == size) Copied.Complete else Copied.ReadError(EOFException("ended at $offset of $size"), offset)
    }

    companion object {
        private val contentRangePattern = Regex("""bytes (\d+)-(\d+)/(\d+|\*)""")

        /** The first byte and the total of a Content-Range header. */
        internal fun contentRange(header: String?): Pair<Long, Long?>? {
            val m = contentRangePattern.matchEntire(header?.trim() ?: return null) ?: return null
            return m.groupValues[1].toLong() to m.groupValues[3].toLongOrNull()
        }

        /** Retry-After in seconds; the date form isn't used by the server. */
        internal fun retryAfter(header: String?): Long? = header?.trim()?.toLongOrNull()?.takeIf { it >= 0 }?.times(1000)
    }
}
