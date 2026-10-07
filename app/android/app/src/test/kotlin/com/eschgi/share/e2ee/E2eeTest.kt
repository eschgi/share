package com.eschgi.share.e2ee

import com.eschgi.share.contract
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.EOFException
import java.io.InputStream
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.util.Arrays

/** Share's formats against the contract's vectors (contract/crypto), and the streams that uploads and downloads read. */
class E2eeTest {
    private val content = contract("crypto/content.json")
    private val fileKey = content.bytes("file_key")
    private val header = content.bytes("header")

    // Checks (check.json).

    @Test
    fun checksCommitMakeCodesAndConfirmAsTheVectorsDo() {
        for (c in contract("crypto/check.json").cases("cases")) {
            val name = c.getString("name")
            val (asker, answer, pub) = Triple(c.bytes("asker_key"), c.bytes("answer_key"), c.bytes("public_key"))
            assertEquals(name, c.getString("commitment"), E2ee.b64u(E2ee.commitment(asker)))
            assertEquals(name, c.getString("code"), E2ee.checkCode(asker, answer, pub))
            val asking = E2ee.confirmKey(c.bytes("asker_private_key"), answer, asker, answer, pub)
            val waiting = E2ee.confirmKey(c.bytes("answer_private_key"), asker, asker, answer, pub)
            assertEquals(name, c.getString("confirm_key"), E2ee.b64u(asking))
            assertEquals(name, c.getString("confirm_key"), E2ee.b64u(waiting))
            assertEquals(name, c.getString("confirmed"), String(E2ee.unlock(waiting, E2ee.checkContext(c.getString("check")), c.bytes("confirmation"))))
        }
    }

    // Signatures (sign.json).

    @Test
    fun signaturesOfTheVectorsVerifyAndTheirMessagesAreMadeTheSame() {
        val v = contract("crypto/sign.json")
        val pub = v.bytes("public_key")
        assertEquals(v.getString("fingerprint"), E2ee.b64u(E2ee.fingerprint(pub)))
        for (c in v.cases("verify")) {
            val name = c.getString("name")
            val m = when {
                c.has("folder_key") -> E2ee.folderKeyMessage(c.getString("folder"), c.getInt("version"), c.bytes("folder_key"))
                c.has("folder_name") -> E2ee.plainMessage(c.getString("folder"), c.getInt("version"), c.getString("folder_name"))
                else -> E2ee.rootMessage(c.bytes("new_root"))
            }
            assertEquals(name, c.getString("message"), E2ee.b64u(m))
            assertTrue(name, E2ee.verify(pub, m, c.bytes("signature")))
        }
        for (c in v.cases("refuse")) assertFalse(c.getString("name"), E2ee.verify(pub, c.bytes("message"), c.bytes("signature")))
        // What Kotlin signs is r and s too, and verifies anywhere.
        val m = E2ee.plainMessage("0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d", 0, "Família")
        repeat(20) {
            val sig = E2ee.sign(v.bytes("private_key"), m)
            assertEquals(64, sig.size)
            assertTrue(E2ee.verify(pub, m, sig))
            assertFalse(E2ee.verify(pub, E2ee.plainMessage("0199a0c4-8f6e-7d2a-9b1c-3e5f7a9b1c3d", 0, "Familia"), sig))
        }
        assertFalse(E2ee.verify(pub, m, ByteArray(63)))
        assertFalse(E2ee.verify(pub.copyOfRange(1, 65), m, E2ee.sign(v.bytes("private_key"), m)))
    }

    // Sealed keys (seal.json).

    @Test
    fun sealedKeysOfTheVectorsOpen() {
        val vectors = contract("crypto/seal.json")
        for (c in vectors.cases("open")) {
            val opened = E2ee.openKey(c.bytes("private_key"), c.bytes("public_key"), c.getString("purpose"), c.getString("aad").toByteArray(), c.bytes("sealed"))
            assertEquals(c.getString("name"), c.getString("plaintext"), E2ee.b64u(opened))
            assertEquals(c.getString("name"), c.getString("public_key"), E2ee.b64u(Hpke.publicKey(c.bytes("private_key"))))
        }
        for (c in vectors.cases("refuse")) {
            assertThrows(c.getString("name"), E2eeException::class.java) {
                E2ee.openKey(c.bytes("private_key"), c.bytes("public_key"), c.getString("purpose"), c.getString("aad").toByteArray(), c.bytes("sealed"))
            }
        }
    }

