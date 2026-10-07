package com.eschgi.share.transfer

import com.eschgi.share.TestServer
import com.eschgi.share.contract
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.HttpURLConnection
import java.net.URL

/** The server's answer to GET /api/s3/files/{id}/url, as downloads and the player read it. */
class S3LinksTest {
    @Volatile private var answer: Triple<Int, String, Map<String, String>> = Triple(200, contract("api/s3_file_url.json").getJSONObject("response").toString(), emptyMap())

    private val server = TestServer { _, res ->
        val (status, body, headers) = answer
        res.send(status, body.toByteArray(), mapOf("Content-Type" to "application/json") + headers)
    }

    @After
    fun stop() = server.close()

    private fun fetch() = S3Links.fetch("q3ld5x2k7mbqz4bwdbyj6qsqxa") { path ->
        (URL("http://127.0.0.1:${server.port}$path").openConnection() as HttpURLConnection).apply { readTimeout = 5_000 }
    }

    private fun error(code: String) = """{"error":{"code":"$code","message":""}}"""

    @Test
    fun readsTheLink() {
        val link = fetch()
        assertTrue("$link", link is S3Links.Answer.Link && link.url == contract("api/s3_file_url.json").getJSONObject("response").getString("url"))
    }

    @Test
    fun saysWhyThereIsNone() {
        answer = Triple(401, error("signed_out"), emptyMap())
        assertEquals(S3Links.Answer.SignedOut, fetch())
        answer = Triple(404, error("not_found"), emptyMap())
        assertEquals(S3Links.Answer.Gone, fetch())
        answer = Triple(503, error("internal"), mapOf("Retry-After" to "4"))
        assertEquals(S3Links.Answer.Retry(null, 503, 4000), fetch())
        answer = Triple(403, error("forbidden"), emptyMap())
        assertEquals(S3Links.Answer.Failed(403, "forbidden"), fetch())
        answer = Triple(200, """{"url": "http://bucket.example.com/share/files/x?X-Amz-Signature=secret"}""", emptyMap())
        assertEquals(S3Links.Answer.Failed(200, "insecure_link"), fetch())
        answer = Triple(200, """{"url": "https://bucket/x?X-Amz-Signature=secret" """, emptyMap())
        val garbled = fetch()
        assertTrue("$garbled", garbled is S3Links.Answer.Retry && !garbled.cause.toString().contains("secret"))
        assertFalse(garbled.toString().contains("secret"))
    }
}
