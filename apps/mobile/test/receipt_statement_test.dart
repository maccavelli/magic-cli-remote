import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

// The phone-side refuse-to-sign rules (MADR 0077 D2/P7): a compromised or
// buggy daemon must not be able to get this device's key onto an unrelated
// statement. Mirrors the daemon's own guards from the other direction — the
// daemon verifies what came back, the phone vets what it's asked to sign.
Map<String, dynamic> _validStatement({String device = 'dev-1'}) => {
  '_type': 'https://mcremote.dev/attestations/receipt/v1',
  'subject': [
    {
      'name': 'session:s1/permission:p1',
      'digest': {'sha256': 'ab'},
    },
  ],
  'predicateType': kPermissionDecisionPredicateType,
  'predicate': {'device_id': device, 'option_id': 'once'},
  'chain': {'scope': 'device:$device', 'prev_sha256': null},
};

void main() {
  test('accepts a well-formed statement naming this device', () {
    expect(receiptStatementRefusalReason(_validStatement(), 'dev-1'), isNull);
  });

  test('refuses an empty or missing subject', () {
    final s = _validStatement()..['subject'] = <dynamic>[];
    expect(receiptStatementRefusalReason(s, 'dev-1'), contains('subject'));
    final s2 = _validStatement()..remove('subject');
    expect(receiptStatementRefusalReason(s2, 'dev-1'), contains('subject'));
  });

  test('refuses an unknown predicateType', () {
    final s = _validStatement()
      ..['predicateType'] =
          'https://mcremote.dev/attestations/session-handoff/v1';
    expect(
      receiptStatementRefusalReason(s, 'dev-1'),
      contains('predicateType'),
    );
  });

  test('refuses a missing chain', () {
    final s = _validStatement()..remove('chain');
    expect(receiptStatementRefusalReason(s, 'dev-1'), contains('chain'));
  });

  test('refuses when this client has no device id yet', () {
    expect(
      receiptStatementRefusalReason(_validStatement(), null),
      contains('device id'),
    );
    expect(
      receiptStatementRefusalReason(_validStatement(), ''),
      contains('device id'),
    );
  });

  test('refuses a chain.scope naming a DIFFERENT device', () {
    // The core defense: a daemon asking this device to sign into another
    // device's chain is asking it to forge someone else's history.
    final s = _validStatement(device: 'dev-other');
    expect(
      receiptStatementRefusalReason(s, 'dev-1'),
      contains('does not name this device'),
    );
  });

  test('refuses a scope that merely contains the device id', () {
    final s = _validStatement()
      ..['chain'] = {'scope': 'device:dev-1-evil', 'prev_sha256': null};
    expect(
      receiptStatementRefusalReason(s, 'dev-1'),
      contains('does not name this device'),
    );
  });
}
