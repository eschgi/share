package com.eschgi.share.zip

import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale

/** A file to pack: its name, length, type, and when it was taken, in ms since 1970. */
data class PackFile(val name: String, val size: Long, val type: String, val taken: Long)

/** What every part's share.json says of the set: its id, name, the sentence for people without Share, when and by what it was made. */
data class PackSet(val id: String, val name: String, val about: String, val made: Long, val app: String, val zone: ZoneId)

/**
 * How files go into ZIPs of at most [limit] bytes each, or into one without a limit (docs/zip-plan.md).
 * Files go in the order they were taken, each into the first part with room for it, so a later photo
 * fills the gap a video left. Only a file too big for a part on its own is cut: its first piece fills
 * what's left of the last part, if that's at least a tenth of the limit, and the rest go into new
 * parts, each as full as allowed. Every part starts with its share.json. The sizes are exact: a part
 * is as big as its layout says, and never bigger than the limit.
 */
class ZipPlan private constructor(val files: List<PackFile>, val limit: Long?, val parts: List<Part>, val tooMany: Boolean) {

    /** A ZIP: its number, its share.json, the entries after it, and its exact layout. */
    class Part(val number: Int, val json: ShareJson, val jsonBytes: ByteArray, val entries: List<Entry>, val layout: ZipLayout) {
        val size: Long get() = layout.size

        /** Whole files in it. */
        val wholeFiles: Int get() = entries.count { it.piece == null }
    }

    /** [length] bytes of file [file] from [offset]: the whole file, or one of its pieces. */
    class Entry(val name: String, val file: Int, val offset: Long, val length: Long, val piece: ShareJson.Piece?)

    /** A file that was cut: its index, its name in the ZIPs, and the parts its pieces are in. */
    data class Cut(val file: Int, val name: String, val parts: List<Int>)

    /** The files that were cut, in the order of the parts. */
    val cut: List<Cut> by lazy {
        parts.flatMap { p -> p.entries.mapNotNull { e -> e.piece?.takeIf { it.number == 1 }?.let { Cut(e.file, it.file, it.parts) } } }
    }

