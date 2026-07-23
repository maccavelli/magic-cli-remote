import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';

import '../../data/local/settings_store.dart';
import '../../data/protocol/picker.dart';
import '../../state/app_providers.dart';
import '../../state/transcripts_notifier.dart';
import '../../theme/celestial.dart';
import '../../theme/widgets.dart';
import '../widgets/option_picker_sheet.dart';

class SessionsScreen extends ConsumerStatefulWidget {
  const SessionsScreen({super.key});

  @override
  ConsumerState<SessionsScreen> createState() => _SessionsScreenState();
}

class _SessionsScreenState extends ConsumerState<SessionsScreen> {
  List<SessionMeta> _sessions = [];
  List<ProviderInfo> _providers = [];
  bool _loading = true;
  String? _error;
  bool _endingIdBusy = false;
  bool _creatingBusy = false;
  String? _version;
  StreamSubscription<SessionEvent>? _events;

  @override
  void initState() {
    super.initState();
    _refresh();
    _loadVersion();
    // Keep the status chips live: a list that still says "Working…" after the
    // "Agent finished" notification arrived reads as broken.
    _events = ref.read(mcremoteClientProvider).events.listen(_onSessionEvent);
  }

  @override
  void dispose() {
    _events?.cancel();
    super.dispose();
  }

  void _onSessionEvent(SessionEvent ev) {
    if (!mounted || ev.sessionId.isEmpty) return;
    String? status;
    if (ev.type == 'session_status' && (ev.status ?? '').isNotEmpty) {
      status = ev.status;
    } else if (ev.type == 'turn_complete') {
      status = 'idle';
    }
    if (status == null) return;
    final i = _sessions.indexWhere((s) => s.id == ev.sessionId);
    if (i < 0 || _sessions[i].status == status) return;
    setState(() {
      _sessions = List.of(_sessions)
        ..[i] = _sessions[i].copyWith(status: status);
    });
  }

  Future<void> _loadVersion() async {
    try {
      final info = await PackageInfo.fromPlatform();
      if (mounted) {
        setState(() {
          _version = '${info.version}+${info.buildNumber}';
        });
      }
    } catch (_) {}
  }

