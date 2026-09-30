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
 * The transfers' notifications, for downloads and for sending: one ongoing while they run
 * (the host's foreground notification), and one summary when they end, with "Try again" if
 * something failed.
 */
object TransferNotification {
    const val ONGOING_ID = 2001
    private const val SUMMARY_ID = 2002
    const val SENDING_ID = 2003
    private const val SENT_ID = 2004
    private const val CHANNEL_ID = "downloads"
    private const val SEND_CHANNEL_ID = "uploads"

    private const val REQ_OPEN = 1
    private const val REQ_CANCEL = 2
    private const val REQ_RETRY = 3
    private const val REQ_CANCEL_UPLOADS = 4
    private const val REQ_RETRY_UPLOADS = 5

    fun ongoing(context: Context, progress: TransferProgress?): Notification {
        val c = localized(context)
        return progress(c, CHANNEL_ID, R.string.downloads_saving, progress, cancel(c, TransferReceiver.ACTION_CANCEL, REQ_CANCEL))
    }

    fun sending(context: Context, progress: TransferProgress?): Notification {
        val c = localized(context)
        return progress(c, SEND_CHANNEL_ID, R.string.uploads_sending, progress, cancel(c, TransferReceiver.ACTION_CANCEL_UPLOADS, REQ_CANCEL_UPLOADS))
    }

    private fun progress(c: Context, channel: String, title: Int, progress: TransferProgress?, cancel: PendingIntent): Notification {
        ensureChannels(c)
        val builder = NotificationCompat.Builder(c, channel)
            .setSmallIcon(R.drawable.ic_stat_share)
            .setColor(accent(c))
            .setContentTitle(c.getString(title))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setCategory(Notification.CATEGORY_PROGRESS)
            .setContentIntent(open(c))
            .addAction(0, c.getString(R.string.downloads_cancel), cancel)
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

        val builder = base(c, CHANNEL_ID).setAutoCancel(true)
        when {
            noSpace -> builder
                .setContentTitle(c.getString(R.string.downloads_no_space))
                .setContentText(c.getString(R.string.downloads_no_space_text))
                .setContentIntent(retry(c, MainActivity.ACTION_RETRY_DOWNLOADS, REQ_RETRY))
            failed > 0 -> builder
                .setContentTitle(c.resources.getQuantityString(R.plurals.downloads_saved, done, done))
                .setContentText(c.resources.getQuantityString(R.plurals.downloads_failed, failed, failed))
                .setContentIntent(open(c))
                .addAction(0, c.getString(R.string.downloads_retry), retry(c, MainActivity.ACTION_RETRY_DOWNLOADS, REQ_RETRY))
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
        post(c, SUMMARY_ID, builder.build())
    }

    /** The phone lost its network; the host goes on when it's back. */
    fun waiting(context: Context) {
        val c = localized(context)
        post(c, SUMMARY_ID, base(c, CHANNEL_ID).setContentTitle(c.getString(R.string.downloads_waiting)).setContentIntent(open(c)).build())
    }

    fun cancelSummary(context: Context) = NotificationManagerCompat.from(context).cancel(SUMMARY_ID)

    /** After sending: what arrived, what didn't, and what waits for a new PIN or a sign-in. */
    fun sent(context: Context, batches: List<String>) {
        if (batches.isEmpty()) return
        val c = localized(context)
        val queue = UploadQueue(c)
        val snapshots = batches.mapNotNull { b -> queue.batch(b)?.let { UploadSnapshot.of(it, queue.rows(b)) } }
        if (snapshots.isEmpty() || snapshots.any { it.running }) return
        val done = snapshots.sumOf { it.done }
        val failed = snapshots.sumOf { it.failed }
        val lost = snapshots.sumOf { it.lost }
        val builder = base(c, SEND_CHANNEL_ID).setAutoCancel(true).setContentIntent(open(c))
        when {
            snapshots.any { it.paused == "pin_ended" } -> builder
                .setContentTitle(c.getString(R.string.uploads_pin_ended))
                .setContentText(c.getString(R.string.uploads_pin_ended_text))
            snapshots.any { it.paused == "signed_out" } -> builder
                .setContentTitle(c.getString(R.string.uploads_signed_out))
                .setContentText(c.getString(R.string.uploads_signed_out_text))
            done == 0 && failed == 0 && lost == 0 -> return // cancelled
            else -> {
                builder.setContentTitle(c.resources.getQuantityString(R.plurals.uploads_sent, done, done))
                val text = listOfNotNull(
                    if (failed > 0) c.resources.getQuantityString(R.plurals.uploads_failed, failed, failed) else null,
                    if (lost > 0) c.resources.getQuantityString(R.plurals.uploads_lost, lost, lost) else null,
                ).joinToString(" ")
                if (text.isNotEmpty()) builder.setContentText(text)
                if (failed > 0) builder.addAction(0, c.getString(R.string.downloads_retry), retry(c, MainActivity.ACTION_RETRY_UPLOADS, REQ_RETRY_UPLOADS))
            }
        }
        post(c, SENT_ID, builder.build())
    }

    fun sendingWaiting(context: Context) {
        val c = localized(context)
        post(c, SENT_ID, base(c, SEND_CHANNEL_ID).setContentTitle(c.getString(R.string.uploads_waiting)).setContentIntent(open(c)).build())
    }

    fun cancelSentSummary(context: Context) = NotificationManagerCompat.from(context).cancel(SENT_ID)

    private fun base(c: Context, channel: String): NotificationCompat.Builder {
        ensureChannels(c)
        return NotificationCompat.Builder(c, channel)
            .setSmallIcon(R.drawable.ic_stat_share)
            .setColor(accent(c))
            .setCategory(Notification.CATEGORY_STATUS)
    }

    @SuppressLint("MissingPermission") // checked with areNotificationsEnabled
    private fun post(c: Context, id: Int, notification: Notification) {
        val manager = NotificationManagerCompat.from(c)
        if (manager.areNotificationsEnabled()) manager.notify(id, notification)
    }

    private fun ensureChannels(c: Context) {
        val manager = c.getSystemService(NotificationManager::class.java) ?: return
        // Creating them again renames them when the language changed.
        manager.createNotificationChannel(NotificationChannel(CHANNEL_ID, c.getString(R.string.downloads_channel), NotificationManager.IMPORTANCE_LOW))
        manager.createNotificationChannel(NotificationChannel(SEND_CHANNEL_ID, c.getString(R.string.uploads_channel), NotificationManager.IMPORTANCE_LOW))
    }

    private fun open(c: Context) = PendingIntent.getActivity(
        c,
        REQ_OPEN,
        Intent(c, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    /** Opens the app, which is allowed to start the job again. */
    private fun retry(c: Context, action: String, request: Int) = PendingIntent.getActivity(
        c,
        request,
        Intent(c, MainActivity::class.java)
            .setAction(action)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private fun cancel(c: Context, action: String, request: Int) = PendingIntent.getBroadcast(
        c,
        request,
        Intent(c, TransferReceiver::class.java).setAction(action),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    /** The accent of the theme picked in the app, as in lib/ui/theme.dart. */
    private fun accent(c: Context): Int = when (runCatching { SecretStore(c).read(SecretStore.THEME) }.getOrNull()) {
        "midnight" -> 0xFF3B7DD8.toInt()
        "moss" -> 0xFF6B8A38.toInt()
        "plum" -> 0xFF9B5CC9.toInt()
        "frost" -> 0xFF2F6BCB.toInt()
        else -> ContextCompat.getColor(c, R.color.accent) // Ember, Black, Linen, Automatic: terracotta
    }

    /** The app's resources in the language picked in the app, if one was. */
    private fun localized(context: Context): Context {
        val code = runCatching { SecretStore(context).read(SecretStore.LANGUAGE) }.getOrNull() ?: return context
        val config = Configuration(context.resources.configuration).apply { setLocale(Locale.forLanguageTag(code)) }
        return context.createConfigurationContext(config)
    }
}
