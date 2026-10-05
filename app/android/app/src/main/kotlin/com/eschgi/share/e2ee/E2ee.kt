package com.eschgi.share.e2ee

import java.io.ByteArrayInputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.security.SecureRandom
import java.util.Base64

/**
 * Something that can't be opened or isn't what it should be: a key or a lock made for something
 * else, a changed or cut-short file, a malformed key or code. An IOException, so a stream that
 * meets one fails like a broken read.
 */
class E2eeException(message: String, cause: Throwable? = null) : IOException(message, cause)

/**
 * Share's end-to-end encryption (docs/e2ee-plan.md, contract/crypto), byte for byte as the
 * server's internal/e2ee and the website's src/e2ee make it: keys sealed for a key pair, locks
 * with a key, a link's secret or a password, a file's contents in chunks, thumbnails and the
 * recovery code. Binary values travel in JSON as base64url without padding.
 */
object E2ee {
    // Purposes: HPKE's info when sealing, HKDF's info when deriving a key.

    /** A file's key, sealed for its folder's key. */
    const val PURPOSE_FILE = "share-e2ee-v1/file"

    /** A folder's private key, for a person or the recovery key. */
    const val PURPOSE_FOLDER = "share-e2ee-v1/folder"

    /** A person's private key, for one of their phones or browsers. */
    const val PURPOSE_PERSON = "share-e2ee-v1/person"

    /** A file's contents. */
    const val PURPOSE_CONTENT = "share-e2ee-v1/content"

    /** A file's thumbnail. */
    const val PURPOSE_THUMB = "share-e2ee-v1/thumb"

    /** Keys locked with an invite link's secret. */
    const val PURPOSE_INVITE = "share-e2ee-v1/invite"

    /** Folder keys locked with a PIN link's secret, and that secret sealed for a folder key. */
    const val PURPOSE_PIN = "share-e2ee-v1/pin"

    /** The recovery key, locked with the recovery code. */
    const val PURPOSE_RECOVERY = "share-e2ee-v1/recovery"

    const val FILE_KEY_SIZE = 32

    /** PBKDF2's work for a password lock, as OWASP recommends for HMAC-SHA256. */
    const val PASSWORD_ITERATIONS = 600_000

    /** Plain bytes in each chunk of a file's contents but the last. */
    const val CHUNK_SIZE = 65536

    /** The start of an encrypted file, before its chunks. */
    const val HEADER_SIZE = 16

    private const val SALT_SIZE = 16
    private const val RECOVERY_CODE_SIZE = 20
    private const val RECOVERY_CODE_SYMBOLS = 32
    private const val CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

    private val magic = "SHE1".toByteArray()
    private val none = ByteArray(0)
    private val random = SecureRandom()

    /** What a folder key, and a file key sealed for it, are bound to. */
    fun folderContext(folderId: String, version: Int): ByteArray = "folder:$folderId:$version".toByteArray()

    /** What a person's private key is bound to. */
    fun personContext(userId: String): ByteArray = "person:$userId".toByteArray()

    /** What the recovery key is bound to. */
    val recoveryContext: ByteArray get() = "recovery".toByteArray()

    /** Seals [key] for the holder of [publicKey], for [purpose] and bound to [aad]. */
    fun sealKey(publicKey: ByteArray, purpose: String, aad: ByteArray, key: ByteArray): ByteArray =
        Hpke.seal(publicKey, purpose.toByteArray(), aad, key)

    /** Opens a sealed key with the key pair it was sealed for. */
    fun openKey(privateKey: ByteArray, publicKey: ByteArray, purpose: String, aad: ByteArray, sealed: ByteArray): ByteArray =
        Hpke.open(privateKey, publicKey, purpose.toByteArray(), aad, sealed)

    /** Locks [plaintext] with a 32-byte key, bound to [aad]: a random 12-byte nonce, then the ciphertext. */
    fun lock(key: ByteArray, aad: ByteArray, plaintext: ByteArray): ByteArray {
        val nonce = randomBytes(NONCE_SIZE)
        return nonce + gcmSeal(key, nonce, aad, plaintext)
    }

    /** Opens what [lock] made. */
    fun unlock(key: ByteArray, aad: ByteArray, locked: ByteArray): ByteArray {
        if (locked.size < NONCE_SIZE + TAG_SIZE) throw E2eeException("too short to be locked")
        return gcmOpen(key, locked.copyOfRange(0, NONCE_SIZE), aad, locked, NONCE_SIZE)
    }

