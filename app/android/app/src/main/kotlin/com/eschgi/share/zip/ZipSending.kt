package com.eschgi.share.zip

import android.app.Activity
import android.app.PendingIntent
import android.content.ClipData
import android.content.ComponentName
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Build
import android.os.StatFs
import android.provider.MediaStore
import android.util.Log
import android.util.Size
import androidx.core.content.FileProvider
import androidx.exifinterface.media.ExifInterface
import com.eschgi.share.BuildConfig
import com.eschgi.share.MainActivity
import com.eschgi.share.transfer.Credentials
import com.eschgi.share.transfer.Downloads
import com.eschgi.share.transfer.Fetcher
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.MediaStoreSink
import com.eschgi.share.transfer.Outbox
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.IOException
import java.io.InputStream
import java.io.RandomAccessFile
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.UUID
import java.util.concurrent.CopyOnWriteArraySet
import kotlin.concurrent.thread

/**
 * Sending as a ZIP (docs/zip-plan.md, screens 97 to 104): the files waiting to be packed, from the
 * share sheet's Send as ZIP, the photo picker or the library; what each choice of where it goes
 * makes of them; packing in the background, one set at a time; and handing the ZIPs to other apps.
 * The ZIPs stay in Share's own storage for a day, as other apps read them when they like, and then
 * go by themselves ([trim]).
 */
object ZipSending {
    private const val TAG = "ZipSending"
    private const val KEEP_MS = 24 * 3600_000L

    /** Room a ZIP must leave on the phone, as the outbox leaves. */
    private const val SPARE = 200L * 1024 * 1024

    /** A file to pack: what goes into the plan, and where its bytes are. */
    class Source(val file: PackFile, val kind: String, val uri: Uri?, val ref: FileRef?)

    /** The parts made, where they are, and who took them. */
    class Packed(val id: String, val name: String, val plan: ZipPlan, val files: List<File>)

    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()

    fun listen(listener: (Map<String, Any?>) -> Unit) = listeners.add(listener)

    fun unlisten(listener: (Map<String, Any?>) -> Unit) = listeners.remove(listener)

    private fun emit(event: Map<String, Any?>) = listeners.forEach { it(event) }

    @Volatile private var sources: List<Source> = emptyList()
    @Volatile private var auth: String = Credentials.DEVICE
    @Volatile private var packed: Packed? = null
    @Volatile private var stopping = false
    @Volatile private var packing: Thread? = null

    /** Files lent by another app's share sheet or the photo picker: they wait here, described, until packed. */
    fun fromUris(context: Context, uris: List<Uri>): List<Map<String, Any?>> {
        val app = context.applicationContext
        val list = uris.mapNotNull { uri ->
            try {
                val picked = Outbox.describe(app, uri) ?: return@mapNotNull null
                Source(PackFile(picked.name, picked.size, picked.mime, taken(app, uri, picked.mime)), kindOf(picked.mime), uri, null)
            } catch (e: Exception) {
                Log.w(TAG, "describing $uri", e)
                null // the app took it back, or it can't be read
            }
        }
        return start(list, Credentials.DEVICE, skipped = uris.size - list.size)
    }

    /**
     * Files of the library, with when each arrived ([taken], ms since 1970): the ones on the phone are
     * packed from there, the others fetched first with [auth]'s key.
     */
    fun fromLibrary(context: Context, refs: List<FileRef>, taken: List<Long>, auth: String): List<Map<String, Any?>> {
        val app = context.applicationContext
        val list = refs.mapIndexed { i, ref ->
            Source(PackFile(ref.name, ref.size, ref.mime, taken.getOrElse(i) { System.currentTimeMillis() }), ref.kind, Downloads.savedUri(app, ref.id), ref)
        }
        return start(list, auth, skipped = 0)
    }

    private fun start(list: List<Source>, auth: String, skipped: Int): List<Map<String, Any?>> {
        stop()
        sources = list
        this.auth = auth
        packed = null
        lastSkipped = skipped
        return waiting()
    }

    /** How many lent files couldn't be read when they arrived. */
    @Volatile var lastSkipped = 0
        private set

    /** The files waiting, as [fromUris] or [fromLibrary] described them; empty without any. */
    fun waiting(): List<Map<String, Any?>> = sources.map { ZipEvents.file(it.file, it.kind, onPhone = it.uri != null) }

