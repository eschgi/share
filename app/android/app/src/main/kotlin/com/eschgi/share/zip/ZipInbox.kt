package com.eschgi.share.zip

import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.io.InputStream
import java.io.RandomAccessFile
import java.security.MessageDigest
import java.util.zip.CRC32

/** A piece whose bytes don't match its CRC-32: its part has to be opened again. */
class BrokenPiece(val file: String, val number: Int) : IOException("piece $number of $file is broken")

/**
 * What this phone keeps of the ZIPs it opened (docs/zip-plan.md), in Share's own storage, one folder
 * for each set: which parts were saved and where their files went, so a set's other parts follow,
 * and the pieces of cut files, each written at its offset into one file until the file is whole.
 * An ordinary ZIP is a set of one part, known by its names, sizes and CRCs. What wasn't touched for
 * two weeks goes by itself ([trim]).
 */
class ZipInbox(private val root: File) {

    /** A set as this phone knows it: [saved] parts, and where their files went: "phone", or "folder" with [folder]. */
    data class SetState(val name: String, val parts: Int, val saved: Set<Int>, val to: String?, val folder: String?, val entries: Set<String>)

    /** A cut file of a set: its name, type and length, and which of its [pieces] are in. */
    data class Cut(val file: String, val type: String, val taken: String, val total: Long, val pieces: Int, val have: Set<Int>, val data: File) {
        val whole: Boolean get() = have.size == pieces
    }

    @Synchronized
    fun set(id: String): SetState? {
        val o = read(File(dir(id), SET)) ?: return null
        return SetState(
            name = o.optString("name"),
            parts = o.optInt("parts", 1),
            saved = ints(o.optJSONArray("saved")),
            to = o.optString("to").ifEmpty { null },
            folder = o.optString("folder").ifEmpty { null },
            entries = strings(o.optJSONArray("entries")),
        )
    }

    /** Remembers that [part] of set [id] was opened, and when; its files go [to] (and into [folder]) once saving starts. */
    @Synchronized
    fun opened(id: String, name: String, parts: Int) {
        val file = File(dir(id), SET)
        val o = read(file) ?: JSONObject()
        write(file, o.put("name", name).put("parts", parts).put("seen", System.currentTimeMillis()))
    }

    /** Where set [id]'s files go from now on, for its other parts too. */
    @Synchronized
    fun goesTo(id: String, to: String, folder: String?) {
        val file = File(dir(id), SET)
        val o = read(file) ?: JSONObject()
        write(file, o.put("to", to).put("folder", folder ?: "").put("seen", System.currentTimeMillis()))
    }

    /** [entry] of set [id] is saved, or sent into a folder: it isn't saved twice. */
    @Synchronized
    fun savedEntry(id: String, entry: String) {
        val file = File(dir(id), SET)
        val o = read(file) ?: JSONObject()
        val entries = o.optJSONArray("entries") ?: JSONArray().also { o.put("entries", it) }
        if (entry !in strings(entries)) entries.put(entry)
        write(file, o.put("seen", System.currentTimeMillis()))
    }

    /** [part] of set [id] is saved whole. */
    @Synchronized
    fun savedPart(id: String, part: Int) {
        val file = File(dir(id), SET)
        val o = read(file) ?: JSONObject()
        val saved = o.optJSONArray("saved") ?: JSONArray().also { o.put("saved", it) }
        if (part !in ints(saved)) saved.put(part)
        write(file, o.put("seen", System.currentTimeMillis()))
    }

