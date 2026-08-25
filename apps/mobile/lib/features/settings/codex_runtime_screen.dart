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
