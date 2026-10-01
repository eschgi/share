package com.eschgi.share.net

import com.eschgi.share.data.ServerConfig
import org.json.JSONException
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import javax.net.ssl.HttpsURLConnection

enum class Route { LOCAL, PUBLIC }

/** Why the public address is used; the names are the Dart enum's (RouteReason). */
enum class RouteReason(val wire: String) {
    NONE("none"),
    NO_LOCAL("noLocal"),
    UNREACHABLE("unreachable"),
    WRONG_CERTIFICATE("wrongCertificate"),
    OTHER_SERVER("otherServer"),
}

/** Which address the phone uses now, and why. Read by RouteStatus.fromMap in platform.dart. */
data class RouteStatus(
    val route: Route,
    val reason: RouteReason = RouteReason.NONE,
    /** How long the local address took to answer. */
    val millis: Long? = null,
    val checking: Boolean = false,
    /**
     * For a server only at home, whose public address is plain http: whether the server there
     * proved to be this phone's, on this network. Until it did, the phone's key doesn't go there.
     */
    val publicVerified: Boolean = false,
) {
    val isLocal: Boolean get() = route == Route.LOCAL

    fun toMap(): Map<String, Any?> = mapOf(
        "route" to route.name.lowercase(),
        "reason" to reason.wire,
        "millis" to millis,
        "checking" to checking,
        "public_verified" to publicVerified,
    )

    companion object {
        val NO_LOCAL = RouteStatus(Route.PUBLIC, RouteReason.NO_LOCAL)
        val UNKNOWN = RouteStatus(Route.PUBLIC)
    }
}

/**
 * Asks the local address whether it is this phone's server, and for a server only at home its
 * public address too. Over https the pinned certificate says so; over plain http the server
 * has to give the proof (HomeProof), for which the probe needs the phone's [token].
 */
object LocalProbe {
    const val TIMEOUT_MS = 1_500

    fun probe(config: ServerConfig, token: String? = null, timeoutMs: Int = TIMEOUT_MS): RouteStatus {
        val publicVerified = config.publicIsHttp && ask(config.publicUrl, config, token, timeoutMs) == RouteReason.NONE
        val local = config.localUrl
        if (local == null || !config.hasLocal) return RouteStatus.NO_LOCAL.copy(publicVerified = publicVerified)
        val started = System.nanoTime()
        val reason = ask(local, config, token, timeoutMs)
        return if (reason == RouteReason.NONE) {
            RouteStatus(Route.LOCAL, millis = (System.nanoTime() - started) / 1_000_000, publicVerified = publicVerified)
        } else {
            RouteStatus(Route.PUBLIC, reason, publicVerified = publicVerified)
        }
    }

    /** Whether [base] is this phone's server: [RouteReason.NONE] if so, otherwise why not. */
    private fun ask(base: String, config: ServerConfig, token: String?, timeoutMs: Int): RouteReason {
        val http = ServerConfig.isHttp(base)
        val conn = try {
            URL("$base/api/info").openConnection() as? HttpURLConnection
        } catch (e: IOException) {
            null
        } ?: return RouteReason.UNREACHABLE
        try {
            if (!http) PinnedTls.pin(conn as? HttpsURLConnection ?: return RouteReason.UNREACHABLE, config.pins)
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.useCaches = false
            conn.instanceFollowRedirects = false
            if (conn.responseCode != 200) return RouteReason.OTHER_SERVER
            val body = conn.inputStream.use { readLimited(it, 64 * 1024) }
            val id = JSONObject(String(body)).optString("server_id")
            if (config.serverId != null && id != config.serverId) return RouteReason.OTHER_SERVER
        } catch (e: IOException) {
            return if (PinnedTls.isMismatch(e)) RouteReason.WRONG_CERTIFICATE else RouteReason.UNREACHABLE
        } catch (e: JSONException) {
            return RouteReason.OTHER_SERVER
        } finally {
            conn.disconnect()
        }
        if (http) {
            val device = config.deviceId
            if (device == null || token == null || !HomeProof.check(base, device, token, timeoutMs)) return RouteReason.OTHER_SERVER
        }
        return RouteReason.NONE
    }
}

/** Reads at most [limit] bytes (InputStream.readNBytes needs Android 13). */
internal fun readLimited(input: InputStream, limit: Int): ByteArray {
    val out = ByteArrayOutputStream()
    val buffer = ByteArray(8 * 1024)
    while (out.size() < limit) {
        val n = input.read(buffer, 0, minOf(buffer.size, limit - out.size()))
        if (n < 0) break
        out.write(buffer, 0, n)
    }
    return out.toByteArray()
}
