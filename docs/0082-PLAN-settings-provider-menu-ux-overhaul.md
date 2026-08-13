<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# Implement restructure settings into a hub with a graphically rich provider area

Associated MADR: [0082-MADR-settings-provider-menu-ux-overhaul.md](0082-MADR-settings-provider-menu-ux-overhaul.md)

| field | value |
| --- | --- |
| status | **implemented** 2026-08-12. P1 `0fff7ca`, P2 `bd9280f`, P3 `458ec37`, P4 `a58ebd7`, P5 `bb814b6` — one commit per phase, each gated on format+analyze+`flutter test` (930 passing at P5). |
| phases | P1 safety + status semantics ✅ · P2 brand-icon pipeline ✅ (68 icons, 276 KB) · P3 hub/spoke restructure ✅ · P4 picker unification + catalog bands ✅ · P5 settings search ✅ |
| rule | Commit per phase; do not push until asked. Each phase leaves the app releasable. |

## Goal

Deliver MADR 0082 D1–D8: a grouped settings hub, a Providers spoke with one
identity card per agent, a per-agent detail screen, one semantic status-chip
system, bundled vendor brand icons with a monogram fallback, picker
unification on the 0079 primitives, a banded catalog, and (last, optional)
settings search — without regressing MADR 0074 D6/D8/D11/D14/D16 or MADR
0079 picker behaviour.

## Scope

In scope: `apps/mobile` only (Flutter). New routes under `/settings`, new
widgets, an asset-generation script under `tools/`, pubspec changes, and the
widget tests named per phase. Out of scope: any daemon or protocol change
(the wire surface from MADR 0074 is used as-is), iOS/Android release plumbing,
and the browser-OAuth loopback workstream (W3).

## Grounding facts (verified against the tree, 2026-08-12)

Every step below builds on these; if one no longer holds, stop and re-verify
before proceeding.