    @Test
    fun keysSealedForFreshKeysOpenOnlyThere() {
        val folder = Hpke.generateKeyPair()
        val other = Hpke.generateKeyPair()
        val aad = E2ee.folderContext("w3dding5x2k7mbqz4bwdbyj6qs", 1)
        val key = E2ee.newFileKey()
        val sealed = E2ee.sealKey(folder.publicKey, E2ee.PURPOSE_FILE, aad, key)
        assertEquals(65 + 32 + 16, sealed.size)
        assertArrayEquals(key, E2ee.openKey(folder.privateKey, folder.publicKey, E2ee.PURPOSE_FILE, aad, sealed))
        assertThrows(E2eeException::class.java) { E2ee.openKey(other.privateKey, other.publicKey, E2ee.PURPOSE_FILE, aad, sealed) }
        assertThrows(E2eeException::class.java) { E2ee.openKey(folder.privateKey, folder.publicKey, E2ee.PURPOSE_FOLDER, aad, sealed) }
        assertThrows(E2eeException::class.java) { E2ee.openKey(folder.privateKey, folder.publicKey, E2ee.PURPOSE_FILE, E2ee.folderContext("w3dding5x2k7mbqz4bwdbyj6qs", 2), sealed) }

        val person = Hpke.generateKeyPair()
        val device = Hpke.generateKeyPair()
        val forDevice = E2ee.sealKey(device.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext("u1"), person.privateKey)
        assertArrayEquals(person.privateKey, E2ee.openKey(device.privateKey, device.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext("u1"), forDevice))
        assertThrows(E2eeException::class.java) { E2ee.openKey(device.privateKey, device.publicKey, E2ee.PURPOSE_PERSON, E2ee.personContext("u2"), forDevice) }
    }

    @Test
    fun contexts() {
        assertEquals("folder:w3dding5x2k7mbqz4bwdbyj6qs:12", String(E2ee.folderContext("w3dding5x2k7mbqz4bwdbyj6qs", 12)))
        assertEquals("person:u7ld5x2k7mbqz4bwdbyj6qsqxa", String(E2ee.personContext("u7ld5x2k7mbqz4bwdbyj6qsqxa")))
        assertEquals("recovery", String(E2ee.recoveryContext))
    }

    // Locks (lock.json).

    @Test
    fun secretLocksOfTheVectorsOpen() {
        for (c in contract("crypto/lock.json").cases("secrets")) {
            val key = E2ee.secretKey(c.bytes("secret"), c.getString("purpose"))
            assertEquals(c.getString("name"), c.getString("key"), E2ee.b64u(key))
            assertEquals(c.getString("name"), c.getString("plaintext"), E2ee.b64u(E2ee.unlock(key, c.getString("aad").toByteArray(), c.bytes("locked"))))
            assertThrows(c.getString("name"), E2eeException::class.java) { E2ee.unlock(key, "person:someone else".toByteArray(), c.bytes("locked")) }
        }
    }

    @Test
    fun passwordLocksOfTheVectorsOpen() {
        // The second password has accents, a check mark and ideographs: PBKDF2 takes its UTF-8 bytes.
        for (c in contract("crypto/lock.json").cases("passwords")) {
            val password = c.getString("password")
            val aad = c.getString("aad").toByteArray()
            assertEquals(password, c.getString("plaintext"), E2ee.b64u(E2ee.passwordUnlock(password, aad, c.bytes("locked"))))
            assertThrows(password, E2eeException::class.java) { E2ee.passwordUnlock("$password ", aad, c.bytes("locked")) }
        }
    }

    @Test
    fun thumbnailOfTheVectorsOpens() {
        val t = contract("crypto/lock.json").getJSONObject("thumb")
        val key = t.bytes("file_key")
        assertEquals(t.getString("thumb_key"), E2ee.b64u(E2ee.thumbKey(key)))
        assertEquals(t.getString("plaintext"), E2ee.b64u(E2ee.openThumb(key, t.bytes("sealed"))))
        val resealed = E2ee.sealThumb(key, t.bytes("plaintext"))
        assertEquals(t.bytes("plaintext").size + 12 + 16, resealed.size)
        assertEquals(t.getString("plaintext"), E2ee.b64u(E2ee.openThumb(key, resealed)))
        assertThrows(E2eeException::class.java) { E2ee.openThumb(E2ee.newFileKey(), resealed) }
    }

