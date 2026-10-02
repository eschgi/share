package com.eschgi.share.transfer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** A text shared into the app opens the Share link in it. */
class OutboxTest {
    @Test
    fun findsTheLinkInAMessage() {
        assertEquals(
            "https://share.example.com/join#shi_0123456789abcdefghijklmnop",
            Outbox.linkIn("Maria invited you to Share: https://share.example.com/join#shi_0123456789abcdefghijklmnop."),
        )
        assertEquals("https://share.example.com/#K7M2Q", Outbox.linkIn("Send your photos (PIN in the link: https://share.example.com/#K7M2Q)!"))
        assertEquals("http://192.168.8.1:8080/#K7M2Q", Outbox.linkIn("http://192.168.8.1:8080/#K7M2Q"))
    }

    @Test
    fun aTextWithoutOneOpensNothing() {
        assertNull(Outbox.linkIn("See you tomorrow"))
        assertNull(Outbox.linkIn("ftp://example.com/file"))
    }
}
