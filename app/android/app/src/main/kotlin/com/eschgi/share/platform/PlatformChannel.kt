package com.eschgi.share.platform

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PersistableBundle
import android.provider.Settings
import android.util.Log
import android.view.WindowManager
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.app.ActivityCompat
import androidx.core.net.toUri
import com.eschgi.share.BuildConfig
import com.eschgi.share.MainActivity
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerStore
import com.eschgi.share.net.RouteMonitor
import com.eschgi.share.net.RouteStatus
import com.eschgi.share.net.ServerInfo
import com.eschgi.share.transfer.Credentials
import com.eschgi.share.transfer.Downloads
import com.eschgi.share.transfer.Fetcher
import com.eschgi.share.transfer.FileRef
import com.eschgi.share.transfer.Outbox
import com.eschgi.share.transfer.Picked
import com.eschgi.share.transfer.Playback
import com.eschgi.share.transfer.UploadBatch
import com.eschgi.share.transfer.Uploads
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
    /** The pick under way: who hears of it, how its files are sent and, signed in, into which folder. */
    private var picking: Triple<MethodChannel.Result, String, String?>? = null

    private val routeListener: (RouteStatus) -> Unit = { send(it.toMap() + ("type" to "route")) }
    private val transferListener: (Map<String, Any?>) -> Unit = { send(it) }

    init {
        methods.setMethodCallHandler(this)
        events.setStreamHandler(this)
        RouteMonitor.init(app)
        io.execute {
            Downloads.resumeIfNeeded(app)
            Uploads.resumeIfNeeded(app)
        }
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
        if (intent.action == MainActivity.ACTION_RETRY_UPLOADS) io.execute { Uploads.retry(app) }
        if (Outbox.isShare(intent)) return receiveShare(intent, initial)
        val link = linkOf(intent) ?: return
        if (initial) initialLink = link else send(mapOf("type" to "link", "url" to link))
    }

    /** "Send with Share": the files wait in the outbox for the Dart side; a shared text with a link opens it. */
    private fun receiveShare(intent: Intent, initial: Boolean) {
        val uris = Outbox.uris(intent)
        if (uris.isEmpty()) {
            val link = Outbox.text(intent)?.let { Outbox.linkIn(it) } ?: return
            if (initial) initialLink = link else send(mapOf("type" to "link", "url" to link))
            return
        }
        // The sharing app lends the files while this screen lasts: keep or copy them now.
        io.execute {
            val received = try {
                Outbox.receive(app, uris)
            } catch (e: Exception) {
                Log.w(TAG, "shared files", e)
                Outbox.Received(0, uris.size)
            }
            send(sharedEvent(received.skipped))
        }
    }

    private fun sharedEvent(skipped: Int = 0): Map<String, Any?> = mapOf("type" to "shared", "count" to Outbox.pending(app).size, "skipped" to skipped)

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "secret.read" -> background(result) { secrets.read(call.argument<String>("key")!!) }
            "secret.write" -> background(result) {
                secrets.write(call.argument<String>("key")!!, call.argument<String>("value"))
                null
            }
            "server.load" -> background(result) { server.load(call.argument<String>("slot") ?: ServerStore.DEVICE) }
            "server.save" -> background(result) {
                server.save(call.argument<String>("json"), call.argument<String>("slot") ?: ServerStore.DEVICE)
                RouteMonitor.invalidate()
                ServerInfo.forget()
                null
            }
            "route.get" -> background(result) {
                val check = call.argument<Boolean>("check") == true
                (if (check) RouteMonitor.check(app) else RouteMonitor.current(app)).toMap()
            }
            "device.name" -> result.success(deviceName())
            "app.version" -> result.success(mapOf("name" to BuildConfig.VERSION_NAME, "code" to BuildConfig.VERSION_CODE.toLong()))
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
            "clipboard.secret" -> {
                // A password: Android 13 and later don't show it in the clipboard's preview.
                val clip = ClipData.newPlainText("", call.argument<String>("text"))
                clip.description.extras = PersistableBundle().apply {
                    putBoolean(if (Build.VERSION.SDK_INT >= 33) ClipDescription.EXTRA_IS_SENSITIVE else "android.content.extra.IS_SENSITIVE", true)
                }
                app.getSystemService(ClipboardManager::class.java).setPrimaryClip(clip)
                result.success(null)
            }
            "text.share" -> {
                val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, call.argument<String>("text"))
                activity.startActivity(Intent.createChooser(send, null))
                result.success(null)
            }
            "transfer.download" -> {
                askForNotifications()
                background(result) { Downloads.enqueue(app, FileRef.parseList(call.argument<String>("files")), auth(call)) }
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
            "upload.pick" -> pick(call.argument<String>("what"), call.argument<String>("auth") ?: UploadBatch.DEVICE, call.argument<String>("folder"), result)
            "shared.count" -> background(result) { Outbox.pending(app).size }
            "shared.send" -> background(result) {
                val auth = call.argument<String>("auth") ?: UploadBatch.DEVICE
                Outbox.sendWith(app) { files -> Uploads.enqueue(app, auth, files, call.argument<String>("folder")) }
            }
            "shared.drop" -> background(result) {
                Outbox.drop(app)
                null
            }
            "upload.cancel" -> background(result) {
                Uploads.cancel(app, call.argument<String>("batch") ?: "")
                null
            }
            "upload.resume" -> background(result) {
                Uploads.resume(app, call.argument<String>("auth"), call.argument<String>("folder"))
                null
            }
            "transfer.saved" -> background(result) { Downloads.saved(app, call.argument<List<String>>("ids") ?: emptyList()) }
            "file.share" -> background(result) {
                val files = FileRef.parseList(call.argument<String>("files"))
                val uris = Fetcher.fetch(app, files, auth(call)) ?: return@background null
                main.post { share(files, uris) }
                null
            }
            "file.open" -> {
                io.execute {
                    try {
                        val file = FileRef.parseList("[" + call.argument<String>("file") + "]").single()
                        val uri = Fetcher.fetch(app, listOf(file), auth(call))?.single()
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
            "file.play" -> background(result) {
                Playback.source(app, FileRef.parseList("[" + call.argument<String>("file") + "]").single(), auth(call))
            }
            "screen.awake" -> {
                // While something plays, the screen stays on.
                if (call.argument<Boolean>("on") == true) {
                    activity.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                } else {
                    activity.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                }
                result.success(null)
            }
            "cache.dir" -> result.success(app.cacheDir.absolutePath)
            else -> result.notImplemented()
        }
    }

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
        RouteMonitor.listen(routeListener)
        Downloads.listen(transferListener)
        Uploads.listen(transferListener)
        io.execute {
            // A new listener starts from where things are, not from the next change.
            send(RouteMonitor.current(app).toMap() + ("type" to "route"))
            Downloads.snapshots(app).forEach { send(it.toMap()) }
            Uploads.snapshots(app).forEach { send(it.toMap()) }
            if (Outbox.pending(app).isNotEmpty()) send(sharedEvent())
        }
    }

    override fun onCancel(arguments: Any?) {
        sink = null
        RouteMonitor.unlisten(routeListener)
        Downloads.unlisten(transferListener)
        Uploads.unlisten(transferListener)
    }

    /** What to send: from the photo picker, or any files from the document picker. */
    /** Whose key a call's files go with: the phone's, unless it says a PIN's. */
    private fun auth(call: MethodCall) = if (call.argument<String>("auth") == Credentials.PIN) Credentials.PIN else Credentials.DEVICE

    private fun pick(what: String?, auth: String, folder: String?, result: MethodChannel.Result) {
        val intent = if (what == "documents") {
            ActivityResultContracts.OpenMultipleDocuments().createIntent(activity, arrayOf("*/*"))
        } else {
            ActivityResultContracts.PickMultipleVisualMedia()
                .createIntent(activity, PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo))
        }
        picking?.first?.success(null) // an earlier pick that never came back
        picking = Triple(result, auth, folder)
        try {
            activity.startActivityForResult(intent, REQUEST_PICK)
        } catch (e: ActivityNotFoundException) {
            picking = null
            result.error("no_app", e.message, null)
        }
    }

    /** The picker's answer; true if it was ours. */
    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode != REQUEST_PICK) return false
        val (result, auth, folder) = picking ?: return true
        picking = null
        val uris = LinkedHashSet<Uri>()
        if (resultCode == Activity.RESULT_OK && data != null) {
            data.data?.let { uris += it }
            data.clipData?.let { clip -> for (i in 0 until clip.itemCount) clip.getItemAt(i).uri?.let { uris += it } }
        }
        if (uris.isEmpty()) {
            result.success(null)
            return true
        }
        io.execute {
            try {
                val picked = uris.mapNotNull { describe(it) }
                val batch = if (picked.isEmpty()) null else Uploads.enqueue(app, auth, picked, folder)
                main.post { result.success(batch) }
            } catch (e: Exception) {
                Log.w(TAG, "sending picked files", e)
                main.post { result.error("failed", e.message, null) }
            }
        }
        return true
    }

    /** Name, size and type of a picked file, keeping the permission to read it after a restart. */
    private fun describe(uri: Uri): Picked? {
        try {
            app.contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
        } catch (e: SecurityException) {
            // Not every picker allows it; then it only works until the phone restarts.
        }
        return Outbox.describe(app, uri)
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
        private const val REQUEST_PICK = 7002
    }
}
