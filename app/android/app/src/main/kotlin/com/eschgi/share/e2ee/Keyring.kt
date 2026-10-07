package com.eschgi.share.e2ee

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.security.SecureRandom
import java.util.UUID
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

/** Nothing goes into a folder: what the server says about its keys can't be checked (docs/e2ee-plan.md). */
class SendRefused(message: String) : IOException(message)

/** Changing a folder needs the recovery key's private key, which this phone doesn't hold (yet): another admin passes it on with an OK. */
class NeedsRoot : IOException("this phone doesn't hold the recovery key")

/**
 * What this phone keeps of what the server says about keys, so that a changed database can't take
 * it back (docs/e2ee-plan.md): the person's public key it holds the private key of; the root it
 * trusts, and whether it only took it from the server the first time ([tofu]); and the newest
 * version of each folder's key it saw, signed by the root.
 */
data class Pins(val person: String? = null, val root: String? = null, val tofu: Boolean = false, val folders: Map<String, Int> = emptyMap()) {
    fun toJson(): JSONObject = JSONObject()
        .put("person", person ?: JSONObject.NULL)
        .put("root", root ?: JSONObject.NULL)
        .put("tofu", tofu)
        .put("folders", JSONObject(folders))

    companion object {
        fun parse(j: JSONObject?): Pins {
            if (j == null) return Pins()
            val f = j.optJSONObject("folders")
            val folders = f?.keys()?.asSequence()?.associateWith { f.optInt(it) }?.filterValues { it > 0 } ?: emptyMap()
            return Pins(j.str("person"), j.str("root"), j.optBoolean("tofu"), folders)
        }
    }
}

/** Where this phone keeps its device key: never on the server, and never in a backup. */
interface DeviceKeyStore {
    fun load(deviceId: String): KeyPair?

    fun save(deviceId: String, pair: KeyPair)

    /** The people's keys checked on this phone, signed in as [deviceId]: user id to public key, in base64url. A store without them asks every time. */
    fun trusted(deviceId: String): Map<String, String> = emptyMap()

    fun trust(deviceId: String, trusted: Map<String, String>) {}

    /** What this phone keeps of the keys, signed in as [deviceId]. */
    fun pins(deviceId: String): Pins

    fun keepPins(deviceId: String, pins: Pins)
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
 * waits for the keys of [folders], or as an admin for the recovery key's private key ([root]),
 * and whose key wasn't checked here before ([kind] person). Only those seen lately, who can
 * answer. The library lists them; Show opens one, which starts its check: [code] is null until
 * the other side answered.
 */
data class Ask(
    val kind: String,
    val id: String,
    val name: String,
    val client: String? = null,
    val since: String? = null,
    val folders: List<String> = emptyList(),
    val root: Boolean = false,
    val code: String? = null,
)

/** A code this phone shows for a check another device asks: who asks ([from], its name), and the code. */
data class ShownCode(val kind: String, val from: String, val code: String)

/**
 * This phone's keys for end-to-end encryption (docs/e2ee-plan.md), as the website's keyring
 * (web/src/e2ee/keyring.ts) has them: the device key stays on the phone; the person's key and
 * the folders' keys are opened again from what the server keeps sealed for this phone, and the
 * to-do list is worked through with them: whoever is online with a key seals it for those who
 * lack it. What the server says about keys counts only as far as the root this phone trusts
 * signed it, and this phone keeps what it saw ([Pins]). Plain JVM code, so the unit tests run it
 * against a fake server.
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

    /** A PIN guest's: the keys of the folder its link opens, and the root its secret opens. */
    @Volatile private var pinFolders: Map<String, KeyPair> = emptyMap()
    @Volatile var guestRoot: ByteArray? = null
        private set

    /** What this phone keeps of the keys. */
    @Volatile private var pins = Pins()

    /** The root this phone trusts: the newest the chain leads to from the one it learned. */
    @Volatile private var root: ByteArray? = null

    /** The root's private key, for an admin who holds it. */
    @Volatile private var rootKey: KeyPair? = null

    /** The person's note, as the server has it. */
    @Volatile private var note: Pins? = null

    /** Signatures checked here, and what came of it. */
    private val verified = ConcurrentHashMap<String, Boolean>()

    /** The person's key was just opened with the password, which so needs no new lock. */
    private var openedWithPassword = false

    /** Who this phone would pass keys on to, after a check: listed, until the person opens one with Show. */
    @Volatile var asks: List<Ask> = emptyList()
        private set

    /** The codes this phone shows for checks other devices ask. */
    @Volatile var codes: List<ShownCode> = emptyList()
        private set

    /** The checks this phone asks, by kind:id: the check, its one-time key, the key it checks, and the answer it revealed its key for, which the code is made from. */
    private class Asking(val check: String, val mine: KeyPair, val key: ByteArray, var answer: ByteArray? = null)

