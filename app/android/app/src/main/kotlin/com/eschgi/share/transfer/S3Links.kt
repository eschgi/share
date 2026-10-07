package com.eschgi.share.transfer

import com.eschgi.share.net.S3Connection
import com.eschgi.share.net.readLimited
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection

/**
 * A file's link in the bucket, from the server (GET /api/s3/files/{id}/url), for downloads and
 * for the player: they fetch it without the phone's key. A link works for about 12 hours, for
 * anyone who has it.
 */
object S3Links {
    sealed interface Answer {
        data class Link(val url: String) : Answer

        /** 401: the phone's key, or the PIN, doesn't work any more. */
        data object SignedOut : Answer

        /** 404: deleted since, or not in a folder the caller sees. */
        data object Gone : Answer

        data class Retry(val cause: Throwable?, val status: Int? = null, val retryAfterMs: Long? = null) : Answer

        data class Failed(val status: Int, val code: String?) : Answer
    }

    /** Asks for file [id]'s link with [open] (on the route, with the key). */
    fun fetch(id: String, open: (path: String) -> HttpURLConnection): Answer {
        val conn = try {
            open("/api/s3/files/$id/url")
        } catch (e: IOException) {
            return Answer.Retry(e)
        }
        return try {
            when (val status = conn.responseCode) {
                200 -> {
                    val url = JSONObject(String(readLimited(conn.inputStream, 64 * 1024))).getString("url")
                    if (S3Connection.allowed(url)) Answer.Link(url) else Answer.Failed(200, "insecure_link")
                }
                401 -> Answer.SignedOut
                404, 410 -> Answer.Gone
                408, 429, in 500..599 -> Answer.Retry(null, status, Downloader.retryAfter(conn.getHeaderField("Retry-After")))
                else -> Answer.Failed(status, errorCode(conn))
            }
        } catch (e: JSONException) {
            Answer.Retry(IOException("the server's answer for $id can't be read")) // not its message: it may hold the link
        } catch (e: IOException) {
            Answer.Retry(e)
        } finally {
            conn.disconnect()
        }
    }
}
