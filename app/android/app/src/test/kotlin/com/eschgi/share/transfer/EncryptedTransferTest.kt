package com.eschgi.share.transfer

import com.eschgi.share.TestServer
import com.eschgi.share.e2ee.ContentCipher
import com.eschgi.share.e2ee.DecryptingStream
import com.eschgi.share.e2ee.E2ee
import com.eschgi.share.e2ee.EncryptingSource
import com.eschgi.share.e2ee.FolderPublicKey
import com.eschgi.share.e2ee.Hpke
import com.eschgi.share.e2ee.Keyring
import com.eschgi.share.e2ee.Keys
import com.eschgi.share.e2ee.SendRefused
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.Base64
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.random.Random

/**
 * Encrypted files on the phone (docs/e2ee-plan.md): downloads decrypt on the way and resume at
 * a chunk, the player's local stream answers ranges with plain bytes, and uploads send the
 * encrypted stream with its enc.
 */
class EncryptedTransferTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val id = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
    private val plain = Random(11).nextBytes(5 * E2ee.CHUNK_SIZE + 1234)
    private val key = E2ee.newFileKey()
    private val header = E2ee.newHeader()

    private fun cipher() = ContentCipher(key, header, plain.size.toLong())

    /** The file as the server keeps it. */
    private var stored = EncryptingSource(cipher()) { ByteArrayInputStream(plain, it.toInt(), plain.size - it.toInt()) }.openAt(0).readBytes()
    private val ranges = CopyOnWriteArrayList<String?>()

    /** How the next answer is cut short, if at all. */
    @Volatile private var cutAfter: Int? = null

    private val server = TestServer { req, res ->
        ranges += req.header("Range")
        val range = req.header("Range")
        if (range != null) {
            val start = range.removePrefix("bytes=").removeSuffix("-").toInt()
            res.send(206, stored.copyOfRange(start, stored.size), mapOf("Content-Range" to "bytes $start-${stored.size - 1}/${stored.size}"), cutAfter = cutAfter)
        } else {
            res.send(200, stored, cutAfter = cutAfter)
        }
    }

    @After
    fun stop() = server.close()

    private fun open(path: String) = (URL("http://127.0.0.1:${server.port}$path").openConnection() as HttpURLConnection).apply {
        connectTimeout = 2_000
        readTimeout = 5_000
    }

    private fun out() = File(tmp.root, "out.bin")

    private fun part() = File(tmp.root, "out.bin.part")

    private fun fetch() = Downloader(bufferSize = 16 * 1024).fetch(id, plain.size.toLong(), FileSink(out()), ::open, cipher = cipher())

    @Test
    fun downloadsDecrypted() {
        assertEquals(Downloader.Outcome.Done, fetch())
        FileSink(out()).commit()
        assertArrayEquals(plain, out().readBytes())
        assertEquals(listOf<String?>(null), ranges)
    }

    @Test
    fun goesOnAtTheChunkItStoppedBefore() {
        part().writeBytes(plain.copyOfRange(0, 2 * E2ee.CHUNK_SIZE))
        assertEquals(Downloader.Outcome.Done, fetch())
        assertEquals(listOf<String?>("bytes=${E2ee.cipherOffsetOfChunk(2)}-"), ranges)
        FileSink(out()).commit()
        assertArrayEquals(plain, out().readBytes())
    }

    @Test
    fun startsOverWhenAChunkWasCutShort() {
        part().writeBytes(plain.copyOfRange(0, E2ee.CHUNK_SIZE + 100))
        assertEquals(Downloader.Outcome.Done, fetch())
        assertEquals(listOf<String?>(null), ranges)
        FileSink(out()).commit()
        assertArrayEquals(plain, out().readBytes())
    }

    @Test
    fun aConnectionCutShortIsTriedAgain() {
        cutAfter = 3 * E2ee.CHUNK_SIZE
        val first = fetch()
        assertTrue(first.toString(), first is Downloader.Outcome.Retry)
        cutAfter = null
        assertEquals(Downloader.Outcome.Done, fetch())
        assertEquals("bytes=${E2ee.cipherOffsetOfChunk(2)}", ranges.last()?.removeSuffix("-"))
        FileSink(out()).commit()
        assertArrayEquals(plain, out().readBytes())
    }

    @Test
    fun aChangedChunkFailsAndLeavesNothing() {
        stored = stored.copyOf().also { it[E2ee.cipherOffsetOfChunk(3).toInt() + 5] = (it[E2ee.cipherOffsetOfChunk(3).toInt() + 5].toInt() xor 1).toByte() }
        val outcome = fetch()
        assertTrue(outcome.toString(), outcome is Downloader.Outcome.Failed)
        assertFalse(part().exists() && part().length() > 0)
    }

    @Test
    fun anotherFilesHeaderFails() {
        stored = stored.copyOf().also { it[10] = (it[10].toInt() xor 1).toByte() }
        assertTrue(fetch() is Downloader.Outcome.Failed)
    }

    @Test
    fun theLocalStreamAnswersRangesWithPlainBytes() {
        val file = FileRef(id, "clip.mp4", plain.size.toLong(), "video/mp4", "video")
        val url = LocalStream.url(file, cipher()) { from ->
            open("/api/files/$id/content").apply { setRequestProperty("Range", "bytes=$from-") }
        }
        fun get(range: String? = null, method: String = "GET"): Triple<Int, Map<String, String>, ByteArray> {
            val conn = URL(url).openConnection() as HttpURLConnection
            conn.requestMethod = method
            range?.let { conn.setRequestProperty("Range", it) }
            val status = conn.responseCode
            val body = (if (status < 400) conn.inputStream else conn.errorStream)?.use { it.readBytes() } ?: ByteArray(0)
            val headers = conn.headerFields.filterKeys { it != null }.mapValues { it.value.first() }
            conn.disconnect()
            return Triple(status, headers, body)
        }
        val whole = get()
        assertEquals(200, whole.first)
        assertArrayEquals(plain, whole.third)
        assertEquals("video/mp4", whole.second["Content-Type"])

        val middle = get("bytes=70000-70009")
        assertEquals(206, middle.first)
        assertEquals("bytes 70000-70009/${plain.size}", middle.second["Content-Range"])
        assertArrayEquals(plain.copyOfRange(70000, 70010), middle.third)
        // Asked from the start of the chunk that holds byte 70000.
        assertEquals("bytes=${E2ee.cipherOffsetOfChunk(1)}-", ranges.last())

        val end = get("bytes=-10")
        assertArrayEquals(plain.copyOfRange(plain.size - 10, plain.size), end.third)
        val open = get("bytes=${3 * E2ee.CHUNK_SIZE}-")
        assertArrayEquals(plain.copyOfRange(3 * E2ee.CHUNK_SIZE, plain.size), open.third)

        assertEquals(416, get("bytes=${plain.size}-").first)
        val head = get(method = "HEAD")
        assertEquals(plain.size.toString(), head.second["Content-Length"])
        val unknown = URL(url.substringBeforeLast('/').substringBeforeLast('/') + "/nope/clip.mp4").openConnection() as HttpURLConnection
        assertEquals(404, unknown.responseCode)
    }

    @Test
    fun tusSendsTheEncryptedStreamWithItsEnc() {
        val uploads = HashMap<String, ByteArrayOutputStream>()
        val metadata = HashMap<String, Map<String, String>>()
        val tus = TestServer { req, res ->
            val method = req.header("X-Test-Method") ?: req.method
            when {
                method == "POST" -> {
                    metadata["u0"] = req.header("Upload-Metadata")!!.split(',').associate { it.substringBefore(' ') to String(Base64.getDecoder().decode(it.substringAfter(' '))) }
                    uploads["u0"] = ByteArrayOutputStream()
                    res.send(201, headers = mapOf("Location" to "/tus/u0", "X-Length" to req.header("Upload-Length")!!))
                }
                method == "PATCH" -> {
                    val u = uploads.getValue("u0")
                    val bytes = ByteArray(req.contentLength.toInt())
                    var n = 0
                    while (n < bytes.size) n += req.body.read(bytes, n, bytes.size - n).also { if (it < 0) error("short") }
                    u.write(bytes)
                    res.send(204, headers = mapOf("Upload-Offset" to u.size().toString()))
                }
                else -> res.send(404)
            }
        }
        try {
            val pair = Hpke.generateKeyPair()
            val seal = UploadSeal.new(FolderPublicKey("f1", 2, pair.publicKey), plain.size.toLong(), 1_758_960_000_000)
            val source = SealedSource(seal, FileSource(File(tmp.root, "clip.mp4").apply { writeBytes(plain) }))
            val outcome = TusUploader(bufferSize = 16 * 1024).upload(
                "clip.mp4", "video/mp4", seal.encryptedSize, source, null, 100_000,
                open = { method, path ->
                    (URL("http://127.0.0.1:${tus.port}$path").openConnection() as HttpURLConnection).apply {
                        if (method == "PATCH") {
                            requestMethod = "POST"
                            setRequestProperty("X-Test-Method", "PATCH")
                        } else {
                            requestMethod = method
                        }
                    }
                },
                folder = "f1",
                enc = seal.enc(),
            )
            assertEquals(UploadOutcome.Done("u0"), outcome)
            val enc = JSONObject(metadata.getValue("u0").getValue("enc"))
            assertEquals(2, enc.getInt("version"))
            assertEquals(plain.size.toLong(), enc.getLong("plain_size"))
            val sent = uploads.getValue("u0").toByteArray()
            assertEquals(E2ee.encryptedSize(plain.size.toLong()), sent.size.toLong())
            // What went up opens with the key sealed in enc, and nothing else.
            val fileKey = E2ee.openKey(pair.privateKey, pair.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext("f1", 2), E2ee.fromB64u(enc.getString("key")))
            val back = ContentCipher(fileKey, E2ee.fromB64u(enc.getString("header")), plain.size.toLong())
            assertArrayEquals(header(sent), back.header)
            assertArrayEquals(plain, DecryptingStream(ByteArrayInputStream(sent, E2ee.HEADER_SIZE, sent.size), back).readBytes())
        } finally {
            tus.close()
        }
    }

    private fun header(sent: ByteArray) = sent.copyOfRange(0, E2ee.HEADER_SIZE)

    @Test
    fun aSealIsKeptAndKnowsItsFile() {
        val pair = Hpke.generateKeyPair()
        val seal = UploadSeal.new(FolderPublicKey("f1", 1, pair.publicKey), 1000, 42)
        val back = UploadSeal.parse(seal.toJson())!!
        assertArrayEquals(seal.key, back.key)
        assertArrayEquals(seal.header, back.header)
        assertEquals(seal.enc().toString(), back.enc().toString())
        assertTrue(back.fits(1000, 42))
        assertFalse(back.fits(1000, 43))
        assertFalse(back.fits(999, 42))
        assertTrue(back.fits(1000, null)) // the provider can't tell: the size has to do
        assertNull(UploadSeal.parse("{}"))
        assertNull(UploadSeal.parse(null))
        // A sealed thumbnail opens with the file's key.
        assertArrayEquals(byteArrayOf(1, 2, 3), E2ee.openThumb(seal.key, seal.sealThumb(byteArrayOf(1, 2, 3))))
    }

    @Test
    fun theFolderKeyToEncryptForIsWhatTheKeysSayOfTheServersWord() {
        val pair = Hpke.generateKeyPair()
        val pub = E2ee.b64u(pair.publicKey)
        var session = JSONObject().put("kind", "pin").put("folder_key", JSONObject.NULL).put("roots", JSONArray())
        val api = TestServer { req, res ->
            val body = when (req.path) {
                "/api/session" -> session
                "/api/folders" -> JSONObject().put(
                    "folders",
                    JSONArray().put(JSONObject().put("id", "f1").put("name", "f1").put("encrypted", true)).put(JSONObject().put("id", "plain").put("name", "plain").put("encrypted", false)),
                )
                else -> return@TestServer res.send(404)
            }
            res.send(200, body.toString().toByteArray(), mapOf("Content-Type" to "application/json"))
        }
        try {
            val open = { path: String -> URL("http://127.0.0.1:${api.port}$path").openConnection() as HttpURLConnection }
            // Someone signed in: the folder as the list describes it goes to the keys, which decide.
            val asked = ArrayList<String>()
            val signedIn = { f: JSONObject ->
                asked += f.getString("id")
                if (f.getBoolean("encrypted")) FolderPublicKey("f1", 3, pair.publicKey) else null
            }
            val guest = { s: JSONObject -> Keyring.guestKey(s, false, null, null) }
            assertEquals(3, UploadSeal.target(Credentials.DEVICE, "f1", open, signedIn, guest)!!.version)
            assertNull(UploadSeal.target(Credentials.DEVICE, "plain", open, signedIn, guest))
            assertNull(UploadSeal.target(Credentials.DEVICE, "gone", open, signedIn, guest))
            assertEquals(listOf("f1", "plain"), asked)
            assertThrows(SendRefused::class.java) { UploadSeal.target(Credentials.DEVICE, "f1", open, { throw SendRefused("unsigned") }, guest) }
            // A guest: the session's key, while the folder is encrypted.
            assertNull(UploadSeal.target(Credentials.PIN, null, open, signedIn, guest))
            session = session.put("folder_key", JSONObject().put("folder", "w3d").put("version", 2).put("public_key", pub).put("signature", E2ee.b64u(ByteArray(64))).put("encrypted", true))
            val pin = UploadSeal.target(Credentials.PIN, null, open, signedIn, guest)!!
            assertEquals("w3d", pin.folder)
            assertEquals(2, pin.version)
        } finally {
            api.close()
        }
    }

    @Test
    fun aSealedThumbnailMustBeASmallJpeg() {
        fun jpeg(width: Int, height: Int, frame: Int = 0xc0) = byteArrayOf(
            0xff.toByte(), 0xd8.toByte(),
            0xff.toByte(), 0xe0.toByte(), 0, 6, 0x4a, 0x46, 0x49, 0x46,
            0xff.toByte(), 0xc4.toByte(), 0, 4, 0, 0,
            0xff.toByte(), frame.toByte(), 0, 0x0b, 8, (height shr 8).toByte(), height.toByte(), (width shr 8).toByte(), width.toByte(), 1, 1, 0x11, 0,
            0xff.toByte(), 0xd9.toByte(),
        )
        assertEquals(512 to 384, Keys.jpegSize(jpeg(512, 384)))
        assertEquals(300 to 4000, Keys.jpegSize(jpeg(300, 4000, 0xc2)))
        assertNull(Keys.jpegSize(byteArrayOf(0x89.toByte(), 0x50, 0x4e, 0x47, 0, 0, 0, 0, 0, 0, 0, 0)))
        assertNull(Keys.jpegSize(jpeg(512, 384).copyOfRange(0, 18)))
        assertNotNull(Keys.jpegSize(jpeg(1, 1)))
    }
}
