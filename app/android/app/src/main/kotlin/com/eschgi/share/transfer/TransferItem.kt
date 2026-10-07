package com.eschgi.share.transfer

import com.eschgi.share.e2ee.SealedFile
import org.json.JSONArray
import org.json.JSONObject

/**
 * A file from the library, as the Dart side hands it over (FileInfo.toJson): with the folder it
 * lies in, and for an encrypted one its key sealed for the folder's key ([enc]). [size] is the
 * plain size, what lands on the phone.
 */
data class FileRef(
    val id: String,
    val name: String,
    val size: Long,
    val mime: String,
    val kind: String,
    val folder: String = "",
    val enc: SealedFile? = null,
) {
    val isMedia: Boolean get() = kind == "photo" || kind == "video"

    /** [enc] as the API has it, for TransferDb; null for a plain file. */
    fun encJson(): String? = enc?.let {
        JSONObject().put("version", it.version).put("key", it.key).put("header", it.header).put("plain_size", it.plainSize).toString()
    }

    companion object {
        fun parseList(json: String?): List<FileRef> {
            if (json.isNullOrEmpty()) return emptyList()
            val array = JSONArray(json)
            return List(array.length()) { i ->
                val o = array.getJSONObject(i)
                FileRef(
                    id = o.getString("id"),
                    name = o.optString("name").ifEmpty { o.getString("id") },
                    size = o.optLong("size"),
                    mime = o.optString("mime").ifEmpty { "application/octet-stream" },
                    kind = o.optString("kind").ifEmpty { "document" },
                    folder = o.optString("folder"),
                    enc = SealedFile.of(o),
                )
            }
        }

        /** A file as TransferDb keeps it: [enc] is [encJson]'s. */
        fun of(id: String, name: String, size: Long, mime: String, kind: String, folder: String?, enc: String?): FileRef {
            val sealed = enc?.let { SealedFile.of(JSONObject().put("id", id).put("folder", folder ?: "").put("size", size).put("enc", JSONObject(it))) }
            return FileRef(id, name, size, mime, kind, folder ?: "", sealed)
        }
    }
}

/** One file of a batch, as TransferDb keeps it. */
data class TransferItem(
    val batch: String,
    val file: FileRef,
    val state: String,
    /** Bytes on the phone so far; the sink's own length is what a resume trusts. */
    val bytes: Long,
    /** Where it's being written: the pending MediaStore row. */
    val target: String?,
    /** Whose key fetches it: the phone's, or a PIN's (Credentials). */
    val auth: String = Credentials.DEVICE,
) {
    companion object {
        const val QUEUED = "queued"
        const val DONE = "done"
        const val FAILED = "failed"
        const val SKIPPED = "skipped" // already on the phone
        const val CANCELLED = "cancelled"
    }
}

/**
 * A batch as the download sheet sees it; read by TransferState.fromMap in platform.dart.
 * Skipped files (already on the phone) don't count towards the totals.
 */
data class BatchSnapshot(
    val batch: String,
    val running: Boolean,
    val total: Int,
    val done: Int,
    val failed: Int,
    val skipped: Int,
    val bytesTotal: Long,
    val bytesDone: Long,
    val media: Int,
    val documents: Int,
    val local: Boolean,
    val noSpace: Boolean,
) {
    fun toMap(): Map<String, Any?> = mapOf(
        "type" to "transfer",
        "batch" to batch,
        "running" to running,
        "total" to total,
        "done" to done,
        "failed" to failed,
        "skipped" to skipped,
        "bytes_total" to bytesTotal,
        "bytes_done" to bytesDone,
        "media" to media,
        "documents" to documents,
        "local" to local,
        "no_space" to noSpace,
    )

    companion object {
        /** [live] has the bytes of the files being downloaded right now, by file id. */
        fun of(
            batch: String,
            running: Boolean,
            noSpace: Boolean,
            items: List<TransferItem>,
            live: Map<String, Long> = emptyMap(),
            local: Boolean = false,
        ): BatchSnapshot {
            val counted = items.filter { it.state != TransferItem.SKIPPED }
            return BatchSnapshot(
                batch = batch,
                running = running,
                total = counted.size,
                done = counted.count { it.state == TransferItem.DONE },
                failed = counted.count { it.state == TransferItem.FAILED },
                skipped = items.size - counted.size,
                bytesTotal = counted.sumOf { it.file.size },
                bytesDone = counted.sumOf {
                    when (it.state) {
                        TransferItem.DONE -> it.file.size
                        else -> (live[it.file.id] ?: it.bytes).coerceIn(0, it.file.size)
                    }
                },
                media = counted.count { it.file.isMedia },
                documents = counted.count { !it.file.isMedia },
                local = local,
                noSpace = noSpace,
            )
        }
    }
}
