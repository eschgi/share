package com.eschgi.share

import com.eschgi.share.data.ServerConfig
import com.eschgi.share.e2ee.DeviceKeyStore
import com.eschgi.share.e2ee.KeyPair
import com.eschgi.share.e2ee.Pins
import com.eschgi.share.e2ee.Keyring
import com.eschgi.share.e2ee.SealedFile
import com.eschgi.share.net.Route
import com.eschgi.share.net.RouteReason
import com.eschgi.share.net.RouteStatus
import com.eschgi.share.transfer.BatchSnapshot
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.Picked
import com.eschgi.share.transfer.Playback
import com.eschgi.share.transfer.TransferItem
import com.eschgi.share.transfer.UploadBatch
import com.eschgi.share.transfer.UploadRow
import com.eschgi.share.transfer.UploadSnapshot
import com.eschgi.share.zip.PackFile
import com.eschgi.share.zip.PackSet
import com.eschgi.share.zip.ShareJson
import com.eschgi.share.zip.ZipEvents
import com.eschgi.share.zip.ZipPlan
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.ZoneId

/** The Kotlin half of contract/app/platform.json; test/platform_test.dart is the Dart half. */
class PlatformContractTest {
    private val fixture = contract("app/platform.json")

    @Test
    fun readsTheServerConfigDartStores() {
        val c = ServerConfig.parse(fixture.getJSONObject("server_config").toString())!!
        assertEquals("https://share.example.com", c.publicUrl)
        assertEquals("https://192.168.8.1:8443", c.localUrl)
        assertEquals(listOf("3f1c9e0a5b7d2c4e6f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6"), c.pins)
        assertEquals("q3m7k2x9w4t8r5n6p2j7h3c9d4", c.serverId)
        assertEquals("dv6kq3zt7wbxl2m4nf5yh8rc", c.deviceId)
        assertEquals(true, c.hasLocal)
        assertEquals(false, c.publicIsHttp)
    }

    @Test
    fun aServerAtHomeOverPlainHttp() {
        // No pins: nothing to pin over plain http; the probe asks for the proof instead.
        val home = ServerConfig.parse("""{"public_url":"https://share.example.com","local_url":"http://192.168.8.1:8080","pins":[],"server_id":"s"}""")!!
        assertEquals(true, home.hasLocal)
        assertEquals(true, home.needsProbe)
        val onlyHome = ServerConfig.parse("""{"public_url":"http://192.168.8.1:8080","local_url":"","pins":[],"server_id":"s"}""")!!
        assertEquals(false, onlyHome.hasLocal)
        assertEquals(true, onlyHome.publicIsHttp)
        assertEquals(true, onlyHome.needsProbe)
    }

    @Test
    fun aConfigWithoutLocalAddress() {
        val c = ServerConfig.parse("""{"public_url":"https://share.example.com/","local_url":"","pins":[],"server_id":null}""")!!
        assertEquals("https://share.example.com", c.publicUrl)
        assertNull(c.localUrl)
        assertNull(c.serverId)
        assertEquals(false, c.hasLocal)
        assertNull(ServerConfig.parse(""))
        assertNull(ServerConfig.parse("{"))
    }

    @Test
    fun readsTheFilesOfADownload() {
        val files = FileRef.parseList(fixture.getJSONArray("files").toString())
        assertEquals(4, files.size)
        assertEquals(FileRef("aaaaaaaaaaaaaaaaaaaaaaaaaa", "IMG_2041.jpg", 1000, "image/jpeg", "photo", "f4mily5x2k7mbqz4bwdbyj6qsq"), files[0])
        assertEquals(listOf(true, true, false, false), files.map { it.isMedia })
        // The video is encrypted: its key, sealed for its folder's key, goes with it.
        val video = files[1]
        assertEquals("VID_0412.mp4", video.name)
        assertEquals(SealedFile("bbbbbbbbbbbbbbbbbbbbbbbbbb", "f4mily5x2k7mbqz4bwdbyj6qsq", 1, video.enc!!.key, "U0hFMQABAAA9p6lLxQCBAA", 2000), video.enc)
        // TransferDb keeps it as JSON and reads it back.
        assertEquals(video, FileRef.of(video.id, video.name, video.size, video.mime, video.kind, video.folder, video.encJson()))
        assertEquals(files[0], FileRef.of(files[0].id, files[0].name, 1000, "image/jpeg", "photo", files[0].folder, null))
    }

    @Test
    fun keysEventIsWhatDartReads() {
        val ring = Keyring({ _, _, _ -> throw java.io.IOException("offline") }, object : DeviceKeyStore {
            override fun load(deviceId: String): KeyPair? = null

            override fun save(deviceId: String, pair: KeyPair) {}

            override fun pins(deviceId: String) = Pins()

            override fun keepPins(deviceId: String, pins: Pins) {}
        })
        val off = ring.state()
        val expected = fixture.getJSONObject("keys_event")
        assertEquals(expected.keys().asSequence().toSet(), off.keys)
        assertEquals("off", off["status"])
        assertEquals(expected.getString("type"), off["type"])
    }

