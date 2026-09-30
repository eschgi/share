package com.eschgi.share.transfer

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import androidx.annotation.RequiresApi
import kotlin.concurrent.thread

/**
 * The transfers' host from Android 14 on: user-initiated data transfer jobs, one for
 * downloads and one for uploads. They start from a tap, may run as long as they need, and the
 * system starts them again after the network comes back.
 */
@RequiresApi(34)
class TransferJobService : JobService() {
    private val stopped = HashMap<Int, Boolean>()

    private fun isStopped(id: Int) = synchronized(stopped) { stopped[id] == true }

    override fun onStartJob(params: JobParameters): Boolean {
        val direction = Direction.ofJob(params.jobId) ?: return false
        synchronized(stopped) { stopped[params.jobId] = false }
        // Required within a few seconds of the start, before anything else.
        setNotification(params, direction.notificationId, direction.ongoing(this, null), JOB_END_NOTIFICATION_POLICY_REMOVE)
        thread(name = direction.work) {
            val result = direction.run(this, object : TransferHost {
                override val isStopped get() = isStopped(params.jobId)

                override fun onProgress(progress: TransferProgress) {
                    if (isStopped) return
                    setNotification(params, direction.notificationId, direction.ongoing(this@TransferJobService, progress), JOB_END_NOTIFICATION_POLICY_REMOVE)
                }
            })
            if (!isStopped(params.jobId)) jobFinished(params, result == RunResult.RESCHEDULE)
        }
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean {
        val direction = Direction.ofJob(params.jobId) ?: return false
        synchronized(stopped) { stopped[params.jobId] = true }
        direction.abortAll()
        if (params.stopReason == JobParameters.STOP_REASON_USER) {
            // Stopped in the Task Manager: the transfers wait until the person goes on.
            direction.pauseAll(this)
            return false
        }
        return true // lost the network, or the system needed the resources: again later
    }

    companion object {
        /** Starts the job; only allowed while the app is visible. */
        fun schedule(context: Context, direction: Direction, bytes: Long) {
            val builder = JobInfo.Builder(direction.jobId, ComponentName(context, TransferJobService::class.java))
                .setUserInitiated(true)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
            if (direction == Direction.DOWNLOADS) {
                builder.setEstimatedNetworkBytes(bytes.coerceAtLeast(1), 0)
            } else {
                builder.setEstimatedNetworkBytes(0, bytes.coerceAtLeast(1))
            }
            val scheduler = context.getSystemService(JobScheduler::class.java)
            check(scheduler.schedule(builder.build()) == JobScheduler.RESULT_SUCCESS) { "job not scheduled" }
        }
    }
}
