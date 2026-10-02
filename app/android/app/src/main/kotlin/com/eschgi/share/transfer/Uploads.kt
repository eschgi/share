package com.eschgi.share.transfer

import android.content.Context
import android.util.Log
import androidx.core.net.toUri
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import java.io.IOException
import java.util.UUID
import java.util.concurrent.CopyOnWriteArraySet
import kotlin.concurrent.thread

/**
 * What the platform channel asks of the uploads: send picked files, cancel, go on after a new
 * PIN or a sign-in, try again. Progress goes to [listen]ers as UploadState maps.
 */
object Uploads {
    private const val TAG = "Uploads"
    private const val KEEP_MS = 7 * 24 * 3600_000L
    private const val RECENT_MS = 3600_000L

    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()

    fun listen(listener: (Map<String, Any?>) -> Unit) = listeners.add(listener)

    fun unlisten(listener: (Map<String, Any?>) -> Unit) = listeners.remove(listener)

    private fun emit(event: Map<String, Any?>) = listeners.forEach { it(event) }

    /**
     * Queues [files] for sending with a phone's key ([auth] device) or a PIN, and returns the
     * batch. A file that was lost and is picked again goes on in its old batch instead.
     */
    fun enqueue(context: Context, auth: String, files: List<Picked>): String? {
        val app = context.applicationContext
        val queue = UploadQueue(app)
        var batch: String? = null
        UploadEngine.locked {
            queue.deleteBatchesBefore(System.currentTimeMillis() - KEEP_MS)
            val fresh = files.filter { f -> queue.relink(auth, f)?.also { batch = it } == null }
            if (fresh.isNotEmpty()) {
                batch = UUID.randomUUID().toString()
                queue.addBatch(batch, auth, fresh, System.currentTimeMillis())
            }
            if (batch != null && !UploadEngine.isRunning()) startHost(app)
        }
        publish(app, HashMap(), null, false)
        return batch
    }

    fun cancel(context: Context, batch: String) {
        val app = context.applicationContext
        val queue = UploadQueue(app)
        val auth = queue.batch(batch)?.auth ?: return
        val open = queue.cancel(batch)
        UploadEngine.abort(batch)
        for (item in open) Outbox.release(app, item.file.uri.toUri())
        publish(app, HashMap(), null, false)
        // The server drops unfinished uploads after a week anyway; saying so now frees the space.
        val started = open.mapNotNull { it.uploadId }
        if (started.isNotEmpty()) thread(name = "cancel uploads") { terminate(app, auth, started) }
    }

    private fun terminate(app: Context, auth: String, uploadIds: List<String>) {
        val token = SecretStore(app).read(if (auth == UploadBatch.PIN) SecretStore.PIN_TOKEN else SecretStore.DEVICE_TOKEN) ?: return
        val config = (if (auth == UploadBatch.PIN) ServerStore(app).pinConfig() else ServerStore(app).config()) ?: return
        val server = ServerConnection(config, token)
        for (id in uploadIds) {
            try {
                val conn = server.open("/tus/$id", false, method = "DELETE")
                conn.setRequestProperty("Tus-Resumable", "1.0.0")
                conn.responseCode
                conn.disconnect()
            } catch (e: IOException) {
                Log.i(TAG, "terminating $id: $e")
            }
        }
    }

    /** After a new PIN or a sign-in: what waited for it goes on. */
    fun resume(context: Context, auth: String?) {
        val app = context.applicationContext
        UploadEngine.locked {
            if (UploadQueue(app).resume(auth) && !UploadEngine.isRunning()) startHost(app)
        }
        publish(app, HashMap(), null, false)
    }

    /** The notification's "Try again": failed files and paused batches go on. */
    fun retry(context: Context) {
        val app = context.applicationContext
        val queue = UploadQueue(app)
        UploadEngine.locked {
            val failed = queue.retryFailed(System.currentTimeMillis() - KEEP_MS)
            val paused = queue.resume(null)
            if ((failed || paused) && !UploadEngine.isRunning()) startHost(app)
        }
        TransferNotification.cancelSentSummary(app)
        publish(app, HashMap(), null, false)
    }

    fun resumeIfNeeded(context: Context) {
        val app = context.applicationContext
        UploadEngine.locked {
            if (!UploadEngine.isRunning() && UploadQueue(app).hasQueued()) startHost(app)
        }
    }

    fun pauseAll(context: Context) {
        UploadQueue(context).pauseActive("user")
        publish(context.applicationContext, HashMap(), null, false)
    }

    /** The batches worth showing, as a new listener should see them first. */
    fun snapshots(context: Context, rate: UploadEngine.Rate? = null, local: Boolean? = null): List<UploadSnapshot> {
        val queue = UploadQueue(context)
        val live = UploadEngine.liveBytes()
        val onLocal = local ?: RouteMonitor.current(context).isLocal
        val sending = UploadEngine.currentBatch()
        return queue.recentBatches(System.currentTimeMillis() - RECENT_MS).map { b ->
            val rows = queue.rows(b.id)
            val snapshot = UploadSnapshot.of(b, rows, live, local = onLocal && b.auth == UploadBatch.DEVICE)
            // The time left only for the batch on its way; the others wait for it.
            if (rate == null || b.id != sending) snapshot else snapshot.copy(etaSeconds = rate.etaSeconds(snapshot.bytesDone, snapshot.bytesTotal - snapshot.bytesDone))
        }
    }

    /** Sends the snapshots that changed since [published], and returns all of them. */
    fun publish(context: Context, published: MutableMap<String, UploadSnapshot>, rate: UploadEngine.Rate?, local: Boolean?): List<UploadSnapshot> {
        val snapshots = snapshots(context, rate, local)
        for (s in snapshots) {
            if (published[s.batch] == s) continue
            published[s.batch] = s
            emit(s.toMap())
        }
        return snapshots
    }

    private fun startHost(app: Context) = Direction.UPLOADS.start(app, UploadQueue(app).queuedBytes())
}