    /** What [limit] makes of the files waiting: the parts, and the files that are cut. */
    fun plan(name: String, about: String, limit: Long?): Map<String, Any?> = ZipEvents.plan(ZipPlan.make(sources.map { it.file }, limit, set(name, about)))

    private fun set(name: String, about: String) = PackSet(UUID.randomUUID().toString(), name, about, System.currentTimeMillis(), BuildConfig.VERSION_NAME, ZoneId.systemDefault())

    /** A small JPEG of file [index], from the phone; null for one that isn't on it, or that has no picture. */
    fun thumb(context: Context, index: Int): ByteArray? {
        val s = sources.getOrNull(index) ?: return null
        val uri = s.uri ?: return null
        if (s.kind == "document") return null
        return try {
            val bitmap = context.contentResolver.loadThumbnail(uri, Size(THUMB, THUMB), null)
            ByteArrayOutputStream().use { out ->
                bitmap.compress(Bitmap.CompressFormat.JPEG, 82, out)
                bitmap.recycle()
                out.toByteArray()
            }
        } catch (e: Exception) {
            null // no picture of it
        }
    }

    private const val THUMB = 320

    /**
     * Packs the files waiting into ZIPs of at most [limit] bytes, or one ZIP, called [name] (and
     * [partName] with {name}, {part} and {parts} for several), on a thread of its own. Events of type
     * "zip" tell how far it is, and what it made.
     */
    fun pack(context: Context, name: String, about: String, partName: String, limit: Long?) {
        val app = context.applicationContext
        stop()
        stopping = false
        val list = sources
        packing = thread(name = "ZipSending") {
            val id = UUID.randomUUID().toString()
            val dir = File(File(app.filesDir, "zips"), id)
            try {
                trim(app)
                val plan = ZipPlan.make(list.map { it.file }, limit, set(name, about))
                if (plan.tooMany || plan.parts.isEmpty()) throw IOException("nothing to pack")
                // The ZIPs, a file of the library at a time, and the room the phone keeps.
                val fetched = list.filter { it.uri == null }.maxOfOrNull { it.file.size } ?: 0L
                val needed = plan.parts.sumOf { it.size } + fetched + SPARE
                val free = StatFs(app.filesDir.path).availableBytes
                if (free < needed) {
                    emit(ZipEvents.noRoom(needed - SPARE, maxOf(0L, free - SPARE)))
                    return@thread
                }
                dir.mkdirs()
                val files = plan.parts.map { p -> File(dir, fileName(name, partName, p.number, plan.parts.size)) }
                write(app, list, plan, files)
                packed = Packed(id, name, plan, files)
                emit(ZipEvents.ready(plan, files.map { it.name }))
            } catch (e: PackStopped) {
                dir.deleteRecursively()
                emit(ZipEvents.stopped())
            } catch (e: SourceChanged) {
                dir.deleteRecursively()
                emit(ZipEvents.failed("changed", list.getOrNull(e.item)?.file?.name))
            } catch (e: Exception) {
                Log.w(TAG, "packing", e)
                dir.deleteRecursively()
                emit(ZipEvents.failed(if (MediaStoreSink.isFull(e)) "no_room" else "failed"))
            } finally {
                if (packing === Thread.currentThread()) packing = null
            }
        }
    }

    /** Writes the parts, fetching library files that aren't on the phone one at a time, each just before it's needed. */
    private fun write(app: Context, list: List<Source>, plan: ZipPlan, files: List<File>) {
        val total = list.sumOf { it.file.size }
        var done = 0L
        var filesDone = 0
        // The last entry of each file: after it the file counts as packed, and a fetched copy goes.
        val last = HashMap<Int, Pair<Int, Int>>()
        for (p in plan.parts) for ((i, e) in p.entries.withIndex()) last[e.file] = p.number to i
        val fetched = HashMap<Int, Uri>()
        var reported = 0L
        fun report(part: Int, fetching: String? = null, force: Boolean = false) {
            val now = System.nanoTime()
            if (!force && now - reported < 200_000_000) return
            reported = now
            emit(ZipEvents.packing(filesDone, list.size, done, total, part, plan.parts.size, fetching))
        }
        for (part in plan.parts) {
            report(part.number, force = true)
            RandomAccessFile(files[part.number - 1], "rw").use { raf ->
                raf.setLength(0)
                ZipWriter(part.layout, ChannelOut(raf.channel)).write(
                    open = { i ->
                        if (i == 0) {
                            ByteArrayInputStream(part.jsonBytes)
                        } else {
                            val e = part.entries[i - 1]
                            val s = list[e.file]
                            val uri = s.uri ?: fetched.getOrPut(e.file) {
                                report(part.number, fetching = s.file.name, force = true)
                                Fetcher.fetch(app, listOf(s.ref!!), auth)?.single() ?: throw PackStopped()
                            }
                            open(app, uri, e.offset, e.length, whole = e.piece == null)
                        }
                    },
                    progress = { n ->
                        done += n
                        report(part.number)
                    },
                    stopped = { stopping },
                )
            }
            for ((i, e) in part.entries.withIndex()) {
                if (last[e.file] != part.number to i) continue
                filesDone++
                list[e.file].ref?.takeIf { e.file in fetched }?.let { Fetcher.forget(app, it) }
            }
        }
        report(plan.parts.size, force = true)
    }