    @Test
    fun locksAreBoundToTheirKeyAndAad() {
        val key = E2ee.secretKey("a secret from a link".toByteArray(), E2ee.PURPOSE_INVITE)
        val aad = "person:u1".toByteArray()
        val locked = E2ee.lock(key, aad, "folder key".toByteArray())
        assertEquals(12 + 10 + 16, locked.size)
        assertEquals("folder key", String(E2ee.unlock(key, aad, locked)))
        assertFalse("a new nonce each time", locked.contentEquals(E2ee.lock(key, aad, "folder key".toByteArray())))
        assertThrows(E2eeException::class.java) { E2ee.unlock(key, "person:u2".toByteArray(), locked) }
        assertThrows(E2eeException::class.java) { E2ee.unlock(E2ee.secretKey("a secret from a link".toByteArray(), E2ee.PURPOSE_PIN), aad, locked) }
        assertThrows(E2eeException::class.java) { E2ee.unlock(key, aad, locked.copyOf(27)) }
    }

    @Test
    fun passwordLocksForFreshKeys() {
        val aad = E2ee.personContext("u1")
        val private = Hpke.generateKeyPair().privateKey
        val locked = E2ee.passwordLock("correct horse", aad, private, iterations = 60_000)
        assertEquals(16 + 4 + 12 + 32 + 16, locked.size)
        assertEquals(60_000, ByteBuffer.wrap(locked, 16, 4).int)
        assertArrayEquals(private, E2ee.passwordUnlock("correct horse", aad, locked))
        assertThrows(E2eeException::class.java) { E2ee.passwordUnlock("Correct horse", aad, locked) }
        assertThrows(E2eeException::class.java) { E2ee.passwordUnlock("correct horse", E2ee.personContext("u2"), locked) }
        assertArrayEquals(private, E2ee.passwordUnlock("", aad, E2ee.passwordLock("", aad, private, iterations = 60_000)))

        // Too few iterations to protect anything, or so many that trying would hang: neither is tried.
        val weak = E2ee.passwordLock("correct horse", aad, private, iterations = 1000)
        assertThrows(E2eeException::class.java) { E2ee.passwordUnlock("correct horse", aad, weak) }
        for (n in listOf(59_999, 6_000_001, -1)) {
            val changed = locked.copyOf().also { ByteBuffer.wrap(it, 16, 4).putInt(n) }
            assertThrows("$n iterations", E2eeException::class.java) { E2ee.passwordUnlock("correct horse", aad, changed) }
        }
        assertThrows(E2eeException::class.java) { E2ee.passwordUnlock("correct horse", aad, locked.copyOf(16 + 4 + 27)) }
    }

    // Contents (content.json).

    @Test
    fun contentKeyOfTheVectors() {
        assertArrayEquals(header, E2ee.parseHeader(header))
        assertEquals(content.getString("content_key"), E2ee.b64u(E2ee.contentKey(fileKey, header)))
    }

    @Test
    fun contentsOfTheVectors() {
        for (c in content.cases("cases")) {
            val size = c.getLong("size")
            val name = "$size bytes"
            val plain = plaintext(size.toInt())
            val cipher = ContentCipher(fileKey, header, size)
            val encrypted = EncryptingSource(cipher, opener(plain)).openAt(0).use { it.readBytes() }
            assertEquals(name, c.getLong("encrypted_size"), encrypted.size.toLong())
            assertEquals(name, c.getLong("encrypted_size"), E2ee.encryptedSize(size))
            assertEquals(name, c.getLong("encrypted_size"), cipher.encryptedSize)
            assertEquals(name, c.getString("sha256"), sha256(encrypted))
            if (c.has("encrypted")) assertEquals(name, c.getString("encrypted"), E2ee.b64u(encrypted))
            assertArrayEquals("$name, chunk by chunk", encrypted, byChunks(cipher, plain))

            val decrypted = DecryptingStream(ByteArrayInputStream(encrypted, 16, encrypted.size - 16), cipher).use { it.readBytes() }
            assertArrayEquals(name, plain, decrypted)
            val byChunk = ByteArrayOutputStream()
            for (i in 0 until cipher.chunks) byChunk.write(cipher.decryptChunk(i, encrypted, E2ee.cipherOffsetOfChunk(i).toInt(), cipher.sealedLength(i)))
            assertArrayEquals("$name, chunk by chunk", plain, byChunk.toByteArray())
        }
    }

