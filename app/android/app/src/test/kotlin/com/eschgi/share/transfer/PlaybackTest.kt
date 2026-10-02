package com.eschgi.share.transfer

import com.eschgi.share.data.ServerConfig
import org.junit.Assert.assertEquals
import org.junit.Test

/** Where the player plays from, for the ways a phone reaches its server. */
class PlaybackTest {
    private fun config(public: String, local: String?) =
        ServerConfig.parse(
            """{"public_url":"$public","local_url":"${local ?: ""}","pins":${if (local?.startsWith("https") == true) "[\"ab\"]" else "[]"},"server_id":"s"}""",
        )!!

    private val tunnel = config("https://share.example.com", null)
    private val homeHttp = config("https://share.example.com", "http://192.168.8.1:8080")
    private val homeHttps = config("https://share.example.com", "https://192.168.8.1:8443")
    private val onlyHome = config("http://192.168.8.1:8080", null)

    private val proved: (Boolean) -> Boolean = { true }
    private val notProved: (Boolean) -> Boolean = { false }

    @Test
    fun aCopyOnThePhoneComesFirst() {
        assertEquals(Playback.Way.Copy, Playback.way(true, homeHttps, local = true, proved))
    }

    @Test
    fun awayFromHomeItStreamsOverThePublicAddress() {
        assertEquals(Playback.Way.Stream("https://share.example.com"), Playback.way(false, tunnel, local = false, notProved))
        assertEquals(Playback.Way.Stream("https://share.example.com"), Playback.way(false, homeHttps, local = false, notProved))
    }

    @Test
    fun atHomeOverPlainHttpOnlyAfterTheProof() {
        assertEquals(Playback.Way.Stream("http://192.168.8.1:8080"), Playback.way(false, homeHttp, local = true, proved))
        // Without it the key goes to the public address instead.
        assertEquals(Playback.Way.Stream("https://share.example.com"), Playback.way(false, homeHttp, local = true, notProved))
    }

    @Test
    fun theHttpsPortAtHomeIsFetchedFirst() {
        // The player can't pin Share's own certificate.
        assertEquals(Playback.Way.Fetch, Playback.way(false, homeHttps, local = true, proved))
    }

    @Test
    fun aServerOnlyAtHomeNeedsTheProof() {
        assertEquals(Playback.Way.Stream("http://192.168.8.1:8080"), Playback.way(false, onlyHome, local = false, proved))
        assertEquals(Playback.Way.Nowhere, Playback.way(false, onlyHome, local = false, notProved))
    }
}