    /**
     * [uri]'s bytes from [offset], [length] of them. A whole file isn't cut short, so the writer
     * notices one that grew since it was described.
     */
    private fun open(app: Context, uri: Uri, offset: Long, length: Long, whole: Boolean): InputStream {
        val fd = app.contentResolver.openFileDescriptor(uri, "r") ?: throw IOException("can't open $uri")
        val input = FileInputStream(fd.fileDescriptor)
        val stream = object : InputStream() {
            override fun read(): Int = input.read()
            override fun read(b: ByteArray, off: Int, len: Int): Int = input.read(b, off, len)
            override fun close() {
                input.close()
                fd.close()
            }
        }
        try {
            if (offset > 0) {
                try {
                    input.channel.position(offset)
                } catch (e: IOException) {
                    // A pipe: read up to there instead.
                    var left = offset
                    while (left > 0) left -= input.skip(left).takeIf { it > 0 } ?: throw IOException("$uri ended before $offset")
                }
            }
        } catch (e: IOException) {
            stream.close()
            throw e
        }
        return if (whole) stream else Bounded(stream, length)
    }

    /** At most [left] bytes of [inner]. */
    private class Bounded(private val inner: InputStream, private var left: Long) : InputStream() {
        override fun read(): Int = if (left-- > 0) inner.read() else -1

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (left <= 0) return -1
            val n = inner.read(b, off, minOf(len.toLong(), left).toInt())
            if (n > 0) left -= n
            return n
        }

