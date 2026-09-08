---
status: proposed
date: 2026-09-08
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0155 — Guard mcremote's config the way the rest of the product guards secrets

Implements [0155-MADR-one-daemon-guards-its-config-the-other-does-not.md](0155-MADR-one-daemon-guards-its-config-the-other-does-not.md)
decisions D1–D9, closing findings F1–F8.

## Goal

1. mcremote's config directory is private on both platforms — an explicit DACL
   with inheritance severed on Windows, mode `0700` on POSIX.
2. A config file that is readable by another principal is detected on every
   read path.
3. Detection is fatal when the file carries an inline secret, and a logged
   warning otherwise.
4. On POSIX, a non-private config file is repaired to `0600` and the repair is
   logged.
5. The message tells the operator what to do on their platform, and that the
   secret may already be exposed.
6. mcrelay's behaviour is unchanged except for the message.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/config/config.go` | P1 | the `HasInlineSecret` predicate (D7) |
| `internal/config/load.go` | P2 | the check on both read paths (D2, D3, D8) |
| `internal/cli/service/setup.go` | P3 | `EnsurePrivateDir` for the config dir (D1) |
| `internal/daemon/daemon.go` | P3 | converge on the read path (D1) |
| `internal/appdirs/owneronly_message.go` (new) | P4 | the shared, platform-accurate message (D4, D9) |
| `internal/relay/fileconfig.go` | P4 | use it at the three existing sites (D4) |
| `internal/config/load_test.go` | P2 | the check's tables |
| `internal/cli/service/setup_test.go` | P3 | config dir privacy |
| `internal/appdirs/*_test.go` | P4 | the message, and F7's convergence claim |
| `docs/0154-PLAN-*.md` | P5 | amend the false claim (D6) |

### Out of scope

* **mcrelay's fatal-on-any-non-private behaviour** (D5). Its config exists only
  to carry host credentials; there is no benign case to preserve, and it does
  not reach users through a self-update feed.
* **Rotating the secret.** D9 says the message mentions it; nothing automates
  it.
* **Removing `relay.secret` from the config surface** (MADR option D). Kept in
  view for when a second secret field appears.
* **W-1, W-2, W-4** from the 2026-09-08 Windows drive.

## Stability rule

