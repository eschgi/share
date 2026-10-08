package com.eschgi.share.e2ee

import android.content.Context
import android.util.Log
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.readLimited
import com.eschgi.share.transfer.Credentials
import org.json.JSONException
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.IOException
import java.util.concurrent.CopyOnWriteArraySet

/**
 * This phone's [Keyring], one for the process, wired to Android: the device key in the
 * [SecretStore] (under a key that never leaves the KeyStore), the server over the route
 * RouteMonitor picked, and who is signed in from what the Dart side stored. Downloads, uploads
 * and the player use it without Flutter; after a restart it opens the keys again by itself.
 */
object Keys {
    private const val TAG = "Keys"

    /** The device key, as JSON: the device's id, and the key pair in base64url. */
    private const val DEVICE_KEY = "e2ee_device_key"

    /** The secret of the link of a PIN that shows an encrypted folder, while the PIN works here. */
    private const val PIN_SECRET = "e2ee_pin_secret"

    /** The root's fingerprint the link of a PIN that only sends into a folder with keys carried, while the PIN works here. */
    private const val PIN_ROOT = "e2ee_pin_root"

    /** The people's keys checked on this phone, as JSON: the device's id, and user id to public key. */
    private const val TRUSTED = "e2ee_trusted"

    /** What this phone keeps of the keys (Pins), as JSON: the device's id, and the pins. */
    private const val PINS = "e2ee_pins"

    /** Who is signed in, as lib/data/session.dart stores it. */
    private const val USER = "user"

    @Volatile private var ring: Keyring? = null

    /** When a missing folder key last made the keys be asked for again. */
    @Volatile private var lookedAt = 0L
    private val listeners = CopyOnWriteArraySet<(Map<String, Any?>) -> Unit>()

    fun listen(l: (Map<String, Any?>) -> Unit) {
        listeners += l
    }

    fun unlisten(l: (Map<String, Any?>) -> Unit) {
        listeners -= l
    }

    fun ring(context: Context): Keyring = ring ?: synchronized(this) {
        ring ?: run {
            val app = context.applicationContext
            Keyring(ServerKeysApi(app, Credentials.DEVICE), SecretKeyStore(app)).also { r ->
                r.onChange = {
                    val s = r.state()
                    listeners.forEach { it(s) }
                }
                ring = r
            }
        }
    }

    /** Who the phone is signed in as; null when signed out. */
    fun account(context: Context): Account? {
        val user = try {
            JSONObject(SecretStore(context).read(USER) ?: return null)
        } catch (e: JSONException) {
            return null
        }
        val id = user.optString("id")
        val device = ServerStore(context).config()?.deviceId
        if (id.isEmpty() || device.isNullOrEmpty()) return null
        return Account(id, device, admin = user.optString("role") == "admin")
    }

    /** Opens the keys, or opens them again and does what is due; with [password] right after signing in with it, [quiet] for a check-in (Keyring.sync). The state. */
    fun sync(context: Context, password: String? = null, quiet: Boolean = false): Map<String, Any?> {
        val r = ring(context)
        val me = account(context)
        if (me == null) {
            r.forget()
            return r.state()
        }
        try {
            r.sync(me, password, quiet)
        } catch (e: IOException) {
            Log.i(TAG, "keys: $e")
        } catch (e: JSONException) {
            Log.w(TAG, "keys: the server's answer can't be read", e)
        }
        return r.state()
    }

    /**
     * The key of an encrypted file, for a download, a copy or the player; [auth] says with whose
     * key it is fetched. Opens the keys first if this process hasn't yet. Throws an
     * [E2eeException] while its folder's key isn't here.
     */
    fun fileKey(context: Context, auth: String, file: SealedFile): ByteArray {
        val r = ring(context)
        // A key that isn't open here may have come meanwhile: ask again, but not for every file.
        if (!r.hasFolderKey(file.folder, file.version) && System.currentTimeMillis() - lookedAt > 15_000) {
            lookedAt = System.currentTimeMillis()
            if (auth == Credentials.PIN) {
                SecretStore(context).read(PIN_SECRET)?.let { secret -> runCatching { r.openPinKeys(ServerKeysApi(context.applicationContext, Credentials.PIN), secret) } }
            } else {
                account(context)?.let { me -> runCatching { r.sync(me, quiet = true) } }
            }
        }
        return r.fileKey(file)
    }

    /** How an encrypted file's bytes are decrypted; null for a plain file. Throws an [E2eeException] while its folder's key isn't here. */
    fun cipher(context: Context, auth: String, file: SealedFile?): ContentCipher? =
        file?.let { ContentCipher(fileKey(context, auth, it), E2ee.fromB64u(it.header), it.plainSize) }

