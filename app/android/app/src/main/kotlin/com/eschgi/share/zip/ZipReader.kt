package com.eschgi.share.zip

import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.channels.FileChannel
import java.time.LocalDateTime
import java.time.ZoneId
import java.util.zip.Inflater
import java.util.zip.InflaterInputStream

/** Not a ZIP, or one broken where it can't be read. */
class NotAZip(message: String) : IOException(message)

/**
 * Reads a ZIP from a file's channel through its central directory: Share's own, or any other, with
 * stored or deflated files, data descriptors, Zip64, and names in UTF-8 or the IBM PC's old code page.
 * Reads are positional, so the channel's position doesn't matter.
 */
class ZipReader(private val channel: FileChannel, private val zone: ZoneId = ZoneId.systemDefault()) {

    /** A file in the ZIP, as its central directory says. */
    data class Entry(
        val name: String,
        val method: Int,
        val flags: Int,
        val crc: Int,
        val compressedSize: Long,
        val size: Long,
        val localOffset: Long,
        /** When it last changed, in ms since 1970, if the ZIP says. */
        val modified: Long?,
    ) {
        val isFolder: Boolean get() = name.endsWith("/")
        val encrypted: Boolean get() = flags and 1 != 0

        /** Stored or deflated, and not encrypted: what Share can open. */
        val readable: Boolean get() = !encrypted && (method == STORED || method == DEFLATED)
    }

    val size: Long = channel.size()

    /** Every entry, in the central directory's order. */
    val entries: List<Entry>

    /** Where the central directory starts: no file's data goes past it. */
    private val dirStart: Long

    init {
        if (size < 22) throw NotAZip("too short")
        val tailLen = minOf(size, 22L + 0xFFFF).toInt()
        val tail = read(size - tailLen, tailLen)
        var end = -1
        for (i in tailLen - 22 downTo 0) {
            if (tail.getInt(i).toLong() and 0xFFFFFFFFL == ZipLayout.END_SIG && i + 22 + tail.u16(i + 20) <= tailLen) {
                end = i
                break
            }
        }
        if (end < 0) throw NotAZip("no end record")
        var count = tail.u16(end + 10).toLong()
        var dirLen = tail.u32(end + 12)
        var dirAt = tail.u32(end + 16)
        val endAt = size - tailLen + end
        if (endAt >= 20) {
            val locator = read(endAt - 20, 20)
            if (locator.u32(0) == ZipLayout.LOCATOR64_SIG) {
                val end64At = locator.getLong(8)
                if (end64At < 0 || end64At + 56 > endAt) throw NotAZip("Zip64 end record out of place")
                val end64 = read(end64At, 56)
                if (end64.u32(0) != ZipLayout.END64_SIG) throw NotAZip("no Zip64 end record")
                count = end64.getLong(32)
                dirLen = end64.getLong(40)
                dirAt = end64.getLong(48)
            }
        }
        if (dirAt < 0 || dirLen < 0 || dirAt + dirLen > endAt || dirLen > MAX_DIRECTORY || count < 0 || count > dirLen / 46) throw NotAZip("central directory out of place")
        dirStart = dirAt
        val dir = read(dirAt, dirLen.toInt())
        val list = ArrayList<Entry>(count.toInt())
        var at = 0
        repeat(count.toInt()) {
            if (at + 46 > dir.limit() || dir.u32(at) != ZipLayout.CENTRAL_SIG) throw NotAZip("broken central directory")
            val flags = dir.u16(at + 8)
            val method = dir.u16(at + 10)
            val time = dir.u16(at + 12)
            val date = dir.u16(at + 14)
            val crc = dir.getInt(at + 16)
            var compressed = dir.u32(at + 20)
            var length = dir.u32(at + 24)
            val nameLen = dir.u16(at + 28)
            val extraLen = dir.u16(at + 30)
            val commentLen = dir.u16(at + 32)
            var offset = dir.u32(at + 42)
            val next = at + 46 + nameLen + extraLen + commentLen
            if (next > dir.limit()) throw NotAZip("broken central directory")
            val nameBytes = dir.array().copyOfRange(dir.arrayOffset() + at + 46, dir.arrayOffset() + at + 46 + nameLen)
            val name = if (flags and ZipLayout.FLAG_UTF8 != 0) String(nameBytes, Charsets.UTF_8) else cp437(nameBytes)
            var modified: Long? = null
            var x = at + 46 + nameLen
            val extraEnd = x + extraLen
            while (x + 4 <= extraEnd) {
                val tag = dir.u16(x)
                val len = dir.u16(x + 2)
                val data = x + 4
                if (data + len > extraEnd) break
                if (tag == ZipLayout.ZIP64_TAG) {
                    // Only the fields that didn't fit, in this order.
                    var y = data
                    if (length == ZipLayout.UINT32_MAX && y + 8 <= data + len) length = dir.getLong(y).also { y += 8 }
                    if (compressed == ZipLayout.UINT32_MAX && y + 8 <= data + len) compressed = dir.getLong(y).also { y += 8 }
                    if (offset == ZipLayout.UINT32_MAX && y + 8 <= data + len) offset = dir.getLong(y)
                } else if (tag == ZipLayout.TIME_TAG && len >= 5 && dir.get(data).toInt() and 1 != 0) {
                    modified = dir.u32(data + 1) * 1000
                }
                x = data + len
            }
            if (modified == null) modified = dosTime(time, date)
            if (length < 0 || compressed < 0 || offset < 0 || offset + 30 > dirAt) throw NotAZip("$name lies outside the ZIP")
            list += Entry(name, method, flags, crc, compressed, length, offset, modified)
            at = next
        }
        entries = list
    }

