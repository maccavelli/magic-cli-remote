import 'dart:async';
import 'dart:ui' show PlatformDispatcher;

import 'package:flutter/material.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app.dart';
import 'data/diagnostics/error_recorder.dart';
import 'data/local/settings_store.dart';
import 'state/app_providers.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();

  // Open the isolate port the foreground service talks back on (MADR 0129 D2).
  //
  // `sendDataToMain` resolves its target with
  // `IsolateNameServer.lookupPortByName`, and the plugin never registers that
  // port for you — the app must, once, before anything can receive from the
  // service isolate. Without this call every message the service sends the UI
  // isolate is looked up, found missing, and dropped in silence.
  //
  // That is not hypothetical: it is why the release acknowledgement D2 is
  // built on had never once been delivered (found 2026-09-02, MADR 0129 P6).
  // `claimOwnership` waited its full three seconds every time and then
  // proceeded on the timeout branch, so handover appeared to work — the
  // service does release promptly on the heartbeat — but the app was dialling
  // on a stopwatch rather than on the acknowledgement, and paid 3s of cold
  // start for the privilege.
  FlutterForegroundTask.initCommunicationPort();

  // MADR 0084 D1. The two hooks Flutter documents, and deliberately *not*
  // runZonedGuarded: PlatformDispatcher.onError supersedes it as of Flutter
  // 3.3, and wrapping runApp in a zone after the bindings are initialised
  // here is the documented recipe for a zone-mismatch error.
  final recorder = ErrorRecorder(SettingsStore());

  FlutterError.onError = (details) {
    // Keep console behaviour exactly as it was; the recorder is additive.
    FlutterError.presentError(details);
    unawaited(
      recorder.record(
        details.exception,
        details.stack,
        source: ErrorSource.flutter,
      ),
    );
  };

  PlatformDispatcher.instance.onError = (error, stack) {
    unawaited(recorder.record(error, stack, source: ErrorSource.platform));
    // Handled: without this, an async error in release is swallowed by the
    // zone with no log, no trace and no user signal (MADR 0084 A1).
    return true;
  };

  // Global by nature, so it is installed once here rather than from a widget
  // builder that would re-assign it on every rebuild (and trip flutter_test's
  // "ErrorWidget.builder was changed" guard in every test that pumps the app).
  ErrorWidget.builder = buildErrorPanel;

  runApp(
    ProviderScope(
      overrides: [errorRecorderProvider.overrideWithValue(recorder)],
      child: const MagicCliRemoteApp(),
    ),
  );
}