Every phase ends with:

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
```

and, before any push, from this Windows host:

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

The cross-build matters here: P4 adds a file whose two implementations differ
per platform, which is the shape that broke MADR 0150 P1.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — no secret is ever logged.** The warning and the error name the *path*
and, on Windows, the offending *trustee*. Never a value, never a fingerprint,
never the field's contents. `providerauth`'s CLI output already holds this line
and it is not weakened here.

**C2 — repair never widens access.** D8's chmod moves a file toward `0600` and
never away from it. A file already `0600` or tighter is untouched, and no phase
may chmod a directory, a symlink, or anything the process does not own.

**C3 — the fatal path keys on the predicate, never on a field name.** D7 exists
because the next secret field will be added by someone who has not read the
record; a `strings.Contains(body, "secret")` or a literal `cfg.Relay.Secret !=
""` at the call site defeats it.

**C4 — mcrelay's semantics do not change.** P4 changes what its refusal *says*,
not when it fires. Its existing tests pass unmodified.

**C5 — the daemon still starts when the config is absent.** The check applies
to a config that exists and is readable; no config at all remains valid, as it
is today.

**The contract most at risk is C1**, and the tempting breach is specific: the
most useful possible warning would say *which* setting is exposed, and
`relay.secret` is the only candidate, so naming it feels harmless. It is not —
the message goes to a log that is shipped, aggregated and pasted into issues,
and "the file at X contains a registration secret" is a map for anyone who
later gets read access. Name the file; let the operator open it.

## Dependency and delivery order

P1 first: the predicate is what P2's check calls, and it is pure.

P2 next, because the check is the record's subject and everything after it is
either repair or wording.

P3 after P2, deliberately in that order even though convergence *fixes* what
the check *detects*. Landing repair first would hide whether the check works:
on Windows every config would silently become compliant, and the check would
have nothing to catch. Detect first, watch it fire, then repair.

P4 is independent and could land anywhere; it is placed here so the message
lands once, shared, rather than being written twice.

P5 last: it is the correction to a record, and it should describe what shipped.

## Implementation Steps

### P1 — the predicate (D7; closes F2 in part)

`internal/config/config.go`:

```go
// HasInlineSecret reports whether this config carries a credential in the file
// itself, as opposed to receiving it from the environment or a flag.
//
// It is a method on Config rather than a field test at the call site because
// the check that uses it is a security control, and the next secret-bearing
// field will be added by someone who has not read MADR 0155. Adding a field
// here is a one-line change; noticing that a call site needed updating is not
// (0155 D7, C3).
func (c Config) HasInlineSecret() bool { ... }
```

Today it returns whether `c.Relay.Secret` is non-empty. The doc comment is the
deliverable as much as the body.

**Verification.** A table test: empty config false; `relay.secret` set true.
Plus a guard test that fails if `Config` grows a field whose mapstructure tag
matches `secret|token|password|api_key` and the predicate was not updated —
reflection over the struct, in the spirit of MADR 0149 D11. If that proves
fragile, drop it and say so in the execution record rather than shipping a
guard that rots.

### P2 — check both read paths (D2, D3, D8; closes F1)

`internal/config/load.go`, at both `ReadInConfig` sites (`:99`, `:107`), after
the file is known and before it is parsed:

* `appdirs.FileIsOwnerOnly(path)`;
* if private, proceed unchanged;
* if not: on POSIX, chmod `0600` and log the repair (D8, C2); re-test;
* if still not private: fatal when `cfg.HasInlineSecret()`, warning otherwise
  (D3).

The ordering is load-bearing and awkward: the predicate needs the parsed
config, and the check wants to run before parsing. Parse first, then decide —
reading a world-readable file is not the harm; continuing to *serve* with an
exposed credential is. Say so in a comment, because the natural instinct is to
gate before the read.

**Verification.**

```bash
go test ./internal/config/ -run TestConfigPermission -count=1 -v
```

Tables: private config → no warning, no error; non-private without a secret →
warning, starts; non-private with a secret → fatal; POSIX non-private → file is
`0600` afterwards. The last needs `testexec.SkipIfNoUnlinkOpenFile`-style
platform handling, or an `appdirs.EnsurePrivateDir` fixture as PLAN 0154 P2
used — a `t.TempDir()` file on Windows carries whatever the host inherits.

### P3 — make the config directory private (D1; closes F4)

Two call sites:

* `internal/cli/service/setup.go:1080` — `os.MkdirAll(dir, 0o700)` becomes
  `appdirs.EnsurePrivateDir(dir)`. The `Chmod`-if-loose block below it becomes
  redundant on POSIX and wrong to keep on Windows; remove it and let the
  primitive own the question.
* `internal/daemon/daemon.go` — converge the config directory next to the
  existing data-directory call, so an installation that never re-runs
  `setup-service` still repairs.

**Verification.** On Windows, a config directory created under a path carrying
a foreign inherited group must come out with inheritance severed, and a config
file already inside it must become owner-only — that is MADR F7, and P4's test
asserts it directly. On POSIX, mode `0700`.

### P4 — one message, correct on both platforms (D4, D9; closes F6)

A shared helper beside `FileIsOwnerOnly` that renders the refusal:

* POSIX: the existing wording, `chmod 0600`.
* Windows: name the trustee that failed `noForeignTrustee`, and give the real
  remedy — move the file under the private config directory, or re-run
  `setup-service`. Never `chmod`.
* Both: one sentence that the secret may already have been exposed and should
  be rotated (D9).

`internal/relay/fileconfig.go`'s three sites call it. mcrelay's *behaviour* is
untouched (C4).

**Verification.** `go test ./internal/relay/ -count=1` unmodified. A message
test per platform. Plus `TestEnsurePrivateDirConverges`, which pins MADR F7 —
the measured claim this whole plan's migration story rests on, and currently
evidence in a document rather than in the suite.

### P5 — correct the 0154 record (D6; closes F5)

PLAN 0154's execution record says `--config` is "unusable on Windows outside a
directory someone deliberately made private". Amend it: the refusals were
caused by one machine-specific group, a home-directory config was accepted, and
the check was correct. Point at 0155 F5.

**Verification.** The amendment is additive; 0154's original text is unedited.

## Verification (whole plan)

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go test ./... && go test -race ./...
grep -rn 'HasInlineSecret' internal/ | grep -v _test     # predicate + one call site
grep -rn 'Relay.Secret' internal/config/load.go          # expect none (C3)
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | A non-private config carrying a secret is fatal | D3, F2 |
| A2 | A non-private config with no secret warns and starts | D3 |
| A3 | On POSIX the file is repaired to `0600` and the repair is logged | D8 |
| A4 | The config directory is private on both platforms | D1, F4 |
| A5 | On Windows, convergence makes a pre-existing config file owner-only | D1, F7 |
| A6 | The Windows message names the trustee and never says `chmod` | D4, F6 |
| A7 | Both messages mention possible exposure | D9 |
| A8 | No secret value appears in any log or error | C1 |
| A9 | The fatal path calls the predicate, not a field | D7, C3 |
| A10 | mcrelay's tests pass unmodified | D5, C4 |
| A11 | The daemon still starts with no config at all | C5 |

**A5 is the criterion most likely to be skipped**, because MADR F7 already
records the measurement and re-proving it in a test feels redundant. It is not:
that measurement is the entire reason option B was rejected and the migration
is believed safe, and it currently lives in prose. If Windows ever stops
re-propagating, this plan's migration story fails silently.

**A8 is the one most likely to be violated while trying to be helpful**, per
C1's note.

**A3 is the one most likely to pass vacuously** — a test that chmods the file
itself and then asserts `0600` proves nothing. It must start from a
world-readable file the *code* repairs.

## Rollout and Rollback

Behaviour change on both platforms, reaching users through a self-update feed.
Each phase reverts independently; P2 is the only one that can stop a daemon,
and reverting it alone restores today's behaviour exactly.

The riskiest interaction is P2 landing without P3 on a Windows host whose
config directory carries a foreign group — the daemon would refuse to start if
the config holds a secret. That is the intended control, but it is also why P3
exists and why the two should ship together rather than across a release
boundary.

Deliberately not mitigated: a config that holds a secret and cannot be made
private will stop the daemon. That is the decision, not an oversight.

## Deferred (named, so they are not mistaken for oversights)

* **Making mcremote fatal on any non-private config** (MADR option B). The
  better end-state, unavailable today because the default location fails on at
  least one real host. Revisit once D1's convergence has been observed working
  across more than one machine.
* **Removing `relay.secret` from the config surface** (option D). More
  attractive the moment a second secret field appears.
* **Automating rotation.** D9 mentions exposure; nothing acts on it.
* **A guard that every secret-bearing field reaches the predicate.** P1
  proposes one by reflection; if it proves fragile it should be dropped rather
  than nursed, and the deferral recorded — MADR 0149 D11's bar is that a class
  guard earns its place at the third instance, and this would be the first.
