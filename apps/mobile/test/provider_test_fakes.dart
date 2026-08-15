/// Shared fakes for the provider surfaces (MADR 0082 P3).
library;

import 'dart:async';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

/// A connected client reporting a fixed provider list, with recordable
/// credential operations and an injectable auth-status push stream.
class FakeAuthClient extends McremoteClient {
  FakeAuthClient(this.providers);

  List<ProviderInfo> providers;
  final removed = <(String, String)>[];

  /// When set, credential operations throw this instead of succeeding.
  Object? credentialError;
  final switched = <(String, String)>[];
  ProviderAuthCatalog? catalogPage;

  /// Injectable `provider.auth_status` pushes (MADR 0074 D10).
  final authPush = StreamController<Map<String, dynamic>>.broadcast();

  final prewarmPush = StreamController<Map<String, dynamic>>.broadcast();
  final prewarmSets = <(String, bool)>[];
  String prewarmEngine = 'running';
  Object? prewarmError;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Stream<Map<String, dynamic>> get providerAuthStatus => authPush.stream;

  @override
  Stream<Map<String, dynamic>> get providerPrewarm => prewarmPush.stream;

  @override
  Future<String> setProviderPrewarm(String providerId, bool value) async {
    final err = prewarmError;
    if (err != null) throw err;
    prewarmSets.add((providerId, value));
    return prewarmEngine;
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => providers;

  @override
  Future<void> clearProviderCredential({
    required String providerId,
    required String upstreamId,
  }) async {
    final err = credentialError;
    if (err != null) throw err;
    removed.add((providerId, upstreamId));
  }

  @override
  Future<void> setActiveUpstream({
    required String providerId,
    required String upstreamId,
  }) async {
    switched.add((providerId, upstreamId));
  }

  @override
  Future<ProviderAuthCatalog?> listUpstreamCatalog({
    required String providerId,
    String query = '',
    int offset = 0,
    int limit = 0,
  }) async => catalogPage;

  @override
  Future<void> dispose() async {
    await authPush.close();
    await prewarmPush.close();
    await super.dispose();
  }
}

/// A store whose default-mode map lives in memory.
class FakeModeStore extends SettingsStore {
  final modes = <String, String>{};

  @override
  Future<String?> getDefaultSessionMode(String provider) async =>
      modes[provider];

  @override
  Future<void> setDefaultSessionMode(String provider, String? mode) async {
    if (mode == null || mode.isEmpty) {
      modes.remove(provider);
    } else {
      modes[provider] = mode;
    }
  }
}

ProviderInfo providerWith(
  String id,
  List<UpstreamAuth> ups, {
  String? active,
  bool ready = true,
  bool? prewarm,
}) => ProviderInfo(
  id: id,
  ready: ready,
  prewarm: prewarm,
  auth: ProviderAuthInfo(
    status: AuthStatus.configured,
    activeUpstream: active,
    upstreams: ups,
  ),
);

const configuredTogether = UpstreamAuth(
  id: 'together',
  label: 'Together AI',
  status: AuthStatus.configured,
);
const configuredDeepseek = UpstreamAuth(
  id: 'deepseek',
  label: 'DeepSeek',
  status: AuthStatus.configured,
);
