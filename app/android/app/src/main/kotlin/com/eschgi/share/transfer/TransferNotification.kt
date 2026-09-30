package com.eschgi.share.transfer

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.text.format.Formatter
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.eschgi.share.MainActivity
import com.eschgi.share.R
import com.eschgi.share.data.SecretStore
import java.util.Locale

/**
 * The downloads' notifications: one ongoing while they run (the host's foreground
 * notification), and one summary when they end, with "Try again" if something failed.
 */
object TransferNotification {
    const val ONGOING_ID = 2001
    private const val SUMMARY_ID = 2002
    private const val CHANNEL_ID = "downloads"

    private const val REQ_OPEN = 1
    private const val REQ_CANCEL = 2
    private const val REQ_RETRY = 3

    fun ongoing(context: Context, progress: DownloadEngine.Progress?): Notification {
        val c = localized(context)
        ensureChannel(c)
        val builder = NotificationCompat.Builder(c, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_share)
            .setColor(ContextCompat.getColor(c, R.color.accent))
            .setContentTitle(c.getString(R.string.downloads_saving))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setCategory(Notification.CATEGORY_PROGRESS)
            .setContentIntent(open(c))
            .addAction(0, c.getString(R.string.downloads_cancel), cancel(c))
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        if (progress == null || progress.bytesTotal <= 0) {
            builder.setProgress(0, 0, true)
        } else {
            builder.setContentText(
                c.getString(
                    R.string.downloads_progress,
                    progress.done,
                    progress.files,
                    Formatter.formatShortFileSize(c, progress.bytesDone),
                    Formatter.formatShortFileSize(c, progress.bytesTotal),
                ),
            )
            val permille = (progress.bytesDone * 1000 / progress.bytesTotal).toInt().coerceIn(0, 1000)
            builder.setProgress(1000, permille, false)
        }
        return builder.build()
    }

    /** After a run: what was saved, and what wasn't. */
    fun summary(context: Context, batches: List<String>) {
        if (batches.isEmpty()) return
        val c = localized(context)
        val db = TransferDb.get(c)
        val snapshots = batches.mapNotNull { b -> db.batch(b)?.let { BatchSnapshot.of(b, it.state == "active", it.noSpace, db.items(b)) } }
        if (snapshots.isEmpty() || snapshots.any { it.running }) return
        val done = snapshots.sumOf { it.done }
        val failed = snapshots.sumOf { it.failed }
        val noSpace = snapshots.any { it.noSpace }
        val media = snapshots.sumOf { it.media }
        val documents = snapshots.sumOf { it.documents }
        if (done == 0 && failed == 0 && !noSpace) return // cancelled, or all skipped

        val builder = base(c).setAutoCancel(true)
        when {
            noSpace -> builder
                .setContentTitle(c.getString(R.string.downloads_no_space))
                .setContentText(c.getString(R.string.downloads_no_space_text))
                .setContentIntent(retry(c))
            failed > 0 -> builder
                .setContentTitle(c.resources.getQuantityString(R.plurals.downloads_saved, done, done))
                .setContentText(c.resources.getQuantityString(R.plurals.downloads_failed, failed, failed))
                .setContentIntent(open(c))
                .addAction(0, c.getString(R.string.downloads_retry), retry(c))
            else -> builder
                .setContentTitle(c.resources.getQuantityString(R.plurals.downloads_saved, done, done))
                .setContentText(
                    c.getString(
                        when {
                            documents == 0 -> R.string.downloads_where_media
                            media == 0 -> R.string.downloads_where_documents
                            else -> R.string.downloads_where_both
                        },
                    ),
                )
                .setContentIntent(open(c))
        }
        post(c, builder.build())
    }

    /** The phone lost its network; the host goes on when it's back. */
    fun waiting(context: Context) {
        val c = localized(context)
        post(c, base(c).setContentTitle(c.getString(R.string.downloads_waiting)).setContentIntent(open(c)).build())
    }

    fun cancelSummary(context: Context) = NotificationManagerCompat.from(context).cancel(SUMMARY_ID)

    private fun base(c: Context): NotificationCompat.Builder {
        ensureChannel(c)
        return NotificationCompat.Builder(c, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_share)
            .setColor(ContextCompat.getColor(c, R.color.accent))
            .setCategory(Notification.CATEGORY_STATUS)
    }

    @SuppressLint("MissingPermission") // checked with areNotificationsEnabled
    private fun post(c: Context, notification: Notification) {
        val manager = NotificationManagerCompat.from(c)
        if (manager.areNotificationsEnabled()) manager.notify(SUMMARY_ID, notification)
    }

    private fun ensureChannel(c: Context) {
        val manager = c.getSystemService(NotificationManager::class.java) ?: return
        val channel = NotificationChannel(CHANNEL_ID, c.getString(R.string.downloads_channel), NotificationManager.IMPORTANCE_LOW)
        manager.createNotificationChannel(channel) // renames it when the language changed
    }

    private fun open(c: Context) = PendingIntent.getActivity(
        c,
        REQ_OPEN,
        Intent(c, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    /** Opens the app, which is allowed to start the job again. */
    private fun retry(c: Context) = PendingIntent.getActivity(
        c,
        REQ_RETRY,
        Intent(c, MainActivity::class.java)
            .setAction(MainActivity.ACTION_RETRY_DOWNLOADS)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun cancel(c: Context) = PendingIntent.getBroadcast(
        c,
        REQ_CANCEL,
        Intent(c, TransferReceiver::class.java).setAction(TransferReceiver.ACTION_CANCEL),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    /** The app's resources in the language picked in the app, if one was. */
    private fun localized(context: Context): Context {
        val code = runCatching { SecretStore(context).read(SecretStore.LANGUAGE) }.getOrNull() ?: return context
        val config = Configuration(context.resources.configuration).apply { setLocale(Locale.forLanguageTag(code)) }
        return context.createConfigurationContext(config)
    }
}
