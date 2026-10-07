package com.eschgi.share.e2ee

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.FixMethodOrder
import org.junit.Test
import org.junit.runners.MethodSorters
import java.io.IOException

/**
 * The keyring's flows against a small fake of the server's keys API (docs/e2ee-plan.md), as
 * the website's test does (web/test/keyring.test.ts): who holds what, what is on whose to-do
 * list, and what each phone can open after its turn. The steps build on each other.
 */
@FixMethodOrder(MethodSorters.NAME_ASCENDING)
class KeyringTest {
    private class Person(val admin: Boolean, val folders: MutableSet<String>) {
        var publicKey: String? = null
        val sealed = HashMap<String, String>() // the person's key, by device
        var lock: String? = null
    }

    private class Pin(val folder: String, val secretSealed: String, val secretVersion: Int, val locked: MutableMap<Int, String>)

    /** A check: the device that asks, the device or person it is for, and what it relays. */
    private class Check(val asker: String, val device: String?, val user: String?, var commitment: String) {
        var answer: String? = null
        var answeredBy: String? = null
        var reveal: String? = null
    }

    /** The server: what it keeps, and who asks. */
    private object Fake {
        var caller: Pair<String, String>? = null // user, device
        var pinCaller: String? = null
        val devices = HashMap<String, Pair<String, String?>>() // device -> user, public key
        val people = HashMap<String, Person>()
        val folders = HashMap<String, MutableList<String>>() // folder -> public keys by version - 1
        val encrypted = HashSet<String>()
        val sealed = HashMap<String, String>() // folder:version:user
        var recovery: Pair<String, String>? = null
        val recoverySealed = HashMap<String, String>() // folder:version
        val pins = HashMap<String, Pin>()
        val rekey = HashSet<String>()
        var offline = false // no answer at all
        val checks = LinkedHashMap<String, Check>()
        var nextCheck = 0
        val swapped = HashMap<String, String>() // a server that names another key for a device that waits
        val away = HashSet<String>() // devices not seen lately, which can't answer a check
    }

    private fun conflict(code: String) = KeysApiError(409, code, code)

    private val api = KeysApi { method, path, body -> serve(method, path, body) }

    private val pinApi = KeysApi { method, path, _ ->
        require(method == "GET" && path == "/api/pin/keys")
        val p = Fake.pins[Fake.pinCaller] ?: throw KeysApiError(401, "unauthorized", "")
        val keys = JSONArray()
        p.locked.forEach { (v, locked) -> keys.put(JSONObject().put("version", v).put("public_key", Fake.folders[p.folder]!![v - 1]).put("locked", locked)) }
        JSONObject().put("folder", p.folder).put("keys", keys)
    }

    private fun sees(p: Person, folder: String) = p.admin || folder in p.folders

    private fun versions() = Fake.folders.flatMap { (f, keys) -> keys.mapIndexed { i, k -> Triple(f, i + 1, k) } }

