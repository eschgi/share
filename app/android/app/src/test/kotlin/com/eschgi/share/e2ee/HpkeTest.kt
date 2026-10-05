package com.eschgi.share.e2ee

import com.eschgi.share.contract
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test
import java.math.BigInteger
import java.security.AlgorithmParameters
import java.security.KeyFactory
import java.security.spec.ECGenParameterSpec
import java.security.spec.ECParameterSpec
import java.security.spec.ECPoint
import java.security.spec.ECPrivateKeySpec
import java.security.spec.ECPublicKeySpec
import javax.crypto.KeyAgreement

fun unhex(s: String): ByteArray = ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }

fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it) }

/** HPKE for the one suite Share seals keys with, against RFC 9180's vector (contract/crypto/hpke_rfc9180.json). */
class HpkeTest {
    private val rfc = contract("crypto/hpke_rfc9180.json")

    private fun hex(name: String) = unhex(rfc.getString(name))

    @Test
    fun theVectorIsTheSuite() {
        assertEquals(0, rfc.getInt("mode"))
        assertEquals(0x0010, rfc.getInt("kem_id"))
        assertEquals(0x0001, rfc.getInt("kdf_id"))
        assertEquals(0x0002, rfc.getInt("aead_id"))
    }

    @Test
    fun sealsAsTheRfcDoes() {
        val sealed = Hpke.seal(hex("pkRm"), hex("info"), hex("aad"), hex("pt"), KeyPair(hex("skEm"), hex("pkEm")))
        assertEquals(rfc.getString("enc") + rfc.getString("ct"), sealed.toHex())
    }

    @Test
    fun opensWhatTheRfcSealed() {
        val opened = Hpke.open(hex("skRm"), hex("pkRm"), hex("info"), hex("aad"), hex("enc") + hex("ct"))
        assertEquals(rfc.getString("pt"), opened.toHex())
    }

    @Test
    fun theStepsInBetween() {
        val dh = KeyAgreement.getInstance("ECDH").run {
            init(KeyFactory.getInstance("EC").generatePrivate(ECPrivateKeySpec(BigInteger(1, hex("skEm")), p256)))
            doPhase(KeyFactory.getInstance("EC").generatePublic(ECPublicKeySpec(pointOf(hex("pkRm")), p256)), true)
            generateSecret()
        }
        assertEquals(rfc.getString("shared_secret"), Hpke.sharedSecret(dh, hex("enc"), hex("pkRm")).toHex())
        val (key, nonce) = Hpke.schedule(hex("shared_secret"), hex("info"))
        assertEquals(rfc.getString("key"), key.toHex())
        assertEquals(rfc.getString("base_nonce"), nonce.toHex())
    }

    @Test
    fun publicKeysOfTheRfcsPrivateKeys() {
        assertEquals(rfc.getString("pkRm"), Hpke.publicKey(hex("skRm")).toHex())
        assertEquals(rfc.getString("pkEm"), Hpke.publicKey(hex("skEm")).toHex())
        assertEquals(rfc.getString("enc"), rfc.getString("pkEm"))
    }

    @Test
    fun freshKeysSealAndOpen() {
        val pair = Hpke.generateKeyPair()
        assertEquals(Hpke.PRIVATE_KEY_SIZE, pair.privateKey.size)
        assertEquals(Hpke.PUBLIC_KEY_SIZE, pair.publicKey.size)
        val info = "share-e2ee-v1/file".toByteArray()
        val aad = "folder:f1:1".toByteArray()
        val key = ByteArray(32) { it.toByte() }
        val sealed = Hpke.seal(pair.publicKey, info, aad, key)
        assertEquals(65 + 32 + 16, sealed.size)
        assertArrayEquals(key, Hpke.open(pair.privateKey, pair.publicKey, info, aad, sealed))
        assertFalse("a new ephemeral key each time", sealed.contentEquals(Hpke.seal(pair.publicKey, info, aad, key)))

        val other = Hpke.generateKeyPair()
        val flipped = sealed.copyOf().also { it[it.size - 1] = (it[it.size - 1].toInt() xor 1).toByte() }
        val refused = mapOf(
            "another key" to { Hpke.open(other.privateKey, other.publicKey, info, aad, sealed) },
            "another info" to { Hpke.open(pair.privateKey, pair.publicKey, "share-e2ee-v1/folder".toByteArray(), aad, sealed) },
            "another aad" to { Hpke.open(pair.privateKey, pair.publicKey, info, "folder:f1:2".toByteArray(), sealed) },
            "another public key" to { Hpke.open(pair.privateKey, other.publicKey, info, aad, sealed) },
            "a flipped bit" to { Hpke.open(pair.privateKey, pair.publicKey, info, aad, flipped) },
            "cut short" to { Hpke.open(pair.privateKey, pair.publicKey, info, aad, sealed.copyOf(40)) },
            "no ciphertext" to { Hpke.open(pair.privateKey, pair.publicKey, info, aad, sealed.copyOf(65 + 15)) },
        )
        for ((name, open) in refused) assertThrows(name, E2eeException::class.java) { open() }
    }

