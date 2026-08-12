<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# MADR 0082 — Restructure settings into a hub with a graphically rich provider area

| field | value |
| --- | --- |
| status | **proposed** 2026-08-12, for review. No code has been changed for this record. |
| related | MADR 0074 (provider credentials from the phone), MADR 0079 (drill-down model picker and its reusable picker primitives), MADR 0073 (active-upstream escape), MADR 0062 (transport routes), MADR 0064 (connect mode/token), MADR 0066 (identity/pin diagnostics), `docs/standards/mobile/flutter.md` |
| evidence | Current code: `apps/mobile/lib/features/settings/settings_screen.dart`, `upstream_catalog_sheet.dart`, `provider_auth_sheet.dart`, `device_flow_sheet.dart`, `apps/mobile/lib/features/widgets/option_picker_sheet.dart`, `apps/mobile/lib/theme/celestial.dart`; external sources in §4 |
| plan | [0082-PLAN-settings-provider-menu-ux-overhaul.md](0082-PLAN-settings-provider-menu-ux-overhaul.md) (proposed, drafted 2026-08-12 for joint review) |

## Context and Problem Statement

The settings screen has grown by accretion across nine MADRs (0052, 0062,
0064, 0065, 0066, 0073, 0074, 0078, 0079). It is now one flat `ListView` of
roughly ten `Divider`-separated sections — Appearance, Notifications,
Sessions, Provider credentials, Working directories, Storage, Connection,
Host, About (`settings_screen.dart:850-1184`) — every row rendered as a
default `ListTile` with a Material symbol on the left and prose underneath.
Each individual addition was reasonable; the sum is a long, visually uniform
scroll in which high-frequency items (provider credentials, default modes)
sit between one-time diagnostics (secret storage failures, SPKI
fingerprints).

The provider area carries the most data and suffers the most. After
MADR 0074, one connected host with all five agents can legitimately show:

* an *Active upstream* row per agent with ≥2 configured upstreams
  (`settings_screen.dart:1230-1241`);
* an *Add credential* row per agent (`:1246-1252`);
* one row per configured upstream per agent, each with a status icon, an
  `· active` suffix, and a trailing remove button (`:1253-1272`);
* behind *Add credential*, a paged catalog of up to **185 vendors** (kilo),
  **184** (opencode) or **73** (goose), rendered as undifferentiated
  text-only `ListTile`s (`upstream_catalog_sheet.dart:259-271`);
* behind each vendor, a method/form sheet with up to four dynamic inputs
  (`provider_auth_sheet.dart`).

Agent identity is conveyed only by a title suffix (`'Add credential ·
${p.id}'`), vendor identity only by a display string. Nothing in the
provider area is visually distinct from, say, the transcript-cache tile.

The task is to decide how the settings screen — and especially the provider
menus — should present this volume of data in a human-friendly, ergonomic,
graphically rich way, without regressing any of the recorded behaviours
(D6 invisibility on old daemons, D8 destructive confirmation, D11 secret
hygiene, D14 switch-only-to-configured, D16 catalog paging).

### Findings from the code assessment

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| F1 | One flat scroll, ~35+ tiles before the first provider row on a typical host; no in-page navigation, no settings search, no subpages except Receipts. | `settings_screen.dart:850-1184` | high |
| F2 | Provider grouping is textual, not visual: rows repeat `· ${p.id}` suffixes instead of being contained under an agent identity. Five agents ⇒ the same three-row pattern five times. | `:1225-1273` | high |
| F3 | No brand identity anywhere: agents and all ~185 vendors share four generic Material icons (`tune`, `add_circle_outline`, `verified_user_outlined`, `key_off_outlined`). The catalog is a wall of typographically identical rows. | `:1277-1282`, `upstream_catalog_sheet.dart:259-271` | high |
| F4 | Status carries no colour semantics. `Configured`, `Quota reached`, `Error`, `Needs setup` differ only by glyph and label; the theme's `CelestialColors.success/caution/running` tokens (`celestial.dart:62-86`) are unused in settings. The route section's probe chips (`:501-526`) already do this better. | `settings_screen.dart:1284-1292` | medium |
| F5 | **Removing a credential is one tap with no confirmation.** The trailing `link_off` button calls `_clearCredential` directly (`:1264-1270` → `:1503-1520`). Every other destructive action on the screen (re-pair, clear credentials, clear cache, codex device flow) confirms first. | `settings_screen.dart` | high |
| F6 | Three different picker idioms coexist on one screen: `SimpleDialog` (default mode `:660-707`, thinking level `:724-742`, connect mode `:316-344`), an ad-hoc bottom-sheet column (active upstream `:1303-1321`), and the bespoke `UpstreamCatalogSheet`. Meanwhile MADR 0079 already extracted `PickerSheetLayout` / `PickerSheetHeader` / `PickerCatalogView` — search, groups, badges, O(1) rows, truncation footer (`option_picker_sheet.dart:20-190`) — and settings uses none of it. | both files | medium |
| F7 | The catalog sheet has search but no structure: no "already configured" band, no popular band, and its subtitle degenerates to repeating the row id (`_rowSubtitle` returns `up.id` in two of four branches, so "Together AI / togetherai"). | `upstream_catalog_sheet.dart:276-283` | medium |
| F8 | Density and information parity: `Active upstream` and `Default mode` for the same agent are in *different sections* (Sessions vs Provider credentials), so an agent's session-affecting state is split across the page. | `settings_screen.dart:966-976`, `:1230-1241` | medium |
| F9 | Diagnostics (secret storage failure text, SPKI fingerprints, cert pin hex) render at the same visual weight as everyday controls, in monospace-hostile `ListTile` subtitles. | `:1059-1152` | low |

