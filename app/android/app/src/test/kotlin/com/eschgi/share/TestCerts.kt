package com.eschgi.share

import java.io.File
import java.security.KeyStore
import java.security.cert.X509Certificate
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext

private val PASS = "test-only".toCharArray()

/** A throwaway self-signed EC certificate for 127.0.0.1 from the JDK's keytool, so no key is committed. */
fun selfSigned(dir: File, name: String): Pair<KeyStore, X509Certificate> {
    val file = File(dir, "$name.p12")
    val keytool = File(System.getProperty("java.home"), "bin/keytool").path
    val process = ProcessBuilder(
        keytool, "-genkeypair", "-alias", "local", "-keyalg", "EC", "-groupname", "secp256r1",
        "-validity", "2", "-dname", "CN=share-test-$name", "-ext", "SAN=ip:127.0.0.1",
        "-keystore", file.path, "-storetype", "PKCS12", "-storepass", String(PASS), "-keypass", String(PASS),
    ).redirectErrorStream(true).start()
    val output = process.inputStream.bufferedReader().readText()
    check(process.waitFor() == 0) { "keytool failed: $output" }
    val store = KeyStore.getInstance("PKCS12").apply { file.inputStream().use { load(it, PASS) } }
    return store to (store.getCertificate("local") as X509Certificate)
}

/** TLS that serves [cert], for a TestServer. */
fun serving(cert: Pair<KeyStore, X509Certificate>): SSLContext {
    val keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm()).apply { init(cert.first, PASS) }
    return SSLContext.getInstance("TLS").apply { init(keys.keyManagers, null, null) }
}
