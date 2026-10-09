package com.eschgi.share.zip

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.io.InputStream
import java.io.RandomAccessFile
import java.time.ZoneId
import java.util.zip.ZipFile

/** How files go into ZIPs: in order, gaps filled, only what can't fit cut, and every part exactly as big as planned. */
class ZipPlanTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val zone = ZoneId.of("Europe/Rome")
    private val set = PackSet("1c6f3a62-5d0e-4c9b-9f7e-2b8f0f4d6a11", "Dolomites 15–18 Oct 2026", "Made with Share.", 1_791_900_000_000L, "0.2.0", zone)
    private val t0 = 1_791_800_000_000L
    private fun photo(i: Int, size: Long) = PackFile("IMG_$i.jpg", size, "image/jpeg", t0 + i * 60_000L)

    @Test
    fun withoutALimitAllGoInOne() {
        val files = (1..30).map { photo(it, 5_000_000) } + PackFile("VID_1.mp4", 6_000_000_000, "video/mp4", t0 + 99 * 60_000L)
        val plan = ZipPlan.make(files, null, set)
        assertEquals(1, plan.parts.size)
        assertEquals(31, plan.parts[0].entries.size)
        assertTrue(plan.cut.isEmpty())
        assertTrue(plan.parts[0].size > 6_150_000_000)
    }

    @Test
    fun filesGoInTheOrderTheyWereTaken() {
        val files = listOf(photo(3, 10), photo(1, 10), photo(2, 10))
        val names = ZipPlan.make(files, null, set).parts[0].entries.map { it.name }
        assertEquals(listOf("IMG_1.jpg", "IMG_2.jpg", "IMG_3.jpg"), names)
    }

    @Test
    fun eachPartHasRoomForItsFilesAndALaterPhotoFillsAGap() {
        val limit = 1_950_000_000L
        val files = listOf(
            photo(1, 900_000_000), photo(2, 900_000_000), photo(3, 900_000_000), // 1 and 2 fill part 1, 3 starts part 2
            photo(4, 100_000_000), // into the gap left in part 1
        )
        val plan = ZipPlan.make(files, limit, set)
        assertEquals(2, plan.parts.size)
        assertEquals(listOf("IMG_1.jpg", "IMG_2.jpg", "IMG_4.jpg"), plan.parts[0].entries.map { it.name })
        assertEquals(listOf("IMG_3.jpg"), plan.parts[1].entries.map { it.name })
        assertTrue(plan.parts.all { it.size <= limit })
        assertTrue(plan.cut.isEmpty())
    }

    @Test
    fun onlyAFileBiggerThanAPartIsCut() {
        // Screen 104: 51 photos and a video of 2.4 GB in ZIPs of 1.95 GB.
        val limit = 1_950_000_000L
        val files = (1..51).map { photo(it, 110_000_000) } + PackFile("VID_20261017_141502.mp4", 2_412_345_678, "video/mp4", t0 + 99 * 60_000L)
        val plan = ZipPlan.make(files, limit, set)
        assertFalse(plan.tooMany)
        assertTrue(plan.parts.all { it.size <= limit })
        assertEquals(1, plan.cut.size)
        val cut = plan.cut.single()
        assertEquals("VID_20261017_141502.mp4", cut.name)
        val pieces = plan.parts.flatMap { p -> p.entries.filter { it.piece != null }.map { p.number to it } }
        assertEquals(listOf("VID_20261017_141502.mp4.001", "VID_20261017_141502.mp4.002"), pieces.map { it.second.name })
        assertEquals(cut.parts, pieces.map { it.first })
        assertEquals(pieces.map { it.first }, pieces.map { it.second.piece!!.parts }.first())
        // The pieces cover the video once, end to end.
        assertEquals(0L, pieces[0].second.offset)
        assertEquals(pieces[0].second.length, pieces[1].second.offset)
        assertEquals(2_412_345_678, pieces.sumOf { it.second.length })
        // Every photo stays whole.
        assertEquals(51, plan.parts.sumOf { it.wholeFiles })
    }

    @Test
    fun theFirstPieceFillsTheLastPartOnlyWhenThereIsRoomEnough() {
        val limit = 1_000_000_000L
        // Part 1 keeps about 300 MB free, more than a tenth: the video starts there.
        val roomy = ZipPlan.make(listOf(photo(1, 700_000_000), PackFile("VID.mp4", 1_200_000_000, "video/mp4", t0 + 10 * 60_000L)), limit, set)
        assertEquals(listOf(1, 2), roomy.cut.single().parts)
        // Part 1 keeps about 50 MB free, less than a tenth: the video starts a new part.
        val tight = ZipPlan.make(listOf(photo(1, 950_000_000), PackFile("VID.mp4", 1_200_000_000, "video/mp4", t0 + 10 * 60_000L)), limit, set)
        assertEquals(listOf(2, 3), tight.cut.single().parts)
    }

    @Test
    fun everyPartIsExactlyAsBigAsPlanned() {
        // Small sizes, so that every part can be written and measured.
        val limit = 300_000L
        val files = (1..20).map { photo(it, (it * 7_919L) % 60_000 + 1_000) } +
            PackFile("VID_1.mp4", 700_000, "video/mp4", t0 + 5 * 60_000L + 1) +
            listOf(photo(21, 0))
        val plan = ZipPlan.make(files, limit, set)
        assertFalse(plan.tooMany)
        assertTrue(plan.parts.size >= 3)
        for (part in plan.parts) {
            val file = tmp.newFile("part${part.number}.zip")
            RandomAccessFile(file, "rw").use { raf ->
                ZipWriter(part.layout, ChannelOut(raf.channel)).write({ i -> source(plan, part, i) })
            }
            assertEquals(part.size, file.length())
            assertTrue(file.length() <= limit)
            ZipFile(file).use { zip ->
                val names = zip.entries().toList().map { it.name }
                assertEquals(listOf(ShareJson.NAME) + part.entries.map { it.name }, names)
                val json = (ShareJson.read(zip.getInputStream(zip.getEntry(ShareJson.NAME)).readBytes()) as ShareJson.Ours).json
                assertEquals(part.json, json)
                assertTrue(json.matches(zip.entries().toList().filter { it.name != ShareJson.NAME }.associate { it.name to it.size }))
                assertEquals(plan.parts.size, json.parts)
                assertEquals(files.size, json.setFiles)
            }
        }
    }

    @Test
    fun moreThan100PartsAreTooMany() {
        val plan = ZipPlan.make((1..80).map { photo(it, 90_000_000) }, 15_000_000, set)
        assertTrue(plan.tooMany)
        assertTrue(plan.parts.isEmpty())
        assertFalse(ZipPlan.make((1..80).map { photo(it, 90_000_000) }, 1_950_000_000, set).tooMany)
    }

    @Test
    fun aLimitWithoutRoomIsTooMany() {
        assertTrue(ZipPlan.make(listOf(photo(1, 10)), 10_000, set).tooMany)
    }

    @Test
    fun noFilesNoParts() {
        val plan = ZipPlan.make(emptyList(), 1_950_000_000, set)
        assertTrue(plan.parts.isEmpty())
        assertFalse(plan.tooMany)
    }

    @Test
    fun namesAreCleanAndUnique() {
        val files = listOf(
            PackFile("IMG_1.jpg", 1, "image/jpeg", t0),
            PackFile("img_1.JPG", 1, "image/jpeg", t0 + 1),
            PackFile("share.json", 1, "application/json", t0 + 2),
            PackFile("a/b\\c:d*e?.pdf", 1, "application/pdf", t0 + 3),
            PackFile("  ..  ", 1, "application/octet-stream", t0 + 4),
            PackFile("noext", 1, "application/octet-stream", t0 + 5),
            PackFile("noext", 1, "application/octet-stream", t0 + 6),
        )
        val names = ZipPlan.make(files, null, set).parts[0].entries.map { it.name }
        assertEquals(listOf("IMG_1.jpg", "img_1 (2).JPG", "share (2).json", "a_b_c_d_e_.pdf", "file", "noext", "noext (2)"), names)
    }

    @Test
    fun aFileNamedLikeAPieceGetsANameOfItsOwn() {
        val limit = 300_000L
        val files = listOf(PackFile("VID.mp4", 500_000, "video/mp4", t0 + 2), PackFile("VID.mp4.001", 10, "application/octet-stream", t0 + 1))
        val plan = ZipPlan.make(files, limit, set)
        val names = plan.parts.flatMap { p -> p.entries.map { it.name } }
        assertTrue("VID.mp4.001" in names)
        assertTrue("VID (2).mp4.001" in names || "VID.mp4 (2).001" in names)
        assertEquals(names.size, names.map { it.lowercase() }.toSet().size)
    }

    @Test
    fun sharesJsonSaysWhenInTheSendersZone() {
        assertEquals("2026-10-04T16:15:02+02:00", ZipPlan.iso(1_791_123_302_000L, zone))
        assertEquals("2026-10-04T14:15:02Z", ZipPlan.iso(1_791_123_302_000L, ZoneId.of("UTC")))
        assertNull(ZipPlan.make(listOf(photo(1, 10)), null, set).parts[0].entries[0].piece)
    }

    /** The bytes of an entry of [part]: share.json, or a slice of a file whose bytes are its index, over and over. */
    private fun source(plan: ZipPlan, part: ZipPlan.Part, i: Int): InputStream {
        if (i == 0) return ByteArrayInputStream(part.jsonBytes)
        val e = part.entries[i - 1]
        return object : InputStream() {
            var left = e.length
            override fun read(): Int = if (left-- > 0) e.file and 0xFF else -1
        }
    }
}