    private fun serve(method: String, path: String, body: JSONObject?): JSONObject {
        if (Fake.offline) throw IOException("no answer")
        val (user, device) = Fake.caller ?: throw KeysApiError(401, "unauthorized", "")
        val p = Fake.people[user]!!
        fun held(folder: String, version: Int, who: String = user) = Fake.sealed.containsKey("$folder:$version:$who")
        when ("$method $path") {
            "GET /api/keys" -> {
                val mine = versions().filter { sees(p, it.first) }
                val folders = JSONArray()
                mine.forEach { (f, v, k) -> folders.put(JSONObject().put("folder", f).put("version", v).put("public_key", k).put("sealed", Fake.sealed["$f:$v:$user"] ?: JSONObject.NULL)) }
                val todoDevices = JSONArray()
                if (p.publicKey != null) {
                    Fake.devices.filter { (id, d) -> d.first == user && d.second != null && id !in p.sealed }.forEach { (id, d) ->
                        todoDevices.put(
                            JSONObject().put("id", id).put("public_key", Fake.swapped[id] ?: d.second).put("name", id).put("client", "app")
                                .put("created_at", "2026-10-06T10:00:00Z").put("active", id !in Fake.away),
                        )
                    }
                }
                val todoPeople = JSONArray() // only admins pass folder keys on to other people
                for ((f, v, _) in mine.filter { p.admin && held(it.first, it.second) }) {
                    Fake.people.filter { (u, q) -> u != user && q.publicKey != null && sees(q, f) && !held(f, v, u) }
                        .forEach { (u, q) ->
                            val active = q.sealed.keys.any { Fake.devices.containsKey(it) && it !in Fake.away }
                            todoPeople.put(JSONObject().put("folder", f).put("version", v).put("user", u).put("name", u).put("public_key", q.publicKey).put("active", active))
                        }
                }
                val todoRecovery = JSONArray()
                if (p.admin && Fake.recovery != null) {
                    mine.filter { held(it.first, it.second) && !Fake.recoverySealed.containsKey("${it.first}:${it.second}") }
                        .forEach { (f, v, _) -> todoRecovery.put(JSONObject().put("folder", f).put("version", v)) }
                }
                val todoPins = JSONArray()
                for ((id, pin) in Fake.pins) {
                    for (v in 1..Fake.folders[pin.folder]!!.size) {
                        if (v !in pin.locked && held(pin.folder, v) && held(pin.folder, pin.secretVersion)) {
                            todoPins.put(JSONObject().put("pin", id).put("folder", pin.folder).put("version", v).put("secret_version", pin.secretVersion).put("secret_sealed", pin.secretSealed))
                        }
                    }
                }
                val todo = JSONObject().put("devices", todoDevices).put("people", todoPeople).put("recovery", todoRecovery)
                    .put("rekey", JSONArray(if (p.admin) Fake.rekey.toList() else emptyList())).put("pins", todoPins)
                val checks = JSONArray()
                for ((id, c) in Fake.checks) {
                    if (c.asker != device && c.device != device && !(c.user == user && (c.answeredBy == null || c.answeredBy == device) && device in p.sealed)) continue
                    checks.put(
                        JSONObject().put("id", id).put("asking", c.asker == device).put("device", c.device ?: JSONObject.NULL).put("user", c.user ?: JSONObject.NULL)
                            .put("from", c.asker).put("commitment", c.commitment).put("answer", c.answer ?: JSONObject.NULL)
                            .put("answered", c.answeredBy == device).put("reveal", c.reveal ?: JSONObject.NULL),
                    )
                }
                return JSONObject()
                    .put("checks", checks)
                    .put("device_key", Fake.devices[device]!!.second ?: JSONObject.NULL)
                    .put("person", JSONObject().put("public_key", p.publicKey ?: JSONObject.NULL).put("sealed", p.sealed[device] ?: JSONObject.NULL).put("password_lock", p.lock ?: JSONObject.NULL).put("held_by", p.sealed.keys.count { Fake.devices.containsKey(it) }))
                    .put("folders", folders)
                    .put("recovery_key", if (p.admin) Fake.recovery?.first ?: JSONObject.NULL else JSONObject.NULL)
                    .put("todo", todo)
            }
            "PUT /api/keys/device" -> {
                Fake.devices[device] = user to body!!.getString("public_key")
                p.sealed.remove(device)
            }
            "PUT /api/keys/person" -> {
                val startOver = body!!.optBoolean("start_over")
                if (p.publicKey != null && !startOver) throw conflict("key_exists")
                if (startOver) {
                    p.sealed.clear()
                    p.lock = null
                    Fake.sealed.keys.removeIf { it.endsWith(":$user") }
                }
                p.publicKey = body.getString("public_key")
                p.sealed[device] = body.getString("sealed")
            }
            "PUT /api/keys/password-lock" -> {
                if (p.publicKey == null) throw conflict("no_key")
                p.lock = body!!.getString("password_lock")
            }
            "POST /api/keys/grants" -> {
                val g = body!!
                g.getJSONArray("devices").objects().forEach {
                    require(Fake.devices[it.getString("device")]?.first == user) { "not their device" }
                    p.sealed[it.getString("device")] = it.getString("sealed")
                }
                // A member's keys for someone else are left out.
                g.getJSONArray("people").objects().filter { it.getString("user") == user || p.admin }
                    .forEach { Fake.sealed["${it.getString("folder")}:${it.getInt("version")}:${it.getString("user")}"] = it.getString("sealed") }
                g.getJSONArray("recovery").objects().forEach { Fake.recoverySealed["${it.getString("folder")}:${it.getInt("version")}"] = it.getString("sealed") }
                g.getJSONArray("pins").objects().forEach { Fake.pins[it.getString("pin")]!!.locked[it.getInt("version")] = it.getString("locked") }
            }
            "POST /api/keys/checks" -> {
                val target = body!!.optString("device").ifEmpty { null }
                val other = body.optString("user").ifEmpty { null }
                if (other != null && !p.admin) throw KeysApiError(403, "forbidden", "only admins pass folder keys on to other people")
                val d = target?.let { Fake.devices[it] }
                val ok = device in p.sealed && if (target != null) d != null && d.first == user && d.second != null && target !in p.sealed else other != user && Fake.people[other]?.publicKey != null
                if (!ok) throw KeysApiError(404, "not_found", "nobody waits there")
                Fake.checks.entries.removeIf { it.value.asker == device && it.value.device == target && it.value.user == other }
                val id = "check${++Fake.nextCheck}"
                Fake.checks[id] = Check(device, target, other, body.getString("commitment"))
                return JSONObject().put("id", id)
            }
            "PUT /api/recovery" -> {
                Fake.recovery = body!!.getString("public_key") to body.getString("locked")
                Fake.recoverySealed.clear()
            }
            "GET /api/recovery" -> {
                val folders = JSONArray()
                Fake.recoverySealed.forEach { (k, sealed) ->
                    val (f, v) = k.split(':')
                    folders.put(JSONObject().put("folder", f).put("version", v.toInt()).put("public_key", Fake.folders[f]!![v.toInt() - 1]).put("sealed", sealed))
                }
                return JSONObject().put("public_key", Fake.recovery?.first ?: JSONObject.NULL).put("locked", Fake.recovery?.second ?: JSONObject.NULL).put("folders", folders)
            }
            else -> {
                Regex("/api/keys/checks/([^/]+)(/answer|/reveal)?").matchEntire(path)?.let { m ->
                    val c = Fake.checks[m.groupValues[1]] ?: throw KeysApiError(404, "not_found", "no such check")
                    when ("$method ${m.groupValues[2]}") {
                        "PUT /answer" -> {
                            if (c.reveal != null || !(c.device == device || (c.user == user && (c.answeredBy == null || c.answeredBy == device) && device in p.sealed))) {
                                throw KeysApiError(404, "not_found", "no such check")
                            }
                            c.answer = body!!.getString("nonce")
                            c.answeredBy = device
                        }
                        "PUT /reveal" -> {
                            if (c.asker != device) throw KeysApiError(404, "not_found", "no such check")
                            if (c.answer != body!!.getString("answer") || c.reveal != null) throw KeysApiError(409, "not_answered", "nothing to reveal for")
                            val nonce = body.getString("nonce")
                            require(E2ee.b64u(E2ee.commitment(E2ee.fromB64u(nonce))) == c.commitment) { "not the nonce committed to" }
                            c.reveal = nonce
                        }
                        "DELETE " -> Fake.checks.remove(m.groupValues[1])
                        else -> error("no $method $path")
                    }
                    return JSONObject()
                }
                Regex("/api/devices/([^/]+)").matchEntire(path)?.let { m ->
                    require(method == "DELETE")
                    val id = m.groupValues[1]
                    if (Fake.devices[id]?.first != user) throw KeysApiError(404, "not_found", "not theirs")
                    Fake.devices.remove(id)
                    p.sealed.remove(id)
                    Fake.checks.entries.removeIf { it.value.device == id || it.value.asker == id }
                    return JSONObject()
                }
                val m = Regex("/api/folders/([^/]+)/(encryption|keys)").matchEntire(path) ?: error("no $method $path")
                val folder = m.groupValues[1]
                val keys = Fake.folders.getValue(folder)
                if (m.groupValues[2] == "encryption") {
                    val on = body!!.getBoolean("encrypted")
                    if (on && keys.isEmpty()) {
                        val key = body.optJSONObject("key") ?: throw conflict("no_key")
                        keys += key.getString("public_key")
                        Fake.sealed["$folder:1:$user"] = key.getString("sealed")
                        Fake.recoverySealed["$folder:1"] = key.getString("recovery_sealed")
                    }
                    if (on) Fake.encrypted += folder else Fake.encrypted -= folder
                    return JSONObject().put("id", folder).put("encrypted", on).put("key_version", if (keys.isEmpty()) JSONObject.NULL else keys.size)
                }
                val version = body!!.getInt("version")
                if (version != keys.size + 1) throw conflict("key_outdated")
                keys += body.getString("public_key")
                Fake.sealed["$folder:$version:$user"] = body.getString("sealed")
                Fake.recoverySealed["$folder:$version"] = body.getString("recovery_sealed")
                Fake.rekey -= folder
            }
        }
        return JSONObject()
    }

