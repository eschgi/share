package com.eschgi.share.e2ee

import java.math.BigInteger
import java.security.AlgorithmParameters
import java.security.GeneralSecurityException
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.PrivateKey
import java.security.PublicKey
import java.security.SecureRandom
import java.security.Signature
import java.security.interfaces.ECPrivateKey
import java.security.interfaces.ECPublicKey
import java.security.spec.ECFieldFp
import java.security.spec.ECGenParameterSpec
import java.security.spec.ECParameterSpec
import java.security.spec.ECPoint
import java.security.spec.ECPrivateKeySpec
import java.security.spec.ECPublicKeySpec
import javax.crypto.Cipher
import javax.crypto.KeyAgreement
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/** A P-256 key pair as Share keeps and sends it: the private key's 32-byte scalar, and the public key as 65 bytes (0x04, X, Y). */
class KeyPair(val privateKey: ByteArray, val publicKey: ByteArray)

/**
 * HPKE in base mode (RFC 9180), single shot, for the one suite Share seals keys with:
 * DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-256-GCM (contract/crypto/hpke_rfc9180.json).
 * Only plain JVM classes, so the unit tests run what the phone runs: ECDH gives P-256's shared
 * x-coordinate, which is HPKE's DH output, and HMAC makes HKDF's two halves, which HPKE uses
 * apart.
 */
object Hpke {
    const val PUBLIC_KEY_SIZE = 65
    const val PRIVATE_KEY_SIZE = 32

    private const val KEM_ID = 0x0010
    private const val KDF_ID = 0x0001
    private const val AEAD_ID = 0x0002
    private const val SECRET_SIZE = 32
    private const val SIGNATURE = "SHA256withECDSA"

    private val version = "HPKE-v1".toByteArray()
    private val kemSuite = "KEM".toByteArray() + i2osp(KEM_ID, 2)
    private val hpkeSuite = "HPKE".toByteArray() + i2osp(KEM_ID, 2) + i2osp(KDF_ID, 2) + i2osp(AEAD_ID, 2)
    private val empty = ByteArray(0)
    private val random = SecureRandom()

    /** P-256, as the JCE describes it. */
    private val curve: ECParameterSpec by lazy {
        AlgorithmParameters.getInstance("EC").run {
            init(ECGenParameterSpec("secp256r1"))
            getParameterSpec(ECParameterSpec::class.java)
        }
    }

    private val prime: BigInteger get() = (curve.curve.field as ECFieldFp).p

    /** A new key pair. */
    fun generateKeyPair(): KeyPair {
        val pair = KeyPairGenerator.getInstance("EC").run {
            initialize(ECGenParameterSpec("secp256r1"), random)
            generateKeyPair()
        }
        val w = (pair.public as ECPublicKey).w
        return KeyPair(fixed((pair.private as ECPrivateKey).s.toByteArray()), encode(w.affineX, w.affineY))
    }

    /**
     * The public key of a private one. The JCE can't multiply a point, but ECDH with the curve's
     * generator gives the public key's x, the curve has two y for it, and a signature made with
     * the private key verifies with only one of them. So the private key only meets the JCE's own
     * arithmetic.
     */
    fun publicKey(privateKey: ByteArray): ByteArray {
        val key = privateKeyOf(privateKey)
        val x = BigInteger(1, dh(key, publicKeyOf(curve.generator)))
        val p = prime
        // P-256's prime is 3 mod 4, so a square root is a power.
        val y = rightSide(x).modPow(p.add(BigInteger.ONE).shiftRight(2), p)
        val message = "share-e2ee-v1/public-key".toByteArray()
        val signature = guard("can't sign") { Signature.getInstance(SIGNATURE).run { initSign(key); update(message); sign() } }
        for (candidate in listOf(y, p.subtract(y).mod(p))) {
            val point = ECPoint(x, candidate)
            val verified = guard("can't verify") { Signature.getInstance(SIGNATURE).run { initVerify(publicKeyOf(point)); update(message); verify(signature) } }
            if (verified) return encode(point.affineX, point.affineY)
        }
        throw E2eeException("no public key for this private key")
    }

