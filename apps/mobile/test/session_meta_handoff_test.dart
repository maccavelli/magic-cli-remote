import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart'
    show SessionMeta, DeviceInfo;

// SessionMeta ownership/handoff parsing (MADR 0078): the phone tells a
// claimable (released/legacy) session from one it owns purely from the wire
// fields, so the sessions menu can offer Claim vs Hand off correctly.

void main() {
  test('an owned session is not claimable', () {
    final s = SessionMeta.fromJson({
      'id': 's1',
      'provider': 'grok',
      'owner_device_id': 'dev-me',
    });
    expect(s.ownerDeviceId, 'dev-me');
    expect(s.isClaimable, isFalse);
  });

  test('an unowned session (no owner) is claimable', () {
    final s = SessionMeta.fromJson({'id': 's1', 'provider': 'grok'});
    expect(s.ownerDeviceId, isNull);
    expect(s.isClaimable, isTrue);
  });

  test('an empty owner string is claimable', () {
    final s = SessionMeta.fromJson({
      'id': 's1',
      'provider': 'grok',
      'owner_device_id': '',
    });
    expect(s.isClaimable, isTrue);
  });

  test('a targeted release carries pending_handoff_to', () {
    final s = SessionMeta.fromJson({
      'id': 's1',
      'provider': 'grok',
      'owner_device_id': '',
      'pending_handoff_to': 'dev-target',
    });
    expect(s.isClaimable, isTrue);
    expect(s.pendingHandoffTo, 'dev-target');
  });

  test('copyWith preserves ownership and handoff fields', () {
    final s = SessionMeta.fromJson({
      'id': 's1',
      'provider': 'grok',
      'owner_device_id': '',
      'pending_handoff_to': 'dev-target',
    });
    final c = s.copyWith(status: 'working');
    expect(c.ownerDeviceId, '');
    expect(c.pendingHandoffTo, 'dev-target');
    expect(c.isClaimable, isTrue);
  });

  test('DeviceInfo parses self flag', () {
    final me = DeviceInfo.fromJson({
      'device_id': 'dev-me',
      'name': 'My Phone',
      'self': true,
    });
    final other = DeviceInfo.fromJson({
      'device_id': 'dev-laptop',
      'name': 'Laptop',
    });
    expect(me.isSelf, isTrue);
    expect(other.isSelf, isFalse);
    expect(other.name, 'Laptop');
  });
}