| # | Fact | Evidence |
| --- | --- | --- |
| G1 | Routing is a flat `GoRouter` in `apps/mobile/lib/app.dart`; `/settings` is a top-level `GoRoute` with no sub-routes (`app.dart:46-48`). The `redirect` only special-cases `/` and `/sessions` (`app.dart:51-70`), so children of `/settings` inherit no redirect behaviour. | `lib/app.dart` |
| G2 | The settings screen is one `ListView` (`settings_screen.dart:850-1184`) with `_sectionHeader` text headers and `Divider`s; the provider credential section is `_providerCredentialSection` (`:1219-1275`) with helpers `_authStatusIcon` (`:1277`), `_authStatusLabel` (`:1284`), `_configuredUpstreams` (`:1294`), `_pickActiveUpstream` (`:1301`), `_browseUpstreamCatalog` (`:1340`), `_openAuthSheet` (`:1365`), `_runDeviceSignIn` (`:1419`), `_clearCredential` (`:1503`), `_pickDefaultMode` (`:633`). | `lib/features/settings/settings_screen.dart` |
| G3 | `_clearCredential` clears with **no confirmation**; it is invoked directly from the trailing `link_off` `IconButton` (`:1264-1270`). A destructive-button style helper `destructiveFilled(ColorScheme)` already exists and is used at `:806`. | same |
| G4 | Existing widget keys in lib: `provider-active-upstream-<pid>` (`:1232`), `provider-add-credential-<pid>` (`:1247`), `provider-auth-tile-<pid>-<uid>` (`:1255`), `active-upstream-option-<uid>` (`:1311`), `device-auth-destructive-confirm` (`:1431`); catalog sheet: `upstream-catalog-{close,search,subtitle,error,empty,row-<id>}`; auth sheet: `provider-auth-{method,submit,secret,input-<key>}`. **No file under `test/` asserts the settings-screen provider keys** (`provider-auth-tile`, `provider-add-credential`, `provider-active-upstream`, `active-upstream-option`, `device-auth-destructive-confirm` grep to zero in `test/`). | grep 2026-08-12 |
| G5 | Flutter tests that exist: `settings_screen_test.dart` (19 `testWidgets`: theme, alerts copy, transports, connect mode, token dialog, re-pair, secret storage, client identity), `upstream_catalog_sheet_test.dart`, `provider_auth_sheet_test.dart`, `provider_auth_push_test.dart`, `device_flow_sheet_test.dart`, `model_provider_step_test.dart`. | `apps/mobile/test/` |
| G6 | 0079 picker primitives: `showOptionPicker(context, {required PickerCatalog catalog, String title, List<String>? initialSelected, String? thinkingIntent}) → Future<PickerResult?>` where `PickerResult.selectedIds`/`thinkingLevel`; `PickerSheetLayout` (85% height + keyboard inset), `PickerSheetHeader({title, source, onClose, onBack})`, `PickerCatalogView` (search, groups from `PickerOption.group`, O(1) rows, truncation footer, `PickerCatalogInteraction.select/navigate`). `PickerCatalog` has a plain constructor (`picker.dart:210-223`) with `PickerSource.staticSource` available; `PickerOption` fields: `id,label,description,group,enabled,meta,thinkingLevels`. | `lib/features/widgets/option_picker_sheet.dart`, `lib/data/protocol/picker.dart` |
| G7 | Theme: `celestialOf(BuildContext)` resolves `CelestialColors` with a brightness fallback for foreign-theme tests (`celestial.dart:140-146`); tokens `success`, `caution`, `running`, `gold*` exist in dark and light (`:62-86`); M3 surface-container roles are already defined (`:179-193`). | `lib/theme/celestial.dart` |
| G8 | Data model (`lib/data/protocol/models.dart:418-640`): `ProviderInfo{id,ready,auth?}`, `ProviderAuthInfo{status,activeUpstream?,upstreams}`, `UpstreamAuth{id,status,label?,methods}` with `display`/`isConfigured`, `AuthMethod{id,type,label,inputs}` with `isApiKey/isDeviceOAuth/isBrowserOAuth`, `AuthStatus.{configured,missing,error,quota}`, `ProviderAuthCatalog{providerId,upstreams,offset,truncated,total,source}`. Client methods used today: `listProviders`, `listUpstreamCatalog({providerId,query,offset,limit})`, `setProviderCredential`, `clearProviderCredential`, `setActiveUpstream`, `startProviderDeviceAuth`, `cancelDeviceAuth`, streams `providerAuthStatus`, `deviceFlows`, `deviceFlowResults`. | models.dart, mcremote_client.dart |
| G9 | Catalog paging: sheet page size 100 (`upstream_catalog_sheet.dart:35`), server default 100 / cap 200 (MADR 0074 D16); the sheet already debounces 250 ms and generation-guards searches (`:87-132`). | sheet + `internal/ws/server.go` |
| G10 | pubspec: **no `flutter_svg`**; the only asset is `assets/MC_icon.png` (`pubspec.yaml:51-54`). | `apps/mobile/pubspec.yaml` |
| G11 | Vendor-id universes: goose is a pinned 73-id table in `internal/provider/goose/catalog.go` (ids include `together`, `aws_bedrock`, `gcp_vertex_ai`, `github_copilot`, `xai`, `zhipu`, `ollama`, `lmstudio`, `venice`, `openrouter`); kilo/opencode ids come from the live engine (fixture `internal/provider/kilo/testdata/provider-7.4.21.json` is a 10-id subset: `anthropic deepseek github-copilot groq kilo openai opencode-go openrouter togetherai xai`); full lists are engine-fetched (185/184 live). | Go tree |
| G12 | Standards: predictive back must keep working via `PopScope`/GoRouter (`docs/standards/mobile/flutter.md:27-28`); CI gates are `dart format --set-exit-if-changed`, `dart analyze`, `flutter test` (`.github/workflows`, Flutter job). | docs, CI |
| G13 | The settings screen already live-refreshes on `providerAuthStatus` pushes (`settings_screen.dart:101-105`) — the pattern each new screen must repeat. | settings_screen.dart |

## Implementation Steps

