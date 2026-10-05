package com.eschgi.share.transfer

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import androidx.core.database.sqlite.transaction

/** A file picked for sending. */
data class Picked(val uri: String, val name: String, val size: Long, val mime: String) {
    val kind: String
        get() = when {
            mime.startsWith("image/") -> "photo"
            mime.startsWith("video/") -> "video"
            else -> "document"
        }
}

/** One file of an upload batch, as the queue keeps it. */
data class UploadRow(
    val batch: String,
    val seq: Int,
    val file: Picked,
    val state: String,
    /** The tus upload; once it's done, the file's id on the server. */
    val uploadId: String?,
    val bytes: Long,
    /** Into an encrypted folder: the file's key and what goes with it, until it is sent (UploadSeal). */
    val seal: String? = null,
) {
    companion object {
        const val QUEUED = "queued"
        const val DONE = "done"
        const val FAILED = "failed"
        const val LOST = "lost" // can't be read any more: pick it again
        const val CANCELLED = "cancelled"
    }
}

/** A batch of files sent together; signed in, into [folder] (a PIN sends into its own). */
data class UploadBatch(val id: String, val auth: String, val state: String, val paused: String?, val createdAt: Long, val folder: String? = null) {
    companion object {
        const val DEVICE = Credentials.DEVICE // signed in
        const val PIN = Credentials.PIN

        /** Why a batch is paused: its folder is gone, or no longer the person's. */
        const val FOLDER_GONE = "folder_gone"
    }
}

/**
 * A batch as the send screen shows it; read by UploadState.fromMap in platform.dart. Only a
 * few files go along: the last one sent, the one on its way and the next ones.
 */
data class UploadSnapshot(
    val batch: String,
    val auth: String,
    val folder: String?,
    val running: Boolean,
    val paused: String?,
    val total: Int,
    val done: Int,
    val failed: Int,
    val lost: Int,
    val bytesTotal: Long,
    val bytesDone: Long,
    val etaSeconds: Long?,
    val local: Boolean,
    val items: List<Map<String, Any?>>,
) {
    fun toMap(): Map<String, Any?> = mapOf(
        "type" to "upload",
        "batch" to batch,
        "auth" to auth,
        "folder" to folder,
        "running" to running,
        "paused" to paused,
        "total" to total,
        "done" to done,
        "failed" to failed,
        "lost" to lost,
        "bytes_total" to bytesTotal,
        "bytes_done" to bytesDone,
        "eta_seconds" to etaSeconds,
        "local" to local,
        "items" to items,
    )

    companion object {
        const val WINDOW = 4

        fun of(batch: UploadBatch, rows: List<UploadRow>, live: Map<String, Long> = emptyMap(), etaSeconds: Long? = null, local: Boolean = false): UploadSnapshot {
            val counted = rows.filter { it.state != UploadRow.CANCELLED }
            fun bytes(r: UploadRow) = if (r.state == UploadRow.DONE) r.file.size else (live[key(r)] ?: r.bytes).coerceIn(0, r.file.size)
            val current = counted.indexOfFirst { it.state == UploadRow.QUEUED }.let { if (it < 0) counted.size else it }
            val from = (current - 1).coerceAtLeast(0)
            val window = counted.subList(from.coerceAtMost(counted.size), (from + WINDOW).coerceAtMost(counted.size))
            return UploadSnapshot(
                batch = batch.id,
                auth = batch.auth,
                folder = batch.folder,
                running = batch.state == "active" && counted.any { it.state == UploadRow.QUEUED },
                paused = if (batch.state == "paused") batch.paused ?: "user" else null,
                total = counted.size,
                done = counted.count { it.state == UploadRow.DONE },
                failed = counted.count { it.state == UploadRow.FAILED },
                lost = counted.count { it.state == UploadRow.LOST },
                bytesTotal = counted.sumOf { it.file.size },
                bytesDone = counted.sumOf { bytes(it) },
                etaSeconds = etaSeconds,
                local = local,
                items = window.map {
                    mapOf(
                        "seq" to it.seq,
                        "name" to it.file.name,
                        "size" to it.file.size,
                        "kind" to it.file.kind,
                        "state" to it.state,
                        "bytes" to bytes(it),
                    )
                },
            )
        }

        fun key(r: UploadRow) = "${r.batch}/${r.seq}"
    }
}

/** The upload tables of [TransferDb]. */
class UploadQueue(context: Context) {
    private val db = TransferDb.get(context)

