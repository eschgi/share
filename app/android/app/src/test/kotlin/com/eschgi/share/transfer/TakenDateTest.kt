package com.eschgi.share.transfer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.ZonedDateTime

/** A video's date, as Android's MediaMetadataRetriever writes it. */
class TakenDateTest {
    @Test
    fun readsAVideosDate() {
        assertEquals(ZonedDateTime.parse("2025-06-14T16:32:10Z").toInstant().toEpochMilli(), TakenDate.videoTime("20250614T163210.000Z"))
    }

    @Test
    fun anEmptyDateIsNone() {
        assertNull("1904, QuickTime's zero", TakenDate.videoTime("19040101T000000.000Z"))
        assertNull(TakenDate.videoTime("19700101T000000.000Z"))
        assertNull(TakenDate.videoTime("garbage"))
        assertNull(TakenDate.videoTime(null))
    }
}
