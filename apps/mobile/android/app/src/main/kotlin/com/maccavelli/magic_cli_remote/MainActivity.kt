package com.maccavelli.magic_cli_remote

import android.os.Bundle
import android.view.WindowManager
import io.flutter.embedding.android.FlutterActivity

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
}