    /** A phone's keyring, with its own device key store. */
    private class Phone(val account: Account, val ring: Keyring)

    private fun phone(user: String, device: String): Phone {
        Fake.devices[device] = user to null
        val keys = HashMap<String, KeyPair>()
        val checked = HashMap<String, Map<String, String>>()
        val store = object : DeviceKeyStore {
            override fun load(deviceId: String) = keys[deviceId]

            override fun save(deviceId: String, pair: KeyPair) {
                keys[deviceId] = pair
            }

            override fun trusted(deviceId: String) = checked[deviceId] ?: emptyMap()

            override fun trust(deviceId: String, trusted: Map<String, String>) {
                checked[deviceId] = trusted
            }
        }
        return Phone(Account(user, device, admin = Fake.people[user]!!.admin), Keyring(api, store))
    }

    private fun Phone.sync(password: String? = null) {
        Fake.caller = account.userId to account.deviceId
        ring.sync(account, password)
    }

    /** A check-in, as the app makes every half minute while it is in front. */
    private fun Phone.checkIn() {
        Fake.caller = account.userId to account.deviceId
        ring.sync(account, quiet = true)
    }

    private fun <T> Phone.call(f: Keyring.() -> T): T {
        Fake.caller = account.userId to account.deviceId
        return ring.f()
    }

