/// Shared interactive picker catalog (mirrors Go [internal/picker]).
library;

/// How many options the client may select.
enum PickerKind {
  single,
  multi;

  static PickerKind parse(String? raw) {
    switch ((raw ?? '').toLowerCase()) {
      case 'multi':
        return PickerKind.multi;
      default:
        return PickerKind.single;
    }
  }
}

/// Where the catalog came from.
enum PickerSource {
  live,
  staticSource,
  merged,
  unknown;

  static PickerSource parse(String? raw) {
    switch ((raw ?? '').toLowerCase()) {
      case 'live':
        return PickerSource.live;
      case 'static':
        return PickerSource.staticSource;
      case 'merged':
        return PickerSource.merged;
      default:
        return PickerSource.unknown;
    }
  }

  String get label {
    switch (this) {
      case PickerSource.live:
        return 'Live catalog';
      case PickerSource.staticSource:
        return 'Offline catalog';
      case PickerSource.merged:
        return 'Live + offline';
      case PickerSource.unknown:
        return '';
    }
  }
}

/// One selectable row.
class PickerOption {
  PickerOption({
    required this.id,
    this.label = '',
    this.description = '',
    this.group = '',
    this.enabled = true,
    this.meta = const {},
  });

  final String id;
  final String label;
  final String description;
  final String group;
  final bool enabled;
  final Map<String, String> meta;

  String get displayLabel => label.isNotEmpty ? label : id;

  /// Engine lifecycle status, e.g. `deprecated`. The daemon ranks deprecated
  /// models last; the row badges them so the ranking is visible rather than
  /// merely felt.
  String get status => meta['status'] ?? '';

  bool get isDeprecated => status.toLowerCase() == 'deprecated';

  /// Context window as the daemon rendered it ("200K"), or empty.
  String get contextWindow => meta['context'] ?? '';

  /// For a model-provider row: whether the host has credentials configured.
  /// Absent means "not applicable" — only provider rows carry it.
  bool? get connected {
    final raw = meta['connected'];
    if (raw == null) return null;
    return raw.toLowerCase() == 'true';
  }

  /// Lowercased haystack for search. Precomputed once per option rather than
  /// rebuilt per keystroke: a provider catalog is 172 rows and a model catalog
  /// can be hundreds.
  late final String searchText = [
    id,
    label,
    description,
    group,
  ].join(' ').toLowerCase();

  factory PickerOption.fromJson(Map<String, dynamic> json) {
    final enabledRaw = json['enabled'];
    // Omitted enabled → true (matches Go pointer semantics).
    final enabled = enabledRaw is bool ? enabledRaw : true;
    final metaRaw = json['meta'];
    final meta = <String, String>{};
    if (metaRaw is Map) {
      metaRaw.forEach((k, v) {
        if (k != null) meta['$k'] = '$v';
      });
    }
    return PickerOption(
      id: json['id'] as String? ?? '',
      label: json['label'] as String? ?? '',
      description: json['description'] as String? ?? '',
      group: json['group'] as String? ?? '',
      enabled: enabled,
      meta: meta,
    );
  }
}

/// Full picker payload for one surface (e.g. models for a provider).
class PickerCatalog {
  PickerCatalog({
    this.kind = PickerKind.single,
    this.source = PickerSource.unknown,
    this.options = const [],
    this.defaultIds = const [],
    this.allowCustom = false,
    this.minSelect = 0,
    this.maxSelect = 1,
    this.provider = '',
    this.modelProvider = '',
    this.truncated = false,
  });

  final PickerKind kind;
  final PickerSource source;
  final List<PickerOption> options;
  final List<String> defaultIds;
  final bool allowCustom;
  final int minSelect;
  final int maxSelect;

  /// Optional scope label (e.g. provider id for models.list_result).
  final String provider;

  /// The model provider the daemon actually scoped to, which is not always the
  /// one requested — a session-scoped request resolves it from the session.
  final String modelProvider;

  /// The daemon dropped options to stay inside the frame budget. Never silent:
  /// a catalog that quietly loses rows reads as "my model does not exist".
  final bool truncated;

  bool get isMulti => kind == PickerKind.multi;

  factory PickerCatalog.fromJson(Map<String, dynamic> json) {
    final optsRaw = json['options'];
    final options = <PickerOption>[];
    if (optsRaw is List) {
      for (final e in optsRaw) {
        if (e is Map<String, dynamic>) {
          options.add(PickerOption.fromJson(e));
        } else if (e is Map) {
          options.add(PickerOption.fromJson(Map<String, dynamic>.from(e)));
        }
      }
    }
    final defRaw = json['default_ids'];
    final defaults = <String>[];
    if (defRaw is List) {
      for (final e in defRaw) {
        if (e != null && '$e'.isNotEmpty) defaults.add('$e');
      }
    }
    final kind = PickerKind.parse(json['kind'] as String?);
    var maxSelect = (json['max_select'] as num?)?.toInt() ?? 0;
    if (kind == PickerKind.single) {
      maxSelect = 1;
    }
    return PickerCatalog(
      kind: kind,
      source: PickerSource.parse(json['source'] as String?),
      options: options,
      defaultIds: defaults,
      allowCustom: json['allow_custom'] as bool? ?? false,
      minSelect: (json['min_select'] as num?)?.toInt() ?? 0,
      maxSelect: maxSelect,
      provider: json['provider'] as String? ?? '',
      modelProvider: json['model_provider'] as String? ?? '',
      truncated: json['truncated'] as bool? ?? false,
    );
  }
}
