package com.eschgi.share.transfer

import android.content.Context
import android.os.Environment
import android.os.StatFs
import android.util.Log
import androidx.core.net.toUri
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import java.io.FileNotFoundException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread

/**
 * Downloads what [TransferDb] has queued, three files at a time, until the queue is empty or
 * its host stops it. The host is a user-initiated job from Android 14 on and a foreground
 * worker before; the engine doesn't care which. Every byte goes straight into the file's
 * sink, so any later run continues where this one stopped.
 */
object DownloadEngine {
    private const val TAG = "DownloadEngine"
    private const val PARALLEL = 3
    private const val MAX_ATTEMPTS = 8
    private const val TICK_MS = 250L
    private const val SPACE_MARGIN = 64L * 1024 * 1024

    enum class Result { FINISHED, RESCHEDULE }

    interface Host {
        val isStopped: Boolean

        /** About once a second, for the host's notification. */
        fun onProgress(progress: Progress)
    }

    data class Progress(val files: Int, val done: Int, val bytesTotal: Long, val bytesDone: Long)

    private val lock = Any()
    @Volatile private var running = false

    /** The downloads in flight, by batch and file id, so they can be stopped. */
    private val aborts = ConcurrentHashMap<String, Abort>()

    /** Their bytes so far, by file id. */
    private val live = ConcurrentHashMap<String, Long>()

    private val downloader = Downloader()

    /** Runs [block] so that a run that's about to end can't miss what it adds to the queue. */
    fun <T> locked(block: () -> T): T = synchronized(lock) { block() }

    fun isRunning() = running

    fun liveBytes(): Map<String, Long> = HashMap(live)

    fun abort(batch: String) {
        aborts.forEach { (key, abort) -> if (key.startsWith("$batch/")) abort.stop() }
    }

    fun abortAll() = aborts.values.forEach { it.stop() }

    /** Why a run ended before the queue did. */
    private enum class Stop { SIGNED_OUT, OFFLINE }

    /** Blocks until the queue is done or [host] stops. */
    fun run(context: Context, host: Host): Result {
        synchronized(lock) {
            if (running) return Result.FINISHED // the running one will see the new files
            running = true
        }
        val app = context.applicationContext
        try {
            TransferNotification.cancelSummary(app)
            val seen = LinkedHashSet<String>()
            while (true) {
                val stopped = runOnce(app, host, seen)
                synchronized(lock) {
                    if (stopped == null && !host.isStopped && TransferDb.get(app).hasQueued()) return@synchronized
                    running = false
                    return when {
                        // Stopped by the system (or the person): the host decides what's next.
                        host.isStopped -> Result.RESCHEDULE
                        stopped == Stop.OFFLINE -> {
                            TransferNotification.waiting(app)
                            Result.RESCHEDULE
                        }
                        else -> {
                            TransferNotification.summary(app, seen.toList())
                            Result.FINISHED
                        }
                    }
                }
            }
        } finally {
            synchronized(lock) { running = false }
        }
    }

    /** One pass over the queue; returns why it stopped early, if it did. */
    private fun runOnce(app: Context, host: Host, seen: MutableSet<String>): Stop? {
        val db = TransferDb.get(app)
        val token = SecretStore(app).read(SecretStore.DEVICE_TOKEN)
        val config = ServerStore(app).config()
        if (token == null || config == null) {
            signedOut(app)
            return Stop.SIGNED_OUT
        }
        val server = ServerConnection(config, token)
        RouteMonitor.check(app) // a fresh look before every run

        val stop = AtomicReference<Stop?>(null)
        val claimed = HashSet<String>()
        val workers = List(PARALLEL) { n ->
            thread(name = "download-$n") {
                while (!host.isStopped && stop.get() == null) {
                    val item = synchronized(claimed) {
                        db.nextQueued(claimed)?.also { claimed += it.file.id }
                    } ?: break
                    synchronized(seen) { seen += item.batch }
                    try {
                        download(app, db, server, item, host, stop)
                    } catch (e: Exception) {
                        Log.w(TAG, "download of ${item.file.id} failed", e)
                        db.finish(item.batch, item.file.id, TransferItem.FAILED, e.toString())
                    } finally {
                        synchronized(claimed) { claimed -= item.file.id }
                        live.remove(item.file.id)
                    }
                }
            }
        }

        var ticks = 0
        val published = HashMap<String, BatchSnapshot>()
        while (workers.any { it.isAlive }) {
            workers.first { it.isAlive }.join(TICK_MS)
            if (host.isStopped || stop.get() != null) abortAll()
            val snapshots = Downloads.publish(app, published)
            if (++ticks % 4 == 0) host.onProgress(progress(synchronized(seen) { snapshots.filter { it.batch in seen } }))
        }
        db.finishDrained()
        Downloads.publish(app, published)
        if (stop.get() == Stop.SIGNED_OUT) signedOut(app)
        return stop.get()
    }