## Decision Drivers

* **Scale honestly**: 5 agents × (status + active upstream + N credentials)
  plus a 73–185-vendor catalog must stay scannable; the catalog must stay
  paged and searchable (MADR 0074 D16 — never inflate `providers.list`).
* **Do not regress recorded guarantees**: D6 (feature invisible without the
  capability), D8 (codex destructive confirm), D11 (secrets never linger or
  echo), D14 (switch only among configured), D16 paging, 0079's picker
  behaviours, predictive-back compliance (`flutter.md`).
* **Graphical richness with substance**: colour, iconography and containment
  should encode *state and identity*, not decoration.
* **One idiom**: pickers, sheets and status chips should share primitives so
  the next MADR's rows inherit the look for free.
* **Phone-first ergonomics**: reachable actions, bottom sheets over dialogs,
  minimal typing (per the repo's existing standards and the sources below).
* **Offline/self-contained app**: any logo assets must ship in the APK/IPA;
  there is no CDN at runtime.

## Research: patterns and prior art (web survey, 2026-08-12)

1. **Group into few top-level categories; push detail to subpages.** Toptal's
   settings-UX guidance: limit top-level categories to four or five, keep
   frequently used settings shallow, move the rest to subpages; plain
   language over jargon; optimal defaults; confirm destructive changes.
   Setproduct's settings survey catalogs the layout options (grouped,
   hierarchical, accordion, cards) and lists "burying settings", "unclear
   labels" and "no confirmation for destructive actions" as the canonical
   mistakes — F5 is exactly that mistake.
2. **The "integrations/connections page" is the closest established pattern
   to our provider menus.** Peer AI clients converge on it: TypingMind puts
   a gear per provider in a Models list with an API-keys tab; LibreChat and
   Open WebUI present provider *connections* as a managed list with add /
   verify / remove; the generic pattern is a card or row per integration
   with a real-time status badge (Carbon's status-indicator pattern;
   Mobbin/Setproduct badge guidance: one content type per badge, dot + label
   for state, badge attached to the parent card).
3. **Material 3 Expressive (shipped with Android 16 QPR1, Sept 2025)
   restyled exactly this kind of screen**: grouped "containerized" lists —
   sections rendered as rounded surface containers with the first/last row
   shaped, generous section headers — plus 15 refreshed components (button
   groups, split buttons, loading indicators) and shape morphing. Google's
   own Settings and Phone apps now use container-grouped lists. Flutter
   exposes the underlying tokens (surface container roles are already in
   `celestial.dart:179-193`), so the look is achievable without waiting for
   framework M3-Expressive widgets.
4. **AI-brand iconography is a solved asset problem.** Lobe Icons
   (`lobehub/lobe-icons`, MIT) maintains 1100+ AI/LLM provider and model
   logos as dependency-free static SVG/PNG/WebP packages usable outside
   React. Vendor ids in our catalogs (`openai`, `anthropic`, `togetherai`,
   `deepseek`, `groq`, …) map nearly 1:1 onto its slugs. Caveat recorded:
   MIT covers the *code/assets packaging*; trademark rights in individual
   logos remain the brands' — normal for this class of library, same
   position every dashboard using it takes.
5. **Settings search matters at this size** (Setproduct, mobile-search
   surveys): with ~40 rows a filter-as-you-type over titles/keywords is the
   cheapest findability win; suggestions and graceful no-results handling
   are the table stakes.

