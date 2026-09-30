package com.eschgi.share.transfer

import android.content.Context
import android.content.pm.ServiceInfo
import android.util.Log
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

/**
 * The downloads' host before Android 14: unique WorkManager work that promotes itself to a
 * dataSync foreground service, so it isn't bound by the ten-minute limit of plain work.
 */
class DownloadWorker(context: Context, params: WorkerParameters) : Worker(context, params) {

    override fun doWork(): Result {
        runCatching { setForegroundAsync(foreground(null)).get() }
            .onFailure { Log.w(TAG, "can't run in the foreground", it) }
        val result = DownloadEngine.run(applicationContext, object : DownloadEngine.Host {
            override val isStopped get() = this@DownloadWorker.isStopped

            override fun onProgress(progress: DownloadEngine.Progress) {
                runCatching { setForegroundAsync(foreground(progress)) }
            }
        })
        return if (result == DownloadEngine.Result.RESCHEDULE) Result.retry() else Result.success()
    }

    override fun onStopped() {
        DownloadEngine.abortAll()
    }

    private fun foreground(progress: DownloadEngine.Progress?) = ForegroundInfo(
        TransferNotification.ONGOING_ID,
        TransferNotification.ongoing(applicationContext, progress),
        ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
    )

    companion object {
        private const val TAG = "DownloadWorker"
        private const val NAME = "downloads"

        fun enqueue(context: Context) {
            val request = OneTimeWorkRequest.Builder(DownloadWorker::class.java)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                .build()
            // Appended, so a run that is just finishing can't swallow the new files.
            WorkManager.getInstance(context).enqueueUniqueWork(NAME, ExistingWorkPolicy.APPEND_OR_REPLACE, request)
        }
    }
}
