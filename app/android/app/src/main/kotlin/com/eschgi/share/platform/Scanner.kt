package com.eschgi.share.platform

import android.app.Activity
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.android.gms.common.moduleinstall.InstallStatusListener
import com.google.android.gms.common.moduleinstall.ModuleInstall
import com.google.android.gms.common.moduleinstall.ModuleInstallClient
import com.google.android.gms.common.moduleinstall.ModuleInstallRequest
import com.google.android.gms.common.moduleinstall.ModuleInstallStatusUpdate.InstallState
import com.google.mlkit.common.MlKitException
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScanner
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import io.flutter.plugin.common.MethodChannel

/**
 * Scans an invite's QR code with Google's code scanner, which needs no camera permission. Its
 * module comes with Google Play services: with the app when it is installed from the Play Store
 * (com.google.mlkit.vision.DEPENDENCIES in the manifest), else here before the first scan, which
 * waits for it a while. Answers the code's text, or null when the person backed out; otherwise an
 * error, and Dart asks for the link instead: "unavailable" without Google Play services,
 * "outdated" when they are too old, "installing" while the module isn't there yet, "failed" for
 * anything else. Before, every error but one came back as a backing out, and nothing happened
 * (issue #5).
 */
class Scanner(private val activity: Activity) {
    private val main = Handler(Looper.getMainLooper())

    fun scan(result: MethodChannel.Result) {
        val reply = Once(result)
        if (GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(activity) != ConnectionResult.SUCCESS) {
            reply.error("unavailable", "no Google Play services")
            return
        }
        val options = GmsBarcodeScannerOptions.Builder()
            .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
            .enableAutoZoom()
            .build()
        val scanner = GmsBarcodeScanning.getClient(activity, options)
        val modules = ModuleInstall.getClient(activity)
        modules.areModulesAvailable(scanner)
            .addOnSuccessListener { if (it.areModulesAvailable()) start(scanner, reply) else install(modules, scanner, reply) }
            .addOnFailureListener { start(scanner, reply) } // the scanner says what is wrong
    }

    /** Installs the scanner's module, and scans once it is there; after [INSTALL_WAIT], the person pastes the link meanwhile. */
    private fun install(modules: ModuleInstallClient, scanner: GmsBarcodeScanner, reply: Once) {
        lateinit var listener: InstallStatusListener
        val giveUp = Runnable {
            modules.unregisterListener(listener)
            reply.error("installing", "the code scanner is still being installed")
        }
        listener = InstallStatusListener { update ->
            when (update.installState) {
                InstallState.STATE_COMPLETED -> {
                    main.removeCallbacks(giveUp)
                    modules.unregisterListener(listener)
                    start(scanner, reply)
                }
                InstallState.STATE_FAILED, InstallState.STATE_CANCELED -> {
                    main.removeCallbacks(giveUp)
                    modules.unregisterListener(listener)
                    Log.w(TAG, "installing the code scanner: state ${update.installState}, error ${update.errorCode}")
                    reply.error("installing", "the code scanner couldn't be installed")
                }
            }
        }
        modules.installModules(ModuleInstallRequest.newBuilder().addApi(scanner).setListener(listener).build())
            .addOnSuccessListener {
                if (it.areModulesAlreadyInstalled()) {
                    modules.unregisterListener(listener)
                    start(scanner, reply)
                } else {
                    main.postDelayed(giveUp, INSTALL_WAIT)
                }
            }
            .addOnFailureListener { e ->
                modules.unregisterListener(listener)
                Log.w(TAG, "installing the code scanner", e)
                reply.error("installing", e.message)
            }
    }

    private fun start(scanner: GmsBarcodeScanner, reply: Once) {
        scanner.startScan()
            .addOnSuccessListener { reply.success(it.rawValue) }
            .addOnCanceledListener { reply.success(null) }
            .addOnFailureListener { e ->
                val code = (e as? MlKitException)?.errorCode
                Log.w(TAG, "scan failed: ${code ?: e.javaClass.simpleName}", e)
                when (code) {
                    // backed out, or a scanner is open already
                    MlKitException.CODE_SCANNER_CANCELLED, MlKitException.CODE_SCANNER_TASK_IN_PROGRESS -> reply.success(null)
                    MlKitException.UNAVAILABLE, MlKitException.CODE_SCANNER_UNAVAILABLE -> {
                        ModuleInstall.getClient(activity).installModules(ModuleInstallRequest.newBuilder().addApi(scanner).build())
                        reply.error("installing", e.message)
                    }
                    MlKitException.CODE_SCANNER_GOOGLE_PLAY_SERVICES_VERSION_TOO_OLD -> reply.error("outdated", e.message)
                    else -> reply.error("failed", e.message)
                }
            }
    }

    /** A result answered once: the scan and the install may both want to. */
    private class Once(private val result: MethodChannel.Result) {
        private var done = false

        fun success(value: Any?) {
            if (done) return
            done = true
            result.success(value)
        }

        fun error(code: String, message: String?) {
            if (done) return
            done = true
            result.error(code, message, null)
        }
    }

    private companion object {
        const val TAG = "Scanner"

        /** How long the first scan waits for the scanner's module to arrive. */
        const val INSTALL_WAIT = 30_000L
    }
}
