package com.eschgi.share

import android.content.Intent
import com.eschgi.share.platform.PlatformChannel
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine

class MainActivity : FlutterActivity() {

    private var platform: PlatformChannel? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        platform = PlatformChannel(this, flutterEngine.dartExecutor.binaryMessenger).also { it.onIntent(intent, initial = true) }
    }

    // singleTop: an invite link tapped while the app is open arrives here.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        platform?.onIntent(intent, initial = false)
    }

    // The pickers of the send screen, and the QR code scanner, answer here.
    @Deprecated("Flutter's embedding still uses it")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (platform?.onActivityResult(requestCode, resultCode, data) != true) super.onActivityResult(requestCode, resultCode, data)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        platform?.onRequestPermissionsResult(requestCode)
    }

    override fun onDestroy() {
        platform?.detach()
        platform = null
        super.onDestroy()
    }

    companion object {
        /** From the downloads' notification: the app opens and starts them again. */
        const val ACTION_RETRY_DOWNLOADS = "com.eschgi.share.RETRY_DOWNLOADS"
        const val ACTION_RETRY_UPLOADS = "com.eschgi.share.RETRY_UPLOADS"

        /** The aliases of this activity in share sheets (AndroidManifest.xml): Send as ZIP, and Open in Share for ZIPs. */
        const val SEND_AS_ZIP = "com.eschgi.share.SendAsZip"
        const val OPEN_IN_SHARE = "com.eschgi.share.OpenInShare"
    }
}
