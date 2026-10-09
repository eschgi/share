package com.eschgi.share.zip

import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId

/** A file in a ZIP: its name inside, its length, and when it last changed, in ms since 1970. */
data class ZipItem(val name: String, val size: Long, val modified: Long)

/**
 * A ZIP of stored files, laid out as the server's zipstream lays one out (server/internal/zipstream):
 * the CRC-32 and sizes in each local header and no data descriptor, which Java's ZipInputStream, and
 * so many Android apps, can't read for stored files; UTF-8 names; Info-ZIP's extended timestamp next
 * to the MS-DOS time; Zip64 fields only for what doesn't fit in 32 bits (APPNOTE.TXT 6.3). Its bytes
 * depend only on the items' names, sizes and times, so a ZIP's size is known before a file is read.
 * The MS-DOS times are the wall clock in [zone], which is what tools without the extended timestamp
 * show.
 */
class ZipLayout(val items: List<ZipItem>, private val zone: ZoneId) {
    private val names = items.map { it.name.toByteArray(Charsets.UTF_8) }

    /** Where each item's local header starts. */
    val offsets = LongArray(items.size)

    /** Where the central directory starts, and its length. */
    val dir: Long
    val dirLen: Long

    /** The whole ZIP's length in bytes. */
    val size: Long

    init {
        val seen = HashSet<String>()
        var off = 0L
        for ((i, item) in items.withIndex()) {
            checkName(item.name, names[i])
            require(seen.add(item.name)) { "${item.name} is in the ZIP twice" }
            require(item.size >= 0) { "${item.name} can't be ${item.size} bytes long" }
            offsets[i] = off
            off += localLen(names[i].size, item.size) + item.size
        }
        dir = off
        dirLen = items.indices.sumOf { centralLen(names[it].size, items[it].size, offsets[it]) }
        size = dir + dirLen + endLen(items.size, dirLen, dir)
    }

    /** Item [i]'s local header, with [crc] as its CRC-32. */
    fun local(i: Int, crc: Int): ByteArray {
        val item = items[i]
        val zip64 = zip64(item.size)
        val out = Bytes(localLen(names[i].size, item.size).toInt())
        out.u32(LOCAL_SIG)
        out.u16(if (zip64) VERSION_45 else VERSION_20)
        out.u16(FLAG_UTF8)
        out.u16(0) // stored
        dosTime(item.modified).let { (time, date) ->
            out.u16(time)
            out.u16(date)
        }
        out.u32(crc.toLong() and UINT32_MAX)
        out.u32(if (zip64) UINT32_MAX else item.size) // compressed
        out.u32(if (zip64) UINT32_MAX else item.size) // uncompressed
        out.u16(names[i].size)
        out.u16(TIME_EXTRA_LEN + if (zip64) 20 else 0)
        out.bytes(names[i])
        if (zip64) {
            // A local Zip64 extra has both sizes, always.
            out.u16(ZIP64_TAG)
            out.u16(16)
            out.u64(item.size)
            out.u64(item.size)
        }
        time(out, item.modified)
        return out.done()
    }

    /** The central directory, with each item's CRC-32 from [crcs]. */
    fun central(crcs: IntArray): ByteArray {
        val out = Bytes(dirLen.toInt())
        for ((i, item) in items.withIndex()) {
            val offset = offsets[i]
            val zip64 = zip64(item.size)
            val k = escaped(item.size, offset)
            val version = if (k > 0) VERSION_45 else VERSION_20
            out.u32(CENTRAL_SIG)
            out.u16(version) // made by: on MS-DOS (high byte 0), so unzip applies the umask
            out.u16(version) // needed
            out.u16(FLAG_UTF8)
            out.u16(0) // stored
            dosTime(item.modified).let { (time, date) ->
                out.u16(time)
                out.u16(date)
            }
            out.u32(crcs[i].toLong() and UINT32_MAX)
            out.u32(if (zip64) UINT32_MAX else item.size)
            out.u32(if (zip64) UINT32_MAX else item.size)
            out.u16(names[i].size)
            out.u16(TIME_EXTRA_LEN + if (k > 0) 4 + 8 * k else 0)
            out.u16(0) // comment length
            out.u16(0) // disk
            out.u16(0) // internal attributes
            out.u32(0) // external attributes
            out.u32(if (offset >= UINT32_MAX) UINT32_MAX else offset)
            out.bytes(names[i])
            if (k > 0) {
                // Only the fields that didn't fit, in this order.
                out.u16(ZIP64_TAG)
                out.u16(8 * k)
                if (zip64) {
                    out.u64(item.size)
                    out.u64(item.size)
                }
                if (offset >= UINT32_MAX) out.u64(offset)
            }
            time(out, item.modified)
        }
        return out.done()
    }

    /**
     * The end of central directory record, after the Zip64 ones when the number of items, or the
     * size or start of the directory, doesn't fit. An item with Zip64 fields puts the directory
     * past 4 GiB, so the Zip64 records are there whenever an item has such fields, as some readers
     * expect.
     */
    fun end(): ByteArray {
        val n = items.size.toLong()
        val out = Bytes(endLen(items.size, dirLen, dir).toInt())
        var n16 = n
        var dirLen32 = dirLen
        var dir32 = dir
        if (needs64(items.size, dirLen, dir)) {
            out.u32(END64_SIG)
            out.u64(44) // the size of the rest of the record
            out.u16(VERSION_45)
            out.u16(VERSION_45)
            out.u32(0) // this disk
            out.u32(0) // the disk where the directory starts
            out.u64(n) // items on this disk
            out.u64(n) // items in all
            out.u64(dirLen)
            out.u64(dir)
            out.u32(LOCATOR64_SIG)
            out.u32(0) // the disk of the Zip64 end record
            out.u64(dir + dirLen)
            out.u32(1) // disks in all
            // All of them, so a reader finds the Zip64 record whichever field it checks.
            n16 = UINT16_MAX.toLong()
            dirLen32 = UINT32_MAX
            dir32 = UINT32_MAX
        }
        out.u32(END_SIG)
        out.u16(0) // this disk
        out.u16(0) // the disk where the directory starts
        out.u16(n16.toInt())
        out.u16(n16.toInt())
        out.u32(dirLen32)
        out.u32(dir32)
        out.u16(0) // comment length
        return out.done()
    }

