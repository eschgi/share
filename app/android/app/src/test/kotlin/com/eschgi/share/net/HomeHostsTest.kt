package com.eschgi.share.net

import com.eschgi.share.contract
import org.junit.Assert.assertEquals
import org.junit.Test

/** contract/home_hosts.json, as the server and the Dart half read it, and some more addresses. */
class HomeHostsTest {
    @Test
    fun theContractsHosts() {
        val hosts = contract("home_hosts.json").getJSONArray("hosts")
        for (i in 0 until hosts.length()) {
            val h = hosts.getJSONObject(i)
            assertEquals(h.getString("host"), h.getBoolean("home"), isHomeHost(h.getString("host")))
        }
    }

    @Test
    fun moreAddresses() {
        for ((host, home) in listOf(
            "[fd12::1]" to true,
            "::ffff:192.168.1.1" to true,
            "::ffff:8.8.8.8" to false,
            "fe80::1%wlan0" to true,
            "share.local." to true,
            "127.0.0.1" to true,
            "01.2.3.4" to false,
            "256.1.1.1" to false,
            "1:2:3:4:5:6:7:8:9" to false,
            "fd12:::1" to false,
            "a.localhost" to true,
            ".local" to false,
        )) {
            assertEquals(host, home, isHomeHost(host))
        }
    }
}
