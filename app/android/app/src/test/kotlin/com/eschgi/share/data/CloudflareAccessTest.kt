package com.eschgi.share.data

import com.eschgi.share.net.ServerConnection
import com.eschgi.share.transfer.Playback
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The Cloudflare Access token goes to the public address only, over https. */
class CloudflareAccessTest {
    private val id = "0123456789abcdef0123456789abcdef.access"
    private val secret = "f".repeat(64)
    private val access = CloudflareAccess("share.example.com", id, secret)

    @Test
    fun readsWhatDartStores() {
        assertEquals(access, CloudflareAccess.parse("""{"host": "Share.Example.com", "client_id": "$id", "client_secret": "$secret", "on": true}"""))
        assertNull("switched off", CloudflareAccess.parse("""{"host": "share.example.com", "client_id": "$id", "client_secret": "$secret", "on": false}"""))
        assertNull(CloudflareAccess.parse("""{"host": "share.example.com", "client_id": "$id"}"""))
        assertNull(CloudflareAccess.parse("not json"))
        assertNull(CloudflareAccess.parse(null))
    }

    @Test
    fun onlyThePublicAddressOverHttps() {
        assertTrue(access.appliesTo("https://share.example.com"))
        assertTrue(access.appliesTo("https://SHARE.example.com:443"))
        assertFalse("plain http", access.appliesTo("http://share.example.com"))
        assertFalse("the local address", access.appliesTo("https://192.168.1.20:8443"))
        assertFalse("a bucket", access.appliesTo("https://bucket.r2.cloudflarestorage.com"))
        assertFalse(access.toString().contains(secret))
    }

    @Test
    fun theServerConnectionAddsItOverThePublicAddress() {
        val config = ServerConfig("https://share.example.com", "http://192.168.1.20:8080", emptyList(), "s1", access = access)
        val public = ServerConnection(config, "shd_key").open("/api/files", local = false)
        assertEquals(id, public.getRequestProperty("CF-Access-Client-Id"))
        assertEquals(secret, public.getRequestProperty("CF-Access-Client-Secret"))
        val home = ServerConnection(config, null).open("/api/info", local = true)
        assertNull("not at home", home.getRequestProperty("CF-Access-Client-Id"))
        val without = ServerConnection(config.copy(access = null), "shd_key").open("/api/files", local = false)
        assertNull(without.getRequestProperty("CF-Access-Client-Id"))
    }

    @Test
    fun theVideoPlayerGetsItToo() {
        @Suppress("UNCHECKED_CAST")
        fun headers(base: String) = Playback.streamOf(base, "f1", "shd_key", access)["headers"] as Map<String, String>
        assertEquals(secret, headers("https://share.example.com")["CF-Access-Client-Secret"])
        assertNull(headers("http://192.168.1.20:8080")["CF-Access-Client-Secret"])
    }
}
