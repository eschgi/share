package com.eschgi.share.transfer

import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import androidx.core.content.FileProvider
import androidx.core.net.toUri
import java.io.File
import java.io.IOException
import java.util.UUID

/**
 * Files shared into the app from other apps' share sheets ("Send with Share"), waiting until the
 * Dart side sends them, with the phone's key or a PIN, or drops them. A file keeps the right to
 * read it where the sharing app allows; otherwise it is copied into the app first, if there is
 * room, because that right would end with the screen that received it. A copy goes once it is
 * sent or given up, and whatever is left after a week.
 */
object Outbox {
    private const val KEEP_MS = 7 * 24 * 3600_000L

    /** Room a copy must leave on the phone. */
    private const val SPARE = 200L * 1024 * 1024

    /** How a share went: the files kept, and those that couldn't be (no room, unreadable). */
    data class Received(val kept: Int, val skipped: Int)

    /** Whether [intent] is a share of files or text. */
    fun isShare(intent: Intent): Boolean = intent.action == Intent.ACTION_SEND || intent.action == Intent.ACTION_SEND_MULTIPLE

    /** The files of a share, without repeats. */
    fun uris(intent: Intent): List<Uri> {
        val out = LinkedHashSet<Uri>()
        if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
            streams(intent)?.let { out += it }
        } else {
            stream(intent)?.let { out += it }
        }
        intent.clipData?.let { clip -> for (i in 0 until clip.itemCount) clip.getItemAt(i).uri?.let { out += it } }
        return out.filter { it.scheme == "content" || it.scheme == "file" }
    }

    /** A shared text, such as a message with an invite or PIN link in it. */
    fun text(intent: Intent): String? = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString()

    /** The first web address in a text, without the punctuation after it; the Dart side tells whether it is Share's. */
    fun linkIn(text: String): String? = Regex("""https?://\S+""").find(text)?.value?.trimEnd('.', ',', ')', '!', '?', ';', ':', '"', '\'')

    @Suppress("DEPRECATION")
    private fun stream(intent: Intent): Uri? =
        if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java) else intent.getParcelableExtra(Intent.EXTRA_STREAM)

    @Suppress("DEPRECATION")
    private fun streams(intent: Intent): List<Uri>? =
        if (Build.VERSION.SDK_INT >= 33) intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java) else intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM)

    /** Keeps the shared files until they are sent. */
    fun receive(context: Context, uris: List<Uri>): Received {
        val app = context.applicationContext
        trim(app)
        var kept = 0
        var skipped = 0
        val now = System.currentTimeMillis()
        for (uri in uris) {
            val file = try {
                describe(app, uri)?.let { keep(app, uri, it) }
            } catch (e: Exception) {
                null // the sharing app took it back, or it can't be read
            }
            if (file == null) {
                skipped++
                continue
            }
            TransferDb.get(app).writableDatabase.insertOrThrow("shared", null, ContentValues().apply {
                put("uri", file.uri)
                put("name", file.name)
                put("size", file.size)
                put("mime", file.mime)
                put("shared_at", now)
            })
            kept++
        }
        return Received(kept, skipped)
    }

    /** Name, size and type of a file another app hands over. */
    fun describe(context: Context, uri: Uri): Picked? {
        val resolver = context.contentResolver
        var name: String? = null
        var size: Long? = null
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                name = c.getString(0)
                if (!c.isNull(1)) size = c.getLong(1)
            }
        }
        if (size == null) size = runCatching { resolver.openAssetFileDescriptor(uri, "r")?.use { it.length.takeIf { n -> n >= 0 } } }.getOrNull()
        val length = size ?: return null // tus has to know it
        return Picked(uri.toString(), name ?: uri.lastPathSegment ?: "file", length, resolver.getType(uri) ?: "application/octet-stream")
    }

    /** The file as it can be read later: the sharing app's, if it lets us keep it, or a copy. */
    private fun keep(app: Context, uri: Uri, file: Picked): Picked? {
        try {
            app.contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
            return file
        } catch (e: SecurityException) {
            // Most share sheets only lend the file: copy it while we may read it.
        }
        val root = File(app.filesDir, "outbox")
        if (root.apply { mkdirs() }.usableSpace - file.size < SPARE) return null
        val dir = File(root, UUID.randomUUID().toString()).apply { mkdirs() }
        val copy = File(dir, safeName(file.name))
        try {
            val input = app.contentResolver.openInputStream(uri) ?: throw IOException("can't open $uri")
            input.use { inp -> copy.outputStream().use { inp.copyTo(it, 256 * 1024) } }
            if (copy.length() != file.size) throw IOException("${file.name}: ${copy.length()} of ${file.size} bytes")
        } catch (e: Exception) {
            dir.deleteRecursively()
            throw e
        }
        return file.copy(uri = FileProvider.getUriForFile(app, "${app.packageName}.files", copy).toString())
    }

    /** The files waiting, in the order they were shared, with their place in the outbox. */
    private fun rows(context: Context): List<Pair<Long, Picked>> =
        TransferDb.get(context).readableDatabase.rawQuery("SELECT seq, uri, name, size, mime FROM shared ORDER BY seq", null).use { c ->
            buildList { while (c.moveToNext()) add(c.getLong(0) to Picked(c.getString(1), c.getString(2), c.getLong(3), c.getString(4))) }
        }

    fun pending(context: Context): List<Picked> = rows(context).map { it.second }

    /**
     * Hands the files waiting to [send], which queues them; they leave the outbox only once it
     * has. What [send] returns, or null without files. One at a time, so no file goes twice.
     */
    @Synchronized
    fun <T> sendWith(context: Context, send: (List<Picked>) -> T): T? {
        val rows = rows(context)
        if (rows.isEmpty()) return null
        val result = send(rows.map { it.second })
        TransferDb.get(context).writableDatabase.delete("shared", "seq <= ?", arrayOf(rows.last().first.toString()))
        return result
    }

    /** "Don't send": the files go, copies and all. */
    fun drop(context: Context) {
        sendWith(context) { files -> files.forEach { release(context, it.uri.toUri()) } }
    }

    /** Sent, or given up: a copy goes, and another app's file gets its right to read back. */
    fun release(context: Context, uri: Uri) {
        val app = context.applicationContext
        if (isCopy(app, uri)) {
            copyFile(app, uri)?.parentFile?.deleteRecursively()
        } else {
            runCatching { app.contentResolver.releasePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION) }
        }
    }

    fun isCopy(context: Context, uri: Uri): Boolean =
        uri.authority == "${context.packageName}.files" && uri.pathSegments.firstOrNull() == "outbox"

    /** The file behind a copy's URI: outbox/<dir>/<name> in the app's files. */
    private fun copyFile(context: Context, uri: Uri): File? {
        val parts = uri.pathSegments
        if (parts.size != 3 || parts.any { it == ".." || it.contains('/') }) return null
        return File(File(File(context.filesDir, "outbox"), parts[1]), parts[2])
    }

    /** Shares older than a week, and copies nobody sent: they go. */
    private fun trim(app: Context) {
        val old = System.currentTimeMillis() - KEEP_MS
        val db = TransferDb.get(app).writableDatabase
        db.rawQuery("SELECT uri FROM shared WHERE shared_at < ?", arrayOf(old.toString())).use { c ->
            while (c.moveToNext()) release(app, c.getString(0).toUri())
        }
        db.delete("shared", "shared_at < ?", arrayOf(old.toString()))
        File(app.filesDir, "outbox").listFiles()?.forEach { dir ->
            if (dir.walkBottomUp().all { it.lastModified() < old }) dir.deleteRecursively()
        }
    }

    private fun safeName(name: String): String {
        val clean = name.replace(Regex("""[/\\\u0000-\u001f]"""), "_").trim().take(120)
        return if (clean.isEmpty() || clean == "." || clean == "..") "file" else clean
    }
}