    fun addBatch(id: String, auth: String, files: List<Picked>, now: Long, folder: String? = null) {
        db.writableDatabase.transaction {
            insertOrThrow("upload_batches", null, ContentValues().apply {
                put("id", id)
                put("created_at", now)
                put("auth", auth)
                put("folder", folder)
            })
            files.forEachIndexed { i, f ->
                insertOrThrow("uploads", null, ContentValues().apply {
                    put("batch", id)
                    put("seq", i)
                    put("uri", f.uri)
                    put("name", f.name)
                    put("size", f.size)
                    put("mime", f.mime)
                })
            }
        }
    }

    /**
     * A file picked again that was lost (same name and size) goes on where it was, with its
     * upload. Returns the batch it belongs to, or null if it wasn't lost.
     */
    fun relink(auth: String, f: Picked): String? {
        val row = db.readableDatabase.rawQuery(
            """SELECT u.batch, u.seq FROM uploads u JOIN upload_batches b ON b.id = u.batch
               WHERE u.state = 'lost' AND u.name = ? AND u.size = ? AND b.auth = ? AND b.state != 'cancelled'
               ORDER BY b.created_at DESC LIMIT 1""",
            arrayOf(f.name, f.size.toString(), auth),
        ).use { if (it.moveToFirst()) it.getString(0) to it.getInt(1) else null } ?: return null
        db.writableDatabase.transaction {
            update("uploads", ContentValues().apply {
                put("uri", f.uri)
                put("state", UploadRow.QUEUED)
                putNull("error")
            }, "batch = ? AND seq = ?", arrayOf(row.first, row.second.toString()))
            update("upload_batches", ContentValues().apply {
                put("state", "active")
                putNull("paused")
            }, "id = ? AND state != 'cancelled'", arrayOf(row.first))
        }
        return row.first
    }

    fun next(): UploadRow? = db.readableDatabase.rawQuery(
        """SELECT $COLUMNS FROM uploads u JOIN upload_batches b ON b.id = u.batch
           WHERE u.state = 'queued' AND b.state = 'active' ORDER BY b.created_at, u.seq LIMIT 1""",
        null,
    ).use { if (it.moveToFirst()) row(it) else null }

    fun hasQueued(): Boolean = db.readableDatabase.rawQuery(
        "SELECT 1 FROM uploads u JOIN upload_batches b ON b.id = u.batch WHERE u.state = 'queued' AND b.state = 'active' LIMIT 1",
        null,
    ).use { it.moveToFirst() }

    fun queuedBytes(): Long = db.readableDatabase.rawQuery(
        "SELECT COALESCE(SUM(u.size - u.bytes), 0) FROM uploads u JOIN upload_batches b ON b.id = u.batch WHERE u.state = 'queued' AND b.state = 'active'",
        null,
    ).use { if (it.moveToFirst()) it.getLong(0) else 0 }

    fun rows(batch: String): List<UploadRow> =
        db.readableDatabase.rawQuery("SELECT $COLUMNS FROM uploads u WHERE u.batch = ? ORDER BY u.seq", arrayOf(batch)).use { c ->
            buildList { while (c.moveToNext()) add(row(c)) }
        }

    fun batch(id: String): UploadBatch? = db.readableDatabase.rawQuery(
        "SELECT $BATCH_COLUMNS FROM upload_batches WHERE id = ?",
        arrayOf(id),
    ).use { if (it.moveToFirst()) batch(it) else null }

    /** Batches worth showing: not finished, or started in the last [since]. */
    fun recentBatches(since: Long): List<UploadBatch> = db.readableDatabase.rawQuery(
        "SELECT $BATCH_COLUMNS FROM upload_batches WHERE state IN ('active', 'paused') OR created_at >= ? ORDER BY created_at",
        arrayOf(since.toString()),
    ).use { c -> buildList { while (c.moveToNext()) add(batch(c)) } }

    fun setUploadId(r: UploadRow, id: String?) = update(r, ContentValues().apply { put("upload_id", id) })

    fun setSeal(r: UploadRow, seal: String?) = update(r, ContentValues().apply { put("seal", seal) })

    fun setBytes(r: UploadRow, bytes: Long) = update(r, ContentValues().apply { put("bytes", bytes) })

    fun finish(r: UploadRow, state: String, error: String? = null, uploadId: String? = r.uploadId) = update(r, ContentValues().apply {
        put("state", state)
        put("error", error)
        put("upload_id", uploadId)
        if (state == UploadRow.DONE) {
            put("bytes", r.file.size)
            putNull("seal") // the file's key isn't needed here any more
        }
    })

    /** Stops the batches of [auth] until they're resumed, e.g. when a PIN ended. */
    fun pause(auth: String, why: String) {
        db.writableDatabase.update("upload_batches", ContentValues().apply {
            put("state", "paused")
            put("paused", why)
        }, "auth = ? AND state = 'active'", arrayOf(auth))
    }

