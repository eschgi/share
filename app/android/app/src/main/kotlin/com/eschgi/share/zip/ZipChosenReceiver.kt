package com.eschgi.share.zip

import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build

/** Android's share sheet says which app took a ZIP; the ZIP screen shows it next to the part. Not exported: only our PendingIntent reaches it. */
class ZipChosenReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        @Suppress("DEPRECATION")
        val component = if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_CHOSEN_COMPONENT, ComponentName::class.java)
        } else {
            intent.getParcelableExtra(Intent.EXTRA_CHOSEN_COMPONENT)
        }
        ZipSending.chosen(context, intent.getIntExtra(EXTRA_PART, 0), component)
    }

    companion object {
        const val EXTRA_PART = "com.eschgi.share.zip.PART"
    }
}