    /** Throws an [E2eeException] unless [publicKey] is a P-256 public key as Share sends it. */
    fun checkPublicKey(publicKey: ByteArray) {
        pointOf(publicKey)
    }

    /** P-256's DH output of a private key and another public key: the shared point's x. */
    fun dh(privateKey: ByteArray, publicKey: ByteArray): ByteArray = dh(privateKeyOf(privateKey), publicKeyOf(pointOf(publicKey)))

    /** Signs [message] with a private key: ECDSA with SHA-256, as r and s in 64 bytes (the JCE gives DER). */
    fun sign(privateKey: ByteArray, message: ByteArray): ByteArray {
        val der = guard("can't sign") { Signature.getInstance(SIGNATURE).run { initSign(privateKeyOf(privateKey)); update(message); sign() } }
        return rawSignature(der)
    }

    /** Whether [signature], r and s in 64 bytes, is [publicKey]'s signature of [message]. */
    fun verify(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean {
        if (signature.size != 64) return false
        return try {
            Signature.getInstance(SIGNATURE).run { initVerify(publicKeyOf(pointOf(publicKey))); update(message); verify(derSignature(signature)) }
        } catch (e: GeneralSecurityException) {
            false
        } catch (e: E2eeException) {
            false
        }
    }

    /** r and s of a DER signature: SEQUENCE { INTEGER r, INTEGER s }, short lengths for P-256. */
    private fun rawSignature(der: ByteArray): ByteArray {
        if (der.size < 8 || der[0] != 0x30.toByte()) throw E2eeException("not a DER signature")
        var at = 2
        fun int(): ByteArray {
            if (der[at] != 2.toByte()) throw E2eeException("not a DER signature")
            val len = der[at + 1].toInt()
            val v = der.copyOfRange(at + 2, at + 2 + len)
            at += 2 + len
            return fixed(BigInteger(1, v).toByteArray())
        }
        return int() + int()
    }

    /** A DER signature of r and s. */
    private fun derSignature(raw: ByteArray): ByteArray {
        fun int(b: ByteArray): ByteArray {
            val v = BigInteger(1, b).toByteArray() // with a leading zero where the top bit is set
            return byteArrayOf(2, v.size.toByte()) + v
        }
        val body = int(raw.copyOfRange(0, 32)) + int(raw.copyOfRange(32, 64))
        return byteArrayOf(0x30, body.size.toByte()) + body
    }

    /** Seals [plaintext] for the holder of [publicKey]'s private key: the encapsulated key (65 bytes), then the ciphertext. */
    fun seal(publicKey: ByteArray, info: ByteArray, aad: ByteArray, plaintext: ByteArray): ByteArray =
        seal(publicKey, info, aad, plaintext, generateKeyPair())

    /** Seals with the given ephemeral key pair; the tests pass the RFC's. */
    internal fun seal(publicKey: ByteArray, info: ByteArray, aad: ByteArray, plaintext: ByteArray, ephemeral: KeyPair): ByteArray {
        val dh = dh(privateKeyOf(ephemeral.privateKey), publicKeyOf(pointOf(publicKey)))
        val (key, nonce) = schedule(sharedSecret(dh, ephemeral.publicKey, publicKey), info)
        return ephemeral.publicKey + gcmSeal(key, nonce, aad, plaintext)
    }

    /** Opens what [seal] made, with the private key and its own public key. Throws an [E2eeException] if it can't. */
    fun open(privateKey: ByteArray, publicKey: ByteArray, info: ByteArray, aad: ByteArray, sealed: ByteArray): ByteArray {
        if (sealed.size < PUBLIC_KEY_SIZE + TAG_SIZE) throw E2eeException("too short to be sealed")
        val enc = sealed.copyOfRange(0, PUBLIC_KEY_SIZE)
        val dh = dh(privateKeyOf(privateKey), publicKeyOf(pointOf(enc)))
        val (key, nonce) = schedule(sharedSecret(dh, enc, publicKey), info)
        return gcmOpen(key, nonce, aad, sealed, PUBLIC_KEY_SIZE, sealed.size - PUBLIC_KEY_SIZE)
    }

    /** DHKEM's shared secret, from the DH output and both public keys. */
    internal fun sharedSecret(dh: ByteArray, enc: ByteArray, recipient: ByteArray): ByteArray {
        val prk = labeledExtract(kemSuite, empty, "eae_prk", dh)
        return labeledExpand(kemSuite, prk, "shared_secret", enc + recipient, SECRET_SIZE)
    }

    /** The key schedule in base mode, with no PSK: the AEAD's key and the first message's nonce. */
    internal fun schedule(sharedSecret: ByteArray, info: ByteArray): Pair<ByteArray, ByteArray> {
        val context = byteArrayOf(0) + labeledExtract(hpkeSuite, empty, "psk_id_hash", empty) + labeledExtract(hpkeSuite, empty, "info_hash", info)
        val secret = labeledExtract(hpkeSuite, sharedSecret, "secret", empty)
        return labeledExpand(hpkeSuite, secret, "key", context, KEY_SIZE) to labeledExpand(hpkeSuite, secret, "base_nonce", context, NONCE_SIZE)
    }

    private fun labeledExtract(suite: ByteArray, salt: ByteArray, label: String, ikm: ByteArray) =
        hkdfExtract(salt, version + suite + label.toByteArray() + ikm)

    private fun labeledExpand(suite: ByteArray, prk: ByteArray, label: String, info: ByteArray, length: Int) =
        hkdfExpand(prk, i2osp(length, 2) + version + suite + label.toByteArray() + info, length)

    /** P-256's DH output: the shared point's x, 32 bytes. */
    private fun dh(privateKey: PrivateKey, publicKey: PublicKey): ByteArray = guard("no shared secret") {
        KeyAgreement.getInstance("ECDH").run {
            init(privateKey)
            doPhase(publicKey, true)
            fixed(generateSecret())
        }
    }

    /** The point of a public key, which must be on the curve: the JDK's KeyFactory doesn't check that (Android's does). */
    private fun pointOf(publicKey: ByteArray): ECPoint {
        if (publicKey.size != PUBLIC_KEY_SIZE || publicKey[0] != 4.toByte()) throw E2eeException("not a P-256 public key")
        val x = BigInteger(1, publicKey.copyOfRange(1, 33))
        val y = BigInteger(1, publicKey.copyOfRange(33, PUBLIC_KEY_SIZE))
        val p = prime
        if (x >= p || y >= p || y.multiply(y).mod(p) != rightSide(x)) throw E2eeException("not a point on P-256")
        return ECPoint(x, y)
    }

    /** x³ + ax + b: y² for a point on the curve. */
    private fun rightSide(x: BigInteger): BigInteger = x.pow(3).add(curve.curve.a.multiply(x)).add(curve.curve.b).mod(prime)

    private fun publicKeyOf(point: ECPoint): PublicKey = guard("not a P-256 public key") {
        KeyFactory.getInstance("EC").generatePublic(ECPublicKeySpec(point, curve))
    }

    private fun privateKeyOf(privateKey: ByteArray): PrivateKey {
        val s = BigInteger(1, privateKey)
        if (privateKey.size != PRIVATE_KEY_SIZE || s.signum() == 0 || s >= curve.order) throw E2eeException("not a P-256 private key")
        return guard("not a P-256 private key") { KeyFactory.getInstance("EC").generatePrivate(ECPrivateKeySpec(s, curve)) }
    }

    private fun encode(x: BigInteger, y: BigInteger) = byteArrayOf(4) + fixed(x.toByteArray()) + fixed(y.toByteArray())

    /** A big-endian number in exactly 32 bytes: BigInteger adds a sign byte, or leaves out leading zeros. */
    private fun fixed(bytes: ByteArray): ByteArray {
        val extra = bytes.size - 32
        if (extra <= 0) return ByteArray(-extra) + bytes
        check((0 until extra).all { bytes[it] == 0.toByte() }) { "more than 32 bytes" }
        return bytes.copyOfRange(extra, bytes.size)
    }

    private fun i2osp(n: Int, length: Int) = ByteArray(length) { (n shr (8 * (length - 1 - it))).toByte() }
}

// The primitives under HPKE and Share's formats: HMAC-SHA256, HKDF and AES-256-GCM.

internal const val KEY_SIZE = 32
internal const val NONCE_SIZE = 12
internal const val TAG_SIZE = 16

/** A failure of the JCE, for something that came from elsewhere, as an [E2eeException]. */
internal inline fun <T> guard(what: String, block: () -> T): T = try {
    block()
} catch (e: GeneralSecurityException) {
    throw E2eeException(what, e)
}

/** HMAC-SHA256 with [key]. HMAC pads a short key with zeros, so an empty key is 32 zero bytes, which the JCE takes (it refuses empty keys). */
internal fun hmac(key: ByteArray): Mac = Mac.getInstance("HmacSHA256").apply {
    init(SecretKeySpec(if (key.isEmpty()) ByteArray(32) else key, "HmacSHA256"))
}

/** HKDF-Extract; no salt is an empty one. */
internal fun hkdfExtract(salt: ByteArray, ikm: ByteArray): ByteArray = hmac(salt).doFinal(ikm)

internal fun hkdfExpand(prk: ByteArray, info: ByteArray, length: Int): ByteArray {
    val mac = hmac(prk)
    val out = ByteArray(length)
    var t = ByteArray(0)
    var at = 0
    var i = 1
    while (at < length) {
        mac.update(t)
        mac.update(info)
        mac.update(i++.toByte())
        t = mac.doFinal()
        t.copyInto(out, at, 0, minOf(t.size, length - at))
        at += t.size
    }
    return out
}

/** HKDF-SHA256's 32-byte key from [secret], with [salt] (empty for none), for [purpose]. */
internal fun hkdf(secret: ByteArray, salt: ByteArray, purpose: String): ByteArray = hkdfExpand(hkdfExtract(salt, secret), purpose.toByteArray(), KEY_SIZE)

/**
 * AES-256-GCM; the ciphertext ends with the 16-byte tag. A new Cipher each time: the JCE refuses
 * to encrypt twice with one Cipher, key and nonce, and a chunk sent again is encrypted again
 * with the same.
 */
internal fun gcmSeal(key: ByteArray, nonce: ByteArray, aad: ByteArray?, plaintext: ByteArray, offset: Int = 0, length: Int = plaintext.size): ByteArray =
    gcm(Cipher.ENCRYPT_MODE, key, nonce, aad).doFinal(plaintext, offset, length)

/** Decrypts what [gcmSeal] made; throws an [E2eeException] if it was changed or is for another key, nonce or aad. */
internal fun gcmOpen(key: ByteArray, nonce: ByteArray, aad: ByteArray?, ciphertext: ByteArray, offset: Int = 0, length: Int = ciphertext.size - offset): ByteArray {
    if (length < TAG_SIZE) throw E2eeException("too short for AES-GCM")
    val cipher = gcm(Cipher.DECRYPT_MODE, key, nonce, aad)
    return guard("can't be opened") { cipher.doFinal(ciphertext, offset, length) }
}

private fun gcm(mode: Int, key: ByteArray, nonce: ByteArray, aad: ByteArray?): Cipher {
    require(key.size == KEY_SIZE) { "AES-256 takes 32-byte keys" }
    return Cipher.getInstance("AES/GCM/NoPadding").apply {
        init(mode, SecretKeySpec(key, "AES"), GCMParameterSpec(TAG_SIZE * 8, nonce))
        if (aad != null && aad.isNotEmpty()) updateAAD(aad)
    }
}
