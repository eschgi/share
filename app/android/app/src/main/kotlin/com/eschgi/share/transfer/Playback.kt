package com.eschgi.share.transfer

import android.content.Context
import android.net.Uri
import com.eschgi.share.BuildConfig
import com.eschgi.share.data.ServerConfig
import com.eschgi.share.e2ee.Keys
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.ServerInfo
import java.io.IOException

/**
 * Where the app's player (lib/ui/player.dart) plays a video or sound from: a copy on the phone
 * if there is one, else straight from the server with the phone's key, or with a PIN's over the
 * public address. The https port at home has Share's own certificate, which the player can't
 * pin: there the file is fetched into the cache first, as for opening it in another app. With
 * the files in a bucket it plays from the file's link there, without any key. An encrypted file
 * plays from [LocalStream], which decrypts it on the phone as it plays.
 */
object Playback {
    /** How to get at a file. */
    sealed interface Way {
        /** Saved on the phone, or whole in the cache. */
        data object Copy : Way

        /** Straight from the server at [base], while it plays. */
        data class Stream(val base: String) : Way

        /** Into the cache first. */
        data object Fetch : Way

        /** Nowhere: plain http that hasn't shown to be this phone's server gets no key. */
        data object Nowhere : Way
    }

    /**
     * The way, apart from the phone, for the tests. [local]: the route says the address at home
     * answers; [httpAllowed] tells whether the key may go over plain http to the local (true) or
     * the public (false) address (RouteMonitor.httpAllowed).
     */
    fun way(hasCopy: Boolean, config: ServerConfig, local: Boolean, httpAllowed: (Boolean) -> Boolean): Way {
        if (hasCopy) return Way.Copy
        if (local && config.hasLocal) {
            val home = config.localUrl!!
            if (!ServerConfig.isHttp(home)) return Way.Fetch
            if (httpAllowed(true)) return Way.Stream(home)
        }
        val public = config.publicUrl
        return if (!ServerConfig.isHttp(public) || httpAllowed(false)) Way.Stream(public) else Way.Nowhere
    }

    /**
     * What file.play answers (contract/app/platform.json play_copy and play_stream): the uri to
     * play, and the headers it needs. Null if fetching it was cancelled; throws if it can't be had.
     */
    fun source(context: Context, file: FileRef, auth: String = Credentials.DEVICE): Map<String, Any?>? {
        val app = context.applicationContext
        val copy = Downloads.savedUri(app, file.id) ?: Fetcher.cached(app, file)
        if (copy != null) return copyOf(copy)
        Keys.cipher(app, auth, file.enc)?.let { cipher ->
            return mapOf("uri" to LocalStream.url(app, file, auth, cipher), "headers" to emptyMap<String, String>())
        }
        val (token, config) = Credentials.of(app, auth) ?: throw IOException(if (auth == Credentials.PIN) "no PIN" else "signed out")
        val home = Credentials.atHome(auth, RouteMonitor.settled(app).isLocal)
        val server = ServerConnection(config, token)
        val info = ServerInfo.of(config) { server.open(it, home, readTimeoutMs = 15_000) } ?: throw IOException("the server didn't say where its files are")
        if (info.storage == ServerInfo.Storage.S3) {
            // A redirect would take the key along: media players send their headers again after one.
            return when (val answer = S3Links.fetch(file.id) { server.open(it, home) }) {
                is S3Links.Answer.Link -> linkOf(answer.url)
                else -> throw IOException("no link to ${file.name}: $answer")
            }
        }
        return when (val way = way(false, config, home) { RouteMonitor.httpAllowed(config, it) }) {
            is Way.Stream -> streamOf(way.base, file.id, token)
            Way.Fetch -> Fetcher.fetch(app, listOf(file), auth)?.single()?.let { copyOf(it) }
            Way.Nowhere -> throw IOException("${config.publicUrl} hasn't shown to be this phone's server on this network")
            Way.Copy -> error("no copy")
        }
    }

    fun copyOf(uri: Uri): Map<String, Any?> = mapOf("uri" to uri.toString(), "headers" to emptyMap<String, String>())

    /** A file's link in the bucket, exactly as the server signed it, with only the app's name. */
    fun linkOf(url: String): Map<String, Any?> = mapOf("uri" to url, "headers" to mapOf("User-Agent" to "Share-Android/${BuildConfig.VERSION_NAME}"))

    fun streamOf(base: String, id: String, token: String): Map<String, Any?> = mapOf(
        "uri" to "$base/api/files/$id/content",
        "headers" to mapOf("Authorization" to "Bearer $token", "User-Agent" to "Share-Android/${BuildConfig.VERSION_NAME}"),
    )
}
