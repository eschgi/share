package com.eschgi.share.transfer

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import androidx.core.database.sqlite.transaction

/**
 * The download queue, kept on disk so it survives the process: batches, their files, and the
 * index of files already saved on this phone (file id → MediaStore URI).
 */
class TransferDb private constructor(context: Context) : SQLiteOpenHelper(context, "transfers.db", null, VERSION) {

    override fun onConfigure(db: SQLiteDatabase) {
        db.setForeignKeyConstraintsEnabled(true)
    }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE batches (
                id TEXT PRIMARY KEY,
                created_at INTEGER NOT NULL,
                state TEXT NOT NULL DEFAULT 'active', -- active | paused | finished | cancelled
                no_space INTEGER NOT NULL DEFAULT 0
            )""",
        )
        db.execSQL(
            """CREATE TABLE items (
                batch TEXT NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
                file_id TEXT NOT NULL,
                name TEXT NOT NULL,
                size INTEGER NOT NULL,
                mime TEXT NOT NULL,
                kind TEXT NOT NULL,
                state TEXT NOT NULL DEFAULT 'queued', -- queued | done | failed | skipped | cancelled
                bytes INTEGER NOT NULL DEFAULT 0,
                target TEXT,
                error TEXT,
                PRIMARY KEY (batch, file_id)
            )""",
        )
        db.execSQL("CREATE INDEX items_queued ON items (state, batch)")
        db.execSQL("CREATE TABLE saved (file_id TEXT PRIMARY KEY, uri TEXT NOT NULL, saved_at INTEGER NOT NULL)")
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        // Only version 1 so far.
    }

    fun addBatch(id: String, files: List<FileRef>, skipped: Set<String>, now: Long) {
        writableDatabase.transaction {
            insertOrThrow("batches", null, ContentValues().apply {
                put("id", id)
                put("created_at", now)
            })
            for (f in files.distinctBy { it.id }) {
                insertOrThrow("items", null, ContentValues().apply {
                    put("batch", id)
                    put("file_id", f.id)
                    put("name", f.name)
                    put("size", f.size)
                    put("mime", f.mime)
                    put("kind", f.kind)
                    put("state", if (f.id in skipped) TransferItem.SKIPPED else TransferItem.QUEUED)
                })
            }
        }
    }

    /** The next file to fetch: oldest batch first, in the order they were picked. */
    fun nextQueued(exclude: Set<String>): TransferItem? {
        readableDatabase.rawQuery(
            """SELECT $ITEM_COLUMNS FROM items i JOIN batches b ON b.id = i.batch
               WHERE i.state = 'queued' AND b.state = 'active'
               ORDER BY b.created_at, i.rowid""",
            null,
        ).use { c ->
            while (c.moveToNext()) {
                val item = item(c)
                if (item.file.id !in exclude) return item
            }
        }
        return null
    }

    fun hasQueued(): Boolean = readableDatabase.rawQuery(
        "SELECT 1 FROM items i JOIN batches b ON b.id = i.batch WHERE i.state = 'queued' AND b.state = 'active' LIMIT 1",
        null,
    ).use { it.moveToFirst() }

    /** Bytes still to fetch, for the job's estimate. */
    fun queuedBytes(): Long = readableDatabase.rawQuery(
        "SELECT COALESCE(SUM(i.size - i.bytes), 0) FROM items i JOIN batches b ON b.id = i.batch WHERE i.state = 'queued' AND b.state = 'active'",
        null,
    ).use { if (it.moveToFirst()) it.getLong(0) else 0 }

    fun items(batch: String): List<TransferItem> =
        readableDatabase.rawQuery("SELECT $ITEM_COLUMNS FROM items i WHERE i.batch = ? ORDER BY i.rowid", arrayOf(batch)).use { c ->
            buildList { while (c.moveToNext()) add(item(c)) }
        }

    fun item(batch: String, fileId: String): TransferItem? = readableDatabase.rawQuery(
        "SELECT $ITEM_COLUMNS FROM items i WHERE i.batch = ? AND i.file_id = ?",
        arrayOf(batch, fileId),
    ).use { if (it.moveToFirst()) item(it) else null }

    data class Batch(val id: String, val state: String, val noSpace: Boolean, val createdAt: Long)

    fun batch(id: String): Batch? =
        readableDatabase.rawQuery("SELECT id, state, no_space, created_at FROM batches WHERE id = ?", arrayOf(id)).use {
            if (it.moveToFirst()) Batch(it.getString(0), it.getString(1), it.getInt(2) != 0, it.getLong(3)) else null
        }

    /** Batches worth showing: not finished, or finished in the last [sinceMs]. */
    fun recentBatches(since: Long): List<Batch> = readableDatabase.rawQuery(
        "SELECT id, state, no_space, created_at FROM batches WHERE state IN ('active', 'paused') OR created_at >= ? ORDER BY created_at",
        arrayOf(since.toString()),
    ).use { c -> buildList { while (c.moveToNext()) add(Batch(c.getString(0), c.getString(1), c.getInt(2) != 0, c.getLong(3))) } }

    fun setTarget(batch: String, fileId: String, target: String?) = update(batch, fileId, ContentValues().apply { put("target", target) })

    fun setBytes(batch: String, fileId: String, bytes: Long) = update(batch, fileId, ContentValues().apply { put("bytes", bytes) })

    fun finish(batch: String, fileId: String, state: String, error: String? = null) =
        update(batch, fileId, ContentValues().apply {
            put("state", state)
            put("error", error)
        })

    fun setBatchState(batch: String, state: String, noSpace: Boolean = false) {
        writableDatabase.update("batches", ContentValues().apply {
            put("state", state)
            put("no_space", if (noSpace) 1 else 0)
        }, "id = ?", arrayOf(batch))
    }

    /** Active batches with nothing left to fetch are finished. */
    fun finishDrained(): List<String> {
        val drained = readableDatabase.rawQuery(
            "SELECT id FROM batches b WHERE state = 'active' AND NOT EXISTS (SELECT 1 FROM items i WHERE i.batch = b.id AND i.state = 'queued')",
            null,
        ).use { c -> buildList { while (c.moveToNext()) add(c.getString(0)) } }
        drained.forEach { setBatchState(it, "finished") }
        return drained
    }

    /** Cancels a batch; returns its unfinished files, whose partial data is to be removed. */
    fun cancel(batch: String): List<TransferItem> = writableDatabase.transaction {
        val open = items(batch).filter { it.state == TransferItem.QUEUED }
        update("items", ContentValues().apply { put("state", TransferItem.CANCELLED) }, "batch = ? AND state = 'queued'", arrayOf(batch))
        update("batches", ContentValues().apply { put("state", "cancelled") }, "id = ?", arrayOf(batch))
        open
    }

    /** Queues failed files and paused batches again. Returns the batches that go on. */
    fun retry(since: Long): List<String> = writableDatabase.transaction {
        val batches = recentBatches(since).filter { it.state != "cancelled" }.map { it.id }
        for (b in batches) {
            update("items", ContentValues().apply {
                put("state", TransferItem.QUEUED)
                putNull("error")
            }, "batch = ? AND state = 'failed'", arrayOf(b))
        }
        val resumed = batches.filter { b ->
            rawQuery("SELECT 1 FROM items WHERE batch = ? AND state = 'queued' LIMIT 1", arrayOf(b)).use { it.moveToFirst() }
        }
        resumed.forEach { setBatchState(it, "active") }
        resumed
    }

    /** Paused batches go on, e.g. when new files are queued after making room. */
    fun resumePaused() {
        writableDatabase.update("batches", ContentValues().apply {
            put("state", "active")
            put("no_space", 0)
        }, "state = 'paused'", null)
    }

    fun pauseActive() {
        writableDatabase.update("batches", ContentValues().apply { put("state", "paused") }, "state = 'active'", null)
    }

    /** Stops everything queued, e.g. when the phone was signed out. */
    fun failQueued(error: String): List<TransferItem> = writableDatabase.transaction {
        val open = rawQuery("SELECT $ITEM_COLUMNS FROM items i WHERE i.state = 'queued'", null).use { c ->
            buildList { while (c.moveToNext()) add(item(c)) }
        }
        update("items", ContentValues().apply {
            put("state", TransferItem.FAILED)
            put("error", error)
        }, "state = 'queued'", null)
        update("batches", ContentValues().apply { put("state", "finished") }, "state IN ('active', 'paused')", null)
        open
    }

    fun deleteBatchesBefore(time: Long) {
        writableDatabase.delete("batches", "created_at < ? AND state IN ('finished', 'cancelled')", arrayOf(time.toString()))
    }

    fun markSaved(fileId: String, uri: String, now: Long) {
        writableDatabase.insertWithOnConflict("saved", null, ContentValues().apply {
            put("file_id", fileId)
            put("uri", uri)
            put("saved_at", now)
        }, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun forgetSaved(fileId: String) {
        writableDatabase.delete("saved", "file_id = ?", arrayOf(fileId))
    }

    /** The saved URIs of those of [ids] that were saved before. */
    fun saved(ids: Collection<String>): Map<String, String> {
        val out = HashMap<String, String>()
        // SQLite allows 999 parameters in older versions; stay well under.
        for (chunk in ids.distinct().chunked(500)) {
            val marks = chunk.joinToString(",") { "?" }
            readableDatabase.rawQuery("SELECT file_id, uri FROM saved WHERE file_id IN ($marks)", chunk.toTypedArray()).use { c ->
                while (c.moveToNext()) out[c.getString(0)] = c.getString(1)
            }
        }
        return out
    }

    private fun update(batch: String, fileId: String, values: ContentValues) {
        writableDatabase.update("items", values, "batch = ? AND file_id = ?", arrayOf(batch, fileId))
    }

    private fun item(c: Cursor) = TransferItem(
        batch = c.getString(0),
        file = FileRef(id = c.getString(1), name = c.getString(2), size = c.getLong(3), mime = c.getString(4), kind = c.getString(5)),
        state = c.getString(6),
        bytes = c.getLong(7),
        target = if (c.isNull(8)) null else c.getString(8),
    )

    companion object {
        private const val VERSION = 1
        private const val ITEM_COLUMNS = "i.batch, i.file_id, i.name, i.size, i.mime, i.kind, i.state, i.bytes, i.target"

        @Volatile private var instance: TransferDb? = null

        fun get(context: Context): TransferDb =
            instance ?: synchronized(this) { instance ?: TransferDb(context.applicationContext).also { instance = it } }
    }
}