    /** The key a link's secret or the recovery code locks with, for [purpose]: HKDF-SHA256 with no salt. */
    fun secretKey(secret: ByteArray, purpose: String): ByteArray = hkdf(secret, none, purpose)

    /**
     * Locks [plaintext] with a password: a salt (16 bytes), the iterations (4 bytes, big endian),
     * then [lock]'s bytes, with PBKDF2-HMAC-SHA256 of the password's UTF-8 bytes as the key.
     */
    fun passwordLock(password: String, aad: ByteArray, plaintext: ByteArray, iterations: Int = PASSWORD_ITERATIONS): ByteArray {
        require(iterations > 0) { "PBKDF2 needs at least one iteration" }
        val salt = randomBytes(SALT_SIZE)
        return salt + u32(iterations.toLong()) + lock(passwordKey(password, salt, iterations), aad, plaintext)
    }

    /** Opens what [passwordLock] made. A lock with fewer than a tenth of today's iterations, or more than ten times as many, isn't tried. */
    fun passwordUnlock(password: String, aad: ByteArray, locked: ByteArray): ByteArray {
        if (locked.size < SALT_SIZE + 4 + NONCE_SIZE + TAG_SIZE) throw E2eeException("too short to be a password lock")
        val iterations = u32(locked, SALT_SIZE)
        if (iterations < PASSWORD_ITERATIONS / 10 || iterations > PASSWORD_ITERATIONS * 10L) throw E2eeException("a password lock needs 60 000 to 6 000 000 iterations")
        val key = passwordKey(password, locked.copyOfRange(0, SALT_SIZE), iterations.toInt())
        return unlock(key, aad, locked.copyOfRange(SALT_SIZE + 4, locked.size))
    }

    /**
     * PBKDF2-HMAC-SHA256 of the password's UTF-8 bytes: one block, 32 bytes. Written out over
     * HMAC rather than taken from SecretKeyFactory, whose PBEKeySpec holds the password as chars
     * and leaves it to the provider how they become bytes.
     */
    private fun passwordKey(password: String, salt: ByteArray, iterations: Int): ByteArray {
        val mac = hmac(password.toByteArray())
        mac.update(salt)
        val u = mac.doFinal(u32(1))
        val key = u.copyOf()
        repeat(iterations - 1) {
            mac.update(u)
            mac.doFinal(u, 0)
            for (j in key.indices) key[j] = (key[j].toInt() xor u[j].toInt()).toByte()
        }
        return key
    }

    // A file's contents: a header, then chunks of CHUNK_SIZE plain bytes (ContentCipher).

    /** A new file's key: 32 random bytes. */
    fun newFileKey(): ByteArray = randomBytes(FILE_KEY_SIZE)

    /** The header of a new encrypted file: "SHE1", the chunk size (4 bytes, big endian), a random nonce prefix of 7 bytes and a zero byte. */
    fun newHeader(): ByteArray {
        val header = ByteArray(HEADER_SIZE)
        magic.copyInto(header)
        u32(CHUNK_SIZE.toLong()).copyInto(header, 4)
        randomBytes(PREFIX_SIZE).copyInto(header, 8)
        return header
    }

    /** A copy of [header] if it is the header of an encrypted file as this version writes it; throws if not. */
    fun parseHeader(header: ByteArray): ByteArray {
        if (header.size != HEADER_SIZE || !header.copyOfRange(0, 4).contentEquals(magic) || u32(header, 4) != CHUNK_SIZE.toLong() || header[HEADER_SIZE - 1] != 0.toByte()) {
            throw E2eeException("not the header of an encrypted file")
        }
        return header.copyOf()
    }

    /** How many chunks [plainSize] bytes take: one at least, as an empty file has one empty chunk. */
    fun chunks(plainSize: Long): Long {
        require(plainSize >= 0) { "a size of $plainSize" }
        return if (plainSize == 0L) 1 else (plainSize + CHUNK_SIZE - 1) / CHUNK_SIZE
    }

    /** The size of [plainSize] bytes once encrypted: the header, the bytes, and a tag for each chunk. */
    fun encryptedSize(plainSize: Long): Long = HEADER_SIZE + plainSize + chunks(plainSize) * TAG_SIZE

    /** Where chunk [i] starts in the encrypted file. */
    fun cipherOffsetOfChunk(i: Long): Long = HEADER_SIZE + i * SEALED_CHUNK_SIZE