    /** The extended timestamp, which unzip tools prefer to the MS-DOS time: in UTC, exact to the second. */
    private fun time(out: Bytes, modified: Long) {
        out.u16(TIME_TAG)
        out.u16(5)
        out.u8(1) // just the modification time
        out.u32(unixTime(modified))
    }

    /**
     * MS-DOS's way of writing the wall clock, which covers 1980 to 2107 in steps of two seconds.
     * Earlier and later times get the first or last one there is.
     */
    private fun dosTime(modified: Long): Pair<Int, Int> {
        var t = LocalDateTime.ofInstant(Instant.ofEpochMilli(modified), zone)
        if (t.year < 1980) t = LocalDateTime.of(1980, 1, 1, 0, 0, 0)
        if (t.year > 2107) t = LocalDateTime.of(2107, 12, 31, 23, 59, 59)
        return ((t.hour shl 11) or (t.minute shl 5) or (t.second / 2)) to (((t.year - 1980) shl 9) or (t.monthValue shl 5) or t.dayOfMonth)
    }

    /** A little-endian buffer of a known length. */
    private class Bytes(n: Int) {
        private val b = ByteArray(n)
        private var at = 0

        fun u8(v: Int) {
            b[at++] = v.toByte()
        }

        fun u16(v: Int) {
            u8(v)
            u8(v ushr 8)
        }

        fun u32(v: Long) {
            for (s in 0 until 32 step 8) u8((v ushr s).toInt())
        }

        fun u64(v: Long) {
            for (s in 0 until 64 step 8) u8((v ushr s).toInt())
        }

        fun bytes(v: ByteArray) {
            v.copyInto(b, at)
            at += v.size
        }

        fun done(): ByteArray {
            check(at == b.size) { "wrote $at of ${b.size} bytes" }
            return b
        }
    }

    companion object {
        const val LOCAL_SIG = 0x04034b50L
        const val CENTRAL_SIG = 0x02014b50L
        const val END64_SIG = 0x06064b50L
        const val LOCATOR64_SIG = 0x07064b50L
        const val END_SIG = 0x06054b50L

        // A 16- or 32-bit field holding its largest value says: look in the Zip64 records.
        const val UINT16_MAX = 0xFFFF
        const val UINT32_MAX = 0xFFFFFFFFL

        private const val VERSION_20 = 20 // 2.0, which every unzip tool reads
        private const val VERSION_45 = 45 // 4.5, for Zip64

        // Bit 11 says the name is UTF-8. Bit 3, for a data descriptor, stays clear.
        const val FLAG_UTF8 = 0x0800

        const val ZIP64_TAG = 0x0001
        const val TIME_TAG = 0x5455 // Info-ZIP's extended timestamp: seconds since 1970, in UTC
        private const val TIME_EXTRA_LEN = 9 // its tag, size, flags and time

        /** The bytes a local header and a central one take, and the end records. */
        fun localLen(nameBytes: Int, size: Long): Long = 30L + nameBytes + TIME_EXTRA_LEN + if (zip64(size)) 20 else 0

        fun centralLen(nameBytes: Int, size: Long, offset: Long): Long {
            val k = escaped(size, offset)
            return 46L + nameBytes + TIME_EXTRA_LEN + if (k > 0) 4 + 8 * k else 0
        }

        fun endLen(items: Int, dirLen: Long, dir: Long): Long = 22L + if (needs64(items, dirLen, dir)) 56 + 20 else 0

        /** Whether the sizes go into a Zip64 extra. A size of 0xFFFFFFFF does too, since that value means "look in the extra". */
        fun zip64(size: Long) = size >= UINT32_MAX

        /** How many fields of a central header go into its Zip64 extra. */
        private fun escaped(size: Long, offset: Long): Int = (if (zip64(size)) 2 else 0) + if (offset >= UINT32_MAX) 1 else 0

        private fun needs64(items: Int, dirLen: Long, dir: Long) = items >= UINT16_MAX || dirLen >= UINT32_MAX || dir >= UINT32_MAX

        /** The extended timestamp's seconds: Java and Android read them signed, so they stop at January 2038; earlier ones get 1970. */
        private fun unixTime(modified: Long): Long = Math.floorDiv(modified, 1000L).coerceIn(0, Int.MAX_VALUE.toLong())

        /**
         * Refuses names a header can't hold or unzip tools would misread: a leading "/" or a ".."
         * leads out of the folder being unpacked into, a trailing "/" makes a folder, "." and empty
         * elements let two names land on one file, Windows tools take a backslash for a separator,
         * and C code ends a name at NUL.
         */
        private fun checkName(name: String, bytes: ByteArray) {
            require(name.isNotEmpty()) { "a name is empty" }
            require(bytes.size <= UINT16_MAX) { "a name of ${bytes.size} bytes is too long" }
            require('\u0000' !in name && '\\' !in name) { "$name contains NUL or a backslash" }
            require(!name.startsWith("/")) { "$name starts with a slash" }
            require(name.split('/').none { it.isEmpty() || it == "." || it == ".." }) { "$name has an empty, \".\" or \"..\" element" }
        }
    }
}
