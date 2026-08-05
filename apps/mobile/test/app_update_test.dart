import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/update/app_update.dart';

void main() {
  group('AppUpdateService base compare (0065)', () {
    test('parseBase', () {
      expect(AppUpdateService.parseBase('0.6.7'), (0, 6, 7));
      expect(AppUpdateService.parseBase('v0.7.0'), (0, 7, 0));
      expect(AppUpdateService.parseBase('0.6.7.4.gabc')?.$3, 7);
      expect(AppUpdateService.parseBase('dev'), isNull);
    });

    test('isNewerBase', () {
      expect(AppUpdateService.isNewerBase('0.6.8', '0.6.7'), isTrue);
      expect(AppUpdateService.isNewerBase('0.6.7', '0.6.7'), isFalse);
      expect(AppUpdateService.isNewerBase('0.6.6', '0.6.7'), isFalse);
      expect(AppUpdateService.isNewerBase('v0.7.0', '0.6.9.1.gdev'), isTrue);
    });
  });
}
