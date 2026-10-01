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
    /** SHA-256 fingerprints (lowercase hex) of the local address's certificate, when it is https. */
    val pins: List<String>,
    /** From /api/info; the local address must answer with the same id to be used. */
    val serverId: String?,
    /** This phone's id on the server, for the proof a server gives over plain http (HomeProof). */
    val deviceId: String? = null,
) {
    /** An address at home to use: plain http, or https with the server's own pinned certificate. */
    val hasLocal: Boolean get() = !localUrl.isNullOrEmpty() && (isHttp(localUrl) || pins.isNotEmpty())

    /** A server only at home: its public address is plain http too, so the route check asks it. */
    val publicIsHttp: Boolean get() = isHttp(publicUrl)

    /** Whether the route check has anything to ask on this network. */
    val needsProbe: Boolean get() = hasLocal || publicIsHttp

    companion object {
        fun isHttp(url: String?): Boolean = url?.startsWith("http://", ignoreCase = true) == true

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
                    deviceId = if (j.isNull("device_id")) null else j.optString("device_id").ifEmpty { null },
                )
            } catch (e: JSONException) {
                null
            }
        }
    }
}

/**
 * Where [ServerConfig] is kept: plain preferences, since none of it is secret. Two slots: the
 * server this phone is signed in to, and the one it sends to with a PIN.
 */
class ServerStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("server", Context.MODE_PRIVATE)

    fun load(slot: String = DEVICE): String? = prefs.getString(key(slot), null)

    fun save(json: String?, slot: String = DEVICE) {
        prefs.edit(commit = true) { if (json == null) remove(key(slot)) else putString(key(slot), json) }
    }

    fun config(): ServerConfig? = ServerConfig.parse(load())

    fun pinConfig(): ServerConfig? = ServerConfig.parse(load(PIN))

    private fun key(slot: String) = if (slot == PIN) "pin_config" else "config"

    companion object {
        const val DEVICE = "device"
        const val PIN = "pin"
    }
}