    /**
     * Where reading the encrypted file starts for the plain bytes from [plainOffset] on: at the
     * chunk that holds that byte, of which [ReadStart.skip] plain bytes come before it. It doesn't
     * need the file's size; [DecryptingStream] takes what it gives for any offset up to that size.
     */
    fun readStart(plainOffset: Long): ReadStart {
        require(plainOffset >= 0) { "an offset of $plainOffset" }
        val chunk = plainOffset / CHUNK_SIZE
        return ReadStart(chunk, cipherOffsetOfChunk(chunk), (plainOffset - chunk * CHUNK_SIZE).toInt())
    }

    /** The key of a file's chunks: HKDF-SHA256 of the file's key, salted with the header. */
    fun contentKey(fileKey: ByteArray, header: ByteArray): ByteArray = hkdf(fileKey, parseHeader(header), PURPOSE_CONTENT)

    /** The key a file's thumbnail is locked with: HKDF-SHA256 of the file's key, with no salt. */
    fun thumbKey(fileKey: ByteArray): ByteArray = hkdf(fileKey, none, PURPOSE_THUMB)

    /** Locks a file's thumbnail with a key from the file's key, and no aad. */
    fun sealThumb(fileKey: ByteArray, jpeg: ByteArray): ByteArray = lock(thumbKey(fileKey), none, jpeg)

    /** Opens what [sealThumb] made. */
    fun openThumb(fileKey: ByteArray, sealed: ByteArray): ByteArray = unlock(thumbKey(fileKey), none, sealed)

    // The recovery code: 20 random bytes as 32 characters of Crockford's base32, in groups of four.

    /** A new recovery code: its secret, and how it is written. */
    fun newRecoveryCode(): Pair<ByteArray, String> {
        val secret = randomBytes(RECOVERY_CODE_SIZE)
        return secret to formatRecoveryCode(secret)
    }

    /** How a recovery code's secret is written: most significant bits first, groups of four joined by dashes. */
    fun formatRecoveryCode(secret: ByteArray): String {
        require(secret.size == RECOVERY_CODE_SIZE) { "a recovery code holds 20 bytes" }
        val out = StringBuilder()
        var acc = 0
        var bits = 0
        var n = 0
        for (b in secret) {
            acc = ((acc shl 8) or (b.toInt() and 0xff)) and 0xffff
            bits += 8
            while (bits >= 5) {
                if (n > 0 && n % 4 == 0) out.append('-')
                out.append(CROCKFORD[(acc shr (bits - 5)) and 31])
                bits -= 5
                n++
            }
        }
        return out.toString()
    }

    /** The secret of a recovery code as someone types it: small letters, O for 0, I and L for 1, dashes and spaces anywhere. Throws for anything else. */
    fun parseRecoveryCode(text: String): ByteArray {
        val out = ByteArray(RECOVERY_CODE_SIZE)
        var acc = 0
        var bits = 0
        var n = 0
        var at = 0
        for (c in text) {
            if (c == '-' || c.isWhitespace()) continue
            val symbol = when (val upper = c.uppercaseChar()) {
                'O' -> '0'
                'I', 'L' -> '1'
                else -> upper
            }
            val v = CROCKFORD.indexOf(symbol)
            if (v < 0) throw E2eeException("a recovery code has only digits and letters")
            if (++n > RECOVERY_CODE_SYMBOLS) throw E2eeException("a recovery code has 32 characters")
            acc = ((acc shl 5) or v) and 0xffff
            bits += 5
            if (bits >= 8) {
                bits -= 8
                out[at++] = (acc shr bits).toByte()
            }
        }
        if (n != RECOVERY_CODE_SYMBOLS) throw E2eeException("a recovery code has 32 characters")
        return out
    }

    /** Bytes as JSON carries them: base64url without padding. */
    fun b64u(bytes: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)

