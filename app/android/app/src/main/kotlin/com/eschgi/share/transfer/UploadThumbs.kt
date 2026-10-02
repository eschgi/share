package com.eschgi.share.transfer

import android.content.ContentResolver
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.util.Size
import androidx.exifinterface.media.ExifInterface
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream

/** A picked file, read through the content resolver. */
class ContentSource(private val resolver: ContentResolver, private val uri: Uri) : UploadSource {
    override fun openAt(offset: Long): InputStream {
        // A real file can seek; a pipe (some providers) has to be read up to the offset.
        resolver.openFileDescriptor(uri, "r")?.let { fd ->
            val stream = ParcelFileDescriptor.AutoCloseInputStream(fd)
            try {
                stream.channel.position(offset)
                return stream
            } catch (e: IOException) {
                stream.close()
            }
        }
        val input = resolver.openInputStream(uri) ?: throw IOException("can't open $uri")
        var skipped = 0L
        while (skipped < offset) {
            val n = input.skip(offset - skipped)
            if (n <= 0) {
                input.close()
                throw IOException("$uri is shorter than $offset bytes")
            }
            skipped += n
        }
        return input
    }
}

/**
 * The picture that goes with a sent file: what the library shows before anyone downloads it,
 * and the size or length of the original. Photos and videos get one from the system; documents
 * only if their app makes one.
 */
object UploadThumbs {
    data class Thumb(val jpeg: ByteArray?, val width: Int?, val height: Int?, val durationMs: Long?)

    private const val SIZE = 512

    fun make(context: Context, uri: Uri, kind: String): Thumb {
        val resolver = context.contentResolver
        val (width, height, duration) = when (kind) {
            "photo" -> photoSize(resolver, uri)
            "video" -> videoSize(context, uri)
            else -> Triple(null, null, null)
        }
        val bitmap = system(resolver, uri) ?: when (kind) {
            "photo" -> decode(resolver, uri)
            "video" -> frame(context, uri)
            else -> null
        }
        val jpeg = bitmap?.let {
            ByteArrayOutputStream().use { out ->
                it.compress(Bitmap.CompressFormat.JPEG, 80, out)
                it.recycle()
                out.toByteArray()
            }
        }
        return Thumb(jpeg, width, height, duration)
    }

    private fun system(resolver: ContentResolver, uri: Uri): Bitmap? = try {
        resolver.loadThumbnail(uri, Size(SIZE, SIZE), null)
    } catch (e: IOException) {
        null
    } catch (e: RuntimeException) {
        null // some providers throw instead of saying they have none
    }

    /** A photo without a system thumbnail: decoded small, upright. */
    private fun decode(resolver: ContentResolver, uri: Uri): Bitmap? = try {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        var sample = 1
        while (bounds.outWidth / (sample * 2) >= SIZE && bounds.outHeight / (sample * 2) >= SIZE) sample *= 2
        val bitmap = resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample }) }
        val degrees = orientation(resolver, uri)
        if (bitmap == null || degrees == 0) {
            bitmap
        } else {
            Bitmap.createBitmap(bitmap, 0, 0, bitmap.width, bitmap.height, Matrix().apply { postRotate(degrees.toFloat()) }, true)
        }
    } catch (e: IOException) {
        null
    } catch (e: OutOfMemoryError) {
        null
    }

    /** A video without a system thumbnail, such as a copy of a shared one: a frame of its first second, small. */
    private fun frame(context: Context, uri: Uri): Bitmap? {
        val retriever = MediaMetadataRetriever()
        return try {
            retriever.setDataSource(context, uri)
            val frame = retriever.getFrameAtTime(1_000_000, MediaMetadataRetriever.OPTION_CLOSEST_SYNC) ?: return null
            val scale = SIZE.toFloat() / maxOf(frame.width, frame.height)
            if (scale >= 1f) {
                frame
            } else {
                val small = Bitmap.createScaledBitmap(frame, (frame.width * scale).toInt().coerceAtLeast(1), (frame.height * scale).toInt().coerceAtLeast(1), true)
                if (small !== frame) frame.recycle()
                small
            }
        } catch (e: RuntimeException) {
            null
        } catch (e: OutOfMemoryError) {
            null
        } finally {
            runCatching { retriever.release() }
        }
    }

    private fun orientation(resolver: ContentResolver, uri: Uri): Int = try {
        resolver.openInputStream(uri)?.use {
            when (ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)) {
                ExifInterface.ORIENTATION_ROTATE_90, ExifInterface.ORIENTATION_TRANSPOSE -> 90
                ExifInterface.ORIENTATION_ROTATE_180 -> 180
                ExifInterface.ORIENTATION_ROTATE_270, ExifInterface.ORIENTATION_TRANSVERSE -> 270
                else -> 0
            }
        } ?: 0
    } catch (e: IOException) {
        0
    }

    private fun photoSize(resolver: ContentResolver, uri: Uri): Triple<Int?, Int?, Long?> = try {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0) {
            Triple(null, null, null)
        } else if (orientation(resolver, uri) % 180 == 90) {
            Triple(bounds.outHeight, bounds.outWidth, null) // as it's seen, upright
        } else {
            Triple(bounds.outWidth, bounds.outHeight, null)
        }
    } catch (e: IOException) {
        Triple(null, null, null)
    }

    private fun videoSize(context: Context, uri: Uri): Triple<Int?, Int?, Long?> {
        val retriever = MediaMetadataRetriever()
        return try {
            retriever.setDataSource(context, uri)
            val w = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)?.toIntOrNull()
            val h = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)?.toIntOrNull()
            val rotation = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_ROTATION)?.toIntOrNull() ?: 0
            val ms = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull()
            if (rotation % 180 == 90) Triple(h, w, ms) else Triple(w, h, ms)
        } catch (e: RuntimeException) {
            Triple(null, null, null)
        } finally {
            runCatching { retriever.release() }
        }
    }
}