    /** Stops one batch until it's resumed, e.g. when its folder is gone. */
    fun pauseBatch(id: String, why: String) {
        db.writableDatabase.update("upload_batches", ContentValues().apply {
            put("state", "paused")
            put("paused", why)
        }, "id = ? AND state = 'active'", arrayOf(id))
    }

    fun pauseActive(why: String) {
        db.writableDatabase.update("upload_batches", ContentValues().apply {
            put("state", "paused")
            put("paused", why)
        }, "state = 'active'", null)
    }

    /**
     * Paused batches of [auth] go on; those whose folder is gone go into [folder], or stay
     * paused without one. Returns whether any go on.
     */
    fun resume(auth: String?, folder: String? = null): Boolean = db.writableDatabase.transaction {
        if (folder != null) {
            update("upload_batches", ContentValues().apply { put("folder", folder) }, "state = 'paused' AND paused = ? AND auth = ?", arrayOf(UploadBatch.FOLDER_GONE, UploadBatch.DEVICE))
        }
        val where = buildList {
            add("state = 'paused'")
            if (auth != null) add("auth = ?")
            if (folder == null) add("IFNULL(paused, '') != '${UploadBatch.FOLDER_GONE}'")
        }.joinToString(" AND ")
        update("upload_batches", ContentValues().apply {
            put("state", "active")
            putNull("paused")
        }, where, auth?.let { arrayOf(it) }) > 0
    }

    /** Failed files go again. */
    fun retryFailed(since: Long): Boolean = db.writableDatabase.transaction {
        val batches = recentBatches(since).filter { it.state != "cancelled" }.map { it.id }
        var any = false
        for (b in batches) {
            val n = update("uploads", ContentValues().apply {
                put("state", UploadRow.QUEUED)
                putNull("error")
            }, "batch = ? AND state = 'failed'", arrayOf(b))
            if (n > 0) {
                any = true
                update("upload_batches", ContentValues().apply {
                    put("state", "active")
                    putNull("paused")
                }, "id = ?", arrayOf(b))
            }
        }
        any
    }

    /** Cancels a batch; returns its unfinished files, whose uploads are to be removed. */
    fun cancel(batch: String): List<UploadRow> = db.writableDatabase.transaction {
        val open = rows(batch).filter { it.state == UploadRow.QUEUED || it.state == UploadRow.LOST }
        update("uploads", ContentValues().apply {
            put("state", UploadRow.CANCELLED)
            putNull("seal")
        }, "batch = ? AND state IN ('queued', 'lost')", arrayOf(batch))
        update("upload_batches", ContentValues().apply { put("state", "cancelled") }, "id = ?", arrayOf(batch))
        open
    }

    /** Active batches with nothing left to send are finished. */
    fun finishDrained(): List<String> {
        val drained = db.readableDatabase.rawQuery(
            "SELECT id FROM upload_batches b WHERE state = 'active' AND NOT EXISTS (SELECT 1 FROM uploads u WHERE u.batch = b.id AND u.state = 'queued')",
            null,
        ).use { c -> buildList { while (c.moveToNext()) add(c.getString(0)) } }
        for (id in drained) {
            db.writableDatabase.update("upload_batches", ContentValues().apply { put("state", "finished") }, "id = ?", arrayOf(id))
        }
        return drained
    }

    fun deleteBatchesBefore(time: Long) {
        db.writableDatabase.delete("upload_batches", "created_at < ? AND state IN ('finished', 'cancelled')", arrayOf(time.toString()))
    }

    private fun update(r: UploadRow, values: ContentValues) {
        db.writableDatabase.update("uploads", values, "batch = ? AND seq = ?", arrayOf(r.batch, r.seq.toString()))
    }

    private fun row(c: Cursor) = UploadRow(
        batch = c.getString(0),
        seq = c.getInt(1),
        file = Picked(uri = c.getString(2), name = c.getString(3), size = c.getLong(4), mime = c.getString(5)),
        state = c.getString(6),
        uploadId = if (c.isNull(7)) null else c.getString(7),
        bytes = c.getLong(8),
        seal = if (c.isNull(9)) null else c.getString(9),
    )

    private fun batch(c: Cursor) = UploadBatch(
        id = c.getString(0),
        auth = c.getString(1),
        state = c.getString(2),
        paused = if (c.isNull(3)) null else c.getString(3),
        createdAt = c.getLong(4),
        folder = if (c.isNull(5)) null else c.getString(5),
    )

    private companion object {
        const val COLUMNS = "u.batch, u.seq, u.uri, u.name, u.size, u.mime, u.state, u.upload_id, u.bytes, u.seal"
        const val BATCH_COLUMNS = "id, auth, state, paused, created_at, folder"
    }
}
