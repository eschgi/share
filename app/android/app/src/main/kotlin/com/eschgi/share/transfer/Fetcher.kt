package com.eschgi.share.transfer

import android.content.Context
import android.net.Uri
import androidx.core.content.FileProvider
import com.eschgi.share.e2ee.Keys
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.ServerInfo
import java.io.File
import java.io.IOException

/**
 * Gets files onto the phone for sharing with another app or opening one (a video in the
 * system player): the saved copy if there is one, otherwise a download into the cache.
 * Progress goes out as the batch "fetch", which the Dart side shows while it waits.
 */
object Fetcher {
    const val BATCH = "fetch"
    private const val ATTEMPTS = 3
    private const val KEEP_MS = 24 * 3600_000L

    @Volatile private var abort: Abort? = null

    fun cancel() {
        abort?.stop()
    }

    /** Content URIs for [files], fetched with [auth]'s key, or null if cancelled. Throws if one can't be fetched. */
    @Synchronized
    fun fetch(context: Context, files: List<FileRef>, auth: String = Credentials.DEVICE): List<Uri>? {
        val app = context.applicationContext
        val dir = File(app.cacheDir, "fetch")
        trim(dir)
        val (token, config) = Credentials.of(app, auth) ?: throw IOException(if (auth == Credentials.PIN) "no PIN" else "signed out")
        val server = ServerConnection(config, token)
        val downloader = Downloader()
        val current = Abort().also { abort = it }
        val uris = ArrayList<Uri>()
        val live = HashMap<String, Long>()
        val finished = HashSet<String>()
        val items = files.map { TransferItem(BATCH, it, TransferItem.QUEUED, 0, null) }
        val local = Credentials.atHome(auth, RouteMonitor.current(app).isLocal)
        var reported = 0L
        fun report(running: Boolean, force: Boolean = true) {
            val now = System.nanoTime()
            if (!force && now - reported < 250_000_000) return
            reported = now
            val states = items.map { if (it.file.id in finished) it.copy(state = TransferItem.DONE) else it }
            // Known after the first file it fetches: from a bucket nothing comes over the address at home.
            Downloads.emit(BatchSnapshot.of(BATCH, running, false, states, live, local && !ServerInfo.knownS3(config)).toMap())
        }
        try {
            for (file in files) {
                if (file.id in finished) continue
                val saved = Downloads.savedUri(app, file.id)
                if (saved != null) {
                    uris += saved
                } else {
                    val target = File(File(dir, file.id), safeName(file))
                    if (!(target.exists() && target.length() == file.size)) {
                        if (!download(app, downloader, server, auth, file, FileSink(target), current) { live[file.id] = it; report(true, force = false) }) return null
                    }
                    target.setLastModified(System.currentTimeMillis())
                    uris += FileProvider.getUriForFile(app, "${app.packageName}.files", target)
                }
                finished += file.id
                report(true)
            }
            return uris
        } finally {
            if (abort === current) abort = null
            report(false)
        }
    }

    /** The copy of [file] fetched into the cache goes, e.g. once it is packed into a ZIP. */
    fun forget(context: Context, file: FileRef) {
        File(File(context.applicationContext.cacheDir, "fetch"), file.id).deleteRecursively()
    }

    /** The copy fetched into the cache earlier, if it is there whole. */
    fun cached(context: Context, file: FileRef): Uri? {
        val app = context.applicationContext
        val target = File(File(File(app.cacheDir, "fetch"), file.id), safeName(file))
        if (!(target.exists() && target.length() == file.size)) return null
        target.setLastModified(System.currentTimeMillis())
        return FileProvider.getUriForFile(app, "${app.packageName}.files", target)
    }

    /** False if it was cancelled. */
    private fun download(
        app: Context,
        downloader: Downloader,
        server: ServerConnection,
        auth: String,
        file: FileRef,
        sink: FileSink,
        abort: Abort,
        onBytes: (Long) -> Unit,
    ): Boolean {
        // An encrypted file lands decrypted in the cache, as a saved one does in the gallery.
        val cipher = Keys.cipher(app, auth, file.enc)
        var attempt = 0
        while (true) {
            val home = Credentials.atHome(auth, RouteMonitor.settled(app).isLocal)
            val open = { path: String -> server.open(path, home) }
            // Asked only now, for a file that isn't on the phone: what is there opens offline.
            val outcome = when (ServerInfo.of(server.config) { server.open(it, home, readTimeoutMs = 15_000) }?.storage) {
                null -> Downloader.Outcome.Retry(IOException("the server didn't say where its files are"))
                ServerInfo.Storage.DISK -> downloader.fetch(file.id, file.size, sink, open = open, abort = abort, onBytes = onBytes, cipher = cipher)
                ServerInfo.Storage.S3 -> downloader.fetchS3(file.id, file.size, sink, link = { S3Links.fetch(file.id, open) }, abort = abort, onBytes = onBytes, cipher = cipher)
            }
            when (outcome) {
                Downloader.Outcome.Done -> {
                    sink.commit()
                    return true
                }
                Downloader.Outcome.Stopped -> {
                    sink.discard()
                    return false
                }
                is Downloader.Outcome.Retry -> {
                    if (++attempt >= ATTEMPTS) throw IOException("couldn't fetch ${file.name}", outcome.cause)
                    if (auth == Credentials.DEVICE) RouteMonitor.check(app) // over either address
                    Thread.sleep(DownloadEngine.backoff(attempt))
                }
                else -> {
                    sink.discard()
                    throw IOException("couldn't fetch ${file.name}: $outcome")
                }
            }
        }
    }

    /** What's in the cache for longer than a day goes; the system may clear it sooner. */
    private fun trim(dir: File) {
        val old = System.currentTimeMillis() - KEEP_MS
        dir.listFiles()?.forEach { entry ->
            if (entry.walkBottomUp().all { it.lastModified() < old }) entry.deleteRecursively()
        }
    }

    private fun safeName(file: FileRef): String {
        val name = file.name.replace(Regex("""[/\\\u0000-\u001f]"""), "_").trim().take(120)
        return if (name.isEmpty() || name == "." || name == "..") file.id else name
    }
}