    companion object {
        /** The most parts a plan may have; a choice that needs more can't be picked. */
        const val MAX_PARTS = 100

        /**
         * The plan for [files] with ZIPs of at most [limit] bytes, or one ZIP for null. With more than
         * [MAX_PARTS] parts it stops counting and says [tooMany], without parts.
         */
        fun make(files: List<PackFile>, limit: Long?, set: PackSet): ZipPlan {
            val order = files.indices.sortedWith(compareBy<Int> { files[it].taken }.thenBy { it })
            val taken = files.map { iso(it.taken, set.zone) }
            val made = iso(set.made, set.zone)
            val setBytes = files.sumOf { it.size }
            val cap = if (limit == null) Long.MAX_VALUE else limit - fixedCost(set, made, files.size, setBytes)
            // Files that will be cut are named first, so that no other file takes one of their pieces' names.
            val names = Names()
            val whole = arrayOfNulls<String>(files.size)
            // A margin leaves room for a " (2)" added to a name.
            val willCut = BooleanArray(files.size) { i ->
                val name = Names.clean(files[i].name)
                entryCost(name, files[i].size, ShareJson.File(name, files[i].type, files[i].size, taken[i])) > cap - NAME_MARGIN
            }
            for (i in order) if (willCut[i]) whole[i] = names.take(files[i].name, cut = true)
            for (i in order) if (!willCut[i]) whole[i] = names.take(files[i].name)

            val parts = ArrayList<Building>()
            fun newPart(): Building? {
                if (parts.size == MAX_PARTS) return null
                return Building(parts.size + 1).also { parts += it }
            }
            if (limit != null && cap <= MIN_ROOM) return ZipPlan(files, limit, emptyList(), tooMany = true)
            for (i in order) {
                val f = files[i]
                val name = whole[i]!!
                val cost = entryCost(name, f.size, ShareJson.File(name, f.type, f.size, taken[i]))
                if (!willCut[i]) {
                    val part = parts.firstOrNull { it.used + cost <= cap } ?: newPart() ?: return ZipPlan(files, limit, emptyList(), tooMany = true)
                    part.add(Entry(name, i, 0, f.size, null), cost)
                    continue
                }
                // Too big for a part on its own: cut, one piece after the other.
                val pieces = ArrayList<Pair<Building, Entry>>()
                var offset = 0L
                var target = parts.lastOrNull()?.takeIf { cap - it.used - pieceCost(name, 1, f.type, taken[i]) >= limit!! / 10 } ?: newPart()
                while (offset < f.size) {
                    val part = target ?: return ZipPlan(files, limit, emptyList(), tooMany = true)
                    val number = pieces.size + 1
                    val pieceName = names.pieceOf(name, number)
                    val cost0 = pieceCost(name, number, f.type, taken[i])
                    val length = minOf(cap - part.used - cost0, f.size - offset)
                    check(length > 0) { "no room for a piece of $name" }
                    val entry = Entry(pieceName, i, offset, length, null)
                    part.add(entry, cost0 + length)
                    pieces += part to entry
                    offset += length
                    if (offset < f.size) target = newPart()
                }
                // Now that it's known where they all went, each piece says so.
                val inParts = pieces.map { it.first.number }
                for ((n, pair) in pieces.withIndex()) {
                    val (part, entry) = pair
                    part.replace(entry, Entry(entry.name, i, entry.offset, entry.length, ShareJson.Piece(name, n + 1, pieces.size, entry.offset, f.size, inParts)))
                }
            }
            if (parts.isEmpty()) return ZipPlan(files, limit, emptyList(), tooMany = false)

            val built = parts.map { b ->
                val json = ShareJson(
                    about = set.about,
                    set = set.id,
                    name = set.name,
                    made = made,
                    app = set.app,
                    part = b.number,
                    parts = parts.size,
                    setFiles = files.size,
                    setBytes = setBytes,
                    files = b.entries.map { e -> ShareJson.File(e.name, files[e.file].type, e.length, taken[e.file], e.piece) },
                )
                val bytes = json.encode()
                val items = listOf(ZipItem(ShareJson.NAME, bytes.size.toLong(), set.made)) + b.entries.map { ZipItem(it.name, it.length, files[it.file].taken) }
                val layout = ZipLayout(items, set.zone)
                if (limit != null) check(layout.size <= limit) { "part ${b.number} has ${layout.size} bytes, more than $limit" }
                Part(b.number, json, bytes, b.entries.toList(), layout)
            }
            return ZipPlan(files, limit, built, tooMany = false)
        }

        /** When something was taken or made, as share.json says it: to the second, with the offset of [zone]. */
        fun iso(ms: Long, zone: ZoneId): String =
            OffsetDateTime.ofInstant(Instant.ofEpochMilli(ms), zone).truncatedTo(ChronoUnit.SECONDS).format(DateTimeFormatter.ISO_OFFSET_DATE_TIME)

        /** A part's room is never this small: a limit that leaves no more than this can't be used. */
        private const val MIN_ROOM = 64 * 1024L

        /** What a " (2)" and its like may add to a name's bytes, twice: in the headers and in share.json. */
        private const val NAME_MARGIN = 64L

        /** The headers a name costs in a ZIP: its local header and its central one, without Zip64, which a limit under 4 GiB never needs. */
        private fun headers(name: String): Long {
            val n = name.toByteArray(Charsets.UTF_8).size
            return ZipLayout.localLen(n, 0) + ZipLayout.centralLen(n, 0, 0)
        }

        /**
         * What a part costs before its entries: the end record, share.json's headers, and its text
         * without files, with three digits for the part numbers, as a set has at most [MAX_PARTS].
         * The closing of a non-empty file list, and a margin, come on top.
         */
        private fun fixedCost(set: PackSet, made: String, setFiles: Int, setBytes: Long): Long {
            val json = ShareJson(set.about, set.id, set.name, made, set.app, MAX_PARTS, MAX_PARTS, setFiles, setBytes, emptyList())
            return ZipLayout.endLen(0, 0, 0) + headers(ShareJson.NAME) + json.encode().size + 16
        }

        /** What a whole file costs in a part: its headers, its data, and its line in share.json with the comma. */
        private fun entryCost(name: String, size: Long, line: ShareJson.File): Long = headers(name) + size + lineBytes(line) + 2

        /**
         * What a piece costs besides its data, at most: its headers and its line in share.json with the
         * largest numbers it could have, and a list of [MAX_PARTS] parts.
         */
        private fun pieceCost(name: String, number: Int, type: String, taken: String): Long {
            val pieceName = Names.pieceName(name, number)
            val line = ShareJson.File(pieceName, type, Long.MAX_VALUE, taken, ShareJson.Piece(name, MAX_PARTS, MAX_PARTS, Long.MAX_VALUE, Long.MAX_VALUE, List(MAX_PARTS) { MAX_PARTS }))
            return headers(pieceName) + lineBytes(line) + 2
        }

        /** The bytes one file's line takes in share.json. */
        private fun lineBytes(f: ShareJson.File): Long {
            val one = ShareJson("", "", "", "", "", 1, 1, 0, 0, listOf(f)).encode().size
            val none = ShareJson("", "", "", "", "", 1, 1, 0, 0, emptyList()).encode().size
            return (one - none).toLong()
        }
    }

