package com.eschgi.share.zip

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.os.StatFs
import android.provider.OpenableColumns
import android.util.Log
import android.webkit.MimeTypeMap
import androidx.exifinterface.media.ExifInterface
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.MediaStoreSink
import com.eschgi.share.transfer.Outbox
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.nio.channels.FileChannel
import java.util.UUID
import java.util.concurrent.CopyOnWriteArraySet
import java.util.zip.CRC32

/**
 * Opening ZIPs (docs/zip-plan.md, screens 105 to 110): one opened with Share from a chat, or
 * several shared at once. Share's own ZIPs say in share.json which set and part they are; any other
 * ZIP is a set of one part. Saving puts photos and videos into the album Share and documents into
 * Download/Share, as downloads do, or into the outbox for a folder of the server. Pieces of cut
 * files wait in [ZipInbox] until their file is whole. The other app lends a ZIP only while Share's
 * screen is there, so everything happens while it is open.
 */
object ZipOpening {
    private const val TAG = "ZipOpening"

    /** Room saving must leave on the phone, as downloads leave. */
    private const val SPARE = 200L * 1024 * 1024

    /** A ZIP that's open: where it came from, how it is read, and what its share.json says. */
    private class Opened(
        val uri: Uri,
        val name: String,
        val pfd: ParcelFileDescriptor?,
        val copy: File?,
        val channel: FileChannel,
        val reader: ZipReader,
        val json: ShareJson?,
        val newer: Boolean,
        val set: String,
        /** Its files, without share.json and folders. */
        val files: List<ZipReader.Entry>,
    )

    /** A file shown on the screen: which ZIP it is in, and its entry. */
    private class Shown(val zip: Opened, val entry: ZipReader.Entry, val type: String, val kind: String, val piece: ShareJson.Piece?, val taken: String)

    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()

    fun listen(listener: (Map<String, Any?>) -> Unit) = listeners.add(listener)

    fun unlisten(listener: (Map<String, Any?>) -> Unit) = listeners.remove(listener)

    private fun emit(event: Map<String, Any?>) = listeners.forEach { it(event) }

    @Volatile private var opened: List<Opened> = emptyList()
    @Volatile private var shown: List<Shown> = emptyList()
    @Volatile private var failed = 0

    fun inbox(context: Context) = ZipInbox(File(context.applicationContext.filesDir, "zip-in"))

    /** Opens [uris] while the app that lends them allows; the ones that aren't ZIPs are counted. */
    @Synchronized
    fun open(context: Context, uris: List<Uri>) {
        val app = context.applicationContext
        close()
        inbox(app).trim()
        val list = ArrayList<Opened>()
        var bad = 0
        for (uri in uris) {
            try {
                list += openOne(app, uri)
            } catch (e: Exception) {
                Log.w(TAG, "opening $uri", e)
                bad++
            }
        }
        opened = list
        failed = bad
        shown = list.flatMap { z ->
            val byEntry = z.json?.files?.associateBy { it.entry }.orEmpty()
            z.files.map { e ->
                val f = byEntry[e.name]
                val type = f?.type?.takeIf { it.isNotBlank() } ?: typeOf(e.name)
                Shown(z, e, type, kindOf(type), f?.piece, f?.taken ?: "")
            }
        }
        for (z in list) if (z.json != null) inbox(app).opened(z.set, z.json.name, z.json.parts)
    }

