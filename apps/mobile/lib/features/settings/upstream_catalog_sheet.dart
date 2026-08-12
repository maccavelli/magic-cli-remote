/// Browse every vendor an agent can be authenticated against (MADR 0074 D16).
///
/// The provider list on the settings screen shows what is *configured*. That
/// is the right default — it is short, and it is what a returning user cares
/// about — but on its own it left the ~185 vendors OpenCode and Kilo support
/// unreachable from the phone: if togetherai had no credential yet, no row
/// existed to tap. This sheet is the other half: a searchable list of what
/// *could* be configured, fetched on demand and paged, because sending it with
/// every provider listing would put tens of kilobytes on the wire each time a
/// chip changed colour.
library;

import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/protocol/models.dart';
import '../widgets/status_chip.dart';
import '../widgets/vendor_icon.dart';
import 'provider_auth_sheet.dart' show bottomInsetFor;

/// Fetches one page of the catalog. Injected so the sheet can be tested
/// without a daemon.
typedef UpstreamCatalogFetcher =
    Future<ProviderAuthCatalog?> Function({
      required String providerId,
      String query,
      int offset,
      int limit,
    });

class UpstreamCatalogSheet extends StatefulWidget {
  const UpstreamCatalogSheet({
    super.key,
    required this.providerId,
    required this.fetch,
    this.pageSize = 100,
  });

  final String providerId;
  final UpstreamCatalogFetcher fetch;
  final int pageSize;

  @override
  State<UpstreamCatalogSheet> createState() => _UpstreamCatalogSheetState();
}

class _UpstreamCatalogSheetState extends State<UpstreamCatalogSheet> {
  final _search = TextEditingController();
  final _scroll = ScrollController();

  final List<UpstreamAuth> _rows = [];
  int _total = 0;
  bool _more = false;
  bool _loading = false;
  bool _isStatic = false;
  String? _error;

  /// Debounce so typing "together" is one request rather than eight.
  Timer? _debounce;

  /// Guards against an in-flight response for an older query overwriting a
  /// newer one — the classic search race.
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    _load(reset: true);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!_scroll.hasClients || _loading || !_more) return;
    final position = _scroll.position;
    if (position.pixels >= position.maxScrollExtent - 320) {
      _load();
    }
  }

  void _onQueryChanged(String _) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 250), () {
      _load(reset: true);
    });
  }

  Future<void> _load({bool reset = false}) async {
    if (_loading) return;
    final generation = reset ? ++_generation : _generation;
    setState(() {
      _loading = true;
      if (reset) _error = null;
    });
    try {
      final page = await widget.fetch(
        providerId: widget.providerId,
        query: _search.text.trim(),
        offset: reset ? 0 : _rows.length,
        limit: widget.pageSize,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        if (reset) _rows.clear();
        if (page == null) {
          // No catalog for this agent — codex and grok each have exactly one
          // upstream, so there is nothing to browse.
          _more = false;
          _total = 0;
        } else {
          _rows.addAll(page.upstreams);
          _total = page.total;
          _more = page.truncated;
          _isStatic = page.isStatic;
        }
        _loading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = '$e';
        _loading = false;
        _more = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final mq = MediaQuery.of(context);
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(bottom: bottomInsetFor(mq)),
      child: SafeArea(
        top: false,
        child: SizedBox(
          height: mq.size.height * 0.8,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        'Add credential · ${widget.providerId}',
                        style: theme.textTheme.titleMedium,
                      ),
                    ),
                    IconButton(
                      key: const Key('upstream-catalog-close'),
                      icon: const Icon(Icons.close),
                      onPressed: () => Navigator.of(context).pop(),
                    ),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: TextField(
                  key: const Key('upstream-catalog-search'),
                  controller: _search,
                  autocorrect: false,
                  enableSuggestions: false,
                  onChanged: _onQueryChanged,
                  decoration: const InputDecoration(
                    prefixIcon: Icon(Icons.search),
                    hintText: 'Search vendors (together, deepseek, xai…)',
                  ),
                ),
              ),
              _subtitle(theme),
              const Divider(height: 1),
              Expanded(child: _body(theme)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _subtitle(ThemeData theme) {
    final parts = <String>[];
    if (_total > 0) {
      parts.add(
        _rows.length < _total
            ? 'showing ${_rows.length} of $_total'
            : '$_total vendors',
      );
    }
    // A pinned table can be older than the agent it describes; saying so is
    // cheaper than a support question about a missing vendor.
    if (_isStatic) parts.add('list pinned to a known CLI version');
    if (parts.isEmpty) return const SizedBox(height: 8);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      child: Align(
        alignment: Alignment.centerLeft,
        child: Text(
          parts.join(' · '),
          key: const Key('upstream-catalog-subtitle'),
          style: theme.textTheme.bodySmall,
        ),
      ),
    );
  }

  Widget _body(ThemeData theme) {
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            _error!,
            key: const Key('upstream-catalog-error'),
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium,
          ),
        ),
      );
    }
    if (_rows.isEmpty) {
      if (_loading) {
        return const Center(child: CircularProgressIndicator());
      }
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            _search.text.trim().isEmpty
                ? 'This agent has no vendor catalog to browse.'
                : 'No vendor matches "${_search.text.trim()}".',
            key: const Key('upstream-catalog-empty'),
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium,
          ),
        ),
      );
    }
    return ListView.builder(
      controller: _scroll,
      itemCount: _rows.length + (_more ? 1 : 0),
      itemBuilder: (context, i) {
        if (i >= _rows.length) {
          return const Padding(
            padding: EdgeInsets.all(16),
            child: Center(child: CircularProgressIndicator()),
          );
        }
        final up = _rows[i];
        final browserOnly =
            up.methods.isNotEmpty && up.methods.every((m) => m.isBrowserOAuth);
        return ListTile(
          key: Key('upstream-catalog-row-${up.id}'),
          leading: VendorIcon(id: up.id, display: up.display, size: 28),
          title: Text(up.display),
          subtitle: _rowSubtitle(up, browserOnly),
          trailing: up.isConfigured
              ? const Icon(Icons.verified_user_outlined)
              : const Icon(Icons.chevron_right),
          // A vendor whose only method needs a browser callback to the host's
          // loopback cannot be set up from here at all, so the row does not
          // pretend otherwise (D7, and the loopback workstream that follows).
          enabled: !browserOnly,
          onTap: browserOnly ? null : () => Navigator.of(context).pop(up),
        );
      },
    );
  }

  /// Method chips instead of prose (MADR 0082 D7-subtitles): what a row
  /// offers is a *set of ways in*, and the old text repeated the raw id under
  /// the display name ("Together AI / togetherai"), which told the user
  /// nothing.
  Widget _rowSubtitle(UpstreamAuth up, bool browserOnly) {
    final chips = <Widget>[
      if (up.isConfigured) StatusChip.auth(up.status),
      if (browserOnly)
        const StatusChip(kind: StatusKind.neutral, label: 'Host only')
      else ...[
        if (up.methods.any((m) => m.isApiKey))
          const StatusChip(kind: StatusKind.neutral, label: 'API key'),
        if (up.methods.any((m) => m.isDeviceOAuth))
          const StatusChip(kind: StatusKind.neutral, label: 'Device code'),
      ],
    ];
    if (chips.isEmpty) return Text(up.display);
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: Wrap(spacing: 6, runSpacing: 4, children: chips),
    );
  }
}
