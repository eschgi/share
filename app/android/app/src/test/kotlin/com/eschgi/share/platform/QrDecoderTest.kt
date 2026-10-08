package com.eschgi.share.platform

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.nio.ByteBuffer

/**
 * Reading QR codes from camera frames as CameraX hands them over: a plane of brightness whose rows
 * are wider than the picture, which may end right after the last pixel, and of which only the part
 * on the screen counts.
 */
class QrDecoderTest {
    private val invite = "https://share.example.com/join#shi_" + "x".repeat(40)

    @Test
    fun readsAnInvite() {
        val frame = frame(invite, x = 170, y = 90)
        assertEquals(invite, QrDecoder().decode(frame, STRIDE, 0, 0, WIDTH, HEIGHT))
    }

    @Test
    fun aFrameWithoutACodeHoldsNothing() {
        val frame = ByteBuffer.wrap(ByteArray(STRIDE * HEIGHT) { GREY })
        assertNull(QrDecoder().decode(frame, STRIDE, 0, 0, WIDTH, HEIGHT))
    }

    @Test
    fun looksOnlyWhereItIsTold() {
        val frame = frame(invite, x = 10, y = 90)
        val decoder = QrDecoder()
        assertNull(decoder.decode(frame, STRIDE, 330, 0, WIDTH - 330, HEIGHT))
        assertEquals(invite, decoder.decode(frame, STRIDE, 0, 0, 330, HEIGHT))
        assertEquals(invite, decoder.decode(frame, STRIDE, 0, 60, 330, HEIGHT - 60))
    }

    @Test
    fun thePlaneMayEndAfterTheLastPixel() {
        val whole = frame(invite, x = 170, y = 170).array()
        val cut = ByteBuffer.wrap(whole.copyOf(STRIDE * (HEIGHT - 1) + WIDTH))
        assertEquals(invite, QrDecoder().decode(cut, STRIDE, 0, 0, WIDTH, HEIGHT))
    }

    @Test
    fun readsFrameAfterFrame() {
        val decoder = QrDecoder()
        val pin = "https://share.example.com/#123456"
        assertEquals(invite, decoder.decode(frame(invite, x = 170, y = 90), STRIDE, 0, 0, WIDTH, HEIGHT))
        assertNull(decoder.decode(ByteBuffer.wrap(ByteArray(STRIDE * HEIGHT) { GREY }), STRIDE, 0, 0, WIDTH, HEIGHT))
        assertEquals(pin, decoder.decode(frame(pin, x = 40, y = 40), STRIDE, 0, 0, WIDTH, HEIGHT))
    }

    /** A grey frame's brightness with [text]'s QR code, dark on white, at ([x], [y]). */
    private fun frame(text: String, x: Int, y: Int): ByteBuffer {
        val bytes = ByteArray(STRIDE * HEIGHT) { GREY }
        val code = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, SIZE, SIZE, mapOf(EncodeHintType.MARGIN to 2))
        for (row in 0 until SIZE) {
            for (col in 0 until SIZE) bytes[(y + row) * STRIDE + x + col] = if (code.get(col, row)) DARK else WHITE
        }
        return ByteBuffer.wrap(bytes)
    }

    private companion object {
        const val WIDTH = 640
        const val HEIGHT = 480
        /** Wider than the picture, as camera planes often are. */
        const val STRIDE = 704
        const val SIZE = 300
        const val GREY: Byte = 0x80.toByte()
        const val DARK: Byte = 0x14
        const val WHITE: Byte = 0xEB.toByte()
    }
}
