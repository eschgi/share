package com.eschgi.share

import com.eschgi.share.data.ServerConfig
import com.eschgi.share.net.Route
import com.eschgi.share.net.RouteReason
import com.eschgi.share.net.RouteStatus
import com.eschgi.share.transfer.BatchSnapshot
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.TransferItem
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
        assertEquals(true, c.hasLocal)
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
}
