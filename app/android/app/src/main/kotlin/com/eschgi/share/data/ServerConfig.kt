package com.eschgi.share.data

import android.content.Context
import androidx.core.content.edit
import org.json.JSONException
import org.json.JSONObject

/**
 * The server's addresses on this phone, as the Dart side stores them (ServerConfig.toJson in
 * lib/data/server.dart). Kotlin reads them too, because downloads and the route check run
 * without Flutter.
 */
data class ServerConfig(
    val publicUrl: String,
    val localUrl: String?,
    /** SHA-256 fingerprints (lowercase hex) of the local address's certificate. */
    val pins: List<String>,
    /** From /api/info; the local address must answer with the same id to be used. */
    val serverId: String?,
) {
    val hasLocal: Boolean get() = !localUrl.isNullOrEmpty() && pins.isNotEmpty()

    companion object {
        fun parse(json: String?): ServerConfig? {
            if (json.isNullOrEmpty()) return null
            return try {
                val j = JSONObject(json)
                val public = j.optString("public_url")
                if (public.isEmpty()) return null
                val pins = j.optJSONArray("pins")
                ServerConfig(
                    publicUrl = public.trimEnd('/'),
                    localUrl = j.optString("local_url").trimEnd('/').ifEmpty { null },
                    pins = List(pins?.length() ?: 0) { pins!!.optString(it) }.filter { it.isNotEmpty() },
                    serverId = if (j.isNull("server_id")) null else j.optString("server_id").ifEmpty { null },
                )
            } catch (e: JSONException) {
                null
            }
        }
    }
}

/** Where [ServerConfig] is kept: plain preferences, since none of it is secret. */
class ServerStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("server", Context.MODE_PRIVATE)

    fun load(): String? = prefs.getString(KEY, null)

    fun save(json: String?) {
        prefs.edit(commit = true) { if (json == null) remove(KEY) else putString(KEY, json) }
    }

    fun config(): ServerConfig? = ServerConfig.parse(load())

    private companion object {
        const val KEY = "config"
    }
}