    /** A part while files go in. */
    private class Building(val number: Int) {
        val entries = ArrayList<Entry>()
        var used = 0L

        fun add(entry: Entry, cost: Long) {
            entries += entry
            used += cost
        }

        fun replace(old: Entry, new: Entry) {
            entries[entries.indexOf(old)] = new
        }
    }
}

/**
 * The names of a ZIP's entries: no folders, nothing file systems refuse, each name once whatever its
 * case, as Windows and macOS unpack them, and none called share.json but share.json itself. A later
 * file of the same name gets " (2)" before its extension. Pieces are called as 7-Zip expects:
 * VID_1.mp4.001, .002 and so on.
 */
internal class Names {
    private val taken = hashSetOf(key(ShareJson.NAME))
    private val cut = HashSet<String>()

    /** A unique, clean name for a file called [raw]; [cut] if it will be cut into pieces. */
    fun take(raw: String, cut: Boolean = false): String {
        val clean = clean(raw)
        val dot = clean.lastIndexOf('.').takeIf { it > 0 } ?: clean.length
        var name = clean
        var n = 2
        while (!free(name)) name = clean.substring(0, dot) + " (${n++})" + clean.substring(dot)
        taken += key(name)
        if (cut) this.cut += key(name)
        return name
    }

    /** Piece [number] of the cut file [whole], named here before. */
    fun pieceOf(whole: String, number: Int): String {
        val name = pieceName(whole, number)
        check(taken.add(key(name))) { "$name is taken" }
        return name
    }

    /** Not taken, and not like a piece of a file that will be cut: on a computer, 7-Zip would join it with that file. */
    private fun free(name: String) = key(name) !in taken && !(PIECE.matches(name) && key(name.substringBeforeLast('.')) in cut)

    companion object {
        private val PIECE = Regex(""".+\.\d{3,}""")

        fun pieceName(whole: String, number: Int) = "%s.%03d".format(Locale.ROOT, whole, number)

        private fun key(name: String) = name.lowercase(Locale.ROOT)

        /** Slashes, backslashes, control characters and what Windows refuses become "_"; dots and spaces at the ends go. */
        fun clean(raw: String): String {
            val s = raw.replace(Regex("""[/\\:*?"<>|\u0000-\u001f\u007f]"""), "_").trim().trim('.', ' ')
            val name = if (s.length <= 150) s else s.substring(0, 140) + s.substring(s.length - 10)
            return name.ifEmpty { "file" }
        }
    }
}
