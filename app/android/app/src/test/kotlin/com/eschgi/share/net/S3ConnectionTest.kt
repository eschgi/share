package com.eschgi.share.net

import com.eschgi.share.TestServer
import com.eschgi.share.selfSigned
import com.eschgi.share.serving
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.FileNotFoundException
import javax.net.ssl.SSLException

/** Requests to a bucket: the link as it is, nothing of the phone's but its name. */
class S3ConnectionTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val servers = mutableListOf<TestServer>()
    private val seen = mutableListOf<Pair<String, String?>>()

    @After
    fun stop() = servers.forEach { it.close() }

    private fun bucket(): Int {
        val server = TestServer { req, res ->
            seen += req.path to req.header("Authorization")
            if (req.path.startsWith("/moved")) res.send(302, headers = mapOf("Location" to "https://elsewhere.example.com/")) else res.send(200, "bytes".toByteArray())
        }
        servers += server
        return server.port
    }

    @Test
    fun onlyHttpsOrAnAddressAtHome() {
        assertTrue(S3Connection.allowed("https://acct.r2.cloudflarestorage.com/share/files/x?X-Amz-Signature=1"))
        assertTrue(S3Connection.allowed("http://192.168.8.52:9000/share/files/x"))
        assertTrue(S3Connection.allowed("http://[fd12::52]:9000/share/files/x"))
        assertFalse(S3Connection.allowed("http://bucket.example.com/share/files/x"))
        assertFalse(S3Connection.allowed("ftp://192.168.8.52/x"))
        assertFalse(S3Connection.allowed("not a link"))
        try {
            S3Connection.open("http://bucket.example.com/share/files/x?X-Amz-Signature=secret")
            fail("a plain http link outside home")
        } catch (e: S3Connection.InsecureLink) {
            assertFalse(e.message!!.contains("secret"))
        }
    }

    @Test
    fun sendsTheLinkAsItIsWithoutTheKey() {
        val port = bucket()
        val query = "X-Amz-Credential=AKIA%2F20261005%2Fauto%2Fs3%2Faws4_request&response-content-disposition=attachment%3B%20filename%3D%22a%20b.jpg%22&X-Amz-Signature=abc"
        val conn = S3Connection.open("http://127.0.0.1:$port/share/files/x?$query")
        assertEquals(200, conn.responseCode)
        assertEquals("bytes", conn.inputStream.use { String(it.readBytes()) })
        assertEquals("/share/files/x?$query" to null, seen.single())

        val moved = S3Connection.open("http://127.0.0.1:$port/moved")
        assertEquals(302, moved.responseCode) // not followed
    }

    @Test
    fun trustsOnlyWhatThePhoneTrusts() {
        val server = TestServer(serving(selfSigned(tmp.root, "bucket"))) { _, res -> res.send(200) }
        servers += server
        try {
            S3Connection.open("https://127.0.0.1:${server.port}/x").responseCode
            fail("a self-signed bucket")
        } catch (e: SSLException) {
            // refused
        }
    }

    @Test
    fun linksLeaveErrorsWithoutTheirQuery() {
        assertEquals("https://acct.r2.cloudflarestorage.com/share/files/x", S3Connection.redact("https://acct.r2.cloudflarestorage.com/share/files/x?X-Amz-Signature=secret"))
        val e = S3Connection.scrub(FileNotFoundException("https://acct.r2.cloudflarestorage.com/share/files/x?X-Amz-Signature=secret"))
        assertEquals("FileNotFoundException: https://acct.r2.cloudflarestorage.com/share/files/x", e.message)
        assertNull(S3Connection.scrub(RuntimeException()).cause)
    }
}