    private fun download(app: Context, db: TransferDb, server: ServerConnection, item: TransferItem, host: Host, stop: AtomicReference<Stop?>) {
        val resolver = app.contentResolver
        val file = item.file
        // Saved already, e.g. by another batch that got there first.
        db.saved(listOf(file.id))[file.id]?.let { uri ->
            if (MediaStoreSink.exists(resolver, uri.toUri())) {
                item.target?.let { MediaStoreSink(resolver, it.toUri()).discard() }
                db.finish(item.batch, file.id, TransferItem.SKIPPED)
                return
            }
            db.forgetSaved(file.id)
        }

        val key = "${item.batch}/${file.id}"
        val abort = Abort()
        aborts[key] = abort
        try {
            var target = item.target
            var attempts = 0
            while (true) {
                if (host.isStopped || stop.get() != null || abort.stopped) break
                val batch = db.batch(item.batch)
                if (batch == null || batch.state != "active") break
                val have = live[file.id] ?: item.bytes
                if (freeSpace() < file.size - have + SPACE_MARGIN) {
                    db.setBatchState(item.batch, "paused", noSpace = true)
                    return
                }
                val sink = target?.let { MediaStoreSink(resolver, it.toUri()) }
                    ?: MediaStoreSink.create(resolver, file).also {
                        target = it.uri.toString()
                        db.setTarget(item.batch, file.id, target)
                    }
                val route = RouteMonitor.current(app)
                val outcome = downloader.fetch(file.id, file.size, sink, open = { server.open(it, route.isLocal) }, abort = abort) {
                    live[file.id] = it
                }
                when (outcome) {
                    Downloader.Outcome.Done -> {
                        val uri = sink.commit()
                        db.markSaved(file.id, uri, System.currentTimeMillis())
                        db.setBytes(item.batch, file.id, file.size)
                        db.finish(item.batch, file.id, TransferItem.DONE)
                        return
                    }
                    Downloader.Outcome.SignedOut -> {
                        stop.compareAndSet(null, Stop.SIGNED_OUT)
                        return
                    }
                    Downloader.Outcome.Gone -> return fail(db, item, sink, "deleted on the server")
                    Downloader.Outcome.Stopped -> break
                    is Downloader.Outcome.Failed -> return fail(db, item, sink, "HTTP ${outcome.status} ${outcome.message}")
                    is Downloader.Outcome.WriteFailed -> {
                        if (MediaStoreSink.isFull(outcome.cause)) {
                            db.setBatchState(item.batch, "paused", noSpace = true)
                            return
                        }
                        if (outcome.cause !is FileNotFoundException || ++attempts >= MAX_ATTEMPTS) {
                            return fail(db, item, sink, outcome.cause.toString())
                        }
                        // The pending file went away (cleared by the system, or by hand): start a new one.
                        target = null
                        live.remove(file.id)
                        db.setTarget(item.batch, file.id, null)
                        db.setBytes(item.batch, file.id, 0)
                    }
                    is Downloader.Outcome.Retry -> {
                        db.setBytes(item.batch, file.id, live[file.id] ?: have)
                        if (!ServerConnection.online(app)) {
                            stop.compareAndSet(null, Stop.OFFLINE) // the host waits for a network
                            return
                        }
                        attempts = if (outcome.progressed) 1 else attempts + 1
                        if (attempts >= MAX_ATTEMPTS) {
                            // The partial file stays, so "Try again" continues it.
                            db.finish(item.batch, file.id, TransferItem.FAILED, outcome.cause?.toString() ?: "HTTP ${outcome.status}")
                            return
                        }
                        if (route.isLocal) RouteMonitor.check(app) // maybe the local address went away
                        pause(outcome.retryAfterMs ?: backoff(attempts), abort, host)
                    }
                }
            }
            // Stopped: keep what arrived, unless the batch was cancelled.
            live[file.id]?.let { db.setBytes(item.batch, file.id, it) }
            if (db.batch(item.batch)?.state == "cancelled") target?.let { MediaStoreSink(resolver, it.toUri()).discard() }
        } finally {
            aborts.remove(key)
        }
    }

    private fun fail(db: TransferDb, item: TransferItem, sink: DownloadSink, error: String) {
        sink.discard()
        db.setTarget(item.batch, item.file.id, null)
        db.finish(item.batch, item.file.id, TransferItem.FAILED, error)
    }

    /** The phone's key stopped working: nothing queued can be fetched any more. */
    private fun signedOut(app: Context) {
        val resolver = app.contentResolver
        abortAll()
        for (item in TransferDb.get(app).failQueued("signed out")) {
            item.target?.let { MediaStoreSink(resolver, it.toUri()).discard() }
        }
        Downloads.publish(app, HashMap())
    }

    /** 1, 2, 4 … 30 seconds. */
    internal fun backoff(attempt: Int): Long = (1000L shl (attempt - 1).coerceIn(0, 5)).coerceAtMost(30_000)

    private fun pause(ms: Long, abort: Abort, host: Host) {
        val until = System.currentTimeMillis() + ms.coerceAtMost(120_000)
        while (System.currentTimeMillis() < until && !abort.stopped && !host.isStopped) Thread.sleep(100)
    }

    @Suppress("DEPRECATION") // the primary shared storage, where MediaStore writes
    private fun freeSpace(): Long = try {
        StatFs(Environment.getExternalStorageDirectory().path).availableBytes
    } catch (e: IllegalArgumentException) {
        Long.MAX_VALUE
    }

    private fun progress(batches: List<BatchSnapshot>) = Progress(
        files = batches.sumOf { it.total },
        done = batches.sumOf { it.done + it.failed },
        bytesTotal = batches.sumOf { it.bytesTotal },
        bytesDone = batches.sumOf { it.bytesDone },
    )
}