    /**
     * Writes [piece] of set [id], [length] bytes from [input], at its offset into its file's data, and
     * checks them against [crc]. A broken piece isn't counted ([BrokenPiece]); it can come again.
     */
    @Synchronized
    fun addPiece(id: String, piece: ShareJson.Piece, type: String, taken: String, input: InputStream, length: Long, crc: Int, progress: (Long) -> Unit = {}): Cut {
        val folder = dir(id).apply { mkdirs() }
        val key = key(piece.file)
        val meta = File(folder, "$key.json")
        val data = File(folder, "$key.part")
        val o = read(meta) ?: JSONObject().put("file", piece.file).put("type", type).put("taken", taken).put("total", piece.total).put("pieces", piece.pieces)
        if (o.getLong("total") != piece.total || o.getInt("pieces") != piece.pieces) throw IOException("${piece.file}: pieces of different sizes")
        val sum = CRC32()
        RandomAccessFile(data, "rw").use { out ->
            out.seek(piece.offset)
            val buf = ByteArray(1 shl 20)
            var left = length
            while (left > 0) {
                val n = input.read(buf, 0, minOf(buf.size.toLong(), left).toInt())
                if (n < 0) throw IOException("piece ${piece.number} of ${piece.file} ended $left bytes early")
                sum.update(buf, 0, n)
                out.write(buf, 0, n)
                left -= n
                progress(n.toLong())
            }
            out.fd.sync()
        }
        if (sum.value.toInt() != crc) throw BrokenPiece(piece.file, piece.number)
        val have = o.optJSONArray("have") ?: JSONArray().also { o.put("have", it) }
        if (piece.number !in ints(have)) have.put(piece.number)
        write(meta, o)
        return cut(o, data)
    }

    /** The cut files of set [id] that wait for pieces, or are whole and wait to go where the set's files go. */
    @Synchronized
    fun cuts(id: String): List<Cut> =
        dir(id).listFiles { f -> f.name.endsWith(".json") && f.name != SET }.orEmpty().sortedBy { it.name }.mapNotNull { meta ->
            read(meta)?.let { cut(it, File(meta.parentFile, meta.name.removeSuffix(".json") + ".part")) }
        }

    /** The whole file went where it belongs: its pieces go. */
    @Synchronized
    fun done(id: String, file: String) {
        val key = key(file)
        File(dir(id), "$key.json").delete()
        File(dir(id), "$key.part").delete()
    }

    /** Sets not touched for two weeks go, with their pieces. */
    @Synchronized
    fun trim(now: Long = System.currentTimeMillis()) {
        val old = now - KEEP_MS
        root.listFiles()?.forEach { set ->
            val seen = read(File(set, SET))?.optLong("seen") ?: 0L
            if (maxOf(seen, set.walkBottomUp().maxOf { it.lastModified() }) < old) set.deleteRecursively()
        }
    }

    private fun cut(o: JSONObject, data: File) = Cut(
        file = o.getString("file"),
        type = o.optString("type"),
        taken = o.optString("taken"),
        total = o.getLong("total"),
        pieces = o.getInt("pieces"),
        have = ints(o.optJSONArray("have")),
        data = data,
    )

    /** A set's folder: its id when that's a plain one, otherwise a hash of it. */
    private fun dir(id: String) = File(root, if (id.matches(Regex("[A-Za-z0-9_-]{1,64}"))) id else key(id))

    private fun read(file: File): JSONObject? = try {
        if (file.isFile) JSONObject(file.readText()) else null
    } catch (e: Exception) {
        null // a broken record counts as none
    }

    /** Writes [o] to [file] whole or not at all: into a new file first, then moved over the old one. */
    private fun write(file: File, o: JSONObject) {
        file.parentFile?.mkdirs()
        val tmp = File(file.parentFile, file.name + ".tmp")
        tmp.writeText(o.toString())
        if (!tmp.renameTo(file)) throw IOException("can't write ${file.name}")
    }

    companion object {
        private const val SET = "set.json"
        private const val KEEP_MS = 14 * 24 * 3600_000L

        private fun ints(a: JSONArray?): Set<Int> = if (a == null) emptySet() else (0 until a.length()).map { a.getInt(it) }.toSet()

        private fun strings(a: JSONArray?): Set<String> = if (a == null) emptySet() else (0 until a.length()).map { a.getString(it) }.toSet()

        /** A name's hash, for a file name that's safe anywhere. */
        fun key(name: String): String =
            MessageDigest.getInstance("SHA-256").digest(name.toByteArray(Charsets.UTF_8)).take(12).joinToString("") { "%02x".format(it) }
    }
}