    /** A check this phone answers: its one-time key, and the commitment it saw before answering, which the revealed key must match. */
    private class Answering(val mine: KeyPair, val commitment: ByteArray)

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
            // A check's confirmation brings this phone the person's key or the root: the rest
            // opens with them right away.
            if (load(account, password)) load(account, null)
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
        pins = Pins()
        root = null
        rootKey = null
        note = null
        asks = emptyList()
        codes = emptyList()
        asking.clear()
        answering.clear()
        shown.clear()
        trusted = emptyMap()
        synchronized(fileKeys) { fileKeys.clear() }
        changed(Status.OFF)
    }

    private fun keep(me: Account, p: Pins) {
        pins = p
        store.keepPins(me.deviceId, p)
    }

    /** One round with the server; true if a check's confirmation brought something new. */
    private fun load(me: Account, password: String?): Boolean {
        var dev = deviceKey(me)
        trusted = store.trusted(me.deviceId)
        pins = store.pins(me.deviceId)
        val kept = pins
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
                keep(me, pins.copy(person = b64u(p.publicKey)))
            } catch (e: KeysApiError) {
                if (e.code != "key_exists") throw e // another one was quicker
            }
            a = api.call("GET", "/api/keys", null)
        }
        person = openPerson(me, dev, a, password)
        val named = a.getJSONObject("person")
        if (person == null && named.str("sealed") != null && named.str("public_key") != null && password == null) {
            // Sealed for this phone, but not a key it was given where a database can't change it: a
            // new device key drops it, so that this phone is asked for again, with a check.
            dev = Hpke.generateKeyPair().also { store.save(me.deviceId, it) }
            device = dev
            keep(me, pins.copy(person = null))
            api.call("PUT", "/api/keys/device", JSONObject().put("public_key", b64u(dev.publicKey)))
            a = api.call("GET", "/api/keys", null)
        }
        if (password != null) typedPassword = password
        typedPassword?.let { if (person != null && !openedWithPassword) keepPasswordLock(me, a, it) }
        if (person != null) typedPassword = null
        note = openNote(me, a)
        learnRoot(a)
        rootKey = openRoot(a)
        folders = openFolders(a)
        answer = a
        val confirmed = answerChecks(me, a)
        if (pins != kept) store.keepPins(me.deviceId, pins)
        if (person != null) {
            try {
                keepNote(me)
            } catch (e: IOException) {
                // written next time
            }
        }
        changed(if (person != null) Status.READY else Status.WAITING)
        if (confirmed) return true
        if (person == null) return false
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
        return false
    }

    /** This phone's device key: kept, or a new one. */
    private fun deviceKey(me: Account): KeyPair {
        device?.let { return it }
        val kept = store.load(me.deviceId) ?: Hpke.generateKeyPair().also { store.save(me.deviceId, it) }
        device = kept
        return kept
    }

    /** Whether [publicKey] signed [message]: checked once per process. */
    private fun signed(publicKey: ByteArray?, message: ByteArray, signature: String?): Boolean {
        if (publicKey == null || signature == null) return false
        val id = "${b64u(publicKey)}:${b64u(message)}:$signature"
        return verified.getOrPut(id) {
            try {
                E2ee.verify(publicKey, message, E2ee.fromB64u(signature))
            } catch (e: E2eeException) {
                false
            }
        }
    }

    /** Whether the root this phone trusts signed that version of a folder's key. */
    private fun signedKey(f: JSONObject): Boolean =
        signed(root, E2ee.folderKeyMessage(f.getString("folder"), f.getInt("version"), E2ee.fromB64u(f.getString("public_key"))), f.str("signature"))

    /** The person's key: sealed for this phone, if it is the key this phone keeps; or locked with the password just typed, which vouches for it, and then sealed for this phone too. */
    private fun openPerson(me: Account, dev: KeyPair, a: JSONObject, password: String?): KeyPair? {
        openedWithPassword = false
        val p = a.getJSONObject("person")
        val named = p.str("public_key") ?: return null
        val pub = E2ee.fromB64u(named)
        val context = E2ee.personContext(me.userId)
        p.str("sealed")?.let { sealed ->
            if (pins.person != named) return@let
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
        keep(me, pins.copy(person = named))
        val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, context, raw)
        api.call("POST", "/api/keys/grants", grants(devices = listOf(JSONObject().put("device", me.deviceId).put("sealed", b64u(sealed)))))
        openedWithPassword = true
        return KeyPair(raw, pub)
    }

    /** The person's note, opened with their key; null without one, or one that doesn't open. */
    private fun openNote(me: Account, a: JSONObject): Pins? {
        val p = person ?: return null
        val locked = a.getJSONObject("person").str("note") ?: return null
        return try {
            Pins.parse(JSONObject(String(E2ee.unlock(E2ee.noteKey(p.privateKey), E2ee.noteContext(me.userId), E2ee.fromB64u(locked)))))
        } catch (e: Exception) {
            null
        }
    }

    /**
     * The root this phone trusts: the one it keeps, or else the person's note's, or else the
     * newest the server names (tofu, which the note's replaces); then the newest the chain leads
     * to from it. And the newest version of each folder's key it signed, kept with the note's.
     */
    private fun learnRoot(a: JSONObject) {
        val roots = a.getJSONArray("roots")
        var start = pins.root?.let(E2ee::fromB64u)
        var tofu = pins.tofu
        val noted = note?.root?.let(E2ee::fromB64u)
        if (noted != null && (start == null || (tofu && !noted.contentEquals(start)))) {
            start = noted
            tofu = false
        }
        if (start == null && roots.length() > 0) {
            start = E2ee.fromB64u(roots.getJSONObject(roots.length() - 1).getString("public_key"))
            tofu = true
        }
        root = start?.let { follow(roots, it) }
        val seen = HashMap(pins.folders)
        note?.folders?.forEach { (f, v) -> seen[f] = maxOf(seen[f] ?: 0, v) }
        for (f in a.getJSONArray("folders").objects()) {
            if (signedKey(f)) seen[f.getString("folder")] = maxOf(seen[f.getString("folder")] ?: 0, f.getInt("version"))
        }
        pins = pins.copy(root = root?.let(::b64u), tofu = root != null && tofu, folders = seen)
    }

    /** Trusts a root that came where a database can't change it: made here, the recovery code, an invite's link, a check. */
    private fun adoptRoot(me: Account, newRoot: ByteArray, roots: JSONArray) {
        val r = follow(roots, newRoot)
        root = r
        keep(me, pins.copy(root = b64u(r), tofu = false))
    }

    /** The newest root's private key, sealed for the person: an admin's, when it is the root this phone trusts. */
    private fun openRoot(a: JSONObject): KeyPair? {
        val p = person ?: return null
        val r = root ?: return null
        val sealed = a.str("root_sealed") ?: return null
        rootKey?.let { if (it.publicKey.contentEquals(r)) return it }
        return try {
            val raw = E2ee.openKey(p.privateKey, p.publicKey, E2ee.PURPOSE_ROOT, E2ee.rootContext, E2ee.fromB64u(sealed))
            if (Hpke.publicKey(raw).contentEquals(r)) KeyPair(raw, r) else null
        } catch (e: E2eeException) {
            null
        }
    }

    /** Writes what this phone keeps into the person's note, where the note lacks it. */
    private fun keepNote(me: Account) {
        val p = person ?: return
        val r = root ?: return
        if (pins.tofu) return
        val n = note
        val folders = HashMap(n?.folders ?: emptyMap())
        var stale = n?.root != b64u(r)
        for ((f, v) in pins.folders) {
            if ((folders[f] ?: 0) < v) {
                folders[f] = v
                stale = true
            }
        }
        if (!stale) return
        val written = Pins(root = b64u(r), folders = folders)
        val body = JSONObject().put("root", written.root).put("folders", JSONObject(folders)).toString().toByteArray()
        api.call("PUT", "/api/keys/note", JSONObject().put("note", b64u(E2ee.lock(E2ee.noteKey(p.privateKey), E2ee.noteContext(me.userId), body))))
        note = written
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

    /** The folders' keys sealed for the person, the root signed, whose private key belongs to the signed public key; those open already stay, as a version's key never changes. */
    private fun openFolders(a: JSONObject): Map<String, KeyPair> {
        val p = person ?: return emptyMap()
        val known = folders
        val out = HashMap<String, KeyPair>()
        for (f in a.getJSONArray("folders").objects()) {
            val sealed = f.str("sealed") ?: continue
            val folder = f.getString("folder")
            val version = f.getInt("version")
            val id = slot(folder, version)
            val pub = E2ee.fromB64u(f.getString("public_key"))
            known[id]?.let {
                if (it.publicKey.contentEquals(pub)) {
                    out[id] = it
                    continue
                }
            }
            if (!signedKey(f)) continue
            try {
                val raw = E2ee.openKey(p.privateKey, p.publicKey, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), E2ee.fromB64u(sealed))
                if (Hpke.publicKey(raw).contentEquals(pub)) out[id] = KeyPair(raw, pub)
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
        val pinGrants = ArrayList<JSONObject>()
        val roots = ArrayList<JSONObject>()
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
        // Admins get the recovery key's private key the same way, from an admin who holds it.
        rootKey?.let { rk ->
            for (n in todo.getJSONArray("roots").objects()) {
                val user = n.getString("user")
                if (trusted[user] == n.getString("public_key")) {
                    roots += JSONObject().put("user", user).put("sealed", b64u(E2ee.sealKey(E2ee.fromB64u(n.getString("public_key")), E2ee.PURPOSE_ROOT, E2ee.rootContext, rk.privateKey)))
                    continue
                }
                if (!n.getBoolean("active")) continue
                waiting[user] = (waiting[user] ?: Ask("person", user, n.getString("name"))).copy(root = true)
            }
        }
        due += waiting.values
        asks = askChecks(a, due)
        // Sealed for the recovery key only when it is the root this phone trusts: a database that
        // names another one gets nothing.
        val r = root
        val serverRoots = a.getJSONArray("roots")
        val newest = if (serverRoots.length() > 0) serverRoots.getJSONObject(serverRoots.length() - 1).getString("public_key") else null
        if (r != null && newest == b64u(r)) {
            for (v in todo.getJSONArray("recovery").objects()) {
                val folder = v.getString("folder")
                val version = v.getInt("version")
                val k = folders[slot(folder, version)] ?: continue
                val sealed = E2ee.sealKey(r, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), k.privateKey)
                recovery += JSONObject().put("folder", folder).put("version", version).put("sealed", b64u(sealed))
            }
        }
        // Versions locked for a PIN's link only for a secret that someone who holds the folder's key
        // locked: it opens with a key from that.
        for (pin in todo.getJSONArray("pins").objects()) {
            val folder = pin.getString("folder")
            val secretVersion = pin.getInt("secret_version")
            val version = pin.getInt("version")
            val holder = folders[slot(folder, secretVersion)] ?: continue
            val k = folders[slot(folder, version)] ?: continue
            try {
                val secret = E2ee.unlock(E2ee.pinSecretKey(holder.privateKey), E2ee.folderContext(folder, secretVersion), E2ee.fromB64u(pin.getString("secret_locked")))
                val locked = E2ee.lock(E2ee.secretKey(secret, E2ee.PURPOSE_PIN), E2ee.folderContext(folder, version), k.privateKey)
                pinGrants += JSONObject().put("pin", pin.getString("pin")).put("version", version).put("locked", b64u(locked))
            } catch (e: E2eeException) {
                // a secret nobody with the folder's key made: that PIN's link gets nothing
            }
        }
        var did = false
        if (people.isNotEmpty() || recovery.isNotEmpty() || pinGrants.isNotEmpty() || roots.isNotEmpty()) {
            api.call("POST", "/api/keys/grants", grants(people = people, recovery = recovery, pins = pinGrants, roots = roots))
            did = true
        }
        if (rootKey != null) {
            for (folder in todo.getJSONArray("rekey").strings()) {
                val version = a.getJSONArray("folders").objects().filter { it.getString("folder") == folder }.maxOfOrNull { it.getInt("version") } ?: continue
                try {
                    api.call("POST", "/api/folders/$folder/keys", newFolderKey(folder, version + 1).put("version", version + 1))
                    did = true
                } catch (e: KeysApiError) {
                    if (e.code != "key_outdated" && e.code != "not_encrypted" && e.status != 404) throw e // someone else made it
                }
            }
        }
        return did
    }

    /**
     * The checks this phone asks: one is opened for every ask the person opened with Show, its
     * one-time key revealed once the other side answered, and the code made then, from that
     * answer: one the server shows later can't change it. Checks of asks closed or gone are
     * closed too. The asks still due.
     */
    private fun askChecks(a: JSONObject, due: List<Ask>): List<Ask> {
        val todo = a.getJSONObject("todo")
        fun keyOf(ask: Ask): ByteArray = E2ee.fromB64u(
            if (ask.kind == "device") todo.getJSONArray("devices").objects().first { it.getString("id") == ask.id }.getString("public_key")
            else (todo.getJSONArray("people").objects().firstOrNull { it.getString("user") == ask.id } ?: todo.getJSONArray("roots").objects().first { it.getString("user") == ask.id }).getString("public_key"),
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
                    val mine = Hpke.generateKeyPair()
                    val body = JSONObject().put(if (ask.kind == "device") "device" else "user", ask.id).put("commitment", b64u(E2ee.commitment(mine.publicKey)))
                    asking[k] = Asking(api.call("POST", "/api/keys/checks", body).getString("id"), mine, key)
                } else if (c.answer == null) {
                    val answer = open?.str("answer")
                    if (answer != null) {
                        api.call("PUT", "/api/keys/checks/${c.check}/reveal", JSONObject().put("key", b64u(c.mine.publicKey)).put("answer", answer))
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
            out += if (c != null && revealed != null) ask.copy(code = E2ee.checkCode(c.mine.publicKey, revealed, c.key)) else ask
        }
        return out
    }

    /**
     * The checks other devices ask of this one: answered with a one-time key of its own, and the
     * code shown once the asking device revealed the key it committed to before this answer. A
     * device check is made from this phone's own key; a person's from the person's key, which must
     * be open here. A confirmation, which only this phone opens, brings the root, and to a device
     * of the person their key; true when it brought something.
     */
    private fun answerChecks(me: Account, a: JSONObject): Boolean {
        val shown = ArrayList<ShownCode>()
        val listed = HashSet<String>()
        var brought = false
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
                        // revealed for a key this phone no longer has: closed, so that a new check starts
                        api.call("DELETE", "/api/keys/checks/$id", null)
                    } else {
                        val ot = Hpke.generateKeyPair()
                        api.call("PUT", "/api/keys/checks/$id/answer", JSONObject().put("key", b64u(ot.publicKey)))
                        answering[id] = Answering(ot, E2ee.fromB64u(c.getString("commitment")))
                    }
                } catch (e: KeysApiError) {
                    if (e.status != 404) throw e // closed, or another device of the person answered
                }
                continue
            }
            val revealed = E2ee.fromB64u(c.str("reveal") ?: continue)
            if (!E2ee.commitment(revealed).contentEquals(mine.commitment)) continue // not the key committed to
            val confirmation = c.str("confirmation")
            if (confirmation == null) {
                shown += ShownCode(kind, c.getString("from"), E2ee.checkCode(revealed, mine.mine.publicKey, key))
                continue
            }
            // Allowed on the other side: what it hands on opens only with this side's one-time key.
            val got = try {
                val k = E2ee.confirmKey(mine.mine.privateKey, revealed, revealed, mine.mine.publicKey, key)
                JSONObject(String(E2ee.unlock(k, E2ee.checkContext(id), E2ee.fromB64u(confirmation))))
            } catch (e: E2eeException) {
                null // not for this side's key: nothing to take
            } catch (e: JSONException) {
                null
            }
            if (got != null && takeConfirmed(me, a, kind, got)) brought = true
            answering.remove(id)
            closeQuietly(id)
        }
        answering.keys.retainAll(listed)
        codes = shown
        return brought
    }

    /** Takes what a check's confirmation brought: the person's key for this device, sealed for itself, and the root. */
    private fun takeConfirmed(me: Account, a: JSONObject, kind: String, got: JSONObject): Boolean {
        var brought = false
        val p = got.optJSONObject("person")
        if (kind == "device" && p != null) {
            val named = p.getString("public_key")
            val pub = E2ee.fromB64u(named)
            val raw = E2ee.fromB64u(p.getString("private_key"))
            if (a.getJSONObject("person").str("public_key") == named && Hpke.publicKey(raw).contentEquals(pub)) {
                keep(me, pins.copy(person = named))
                val dev = deviceKey(me)
                val sealed = E2ee.sealKey(dev.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext(me.userId), raw)
                api.call("POST", "/api/keys/grants", grants(devices = listOf(JSONObject().put("device", me.deviceId).put("sealed", b64u(sealed)))))
                person = KeyPair(raw, pub)
                brought = true
            }
        }
        val given = got.str("root") ?: return brought
        val r = root
        if (r == null || pins.tofu || given != b64u(r)) {
            val newRoot = E2ee.fromB64u(given)
            // A root the chain leads to from this one is older: this phone keeps the newer one.
            if (r == null || pins.tofu || !follow(a.getJSONArray("roots"), newRoot).contentEquals(r)) {
                adoptRoot(me, newRoot, a.getJSONArray("roots"))
                brought = true
            }
        }
        return brought
    }

    private fun closeQuietly(check: String) {
        try {
            api.call("DELETE", "/api/keys/checks/$check", null)
        } catch (e: IOException) {
            // gone already, or no answer: it ends by itself
        }
    }

    /**
     * Allows an ask after the person compared the code: hands the person's key on to the device
     * in the check's confirmation, or seals the folder keys, and for an admin the recovery key's
     * private key, for the person, whose key counts as checked here from now on; and hands on the
     * root this phone trusts. Then checks in. Throws an [E2eeException] while there is no code to
     * compare.
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
        val confirmed = JSONObject().put("root", root?.takeIf { !pins.tofu }?.let(::b64u) ?: JSONObject.NULL)
        if (kind == "device") {
            confirmed.put("person", JSONObject().put("public_key", b64u(p.publicKey)).put("private_key", b64u(p.privateKey)))
        } else {
            val people = ArrayList<JSONObject>()
            val todo = a.getJSONObject("todo")
            for (n in todo.getJSONArray("people").objects()) {
                val folder = n.getString("folder")
                val version = n.getInt("version")
                val key = folders[slot(folder, version)] ?: continue
                if (n.getString("user") != id || !E2ee.fromB64u(n.getString("public_key")).contentEquals(c.key)) continue
                val sealed = E2ee.sealKey(c.key, E2ee.PURPOSE_FOLDER, E2ee.folderContext(folder, version), key.privateKey)
                people += JSONObject().put("folder", folder).put("version", version).put("user", id).put("sealed", b64u(sealed))
            }
            val roots = ArrayList<JSONObject>()
            val rk = rootKey
            if (rk != null && todo.getJSONArray("roots").objects().any { it.getString("user") == id && E2ee.fromB64u(it.getString("public_key")).contentEquals(c.key) }) {
                roots += JSONObject().put("user", id).put("sealed", b64u(E2ee.sealKey(c.key, E2ee.PURPOSE_ROOT, E2ee.rootContext, rk.privateKey)))
            }
            api.call("POST", "/api/keys/grants", grants(people = people, roots = roots))
            trusted = trusted + (id to b64u(c.key))
            store.trust(me.deviceId, trusted)
        }
        val key = E2ee.confirmKey(c.mine.privateKey, revealed, c.mine.publicKey, revealed, c.key)
        api.call("PUT", "/api/keys/checks/${c.check}/confirm", JSONObject().put("confirmation", b64u(E2ee.lock(key, E2ee.checkContext(c.check), confirmed.toString().toByteArray()))))
        // The waiting side closes the check once it read the confirmation.
        shown -= k
        asking.remove(k)
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

    /** A new version of a folder's key, signed by the root, sealed for this person and the recovery key; it is open here from now on. */
    private fun newFolderKey(folder: String, version: Int): JSONObject {
        val p = person ?: throw E2eeException("the person's key isn't open here")
        val rk = rootKey ?: throw NeedsRoot()
        val pair = Hpke.generateKeyPair()
        val aad = E2ee.folderContext(folder, version)
        val sealed = E2ee.sealKey(p.publicKey, E2ee.PURPOSE_FOLDER, aad, pair.privateKey)
        val recoverySealed = E2ee.sealKey(rk.publicKey, E2ee.PURPOSE_FOLDER, aad, pair.privateKey)
        val signature = E2ee.sign(rk.privateKey, E2ee.folderKeyMessage(folder, version, pair.publicKey))
        folders = folders + (slot(folder, version) to pair)
        return JSONObject().put("public_key", b64u(pair.publicKey)).put("signature", b64u(signature)).put("sealed", b64u(sealed)).put("recovery_sealed", b64u(recoverySealed))
    }

    /** The root's plain statement for a folder. */
    private fun plainSignature(folder: String, version: Int, name: String): String {
        val rk = rootKey ?: throw NeedsRoot()
        return b64u(E2ee.sign(rk.privateKey, E2ee.plainMessage(folder, version, name)))
    }

    /** Whether version [version] of [folder]'s key is open here (as a PIN guest's too). */
    fun hasFolderKey(folder: String, version: Int): Boolean = folders.containsKey(slot(folder, version)) || pinFolders.containsKey(slot(folder, version))

    /**
     * The newest version of a folder's key, signed by the root this phone trusts; null when the
     * folder has none. Throws [SendRefused] when the server shows fewer versions than this phone,
     * or the person's note, saw, or the newest isn't signed.
     */
    private fun newestSigned(a: JSONObject, folder: String): FolderPublicKey? {
        val newest = a.getJSONArray("folders").objects().filter { it.getString("folder") == folder }.maxByOrNull { it.getInt("version") }
        if ((newest?.getInt("version") ?: 0) < (pins.folders[folder] ?: 0)) throw SendRefused("the folder shows fewer versions of its key than seen before")
        if (newest == null) return null
        if (!signedKey(newest)) throw SendRefused("the folder's newest key isn't signed by the root")
        return FolderPublicKey(folder, newest.getInt("version"), E2ee.fromB64u(newest.getString("public_key")))
    }

    /**
     * What a new file into [folder] (as GET /api/folders describes it) is sealed for: its newest
     * key, signed by the root this phone trusts; null to send it plain, which only the root's
     * plain statement for the folder's newest version and its name allows, once there is a root.
     * A folder switched off without one still gets encrypted files, which the server takes.
     * Throws [SendRefused] when nothing may go in.
     */
    fun sendKey(folder: JSONObject): FolderPublicKey? {
        val a = answer ?: throw SendRefused("the keys couldn't be loaded")
        val id = folder.getString("id")
        val r = root
        if (r == null) {
            // No root anywhere yet: no folder can have keys, and everything goes plain.
            if (a.getJSONArray("roots").length() > 0 || pins.root != null || a.getJSONArray("folders").objects().any { it.getString("folder") == id }) throw SendRefused("no root to check the keys with")
            return null
        }
        val newest = newestSigned(a, id)
        if (newest != null && folder.optBoolean("encrypted")) return newest
        if (signed(r, E2ee.plainMessage(id, newest?.version ?: 0, folder.getString("name")), folder.str("plain_signature"))) return null
        return newest ?: throw SendRefused("the folder is neither signed as plain nor has a signed key")
    }

    /** Whether changing folders needs the recovery key's private key here, and it isn't: once there is a root, only an admin's phone that holds it makes folders, switches them and renames plain ones. */
    fun lacksRoot(): Boolean = (answer?.getJSONArray("roots")?.length() ?: 0) > 0 && rootKey == null

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
    fun hasRecovery(): Boolean = (answer?.getJSONArray("roots")?.length() ?: 0) > 0

    /** Admins: turns encryption on for a folder ([keyVersion], its newest version, or null), with its key's next version. The folder as the server gives it. */
    @Synchronized
    fun encryptFolder(folder: String, keyVersion: Int?): JSONObject {
        val key = newFolderKey(folder, (keyVersion ?: 0) + 1)
        val info = api.call("PUT", "/api/folders/$folder/encryption", JSONObject().put("encrypted", true).put("key", key))
        account?.let { sync(it) }
        return info
    }

    /** Admins: turns encryption off for a folder, with the root's plain statement for its newest version ([keyVersion]) and its [name]. The folder as the server gives it. */
    @Synchronized
    fun switchOff(folder: String, keyVersion: Int?, name: String): JSONObject {
        val info = api.call("PUT", "/api/folders/$folder/encryption", JSONObject().put("encrypted", false).put("plain_signature", plainSignature(folder, keyVersion ?: 0, name)))
        account?.let { sync(it) }
        return info
    }

    /** Admins: the body of a new folder (contract/api/folder_create.json): once there is a root, under an id picked here, signed as plain. */
    fun newFolder(name: String): JSONObject {
        if (!hasRecovery()) return JSONObject().put("name", name)
        val id = UUID.randomUUID().toString()
        return JSONObject().put("id", id).put("name", name).put("plain_signature", plainSignature(id, 0, name))
    }

    /** Admins: the body of a new name for [folder] (as GET /api/folders describes it), signed anew for one that sends plain. */
    fun renamed(folder: JSONObject, name: String): JSONObject {
        val body = JSONObject().put("name", name)
        if (folder.optBoolean("encrypted") || !hasRecovery()) return body
        val version = if (folder.isNull("key_version")) 0 else folder.optInt("key_version")
        return body.put("plain_signature", plainSignature(folder.getString("id"), version, name))
    }

    /**
     * Admins: a new recovery key; returns its code, which is shown once. The first signs every
     * folder as plain; a later one is signed by the one before, which this phone holds, and signs
     * anew all it signed, after checking each signature.
     */
    @Synchronized
    fun makeRecovery(): String {
        val me = account ?: throw E2eeException("nobody signed in")
        val p = person ?: throw E2eeException("the person's key isn't open here")
        val r = api.call("GET", "/api/recovery", null)
        val chain = r.getJSONArray("roots")
        val old = if (chain.length() > 0) rootKey else null
        if (chain.length() > 0 && (old == null || chain.getJSONObject(chain.length() - 1).getString("public_key") != b64u(old.publicKey))) throw NeedsRoot()
        val (secret, code) = E2ee.newRecoveryCode()
        val pair = Hpke.generateKeyPair()
        val sign = r.getJSONObject("sign")
        val keys = JSONArray()
        for (f in sign.getJSONArray("folder_keys").objects()) {
            val m = E2ee.folderKeyMessage(f.getString("folder"), f.getInt("version"), E2ee.fromB64u(f.getString("public_key")))
            if (!E2ee.verify(old!!.publicKey, m, E2ee.fromB64u(f.getString("signature")))) throw E2eeException("${f.getString("folder")}:${f.getInt("version")} isn't signed by the recovery key")
            keys.put(JSONObject().put("folder", f.getString("folder")).put("version", f.getInt("version")).put("signature", b64u(E2ee.sign(pair.privateKey, m))))
        }
        val plain = JSONArray()
        for (f in sign.getJSONArray("plain").objects()) {
            val m = E2ee.plainMessage(f.getString("folder"), f.getInt("version"), f.getString("name"))
            if (old != null && !E2ee.verify(old.publicKey, m, E2ee.fromB64u(f.str("signature") ?: ""))) throw E2eeException("${f.getString("name")} isn't signed by the recovery key")
            plain.put(JSONObject().put("folder", f.getString("folder")).put("signature", b64u(E2ee.sign(pair.privateKey, m))))
        }
        api.call(
            "PUT", "/api/recovery",
            JSONObject()
                .put("public_key", b64u(pair.publicKey))
                .put("locked", b64u(E2ee.lock(E2ee.secretKey(secret, E2ee.PURPOSE_RECOVERY), E2ee.recoveryContext, pair.privateKey)))
                .put("signature", old?.let { b64u(E2ee.sign(it.privateKey, E2ee.rootMessage(pair.publicKey))) } ?: JSONObject.NULL)
                .put("sealed", b64u(E2ee.sealKey(p.publicKey, E2ee.PURPOSE_ROOT, E2ee.rootContext, pair.privateKey)))
                .put("folder_keys", keys)
                .put("plain", plain),
        )
        adoptRoot(me, pair.publicKey, JSONArray())
        rootKey = pair
        sync(me)
        return code
    }

    /**
     * Admins: opens every encrypted folder with the recovery code and keeps its keys for this
     * person, and the recovery key's private key, which this phone trusts from now on; starts over
     * first when this phone has no person key. Returns how many versions of folder keys it opened;
     * throws an [E2eeException] for a wrong code.
     */
    @Synchronized
    fun useRecoveryCode(code: String): Int {
        val me = account ?: throw E2eeException("nobody signed in")
        val secret = E2ee.parseRecoveryCode(code)
        val r = api.call("GET", "/api/recovery", null)
        val locked = r.str("locked") ?: throw E2eeException("no recovery key")
        val public = E2ee.fromB64u(r.str("public_key") ?: throw E2eeException("no recovery key"))
        val raw = E2ee.unlock(E2ee.secretKey(secret, E2ee.PURPOSE_RECOVERY), E2ee.recoveryContext, E2ee.fromB64u(locked))
        if (!Hpke.publicKey(raw).contentEquals(public)) throw E2eeException("not the recovery key")
        val recovery = KeyPair(raw, public)
        if (person == null) startOver()
        val p = person ?: throw E2eeException("the person's key isn't open here")
        adoptRoot(me, public, r.getJSONArray("roots"))
        rootKey = recovery
        val people = ArrayList<JSONObject>()
        for (f in r.getJSONArray("folders").objects()) {
            val folder = f.getString("folder")
            val version = f.getInt("version")
            val aad = E2ee.folderContext(folder, version)
            val pub = E2ee.fromB64u(f.getString("public_key"))
            if (!E2ee.verify(public, E2ee.folderKeyMessage(folder, version, pub), E2ee.fromB64u(f.getString("signature")))) continue
            try {
                val folderRaw = E2ee.openKey(recovery.privateKey, recovery.publicKey, E2ee.PURPOSE_FOLDER, aad, E2ee.fromB64u(f.getString("sealed")))
                if (!Hpke.publicKey(folderRaw).contentEquals(pub)) continue
                val sealed = E2ee.sealKey(p.publicKey, E2ee.PURPOSE_FOLDER, aad, folderRaw)
                people += JSONObject().put("folder", folder).put("version", version).put("user", me.userId).put("sealed", b64u(sealed))
            } catch (e: E2eeException) {
                // sealed for an older recovery key
            }
        }
        val roots = if (me.admin) listOf(JSONObject().put("user", me.userId).put("sealed", b64u(E2ee.sealKey(p.publicKey, E2ee.PURPOSE_ROOT, E2ee.rootContext, raw)))) else emptyList()
        api.call("POST", "/api/keys/grants", grants(people = people, roots = roots))
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
        keep(me, pins.copy(person = b64u(p.publicKey)))
        person = p
        sync(me)
    }

    /** The root this phone trusts, locked with a link's secret, for those who join with it. */
    private fun lockedRoot(key: ByteArray): String? = root?.takeIf { !pins.tofu }?.let { b64u(E2ee.lock(key, E2ee.rootContext, it)) }

    /** Every version of the keys of [only] these folders (every encrypted one with null), and the root, locked with a new secret for an invite's link: the secret, the keys and the root. */
    fun inviteKeys(only: List<String>?): Triple<String, JSONArray, String?> {
        val secret = randomBytes(32)
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_INVITE)
        val keys = JSONArray()
        for ((id, pair) in folders) {
            val folder = id.substringBeforeLast(':')
            val version = id.substringAfterLast(':').toInt()
            if (only != null && folder !in only) continue
            keys.put(JSONObject().put("folder", folder).put("version", version).put("locked", b64u(E2ee.lock(key, E2ee.folderContext(folder, version), pair.privateKey))))
        }
        return Triple(b64u(secret), keys, lockedRoot(key))
    }

    /** This person's own key and the root, locked with a new secret for the link of an invite for another of their phones or browsers: the secret, the lock and the root. */
    fun personKeyForInvite(): Triple<String, String, String?>? {
        val p = person ?: return null
        val me = account ?: return null
        val secret = randomBytes(32)
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_INVITE)
        return Triple(b64u(secret), b64u(E2ee.lock(key, E2ee.personContext(me.userId), p.privateKey)), lockedRoot(key))
    }

    /** What the link of a PIN that shows an encrypted folder needs: a new secret, and the body to create the PIN with (contract/api/pin_create.json): the secret locked with a key from the folder's newest key, and the root and every version of the folder's key locked with it. */
    fun pinSecret(folder: String): Pair<String, JSONObject>? {
        val a = answer ?: return null
        val newest = try {
            newestSigned(a, folder)
        } catch (e: SendRefused) {
            null
        } ?: return null
        val holder = folders[slot(folder, newest.version)] ?: return null
        val r = root ?: return null
        val secret = randomBytes(32)
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_PIN)
        val locked = E2ee.lock(E2ee.pinSecretKey(holder.privateKey), E2ee.folderContext(folder, newest.version), secret)
        val keys = JSONArray()
        for ((id, pair) in folders) {
            if (id.substringBeforeLast(':') != folder) continue
            val version = id.substringAfterLast(':').toInt()
            keys.put(JSONObject().put("version", version).put("locked", b64u(E2ee.lock(key, E2ee.folderContext(folder, version), pair.privateKey))))
        }
        return b64u(secret) to JSONObject().put("locked", b64u(locked)).put("version", newest.version).put("root", b64u(E2ee.lock(key, E2ee.rootContext, r))).put("keys", keys)
    }

    /** The secret of the link of a PIN that shows an encrypted folder, opened with a key from the folder's key; null without that key. */
    fun pinLinkSecret(folder: String, locked: String, version: Int): String? {
        val pair = folders[slot(folder, version)] ?: return null
        return try {
            b64u(E2ee.unlock(E2ee.pinSecretKey(pair.privateKey), E2ee.folderContext(folder, version), E2ee.fromB64u(locked)))
        } catch (e: E2eeException) {
            null
        }
    }

    /** What follows the code in the link of a PIN that only sends into a folder with keys: the root's fingerprint, which guests check the folder's key with; null otherwise. */
    fun pinLinkRoot(folder: String): String? {
        val r = root ?: return null
        if (pins.tofu || answer?.getJSONArray("folders")?.objects()?.none { it.getString("folder") == folder } != false) return null
        return b64u(E2ee.fingerprint(r))
    }

    /** Admins moving encrypted [files] into [target]: their keys, sealed for the target's newest key, which the root must have signed (contract/api/files_move.json). */
    fun moveKeys(files: List<JSONObject>, target: String): JSONArray {
        val out = JSONArray()
        var newest: FolderPublicKey? = null
        for (f in files) {
            val sealed = SealedFile.of(f) ?: continue
            if (sealed.folder == target) continue
            if (newest == null) newest = newestSigned(answer ?: throw E2eeException("the keys aren't loaded"), target) ?: throw E2eeException("the folder was never encrypted")
            val key = fileKey(sealed)
            val resealed = E2ee.sealKey(newest.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext(target, newest.version), key)
            out.put(JSONObject().put("id", sealed.id).put("version", newest.version).put("key", b64u(resealed)))
        }
        return out
    }

    /** A PIN guest's keys: the folder's keys and the root, locked with the secret of the PIN's link, from [pinApi] with the PIN's key. How many opened. */
    fun openPinKeys(pinApi: KeysApi, secret: String): Int {
        val k = pinApi.call("GET", "/api/pin/keys", null)
        val folder = k.getString("folder")
        val key = E2ee.secretKey(E2ee.fromB64u(secret), E2ee.PURPOSE_PIN)
        guestRoot = k.str("root")?.let {
            try {
                E2ee.unlock(key, E2ee.rootContext, E2ee.fromB64u(it))
            } catch (e: E2eeException) {
                null
            }
        }
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
        guestRoot = null
        changed()
    }

    /**
     * Sets up the keys of a phone that just accepted an invite, with the keys its link's
     * [secret] unlocks: the person's own (a new phone of someone), or folder keys for a new
     * person, which get sealed for their new person key; and the [lockedRoot] to trust. Without a
     * secret, or with a wrong one, the phone waits for the others like any new one. [sync] comes
     * after.
     */
    @Synchronized
    fun fromInvite(me: Account, secret: String?, keys: List<JSONObject>, lockedRoot: String?) {
        if (account != me) forget()
        account = me
        val dev = deviceKey(me)
        api.call("PUT", "/api/keys/device", JSONObject().put("public_key", b64u(dev.publicKey)))
        if (secret == null) return
        val key = E2ee.secretKey(E2ee.fromB64u(secret), E2ee.PURPOSE_INVITE)
        val given = lockedRoot?.let {
            try {
                E2ee.unlock(key, E2ee.rootContext, E2ee.fromB64u(it))
            } catch (e: E2eeException) {
                null
            }
        }
        var kept = store.pins(me.deviceId)
        if (given != null) kept = kept.copy(root = b64u(given), tofu = false)
        val context = E2ee.personContext(me.userId)
        val own = keys.firstOrNull { it.isNull("folder") }
        if (own != null) {
            val raw = E2ee.unlock(key, context, E2ee.fromB64u(own.getString("locked")))
            if (!Hpke.publicKey(raw).contentEquals(E2ee.fromB64u(own.getString("public_key")))) return
            keep(me, kept.copy(person = own.getString("public_key")))
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
        keep(me, kept.copy(person = b64u(p.publicKey)))
        val people = ArrayList<JSONObject>()
        for (k in keys) {
            val folder = k.optString("folder").takeIf { !k.isNull("folder") && it.isNotEmpty() } ?: continue
            val version = k.getInt("version")
            val aad = E2ee.folderContext(folder, version)
            val pub = E2ee.fromB64u(k.getString("public_key"))
            // Only keys the root the link brought signed.
            val signature = k.str("signature") ?: continue
            if (given == null || !E2ee.verify(given, E2ee.folderKeyMessage(folder, version, pub), E2ee.fromB64u(signature))) continue
            try {
                val raw = E2ee.unlock(key, aad, E2ee.fromB64u(k.getString("locked")))
                if (!Hpke.publicKey(raw).contentEquals(pub)) continue
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
        "lacks_root" to lacksRoot(),
        "encrypted_folders" to (answer?.getJSONArray("folders")?.objects()?.map { it.getString("folder") }?.distinct()?.size ?: 0),
        "open" to (folders.keys + pinFolders.keys).sorted(),
        "asks" to asks.map { askMap(it) },
        "codes" to codes.map { mapOf("kind" to it.kind, "from" to it.from, "code" to it.code) },
        "waits_for_folders" to waitsForFolders(),
        "pace" to pace(),
    )

    private fun askMap(it: Ask) = mapOf(
        "kind" to it.kind, "id" to it.id, "name" to it.name, "client" to it.client, "since" to it.since,
        "folders" to it.folders, "root" to it.root, "code" to it.code, "key_changed" to keyChanged(it),
    )

    companion object {
        private val random = SecureRandom()

        private fun slot(folder: String, version: Int) = "$folder:$version"

        private fun randomBytes(n: Int) = ByteArray(n).also { random.nextBytes(it) }

        private fun b64u(bytes: ByteArray) = E2ee.b64u(bytes)

        /** The newest root the chain leads to from [root], each signed by the one before; [root] itself where the chain doesn't name it. */
        fun follow(roots: JSONArray, root: ByteArray): ByteArray {
            val chain = roots.objects()
            val at = chain.indexOfFirst { it.getString("public_key") == b64u(root) }
            if (at < 0) return root
            var newest = root
            for (r in chain.drop(at + 1)) {
                val next = E2ee.fromB64u(r.getString("public_key"))
                val sig = r.str("signature") ?: break
                if (!E2ee.verify(newest, E2ee.rootMessage(next), E2ee.fromB64u(sig))) break
                newest = next
            }
            return newest
        }

        /**
         * A PIN guest's key to encrypt for, from its [session] (contract/api/session.json). A link
         * that names the root ([fingerprint], or the [root] the secret of a PIN that shows its
         * folder opened) takes the folder's newest key only signed by the newest root the chain
         * leads to from it, and always encrypts: [SendRefused] otherwise. Without one ([anchored]
         * false), the server's word counts: encrypted while the folder says so, plain otherwise.
         */
        fun guestKey(session: JSONObject, anchored: Boolean, fingerprint: String?, root: ByteArray?): FolderPublicKey? {
            val k = session.optJSONObject("folder_key")
            if (!anchored) return k?.takeIf { it.optBoolean("encrypted") }?.let { FolderPublicKey(it.getString("folder"), it.getInt("version"), E2ee.fromB64u(it.getString("public_key"))) }
            val roots = session.optJSONArray("roots") ?: JSONArray()
            val named = root ?: fingerprint?.let { fp -> roots.objects().map { E2ee.fromB64u(it.getString("public_key")) }.firstOrNull { b64u(E2ee.fingerprint(it)) == fp } }
            if (named == null || k == null) throw SendRefused("the link's root isn't there, or the folder has no key")
            val newest = follow(roots, named)
            val folder = k.getString("folder")
            val version = k.getInt("version")
            val pub = E2ee.fromB64u(k.getString("public_key"))
            if (!E2ee.verify(newest, E2ee.folderKeyMessage(folder, version, pub), E2ee.fromB64u(k.str("signature") ?: ""))) throw SendRefused("the folder's key isn't signed by the link's root")
            return FolderPublicKey(folder, version, pub)
        }

        private fun grants(
            devices: List<JSONObject> = emptyList(),
            people: List<JSONObject> = emptyList(),
            recovery: List<JSONObject> = emptyList(),
            pins: List<JSONObject> = emptyList(),
            roots: List<JSONObject> = emptyList(),
        ) = JSONObject().put("devices", JSONArray(devices)).put("people", JSONArray(people)).put("recovery", JSONArray(recovery)).put("pins", JSONArray(pins)).put("roots", JSONArray(roots))
    }
}

/** A string field, or null when it is missing, null or empty. */
internal fun JSONObject.str(name: String): String? = if (isNull(name)) null else optString(name).ifEmpty { null }

internal fun JSONArray.objects(): List<JSONObject> = List(length()) { getJSONObject(it) }

internal fun JSONArray.strings(): List<String> = List(length()) { getString(it) }
