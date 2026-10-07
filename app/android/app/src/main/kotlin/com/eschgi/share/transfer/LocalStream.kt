package com.eschgi.share.transfer

import android.content.Context
import android.util.Log
import com.eschgi.share.e2ee.ContentCipher
import com.eschgi.share.e2ee.DecryptingStream
import com.eschgi.share.e2ee.E2ee
import com.eschgi.share.e2ee.E2eeException
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.S3Connection
import com.eschgi.share.net.ServerConnection
import com.eschgi.share.net.ServerInfo
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.URLEncoder
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap
import kotlin.concurrent.thread

/**
 * Encrypted videos and sounds for the player (docs/e2ee-plan.md), the way the website's
 * service worker serves them: a small HTTP server on 127.0.0.1 answers ranges with the plain
 * bytes, fetching the encrypted ones from the server or the bucket from the chunk a range
 * starts in, and decrypting them on the way, so the player seeks. Each file gets an address with
 * a random token, which only the player is told; addresses work for 12 hours while the app runs.
 */
object LocalStream {
    private const val TAG = "LocalStream"
    private const val LIFE_MS = 12 * 3600_000L

    /** What an address plays: the file, how its stored bytes from an offset on are fetched, and how they decrypt. */
    private class Entry(val file: FileRef, val stored: (from: Long) -> HttpURLConnection, val cipher: ContentCipher, val at: Long)

    private val entries = ConcurrentHashMap<String, Entry>()
    private val random = SecureRandom()
    private var socket: ServerSocket? = null

    /** The address the player plays [file] from, fetched with [auth]'s key and decrypted with [cipher]. */
    fun url(context: Context, file: FileRef, auth: String, cipher: ContentCipher): String {
        val app = context.applicationContext
        return url(file, cipher) { from -> encrypted(app, file, auth, from) }
    }