    @Test
    fun routeEventIsWhatDartReads() {
        val event = RouteStatus(Route.LOCAL, millis = 12).toMap() + ("type" to "route")
        assertEquals(fixture.getJSONObject("route_event").toMap(), event.numbersAsLong())
        val reasons = fixture.getJSONArray("route_reasons")
        assertEquals(List(reasons.length()) { reasons.getString(it) }, RouteReason.entries.map { it.wire })
    }

    @Test
    fun transferEventIsWhatDartReads() {
        val files = FileRef.parseList(fixture.getJSONArray("files").toString()).associateBy { it.id }
        val rows = fixture.getJSONArray("batch_items")
        val expected = fixture.getJSONObject("transfer_event")
        val batch = expected.getString("batch")
        val items = List(rows.length()) { i ->
            val r = rows.getJSONObject(i)
            TransferItem(batch, files.getValue(r.getString("file")), r.getString("state"), r.getLong("bytes"), null)
        }
        val snapshot = BatchSnapshot.of(batch, running = true, noSpace = false, items = items, local = true)
        assertEquals(expected.toMap(), snapshot.toMap().numbersAsLong())
    }

    @Test
    fun liveBytesCountWhileRunning() {
        val file = FileRef("a", "a.jpg", 1000, "image/jpeg", "photo")
        val item = TransferItem("b", file, TransferItem.QUEUED, 100, null)
        assertEquals(400, BatchSnapshot.of("b", true, false, listOf(item), mapOf("a" to 400L)).bytesDone)
        // Never more than the file, whatever a sink reports.
        assertEquals(1000, BatchSnapshot.of("b", true, false, listOf(item), mapOf("a" to 5000L)).bytesDone)
    }

    @Test
    fun uploadEventIsWhatDartReads() {
        val rows = fixture.getJSONArray("upload_rows")
        val expected = fixture.getJSONObject("upload_event")
        val batch = UploadBatch(expected.getString("batch"), UploadBatch.DEVICE, "active", null, 0, expected.getString("folder"))
        val items = List(rows.length()) { i ->
            val r = rows.getJSONObject(i)
            val file = Picked("content://picked/$i", r.getString("name"), r.getLong("size"), r.getString("mime"))
            UploadRow(batch.id, r.getInt("seq"), file, r.getString("state"), null, r.getLong("bytes"))
        }
        val snapshot = UploadSnapshot.of(batch, items, etaSeconds = 120, local = true)
        assertEquals(expected.toMap(), snapshot.toMap().numbersAsLong())
        val paused = fixture.getJSONArray("upload_paused")
        assertEquals(listOf("pin_ended", "signed_out", UploadBatch.FOLDER_GONE, "user"), List(paused.length()) { paused.getString(it) })
    }

    @Test
    fun playAnswersAreWhatDartReads() {
        val stream = fixture.getJSONObject("play_stream")
        val answer = Playback.streamOf("https://share.example.com", "bbbbbbbbbbbbbbbbbbbbbbbbbb", "shd_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG")
        assertEquals(stream.getString("uri"), answer["uri"])
        val headers = stream.getJSONObject("headers")
        @Suppress("UNCHECKED_CAST")
        val sent = answer["headers"] as Map<String, String>
        assertEquals(headers.keys().asSequence().toSet(), sent.keys)
        assertEquals(headers.getString("Authorization"), sent["Authorization"])
        assertEquals(fixture.getJSONObject("play_copy").keys().asSequence().toSet(), answer.keys)
    }

    @Test
    fun playingFromABucketKeepsTheLinkAsItIs() {
        val expected = fixture.getJSONObject("play_s3")
        val answer = Playback.linkOf(expected.getString("uri"))
        assertEquals(expected.getString("uri"), answer["uri"])
        @Suppress("UNCHECKED_CAST")
        val sent = answer["headers"] as Map<String, String>
        assertEquals(expected.getJSONObject("headers").keys().asSequence().toSet(), sent.keys) // the app's name only, no key
        assertEquals(fixture.getJSONObject("play_copy").keys().asSequence().toSet(), answer.keys)
    }

    private fun fixtureMap(name: String) = fixture.getJSONObject(name).toMap().numbersAsLong()

    @Test
    fun filesToPackAreWhatDartReads() {
        val take = fixture.getJSONObject("zip_take")
        val files = take.getJSONArray("files")
        assertEquals(files.getJSONObject(0).toMap().numbersAsLong(), ZipEvents.file(PackFile("IMG_20261004_161502.jpg", 20_000_000, "image/jpeg", 1_791_123_302_000), "photo", true).numbersAsLong())
        assertEquals(files.getJSONObject(1).toMap().numbersAsLong(), ZipEvents.file(PackFile("VID_20261004_161944.mp4", 36_000_000, "video/mp4", 1_791_123_584_000), "video", false).numbersAsLong())
    }

