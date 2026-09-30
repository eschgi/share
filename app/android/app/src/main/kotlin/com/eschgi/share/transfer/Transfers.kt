package com.eschgi.share.transfer

import android.app.Notification
import android.content.Context
import android.os.Build
import android.util.Log

/** What runs the transfers: a user-initiated job from Android 14 on, a foreground worker before. */
interface TransferHost {
    val isStopped: Boolean

    /** About once a second, for the host's notification. */
    fun onProgress(progress: TransferProgress)
}

data class TransferProgress(val files: Int, val done: Int, val bytesTotal: Long, val bytesDone: Long)

enum class RunResult { FINISHED, RESCHEDULE }

/** The JobScheduler ids, outside the range ShareApplication gives WorkManager. */
const val DOWNLOAD_JOB_ID = 1
const val UPLOAD_JOB_ID = 2

/** Downloads and uploads: each with its own job, work and notification, and the same hosts. */
enum class Direction(val jobId: Int, val work: String) {
    DOWNLOADS(DOWNLOAD_JOB_ID, "downloads"),
    UPLOADS(UPLOAD_JOB_ID, "uploads");

    fun run(context: Context, host: TransferHost): RunResult = when (this) {
        DOWNLOADS -> DownloadEngine.run(context, host)
        UPLOADS -> UploadEngine.run(context, host)
    }

    fun abortAll() = when (this) {
        DOWNLOADS -> DownloadEngine.abortAll()
        UPLOADS -> UploadEngine.abortAll()
    }

    /** The person stopped it in the Task Manager: it waits for them. */
    fun pauseAll(context: Context) = when (this) {
        DOWNLOADS -> Downloads.pauseAll(context)
        UPLOADS -> Uploads.pauseAll(context)
    }

    val notificationId: Int
        get() = when (this) {
            DOWNLOADS -> TransferNotification.ONGOING_ID
            UPLOADS -> TransferNotification.SENDING_ID
        }

    fun ongoing(context: Context, progress: TransferProgress?): Notification = when (this) {
        DOWNLOADS -> TransferNotification.ongoing(context, progress)
        UPLOADS -> TransferNotification.sending(context, progress)
    }

    /** Starts this direction's host. From Android 14 on that's only allowed while the app is visible. */
    fun start(context: Context, bytes: Long) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            try {
                TransferJobService.schedule(context, this, bytes)
                return
            } catch (e: RuntimeException) {
                Log.w("Transfers", "can't schedule a user-initiated job for $work", e)
            }
        }
        TransferWorker.enqueue(context, this)
    }

    companion object {
        fun ofJob(id: Int): Direction? = entries.firstOrNull { it.jobId == id }

        fun ofWork(name: String?): Direction = entries.firstOrNull { it.work == name } ?: DOWNLOADS
    }
}
