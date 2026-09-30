package com.eschgi.share.net

import com.eschgi.share.TestServer
import com.eschgi.share.data.ServerConfig
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.ClassRule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.net.ServerSocket
import java.security.KeyStore
import java.security.MessageDigest
import java.security.cert.X509Certificate
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext

/**
 * The local address check against a real TLS server with a self-signed certificate, like
 * the one `share` makes for its local listener.
 */
class LocalProbeTest {
    private val servers = mutableListOf<TestServer>()

    @After
    fun stop() = servers.forEach { it.close() }

    @Test
    fun pinnedCertificateAndSameServer() {
        val port = serve(certA, "server-1")
        val status = LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = "server-1"))
        assertEquals(Route.LOCAL, status.route)
        assertTrue(status.millis!! >= 0)
    }

    @Test
    fun anyPinOfTheListCounts() {
        // Current and next certificate, while the server rotates it.
        val port = serve(certA, "server-1")
        assertEquals(Route.LOCAL, LocalProbe.probe(config(port, pins = listOf(fingerprintB, fingerprintA), serverId = "server-1")).route)
    }

    @Test
    fun anotherCertificateIsNotTrusted() {
        val port = serve(certB, "server-1")
        assertEquals(
            RouteStatus(Route.PUBLIC, RouteReason.WRONG_CERTIFICATE),
            LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = "server-1")),
        )
    }

    @Test
    fun anotherServer() {
        val port = serve(certA, "server-2")
        assertEquals(
            RouteStatus(Route.PUBLIC, RouteReason.OTHER_SERVER),
            LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = "server-1")),
        )
    }

    @Test
    fun withoutAKnownServerIdThePinDecides() {
        val port = serve(certA, "server-2")
        assertEquals(Route.LOCAL, LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = null)).route)
    }

    @Test
    fun nothingAnswers() {
        val port = ServerSocket(0).use { it.localPort }
        assertEquals(
            RouteStatus(Route.PUBLIC, RouteReason.UNREACHABLE),
            LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = "server-1")),
        )
    }

    @Test
    fun noLocalAddress() {
        assertEquals(RouteStatus.NO_LOCAL, LocalProbe.probe(ServerConfig("https://share.example.com", null, emptyList(), null)))
        // An address without pins can't be trusted, so it isn't tried.
        assertEquals(RouteStatus.NO_LOCAL, LocalProbe.probe(config(8443, pins = emptyList(), serverId = null)))
    }

    @Test
    fun fingerprintIsTheSha256OfTheDer() {
        val expected = MessageDigest.getInstance("SHA-256").digest(certA.second.encoded).joinToString("") { "%02x".format(it) }
        assertEquals(expected, fingerprintA)
        assertTrue(Regex("[0-9a-f]{64}").matches(fingerprintA))
    }

    private fun config(port: Int, pins: List<String>, serverId: String?) =
        ServerConfig("https://share.example.com", "https://127.0.0.1:$port", pins, serverId)

    private fun serve(cert: Pair<KeyStore, X509Certificate>, serverId: String): Int {
        val keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()).apply { init(cert.first, PASS) }
        val tls = SSLContext.getInstance("TLS").apply { init(keys.keyManagers, null, null) }
        val server = TestServer(tls) { req, res ->
            if (req.path == "/api/info") {
                res.send(200, """{"server_id":"$serverId","name":"Share","api_version":1}""".toByteArray(), mapOf("Content-Type" to "application/json"))
            } else {
                res.send(404)
            }
        }
        servers += server
        return server.port
    }

    companion object {
        private val PASS = "test-only".toCharArray()

        @get:ClassRule
        @JvmStatic
        val tmp = TemporaryFolder()

        private lateinit var certA: Pair<KeyStore, X509Certificate>
        private lateinit var certB: Pair<KeyStore, X509Certificate>
        private val fingerprintA get() = PinnedTls.fingerprint(certA.second)
        private val fingerprintB get() = PinnedTls.fingerprint(certB.second)

        @BeforeClass
        @JvmStatic
        fun certificates() {
            certA = selfSigned("a")
            certB = selfSigned("b")
        }

        /** A throwaway self-signed EC certificate from the JDK's keytool, so no key is committed. */
        private fun selfSigned(name: String): Pair<KeyStore, X509Certificate> {
            val file = File(tmp.root, "$name.p12")
            val keytool = File(System.getProperty("java.home"), "bin/keytool").path
            val process = ProcessBuilder(
                keytool, "-genkeypair", "-alias", "local", "-keyalg", "EC", "-groupname", "secp256r1",
                "-validity", "2", "-dname", "CN=share-test-$name", "-ext", "SAN=ip:127.0.0.1",
                "-keystore", file.path, "-storetype", "PKCS12", "-storepass", String(PASS), "-keypass", String(PASS),
            ).redirectErrorStream(true).start()
            val output = process.inputStream.bufferedReader().readText()
            check(process.waitFor() == 0) { "keytool failed: $output" }
            val store = KeyStore.getInstance("PKCS12").apply { file.inputStream().use { load(it, PASS) } }
            return store to (store.getCertificate("local") as X509Certificate)
        }
    }
}
