package com.eschgi.share.transfer

import android.content.Context
import android.net.Uri
import android.os.Build
import android.util.Log
import androidx.core.net.toUri
import com.eschgi.share.net.RouteMonitor
import java.util.UUID
import java.util.concurrent.CopyOnWriteArraySet

/**
 * What the platform channel asks of the downloads: queue a batch, cancel it, try again, and
 * which files are on the phone already. Progress goes to [listen]ers as TransferState maps.
 */
object Downloads {
    private const val TAG = "Downloads"
    private const val KEEP_MS = 7 * 24 * 3600_000L

    /** How long a finished batch is still reported to a new listener. */
    private const val RECENT_MS = 3600_000L

    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()

    fun listen(listener: (Map<String, Any?>) -> Unit) = listeners.add(listener)

    fun unlisten(listener: (Map<String, Any?>) -> Unit) = listeners.remove(listener)

    fun emit(event: Map<String, Any?>) = listeners.forEach { it(event) }

    /** Queues [files] and returns the batch id. Files already on the phone are skipped. */
    fun enqueue(context: Context, files: List<FileRef>): String {
        val app = context.applicationContext
        val db = TransferDb.get(app)
        val now = System.currentTimeMillis()
        val onPhone = saved(app, files.map { it.id }).toSet()
        val id = UUID.randomUUID().toString()
        DownloadEngine.locked {
            db.deleteBatchesBefore(now - KEEP_MS)
            db.addBatch(id, files, onPhone, now)
            db.resumePaused()
            if (!DownloadEngine.isRunning()) startHost(app)
        }
        publish(app, HashMap())
        return id
    }

    fun cancel(context: Context, batch: String) {
        val app = context.applicationContext
        val open = TransferDb.get(app).cancel(batch)
        DownloadEngine.abort(batch)
        for (item in open) item.target?.let { MediaStoreSink(app.contentResolver, it.toUri()).discard() }
        publish(app, HashMap())
    }

    /** The notification's "Try again": failed files and paused batches go on. */
    fun retry(context: Context) {
        val app = context.applicationContext
        DownloadEngine.locked {
            val resumed = TransferDb.get(app).retry(System.currentTimeMillis() - KEEP_MS)
            if (resumed.isNotEmpty() && !DownloadEngine.isRunning()) startHost(app)
        }
        TransferNotification.cancelSummary(app)
        publish(app, HashMap())
    }

    /**
     * When the app opens: downloads that were still queued (after a reboot, or when the system
     * gave up on the job) go on.
     */
    fun resumeIfNeeded(context: Context) {
        val app = context.applicationContext
        DownloadEngine.locked {
            if (!DownloadEngine.isRunning() && TransferDb.get(app).hasQueued()) startHost(app)
        }
    }

    /** The person stopped the downloads (Task Manager): they wait for "Try again". */
    fun pauseAll(context: Context) {
        TransferDb.get(context).pauseActive()
        publish(context.applicationContext, HashMap())
    }

    /** Which of [ids] are saved on this phone and still there. */
    fun saved(context: Context, ids: List<String>): List<String> {
        val db = TransferDb.get(context)
        val resolver = context.contentResolver
        return db.saved(ids).mapNotNull { (id, uri) ->
            if (MediaStoreSink.exists(resolver, uri.toUri())) {
                id
            } else {
                db.forgetSaved(id) // deleted in the gallery
                null
            }
        }
    }

    /** Where a saved file is, if it still is. */
    fun savedUri(context: Context, id: String): Uri? {
        val uri = TransferDb.get(context).saved(listOf(id))[id]?.toUri() ?: return null
        return uri.takeIf { MediaStoreSink.exists(context.contentResolver, it) }
    }

    /** The batches worth showing, as a new listener should see them first. */
    fun snapshots(context: Context): List<BatchSnapshot> {
        val db = TransferDb.get(context)
        val live = DownloadEngine.liveBytes()
        val local = RouteMonitor.current(context).isLocal
        return db.recentBatches(System.currentTimeMillis() - RECENT_MS).map { b ->
            BatchSnapshot.of(b.id, running = b.state == "active", noSpace = b.noSpace, items = db.items(b.id), live = live, local = local)
        }
    }

    /** Sends the snapshots that changed since [published], and returns all of them. */
    fun publish(context: Context, published: MutableMap<String, BatchSnapshot>): List<BatchSnapshot> {
        val snapshots = snapshots(context)
        for (s in snapshots) {
            if (published[s.batch] == s) continue
            published[s.batch] = s
            emit(s.toMap())
        }
        return snapshots
    }

    private fun startHost(app: Context) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            try {
                DownloadJobService.schedule(app, TransferDb.get(app).queuedBytes())
                return
            } catch (e: RuntimeException) {
                // Not allowed right now (e.g. the app isn't visible): a foreground worker instead.
                Log.w(TAG, "can't schedule a user-initiated job", e)
            }
        }
        DownloadWorker.enqueue(app)
    }
}
