package com.eschgi.share

import org.json.JSONObject
import java.io.File

/** A fixture from contract/ at the top of the repository; unit tests run in android/app. */
fun contract(name: String): JSONObject {
    var dir: File? = File("").absoluteFile
    while (dir != null && !File(dir, "contract").isDirectory) dir = dir.parentFile
    requireNotNull(dir) { "contract/ not found above ${File("").absolutePath}" }
    return JSONObject(File(dir, "contract/$name").readText())
}

fun JSONObject.toMap(): Map<String, Any?> = keys().asSequence().associateWith { k -> get(k).takeIf { it != JSONObject.NULL } }.numbersAsLong()

/** Ints and Longs compare equal: the channel's codec doesn't keep them apart either. */
fun Map<String, Any?>.numbersAsLong(): Map<String, Any?> = mapValues { (_, v) -> if (v is Int) v.toLong() else v }