    /** A check to its end: the asking phone lists the ask, the person opens it with Show, which opens the check, the other side answers, the asking phone reveals its nonce, both show the same code, and the person at the asking phone allows it. */
    private fun approve(asker: Phone, other: Phone, kind: String, id: String) {
        asker.checkIn()
        asker.call { show(kind, id) }
        for (p in listOf(other, asker, other)) p.checkIn()
        val ask = asker.ring.asks.first { it.kind == kind && it.id == id }
        assertTrue("a code: $ask", ask.code?.matches(Regex("\\d{6}")) == true)
        assertTrue(other.ring.codes.any { it.code == ask.code })
        asker.call { allow(kind, id) }
        other.checkIn()
    }

    private fun file(folder: String, seal: Pair<ByteArray, JSONObject>) =
        SealedFile("file1", folder, seal.second.getInt("version"), seal.second.getString("key"), seal.second.getString("header"), 1000)

    companion object {
        init {
            Fake.folders["f1"] = mutableListOf()
            Fake.folders["f2"] = mutableListOf()
            Fake.folders["plain"] = mutableListOf()
            Fake.people["ada"] = Person(true, HashSet())
            Fake.people["max"] = Person(false, hashSetOf("f1"))
            Fake.people["eve"] = Person(false, hashSetOf("f1"))
            Fake.people["zed"] = Person(true, HashSet())
        }

        private var a1: Phone? = null
        private var code = ""
        private var fileKey = ByteArray(0)
        private var seal: Pair<ByteArray, JSONObject>? = null
    }

    /** A file key sealed for the folder's newest key, as an upload makes it, with the API's enc. */
    private fun sealFor(target: FolderPublicKey): Pair<ByteArray, JSONObject> {
        val key = E2ee.newFileKey()
        val sealed = E2ee.sealKey(target.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext(target.folder, target.version), key)
        return key to JSONObject().put("version", target.version).put("key", E2ee.b64u(sealed)).put("header", E2ee.b64u(E2ee.newHeader())).put("plain_size", 1000)
    }

    @Test
    fun a_firstPhoneMakesThePersonKey() {
        val a = phone("ada", "a1").also { a1 = it }
        a.sync()
        assertEquals(Keyring.Status.READY, a.ring.status)
        assertNotNull(Fake.people["ada"]!!.publicKey)
        assertTrue("a1" in Fake.people["ada"]!!.sealed)
        assertFalse(a.ring.hasRecovery())

        // Nobody is asked about while no folder is encrypted.
        phone("ada", "a0").sync()
        a.checkIn()
        assertEquals(emptyList<Ask>(), a.ring.asks)
        Fake.devices.remove("a0")
    }

    @Test
    fun b_recoveryKeyThenFolderKeyWhichSealsFiles() {
        val a = a1!!
        assertThrows(E2eeException::class.java) { a.call { encryptFolder("f1", null) } }
        code = a.call { makeRecovery() }
        assertTrue(Regex("([0-9A-Z]{4}-){7}[0-9A-Z]{4}").matches(code))
        assertTrue(a.ring.hasRecovery())
        a.call { encryptFolder("f1", null) }
        assertEquals(1, Fake.folders["f1"]!!.size)
        assertTrue(Fake.recoverySealed.containsKey("f1:1"))
        assertTrue(a.ring.hasFolderKey("f1", 1))
        val target = a.ring.encryptFor("f1")!!
        assertEquals(1, target.version)
        assertNull(a.ring.encryptFor("plain"))
        seal = sealFor(target)
        fileKey = seal!!.first
        assertArrayEquals(fileKey, a.ring.fileKey(file("f1", seal!!)))
    }