    /** Bytes from base64url without padding; throws for anything else. */
    fun fromB64u(text: String): ByteArray {
        if (!text.all { it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_' }) throw E2eeException("not base64url")
        return try {
            Base64.getUrlDecoder().decode(text)
        } catch (e: IllegalArgumentException) {
            throw E2eeException("not base64url", e)
        }
    }

    private fun randomBytes(n: Int): ByteArray = ByteArray(n).also { random.nextBytes(it) }

    /** [n]'s low 32 bits, big endian. */
    internal fun u32(n: Long): ByteArray = byteArrayOf((n shr 24).toByte(), (n shr 16).toByte(), (n shr 8).toByte(), n.toByte())

    private fun u32(b: ByteArray, at: Int): Long = (0 until 4).fold(0L) { acc, k -> (acc shl 8) or (b[at + k].toLong() and 0xff) }
}

/** The nonce prefix in a header, after "SHE1" and the chunk size. */
private const val PREFIX_SIZE = 7

/** A whole chunk, encrypted: its plain bytes and the tag. */
private const val SEALED_CHUNK_SIZE = E2ee.CHUNK_SIZE + TAG_SIZE

/**
 * Where reading an encrypted file starts for the plain bytes from an offset on: [chunk], which
 * starts at [cipherOffset] in the encrypted file, and how many of its plain bytes come before the
 * offset ([skip]).
 */
data class ReadStart(val chunk: Long, val cipherOffset: Long, val skip: Int)

/**
 * Encrypts and decrypts one file's chunks (contract/crypto/content.json): AES-256-GCM with
 * [E2ee.contentKey], no aad, and a nonce of the header's prefix, the chunk's number (4 bytes, big
 * endian) and 1 for the last chunk, else 0, so chunks can't be dropped, reordered or cut off
 * unnoticed. Every chunk holds [E2ee.CHUNK_SIZE] plain bytes but the last, which holds the rest:
 * 1 to CHUNK_SIZE, or none in an empty file.
 */
class ContentCipher(fileKey: ByteArray, header: ByteArray, val plainSize: Long) {
    private val headerBytes = E2ee.parseHeader(header)
    private val key = E2ee.contentKey(fileKey, headerBytes)

    /** How many chunks the file has. */
    val chunks = E2ee.chunks(plainSize)

    /** The encrypted file's size. */
    val encryptedSize = E2ee.encryptedSize(plainSize)

    /** The header the encrypted file starts with. */
    val header: ByteArray get() = headerBytes.copyOf()

    init {
        // A nonce holds a chunk's number in 4 bytes.
        require(chunks <= 1L shl 32) { "too big to encrypt" }
    }

    /** How many plain bytes chunk [i] holds. */
    fun plainLength(i: Long): Int {
        require(i in 0 until chunks) { "no chunk $i of $chunks" }
        return if (i < chunks - 1) E2ee.CHUNK_SIZE else (plainSize - i * E2ee.CHUNK_SIZE).toInt()
    }

    /** The plain bytes chunk [i] holds, as offsets in the file. */
    fun plainRange(i: Long): LongRange {
        val start = i * E2ee.CHUNK_SIZE
        return start until start + plainLength(i)
    }

    /** How many bytes chunk [i] takes encrypted: its plain bytes and the tag. */
    fun sealedLength(i: Long): Int = plainLength(i) + TAG_SIZE

    /** Encrypts chunk [i] from [len] bytes of [plain] at [off], which must be all of its plain bytes. */
    fun encryptChunk(i: Long, plain: ByteArray, off: Int = 0, len: Int = plain.size - off): ByteArray {
        require(len == plainLength(i)) { "chunk $i holds ${plainLength(i)} bytes, not $len" }
        return gcmSeal(key, nonce(i), null, plain, off, len)
    }

    /** Decrypts chunk [i] from [len] bytes of [sealed] at [off]; throws an [E2eeException] if they aren't that chunk of this file. */
    fun decryptChunk(i: Long, sealed: ByteArray, off: Int = 0, len: Int = sealed.size - off): ByteArray {
        if (i !in 0 until chunks || len != sealedLength(i)) throw E2eeException("not chunk $i of this file")
        return gcmOpen(key, nonce(i), null, sealed, off, len)
    }

    private fun nonce(i: Long): ByteArray {
        val nonce = ByteArray(NONCE_SIZE)
        headerBytes.copyInto(nonce, 0, 8, 8 + PREFIX_SIZE)
        E2ee.u32(i).copyInto(nonce, PREFIX_SIZE)
        if (i == chunks - 1) nonce[NONCE_SIZE - 1] = 1
        return nonce
    }
}

/**
 * A file's encrypted bytes, made as they are read, from any offset: the header, then the chunks.
 * [plaintext] opens the file at a plain offset, always a chunk's start. The same file key, header
 * and contents always give the same bytes, so a piece sent again after an interruption is the one
 * sent before.
 */
class EncryptingSource(private val cipher: ContentCipher, private val plaintext: (offset: Long) -> InputStream) {
    /** The encrypted file's size. */
    val size: Long get() = cipher.encryptedSize

    /** The encrypted bytes from [offset] on. If the file turns out shorter than its size, a read fails with an EOFException. */
    fun openAt(offset: Long): InputStream {
        require(offset in 0..size) { "offset $offset of $size bytes" }
        if (offset == size) return ByteArrayInputStream(ByteArray(0))
        val chunk = if (offset < E2ee.HEADER_SIZE) 0L else (offset - E2ee.HEADER_SIZE) / SEALED_CHUNK_SIZE
        return Encrypting(plaintext(chunk * E2ee.CHUNK_SIZE), chunk, offset)
    }

    /** The encrypted bytes from [offset] on, which lies in chunk [next] or in the header before it; [input] reads the file from that chunk's start. */
    private inner class Encrypting(private val input: InputStream, private var next: Long, offset: Long) : InputStream() {
        // What is ready to be read: the rest of the header, then one encrypted chunk at a time.
        private var buf = if (offset < E2ee.HEADER_SIZE) cipher.header else ByteArray(0)
        private var pos = if (offset < E2ee.HEADER_SIZE) offset.toInt() else 0

        /** How many bytes of the first chunk come before the offset. */
        private var skip = maxOf(0L, offset - E2ee.cipherOffsetOfChunk(next)).toInt()
        private var plain = ByteArray(0)

        override fun read(): Int {
            val one = ByteArray(1)
            return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xff
        }

        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (off < 0 || len < 0 || len > b.size - off) throw IndexOutOfBoundsException()
            if (len == 0) return 0
            while (pos == buf.size) {
                if (next == cipher.chunks) return -1
                val i = next++
                val length = cipher.plainLength(i)
                if (plain.size < length) plain = ByteArray(length)
                if (readUpTo(input, plain, length) < length) throw EOFException("the file is shorter than it was")
                buf = cipher.encryptChunk(i, plain, 0, length)
                pos = skip
                skip = 0
            }
            val n = minOf(len, buf.size - pos)
            buf.copyInto(b, off, pos, pos + n)
            pos += n
            return n
        }

        override fun available(): Int = buf.size - pos

        override fun close() = input.close()
    }
}

/**
 * The plain bytes of an encrypted file, from a stream of its chunks: [input] starts where chunk
 * [firstChunk] starts, and the first [skip] plain bytes of that chunk are left out
 * ([E2ee.readStart] gives both for a plain offset). Every chunk but the last decrypts fine on its
 * own, so the stream knows where the file ends: a changed chunk, a stream cut short before the
 * file's last chunk, or bytes after it fail the read with an [E2eeException], and every read after.
 */
class DecryptingStream(private val input: InputStream, private val cipher: ContentCipher, firstChunk: Long = 0, skip: Int = 0) : InputStream() {
    private var next = firstChunk
    private var skip = skip
    private var buf = ByteArray(0)
    private var pos = 0
    private var sealed = ByteArray(0)
    private var ended = false
    private var failure: E2eeException? = null

