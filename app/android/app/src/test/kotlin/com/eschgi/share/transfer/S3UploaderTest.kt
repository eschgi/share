package com.eschgi.share.transfer

import com.eschgi.share.TestServer
import com.eschgi.share.contract
import org.json.JSONArray
import org.json.JSONObject
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
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.random.Random

/** Uploads against a small server that plays both Share's /api/s3/uploads and the bucket. */
class S3UploaderTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val partSize = 1024L * 1024
    private val data = Random(5).nextBytes(3 * 1024 * 1024 + 5)

    private class Upload(val id: String, val body: JSONObject, val size: Long, val partSize: Long) {
        val parts = ConcurrentHashMap<Int, ByteArray>()
        @Volatile var complete = false
    }

    private val uploads = ConcurrentHashMap<String, Upload>()
    private val requests = CopyOnWriteArrayList<String>()
    private val issued = ConcurrentHashMap.newKeySet<String>()
    private val problems = CopyOnWriteArrayList<String>()
    private var next = 0
    private var version = 0

    /** Where the links point: this server, or e.g. plain http on the internet. */
    @Volatile private var bucketBase: String? = null

    /** Set by tests: answers for the next requests, before the logic sees them. */
    @Volatile private var interfere: ((TestServer.Request, TestServer.Response) -> Boolean)? = null

    private val server = TestServer { req, res ->
        val path = req.path.substringBefore('?')
        requests += "${req.method} ${if (path.startsWith("/bucket/")) "bucket " + path.removePrefix("/bucket/") else path}"
        if (interfere?.invoke(req, res) == true) return@TestServer
        if (path.startsWith("/bucket/")) return@TestServer bucket(req, res)
        if (req.header("Authorization") != "Bearer key") return@TestServer res.json(401, error("unauthorized"))
        val m = Regex("""/api/s3/uploads(?:/([^/]+)(/parts|/complete)?)?""").matchEntire(path) ?: return@TestServer res.json(404, error("not_found"))
        val id = m.groupValues[1]
        if (id.isEmpty()) {
            val body = JSONObject(String(req.body.readNBytes(req.contentLength.toInt())))
            val up = Upload("u${next++}", body, body.getLong("size"), partSize)
            uploads[up.id] = up
            return@TestServer res.json(201, JSONObject().put("id", up.id).put("part_size", up.partSize).put("parts", parts(up))
                .put("urls", links(up, (1..minOf(parts(up), 10)).toList())).put("expires_at", "2026-10-05T12:00:00Z"))
        }
        val up = uploads[id] ?: return@TestServer res.json(404, error("not_found"))
        when (m.groupValues[2]) {
            "/parts" -> {
                val wanted = JSONObject(String(req.body.readNBytes(req.contentLength.toInt()))).getJSONArray("parts")
                res.json(200, JSONObject().put("urls", links(up, (0 until wanted.length()).map { wanted.getInt(it) })).put("expires_at", ""))
            }
            "/complete" -> {
                req.body.readNBytes(req.contentLength.toInt())
                if ((1..parts(up)).any { up.parts[it]?.size?.toLong() != S3Uploader.partLen(it, up.size, up.partSize) }) return@TestServer res.json(409, error("s3_parts_missing"))
                up.complete = true
                res.json(200, JSONObject().put("id", up.id))
            }
            else -> if (req.method == "DELETE") {
                uploads.remove(id)
                res.send(204)
            } else {
                val done = (1..parts(up)).filter { up.parts[it]?.size?.toLong() == S3Uploader.partLen(it, up.size, up.partSize) }
                res.json(200, JSONObject().put("id", up.id).put("state", if (up.complete) "complete" else "receiving").put("size", up.size)
                    .put("part_size", up.partSize).put("parts", parts(up)).put("done_parts", JSONArray(done)))
            }
        }
    }

    private fun bucket(req: TestServer.Request, res: TestServer.Response) {
        val link = "http://127.0.0.1:${server.port}${req.path}"
        if (req.header("Authorization") != null) problems += "the bucket got the key"
        if (link !in issued) problems += "a link that wasn't handed out: ${req.path}"
        val (id, n) = req.path.substringBefore('?').removePrefix("/bucket/").split('/').let { it[0] to it[1].toInt() }
        val up = uploads[id] ?: return res.send(404, "<Error><Code>NoSuchUpload</Code></Error>".toByteArray())
        if (req.contentLength != S3Uploader.partLen(n, up.size, up.partSize)) problems += "part $n with Content-Length ${req.contentLength}"
        // Keeps nothing of a part that breaks off, like S3.
        val bytes = ByteArrayOutputStream()
        val buffer = ByteArray(64 * 1024)
        var left = req.contentLength
        try {
            while (left > 0) {
                val got = req.body.read(buffer, 0, minOf(buffer.size.toLong(), left).toInt())
                if (got < 0) return
                bytes.write(buffer, 0, got)
                left -= got
            }
        } catch (e: IOException) {
            return
        }
        up.parts[n] = bytes.toByteArray()
        res.send(200, headers = mapOf("ETag" to "\"etag$n\""))
    }

    private fun parts(up: Upload) = S3Uploader.partsOf(up.size, up.partSize)

    private fun links(up: Upload, numbers: List<Int>): JSONArray {
        version++
        val base = bucketBase ?: "http://127.0.0.1:${server.port}"
        val out = JSONArray()
        for (n in numbers) {
            // An AWS-like query, with escapes that must arrive as they are.
            val link = "$base/bucket/${up.id}/$n?partNumber=$n&uploadId=${up.id}&X-Amz-Credential=AKIA%2F20261005%2Fauto%2Fs3%2Faws4_request&X-Amz-SignedHeaders=content-length%3Bhost&v=$version&X-Amz-Signature=sig"
            issued += link
            out.put(JSONObject().put("number", n).put("url", link).put("size", S3Uploader.partLen(n, up.size, up.partSize)))
        }
        return out
    }

    private fun error(code: String) = JSONObject().put("error", JSONObject().put("code", code).put("message", ""))

    private fun TestServer.Response.json(status: Int, body: JSONObject) =
        send(status, body.toString().toByteArray(), mapOf("Content-Type" to "application/json"))

    @After
    fun stop() = server.close()

    private fun open(method: String, path: String): HttpURLConnection =
        (URL("http://127.0.0.1:${server.port}$path").openConnection() as HttpURLConnection).apply {
            connectTimeout = 2_000
            readTimeout = 5_000
            requestMethod = method
            setRequestProperty("Authorization", "Bearer key")
        }

    private var clock = 0L

    private fun file(bytes: ByteArray = data) = File(tmp.root, "VID_1.mp4").apply { writeBytes(bytes) }

    private fun upload(
        uploadId: String? = null,
        bytes: ByteArray = data,
        source: UploadSource = FileSource(file(bytes)),
        abort: Abort = Abort(),
        created: MutableList<String> = mutableListOf(),
        open: (String, String) -> HttpURLConnection = ::open,
        onBytes: (Long) -> Unit = {},
    ) = S3Uploader(bufferSize = 64 * 1024, now = { clock }).upload(
        "VID_1.mp4", bytes.size.toLong(), source, uploadId, open, abort, { created += it }, onBytes, "f4mily5x2k7mbqz4bwdbyj6qsq", 1758960000000,
    )

    private fun bytesOf(id: String) = ByteArrayOutputStream().apply {
        val up = uploads.getValue(id)
        for (n in 1..parts(up)) write(up.parts.getValue(n))
    }.toByteArray()

    private fun bucketPuts() = requests.filter { it.startsWith("PUT bucket") }

    @Test
    fun partsGoStraightToTheBucket() {
        val created = mutableListOf<String>()
        val seen = mutableListOf<Long>()
        assertEquals(UploadOutcome.Done("u0"), upload(created = created) { seen += it })
        assertEquals(listOf("u0"), created)
        assertArrayEquals(data, bytesOf("u0"))
        assertEquals(
            listOf("POST /api/s3/uploads", "PUT bucket u0/1", "PUT bucket u0/2", "PUT bucket u0/3", "PUT bucket u0/4", "POST /api/s3/uploads/u0/complete"),
            requests,
        )
        assertEquals(data.size.toLong(), seen.last())
        assertEquals(emptyList<String>(), problems)
        // The start says what the contract's example says.
        val keys = contract("api/s3_upload_create.json").getJSONObject("request").keys().asSequence().toSet()
        assertEquals(keys, uploads.getValue("u0").body.keys().asSequence().toSet())
    }

    @Test
    fun goesOnWithThePartsTheBucketHas() {
        upload()
        uploads.getValue("u0").apply {
            parts.remove(2)
            parts.remove(4)
            complete = false
        }
        requests.clear()
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "u0"))
        assertEquals(
            listOf("GET /api/s3/uploads/u0", "POST /api/s3/uploads/u0/parts", "PUT bucket u0/2", "PUT bucket u0/4", "POST /api/s3/uploads/u0/complete"),
            requests,
        )
        assertArrayEquals(data, bytesOf("u0"))
    }

    @Test
    fun anUploadTheServerForgotStartsOverOnce() {
        val created = mutableListOf<String>()
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "gone", created = created))
        assertEquals(listOf("u0"), created)
        assertEquals("GET /api/s3/uploads/gone", requests.first())
    }

    @Test
    fun aRefusedLinkIsAskedForAgainOnce() {
        var refusals = 1
        interfere = { req, res ->
            if (req.method == "PUT" && req.path.startsWith("/bucket/u0/2") && refusals-- > 0) {
                req.body.readNBytes(req.contentLength.toInt())
                res.send(403, "<Error><Code>AccessDenied</Code></Error>".toByteArray())
                true
            } else false
        }
        assertEquals(UploadOutcome.Done("u0"), upload())
        assertTrue(requests.contains("POST /api/s3/uploads/u0/parts"))
        assertEquals(listOf("PUT bucket u0/1", "PUT bucket u0/2", "PUT bucket u0/2", "PUT bucket u0/3", "PUT bucket u0/4"), bucketPuts())

        // The bucket keeps refusing: wait, never sign out.
        refusals = 99
        interfere = { req, res ->
            if (req.method == "PUT") {
                req.body.readNBytes(req.contentLength.toInt())
                res.send(401)
                true
            } else false
        }
        assertEquals(UploadOutcome.Retry(null, 401, null, false), upload(bytes = data.copyOf(1000)))
    }

    @Test
    fun oldLinksAreAskedForAgain() {
        upload(onBytes = { if (it >= partSize) clock += 46L * 60 * 1_000_000_000 })
        assertEquals(listOf("POST /api/s3/uploads", "PUT bucket u0/1", "POST /api/s3/uploads/u0/parts"), requests.take(3))
        assertArrayEquals(data, bytesOf("u0"))
    }

    @Test
    fun aCutPartIsSentAgainLater() {
        interfere = { req, _ ->
            if (req.method == "PUT" && req.path.startsWith("/bucket/u0/3")) {
                req.body.readNBytes(1000)
                true // and goes away without an answer
            } else false
        }
        val first = upload()
        assertTrue("$first", first is UploadOutcome.Retry && first.progressed)
        interfere = null
        assertEquals(UploadOutcome.Done("u0"), upload(uploadId = "u0"))
        assertArrayEquals(data, bytesOf("u0"))
    }

    @Test
    fun missingPartsAreSentWhenCompletingSaysSo() {
        var lose = 1
        interfere = { req, res ->
            if (req.path.endsWith("/complete") && lose-- > 0) {
                uploads.getValue("u0").parts.remove(2) // a part sent again meanwhile, which then failed
                false
            } else false
        }
        assertEquals(UploadOutcome.Done("u0"), upload())
        assertEquals(listOf("PUT bucket u0/1", "PUT bucket u0/2", "PUT bucket u0/3", "PUT bucket u0/4", "PUT bucket u0/2"), bucketPuts())

        // A server that never finds them complete: wait.
        requests.clear()
        interfere = { req, res ->
            if (req.path.endsWith("/complete")) {
                req.body.readNBytes(req.contentLength.toInt())
                res.json(409, error("s3_parts_missing"))
                true
            } else false
        }
        assertEquals(UploadOutcome.Retry(null, 409, null, true), upload(bytes = data.copyOf(10)))
        assertEquals(3, requests.count { it.endsWith("/complete") })
    }

    @Test
    fun anEmptyFileNeedsNoParts() {
        assertEquals(UploadOutcome.Done("u0"), upload(bytes = ByteArray(0)))
        assertEquals(listOf("POST /api/s3/uploads", "POST /api/s3/uploads/u0/complete"), requests)
    }

    @Test
    fun plainHttpOnTheInternetIsNeverSent() {
        bucketBase = "http://bucket.example.com"
        assertEquals(UploadOutcome.Failed(0, "insecure_link"), upload())
        assertTrue(bucketPuts().isEmpty())
    }

    @Test
    fun theServersAnswersStillCount() {
        interfere = { req, res ->
            if (req.path.startsWith("/api/")) {
                res.json(401, error("session_ended"))
                true
            } else false
        }
        assertEquals(UploadOutcome.PinEnded, upload())
        interfere = { req, res ->
            if (req.path == "/api/s3/uploads") {
                res.json(404, error("folder_gone"))
                true
            } else false
        }
        assertEquals(UploadOutcome.Failed(404, "folder_gone"), upload())
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
        assertEquals(UploadOutcome.Stopped, upload(abort = abort) { if (it > 1_500_000) abort.stop() })
        assertTrue(uploads.getValue("u0").parts.size < 4)
    }
}
