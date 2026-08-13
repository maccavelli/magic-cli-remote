/// On-device failure diary (MADR 0084 D1/D2).
///
/// This app ships no telemetry and talks to a host the user runs themselves,
/// so when something fails there is no crash service to consult — whatever the
/// device recorded is the entire diagnostic story. Before this existed, an
/// uncaught async error in release vanished silently and a build-phase throw
/// rendered a grey box with nothing to report.
///
/// **Secret safety.** Entries persist `error.runtimeType` and
/// `error.toString()`. That is only safe because no credential path puts a
/// secret into its error text — MADR 0074 D11 established that rule for the
/// write paths, and MADR 0083 D5 keeps raw engine text on the daemon side
/// rather than forwarding it to the phone. Both are load-bearing here: if
/// either changes, this recorder becomes a way to leak a key into a
/// copy-to-clipboard button.
library;

import 'package:flutter/foundation.dart';

import '../local/settings_store.dart';

/// Longest stored message. Matches the bound MADR 0066 D5 already uses for
/// the storage-failure row: a platform exception can carry a stack-sized
/// message, and this store is not a log.
const int kRecordedErrorMaxMessageChars = 300;

/// Longest stored stack, in lines and characters. The top frames carry the
/// signal; the tail is framework plumbing.
const int kRecordedErrorMaxStackLines = 20;
const int kRecordedErrorMaxStackChars = 2000;

/// How many failures are kept. A ring, newest last.
const int kRecordedErrorRingSize = 5;

/// Where a recorded failure came from.
class ErrorSource {
  /// `FlutterError.onError` — build, layout and paint.
  static const flutter = 'flutter';

  /// `PlatformDispatcher.instance.onError` — async and platform-channel.
  static const platform = 'platform';

  /// Reported explicitly by app code that caught something user-affecting.
  static const app = 'app';
}

/// One recorded failure.
@immutable
class RecordedError {
  const RecordedError({
    required this.kind,
    required this.message,
    required this.stack,
    required this.at,
    required this.source,
  });

  /// The exception's runtime type — never the object, whose fields could hold
  /// anything.
  final String kind;

  /// Bounded to [kRecordedErrorMaxMessageChars].
  final String message;

  /// Bounded to [kRecordedErrorMaxStackLines] / [kRecordedErrorMaxStackChars];
  /// empty when the failure carried no stack.
  final String stack;

  final DateTime at;

  /// One of [ErrorSource]'s values.
  final String source;

  Map<String, dynamic> toJson() => {
    'kind': kind,
    'message': message,
    if (stack.isNotEmpty) 'stack': stack,
    'at': at.toUtc().toIso8601String(),
    'source': source,
  };

  /// Returns null for an entry that cannot be read back — a malformed record
  /// is dropped, never allowed to break the list it lives in.
  static RecordedError? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final m = Map<String, dynamic>.from(raw);
    final at = DateTime.tryParse(m['at'] as String? ?? '');
    if (at == null) return null;
    return RecordedError(
      kind: m['kind'] as String? ?? 'Error',
      message: m['message'] as String? ?? '',
      stack: m['stack'] as String? ?? '',
      at: at,
      source: m['source'] as String? ?? ErrorSource.app,
    );
  }

  /// The text the "Copy details" affordance puts on the clipboard.
  String toClipboardText() {
    final buffer = StringBuffer()
      ..writeln('$kind ($source)')
      ..writeln(at.toLocal().toString())
      ..writeln(message);
    if (stack.isNotEmpty) buffer.writeln(stack);
    return buffer.toString();
  }
}

/// Persists the last [kRecordedErrorRingSize] failures through [SettingsStore].
class ErrorRecorder {
  ErrorRecorder(this._store, {DateTime Function()? clock})
    : _clock = clock ?? DateTime.now;

  final SettingsStore _store;
  final DateTime Function() _clock;

  /// Records one failure. **Never throws and never rethrows**: the diagnostics
  /// layer failing must not be able to take down the error path it exists to
  /// serve — that would turn one bug into a crash loop.
  Future<void> record(
    Object error,
    StackTrace? stack, {
    required String source,
  }) async {
    try {
      final entry = RecordedError(
        kind: error.runtimeType.toString(),
        message: _clip(error.toString(), kRecordedErrorMaxMessageChars),
        stack: _clipStack(stack),
        at: _clock(),
        source: source,
      );
      await _store.appendRecentError(entry.toJson());
    } catch (e) {
      // Deliberately terminal: there is nowhere left to report to.
      debugPrint('ErrorRecorder.record failed: $e');
    }
  }

  /// Newest first, for display.
  Future<List<RecordedError>> recent() async {
    try {
      final raw = await _store.getRecentErrors();
      final out = <RecordedError>[];
      for (final e in raw) {
        final parsed = RecordedError.tryParse(e);
        if (parsed != null) out.add(parsed);
      }
      return out.reversed.toList();
    } catch (e) {
      debugPrint('ErrorRecorder.recent failed: $e');
      return const [];
    }
  }

  Future<void> clear() async {
    try {
      await _store.clearRecentErrors();
    } catch (e) {
      debugPrint('ErrorRecorder.clear failed: $e');
    }
  }

  static String _clip(String s, int max) =>
      s.length > max ? s.substring(0, max) : s;

  static String _clipStack(StackTrace? stack) {
    if (stack == null) return '';
    final lines = stack.toString().split('\n');
    final head = lines.length > kRecordedErrorMaxStackLines
        ? lines.sublist(0, kRecordedErrorMaxStackLines)
        : lines;
    return _clip(head.join('\n'), kRecordedErrorMaxStackChars);
  }
}
