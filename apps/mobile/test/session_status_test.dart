import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/session_status.dart';

void main() {
  test('maps known wire statuses to friendly labels', () {
    expect(humanSessionStatus('running'), 'Working…');
    expect(humanSessionStatus('idle'), 'Idle');
    expect(humanSessionStatus('error'), 'Error');
    expect(humanSessionStatus('disconnected'), 'Disconnected');
    expect(humanSessionStatus('cancelled'), 'Cancelled');
  });

  test('title-cases unknown statuses and blanks show a dash', () {
    expect(humanSessionStatus('paused'), 'Paused');
    expect(humanSessionStatus(''), '—');
  });
}