    @Test
    fun aFreshKeyAndHeader() {
        val key = E2ee.newFileKey()
        val head = E2ee.newHeader()
        val plain = plaintext(150_001)
        val cipher = ContentCipher(key, head, plain.size.toLong())
        val encrypted = EncryptingSource(cipher, opener(plain)).openAt(0).use { it.readBytes() }
        assertEquals(E2ee.encryptedSize(plain.size.toLong()), encrypted.size.toLong())
        assertArrayEquals(head, encrypted.copyOf(16))
        assertArrayEquals(plain, DecryptingStream(ByteArrayInputStream(encrypted, 16, encrypted.size - 16), ContentCipher(key, head, plain.size.toLong())).readBytes())
        assertFalse("another key, other bytes", encrypted.contentEquals(byChunks(ContentCipher(E2ee.newFileKey(), head, plain.size.toLong()), plain)))
    }

    @Test
    fun everyOffsetOfASmallFile() {
        // Two chunks, the second a short one: from every offset, in the header, in either chunk and
        // at their edges, the stream is the rest of the whole encryption, in reads of any size.
        val plain = plaintext(E2ee.CHUNK_SIZE + 300)
        val cipher = ContentCipher(fileKey, header, plain.size.toLong())
        val source = EncryptingSource(cipher, opener(plain))
        val whole = byChunks(cipher, plain)
        assertEquals(whole.size.toLong(), source.size)
        val buf = ByteArray(whole.size + 1)
        for (offset in 0..whole.size) {
            val n = source.openAt(offset.toLong()).use { readAll(it, buf, 4093 + offset % 7) }
            if (n != whole.size - offset || !Arrays.equals(buf, 0, n, whole, offset, whole.size)) fail("from $offset: $n bytes, not the encryption's ${whole.size - offset}")
        }
    }

    @Test
    fun someOffsetsOfALargerFile() {
        val plain = plaintext(200_000)
        val cipher = ContentCipher(fileKey, header, plain.size.toLong())
        val source = EncryptingSource(cipher, opener(plain))
        val whole = byChunks(cipher, plain)
        assertEquals(content.cases("cases").first { it.getLong("size") == 200_000L }.getString("sha256"), sha256(whole))
        val edges = (1L..3L).flatMap { val at = E2ee.cipherOffsetOfChunk(it); listOf(at - 1, at, at + 1) }
        for (offset in listOf(0L, 1L, 15L, 16L, 17L, 100_000L) + edges + listOf(whole.size - 1L, whole.size.toLong())) {
            val got = source.openAt(offset).use { it.readBytes() }
            assertArrayEquals("from $offset", whole.copyOfRange(offset.toInt(), whole.size), got)
        }
        // A byte at a time across a chunk's end, and from a file that comes a little at a time.
        val trickling = EncryptingSource(cipher) { Trickle(opener(plain)(it), 999) }
        trickling.openAt(E2ee.cipherOffsetOfChunk(2) - 3).use { input ->
            for (k in -3 until 3) assertEquals("byte $k", whole[(E2ee.cipherOffsetOfChunk(2) + k).toInt()].toInt() and 0xff, input.read())
        }
        assertArrayEquals(whole, trickling.openAt(0).use { it.readBytes() })
        assertThrows(IllegalArgumentException::class.java) { source.openAt(whole.size + 1L) }
        assertThrows(IllegalArgumentException::class.java) { source.openAt(-1) }
    }

    @Test
    fun aFileThatGotShorterFailsToRead() {
        val plain = plaintext(E2ee.CHUNK_SIZE + 300)
        // The upload started when the file had a byte more.
        val source = EncryptingSource(ContentCipher(fileKey, header, plain.size + 1L), opener(plain))
        assertThrows(EOFException::class.java) { source.openAt(0).use { it.readBytes() } }
        assertThrows(EOFException::class.java) { source.openAt(source.size - 1).use { it.readBytes() } }
    }

