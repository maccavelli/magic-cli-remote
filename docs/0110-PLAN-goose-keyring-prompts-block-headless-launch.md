# Implement a Goose keyring-backend setting in mcremote configuration

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 MD060 -->

Associated MADR: [0110-MADR-goose-keyring-prompts-block-headless-launch.md](0110-MADR-goose-keyring-prompts-block-headless-launch.md)

Plan status: **approved by the Project Owner on 2026-08-21.** MADR 0110 is
accepted and execution of P1-P5 is authorized.

**P0 is already complete** (2026-08-21) because it was research only, and its
finding set this plan's shape. F13 came back confirmed: Goose *does* read
`GOOSE_DISABLE_KEYRING` from `config.yaml`, on both construction paths. Per
P0's own instruction MADR D9 was reconsidered and adopted, so the enforcement
mechanism is a reconciled key in Goose's config file rather than an environment
variable on the child. P1 through P5 are written against that mechanism.

## Goal

Give mcremote a first-class setting that decides which secret backend Goose
uses, so a phone-initiated Goose session starts with nobody at the host.

The work is complete when all of the following are true:

1. `providers.goose.keyring_disabled` exists in mcremote's configuration,
   defaults to `true`, is registered with viper like every other Goose key, is
   documented, and appears in the example configs.
2. When the setting is effective, mcremote reconciles `GOOSE_DISABLE_KEYRING`
   in `~/.config/goose/config.yaml` so Goose reads
   `~/.config/goose/secrets.yaml` instead of the OS keyring — for every Goose
   launch, mcremote's and the operator's alike.
3. mcremote never silently switches a host into a backend that holds no
   credentials. A host whose file store is empty while its Goose config
   declares providers keeps the keyring and reports one actionable message.
4. mcremote's own reading of `GOOSE_DISABLE_KEYRING` matches Goose's exactly,
   including the two cases where they differ today (MADR 0110 F12), so the
   backend mcremote reports is the backend Goose uses.
5. An operator who sets `keyring_disabled: false` gets the line removed and
   Goose back on its own default; one who set `GOOSE_DISABLE_KEYRING` by hand
   keeps their setting untouched; one who set it in the daemon's environment
   stays in control.
6. No secret value reaches a log, an error string, a transcript, or a receipt.
7. The existing Goose provider, catalog, auth, and config tests stay green, and
   the `live_goose` suite passes against the installed binary.

## Evidence Baseline

This plan is written against commit `a28f0dc` on `master` and the facts
recorded in MADR 0110 on 2026-08-21.

| Area | Observed fact | Plan consequence |
| --- | --- | --- |
| Default backend | `GOOSE_DISABLE_KEYRING` is unset on this host; Goose uses the Keychain (MADR 0110 F1, F11). | The new setting changes a real default, so it needs the P3 guard rather than a bare flip. |
| Prompt cause | The `goose` binary is ad-hoc, linker-signed, `TeamIdentifier=not set` (F3). | No amount of mcremote configuration makes "Always Allow" stick. Only changing the backend helps. |
| Config writing | `credstore.SetGooseActiveProvider` (`write.go:170-207`) already rewrites one scalar in Goose's `config.yaml` by line surgery, deliberately avoiding a YAML round-trip so the operator's comments and formatting survive. | P2 adds a sibling writer for `GOOSE_DISABLE_KEYRING` using the same technique, so mcremote gains no new way of touching that file. |
| Config shape | `GooseProviderConfig` (`internal/config/config.go:537-551`) embeds `ACPProviderConfig` and adds `WithBuiltins` and `StreamCoalesceMs`. | The new key is a sibling of `StreamCoalesceMs`, not a change to the shared ACP struct: it is Goose-specific. |
| Default-true booleans | `providers.goose.enabled` is a plain `bool` defaulted true through `v.SetDefault` (`internal/config/load.go:305`). | A plain `bool` plus `SetDefault(true)` is sufficient. Viper distinguishes "absent" from an explicit `false`, so no `*bool` and no tri-state is needed. |
| Auth reporting | `authStatus` and friends are free functions bound into the spec at `internal/provider/goose/goose.go:57-60`, calling `credstore.GooseKeyringDisabled` (`goose/auth.go:38,146,179,211`). | Because both Goose and mcremote now read the same key from the same file, these need no new plumbing. P4 only proves the agreement; the spec threading the earlier draft required is dropped. |
| Goose semantics | `base.rs:206` disables on env **presence** (`is_ok()`); `base.rs:299-301` disables on a config value of `true`, `"true"`, or `"1"` only. | mcremote must mirror both branches exactly. `isFalsey` matches neither, which is the F12 defect P3 fixes. |
| Comment stripping | `splitYAMLScalar` (`credstore.go`) strips a trailing ` #` from a scalar value, and YAML strips it for Goose. | An ownership marker comment on the written line is invisible to both readers, which is what makes selective removal possible. |
| Example configs | `configs/config.example.yaml:142-158` carries the full annotated Goose block; `configs/config.prod.example.yaml:109-115` carries a shorter one. | Both are updated; the annotated one carries the explanation. |

