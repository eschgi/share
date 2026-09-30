package com.eschgi.share.net

import android.annotation.SuppressLint
import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.concurrent.ConcurrentHashMap
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.X509TrustManager

/**
 * TLS for the local address. Its certificate is self-signed, so instead of the usual checks
 * a connection is trusted only if the certificate's SHA-256 is one of the pins the server
 * handed out over the public address. Only ever set on a single connection, never globally.
 */
object PinnedTls {
    /** The local address showed a certificate that isn't pinned. */
    class PinMismatch(val fingerprint: String) : CertificateException("certificate $fingerprint $NOT_PINNED")

    private const val NOT_PINNED = "is not pinned"

    private val factories = ConcurrentHashMap<List<String>, SSLSocketFactory>()

    /** Lowercase hex SHA-256 of the certificate's DER, as in local_cert_sha256. */
    fun fingerprint(cert: X509Certificate): String =
        MessageDigest.getInstance("SHA-256").digest(cert.encoded).joinToString("") { "%02x".format(it) }

    fun pin(conn: HttpsURLConnection, pins: Collection<String>) {
        conn.sslSocketFactory = socketFactory(pins)
        // The pin already says which certificate this is, so its names don't matter: the
        // address typed on screen 16 may be an IP where the certificate has a host name.
        // The host is still compared, so the connection can't be redirected elsewhere.
        val host = bare(conn.url.host)
        conn.hostnameVerifier = HostnameVerifier { hostname, _ -> bare(hostname).equals(host, ignoreCase = true) }
    }

    /**
     * Whether [e] (or what caused it) is a certificate that isn't pinned. TLS stacks wrap the
     * trust manager's exception differently; some keep only its message.
     */
    fun isMismatch(e: Throwable): Boolean {
        var cause: Throwable? = e
        while (cause != null) {
            if (cause is PinMismatch || cause.message?.contains(NOT_PINNED) == true) return true
            cause = cause.cause
        }
        return false
    }

    /** Cached per set of pins, so connections to the same server can reuse TLS sessions. */
    private fun socketFactory(pins: Collection<String>): SSLSocketFactory =
        factories.getOrPut(pins.map { it.lowercase() }.sorted()) {
            val trust = PinTrustManager(pins.map { it.lowercase() }.toSet())
            SSLContext.getInstance("TLS").apply { init(null, arrayOf(trust), null) }.socketFactory
        }

    private fun bare(host: String) = host.removePrefix("[").removeSuffix("]")

    // Deliberately not the system's checks: the certificate is self-signed, and the pin is
    // the trust decision. It rejects everything that isn't pinned.
    @SuppressLint("CustomX509TrustManager")
    private class PinTrustManager(private val pins: Set<String>) : X509TrustManager {
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
            val leaf = chain?.firstOrNull() ?: throw CertificateException("no certificate")
            val fingerprint = fingerprint(leaf)
            if (fingerprint !in pins) throw PinMismatch(fingerprint)
        }

        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) =
            throw CertificateException("not a server")

        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    }
}