    @Test
    fun c_anotherPhoneWaitsUntilOneWithTheKeyPassesItOnAfterACheck() {
        val a2 = phone("ada", "a2")
        a2.sync()
        assertEquals(Keyring.Status.WAITING, a2.ring.status)
        assertThrows(E2eeException::class.java) { a2.ring.fileKey(file("f1", seal!!)) }

        // The phone with the key lists it, and opens nothing by itself: no check before Show, and
        // nothing sealed until the person compared the codes.
        val a = a1!!
        a.checkIn()
        assertEquals(listOf(Ask("device", "a2", "a2", "app", "2026-10-06T10:00:00Z")), a.ring.asks)
        assertTrue(Fake.checks.isEmpty())
        assertEquals(30_000L, a.ring.pace())
        assertThrows(E2eeException::class.java) { a.call { allow("device", "a2") } }
        a2.checkIn()
        assertEquals(emptyList<ShownCode>(), a2.ring.codes)
        assertFalse("a2" in Fake.people["ada"]!!.sealed)

        approve(a, a2, "device", "a2")
        assertEquals(Keyring.Status.READY, a2.ring.status)
        assertArrayEquals(fileKey, a2.ring.fileKey(file("f1", seal!!)))
        assertEquals(emptyList<Ask>(), a.ring.asks)
    }

    @Test
    fun ca_checkInsPassTheKeyOnToAWaitingPhoneWhichOpensItWithoutLoading() {
        val aw = phone("ada", "aw")
        aw.sync()
        assertEquals(Keyring.Status.WAITING, aw.ring.status)
        val seen = ArrayList<Keyring.Status>()
        aw.ring.onChange = { seen += aw.ring.status }
        approve(a1!!, aw, "device", "aw")
        assertEquals(Keyring.Status.READY, aw.ring.status)
        assertFalse("a check-in never shows loading: $seen", Keyring.Status.LOADING in seen)
        assertArrayEquals(fileKey, aw.ring.fileKey(file("f1", seal!!)))

        // Without an answer, a check-in keeps what is open, and the status.
        Fake.offline = true
        try {
            assertThrows(IOException::class.java) { aw.checkIn() }
        } finally {
            Fake.offline = false
        }
        assertEquals(Keyring.Status.READY, aw.ring.status)
        assertArrayEquals(fileKey, aw.ring.fileKey(file("f1", seal!!)))
    }

    @Test
    fun cb_notMeSignsAPhoneOutAndASwappedKeyShowsTwoCodes() {
        val a = a1!!
        phone("ada", "x1").sync()
        a.checkIn()
        a.call { deny("device", "x1") }
        assertFalse(Fake.devices.containsKey("x1"))
        a.checkIn()
        assertTrue(a.ring.asks.none { it.id == "x1" })

        // A server that names its own key for a phone that waits can't make the codes match.
        val y1 = phone("ada", "y1")
        y1.sync()
        Fake.swapped["y1"] = Fake.people["ada"]!!.publicKey!!
        a.checkIn()
        a.call { show("device", "y1") }
        for (p in listOf(y1, a, y1)) p.checkIn()
        val ask = a.ring.asks.first { it.id == "y1" }
        assertNotNull(ask.code)
        assertEquals(1, y1.ring.codes.size)
        assertNotEquals(ask.code, y1.ring.codes[0].code)
        Fake.swapped.remove("y1")
        a.call { deny("device", "y1") }
    }

    @Test
    fun cc_listsOnlyPhonesSeenLatelyAndChecksInSoonerWhileACheckRuns() {
        val a = a1!!
        val az = phone("ada", "az")
        az.sync()
        assertEquals(5_000L, az.ring.pace())
        Fake.away += "az"
        a.checkIn()
        assertEquals(emptyList<Ask>(), a.ring.asks)
        assertEquals(30_000L, a.ring.pace())

        Fake.away -= "az"
        a.checkIn()
        assertEquals(listOf("az"), a.ring.asks.map { it.id })
        assertEquals(30_000L, a.ring.pace())
        a.call { show("device", "az") }
        assertEquals(2_000L, a.ring.pace())
        az.checkIn()
        assertEquals(2_000L, az.ring.pace())

        // Not now: the check ends, on both sides, and the ask stays listed.
        a.call { hide("device", "az") }
        assertEquals(30_000L, a.ring.pace())
        assertTrue(Fake.checks.values.none { it.device == "az" })
        a.checkIn()
        assertEquals(listOf("az"), a.ring.asks.map { it.id })
        az.checkIn()
        assertEquals(5_000L, az.ring.pace())
        a.call { deny("device", "az") }
        assertEquals(emptyList<Ask>(), a.ring.asks)
    }