    /** Where [e]'s data starts, after its local header; it ends before the central directory. */
    fun dataOffset(e: Entry): Long {
        val local = read(e.localOffset, 30)
        if (local.u32(0) != ZipLayout.LOCAL_SIG) throw NotAZip("${e.name} has no local header")
        val start = e.localOffset + 30 + local.u16(26) + local.u16(28)
        if (start + e.compressedSize > dirStart) throw NotAZip("${e.name} runs into the central directory")
        return start
    }

    /** [e]'s bytes as they were packed: at most its length, stored or inflated. It doesn't check the CRC; whoever copies it does. */
    fun open(e: Entry): InputStream {
        if (!e.readable) throw IOException("${e.name} can't be opened: method ${e.method}${if (e.encrypted) ", encrypted" else ""}")
        val raw = ChannelInput(channel, dataOffset(e), e.compressedSize)
        if (e.method == STORED) return raw
        val inflater = Inflater(true)
        return Limited(object : InflaterInputStream(raw, inflater, 64 * 1024) {
            override fun close() {
                super.close()
                inflater.end()
            }
        }, e.size)
    }

    private fun read(at: Long, len: Int): ByteBuffer {
        val buf = ByteBuffer.allocate(len).order(ByteOrder.LITTLE_ENDIAN)
        var pos = at
        while (buf.hasRemaining()) {
            val n = channel.read(buf, pos)
            if (n < 0) throw EOFException("the ZIP ended at $pos")
            pos += n
        }
        buf.flip()
        return buf
    }

    private fun ByteBuffer.u16(at: Int): Int = getShort(at).toInt() and 0xFFFF

    private fun ByteBuffer.u32(at: Int): Long = getInt(at).toLong() and 0xFFFFFFFFL

    private fun dosTime(time: Int, date: Int): Long? = try {
        LocalDateTime.of(1980 + (date ushr 9), (date ushr 5) and 15, date and 31, time ushr 11, (time ushr 5) and 63, (time and 31) * 2)
            .atZone(zone).toInstant().toEpochMilli()
    } catch (e: Exception) {
        null // not a date, e.g. zero
    }

    /** A stretch of the channel, read positionally. */
    private class ChannelInput(private val channel: FileChannel, private var at: Long, private var left: Long) : InputStream() {
        override fun read(): Int {
            val one = ByteArray(1)
            return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
        }

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (left <= 0) return -1
            val n = channel.read(ByteBuffer.wrap(b, off, minOf(len.toLong(), left).toInt()), at)
            if (n < 0) throw EOFException("the ZIP ended at $at")
            at += n
            left -= n
            return n
        }

        override fun available(): Int = minOf(left, Int.MAX_VALUE.toLong()).toInt()
    }

    /** An inflated stream that ends where its entry says, so a ZIP can't unpack into more than it claims. */
    private class Limited(private val inner: InputStream, private var left: Long) : InputStream() {
        override fun read(): Int {
            val one = ByteArray(1)
            return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
        }

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (left <= 0) {
                if (inner.read() >= 0) throw IOException("an entry is longer than it says")
                return -1
            }
            val n = inner.read(b, off, minOf(len.toLong(), left).toInt())
            if (n > 0) left -= n
            return n
        }

        override fun close() = inner.close()
    }

    companion object {
        const val STORED = 0
        const val DEFLATED = 8

        /** Larger central directories are refused: that's millions of files. */
        private const val MAX_DIRECTORY = 256L shl 20

        /** The IBM PC's code page 437, for names without the UTF-8 flag: its upper half. */
        private const val CP437 =
            "ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜ¢£¥₧ƒáíóúñÑªº¿⌐¬½¼¡«»░▒▓│┤╡╢╖╕╣║╗╝╜╛┐└┴┬├─┼╞╟╚╔╩╦╠═╬╧╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀αßΓπΣσµτΦΘΩδ∞φε∩≡±≥≤⌠⌡÷≈°∙·√ⁿ²■ "

        fun cp437(bytes: ByteArray): String = buildString(bytes.size) {
            for (b in bytes) {
                val c = b.toInt() and 0xFF
                append(if (c < 0x80) c.toChar() else CP437[c - 0x80])
            }
        }
    }
}