Sources:
[Toptal — How to improve app settings UX](https://www.toptal.com/designers/ux/settings-ux) ·
[Setproduct — Settings UI design](https://www.setproduct.com/blog/settings-ui-design) ·
[Setproduct — Badge UI design](https://www.setproduct.com/blog/badge-ui-design) ·
[Carbon Design System — status indicator pattern](https://carbondesignsystem.com/patterns/status-indicator-pattern/) ·
[Mobbin — Badge glossary](https://mobbin.com/glossary/badge) ·
[Google — Material 3 Expressive launch](https://blog.google/products-and-platforms/platforms/android/material-3-expressive-android-wearos-launch/) ·
[Supercharge — M3 Expressive components/motion/shapes](https://supercharge.design/blog/material-3-expressive) ·
[9to5Google — M3 Expressive redesign tracker](https://9to5google.com/2025/11/17/google-material-3-expressive-redesign/) ·
[lobehub/lobe-icons (MIT, static SVG/PNG/WebP)](https://github.com/lobehub/lobe-icons) ·
[TypingMind — API keys setup](https://docs.typingmind.com/manage-and-connect-ai-models/set-up-api-keys) ·
[Wezom — mobile app design best practices 2026](https://wezom.com/blog/mobile-app-design-best-practices-in-2025)

## Considered Options

* **O1 — Polish in place**: keep the flat page; fix F5 (confirm removal),
  add colour to status icons, better subtitles. No structural change.
* **O2 — Hub-and-spoke with a rich provider area**: settings home becomes a
  short grouped hub (M3 container-grouped sections); *Providers* becomes a
  dedicated spoke — one identity card per agent, drilling into a per-agent
  detail screen that unifies status, active upstream, default mode,
  credentials and the vendor catalog; brand icons throughout; one picker
  idiom (0079 primitives); semantic status chips.
* **O3 — Tabbed settings**: split the page into tabs (General / Providers /
  Connection / Advanced) with the same row styling as today.

## Decision Outcome

Chosen option: **"O2 — hub-and-spoke with a rich provider area"**, because it
is the only option that fixes the structural findings (F1, F2, F8) rather
than repainting them, matches the pattern peer products and the platform
itself converged on (§4.2–4.3), and gives every future provider feature a
place to land. O1 is kept *inside* O2 as its first increment (the safety and
colour fixes are wanted regardless); O3 is rejected because tabs cap out
quickly (nine sections don't fit 4–5 tabs without a junk-drawer tab) and
hide state behind unlabelled horizontal navigation.

### Sub-decisions

**D1 — Settings home becomes a hub of grouped section containers.**
Sections render as rounded `surfaceContainerLow` cards (first/last-row
shaping, M3-Expressive-style), each with a compact header. Inline content
stays for small sections (Appearance, Notifications). Three sections become
spokes with a one-line summary row: **Providers** ("2 of 5 agents ready ·
kilo quota reached"), **Connection & security** (host + route + pin +
identity + re-pair), **Storage & diagnostics** (cache, secret storage,
receipts). Order by frequency of use: Providers, Sessions defaults,
Notifications, Appearance, Working directories, Connection & security,
Storage & diagnostics, About.

**D2 — A Providers screen: one identity card per agent.**
Each card carries the agent's brand icon and name, a semantic status chip
(D4), the active upstream, and a credential count ("3 credentials · together
active"). Tapping opens the agent detail screen (D3). Cards for agents
without `provider_auth` state render without the credential rows —
preserving 0074 D6's "invisible on old daemons" at the card level. The
Providers spoke itself appears only when at least one provider reports;
otherwise the hub row is absent, exactly as `_providerCredentialSection`
vanishes today.

**D3 — A per-agent detail screen unifies what F8 found split.**
One screen per agent: status header; *Active upstream* selector (only with
≥2 configured — D14 unchanged); *Default mode* (moved here from Sessions;
the Sessions section keeps only cross-agent defaults like thinking level);
the configured-credential list with per-vendor logos and status chips; and
the *Add credential* entry into the catalog. Remove-credential moves behind
a confirmation dialog (fixes F5) and gains a swipe-to-reveal affordance on
the row; the bare one-tap trailing icon goes away.

**D4 — One semantic status system, used everywhere.**
A small `StatusChip` widget (dot + label, one content type per badge per the
badge guidance): `configured → CelestialColors.success`, `quota reached →
caution`, `error → colorScheme.error`, `needs setup → onSurfaceVariant`,
`active` as a filled primary chip. The route section's probe chips migrate
to it; the provider cards, credential rows and catalog rows consume it. No
new colour tokens — the celestial extension already defines them.

**D5 — Vendor and agent brand icons, bundled, with a monogram fallback.**
Adopt a curated subset of Lobe Icons static SVGs as Flutter assets (the ~200
ids our catalogs actually emit, monochrome variants preferred so they tint
with the theme), rendered via `flutter_svg`, keyed by upstream id with a
deterministic two-letter monogram avatar (id-hashed background from the
celestial palette) when no asset matches. Assets ship in the app — no
runtime CDN. License: MIT for the set; trademark caveat recorded in §4.4.
The monogram fallback is mandatory: goose's pinned table and future engine
catalogs will always contain ids we have no logo for.

**D6 — One picker idiom: the 0079 primitives.**
`UpstreamCatalogSheet` is rebuilt on `PickerSheetLayout` +
`PickerCatalogView` (which already do search, groups, badges and truncation
footers), and the three `SimpleDialog` pickers (default mode, thinking
level, connect mode) plus the ad-hoc active-upstream sheet become
`showOptionPicker` sheets. The catalog keeps its server-side paging and
250 ms debounce — `PickerCatalogView`'s in-memory filtering is layered on
top of, not instead of, the wire query (D16 unchanged).

**D7 — The catalog gets bands, logos and honest subtitles.**
Rows render logo + display name + method chips ("API key", "Device code",
"Host only") instead of the id-repeating subtitle. Grouping: **Configured**
first, then **Popular** (a small static list of the usual suspects —
openai, anthropic, google, togetherai, deepseek, groq, mistral, openrouter),
then **All vendors** alphabetically; search collapses bands. Browser-only
vendors stay visible-but-disabled with the "Requires host access" chip
(unchanged from D16's shipped behaviour). "Showing N of M" and the
pinned-list note stay.

**D8 — Settings search on the hub.**
A filter field over row titles/keywords across all sections and spokes,
appearing as the first hub element. Cheap at this row count and the
standard findability answer (§4.5). Deferrable to a later increment without
weakening D1–D7.

### Consequences

* Good, because the provider area becomes a legible product surface — one
  card per agent, one screen per agent, brand-recognisable vendors — instead
  of a suffix-labelled tile soup, and the phone finally *looks like* the
  multi-provider control plane 0074 made it.
* Good, because a real destructive-action gap (F5) is closed, and status
  becomes glanceable through one chip system instead of four grey glyphs.
* Good, because picker unification deletes three bespoke idioms and reuses
  tested 0079 code; future catalogs inherit search/groups/badges for free.
* Good, because the hub shortens the everyday page: diagnostics and
  security detail move one level down but remain two taps away.
* Neutral, because the visual language is M3-Expressive-*styled* via theme
  tokens rather than pinned to framework M3-Expressive widgets — Flutter's
  own component refresh can be adopted later without re-deciding.
* Bad, because bundled logos add app size (estimated 150–300 KB for ~200
  optimized monochrome SVGs) and a curation duty when catalogs gain
  vendors; the monogram fallback bounds the failure to "less pretty".
* Bad, because moving *Default mode* and splitting the flat page relocates
  rows users may have muscle-memory for; mitigated by the hub summaries and
  (D8) search.
* Bad, because it is a real chunk of Flutter work (new routes, cards,
  chips, asset pipeline, migrated tests — the existing widget tests key on
  `Key('provider-auth-tile-…')` etc. and will need updating alongside).

### Confirmation

* Widget tests: hub renders all sections; Providers spoke absent without
  `provider_auth` state (D6 parity); per-agent screen shows unified rows;
  credential removal always passes through the confirmation dialog; catalog
  bands order Configured → Popular → All; monogram fallback renders for an
  unknown id; status chip colours map as specified.
* Existing 0074/0079 test suites stay green (catalog paging, device-flow
  sheet, picker behaviours, predictive back per `flutter.md`).
* Manual smoke on Android + iOS simulator: five-agent host, one agent in
  quota state, one credential add + remove end-to-end from the new screens.

## More Information

Illustrative structure (not a pixel spec):

```text
Settings (hub)                     Providers                    kilo
┌─ Search settings ─────────┐      ┌──────────────────────┐     ┌──────────────────────┐
├─ Providers ──────────────▸│      │ ◆ kilo               │     │ ◆ kilo   ● configured│
│  2 of 5 ready · kilo quota│      │ ● quota · together✓  │     │ Active upstream      │
├─ Sessions ────────────────┤      ├──────────────────────┤     │   together ▾         │
│  thinking · send-on-enter │      │ ◆ opencode           │     │ Default mode  code ▾ │
├─ Notifications ───────────┤      │ ● ready · 3 creds    │     │ Credentials          │
├─ Appearance ──────────────┤      ├──────────────────────┤     │  [logo] Together  ●  │
├─ Working directories ─────┤      │ ◆ goose  ● ready     │     │  [logo] DeepSeek  ●  │
├─ Connection & security ──▸│      │ ◆ codex  ● ready     │     │  + Add credential ──▸│
├─ Storage & diagnostics ──▸│      │ ◆ grok   ○ needs setup     └──────────────────────┘
└─ About ───────────────────┘      └──────────────────────┘
```

If accepted, the implementation plan should sequence: (P-a) F5 confirmation
+ D4 status chips + D7 catalog subtitles — small, immediately shippable;
(P-b) D5 asset pipeline + logos; (P-c) D1–D3 restructure; (P-d) D6 picker
unification; (P-e) D8 search. Each phase leaves the app releasable.
