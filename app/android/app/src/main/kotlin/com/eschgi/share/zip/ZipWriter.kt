package com.eschgi.share.zip

import java.io.IOException
import java.io.InputStream
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.util.zip.CRC32

/** Where a ZIP is written: one byte after the other, and a CRC-32 filled in afterwards. */
interface ZipOut {
    val position: Long

    fun write(bytes: ByteArray, off: Int = 0, len: Int = bytes.size)

    fun writeAt(position: Long, bytes: ByteArray)
}

/** A file's channel as a [ZipOut]. */
class ChannelOut(private val channel: FileChannel) : ZipOut {
    override val position: Long get() = channel.position()

    override fun write(bytes: ByteArray, off: Int, len: Int) {
        val buf = ByteBuffer.wrap(bytes, off, len)
        while (buf.hasRemaining()) channel.write(buf)
    }

    override fun writeAt(position: Long, bytes: ByteArray) {
        val buf = ByteBuffer.wrap(bytes)
        var at = position
        while (buf.hasRemaining()) at += channel.write(buf, at)
    }
}

/** A file to pack wasn't what it was when the plan was made: shorter or longer, or gone. */
class SourceChanged(val item: Int, message: String) : IOException(message)

/** Packing was stopped. */
class PackStopped : IOException("stopped")

/**
 * Writes [layout]'s ZIP to [out], reading each file once from the stream [open] gives for it: the
 * local header first with zero for the CRC-32, then the data while the CRC is computed, then the
 * CRC into the header. The sizes are known from the start, so nothing else changes afterwards.
 */
class ZipWriter(private val layout: ZipLayout, private val out: ZipOut) {

    /** Writes the ZIP and returns the items' CRC-32s; [progress] hears of the files' bytes as they go. */
    fun write(open: (Int) -> InputStream, progress: (Long) -> Unit = {}, stopped: () -> Boolean = { false }): IntArray {
        val crcs = IntArray(layout.items.size)
        val buf = ByteArray(BUFFER)
        for ((i, item) in layout.items.withIndex()) {
            check(out.position == layout.offsets[i]) { "item $i at ${out.position}, planned at ${layout.offsets[i]}" }
            if (stopped()) throw PackStopped()
            out.write(layout.local(i, 0))
            val crc = CRC32()
            open(i).use { input ->
                var left = item.size
                while (left > 0) {
                    if (stopped()) throw PackStopped()
                    val n = input.read(buf, 0, minOf(buf.size.toLong(), left).toInt())
                    if (n < 0) throw SourceChanged(i, "${item.name} ended $left bytes early")
                    crc.update(buf, 0, n)
                    out.write(buf, 0, n)
                    left -= n
                    progress(n.toLong())
                }
                if (input.read() >= 0) throw SourceChanged(i, "${item.name} is longer than it was")
            }
            crcs[i] = crc.value.toInt()
            out.writeAt(layout.offsets[i] + CRC_AT, le32(crcs[i]))
        }
        out.write(layout.central(crcs))
        out.write(layout.end())
        check(out.position == layout.size) { "wrote ${out.position} bytes, planned ${layout.size}" }
        return crcs
    }

    companion object {
        private const val BUFFER = 1 shl 20

        /** Where the CRC-32 is in a local header. */
        private const val CRC_AT = 14

        private fun le32(v: Int) = byteArrayOf(v.toByte(), (v ushr 8).toByte(), (v ushr 16).toByte(), (v ushr 24).toByte())
    }
}
