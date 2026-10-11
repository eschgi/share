package com.eschgi.share.transfer

import android.content.Context
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.provider.MediaStore
import androidx.exifinterface.media.ExifInterface
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

/**
 * When a photo or video was taken, in ms since 1970 (docs/sort-plan.md): as the gallery or the
 * photo picker say, else from the file itself, a photo's EXIF or a video's header. Null when
 * nothing says, such as for a document or a screenshot without a date; an upload into an
 * encrypted folder never asks, so that the server never learns it.
 */
object TakenDate {
    fun of(context: Context, uri: Uri, mime: String): Long? {
        val app = context.applicationContext
        if (!mime.startsWith("image/") && !mime.startsWith("video/")) return null
        for (column in listOf(MediaStore.MediaColumns.DATE_TAKEN, "date_taken_millis")) {
            val value = try {
                app.contentResolver.query(uri, arrayOf(column), null, null, null)?.use { c ->
                    val i = c.getColumnIndex(column)
                    if (i >= 0 && c.moveToFirst() && !c.isNull(i)) c.getLong(i) else null
                }
            } catch (e: Exception) {
                null // a provider that doesn't know the column
            }
            if (value != null && value > 0) return value
        }
        return try {
            if (mime.startsWith("image/")) {
                app.contentResolver.openInputStream(uri)?.use { exifTime(ExifInterface(it)) }
            } else {
                MediaMetadataRetriever().run {
                    try {
                        setDataSource(app, uri)
                        videoTime(extractMetadata(MediaMetadataRetriever.METADATA_KEY_DATE))
                    } finally {
                        release()
                    }
                }
            }
        } catch (e: Exception) {
            null // nothing readable in it
        }
    }

    /** DateTimeOriginal, with its offset when the camera wrote one; else as this phone's time. */
    fun exifTime(exif: ExifInterface): Long? {
        val text = exif.getAttribute(ExifInterface.TAG_DATETIME_ORIGINAL) ?: exif.getAttribute(ExifInterface.TAG_DATETIME) ?: return null
        val local = runCatching { LocalDateTime.parse(text.trim(), EXIF_TIME) }.getOrNull() ?: return null
        val offset = exif.getAttribute(ExifInterface.TAG_OFFSET_TIME_ORIGINAL)?.let { runCatching { ZoneOffset.of(it.trim()) }.getOrNull() }
        return (if (offset != null) local.atOffset(offset).toInstant() else local.atZone(ZoneId.systemDefault()).toInstant()).toEpochMilli()
    }

    /** A video's date as MediaMetadataRetriever writes it, "20250614T163210.000Z"; none for 1904 or 1970, which mean empty. */
    fun videoTime(text: String?): Long? {
        val at = runCatching { ZonedDateTime.parse(text?.trim() ?: return null, VIDEO_TIME) }.getOrNull() ?: return null
        return if (at.year > 1970) at.toInstant().toEpochMilli() else null
    }

    private val EXIF_TIME = DateTimeFormatter.ofPattern("yyyy:MM:dd HH:mm:ss")
    private val VIDEO_TIME = DateTimeFormatter.ofPattern("yyyyMMdd'T'HHmmss.SSSX")
}