    /**
     * An encrypted file's thumbnail, opened: a JPEG of at most 1024 pixels a side. The server
     * can't look into a sealed one, so its size is checked here, before the app draws it.
     */
    fun thumb(context: Context, auth: String, file: SealedFile, sealed: ByteArray): ByteArray {
        val jpeg = E2ee.openThumb(fileKey(context, auth, file), sealed)
        val size = jpegSize(jpeg) ?: throw E2eeException("not a thumbnail")
        if (size.first > MAX_THUMB_SIDE || size.second > MAX_THUMB_SIDE) throw E2eeException("not a thumbnail")
        return jpeg
    }

    /** A whole encrypted file, decrypted: [data] as stored, from its header on (a photo for the viewer). */
    fun decrypt(context: Context, auth: String, file: SealedFile, data: ByteArray): ByteArray {
        val cipher = cipher(context, auth, file) ?: throw E2eeException("not encrypted")
        if (data.size < E2ee.HEADER_SIZE || !data.copyOfRange(0, E2ee.HEADER_SIZE).contentEquals(cipher.header)) throw E2eeException("another header")
        return DecryptingStream(ByteArrayInputStream(data, E2ee.HEADER_SIZE, data.size - E2ee.HEADER_SIZE), cipher).readBytes()
    }

    /** The biggest thumbnail anyone makes: 512 pixels a side today, 1024 at most. */
    private const val MAX_THUMB_SIDE = 1024

    /** The width and height a JPEG says it has in its frame header; null for anything else. */
    internal fun jpegSize(b: ByteArray): Pair<Int, Int>? {
        fun at(i: Int) = b[i].toInt() and 0xff
        if (b.size < 4 || at(0) != 0xff || at(1) != 0xd8) return null
        var i = 2
        while (i + 9 < b.size) {
            if (at(i) != 0xff) return null
            val marker = at(i + 1)
            if (marker == 0xd8 || marker == 0x01 || marker in 0xd0..0xd7) {
                i += 2
                continue
            }
            val length = (at(i + 2) shl 8) or at(i + 3)
            // Start of frame: baseline, progressive and the rest, but not DHT (c4), JPG (c8), DAC (cc).
            if (marker in 0xc0..0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc) {
                return ((at(i + 7) shl 8) or at(i + 8)) to ((at(i + 5) shl 8) or at(i + 6))
            }
            i += 2 + length
        }
        return null
    }

    /**
     * What the link of the PIN just unlocked carries after its code, kept while the PIN works
     * here: the [secret] of one that shows an encrypted folder, which opens it, or the [root]'s
     * fingerprint of one that only sends into a folder with keys; both name the root its uploads
     * check the folder's key with. A PIN typed without either forgets them. How many versions of
     * the folder's key opened.
     */
    fun pinLink(context: Context, secret: String?, root: String?): Int {
        val store = SecretStore(context)
        store.write(PIN_SECRET, secret)
        store.write(PIN_ROOT, root)
        ring(context).forgetPin()
        return if (secret == null) 0 else ring(context).openPinKeys(ServerKeysApi(context.applicationContext, Credentials.PIN), secret)
    }

    /** The PIN's keys go, with what its link carried; e.g. when the PIN ended or another one came. */
    fun forgetPin(context: Context) {
        pinLink(context, null, null)
    }

    /**
     * What a new file into [folder] (as GET /api/folders describes it) is sealed for, someone
     * signed in: after a check-in, the folder's newest key signed by the root this phone trusts,
     * or null for plain where the root's plain statement allows it. Throws [SendRefused] when
     * nothing may go in.
     */
    fun sendKey(context: Context, folder: JSONObject): FolderPublicKey? {
        val r = ring(context)
        val me = account(context) ?: throw IOException("signed out")
        r.sync(me, quiet = true)
        return r.sendKey(folder)
    }

    /** What a PIN guest's new file is sealed for, from the PIN's [session]: checked with the root its link named, if it named one (Keyring.guestKey). */
    fun guestKey(context: Context, session: JSONObject): FolderPublicKey? {
        val store = SecretStore(context)
        val fingerprint = store.read(PIN_ROOT)
        val secret = store.read(PIN_SECRET)
        val r = ring(context)
        if (secret != null && r.guestRoot == null) reopenPin(context)
        return Keyring.guestKey(session, fingerprint != null || secret != null, fingerprint, r.guestRoot)
    }

