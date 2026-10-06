package com.eschgi.share.e2ee

import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.security.SecureRandom
import java.util.concurrent.ConcurrentHashMap

/** The server's keys API, as the keyring asks it. */
fun interface KeysApi {
    /**
     * A request with a JSON body, or none: the answer's JSON, empty for 204. Throws a
     * [KeysApiError] for an error answer, another IOException when there was none.
     */
    fun call(method: String, path: String, body: JSONObject?): JSONObject
}

/** An error answer of the server, with its code from contract/errors.json. */
class KeysApiError(val status: Int, val code: String, message: String) : IOException("$status $code: $message")

/** Where this phone keeps its device key: never on the server, and never in a backup. */
interface DeviceKeyStore {
    fun load(deviceId: String): KeyPair?

    fun save(deviceId: String, pair: KeyPair)

    /** The people's keys checked on this phone, signed in as [deviceId]: user id to public key, in base64url. A store without them asks every time. */
    fun trusted(deviceId: String): Map<String, String> = emptyMap()

    fun trust(deviceId: String, trusted: Map<String, String>) {}
}

/** Who this phone is signed in as: the person, the phone's id on the server, and whether the person is an admin. */
data class Account(val userId: String, val deviceId: String, val admin: Boolean = false)

/** An encrypted file's key, sealed for version [version] of its folder's key ([FileRef]'s enc). */
data class SealedFile(val id: String, val folder: String, val version: Int, val key: String, val header: String, val plainSize: Long) {
    companion object {
        /** From a file as the API gives it, with its folder and enc; null for a plain file. */
        fun of(file: JSONObject): SealedFile? {
            val enc = file.optJSONObject("enc") ?: return null
            return SealedFile(
                id = file.getString("id"),
                folder = file.optString("folder"),
                version = enc.getInt("version"),
                key = enc.getString("key"),
                header = enc.getString("header"),
                plainSize = enc.optLong("plain_size", file.optLong("size")),
            )
        }
    }
}

/** The newest version of a folder's key, which new files are encrypted for. */
data class FolderPublicKey(val folder: String, val version: Int, val publicKey: ByteArray)

/**
 * Someone this phone would pass keys on to once the person allows it, after both screens showed
 * the same code (docs/e2ee-plan.md): a phone or browser of the person that waits for their key
 * ([kind] device, with its [client], app or web, and when it signed in), or another person who
 * waits for the keys of [folders] and whose key wasn't checked here before ([kind] person). Only
 * those seen lately, who can answer. The library lists them; Show opens one, which starts its
 * check: [code] is null until the other side answered.
 */
data class Ask(
    val kind: String,
    val id: String,
    val name: String,
    val client: String? = null,
    val since: String? = null,
    val folders: List<String> = emptyList(),
    val code: String? = null,
)

/** A code this phone shows for a check another device asks: who asks ([from], its name), and the code. */
data class ShownCode(val kind: String, val from: String, val code: String)

/**
 * This phone's keys for end-to-end encryption (docs/e2ee-plan.md), as the website's keyring
 * (web/src/e2ee/keyring.ts) has them: the device key stays on the phone; the person's key and
 * the folders' keys are opened again from what the server keeps sealed for this phone, and the
 * to-do list is worked through with them: whoever is online with a key seals it for those who
 * lack it. Plain JVM code, so the unit tests run it against a fake server.
 */
class Keyring(private val api: KeysApi, private val store: DeviceKeyStore) {
    /**
     * Where this phone stands: [OFF] before it knows anyone; [READY] with the person's key open;
     * [WAITING] while no other phone or browser of the person sealed it for this one; [FAILED]
     * when the server couldn't be asked.
     */
    enum class Status { OFF, LOADING, READY, WAITING, FAILED }

    @Volatile var status = Status.OFF
        private set

    @Volatile private var account: Account? = null
    @Volatile private var answer: JSONObject? = null
    @Volatile private var device: KeyPair? = null
    @Volatile private var person: KeyPair? = null

    /** The folders' keys open here, by "folder:version"; replaced whole, never changed. */
    @Volatile private var folders: Map<String, KeyPair> = emptyMap()

    /** A PIN guest's: the keys of the folder its link opens. */
    @Volatile private var pinFolders: Map<String, KeyPair> = emptyMap()

    /** The person's key was just opened with the password, which so needs no new lock. */
    private var openedWithPassword = false

    /** Who this phone would pass keys on to, after a check: listed, until the person opens one with Show. */
    @Volatile var asks: List<Ask> = emptyList()
        private set

    /** The codes this phone shows for checks other devices ask. */
    @Volatile var codes: List<ShownCode> = emptyList()
        private set

    /** The checks this phone asks, by kind:id: the check, the nonce it reveals after the answer, the key it checks, and the answer it revealed the nonce for, which the code is made from. */
    private class Asking(val check: String, val nonce: ByteArray, val key: ByteArray, var answer: ByteArray? = null)

    /** A check this phone answers: its nonce, and the commitment it saw before answering, which the revealed nonce must match. */
    private class Answering(val nonce: ByteArray, val commitment: ByteArray)

