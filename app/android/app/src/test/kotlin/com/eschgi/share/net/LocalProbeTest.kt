package com.eschgi.share.net

import com.eschgi.share.TestServer
import com.eschgi.share.data.ServerConfig
import com.eschgi.share.selfSigned
import com.eschgi.share.serving
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.ClassRule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.net.ServerSocket
import java.security.KeyStore
import java.security.MessageDigest
import java.security.cert.X509Certificate

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
    fun aBusyServerGetsLongerToAnswerThanToTakeTheConnection() {
        // It took the connection at once; the answer comes after the time a connection gets.
        val port = serve(certA, "server-1", delayMs = 400)
        val status = LocalProbe.probe(config(port, pins = listOf(fingerprintA), serverId = "server-1"), connectMs = 200, answerMs = 3_000)
        assertEquals(Route.LOCAL, status.route)
    }

    @Test
    fun aServerErrorIsNoAnswerYet() {
        // E.g. while the server starts: it is asked again soon (RouteMonitor), not taken for another one.
        val port = serve(certA, "server-1", status = 503)
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

    /** A server with this id, which answers /api/info after [delayMs], with [status]. */
    private fun serve(cert: Pair<KeyStore, X509Certificate>, serverId: String, delayMs: Long = 0, status: Int = 200): Int {
        val server = TestServer(serving(cert)) { req, res ->
            if (req.path != "/api/info") {
                res.send(404)
            } else {
                Thread.sleep(delayMs)
                if (status != 200) {
                    res.send(status)
                } else {
                    res.send(200, """{"server_id":"$serverId","name":"Share","api_version":1}""".toByteArray(), mapOf("Content-Type" to "application/json"))
                }
            }
        }
        servers += server
        return server.port
    }

    companion object {
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
            certA = selfSigned(tmp.root, "a")
            certB = selfSigned(tmp.root, "b")
        }
    }
}
