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
import '../../theme/starfield.dart';
import '../../theme/top_notification.dart';
import '../../theme/widgets.dart';
import '../widgets/option_picker_sheet.dart';

/// Vertical gap between every field in the new-session dialog. One constant so
/// the spacing is identical by construction rather than by three matching
/// literals that drift apart on the next edit.
const _newSessionFieldGap = SizedBox(height: 20);

/// Left inset of an [InputDecorator]'s floating label inside its outline
/// border, under this app's input theme. Added to the dialog title padding so
/// the header starts on the same vertical line as the field labels below it.
/// Measured, not assumed — see the "new-session dialog layout" tests.
const _inputContentInset = 16.0;

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
      showTopNotification(context, 'Reconnect to the host first');
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
      showTopNotification(context, 'Resume failed: ${friendlyOpError(e)}');
    } finally {
      if (mounted) setState(() => _creatingBusy = false);
    }
    if (location != null && mounted) await _openSession(location);
  }

  Future<void> _renameSession(SessionMeta session) async {
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
      return;
    }
    var name = session.name;
    final accepted = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Rename session'),
        content: TextFormField(
          initialValue: session.name,
          autofocus: true,
          maxLength: 256,
          textInputAction: TextInputAction.done,
          onChanged: (value) => name = value,
          onFieldSubmitted: (_) => Navigator.pop(ctx, true),
          decoration: const InputDecoration(
            labelText: 'Session name',
            border: OutlineInputBorder(),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    name = name.trim();
    if (accepted != true || name.isEmpty || !mounted) return;
    try {
      final renamed = await client.renameSession(session.id, name);
      if (!mounted) return;
      final index = _sessions.indexWhere((s) => s.id == session.id);
      if (index >= 0) {
        setState(() {
          _sessions = List.of(_sessions)..[index] = renamed;
        });
      }
      ref.read(notificationCoordinatorProvider).sessionLabels[session.id] =
          renamed.name;
    } catch (e) {
      if (!mounted) return;
      showTopNotification(context, 'Rename failed: ${friendlyOpError(e)}');
    }
  }

  Future<void> _createSession() async {
    // The recents read is async before the sheet appears — without this guard
    // a double tap stacks two sheets and two session.create.
    if (_creatingBusy) return;
    final client = ref.read(mcremoteClientProvider);
    if (client.state != McConnectionState.connected) {
      showTopNotification(context, 'Reconnect to the host first');
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
    // Tracked via onChanged instead of a controller owned here: the dialog
    // future resolves while the route is still animating out, and disposing a
    // controller then races an active IME composition (clearComposing on a
    // disposed controller). Letting the field own its controller defers
    // disposal until after unmount.
    var name = '';
    final settings = ref.read(settingsStoreProvider);
    // Recently used working directories, newest first, for the cwd menu.
    List<String> recentCwds = const [];
    try {
      recentCwds = await settings.getRecentCwds();
    } catch (_) {}
    if (!mounted) return null;
    // Reported by the daemon on auth; shown as the default when no path is
    // chosen and used to seed the free-form path input.
    final homeDir = client.hostHomeDir ?? '';
    // No pre-selected provider: the menu opens on "Choose provider" and
    // Create stays disabled until one is picked.
    String? provider;
    String model = '';
    // OpenCode agent name (build/plan/…); empty = engine default.
    String agent = '';
    // A selected provider-native conversation to load through session.create.
    // Empty means create a new agent-native conversation.
    AgentSessionMeta? nativeSession;
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
                showTopNotification(ctx, 'Could not load models: $e');
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

            Future<void> pickAgent() async {
              final p = provider;
              if (p == null || p.isEmpty) return;
              PickerCatalog catalog;
              try {
                catalog = await client.listAgents(p);
              } catch (e) {
                if (!ctx.mounted) return;
                showTopNotification(ctx, 'Could not load agents: $e');
                catalog = PickerCatalog(allowCustom: true, provider: p);
              }
              if (!ctx.mounted) return;
              // Nothing to pick and no free-text — skip quietly.
              if (catalog.options.isEmpty && !catalog.allowCustom) return;
              final result = await showOptionPicker(
                ctx,
                catalog: catalog,
                title: 'Agent · $p',
                initialSelected: agent.isEmpty ? null : [agent],
              );
              if (result == null || !ctx.mounted) return;
              setModal(() => agent = result.single ?? '');
            }

            Future<void> pickNativeSession() async {
              final p = provider;
              if (p == null || p.isEmpty) return;
              List<AgentSessionMeta> sessions;
              try {
                sessions = await client.listAgentSessions(p);
              } catch (e) {
                if (!ctx.mounted) return;
                showTopNotification(
                  ctx,
                  'Could not load existing sessions: $e',
                );
                return;
              }
              if (!ctx.mounted) return;
              final selected = await showDialog<AgentSessionMeta>(
                context: ctx,
                builder: (pickCtx) => AlertDialog(
                  title: Text('Resume session · $p'),
                  content: SizedBox(
                    width: double.maxFinite,
                    child: sessions.isEmpty
                        ? const Text('No resumable sessions were reported.')
                        : ListView.builder(
                            shrinkWrap: true,
                            itemCount: sessions.length,
                            itemBuilder: (_, index) {
                              final session = sessions[index];
                              final details = [
                                if (session.cwd.isNotEmpty) session.cwd,
                                if (session.updatedAt != null)
                                  'Updated ${session.updatedAt!.toLocal()}',
                              ].join('\n');
                              return ListTile(
                                title: Text(
                                  session.displayName,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                subtitle: details.isEmpty
                                    ? null
                                    : Text(details),
                                onTap: () => Navigator.pop(pickCtx, session),
                              );
                            },
                          ),
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(pickCtx),
                      child: const Text('Cancel'),
                    ),
                  ],
                ),
              );
              if (selected == null || !ctx.mounted) return;
              setModal(() {
                nativeSession = selected;
                // ACP requires a cwd for session/load. Prefer the native
                // session's recorded workspace when the agent supplied it.
                if (selected.cwd.isNotEmpty) cwd = selected.cwd;
              });
            }

            return AlertDialog(
              // Slightly wider and taller than the stock dialog: long paths
              // and model ids need the room.
              insetPadding: const EdgeInsets.symmetric(
                horizontal: 24,
                vertical: 24,
              ),
              // Default titlePadding puts the header flush with the field
              // *borders*; the inset lines it up with the field labels instead.
              titlePadding: const EdgeInsets.fromLTRB(
                24 + _inputContentInset,
                24,
                24,
                0,
              ),
              title: const Text('Begin new session'),
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
                              labelText: 'Available providers',
                              floatingLabelBehavior:
                                  FloatingLabelBehavior.always,
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
                                agent = '';
                                nativeSession = null;
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
                        _newSessionFieldGap,
                        TextField(
                          onChanged: (v) => name = v,
                          decoration: const InputDecoration(
                            labelText: 'Friendly name',
                            // Pinned like the neighbouring menus, whose labels
                            // always sit on the border: a label that starts
                            // inside the box and floats on focus makes this
                            // one field read as taller and unevenly spaced.
                            floatingLabelBehavior: FloatingLabelBehavior.always,
                            hintText: 'Optional',
                            border: OutlineInputBorder(),
                          ),
                        ),
                        _newSessionFieldGap,
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
                        _newSessionFieldGap,
                        InputDecorator(
                          decoration: const InputDecoration(
                            labelText: 'Select model (optional)',
                            border: OutlineInputBorder(),
                          ),
                          // No padding around the row: the decorator already
                          // supplies the same content insets the real fields
                          // use, and an extra 4pt top/bottom made this field
                          // 64pt tall next to their 56pt.
                          child: InkWell(
                            onTap: pickModel,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    model.isEmpty ? 'Provider default' : model,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
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
                        _newSessionFieldGap,
                        InputDecorator(
                          decoration: const InputDecoration(
                            labelText: 'Resume existing session (optional)',
                            border: OutlineInputBorder(),
                          ),
                          child: InkWell(
                            onTap: provider == null ? null : pickNativeSession,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    nativeSession == null
                                        ? 'Start a new conversation'
                                        : nativeSession!.displayName,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(
                                      color: nativeSession == null
                                          ? Theme.of(
                                              ctx,
                                            ).colorScheme.onSurfaceVariant
                                          : null,
                                    ),
                                  ),
                                ),
                                const Icon(Icons.history),
                              ],
                            ),
                          ),
                        ),
                        // Agent picker (OpenCode GET /agent via agents.list).
                        // Shown for every provider; empty catalogs still allow
                        // free-text or "engine default".
                        _newSessionFieldGap,
                        InputDecorator(
                          decoration: const InputDecoration(
                            labelText: 'Select agent (optional)',
                            border: OutlineInputBorder(),
                          ),
                          child: InkWell(
                            onTap: provider == null ? null : pickAgent,
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    agent.isEmpty ? 'Provider default' : agent,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(
                                      color: agent.isEmpty
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

    name = name.trim();
    if (ok != true || provider == null) return null;
    try {
      final meta = await client.createSession(
        provider: provider,
        name: name.isEmpty ? null : name,
        cwd: cwd.isEmpty ? null : cwd,
        model: model.isEmpty ? null : model,
        agent: agent.isEmpty ? null : agent,
        agentSessionId: nativeSession?.id,
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
      showTopNotification(context, 'Create failed: ${friendlyOpError(e)}');
      return null;
    }
  }

  /// Free-form path entry for the working-directory menu. Returns the entered
  /// text (possibly empty = host home default), or null when cancelled.
  Future<String?> _promptForPath(
    BuildContext ctx, {
    required String initial,
  }) async {
    // The field owns its controller (seeded via initialValue) so disposal is
    // sequenced after the route unmounts — see _createSessionFlow.
    var text = initial;
    return showDialog<String>(
      context: ctx,
      builder: (dctx) => AlertDialog(
        title: const Text('Working directory'),
        content: TextFormField(
          initialValue: initial,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: 'Absolute path on host',
            helperText: 'Leave empty for the host home directory',
            border: OutlineInputBorder(),
          ),
          onChanged: (v) => text = v,
          onFieldSubmitted: (v) => Navigator.pop(dctx, v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dctx),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dctx, text),
            child: const Text('Use path'),
          ),
        ],
      ),
    );
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
    // Resolve everything that needs `ref` before the first await — the
    // disconnect triggers the redirect that disposes this screen.
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    await client.disconnect(manual: true);
    transcripts.clearAll();
    if (mounted) context.go('/');
  }

  Future<void> _reconnect() async {
    try {
      final store = ref.read(settingsStoreProvider);
      await ref.read(mcremoteClientProvider).reconnectFromStore(store);
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      showTopNotification(context, 'Reconnect failed: $e');
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
      showTopNotification(
        context,
        'Reconnect to the host first — the session lives there.',
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
      showTopNotification(context, 'Ended $label');
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      setState(() => _sessions = previous);
      showTopNotification(context, 'End failed: ${friendlyOpError(e)}');
    }
  }

  /// Display name of the paired host: the hostname part of the endpoint the
  /// phone connected to. Falls back to the generic word when unknown.
  static String _hostnameOf(String? input) {
    if (input == null || input.trim().isEmpty) return 'host';
    try {
      return SettingsStore.parseEndpoint(input).host;
    } catch (_) {
      return 'host';
    }
  }

  String _connLabel(McConnectionState? s, String hostname) {
    switch (s) {
      case McConnectionState.connected:
        return 'Connected to $hostname';
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
    final client = ref.watch(mcremoteClientProvider);

    ref.listen(connectionStateProvider, (prev, next) {
      final s = next.asData?.value;
      if (s == McConnectionState.connected) {
        _refresh();
      }
    });

    // Host is a ValueNotifier on the client so the label updates even when
    // connection state is already "connected" and only the dialled host moves.
    return ValueListenableBuilder<String?>(
      valueListenable: client.hostInputListenable,
      builder: (context, hostInput, _) {
        final connLabel = _connLabel(connState, _hostnameOf(hostInput));
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
              // Same space gradient + starfield as Connect and Chat, so the
              // three screens share one sky instead of this one sitting on a
              // flat surface.
              const Positioned.fill(child: CelestialBackdrop()),
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
                        final fg = linking
                            ? scheme.onTertiaryContainer
                            : scheme.onErrorContainer;
                        return ConnBanner(
                          kind: linking
                              ? ConnBannerKind.linking
                              : ConnBannerKind.offline,
                          message: connError != null
                              ? 'Connection error'
                              : connLabel,
                          subtitle:
                              connError ??
                              'Pairing stays active until you sign out.',
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
                          trailing: TextButton(
                            onPressed: _reconnect,
                            child: Text(
                              'Retry now',
                              style: TextStyle(color: fg),
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
                        // light, not a completed action. No shared ConnBanner
                        // variant — chat has no healthy strip of this form.
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
                          connLabel,
                          style: Theme.of(context).textTheme.bodySmall,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                  if (_error != null && healthy)
                    Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                        _error!,
                        style: TextStyle(color: scheme.error),
                      ),
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
                                                  color: scheme.primary
                                                      .withValues(alpha: 0.7),
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
                                                    color:
                                                        scheme.onSurfaceVariant,
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
                                                    icon: const Icon(
                                                      Icons.sync,
                                                    ),
                                                    label: const Text(
                                                      'Retry now',
                                                    ),
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
                                          borderRadius: BorderRadius.circular(
                                            16,
                                          ),
                                        ),
                                        title: Text(
                                          title,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        subtitle: Text(
                                          subtitleParts.join(' · '),
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        trailing: Row(
                                          mainAxisSize: MainAxisSize.min,
                                          children: [
                                            StatusChip(
                                              status: s.live
                                                  ? s.status
                                                  : 'closed',
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
                                                } else if (v == 'rename') {
                                                  await _renameSession(s);
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
                                                if (healthy)
                                                  const PopupMenuItem(
                                                    value: 'rename',
                                                    child: Text('Rename'),
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
                                            : Opacity(
                                                opacity: 0.6,
                                                child: tile,
                                              ),
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
                          style: Theme.of(context).textTheme.labelSmall
                              ?.copyWith(
                                color: scheme.onSurfaceVariant.withValues(
                                  alpha: 0.5,
                                ),
                              ),
                        ),
                      ),
                    ),
                ],
              ),
            ],
          ),
        );
      },
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