    @Test
    fun cd_aServerCantFitTheCodesByChangingWhatItRelaysAfterwards() {
        val a = a1!!
        val at = phone("ada", "at")
        at.sync()
        a.checkIn()
        a.call { show("device", "at") }
        at.checkIn()
        val (id, x) = Fake.checks.entries.first { it.value.device == "at" }.toPair()
        // A commitment and a nonce of the server's own, after the phone answered: no code.
        val own = ByteArray(32) { 7 }
        val real = x.commitment
        x.commitment = E2ee.b64u(E2ee.commitment(own))
        x.reveal = E2ee.b64u(own)
        at.checkIn()
        assertEquals(emptyList<ShownCode>(), at.ring.codes)

        // The asking phone's code stays the one made from the answer it revealed for.
        x.commitment = real
        x.reveal = null
        at.checkIn() // the phone has its nonce: it doesn't answer again
        a.checkIn()
        val code = a.ring.asks.first { it.id == "at" }.code!!
        assertNotNull(Fake.checks[id]!!.reveal)
        Fake.checks[id]!!.answer = E2ee.b64u(ByteArray(32) { 9 })
        a.checkIn()
        assertEquals(code, a.ring.asks.first { it.id == "at" }.code)
        at.checkIn()
        assertEquals(listOf(ShownCode("device", "a1", code)), at.ring.codes)
        a.call { deny("device", "at") }
    }

    @Test
    fun d_thePasswordOpensTheKeysOnANewPhone() {
        a1!!.sync("correct horse battery")
        assertNotNull(Fake.people["ada"]!!.lock)
        val wrong = phone("ada", "a3")
        wrong.sync("wrong horse battery")
        assertEquals(Keyring.Status.WAITING, wrong.ring.status)
        val a4 = phone("ada", "a4")
        a4.sync("correct horse battery")
        assertEquals(Keyring.Status.READY, a4.ring.status)
        assertTrue(a4.ring.hasFolderKey("f1", 1))
        assertTrue("a4" in Fake.people["ada"]!!.sealed)
        // It doesn't ask about itself, although the list it got before the password opened the key named it.
        assertEquals(listOf("a3"), a4.ring.asks.map { it.id })
        // The phone with the wrong password waits for a check like any new one.
        approve(a4, wrong, "device", "a3")
        assertEquals(Keyring.Status.READY, wrong.ring.status)
        // A new password, locked anew.
        val lock = a4.call { passwordLock("staple") }
        assertArrayEquals(Hpke.publicKey(E2ee.passwordUnlock("staple", E2ee.personContext("ada"), lock)), E2ee.fromB64u(Fake.people["ada"]!!.publicKey!!))
    }

    @Test
    fun e_anAdminSealsTheFolderKeyForSomeoneWhoSeesItAfterACheck() {
        val m1 = phone("max", "m1")
        m1.sync()
        assertEquals(Keyring.Status.READY, m1.ring.status)
        assertFalse(m1.ring.hasFolderKey("f1", 1))
        assertTrue(m1.ring.waitsForFolders())
        a1!!.checkIn()
        assertEquals(listOf(Ask("person", "max", "max", folders = listOf("f1"))), a1!!.ring.asks)
        assertFalse(a1!!.ring.keyChanged(a1!!.ring.asks[0]))
        approve(a1!!, m1, "person", "max")
        assertArrayEquals(fileKey, m1.ring.fileKey(file("f1", seal!!)))
        assertFalse(m1.ring.waitsForFolders())
        a1!!.call { encryptFolder("f2", null) }
        m1.sync()
        assertNull(m1.ring.encryptFor("f2"))
    }

    @Test
    fun ea_aPasswordTypedWhileWaitingLocksTheKeyOnceItArrives() {
        val e1 = phone("eve", "e1")
        e1.sync() // her first phone makes her key, without a password
        approve(a1!!, e1, "person", "eve") // the admin seals f1 for her
        assertTrue(e1.ring.hasFolderKey("f1", 1))
        assertNull(Fake.people["eve"]!!.lock)

        // A password from an admin opens no lock: another phone waits, until hers seals it the key.
        val e2 = phone("eve", "e2")
        e2.sync("eve password")
        assertEquals(Keyring.Status.WAITING, e2.ring.status)
        approve(e1, e2, "device", "e2")
        assertEquals(Keyring.Status.READY, e2.ring.status)
        assertNotNull(Fake.people["eve"]!!.lock)

        // So the next phone opens it with the password at once.
        val e3 = phone("eve", "e3")
        e3.sync("eve password")
        assertEquals(Keyring.Status.READY, e3.ring.status)
        assertTrue(e3.ring.hasFolderKey("f1", 1))
    }