## Scope

### Files expected to change

* `internal/config/config.go` — the new field and its default.
* `internal/config/load.go` — the `v.SetDefault` registration.
* `internal/config/config_test.go` — default and override coverage.
* `internal/provider/credstore/write.go` — write the key into Goose's config.
* `internal/provider/credstore/credstore.go` — align `GooseKeyringDisabled`
  with Goose's exact semantics, now known from P0.
* `internal/daemon/daemon.go` — evaluate the guard and reconcile before the
  Goose provider is constructed.
* `configs/config.example.yaml`, `configs/config.prod.example.yaml`
* `docs/config.md`

### Files to add

* `internal/provider/goose/keyring.go` and `keyring_test.go` — the effective
  value, the guard rule, and its typed outcome.

### Explicit non-goals

* Migrating existing secrets out of the OS keyring (MADR 0110 D3). The operator
  populates the file store with Goose's own tooling.
* Enrolling Goose's file store into the MADR 0074 credential coordinator.
* Any change to how Goose is signed, installed, or updated.
* Any change to another provider's configuration or spawn path. In particular
  `acphttp` is untouched: the environment mechanism this plan originally used
  was dropped when P0 confirmed F13, so OpenCode's shared spawn path does not
  change at all.

## Fixed Interfaces and Invariants

### The configuration key

```yaml
providers:
  goose:
    keyring_disabled: true
```

* Type `bool`, mapstructure tag `keyring_disabled`, default `true`.
* Registered as `v.SetDefault("providers.goose.keyring_disabled", true)`.
* It is mcremote's own key with mcremote's own boolean semantics. It is
  deliberately **not** named `GOOSE_DISABLE_KEYRING`, so it cannot inherit
  Goose's presence-versus-truth ambiguity (MADR 0110 F12/D8).

### Enforcement mechanism

mcremote reconciles the `GOOSE_DISABLE_KEYRING` key in
`~/.config/goose/config.yaml` to match `providers.goose.keyring_disabled`.

Goose's own semantics, confirmed in P0 and mirrored exactly:

```text
env  GOOSE_DISABLE_KEYRING  -> presence alone disables (base.rs:206, is_ok())
yaml GOOSE_DISABLE_KEYRING  -> disables only for true, "true", or "1"
                               (base.rs:299-301, keyring_disabled_value)
```

`keyring_disabled: true` writes the key with an ownership marker:

```yaml
GOOSE_DISABLE_KEYRING: true  # managed by mcremote (providers.goose.keyring_disabled)
```

`keyring_disabled: false` **removes** that line, so Goose falls back to its own
default. Removal and an explicit `false` are identical to Goose, so removal is
chosen for leaving the file as it was.

The marker is what makes removal safe. mcremote removes the key only when the
marker is present; a `GOOSE_DISABLE_KEYRING` the operator wrote by hand is left
alone and logged. Neither reader sees the marker: YAML strips the comment, and
mcremote's `splitYAMLScalar` already strips a trailing ` #`
(`credstore.go`).

### Exact signatures and constants

These are fixed so the implementation is not a design exercise. Any deviation
is a plan amendment, not an implementation detail.