    @Test
    fun decryptsFromAnyChunk() {
        for (size in listOf(3 * E2ee.CHUNK_SIZE + 77, 2 * E2ee.CHUNK_SIZE)) {
            val plain = plaintext(size)
            val cipher = ContentCipher(fileKey, header, size.toLong())
            val whole = byChunks(cipher, plain)
            for (p in listOf(0L, 1L, 65_535L, 65_536L, 65_537L, 2L * E2ee.CHUNK_SIZE - 1, size - 1L, size.toLong())) {
                val start = E2ee.readStart(p)
                val input = ByteArrayInputStream(whole, start.cipherOffset.toInt(), whole.size - start.cipherOffset.toInt())
                val got = DecryptingStream(Trickle(input, 1000), cipher, start.chunk, start.skip).use { it.readBytes() }
                assertArrayEquals("$size bytes from $p", plain.copyOfRange(p.toInt(), size), got)
            }
        }
        val cipher = ContentCipher(fileKey, header, 10)
        assertThrows(IllegalArgumentException::class.java) { DecryptingStream(ByteArrayInputStream(ByteArray(0)), cipher, 2) }
        assertThrows(IllegalArgumentException::class.java) { DecryptingStream(ByteArrayInputStream(ByteArray(0)), cipher, 0, 11) }
        assertThrows(IllegalArgumentException::class.java) { DecryptingStream(ByteArrayInputStream(ByteArray(0)), cipher, 1, 1) }
    }

    @Test
    fun aChangedOrIncompleteFileFails() {
        val plain = plaintext(3 * E2ee.CHUNK_SIZE)
        val cipher = ContentCipher(fileKey, header, plain.size.toLong())
        val chunks = byChunks(cipher, plain).let { it.copyOfRange(16, it.size) }
        val c = E2ee.CHUNK_SIZE + 16
        assertArrayEquals(plain, DecryptingStream(ByteArrayInputStream(chunks), cipher).readBytes())

        val tampered = mapOf(
            "a flipped bit" to chunks.copyOf().also { it[5] = (it[5].toInt() xor 1).toByte() },
            "a flipped bit in the last tag" to chunks.copyOf().also { it[it.size - 1] = (it[it.size - 1].toInt() xor 1).toByte() },
            "swapped chunks" to chunks.copyOfRange(c, 2 * c) + chunks.copyOfRange(0, c) + chunks.copyOfRange(2 * c, 3 * c),
            "the last chunk dropped" to chunks.copyOf(2 * c),
            "cut inside a chunk" to chunks.copyOf(2 * c + 100),
            "a byte more" to chunks + byteArrayOf(0),
            "the last chunk twice" to chunks + chunks.copyOfRange(2 * c, 3 * c),
            "nothing" to ByteArray(0),
        )
        for ((name, bytes) in tampered) {
            val stream = DecryptingStream(Trickle(ByteArrayInputStream(bytes), 5000), cipher)
            assertThrows(name, E2eeException::class.java) { stream.readBytes() }
            assertThrows("$name, read again", E2eeException::class.java) { stream.read() }
        }
        // The chunks open only with the file's own key and header.
        for ((name, other) in mapOf("another file key" to ContentCipher(E2ee.newFileKey(), header, plain.size.toLong()), "another header" to ContentCipher(fileKey, E2ee.newHeader(), plain.size.toLong()))) {
            assertThrows(name, E2eeException::class.java) { DecryptingStream(ByteArrayInputStream(chunks), other).readBytes() }
        }
        // Cut at a chunk's end it isn't a shorter file either: its last chunk wasn't encrypted as the last.
        val shorter = ContentCipher(fileKey, header, 2L * E2ee.CHUNK_SIZE)
        assertThrows(E2eeException::class.java) { DecryptingStream(ByteArrayInputStream(chunks.copyOf(2 * c)), shorter).readBytes() }
        // Nor do chunks open as other chunks.
        assertThrows(E2eeException::class.java) { DecryptingStream(ByteArrayInputStream(chunks, c, 2 * c), cipher, 0).readBytes() }
        assertThrows(E2eeException::class.java) { cipher.decryptChunk(1, chunks, 0, c) }
    }

