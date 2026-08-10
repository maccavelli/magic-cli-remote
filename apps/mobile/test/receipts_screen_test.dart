import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:magic_cli_remote/data/ws/receipts.dart';
import 'package:magic_cli_remote/features/settings/receipts_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

// The receipts screen shows this device's chain with a LOCALLY-recomputed
// ✓/✗/⚠ badge (MADR 0078 D7/D9) — the verdict is provided by the client's
// verifyChainEntry, and the screen renders it faithfully.

class _MockReceiptsClient extends McremoteClient {
  _MockReceiptsClient(this.entries);

  final List<ReceiptEntry> entries;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<ReceiptEntry>> listReceipts() async => entries;
}

ReceiptEntry _entry(
  String predicateType,
  ReceiptVerdict verdict, {
  String? to,
}) {
  final predicate = <String, dynamic>{
    'tool_name': 'bash',
    'option_id': 'once',
    'detail': 'echo hi',
  };
  if (to != null) predicate['to_device_id'] = to;
  return ReceiptEntry(
    jws: 'h.p.s',
    statement: {
      'subject': [
        {'name': 'session:s1/x'},
      ],
      'predicateType': predicateType,
      'predicate': predicate,
      'chain': {'scope': 'device:d', 'prev_sha256': null},
    },
    verdict: verdict,
  );
}

Widget _wrap(McremoteClient client) {
  return ProviderScope(
    overrides: [mcremoteClientProvider.overrideWithValue(client)],
    child: const MaterialApp(home: ReceiptsScreen()),
  );
}

void main() {
  testWidgets('renders verified / failed / unverifiable badges', (
    tester,
  ) async {
    final client = _MockReceiptsClient([
      _entry(kPermissionDecisionPredicate, ReceiptVerdict.verified),
      _entry(kPermissionDecisionPredicate, ReceiptVerdict.failed),
      _entry(kReceiptUnavailablePredicate, ReceiptVerdict.unverifiable),
    ]);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    // One of each badge icon, keyed to the verdict.
    expect(find.byIcon(Icons.verified), findsOneWidget);
    expect(find.byIcon(Icons.gpp_bad), findsOneWidget);
    expect(find.byIcon(Icons.help_outline), findsOneWidget);
    // The verified entry's human label is present (part of a multi-line
    // subtitle, so match on substring).
    expect(
      find.textContaining('Signature verified on this device'),
      findsOneWidget,
    );
  });

  testWidgets('renders a handoff-release entry with its target', (
    tester,
  ) async {
    final client = _MockReceiptsClient([
      _entry(kHandoffReleasePredicate, ReceiptVerdict.verified, to: 'dev-2'),
    ]);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    expect(find.text('Session released → dev-2'), findsOneWidget);
  });

  testWidgets('empty chain shows the daemon-opt-in explanation', (
    tester,
  ) async {
    final client = _MockReceiptsClient(const []);
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();

    expect(find.text('No receipts yet'), findsOneWidget);
    expect(find.textContaining('daemon opt-in'), findsOneWidget);
  });
}
