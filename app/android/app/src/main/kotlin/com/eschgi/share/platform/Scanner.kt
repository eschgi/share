package com.eschgi.share.platform

import android.app.Activity
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.mlkit.common.MlKitException
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import io.flutter.plugin.common.MethodChannel

/**
 * Scans an invite's QR code with Google's code scanner, which needs no camera permission.
 * Without Google Play services it answers "unavailable", and the person pastes the link.
 */
class Scanner(private val activity: Activity) {

    fun scan(result: MethodChannel.Result) {
        if (GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(activity) != ConnectionResult.SUCCESS) {
            result.error("unavailable", "no Google Play services", null)
            return
        }
        val options = GmsBarcodeScannerOptions.Builder()
            .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
            .enableAutoZoom()
            .build()
        GmsBarcodeScanning.getClient(activity, options).startScan()
            .addOnSuccessListener { result.success(it.rawValue) }
            .addOnCanceledListener { result.success(null) }
            .addOnFailureListener { e ->
                val unavailable = e is MlKitException && e.errorCode == MlKitException.UNAVAILABLE
                result.error(if (unavailable) "unavailable" else "failed", e.message, null)
            }
    }
}
