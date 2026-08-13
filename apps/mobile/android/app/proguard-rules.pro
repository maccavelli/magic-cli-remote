# R8 keep rules for the release build (MADR 0084 D7).
#
# ***R8 IS CURRENTLY DISABLED.*** This file is retained, unused, because the
# rules below were researched and are the correct starting point if R8 is
# re-enabled. To turn it on, set isMinifyEnabled/isShrinkResources and add
# proguardFiles(...) to buildTypes.release in build.gradle.kts.
#
# Why it is off, measured 2026-08-13 with two *clean* builds (arm64 release;
# an earlier incremental comparison was discarded as invalid):
#
#   without R8: 40,286,708 bytes, dex total 4,108,948
#   with R8:    40,286,708 bytes, dex total 4,108,948   <- byte-identical
#
# R8 genuinely ran (it emitted a 33 MB mapping.txt); it simply had nothing to
# remove, because the keep rules below — the safe ones, covering everything
# reached reflectively or by manifest name — preserve essentially the whole
# Java/Kotlin surface.
#
# The ceiling is small by construction: a Flutter APK is dominated by native
# code (libflutter.so 11.6 MB, libapp.so 9.8 MB) against 4.1 MB of total dex,
# so even aggressive shrinking touches ~10% of the artifact. Tightening these
# rules to claim part of that trades directly against release-only runtime
# failures — a missing keep rule does not fail the build, it fails on a user's
# phone. With no measurable benefit on the safe setting, R8 stays off.
#
# To revisit: tighten the rules *with a device attached*, and validate the
# reflective paths (notifications, foreground service, QR scanner, speech,
# image picker, APK installer) against a release build before trusting it.
#
# Every rule here exists because something is reached by *name* rather than by
# a call R8 can see: reflection, a manifest entry, or a platform callback. A
# missing rule does not fail the build — it fails at runtime, in release only,
# which is why P6's device matrix exists and why this file names the reason
# for each rule rather than accumulating copied wildcards.

# --- Flutter embedding -------------------------------------------------------
# The engine instantiates these by name from the manifest / native side.
-keep class io.flutter.app.** { *; }
-keep class io.flutter.plugin.**  { *; }
-keep class io.flutter.util.**  { *; }
-keep class io.flutter.view.**  { *; }
-keep class io.flutter.**  { *; }
-keep class io.flutter.plugins.**  { *; }

# --- This app's manifest-referenced classes ----------------------------------
# MainActivity, the install-status receiver and the installer are named as
# strings in AndroidManifest.xml (MADR 0065 P4/P5), so nothing in bytecode
# references them.
-keep class com.maccavelli.magic_cli_remote.MainActivity { *; }
-keep class com.maccavelli.magic_cli_remote.UpdateInstallReceiver { *; }
-keep class com.maccavelli.magic_cli_remote.UpdateInstaller { *; }

# --- flutter_foreground_task -------------------------------------------------
# The service and its restart receiver are manifest entries; the task handler
# is resolved through a Dart entry point the Java side starts by name.
-keep class com.pravera.flutter_foreground_task.** { *; }

# --- flutter_local_notifications ---------------------------------------------
# Scheduled notifications are rehydrated from serialized payloads via Gson,
# which reads field names reflectively. Losing them silently drops the
# approval alerts this app exists to deliver.
-keep class com.dexterous.** { *; }
-keepattributes *Annotation*
-keepattributes Signature
-dontwarn com.google.gson.**

# --- speech_to_text ----------------------------------------------------------
# Android's SpeechRecognizer calls the listener back through the platform.
-keep class com.csdcorp.speech_to_text.** { *; }

# --- mobile_scanner / ML Kit -------------------------------------------------
# ML Kit loads model and detector classes reflectively; its own consumer rules
# cover most of it, but the optional-module warnings are not errors here.
-keep class com.google.mlkit.** { *; }
-dontwarn com.google.mlkit.**

# --- flutter_secure_storage --------------------------------------------------
# Keystore-backed cipher selection reflects over provider class names.
-keep class com.it_nomads.fluttersecurestorage.** { *; }

# --- Kotlin / coroutines runtime --------------------------------------------
-keepclassmembers class kotlinx.coroutines.** { volatile <fields>; }
-dontwarn kotlinx.coroutines.**
-dontwarn kotlin.**

# --- Play Core (referenced by the Flutter embedding, not bundled) ------------
# Flutter's deferred-components support references these; this app does not
# use deferred components, so the references are unresolved by design.
-dontwarn com.google.android.play.core.**
