import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/settings/upstream_catalog_sheet.dart';

/// A catalog stub shaped like the daemon's: server-side filtering and paging,
/// so the widget is exercised against the contract it actually talks to
/// (MADR 0074 D16).
class _FakeCatalog {
  _FakeCatalog(this.all, {this.source = 'engine'});

  final List<UpstreamAuth> all;
  final String source;
  final List<String> queries = [];
  int calls = 0;

  Future<ProviderAuthCatalog?> fetch({
    required String providerId,
    String query = '',
    int offset = 0,
    int limit = 0,
  }) async {
    calls++;
    queries.add(query);
    final matched = query.isEmpty
        ? all
        : all
              .where(
                (u) =>
                    u.id.toLowerCase().contains(query.toLowerCase()) ||
                    u.display.toLowerCase().contains(query.toLowerCase()),
              )
              .toList();
    final page = limit <= 0 ? 100 : limit;
    final start = offset.clamp(0, matched.length);
    final end = (start + page).clamp(0, matched.length);
    return ProviderAuthCatalog(
      providerId: providerId,
      upstreams: matched.sublist(start, end),
      offset: start,
      total: matched.length,
      truncated: end < matched.length,
      source: source,
    );
  }
}

UpstreamAuth _vendor(String id, String label, {String type = 'api_key'}) =>
    UpstreamAuth(
      id: id,
      label: label,
      status: 'missing',
      methods: [AuthMethod(id: '$id:0', type: type, label: 'API key')],
    );

