package com.eschgi.share.transfer

import com.eschgi.share.TestServer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.random.Random

/** Uploads against a small tus server that behaves like the server's /tus/. */
class TusUploaderTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val data = Random(3).nextBytes(3 * 1024 * 1024 + 5)

    /** The uploads the server has: their length, the folder they go into and the bytes so far. */
    private class Upload(val length: Long, val name: String, val folder: String? = null, val lastModified: String? = null) {
        val bytes = ByteArrayOutputStream()
    }

    private val uploads = ConcurrentHashMap<String, Upload>()
    private val requests = CopyOnWriteArrayList<String>()
    private var next = 0

    /** Set by tests: answers for the next requests, before the tus logic sees them. */
    @Volatile private var interfere: ((TestServer.Request, TestServer.Response) -> Boolean)? = null

    private val server = TestServer { req, res ->
        // The JDK can't send PATCH; the tests' connections send POST and say so.
        val method = req.header("X-Test-Method") ?: req.method
        requests += "$method ${req.path}"
        if (interfere?.invoke(req, res) == true) return@TestServer
        val id = req.path.removePrefix("/tus/")
        when {
            method == "POST" && req.path == "/tus/" -> {
                val new = "u${next++}"
                val meta = req.header("Upload-Metadata")!!.split(',').associate { it.substringBefore(' ') to it.substringAfter(' ') }
                fun text(key: String) = meta[key]?.let { String(Base64.getDecoder().decode(it)) }
                uploads[new] = Upload(req.header("Upload-Length")!!.toLong(), text("filename")!!, text("folder"), text("lastModified"))
                res.send(201, headers = mapOf("Location" to "/tus/$new"))
            }
            method == "HEAD" -> {
                val u = uploads[id] ?: return@TestServer res.send(404)
                res.send(200, headers = mapOf("Upload-Offset" to u.bytes.size().toString(), "Upload-Length" to u.length.toString()))
            }
            method == "PATCH" -> {
                val u = uploads[id] ?: return@TestServer res.send(404)
                if (req.header("Upload-Offset")!!.toLong() != u.bytes.size().toLong()) return@TestServer res.send(409)
                // Keeps what arrived even if the connection breaks, like tusd.
                val buffer = ByteArray(64 * 1024)
                var left = req.contentLength
                try {
                    while (left > 0) {
                        val n = req.body.read(buffer, 0, minOf(buffer.size.toLong(), left).toInt())
                        if (n < 0) break
                        synchronized(u) { u.bytes.write(buffer, 0, n) }
                        left -= n
                    }
                } catch (e: IOException) {
                    return@TestServer
                }
                res.send(204, headers = mapOf("Upload-Offset" to u.bytes.size().toString()))
            }
            else -> res.send(404)
        }
    }

    @After
    fun stop() = server.close()

    private fun open(method: String, path: String): HttpURLConnection =
        (URL("http://127.0.0.1:${server.port}$path").openConnection() as HttpURLConnection).apply {
            connectTimeout = 2_000
            readTimeout = 5_000
            if (method == "PATCH") {
                requestMethod = "POST"
                setRequestProperty("X-Test-Method", "PATCH")
            } else {
                requestMethod = method
            }
        }

    private fun file(bytes: ByteArray = data) = File(tmp.root, "IMG_1.jpg").apply { writeBytes(bytes) }

    private fun upload(
        uploadId: String? = null,
        bytes: ByteArray = data,
        source: UploadSource = FileSource(file(bytes)),
        abort: Abort = Abort(),
        created: MutableList<String> = mutableListOf(),
        folder: String? = null,
        lastModified: Long? = null,
        onBytes: (Long) -> Unit = {},
    ) = TusUploader(bufferSize = 64 * 1024).upload(
        "IMG_1.jpg", "image/jpeg", bytes.size.toLong(), source, uploadId, 1024 * 1024, ::open, abort, { created += it }, onBytes, folder, lastModified,
    )

    @Test
    fun inChunks() {
        val created = mutableListOf<String>()
        val seen = mutableListOf<Long>()
        val outcome = upload(created = created) { seen += it }
        assertEquals(UploadOutcome.Done("u0"), outcome)
        assertEquals(listOf("u0"), created)
        assertArrayEquals(data, uploads.getValue("u0").bytes.toByteArray())
        assertEquals("IMG_1.jpg", uploads.getValue("u0").name)
        assertEquals(null, uploads.getValue("u0").folder) // a PIN sends into its own
        assertEquals(listOf("POST /tus/", "PATCH /tus/u0", "PATCH /tus/u0", "PATCH /tus/u0", "PATCH /tus/u0"), requests)
        assertEquals(data.size.toLong(), seen.last())
    }

    @Test
    fun intoAFolder() {
        assertEquals(UploadOutcome.Done("u0"), upload(folder = "f4mily5x2k7mbqz4bwdbyj6qsq"))
        assertEquals("f4mily5x2k7mbqz4bwdbyj6qsq", uploads.getValue("u0").folder)
        assertEquals(null, uploads.getValue("u0").lastModified) // not known: not sent
    }

    @Test
    fun withTheFilesTime() {
        assertEquals(UploadOutcome.Done("u0"), upload(lastModified = 1758960000000))
        assertEquals("1758960000000", uploads.getValue("u0").lastModified)
    }

    @Test
    fun anUploadTheServerForgotStartsOverInItsFolder() {
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "gone", folder = "w3dding5x2k7mbqz4bwdbyj6qs"))
        assertEquals("w3dding5x2k7mbqz4bwdbyj6qs", uploads.getValue("u0").folder)
    }

    @Test
    fun goesOnWhereTheServerHasIt() {
        uploads["u9"] = Upload(data.size.toLong(), "IMG_1.jpg").apply { bytes.write(data, 0, 2_000_000) }
        assertEquals(UploadOutcome.Done("u9"), upload(uploadId = "u9"))
        assertArrayEquals(data, uploads.getValue("u9").bytes.toByteArray())
        assertEquals(listOf("HEAD /tus/u9", "PATCH /tus/u9", "PATCH /tus/u9"), requests)
    }

    @Test
    fun aBrokenConnectionKeepsWhatArrived() {
        var cut = true
        interfere = { req, _ ->
            val patch = req.header("X-Test-Method") == "PATCH"
            if (patch && cut && uploads.getValue("u0").bytes.size() > 0) {
                // Read part of the second chunk, then drop the connection.
                cut = false
                val part = ByteArray(300_000)
                var got = 0
                while (got < part.size) got += req.body.read(part, got, part.size - got).coerceAtLeast(0)
                uploads.getValue("u0").bytes.write(part)
                throw IOException("gone")
            }
            false
        }
        val first = upload()
        assertTrue("$first", first is UploadOutcome.Retry && first.progressed)
        interfere = null
        requests.clear()
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "u0"))
        assertArrayEquals(data, uploads.getValue("u0").bytes.toByteArray())
        assertEquals("HEAD /tus/u0", requests.first())
    }

    @Test
    fun anOffsetConflictAsksAgain() {
        uploads["u9"] = Upload(data.size.toLong(), "IMG_1.jpg")
        var conflicts = 1
        interfere = { req, res ->
            if (req.header("X-Test-Method") == "PATCH" && conflicts-- > 0) {
                // Read the body first: answering early would race the client still writing it.
                var left = req.contentLength
                val sink = ByteArray(64 * 1024)
                while (left > 0) {
                    val n = req.body.read(sink, 0, minOf(sink.size.toLong(), left).toInt())
                    if (n < 0) break
                    left -= n
                }
                res.send(409)
                true
            } else {
                false
            }
        }
        assertEquals(UploadOutcome.Done("u9"), upload(uploadId = "u9"))
        assertEquals(listOf("HEAD /tus/u9", "PATCH /tus/u9", "HEAD /tus/u9"), requests.take(3))
    }

    @Test
    fun anUploadTheServerForgotStartsOver() {
        val created = mutableListOf<String>()
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "gone", created = created))
        assertEquals(listOf("u0"), created)
        assertArrayEquals(data, uploads.getValue("u0").bytes.toByteArray())
    }

    @Test
    fun answersThatEndIt() {
        fun answer(status: Int, code: String? = null): UploadOutcome {
            interfere = { _, res ->
                val body = code?.let { """{"error":{"code":"$it","message":""}}""".toByteArray() } ?: ByteArray(0)
                res.send(status, body, mapOf("Content-Type" to "application/json", "Retry-After" to "4"))
                true
            }
            return upload()
        }
        assertEquals(UploadOutcome.PinEnded, answer(401, "session_ended"))
        assertEquals(UploadOutcome.SignedOut, answer(401, "signed_out"))
        assertEquals(UploadOutcome.Failed(413, "too_large"), answer(413, "too_large"))
        assertEquals(UploadOutcome.Failed(404, "folder_gone"), answer(404, "folder_gone"))
        assertEquals(UploadOutcome.Failed(403, "no_folder"), answer(403, "no_folder"))
        assertEquals(UploadOutcome.Retry(null, 503, 4000), answer(503))
    }

    @Test
    fun anEmptyFileIsDoneWhenMade() {
        assertEquals(UploadOutcome.Done("u0"), upload(bytes = ByteArray(0)))
        assertEquals(listOf("POST /tus/"), requests)
    }

    @Test
    fun aFileThatCantBeReadHasToBePickedAgain() {
        val gone = object : UploadSource {
            override fun openAt(offset: Long) = throw SecurityException("no permission any more")
        }
        assertEquals(UploadOutcome.Lost, upload(source = gone))
    }

    @Test
    fun stopsMidway() {
        val abort = Abort()
        val outcome = upload(abort = abort) { if (it > 1_500_000) abort.stop() }
        assertEquals(UploadOutcome.Stopped, outcome)
        assertTrue(uploads.getValue("u0").bytes.size() < data.size)
    }
}
