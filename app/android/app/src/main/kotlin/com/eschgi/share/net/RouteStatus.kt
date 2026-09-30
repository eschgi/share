package com.eschgi.share.net

import com.eschgi.share.data.ServerConfig
import org.json.JSONException
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
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
) {
    val isLocal: Boolean get() = route == Route.LOCAL

    fun toMap(): Map<String, Any?> = mapOf(
        "route" to route.name.lowercase(),
        "reason" to reason.wire,
        "millis" to millis,
        "checking" to checking,
    )

    companion object {
        val NO_LOCAL = RouteStatus(Route.PUBLIC, RouteReason.NO_LOCAL)
        val UNKNOWN = RouteStatus(Route.PUBLIC)
    }
}

/** Asks the local address whether it is this phone's server. */
object LocalProbe {
    const val TIMEOUT_MS = 1_500

    fun probe(config: ServerConfig, timeoutMs: Int = TIMEOUT_MS): RouteStatus {
        val local = config.localUrl
        if (local == null || !config.hasLocal) return RouteStatus.NO_LOCAL
        val started = System.nanoTime()
        val conn = try {
            URL("$local/api/info").openConnection() as? HttpsURLConnection
        } catch (e: IOException) {
            null
        } ?: return RouteStatus(Route.PUBLIC, RouteReason.UNREACHABLE)
        return try {
            PinnedTls.pin(conn, config.pins)
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.useCaches = false
            conn.instanceFollowRedirects = false
            if (conn.responseCode != 200) return RouteStatus(Route.PUBLIC, RouteReason.OTHER_SERVER)
            val body = conn.inputStream.use { readLimited(it, 64 * 1024) }
            val id = JSONObject(String(body)).optString("server_id")
            if (config.serverId != null && id != config.serverId) {
                RouteStatus(Route.PUBLIC, RouteReason.OTHER_SERVER)
            } else {
                RouteStatus(Route.LOCAL, millis = (System.nanoTime() - started) / 1_000_000)
            }
        } catch (e: IOException) {
            RouteStatus(Route.PUBLIC, if (PinnedTls.isMismatch(e)) RouteReason.WRONG_CERTIFICATE else RouteReason.UNREACHABLE)
        } catch (e: JSONException) {
            RouteStatus(Route.PUBLIC, RouteReason.OTHER_SERVER)
        } finally {
            conn.disconnect()
        }
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