    /** The address for [file], whose stored bytes from an offset on [stored] fetches; for the tests too. */
    internal fun url(file: FileRef, cipher: ContentCipher, stored: (from: Long) -> HttpURLConnection): String {
        val now = System.currentTimeMillis()
        entries.entries.removeIf { now - it.value.at > LIFE_MS }
        val token = Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(18).also { random.nextBytes(it) })
        entries[token] = Entry(file, stored, cipher, now)
        return "http://127.0.0.1:${port()}/$token/${URLEncoder.encode(file.name, "UTF-8").replace("+", "%20")}"
    }

    @Synchronized
    private fun port(): Int {
        socket?.takeIf { !it.isClosed }?.let { return it.localPort }
        val s = ServerSocket(0, 16, InetAddress.getByName("127.0.0.1"))
        socket = s
        thread(isDaemon = true, name = "local-stream") {
            while (!s.isClosed) {
                val client = try {
                    s.accept()
                } catch (e: IOException) {
                    break
                }
                thread(isDaemon = true, name = "local-stream-client") { serve(client) }
            }
        }
        return s.localPort
    }

    private fun serve(client: Socket) {
        client.use {
            try {
                client.soTimeout = 30_000
                val input = client.getInputStream()
                val out = client.getOutputStream()
                val head = readHead(input) ?: return
                val lines = head.split("\r\n")
                val parts = lines.first().split(' ')
                if (parts.size < 2) return respond(out, 400)
                val method = parts[0]
                val headers = lines.drop(1).filter { ':' in it }.associate { it.substringBefore(':').trim().lowercase() to it.substringAfter(':').trim() }
                if (method != "GET" && method != "HEAD") return respond(out, 405)
                val entry = entries[parts[1].trimStart('/').substringBefore('/')] ?: return respond(out, 404)
                answer(entry, method == "HEAD", headers["range"], out)
            } catch (e: IOException) {
                // the player went away, or the server did
            }
        }
    }

    /** One answer: the whole file, or the range asked for, decrypted. */
    private fun answer(e: Entry, head: Boolean, rangeHeader: String?, out: OutputStream) {
        val size = e.cipher.plainSize
        val range = parseRange(rangeHeader, size)
        if (range == Range.Unsatisfiable) return respond(out, 416, mapOf("Content-Range" to "bytes */$size"))
        val (start, end) = (range as? Range.Span)?.let { it.start to it.end } ?: (0L to size)
        val headers = mapOf(
            "Content-Type" to e.file.mime,
            "Content-Length" to (end - start).toString(),
            "Accept-Ranges" to "bytes",
            "Cache-Control" to "no-store",
        ) + if (range is Range.Span) mapOf("Content-Range" to "bytes $start-${end - 1}/$size") else emptyMap()
        val status = if (range is Range.Span) 206 else 200
        if (head || end == start) return respond(out, status, headers)
        val at = E2ee.readStart(start)
        val conn = e.stored(at.cipherOffset)
        val answered = conn.responseCode
        if (answered != 206 || Downloader.contentRange(conn.getHeaderField("Content-Range"))?.first != at.cipherOffset) {
            conn.disconnect()
            throw IOException("the stored bytes from ${at.cipherOffset}: HTTP $answered")
        }
        try {
            val plain = DecryptingStream(conn.inputStream, e.cipher, at.chunk, at.skip)
            respond(out, status, headers)
            val buffer = ByteArray(64 * 1024)
            var left = end - start
            while (left > 0) {
                val n = plain.read(buffer, 0, minOf(buffer.size.toLong(), left).toInt())
                if (n < 0) break
                out.write(buffer, 0, n)
                left -= n
            }
            out.flush()
        } catch (x: E2eeException) {
            Log.w(TAG, "${e.file.id} doesn't decrypt: ${x.message}")
        } finally {
            conn.disconnect()
        }
    }

    /** A request for the stored bytes of [file] from [from] on: to the server with the key, or to the bucket by the file's link. */
    private fun encrypted(app: Context, file: FileRef, auth: String, from: Long): HttpURLConnection {
        val (token, config) = Credentials.of(app, auth) ?: throw IOException("signed out")
        val server = ServerConnection(config, token)
        val home = Credentials.atHome(auth, RouteMonitor.current(app).isLocal)
        val info = ServerInfo.of(config) { server.open(it, home, readTimeoutMs = 15_000) } ?: throw IOException("the server didn't say where its files are")
        val conn = if (info.storage == ServerInfo.Storage.S3) {
            when (val link = S3Links.fetch(file.id) { server.open(it, home) }) {
                is S3Links.Answer.Link -> S3Connection.open(link.url)
                else -> throw IOException("no link: $link")
            }
        } else {
            server.open("/api/files/${file.id}/content", home)
        }
        conn.setRequestProperty("Accept-Encoding", "identity")
        conn.setRequestProperty("Range", "bytes=$from-")
        return conn
    }

    /** A range of a file, or none; as the server (and sw.ts) read them. */
    internal sealed interface Range {
        data class Span(val start: Long, val end: Long) : Range

        data object Unsatisfiable : Range
    }

    /** A Range header's span of a file of [size] bytes: [start, end), null for none or one that isn't understood. */
    internal fun parseRange(header: String?, size: Long): Range? {
        val m = Regex("""bytes=(\d*)-(\d*)""").matchEntire(header?.trim() ?: return null) ?: return null
        val (a, b) = m.destructured
        if (a.isEmpty() && b.isEmpty()) return null
        if (a.isEmpty()) return Range.Span(maxOf(0, size - b.toLong()), size)
        val start = a.toLong()
        val end = if (b.isEmpty()) size else minOf(size, b.toLong() + 1)
        if (start >= size || start >= end) return Range.Unsatisfiable
        return Range.Span(start, end)
    }

    private fun respond(out: OutputStream, status: Int, headers: Map<String, String> = emptyMap()) {
        val reason = when (status) {
            200 -> "OK"
            206 -> "Partial Content"
            404 -> "Not Found"
            405 -> "Method Not Allowed"
            416 -> "Range Not Satisfiable"
            else -> "Error"
        }
        val text = StringBuilder("HTTP/1.1 $status $reason\r\n")
        headers.forEach { (k, v) -> text.append("$k: $v\r\n") }
        if (!headers.containsKey("Content-Length")) text.append("Content-Length: 0\r\n")
        text.append("Connection: close\r\n\r\n")
        out.write(text.toString().toByteArray(Charsets.ISO_8859_1))
        out.flush()
    }

    /** The request's head, up to its empty line; null if the connection ends first or it is too long. */
    private fun readHead(input: InputStream): String? {
        val head = ByteArrayOutputStream()
        var matched = 0
        while (matched < 4) {
            val b = input.read()
            if (b < 0 || head.size() > 16 * 1024) return null
            head.write(b)
            matched = if (b == "\r\n\r\n"[matched].code) matched + 1 else if (b == '\r'.code) 1 else 0
        }
        return head.toString(Charsets.ISO_8859_1.name()).trimEnd()
    }
}
