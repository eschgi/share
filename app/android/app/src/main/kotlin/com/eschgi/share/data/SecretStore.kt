package com.eschgi.share.data

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import androidx.core.content.edit
import java.security.GeneralSecurityException
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The phone's key for the server (and the few other things Dart keeps through
 * secret.write), encrypted with AES-GCM under a key that never leaves the Android KeyStore.
 * The ciphertext sits in preferences, which backups and device transfers leave out (see
 * data_extraction_rules.xml); on another phone it couldn't be decrypted anyway.
 */
class SecretStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("secrets", Context.MODE_PRIVATE)

    @Synchronized
    fun read(name: String): String? {
        val stored = prefs.getString(name, null) ?: return null
        return try {
            decrypt(name, stored)
        } catch (e: GeneralSecurityException) {
            // The KeyStore lost the key (e.g. after a restore); the value is gone for good.
            Log.w(TAG, "can't decrypt $name, dropping it", e)
            prefs.edit(commit = true) { remove(name) }
            null
        } catch (e: IllegalArgumentException) {
            Log.w(TAG, "can't decode $name, dropping it", e)
            prefs.edit(commit = true) { remove(name) }
            null
        }
    }

    /** Written through before it returns: Dart goes on as if it's stored. */
    @Synchronized
    fun write(name: String, value: String?) {
        val sealed = value?.let { encrypt(name, it) }
        prefs.edit(commit = true) { if (sealed == null) remove(name) else putString(name, sealed) }
    }

    private fun encrypt(name: String, value: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        // The name is bound in, so a value can't be moved to another name.
        cipher.updateAAD(name.toByteArray())
        val sealed = cipher.doFinal(value.toByteArray())
        return "v1:" + b64(cipher.iv) + ":" + b64(sealed)
    }

    private fun decrypt(name: String, stored: String): String {
        val parts = stored.split(':')
        require(parts.size == 3 && parts[0] == "v1") { "unknown format" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, unb64(parts[1])))
        cipher.updateAAD(name.toByteArray())
        return String(cipher.doFinal(unb64(parts[2])))
    }

    private fun key(): SecretKey {
        val keyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (keyStore.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    private fun b64(bytes: ByteArray) = Base64.encodeToString(bytes, Base64.NO_WRAP)

    private fun unb64(text: String) = Base64.decode(text, Base64.NO_WRAP)

    companion object {
        /** The device key (shd_…), written by the Dart session code. */
        const val DEVICE_TOKEN = "device_token"

        /** The language picked in the app, or none for the phone's. */
        const val LANGUAGE = "language"

        private const val TAG = "SecretStore"
        private const val KEYSTORE = "AndroidKeyStore"
        private const val ALIAS = "share_secrets"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
    }
}
