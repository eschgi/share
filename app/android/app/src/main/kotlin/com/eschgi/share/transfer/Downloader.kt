package com.eschgi.share.transfer

import com.eschgi.share.e2ee.ContentCipher
import com.eschgi.share.e2ee.DecryptingStream
import com.eschgi.share.e2ee.E2ee
import com.eschgi.share.e2ee.E2eeException
import com.eschgi.share.net.S3Connection
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
 * One file's download: from GET /api/files/{id}/content, or from the file's link in a bucket.
 * What the sink already has is kept and the rest asked for with Range (from the server with
 * If-Range, the file's id being its ETag); a file's content never changes, so a resume is
 * always safe. An encrypted file (docs/e2ee-plan.md) is decrypted on the way with its
 * [ContentCipher]: the sink gets the plain bytes, a whole chunk at a time, and a resume asks
 * for the encrypted bytes from the chunk where the sink stopped.
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
     * holds as they arrive. With [cipher], the file is encrypted and [size] its plain size.
     */
    fun fetch(
        id: String,
        size: Long,
        sink: DownloadSink,
        open: (path: String) -> HttpURLConnection,
        abort: Abort = Abort(),
        onBytes: (Long) -> Unit = {},
        cipher: ContentCipher? = null,
    ): Outcome = receive(size, sink, abort, onBytes, cipher, connect = { offset ->
        open("/api/files/$id/content").apply { if (offset > 0) setRequestProperty("If-Range", "\"$id\"") }
    }) { status, conn ->
        when (status) {
            401 -> Outcome.SignedOut
            404, 410 -> Outcome.Gone
            else -> Outcome.Failed(status, conn.responseMessage ?: "")
        }
    }

    /**
     * Downloads file [id] from the bucket (a server whose files are there), with links from
     * [link]: the phone's key never goes to the bucket. Objects never change, so a resume needs
     * only Range. A refused link gets one fresh one and keeps what arrived; a missing object
     * asks the server whether the file is gone.
     */
    fun fetchS3(
        id: String,
        size: Long,
        sink: DownloadSink,
        link: () -> S3Links.Answer,
        abort: Abort = Abort(),
        onBytes: (Long) -> Unit = {},
        bucket: (url: String) -> HttpURLConnection = { S3Connection.open(it) },
        cipher: ContentCipher? = null,
    ): Outcome {
        var refreshed = false
        while (true) {
            val url = when (val answer = link()) {
                is S3Links.Answer.Link -> answer.url
                else -> return outcomeOf(answer)
            }
            if (!S3Connection.allowed(url)) return Outcome.Failed(0, "a link to plain http outside home")
            var refusal = 0
            val outcome = receive(size, sink, abort, onBytes, cipher, connect = { bucket(url) }) { status, _ ->
                refusal = status
                Outcome.Failed(status, "")
            }
            when (refusal) {
                0 -> return if (outcome is Outcome.Retry && outcome.cause != null) outcome.copy(cause = S3Connection.scrub(outcome.cause)) else outcome
                401, 403 -> if (refreshed) return Outcome.Retry(null, refusal) else refreshed = true // a link that ran out
                404 -> return when (val answer = link()) {
                    is S3Links.Answer.Link -> Outcome.Failed(404, "the bucket doesn't have the file")
                    else -> outcomeOf(answer)
                }
                else -> return outcome
            }
        }
    }

    /** The server's answer instead of a link. */
    private fun outcomeOf(answer: S3Links.Answer): Outcome = when (answer) {
        is S3Links.Answer.Link -> error("a link")
        S3Links.Answer.SignedOut -> Outcome.SignedOut
        S3Links.Answer.Gone -> Outcome.Gone
        is S3Links.Answer.Retry -> Outcome.Retry(answer.cause, answer.status, answer.retryAfterMs)
        is S3Links.Answer.Failed -> Outcome.Failed(answer.status, answer.code ?: "")
    }

    /**
     * What a download from the server and one from the bucket share: what the sink has stays,
     * and the rest comes with Range, over a request [connect] makes for that offset. Answers
     * that aren't data and that waiting won't change go to [refused].
     */
    private fun receive(
        size: Long,
        sink: DownloadSink,
        abort: Abort,
        onBytes: (Long) -> Unit,
        cipher: ContentCipher?,
        connect: (offset: Long) -> HttpURLConnection,
        refused: (status: Int, conn: HttpURLConnection) -> Outcome,
    ): Outcome {
        var offset = try {
            sink.length()
        } catch (e: IOException) {
            return Outcome.WriteFailed(e)
        }
        // A plain offset that doesn't start a chunk: the end of a chunk didn't make it.
        if (offset > size || (cipher != null && offset % E2ee.CHUNK_SIZE != 0L && offset != size)) {
            sink.truncate()
            offset = 0
        }
        if (offset == size) return Outcome.Done // everything arrived last time
        if (abort.stopped) return Outcome.Stopped

        // Where in the stored bytes to go on: for an encrypted file, at the chunk it stopped before.
        var from = if (cipher == null || offset == 0L) offset else E2ee.readStart(offset).cipherOffset
        val total = cipher?.encryptedSize ?: size
        val conn = try {
            connect(from)
        } catch (e: IOException) {
            return Outcome.Retry(e)
        }
        abort.connection = conn
        try {
            // Without it Android would ask for gzip and unpack it, and ranges wouldn't fit.
            conn.setRequestProperty("Accept-Encoding", "identity")
            if (from > 0) conn.setRequestProperty("Range", "bytes=$from-")
            val status = try {
                conn.responseCode
            } catch (e: IOException) {
                return if (abort.stopped) Outcome.Stopped else Outcome.Retry(e)
            }
            when (status) {
                200 -> if (offset > 0) {
                    // All of it after all (the server's If-Range didn't match).
                    sink.truncate()
                    offset = 0
                    from = 0
                    onBytes(0)
                }
                206 -> {
                    val range = contentRange(conn.getHeaderField("Content-Range"))
                    if (range == null || range.first != from || (range.second != null && range.second != total)) {
                        sink.truncate()
                        onBytes(0)
                        return Outcome.Retry(IOException("unexpected range ${conn.getHeaderField("Content-Range")}"), status)
                    }
                }
                416 -> {
                    // Asked past the end: what we have doesn't fit this file. Start over.
                    sink.truncate()
                    onBytes(0)
                    return Outcome.Retry(null, status)
                }
                408, 429, 500, 502, 503, 504, in 520..530 ->
                    return Outcome.Retry(null, status, retryAfter(conn.getHeaderField("Retry-After")))
                else -> return refused(status, conn)
            }

            val input = try {
                val raw = conn.inputStream
                if (cipher == null) raw else decrypting(raw, cipher, from, offset)
            } catch (e: E2eeException) {
                return Outcome.Failed(status, "not this file: ${e.message}")
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
                is Copied.Broken -> {
                    // Changed on the way or on the server: it won't decrypt however often it comes.
                    sink.truncate()
                    Outcome.Failed(status, "can't be decrypted: ${copied.cause.message}")
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
        data class Broken(val cause: E2eeException) : Copied
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
            } catch (e: E2eeException) {
                return Copied.Broken(e)
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

    /**
     * The plain bytes from [offset] on, of an encrypted file whose stored bytes [raw] gives from
     * [from]: from the start, the header comes first, and must be this file's. A stream that
     * ends early fails as a broken connection does, so the download tries again.
     */
    private fun decrypting(raw: InputStream, cipher: ContentCipher, from: Long, offset: Long): InputStream {
        val input = ExactLength(raw, cipher.encryptedSize - from)
        if (from == 0L) {
            val header = ByteArray(E2ee.HEADER_SIZE)
            var n = 0
            while (n < header.size) {
                val r = input.read(header, n, header.size - n)
                if (r < 0) throw EOFException("the header was cut short")
                n += r
            }
            if (!header.contentEquals(cipher.header)) throw E2eeException("another header")
        }
        return DecryptingStream(input, cipher, offset / E2ee.CHUNK_SIZE)
    }

    /** [input], which must give [length] bytes: ending before is an EOFException. */
    private class ExactLength(private val input: InputStream, private var length: Long) : InputStream() {
        override fun read(): Int {
            val one = ByteArray(1)
            return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xff
        }

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (len == 0) return 0
            val n = input.read(b, off, len)
            if (n < 0) {
                if (length > 0) throw EOFException("$length bytes short")
                return -1
            }
            length -= n
            return n
        }

        override fun close() = input.close()
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