Conventions for all phases: run the format/analyze/test gate before each
commit (`cd apps/mobile && dart format . && dart analyze && flutter test`);
new user-visible strings follow the existing sentence-case copy style; every
new interactive widget gets a `Key` so tests are not text-coupled.

### P1 — Safety and status semantics (MADR F5 fix, D4, D7-subtitles)

1. **New file** `apps/mobile/lib/features/widgets/status_chip.dart`:

   ```dart
   enum StatusKind { ok, caution, error, neutral, active }
   class StatusChip extends StatelessWidget {
     const StatusChip({super.key, required this.kind, required this.label, this.compact = true});
     factory StatusChip.auth(String status, {bool active = false}) // maps G8 AuthStatus
   }
   ```

   Rendering: an 8 px dot + `labelSmall` text inside a `visualDensity:
   compact` pill on `surfaceContainerHigh`; colours via `celestialOf(context)`
   (G7): `ok → success`, `caution → caution`, `error → colorScheme.error`,
   `neutral → onSurfaceVariant`, `active → primary` (filled pill,
   `onPrimary` text). `StatusChip.auth` maps `configured→ok('Configured')`,
   `quota→caution('Quota reached')`, `error→error('Error')`,
   `missing→neutral('Needs setup')`; `active:true` appends a second
   `active` chip at the call site, never a suffix string. Root widget key:
   `Key('status-chip-<kind>')`.
2. **Edit** `settings_screen.dart`: replace `_authStatusIcon`/
   `_authStatusLabel` (G2) usage in `_providerCredentialSection`: each
   upstream row's `subtitle` becomes a `Wrap(spacing: 6)` of
   `StatusChip.auth(up.status)` plus, when `p.auth!.activeUpstream == up.id`,
   `const StatusChip(kind: StatusKind.active, label: 'Active')`. Keep the
   leading icon for now (P2 replaces it with the vendor icon). Delete the two
   static helpers once unreferenced.
3. **Edit** `settings_screen.dart` `_clearCredential` (G3): prepend a
   confirmation dialog before any client call:

   ```dart
   final ok = await showDialog<bool>(... AlertDialog(
     key: const Key('remove-credential-confirm'),
     title: Text('Remove ${up.display} credential?'),
     content: Text('$providerId will lose access to ${up.display} until a new '
         'credential is added. The key is deleted on the host.'),
     actions: [TextButton 'Cancel' → false,
               FilledButton(style: destructiveFilled(scheme)) 'Remove' → true]));
   if (ok != true || !mounted) return;
   ```

   (`destructiveFilled` import already present, G3.)
4. **Edit** `_probeChip` (`settings_screen.dart:501-526`): reimplement on
   `StatusChip` — `operational → ok('<label> · up')`, configured-but-silent
   `→ neutral('<label> · no answer')`, not configured `→ neutral('<label> ·
   not paired')`. Remove the local `Chip` construction.
5. **Edit** `upstream_catalog_sheet.dart` `_rowSubtitle` (G9 file, `:276-283`):
   stop echoing `up.id`. Row subtitle becomes a `Wrap` of method chips derived
   from `up.methods`: any `isApiKey → 'API key'`, any `isDeviceOAuth →
   'Device code'`, browser-only → single `'Host only'` chip (row stays
   disabled, unchanged); configured rows lead with `StatusChip.auth`. Plain
   `Text` fallback `up.display` when `methods` is empty.
6. **Tests** (closing the G4 coverage gap — these are new, not migrations):
   * extend `test/settings_screen_test.dart` with a provider-section group
     that pumps `SettingsScreen` with a fake client reporting one provider
     (`kilo`) with upstreams in all four states: asserts one
     `status-chip-ok/caution/error/neutral` each, the `Active` chip on the
     active upstream, and that tapping the remove button shows
     `remove-credential-confirm` and only calls `clearProviderCredential`
     after 'Remove' (and never on 'Cancel');
   * extend `test/upstream_catalog_sheet_test.dart`: a row with an api-key
     method shows an `API key` chip and no raw-id subtitle; a device-flow row
     shows `Device code`.
7. Gate + commit (message via repo hook).

### P2 — Vendor/agent brand icons (MADR D5)