    @Test
    fun publicKeysOfFreshPrivateKeys() {
        // Half of them have the other y; the signature has to pick the right one each time.
        repeat(16) {
            val pair = Hpke.generateKeyPair()
            assertArrayEquals(pair.publicKey, Hpke.publicKey(pair.privateKey))
        }
    }

    @Test
    fun publicKeysOffTheCurveAreRefused() {
        val good = hex("pkRm")
        Hpke.checkPublicKey(good)
        val g = p256.generator
        val bad = mapOf(
            "y changed" to good.copyOf().also { it[64] = (it[64].toInt() xor 1).toByte() },
            "the other prefix" to good.copyOf().also { it[0] = 2 },
            "compressed" to byteArrayOf(2) + good.copyOfRange(1, 33),
            "a byte short" to good.copyOf(64),
            "a byte more" to good + byteArrayOf(0),
            "zeros" to ByteArray(65).also { it[0] = 4 },
            "x is p" to encode(P, g.affineY),
            "y is p" to encode(g.affineX, P),
        )
        for ((name, key) in bad) {
            assertThrows(name, E2eeException::class.java) { Hpke.checkPublicKey(key) }
            assertThrows(name, E2eeException::class.java) { Hpke.seal(key, ByteArray(0), ByteArray(0), ByteArray(32)) }
        }
        // The encapsulated key is a public key too.
        val sealed = Hpke.seal(good, ByteArray(0), ByteArray(0), ByteArray(32))
        sealed[64] = (sealed[64].toInt() xor 1).toByte()
        assertThrows(E2eeException::class.java) { Hpke.open(hex("skRm"), good, ByteArray(0), ByteArray(0), sealed) }
    }

    @Test
    fun privateKeysOutOfRangeAreRefused() {
        val sealed = Hpke.seal(hex("pkRm"), ByteArray(0), ByteArray(0), ByteArray(32))
        for (key in listOf(ByteArray(32), fixed(N), fixed(N + BigInteger.ONE), hex("skRm").copyOf(31), byteArrayOf(0) + hex("skRm"))) {
            assertThrows(E2eeException::class.java) { Hpke.publicKey(key) }
            assertThrows(E2eeException::class.java) { Hpke.open(key, hex("pkRm"), ByteArray(0), ByteArray(0), sealed) }
        }
        // The smallest and the largest work: 1 is the generator, n - 1 its negative.
        val g = p256.generator
        assertArrayEquals(encode(g.affineX, g.affineY), Hpke.publicKey(fixed(BigInteger.ONE)))
        assertArrayEquals(encode(g.affineX, P - g.affineY), Hpke.publicKey(fixed(N - BigInteger.ONE)))
    }

    companion object {
        val p256: ECParameterSpec = AlgorithmParameters.getInstance("EC").run {
            init(ECGenParameterSpec("secp256r1"))
            getParameterSpec(ECParameterSpec::class.java)
        }

        /** P-256's prime and order, written out apart from the code. */
        private val P = BigInteger("ffffffff00000001000000000000000000000000ffffffffffffffffffffffff", 16)
        private val N = BigInteger("ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551", 16)

        private fun fixed(n: BigInteger): ByteArray {
            val b = n.toByteArray()
            return if (b.size > 32) b.copyOfRange(b.size - 32, b.size) else ByteArray(32 - b.size) + b
        }

        private fun encode(x: BigInteger, y: BigInteger): ByteArray {
            val bx = x.toByteArray().dropWhile { it == 0.toByte() }.toByteArray()
            val by = y.toByteArray().dropWhile { it == 0.toByte() }.toByteArray()
            return byteArrayOf(4) + ByteArray(32 - bx.size) + bx + ByteArray(maxOf(0, 32 - by.size)) + by
        }

        private fun pointOf(key: ByteArray) = ECPoint(BigInteger(1, key.copyOfRange(1, 33)), BigInteger(1, key.copyOfRange(33, 65)))
    }
}
