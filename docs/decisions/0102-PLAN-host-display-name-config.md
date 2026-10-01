# Implement host display name configuration

Associated MADR: [0102-MADR-host-display-name-config.md](./0102-MADR-host-display-name-config.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

## Goal

Add a top-level `display_name` key to mcremote `config.yaml`. The daemon
reports it on the frames that mark the socket authenticated (`auth_ok` and
`pair_ok`); the phone shows it at the top of the sessions landing screen
and as the first line of the settings Host row instead of (sessions) or
above (settings) the dialled address (e.g. a Tailscale IPv4). Empty (the
default) reproduces today's behaviour exactly on every phone vintage.

Review decisions already fixed by the MADR (2026-08-18): 128-character cap
enforced by `Validate` (rune count); the settings Host row adopts the name
in this same change; env alias `MCREMOTE_DISPLAY_NAME` is in scope. No CLI
flag is added.

Assessment refinements (2026-08-19): `pair_ok` carries the same field so
first enrollment is not a special case; `Load` trims (Validate is a value
receiver and cannot); load tests extend `config_test.go`; env-isolation is
mandatory in those tests.

## Scope

**In scope**

* Go daemon: config struct, defaults/env wiring, validation, `auth_ok`
  and `pair_ok` payload fields, WS server plumbing, daemon wiring.
* All four YAML templates, `docs/config.md`, and the README YAML-surface
  table (MADR 0090 parity gate + `docs/standards/go/config.md` "update
  user documentation in the same change").
* `docs/protocol-v1.md` note for the optional `auth_ok` / `pair_ok`
  fields.
* Phone app: client-side capture from both success frames, sessions-screen
  label substitution, settings Host row, plus Dart tests.

**Out of scope**

* Pairing, TLS, or relay routing changes — `display_name` is a pure label
  and never influences how the phone reaches the host.
* Persisting the name on the phone (it stays in-memory, like
  `hostHomeDir`).
* Mid-session label refresh — the value is reported at auth/pair time;
  phones pick up a rename on their next authenticated frame.
* Any `mcremote` CLI flag.
* Chat linking banner ("Connecting to host…"), FGS / notification-
  coordinator "Connected to host" copy — they never showed the dialled
  address.
* `GET /v1/hello` — unauthenticated, not a display channel.

## Implementation Steps

Commit after each phase (per AGENTS.md). Phase 1 keeps the config struct
and all YAML templates in one commit because the MADR 0090 parity tests
compare them and would fail a split commit.

### Phase 1 — Go config surface, templates, and docs

**1.1 `internal/config/config.go` — struct field**

In `type Config struct`, immediately after the `DataDir` field
(currently line 22), add:

```go
	// DisplayName is the friendly host name reported to phones in auth_ok
	// and pair_ok (MADR 0102). Empty = phones show the dialled address.
	// Pure label: never used for routing, TLS, or pairing.
	DisplayName string `mapstructure:"display_name"`
```

**1.2 `internal/config/config.go` — validation**

In `func (c Config) Validate() error`, immediately after the
`listen.port` range check (currently lines 1027-1029), add:

```go
	if n := utf8.RuneCountInString(c.DisplayName); n > 128 {
		return fmt.Errorf("display_name must be 128 characters or fewer, got %d", n)
	}
```

Add `"unicode/utf8"` to the import block. Do **not** assign
`c.DisplayName = strings.TrimSpace(...)` here: `Validate` is a value
receiver (`func (c Config) Validate() error`) and a mutation would not
reach the caller. `Defaults()` needs no change — the zero value `""` is
the default.

**1.3 `internal/config/load.go` — trim, default, env binding**

* In `Load`, immediately after `v.Unmarshal(&cfg)` succeeds
  (currently lines 121-123), before `cfg.Diagnostics = diags`, add:

  ```go
	cfg.DisplayName = strings.TrimSpace(cfg.DisplayName)
  ```

  `strings` is already imported.

* In `Load`'s env-binding block, after the `data_dir` bind
  (currently line 46), add:

  ```go
	_ = v.BindEnv("display_name", "MCREMOTE_DISPLAY_NAME")
  ```

  This matches the `data_dir` / `MCREMOTE_DATA_DIR` pairing. The thing
  that makes `AutomaticEnv` resolve the var is `SetDefault` (see
  `config_test.go:47-50`); BindEnv documents the alias.

* In `setDefaults`, after `v.SetDefault("data_dir", d.DataDir)`
  (currently line 262), add:

  ```go
	v.SetDefault("display_name", d.DisplayName)
  ```

  The `SetDefault` is mandatory even for an empty value: viper's
  `AutomaticEnv` only resolves keys it knows, so without it
  `MCREMOTE_DISPLAY_NAME` would be silently ignored.

**1.4 `internal/config/config_test.go` — load tests**

There is no `load_test.go`. `Load` tests already live in this file
(`TestPairAdvertiseHost`, `TestLoadFileAndEnv`, `TestReceiptsEnvOverride`).
Add the new cases here, `package config_test`.

Every case that does **not** intend to read `MCREMOTE_DISPLAY_NAME` must
`t.Setenv("MCREMOTE_DISPLAY_NAME", "")` so a developer shell that happens
to export the var cannot flip the result (env beats file). Env-only and
unset cases must also point XDG at an empty temp dir so `Load(LoadOptions{})`
does not pick up `~/.config/mcremote/config.yaml`.

| Test | Setup | Assert |
|---|---|---|
| `TestLoadDisplayNameFromFile` | `t.Setenv("MCREMOTE_DISPLAY_NAME", "")`; file contains `display_name: "  Studio Mac  "` | `cfg.DisplayName == "Studio Mac"` (trimmed) |
| `TestLoadDisplayNameFromEnv` | XDG isolation; no file; `t.Setenv("MCREMOTE_DISPLAY_NAME", "Env Name")` | `cfg.DisplayName == "Env Name"` |
| `TestLoadDisplayNameEnvBeatsFile` | file sets `display_name: "File Name"`; env sets `Env Name` | `cfg.DisplayName == "Env Name"` |
| `TestLoadDisplayNameTooLong` | `t.Setenv("MCREMOTE_DISPLAY_NAME", "")`; file sets `display_name` to `strings.Repeat("é", 129)` | `Load` returns error containing `display_name` (rune count, not byte count) |
| `TestLoadDisplayNameUnset` | XDG isolation; `t.Setenv("MCREMOTE_DISPLAY_NAME", "")`; no file | `cfg.DisplayName == ""` |

Harness rules (deterministic):

* File cases: write the YAML to `filepath.Join(t.TempDir(),
  "config.yaml")` and pass it via
  `config.LoadOptions{ConfigFile: <absolute path>}`.
* Env-only and unset cases: `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`
  so `Load(LoadOptions{})` finds no `mcremote/config.yaml` and falls
  back to defaults plus env (the `ConfigFileNotFoundError` path in
  `Load`). Do not use `MCREMOTE_CONFIG` here — an explicit config path
  that does not exist is a hard error by design.

**1.5 `internal/config/config_test.go` — Validate unit test**

Add `TestDisplayNameValidate` following the style of the existing
`TestTLSValidate` table (currently line 300):

```go
func TestDisplayNameValidate(t *testing.T) {
	cfg := config.Defaults()
	cfg.DisplayName = strings.Repeat("a", 128)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("128-char display_name must be valid: %v", err)
	}
	cfg.DisplayName = strings.Repeat("a", 129)
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "display_name") {
		t.Fatalf("129-char display_name must fail, got %v", err)
	}
	cfg.DisplayName = strings.Repeat("é", 128)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("128-rune display_name must be valid: %v", err)
	}
}
```

**1.6 YAML templates — identical key in all four files**

Insert directly after the `data_dir: ""` line in each file. Line
anchors: `internal/cli/service/defaults_mcremote.yaml:32`,
`configs/config.example.yaml:58`,
`configs/config.prod.example.yaml:60`,
`configs/config.mesh-grok.yaml:62`.

Seed (`defaults_mcremote.yaml`, short seed-style comment):

```yaml

# Friendly host name shown on the phone's sessions screen (MADR 0102).
# Empty = the dialled address. Env: MCREMOTE_DISPLAY_NAME.
display_name: ""
```

The three documented examples (`config.example.yaml`,
`config.prod.example.yaml`, `config.mesh-grok.yaml`):

```yaml

# Friendly host name the phone shows at the top of the sessions screen
# instead of the dialled address (e.g. a Tailscale IP). Empty = show the
# address. Reported in auth_ok and pair_ok; a phone picks it up on its
# next connect (or on first pair). Max 128 characters. Env:
# MCREMOTE_DISPLAY_NAME.
display_name: ""
```

Do **not** add `display_name` to `omittedConfigKeys` in
`template_parity_test.go` — the parity tests must pass unexempted.

**1.7 `docs/config.md` — two table rows**

* Settings table: after the `data_dir` row (currently line 84), add:

  ```markdown
  | `display_name` | *(empty — phones show the dialled address)* — friendly host name reported to phones at auth/pair and shown at the top of the sessions screen (MADR 0102). Max 128 characters; takes effect on the phone's next connect |
  ```

* Environment table: after the `MCREMOTE_DATA_DIR` row
  (currently line 353), add:

  ```markdown
  | `MCREMOTE_DISPLAY_NAME` | `display_name` | Friendly host name shown on the phone's sessions screen (max 128 chars) |
  ```

**1.8 `README.md` — YAML-surface table**

After the `data_dir` row in the section table (currently line 722), add:

```markdown
| `display_name` | friendly host name shown on the phone (empty = dialled address) |
```

**Phase 1 verification**

```bash
make pre-add-check FILES="internal/config/config.go internal/config/load.go internal/config/config_test.go"
go test ./internal/config/... ./internal/cli/service/...
```

Both must pass — the second run includes
`TestTemplatesSpellEveryConfigKey` and
`TestTemplateTopLevelKeysMatchExample`, which prove 1.6 is complete.
Commit (no `-m`; `git commit --no-edit` per the prepare-commit-msg hook).

### Phase 2 — Protocol field and daemon plumbing

**2.1 `internal/protocol/messages.go`**

In `AuthOKPayload`, immediately after `HomeDir` (currently line 221),
add:

```go
	// DisplayName is the operator-configured friendly host name (MADR
	// 0102). Empty = phones fall back to the dialled address. Set for v1
	// and v2 alike; clients that don't know it ignore it.
	DisplayName string `json:"display_name,omitempty"`
```

In `PairOKPayload`, immediately after `DeviceName` (currently line 261),
add the same field (same comment, same tag). First enrollment lands on
`pair_ok` and never sees `auth_ok` (`mcremote_client.dart:1956-2001`).

**2.2 `internal/ws/server.go` — option, field, payloads**

* In `type Options struct`, after `HeadscaleURL string`
  (currently line 200), add:

  ```go
	// DisplayName is the operator-configured friendly host name reported
	// in auth_ok and pair_ok (MADR 0102). Empty = phones show the dialled
	// address.
	DisplayName string
  ```

* In `type Server struct`, after the `headscaleURL string` field
  (currently line 50), add:

  ```go
	displayName string
  ```

* In `New`, after `headscaleURL: opts.HeadscaleURL,`
  (currently line 238), add:

  ```go
		displayName:        opts.DisplayName,
  ```

* In the auth handler, extend the payload literal (currently
  lines 994-999):

  ```go
	home, _ := os.UserHomeDir()
	payload := protocol.AuthOKPayload{
		DeviceID:    dev.ID,
		DeviceName:  dev.Name,
		HomeDir:     home,
		DisplayName: s.displayName,
	}
  ```

  This single `AuthOKPayload` construction site covers v1 auth, v2 auth,
  and the v2 resume fast path (v2 fields are written onto the same
  struct afterwards). There is no other `AuthOKPayload{` in the tree.

* In the pair handler, extend the payload literal (currently
  lines 1160-1164):

  ```go
	pairOK := protocol.PairOKPayload{
		Token:       token,
		DeviceID:    dev.ID,
		DeviceName:  dev.Name,
		DisplayName: s.displayName,
	}
  ```

**2.3 `internal/daemon/daemon.go` — wiring**

In the `ws.New(ws.Options{…})` literal (currently lines 314-332), add
after `HeadscaleURL: cfg.Headscale.ControlURL,`:

```go
		DisplayName:        cfg.DisplayName,
```

**2.4 `internal/ws/negotiation_test.go` — payload tests**

Reuse **this file's** harness: `newNegotiationServer` + `dialNegotiation`
+ `writeEnv`/`readEnv` (`writeEnv`/`readEnv` live in `server_test.go`,
same `ws_test` package). Do **not** look for `TestHelloReportsTLS` (it
does not exist) or `authConn` (that helper is in `replacement_test.go`).

Extend the existing helper so DisplayName can be set without copying the
store/httptest setup:

```go
func newNegotiationServer(t *testing.T) (string, string) {
	return newNegotiationServerWith(t, "")
}

func newNegotiationServerWith(t *testing.T, displayName string) (string, string) {
	t.Helper()
	// identical to today's newNegotiationServer, except
	// Options.DisplayName: displayName
}
```

Then add:

```go
func TestAuthOKCarriesDisplayName(t *testing.T) {
	url, token := newNegotiationServerWith(t, "Studio Mac")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialNegotiation(ctx, t, url)

	env, _ := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, env)
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(frame["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := json.Unmarshal(payload["display_name"], &name); err != nil {
		t.Fatalf("display_name: %v (payload %s)", err, frame["payload"])
	}
	if name != "Studio Mac" {
		t.Fatalf("display_name=%q", name)
	}
}

func TestV2AuthOKCarriesDisplayName(t *testing.T) {
	// Same as above, but AuthPayload.Protocols: []int{1, 2} and
	// decode via protocol.AuthOKPayload (this file's TestV2NegotiationAndCaps
	// shape). Assert ok.DisplayName == "Studio Mac".
}

func TestAuthOKOmitsEmptyDisplayName(t *testing.T) {
	url, token := newNegotiationServer(t) // DisplayName unset
	// v1 auth, decode payload as map[string]json.RawMessage (same as
	// TestV1AuthOKIsByteIdentical). Assert no "display_name" key.
}

func TestPairOKCarriesDisplayName(t *testing.T) {
	// newNegotiationServerWith cannot mint a pair code (it only
	// store.Create()s a durable device token). Copy the setup from
	// TestWSPairClaim in server_test.go:472-522: OpenStore +
	// OpenPairCodeStore, codes.Create("phone", 5*time.Minute),
	// ws.New(ws.Options{Store, PairCodes, Sessions, Registry,
	// RequireDeviceToken: true, DisplayName: "Studio Mac"}), httptest,
	// pair.claim with info.Display. Decode pair_ok as
	// protocol.PairOKPayload; assert ok.DisplayName == "Studio Mac".
	// Do not refactor server_test.go as part of this change.
}
```

**2.5 `docs/protocol-v1.md` — contract note**

After the `auth_ok` success example (currently lines 180-182), insert:

```markdown
The `auth_ok` payload may additionally carry `home_dir` (the daemon
user's home directory) and `display_name` (the operator-configured
friendly host name, MADR 0102). Both are optional display aids: older
daemons omit them, and clients must fall back to their own defaults
(the dialled address for `display_name`).
```

After the `pair_ok` success example (currently lines 208-219), insert:

```markdown
`pair_ok` may also carry `display_name` (same meaning as on `auth_ok`).
First enrollment never sees `auth_ok`; clients that show a host label
must read the field here as well.
```

**Phase 2 verification**

```bash
make pre-add-check FILES="internal/protocol/messages.go internal/ws/server.go internal/ws/negotiation_test.go internal/daemon/daemon.go"
go test ./internal/protocol/... ./internal/ws/... ./internal/daemon/...
```

Confirm `TestV1AuthOKIsByteIdentical` still passes (empty `display_name`
is omitted; that test only forbids `protocol`/`caps`).
Commit.

### Phase 3 — Phone app

**3.1 `apps/mobile/lib/data/ws/mcremote_client.dart` — capture**

* After `hostHomeDir` (currently lines 501-503), add:

  ```dart
  /// The operator-configured friendly host name, reported on auth and
  /// pair (MADR 0102). Null until the first successful authenticated
  /// frame; UI falls back to the dialled address.
  String? hostDisplayName;

  /// Notifies when [hostDisplayName] changes so labels rebuild without
  /// waiting for a connection-state transition.
  final ValueNotifier<String?> hostDisplayNameListenable =
      ValueNotifier<String?>(null);
  ```

* Add a setter next to `_setLastHostInput` (currently lines 748-755):

  ```dart
  void _setHostDisplayName(String? value) {
    final next =
        (value == null || value.trim().isEmpty) ? null : value.trim();
    hostDisplayName = next;
    if (hostDisplayNameListenable.value != next) {
      hostDisplayNameListenable.value = next;
    }
  }
  ```

  Always call this on an authenticated frame, even when the key is
  absent: an operator who unsets the name must clear a leftover value
  on the next connect. (`hostHomeDir` only writes when non-empty and
  would not clear; do not copy that part of the pattern.)

* In the auth-success block, after the `home_dir` read (currently
  lines 2214-2215), add:

  ```dart
  _setHostDisplayName(auth.payload?['display_name'] as String?);
  ```

* In the pair-success block, after the `device_name` read (currently
  lines 1956-1957), add the same call against `res.payload`.

* In `_noteHost`'s authority-change reset branch (currently
  lines 2279-2283), add `_setHostDisplayName(null);` alongside the
  existing `hostHomeDir = null;` reset.

* In `dispose()` (currently line 3774 region), add
  `hostDisplayNameListenable.dispose();` next to
  `hostInputListenable.dispose();`.

`MockMcremoteClient` (`sessions_screen_test.dart`) and `_FakeClient`
(`settings_screen_test.dart`) both **extend** `McremoteClient`, so the
new field and notifier appear automatically. Do not add stub copies.

**3.2 `apps/mobile/lib/features/sessions/sessions_screen.dart` — label**

Today `build` is `hostInputListenable` → `linkHealth` → `_connLabel`
(`:1360-1377`). Nest the display-name builder between those two. Do not
change `_connLabel` itself, the `ConnBanner`, or the connected chip —
they render `connLabel` unchanged.

```dart
return ValueListenableBuilder<String?>(
  valueListenable: client.hostInputListenable,
  builder: (context, hostInput, _) {
    return ValueListenableBuilder<String?>(
      valueListenable: client.hostDisplayNameListenable,
      builder: (context, displayName, _) {
        // Operator's friendly name wins over the dialled address
        // (MADR 0102); empty falls back to today's derivation.
        final hostLabel = (displayName == null || displayName.isEmpty)
            ? _hostnameOf(hostInput)
            : displayName;
        return ValueListenableBuilder<LinkHealth>(
          valueListenable: healthClient.linkHealth,
          builder: (context, linkHealth, _) {
            final degraded = connected && linkHealth != LinkHealth.fresh;
            final connLabel = _connLabel(
              connState,
              hostLabel, // was: _hostnameOf(hostInput)
              health: linkHealth,
              transport: activeTransport,
            );
            return Scaffold(
              // …existing body unchanged…
```

Connecting / reconnecting / error / disconnected strings inside
`_connLabel` still do not interpolate the hostname; that is existing
behaviour, not a regression.

**3.3 `apps/mobile/lib/features/settings/settings_screen.dart` — Host row**

Replace the Host `ListTile` subtitle (currently lines 1263-1269) with a
notifier-driven subtitle that prefers the display name and keeps the
endpoint as a second line for diagnostics:

```dart
ListTile(
  leading: const Icon(Icons.dns_outlined),
  title: const Text('Host'),
  subtitle: ValueListenableBuilder<String?>(
    valueListenable: ref
        .read(mcremoteClientProvider)
        .hostDisplayNameListenable,
    builder: (context, displayName, _) {
      final endpoint =
          _host == null || _host!.isEmpty ? '—' : _host!;
      if (displayName == null || displayName.isEmpty) {
        return Text(endpoint);
      }
      return Text('$displayName\n$endpoint');
    },
  ),
),
```

**3.4 `apps/mobile/test/mcremote_client_test.dart` — auth/pair parsing**

Change `_AuthServer.start` (currently line 23) to accept an optional
payload merge. Existing callers pass only `deviceId` and stay unchanged:

```dart
static Future<_AuthServer> start(
  String deviceId, {
  Map<String, dynamic> extraPayload = const {},
}) async {
  // …
  'payload': {
    'device_id': deviceId,
    'device_name': deviceId,
    if (type == 'pair.claim') 'token': 'mcr_$deviceId',
    ...extraPayload,
  },
}
```

The same merge applies to both `auth_ok` and `pair_ok` (the server
already uses one payload map for both types).

Add tests (reuse the existing `McremoteClient` + `SettingsStore(secure:
_MemorySecureStorage())` + `TlsMode.off` connect/claim pattern already
in this file):

* `connect` against `_AuthServer.start('dev', extraPayload:
  {'display_name': 'Studio Mac'})` → `client.hostDisplayName ==
  'Studio Mac'` and `client.hostDisplayNameListenable.value ==
  'Studio Mac'`.
* `connect` against `_AuthServer.start('dev')` (no key, old daemon) →
  `hostDisplayName` stays null.
* `claimPairCode` against a server whose `extraPayload` includes
  `'display_name': 'Studio Mac'` → same assertions as the first case.
  This is the first-enrollment path.
* Connect to host A with a name, then connect to host B **without** the
  key: `_noteHost` must clear `hostDisplayName` (different
  `_authorityOf`). Use two `_AuthServer`s as the existing stale-attempt
  test already does.

**3.5 `apps/mobile/test/sessions_screen_test.dart` — label preference**

Mirror the existing "connection banner host label tracks
hostInputListenable" test (currently line 230):

```dart
testWidgets('connection banner prefers hostDisplayName', (tester) async {
  final client = MockMcremoteClient();
  await tester.pumpWidget(_wrap(client));
  await tester.pumpAndSettle();

  client.hostInputListenable.value = '10.0.2.2:7531';
  await tester.pump();
  expect(find.text('Connected to 10.0.2.2'), findsOneWidget);

  client.hostDisplayNameListenable.value = 'Studio Mac';
  await tester.pump();
  expect(find.textContaining('Connected to Studio Mac'), findsOneWidget);

  client.hostDisplayNameListenable.value = null;
  await tester.pump();
  expect(find.text('Connected to 10.0.2.2'), findsOneWidget);
});
```

(`MockMcremoteClient` reports `activeTransport` null, so no
"over …" suffix — match accordingly.)

**3.6 `apps/mobile/test/settings_screen_test.dart` — Host row**

Reuse the existing `pumpSettings` helper (currently line 265). It
returns the `_FakeClient`, already sets `physicalSize` to
`Size(1000, 6000)` so the Host row is not culled, and `_FakeStore`
defaults `host` to `10.0.0.5:7531`.

```dart
testWidgets('Host row prefers display name above the endpoint', (tester) async {
  final client = await pumpSettings(
    tester,
    store: _FakeStore(),
    probes: _FakeProbes(),
  );
  expect(find.text('10.0.0.5:7531'), findsOneWidget);

  client.hostDisplayNameListenable.value = 'Studio Mac';
  await tester.pump();
  expect(find.text('Studio Mac\n10.0.0.5:7531'), findsOneWidget);

  client.hostDisplayNameListenable.value = null;
  await tester.pump();
  expect(find.text('10.0.0.5:7531'), findsOneWidget);
});
```

**Phase 3 verification**

```bash
dart format apps/mobile/lib apps/mobile/test
flutter analyze
flutter test
```

Run from `apps/mobile` (or via `make preflight`, which also runs the Go
checks). Stage only after `dart format` is clean — CI runs
`dart format --output=none --set-exit-if-changed .` over `apps/mobile`.
Commit.

### Phase 4 — Full-suite verification and acceptance

1. `make test` — whole Go suite, including the MADR 0090 parity tests
   with the new key present in all four templates.
2. `make race` — race suite (required by AGENTS.md before push-worthy
   work).
3. `make preflight` — gofmt, `go mod tidy` drift, and the full mobile
   trio (`flutter analyze`, `dart format` check, `flutter test`).
4. Manual acceptance (real daemon + phone, or emulator):
   * Set `display_name: "Studio Mac"` in
     `~/.config/mcremote/config.yaml`, restart the daemon
     (`mcremote service restart` or the platform equivalent), reconnect
     the phone: sessions-screen header reads
     "Connected to Studio Mac over <transport>"; Settings → Host shows
     "Studio Mac" above the endpoint.
   * Remove the key, restart, reconnect: the address label returns.
   * First pair against a named daemon (sign out, claim a new code):
     the sessions header shows the name without a second connect.
   * Relay transport (if configured): same label behaviour via relay.
5. Live-tagged CLI suites are unaffected and need not run (no provider
   surface changed).

## Verification

| MADR 0102 Confirmation item | Where proven |
|---|---|
| Loads from YAML and `MCREMOTE_DISPLAY_NAME` | `internal/config/config_test.go` (1.4) |
| Trimmed by `Load`; >128 runes rejected by `Validate` | `TestDisplayNameValidate` (1.5) + `TestLoadDisplayNameTooLong` (1.4) |
| Appears in `auth_ok` when set (v1 and v2); absent when empty | `TestAuthOKCarriesDisplayName`, `TestV2AuthOKCarriesDisplayName`, `TestAuthOKOmitsEmptyDisplayName` (2.4) |
| Appears in `pair_ok` when set | `TestPairOKCarriesDisplayName` (2.4) + Dart `claimPairCode` case (3.4) |
| Parity tests pass unexempted | Phase 1 verification run of `go test ./internal/cli/service/...` |
| Phone prefers name, falls back to hostname, resets on host change | 3.5, 3.6, 3.4 host-B case |
| Manual: header shows the name; unset restores the address; first pair shows the name | Phase 4 step 4 |

## Rollout and Rollback

**Rollout.** No migration: the key defaults to empty everywhere, the
setup-service seed never overwrites a live `config.yaml`, and old phones
ignore the new JSON field. Operators opt in by adding one YAML line and
restarting the daemon; each phone picks the name up on its next
authenticated frame (`auth_ok` on reconnect, `pair_ok` on first pair).

**Rollback.** Delete the key (or revert the commits) and restart the
daemon; phones fall back to the dialled-address label on their next
authenticated frame. The name is never persisted on the phone, so
nothing needs cleaning.

**Compatibility matrix.** New daemon + old phone: unknown JSON field
ignored. Old daemon + new phone: field absent → fallback path. Same
behaviour over direct-mesh and relay transports (auth and pair always
terminate at the daemon).