  Future<void> _refresh() async {
    if (!mounted) return;
    final client = ref.read(mcremoteClientProvider);
    // Stay on this screen while paired; never bounce to Connect on a blip.
    if (client.state == McConnectionState.reconnecting ||
        client.state == McConnectionState.connecting ||
        client.state == McConnectionState.authenticating) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = null;
        });
      }
      return;
    }
    if (client.state != McConnectionState.connected) {
      if (mounted) {
        setState(() {
          _loading = false;
          // Keep last known session list visible while offline, but drop the
          // stale error so it does not stay pinned once we come back.
          _error = null;
        });
      }
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final sessions = await client.listSessions();
      final providers = await client.listProviders();
      if (!mounted) return;
      // Authoritative status: a socket drop can lose the turn_complete that
      // would move a transcript out of 'running', which otherwise leaves the
      // chat composer disabled. Also evicts transcripts for sessions the host
      // no longer reports.
      ref.read(transcriptsProvider.notifier).syncFromMeta(sessions);
      // Feed display names to the notification layer so its bodies can say
      // "Fix the build" instead of a truncated session id.
      final coord = ref.read(notificationCoordinatorProvider);
      coord.sessionLabels
        ..clear()
        ..addEntries(
          sessions
              .where((s) => s.name.isNotEmpty)
              .map((s) => MapEntry(s.id, s.name)),
        );
      setState(() {
        _sessions = sessions;
        _providers = providers;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = friendlyOpError(e);
        _loading = false;
      });
    }
  }

  /// Resume a closed session: re-create it on the host under the same id and
  /// agent conversation (`agent_session_id`), then open its chat.
  Future<void> _resumeSession(SessionMeta s) async {
    // Shares the create guard: a double tap on a closed row would otherwise
    // issue two session.resume calls for the same id.
    if (_creatingBusy) return;
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    setState(() => _creatingBusy = true);
    String? location;
    try {
      final meta = await client.resumeSession(s);
      if (!mounted) return;
      final q = meta.name.isNotEmpty
          ? '?name=${Uri.encodeComponent(meta.name)}'
          : '';
      location = '/sessions/${meta.id}$q';
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Resume failed: ${friendlyOpError(e)}')),
      );
    } finally {
      if (mounted) setState(() => _creatingBusy = false);
    }
    if (location != null && mounted) await _openSession(location);
  }

  Future<void> _createSession() async {
    // The recents read is async before the sheet appears — without this guard
    // a double tap stacks two sheets and two session.create.
    if (_creatingBusy) return;
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Reconnect to the host first')),
      );
      return;
    }
    setState(() => _creatingBusy = true);
    String? location;
    try {
      location = await _createSessionFlow(client);
    } finally {
      // Release the button BEFORE navigating: `context.push` resolves only
      // when the chat route pops, and a chat exited via a redirect (`go`)
      // never resolves it — holding the flag across navigation wedged the
      // New-session button for the rest of the screen's life.
      if (mounted) setState(() => _creatingBusy = false);
    }
    if (location != null && mounted) await _openSession(location);
  }

  /// Runs the create dialog + `session.create`. Returns the chat route to
  /// open, or null when cancelled/failed. Never navigates — the caller owns
  /// navigation so the busy flag can be released first.
  Future<String?> _createSessionFlow(McremoteClient client) async {
    final nameCtrl = TextEditingController();
    final settings = ref.read(settingsStoreProvider);
    // Recently used working directories, newest first, for the cwd menu.
    List<String> recentCwds = const [];
    try {
      recentCwds = await settings.getRecentCwds();
    } catch (_) {}
    if (!mounted) {
      nameCtrl.dispose();
      return null;
    }
    // Reported by the daemon on auth; shown as the default when no path is
    // chosen and used to seed the free-form path input.
    final homeDir = client.hostHomeDir ?? '';
    // No pre-selected provider: the menu opens on "Choose provider" and
    // Create stays disabled until one is picked.
    String? provider;
    String model = '';
    // '' = daemon default (the host user's home directory).
    var cwd = '';
    // Sentinel menu entry that opens the free-form path input.
    const specifyPath = '\u0000specify-path';
    // Bumped to rebuild the cwd menu after "Specify path…" — the form field
    // would otherwise keep the sentinel as its internal selection.
    var cwdMenuEpoch = 0;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setModal) {
            Future<void> pickModel() async {
              final p = provider;
              if (p == null || p.isEmpty) return;
              PickerCatalog catalog;
              try {
                catalog = await client.listModels(p);
              } catch (e) {
                if (!ctx.mounted) return;
                ScaffoldMessenger.of(ctx).showSnackBar(
                  SnackBar(content: Text('Could not load models: $e')),
                );
                catalog = PickerCatalog(allowCustom: true, provider: p);
              }
              if (!ctx.mounted) return;
              final result = await showOptionPicker(
                ctx,
                catalog: catalog,
                title: 'Model · $p',
                initialSelected: model.isEmpty ? null : [model],
              );
              if (result == null || !ctx.mounted) return;
              setModal(() => model = result.single ?? '');
            }

            return AlertDialog(
              // Slightly wider and taller than the stock dialog: long paths
              // and model ids need the room.
              insetPadding: const EdgeInsets.symmetric(
                horizontal: 24,
                vertical: 24,
              ),
              title: const Text('New session'),
              content: SizedBox(
                width: double.maxFinite,
                child: SingleChildScrollView(
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(minHeight: 360),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        if (_providers.isEmpty)
                          const Text('No providers reported by host.')
                        else
                          DropdownButtonFormField<String>(
                            // ignore: deprecated_member_use
                            value: provider,
                            hint: const Text('Choose provider'),
                            decoration: const InputDecoration(
                              border: OutlineInputBorder(),
                            ),
                            items: _providers
                                .map(
                                  (p) => DropdownMenuItem(
                                    value: p.id,
                                    // Not-ready providers (binary missing on the
                                    // host) can't start sessions — disable them
                                    // instead of just annotating.
                                    enabled: p.ready,
                                    child: Opacity(
                                      opacity: p.ready ? 1 : 0.5,
                                      child: Text(
                                        '${p.id}${p.ready ? '' : ' (not installed on host)'}',
                                      ),
                                    ),
                                  ),
                                )
                                .toList(),
                            onChanged: (v) async {
                              setModal(() {
                                provider = v;
                                model = '';
                              });
                              if (v == null) return;
                              try {
                                final pref = await settings.getPreferredModel(
                                  v,
                                );
                                if (pref != null &&
                                    pref.isNotEmpty &&
                                    ctx.mounted) {
                                  setModal(() => model = pref);
                                }
                              } catch (_) {}
                            },
                          ),
                        const SizedBox(height: 12),
                        TextField(
                          controller: nameCtrl,
                          decoration: const InputDecoration(
                            labelText: 'Name (optional)',
                            border: OutlineInputBorder(),
                          ),
                        ),
                        const SizedBox(height: 12),
                        DropdownButtonFormField<String>(
                          key: ValueKey('cwd-menu-$cwdMenuEpoch'),
                          // ignore: deprecated_member_use
                          value: cwd.isEmpty ? null : cwd,
                          isExpanded: true,
                          // Unset = the daemon default: the host home directory.
                          hint: Text(
                            homeDir.isEmpty ? 'Host home directory' : homeDir,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                          decoration: const InputDecoration(
                            labelText: 'Working directory',
                            border: OutlineInputBorder(),
                          ),
                          items: [
                            const DropdownMenuItem(
                              value: specifyPath,
                              child: Text('Specify path…'),
                            ),
                            // A custom path entered this dialog run must be a
                            // menu value or the form field asserts.
                            if (cwd.isNotEmpty && !recentCwds.contains(cwd))
                              DropdownMenuItem(
                                value: cwd,
                                child: Text(
                                  cwd,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                            for (final p in recentCwds)
                              DropdownMenuItem(
                                value: p,
                                child: Text(
                                  p,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                          ],
                          onChanged: (v) async {
                            if (v == null) return;
                            if (v != specifyPath) {
                              setModal(() => cwd = v);
                              return;
                            }
                            final entered = await _promptForPath(
                              ctx,
                              initial: cwd.isNotEmpty ? cwd : homeDir,
                            );
                            if (!ctx.mounted) return;
                            setModal(() {
                              // Rebuild the menu even on cancel — the form field
                              // must not keep "Specify path…" as its selection.
                              cwdMenuEpoch++;
                              // Empty input = daemon default (host home).
                              if (entered != null) cwd = entered.trim();
                            });
                          },
                        ),
                        const SizedBox(height: 12),
                        InputDecorator(
                          decoration: const InputDecoration(
                            labelText: 'Select model (optional)',
                            border: OutlineInputBorder(),
                          ),
                          child: InkWell(
                            onTap: pickModel,
                            child: Padding(
                              padding: const EdgeInsets.symmetric(vertical: 4),
                              child: Row(
                                children: [
                                  Expanded(
                                    child: Text(
                                      model.isEmpty
                                          ? 'Provider default'
                                          : model,
                                      style: TextStyle(
                                        color: model.isEmpty
                                            ? Theme.of(
                                                ctx,
                                              ).colorScheme.onSurfaceVariant
                                            : null,
                                      ),
                                    ),
                                  ),
                                  const Icon(Icons.arrow_drop_down),
                                ],
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.pop(ctx, false),
                  child: const Text('Cancel'),
                ),
                FilledButton(
                  // Disabled until a provider is picked — the session must
                  // not fall back to one the user never chose.
                  onPressed: provider == null
                      ? null
                      : () => Navigator.pop(ctx, true),
                  child: const Text('Create'),
                ),
              ],
            );
          },
        );
      },
    );

    final name = nameCtrl.text.trim();
    nameCtrl.dispose();

    if (ok != true || provider == null) return null;
    try {
      final meta = await client.createSession(
        provider: provider,
        name: name.isEmpty ? null : name,
        cwd: cwd.isEmpty ? null : cwd,
        model: model.isEmpty ? null : model,
      );
      // Remember the directory actually used (the daemon reports the resolved
      // path, e.g. the host home when none was chosen) so it shows up in the
      // recent-paths menu next time.
      final usedCwd = meta.cwd ?? cwd;
      if (usedCwd.isNotEmpty) {
        try {
          await settings.addRecentCwd(usedCwd);
        } catch (_) {}
      }
      final prov = provider;
      if (prov != null && model.isNotEmpty) {
        try {
          await settings.setPreferredModel(prov, model);
        } catch (_) {}
      }
      if (!mounted) return null;
      final q = meta.name.isNotEmpty
          ? '?name=${Uri.encodeComponent(meta.name)}'
          : '';
      return '/sessions/${meta.id}$q';
    } catch (e) {
      if (!mounted) return null;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Create failed: ${friendlyOpError(e)}')),
      );
      return null;
    }
  }

  /// Free-form path entry for the working-directory menu. Returns the entered
  /// text (possibly empty = host home default), or null when cancelled.
  Future<String?> _promptForPath(
    BuildContext ctx, {
    required String initial,
  }) async {
    final ctrl = TextEditingController(text: initial);
    final result = await showDialog<String>(
      context: ctx,
      builder: (dctx) => AlertDialog(
        title: const Text('Working directory'),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: 'Absolute path on host',
            helperText: 'Leave empty for the host home directory',
            border: OutlineInputBorder(),
          ),
          onSubmitted: (v) => Navigator.pop(dctx, v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dctx),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dctx, ctrl.text),
            child: const Text('Use path'),
          ),
        ],
      ),
    );
    ctrl.dispose();
    return result;
  }

  /// Push the chat route and refresh on return. This page can be removed from
  /// the stack while chat is on top (sign-out, invalid_token redirect), so the
  /// mounted check between the two is required — `_refresh` uses `ref`.
  Future<void> _openSession(String location) async {
    await context.push(location);
    if (!mounted) return;
    await _refresh();
  }

  Future<void> _signOut() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Sign out of host?'),
        content: const Text(
          'Stops auto-reconnect on this phone until you connect again. '
          'Agent sessions on the host keep running. '
          'Your device token stays saved so you can reconnect without a new QR '
          '(use Clear credentials on the connect screen to wipe it).',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Stay connected'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Sign out'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final client = ref.read(mcremoteClientProvider);
    await client.disconnect(manual: true);
    ref.read(transcriptsProvider.notifier).clearAll();
    if (mounted) context.go('/');
  }

  Future<void> _reconnect() async {
    try {
      final store = ref.read(settingsStoreProvider);
      await ref.read(mcremoteClientProvider).reconnectFromStore(store);
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Reconnect failed: $e')));
    }
  }

  Future<void> _endSession(SessionMeta s) async {
    if (_endingIdBusy) return;
    // Claim the flag before the confirm dialog: two fast long-presses would
    // otherwise open two dialogs and issue two cancel+close pairs.
    setState(() => _endingIdBusy = true);
    try {
      await _endSessionFlow(s);
    } finally {
      if (mounted) setState(() => _endingIdBusy = false);
    }
  }

  Future<void> _endSessionFlow(SessionMeta s) async {
    final label = s.name.isEmpty
        ? (s.id.length > 8 ? s.id.substring(0, 8) : s.id)
        : s.name;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('End agent session?'),
        content: Text(
          'Stops “$label” on the host and removes it from this list.\n\n'
          'This does not sign the phone out of the host.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: destructiveFilled(Theme.of(ctx).colorScheme),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('End session'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;

    final client = ref.read(mcremoteClientProvider);
    // Ending happens on the host: claiming success offline would wipe local
    // state while the session resurrects on the next refresh.
    if (client.state != McConnectionState.connected) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text(
            'Reconnect to the host first — the session lives there.',
          ),
        ),
      );
      return;
    }

    // Optimistic remove so the list doesn't look stuck.
    final previous = List<SessionMeta>.from(_sessions);
    setState(() {
      _sessions = _sessions.where((x) => x.id != s.id).toList();
    });

    try {
      if (s.live) {
        try {
          await client.cancel(s.id);
        } catch (_) {}
      }
      // session.delete closes a live session *and* purges the disk record,
      // so it covers both cases. Previously a non-live row skipped the host
      // entirely and simply reappeared on the next refresh.
      await client.deleteSession(s.id);
      if (!mounted) return;
      // Clear the local transcript only after the host confirmed the delete.
      ref.read(transcriptsProvider.notifier).clearSession(s.id);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Ended $label')));
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      setState(() => _sessions = previous);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('End failed: ${friendlyOpError(e)}')),
      );
    }
  }

  /// Display name of the paired host: the hostname part of the endpoint the
  /// phone connected to. Falls back to the generic word when unknown.
  String _hostname() {
    final input = ref.read(mcremoteClientProvider).lastHostInput ?? '';
    if (input.trim().isEmpty) return 'host';
    try {
      return SettingsStore.parseEndpoint(input).host;
    } catch (_) {
      return 'host';
    }
  }

  String _connLabel(McConnectionState? s) {
    switch (s) {
      case McConnectionState.connected:
        return 'Connected to ${_hostname()}';
      case McConnectionState.reconnecting:
        return 'Reconnecting to host…';
      case McConnectionState.connecting:
        return 'Connecting…';
      case McConnectionState.authenticating:
        return 'Authenticating…';
      case McConnectionState.error:
        return 'Connection lost — retrying…';
      case McConnectionState.disconnected:
      case null:
        return 'Disconnected';
    }
  }

  @override
  Widget build(BuildContext context) {
    final conn = ref.watch(connectionStateProvider);
    final connState = conn.asData?.value;
    // The stream can fail outright; without this it renders as "Disconnected".
    final connError = conn.hasError ? conn.error.toString() : null;
    final healthy = connState == McConnectionState.connected;
    final scheme = Theme.of(context).colorScheme;

    ref.listen(connectionStateProvider, (prev, next) {
      final s = next.asData?.value;
      if (s == McConnectionState.connected) {
        _refresh();
      }
    });

    return Scaffold(
      appBar: AppBar(
        title: const Text('Sessions'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            onPressed: healthy && !_loading ? _refresh : null,
            icon: const Icon(Icons.refresh),
          ),
          IconButton(
            tooltip: 'Settings',
            onPressed: () => context.push('/settings'),
            icon: const Icon(Icons.settings_outlined),
          ),
          IconButton(
            tooltip: 'Sign out of host',
            onPressed: _signOut,
            icon: const Icon(Icons.logout),
          ),
        ],
      ),

      body: Stack(
        children: [
          // The MC monogram, faint and full-bleed, dissolved into the surface so
          // it reads as part of the background rather than a foreground image.
          const Positioned.fill(child: _SessionsBackdrop()),
          Column(
            children: [
              if (!healthy)
                Builder(
                  builder: (context) {
                    // "Linking" (reconnecting/connecting/authenticating) reads as a
                    // transient/tertiary state; a hard drop reads as an error.
                    final linking =
                        connState == McConnectionState.reconnecting ||
                        connState == McConnectionState.connecting ||
                        connState == McConnectionState.authenticating;
                    final bg = linking
                        ? scheme.tertiaryContainer
                        : scheme.errorContainer;
                    final fg = linking
                        ? scheme.onTertiaryContainer
                        : scheme.onErrorContainer;
                    return Material(
                      color: bg,
                      child: ListTile(
                        dense: true,
                        leading: linking
                            ? SizedBox(
                                width: 22,
                                height: 22,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                  color: fg,
                                ),
                              )
                            : Icon(Icons.wifi_off, color: fg),
                        title: Text(
                          connError != null
                              ? 'Connection error'
                              : _connLabel(connState),
                          style: TextStyle(color: fg),
                        ),
                        subtitle: Text(
                          connError ??
                              'Pairing stays active until you sign out.',
                          style: TextStyle(fontSize: 12, color: fg),
                        ),
                        trailing: TextButton(
                          onPressed: _reconnect,
                          child: Text('Retry now', style: TextStyle(color: fg)),
                        ),
                      ),
                    );
                  },
                )
              else
                Material(
                  color: scheme.surfaceContainerHighest,
                  child: ListTile(
                    dense: true,
                    // Green dot, not a check icon: connectivity is a state
                    // light, not a completed action.
                    leading: SizedBox(
                      width: 20,
                      height: 20,
                      child: Center(
                        child: Container(
                          width: 10,
                          height: 10,
                          decoration: BoxDecoration(
                            color: celestialOf(context).success,
                            shape: BoxShape.circle,
                            boxShadow: [
                              BoxShadow(
                                color: celestialOf(
                                  context,
                                ).success.withValues(alpha: 0.55),
                                blurRadius: 6,
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                    title: Text(
                      _connLabel(connState),
                      style: Theme.of(context).textTheme.bodySmall,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ),
              if (_error != null && healthy)
                Padding(
                  padding: const EdgeInsets.all(12),
                  child: Text(_error!, style: TextStyle(color: scheme.error)),
                ),
              Expanded(
                child: _loading && healthy
                    ? const Center(child: CircularProgressIndicator())
                    : RefreshIndicator(
                        onRefresh: _refresh,
                        child: _sessions.isEmpty
                            // AlwaysScrollable so pull-to-refresh works on the
                            // empty state too, not only on a populated list.
                            ? LayoutBuilder(
                                builder: (ctx, constraints) => SingleChildScrollView(
                                  physics:
                                      const AlwaysScrollableScrollPhysics(),
                                  child: ConstrainedBox(
                                    constraints: BoxConstraints(
                                      minHeight: constraints.maxHeight,
                                    ),
                                    child: Center(
                                      child: Padding(
                                        padding: const EdgeInsets.all(32),
                                        child: Column(
                                          mainAxisSize: MainAxisSize.min,
                                          children: [
                                            Icon(
                                              Icons.auto_awesome,
                                              size: 48,
                                              color: scheme.primary.withValues(
                                                alpha: 0.7,
                                              ),
                                            ),
                                            const SizedBox(height: 12),
                                            Text(
                                              healthy
                                                  ? 'No sessions on this device'
                                                  : 'Waiting for host connection',
                                              style: Theme.of(
                                                context,
                                              ).textTheme.titleMedium,
                                            ),
                                            const SizedBox(height: 8),
                                            Text(
                                              healthy
                                                  ? 'Create one to start. Sessions you open on another phone stay on that device.'
                                                  : 'Your pairing is still active. We reconnect automatically when the phone wakes.',
                                              textAlign: TextAlign.center,
                                              style: TextStyle(
                                                color: scheme.onSurfaceVariant,
                                              ),
                                            ),
                                            const SizedBox(height: 16),
                                            if (healthy)
                                              FilledButton.icon(
                                                onPressed: _creatingBusy
                                                    ? null
                                                    : _createSession,
                                                icon: const Icon(Icons.add),
                                                label: const Text(
                                                  'New session',
                                                ),
                                              )
                                            else
                                              FilledButton.tonalIcon(
                                                onPressed: _reconnect,
                                                icon: const Icon(Icons.sync),
                                                label: const Text('Retry now'),
                                              ),
                                          ],
                                        ),
                                      ),
                                    ),
                                  ),
                                ),
                              )
                            : ListView.builder(
                                itemCount: _sessions.length,
                                padding: const EdgeInsets.only(
                                  top: 4,
                                  bottom: 4,
                                ),
                                itemBuilder: (ctx, i) {
                                  final s = _sessions[i];
                                  final title = s.name.isEmpty
                                      ? (s.id.length >= 8
                                            ? 'Session ${s.id.substring(0, 8)}'
                                            : s.id)
                                      : s.name;
                                  final subtitleParts = [
                                    s.provider,
                                    if (s.model.isNotEmpty) s.model,
                                    if ((s.cwd ?? '').isNotEmpty) s.cwd!,
                                  ];
                                  final tile = ListTile(
                                    enabled: healthy,
                                    shape: RoundedRectangleBorder(
                                      borderRadius: BorderRadius.circular(16),
                                    ),
                                    title: Text(title),
                                    subtitle: Text(
                                      subtitleParts.join(' · '),
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                    trailing: Row(
                                      mainAxisSize: MainAxisSize.min,
                                      children: [
                                        StatusChip(
                                          status: s.live ? s.status : 'closed',
                                        ),
                                        PopupMenuButton<String>(
                                          tooltip: 'Session actions',
                                          onSelected: (v) async {
                                            if (v == 'open' &&
                                                s.live &&
                                                healthy) {
                                              final q = s.name.isNotEmpty
                                                  ? '?name=${Uri.encodeComponent(s.name)}'
                                                  : '';
                                              await _openSession(
                                                '/sessions/${s.id}$q',
                                              );
                                            } else if (v == 'resume') {
                                              await _resumeSession(s);
                                            } else if (v == 'end') {
                                              await _endSession(s);
                                            }
                                          },
                                          itemBuilder: (_) => [
                                            if (s.live && healthy)
                                              const PopupMenuItem(
                                                value: 'open',
                                                child: Text('Open'),
                                              ),
                                            if (!s.live && healthy)
                                              const PopupMenuItem(
                                                value: 'resume',
                                                child: Text('Resume'),
                                              ),
                                            const PopupMenuItem(
                                              value: 'end',
                                              child: Text('End session'),
                                            ),
                                          ],
                                        ),
                                      ],
                                    ),
                                    // A closed session is one tap from living
                                    // again — resume re-creates it on the host
                                    // with its agent conversation intact.
                                    onTap: !healthy
                                        ? null
                                        : s.live
                                        ? () async {
                                            final q = s.name.isNotEmpty
                                                ? '?name=${Uri.encodeComponent(s.name)}'
                                                : '';
                                            await _openSession(
                                              '/sessions/${s.id}$q',
                                            );
                                          }
                                        : () => _resumeSession(s),
                                    onLongPress: _endingIdBusy
                                        ? null
                                        : () => _endSession(s),
                                  );
                                  return Card(
                                    child: s.live
                                        ? tile
                                        : Opacity(opacity: 0.6, child: tile),
                                  );
                                },
                              ),
                      ),
              ),
              // Thumb-reachable create action for a populated list. The empty state
              // has its own CTA; without this, a new session was unreachable once
              // any session existed (kept as a bottom button, not a FAB).
              if (healthy && _sessions.isNotEmpty)
                SafeArea(
                  top: false,
                  bottom: _version == null,
                  minimum: const EdgeInsets.fromLTRB(12, 4, 12, 4),
                  child: SizedBox(
                    width: double.infinity,
                    child: FilledButton.icon(
                      onPressed: _creatingBusy ? null : _createSession,
                      icon: const Icon(Icons.add),
                      label: const Text('New session'),
                    ),
                  ),
                ),
              if (_version != null)
                SafeArea(
                  top: false,
                  child: Padding(
                    padding: const EdgeInsets.all(8.0),
                    child: Text(
                      'v$_version',
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: scheme.onSurfaceVariant.withValues(alpha: 0.5),
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

/// A faint, full-bleed MC monogram behind the sessions list. Opacity is tuned
/// per brightness so it dissolves into the surface instead of competing with the
/// list rows; [IgnorePointer] keeps it from stealing taps/scroll.
class _SessionsBackdrop extends StatelessWidget {
  const _SessionsBackdrop();

  @override
  Widget build(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return IgnorePointer(
      child: Opacity(
        opacity: dark ? 0.10 : 0.05,
        child: Image.asset(
          'assets/MC_icon.png',
          fit: BoxFit.cover,
          alignment: Alignment.center,
        ),
      ),
    );
  }
}