    @Test
    fun aPlanIsWhatDartReads() {
        // Three files in ZIPs of at most 300,000 bytes: two photos, and a video cut into three pieces.
        val t0 = 1_791_123_302_000L
        val set = PackSet("1c6f3a62-5d0e-4c9b-9f7e-2b8f0f4d6a11", "Photos 4 Oct 2026", "Made with Share.", t0, "0.2.0", ZoneId.of("Europe/Rome"))
        val files = listOf(PackFile("IMG_20261004_161502.jpg", 100_000, "image/jpeg", t0), PackFile("IMG_20261004_161503.jpg", 120_000, "image/jpeg", t0 + 1000), PackFile("VID_20261004_161944.mp4", 500_000, "video/mp4", t0 + 2000))
        val made = ZipEvents.plan(ZipPlan.make(files, 300_000, set)).numbersAsLong()
        // The bytes depend on every header; the rest is as Dart reads it.
        fun withoutBytes(m: Map<String, Any?>): Map<String, Any?> =
            @Suppress("UNCHECKED_CAST")
            (m + ("parts" to (m["parts"] as List<Map<String, Any?>>).map { it - "bytes" }))
        assertEquals(withoutBytes(fixtureMap("zip_plan")), withoutBytes(made))
        @Suppress("UNCHECKED_CAST")
        assertEquals(true, (made["parts"] as List<Map<String, Any?>>).all { (it["bytes"] as Long) <= 300_000 })
        val ready = ZipEvents.ready(ZipPlan.make(files.take(1), null, set), listOf("Photos 4 Oct 2026.zip")).numbersAsLong()
        assertEquals(fixtureMap("zip_ready").keys, ready.keys)
    }

    @Test
    fun packingEventsAreWhatDartReads() {
        assertEquals(fixtureMap("zip_packing"), ZipEvents.packing(8, 14, 200_000_000, 312_000_000, 1, 1, null).numbersAsLong())
        assertEquals(fixtureMap("zip_no_room"), ZipEvents.noRoom(7_012_000_000, 3_100_000_000).numbersAsLong())
        assertEquals(fixtureMap("zip_failed"), ZipEvents.failed("changed", "IMG_20261004_161502.jpg").numbersAsLong())
        assertEquals(fixtureMap("zip_sent"), ZipEvents.sent(0, "WhatsApp").numbersAsLong())
    }

    @Test
    fun whatAZipHoldsIsWhatDartReads() {
        val piece = ShareJson.Piece("VID_20261017_141502.mp4", 1, 2, 0, 2_412_345_678, listOf(3, 4))
        val contents = ZipEvents.contents(
            name = "Dolomites 15–18 Oct 2026",
            zips = 1,
            broken = 0,
            newer = false,
            set = ZipEvents.set(4, listOf(3), listOf(1, 2), "phone", null),
            fromWhatsapp = true,
            bytes = 1_875_000_000,
            files = listOf(
                ZipEvents.entry(0, "IMG_20261017_101502.jpg", "photo", 70_000_000, readable = true, saved = false, piece = null),
                ZipEvents.entry(1, "VID_20261017_141502.mp4.001", "video", 1_105_000_000, readable = true, saved = false, piece = piece),
            ),
            joins = listOf(ZipEvents.join("VID_20261017_141502.mp4", "video", 2_412_345_678, 2, listOf(1), listOf(3, 4))),
        )
        assertEquals(fixtureMap("zip_contents"), contents.numbersAsLong())
        assertEquals(fixtureMap("zip_saving"), ZipEvents.saving(7, 14, 150_000_000, 312_000_000).numbersAsLong())
        val saved = ZipEvents.saved(
            11,
            listOf(mapOf("file" to "VID_20261017_141502.mp4", "kind" to "video", "total" to 2_412_345_678L, "parts" to listOf(3, 4))),
            listOf(mapOf("file" to "IMG_20261017_103001.jpg", "part" to 3)),
            "phone",
            null,
        )
        assertEquals(fixtureMap("zip_saved"), saved.numbersAsLong())
    }

    @Test
    fun aPausedBatchSaysWhy() {
        val batch = UploadBatch("b", UploadBatch.PIN, "paused", "pin_ended", 0)
        val row = UploadRow("b", 0, Picked("content://x", "a.jpg", 10, "image/jpeg"), UploadRow.QUEUED, "u1", 4)
        val s = UploadSnapshot.of(batch, listOf(row))
        assertEquals(false, s.running)
        assertEquals("pin_ended", s.paused)
        assertEquals(4L, s.bytesDone)
    }
}
