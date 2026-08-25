import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

void main() {
  test(
    'managed profiles and effective settings parse without policy values',
    () {
      final runtime = CodexRuntimeSnapshot.fromJson({
        'transport': 'stdio',
        'generation': 1,
        'config': {
          'approval_policy': 'on-request',
          'sandbox_mode': 'workspace-write',
          'provenance': 'managed',
          'requested_profile_id': 'team',
          'effective_profile_id': ':workspace',
          'requested_reviewer': 'auto_review',
          'effective_reviewer': 'user',
          'policy_detail': 'Required by managed policy',
        },
        'permission_profiles': [
          {'id': ':workspace', 'allowed': true},
          {
            'id': ':danger-full-access',
            'allowed': false,
            'dangerous': true,
            'description': 'No sandbox',
          },
        ],
      });
      expect(runtime.requestedProfileId, 'team');
      expect(runtime.effectiveProfileId, ':workspace');
      expect(runtime.requestedReviewer, 'auto_review');
      expect(runtime.effectiveReviewer, 'user');
      expect(runtime.permissionProfiles.last.allowed, isFalse);
      expect(runtime.permissionProfiles.last.dangerous, isTrue);
      expect(runtime.policyDetail, 'Required by managed policy');
    },
  );

  test('dangerous permission selection requires explicit confirmation', () {
    const safe = CodexPermissionProfile(id: ':workspace', allowed: true);
    const dangerous = CodexPermissionProfile(
      id: ':danger-full-access',
      allowed: true,
      dangerous: true,
    );
    expect(safe.requiresConfirmation, isFalse);
    expect(dangerous.requiresConfirmation, isTrue);
  });
}