    init {
        require(firstChunk in 0..cipher.chunks) { "no chunk $firstChunk of ${cipher.chunks}" }
        require(skip >= 0 && (skip == 0 || firstChunk < cipher.chunks && skip <= cipher.plainLength(firstChunk))) { "can't skip $skip bytes of chunk $firstChunk" }
    }

    override fun read(): Int {
        val one = ByteArray(1)
        return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xff
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        if (off < 0 || len < 0 || len > b.size - off) throw IndexOutOfBoundsException()
        failure?.let { throw it }
        if (len == 0) return 0
        try {
            while (pos == buf.size) {
                if (next == cipher.chunks) {
                    end()
                    return -1
                }
                val i = next++
                val length = cipher.sealedLength(i)
                if (sealed.size < length) sealed = ByteArray(length)
                if (readUpTo(input, sealed, length) < length) throw E2eeException("the file was cut short")
                buf = cipher.decryptChunk(i, sealed, 0, length)
                pos = skip
                skip = 0
                // Before the last chunk's bytes go out, the stream must end with it.
                if (next == cipher.chunks) end()
            }
        } catch (e: E2eeException) {
            failure = e
            throw e
        }
        val n = minOf(len, buf.size - pos)
        buf.copyInto(b, off, pos, pos + n)
        pos += n
        return n
    }

    private fun end() {
        if (ended) return
        if (input.read() >= 0) throw E2eeException("more than the file")
        ended = true
    }

    override fun available(): Int = buf.size - pos

    override fun close() = input.close()
}

/** Reads [length] bytes into [buf], fewer only where [input] ends; how many it read. */
private fun readUpTo(input: InputStream, buf: ByteArray, length: Int): Int {
    var n = 0
    while (n < length) {
        val r = input.read(buf, n, length - n)
        if (r < 0) break
        n += r
    }
    return n
}