    /** The PIN guest's keys again, after the app started anew; how many opened. */
    fun reopenPin(context: Context): Int {
        val secret = SecretStore(context).read(PIN_SECRET) ?: return 0
        return try {
            ring(context).openPinKeys(ServerKeysApi(context.applicationContext, Credentials.PIN), secret)
        } catch (e: IOException) {
            Log.i(TAG, "PIN keys: $e")
            0
        }
    }

    /** The device key in the [SecretStore]: one, for the device id the phone has now; so are the people's keys checked here. */
    private class SecretKeyStore(private val app: Context) : DeviceKeyStore {
        override fun load(deviceId: String): KeyPair? {
            val raw = SecretStore(app).read(DEVICE_KEY) ?: return null
            return try {
                val j = JSONObject(raw)
                if (j.optString("device") != deviceId) null else KeyPair(E2ee.fromB64u(j.getString("private")), E2ee.fromB64u(j.getString("public")))
            } catch (e: JSONException) {
                null
            } catch (e: E2eeException) {
                null
            }
        }

        override fun save(deviceId: String, pair: KeyPair) {
            val j = JSONObject().put("device", deviceId).put("private", E2ee.b64u(pair.privateKey)).put("public", E2ee.b64u(pair.publicKey))
            SecretStore(app).write(DEVICE_KEY, j.toString())
        }

        override fun trusted(deviceId: String): Map<String, String> {
            val raw = SecretStore(app).read(TRUSTED) ?: return emptyMap()
            return try {
                val j = JSONObject(raw)
                if (j.optString("device") != deviceId) return emptyMap()
                val keys = j.getJSONObject("keys")
                keys.keys().asSequence().associateWith { keys.getString(it) }
            } catch (e: JSONException) {
                emptyMap()
            }
        }

        override fun trust(deviceId: String, trusted: Map<String, String>) {
            SecretStore(app).write(TRUSTED, JSONObject().put("device", deviceId).put("keys", JSONObject(trusted)).toString())
        }

        override fun pins(deviceId: String): Pins {
            val raw = SecretStore(app).read(PINS) ?: return Pins()
            return try {
                val j = JSONObject(raw)
                if (j.optString("device") != deviceId) Pins() else Pins.parse(j.optJSONObject("pins"))
            } catch (e: JSONException) {
                Pins()
            }
        }

        override fun keepPins(deviceId: String, pins: Pins) {
            SecretStore(app).write(PINS, JSONObject().put("device", deviceId).put("pins", pins.toJson()).toString())
        }
    }
}

/**
 * The keys API over [ServerConnection], with [auth]'s key: the phone's over the route it
 * picked, a PIN's over the public address. A read that fails at home is tried once more over the
 * public address, and one that fails over the public address once more at home when the local
 * address answers again, as the Dart side does.
 */
class ServerKeysApi(private val app: Context, private val auth: String) : KeysApi {
    override fun call(method: String, path: String, body: JSONObject?): JSONObject {
        val (token, config) = Credentials.of(app, auth) ?: throw IOException(if (auth == Credentials.PIN) "no PIN" else "signed out")
        val server = ServerConnection(config, token)
        val home = Credentials.atHome(auth, RouteMonitor.settled(app).isLocal)
        return try {
            send(server, home, method, path, body)
        } catch (e: KeysApiError) {
            throw e
        } catch (e: IOException) {
            if (auth != Credentials.DEVICE || method != "GET") throw e
            val local = RouteMonitor.check(app).isLocal
            if (!home && !local) throw e
            send(server, !home, method, path, body)
        }
    }

    private fun send(server: ServerConnection, home: Boolean, method: String, path: String, body: JSONObject?): JSONObject {
        val conn = server.open(path, home, method = method)
        try {
            if (body != null) {
                val bytes = body.toString().toByteArray()
                conn.setRequestProperty("Content-Type", "application/json")
                conn.doOutput = true
                conn.outputStream.use { it.write(bytes) }
            }
            val status = conn.responseCode
            if (status !in 200..299) {
                val text = try {
                    conn.errorStream?.use { String(readLimited(it, 64 * 1024)) } ?: ""
                } catch (e: IOException) {
                    ""
                }
                val error = try {
                    JSONObject(text).optJSONObject("error")
                } catch (e: JSONException) {
                    null
                }
                throw KeysApiError(status, error?.optString("code")?.ifEmpty { null } ?: "unknown", error?.optString("message") ?: "")
            }
            if (status == 204) return JSONObject()
            val text = String(readLimited(conn.inputStream, 8 * 1024 * 1024))
            return if (text.isBlank()) JSONObject() else JSONObject(text)
        } catch (e: JSONException) {
            throw IOException("the server's answer to $method $path can't be read", e)
        } finally {
            conn.disconnect()
        }
    }
}