Future<void> _pump(
  WidgetTester tester,
  _FakeCatalog fake, {
  List<UpstreamAuth> configured = const [],
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: UpstreamCatalogSheet(
          providerId: 'opencode',
          fetch: fake.fetch,
          configured: configured,
          pageSize: 100,
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('lists vendors that have no credential yet', (tester) async {
    final fake = _FakeCatalog([
      _vendor('togetherai', 'Together AI'),
      _vendor('deepseek', 'DeepSeek'),
    ]);
    await _pump(tester, fake);

    expect(find.byKey(const Key('upstream-catalog-row-togetherai')), findsOne);
    expect(find.text('Together AI'), findsOne);
  });

  testWidgets('search narrows the list through the daemon', (tester) async {
    final fake = _FakeCatalog([
      _vendor('togetherai', 'Together AI'),
      _vendor('deepseek', 'DeepSeek'),
    ]);
    await _pump(tester, fake);

    await tester.enterText(
      find.byKey(const Key('upstream-catalog-search')),
      'together',
    );
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pumpAndSettle();

    expect(fake.queries.contains('together'), isTrue);
    expect(
      find.byKey(const Key('upstream-catalog-row-deepseek')),
      findsNothing,
    );
    expect(find.byKey(const Key('upstream-catalog-row-togetherai')), findsOne);
  });

  testWidgets('a partial list says how much it is showing', (tester) async {
    final fake = _FakeCatalog([
      for (var i = 0; i < 150; i++) _vendor('vendor-$i', 'Vendor $i'),
    ]);
    await _pump(tester, fake);

    final subtitle = tester.widget<Text>(
      find.byKey(const Key('upstream-catalog-subtitle')),
    );
    expect(subtitle.data, contains('showing 100 of 150'));
  });

  testWidgets('a pinned catalog is labelled as pinned', (tester) async {
    final fake = _FakeCatalog([
      _vendor('together', 'Together AI'),
    ], source: 'static');
    await _pump(tester, fake);

    final subtitle = tester.widget<Text>(
      find.byKey(const Key('upstream-catalog-subtitle')),
    );
    expect(subtitle.data, contains('pinned'));
  });

  testWidgets('browser-only vendors are not tappable', (tester) async {
    final fake = _FakeCatalog([
      _vendor('gitlab', 'GitLab', type: 'oauth_browser'),
    ]);
    await _pump(tester, fake);

    final tile = tester.widget<ListTile>(
      find.byKey(const Key('upstream-catalog-row-gitlab')),
    );
    expect(tile.enabled, isFalse);
    expect(tile.onTap, isNull);
  });

  testWidgets('an agent with no catalog says so instead of erroring', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: UpstreamCatalogSheet(
            providerId: 'codex',
            fetch:
                ({
                  required String providerId,
                  String query = '',
                  int offset = 0,
                  int limit = 0,
                }) async => null,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('upstream-catalog-empty')), findsOne);
  });

  testWidgets('rows describe methods as chips, not by echoing the id', (
    tester,
  ) async {
    final fake = _FakeCatalog([
      _vendor('togetherai', 'Together AI'),
      _vendor('kilo', 'Kilo Gateway', type: 'oauth_device'),
    ]);
    await _pump(tester, fake);

    expect(find.text('API key'), findsOneWidget);
    expect(find.text('Device code'), findsOneWidget);
    // The old subtitle repeated the raw id under the display name.
    expect(find.text('togetherai'), findsNothing);
    // Each row carries its vendor identity (MADR 0082 D5) — togetherai has a
    // bundled brand icon, and an id with none falls back to a monogram
    // rather than a blank.
    expect(find.byKey(const Key('vendor-icon-togetherai')), findsOneWidget);
  });

  testWidgets('a browser-only row is labelled Host only', (tester) async {
    final fake = _FakeCatalog([
      _vendor('gitlab', 'GitLab', type: 'oauth_browser'),
    ]);
    await _pump(tester, fake);

    expect(find.text('Host only'), findsOneWidget);
    expect(find.text('API key'), findsNothing);
  });

  testWidgets('the browse view is banded: Configured, Popular, All', (
    tester,
  ) async {
    final fake = _FakeCatalog([
      _vendor('togetherai', 'Together AI'),
      _vendor('deepseek', 'DeepSeek'),
      _vendor('zzz-obscure', 'Obscure Vendor'),
    ]);
    await _pump(
      tester,
      fake,
      configured: const [
        UpstreamAuth(
          id: 'togetherai',
          label: 'Together AI',
          status: 'configured',
        ),
      ],
    );

    final bands = ['configured', 'popular', 'all-vendors'];
    final ys = [
      for (final b in bands)
        tester.getTopLeft(find.byKey(Key('upstream-catalog-band-$b'))).dy,
    ];
    expect(ys[0] < ys[1] && ys[1] < ys[2], isTrue);
    // The configured vendor is not duplicated into the paged rows, and its
    // row keeps the status chip ('Configured' = band header + chip label).
    expect(
      find.byKey(const Key('upstream-catalog-row-togetherai')),
      findsOneWidget,
    );
    expect(find.text('Configured'), findsNWidgets(2));
  });

  testWidgets('searching collapses the bands', (tester) async {
    final fake = _FakeCatalog([
      _vendor('togetherai', 'Together AI'),
      _vendor('deepseek', 'DeepSeek'),
    ]);
    await _pump(
      tester,
      fake,
      configured: const [
        UpstreamAuth(
          id: 'togetherai',
          label: 'Together AI',
          status: 'configured',
        ),
      ],
    );

    await tester.enterText(
      find.byKey(const Key('upstream-catalog-search')),
      'deep',
    );
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('upstream-catalog-band-configured')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('upstream-catalog-band-popular')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('upstream-catalog-row-deepseek')),
      findsOneWidget,
    );
  });

  testWidgets('the sheet list clears a simulated gesture-nav inset', (
    tester,
  ) async {
    // MADR 0083 L5: 0082 P4 dropped the sheet's bottom SafeArea; this pins
    // its restoration — the rows must render inside a bottom SafeArea that
    // consumes the faked inset.
    tester.view.viewPadding = FakeViewPadding(bottom: 96);
    addTearDown(tester.view.resetViewPadding);
    final fake = _FakeCatalog([
      for (var i = 0; i < 30; i++) _vendor('v$i', 'Vendor $i'),
    ]);
    await _pump(tester, fake);

    final safeAreas = find.ancestor(
      of: find.byKey(const Key('upstream-catalog-row-v0')),
      matching: find.byType(SafeArea),
    );
    expect(safeAreas, findsWidgets);
    expect(tester.widgetList<SafeArea>(safeAreas).first.bottom, isTrue);
  });

  testWidgets('picking a vendor returns it to the caller', (tester) async {
    final fake = _FakeCatalog([_vendor('togetherai', 'Together AI')]);
    UpstreamAuth? picked;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () async {
                picked = await showModalBottomSheet<UpstreamAuth>(
                  context: context,
                  isScrollControlled: true,
                  builder: (_) => UpstreamCatalogSheet(
                    providerId: 'opencode',
                    fetch: fake.fetch,
                  ),
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('upstream-catalog-row-togetherai')));
    await tester.pumpAndSettle();

    expect(picked?.id, 'togetherai');
  });
}
