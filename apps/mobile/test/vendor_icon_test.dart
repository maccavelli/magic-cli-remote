import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/widgets/vendor_icon.dart';

Future<void> _pump(WidgetTester tester, Widget child) => tester.pumpWidget(
  MaterialApp(
    home: Scaffold(body: Center(child: child)),
  ),
);

void main() {
  testWidgets('a manifest id renders its bundled brand SVG', (tester) async {
    // `openai` is guaranteed by the committed asset set: sync.sh matches it
    // by identity against the icon package.
    await _pump(tester, const VendorIcon(id: 'openai'));

    expect(find.byKey(const Key('vendor-icon-openai')), findsOneWidget);
    expect(find.byType(SvgPicture), findsOneWidget);
    expect(find.byKey(const Key('vendor-icon-monogram-openai')), findsNothing);
  });

  testWidgets('an unknown id renders a deterministic monogram', (tester) async {
    await _pump(
      tester,
      const VendorIcon(id: 'no-such-vendor', display: 'No Such Vendor'),
    );

    expect(find.byKey(const Key('vendor-icon-no-such-vendor')), findsOneWidget);
    expect(
      find.byKey(const Key('vendor-icon-monogram-no-such-vendor')),
      findsOneWidget,
    );
    expect(find.byType(SvgPicture), findsNothing);
    expect(find.text('NO'), findsOneWidget);
  });

  testWidgets('the monogram colour is a stable function of the id', (
    tester,
  ) async {
    await _pump(tester, const VendorIcon(id: 'stable-vendor'));
    final first = tester
        .widget<CircleAvatar>(find.byType(CircleAvatar))
        .backgroundColor;

    await _pump(tester, const VendorIcon(id: 'stable-vendor'));
    final second = tester
        .widget<CircleAvatar>(find.byType(CircleAvatar))
        .backgroundColor;

    expect(first, second);
  });
}