```go
// internal/provider/credstore/write.go
const GooseKeyringMarker = "# managed by mcremote (providers.goose.keyring_disabled)"

// SetGooseKeyringDisabled reconciles the GOOSE_DISABLE_KEYRING key.
//
// disabled==true writes "GOOSE_DISABLE_KEYRING: true  <marker>".
// disabled==false removes the line, but only when it carries the marker.
// Returns wroteChange=false when the file already matched.
// Returns ErrGooseKeyringOperatorOwned when an unmarked line is present and
// removal was requested; the file is left untouched.
func SetGooseKeyringDisabled(path string, disabled bool) (wroteChange bool, err error)

var ErrGooseKeyringOperatorOwned = errors.New(
    "goose config has an operator-set GOOSE_DISABLE_KEYRING; leaving it alone")

// internal/provider/credstore/credstore.go — split to match Goose exactly.
// env branch: presence only (base.rs:206). config branch: true/"true"/"1"
// only (base.rs:299-301).
func GooseKeyringDisabled(configPath string) bool
func gooseKeyringDisabledValue(v string) bool
```

```go
// internal/provider/goose/keyring.go
type GuardOutcome string

const (
    OutcomeSwitch        GuardOutcome = "switch"          // key reconciled
    OutcomeHold          GuardOutcome = "hold"            // file store empty
    OutcomeHostControls  GuardOutcome = "host_controls"   // env var present
    OutcomeOperatorOwned GuardOutcome = "operator_owned"  // unmarked line
    OutcomeNoChange      GuardOutcome = "no_change"       // already correct
)

type Result struct {
    Outcome GuardOutcome
    Reason  string // safe for logs and for the phone; never a secret or a path
}

func EffectiveKeyringDisabled(cfgValue bool) (disabled, hostControls bool)
func Reconcile(cfgValue bool) (Result, error)
```

### Exact operator-facing strings

Fixed so tests can assert them and so the operator sees the same words in the
log and on the phone.

| Outcome | Reason string |
| --- | --- |
| `hold` | `goose secrets are not in secrets.yaml, so switching would start goose with no credentials; run: GOOSE_DISABLE_KEYRING=1 goose configure` |
| `host_controls` | `GOOSE_DISABLE_KEYRING is set in the daemon environment; goose config not modified` |
| `operator_owned` | `goose config has a hand-set GOOSE_DISABLE_KEYRING; mcremote is not managing it` |
| `switch` | `goose will read secrets.yaml` |
| `no_change` | (empty; nothing is logged) |

### Effective value

Resolved once, at daemon construction:

```text
1. If GOOSE_DISABLE_KEYRING is present in the daemon's own environment, the
   host has spoken to Goose directly and presence alone already disables the
   keyring. Reconcile nothing, and log that the host is in control.
2. Otherwise reconcile the config key to providers.goose.keyring_disabled.
3. If that means disabling the keyring, apply the guard below first.
```

Step 1 exists because the environment branch is presence-only: mcremote cannot
express "enabled" through it, and writing a config key that the environment
overrides would be a setting with no effect.

### The guard

The guard answers one question: *would switching the backend leave Goose with
no credentials?*

```text
switch when: the file store holds at least one secret
        or: the Goose config declares no configured provider (a cold host)
hold  when: the file store is empty AND the Goose config declares providers
```

The hold case is the one that matters: it means the secrets live in the keyring
and nowhere else, so switching would start Goose with nothing. It is evaluated
from files mcremote already reads — `credstore.ReadGooseSecretNames` and
`credstore.ReadGooseConfig` — with no keyring access and no subprocess, so it
cannot itself trigger a prompt.

On hold, mcremote:

* does not write `GOOSE_DISABLE_KEYRING` into Goose's config;
* logs exactly one message naming the remediation command; and
* reports a typed state the phone can render, so the operator learns this from
  the phone rather than from a host log they cannot see.

### Secret handling

The guard reads secret **names** only, through the existing
`ReadGooseSecretNames`, which is already written so values never leave it. No
step in this plan reads, logs, or transports a secret value.

## Implementation Steps

### Phase and commit contract

Phases run P0 through P5 in order. Within each phase:

1. write the failing test first;
2. implement only that phase's files;
3. run the phase's focused tests, then the affected package tests;
4. run `gofmt` on changed Go files;
5. run `make pre-add-check FILES="<every changed Go file>"` before staging —
   the repository's `PreToolUse` hook blocks a commit whose staged Go files
   fail it, so this is a gate rather than a courtesy;
6. run `go test -race` for every changed package;
7. stage only that phase and commit with `git commit --no-edit`. Never `-m`:
   the message comes from the repository's `prepare-commit-msg` hook.

If a phase reveals work outside this plan, stop and amend rather than
implementing opportunistically.

