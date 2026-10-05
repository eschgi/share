package com.eschgi.share.transfer

import com.eschgi.share.TestServer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.random.Random

/** Downloads against a small server that answers like GET /api/files/{id}/content. */
class DownloaderTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val id = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
    private val data = Random(7).nextBytes(3 * 1024 * 1024 + 17)
    private val requests = CopyOnWriteArrayList<TestServer.Request>()

    /** What the next answer does; tests change it. */
    @Volatile private var behaviour: (TestServer.Request, TestServer.Response) -> Unit = { req, res -> serve(req, res) }

    private val server = TestServer { req, res ->
        requests += req
        behaviour(req, res)
    }

    @After
    fun stop() = server.close()

    private fun open(path: String) = (URL("http://127.0.0.1:${server.port}$path").openConnection() as HttpURLConnection).apply {
        connectTimeout = 2_000
        readTimeout = 5_000
    }

    private fun sink() = FileSink(File(tmp.root, "out.bin"))

    private fun partial() = File(tmp.root, "out.bin.part")

    private fun fetch(sink: DownloadSink, abort: Abort = Abort(), onBytes: (Long) -> Unit = {}) =
        Downloader(bufferSize = 64 * 1024).fetch(id, data.size.toLong(), sink, ::open, abort, onBytes)

    /** Range and If-Range as the server does them: the ETag is the file's id. */
    private fun serve(req: TestServer.Request, res: TestServer.Response, cutAfter: Int? = null) {
        val range = req.header("Range")
        val ifRange = req.header("If-Range")
        if (range != null && (ifRange == null || ifRange == "\"$id\"")) {
            val start = range.removePrefix("bytes=").removeSuffix("-").toInt()
            val part = data.copyOfRange(start, data.size)
            res.send(206, part, mapOf("Content-Range" to "bytes $start-${data.size - 1}/${data.size}"), cutAfter = cutAfter)
        } else {
            res.send(200, data, cutAfter = cutAfter)
        }
    }

    @Test
    fun wholeFile() {
        val seen = mutableListOf<Long>()
        assertEquals(Downloader.Outcome.Done, fetch(sink()) { seen += it })
        assertArrayEquals(data, partial().readBytes())
        assertEquals(data.size.toLong(), seen.last())
        assertEquals(null, requests.single().header("Range"))
        assertEquals("identity", requests.single().header("Accept-Encoding"))
        assertEquals("/api/files/$id/content", requests.single().path)
    }

    @Test
    fun resumesWhereItStopped() {
        partial().writeBytes(data.copyOf(1_000_000))
        assertEquals(Downloader.Outcome.Done, fetch(sink()))
        assertArrayEquals(data, partial().readBytes())
        assertEquals("bytes=1000000-", requests.single().header("Range"))
        assertEquals("\"$id\"", requests.single().header("If-Range"))
    }

    @Test
    fun startsOverWhenTheServerSendsAllOfIt() {
        // If-Range didn't match, say: a 200 with the whole file.
        partial().writeBytes(ByteArray(1000) { 1 })
        behaviour = { _, res -> res.send(200, data) }
        assertEquals(Downloader.Outcome.Done, fetch(sink()))
        assertArrayEquals(data, partial().readBytes())
    }

    @Test
    fun aBrokenConnectionKeepsWhatArrived() {
        behaviour = { req, res -> serve(req, res, cutAfter = 1_500_000) }
        val first = fetch(sink())
        assertTrue("$first", first is Downloader.Outcome.Retry && first.progressed)
        val kept = partial().length()
        assertTrue("kept $kept", kept in 1..1_500_000)

        behaviour = { req, res -> serve(req, res) }
        assertEquals(Downloader.Outcome.Done, fetch(sink()))
        assertArrayEquals(data, partial().readBytes())
        assertEquals("bytes=$kept-", requests.last().header("Range"))
    }

    @Test
    fun nothingToFetchWhenItsAllThere() {
        partial().writeBytes(data)
        assertEquals(Downloader.Outcome.Done, fetch(sink()))
        assertTrue(requests.isEmpty())
    }

    @Test
    fun tooMuchOnThePhoneStartsOver() {
        partial().writeBytes(data + byteArrayOf(1, 2, 3))
        assertEquals(Downloader.Outcome.Done, fetch(sink()))
        assertArrayEquals(data, partial().readBytes())
        assertEquals(null, requests.single().header("Range"))
    }

    @Test
    fun aRangeThatDoesntFitStartsOver() {
        partial().writeBytes(data.copyOf(1000))
        behaviour = { _, res -> res.send(206, data, mapOf("Content-Range" to "bytes 0-${data.size - 1}/${data.size}")) }
        assertTrue(fetch(sink()) is Downloader.Outcome.Retry)
        assertEquals(0, partial().length())
    }

    @Test
    fun answersThatEndIt() {
        behaviour = { _, res -> res.send(401) }
        assertEquals(Downloader.Outcome.SignedOut, fetch(sink()))
        behaviour = { _, res -> res.send(404) }
        assertEquals(Downloader.Outcome.Gone, fetch(sink()))
        behaviour = { _, res -> res.send(400) }
        assertTrue(fetch(sink()) is Downloader.Outcome.Failed)
    }

    @Test
    fun busyServerSaysWhenToComeBack() {
        behaviour = { _, res -> res.send(503, headers = mapOf("Retry-After" to "7")) }
        assertEquals(Downloader.Outcome.Retry(null, 503, 7000), fetch(sink()))
        // Cloudflare's own answer when the server is down.
        behaviour = { _, res -> res.send(522) }
        assertEquals(Downloader.Outcome.Retry(null, 522, null), fetch(sink()))
    }

    @Test
    fun longerThanTheLibrarySaid() {
        behaviour = { _, res -> res.send(200, data + ByteArray(10)) }
        assertTrue(fetch(sink()) is Downloader.Outcome.Failed)
        assertEquals(0, partial().length())
    }

    @Test
    fun stopsMidway() {
        val abort = Abort()
        val outcome = fetch(sink(), abort) { if (it > 500_000) abort.stop() }
        assertEquals(Downloader.Outcome.Stopped, outcome)
        assertTrue(partial().length() in 500_001 until data.size)
    }

    @Test
    fun connectionDropsBeforeTheAnswer() {
        behaviour = { _, _ -> throw IOException("dropped") }
        val outcome = fetch(sink())
        assertTrue("$outcome", outcome is Downloader.Outcome.Retry && !outcome.progressed)
    }

    @Test
    fun emptyFile() {
        val empty = FileSink(File(tmp.root, "empty.txt"))
        assertEquals(Downloader.Outcome.Done, Downloader().fetch(id, 0, empty, ::open))
        assertEquals(File(tmp.root, "empty.txt").path, empty.commit())
        assertEquals(0, File(tmp.root, "empty.txt").length())
        assertTrue(requests.isEmpty())
    }

    @Test
    fun headers() {
        assertEquals(12L to 100L, Downloader.contentRange("bytes 12-99/100"))
        assertEquals(12L to null, Downloader.contentRange("bytes 12-99/*"))
        assertEquals(null, Downloader.contentRange("items 1-2/3"))
        assertEquals(30_000L, Downloader.retryAfter("30"))
        assertEquals(null, Downloader.retryAfter("Wed, 21 Oct 2026 07:28:00 GMT"))
    }

    @Test
    fun backoffGrowsToHalfAMinute() {
        assertEquals(listOf(1000L, 2000L, 4000L, 8000L, 16000L, 30000L, 30000L), (1..7).map { DownloadEngine.backoff(it) })
    }

    // From a bucket: the same server plays it, at /bucket/ with a signed-looking query.

    private var linksGiven = 0

    private fun bucketLink() = "http://127.0.0.1:${server.port}/bucket/$id?X-Amz-Credential=AKIA%2F20261005%2Fauto%2Fs3%2Faws4_request&n=${linksGiven++}&X-Amz-Signature=sig"

    private fun fetchS3(sink: DownloadSink, link: () -> S3Links.Answer = { S3Links.Answer.Link(bucketLink()) }) =
        Downloader(bufferSize = 64 * 1024).fetchS3(id, data.size.toLong(), sink, link)

    @Test
    fun fromTheBucketWithoutTheKey() {
        assertEquals(Downloader.Outcome.Done, fetchS3(sink()))
        assertArrayEquals(data, partial().readBytes())
        val req = requests.single()
        assertTrue(req.path.startsWith("/bucket/$id?X-Amz-Credential=AKIA%2F20261005%2Fauto%2Fs3%2Faws4_request&"))
        assertEquals(null, req.header("Authorization"))
        assertEquals("identity", req.header("Accept-Encoding"))
    }

    @Test
    fun resumesFromTheBucketWithRangeAlone() {
        partial().writeBytes(data.copyOf(1_000_000))
        assertEquals(Downloader.Outcome.Done, fetchS3(sink()))
        assertArrayEquals(data, partial().readBytes())
        assertEquals("bytes=1000000-", requests.single().header("Range"))
        assertEquals(null, requests.single().header("If-Range")) // an object never changes
    }

    @Test
    fun aRefusedLinkIsAskedForAgainKeepingWhatArrived() {
        partial().writeBytes(data.copyOf(500_000))
        var refusals = 1
        behaviour = { req, res -> if (refusals-- > 0) res.send(403) else serve(req, res) }
        assertEquals(Downloader.Outcome.Done, fetchS3(sink()))
        assertEquals(2, linksGiven)
        assertEquals(listOf("bytes=500000-", "bytes=500000-"), requests.map { it.header("Range") })
        assertArrayEquals(data, partial().readBytes())

        // The bucket keeps refusing, even with a 401: wait, never sign out.
        requests.clear()
        behaviour = { _, res -> res.send(401) }
        assertEquals(Downloader.Outcome.Retry(null, 401), fetchS3(sink().also { it.truncate() }))
        assertEquals(2, requests.size)
    }

    @Test
    fun aMissingObjectAsksTheServer() {
        behaviour = { _, res -> res.send(404) }
        var asked = 0
        assertEquals(Downloader.Outcome.Gone, fetchS3(sink()) { if (asked++ == 0) S3Links.Answer.Link(bucketLink()) else S3Links.Answer.Gone })
        assertEquals(Downloader.Outcome.Failed(404, "the bucket doesn't have the file"), fetchS3(sink()))
        assertEquals(Downloader.Outcome.SignedOut, fetchS3(sink()) { S3Links.Answer.SignedOut })
        assertEquals(Downloader.Outcome.Failed(0, "a link to plain http outside home"), fetchS3(sink()) { S3Links.Answer.Link("http://bucket.example.com/x") })
    }
}