    @Test
    fun headers() {
        val h = E2ee.newHeader()
        assertArrayEquals(h, E2ee.parseHeader(h))
        assertEquals("SHE1", String(h, 0, 4))
        assertEquals(65536, ByteBuffer.wrap(h, 4, 4).int)
        assertEquals(0, h[15].toInt())
        assertFalse("a new nonce prefix each time", h.contentEquals(E2ee.newHeader()))
        val bad = mapOf(
            "another version" to "SHE2".toByteArray() + h.copyOfRange(4, 16),
            "another chunk size" to h.copyOf().also { it[5] = 2 },
            "no zero at the end" to h.copyOf().also { it[15] = 1 },
            "a byte short" to h.copyOf(15),
            "a byte more" to h + byteArrayOf(0),
        )
        for ((name, b) in bad) {
            assertThrows(name, E2eeException::class.java) { E2ee.parseHeader(b) }
            assertThrows(name, E2eeException::class.java) { ContentCipher(fileKey, b, 1) }
        }
    }

    @Test
    fun sizesAndRanges() {
        assertEquals(1L, E2ee.chunks(0))
        assertEquals(1L, E2ee.chunks(65_536))
        assertEquals(2L, E2ee.chunks(65_537))
        assertEquals(32L, E2ee.encryptedSize(0))
        assertEquals(65_568L, E2ee.encryptedSize(65_536))
        assertEquals(65_585L, E2ee.encryptedSize(65_537))
        assertEquals(16L, E2ee.cipherOffsetOfChunk(0))
        assertEquals(16L + 65_552, E2ee.cipherOffsetOfChunk(1))
        assertEquals(16L + 2 * 65_552, E2ee.cipherOffsetOfChunk(2))
        assertEquals(ReadStart(0, 16, 0), E2ee.readStart(0))
        assertEquals(ReadStart(0, 16, 65_535), E2ee.readStart(65_535))
        assertEquals(ReadStart(1, 16 + 65_552, 0), E2ee.readStart(65_536))
        assertEquals(ReadStart(1, 16 + 65_552, 1), E2ee.readStart(65_537))

        val cipher = ContentCipher(fileKey, header, 2L * E2ee.CHUNK_SIZE + 10)
        assertEquals(3L, cipher.chunks)
        assertEquals(0L until 65_536L, cipher.plainRange(0))
        assertEquals(131_072L until 131_082L, cipher.plainRange(2))
        assertEquals(65_552, cipher.sealedLength(1))
        assertEquals(26, cipher.sealedLength(2))
        val empty = ContentCipher(fileKey, header, 0)
        assertEquals(1L, empty.chunks)
        assertTrue(empty.plainRange(0).isEmpty())
        assertEquals(16, empty.sealedLength(0))

        assertThrows(IllegalArgumentException::class.java) { cipher.plainLength(3) }
        assertThrows(IllegalArgumentException::class.java) { cipher.encryptChunk(0, ByteArray(100)) }
        assertThrows(IllegalArgumentException::class.java) { cipher.encryptChunk(3, ByteArray(0)) }
        assertThrows(IllegalArgumentException::class.java) { E2ee.chunks(-1) }
        assertThrows(E2eeException::class.java) { cipher.decryptChunk(3, ByteArray(26)) }
        assertThrows(E2eeException::class.java) { cipher.decryptChunk(2, ByteArray(25)) }
    }

    // The recovery code (recovery.json).

    @Test
    fun recoveryCodeOfTheVectors() {
        val v = contract("crypto/recovery.json")
        val secret = v.bytes("secret")
        assertEquals(v.getString("code"), E2ee.formatRecoveryCode(secret))
        for (text in listOf(v.getString("code")) + v.strings("also_reads")) assertArrayEquals(text, secret, E2ee.parseRecoveryCode(text))
        for (text in v.strings("refuses")) assertThrows(text, E2eeException::class.java) { E2ee.parseRecoveryCode(text) }
        val key = E2ee.secretKey(secret, E2ee.PURPOSE_RECOVERY)
        assertEquals(v.getString("key"), E2ee.b64u(key))
        val lock = v.getJSONObject("lock")
        val private = E2ee.unlock(key, E2ee.recoveryContext, lock.bytes("locked"))
        assertEquals(lock.getString("private_key"), E2ee.b64u(private))
        assertEquals(lock.getString("public_key"), E2ee.b64u(Hpke.publicKey(private)))
    }

