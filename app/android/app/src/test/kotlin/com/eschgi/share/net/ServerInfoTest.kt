package com.eschgi.share.net

import com.eschgi.share.TestServer
import com.eschgi.share.data.ServerConfig
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.HttpURLConnection
import java.net.URL

/** What a server says about where its files are, asked once. */
class ServerInfoTest {
    @Volatile private var asked = 0
    @Volatile private var status = 200
    @Volatile private var body = """{"server_id": "s1", "storage": "s3", "chunk_size_bytes": 5242880}"""

    private val server = TestServer { _, res ->
        asked++
        res.send(status, body.toByteArray(), mapOf("Content-Type" to "application/json"))
    }

    private val config get() = ServerConfig("http://127.0.0.1:${server.port}", null, emptyList(), null)

    private fun open(path: String) = URL(config.publicUrl + path).openConnection() as HttpURLConnection

    @After
    fun stop() {
        server.close()
        ServerInfo.forget()
    }

    @Test
    fun asksOnceAndKeepsIt() {
        val s3 = ServerInfo.Info(ServerInfo.Storage.S3, 5L * 1024 * 1024)
        assertFalse(ServerInfo.knownS3(config))
        assertEquals(s3, ServerInfo.of(config, ::open))
        assertEquals(s3, ServerInfo.of(config, ::open))
        assertEquals(1, asked)
        assertTrue(ServerInfo.knownS3(config))
        ServerInfo.forget()
        assertNull(ServerInfo.known(config))
    }

    @Test
    fun aFailedAnswerIsntKept() {
        status = 503
        assertNull(ServerInfo.of(config, ::open))
        assertNull(ServerInfo.known(config))
        status = 200
        body = """{"chunk_size_bytes": 0}"""
        assertEquals(ServerInfo.Info(ServerInfo.Storage.DISK, ServerInfo.DEFAULT_CHUNK), ServerInfo.of(config, ::open))
        assertEquals(2, asked)
    }

    @Test
    fun anythingButS3IsTheDrive() {
        assertEquals(ServerInfo.Storage.DISK, ServerInfo.parse("""{"storage": "disk"}""").storage)
        assertEquals(ServerInfo.Storage.DISK, ServerInfo.parse("{}").storage)
        assertEquals(ServerInfo.Storage.S3, ServerInfo.parse("""{"storage": "s3", "chunk_size_bytes": 20971520}""").storage)
        assertEquals(20L * 1024 * 1024, ServerInfo.parse("""{"chunk_size_bytes": 20971520}""").chunkSize)
    }
}