    private val asking = ConcurrentHashMap<String, Asking>()
    private val answering = ConcurrentHashMap<String, Answering>()

    /** The asks the person opened with Show, kind:id: only those get a check. */
    private val shown: MutableSet<String> = ConcurrentHashMap.newKeySet()

    /** The people's keys checked on this phone: user id to key. */
    @Volatile private var trusted: Map<String, String> = emptyMap()

    /**
     * A password typed on this phone while the person's key wasn't open here: it locks the key
     * once the key opens (another device sealed it, or this one made a new one), so that the next
     * device opens it with the password. Only ever in memory.
     */
    private var typedPassword: String? = null

    private val fileKeys = object : LinkedHashMap<String, ByteArray>(256, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, ByteArray>?) = size > 4096
    }

    /** Hears every change of the status or the keys. */
    @Volatile var onChange: (() -> Unit)? = null

    private fun changed(to: Status? = null) {
        if (to != null) status = to
        onChange?.invoke()
    }

    /** Who the keys are for; null before [sync]. */
    val who: Account? get() = account

    /**
     * Opens the keys of [account] on this phone and does what is due; with [password], right
     * after signing in with it. [quiet]: a check-in, which the app makes every half minute while
     * it is in front; it doesn't pass through [Status.LOADING], and keeps the status if it fails.
     */
    @Synchronized
    fun sync(account: Account, password: String? = null, quiet: Boolean = false) {
        if (this.account != account) forget()
        this.account = account
        if (!quiet) changed(Status.LOADING)
        try {
            load(account, password)
        } catch (e: Exception) {
            if (!quiet) changed(Status.FAILED)
            throw e
        }
    }

    /** Forgets everything, e.g. when the phone signs out. */
    @Synchronized
    fun forget() {
        account = null
        answer = null
        device = null
        person = null
        typedPassword = null
        folders = emptyMap()
        asks = emptyList()
        codes = emptyList()
        asking.clear()
        answering.clear()
        shown.clear()
        trusted = emptyMap()
        synchronized(fileKeys) { fileKeys.clear() }
        changed(Status.OFF)
    }

    private fun load(me: Account, password: String?) {
        val dev = deviceKey(me)
        trusted = store.trusted(me.deviceId)
        var a = api.call("GET", "/api/keys", null)
        if (a.str("device_key") != b64u(dev.publicKey)) {
            api.call("PUT", "/api/keys/device", JSONObject().put("public_key", b64u(dev.publicKey)))
            a = api.call("GET", "/api/keys", null)
        }
        // The first phone or browser of this person makes their key, and so does a member's once
        // their key is lost: no phone or browser holds it any more, and no password opens it.
        // Nothing is lost with a new one: whoever has Share open with the folders seals them for it
        // again. An admin's phone asks instead, as the recovery code opens every folder again.
        val known = a.getJSONObject("person")
        val lost = known.str("public_key") != null && known.getInt("held_by") == 0 && known.str("password_lock") == null && !me.admin
        if (known.str("public_key") == null || lost) {
            val p = Hpke.generateKeyPair()
            val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext(me.userId), p.privateKey)
            try {
                api.call("PUT", "/api/keys/person", JSONObject().put("public_key", b64u(p.publicKey)).put("sealed", b64u(sealed)).put("start_over", lost))
            } catch (e: KeysApiError) {
                if (e.code != "key_exists") throw e // another one was quicker
            }
            a = api.call("GET", "/api/keys", null)
        }
        person = openPerson(me, dev, a, password)
        if (password != null) typedPassword = password
        typedPassword?.let { if (person != null && !openedWithPassword) keepPasswordLock(me, a, it) }
        if (person != null) typedPassword = null
        folders = openFolders(a)
        answer = a
        answerChecks(a)
        changed(if (person != null) Status.READY else Status.WAITING)
        if (person == null) return
        val asked = asks
        val did = work(me, a)
        if (did) {
            // What was done may have brought new versions to open and seal for others.
            val again = api.call("GET", "/api/keys", null)
            answer = again
            folders = openFolders(again)
            work(me, again)
        }
        if (did || asks != asked) changed()
    }

    /** This phone's device key: kept, or a new one. */
    private fun deviceKey(me: Account): KeyPair {
        device?.let { return it }
        val kept = store.load(me.deviceId) ?: Hpke.generateKeyPair().also { store.save(me.deviceId, it) }
        device = kept
        return kept
    }

    /** The person's key: sealed for this phone, or locked with the password just typed, which then gets sealed for this phone too. */
    private fun openPerson(me: Account, dev: KeyPair, a: JSONObject, password: String?): KeyPair? {
        openedWithPassword = false
        val p = a.getJSONObject("person")
        val pub = p.str("public_key")?.let(E2ee::fromB64u) ?: return null
        val context = E2ee.personContext(me.userId)
        p.str("sealed")?.let { sealed ->
            try {
                val raw = E2ee.openKey(dev.privateKey, dev.publicKey, E2ee.PURPOSE_PERSON, context, E2ee.fromB64u(sealed))
                // Checks show codes made from this key, so the key the server names must be this one's.
                if (Hpke.publicKey(raw).contentEquals(pub)) return KeyPair(raw, pub)
            } catch (e: E2eeException) {
                // sealed for a key this phone lost: wait, or use the password
            }
        }
        val lock = p.str("password_lock")
        if (password == null || lock == null) return null
        val raw = try {
            E2ee.passwordUnlock(password, context, E2ee.fromB64u(lock))
        } catch (e: E2eeException) {
            return null
        }
        if (!Hpke.publicKey(raw).contentEquals(pub)) return null
        val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, context, raw)
        api.call("POST", "/api/keys/grants", grants(devices = listOf(JSONObject().put("device", me.deviceId).put("sealed", b64u(sealed)))))
        openedWithPassword = true
        return KeyPair(raw, pub)
    }

    /** Keeps the person's key locked with the password just typed, unless that lock is there. */
    private fun keepPasswordLock(me: Account, a: JSONObject, password: String) {
        a.getJSONObject("person").str("password_lock")?.let {
            try {
                E2ee.passwordUnlock(password, E2ee.personContext(me.userId), E2ee.fromB64u(it))
                return
            } catch (e: E2eeException) {
                // a lock with an older password
            }
        }
        api.call("PUT", "/api/keys/password-lock", JSONObject().put("password_lock", b64u(passwordLock(password))))
    }

    /** The folders' keys sealed for the person; those open already stay, as a version's key never changes. */
    private fun openFolders(a: JSONObject): Map<String, KeyPair> {
        val p = person ?: return emptyMap()
        val known = folders
        val out = HashMap<String, KeyPair>()
        for (f in a.getJSONArray("folders").objects()) {
            val sealed = f.str("sealed") ?: continue
            val folder = f.getString("folder")
            val version = f.getInt("version")
            val id = slot(folder, version)
            known[id]?.let {
                out[id] = it
                continue
            }
            try {
                val raw = E2ee.openKey(p.privateKey, p.publicKey, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), E2ee.fromB64u(sealed))
                out[id] = KeyPair(raw, E2ee.fromB64u(f.getString("public_key")))
            } catch (e: E2eeException) {
                // sealed for an older key of the person, or broken: the to-do list brings a new one
            }
        }
        return out
    }

    /** Seals and locks what the to-do list asks for, and asks first where a check is due; true if it did anything. */
    private fun work(me: Account, a: JSONObject): Boolean {
        if (person == null) return false
        val todo = a.getJSONObject("todo")
        val people = ArrayList<JSONObject>()
        val recovery = ArrayList<JSONObject>()
        val pins = ArrayList<JSONObject>()
        // The person's other devices get their key only after a check, once the person sees an
        // encrypted folder: where none is, nobody is asked. So do people whose key wasn't checked
        // here before. A check needs someone to answer it: only those seen lately are asked. This
        // phone isn't, when it opened the key with the password after the list was made.
        val due = ArrayList<Ask>()
        val encrypted = a.getJSONArray("folders").length() > 0
        for (d in todo.getJSONArray("devices").objects()) {
            val id = d.getString("id")
            if (!encrypted || !d.getBoolean("active") || id == me.deviceId) continue
            due += Ask("device", id, d.getString("name"), d.getString("client"), d.getString("created_at"))
        }
        val waiting = LinkedHashMap<String, Ask>()
        for (n in todo.getJSONArray("people").objects()) {
            val folder = n.getString("folder")
            val version = n.getInt("version")
            val k = folders[slot(folder, version)] ?: continue
            val user = n.getString("user")
            if (trusted[user] == n.getString("public_key")) {
                val sealed = E2ee.sealKey(E2ee.fromB64u(n.getString("public_key")), E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), k.privateKey)
                people += JSONObject().put("folder", folder).put("version", version).put("user", user).put("sealed", b64u(sealed))
                continue
            }
            if (!n.getBoolean("active")) continue
            val ask = waiting[user] ?: Ask("person", user, n.getString("name"))
            waiting[user] = if (folder in ask.folders) ask else ask.copy(folders = ask.folders + folder)
        }
        due += waiting.values
        asks = askChecks(a, due)
        a.str("recovery_key")?.let { key ->
            val recoveryKey = E2ee.fromB64u(key)
            for (v in todo.getJSONArray("recovery").objects()) {
                val folder = v.getString("folder")
                val version = v.getInt("version")
                val k = folders[slot(folder, version)] ?: continue
                val sealed = E2ee.sealKey(recoveryKey, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), k.privateKey)
                recovery += JSONObject().put("folder", folder).put("version", version).put("sealed", b64u(sealed))
            }
        }
        for (pin in todo.getJSONArray("pins").objects()) {
            val folder = pin.getString("folder")
            val secretVersion = pin.getInt("secret_version")
            val version = pin.getInt("version")
            val holder = folders[slot(folder, secretVersion)] ?: continue
            val k = folders[slot(folder, version)] ?: continue
            try {
                val secret = E2ee.openKey(holder.privateKey, holder.publicKey, E2ee.PURPOSE_PIN, E2ee.folderContext(folder, secretVersion), E2ee.fromB64u(pin.getString("secret_sealed")))
                val locked = E2ee.lock(E2ee.secretKey(secret, E2ee.PURPOSE_PIN), E2ee.folderContext(folder, version), k.privateKey)
                pins += JSONObject().put("pin", pin.getString("pin")).put("version", version).put("locked", b64u(locked))
            } catch (e: E2eeException) {
                // a broken secret: that PIN's link reads only older files
            }
        }
        var did = false
        if (people.isNotEmpty() || recovery.isNotEmpty() || pins.isNotEmpty()) {
            api.call("POST", "/api/keys/grants", grants(people = people, recovery = recovery, pins = pins))
            did = true
        }
        val recoveryKey = a.str("recovery_key")?.let(E2ee::fromB64u)
        for (folder in todo.getJSONArray("rekey").strings()) {
            val newest = newestIn(a, folder) ?: continue
            if (recoveryKey == null) continue
            try {
                val key = newFolderKey(folder, newest.version + 1, recoveryKey)
                api.call("POST", "/api/folders/$folder/keys", key.put("version", newest.version + 1))
                did = true
            } catch (e: KeysApiError) {
                if (e.code != "key_outdated" && e.status != 404) throw e // someone else made it
            }
        }
        return did
    }

    /**
     * The checks this phone asks: one is opened for every ask the person opened with Show, its
     * nonce revealed once the other side answered, and the code made then, from that answer: one
     * the server shows later can't change it. Checks of asks closed or gone are closed too. The
     * asks still due.
     */
    private fun askChecks(a: JSONObject, due: List<Ask>): List<Ask> {
        val todo = a.getJSONObject("todo")
        fun keyOf(ask: Ask): ByteArray = E2ee.fromB64u(
            if (ask.kind == "device") todo.getJSONArray("devices").objects().first { it.getString("id") == ask.id }.getString("public_key")
            else todo.getJSONArray("people").objects().first { it.getString("user") == ask.id }.getString("public_key"),
        )
        val checks = a.getJSONArray("checks").objects()
        val keys = due.map { "${it.kind}:${it.id}" }.toSet()
        shown.retainAll(keys)
        for ((k, c) in asking.entries.toList()) {
            if (k in shown) continue
            asking.remove(k)
            closeQuietly(c.check)
        }
        val out = ArrayList<Ask>()
        for (ask in due) {
            val k = "${ask.kind}:${ask.id}"
            if (k !in shown) {
                out += ask
                continue
            }
            val key = keyOf(ask)
            var c = asking[k]
            val open = c?.let { mine -> checks.firstOrNull { it.getBoolean("asking") && it.getString("id") == mine.check } }
            if (c != null && (open == null || !c.key.contentEquals(key))) {
                // closed, out of time, or another key now: a new check
                if (open != null) closeQuietly(c.check)
                asking.remove(k)
                c = null
            }
            try {
                if (c == null) {
                    val nonce = randomBytes(E2ee.CHECK_NONCE_SIZE)
                    val body = JSONObject().put(if (ask.kind == "device") "device" else "user", ask.id).put("commitment", b64u(E2ee.commitment(nonce)))
                    asking[k] = Asking(api.call("POST", "/api/keys/checks", body).getString("id"), nonce, key)
                } else if (c.answer == null) {
                    val answer = open?.str("answer")
                    if (answer != null) {
                        api.call("PUT", "/api/keys/checks/${c.check}/reveal", JSONObject().put("nonce", b64u(c.nonce)).put("answer", answer))
                        c.answer = E2ee.fromB64u(answer)
                    }
                }
            } catch (e: KeysApiError) {
                if (c == null && e.status == 404) {
                    shown -= k // nobody waits there any more
                    continue
                }
                if (e.status == 404) asking.remove(k) // the check is gone: a new one next time
                else if (e.status != 409) throw e // 409: answered anew meanwhile; revealed for that next time
            }
            val revealed = c?.answer
            out += if (c != null && revealed != null) ask.copy(code = E2ee.checkCode(c.nonce, revealed, c.key)) else ask
        }
        return out
    }

    /**
     * The checks other devices ask of this one: answered with a nonce of its own, and the code
     * shown once the asking device revealed the nonce it committed to before this answer. A device
     * check is made from this phone's own key; a person's from the person's key, which must be
     * open here.
     */
    private fun answerChecks(a: JSONObject) {
        val shown = ArrayList<ShownCode>()
        val listed = HashSet<String>()
        for (c in a.getJSONArray("checks").objects()) {
            if (c.getBoolean("asking")) continue
            val kind = if (c.isNull("device")) "person" else "device"
            val key = (if (kind == "device") device else person)?.publicKey ?: continue
            val id = c.getString("id")
            listed += id
            val mine = answering[id]
            if (mine == null || !c.getBoolean("answered")) {
                try {
                    if (c.str("reveal") != null) {
                        // revealed for a nonce this phone no longer has: closed, so that a new check starts
                        api.call("DELETE", "/api/keys/checks/$id", null)
                    } else {
                        val nonce = randomBytes(E2ee.CHECK_NONCE_SIZE)
                        api.call("PUT", "/api/keys/checks/$id/answer", JSONObject().put("nonce", b64u(nonce)))
                        answering[id] = Answering(nonce, E2ee.fromB64u(c.getString("commitment")))
                    }
                } catch (e: KeysApiError) {
                    if (e.status != 404) throw e // closed, or another device of the person answered
                }
                continue
            }
            val revealed = E2ee.fromB64u(c.str("reveal") ?: continue)
            if (!E2ee.commitment(revealed).contentEquals(mine.commitment)) continue // not the nonce committed to
            shown += ShownCode(kind, c.getString("from"), E2ee.checkCode(revealed, mine.nonce, key))
        }
        answering.keys.retainAll(listed)
        codes = shown
    }

    private fun closeQuietly(check: String) {
        try {
            api.call("DELETE", "/api/keys/checks/$check", null)
        } catch (e: IOException) {
            // gone already, or no answer: it ends by itself
        }
    }

    /**
     * Allows an ask after the person compared the code: the person's key sealed for the device, or
     * the folder keys for the person, whose key counts as checked here from now on. Then checks
     * in. Throws an [E2eeException] while there is no code to compare.
     */
    @Synchronized
    fun allow(kind: String, id: String) {
        val me = account ?: throw E2eeException("nobody signed in")
        val k = "$kind:$id"
        val c = asking[k]
        val revealed = c?.answer
        val p = person
        val a = answer
        if (c == null || revealed == null || p == null || a == null) throw E2eeException("no code to compare yet")
        if (kind == "device") {
            val sealed = E2ee.sealKey(c.key, E2ee.PURPOSE_PERSON, E2ee.personContext(me.userId), p.privateKey)
            api.call("POST", "/api/keys/grants", grants(devices = listOf(JSONObject().put("device", id).put("sealed", b64u(sealed)))))
        } else {
            val people = ArrayList<JSONObject>()
            for (n in a.getJSONObject("todo").getJSONArray("people").objects()) {
                val folder = n.getString("folder")
                val version = n.getInt("version")
                val key = folders[slot(folder, version)] ?: continue
                if (n.getString("user") != id || !E2ee.fromB64u(n.getString("public_key")).contentEquals(c.key)) continue
                val sealed = E2ee.sealKey(c.key, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), key.privateKey)
                people += JSONObject().put("folder", folder).put("version", version).put("user", id).put("sealed", b64u(sealed))
            }
            trusted = trusted + (id to b64u(c.key))
            store.trust(me.deviceId, trusted)
            api.call("POST", "/api/keys/grants", grants(people = people))
        }
        shown -= k
        asking.remove(k)
        closeQuietly(c.check)
        asks = asks.filter { !(it.kind == kind && it.id == id) }
        changed()
        sync(me, quiet = true)
    }

    /** Opens an ask (Show): a check-in starts its check, and its code comes once the other side answered. */
    @Synchronized
    fun show(kind: String, id: String) {
        val me = account ?: throw E2eeException("nobody signed in")
        shown += "$kind:$id"
        changed()
        sync(me, quiet = true)
    }

    /** Closes an ask's sheet ("Not now"): its check ends, and the ask stays listed. */
    @Synchronized
    fun hide(kind: String, id: String) {
        val k = "$kind:$id"
        shown -= k
        asking.remove(k)?.let { closeQuietly(it.check) }
        asks = asks.map { if (it.kind == kind && it.id == id) it.copy(code = null) else it }
        changed()
    }

    /** Not me: a phone or browser that asked for the person's key, and isn't theirs, is signed out. */
    @Synchronized
    fun deny(kind: String, id: String) {
        api.call("DELETE", "/api/devices/$id", null)
        hide(kind, id)
        asks = asks.filter { !(it.kind == kind && it.id == id) }
        changed()
    }

    /** Whether an ask is for a person whose key was checked here before, and changed since: their key was lost. */
    fun keyChanged(ask: Ask): Boolean = ask.kind == "person" && trusted.containsKey(ask.id)

    /** Whether the person waits for folder keys: a version of an encrypted folder they see isn't sealed for them yet, so someone who has it seals it after a check. */
    fun waitsForFolders(): Boolean = status == Status.READY && (answer?.getJSONArray("folders")?.objects()?.any { it.isNull("sealed") } ?: false)

    /**
     * How long until the next check-in, in milliseconds: soon while a check runs, opened here with
     * Show or answered here, as the code and then the keys come right after the other side's turn;
     * a little later while this phone waits for keys; otherwise half a minute.
     */
    fun pace(): Long = when {
        shown.isNotEmpty() || answering.isNotEmpty() -> 2_000
        status == Status.WAITING || waitsForFolders() -> 5_000
        else -> 30_000
    }

    /** A new version of a folder's key, sealed for this person and the recovery key; it is open here from now on. */
    private fun newFolderKey(folder: String, version: Int, recovery: ByteArray): JSONObject {
        val p = person ?: throw E2eeException("the person's key isn't open here")
        val pair = Hpke.generateKeyPair()
        val aad = E2ee.folderContext(folder, version)
        val sealed = E2ee.sealKey(p.publicKey, E2ee.PURPOSE_FOLDER, aad, pair.privateKey)
        val recoverySealed = E2ee.sealKey(recovery, E2ee.PURPOSE_FOLDER, aad, pair.privateKey)
        folders = folders + (slot(folder, version) to pair)
        return JSONObject().put("public_key", b64u(pair.publicKey)).put("sealed", b64u(sealed)).put("recovery_sealed", b64u(recoverySealed))
    }

    /** Whether version [version] of [folder]'s key is open here (as a PIN guest's too). */
    fun hasFolderKey(folder: String, version: Int): Boolean = folders.containsKey(slot(folder, version)) || pinFolders.containsKey(slot(folder, version))

    /** The newest version of a folder's key, to encrypt for; null if it was never encrypted (or isn't the person's). */
    fun encryptFor(folder: String): FolderPublicKey? = answer?.let { newestIn(it, folder) }

    /** The key of an encrypted file, opened with its folder's key. Throws an [E2eeException] while that key isn't open here. */
    fun fileKey(file: SealedFile): ByteArray {
        val id = "${file.id}:${file.folder}:${file.version}:${file.key}"
        synchronized(fileKeys) { fileKeys[id]?.let { return it } }
        val pair = folders[slot(file.folder, file.version)] ?: pinFolders[slot(file.folder, file.version)]
            ?: throw E2eeException("this folder's key isn't open here")
        val key = E2ee.openKey(pair.privateKey, pair.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext(file.folder, file.version), E2ee.fromB64u(file.key))
        synchronized(fileKeys) { fileKeys[id] = key }
        return key
    }

    /** The person's key locked with a password, for a new password. */
    fun passwordLock(password: String): ByteArray {
        val p = person ?: throw E2eeException("the person's key isn't open here")
        val me = account ?: throw E2eeException("nobody signed in")
        return E2ee.passwordLock(password, E2ee.personContext(me.userId), p.privateKey)
    }

    /** Whether the server has a recovery key yet, which the first encrypted folder needs. */
    fun hasRecovery(): Boolean = answer?.str("recovery_key") != null

    /** Admins: turns encryption on for a folder ([keyVersion] says whether it had a key); the first time with its key's first version. The folder as the server gives it. */
    @Synchronized
    fun encryptFolder(folder: String, keyVersion: Int?): JSONObject {
        if (keyVersion != null) return api.call("PUT", "/api/folders/$folder/encryption", JSONObject().put("encrypted", true))
        val recovery = answer?.str("recovery_key") ?: throw E2eeException("no recovery key yet")
        if (person == null) throw E2eeException("the person's key isn't open here")
        val key = newFolderKey(folder, 1, E2ee.fromB64u(recovery))
        val info = api.call("PUT", "/api/folders/$folder/encryption", JSONObject().put("encrypted", true).put("key", key))
        account?.let { sync(it) }
        return info
    }

    /** Admins: a new recovery key; returns its code, which is shown once. */
    @Synchronized
    fun makeRecovery(): String {
        val (secret, code) = E2ee.newRecoveryCode()
        val pair = Hpke.generateKeyPair()
        val locked = E2ee.lock(E2ee.secretKey(secret, E2ee.PURPOSE_RECOVERY), E2ee.recoveryContext, pair.privateKey)
        api.call("PUT", "/api/recovery", JSONObject().put("public_key", b64u(pair.publicKey)).put("locked", b64u(locked)))
        account?.let { sync(it) }
        return code
    }

    /**
     * Admins: opens every encrypted folder with the recovery code and keeps its keys for this
     * person; starts over first when this phone has no person key. Returns how many versions of
     * folder keys it opened; throws an [E2eeException] for a wrong code.
     */
    @Synchronized
    fun useRecoveryCode(code: String): Int {
        val me = account ?: throw E2eeException("nobody signed in")
        val secret = E2ee.parseRecoveryCode(code)
        val r = api.call("GET", "/api/recovery", null)
        val locked = r.str("locked") ?: throw E2eeException("no recovery key")
        val public = r.str("public_key") ?: throw E2eeException("no recovery key")
        val raw = E2ee.unlock(E2ee.secretKey(secret, E2ee.PURPOSE_RECOVERY), E2ee.recoveryContext, E2ee.fromB64u(locked))
        val recovery = KeyPair(raw, E2ee.fromB64u(public))
        if (person == null) startOver()
        val p = person ?: throw E2eeException("the person's key isn't open here")
        val people = ArrayList<JSONObject>()
        for (f in r.getJSONArray("folders").objects()) {
            val folder = f.getString("folder")
            val version = f.getInt("version")
            val aad = E2ee.folderContext(folder, version)
            try {
                val folderRaw = E2ee.openKey(recovery.privateKey, recovery.publicKey, E2ee.PURPOSE_FOLDER, aad, E2ee.fromB64u(f.getString("sealed")))
                val sealed = E2ee.sealKey(p.publicKey, E2ee.PURPOSE_FOLDER, aad, folderRaw)
                people += JSONObject().put("folder", folder).put("version", version).put("user", me.userId).put("sealed", b64u(sealed))
            } catch (e: E2eeException) {
                // sealed for an older recovery key
            }
        }
        if (people.isNotEmpty()) api.call("POST", "/api/keys/grants", grants(people = people))
        sync(me)
        return people.size
    }

    /** Makes a new person key on this phone, when no other device of the person will come: what was sealed for the old one goes, and the others seal again. */
    @Synchronized
    fun startOver() {
        val me = account ?: throw E2eeException("nobody signed in")
        val dev = deviceKey(me)
        val p = Hpke.generateKeyPair()
        val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext(me.userId), p.privateKey)
        api.call("PUT", "/api/keys/person", JSONObject().put("public_key", b64u(p.publicKey)).put("sealed", b64u(sealed)).put("start_over", true))
        person = p
        sync(me)
    }

    /** Every version of the keys of [only] these folders (every encrypted one with null), locked with a new secret for an invite's link: the secret, and the keys. */
    fun inviteKeys(only: List<String>?): Pair<String, JSONArray> {
        val secret = randomBytes(32)
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_INVITE)
        val keys = JSONArray()
        for ((id, pair) in folders) {
            val folder = id.substringBeforeLast(':')
            val version = id.substringAfterLast(':').toInt()
            if (only != null && folder !in only) continue
            keys.put(JSONObject().put("folder", folder).put("version", version).put("locked", b64u(E2ee.lock(key, E2ee.folderContext(folder, version), pair.privateKey))))
        }
        return b64u(secret) to keys
    }

    /** This person's own key, locked with a new secret for the link of an invite for another of their phones or browsers: the secret, and the lock. */
    fun personKeyForInvite(): Pair<String, String>? {
        val p = person ?: return null
        val me = account ?: return null
        val secret = randomBytes(32)
        val locked = E2ee.lock(E2ee.secretKey(secret, E2ee.PURPOSE_INVITE), E2ee.personContext(me.userId), p.privateKey)
        return b64u(secret) to b64u(locked)
    }

    /** What the link of a PIN that shows an encrypted folder needs: a new secret, and the body to create the PIN with (contract/api/pin_create.json). */
    fun pinSecret(folder: String): Pair<String, JSONObject>? {
        val newest = encryptFor(folder) ?: return null
        val secret = randomBytes(32)
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_PIN)
        val sealed = E2ee.sealKey(newest.publicKey, E2ee.PURPOSE_PIN, E2ee.folderContext(folder, newest.version), secret)
        val keys = JSONArray()
        for ((id, pair) in folders) {
            if (id.substringBeforeLast(':') != folder) continue
            val version = id.substringAfterLast(':').toInt()
            keys.put(JSONObject().put("version", version).put("locked", b64u(E2ee.lock(key, E2ee.folderContext(folder, version), pair.privateKey))))
        }
        return b64u(secret) to JSONObject().put("sealed", b64u(sealed)).put("version", newest.version).put("keys", keys)
    }

    /** The secret of the link of a PIN that shows an encrypted folder, opened with the folder's key; null without that key. */
    fun pinLinkSecret(folder: String, sealed: String, version: Int): String? {
        val pair = folders[slot(folder, version)] ?: return null
        return try {
            b64u(E2ee.openKey(pair.privateKey, pair.publicKey, E2ee.PURPOSE_PIN, E2ee.folderContext(folder, version), E2ee.fromB64u(sealed)))
        } catch (e: E2eeException) {
            null
        }
    }

    /** Admins moving encrypted [files] into [target]: their keys, sealed for the target's newest key (contract/api/files_move.json). */
    fun moveKeys(files: List<JSONObject>, target: String): JSONArray {
        val newest = encryptFor(target)
        val out = JSONArray()
        for (f in files) {
            val sealed = SealedFile.of(f) ?: continue
            if (sealed.folder == target) continue
            if (newest == null) throw E2eeException("the folder was never encrypted")
            val key = fileKey(sealed)
            val resealed = E2ee.sealKey(newest.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext(target, newest.version), key)
            out.put(JSONObject().put("id", sealed.id).put("version", newest.version).put("key", b64u(resealed)))
        }
        return out
    }

    /** A PIN guest's keys: the folder's keys locked with the secret of the PIN's link, from [pinApi] with the PIN's key. How many opened. */
    fun openPinKeys(pinApi: KeysApi, secret: String): Int {
        val k = pinApi.call("GET", "/api/pin/keys", null)
        val folder = k.getString("folder")
        val key = E2ee.secretKey(E2ee.fromB64u(secret), E2ee.PURPOSE_PIN)
        val out = HashMap<String, KeyPair>()
        for (v in k.getJSONArray("keys").objects()) {
            val version = v.getInt("version")
            try {
                val raw = E2ee.unlock(key, E2ee.folderContext(folder, version), E2ee.fromB64u(v.getString("locked")))
                out[slot(folder, version)] = KeyPair(raw, E2ee.fromB64u(v.getString("public_key")))
            } catch (e: E2eeException) {
                // locked with another secret
            }
        }
        pinFolders = out
        changed()
        return out.size
    }

    /** The PIN guest's keys go, e.g. when the PIN ended. */
    fun forgetPin() {
        pinFolders = emptyMap()
        changed()
    }

    /**
     * Sets up the keys of a phone that just accepted an invite, with the keys its link's
     * [secret] unlocks: the person's own (a new phone of someone), or folder keys for a new
     * person, which get sealed for their new person key. Without a secret, or with a wrong one,
     * the phone waits for the others like any new one. [sync] comes after.
     */
    @Synchronized
    fun fromInvite(me: Account, secret: String?, keys: List<JSONObject>) {
        if (account != me) forget()
        account = me
        val dev = deviceKey(me)
        api.call("PUT", "/api/keys/device", JSONObject().put("public_key", b64u(dev.publicKey)))
        if (secret == null) return
        val key = E2ee.secretKey(E2ee.fromB64u(secret), E2ee.PURPOSE_INVITE)
        val context = E2ee.personContext(me.userId)
        val own = keys.firstOrNull { it.isNull("folder") }
        if (own != null) {
            val raw = E2ee.unlock(key, context, E2ee.fromB64u(own.getString("locked")))
            val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, context, raw)
            api.call("POST", "/api/keys/grants", grants(devices = listOf(JSONObject().put("device", me.deviceId).put("sealed", b64u(sealed)))))
            return
        }
        val p = Hpke.generateKeyPair()
        try {
            api.call("PUT", "/api/keys/person", JSONObject().put("public_key", b64u(p.publicKey)).put("sealed", b64u(E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, context, p.privateKey))))
        } catch (e: KeysApiError) {
            if (e.code == "key_exists") return // someone joining twice: the key there stays
            throw e
        }
        val people = ArrayList<JSONObject>()
        for (k in keys) {
            val folder = k.optString("folder").takeIf { !k.isNull("folder") && it.isNotEmpty() } ?: continue
            val version = k.getInt("version")
            val aad = E2ee.folderContext(folder, version)
            try {
                val raw = E2ee.unlock(key, aad, E2ee.fromB64u(k.getString("locked")))
                people += JSONObject().put("folder", folder).put("version", version).put("user", me.userId).put("sealed", b64u(E2ee.sealKey(p.publicKey, E2ee.PURPOSE_FOLDER, aad, raw)))
            } catch (e: E2eeException) {
                // locked with another secret
            }
        }
        if (people.isNotEmpty()) api.call("POST", "/api/keys/grants", grants(people = people))
    }

    /** What the app's screens show (contract/app/platform.json keys_event). */
    fun state(): Map<String, Any?> = mapOf(
        "type" to "keys",
        "status" to status.name.lowercase(),
        "has_recovery" to hasRecovery(),
        "encrypted_folders" to (answer?.getJSONArray("folders")?.objects()?.map { it.getString("folder") }?.distinct()?.size ?: 0),
        "open" to (folders.keys + pinFolders.keys).sorted(),
        "asks" to asks.map { askMap(it) },
        "codes" to codes.map { mapOf("kind" to it.kind, "from" to it.from, "code" to it.code) },
        "waits_for_folders" to waitsForFolders(),
        "pace" to pace(),
    )

    private fun askMap(it: Ask) = mapOf(
        "kind" to it.kind, "id" to it.id, "name" to it.name, "client" to it.client, "since" to it.since,
        "folders" to it.folders, "code" to it.code, "key_changed" to keyChanged(it),
    )

    companion object {
        private val random = SecureRandom()

        private fun slot(folder: String, version: Int) = "$folder:$version"

        private fun randomBytes(n: Int) = ByteArray(n).also { random.nextBytes(it) }

        private fun b64u(bytes: ByteArray) = E2ee.b64u(bytes)

        private fun newestIn(a: JSONObject, folder: String): FolderPublicKey? {
            var best: JSONObject? = null
            for (f in a.getJSONArray("folders").objects()) {
                if (f.getString("folder") == folder && (best == null || f.getInt("version") > best.getInt("version"))) best = f
            }
            return best?.let { FolderPublicKey(folder, it.getInt("version"), E2ee.fromB64u(it.getString("public_key"))) }
        }

        private fun grants(
            devices: List<JSONObject> = emptyList(),
            people: List<JSONObject> = emptyList(),
            recovery: List<JSONObject> = emptyList(),
            pins: List<JSONObject> = emptyList(),
        ) = JSONObject().put("devices", JSONArray(devices)).put("people", JSONArray(people)).put("recovery", JSONArray(recovery)).put("pins", JSONArray(pins))
    }
}

/** A string field, or null when it is missing, null or empty. */
internal fun JSONObject.str(name: String): String? = if (isNull(name)) null else optString(name).ifEmpty { null }

internal fun JSONArray.objects(): List<JSONObject> = List(length()) { getJSONObject(it) }

internal fun JSONArray.strings(): List<String> = List(length()) { getString(it) }
