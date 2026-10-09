package com.eschgi.share.zip

import com.eschgi.share.contract
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** share.json as contract/app/share_zip.json has it: what the app writes, reads, and refuses. */
class ShareJsonTest {
    private val c = contract("app/share_zip.json")
    private val example get() = JSONObject(c.getJSONObject("example").toString())
    private val entries: Map<String, Long>
        get() = c.getJSONObject("entries").let { e -> e.keys().asSequence().associateWith { e.getLong(it) } }

    private fun read(o: JSONObject) = ShareJson.read(o.toString().toByteArray())

    /** The example, changed as a case of the contract says. */
    private fun changed(case: JSONObject): ByteArray {
        if (case.has("text")) return case.getString("text").toByteArray()
        val o = example
        case.optJSONObject("change")?.let { ch -> ch.keys().forEach { o.put(it, ch.get(it)) } }
        case.optJSONArray("remove")?.let { r -> for (i in 0 until r.length()) o.remove(r.getString(i)) }
        if (case.has("file")) {
            val f = o.getJSONArray("files").getJSONObject(case.getInt("file"))
            case.optJSONObject("change_file")?.let { ch -> ch.keys().forEach { f.put(it, ch.get(it)) } }
            case.optJSONArray("remove_file")?.let { r -> for (i in 0 until r.length()) f.remove(r.getString(i)) }
            case.optJSONObject("change_piece")?.let { ch -> ch.keys().forEach { f.getJSONObject("piece").put(it, ch.get(it)) } }
        }
        return o.toString().toByteArray()
    }

    @Test
    fun theExampleIsSharesAndMatchesItsZip() {
        val json = (read(example) as ShareJson.Ours).json
        assertEquals("1c6f3a62-5d0e-4c9b-9f7e-2b8f0f4d6a11", json.set)
        assertEquals("Dolomites 15–18 Oct 2026", json.name)
        assertEquals(3, json.part)
        assertEquals(4, json.parts)
        assertEquals(52, json.setFiles)
        assertEquals(7_012_345_678, json.setBytes)
        val piece = json.files[1].piece!!
        assertEquals(ShareJson.Piece("VID_20261017_141502.mp4", 1, 2, 0, 2_412_345_678, listOf(3, 4)), piece)
        assertTrue(json.matches(entries))
    }

    @Test
    fun whatTheAppWritesReadsBackTheSame() {
        val json = (read(example) as ShareJson.Ours).json
        val bytes = json.encode()
        assertEquals(json, (ShareJson.read(bytes) as ShareJson.Ours).json)
        // Two-space indents and one line for each file.
        val text = String(bytes)
        assertTrue(text.startsWith("{\n  \"share_zip\": 1,\n  \"about\": "))
        assertTrue(text.contains("\n    {\"entry\": \"IMG_20261017_101502.jpg\", \"type\": \"image/jpeg\", \"size\": 4123456, \"taken\": \"2026-10-17T10:15:02+02:00\"},\n"))
        assertTrue(text.endsWith("\n  ]\n}\n"))
    }

    @Test
    fun textThatNeedsEscapingStaysTheSame() {
        val json = ShareJson("a \"quote\", a \\ and a\ttab\n", "set-1", "Ötzi's \u0001 photos", "", "", 1, 1, 0, 0, emptyList())
        assertEquals(json, (ShareJson.read(json.encode()) as ShareJson.Ours).json)
        assertTrue(String(json.encode()).contains("\"files\": []\n}"))
    }

    @Test
    fun brokenOnesAreOrdinaryZips() {
        val broken = c.getJSONArray("broken")
        assertTrue(broken.length() > 10)
        for (i in 0 until broken.length()) {
            val case = broken.getJSONObject(i)
            assertEquals(case.getString("why"), ShareJson.Other, ShareJson.read(changed(case)))
        }
    }

    @Test
    fun mismatchedOnesDontMatchTheirZip() {
        val cases = c.getJSONArray("mismatched")
        for (i in 0 until cases.length()) {
            val case = cases.getJSONObject(i)
            val json = (ShareJson.read(changed(case)) as? ShareJson.Ours)?.json
            val sizes = case.optJSONObject("entries")?.let { e -> e.keys().asSequence().associateWith { e.getLong(it) } } ?: entries
            assertFalse(case.getString("why"), json == null || json.matches(sizes))
        }
    }

    @Test
    fun aNewerOneSaysSo() {
        assertEquals(ShareJson.Newer, ShareJson.read(c.getJSONObject("newer").toString().toByteArray()))
    }
}
