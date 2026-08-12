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

Future<void> _pump(WidgetTester tester, _FakeCatalog fake) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: UpstreamCatalogSheet(
          providerId: 'opencode',
          fetch: fake.fetch,
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
