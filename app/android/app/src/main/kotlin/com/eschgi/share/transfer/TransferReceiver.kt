package com.eschgi.share.transfer

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** The ongoing notifications' Cancel. Not exported: only our own PendingIntents fire it. */
class TransferReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val action = intent.action
        if (action != ACTION_CANCEL && action != ACTION_CANCEL_UPLOADS) return
        val pending = goAsync()
        Thread {
            try {
                if (action == ACTION_CANCEL) {
                    TransferDb.get(context).recentBatches(0).filter { it.state == "active" || it.state == "paused" }
                        .forEach { Downloads.cancel(context, it.id) }
                } else {
                    UploadQueue(context).recentBatches(0).filter { it.state == "active" || it.state == "paused" }
                        .forEach { Uploads.cancel(context, it.id) }
                }
            } finally {
                pending.finish()
            }
        }.start()
    }

    companion object {
        const val ACTION_CANCEL = "com.eschgi.share.CANCEL_DOWNLOADS"
        const val ACTION_CANCEL_UPLOADS = "com.eschgi.share.CANCEL_UPLOADS"
    }
}
