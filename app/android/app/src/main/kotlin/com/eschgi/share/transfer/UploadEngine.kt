package com.eschgi.share.transfer

import android.content.Context
import android.util.Log
import androidx.core.net.toUri
import com.eschgi.share.e2ee.E2eeException
import com.eschgi.share.e2ee.FolderPublicKey
import com.eschgi.share.e2ee.Keys
import com.eschgi.share.e2ee.SendRefused
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.ServerInfo
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap

/**
 * Sends what [UploadQueue] has queued, one file at a time, until the queue is empty or the
 * host stops it. Signed-in phones send over the route RouteMonitor picked; with a PIN over the
 * public address. Every file keeps its tus upload, so any later run goes on where the server has
 * it. Into an encrypted folder each file goes up encrypted, with a key of its own (UploadSeal).
 */
object UploadEngine {
    private const val TAG = "UploadEngine"
    private const val MAX_ATTEMPTS = 8

    /** The server's answers when a batch's folder is gone, or the person sees no folder any more. */
    private val FOLDER_GONE = setOf("folder_gone", "no_folder")

    /** A file that didn't go: what the server says about its folder's keys can't be checked (docs/e2ee-plan.md). */
    const val UNCHECKED = "unchecked"

    /** Over the local address nothing limits a request; over Cloudflare 100 MB does. */
    private const val LOCAL_CHUNK = 64L * 1024 * 1024

    private val lock = Any()
    @Volatile private var running = false
    @Volatile private var current: Pair<String, Abort>? = null

    /** Bytes of the file on its way, by batch/seq. */
    private val live = ConcurrentHashMap<String, Long>()

