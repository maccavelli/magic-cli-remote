import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../state/app_providers.dart';
import 'section_card.dart';

class CodexRuntimeScreen extends ConsumerStatefulWidget {
  const CodexRuntimeScreen({super.key});

  @override
  ConsumerState<CodexRuntimeScreen> createState() => _CodexRuntimeScreenState();
}

class _CodexRuntimeScreenState extends ConsumerState<CodexRuntimeScreen> {
  CodexRuntimeSnapshot? _runtime;
  Map<String, dynamic>? _doctor;
  String? _error;
  bool _loading = true;
  bool _diagnosing = false;
  bool _savingPermissions = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final runtime = await ref.read(mcremoteClientProvider).readCodexRuntime();
      if (!mounted) return;
      setState(() {
        _runtime = runtime;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = error.toString();
      });
    }
  }

  Future<void> _runDoctor() async {
    setState(() {
      _diagnosing = true;
      _error = null;
    });
    try {
      final report = await ref.read(mcremoteClientProvider).runCodexDoctor();
      if (!mounted) return;
      setState(() {
        _doctor = report;
        _diagnosing = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _diagnosing = false;
        _error = error.toString();
      });
    }
  }

  Future<void> _writePermissions({String? profile, String? reviewer}) async {
    final runtime = _runtime;
    if (runtime == null) return;
    final allowedProfiles = runtime.permissionProfiles
        .where((item) => item.allowed)
        .toList(growable: false);
    final selectedProfile =
        profile ??
        (runtime.requestedProfileId.isNotEmpty
            ? runtime.requestedProfileId
            : runtime.effectiveProfileId.isNotEmpty
            ? runtime.effectiveProfileId
            : allowedProfiles.isEmpty
            ? ''
            : allowedProfiles.first.id);
    final selectedReviewer =
        reviewer ??
        (runtime.requestedReviewer.isNotEmpty
            ? runtime.requestedReviewer
            : runtime.effectiveReviewer.isNotEmpty
            ? runtime.effectiveReviewer
            : 'user');
    if (selectedProfile.isEmpty) return;
    setState(() => _savingPermissions = true);
    try {
      final updated = await ref
          .read(mcremoteClientProvider)
          .writeCodexPermissions(selectedProfile, selectedReviewer);
      if (!mounted) return;
      setState(() {
        _runtime = updated;
        _savingPermissions = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _savingPermissions = false;
        _error = error.toString();
      });
    }
  }

  Future<void> _chooseProfile(CodexPermissionProfile profile) async {
    if (profile.requiresConfirmation) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('Use dangerous permissions?'),
          content: const Text(
            'This profile can remove sandbox protection. Continue only if you understand the host impact.',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Use profile'),
            ),
          ],
        ),
      );
      if (confirmed != true) return;
    }
    await _writePermissions(profile: profile.id);
  }

  Future<void> _showProfiles() async {
    final profiles =
        _runtime?.permissionProfiles.where((p) => p.allowed).toList() ??
        const <CodexPermissionProfile>[];
    await showModalBottomSheet<void>(
      context: context,
      builder: (context) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          children: [
            for (final profile in profiles)
              ListTile(
                leading: Icon(
                  profile.dangerous
                      ? Icons.warning_amber_rounded
                      : Icons.shield_outlined,
                ),
                title: Text(
                  profile.id == 'auto'
                      ? 'Unattended auto (dangerous)'
                      : profile.id,
                ),
                subtitle: profile.description.isEmpty
                    ? null
                    : Text(profile.description),
                onTap: () {
                  Navigator.pop(context);
                  unawaited(_chooseProfile(profile));
                },
              ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Codex Runtime')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                padding: listBottomPadding(context),
                children: [
                  if (_error != null)
                    ListTile(
                      leading: const Icon(Icons.error_outline),
                      title: const Text('Runtime request failed'),
                      subtitle: Text(_error!),
                    ),
                  if (_runtime case final runtime?) ...[
                    SettingsSection(
                      title: 'Engine',
                      children: [
                        _row('Version', runtime.codexVersion),
                        _row('Transport', runtime.transport),
                        _row('Generation', '${runtime.generation}'),
                        _row('Model', runtime.model),
                      ],
                    ),
                    SettingsSection(
                      title: 'Permissions',
                      children: [
                        ListTile(
                          key: const Key('codex-permission-profile'),
                          leading: const Icon(Icons.shield_outlined),
                          title: const Text('Permission profile'),
                          subtitle: Text(
                            runtime.requestedProfileId ==
                                        runtime.effectiveProfileId ||
                                    runtime.requestedProfileId.isEmpty
                                ? runtime.effectiveProfileId
                                : 'Requested ${runtime.requestedProfileId} · effective ${runtime.effectiveProfileId}',
                          ),
                          trailing: _savingPermissions
                              ? const SizedBox.square(
                                  dimension: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                )
                              : const Icon(Icons.chevron_right),
                          onTap: _savingPermissions ? null : _showProfiles,
                        ),
                        ListTile(
                          key: const Key('codex-approvals-reviewer'),
                          leading: const Icon(Icons.fact_check_outlined),
                          title: const Text('Approval reviewer'),
                          subtitle: Text(
                            runtime.requestedReviewer ==
                                        runtime.effectiveReviewer ||
                                    runtime.requestedReviewer.isEmpty
                                ? runtime.effectiveReviewer
                                : 'Requested ${runtime.requestedReviewer} · effective ${runtime.effectiveReviewer}',
                          ),
                          trailing: PopupMenuButton<String>(
                            enabled: !_savingPermissions,
                            onSelected: (value) =>
                                unawaited(_writePermissions(reviewer: value)),
                            itemBuilder: (_) => const [
                              PopupMenuItem(
                                value: 'user',
                                child: Text('User review'),
                              ),
                              PopupMenuItem(
                                value: 'auto_review',
                                child: Text('Guardian auto-review'),
                              ),
                            ],
                          ),
                        ),
                        if (runtime.policyDetail.isNotEmpty)
                          ListTile(
                            leading: const Icon(Icons.policy_outlined),
                            title: const Text('Managed policy'),
                            subtitle: Text(runtime.policyDetail),
                          ),
                      ],
                    ),
                    SettingsSection(
                      title: 'Account and usage',
                      children: [
                        _row('Plan', runtime.accountPlan),
                        _row(
                          'Context',
                          '${runtime.tokens} / ${runtime.contextWindow} tokens',
                        ),
                        _row(
                          'Primary rate window',
                          '${runtime.primaryRateUsedPercent}% used',
                        ),
                      ],
                    ),
                    if (runtime.mcpServers.isNotEmpty)
                      SettingsSection(
                        title: 'MCP servers',
                        children: [
                          for (final server in runtime.mcpServers)
                            ListTile(
                              leading: const Icon(Icons.extension_outlined),
                              title: Text(server.name),
                              subtitle: Text(
                                server.error.isEmpty
                                    ? server.status
                                    : '${server.status} · ${server.error}',
                              ),
                            ),
                        ],
                      ),
                  ],
                  SettingsSection(
                    title: 'Host diagnostics',
                    children: [
                      ListTile(
                        key: const Key('codex-run-doctor'),
                        leading: const Icon(Icons.health_and_safety_outlined),
                        title: const Text('Run Codex Doctor'),
                        subtitle: const Text(
                          'Read-only, redacted host checks. Nothing is uploaded or repaired.',
                        ),
                        trailing: _diagnosing
                            ? const SizedBox.square(
                                dimension: 20,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : const Icon(Icons.chevron_right),
                        onTap: _diagnosing ? null : _runDoctor,
                      ),
                      if (_doctor case final report?)
                        ListTile(
                          leading: const Icon(Icons.fact_check_outlined),
                          title: Text(
                            'Overall: ${report['overallStatus'] ?? 'unknown'}',
                          ),
                          subtitle: Text(
                            '${(report['checks'] as List?)?.length ?? 0} sanitized checks · schema ${report['schemaVersion'] ?? '?'}',
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
    );
  }

  Widget _row(String label, String value) => ListTile(
    title: Text(label),
    trailing: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 220),
      child: Text(value.isEmpty ? 'Unknown' : value, textAlign: TextAlign.end),
    ),
  );
}
