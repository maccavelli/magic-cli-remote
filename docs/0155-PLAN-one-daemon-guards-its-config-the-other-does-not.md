---
status: in-progress
date: 2026-09-12
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

## Amendment — 2026-09-08: the predicate needs provenance, not a Config value

D7 and P1 describe `HasInlineSecret` as a method on `Config`. It cannot be one,
and the reason is a fact neither the MADR nor the plan accounted for.

**`MCREMOTE_RELAY_SECRET` and `--relay-secret` populate the same field as
`relay.secret` in YAML.** `load.go` calls `v.SetEnvPrefix("MCREMOTE")` and
`v.AutomaticEnv()`, and binds flags into the same viper instance, so by the
time a `Config` exists its `Relay.Secret` may have come from any of the three.
A method on the merged value cannot tell which — it would report a secret for
the arrangement `RelayConfig`'s own doc comment *recommends* (url and host_id
in YAML, secret from the environment), and fail the daemon over a config that
has nothing sensitive on disk.

**Corrected P1.** A package-level function taking a provenance oracle:

```go
var secretConfigKeys = []string{"relay.secret"}

func HasInlineSecret(inFile func(key string) bool) bool
```

Production passes `viper.InConfig`, which answers for the config file
specifically; tests pass a map. The list of secret keys stays in exactly one
place, which is what D7 and C3 were actually protecting — that part is
unchanged, and the guard test enforces it.

**P2 inherits a constraint from this.** The oracle is the viper instance, so
the check must run where that instance is in scope, not from a `Config` handed
onward. This makes P2's already-awkward ordering — parse, then decide — the
only workable one, rather than merely the preferable one.

## Amendment — 2026-09-08: what P2 actually needed

Four deviations from P2 as written, none of them changing D2, D3 or D8.

**One check site, not two.** The plan says "at both `ReadInConfig` sites". Both
branches converge on `usedConfigFile`, so the guard runs once against that,
after `Unmarshal`. This is strictly better: two call sites is two places for a
future edit to update one of.

