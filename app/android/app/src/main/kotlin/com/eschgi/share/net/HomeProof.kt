package com.eschgi.share.net

import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * Whether the server at an address over plain http is this phone's own, before the phone sends
 * its key there. Nothing vouches for a server over plain http, and many homes use the same
 * addresses, so the phone asks for an HMAC of a fresh nonce, keyed with the hash of its key,
 * which only its own server keeps (contract/api/home_proof.json).
 */
object HomeProof {
    private val random = SecureRandom()

    /** What the phone's own server answers for [nonce]. */
    fun expected(token: String, nonce: String): String {
        val key = MessageDigest.getInstance("SHA-256").digest(token.toByteArray())
        val mac = Mac.getInstance("HmacSHA256").apply { init(SecretKeySpec(key, "HmacSHA256")) }
        return mac.doFinal("share-home-proof:$nonce".toByteArray()).joinToString("") { "%02x".format(it) }
    }

    /** Asks the server at [base] for the proof; false for a wrong one or no answer. */
    fun check(base: String, deviceId: String, token: String, timeoutMs: Int): Boolean {
        val nonce = Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(24).also { random.nextBytes(it) })
        val conn = try {
            URL("$base/api/home/proof").openConnection() as HttpURLConnection
        } catch (e: IOException) {
            return false
        }
        return try {
            conn.requestMethod = "POST"
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.useCaches = false
            conn.instanceFollowRedirects = false
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/json")
            conn.outputStream.use { it.write(JSONObject().put("device", deviceId).put("nonce", nonce).toString().toByteArray()) }
            if (conn.responseCode != 200) return false
            val proof = JSONObject(String(conn.inputStream.use { readLimited(it, 4 * 1024) })).optString("proof")
            MessageDigest.isEqual(proof.toByteArray(), expected(token, nonce).toByteArray())
        } catch (e: IOException) {
            false
        } catch (e: JSONException) {
            false
        } finally {
            conn.disconnect()
        }
    }
}
