package com.eschgi.share.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import com.eschgi.share.BuildConfig
import com.eschgi.share.data.ServerConfig
import java.net.HttpURLConnection
import java.net.URL
import javax.net.ssl.HttpsURLConnection

/** Requests to this phone's server with its key, over the local or the public address. */
class ServerConnection(private val config: ServerConfig, private val token: String?) {

    fun open(path: String, local: Boolean, readTimeoutMs: Int = 60_000): HttpURLConnection {
        val useLocal = local && config.hasLocal
        val base = if (useLocal) config.localUrl!! else config.publicUrl
        val conn = URL(base + path).openConnection() as HttpURLConnection
        if (useLocal) PinnedTls.pin(conn as HttpsURLConnection, config.pins)
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
        /** Whether the phone has any network at all; without one, waiting beats retrying. */
        fun online(context: Context): Boolean {
            val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return true
            val network = connectivity.activeNetwork ?: return false
            return connectivity.getNetworkCapabilities(network)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) ?: false
        }
    }
}
