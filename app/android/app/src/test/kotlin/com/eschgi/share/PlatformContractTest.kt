package com.eschgi.share

import com.eschgi.share.data.ServerConfig
import com.eschgi.share.net.Route
import com.eschgi.share.net.RouteReason
import com.eschgi.share.net.RouteStatus
import com.eschgi.share.transfer.BatchSnapshot
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.Picked
import com.eschgi.share.transfer.TransferItem
import com.eschgi.share.transfer.UploadBatch
import com.eschgi.share.transfer.UploadRow
import com.eschgi.share.transfer.UploadSnapshot
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

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
        assertEquals(FileRef("bbbbbbbbbbbbbbbbbbbbbbbbbb", "VID_0412.mp4", 2000, "video/mp4", "video"), files[1])
        assertEquals(listOf(true, true, false, false), files.map { it.isMedia })
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
        val batch = UploadBatch(expected.getString("batch"), UploadBatch.DEVICE, "active", null, 0)
        val items = List(rows.length()) { i ->
            val r = rows.getJSONObject(i)
            val file = Picked("content://picked/$i", r.getString("name"), r.getLong("size"), r.getString("mime"))
            UploadRow(batch.id, r.getInt("seq"), file, r.getString("state"), null, r.getLong("bytes"))
        }
        val snapshot = UploadSnapshot.of(batch, items, etaSeconds = 120, local = true)
        assertEquals(expected.toMap(), snapshot.toMap().numbersAsLong())
        val paused = fixture.getJSONArray("upload_paused")
        assertEquals(listOf("pin_ended", "signed_out", "user"), List(paused.length()) { paused.getString(it) })
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
