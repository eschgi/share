package com.eschgi.share

import android.app.Application
import androidx.work.Configuration
import com.eschgi.share.transfer.DOWNLOAD_JOB_ID

class ShareApplication : Application(), Configuration.Provider {
    // WorkManager's own JobScheduler jobs would otherwise take any id, including the one of
    // DownloadJobService, and scheduling one would replace the other.
    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder()
            .setJobSchedulerJobIdRange(DOWNLOAD_JOB_ID + 1_000, DOWNLOAD_JOB_ID + 100_000)
            .build()
}