    private fun openOne(app: Context, uri: Uri): Opened {
        val display = runCatching {
            app.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c -> if (c.moveToFirst()) c.getString(0) else null }
        }.getOrNull() ?: uri.lastPathSegment ?: "ZIP"
        var pfd: ParcelFileDescriptor? = app.contentResolver.openFileDescriptor(uri, "r") ?: throw IOException("can't open $uri")
        var copy: File? = null
        var channel = FileInputStream(pfd!!.fileDescriptor).channel
        try {
            channel.position(0)
            channel.size()
        } catch (e: IOException) {
            // A pipe, which can't seek: the ZIP is copied into the cache first.
            channel.close()
            pfd.close()
            pfd = null
            copy = File(File(app.cacheDir, "zip-open").apply { mkdirs() }, UUID.randomUUID().toString())
            app.contentResolver.openInputStream(uri)!!.use { input -> copy.outputStream().use { input.copyTo(it, 1 shl 20) } }
            channel = FileInputStream(copy).channel
        }
        try {
            val reader = ZipReader(channel)
            val entries = reader.entries.filter { !it.isFolder }
            val jsonEntry = entries.firstOrNull { it.name == ShareJson.NAME && it.readable && it.size <= ShareJson.MAX_BYTES }
            val read = jsonEntry?.let { e -> reader.open(e).use { ShareJson.read(it.readBytes()) } } ?: ShareJson.Other
            val others = entries.filter { it !== jsonEntry }
            val json = (read as? ShareJson.Ours)?.json?.takeIf { j -> j.matches(others.associate { it.name to it.size }) }
            val files = if (json != null) others else entries
            val set = json?.set ?: "plain-" + ZipInbox.key(display + files.joinToString("") { "|${it.name}|${it.size}|${it.crc}" })
            val name = json?.name ?: display.removeSuffix(".zip").removeSuffix(".ZIP")
            return Opened(uri, name, pfd, copy, channel, reader, json, read == ShareJson.Newer, set, files)
        } catch (e: Exception) {
            channel.close()
            pfd?.close()
            copy?.delete()
            throw e
        }
    }

    /** What the screen shows of the ZIPs open (contract/app/platform.json zip_contents). */
    @Synchronized
    fun contents(context: Context): Map<String, Any?>? {
        val list = opened
        if (list.isEmpty()) return if (failed > 0) ZipEvents.contents(null, 0, failed, false, null, false, 0, emptyList(), emptyList()) else null
        val inbox = inbox(context)
        val sets = list.map { it.set }.distinct()
        val state = sets.singleOrNull()?.let { inbox.set(it) }
        val json = list.first().json?.takeIf { sets.size == 1 }
        val stored = if (sets.size == 1) inbox.cuts(sets[0]).associateBy { it.file } else emptyMap()
        // Cut files: the pieces this phone has, and those in the ZIPs open now.
        val joins = shown.filter { it.piece != null }.groupBy { it.zip.set to it.piece!!.file }.map { (key, here) ->
            val p = here.first().piece!!
            val have = (stored[key.second]?.have.orEmpty() + here.map { it.piece!!.number }).toSortedSet()
            ZipEvents.join(p.file, here.first().kind, p.total, p.pieces, have.toList(), p.parts)
        }
        return ZipEvents.contents(
            name = if (sets.size == 1) list.first().name else null,
            zips = list.size,
            broken = failed,
            newer = list.any { it.newer },
            set = json?.let { ZipEvents.set(it.parts, list.mapNotNull { z -> z.json?.part }.distinct().sorted(), state?.saved.orEmpty().sorted(), state?.to, state?.folder) },
            fromWhatsapp = list.any { it.uri.authority.orEmpty().contains("whatsapp") },
            bytes = list.sumOf { z -> z.files.sumOf { it.size } },
            files = shown.mapIndexed { i, s ->
                val saved = state?.entries?.contains(s.entry.name) == true || s.piece?.let { state?.entries?.contains(it.file) } == true
                ZipEvents.entry(i, s.entry.name.substringAfterLast('/'), s.kind, s.entry.size, s.entry.readable, saved, s.piece)
            },
            joins = joins,
        )
    }

    /**
     * A small JPEG of file [index], from the ZIP itself, with a video's length: {jpeg, duration_ms}.
     * Null for one without a picture, or a compressed video.
     */
    fun thumb(index: Int): Map<String, Any?>? {
        val s = shown.getOrNull(index) ?: return null
        if (!s.entry.readable) return null
        return try {
            var duration: Long? = null
            val bitmap = when (s.kind) {
                "photo" -> photoThumb(s)
                "video" -> if (s.entry.method == ZipReader.STORED && (s.piece == null || s.piece.number == 1)) videoThumb(s) { duration = it } else null
                else -> null
            } ?: return null
            val jpeg = ByteArrayOutputStream().use { out ->
                bitmap.compress(Bitmap.CompressFormat.JPEG, 82, out)
                bitmap.recycle()
                out.toByteArray()
            }
            mapOf("jpeg" to jpeg, "duration_ms" to duration?.takeIf { s.piece == null })
        } catch (e: Exception) {
            null
        }
    }

    private fun photoThumb(s: Shown): Bitmap? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        s.zip.reader.open(s.entry).use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0) return null
        var sample = 1
        while (minOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= THUMB) sample *= 2
        val bitmap = s.zip.reader.open(s.entry).use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample }) } ?: return null
        val degrees = runCatching { s.zip.reader.open(s.entry).use { ExifInterface(it).rotationDegrees } }.getOrDefault(0)
        if (degrees == 0) return bitmap
        return Bitmap.createBitmap(bitmap, 0, 0, bitmap.width, bitmap.height, Matrix().apply { postRotate(degrees.toFloat()) }, true).also { if (it !== bitmap) bitmap.recycle() }
    }

    private fun videoThumb(s: Shown, duration: (Long?) -> Unit): Bitmap? {
        val retriever = MediaMetadataRetriever()
        val copy = if (s.zip.pfd == null) FileInputStream(s.zip.copy!!) else null
        return try {
            retriever.setDataSource(s.zip.pfd?.fileDescriptor ?: copy!!.fd, s.zip.reader.dataOffset(s.entry), s.entry.size)
            duration(retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull())
            retriever.getScaledFrameAtTime(1_000_000, MediaMetadataRetriever.OPTION_CLOSEST_SYNC, THUMB, THUMB)
        } finally {
            retriever.release()
            copy?.close()
        }
    }

    private const val THUMB = 320

    /**
     * Saves what's open [to] "phone" or, for [folder], into the outbox; events of type "zip_save" tell
     * how far it is. Files saved before are skipped; pieces wait for the rest of their file.
     */
    @Synchronized
    fun save(context: Context, to: String, folder: String?) {
        val app = context.applicationContext
        val inbox = inbox(app)
        val list = shown.filter { it.entry.readable }
        val sets = opened.map { it.set }.distinct()
        for (set in sets) inbox.goesTo(set, to, folder)
        // What this phone has already: files saved before, and pieces waiting for the rest of their file.
        val done = HashMap<String, Set<String>>()
        val stored = HashMap<String, Map<String, ZipInbox.Cut>>()
        for (set in sets) {
            done[set] = inbox.set(set)?.entries.orEmpty()
            stored[set] = inbox.cuts(set).associateBy { it.file }
        }
        val todo = list.filter { s ->
            val have = done[s.zip.set].orEmpty()
            val p = s.piece
            s.entry.name !in have && (p == null || p.file !in have && p.number !in stored[s.zip.set]?.get(p.file)?.have.orEmpty())
        }
        val total = todo.sumOf { it.entry.size }
        val free = StatFs(app.filesDir.path).availableBytes
        if (free - SPARE < total) {
            emit(ZipEvents.saveNoRoom(total, maxOf(0L, free - SPARE)))
            return
        }
        var bytes = 0L
        var saved = 0
        val joined = ArrayList<Map<String, Any?>>()
        val broken = ArrayList<Map<String, Any?>>()
        var reported = 0L
        fun report(force: Boolean = false) {
            val now = System.nanoTime()
            if (!force && now - reported < 200_000_000) return
            reported = now
            emit(ZipEvents.saving(saved, todo.size, bytes, total))
        }
        try {
            report(force = true)
            for (s in todo) {
                val set = s.zip.set
                val piece = s.piece
                if (piece == null) {
                    try {
                        s.zip.reader.open(s.entry).use { input ->
                            deliver(app, to, s.entry.name.substringAfterLast('/'), s.type, s.kind, s.entry.size) { out -> copyChecked(input, out, s.entry) { bytes += it; report() } }
                        }
                        inbox.savedEntry(set, s.entry.name)
                        saved++
                    } catch (e: BrokenEntry) {
                        broken += mapOf("file" to s.entry.name, "part" to s.zip.json?.part)
                    }
                    continue
                }
                try {
                    s.zip.reader.open(s.entry).use { input -> inbox.addPiece(set, piece, s.type, s.taken, input, s.entry.size, s.entry.crc) { bytes += it; report() } }
                } catch (e: BrokenPiece) {
                    broken += mapOf("file" to piece.file, "number" to piece.number, "part" to s.zip.json?.part)
                }
            }
            // Cut files whose pieces are all in go where the set's files go, also ones left from before.
            for (set in sets) {
                for (cut in inbox.cuts(set)) {
                    if (!cut.whole || cut.data.length() != cut.total) continue
                    deliverWhole(app, to, cut)
                    inbox.done(set, cut.file)
                    inbox.savedEntry(set, cut.file)
                    val parts = shown.firstOrNull { it.zip.set == set && it.piece?.file == cut.file }?.piece?.parts.orEmpty()
                    joined += mapOf("file" to cut.file, "kind" to kindOf(cut.type), "total" to cut.total, "parts" to parts)
                    saved++
                }
            }
            for (z in opened) {
                val json = z.json ?: continue
                if (broken.none { it["part"] == json.part }) inbox.savedPart(z.set, json.part)
            }
            emit(ZipEvents.saved(saved, joined, broken, to, folder))
        } catch (e: Exception) {
            Log.w(TAG, "saving", e)
            emit(ZipEvents.saveFailed(if (MediaStoreSink.isFull(e)) "no_room" else "failed", saved))
        }
    }

    /** A file whose bytes don't match its CRC-32: it isn't kept. */
    private class BrokenEntry(name: String) : IOException("$name is broken")

    /** Copies [input] into [out], checking it against [entry]'s CRC-32. */
    private fun copyChecked(input: InputStream, out: OutputStream, entry: ZipReader.Entry, progress: (Long) -> Unit) {
        val crc = CRC32()
        val buf = ByteArray(1 shl 20)
        var n = 0L
        while (true) {
            val k = input.read(buf)
            if (k < 0) break
            crc.update(buf, 0, k)
            out.write(buf, 0, k)
            n += k
            progress(k.toLong())
        }
        if (n != entry.size || crc.value.toInt() != entry.crc) throw BrokenEntry(entry.name)
    }

    /** A file out of a ZIP goes where it belongs: into the gallery or Downloads, or into the outbox for a folder. */
    private fun deliver(app: Context, to: String, name: String, type: String, kind: String, size: Long, write: (OutputStream) -> Unit) {
        if (to == FOLDER) {
            Outbox.keepMade(app, name, type) { file -> file.outputStream().use(write) }
            return
        }
        val sink = MediaStoreSink.create(app.contentResolver, FileRef(UUID.randomUUID().toString(), name, size, type, kind))
        try {
            sink.append().use(write)
            sink.commit()
        } catch (e: Exception) {
            sink.discard()
            throw e
        }
    }

    /** A cut file that's whole again goes where the set's files go; into the outbox it moves without a copy. */
    private fun deliverWhole(app: Context, to: String, cut: ZipInbox.Cut) {
        if (to == FOLDER) {
            Outbox.keepMade(app, cut.file, cut.type) { file -> if (!cut.data.renameTo(file)) cut.data.inputStream().use { input -> file.outputStream().use { input.copyTo(it, 1 shl 20) } } }
            return
        }
        cut.data.inputStream().use { input ->
            deliver(app, to, cut.file, cut.type, kindOf(cut.type), cut.total) { out -> input.copyTo(out, 1 shl 20) }
        }
    }

    /** The screen closed: the ZIPs go back to the app that lent them. */
    @Synchronized
    fun close() {
        for (z in opened) {
            runCatching { z.channel.close() }
            runCatching { z.pfd?.close() }
            z.copy?.delete()
        }
        opened = emptyList()
        shown = emptyList()
        failed = 0
    }

    const val PHONE = "phone"
    const val FOLDER = "folder"

    private fun typeOf(name: String): String =
        MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase()) ?: "application/octet-stream"

    private fun kindOf(type: String) = when {
        type.startsWith("image/") -> "photo"
        type.startsWith("video/") -> "video"
        else -> "document"
    }
}
