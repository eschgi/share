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
import android.os.storage.StorageManager
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
import com.eschgi.share.e2ee.E2ee
import com.eschgi.share.e2ee.E2eeException
import com.eschgi.share.e2ee.Keys
import com.eschgi.share.e2ee.KeysApiError
import com.eschgi.share.e2ee.NeedsRoot
import com.eschgi.share.e2ee.SealedFile
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
import com.eschgi.share.zip.ZipOpening
import com.eschgi.share.zip.ZipSending
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
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

    private var sink: EventChannel.EventSink? = null
    private var initialLink: String? = null
    /** The pick under way: who hears of it, how its files are sent and, signed in, into which folder. */
    private var picking: Triple<MethodChannel.Result, String, String?>? = null
    /** Who hears of the scan under way. */
    private var scanning: MethodChannel.Result? = null
    /** Who hears of the photo picker opened for a ZIP. */
    private var zipPicking: MethodChannel.Result? = null

    /**
     * ZIPs (docs/zip-plan.md): what arrived for the Dart side to take, "pack" or "open", and one thread
     * for them, so that taking it waits until the files are described or the ZIPs open.
     */
    @Volatile private var zipWaiting: String? = null
    private val zipIo = Executors.newSingleThreadExecutor()

    private val routeListener: (RouteStatus) -> Unit = { send(it.toMap() + ("type" to "route")) }
    private val transferListener: (Map<String, Any?>) -> Unit = { send(it) }
    private val keysListener: (Map<String, Any?>) -> Unit = { send(it) }
    private val zipListener: (Map<String, Any?>) -> Unit = { send(it) }

    init {
        methods.setMethodCallHandler(this)
        events.setStreamHandler(this)
        RouteMonitor.init(app)
        io.execute {
            Downloads.resumeIfNeeded(app)
            Uploads.resumeIfNeeded(app)
            // ZIPs sent a day ago, and pieces two weeks old, go by themselves.
            ZipSending.trim(app)
            ZipOpening.inbox(app).trim()
        }
    }

    fun detach() {
        methods.setMethodCallHandler(null)
        events.setStreamHandler(null)
        onCancel(null)
        io.shutdown()
        zipIo.shutdown()
    }

    /** An intent that opened the app, or reached it while open. */
    fun onIntent(intent: Intent?, initial: Boolean) {
        if (intent == null) return
        if (intent.action == MainActivity.ACTION_RETRY_DOWNLOADS) io.execute { Downloads.retry(app) }
        if (intent.action == MainActivity.ACTION_RETRY_UPLOADS) io.execute { Uploads.retry(app) }
        if (isSendAsZip(intent)) return receiveZip(intent, ZIP_PACK, initial)
        if (isZipToOpen(intent)) return receiveZip(intent, ZIP_OPEN, initial)
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

    /** The share sheet's Send as ZIP. */
    private fun isSendAsZip(intent: Intent) = Outbox.isShare(intent) && intent.component?.className == MainActivity.SEND_AS_ZIP

    /** A ZIP opened with Share (Open with), or ZIPs shared to Open in Share. */
    private fun isZipToOpen(intent: Intent): Boolean {
        if (Outbox.isShare(intent)) return intent.component?.className == MainActivity.OPEN_IN_SHARE
        if (intent.action != Intent.ACTION_VIEW || intent.data?.scheme != "content" && intent.data?.scheme != "file") return false
        return intent.type in ZIP_TYPES || intent.data?.lastPathSegment.orEmpty().endsWith(".zip", ignoreCase = true)
    }

    /**
     * Files for a ZIP, or ZIPs to open: the app that lends them allows it only while this screen is
     * there, so they are described or opened at once; the Dart side takes them with zip.take.
     */
    private fun receiveZip(intent: Intent, kind: String, initial: Boolean) {
        val uris = if (Outbox.isShare(intent)) Outbox.uris(intent) else listOfNotNull(intent.data)
        if (uris.isEmpty()) return
        zipIo.execute {
            try {
                if (kind == ZIP_PACK) ZipSending.fromUris(app, uris) else ZipOpening.open(app, uris)
                zipWaiting = kind
                if (!initial) send(mapOf("type" to "zip_in", "kind" to kind))
            } catch (e: Exception) {
                Log.w(TAG, "receiving a ZIP", e)
            }
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
            "scan" -> io.execute {
                val language = runCatching { secrets.read(SecretStore.LANGUAGE) }.getOrNull()
                main.post { scan(language, result) }
            }
            "app.settings" -> {
                // Share's page there, with its permissions.
                try {
                    activity.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", app.packageName, null)))
                    result.success(null)
                } catch (e: ActivityNotFoundException) {
                    result.error("no_app", e.message, null)
                }
            }
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
            "print.page" -> {
                try {
                    PagePrinter.print(activity, call.argument<ByteArray>("png")!!, call.argument<String>("name") ?: "Share")
                    result.success(null)
                } catch (e: RuntimeException) {
                    Log.w(TAG, "printing", e)
                    result.error("failed", e.message, null)
                }
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
            "zip.take" -> zipIo.execute {
                val kind = zipWaiting
                zipWaiting = null
                val value = when (kind) {
                    ZIP_PACK -> mapOf("kind" to kind, "files" to ZipSending.waiting(), "skipped" to ZipSending.lastSkipped)
                    ZIP_OPEN -> mapOf("kind" to kind)
                    else -> null
                }
                main.post { result.success(value) }
            }
            "zip.pick" -> {
                zipPicking?.success(null) // an earlier pick that never came back
                zipPicking = result
                try {
                    activity.startActivityForResult(
                        ActivityResultContracts.PickMultipleVisualMedia().createIntent(activity, PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo)),
                        REQUEST_ZIP_PICK,
                    )
                } catch (e: ActivityNotFoundException) {
                    zipPicking = null
                    result.error("no_app", e.message, null)
                }
            }
            "zip.library" -> background(result) {
                ZipSending.fromLibrary(app, FileRef.parseList(call.argument<String>("files")), call.argument<List<Number>>("taken").orEmpty().map { it.toLong() }, auth(call))
            }
            "zip.thumb" -> background(result) { ZipSending.thumb(app, call.argument<Int>("index") ?: -1) }
            "zip.plan" -> background(result) {
                ZipSending.plan(call.argument<String>("name") ?: "", call.argument<String>("about") ?: "", call.argument<Number>("limit")?.toLong())
            }
            "zip.pack" -> {
                ZipSending.pack(app, call.argument<String>("name") ?: "", call.argument<String>("about") ?: "", call.argument<String>("part_name") ?: "{name} ({part}/{parts})", call.argument<Number>("limit")?.toLong())
                result.success(null)
            }
            "zip.stop" -> background(result) {
                ZipSending.stop()
                null
            }
            "zip.send" -> {
                try {
                    ZipSending.send(activity, call.argument<Int>("part"))
                    result.success(null)
                } catch (e: ActivityNotFoundException) {
                    result.error("no_app", e.message, null)
                }
            }
            "zip.save_downloads" -> background(result) { ZipSending.saveToDownloads(app) }
            "zip.close" -> background(result) {
                ZipSending.close()
                null
            }
            "zip.contents" -> background(result) { ZipOpening.contents(app) }
            "zip.open_thumb" -> background(result) { ZipOpening.thumb(call.argument<Int>("index") ?: -1) }
            "zip.save" -> background(result) {
                val to = call.argument<String>("to") ?: ZipOpening.PHONE
                ZipOpening.save(app, to, call.argument<String>("folder"))
                if (to == ZipOpening.FOLDER) send(sharedEvent())
                null
            }
            "zip.close_opened" -> background(result) {
                ZipOpening.close()
                null
            }
            "gallery.open" -> {
                try {
                    activity.startActivity(Intent.makeMainSelectorActivity(Intent.ACTION_MAIN, Intent.CATEGORY_APP_GALLERY))
                    result.success(null)
                } catch (e: ActivityNotFoundException) {
                    result.error("no_app", e.message, null)
                }
            }
            "storage.free" -> {
                // Android's own way to free up space.
                try {
                    activity.startActivity(Intent(StorageManager.ACTION_MANAGE_STORAGE))
                    result.success(null)
                } catch (e: ActivityNotFoundException) {
                    result.error("no_app", e.message, null)
                }
            }
            else -> if (call.method.startsWith("keys.")) keys(call, result) else result.notImplemented()
        }
    }

    /**
     * End-to-end encryption (docs/e2ee-plan.md): the keys live here, in [Keys], where downloads
     * and uploads use them without Flutter; the screens ask through these methods. A key that
     * isn't open fails as "sealed", an error answer of the server with its code, no answer as
     * "offline".
     */
    private fun keys(call: MethodCall, result: MethodChannel.Result) {
        val ring = Keys.ring(app)
        fun file() = SealedFile.of(JSONObject(call.argument<String>("file")!!)) ?: throw E2eeException("not encrypted")
        io.execute {
            val value: Any? = try {
                when (call.method) {
                    "keys.sync" -> Keys.sync(app, call.argument<String>("password"), call.argument<Boolean>("quiet") == true)
                    "keys.state" -> ring.state()
                    "keys.thumb" -> Keys.thumb(app, auth(call), file(), call.argument<ByteArray>("data")!!)
                    "keys.decrypt" -> Keys.decrypt(app, auth(call), file(), call.argument<ByteArray>("data")!!)
                    "keys.encrypt_folder" -> ring.encryptFolder(call.argument<String>("folder")!!, call.argument<Int>("key_version")).toString()
                    "keys.switch_off" -> ring.switchOff(call.argument<String>("folder")!!, call.argument<Int>("key_version"), call.argument<String>("name")!!).toString()
                    "keys.new_folder" -> ring.newFolder(call.argument<String>("name")!!).toString()
                    "keys.renamed" -> ring.renamed(JSONObject(call.argument<String>("folder")!!), call.argument<String>("name")!!).toString()
                    "keys.make_recovery" -> ring.makeRecovery()
                    "keys.use_recovery" -> ring.useRecoveryCode(call.argument<String>("code")!!)
                    "keys.start_over" -> {
                        ring.startOver()
                        ring.state()
                    }
                    "keys.password_lock" -> if (ring.status == com.eschgi.share.e2ee.Keyring.Status.READY) E2ee.b64u(ring.passwordLock(call.argument<String>("password")!!)) else null
                    "keys.invite_keys" -> ring.inviteKeys(call.argument<List<String>>("folders")).let { (secret, keys, root) -> mapOf("secret" to secret, "keys" to keys.toString(), "root" to root) }
                    "keys.person_key" -> ring.personKeyForInvite()?.let { (secret, locked, root) -> mapOf("secret" to secret, "locked" to locked, "root" to root) }
                    "keys.pin_secret" -> ring.pinSecret(call.argument<String>("folder")!!)?.let { (secret, body) -> mapOf("secret" to secret, "body" to body.toString()) }
                    "keys.pin_link_secret" -> ring.pinLinkSecret(call.argument<String>("folder")!!, call.argument<String>("locked")!!, call.argument<Int>("version")!!)
                    "keys.pin_link_root" -> ring.pinLinkRoot(call.argument<String>("folder")!!)
                    "keys.move_keys" -> {
                        val files = JSONArray(call.argument<String>("files")!!)
                        ring.moveKeys(List(files.length()) { files.getJSONObject(it) }, call.argument<String>("target")!!).toString()
                    }
                    "keys.from_invite" -> {
                        val keys = JSONArray(call.argument<String>("keys") ?: "[]")
                        Keys.account(app)?.let { ring.fromInvite(it, call.argument<String>("secret"), List(keys.length()) { i -> keys.getJSONObject(i) }, call.argument<String>("root")) }
                        Keys.sync(app)
                    }
                    "keys.allow" -> {
                        ring.allow(call.argument<String>("kind")!!, call.argument<String>("id")!!)
                        ring.state()
                    }
                    "keys.deny" -> {
                        ring.deny(call.argument<String>("kind")!!, call.argument<String>("id")!!)
                        ring.state()
                    }
                    "keys.show" -> {
                        ring.show(call.argument<String>("kind")!!, call.argument<String>("id")!!)
                        ring.state()
                    }
                    "keys.hide" -> {
                        ring.hide(call.argument<String>("kind")!!, call.argument<String>("id")!!)
                        ring.state()
                    }
                    "keys.pin_link" -> Keys.pinLink(app, call.argument<String>("secret"), call.argument<String>("root"))
                    "keys.forget_pin" -> {
                        Keys.forgetPin(app)
                        null
                    }
                    else -> {
                        main.post { result.notImplemented() }
                        return@execute
                    }
                }
            } catch (e: E2eeException) {
                main.post { result.error("sealed", e.message, null) }
                return@execute
            } catch (e: NeedsRoot) {
                main.post { result.error("needs_root", e.message, null) }
                return@execute
            } catch (e: KeysApiError) {
                main.post { result.error(e.code, e.message, e.status) }
                return@execute
            } catch (e: IOException) {
                main.post { result.error("offline", e.message, null) }
                return@execute
            } catch (e: Exception) {
                Log.w(TAG, "${call.method} failed", e)
                main.post { result.error("failed", e.message, null) }
                return@execute
            }
            main.post { result.success(value) }
        }
    }

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        sink = events
        RouteMonitor.listen(routeListener)
        Downloads.listen(transferListener)
        Uploads.listen(transferListener)
        Keys.listen(keysListener)
        ZipSending.listen(zipListener)
        ZipOpening.listen(zipListener)
        io.execute {
            // A new listener starts from where things are, not from the next change.
            send(RouteMonitor.current(app).toMap() + ("type" to "route"))
            send(Keys.ring(app).state())
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
        Keys.unlisten(keysListener)
        ZipSending.unlisten(zipListener)
        ZipOpening.unlisten(zipListener)
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

    /** Share's QR code scanner, in the language picked in the app. */
    private fun scan(language: String?, result: MethodChannel.Result) {
        scanning?.success(null) // an earlier scan that never came back
        scanning = result
        activity.startActivityForResult(Intent(activity, ScanActivity::class.java).putExtra(ScanActivity.EXTRA_LANGUAGE, language), REQUEST_SCAN)
    }

    /** The scanner's answer: the code's text, null when closed, or why it couldn't scan. */
    private fun onScanned(resultCode: Int, data: Intent?) {
        val result = scanning ?: return
        scanning = null
        when (resultCode) {
            Activity.RESULT_OK -> result.success(data?.getStringExtra(ScanActivity.EXTRA_TEXT))
            ScanActivity.RESULT_PROBLEM -> result.error(data?.getStringExtra(ScanActivity.EXTRA_PROBLEM) ?: ScanActivity.PROBLEM_CAMERA, null, null)
            else -> result.success(null)
        }
    }

    /** The picker's or the scanner's answer; true if it was ours. */
    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        if (requestCode == REQUEST_SCAN) {
            onScanned(resultCode, data)
            return true
        }
        if (requestCode == REQUEST_ZIP_PICK) {
            onZipPicked(resultCode, data)
            return true
        }
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

    /** Photos and videos picked for a ZIP: described for the screen, which packs them while it's open. */
    private fun onZipPicked(resultCode: Int, data: Intent?) {
        val result = zipPicking ?: return
        zipPicking = null
        val uris = LinkedHashSet<Uri>()
        if (resultCode == Activity.RESULT_OK && data != null) {
            data.data?.let { uris += it }
            data.clipData?.let { clip -> for (i in 0 until clip.itemCount) clip.getItemAt(i).uri?.let { uris += it } }
        }
        if (uris.isEmpty()) {
            result.success(null)
            return
        }
        zipIo.execute {
            val files = try {
                ZipSending.fromUris(app, uris.toList())
            } catch (e: Exception) {
                Log.w(TAG, "describing picked files", e)
                null
            }
            main.post { result.success(files) }
        }
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
        private const val REQUEST_SCAN = 7003
        private const val REQUEST_ZIP_PICK = 7004
        private const val ZIP_PACK = "pack"
        private const val ZIP_OPEN = "open"
        private val ZIP_TYPES = setOf("application/zip", "application/x-zip-compressed", "application/x-zip")
    }
}
