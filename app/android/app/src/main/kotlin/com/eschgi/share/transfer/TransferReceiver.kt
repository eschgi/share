package com.eschgi.share.transfer

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** The ongoing notification's Cancel. Not exported: only our own PendingIntent fires it. */
class TransferReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_CANCEL) return
        val pending = goAsync()
        Thread {
            try {
                val db = TransferDb.get(context)
                db.recentBatches(0).filter { it.state == "active" || it.state == "paused" }.forEach { Downloads.cancel(context, it.id) }
            } finally {
                pending.finish()
            }
        }.start()
    }

    companion object {
        const val ACTION_CANCEL = "com.eschgi.share.CANCEL_DOWNLOADS"
    }
}
