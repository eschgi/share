package com.eschgi.share

import android.app.Application
import androidx.work.Configuration

class ShareApplication : Application(), Configuration.Provider {
    // WorkManager's own JobScheduler jobs would otherwise take any id, including those of
    // TransferJobService, and scheduling one would replace the other.
    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder()
            .setJobSchedulerJobIdRange(1_000, 100_000)
            .build()
}
