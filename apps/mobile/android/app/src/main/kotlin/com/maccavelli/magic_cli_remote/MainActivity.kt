package com.maccavelli.magic_cli_remote

import android.os.Bundle
import android.view.WindowManager
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    /**
     * Off-thread worker for the APK install (MADR 0126 D6/F5).
     *
     * A MethodChannel handler runs on the platform MAIN thread, and
     * `UpdateInstaller.installApkSession` streams the whole APK into a
     * PackageInstaller session and fsyncs it. The release APK measures ~41 MB,
     * so that is a multi-second read-plus-write on the UI thread — past
     * Android's 5 s ANR threshold on slow storage, and blocking every frame
     * until it returns.
     */
    private val installExecutor: ExecutorService = Executors.newSingleThreadExecutor()

    override fun onDestroy() {
        installExecutor.shutdown()
        super.onDestroy()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        // FLAG_SECURE: the connect screen renders the device token in a
        // revealable field. Without this, the token can leak via screenshots,
        // screen recording, and the recents-app thumbnail (which is written to
        // disk by the system). Set before super.onCreate so the very first
        // frame is already protected.
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE,
        )
        super.onCreate(savedInstanceState)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "mcremote/app_update",
        ).setMethodCallHandler { call, result ->
            when (call.method) {
                "installApk" -> {
                    val path = call.argument<String>("path")
                    val preferSession = call.argument<Boolean>("preferSession") ?: true
                    if (path.isNullOrEmpty()) {
                        result.error("bad_args", "path required", null)
                        return@setMethodCallHandler
                    }
                    // Behaviour is preserved exactly — session first, silent
                    // fallback to the v1 intent, same error codes. The only
                    // change is which thread does the copying. `result` may
                    // only be completed on the platform thread, and
                    // startActivity must run there too, so both are marshalled
                    // back with runOnUiThread.
                    installExecutor.execute {
                        var sessionError: Exception? = null
                        if (preferSession) {
                            try {
                                UpdateInstaller.installApkSession(this, path)
                                runOnUiThread { result.success(null) }
                                return@execute
                            } catch (e: Exception) {
                                sessionError = e
                            }
                        }
                        try {
                            val intent = UpdateInstaller.installApkIntent(this, path)
                            runOnUiThread {
                                try {
                                    startActivity(intent)
                                    result.success(null)
                                } catch (e: Exception) {
                                    result.error("install_failed", e.message, null)
                                }
                            }
                        } catch (e: Exception) {
                            // Report the session failure when the fallback
                            // could not even be built: previously only the
                            // second error survived, and the first is the
                            // interesting one.
                            val msg = e.message ?: sessionError?.message
                            runOnUiThread { result.error("install_failed", msg, null) }
                        }
                    }
                }
                else -> result.notImplemented()
            }
        }
    }
}
