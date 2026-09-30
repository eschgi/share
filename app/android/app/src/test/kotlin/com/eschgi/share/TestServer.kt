package com.eschgi.share

import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import javax.net.ServerSocketFactory
import javax.net.ssl.SSLContext
import kotlin.concurrent.thread

/**
 * A small HTTP/1.1 server for the unit tests, over TLS when given a context. (The JDK's
 * com.sun.net.httpserver isn't on the Android unit tests' compile classpath.) One request
 * per connection, which HttpURLConnection copes with.
 */
class TestServer(tls: SSLContext? = null, private val handle: (Request, Response) -> Unit) : Closeable {

    class Request(val method: String, val path: String, private val headers: Map<String, String>, val body: InputStream) {
        fun header(name: String): String? = headers[name.lowercase()]

        val contentLength: Long get() = header("Content-Length")?.toLongOrNull() ?: 0
    }

    class Response internal constructor(private val out: OutputStream) {
        internal var sent = false

        /**
         * Answers with [body]; [length] is the Content-Length announced, and [cutAfter] ends the
         * connection after that many body bytes, as a dropped connection would.
         */
        fun send(status: Int, body: ByteArray = ByteArray(0), headers: Map<String, String> = emptyMap(), length: Long = body.size.toLong(), cutAfter: Int? = null) {
            sent = true
            val head = StringBuilder("HTTP/1.1 $status ${reason(status)}\r\n")
            headers.forEach { (k, v) -> head.append("$k: $v\r\n") }
            head.append("Content-Length: $length\r\nConnection: close\r\n\r\n")
            out.write(head.toString().toByteArray())
            out.write(body, 0, minOf(body.size, cutAfter ?: body.size))
            out.flush()
        }

        private fun reason(status: Int) = when (status) {
            200 -> "OK"
            201 -> "Created"
            204 -> "No Content"
            206 -> "Partial Content"
            401 -> "Unauthorized"
            404 -> "Not Found"
            503 -> "Service Unavailable"
            else -> "Status"
        }
    }

    private val socket: ServerSocket =
        (tls?.serverSocketFactory ?: ServerSocketFactory.getDefault()).createServerSocket(0, 50, InetAddress.getByName("127.0.0.1"))

    val port: Int get() = socket.localPort

    init {
        thread(isDaemon = true, name = "test-server") {
            while (!socket.isClosed) {
                val client = try {
                    socket.accept()
                } catch (e: IOException) {
                    break
                }
                thread(isDaemon = true) { serve(client) }
            }
        }
    }

    private fun serve(client: Socket) {
        client.use {
            try {
                val input = it.getInputStream()
                val lines = readHead(input).split("\r\n")
                val (method, path) = lines.first().split(' ').let { parts -> parts[0] to parts[1] }
                val headers = lines.drop(1).filter { line -> ':' in line }
                    .associate { line -> line.substringBefore(':').trim().lowercase() to line.substringAfter(':').trim() }
                val response = Response(it.getOutputStream())
                handle(Request(method, path, headers, input), response)
                if (!response.sent) response.send(500)
            } catch (e: IOException) {
                // e.g. the client refused the certificate
            }
        }
    }

    private fun readHead(input: InputStream): String {
        val head = ByteArrayOutputStream()
        var matched = 0
        while (matched < 4) {
            val b = input.read()
            if (b < 0) throw IOException("closed before the request ended")
            head.write(b)
            matched = if (b == "\r\n\r\n"[matched].code) matched + 1 else if (b == '\r'.code) 1 else 0
        }
        return head.toString(Charsets.ISO_8859_1.name()).trimEnd()
    }

    override fun close() = socket.close()
}