---

### Phase P0 — Settle what Goose actually reads

This phase writes no product code. It exists because two facts this plan
depends on are recorded in the MADR as assumptions, and building on an
unverified assumption is how the last credential change went wrong.

#### What driving the binary already established (2026-08-21, goose 1.47.0)

Empirical probing was attempted first, on the principle that the installed
binary is better evidence than source that may not match it. Results:

* `XDG_CONFIG_HOME` cleanly isolates Goose's config directory. `goose info`
  reports the redirected `Config dir` and `Config yaml`, and creates no files.
  Every probe below ran fully isolated from the operator's real Goose config.
* **`GOOSE_DISABLE_KEYRING` set in `config.yaml` does reach Goose's resolved
  configuration map.** `goose info -v` printed it under "goose Configuration"
  alongside `GOOSE_MODEL` and `GOOSE_PROVIDER` — and those two were *derived*
  from `active_provider` and `providers.<id>.model` rather than written into
  the file, which proves the output is the resolved map and not a raw dump of
  `config.yaml`. This is real evidence for F13, but it is not conclusive: it
  shows the key is available to the config layer, not that the keyring branch
  reads it from there rather than from `std::env::var`.
* **`goose info --check` is not a usable probe. Do not repeat this.** It
  completes identically with the keyring enabled, with it disabled, with a
  fake key in `secrets.yaml`, and with no secrets file at all — and raises no
  keychain dialog even when the keyring is fully enabled. It never reads the
  secret store, so it cannot discriminate backends. Several hours of A/B
  comparison on this command measure nothing.

Two safe-probe constraints were established and must be respected by whatever
method P0 finally uses:

* Any operation that makes Goose **read** a secret with the keyring enabled
  will raise a keychain dialog on the host. Acceptable only with the operator
  present and forewarned.
* Any operation that makes Goose **write** a secret with the keyring enabled
  writes to the operator's real `goose`/`secrets` keychain item. This must not
  be attempted: the item is a single blob holding every Goose secret, so a
  careless write can destroy working credentials.

#### Steps

1. Read Goose's source at the installed version —
   `crates/goose/src/config/base.rs` around the keyring branch. This is now the
   recommended route: it is zero-risk, takes minutes, and answers both
   questions exactly, whereas every safe behavioural probe found so far is
   either non-discriminating or requires a host dialog. Establish:
   * **F12** — is `GOOSE_DISABLE_KEYRING` tested for presence, or parsed as a
     boolean? Specifically, does `GOOSE_DISABLE_KEYRING=0` disable the keyring?
   * **F13** — is the key read from `config.yaml`, or only from the process
     environment?
2. Record both answers in the MADR as confirmed facts, replacing the
   "assumption, not confirmed" markers.
3. Decide the two consequences that follow:
   * If F13 shows `config.yaml` **is** honoured, raise MADR D9 for
     reconsideration before P1 starts — writing the key there removes the
     split-store consequence entirely, and would change this plan's shape.
   * If F13 shows it is **not** honoured, `credstore.GooseKeyringDisabled`'s
     `config.yaml` branch (`credstore.go:113-130`) reports a state Goose is not
     in. Fix it in P4 and note it as a defect fixed in passing.

2b. If the source is unavailable, the fallback is a behavioural probe that
   *does* read a secret — for example starting a session against a configured
   provider — run twice in an isolated `XDG_CONFIG_HOME`: once with the key in
   `config.yaml` only, once with neither. A dialog in the first case proves
   `config.yaml` is not honoured. Requires the operator's consent, because it
   raises that dialog on their machine.

#### Verification

Findings are written into the MADR with the exact file and line consulted, or
with the exact probe and observed outcome if the behavioural route is used.

#### Acceptance

No product file has changed. Both assumptions are now confirmed facts or the
plan is amended before proceeding.

---

### Phase P1 — The configuration key

#### Tests first

1. A config test proving `keyring_disabled` defaults to `true` when the key is
   absent from the file entirely.
2. A config test proving an explicit `keyring_disabled: false` survives load —
   this is the case a plain `bool` would get wrong if defaults were applied
   after unmarshal rather than through viper.
3. A test proving the key is registered with `SetDefault`, matching the
   existing convention that every Goose key is reachable through an env
   override.

#### Production steps

