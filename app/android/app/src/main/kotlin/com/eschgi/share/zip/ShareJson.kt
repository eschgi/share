package com.eschgi.share.zip

import org.json.JSONException
import org.json.JSONObject

/**
 * share.json, the first entry of every ZIP Share makes (docs/zip-plan.md): the set the ZIP belongs
 * to, which part it is, and what every other entry is, with the pieces of cut files. Share on the
 * other phone reads it; someone without Share finds [about].
 */
data class ShareJson(
    val about: String,
    val set: String,
    val name: String,
    val made: String,
    val app: String,
    val part: Int,
    val parts: Int,
    val setFiles: Int,
    val setBytes: Long,
    val files: List<File>,
) {
    /** An entry: its name in the ZIP, type, length, when it was taken, and the piece it is, if it is one. */
    data class File(val entry: String, val type: String, val size: Long, val taken: String, val piece: Piece? = null)

    /** Piece [number] of [pieces] of the cut file [file], [total] bytes long, at [offset]; [parts] hold its pieces. */
    data class Piece(val file: String, val number: Int, val pieces: Int, val offset: Long, val total: Long, val parts: List<Int>)

    /**
     * The text, always the same for the same values, so its length is part of the plan: two-space
     * indents and one line for each file, readable in an editor.
     */
    fun encode(): ByteArray = buildString {
        append("{\n")
        append("  \"share_zip\": ").append(VERSION).append(",\n")
        append("  \"about\": ").string(about).append(",\n")
        append("  \"set\": ").string(set).append(",\n")
        append("  \"name\": ").string(name).append(",\n")
        append("  \"made\": ").string(made).append(",\n")
        append("  \"app\": ").string(app).append(",\n")
        append("  \"part\": ").append(part).append(",\n")
        append("  \"parts\": ").append(parts).append(",\n")
        append("  \"set_files\": ").append(setFiles).append(",\n")
        append("  \"set_bytes\": ").append(setBytes).append(",\n")
        append("  \"files\": [")
        files.forEachIndexed { i, f ->
            append(if (i == 0) "\n" else ",\n")
            append("    {\"entry\": ").string(f.entry)
            append(", \"type\": ").string(f.type)
            append(", \"size\": ").append(f.size)
            append(", \"taken\": ").string(f.taken)
            f.piece?.let { p ->
                append(", \"piece\": {\"file\": ").string(p.file)
                append(", \"number\": ").append(p.number)
                append(", \"pieces\": ").append(p.pieces)
                append(", \"offset\": ").append(p.offset)
                append(", \"total\": ").append(p.total)
                append(", \"parts\": [").append(p.parts.joinToString(", ")).append("]}")
            }
            append("}")
        }
        append(if (files.isEmpty()) "]\n" else "\n  ]\n")
        append("}\n")
    }.toByteArray(Charsets.UTF_8)

    /**
     * Whether this is true of the ZIP it came in, whose other entries have the [sizes] given by
     * name: every entry listed once with its length, and every piece inside its file and its set.
     */
    fun matches(sizes: Map<String, Long>): Boolean {
        if (files.size != sizes.size || files.map { it.entry }.toSet() != sizes.keys) return false
        return files.all { f ->
            val p = f.piece
            sizes[f.entry] == f.size && (p == null || p.offset + f.size <= p.total && p.parts.all { it in 1..parts } && part in p.parts)
        }
    }

    /** What a ZIP's share.json turned out to be. */
    sealed interface Read

    /** Share's, of a version this app knows. */
    data class Ours(val json: ShareJson) : Read

    /** Share's, made by a newer version: the ZIP opens as an ordinary one, saying so. */
    data object Newer : Read

    /** Not Share's, or broken: the ZIP opens as an ordinary one, and this is just a file in it. */
    data object Other : Read

    companion object {
        const val NAME = "share.json"
        const val VERSION = 1

        /** Larger ones aren't Share's: a set has at most 100 parts, a part at most a few thousand entries. */
        const val MAX_BYTES = 4L shl 20

        /** Reads [bytes] strictly: a missing field, or a value out of range, makes it [Other]. Unknown fields are left alone. */
        fun read(bytes: ByteArray): Read = try {
            val o = JSONObject(String(bytes, Charsets.UTF_8))
            val version = o.getInt("share_zip")
            when {
                version > VERSION -> Newer
                version < VERSION -> Other
                else -> parse(o)?.let { Ours(it) } ?: Other
            }
        } catch (e: JSONException) {
            Other
        }

        private fun parse(o: JSONObject): ShareJson? {
            val part = o.getInt("part")
            val parts = o.getInt("parts")
            if (parts !in 1..MAX_PARTS || part !in 1..parts) return null
            val list = o.getJSONArray("files")
            val files = List(list.length()) { i ->
                val f = list.getJSONObject(i)
                val size = f.getLong("size")
                if (size < 0) return null
                File(f.getString("entry"), f.getString("type"), size, f.optString("taken"), f.optJSONObject("piece")?.let { piece(it) ?: return null })
            }
            val json = ShareJson(
                about = o.optString("about"),
                set = o.getString("set").takeIf { it.length in 1..64 } ?: return null,
                name = o.getString("name").takeIf { it.isNotBlank() && it.length <= 200 } ?: return null,
                made = o.optString("made"),
                app = o.optString("app"),
                part = part,
                parts = parts,
                setFiles = o.getInt("set_files").takeIf { it >= 0 } ?: return null,
                setBytes = o.getLong("set_bytes").takeIf { it >= 0 } ?: return null,
                files = files,
            )
            return json
        }

        private fun piece(p: JSONObject): Piece? {
            val pieces = p.getInt("pieces")
            val number = p.getInt("number")
            val offset = p.getLong("offset")
            val total = p.getLong("total")
            val list = p.getJSONArray("parts")
            val parts = List(list.length()) { list.getInt(it) }
            if (pieces !in 2..MAX_PARTS || number !in 1..pieces || offset < 0 || total <= 0 || offset >= total || parts.size != pieces) return null
            val file = p.getString("file").takeIf { it.isNotEmpty() && '/' !in it && '\\' !in it } ?: return null
            return Piece(file, number, pieces, offset, total, parts)
        }

        /** No set has more parts than this; the app makes at most 100. */
        private const val MAX_PARTS = 10_000

        private fun StringBuilder.string(s: String): StringBuilder {
            append('"')
            for (c in s) {
                when {
                    c == '"' -> append("\\\"")
                    c == '\\' -> append("\\\\")
                    c == '\n' -> append("\\n")
                    c == '\r' -> append("\\r")
                    c == '\t' -> append("\\t")
                    c < ' ' -> append("\\u").append(Integer.toHexString(c.code).padStart(4, '0'))
                    else -> append(c)
                }
            }
            return append('"')
        }
    }
}
