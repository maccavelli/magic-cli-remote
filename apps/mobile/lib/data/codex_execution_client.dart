import 'protocol/models.dart';

/// The Codex execution surface (MADR 0109 D10/D11/D36/D37, plan P7).
///
/// The three execution authorities are separate methods on purpose. Nothing
/// in this interface lets a caller reach the unsandboxed host through the
/// sandboxed one: [runSandboxedExec] takes argv and never a command string,
/// and [runUnsandboxedShell] takes a command string and never argv. Both
/// unsandboxed entry points require the caller to have taken a fresh
/// confirmation for that exact invocation — the daemon re-checks it, so an
/// unconfirmed call fails rather than running.
abstract mixin class CodexExecutionClient {
  /// Terminals for one session: daemon-owned plus negotiated native entries.
  Future<List<CodexTerminalInfo>> listTerminals(String sessionId);

  /// Retained chunks after [afterSequence]. The result reports a gap when the
  /// bounded buffer already dropped that position.
  Future<CodexTerminalBuffer> readTerminalOutput(
    String sessionId,
    String terminalId, {
    int afterSequence = 0,
  });

  /// Argv-only, sandboxed under the session's permission profile.
  Future<CodexExecResult> runSandboxedExec(
    String sessionId,
    List<String> argv, {
    String cwd = '',
    String permissionProfileId = '',
    int timeoutMs = 0,
  });

  /// Full host access. [confirmed] must be the user's fresh confirmation for
  /// this exact command; the daemon refuses the call without it.
  Future<void> runUnsandboxedShell(
    String sessionId,
    String command, {
    required bool confirmed,
  });

  /// Default-off standalone unsandboxed process, confirmed per spawn.
  Future<String> spawnStandaloneProcess(
    String sessionId,
    List<String> argv, {
    required String cwd,
    required bool confirmed,
    Map<String, String> env = const {},
    bool tty = false,
    int rows = 0,
    int cols = 0,
  });

  Future<void> writeTerminal(
    String sessionId,
    String terminalId,
    String text, {
    bool closeStdin = false,
  });

  Future<void> resizeTerminal(
    String sessionId,
    String terminalId,
    int rows,
    int cols,
  );

  Future<void> stopTerminal(String sessionId, String terminalId);

  Future<int> stopAllTerminals(String sessionId);

  /// Host-configured environments. The projection carries ids and allowed
  /// roots; no endpoint or credential ever reaches the phone.
  Future<List<CodexExecutionEnvironment>> listExecutionEnvironments() async =>
      const [];

  Future<CodexEnvironmentStatus> readEnvironmentStatus(
    String environmentId,
  ) async => const CodexEnvironmentStatus();

  /// Selects where turns run. Passing a null [environmentId] explicitly
  /// disables the sticky selection; not calling this at all preserves it.
  Future<void> selectExecutionEnvironment(
    String sessionId,
    String? environmentId, {
    required bool confirmed,
    String cwd = '',
    List<String> runtimeWorkspaceRoots = const [],
  }) async => throw UnsupportedError('execution environments unavailable');

  /// Live `codex.terminal.output` pushes for the owning device.
  Stream<Map<String, dynamic>> get codexTerminalOutput =>
      const Stream<Map<String, dynamic>>.empty();
}