1. **Add dependency**: `flutter_svg: ^2.0.10` to `pubspec.yaml`
   `dependencies` (G10). Run `flutter pub get`.
2. **New script** `tools/vendor-icons/sync.sh` (host-side, not shipped):
   * pins `LOBE_ICONS_VERSION` (npm `@lobehub/icons-static-svg`, MIT — MADR
     §4.4), downloads the tarball with `npm pack` or `curl` to a temp dir;
   * reads the id union from two checked-in inputs it also maintains:
     `tools/vendor-icons/ids.txt` (one upstream id per line — seeded from the
     goose table in `internal/provider/goose/catalog.go` (G11) plus the ids
     observed from kilo/opencode; the live-tagged Go tests print these, and
     the file is committed so the build never needs an engine) and
     `tools/vendor-icons/map.json` (id → lobehub slug overrides);
   * starter `map.json` (extend as needed, unmapped ids fall through to
     identity match then monogram): `togetherai/together→together`,
     `github-copilot/github_copilot/copilot-acp→githubcopilot`,
     `aws_bedrock→bedrock`, `azure_openai/azure_foundry/azure→azure`,
     `gcp_vertex_ai→vertexai`, `google/gemini-cli/gemini_oauth→gemini`,
     `xai/xai_oauth/grok→grok`, `chatgpt_codex/codex/codex-acp→openai`,
     `claude-code/claude-acp→claude`, `fireworks-ai→fireworks`,
     `moonshot/kimi_code→moonshot`, `zai/zhipu→zhipu`,
     `opencode_go/opencode-go→opencode`, `ollama/ollama_cloud→ollama`,
     `vercel_ai_gateway→vercel`, `alibaba→qwen`, `custom_deepseek→deepseek`,
     `huggingface→huggingface`, `lmstudio→lmstudio`; agent ids
     `goose→goose`(fallback monogram if absent), `kilo→kilocode`;
   * copies the **monochrome** SVG variant for every resolvable id into
     `apps/mobile/assets/vendor_icons/<id>.svg` (file named by *our* id, so
     lookup needs no map at runtime), `svgo`-optimizes when available, and
     emits `apps/mobile/lib/features/widgets/vendor_icon_manifest.g.dart`
     containing `const Set<String> kVendorIconIds = {...};` — lookup is a
     compile-time set, no runtime IO or AssetManifest probing;
   * prints the total asset byte size; **abort the phase if > 400 KB**
     (MADR consequence estimated 150–300 KB).
3. **Edit** `pubspec.yaml`: add `- assets/vendor_icons/` under `assets:`.
4. **New file** `apps/mobile/lib/features/widgets/vendor_icon.dart`:

   ```dart
   class VendorIcon extends StatelessWidget {
     const VendorIcon({super.key, required this.id, this.display, this.size = 24});
   }
   ```

   If `kVendorIconIds.contains(id)`: `SvgPicture.asset('assets/vendor_icons/$id.svg',
   width/height: size, colorFilter: ColorFilter.mode(scheme.onSurface, srcIn))`.
   Else: a `CircleAvatar` monogram — first two letters of `display ?? id`
   uppercased, background chosen from a fixed 6-colour palette built from
   celestial tokens by `id.hashCode % 6`, foreground `onPrimaryContainer`.
   Key: `Key('vendor-icon-<id>')`; the monogram branch additionally
   `Key('vendor-icon-monogram-<id>')`.
5. **Adopt**: catalog sheet rows (`leading: VendorIcon(id: up.id, display:
   up.display)`), settings provider rows (replace the `Icon(_authStatusIcon…)`
   leading kept in P1 — status is on the chip now), auth sheet title row
   (icon + `widget.upstream.display`), device-flow sheet header.
6. **Tests**: new `test/vendor_icon_test.dart` — a manifest id renders
   `SvgPicture` (pick one guaranteed by the committed assets, e.g.
   `openai`); an unknown id (`no-such-vendor`) renders the monogram key with
   deterministic initials 'NO'; two different unknown ids get different
   background colours only if their hashes differ (assert via widget
   predicate, not exact colour). Extend `upstream_catalog_sheet_test.dart`:
   rows carry `vendor-icon-<id>`.
