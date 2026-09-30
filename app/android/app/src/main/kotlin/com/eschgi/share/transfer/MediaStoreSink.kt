package com.eschgi.share.transfer

import android.content.ContentResolver
import android.content.ContentValues
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.MediaStore
import android.system.ErrnoException
import android.system.OsConstants
import java.io.FileNotFoundException
import java.io.IOException
import java.io.OutputStream

/**
 * A download into the shared storage: photos and videos into Pictures/Share, which galleries
 * show as the album "Share", documents into Download/Share. The row stays pending (hidden
 * from other apps) until [commit], and a resume continues at the length it already has.
 */
class MediaStoreSink(private val resolver: ContentResolver, val uri: Uri) : DownloadSink {

    override fun length(): Long = resolver.openFileDescriptor(uri, "r")?.use { it.statSize }
        ?: throw FileNotFoundException(uri.toString())

    override fun append(): OutputStream {
        val fd = resolver.openFileDescriptor(uri, "rw") ?: throw FileNotFoundException(uri.toString())
        // Closing this closes the ParcelFileDescriptor too, the way it expects to be closed.
        val stream = ParcelFileDescriptor.AutoCloseOutputStream(fd)
        try {
            stream.channel.position(fd.statSize)
        } catch (e: IOException) {
            stream.close()
            throw e
        }
        return object : OutputStream() {
            override fun write(b: Int) = stream.write(b)
            override fun write(b: ByteArray, off: Int, len: Int) = stream.write(b, off, len)
            override fun close() {
                try {
                    stream.fd.sync() // a resume trusts the length, so it has to be on disk
                } finally {
                    stream.close()
                }
            }
        }
    }

    override fun truncate() {
        resolver.openFileDescriptor(uri, "rwt")?.close()
    }

    override fun commit(): String {
        resolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
        return uri.toString()
    }

    override fun discard() {
        runCatching { resolver.delete(uri, null, null) }
    }

    companion object {
        const val ALBUM = "Share"

        /** A new pending row for [file]. Media the gallery won't take goes to Download/Share. */
        fun create(resolver: ContentResolver, file: FileRef): MediaStoreSink {
            if (file.isMedia) {
                val collection = if (file.kind == "video") {
                    MediaStore.Video.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
                } else {
                    MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
                }
                try {
                    insert(resolver, collection, file, "Pictures/$ALBUM")?.let { return MediaStoreSink(resolver, it) }
                } catch (e: IllegalArgumentException) {
                    // e.g. a MIME type the media collections don't accept
                }
            }
            val downloads = MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
            val uri = insert(resolver, downloads, file, "Download/$ALBUM") ?: throw IOException("MediaStore refused ${file.name}")
            return MediaStoreSink(resolver, uri)
        }

        /** Whether the row is still there (not deleted or in the trash) and finished. */
        fun exists(resolver: ContentResolver, uri: Uri): Boolean = try {
            resolver.query(uri, arrayOf(MediaStore.MediaColumns._ID, MediaStore.MediaColumns.IS_PENDING), null, null, null)?.use {
                it.moveToFirst() && it.getInt(1) == 0
            } ?: false
        } catch (e: SecurityException) {
            false
        } catch (e: IllegalArgumentException) {
            false
        }

        /** Whether a failed write means the phone is full. */
        fun isFull(e: Throwable): Boolean {
            var cause: Throwable? = e
            while (cause != null) {
                if (cause is ErrnoException && cause.errno == OsConstants.ENOSPC) return true
                if (cause.message?.contains("ENOSPC") == true || cause.message?.contains("No space left") == true) return true
                cause = cause.cause
            }
            return false
        }

        private fun insert(resolver: ContentResolver, collection: Uri, file: FileRef, path: String): Uri? =
            resolver.insert(
                collection,
                ContentValues().apply {
                    put(MediaStore.MediaColumns.DISPLAY_NAME, file.name)
                    put(MediaStore.MediaColumns.MIME_TYPE, file.mime)
                    put(MediaStore.MediaColumns.RELATIVE_PATH, path)
                    put(MediaStore.MediaColumns.IS_PENDING, 1)
                },
            )
    }
}
