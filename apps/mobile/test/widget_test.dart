import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:magic_cli_remote/app.dart';

void main() {
  testWidgets('Connect screen loads', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: MagicCliRemoteApp()));
    await tester.pumpAndSettle();
    expect(find.text('Magic CLI Remote'), findsOneWidget);
    expect(find.text('Connect'), findsOneWidget);
  });
}
