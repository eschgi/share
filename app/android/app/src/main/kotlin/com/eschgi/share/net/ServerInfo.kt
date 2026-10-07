package com.eschgi.share.net

import com.eschgi.share.data.ServerConfig
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.util.concurrent.ConcurrentHashMap

/**
 * What a server's /api/info says that transfers need: where its files are (on its drive, sent
 * over tus, or in a bucket, sent and fetched there directly) and how big a piece may be. Asked
 * once per server while the app runs, and only when a transfer needs it, so files already on
 * the phone still open offline. A failed answer isn't kept.
 */
object ServerInfo {
    enum class Storage { DISK, S3 }

    data class Info(val storage: Storage, val chunkSize: Long)

    /** What the server takes in one request when it doesn't say. */
    const val DEFAULT_CHUNK = 20L * 1024 * 1024

    private val known = ConcurrentHashMap<String, Info>()

    /** The server's info: known already, or asked with [open] (the caller's route); null when it can't be asked now. */
    fun of(config: ServerConfig, open: (path: String) -> HttpURLConnection): Info? {
        known[config.publicUrl]?.let { return it }
        return try {
            val conn = open("/api/info")
            try {
                if (conn.responseCode != 200) return null
                parse(String(readLimited(conn.inputStream, 64 * 1024))).also { known[config.publicUrl] = it }
            } finally {
                conn.disconnect()
            }
        } catch (e: IOException) {
            null
        } catch (e: JSONException) {
            null
        }
    }

    /** What is known about the server already, without asking. */
    fun known(config: ServerConfig?): Info? = config?.let { known[it.publicUrl] }

    /** Whether the server's files are in a bucket, as far as is known. */
    fun knownS3(config: ServerConfig?): Boolean = known(config)?.storage == Storage.S3

    /** Forgets what servers said, when the phone's server changes. */
    fun forget() = known.clear()

    internal fun parse(json: String): Info {
        val o = JSONObject(json)
        val chunk = o.optLong("chunk_size_bytes")
        return Info(if (o.optString("storage") == "s3") Storage.S3 else Storage.DISK, if (chunk > 0) chunk else DEFAULT_CHUNK)
    }
}