**Two new files, because the repair is platform-specific.** The scope table
lists only `load.go`. `chmod` is meaningless on Windows — it toggles the
read-only attribute and touches no ACL — so a shared implementation would
report a repair that did not happen, which is exactly the failure MADR 0116 D22
exists to prevent. `repair_unix.go` / `repair_other.go` carry
`repairOwnerOnly` and `ownerOnlyRemedy`, split by build tag rather than by
`runtime.GOOS` (MADR 0144's rule). P4 moves the remedy beside
`appdirs.FileIsOwnerOnly` so mcrelay's three sites share it.

**The warning needed a logger, because diagnostics are never surfaced.**
`cfg.Diagnostics` is populated by `Load` and rendered by exactly one consumer —
`mcremote paths --json`. Nothing in `daemon.go` or `serve.go` reads it, so a
Diagnostic alone would be invisible to the operator of a running daemon and D3's
"logged warning" would not exist. The guard emits both: `slog.Default().Warn`
for the operator, and a `Diagnostic` with code `config_not_owner_only` for
`paths --json`.

*That diagnostics are collected and never logged is worth its own look. It is
not this record's subject and is not fixed here.*

**D9's sentence goes only in the fatal message.** The warning path exists
precisely because there is no credential in the file; telling that operator to
"rotate the exposed credential" would be advice about a secret that is not
there.

### The fixture was the hard part, and it is the part that matters

The first version of the tests skipped the tolerate-case and the fatal-case on
Windows, because making a file non-private there is not a `chmod`. That would
have left the security control this record exists to add **untested on the only
platform where its migration risk is real** — the same shape as the skips this
codebase has spent MADR 0147, 0151 and 0154 removing.

`configperm_windows_test.go` grants `*S-1-5-32-545` (BUILTIN\Users) read access
with `icacls`, chosen because `FileIsOwnerOnly` tolerates only the owner,
SYSTEM and Administrators, and Users exists on every Windows install. Crucially
it adds an **explicit** ACE rather than relying on inheritance: this host has a
third-party group inherited into `%TEMP%`, and a fixture built on that would
pass or fail according to which machine ran it.

All five tables now run on both platforms with no skips.

## Amendment — 2026-09-08 (second): P3 must not converge a directory it does not own

P3 says "converge the config directory next to the existing data-directory
call". Taken literally that is wrong, and dangerously so.

`--config` may point anywhere: a repository checkout, a home directory, a
shared ops path. `EnsurePrivateDir` severs inheritance and installs a DACL
naming only the owner and SYSTEM. Applying that to whatever directory happens
to contain the config would silently re-permission a directory the operator
keeps for other purposes — a far larger side effect than the problem being
fixed, and one no message would explain.

**Corrected P3.** The daemon converges `cfg.Paths.ConfigDir` — the directory
the product owns — and only when the config it actually read lives inside it:

```go
if cfg.ConfigFile != "" && cfg.Paths.ConfigDir != "" &&
    filepath.Dir(cfg.ConfigFile) == filepath.Clean(cfg.Paths.ConfigDir) {
```

A config elsewhere still gets the guard from P2; only the automatic repair is
withheld. That is the right split: detecting an exposure is always this
daemon's business, and re-permissioning someone else's directory never is.

The failure to converge is a warning rather than fatal. The daemon has not yet
established that anything is wrong — P2 already decided whether to start — so a
repair that could not run must not become a second, later refusal.

### Verified end to end on Windows, against the built binary

| Config | Result |
| --- | --- |
| exposed, carries `relay.secret` | **exit 1**: *"…contains a credential: move it under the private config directory, or re-run: mcremote setup-service --force; treat that credential as exposed and rotate it"* |
| exposed, no secret | **exit 0**, `WARN config file is not private`, layout printed |
| exposed, no secret, `--json` | **exit 0**, stdout is valid JSON carrying a `config_not_owner_only` diagnostic |

The message names no `chmod`, names no field, and carries D9's rotation advice
only where a credential exists. The warning goes to stderr, so `--json` stdout
stays machine-readable — checked, because a diagnostic that corrupts the JSON
it is reported in would be worse than no diagnostic.

## Amendment — 2026-09-12: P2's ordering made the POSIX fatal branch unreachable

Implements the MADR amendment of the same date. D3, D8 and D9 are unchanged;
this corrects how P2 composed them.

**Status correction.** This plan's frontmatter said `proposed` while P1–P3 had
already been committed (`de149c8`, `a372e06`, `71bc2e5`). It is `in-progress`:
P4 and P5 have not run, and P6 below is new.

### What went wrong in P2

P2 said: repair to `0600`, re-test, and *"if still not private: fatal when a
secret is present"*. On POSIX the repair succeeds for any file the process owns,
so the fatal branch could never be reached. MADR Confirmation 1 required
`exit non-zero` there. The fixture meant to reach that branch
(`makeUnrepairable`, unix) removed write permission from the directory. That
does not prevent `chmod(2)`, which needs only file ownership. All of P2's
verification ran on Windows, where both problems are invisible. CI run
`34717428526` found it on the first Linux execution.

### P6 — exposure is judged on the file as found (D3, D8, D9; MADR amendment 2026-09-12)

**In scope (the only files this phase may touch):**

* `internal/config/load.go` — `guardConfigFile` only
* `internal/config/configperm_test.go`
* `internal/config/configperm_unix_test.go`
* `internal/config/configperm_windows_test.go`
* this pair's two documents

**Change.** In `guardConfigFile`, record whether the file was private **before**
any repair, and decide fatal-or-warn from that:

1. `exposed := !FileIsOwnerOnly(path)`. If not exposed, return nil (unchanged).
2. If exposed, attempt the D8 repair and log it exactly as now (unchanged).
3. If exposed and `HasInlineSecret`: **fatal, whether or not the repair
   succeeded.**
   * Repaired: the message says the file *was* readable by another principal
     and contains a credential, that its permissions have been tightened to
     `0600`, and that the credential must be treated as exposed and rotated
     before starting again. No `chmod` remedy, since it has already been done.
   * Not repaired: the current message, with `ownerOnlyRemedy` and the rotate
     advice (unchanged).
4. If exposed, no secret, and repaired: return nil with no Diagnostic
   (unchanged; the tolerate test's POSIX branch already asserts this).
5. If exposed, no secret, and not repaired: WARN plus the
   `config_not_owner_only` Diagnostic (unchanged).

**Tests.**

* Delete `makeUnrepairable` from both platform files. Its unix premise is false,
  and the fatal case no longer depends on repair failing.
* `TestGuardConfigFileIsFatalWithAnInlineSecret` runs on every platform with no
  skip. It asserts: an error; the error contains `rotate` and the path; it names
  no field or value (C1). **On POSIX** it also asserts the file is now owner-only
  (the repair ran; A3 non-vacuous) and that the message says the permissions
  were tightened and does not tell the operator to `chmod`. **On Windows** it
  also asserts the file is still not private (no repair; unchanged remedy).
* The not-repaired fatal message keeps its coverage through the Windows run.
  `repairOwnerOnly` returns false there, which is the same code path a failed
  POSIX repair takes, so no test seam is added to production code.

### Contracts for P6

* **C6-1 — No production seam for tests.** No injectable repair function, no
  test-only flag. The two fatal messages are reached by the platforms that
  naturally produce them.
* **C6-2 — The secret-free path does not change.** P6 must not turn a repaired
  secret-free config into a refusal or a Diagnostic. This is the contract most
  at risk: "fatal when exposed" is one conditional away from "fatal when exposed
  and a secret is present".
* **C6-3 — Evidence from the platform that failed.** A Windows-only green run
  does not verify P6. That is the precise way P2 shipped broken. Linux
  verification is required before commit (below), not left to CI after push.

### Verification

On the Windows host, as the stability rule already requires:

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
GOOS=linux go vet ./internal/config/
go test ./internal/config/ -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

**And on Linux, as a non-root user**, in this host's WSL `Ubuntu-24.04`
distribution (uid 1000, ext4 `/tmp`). It has no Go today, so this step needs
the official `go1.26.6.linux-amd64` toolchain installed under the WSL user's
home. That download requires the owner's permission:

Run from a private clone on the WSL ext4 filesystem, never the Windows working
tree. `chmod` on `/mnt/c` (DrvFs) does not behave like POSIX, and a `git stash`
there would rewrite the Windows checkout.

```bash
# clone HEAD (pre-P6) into WSL
wsl -d Ubuntu-24.04 -- bash -lc 'rm -rf ~/mcr-0155 && git clone -q /mnt/c/Users/macsm/gitrepos/magic-cli-remote ~/mcr-0155'
# 1. baseline: the unmodified tree reproduces CI's failure on Linux
wsl -d Ubuntu-24.04 -- bash -lc 'cd ~/mcr-0155 && go test ./internal/config/ -run TestGuardConfigFile -count=1'
# 2. negative control: P6's TESTS over pre-P6 load.go must still fail
#    (copy only the three *_test.go files from the Windows tree)
# 3. the fix: copy P6's load.go too; everything passes
wsl -d Ubuntu-24.04 -- bash -lc 'cd ~/mcr-0155 && go test ./internal/config/ -count=1 && go test ./... -count=1'
# 4. the race lane, if the distro has a C toolchain
wsl -d Ubuntu-24.04 -- bash -lc 'cd ~/mcr-0155 && CGO_ENABLED=1 go test -race ./internal/config/ -count=1'
```

Steps 1 and 2 must fail with CI's exact message
(`…carrying a credential must be fatal`) before step 3 is trusted. Step 4 covers
the `ubuntu-latest` lane's `-race`, which cannot run on this Windows host (it
needs cgo, refused by MADR 0116 C7). If the distro has no `gcc`, record that
step 4 did not run. Do not install a compiler to make it pass; CI's race lane
remains the check for it.

### Acceptance for P6

| # | Criterion | Source |
| --- | --- | --- |
| A12 | Linux, non-root: an exposed config carrying a secret is fatal **and** the file is `0600` afterwards | D3, D8, MADR Confirmation 1 |
| A13 | That fatal message says the permissions were tightened, says to rotate, names the path, and names no field or value | D9, C1 |
| A14 | Linux: an exposed secret-free config is repaired and starts with no Diagnostic, unchanged | D3, C6-2 |
| A15 | Windows: all five `TestGuardConfigFile*` tables pass unchanged in outcome | D3 |
| A16 | The Linux negative control fails with CI's message before the fix | C6-3 |
| A17 | `go test ./...` green on `ubuntu-latest` (with `-race`), `ubuntu-24.04-arm` and `windows-latest` in CI after the owner pushes | CI run to be named in the execution record |

**A16 is the one most likely to be skipped.** Once the fix is written it feels
redundant. But P2's own tests passed everywhere they were run, and only a run on
the failing platform, against the unfixed code, proves the new test detects the
defect rather than coexisting with it.

### Delivery

P6 runs before P4. P4 moves the remedy text into a shared helper, so it should
move the corrected messages, not the broken ones. One commit, and no push
without an explicit instruction.

## Execution record — 2026-09-12: P6

P6 ran and is committed as `410e1d6`. It was not pushed. P4 and P5 have still not
run, so the plan stays `in-progress`.

| # | Result | Evidence |
| --- | --- | --- |
| A12 | **Met on Linux** | WSL Ubuntu-24.04, uid 1000, ext4 `/tmp`: fatal, the file is owner-only afterwards, and the message says `tightened to 0600` |
| A13 | **Met** | The same run: the message contains `rotate` and the path, names no field or value, and does not contain `chmod` |
| A14 | **Met** | `TestGuardConfigFileToleratesAnExposedConfigWithoutASecret` passes on Linux unchanged: repaired, no Diagnostic |
| A15 | **Met** | All five `TestGuardConfigFile*` pass natively on Windows; the fatal test's Windows branch asserts the file stays exposed |
| A16 | **Met** | Linux: the pre-P6 tree fails with CI's exact message, and so do P6's tests over pre-P6 `load.go` |
| A17 | **Pending** | Needs CI after the owner pushes |

Also on Linux: `go vet ./internal/config/`, `go test ./...` for the whole module,
and `CGO_ENABLED=1 go test -race ./internal/config/` all pass. On Windows:
`ci-windows-local` passes all checks, and gofmt is clean.

### What the amendment predicted incorrectly

1. **The stability rule's cross-build fails on this host as written.**
   `GOOS=linux go build ./...` errors inside `runtime/cgo`
   (`grp.h: No such file or directory`). `~/toolchains/mingw64` puts a gcc on
   `PATH`, so `CGO_ENABLED` defaults to 1 and Go tries to compile Linux's cgo
   runtime against Windows headers. With `CGO_ENABLED=0`, which is how CI and
   MADR 0116 C7 build, windows/linux/darwin builds, linux/darwin vet and
   linux/darwin test compilation all pass. The rule should say
   `CGO_ENABLED=0` explicitly. Whether P1–P3's "cross-build" ever ran on this
   host as written is not recorded.
2. **The distro has gcc.** P6 allowed for its absence and said to record it.
   Instead the race detector ran, which covers the `ubuntu-latest` lane locally.
3. **A fresh clone's `go test ./...` needs the whole module graph.** The plan
   did not anticipate that, and fetching it would have gone beyond the approved
   toolchain download. It was served from the Windows host's existing cache
   through `GOPROXY=file:///mnt/c/Users/macsm/go/pkg/mod/cache/download`, with
   no network fallback, `go.sum` still verifying every hash, and Linux-side
   `GOMODCACHE`/`GOCACHE`. The `go: downloading` lines in that run are unpacks
   from that cache.
4. **Driving WSL from Git Bash needs two workarounds** the verification block
   did not show. `wsl.exe` arguments containing `?`/`&` are mangled, and Git
   Bash rewrites `/mnt/c/...` into a Windows path unless `MSYS_NO_PATHCONV=1`.
   Every WSL step therefore ran as a script file invoked by path.

### Toolchain installed for this

`go1.26.6.linux-amd64.tar.gz` (66,890,545 bytes, SHA-256
`708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89`, matching the
go.dev release index), installed at `~/sdk/go1.26.6` in the WSL user's home.
There were no profile edits and no sudo, so it is not on `PATH` by default.
The verification clone is `~/mcr-0155`, with caches under
`~/.cache/mcr-0155`. It is disposable.
