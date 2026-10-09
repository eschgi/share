package com.eschgi.share.zip

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.io.File
import java.io.InputStream
import java.io.RandomAccessFile
import java.time.ZoneId
import java.util.zip.CRC32
import java.util.zip.ZipFile
import java.util.zip.ZipInputStream
import kotlin.random.Random

/** Share's ZIPs read as Java and Android read ZIPs, with the bytes, CRCs, names and times that went in. */
class ZipWriterTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val zone = ZoneId.of("Europe/Rome")
    private val sunday = 1_791_123_302_000L // 2026-10-04 16:15:02 in Rome

    private fun write(file: File, items: List<ZipItem>, data: List<ByteArray>): Pair<ZipLayout, IntArray> {
        val layout = ZipLayout(items, zone)
        val crcs = RandomAccessFile(file, "rw").use { raf ->
            ZipWriter(layout, ChannelOut(raf.channel)).write({ ByteArrayInputStream(data[it]) })
        }
        return layout to crcs
    }

    private val data = listOf(
        """{"share_zip": 1}""".toByteArray(),
        Random(1).nextBytes(150_000),
        Random(2).nextBytes(3_000),
        ByteArray(0),
    )
    private val items = listOf(
        ZipItem("share.json", data[0].size.toLong(), sunday),
        ZipItem("IMG_20261004_161502.jpg", data[1].size.toLong(), sunday),
        ZipItem("Über die Brücke.pdf", data[2].size.toLong(), sunday - 86_400_000),
        ZipItem("empty.txt", 0, sunday),
    )

    @Test
    fun javasZipFileReadsIt() {
        val file = tmp.newFile("a.zip")
        val (layout, crcs) = write(file, items, data)
        assertEquals(layout.size, file.length())
        ZipFile(file).use { zip ->
            val entries = zip.entries().toList()
            assertEquals(items.map { it.name }, entries.map { it.name })
            for ((i, e) in entries.withIndex()) {
                assertEquals(java.util.zip.ZipEntry.STORED, e.method)
                assertEquals(items[i].size, e.size)
                assertEquals(crc(data[i]), e.crc)
                assertEquals(crcs[i].toLong() and 0xFFFFFFFFL, e.crc)
                assertArrayEquals(data[i], zip.getInputStream(e).readBytes())
                // The extended timestamp, to the second.
                assertEquals(items[i].modified / 1000, e.lastModifiedTime.toMillis() / 1000)
            }
        }
    }

    @Test
    fun javasZipInputStreamReadsIt() {
        // Android apps that unzip a stream can't read stored files with a data descriptor; there is none.
        val file = tmp.newFile("a.zip")
        write(file, items, data)
        ZipInputStream(file.inputStream()).use { zip ->
            for (i in items.indices) {
                val e = zip.nextEntry
                assertEquals(items[i].name, e.name)
                assertArrayEquals(data[i], zip.readBytes())
            }
            assertNull(zip.nextEntry)
        }
    }

    @Test
    fun theMsDosTimeIsTheWallClock() {
        val file = tmp.newFile("a.zip")
        write(file, items, data)
        val raw = file.readBytes()
        val time = (raw[10].toInt() and 0xFF) or ((raw[11].toInt() and 0xFF) shl 8)
        val date = (raw[12].toInt() and 0xFF) or ((raw[13].toInt() and 0xFF) shl 8)
        assertEquals(16, time ushr 11)
        assertEquals(15, (time ushr 5) and 63)
        assertEquals(2, (time and 31) * 2)
        assertEquals(2026, 1980 + (date ushr 9))
        assertEquals(10, (date ushr 5) and 15)
        assertEquals(4, date and 31)
    }

    @Test
    fun aFileThatChangedStopsIt() {
        val layout = ZipLayout(items, zone)
        for (wrong in listOf(data[1].copyOf(100), data[1] + byteArrayOf(1))) {
            val file = tmp.newFile()
            try {
                RandomAccessFile(file, "rw").use { raf ->
                    ZipWriter(layout, ChannelOut(raf.channel)).write({ if (it == 1) ByteArrayInputStream(wrong) else ByteArrayInputStream(data[it]) })
                }
                fail("a file of ${wrong.size} bytes for ${data[1].size} went in")
            } catch (e: SourceChanged) {
                assertEquals(1, e.item)
            }
        }
    }

    @Test
    fun stopStopsIt() {
        val file = tmp.newFile()
        try {
            RandomAccessFile(file, "rw").use { raf -> ZipWriter(ZipLayout(items, zone), ChannelOut(raf.channel)).write({ ByteArrayInputStream(data[it]) }, stopped = { true }) }
            fail("it didn't stop")
        } catch (e: PackStopped) {
            // as it should
        }
    }

    @Test
    fun namesThatUnpackWrongAreRefused() {
        for (name in listOf("", "/etc/passwd", "a/../b", "a\\b", "a//b", "./a", "a\u0000b")) {
            try {
                ZipLayout(listOf(ZipItem(name, 0, sunday)), zone)
                fail("$name was taken")
            } catch (e: IllegalArgumentException) {
                // as it should
            }
        }
        try {
            ZipLayout(listOf(ZipItem("a.jpg", 0, sunday), ZipItem("a.jpg", 1, sunday)), zone)
            fail("a name went in twice")
        } catch (e: IllegalArgumentException) {
            // as it should
        }
    }

    @Test
    fun aFileOver4GiBGetsZip64() {
        // A sparse file: the zeros aren't written, so the test needs no 4 GiB of disk.
        val big = (1L shl 32) + 12_345
        val small = Random(3).nextBytes(1_000)
        val list = listOf(ZipItem("small.bin", small.size.toLong(), sunday), ZipItem("big.bin", big, sunday), ZipItem("after.bin", small.size.toLong(), sunday))
        val layout = ZipLayout(list, zone)
        val file = tmp.newFile("big.zip")
        val crcs = RandomAccessFile(file, "rw").use { raf ->
            ZipWriter(layout, Sparse(ChannelOut(raf.channel), raf)).write({ if (it == 1) Zeros(big) else ByteArrayInputStream(small) })
        }
        assertEquals(layout.size, file.length())
        assertEquals(zerosCrc(big), crcs[1].toLong() and 0xFFFFFFFFL)
        ZipFile(file).use { zip ->
            assertEquals(big, zip.getEntry("big.bin").size)
            assertArrayEquals(small, zip.getInputStream(zip.getEntry("after.bin")).readBytes())
        }
        RandomAccessFile(file, "r").use { raf ->
            val reader = ZipReader(raf.channel, zone)
            assertEquals(list.map { it.name }, reader.entries.map { it.name })
            assertEquals(big, reader.entries[1].size)
            assertTrue(reader.entries[2].localOffset > big)
            assertArrayEquals(small, reader.open(reader.entries[2]).readBytes())
        }
    }

    /** Leaves a hole for every buffer of zeros, so a test can write gigabytes of them. */
    private class Sparse(private val inner: ZipOut, private val raf: RandomAccessFile) : ZipOut {
        override val position: Long get() = inner.position

        override fun write(bytes: ByteArray, off: Int, len: Int) {
            if (len >= 4096 && (off until off + len).all { bytes[it] == 0.toByte() }) {
                raf.channel.position(raf.channel.position() + len)
                if (raf.length() < raf.channel.position()) raf.setLength(raf.channel.position())
            } else {
                inner.write(bytes, off, len)
            }
        }

        override fun writeAt(position: Long, bytes: ByteArray) = inner.writeAt(position, bytes)
    }

    /** [n] zero bytes. */
    private class Zeros(private var n: Long) : InputStream() {
        override fun read(): Int = if (n-- > 0) 0 else -1

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (n <= 0) return -1
            val k = minOf(len.toLong(), n).toInt()
            b.fill(0, off, off + k)
            n -= k
            return k
        }
    }

    companion object {
        fun crc(b: ByteArray): Long = CRC32().apply { update(b) }.value

        fun zerosCrc(n: Long): Long {
            val crc = CRC32()
            val buf = ByteArray(1 shl 20)
            var left = n
            while (left > 0) {
                val k = minOf(buf.size.toLong(), left).toInt()
                crc.update(buf, 0, k)
                left -= k
            }
            return crc.value
        }
    }
}