    @Test
    fun eb_aMembersLostKeyIsReplacedByItselfAndAnAdminSealsTheFoldersForIt() {
        // Every phone of hers is signed out, and an admin gives her a new password: the lock goes.
        val eve = Fake.people["eve"]!!
        for (d in listOf("e1", "e2", "e3")) {
            Fake.devices.remove(d)
            eve.sealed.remove(d)
        }
        eve.lock = null
        val e4 = phone("eve", "e4")
        e4.sync("new eve password")
        assertEquals(Keyring.Status.READY, e4.ring.status)
        assertFalse(e4.ring.hasFolderKey("f1", 1))
        assertNotNull(eve.lock)
        // Her key is new, so the admin's phone checks it again.
        a1!!.checkIn()
        assertTrue(a1!!.ring.keyChanged(a1!!.ring.asks.first { it.kind == "person" && it.id == "eve" }))
        approve(a1!!, e4, "person", "eve")
        assertArrayEquals(fileKey, e4.ring.fileKey(file("f1", seal!!)))

        // An admin's phone asks instead, as the recovery code opens every folder again.
        val z1 = phone("zed", "z1")
        z1.sync()
        Fake.devices.remove("z1")
        Fake.people["zed"]!!.sealed.remove("z1")
        val z2 = phone("zed", "z2")
        z2.sync("zed password")
        assertEquals(Keyring.Status.WAITING, z2.ring.status)
    }

    @Test
    fun f_movesKeysToEncryptedFoldersOnly() {
        val a = a1!!
        val f = JSONObject().put("id", "file1").put("folder", "f1").put("enc", seal!!.second)
        val moved = a.call { moveKeys(listOf(f), "f2") }.getJSONObject(0)
        assertEquals(1, moved.getInt("version"))
        val there = SealedFile("file1", "f2", 1, moved.getString("key"), seal!!.second.getString("header"), 1000)
        assertArrayEquals(fileKey, a.ring.fileKey(there))
        assertThrows(E2eeException::class.java) { a.call { moveKeys(listOf(f), "plain") } }
        assertEquals(0, a.call { moveKeys(listOf(JSONObject().put("id", "x").put("folder", "f1")), "plain") }.length())
    }

    @Test
    fun g_anInviteLinksSecretBringsTheFolderKeys() {
        val (secret, locked) = a1!!.call { inviteKeys(listOf("f1")) }
        val keys = locked.objects().map { it.put("public_key", Fake.folders[it.getString("folder")]!![it.getInt("version") - 1]) }
        assertEquals(listOf("f1"), keys.map { it.getString("folder") })
        Fake.people["eva"] = Person(false, hashSetOf("f1"))
        val e1 = phone("eva", "e1")
        e1.call { fromInvite(e1.account, secret, keys) }
        e1.sync()
        assertArrayEquals(fileKey, e1.ring.fileKey(file("f1", seal!!)))
        // A link with another secret: the phone waits for the family.
        Fake.people["leo"] = Person(false, hashSetOf("f1"))
        val l1 = phone("leo", "l1")
        val other = a1!!.ring.inviteKeys(listOf("f2")).first
        l1.call { fromInvite(l1.account, other, keys) }
        l1.sync()
        assertEquals(Keyring.Status.READY, l1.ring.status)
        assertFalse(l1.ring.hasFolderKey("f1", 1))

        // A member who holds f1 doesn't list Leo: only admins pass folder keys on to other people.
        e1.checkIn()
        assertTrue(e1.ring.hasFolderKey("f1", 1))
        assertEquals(emptyList<Ask>(), e1.ring.asks)

        // Leo is listed; Show opens his check, and Not now ends it, while he stays listed.
        val a = a1!!
        a.checkIn()
        assertTrue(Fake.checks.values.none { it.user == "leo" })
        a.call { show("person", "leo") }
        assertTrue(Fake.checks.values.any { it.user == "leo" })
        a.call { hide("person", "leo") }
        a.checkIn()
        assertTrue(a.ring.asks.any { it.id == "leo" })
        assertTrue(Fake.checks.values.none { it.user == "leo" })
        assertTrue(Fake.devices.containsKey("l1"))
    }

