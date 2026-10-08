package com.eschgi.share.platform

import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import java.nio.ByteBuffer

/**
 * Finds a QR code in a camera frame's brightness, the Y plane of YUV_420_888, with ZXing. One per
 * thread: the reader and the copy of the frame are reused from frame to frame.
 */
class QrDecoder {
    private val reader = QRCodeReader()
    private var frame = ByteArray(0)

    /**
     * The text of a QR code in the [width] × [height] pixels from ([left], [top]) of a frame whose
     * rows lie [rowStride] bytes apart in [luminance], or null if they hold none.
     */
    fun decode(luminance: ByteBuffer, rowStride: Int, left: Int, top: Int, width: Int, height: Int): String? {
        // ZXing wants whole rows, and the plane may end right after the last row's last pixel.
        val rows = top + height
        if (frame.size != rowStride * rows) frame = ByteArray(rowStride * rows)
        luminance.rewind()
        luminance.get(frame, 0, minOf(luminance.remaining(), frame.size))
        val source = PlanarYUVLuminanceSource(frame, rowStride, rows, left, top, width, height, false)
        return try {
            reader.decode(BinaryBitmap(HybridBinarizer(source)), HINTS).text
        } catch (e: ReaderException) {
            null
        } finally {
            reader.reset()
        }
    }

    private companion object {
        /** The app and the website put links into their codes as UTF-8. */
        val HINTS = mapOf(DecodeHintType.CHARACTER_SET to "UTF-8")
    }
}
