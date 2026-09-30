package com.eschgi.share.transfer

import android.content.Context
import android.content.pm.ServiceInfo
import android.util.Log
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.Data
import androidx.work.ExistingWorkPolicy
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

/**
 * The transfers' host before Android 14: unique WorkManager work per direction that promotes
 * itself to a dataSync foreground service, so it isn't bound by the ten-minute limit of plain
 * work.
 */
class TransferWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    private val direction = Direction.ofWork(params.inputData.getString(DIRECTION))

    override fun doWork(): Result {
        runCatching { setForegroundAsync(foreground(null)).get() }
            .onFailure { Log.w(TAG, "can't run in the foreground", it) }
        val result = direction.run(applicationContext, object : TransferHost {
            override val isStopped get() = this@TransferWorker.isStopped

            override fun onProgress(progress: TransferProgress) {
                runCatching { setForegroundAsync(foreground(progress)) }
            }
        })
        return if (result == RunResult.RESCHEDULE) Result.retry() else Result.success()
    }

    override fun onStopped() {
        direction.abortAll()
    }

    private fun foreground(progress: TransferProgress?) = ForegroundInfo(
        direction.notificationId,
        direction.ongoing(applicationContext, progress),
        ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
    )

    companion object {
        private const val TAG = "TransferWorker"
        private const val DIRECTION = "direction"

        fun enqueue(context: Context, direction: Direction) {
            val request = OneTimeWorkRequest.Builder(TransferWorker::class.java)
                .setInputData(Data.Builder().putString(DIRECTION, direction.work).build())
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                .build()
            // Appended, so a run that is just finishing can't swallow the new files.
            WorkManager.getInstance(context).enqueueUniqueWork(direction.work, ExistingWorkPolicy.APPEND_OR_REPLACE, request)
        }
    }
}
