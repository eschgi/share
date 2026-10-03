package com.eschgi.share.transfer

import android.content.Context
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerConfig
import com.eschgi.share.data.ServerStore

/**
 * Whose key a transfer goes with: the signed-in phone's ([DEVICE]), over the route RouteMonitor
 * picked, or a PIN's ([PIN]), always over the public address.
 */
object Credentials {
    const val DEVICE = "device"
    const val PIN = "pin"

    /** The key and the server for [auth]; null without them (signed out, or no PIN). */
    fun of(context: Context, auth: String): Pair<String, ServerConfig>? {
        val token = SecretStore(context).read(if (auth == PIN) SecretStore.PIN_TOKEN else SecretStore.DEVICE_TOKEN) ?: return null
        val servers = ServerStore(context)
        val config = (if (auth == PIN) servers.pinConfig() else servers.config()) ?: return null
        return token to config
    }

    /** Whether [auth] goes over the address at home when the route says it answers. */
    fun atHome(auth: String, routeIsLocal: Boolean) = auth == DEVICE && routeIsLocal
}
