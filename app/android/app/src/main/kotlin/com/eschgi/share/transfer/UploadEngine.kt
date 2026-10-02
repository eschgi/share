package com.eschgi.share.transfer

import android.content.Context
import android.content.Intent
import android.util.Log
import androidx.core.net.toUri
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerConfig
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.readLimited
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap

/**
 * Sends what [UploadQueue] has queued, one file at a time, until the queue is empty or the
 * host stops it. Signed-in phones send over the route RouteMonitor picked; with a PIN over the
 * public address. Every file keeps its tus upload, so any later run goes on where the server has
 * it.
 */
object UploadEngine {
    private const val TAG = "UploadEngine"
    private const val MAX_ATTEMPTS = 8

    /** Over the local address nothing limits a request; over Cloudflare 100 MB does. */
    private const val LOCAL_CHUNK = 64L * 1024 * 1024
    private const val PUBLIC_CHUNK = 20L * 1024 * 1024

    private val lock = Any()
    @Volatile private var running = false
    @Volatile private var current: Pair<String, Abort>? = null

    /** Bytes of the file on its way, by batch/seq. */
    private val live = ConcurrentHashMap<String, Long>()

    private val uploader = Uploader()

    fun <T> locked(block: () -> T): T = synchronized(lock) { block() }

    fun isRunning() = running

    fun liveBytes(): Map<String, Long> = HashMap(live)

    fun abortAll() {
        current?.second?.stop()
    }

    /** The batch whose file is on its way now. */
    fun currentBatch(): String? = current?.first?.substringBefore('/')

    fun abort(batch: String) {
        current?.let { (key, abort) -> if (key.startsWith("$batch/")) abort.stop() }
    }

    /** Blocks until the queue is done or [host] stops. */
    fun run(context: Context, host: TransferHost): RunResult {
        synchronized(lock) {
            if (running) return RunResult.FINISHED // the running one will see the new files
            running = true
        }
        val app = context.applicationContext
        val queue = UploadQueue(app)
        val seen = LinkedHashSet<String>()
        val progress = Progress(app, host, seen)
        try {
            TransferNotification.cancelSentSummary(app)
            var attempts = 0
            var lastKey: String? = null
            val chunks = HashMap<String, Long>()
            while (true) {
                if (host.isStopped) return RunResult.RESCHEDULE
                val item = synchronized(lock) {
                    queue.next() ?: run {
                        queue.finishDrained()
                        running = false
                        progress.publish(force = true)
                        TransferNotification.sent(app, seen.toList())
                        return RunResult.FINISHED
                    }
                }
                seen += item.batch
                val key = UploadSnapshot.key(item)
                if (key != lastKey) {
                    attempts = 0
                    lastKey = key
                }
                val batch = queue.batch(item.batch) ?: continue
                val credentials = credentials(app, batch.auth)
                if (credentials == null) {
                    queue.pause(batch.auth, if (batch.auth == UploadBatch.PIN) "pin_ended" else "signed_out")
                    continue
                }
                val (token, config) = credentials
                val server = ServerConnection(config, token)
                val local = batch.auth == UploadBatch.DEVICE && RouteMonitor.settled(app).isLocal
                val chunk = chunks.getOrPut("${config.publicUrl}/$local") { chunkSize(server, local) }
                val abort = Abort()
                current = key to abort
                progress.local = local
                val outcome = try {
                    uploader.upload(
                        item.file.name, item.file.mime, item.file.size, ContentSource(app.contentResolver, item.file.uri.toUri()), item.uploadId, chunk,
                        open = { method, path -> server.open(path, local, method = method) },
                        abort = abort,
                        onCreated = { queue.setUploadId(item, it) },
                        onBytes = {
                            live[key] = it
                            progress.publish()
                        },
                    )
                } finally {
                    current = null
                }
                live[key]?.let { queue.setBytes(item, it) }
                when (outcome) {
                    is Uploader.Outcome.Done -> {
                        queue.finish(item, UploadRow.DONE, uploadId = outcome.id)
                        live.remove(key)
                        // The thumbnail reads the file once more: its grant goes only afterwards.
                        try {
                            sendThumb(app, server, local, item, outcome.id)
                        } finally {
                            release(app, item)
                        }
                    }
                    Uploader.Outcome.SignedOut, Uploader.Outcome.PinEnded ->
                        // Which of the two it is follows from how the batch sends, not from the answer.
                        queue.pause(batch.auth, if (batch.auth == UploadBatch.PIN) "pin_ended" else "signed_out")
                    Uploader.Outcome.Lost -> queue.finish(item, UploadRow.LOST)
                    Uploader.Outcome.Stopped -> {
                        if (host.isStopped) return RunResult.RESCHEDULE
                        // Cancelled: the batch is gone from the queue already.
                    }
                    is Uploader.Outcome.Failed -> {
                        queue.finish(item, UploadRow.FAILED, "HTTP ${outcome.status} ${outcome.code ?: ""}".trim())
                        release(app, item)
                    }
                    is Uploader.Outcome.Retry -> {
                        if (!ServerConnection.online(app)) {
                            synchronized(lock) { running = false }
                            TransferNotification.sendingWaiting(app)
                            return RunResult.RESCHEDULE // the host waits for a network
                        }
                        attempts = if (outcome.progressed) 1 else attempts + 1
                        if (attempts >= MAX_ATTEMPTS) {
                            queue.finish(item, UploadRow.FAILED, outcome.cause?.toString() ?: "HTTP ${outcome.status}")
                        } else {
                            if (local) RouteMonitor.check(app) // maybe the local address went away
                            if (!rest(outcome.retryAfterMs ?: DownloadEngine.backoff(attempts), host)) return RunResult.RESCHEDULE
                        }
                    }
                }
                progress.publish(force = true)
            }
        } finally {
            synchronized(lock) { running = false }
        }
    }

