package com.eschgi.share.transfer

import android.content.Context
import android.net.Uri
import androidx.core.content.FileProvider
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
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

    /** Content URIs for [files], or null if cancelled. Throws if one can't be fetched. */
    @Synchronized
    fun fetch(context: Context, files: List<FileRef>): List<Uri>? {
        val app = context.applicationContext
        val dir = File(app.cacheDir, "fetch")
        trim(dir)
        val token = SecretStore(app).read(SecretStore.DEVICE_TOKEN) ?: throw IOException("signed out")
        val config = ServerStore(app).config() ?: throw IOException("no server")
        val server = ServerConnection(config, token)
        val downloader = Downloader()
        val current = Abort().also { abort = it }
        val uris = ArrayList<Uri>()
        val live = HashMap<String, Long>()
        val finished = HashSet<String>()
        val items = files.map { TransferItem(BATCH, it, TransferItem.QUEUED, 0, null) }
        val local = RouteMonitor.current(app).isLocal
        var reported = 0L
        fun report(running: Boolean, force: Boolean = true) {
            val now = System.nanoTime()
            if (!force && now - reported < 250_000_000) return
            reported = now
            val states = items.map { if (it.file.id in finished) it.copy(state = TransferItem.DONE) else it }
            Downloads.emit(BatchSnapshot.of(BATCH, running, false, states, live, local).toMap())
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
                        if (!download(app, downloader, server, file, FileSink(target), current) { live[file.id] = it; report(true, force = false) }) return null
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

    /** False if it was cancelled. */
    private fun download(
        app: Context,
        downloader: Downloader,
        server: ServerConnection,
        file: FileRef,
        sink: FileSink,
        abort: Abort,
        onBytes: (Long) -> Unit,
    ): Boolean {
        var attempt = 0
        while (true) {
            val route = RouteMonitor.current(app)
            when (val outcome = downloader.fetch(file.id, file.size, sink, open = { server.open(it, route.isLocal) }, abort = abort, onBytes = onBytes)) {
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
                    if (route.isLocal) RouteMonitor.check(app)
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
