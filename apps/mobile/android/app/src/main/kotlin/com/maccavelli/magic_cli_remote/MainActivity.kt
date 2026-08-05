package com.maccavelli.magic_cli_remote

import android.os.Bundle
import android.view.WindowManager
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
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
                    try {
                        if (preferSession) {
                            try {
                                UpdateInstaller.installApkSession(this, path)
                                result.success(null)
                                return@setMethodCallHandler
                            } catch (e: Exception) {
                                // Fall through to v1 intent.
                            }
                        }
                        val intent = UpdateInstaller.installApkIntent(this, path)
                        startActivity(intent)
                        result.success(null)
                    } catch (e: Exception) {
                        result.error("install_failed", e.message, null)
                    }
                }
                else -> result.notImplemented()
            }
        }
    }
}
