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

/// One selectable reasoning/thinking setting for a model (MADR 0052).
///
/// Never a client-invented ladder: the daemon passes through what the provider
/// advertised. [id] is the wire value sent back as `thinking_level` /
/// `/thinking`; empty means the row is unusable and is dropped on parse.
class ThinkingLevel {
  const ThinkingLevel({
    required this.id,
    this.label = '',
    this.description = '',
    this.isDefault = false,
  });

  /// Wire value (`low`, `xhigh`, …).
  final String id;

  /// Provider display name when supplied (grok: "High Effort"); empty → show [id].
  final String label;

  /// Provider prose for the rung.
  final String description;

  /// True when this is the provider's own default for the model.
  final bool isDefault;

  String get displayLabel => label.isNotEmpty ? label : id;

  factory ThinkingLevel.fromJson(Map<String, dynamic> json) {
    return ThinkingLevel(
      id: json['id'] as String? ?? '',
      label: json['label'] as String? ?? '',
      description: json['description'] as String? ?? '',
      // JSON key is `default`; Dart reserves that word.
      isDefault: json['default'] as bool? ?? false,
    );
  }
}

/// Global settings intents the user may store as "Default thinking level".
/// Restricted to names every measured ladder shares so a background preference
/// can never select `xhigh` / `max` / `ultra` (MADR 0052 D3).
const kThinkingLevelIntents = {'low', 'medium', 'high'};

/// Resolve the user's global intent against one model's advertised ladder.
///
/// Exact name match only, then the model's own default, then nothing.
/// Deliberately NOT rank interpolation: on a six-rung model that would map
/// "High" onto `ultra`, silently escalating cost (MADR 0052 D3).
String? resolveThinkingLevel(List<ThinkingLevel> levels, String? intent) {
  if (levels.isEmpty) return null;
  final want = intent?.trim() ?? '';
  if (want.isEmpty) return null;
  for (final l in levels) {
    if (l.id == want) return l.id;
  }
  for (final l in levels) {
    if (l.isDefault) return l.id;
  }
  return null;
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
    this.thinkingLevels = const [],
  });

  final String id;
  final String label;
  final String description;
  final String group;
  final bool enabled;
  final Map<String, String> meta;

  /// Reasoning/thinking settings this model accepts, cheapest-first.
  /// Empty means no selectable level (opencode, goose, some models).
  final List<ThinkingLevel> thinkingLevels;

  String get displayLabel => label.isNotEmpty ? label : id;

  /// Engine lifecycle status, e.g. `deprecated`. The daemon ranks deprecated
  /// models last; the row badges them so the ranking is visible rather than
  /// merely felt.
  String get status => meta['status'] ?? '';

  bool get isDeprecated => status.toLowerCase() == 'deprecated';

  /// Context window as the daemon rendered it ("200K"), or empty.
  String get contextWindow => meta['context'] ?? '';

  /// Number of models reported for a model-provider row, or empty.
  String get modelCount => meta['model_count'] ?? '';

  /// Provider-qualified default model for a model-provider row, or empty.
  String get defaultModel => meta['default_model'] ?? '';

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
    final levels = <ThinkingLevel>[];
    final levelsRaw = json['thinking_levels'];
    if (levelsRaw is List) {
      for (final e in levelsRaw) {
        if (e is Map<String, dynamic>) {
          final l = ThinkingLevel.fromJson(e);
          if (l.id.isNotEmpty) levels.add(l);
        } else if (e is Map) {
          final l = ThinkingLevel.fromJson(Map<String, dynamic>.from(e));
          if (l.id.isNotEmpty) levels.add(l);
        }
      }
    }
    return PickerOption(
      id: json['id'] as String? ?? '',
      label: json['label'] as String? ?? '',
      description: json['description'] as String? ?? '',
      group: json['group'] as String? ?? '',
      enabled: enabled,
      meta: meta,
      thinkingLevels: levels,
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
