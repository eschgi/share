package com.eschgi.share.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import com.eschgi.share.BuildConfig
import com.eschgi.share.data.ServerConfig
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import javax.net.ssl.HttpsURLConnection

/** Requests to this phone's server with its key, over the local or the public address. */
class ServerConnection(val config: ServerConfig, private val token: String?) {

    fun open(path: String, local: Boolean, readTimeoutMs: Int = 60_000, method: String = "GET"): HttpURLConnection {
        val useLocal = local && config.hasLocal
        val base = if (useLocal) config.localUrl!! else config.publicUrl
        // Over plain http the phone's key goes only to a server that proved here to be its own.
        if (ServerConfig.isHttp(base) && token?.startsWith(DEVICE_TOKEN_PREFIX) == true && !RouteMonitor.httpAllowed(config, useLocal)) {
            throw IOException("$base hasn't shown to be this phone's server on this network")
        }
        val conn = URL(base + path).openConnection() as HttpURLConnection
        conn.requestMethod = method // Android's HttpURLConnection takes PATCH too, which tus needs
        if (useLocal && conn is HttpsURLConnection) PinnedTls.pin(conn, config.pins)
        conn.connectTimeout = if (useLocal) 3_000 else 15_000
        conn.readTimeout = readTimeoutMs
        conn.useCaches = false
        // The API never redirects, and a redirect must not carry the key somewhere else.
        conn.instanceFollowRedirects = false
        conn.setRequestProperty("User-Agent", "Share-Android/${BuildConfig.VERSION_NAME}")
        token?.let { conn.setRequestProperty("Authorization", "Bearer $it") }
        return conn
    }

    companion object {
        /** A signed-in phone's key; PIN sessions' keys (shp_) only let people send. */
        private const val DEVICE_TOKEN_PREFIX = "shd_"

        /** Whether the phone has any network at all; without one, waiting beats retrying. */
        fun online(context: Context): Boolean {
            val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return true
            val network = connectivity.activeNetwork ?: return false
            return connectivity.getNetworkCapabilities(network)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) ?: false
        }
    }
}
