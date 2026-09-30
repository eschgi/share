package com.eschgi.share.platform

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.Log
import androidx.core.net.toUri
import androidx.core.app.ActivityCompat
import com.eschgi.share.BuildConfig
import com.eschgi.share.MainActivity
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.RouteStatus
import com.eschgi.share.transfer.Downloads
import com.eschgi.share.transfer.Fetcher
import com.eschgi.share.transfer.FileRef
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

/**
 * The Dart side's Platform (lib/data/platform.dart): one method channel, and one event
 * channel for routes, links and transfers. Anything that touches the disk, the KeyStore or
 * the network runs off the main thread.
 */
class PlatformChannel(private val activity: Activity, messenger: BinaryMessenger) :
    MethodChannel.MethodCallHandler, EventChannel.StreamHandler {

    private val app = activity.applicationContext
    private val methods = MethodChannel(messenger, "com.eschgi.share/platform")
    private val events = EventChannel(messenger, "com.eschgi.share/events")
    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newFixedThreadPool(4)
    private val secrets = SecretStore(app)
    private val server = ServerStore(app)
    private val scanner = Scanner(activity)

    private var sink: EventChannel.EventSink? = null
    private var initialLink: String? = null

    private val routeListener: (RouteStatus) -> Unit = { send(it.toMap() + ("type" to "route")) }
    private val transferListener: (Map<String, Any?>) -> Unit = { send(it) }

    init {
        methods.setMethodCallHandler(this)
        events.setStreamHandler(this)
        RouteMonitor.init(app)
        io.execute { Downloads.resumeIfNeeded(app) }
    }

    fun detach() {
        methods.setMethodCallHandler(null)
        events.setStreamHandler(null)
        onCancel(null)
        io.shutdown()
    }

    /** An intent that opened the app, or reached it while open. */
    fun onIntent(intent: Intent?, initial: Boolean) {
        if (intent == null) return
        if (intent.action == MainActivity.ACTION_RETRY_DOWNLOADS) io.execute { Downloads.retry(app) }
        val link = linkOf(intent) ?: return
        if (initial) initialLink = link else send(mapOf("type" to "link", "url" to link))
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "secret.read" -> background(result) { secrets.read(call.argument<String>("key")!!) }
            "secret.write" -> background(result) {
                secrets.write(call.argument<String>("key")!!, call.argument<String>("value"))
                null
            }
            "server.load" -> background(result) { server.load() }
            "server.save" -> background(result) {
                server.save(call.argument<String>("json"))
                RouteMonitor.invalidate()
                null
            }
            "route.get" -> background(result) {
                val check = call.argument<Boolean>("check") == true
                (if (check) RouteMonitor.check(app) else RouteMonitor.current(app)).toMap()
            }
            "device.name" -> result.success(deviceName())
            "link.initial" -> {
                result.success(initialLink)
                initialLink = null
            }
            "scan" -> scanner.scan(result)
            "url.open" -> {
                val url = call.argument<String>("url")
                try {
                    activity.startActivity(Intent(Intent.ACTION_VIEW, url.orEmpty().toUri()))
                    result.success(null)
                } catch (e: ActivityNotFoundException) {
                    result.error("no_app", e.message, null)
                }
            }
            "transfer.download" -> {
                askForNotifications()
                background(result) { Downloads.enqueue(app, FileRef.parseList(call.argument<String>("files"))) }
            }
            "transfer.cancel" -> {
                val batch = call.argument<String>("batch") ?: ""
                if (batch == Fetcher.BATCH) {
                    Fetcher.cancel()
                    result.success(null)
                } else {
                    background(result) {
                        Downloads.cancel(app, batch)
                        null
                    }
                }
            }
            "transfer.saved" -> background(result) { Downloads.saved(app, call.argument<List<String>>("ids") ?: emptyList()) }
            "file.share" -> background(result) {
                val files = FileRef.parseList(call.argument<String>("files"))
                val uris = Fetcher.fetch(app, files) ?: return@background null
                main.post { share(files, uris) }
                null
            }
            "file.open" -> {
                io.execute {
                    try {
                        val file = FileRef.parseList("[" + call.argument<String>("file") + "]").single()
                        val uri = Fetcher.fetch(app, listOf(file))?.single()
                        main.post {
                            if (uri == null) {
                                result.success(null)
                                return@post
                            }
                            try {
                                activity.startActivity(
                                    Intent(Intent.ACTION_VIEW).setDataAndType(uri, file.mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION),
                                )
                                result.success(null)
                            } catch (e: ActivityNotFoundException) {
                                result.error("no_app", e.message, null)
                            }
                        }
                    } catch (e: Exception) {
                        Log.w(TAG, "file.open", e)
                        main.post { result.error("failed", e.message, null) }
                    }
                }
            }
            "cache.dir" -> result.success(app.cacheDir.absolutePath)
            else -> result.notImplemented()
        }
    }

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
        RouteMonitor.listen(routeListener)
        Downloads.listen(transferListener)
        io.execute {
            // A new listener starts from where things are, not from the next change.
            send(RouteMonitor.current(app).toMap() + ("type" to "route"))
            Downloads.snapshots(app).forEach { send(it.toMap()) }
        }
    }

    override fun onCancel(arguments: Any?) {
        sink = null
        RouteMonitor.unlisten(routeListener)
        Downloads.unlisten(transferListener)
    }

    fun onRequestPermissionsResult(requestCode: Int) {
        // Nothing waits on it: without the permission, downloads run without a notification.
        if (requestCode == REQUEST_NOTIFICATIONS) Log.i(TAG, "notification permission answered")
    }

    private fun send(event: Map<String, Any?>) {
        main.post { sink?.success(event) }
    }

    private fun background(result: MethodChannel.Result, work: () -> Any?) {
        io.execute {
            try {
                val value = work()
                main.post { result.success(value) }
            } catch (e: Exception) {
                Log.w(TAG, "platform call failed", e)
                main.post { result.error("failed", e.message, null) }
            }
        }
    }

    private fun share(files: List<FileRef>, uris: List<Uri>) {
        if (uris.isEmpty()) return
        val types = files.map { it.mime }.distinct()
        val type = when {
            types.size == 1 -> types[0]
            types.map { it.substringBefore('/') }.distinct().size == 1 -> types[0].substringBefore('/') + "/*"
            else -> "*/*"
        }
        val intent = if (uris.size == 1) {
            Intent(Intent.ACTION_SEND).putExtra(Intent.EXTRA_STREAM, uris[0])
        } else {
            Intent(Intent.ACTION_SEND_MULTIPLE).putParcelableArrayListExtra(Intent.EXTRA_STREAM, ArrayList(uris))
        }
        intent.type = type
        intent.clipData = ClipData.newRawUri(null, uris[0]).apply { uris.drop(1).forEach { addItem(ClipData.Item(it)) } }
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        activity.startActivity(Intent.createChooser(intent, null))
    }

    private fun askForNotifications() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (ActivityCompat.checkSelfPermission(activity, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) return
        ActivityCompat.requestPermissions(activity, arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFICATIONS)
    }

    private fun deviceName(): String {
        val named = Settings.Global.getString(app.contentResolver, Settings.Global.DEVICE_NAME)
        if (!named.isNullOrBlank()) return named.trim()
        val maker = Build.MANUFACTURER.replaceFirstChar { it.titlecase() }
        return if (Build.MODEL.startsWith(Build.MANUFACTURER, ignoreCase = true)) Build.MODEL else "$maker ${Build.MODEL}"
    }

    /** The invite link the invite page hands over: com.eschgi.share://join?server=…&token=…. */
    private fun linkOf(intent: Intent): String? {
        if (intent.action != Intent.ACTION_VIEW) return null
        val data = intent.data ?: return null
        return data.toString().takeIf { data.scheme == BuildConfig.LINK_SCHEME }
    }

    companion object {
        private const val TAG = "PlatformChannel"
        private const val REQUEST_NOTIFICATIONS = 7001
    }
}