1. Add to `GooseProviderConfig` (`internal/config/config.go`), directly after
   `StreamCoalesceMs`:

   ```go
   // KeyringDisabled makes Goose read its secrets from
   // ~/.config/goose/secrets.yaml instead of the OS keyring, by setting
   // GOOSE_DISABLE_KEYRING for the Goose child process.
   //
   // Default true because the daemon is headless: on macOS the keyring
   // prompts for the login password on every read, and an ad-hoc signed
   // Goose binary cannot hold a durable "Always Allow" grant, so a
   // phone-initiated session blocks on a dialog nobody is there to answer
   // (MADR 0110).
   KeyringDisabled bool `mapstructure:"keyring_disabled"`
   ```

2. Set the default in the `Default()` literal alongside the other Goose fields.
3. Register it in `internal/config/load.go` next to line 313:
   `v.SetDefault("providers.goose.keyring_disabled", d.Providers.Goose.KeyringDisabled)`.
4. Add the key to `configs/config.example.yaml` in the Goose block after
   `stream_coalesce_ms`, with a comment stating what it does, why it defaults
   true, and that the file store must already hold the secrets. Add the bare
   key to `configs/config.prod.example.yaml`.
5. Document it in `docs/config.md` in the Goose provider table.

#### Verification

```bash
go test -count=1 ./internal/config
go test -race -count=1 ./internal/config
```

#### Acceptance

An absent key yields `true`; an explicit `false` yields `false`; the example
configs and `docs/config.md` describe the key; no behaviour has changed yet,
because nothing consumes the value.

---

### Phase P2 — Write the key into Goose's config

#### Tests first

1. Replacing an existing top-level `GOOSE_DISABLE_KEYRING` line leaves every
   other byte of the file identical, including comments, blank lines, key
   order, and indentation.
2. An absent key is prepended, so it cannot land inside another block's
   indented body.
3. An **indented** `GOOSE_DISABLE_KEYRING` under some other mapping is ignored,
   matching how `GooseKeyringDisabled` already refuses nested keys.
4. A missing `config.yaml` is created containing just the key, mode `0600`.
5. Writing a value already present is a no-op: the file is not rewritten, so a
   daemon restart does not churn a stable file.
5b. Toggling off removes a **marked** line and leaves the file otherwise
   byte-identical, including the absence of any blank-line scar.
5c. Toggling off leaves an **unmarked** operator-authored line untouched and
   reports that it did so.
5d. The marker survives a read: `GooseKeyringDisabled` returns `true` for a
   marked `GOOSE_DISABLE_KEYRING: true  # managed by mcremote …` line, proving
   the trailing comment does not corrupt the value on either reader.
6. The literals written are `true` and `false`, asserted against a local
   reimplementation of Goose's `keyring_disabled_value` rule, so a later edit
   cannot silently write something Goose reads as the opposite.

#### Production steps

1. Add `credstore.SetGooseKeyringDisabled(path string, disabled bool) error` to
   `internal/provider/credstore/write.go`, modelled directly on
   `SetGooseActiveProvider` (`write.go:170-207`) — line surgery, not a YAML
   round-trip, for exactly the reason documented there: the operator edits this
   file by hand and a round-trip would reformat it and drop their comments.
2. `disabled == true` writes the key with the marker comment, replacing an
   existing top-level line or prepending when absent.
3. `disabled == false` **removes** the line, but only when it carries the
   marker. An unmarked line is left in place and reported to the caller so the
   daemon can log that it is deferring to an operator-authored setting.
4. Read the current state first and return without writing when it already
   matches, so the common path touches nothing.
5. Reuse the existing `writeFileAtomic` and `splitYAMLScalar` helpers so
   atomicity and parsing stay identical to the neighbouring writer.

#### Verification

```bash
go test -count=1 ./internal/provider/credstore -run 'TestSetGooseKeyringDisabled'
go test -race -count=1 ./internal/provider/credstore
```

Required test names, one per numbered case above, in
`internal/provider/credstore/write_test.go`:

```text
TestSetGooseKeyringDisabledReplacesInPlace
TestSetGooseKeyringDisabledPrependsWhenAbsent
TestSetGooseKeyringDisabledIgnoresIndentedKey
TestSetGooseKeyringDisabledCreatesMissingFile
TestSetGooseKeyringDisabledIsNoOpWhenUnchanged
TestSetGooseKeyringDisabledRemovesMarkedLine
TestSetGooseKeyringDisabledRefusesUnmarkedLine
TestSetGooseKeyringDisabledMarkerSurvivesRead
TestSetGooseKeyringDisabledWritesGooseReadableLiterals
```

