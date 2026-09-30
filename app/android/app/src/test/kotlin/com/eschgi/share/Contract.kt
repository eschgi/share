package com.eschgi.share

import org.json.JSONArray
import org.json.JSONObject
import java.io.File

/** A fixture from contract/ at the top of the repository; unit tests run in android/app. */
fun contract(name: String): JSONObject {
    var dir: File? = File("").absoluteFile
    while (dir != null && !File(dir, "contract").isDirectory) dir = dir.parentFile
    requireNotNull(dir) { "contract/ not found above ${File("").absolutePath}" }
    return JSONObject(File(dir, "contract/$name").readText())
}

fun JSONObject.toMap(): Map<String, Any?> = plain(this) as Map<String, Any?>

/** Ints and Longs compare equal: the channel's codec doesn't keep them apart either. */
@Suppress("UNCHECKED_CAST")
fun Map<String, Any?>.numbersAsLong(): Map<String, Any?> = plain(this) as Map<String, Any?>

/** JSON and Kotlin values as the same plain maps, lists and longs. */
private fun plain(v: Any?): Any? = when (v) {
    JSONObject.NULL, null -> null
    is JSONObject -> v.keys().asSequence().associateWith { plain(v.get(it)) }
    is JSONArray -> List(v.length()) { plain(v.get(it)) }
    is Map<*, *> -> v.entries.associate { (k, x) -> k as String to plain(x) }
    is List<*> -> v.map(::plain)
    is Int -> v.toLong()
    else -> v
}
