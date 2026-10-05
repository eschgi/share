package com.eschgi.share.net

import com.eschgi.share.BuildConfig
import java.io.IOException
import java.net.HttpURLConnection
import java.net.MalformedURLException
import java.net.URL

/**
 * Requests to a bucket, with the links Share's server signs: the phone's key never goes there.
 * Over https with the system's trust; plain http only to an address at home, such as MinIO in
 * the same network. A link works for anyone who has it, so it never ends up in an error or the
 * log with its query.
 */
object S3Connection {
    /** A link to plain http outside home networks: it isn't followed. */
    class InsecureLink(link: String) : IOException("$link: plain http is only for a bucket at home")

    /** Whether the app may use a link: https, or http to an address at home. */
    fun allowed(url: String): Boolean = try {
        val u = URL(url)
        u.protocol == "https" || (u.protocol == "http" && isHomeHost(u.host))
    } catch (e: MalformedURLException) {
        false
    }

    /** A request to the link exactly as given, with nothing of the phone's but its name. */
    fun open(url: String, method: String = "GET", readTimeoutMs: Int = 60_000): HttpURLConnection {
        if (!allowed(url)) throw InsecureLink(redact(url))
        val conn = URL(url).openConnection() as HttpURLConnection
        conn.requestMethod = method
        conn.connectTimeout = 15_000
        conn.readTimeout = readTimeoutMs
        conn.useCaches = false
        conn.instanceFollowRedirects = false
        conn.setRequestProperty("User-Agent", "Share-Android/${BuildConfig.VERSION_NAME}")
        return conn
    }

    /** A link without its query: where it leads, without what lets one in. */
    fun redact(url: String) = url.substringBefore('?')

    /** An error with every link in its message cut at the query; Android puts the whole link into some. */
    fun scrub(e: Throwable): IOException = IOException("${e.javaClass.simpleName}: ${LINK.replace(e.message.orEmpty()) { redact(it.value) }}")

    private val LINK = Regex("""https?://\S+""")
}