    /** The key and the server of a batch: the phone's, or the PIN's. */
    private fun credentials(app: Context, auth: String): Pair<String, ServerConfig>? {
        val secrets = SecretStore(app)
        val servers = ServerStore(app)
        val token = secrets.read(if (auth == UploadBatch.PIN) SecretStore.PIN_TOKEN else SecretStore.DEVICE_TOKEN) ?: return null
        val config = (if (auth == UploadBatch.PIN) servers.pinConfig() else servers.config()) ?: return null
        return token to config
    }

    /** What the server takes in one request over the public address (config: chunk_size_mib). */
    private fun chunkSize(server: ServerConnection, local: Boolean): Long {
        if (local) return LOCAL_CHUNK
        return try {
            val conn = server.open("/api/info", false, readTimeoutMs = 15_000)
            try {
                if (conn.responseCode != 200) return PUBLIC_CHUNK
                val size = JSONObject(String(readLimited(conn.inputStream, 64 * 1024))).optLong("chunk_size_bytes")
                if (size > 0) size else PUBLIC_CHUNK
            } finally {
                conn.disconnect()
            }
        } catch (e: IOException) {
            PUBLIC_CHUNK
        } catch (e: JSONException) {
            PUBLIC_CHUNK
        }
    }

    /** The picture of a sent file. Best effort: photos without one get one from the server. */
    private fun sendThumb(app: Context, server: ServerConnection, local: Boolean, item: UploadRow, fileId: String) {
        val thumb = try {
            UploadThumbs.make(app, item.file.uri.toUri(), item.file.kind)
        } catch (e: Exception) {
            Log.w(TAG, "thumbnail of ${item.file.name}", e)
            return
        }
        val jpeg = thumb.jpeg ?: return
        val query = listOfNotNull(
            thumb.width?.let { "width=$it" },
            thumb.height?.let { "height=$it" },
            thumb.durationMs?.let { "duration_ms=$it" },
        ).joinToString("&")
        try {
            val conn = server.open("/api/files/$fileId/thumb" + if (query.isEmpty()) "" else "?$query", local, method = "PUT")
            try {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "image/jpeg")
                conn.setFixedLengthStreamingMode(jpeg.size)
                conn.outputStream.use { it.write(jpeg) }
                if (conn.responseCode != 204) Log.i(TAG, "thumbnail of ${item.file.name}: ${conn.responseCode}")
            } finally {
                conn.disconnect()
            }
        } catch (e: IOException) {
            Log.i(TAG, "thumbnail of ${item.file.name}: $e")
        }
    }

    /** Sent (or refused for good): the picked file's grant isn't needed any more. */
    private fun release(app: Context, item: UploadRow) {
        runCatching { app.contentResolver.releasePersistableUriPermission(item.file.uri.toUri(), Intent.FLAG_GRANT_READ_URI_PERMISSION) }
    }

    /** Waits [ms]; false if the host stopped meanwhile. */
    private fun rest(ms: Long, host: TransferHost): Boolean {
        val until = System.currentTimeMillis() + ms.coerceAtMost(120_000)
        while (System.currentTimeMillis() < until) {
            if (host.isStopped) return false
            Thread.sleep(100)
        }
        return true
    }

    /** Progress for the send screen (4 times a second) and the notification (once a second). */
    private class Progress(private val app: Context, private val host: TransferHost, private val seen: Set<String>) {
        var local = false
        private var shown = 0L
        private var told = 0L
        private val published = HashMap<String, UploadSnapshot>()
        private val rate = Rate()

        fun publish(force: Boolean = false) {
            val now = System.nanoTime()
            if (!force && now - shown < 250_000_000) return
            shown = now
            val snapshots = Uploads.publish(app, published, rate, local)
            if (force || now - told > 1_000_000_000) {
                told = now
                val mine = snapshots.filter { it.batch in seen }
                host.onProgress(
                    TransferProgress(
                        files = mine.sumOf { it.total },
                        done = mine.sumOf { it.done + it.failed + it.lost },
                        bytesTotal = mine.sumOf { it.bytesTotal },
                        bytesDone = mine.sumOf { it.bytesDone },
                    ),
                )
            }
        }
    }

    /** How fast bytes go out, smoothed, for "about 2 min left". */
    class Rate {
        private var lastBytes = -1L
        private var lastAt = 0L
        private var perSecond = 0.0

        /** Takes the bytes sent so far and gives the seconds left for [remaining], once it knows. */
        fun etaSeconds(sent: Long, remaining: Long): Long? {
            val now = System.nanoTime()
            if (lastBytes >= 0 && sent >= lastBytes && now > lastAt) {
                val seconds = (now - lastAt) / 1e9
                if (seconds > 0.2) {
                    val current = (sent - lastBytes) / seconds
                    perSecond = if (perSecond == 0.0) current else perSecond * 0.8 + current * 0.2
                    lastBytes = sent
                    lastAt = now
                }
            } else {
                lastBytes = sent
                lastAt = now
            }
            return if (perSecond > 1) (remaining / perSecond).toLong() else null
        }
    }
}