7. Gate + commit. Record the measured asset-size delta in the commit body.

### P3 — Hub, Providers spoke, per-agent detail (MADR D1–D3)

1. **New file** `apps/mobile/lib/features/settings/section_card.dart`:
   `SettingsSection({required String title, String? subtitle, List<Widget>
   children, VoidCallback? onOpen})` — a rounded `Card`-like container on
   `surfaceContainerLow` (radius 16, zero elevation, `clipBehavior:
   Clip.antiAlias`), header row (title `labelLarge` primary-coloured, same
   as `_sectionHeader` today), children as-is; when `onOpen != null` the
   whole header is tappable with a `chevron_right` (a *spoke*). This is the
   M3-Expressive-style container grouping from MADR §4.3 built from existing
   theme roles (G7) — no new dependency.
2. **Routing** (G1): in `app.dart` give `/settings` sub-routes:

   ```dart
   GoRoute(path: '/settings', builder: … SettingsScreen(), routes: [
     GoRoute(path: 'providers', builder: … ProvidersScreen(), routes: [
       GoRoute(path: ':pid', builder: (c, s) =>
         ProviderDetailScreen(providerId: s.pathParameters['pid']!)),
     ]),
   ]),
   ```

   Plain `Scaffold`s under GoRouter satisfy predictive back (G12); no
   `PopScope` needed because nothing intercepts pops.
3. **New file** `providers_screen.dart` (`ProvidersScreen extends
   ConsumerStatefulWidget`): loads `client.listProviders()` in `initState`,
   re-loads on `providerAuthStatus` pushes (mirror `settings_screen.dart:
   101-105`, G13), and renders one card per provider (`Key('provider-card-
   <pid>')`): `VendorIcon(id: p.id)`, provider id as title, subtitle chips —
   `StatusChip.auth` of the *worst* upstream status when `p.auth != null`
   (`error > quota > missing > configured` precedence), else
   `ready ? ok('Ready') : neutral('Not ready')` — plus a trailing summary
   `Text('<n> credentials · <active> active')` when auth is present. Tap →
   `context.push('/settings/providers/${p.id}')`. Disconnected state: the
   screen shows a single explanatory tile (reuse `friendlyOpError` copy
   style), no spinner-forever.
4. **New file** `provider_detail_screen.dart` (`ProviderDetailScreen`):
   **move** from `settings_screen.dart`, changing only widget context:
   `_pickActiveUpstream`, `_browseUpstreamCatalog`, `_openAuthSheet`,
   `_runDeviceSignIn`, `_clearCredential` (P1 version), `_pickDefaultMode`
   (G2). Screen layout in `SettingsSection`s: *Status* (agent VendorIcon +
   worst-status chip + active upstream); *Session defaults* (`Default mode`
   tile — moved per D3); *Credentials* (one row per `p.auth!.upstreams`
   entry, keys preserved verbatim: `provider-auth-tile-<pid>-<uid>`,
   trailing remove button, `provider-active-upstream-<pid>` tile when ≥2
   configured — D14 gate unchanged; `provider-add-credential-<pid>` last).
   All existing behaviour keys (G4) keep their strings so P1's new tests
   move here by changing only the pumped widget.
5. **Edit** `settings_screen.dart` into the hub: delete
   `_providerCredentialSection` + moved helpers and the per-provider
   `Default mode` tiles (`:966-976`); wrap the remaining sections in
   `SettingsSection`s in the MADR D1 order; add the **Providers** spoke
   section (`Key('settings-providers-spoke')`) with subtitle
   `'<ready>/<total> agents ready'` plus `' · <pid> <anomaly>'` for the
   first agent whose worst status is `quota` or `error`; hide the spoke
   entirely when `_providers.isEmpty` (capability parity: a daemon without
   `provider_auth` still lists providers, so the spoke shows cards without
   credential sections — MADR D2; a disconnected phone shows the spoke with
   the disconnected tile). Move Connection+Host rows under one *Connection &
   security* section and Storage+Receipts+Secret-storage under *Storage &
   diagnostics*; these stay inline (containers only, no new screens) in this
   phase to bound the diff.