    private val tus = TusUploader()
    private val s3 = S3Uploader()

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
        // The key each batch's folder is encrypted for (null: plain), asked once a run.
        val targets = HashMap<String, FolderPublicKey?>()
        // How often each file got a new seal because the server asked for one.
        val reseals = HashMap<String, Int>()
        try {
            TransferNotification.cancelSentSummary(app)
            var attempts = 0
            var lastKey: String? = null
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
                if (batch.auth == UploadBatch.DEVICE && batch.folder == null) {
                    // Queued before there were folders: the person says which one.
                    queue.pauseBatch(batch.id, UploadBatch.FOLDER_GONE)
                    continue
                }
                val credentials = Credentials.of(app, batch.auth)
                if (credentials == null) {
                    queue.pause(batch.auth, if (batch.auth == UploadBatch.PIN) "pin_ended" else "signed_out")
                    continue
                }
                val (token, config) = credentials
                val server = ServerConnection(config, token)
                val local = batch.auth == UploadBatch.DEVICE && RouteMonitor.settled(app).isLocal
                val info = ServerInfo.of(config) { server.open(it, local, readTimeoutMs = 15_000) }
                val abort = Abort()
                current = key to abort
                // Into a bucket the bytes go over the internet, wherever the phone is.
                progress.local = local && info?.storage == ServerInfo.Storage.DISK
                val plain = ContentSource(app.contentResolver, item.file.uri.toUri())
                val lastModified = Outbox.lastModified(app, item.file.uri.toUri())
                val onBytes = { bytes: Long ->
                    live[key] = bytes
                    progress.publish()
                }
                var seal: UploadSeal? = null
                val outcome = try {
                    val (sealed, uploadId) = sealOf(queue, item, lastModified, targets) { batchId ->
                        UploadSeal.target(
                            batch.auth, batch.folder, { path -> server.open(path, local, readTimeoutMs = 15_000) },
                            signedIn = { Keys.sendKey(app, it) }, guest = { Keys.guestKey(app, it) },
                        ).also { targets[batchId] = it }
                    }
                    seal = sealed
                    // The encrypted stream goes up for a file into an encrypted folder.
                    val source = seal?.let { SealedSource(it, plain) } ?: plain
                    val size = seal?.encryptedSize ?: item.file.size
                    when (info?.storage) {
                        null -> UploadOutcome.Retry(IOException("the server didn't say how it takes files"))
                        ServerInfo.Storage.DISK -> tus.upload(
                            item.file.name, item.file.mime, size, source, uploadId,
                            if (local) LOCAL_CHUNK else info.chunkSize,
                            open = { method, path -> server.open(path, local, method = method) },
                            abort = abort,
                            onCreated = { queue.setUploadId(item, it) },
                            onBytes = onBytes,
                            folder = batch.folder,
                            lastModified = lastModified,
                            enc = seal?.enc(),
                        )
                        ServerInfo.Storage.S3 -> s3.upload(
                            item.file.name, size, source, uploadId,
                            open = { method, path -> server.open(path, local, method = method) },
                            abort = abort,
                            onCreated = { queue.setUploadId(item, it) },
                            onBytes = onBytes,
                            folder = batch.folder,
                            lastModified = lastModified,
                            enc = seal?.enc(),
                        )
                    }
                } catch (e: SendRefused) {
                    // What the server says about the folder's keys can't be checked: nothing goes in.
                    UploadOutcome.Failed(0, UNCHECKED)
                } catch (e: UploadSeal.HttpFailure) {
                    when (e.status) {
                        401 -> if (batch.auth == UploadBatch.PIN) UploadOutcome.PinEnded else UploadOutcome.SignedOut
                        404 -> UploadOutcome.Failed(404, "folder_gone")
                        else -> UploadOutcome.Retry(e, e.status)
                    }
                } catch (e: IOException) {
                    UploadOutcome.Retry(e) // asking which key to encrypt for failed
                } finally {
                    current = null
                }
                live[key]?.let { queue.setBytes(item, it) }
                when (outcome) {
                    is UploadOutcome.Done -> {
                        queue.finish(item, UploadRow.DONE, uploadId = outcome.id)
                        live.remove(key)
                        // The thumbnail reads the file once more: its grant goes only afterwards.
                        try {
                            sendThumb(app, server, local, item, outcome.id, seal)
                        } finally {
                            release(app, item)
                        }
                    }
                    UploadOutcome.SignedOut, UploadOutcome.PinEnded ->
                        // Which of the two it is follows from how the batch sends, not from the answer.
                        queue.pause(batch.auth, if (batch.auth == UploadBatch.PIN) "pin_ended" else "signed_out")
                    UploadOutcome.Lost -> queue.finish(item, UploadRow.LOST)
                    UploadOutcome.Stopped -> {
                        if (host.isStopped) return RunResult.RESCHEDULE
                        // Cancelled: the batch is gone from the queue already.
                    }
                    is UploadOutcome.Failed -> {
                        if (outcome.code in UploadSeal.RESEAL && (reseals[key] ?: 0) < 2) {
                            // Sealed for an older version of the folder's key, or plain into a folder
                            // encrypted since (or the other way round): a new seal, and a new upload.
                            reseals[key] = (reseals[key] ?: 0) + 1
                            targets.remove(batch.id)
                            queue.setSeal(item, null)
                            queue.setUploadId(item, null)
                            live.remove(key)
                        } else if (outcome.code in FOLDER_GONE) {
                            // The file stays queued until the person chooses another folder.
                            queue.pauseBatch(batch.id, UploadBatch.FOLDER_GONE)
                        } else {
                            queue.finish(item, UploadRow.FAILED, if (outcome.code == UNCHECKED) UNCHECKED else "HTTP ${outcome.status} ${outcome.code ?: ""}".trim())
                            release(app, item)
                        }
                    }
                    is UploadOutcome.Retry -> {
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

    /**
     * The seal [item] goes up with, and the upload to go on with: the one it has, unless the file
     * changed since (or can't tell, for one already under way): then a new key and a new upload,
     * so no chunk is ever encrypted twice with the same nonce for other bytes. A file that
     * hasn't started gets a seal when its folder is encrypted ([target] asks once a batch); one
     * under way without a seal goes on plain, as the server took it.
     */
    private fun sealOf(
        queue: UploadQueue,
        item: UploadRow,
        lastModified: Long?,
        targets: Map<String, FolderPublicKey?>,
        target: (batch: String) -> FolderPublicKey?,
    ): Pair<UploadSeal?, String?> {
        var seal = UploadSeal.parse(item.seal)
        var uploadId = item.uploadId
        if (seal != null) {
            val same = seal.fits(item.file.size, lastModified) && (uploadId == null || (lastModified != null && seal.lastModified != null))
            if (!same) {
                seal = null
                uploadId = null
                queue.setSeal(item, null)
                queue.setUploadId(item, null)
            }
        }
        if (seal == null && uploadId == null) {
            val folder = if (targets.containsKey(item.batch)) targets[item.batch] else target(item.batch)
            if (folder != null) {
                seal = UploadSeal.new(folder, item.file.size, lastModified)
                queue.setSeal(item, seal.toJson())
            }
        }
        return seal to uploadId
    }

    /** The picture of a sent file, sealed with its key for an encrypted one. Best effort: photos without one get one from the server. */
    private fun sendThumb(app: Context, server: ServerConnection, local: Boolean, item: UploadRow, fileId: String, seal: UploadSeal?) {
        val thumb = try {
            UploadThumbs.make(app, item.file.uri.toUri(), item.file.kind)
        } catch (e: Exception) {
            Log.w(TAG, "thumbnail of ${item.file.name}", e)
            return
        }
        val jpeg = thumb.jpeg?.let { j ->
            try {
                seal?.sealThumb(j) ?: j
            } catch (e: E2eeException) {
                null
            }
        } ?: return
        val query = listOfNotNull(
            thumb.width?.let { "width=$it" },
            thumb.height?.let { "height=$it" },
            thumb.durationMs?.let { "duration_ms=$it" },
        ).joinToString("&")
        try {
            val conn = server.open("/api/files/$fileId/thumb" + if (query.isEmpty()) "" else "?$query", local, method = "PUT")
            try {
                conn.doOutput = true
                // A sealed thumbnail is nothing the server can look into.
                conn.setRequestProperty("Content-Type", if (seal != null) "application/octet-stream" else "image/jpeg")
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

    /** Sent (or refused for good): the picked file's grant isn't needed any more, nor a copy of a shared one. */
    private fun release(app: Context, item: UploadRow) {
        Outbox.release(app, item.file.uri.toUri())
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
