package com.eschgi.share.platform

import android.app.Activity
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Paint
import android.graphics.RectF
import android.os.Bundle
import android.os.CancellationSignal
import android.os.ParcelFileDescriptor
import android.print.PageRange
import android.print.PrintAttributes
import android.print.PrintDocumentAdapter
import android.print.PrintDocumentInfo
import android.print.PrintManager
import android.print.pdf.PrintedPdfDocument
import java.io.FileOutputStream
import java.io.IOException

/**
 * Hands one page to Android's printing, which lists the printers nearby and Save as PDF. The Dart
 * side draws the page, a PIN's poster or four cards for the tables (screens 71 and 72), as a PNG
 * at 300 dpi; here it fills the paper's printable area as far as its shape allows.
 */
object PagePrinter {
    fun print(activity: Activity, png: ByteArray, name: String) {
        val printing = activity.getSystemService(PrintManager::class.java) ?: throw IllegalStateException("no printing on this phone")
        val a4 = PrintAttributes.Builder()
            .setMediaSize(PrintAttributes.MediaSize.ISO_A4)
            .setColorMode(PrintAttributes.COLOR_MODE_COLOR)
            .build()
        printing.print(name, Page(activity, png, name), a4)
    }

    private class Page(private val context: Context, private val png: ByteArray, private val name: String) : PrintDocumentAdapter() {
        private var attributes: PrintAttributes? = null
        private var picture: Bitmap? = null

        override fun onLayout(
            before: PrintAttributes?,
            now: PrintAttributes,
            cancel: CancellationSignal?,
            callback: LayoutResultCallback,
            extras: Bundle?,
        ) {
            if (cancel?.isCanceled == true) return callback.onLayoutCancelled()
            attributes = now
            val info = PrintDocumentInfo.Builder("$name.pdf")
                .setContentType(PrintDocumentInfo.CONTENT_TYPE_DOCUMENT)
                .setPageCount(1)
                .build()
            callback.onLayoutFinished(info, now != before)
        }

        override fun onWrite(pages: Array<out PageRange>, destination: ParcelFileDescriptor, cancel: CancellationSignal?, callback: WriteResultCallback) {
            val pdf = PrintedPdfDocument(context, attributes ?: return callback.onWriteFailed("not laid out"))
            try {
                val page = pdf.startPage(0)
                val drawn = picture ?: (BitmapFactory.decodeByteArray(png, 0, png.size) ?: throw IOException("the page isn't a picture")).also { picture = it }
                // As large as the printable area allows, in the page's own shape, in the middle.
                val area = RectF(page.info.contentRect)
                val scale = minOf(area.width() / drawn.width, area.height() / drawn.height)
                val width = drawn.width * scale
                val height = drawn.height * scale
                val left = area.left + (area.width() - width) / 2
                val top = area.top + (area.height() - height) / 2
                page.canvas.drawBitmap(drawn, null, RectF(left, top, left + width, top + height), Paint(Paint.FILTER_BITMAP_FLAG))
                pdf.finishPage(page)
                if (cancel?.isCanceled == true) return callback.onWriteCancelled()
                FileOutputStream(destination.fileDescriptor).use { pdf.writeTo(it) }
                callback.onWriteFinished(arrayOf(PageRange.ALL_PAGES))
            } catch (e: IOException) {
                callback.onWriteFailed(e.message)
            } finally {
                pdf.close()
            }
        }

        override fun onFinish() {
            picture?.recycle()
            picture = null
        }
    }
}