#### Acceptance

The key is set, replaced, or created without disturbing anything else in the
file; an unchanged value writes nothing; the file stays `0600`.

---

### Phase P3 — Effective value, guard, and Goose-exact semantics

#### Tests first

1. Effective-value table: config true, config false, host env present set to
   `1`, host env present set to `0`, host env present but empty. Every case
   where the host env is present must resolve to "host controls, reconcile
   nothing" — including `0`, because Goose's env branch is presence-only and
   mcremote must not pretend otherwise.
2. Guard tests over the four combinations of (file store holds secrets, Goose
   config declares providers), asserting switch or hold for each.
3. **Semantics-parity tests** for `credstore.GooseKeyringDisabled`:
   * config value `true`, `"true"`, `"1"` → disabled;
   * config value `false`, `"0"`, `"no"`, `"off"`, `"maybe"`, `""` → enabled;
   * env var present with any value, including `0` and empty → disabled.
   The `"maybe"` and `"0"`-in-env cases are precisely where today's `isFalsey`
   handling disagrees with Goose (MADR 0110 F12). Both must now agree.
4. A guard test proving no secret value reaches the returned state or any log
   line, using a sentinel value in the file store.
5. A test proving the guard runs no subprocess and touches no keyring, so it
   can never raise the prompt this work exists to remove.

#### Production steps

1. Split `credstore.GooseKeyringDisabled` into the two branches Goose uses:
   * environment: `os.LookupEnv` presence, ignoring the value entirely;
   * config file: a new `gooseKeyringDisabledValue(string) bool` accepting only
     `true`, `"true"`, and `"1"`.
   Stop using `isFalsey` for this key. Leave `isFalsey` for its other callers
   and comment why this key does not use it.
2. Add `internal/provider/goose/keyring.go` with:
   * `EffectiveKeyringDisabled(cfgValue bool) (disabled, hostControls bool)`;
   * `GuardOutcome` — `switch`, `hold_secrets_elsewhere`, or `host_controls` —
     each with a `Reason` safe for logs and for the phone;
   * `Reconcile(cfgValue bool) (GuardOutcome, error)`, which resolves the
     effective value, applies the guard, and calls
     `credstore.SetGooseKeyringDisabled` only on `switch`.
3. The hold reason names the remediation verbatim:
   `GOOSE_DISABLE_KEYRING=1 goose configure`.

#### Verification

```bash
go test -count=1 ./internal/provider/goose -run 'TestEffectiveKeyring|TestGuard|TestReconcile'
go test -count=1 ./internal/provider/credstore -run 'TestGooseKeyringDisabled'
go test -race -count=1 ./internal/provider/goose ./internal/provider/credstore
```

Required test names:

```text
internal/provider/goose/keyring_test.go
  TestEffectiveKeyringDisabledTable
  TestEffectiveKeyringHostEnvAlwaysWins        // incl. "0" and ""
  TestGuardHoldsWhenSecretsElsewhere
  TestGuardSwitchesOnColdHost
  TestGuardSwitchesWhenFileStorePopulated
  TestGuardNeverLogsASecret                    // sentinel value
  TestGuardRunsNoSubprocessAndTouchesNoKeyring
  TestReconcileOutcomes                        // one case per GuardOutcome

internal/provider/credstore/credstore_test.go
  TestGooseKeyringDisabledMatchesGooseConfigRule   // true/"true"/"1" only
  TestGooseKeyringDisabledMatchesGooseEnvRule      // presence only
  TestGooseKeyringDisabledRejectsArbitraryStrings  // "maybe" -> enabled
```

The last three are the F12 parity tests. `TestGooseKeyringDisabledRejects
ArbitraryStrings` and the `"0"`-in-environment case in
`TestGooseKeyringDisabledMatchesGooseEnvRule` are the two that fail against
today's `isFalsey` implementation, and are therefore the ones that must be
written before the fix.

#### Acceptance

Every resolution and guard combination is covered; mcremote's reading matches
Goose's on the cases where they previously differed; the host's own variable
always wins; no secret and no subprocess in the path.

---

### Phase P4 — Confirm reporting agreement