        override fun close() = inner.close()
    }

    /** Stops packing, if it runs; what was packed so far goes. */
    fun stop() {
        val running = packing ?: return
        stopping = true
        Fetcher.cancel()
        running.join(10_000)
    }

    /** The screen closed: the files waiting are forgotten. The ZIPs stay for a day, for apps that still read them. */
    fun close() {
        stop()
        sources = emptyList()
        packed = null
    }

    /**
     * Opens Android's share sheet with part [part] of what was packed, or all parts, without Share in it:
     * a ZIP isn't sent to the server or packed again by mistake. [chosen] hears which app took it.
     */
    fun send(activity: Activity, part: Int?) {
        val p = packed ?: return
        val files = if (part == null) p.files else listOfNotNull(p.files.getOrNull(part - 1))
        if (files.isEmpty()) return
        val uris = files.map { FileProvider.getUriForFile(activity, "${activity.packageName}.files", it) }
        val intent = if (uris.size == 1) {
            Intent(Intent.ACTION_SEND).putExtra(Intent.EXTRA_STREAM, uris[0])
        } else {
            Intent(Intent.ACTION_SEND_MULTIPLE).putParcelableArrayListExtra(Intent.EXTRA_STREAM, ArrayList(uris))
        }
        intent.type = "application/zip"
        intent.clipData = ClipData.newRawUri(null, uris[0]).apply { uris.drop(1).forEach { addItem(ClipData.Item(it)) } }
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        val callback = PendingIntent.getBroadcast(
            activity,
            part ?: 0,
            Intent(activity, ZipChosenReceiver::class.java).putExtra(ZipChosenReceiver.EXTRA_PART, part ?: 0),
            // Mutable, so the share sheet can add which app was chosen; the intent names our receiver.
            (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0) or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val chooser = Intent.createChooser(intent, null, callback.intentSender)
        chooser.putExtra(Intent.EXTRA_EXCLUDE_COMPONENTS, ownComponents(activity))
        activity.startActivity(chooser)
    }

    /** An app took a part, or all of them (part 0): the screen says so. */
    fun chosen(context: Context, part: Int, component: ComponentName?) {
        val label = component?.let {
            runCatching { context.packageManager.getApplicationLabel(context.packageManager.getApplicationInfo(it.packageName, 0)).toString() }.getOrNull()
        }
        emit(ZipEvents.sent(part, label))
    }

    private fun ownComponents(context: Context): Array<ComponentName> =
        arrayOf(MainActivity::class.java.name, MainActivity.SEND_AS_ZIP, MainActivity.OPEN_IN_SHARE).map { ComponentName(context.packageName, it) }.toTypedArray()

    /** Copies what was packed into Download/Share, where it stays; how many ZIPs. */
    fun saveToDownloads(context: Context): Int {
        val p = packed ?: return 0
        val resolver = context.contentResolver
        for (file in p.files) {
            val uri = resolver.insert(
                MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY),
                ContentValues().apply {
                    put(MediaStore.MediaColumns.DISPLAY_NAME, file.name)
                    put(MediaStore.MediaColumns.MIME_TYPE, "application/zip")
                    put(MediaStore.MediaColumns.RELATIVE_PATH, "Download/${MediaStoreSink.ALBUM}")
                    put(MediaStore.MediaColumns.IS_PENDING, 1)
                },
            ) ?: throw IOException("MediaStore refused ${file.name}")
            try {
                resolver.openOutputStream(uri, "w")!!.use { out -> file.inputStream().use { it.copyTo(out, 1 shl 20) } }
                resolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
            } catch (e: Exception) {
                runCatching { resolver.delete(uri, null, null) }
                throw e
            }
        }
        return p.files.size
    }

    /** ZIPs older than a day go; apps that read them have long had them. */
    fun trim(context: Context) {
        val old = System.currentTimeMillis() - KEEP_MS
        val current = packed?.id
        File(context.applicationContext.filesDir, "zips").listFiles()?.forEach { dir ->
            if (dir.name != current && dir.walkBottomUp().all { it.lastModified() < old }) dir.deleteRecursively()
        }
    }

    /** "Photos 4 Oct 2026.zip", or with several parts [partName] filled in: "Photos 4 Oct 2026 (1 of 4).zip". */
    fun fileName(name: String, partName: String, part: Int, parts: Int): String {
        val base = if (parts == 1) name else partName.replace("{name}", name).replace("{part}", "$part").replace("{parts}", "$parts")
        return Names.clean(base).take(200) + ".zip"
    }

    /**
     * A photo's EXIF date, "2026:10:04 16:15:02", with its offset where the camera wrote one; without
     * one, in the phone's time zone, where it was most likely taken.
     */
    private fun exifTime(exif: ExifInterface): Long? {
        val text = exif.getAttribute(ExifInterface.TAG_DATETIME_ORIGINAL) ?: exif.getAttribute(ExifInterface.TAG_DATETIME) ?: return null
        val local = runCatching { LocalDateTime.parse(text.trim(), EXIF_TIME) }.getOrNull() ?: return null
        val offset = exif.getAttribute(ExifInterface.TAG_OFFSET_TIME_ORIGINAL)?.let { runCatching { ZoneOffset.of(it.trim()) }.getOrNull() }
        return (if (offset != null) local.atOffset(offset).toInstant() else local.atZone(ZoneId.systemDefault()).toInstant()).toEpochMilli()
    }

    private val EXIF_TIME = DateTimeFormatter.ofPattern("yyyy:MM:dd HH:mm:ss")

    private fun kindOf(mime: String) = when {
        mime.startsWith("image/") -> "photo"
        mime.startsWith("video/") -> "video"
        else -> "document"
    }

    /**
     * When a lent file was taken, in ms since 1970: as MediaStore or the photo picker say, else a
     * photo's EXIF date, else when the file last changed, else now.
     */
    private fun taken(app: Context, uri: Uri, mime: String): Long {
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
        if (mime.startsWith("image/")) {
            try {
                app.contentResolver.openInputStream(uri)?.use { exifTime(ExifInterface(it)) }?.let { return it }
            } catch (e: Exception) {
                // no EXIF
            }
        }
        return Outbox.lastModified(app, uri) ?: System.currentTimeMillis()
    }
}