    @Test
    fun h_anInviteBringsThePersonsOwnKeyToTheirNewPhone() {
        val (secret, locked) = a1!!.call { personKeyForInvite() }!!
        val a5 = phone("ada", "a5")
        val own = JSONObject().put("folder", JSONObject.NULL).put("version", 0).put("public_key", Fake.people["ada"]!!.publicKey).put("locked", locked)
        a5.call { fromInvite(a5.account, secret, listOf(own)) }
        a5.sync()
        assertEquals(Keyring.Status.READY, a5.ring.status)
        assertTrue(a5.ring.hasFolderKey("f2", 1))
    }

    @Test
    fun i_aPinLinksSecretOpensTheFolderForGuests() {
        val a = a1!!
        val (secret, body) = a.call { pinSecret("f1") }!!
        assertEquals(listOf(1), body.getJSONArray("keys").objects().map { it.getInt("version") })
        Fake.pins["p1"] = Pin("f1", body.getString("sealed"), body.getInt("version"), body.getJSONArray("keys").objects().associate { it.getInt("version") to it.getString("locked") }.toMutableMap())
        assertEquals(secret, a.ring.pinLinkSecret("f1", body.getString("sealed"), body.getInt("version")))
        assertNull(a.ring.pinSecret("plain"))
        Fake.pinCaller = "p1"
        val guest = Keyring(api, object : DeviceKeyStore {
            override fun load(deviceId: String): KeyPair? = null

            override fun save(deviceId: String, pair: KeyPair) {}
        })
        assertEquals(1, guest.openPinKeys(pinApi, secret))
        assertArrayEquals(fileKey, guest.fileKey(file("f1", seal!!)))
        assertEquals(0, guest.openPinKeys(pinApi, a.ring.pinSecret("f1")!!.first))
    }

    @Test
    fun j_aNewFolderKeyVersionWhenAskedWhichThePinAndRecoveryGetToo() {
        Fake.rekey += "f1"
        a1!!.sync()
        assertEquals(2, Fake.folders["f1"]!!.size)
        assertEquals(2, a1!!.ring.encryptFor("f1")!!.version)
        assertTrue(Fake.recoverySealed.containsKey("f1:2"))
        assertTrue(Fake.pins["p1"]!!.locked.containsKey(2))
        assertTrue(Fake.sealed.containsKey("f1:2:max"))
    }

    @Test
    fun k_theRecoveryCodeRestoresAnAdminOnANewPhone() {
        val a6 = phone("ada", "a6")
        a6.sync()
        assertEquals(Keyring.Status.WAITING, a6.ring.status)
        assertThrows(E2eeException::class.java) { a6.call { useRecoveryCode("0000-0000-0000-0000-0000-0000-0000-0000") } }
        assertThrows(E2eeException::class.java) { a6.call { useRecoveryCode("not a code") } }
        assertEquals(3, a6.call { useRecoveryCode(code.lowercase()) }) // f1 twice, f2
        assertEquals(Keyring.Status.READY, a6.ring.status)
        assertArrayEquals(fileKey, a6.ring.fileKey(file("f1", seal!!)))
        // It started over with a new person key, which reaches the person's other phones after a check.
        assertFalse("a1" in Fake.people["ada"]!!.sealed)
        approve(a6, a1!!, "device", "a1")
        assertEquals(Keyring.Status.READY, a1!!.ring.status)
        assertTrue(a1!!.ring.hasFolderKey("f1", 2))
    }

    @Test
    fun l_aNewRecoveryCodeGetsTheFolderKeysSealedAgain() {
        val next = a1!!.call { makeRecovery() }
        assertTrue(next != code)
        assertEquals(setOf("f1:1", "f1:2", "f2:1"), Fake.recoverySealed.keys)
        val a7 = phone("ada", "a7")
        a7.sync()
        assertThrows(E2eeException::class.java) { a7.call { useRecoveryCode(code) } }
        assertEquals(3, a7.call { useRecoveryCode(next) })
    }

    @Test
    fun m_whatTheScreensAreTold() {
        val s = a1!!.ring.state()
        assertEquals("keys", s["type"])
        assertEquals("ready", s["status"])
        assertEquals(true, s["has_recovery"])
        assertEquals(2, s["encrypted_folders"])
        assertEquals(listOf("f1:1", "f1:2", "f2:1"), s["open"])
        assertEquals(false, s["waits_for_folders"])
        assertEquals(a1!!.ring.pace(), s["pace"])
        assertEquals(a1!!.ring.asks.map { it.id }, (s["asks"] as List<*>).map { (it as Map<*, *>)["id"] })
        assertEquals(emptyList<Any>(), s["codes"])
    }
}
