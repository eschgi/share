package com.eschgi.share.data

import org.json.JSONException
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/**
 * The Cloudflare Access service token the app shows Cloudflare in front of the server
 * (docs/cloudflare-access-plan.md), as the Dart side keeps it in [SecretStore] under
 * [SecretStore.CLOUDFLARE_ACCESS] (CloudflareAccess in lib/data/access.dart). It goes only to
 * the public address of the server at [host], over https: never to the local address, which
 * skips Cloudflare, or to a bucket.
 */
data class CloudflareAccess(val host: String, val clientId: String, val clientSecret: String) {

    fun appliesTo(base: String): Boolean {
        val url = try {
            URL(base)
        } catch (e: java.net.MalformedURLException) {
            return false
        }
        return url.protocol.equals("https", ignoreCase = true) && url.host.equals(host, ignoreCase = true)
    }

    /** Adds the two headers to a request to [base], if the token belongs there. */
    fun applyTo(conn: HttpURLConnection, base: String) {
        if (!appliesTo(base)) return
        conn.setRequestProperty("CF-Access-Client-Id", clientId)
        conn.setRequestProperty("CF-Access-Client-Secret", clientSecret)
    }

    override fun toString() = "CloudflareAccess($host, $clientId)" // never the secret

    companion object {
        /** The stored JSON; null without a token, or with one that's switched off. */
        fun parse(json: String?): CloudflareAccess? {
            if (json.isNullOrEmpty()) return null
            return try {
                val j = JSONObject(json)
                if (!j.optBoolean("on", true)) return null
                val host = j.optString("host")
                val id = j.optString("client_id")
                val secret = j.optString("client_secret")
                if (host.isEmpty() || id.isEmpty() || secret.isEmpty()) null else CloudflareAccess(host.lowercase(), id, secret)
            } catch (e: JSONException) {
                null
            }
        }
    }
}