6. **Tests**:
   * `settings_screen_test.dart`: existing 19 tests keep passing with only
     finder adjustments (rows now inside `SettingsSection` containers — the
     tests find by text/key, G5, so most need no change); add: spoke hidden
     with an empty provider list; spoke subtitle counts ready agents; spoke
     navigates (pump with a `GoRouter` test harness — follow
     `connect_screen_test.dart`'s existing router pattern, G5).
   * new `test/providers_screen_test.dart`: card per provider; worst-status
     chip precedence (provider with `quota`+`configured` upstreams shows
     caution); push-refresh (reuse the `_PushServer` idiom from
     `provider_auth_push_test.dart` or a fake client) updates a chip without
     re-pumping.
   * new `test/provider_detail_screen_test.dart`: **port P1's provider-
     section tests here** (the four status chips, confirm-before-remove) and
     add: `Default mode` tile present and persisting via `SettingsStore`
     (assert `getDefaultSessionMode`), active-upstream tile only with ≥2
     configured, add-credential opens the catalog sheet.
7. Gate + commit.

### P4 — One picker idiom + catalog bands (MADR D6, D7)

1. **Catalog sheet on the 0079 chrome** (`upstream_catalog_sheet.dart`):
   wrap content in `PickerSheetLayout` + `PickerSheetHeader(title: 'Add
   credential · <pid>', source: PickerSource.staticSource when isStatic else
   .live, onClose: pop)` (G6) — the header gains 0079's source badge for
   free, replacing the hand-rolled title row and the 0.8-height `SizedBox`.
   The sheet **keeps its own fetch/paging/debounce state machine** (G9);
   `PickerCatalogView` is not adopted here because it renders a complete
   in-memory catalog and the wire catalog is server-paged (G6 vs G9) — a
   footnote comment in the file records this boundary.
2. **Bands** (D7): the sheet gains `final List<UpstreamAuth> configured;`
   passed by the caller (`ProviderDetailScreen` has `p.auth!.upstreams`).
   With an empty query, render three labelled bands as list headers:
   *Configured* (the passed list, marked with their status chips), *Popular*
   (`static const kPopularUpstreams = ['openai','anthropic','google',
   'togetherai','deepseek','groq','mistral','openrouter']` intersected with
   loaded rows, minus configured), *All vendors* (everything else, in server
   order). A non-empty query renders flat results (server-filtered, G9).
   Configured ids are de-duplicated out of the paged rows by id. Band
   headers: `Key('upstream-catalog-band-<name>')`.
3. **Dialog pickers → `showOptionPicker`** (G6), one call site each:
   * `_pickDefaultMode` (moved to detail screen): options from the collected
     `SessionMode`s (`PickerOption(id: m.id, label: m.name.isEmpty ? m.id :
     m.name, description: m.dangerous ? 'Runs without approvals' : '',
     group: '')`) plus a first `Provider default` option (`id: ''`);
     `initialSelected: [current]`. The dangerous-mode confirmation moves to
     *after* the picker returns and before persisting — same dialog copy as
     today (`settings_screen.dart:679-699`), so B2 semantics are unchanged.
   * `_pickDefaultThinkingLevel` (hub): four options, same ids as today
     (`''|low|medium|high`).
   * `_pickConnectMode` (hub): two options, ids `auto|select`, descriptions
     copied verbatim from `settings_screen.dart:321-332`.
   * `_pickActiveUpstream` (detail): options from `_configuredUpstreams`
     with `initialSelected: [active]`; delete the ad-hoc bottom sheet
     (`:1303-1321`) and its `active-upstream-option-<id>` keys — the picker
     rows carry 0079's own option keys; update/port any P3 test finders.
   * All four: `PickerCatalog(source: PickerSource.staticSource, options:
     …, defaultIds: […])`; a `null` result means cancelled (G6), preserve
     current no-op behaviour.
   * Delete the three `SimpleDialog` builders once unreferenced.
4. **Tests**: extend `upstream_catalog_sheet_test.dart` — bands appear in
   order Configured/Popular/All on empty query with a configured `togetherai`
   passed in; the configured row is not duplicated in *All*; searching
   collapses bands (no band keys present). Detail/hub tests: picking a mode
   via the option picker persists; dangerous mode still confirms after
   selection; cancel persists nothing. Verify `model_provider_step_test.dart`
   and `model_picker_test.dart` stay green (shared primitives untouched —
   only new call sites).
5. Gate + commit.

### P5 — Settings search (MADR D8, optional/deferrable)

1. **Edit** `settings_screen.dart`: a `TextField` (`Key('settings-search')`)
   as the first hub element filtering a `static const List<({String title,
   String keywords, String target})> kSettingsIndex` — one entry per hub row
   and per spoke row (targets: `'inline:<section>'` scrolls via a per-section
   `GlobalKey` + `Scrollable.ensureVisible`; `'route:/settings/providers'`
   pushes). While the query is non-empty the hub renders only matching
   entries as tappable rows; empty query restores the sections.
2. **Tests**: querying `credential` surfaces the Providers entry and taps
   navigate; querying gibberish shows the standard empty copy; clearing
   restores all sections.
3. Gate + commit.

## Verification

Per phase (all must pass before the phase commit):

```sh
cd apps/mobile
dart format --output=none --set-exit-if-changed .
dart analyze
flutter test
```

Whole-plan acceptance (after P4; P5 independent):

1. All pre-existing suites green, including `provider_auth_push_test.dart`,
   `device_flow_sheet_test.dart`, `model_provider_step_test.dart` (G5) —
   proves 0074 D8/D10 flows and 0079 pickers unregressed.
2. New coverage exists for every G4 key that had none: confirm-remove,
   status chips, provider cards, detail screen, catalog bands, vendor icons.
3. `flutter build apk --debug` succeeds; APK size delta from P2 assets is
   recorded and ≤ 400 KB. **Verified 2026-08-12** after a toolchain fix:
   the dev host's only JDK was Homebrew OpenJDK 26, which Gradle 9.1.0
   rejects ("Unsupported class file major version 70") — pre-existing and
   unrelated to 0082. Fixed by installing OpenJDK 21 and pointing Flutter at
   it via `flutter config --jdk-dir` (no global JAVA_HOME change).
   `flutter build apk --debug` then succeeded; all 68 vendor icons are in
   the APK at 85 KB compressed (276 KB raw — in budget).
4. Manual smoke (Android device or emulator + iOS simulator, five-agent
   host): hub renders grouped sections; Providers spoke → kilo card → detail;
   add a Together AI key end-to-end from the banded catalog (logo visible);
   remove it via the confirmation; switch active upstream; codex device-flow
   confirm still appears (`device-auth-destructive-confirm` untouched);
   predictive-back swipe from detail → providers → hub behaves (G12).
5. Daemon-compat check: against a pre-0074 daemon build (no `provider_auth`
   cap), the Providers spoke shows cards without credential sections and no
   catalog entry — 0074 D6 parity at the new surface.

## Rollout and Rollback

* One commit per phase (repo rule), pushed only when asked; every phase
  leaves the app releasable, so a release can ship after any prefix of
  P1–P5.
* P1 and P2 are pure additions plus row-local edits — rollback is a single
  `git revert` each. P3 is the structural commit; its revert restores the
  flat screen wholesale (helpers move, they are not rewritten). P4 deletes
  the `SimpleDialog`s only after the picker replacements land in the same
  commit — revert restores them together. P5 is isolated.
* The vendor-icon asset set is regenerable and removable: deleting
  `assets/vendor_icons/` + rerunning `sync.sh` with an empty `ids.txt`
  degrades every icon to the monogram fallback with no code change.
* No daemon, protocol, or stored-preference format changes anywhere in the
  plan — `SettingsStore` keys (`default mode`, `thinking level`, `connect
  mode`) are read/written unchanged, so downgrade/upgrade cycles are safe.
