package com.eschgi.share.transfer

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import androidx.annotation.RequiresApi
import kotlin.concurrent.thread

/** The download job's JobScheduler id, outside the range ShareApplication gives WorkManager. */
const val DOWNLOAD_JOB_ID = 1

/**
 * The downloads' host from Android 14 on: a user-initiated data transfer job. It is started by
 * a tap, may run as long as it needs, and the system restarts it after losing the network.
 */
@RequiresApi(34)
class DownloadJobService : JobService() {
    @Volatile private var stopped = false

    override fun onStartJob(params: JobParameters): Boolean {
        stopped = false
        // Required within a few seconds of the start, before anything else.
        setNotification(params, TransferNotification.ONGOING_ID, TransferNotification.ongoing(this, null), JOB_END_NOTIFICATION_POLICY_REMOVE)
        thread(name = "downloads") {
            val result = DownloadEngine.run(this, object : DownloadEngine.Host {
                override val isStopped get() = stopped

                override fun onProgress(progress: DownloadEngine.Progress) {
                    if (stopped) return
                    setNotification(
                        params,
                        TransferNotification.ONGOING_ID,
                        TransferNotification.ongoing(this@DownloadJobService, progress),
                        JOB_END_NOTIFICATION_POLICY_REMOVE,
                    )
                }
            })
            if (!stopped) jobFinished(params, result == DownloadEngine.Result.RESCHEDULE)
        }
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean {
        stopped = true
        DownloadEngine.abortAll()
        if (params.stopReason == JobParameters.STOP_REASON_USER) {
            // Stopped in the Task Manager: the downloads wait until the person goes on.
            Downloads.pauseAll(this)
            return false
        }
        return true // lost the network, or the system needed the resources: again later
    }

    companion object {
        /** Starts the job; only allowed while the app is visible. */
        fun schedule(context: Context, bytes: Long) {
            val job = JobInfo.Builder(DOWNLOAD_JOB_ID, ComponentName(context, DownloadJobService::class.java))
                .setUserInitiated(true)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setEstimatedNetworkBytes(bytes.coerceAtLeast(1), 0)
                .build()
            val scheduler = context.getSystemService(JobScheduler::class.java)
            check(scheduler.schedule(job) == JobScheduler.RESULT_SUCCESS) { "job not scheduled" }
        }
    }
}
