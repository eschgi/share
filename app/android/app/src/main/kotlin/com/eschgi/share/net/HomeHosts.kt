package com.eschgi.share.net

import java.util.Locale

/**
 * Whether a host is on a home network or this phone: the only places where the app speaks plain
 * http, as the server does (contract/home_hosts.json, and isHomeHost in lib/data/server.dart). A
 * name that public DNS could answer doesn't count; it could lead to the internet. Addresses are
 * read here by hand: InetAddress would ask DNS about anything that isn't one.
 */
fun isHomeHost(host: String): Boolean {
    var h = host.lowercase(Locale.ROOT)
    if (h.endsWith('.')) h = h.dropLast(1)
    if (h.startsWith('[') && h.endsWith(']')) h = h.substring(1, h.length - 1)
    ipv4(h)?.let { return homeV4(it) }
    if (':' in h) return ipv6(h)?.let(::homeV6) ?: false
    if (h == "localhost" || h.endsWith(".localhost")) return true
    return listOf(".local", ".home.arpa", ".internal").any { h.endsWith(it) && h.length > it.length }
}

/** The 4 bytes of a dotted IPv4 address, or null. */
private fun ipv4(s: String): IntArray? {
    val parts = s.split('.')
    if (parts.size != 4) return null
    return IntArray(4) { i ->
        val p = parts[i]
        if (p.isEmpty() || p.length > 3 || !p.all { it in '0'..'9' } || (p.length > 1 && p[0] == '0')) return null
        p.toInt().takeIf { it <= 255 } ?: return null
    }
}

private fun homeV4(b: IntArray) =
    b[0] == 127 || b[0] == 10 || (b[0] == 172 && b[1] in 16..31) || (b[0] == 192 && b[1] == 168) || (b[0] == 169 && b[1] == 254)

/** The 16 bytes of an IPv6 address, which may end in IPv4 and have a zone (%wlan0); or null. */
private fun ipv6(s: String): IntArray? {
    val halves = s.substringBefore('%').split("::")
    if (halves.size > 2) return null
    fun groups(part: String): List<Int>? {
        if (part.isEmpty()) return emptyList()
        val pieces = part.split(':')
        val out = mutableListOf<Int>()
        for ((i, p) in pieces.withIndex()) {
            if (i == pieces.lastIndex && '.' in p) {
                val v4 = ipv4(p) ?: return null
                out += (v4[0] shl 8) or v4[1]
                out += (v4[2] shl 8) or v4[3]
            } else {
                if (p.isEmpty() || p.length > 4 || !p.all { it in '0'..'9' || it in 'a'..'f' }) return null
                out += p.toInt(16)
            }
        }
        return out
    }
    val head = groups(halves[0]) ?: return null
    val tail = if (halves.size == 2) groups(halves[1]) ?: return null else emptyList()
    val gap = 8 - head.size - tail.size
    if (if (halves.size == 2) gap < 1 else gap != 0) return null
    val all = head + List(if (halves.size == 2) gap else 0) { 0 } + tail
    return IntArray(16) { i -> (all[i / 2] shr if (i % 2 == 0) 8 else 0) and 0xff }
}

private fun homeV6(b: IntArray): Boolean {
    if ((0 until 15).all { b[it] == 0 } && b[15] == 1) return true // ::1
    if (b[0] == 0xfe && (b[1] and 0xc0) == 0x80) return true // link-local, fe80::/10
    if ((b[0] and 0xfe) == 0xfc) return true // unique local, fc00::/7
    val mapped = (0 until 10).all { b[it] == 0 } && b[10] == 0xff && b[11] == 0xff
    return mapped && homeV4(intArrayOf(b[12], b[13], b[14], b[15]))
}
