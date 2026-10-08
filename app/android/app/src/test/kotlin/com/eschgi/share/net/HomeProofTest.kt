package com.eschgi.share.net

import com.eschgi.share.TestServer
import com.eschgi.share.contract
import com.eschgi.share.data.ServerConfig
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.MessageDigest
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * The address at home over plain http: nothing vouches for the server there, so the probe
 * asks it for the proof that only the phone's own server can give.
 */
class HomeProofTest {
    private val servers = mutableListOf<TestServer>()

    @After
    fun stop() = servers.forEach { it.close() }

    @Test
    fun theProofMatchesTheServers() {
        val ex = contract("api/home_proof.json").getJSONObject("example")
        assertEquals(ex.getString("proof"), HomeProof.expected(ex.getString("token"), ex.getString("nonce")))
    }

    @Test
    fun homeAddressWithTheRightProof() {
        val port = serve(SERVER_ID, knows = TOKEN)
        val status = LocalProbe.probe(home(port), TOKEN)
        assertEquals(Route.LOCAL, status.route)
    }

    @Test
    fun aServerThatOnlyRepeatsTheIdIsNotUsed() {
        // It answers /api/info like the real server, but can't know the phone's key.
        val port = serve(SERVER_ID, knows = "shd_someoneElsesKey000000000000000000000000000")
        val status = LocalProbe.probe(home(port), TOKEN)
        assertEquals(Route.PUBLIC, status.route)
        assertEquals(RouteReason.OTHER_SERVER, status.reason)
    }

    @Test
    fun withoutTheKeyNothingIsUsed() {
        val port = serve(SERVER_ID, knows = TOKEN)
        assertEquals(RouteReason.OTHER_SERVER, LocalProbe.probe(home(port), token = null).reason)
        assertEquals(RouteReason.OTHER_SERVER, LocalProbe.probe(home(port).copy(deviceId = null), TOKEN).reason)
    }

    @Test
    fun anotherServerAtTheSameAddress() {
        val port = serve("another-server", knows = TOKEN)
        assertEquals(RouteReason.OTHER_SERVER, LocalProbe.probe(home(port), TOKEN).reason)
    }

    @Test
    fun aProofThatDoesNotComeIsNoAnswer() {
        // Too busy to answer in time says nothing about whose server it is: it is asked again
        // soon (RouteMonitor), not taken for another one.
        val port = serve(SERVER_ID, knows = TOKEN, proofDelayMs = 1_000)
        assertEquals(RouteReason.UNREACHABLE, LocalProbe.probe(home(port), TOKEN, answerMs = 200).reason)
        val failing = serve(SERVER_ID, knows = TOKEN, proofStatus = 503)
        assertEquals(RouteReason.UNREACHABLE, LocalProbe.probe(home(failing), TOKEN).reason)
    }

    @Test
    fun aPublicAddressOverPlainHttpIsCheckedToo() {
        val port = serve(SERVER_ID, knows = TOKEN)
        val onlyHome = ServerConfig("http://127.0.0.1:$port", null, emptyList(), SERVER_ID, DEVICE)
        val status = LocalProbe.probe(onlyHome, TOKEN)
        assertEquals(RouteReason.NO_LOCAL, status.reason)
        assertTrue(status.publicVerified)

        val impostor = serve(SERVER_ID, knows = "shd_someoneElsesKey000000000000000000000000000")
        assertFalse(LocalProbe.probe(onlyHome.copy(publicUrl = "http://127.0.0.1:$impostor"), TOKEN).publicVerified)
        assertFalse(LocalProbe.probe(onlyHome.copy(publicUrl = "http://127.0.0.1:1"), TOKEN).publicVerified)
    }

    private fun home(port: Int) = ServerConfig("https://share.example.com", "http://127.0.0.1:$port", emptyList(), SERVER_ID, DEVICE)

    /**
     * A plain-http server with this id, which gives proofs for the key it [knows]: after
     * [proofDelayMs], or none but [proofStatus].
     */
    private fun serve(serverId: String, knows: String, proofDelayMs: Long = 0, proofStatus: Int = 200): Int {
        val server = TestServer { req, res ->
            when (req.path) {
                "/api/info" -> res.send(200, """{"server_id":"$serverId","name":"Share","api_version":1}""".toByteArray())
                "/api/home/proof" -> {
                    val body = JSONObject(String(req.body.readNBytes(req.contentLength.toInt())))
                    Thread.sleep(proofDelayMs)
                    if (proofStatus != 200) {
                        res.send(proofStatus)
                    } else if (body.getString("device") != DEVICE) {
                        res.send(404)
                    } else {
                        res.send(200, JSONObject().put("proof", proof(knows, body.getString("nonce"))).toString().toByteArray())
                    }
                }
                else -> res.send(404)
            }
        }
        servers += server
        return server.port
    }

    companion object {
        private const val TOKEN = "shd_9xQe3vR2mK7pL1sT8wY4zA6bC0dF5gH2jN3kM8qR1sT"
        private const val DEVICE = "dv6kq3zt7wbxl2m4nf5yh8rc"
        private const val SERVER_ID = "server-1"

        /** The server's side of the proof, written out apart from HomeProof. */
        private fun proof(token: String, nonce: String): String {
            val key = MessageDigest.getInstance("SHA-256").digest(token.toByteArray())
            val mac = Mac.getInstance("HmacSHA256").apply { init(SecretKeySpec(key, "HmacSHA256")) }
            return mac.doFinal("share-home-proof:$nonce".toByteArray()).joinToString("") { "%02x".format(it) }
        }
    }
}
