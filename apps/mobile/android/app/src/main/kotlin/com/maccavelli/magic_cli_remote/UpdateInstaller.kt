package com.maccavelli.magic_cli_remote

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import androidx.core.content.FileProvider
import java.io.File

/**
 * Install helpers for in-app APK updates (MADR 0065 P4/P5).
 *
 * v1: ACTION_VIEW / ACTION_INSTALL_PACKAGE via FileProvider.
 * v2: PackageInstaller session with USER_ACTION_NOT_REQUIRED when possible;
 *     falls back to pending-user-action intent.
 */
object UpdateInstaller {
    private const val AUTHORITY_SUFFIX = ".fileprovider"
    const val ACTION_INSTALL_STATUS = "com.maccavelli.magic_cli_remote.INSTALL_STATUS"

    fun contentUri(context: Context, path: String): Uri {
        val file = File(path)
        val authority = context.packageName + AUTHORITY_SUFFIX
        return FileProvider.getUriForFile(context, authority, file)
    }

    /** v1 install intent. */
    fun installApkIntent(context: Context, path: String): Intent {
        val uri = contentUri(context, path)
        return Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, "application/vnd.android.package-archive")
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
    }

    /**
     * v2 session install. Returns null when the session was committed and
     * no immediate user action is required; otherwise returns an Intent the
     * host should start for confirmation.
     */
    fun installApkSession(context: Context, path: String): Intent? {
        val file = File(path)
        if (!file.isFile) {
            throw IllegalArgumentException("APK not found: $path")
        }
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(
            PackageInstaller.SessionParams.MODE_FULL_INSTALL,
        )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            try {
                // Prefer dialog-free when the app is the installer of record.
                params.setRequireUserAction(
                    PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED,
                )
            } catch (_: Throwable) {
                // Older platform images may lack the constant.
            }
        }
        val sessionId = installer.createSession(params)
        installer.openSession(sessionId).use { session ->
            file.inputStream().use { input ->
                session.openWrite("package", 0, file.length()).use { out ->
                    input.copyTo(out)
                    session.fsync(out)
                }
            }
            val statusIntent = Intent(ACTION_INSTALL_STATUS).apply {
                setPackage(context.packageName)
            }
            val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    PendingIntent.FLAG_MUTABLE
                } else {
                    0
                }
            val pending = PendingIntent.getBroadcast(
                context,
                sessionId,
                statusIntent,
                flags,
            )
            session.commit(pending.intentSender)
        }
        return null
    }
}

/** Handles PackageInstaller status + MY_PACKAGE_REPLACED notification. */
class UpdateInstallReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            UpdateInstaller.ACTION_INSTALL_STATUS -> {
                val status = intent.getIntExtra(
                    PackageInstaller.EXTRA_STATUS,
                    PackageInstaller.STATUS_FAILURE,
                )
                if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
                    @Suppress("DEPRECATION")
                    val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
                    if (confirm != null) {
                        confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        context.startActivity(confirm)
                    }
                }
            }
            Intent.ACTION_MY_PACKAGE_REPLACED -> {
                // Notification is best-effort via Flutter local notifications
                // when the engine is up; system toast is avoided. A full
                // NotificationManager post keeps the "tap to open" UX without
                // starting an activity from the background (0065 F4).
                val nm = context.getSystemService(Context.NOTIFICATION_SERVICE)
                    as android.app.NotificationManager
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    val ch = android.app.NotificationChannel(
                        "app_updates",
                        "App updates",
                        android.app.NotificationManager.IMPORTANCE_DEFAULT,
                    )
                    nm.createNotificationChannel(ch)
                }
                val launch = context.packageManager
                    .getLaunchIntentForPackage(context.packageName)
                val pi = PendingIntent.getActivity(
                    context,
                    0,
                    launch,
                    PendingIntent.FLAG_UPDATE_CURRENT or
                        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                            PendingIntent.FLAG_IMMUTABLE
                        } else {
                            0
                        },
                )
                val n = androidx.core.app.NotificationCompat.Builder(context, "app_updates")
                    .setSmallIcon(android.R.drawable.stat_sys_download_done)
                    .setContentTitle("Updated")
                    .setContentText("Tap to open Magic CLI Remote")
                    .setContentIntent(pi)
                    .setAutoCancel(true)
                    .build()
                nm.notify(0x06_65, n)
            }
        }
    }
}