    @Test
    fun newRecoveryCodes() {
        val (secret, code) = E2ee.newRecoveryCode()
        assertEquals(20, secret.size)
        assertTrue(code, Regex("([0-9A-HJKMNP-TV-Z]{4}-){7}[0-9A-HJKMNP-TV-Z]{4}").matches(code))
        assertArrayEquals(secret, E2ee.parseRecoveryCode(code))
        // As typed from paper: small letters, spaces and line breaks anywhere.
        assertArrayEquals(secret, E2ee.parseRecoveryCode(code.lowercase().replace('-', ' ').chunked(10).joinToString("\n\t")))

        // O reads as 0, and I and L as 1.
        assertEquals("0000-0000-0000-0000-0000-0000-0000-0000", E2ee.formatRecoveryCode(ByteArray(20)))
        assertArrayEquals(ByteArray(20), E2ee.parseRecoveryCode("OOOO-oooo-0000-0000-0000-0000-0000-0000"))
        val ones = ByteArray(20) { byteArrayOf(0x08, 0x42, 0x10, 0x84.toByte(), 0x21)[it % 5] }
        assertEquals("1111-1111-1111-1111-1111-1111-1111-1111", E2ee.formatRecoveryCode(ones))
        assertArrayEquals(ones, E2ee.parseRecoveryCode("IiLl-1111-iIlL-1111-1111-1111-1111-1111"))

        for (bad in listOf("", "0000-0000-0000-0000-0000-0000-0000-000U", "0000-0000-0000-0000-0000-0000-0000-000u", "0000-0000-0000-0000-0000-0000-0000-0000-0", "0000_0000-0000-0000-0000-0000-0000-0000")) {
            assertThrows(bad, E2eeException::class.java) { E2ee.parseRecoveryCode(bad) }
        }
    }

    @Test
    fun base64url() {
        val bytes = byteArrayOf(0xfb.toByte(), 0xff.toByte(), 0x01)
        assertEquals("-_8B", E2ee.b64u(bytes))
        assertEquals("-_8", E2ee.b64u(bytes.copyOf(2)))
        assertArrayEquals(bytes, E2ee.fromB64u("-_8B"))
        assertArrayEquals(bytes.copyOf(2), E2ee.fromB64u("-_8"))
        assertArrayEquals(ByteArray(0), E2ee.fromB64u(""))
        for (bad in listOf("-_8=", "+/8B", "-_8B ", "A")) assertThrows(bad, E2eeException::class.java) { E2ee.fromB64u(bad) }
    }

    /** Gives at most [most] bytes a read, as a network stream or a provider's pipe may. */
    private class Trickle(private val input: InputStream, private val most: Int) : InputStream() {
        override fun read() = input.read()

        override fun read(b: ByteArray, off: Int, len: Int) = input.read(b, off, minOf(len, most))

        override fun close() = input.close()
    }

    private companion object {
        fun JSONObject.bytes(name: String): ByteArray = E2ee.fromB64u(getString(name))

        fun JSONObject.cases(name: String): List<JSONObject> = getJSONArray(name).let { a -> List(a.length()) { a.getJSONObject(it) } }

        fun JSONObject.strings(name: String): List<String> = getJSONArray(name).let { a -> List(a.length()) { a.getString(it) } }

        /** The vectors' plaintext: byte i is i mod 251. */
        fun plaintext(n: Int) = ByteArray(n) { (it % 251).toByte() }

        fun sha256(b: ByteArray) = MessageDigest.getInstance("SHA-256").digest(b).toHex()

        /** Opens [plain] at an offset, which must be a chunk's start. */
        fun opener(plain: ByteArray): (Long) -> InputStream = { offset ->
            assertEquals("not a chunk's start", 0L, offset % E2ee.CHUNK_SIZE)
            ByteArrayInputStream(plain, offset.toInt(), plain.size - offset.toInt())
        }

        /** The whole encrypted file, made chunk by chunk. */
        fun byChunks(cipher: ContentCipher, plain: ByteArray): ByteArray {
            val out = ByteArrayOutputStream()
            out.write(cipher.header)
            for (i in 0 until cipher.chunks) out.write(cipher.encryptChunk(i, plain, cipher.plainRange(i).first.toInt(), cipher.plainLength(i)))
            return out.toByteArray()
        }

        /** Reads [input] to its end into [buf], in reads of up to [step] bytes; how many it held (buf.size if as many or more). */
        fun readAll(input: InputStream, buf: ByteArray, step: Int): Int {
            var n = 0
            while (n < buf.size) {
                val r = input.read(buf, n, minOf(step, buf.size - n))
                if (r < 0) break
                n += r
            }
            return n
        }
    }
}