P0 collapsed most of this phase. Because Goose and mcremote now read the *same*
key from the *same* file, and P3 makes mcremote's reading match Goose's rule,
`authStatus` needs no new plumbing — `credstore.GooseKeyringDisabled` is
already authoritative. The original plan threaded an effective value through
the spec to work around a variable only the child would see; that mechanism is
gone, and so is the threading.

What remains is proving the agreement rather than assuming it.

#### Tests first

1. A parity test driving both readings from one fixture directory: for each F12
   case, assert `GooseKeyringDisabled` returns what Goose's verified rule
   returns.
2. A test proving `authStatus` reports the file store as authoritative after a
   `switch` reconciliation, and the keyring after a `false` one.
3. A test proving `setCredential` and `clearCredential` return
   `ErrGooseKeyringManaged` only when the keyring is genuinely in use, so an
   operator already on files is never told to run `goose configure`.

#### Production steps

1. None expected beyond P3. If a test here fails, the fix belongs in P3's
   semantics function, not in a second code path.
2. Update `ErrGooseKeyringManaged`'s message to name the new setting as the
   supported route, since the operator now has one.

#### Verification

```bash
go test -count=1 ./internal/provider/goose -run 'TestAuthStatusBackend|TestGooseParity'
go test -race -count=1 ./internal/provider/goose
```

Required test names in `internal/provider/goose/auth_test.go`:

```text
TestGooseParityWithGooseRule        // shared fixture, all F12 cases
TestAuthStatusBackendAfterSwitch
TestAuthStatusBackendAfterDisableFalse
TestSetCredentialRefusesOnlyWhenKeyringLive
```

#### Acceptance

mcremote's reported backend equals Goose's actual backend in every combination
of config value and host environment, proven by shared-fixture parity tests
rather than by inspection.

---

### Phase P5 — Daemon wiring, docs, and acceptance

#### Tests first

1. A daemon construction test proving `Reconcile` runs before the Goose
   provider is registered, so the first engine already sees the intended
   backend.
2. A test proving a `hold` outcome logs exactly one message containing the
   remediation command, and does not write the config file.
3. A test proving a `host_controls` outcome writes nothing and logs once.
4. A test proving no other provider's construction changed.

#### Production steps

1. In `internal/daemon/daemon.go`, inside the existing
   `if cfg.Providers.Goose.Enabled` block and **before** `gp :=
   goose.NewWithLogger(acpCfg, log)` (currently `daemon.go:159`), call
   `goose.Reconcile(cfg.Providers.Goose.KeyringDisabled)`. Placing it before
   construction is what guarantees the first engine — including a prewarmed
   one — already sees the intended backend.
2. Log the outcome once: info for `switch` and `host_controls`, warn for
   `hold`. Never log the config file's contents.
3. A reconciliation error is non-fatal — log and continue with Goose enabled.
   Failing to write a preference must not take the provider down.
4. Surface the outcome in the Goose auth state so the phone renders the hold
   case, rather than leaving the operator to find it in a host log.
5. Update `docs/config.md` and the provider matrix.

#### Verification

```bash
make test
make race
make vet
make pre-add-check FILES="<changed Go files>"
make live-goose
cd apps/mobile && flutter analyze && flutter test
git diff --check
```

#### Acceptance

* With a populated file store, a phone-initiated Goose session starts and
  completes a prompt with nobody at the host, and no keychain dialog appears.
* `~/.config/goose/config.yaml` contains `GOOSE_DISABLE_KEYRING: true` and is
  otherwise byte-identical to before, comments included.
* The same succeeds after the `goose` binary is replaced with a rebuilt one.
* A Goose run by hand at a terminal uses the same secret store as the one
  mcremote spawns — the property that motivated this mechanism.
* With an empty file store and configured providers, the config is not written,
  one actionable message is logged, and the phone shows the typed state.
* `keyring_disabled: false` writes `GOOSE_DISABLE_KEYRING: false` and Goose
  returns to the keyring.
* A host with the variable in its own environment sees no config write.
* A recursive search of logs, transcripts, and receipts contains no secret.

#### Manual acceptance procedure

Run on the affected host, in this order. Steps 1-3 need someone at the machine;
step 4 is the one that proves the fix.

