package com.eschgi.share.transfer

import com.eschgi.share.e2ee.ContentCipher
import com.eschgi.share.e2ee.E2ee
import com.eschgi.share.e2ee.E2eeException
import com.eschgi.share.e2ee.EncryptingSource
import com.eschgi.share.e2ee.FolderPublicKey
import com.eschgi.share.e2ee.SendRefused
import com.eschgi.share.net.readLimited
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection

/**
 * What an upload into an encrypted folder keeps until it is done (docs/e2ee-plan.md), as JSON in
 * its row: the file's key, which never leaves the phone, its header, and what the server gets:
 * the key sealed for version [version] of the folder's key. The same key and header give the
 * same encrypted bytes, so a piece sent again after an interruption is the one sent before;
 * [size] and [lastModified] tell whether the file is still the one the seal was made for.
 */
class UploadSeal(
    val key: ByteArray,
    val header: ByteArray,
    val folder: String,
    val version: Int,
    val sealed: ByteArray,
    val size: Long,
    val lastModified: Long?,
) {
    fun toJson(): String = JSONObject()
        .put("key", E2ee.b64u(key))
        .put("header", E2ee.b64u(header))
        .put("folder", folder)
        .put("version", version)
        .put("sealed", E2ee.b64u(sealed))
        .put("size", size)
        .put("last_modified", lastModified ?: JSONObject.NULL)
        .toString()

    /** The enc the server takes with the upload: tus's metadata, the S3 upload's field (contract/api/s3_upload_create_encrypted.json). */
    fun enc(): JSONObject = JSONObject()
        .put("version", version)
        .put("key", E2ee.b64u(sealed))
        .put("header", E2ee.b64u(header))
        .put("plain_size", size)

    fun cipher() = ContentCipher(key, header, size)

    /** The bytes that go up: the file's encrypted stream. */
    val encryptedSize: Long get() = E2ee.encryptedSize(size)

    /** Whether a file of [size] bytes, last changed at [lastModified], is the one this seal was made for. */
    fun fits(size: Long, lastModified: Long?) = this.size == size && (this.lastModified == null || lastModified == null || this.lastModified == lastModified)

    /** The file's thumbnail, sealed with a key from the file's key. */
    fun sealThumb(jpeg: ByteArray): ByteArray = E2ee.sealThumb(key, jpeg)

    companion object {
        /** A new key and header for a file of [size] bytes, its key sealed for [target], the folder's newest key. */
        fun new(target: FolderPublicKey, size: Long, lastModified: Long?): UploadSeal {
            val key = E2ee.newFileKey()
            val sealed = E2ee.sealKey(target.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext(target.folder, target.version), key)
            return UploadSeal(key, E2ee.newHeader(), target.folder, target.version, sealed, size, lastModified)
        }

        fun parse(json: String?): UploadSeal? {
            if (json.isNullOrEmpty()) return null
            return try {
                val j = JSONObject(json)
                UploadSeal(
                    key = E2ee.fromB64u(j.getString("key")),
                    header = E2ee.fromB64u(j.getString("header")),
                    folder = j.getString("folder"),
                    version = j.getInt("version"),
                    sealed = E2ee.fromB64u(j.getString("sealed")),
                    size = j.getLong("size"),
                    lastModified = if (j.isNull("last_modified")) null else j.getLong("last_modified"),
                )
            } catch (e: JSONException) {
                null
            } catch (e: E2eeException) {
                null
            }
        }

        /** The server's answers that mean a new seal, for another version of the folder's key, or none at all. */
        val RESEAL = setOf("key_outdated", "encryption_required", "not_encrypted")

        /**
         * The key new files into [folder] are encrypted for; null while they go plain. A PIN asks
         * its session (contract/api/session.json), someone signed in the folder list; the keys
         * decide, with the root this phone trusts: [signedIn] for the folder as the list describes
         * it (Keys.sendKey), [guest] for the session (Keys.guestKey). They throw [SendRefused] when
         * nothing may go into the folder. [open] makes a GET with the batch's key and route.
         */
        fun target(
            auth: String,
            folder: String?,
            open: (path: String) -> HttpURLConnection,
            signedIn: (folder: JSONObject) -> FolderPublicKey?,
            guest: (session: JSONObject) -> FolderPublicKey?,
        ): FolderPublicKey? {
            if (auth == Credentials.PIN) return guest(getJson(open, "/api/session"))
            if (folder == null) return null
            val folders = getJson(open, "/api/folders").getJSONArray("folders")
            val info = (0 until folders.length()).map { folders.getJSONObject(it) }.firstOrNull { it.getString("id") == folder } ?: return null
            return signedIn(info)
        }

        private fun getJson(open: (path: String) -> HttpURLConnection, path: String): JSONObject {
            val conn = open(path)
            try {
                val status = conn.responseCode
                if (status != 200) throw HttpFailure(status, path)
                return JSONObject(String(readLimited(conn.inputStream, 8 * 1024 * 1024)))
            } catch (e: JSONException) {
                throw IOException("the server's answer to $path can't be read", e)
            } finally {
                conn.disconnect()
            }
        }
    }

    /** An answer other than 200 while asking which key to encrypt for. */
    class HttpFailure(val status: Int, path: String) : IOException("HTTP $status for $path")
}

/** The encrypted stream of a file read from [plain], from any offset: what goes up for an upload into an encrypted folder. */
class SealedSource(seal: UploadSeal, plain: UploadSource) : UploadSource {
    private val source = EncryptingSource(seal.cipher()) { plain.openAt(it) }

    override fun openAt(offset: Long): InputStream = source.openAt(offset)
}
