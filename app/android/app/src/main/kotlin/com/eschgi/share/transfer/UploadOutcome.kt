package com.eschgi.share.transfer

import org.json.JSONException
import org.json.JSONObject
import java.io.File
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection

/** Where an upload's bytes come from: a picked file, or in tests a plain one. */
interface UploadSource {
    /** The bytes from [offset] on. Throws if the file can't be read any more. */
    fun openAt(offset: Long): InputStream
}

class FileSource(private val file: File) : UploadSource {
    override fun openAt(offset: Long): InputStream =
        FileInputStream(file).also { it.channel.position(offset) }
}

/** How one file's upload ended, whichever way it went to the server. */
sealed interface UploadOutcome {
    /** All of it is on the server; [id] is the file's id in the library. */
    data class Done(val id: String) : UploadOutcome

    /** 401 for a phone: its key doesn't work any more. */
    data object SignedOut : UploadOutcome

    /** 401 for a PIN: it ended. Unlocking again moves the upload to the new PIN. */
    data object PinEnded : UploadOutcome

    /** The picked file can't be read any more: it has to be picked again. */
    data object Lost : UploadOutcome

    data object Stopped : UploadOutcome

    /** An answer that trying again won't change: too big, not allowed, not ours. */
    data class Failed(val status: Int, val code: String?) : UploadOutcome

    data class Retry(
        val cause: Throwable?,
        val status: Int? = null,
        val retryAfterMs: Long? = null,
        val progressed: Boolean = false,
    ) : UploadOutcome
}

/** The picked file failed while it was being read. */
internal class SourceError(cause: Throwable) : IOException(cause)

/** Sends [length] bytes of the picked file from [input] to [out]; a failing read is a [SourceError]. */
internal fun copySource(input: InputStream, out: OutputStream, length: Long, abort: Abort, bufferSize: Int, onBytes: (Long) -> Unit) {
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

/** What the server's error answer means for the upload. */
internal fun outcomeOf(conn: HttpURLConnection): UploadOutcome {
    val status = conn.responseCode
    val code = errorCode(conn)
    return when {
        status == 401 && code == "session_ended" -> UploadOutcome.PinEnded
        status == 401 -> UploadOutcome.SignedOut
        status == 408 || status == 429 || status >= 500 ->
            UploadOutcome.Retry(null, status, Downloader.retryAfter(conn.getHeaderField("Retry-After")))
        else -> UploadOutcome.Failed(status, code)
    }
}

/** The code of the server's JSON error, if the answer is one (tusd's own are text). */
internal fun errorCode(conn: HttpURLConnection): String? = try {
    val body = conn.errorStream?.use { String(it.readBytes(), Charsets.UTF_8) } ?: return null
    JSONObject(body).optJSONObject("error")?.optString("code")?.ifEmpty { null }
} catch (e: IOException) {
    null
} catch (e: JSONException) {
    null
}
