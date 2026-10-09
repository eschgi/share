package com.eschgi.share.zip

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.charset.Charset
import java.time.ZoneId
import java.util.zip.CRC32
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import kotlin.random.Random

/** ZIPs from other tools, as a computer or another app makes them, open too; broken ones say so. */
class ZipReaderTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val zone = ZoneId.of("Europe/Rome")
    private val photo = Random(7).nextBytes(80_000)
    private val text = "Hello, Ötzi! ".repeat(2_000).toByteArray()

    private fun <T> reading(file: File, block: (ZipReader) -> T): T = RandomAccessFile(file, "r").use { block(ZipReader(it.channel, zone)) }

    private fun zip(charset: Charset = Charsets.UTF_8, comment: String? = null, fill: ZipOutputStream.() -> Unit): File {
        val out = ByteArrayOutputStream()
        ZipOutputStream(out, charset).use {
            if (comment != null) it.setComment(comment)
            it.fill()
        }
        return tmp.newFile().apply { writeBytes(out.toByteArray()) }
    }

    @Test
    fun deflatedEntriesWithDataDescriptorsOpen() {
        // ZipOutputStream writes a data descriptor after every deflated entry.
        val file = zip {
            putNextEntry(ZipEntry("folder/"))
            putNextEntry(ZipEntry("folder/notes.txt"))
            write(text)
            putNextEntry(ZipEntry("IMG_1.jpg"))
            write(photo)
        }
        reading(file) { zip ->
            assertEquals(listOf("folder/", "folder/notes.txt", "IMG_1.jpg"), zip.entries.map { it.name })
            assertTrue(zip.entries[0].isFolder)
            val notes = zip.entries[1]
            assertEquals(ZipReader.DEFLATED, notes.method)
            assertTrue(notes.readable)
            assertEquals(text.size.toLong(), notes.size)
            assertTrue(notes.compressedSize < notes.size)
            assertArrayEquals(text, zip.open(notes).readBytes())
            assertEquals(crc(photo), zip.entries[2].crc)
            assertArrayEquals(photo, zip.open(zip.entries[2]).readBytes())
        }
    }

    @Test
    fun storedEntriesOpenWhereTheirDataIs() {
        val file = zip {
            putNextEntry(ZipEntry("IMG_1.jpg").apply {
                method = ZipEntry.STORED
                size = photo.size.toLong()
                crc = CRC32().apply { update(photo) }.value
            })
            write(photo)
        }
        reading(file) { zip ->
            val e = zip.entries.single()
            assertEquals(ZipReader.STORED, e.method)
            val at = zip.dataOffset(e)
            assertArrayEquals(photo, file.readBytes().copyOfRange(at.toInt(), at.toInt() + photo.size))
            assertArrayEquals(photo, zip.open(e).readBytes())
        }
    }

    @Test
    fun namesWithoutTheUtf8FlagAreTheOldCodePage() {
        val file = zip(Charset.forName("IBM437")) { putNextEntry(ZipEntry("Übung Ärger.txt")); write(text) }
        reading(file) { zip -> assertEquals("Übung Ärger.txt", zip.entries.single().name) }
        assertEquals("ÇüéâäàåçêëèïîìÄÅ", ZipReader.cp437(ByteArray(16) { (0x80 + it).toByte() }))
    }

    @Test
    fun aCommentAtTheEndIsSkipped() {
        val file = zip(comment = "Made on a computer. ".repeat(50)) { putNextEntry(ZipEntry("a.txt")); write(text) }
        reading(file) { zip -> assertArrayEquals(text, zip.open(zip.entries.single()).readBytes()) }
    }

    @Test
    fun anEncryptedEntryIsListedButDoesntOpen() {
        val file = zip { putNextEntry(ZipEntry("secret.txt")); write(text) }
        // Sets the "encrypted" flag in the central directory, as a ZIP with a password has it.
        val bytes = file.readBytes()
        val central = (bytes.size - 22 downTo 0).first { bytes[it] == 0x50.toByte() && bytes[it + 1] == 0x4b.toByte() && bytes[it + 2] == 1.toByte() && bytes[it + 3] == 2.toByte() }
        bytes[central + 8] = (bytes[central + 8].toInt() or 1).toByte()
        file.writeBytes(bytes)
        reading(file) { zip ->
            val e = zip.entries.single()
            assertTrue(e.encrypted)
            assertFalse(e.readable)
            try {
                zip.open(e)
                fail("an encrypted entry opened")
            } catch (expected: IOException) {
                // as it should
            }
        }
    }

    @Test
    fun brokenOnesSaySo() {
        val good = zip { putNextEntry(ZipEntry("a.txt")); write(text) }.readBytes()
        for ((why, bytes) in listOf(
            "empty" to ByteArray(0),
            "not a ZIP" to "just some text, long enough to be looked at".toByteArray(),
            "cut off at the end" to good.copyOf(good.size - 10),
            "cut off in the middle" to good.copyOf(good.size / 2),
        )) {
            val file = tmp.newFile().apply { writeBytes(bytes) }
            try {
                reading(file) { it.entries }
                fail("$why opened")
            } catch (expected: IOException) {
                // NotAZip, or the end of the file
            }
        }
    }

    @Test
    fun anEntryCantInflateIntoMoreThanItSays() {
        val file = zip { putNextEntry(ZipEntry("a.txt")); write(text) }
        // Makes the central directory say the entry is shorter than what inflating it gives.
        val bytes = file.readBytes()
        val central = (bytes.size - 22 downTo 0).first { bytes[it] == 0x50.toByte() && bytes[it + 1] == 0x4b.toByte() && bytes[it + 2] == 1.toByte() && bytes[it + 3] == 2.toByte() }
        bytes[central + 24] = 10
        for (k in 25..27) bytes[central + k] = 0
        file.writeBytes(bytes)
        reading(file) { zip ->
            try {
                zip.open(zip.entries.single()).readBytes()
                fail("it inflated into more than it said")
            } catch (expected: IOException) {
                // as it should
            }
        }
    }

    private fun crc(b: ByteArray) = CRC32().apply { update(b) }.value.toInt()
}