```text
1. Populate the file store, the one interactive step (MADR 0110 D3):
     GOOSE_DISABLE_KEYRING=1 goose configure
   Confirm ~/.config/goose/secrets.yaml now exists and is 0600.

2. cp ~/.config/goose/config.yaml /tmp/goose-config.before

3. Restart the daemon. Then:
     diff /tmp/goose-config.before ~/.config/goose/config.yaml
   Expect exactly one added line, carrying the marker comment.

4. Walk away from the host. From the phone, start a Goose session and send a
   prompt. It must complete. No keychain dialog may appear on the host.

5. Toggle off: set providers.goose.keyring_disabled: false, restart, then:
     diff /tmp/goose-config.before ~/.config/goose/config.yaml
   Expect no differences at all — the line is removed, not set to false.
```

## Verification

### Phase acceptance matrix

| Phase | Required observable outcome |
| --- | --- |
| P0 | F12 and F13 are confirmed facts; D9 reconsidered if `config.yaml` is honoured. |
| P1 | Key exists, defaults true, explicit false survives, documented and templated. |
| P2 | Key written, replaced, or created with the rest of the file untouched; an unchanged value writes nothing. |
| P3 | Effective value and guard covered; mcremote's semantics match Goose exactly; host variable wins. |
| P4 | Reported backend equals actual backend, proven by shared-fixture parity. |
| P5 | Headless Goose session works; hold path is actionable; full matrix green. |

### Cross-cutting tests that must stay green

* Goose provider, catalog, auth, and command-table tests.
* OpenCode and the shared `acphttp` path, which this plan no longer touches —
  a regression there would mean the scope leaked.
* Config load, default, and env-override tests.
* `live_goose` against the installed binary.

## Rollout and Rollback

### Rollout

1. Ship with the default `true` and the guard active. A host with secrets only
   in the keyring is held, not broken, and is told what to do.
2. After the operator populates the file store, the guard switches on the next
   daemon start with no further configuration.

### Rollback

Set `providers.goose.keyring_disabled: false` and restart. The daemon removes
the line it added, Goose falls back to its own default of keyring-enabled, and
the config file returns to exactly its pre-change contents.

Rollback leaves no residue, which is the point of the marker: mcremote removes
only what it wrote. An operator who had set `GOOSE_DISABLE_KEYRING` by hand
keeps their line either way.

`secrets.yaml` is Goose's file, written by Goose, and is never touched here.

## Implementation Log

Intentionally empty until execution is approved.

| Phase | Commit | Verification | Notes |
| --- | --- | --- | --- |
| P0 | **Complete** (no commit — research only) | Source read at goose 1.47.0, matching the installed binary | F12 and F13 confirmed; F14 withdrawn; MADR D1/D9 revised to the config-file mechanism. |
| P1 | Not started | Not run | |
| P2 | Not started | Not run | |
| P3 | Not started | Not run | |
| P4 | Not started | Not run | |
| P5 | Not started | Not run | |

## Review Checklist

**Owner review 2026-08-21 — the three open questions are answered:**

1. *Is writing a key into the operator's Goose config the right mechanism?* —
   **Yes.** D1/D9 stand as written.
2. *Marker, or decline to manage once a hand-set line is seen?* — **Marker.**
   D10 is locked accordingly.
3. *Are the fixed signatures, outcome strings, and test names specific enough?*
   — **Yes.** They are now binding; a deviation is a plan amendment.

The remaining items were settled by the plan itself and are recorded for the
implementer to re-check at execution time:

* P0 is complete and both assumptions are confirmed. Given F13, is writing a
  key into the operator's Goose config file the right mechanism? **Answered
  yes above.**
* Is the default `true` safe, given that the guard is what prevents it from
  starting Goose with no credentials?
* Is the guard free of any keyring access or subprocess, so it cannot itself
  cause the prompt this work removes?
* Does mcremote's reported backend always match the one in use (P4)?
* Is the host's own `GOOSE_DISABLE_KEYRING` always honoured over ours?
* Does P3 make mcremote's reading of the key match Goose's exactly, including
  the two cases where they differ today?
* Does toggling off leave the operator's `config.yaml` byte-identical to
  before mcremote touched it?
* Is the ownership marker the right way to avoid deleting a hand-set line, or
  should mcremote decline to manage the key at all once it sees one?
  **Answered: marker.**
* Are the fixed signatures, outcome strings, and test names specific enough
  that two implementers would produce the same thing? **Answered yes.**
