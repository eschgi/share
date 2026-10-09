package com.eschgi.share.zip

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.util.zip.CRC32
import kotlin.random.Random

/** What the receiving phone keeps: which parts are saved and where, and pieces until their file is whole. */
class ZipInboxTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val video = Random(9).nextBytes(250_000)
    private val cuts = listOf(0 until 100_000, 100_000 until 200_000, 200_000 until 250_000)
    private val set = "1c6f3a62-5d0e-4c9b-9f7e-2b8f0f4d6a11"

    private fun piece(n: Int) = ShareJson.Piece("VID_1.mp4", n, 3, cuts[n - 1].first.toLong(), video.size.toLong(), listOf(2, 3, 4))

    private fun add(inbox: ZipInbox, n: Int, bytes: ByteArray = video.sliceArray(cuts[n - 1])): ZipInbox.Cut =
        inbox.addPiece(set, piece(n), "video/mp4", "2026-10-17T14:15:02+02:00", ByteArrayInputStream(bytes), bytes.size.toLong(), crc(video.sliceArray(cuts[n - 1])))

    @Test
    fun piecesInAnyOrderMakeTheWholeFile() {
        for (order in listOf(listOf(1, 2, 3), listOf(3, 1, 2), listOf(2, 2, 3, 1))) {
            val inbox = ZipInbox(tmp.newFolder())
            var cut: ZipInbox.Cut? = null
            for (n in order) {
                assertFalse(cut?.whole ?: false)
                cut = add(inbox, n)
            }
            assertTrue(cut!!.whole)
            assertEquals(setOf(1, 2, 3), cut.have)
            assertArrayEquals(video, cut.data.readBytes())
            assertEquals(listOf(cut), inbox.cuts(set))
            inbox.done(set, "VID_1.mp4")
            assertTrue(inbox.cuts(set).isEmpty())
            assertFalse(cut.data.exists())
        }
    }

    @Test
    fun aBrokenPieceIsntCountedAndCanComeAgain() {
        val inbox = ZipInbox(tmp.newFolder())
        add(inbox, 1)
        val broken = video.sliceArray(cuts[1]).also { it[500] = (it[500] + 1).toByte() }
        try {
            add(inbox, 2, broken)
            fail("a broken piece went in")
        } catch (e: BrokenPiece) {
            assertEquals(2, e.number)
        }
        assertEquals(setOf(1), inbox.cuts(set).single().have)
        add(inbox, 2)
        val cut = add(inbox, 3)
        assertTrue(cut.whole)
        assertArrayEquals(video, cut.data.readBytes())
    }

    @Test
    fun aSetRemembersItsPartsAndWhereTheyWent() {
        val inbox = ZipInbox(tmp.newFolder())
        assertNull(inbox.set(set))
        inbox.opened(set, "Dolomites 15–18 Oct 2026", 4)
        assertEquals(ZipInbox.SetState("Dolomites 15–18 Oct 2026", 4, emptySet(), null, null, emptySet()), inbox.set(set))
        inbox.goesTo(set, "folder", "f4mily")
        inbox.savedEntry(set, "IMG_1.jpg")
        inbox.savedEntry(set, "IMG_1.jpg")
        inbox.savedPart(set, 3)
        inbox.savedPart(set, 1)
        assertEquals(ZipInbox.SetState("Dolomites 15–18 Oct 2026", 4, setOf(1, 3), "folder", "f4mily", setOf("IMG_1.jpg")), inbox.set(set))
        // Ids that aren't plain still get a folder of their own.
        inbox.opened("../../etc", "x", 1)
        assertEquals("x", inbox.set("../../etc")!!.name)
    }

    @Test
    fun whatWasntTouchedForTwoWeeksGoes() {
        val root = tmp.newFolder()
        val inbox = ZipInbox(root)
        add(inbox, 1)
        inbox.opened(set, "Dolomites", 4)
        inbox.opened("other", "Other", 1)
        val now = System.currentTimeMillis()
        inbox.trim(now + 13L * 24 * 3600_000)
        assertEquals(1, inbox.cuts(set).size)
        root.walkBottomUp().forEach { it.setLastModified(now - 20L * 24 * 3600_000) }
        inbox.trim(now + 15L * 24 * 3600_000)
        assertTrue(inbox.cuts(set).isEmpty())
        assertNull(inbox.set(set))
        assertNull(inbox.set("other"))
    }

    private fun crc(b: ByteArray) = CRC32().apply { update(b) }.value.toInt()
}
