package com.eschgi.share.zip

/**
 * What the Dart side gets of ZIPs over the platform channel (contract/app/platform.json zip_*):
 * plain maps, made here so that PlatformContractTest can check them against the fixtures.
 */
object ZipEvents {
    /** A file waiting to be packed: [onPhone] unless it has to come from the server first. */
    fun file(f: PackFile, kind: String, onPhone: Boolean): Map<String, Any?> =
        mapOf("name" to f.name, "size" to f.size, "type" to f.type, "kind" to kind, "taken" to f.taken, "on_phone" to onPhone)

    /** What a choice of where it goes makes: the parts, with [names] once they are packed, and the files cut. */
    fun plan(plan: ZipPlan, names: List<String>? = null): Map<String, Any?> = mapOf(
        "too_many" to plan.tooMany,
        "parts" to plan.parts.map { p ->
            mapOf(
                "number" to p.number,
                "name" to names?.getOrNull(p.number - 1),
                "bytes" to p.size,
                "files" to p.wholeFiles,
                "pieces" to p.entries.mapNotNull { it.piece }.map { mapOf("file" to it.file, "number" to it.number, "pieces" to it.pieces) },
            )
        },
        "cut" to plan.cut.map { c -> mapOf("name" to c.name, "size" to plan.files[c.file].size, "parts" to c.parts) },
    )

    /** Packing is done: the parts, by the names their files have. */
    fun ready(plan: ZipPlan, names: List<String>): Map<String, Any?> = mapOf("type" to "zip", "state" to "ready") + plan(plan, names)

    /** How far packing is: files packed of all, bytes, which part, and the library file being fetched. */
    fun packing(filesDone: Int, files: Int, bytesDone: Long, bytesTotal: Long, part: Int, parts: Int, fetching: String?): Map<String, Any?> = mapOf(
        "type" to "zip", "state" to "packing", "files_done" to filesDone, "files" to files, "bytes_done" to bytesDone,
        "bytes_total" to bytesTotal, "part" to part, "parts" to parts, "fetching" to fetching,
    )

    /** Not enough room to pack: what packing needs, and what's free, both without the room the phone keeps. */
    fun noRoom(needed: Long, free: Long): Map<String, Any?> = mapOf("type" to "zip", "state" to "no_room", "needed" to needed, "free" to free)

    /** Packing failed: [reason] changed (with the [file] that changed), no_room or failed. */
    fun failed(reason: String, file: String? = null): Map<String, Any?> = mapOf("type" to "zip", "state" to "failed", "reason" to reason, "file" to file)

    fun stopped(): Map<String, Any?> = mapOf("type" to "zip", "state" to "stopped")

    /** An app took part [part], or all parts (0): its name, if Android says. */
    fun sent(part: Int, app: String?): Map<String, Any?> = mapOf("type" to "zip_sent", "part" to part, "app" to app)

    /** A file of the ZIPs open: [piece] for a piece of a cut file, with its file, number and pieces. */
    fun entry(index: Int, name: String, kind: String, size: Long, readable: Boolean, saved: Boolean, piece: ShareJson.Piece?): Map<String, Any?> = mapOf(
        "index" to index, "name" to name, "kind" to kind, "size" to size, "readable" to readable, "saved" to saved,
        "piece" to piece?.let { mapOf("file" to it.file, "number" to it.number, "pieces" to it.pieces) },
    )

    /** A cut file: the pieces this phone has, from before or in the ZIPs open, and the parts holding them. */
    fun join(file: String, kind: String, total: Long, pieces: Int, have: List<Int>, parts: List<Int>): Map<String, Any?> =
        mapOf("file" to file, "kind" to kind, "total" to total, "pieces" to pieces, "have" to have, "parts" to parts)

    /** A set as this phone knows it: its parts, the ones open now and the ones saved, and where its files went. */
    fun set(parts: Int, here: List<Int>, saved: List<Int>, to: String?, folder: String?): Map<String, Any?> =
        mapOf("parts" to parts, "here" to here, "saved" to saved, "to" to to, "folder" to folder)

    /** What the ZIPs open hold (zip.contents). [name] is null for ZIPs of several sets. */
    fun contents(
        name: String?,
        zips: Int,
        broken: Int,
        newer: Boolean,
        set: Map<String, Any?>?,
        fromWhatsapp: Boolean,
        bytes: Long,
        files: List<Map<String, Any?>>,
        joins: List<Map<String, Any?>>,
    ): Map<String, Any?> = mapOf(
        "name" to name, "zips" to zips, "broken" to broken, "newer" to newer, "set" to set,
        "from_whatsapp" to fromWhatsapp, "bytes" to bytes, "files" to files, "joins" to joins,
    )

    /** How far saving is. */
    fun saving(done: Int, total: Int, bytesDone: Long, bytesTotal: Long): Map<String, Any?> =
        mapOf("type" to "zip_save", "state" to "saving", "done" to done, "total" to total, "bytes_done" to bytesDone, "bytes_total" to bytesTotal)

    /** Saved: how many files, the cut files put back together, the broken ones, and where they went. */
    fun saved(saved: Int, joined: List<Map<String, Any?>>, broken: List<Map<String, Any?>>, to: String, folder: String?): Map<String, Any?> =
        mapOf("type" to "zip_save", "state" to "done", "saved" to saved, "joined" to joined, "broken" to broken, "to" to to, "folder" to folder)

    fun saveNoRoom(needed: Long, free: Long): Map<String, Any?> = mapOf("type" to "zip_save", "state" to "no_room", "needed" to needed, "free" to free)

    fun saveFailed(reason: String, saved: Int): Map<String, Any?> = mapOf("type" to "zip_save", "state" to "failed", "reason" to reason, "saved" to saved)
}
