import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// A [ProviderContainer] for tests that drive [transcriptsProvider], whose
/// transcript saves finish before the test ends.
///
/// Disposing the notifier flushes pending sessions to its cache without
/// awaiting the write, which is right for the app and wrong for tests: the
/// write resolves its directory lazily through the global path provider that
/// every test's setUp replaces, so a save that outlives its test can land in
/// the next test's directory, sweep that test's temp files and overwrite its
/// transcript (MADR 0173 F6, F7).
///
/// This gives the notifier a cache of its own and registers a teardown that
/// disposes the container and then waits for that cache's queue.
///
/// A test that needs its own [TranscriptCache] passes it as [cache] rather
/// than setting `debugCache` afterwards: replacing the cache behind the
/// helper's back leaves the teardown waiting on a cache that no longer
/// receives the disposal flush (PLAN 0173, Deviation 1).
ProviderContainer transcriptsTestContainer(
  void Function(dynamic Function()) addTearDown, {
  List<Override> overrides = const [],
  TranscriptCache? cache,
}) {
  final container = ProviderContainer(overrides: overrides);
  final drained = cache ?? TranscriptCache();
  container.read(transcriptsProvider.notifier).debugCache = drained;
  addTearDown(() async {
    container.dispose();
    await drained.debugWhenIdle;
  });
  return container;
}
