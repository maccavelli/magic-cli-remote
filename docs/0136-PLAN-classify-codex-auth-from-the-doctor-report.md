---
status: proposed
date: 2026-09-03
associated-madr: "0136-MADR-classify-codex-auth-from-the-doctor-report.md"
---

# Implement: read Codex credential reality from `codex doctor --json`

Associated MADR: [0136-MADR-classify-codex-auth-from-the-doctor-report.md](0136-MADR-classify-codex-auth-from-the-doctor-report.md)

## Goal

Replace a probe that cannot fail with one that reports the resolved storage
backend and the health of the stored credential as separate facts, so that a
broken Codex credential escalates again while a genuinely unprotectable one
stays quiet. Retire `RealityExternal`; make `RealityLoggedOut` reachable; keep
MADR 0134's `StateExternal` and re-base it on evidence that supports it.

## Scope

**In scope:**

* `internal/provider/codex/store_reality.go` — the probe and the
  classification.
* `internal/provider/codex/adapter.go` — `CredentialIsExternal` maps from the
  new classification.
* `internal/provider/codex/authstore.go` — `DetectCredentialStore` becomes the
  fallback for when the CLI cannot be run, not the primary source.
* `internal/provider/codex/logout.go` — `backupProjection` and
  `describeReality` follow the retired/renamed values.
* Tests, including a new fixtures directory holding the four recorded
  `doctor --json` reports.

**Explicitly out of scope:**

* `internal/providerauth`. `StateExternal`, `RealityReporter` and
  `recoverIdle`'s branch are unchanged — only what feeds them changes.
* MADR 0134's decision. Its state keeps its meaning; this replaces its trigger.
* The manifest format, so nothing here interacts with MADR 0135.
* Repairing the reporting host's Codex credential. That is an operator action,
  noted in Rollout, and deliberately not automated.
* Any use of `doctor --json` beyond `checks["auth.credentials"]`. The rest of
  the report is not consumed and must not become a dependency.

## Implementation Steps

### Phase 1 — record the fixtures before writing any code

1. Capture `codex doctor --json` verbatim into
   `internal/provider/codex/testdata/doctor/` for the four measured states:
   `no-credentials.json`, `incomplete-file.json`, `env-provided.json` (the
   reporting host's shape, which has `auth env vars present`), and
   `keyring-backend.json`. Redact nothing beyond what Codex already redacts;
   these reports contain no token material, which must be re-checked by eye
   before committing.
2. Record in this plan the `codex-cli` version and `schemaVersion` each fixture
   came from, so a future mismatch is diagnosable rather than mysterious.

Commit at the end of the phase.

### Phase 2 — the parser, with no behaviour change

3. Add a typed reader for the report: `schemaVersion`, then
   `checks["auth.credentials"]` → `status`, `summary`, and the `details` map.
   Details values are JSON **strings**, including `"false"` and `"true"`, and
   `stored auth issue` is an **array**; decode accordingly rather than assuming
   Go types.
4. Every failure returns a single sentinel meaning "cannot tell":
   unreadable output, malformed JSON, unrecognised `schemaVersion`, missing
   check, missing `auth storage mode`. No partial interpretation.
5. Unit-test the parser against the Phase 1 fixtures. Nothing calls it yet.

Commit at the end of the phase.

### Phase 3 — classify from it

6. Rewrite `ObserveCredentialStore` to run `codex doctor --json` and map per the
   MADR: unrecognised → `RealityUnknown`; backend not `File` →
   `RealityUnsupported`; backend `File` → `RealityFileProtected` when a usable
   stored credential exists, `RealityLoggedOut` when none exists, and **not
   external** when one exists but is incomplete.
7. Decide "usable stored credential" from the fixtures, not from prose: on
   0.152.1 that is `stored ChatGPT tokens`, `stored API key` or
   `stored agent identity` being `"true"`, with `status` corroborating. Write
   the rule so a future field addition fails closed.
8. Retire `RealityExternal`: remove the constant and every branch, updating
   `describeReality` and `backupProjection`. `CredentialIsExternal` returns true
   for `RealityUnsupported` only.
9. Keep `DetectCredentialStore`, called only when the probe returns the
   "cannot tell" sentinel — for example when `bin` is empty. Its existing
   `auto`-is-unsupported behaviour stays as-is on that path and is documented as
   the pessimistic fallback it is.
10. Keep the existing cache and window untouched.

Commit at the end of the phase.

### Phase 4 — tests, each seen to fail first

Run every new assertion against a deliberately broken input before trusting it —
a `git worktree` at the previous commit, never by dirtying the tree.

11. One test per fixture asserting its classification. The `incomplete-file`
    and `env-provided` cases **must classify as not-external**; against the
    current tree both classify as external, which is the shipped regression.
12. A test asserts `env-provided` — which has `auth env vars present` — never
    yields `external` or `unsupported`, pinning that a per-process fact stays
    out of a host-wide state.
13. Tests for unrecognised `schemaVersion`, malformed JSON, and a missing
    `auth.credentials` key, each asserting `RealityUnknown`.
14. A test asserting `RealityLoggedOut` is produced by `no-credentials.json` —
    the value that is unreachable today.
15. An end-to-end test in `internal/daemon` mirroring
    `TestCredentialGuardPassesTheCodexBinary`: a stub binary printing
    `incomplete-file.json` for `doctor --json` must leave the provider in
    `recovery_required`, and one printing `keyring-backend.json` must reach
    `StateExternal`.

Commit at the end of the phase.

### Phase 5 — verify on the reporting host

16. `make install`. Expect the operator-decision warning for codex to **return**
    and the manifest to leave `external`. This is the intended outcome, not a
    regression: the credential really is broken.
17. Repair the credential as an operator: `codex login` with `OPENAI_API_KEY`
    unset, so `auth.json` gets full ChatGPT tokens and refresh metadata.
18. Confirm `codex doctor` reports `auth.credentials` ok, the daemon returns the
    provider to `idle` at the next checkpoint with no sign-in from the phone,
    and a `refresh` generation is recorded after the next token rotation.

## Verification

```bash
make pre-add-check
go test ./internal/provider/codex/... ./internal/providerauth/... \
        ./internal/daemon/... -count=1
go test ./... -count=1
make vet
make lint
```

Host checks, read-only:

```bash
codex doctor --json | python3 -c "import json,sys; c=json.load(sys.stdin)['checks']['auth.credentials']; print(c['status'], '|', c['summary'])"
grep -a "credential state\|keeps its credential outside" ~/Library/Logs/mcremote/mcremote.err.log | tail -5
python3 -c "import json;print(json.load(open('$HOME/.local/share/mcremote/provider-auth/codex/manifest.json'))['state'])"
```

### Acceptance criteria

* Every command above passes with no findings.
* Test 11 fails against the previous commit for both the incomplete and
  env-provided fixtures, with the output recorded verbatim in this plan.
* Tests 13 and 14 pass, demonstrating the conservative fallback and the newly
  reachable classification.
* On the host, before the operator repair: state is **not** `external` and the
  warning is back.
* On the host, after the repair: `doctor` reports ok, manifest state `idle`,
  and no sign-in was performed from the phone.
* `grep -rn "RealityExternal" internal/` returns nothing.
* `git diff --stat` touches only the files named in Scope.

## Rollout and Rollback

**Rollout.** Daemon-side only, effective on the next restart. No phone update,
no protocol change, no manifest change — so this does not interact with the
downgrade constraints recorded in MADR 0133 and 0134.

Expect the codex warning to come back on hosts whose credential is genuinely
broken. That is the point, and it should be stated plainly in the handoff so it
is not mistaken for a regression.

**Rollback.** Revert the phase commits. `StateExternal` may already be recorded
in a manifest; it remains a valid state after a revert, and the pre-0136 probe
would simply re-enter it. No credential, generation, or manifest field is
altered by this work.

**Operator note.** The reporting host needs the Phase 5 step 17 repair
regardless of whether this plan is executed. The daemon cannot see the
`OPENAI_API_KEY` that masks the fault in an interactive shell, so Codex sessions
started from the phone are running against the incomplete credential today.

**Known fragility, accepted.** A Codex release that renames the check id, the
`details` keys, or bumps `schemaVersion` degrades this to `RealityUnknown` —
safe, but silently less useful. The fixtures record the version they came from
so the mismatch is diagnosable; nothing detects it automatically, and building
that is not in scope.

## Execution Record

### Phase 1 — 2026-09-03, complete

Fixtures in `internal/provider/codex/testdata/doctor/`, all captured from
**codex-cli 0.152.1**, all reporting **`schemaVersion: 1`** (recorded inside
each file as `codexVersion` / `schemaVersion`, so a future mismatch is
diagnosable):

| fixture | `status` | `summary` | `auth storage mode` |
| --- | --- | --- | --- |
| `file-protected.json` | `ok` | auth is configured | `File` |
| `no-credentials.json` | `fail` | no Codex credentials were found | `File` |
| `incomplete-file.json` | `fail` | stored credentials are incomplete | `File` |
| `env-provided.json` | `warning` | auth is provided by environment, but stored credentials are incomplete | `File` |
| `keyring-backend.json` | `fail` | no Codex credentials were found | `Keyring` |

### Deviations

**2026-09-03 — fixtures are the `auth.credentials` check, not the whole
report.** Step 1 said "verbatim". The full report carries the operator's
absolute home paths, an MCP endpoint, endpoint-security product names and git
repository details, none of which the parser reads and none of which belongs in
version control. Each fixture therefore holds `schemaVersion`, `codexVersion`
and `checks["auth.credentials"]` only, with the one remaining path value
(`auth file`, also unread) replaced by `/home/user/.codex/auth.json`. Every
other value is byte-verbatim. Checked by eye and by grep: no key material and no
host paths remain.

**2026-09-03 — a fifth fixture was added.** The plan named four, none of which
is a *healthy* host, yet step 7 has to define "usable stored credential" from
evidence. `file-protected.json` was produced by running
`codex login --with-api-key` in a throwaway `CODEX_HOME` with the obviously fake
key `sk-test-NOT-A-REAL-KEY-0136-fixture`; the home was deleted afterwards. It
is what settles the rule: a usable credential is exactly `status == "ok"`, and
everything else fails closed.

### Phase 2 — 2026-09-03, complete

`doctorreport.go` adds a typed reader: `schemaVersion`, then
`checks["auth.credentials"]` → `status`, `summary`, `details`. Details are
decoded as `map[string]json.RawMessage` and read through `detailString`, because
the values are JSON **strings** even for booleans (`"false"`) and
`stored auth issue` is an **array** — a value of any other shape reads as empty
rather than failing the parse, so a detail this code does not need cannot break
the classification.

Every uninterpretable input returns the single `errDoctorUnusable` sentinel:
malformed JSON, unrecognised `schemaVersion` (including absent, which decodes as
0), a missing `auth.credentials` key, and a missing or blank
`auth storage mode`.

`authCredentials` deliberately carries facts rather than a conclusion —
`StorageMode`, `Usable`, `EnvVarsPresent`, `Status`, `Summary` — so the mapping
to a `StoreReality` stays reviewable in one place in Phase 3.

`Usable` is `status == "ok"`, which the Phase 1 fixtures settle: the healthy
host reports `ok` / "auth is configured", and all four unusable shapes report
`fail` or `warning`.

**Tests.** `TestParseDoctorAuthFixtures` pins one parse per fixture.
`TestParseDoctorAuthFailsClosed` covers ten uninterpretable inputs and ends with
a control assertion that the real fixture still parses — so the ten are failing
for their own reasons and not because the parser rejects everything.
`TestStoredAuthIssueArrayDoesNotBreakParsing` pins the array-valued detail.

```text
go test ./internal/provider/codex/... -count=1  -> ok (10.050s)
make pre-add-check                              -> 715 file(s) clean
```

Nothing calls the parser yet; behaviour is unchanged, as the phase requires.

### Deviations (continued)

**2026-09-03 — execution paused between Phase 2 and Phase 3 to fix two
credential-destroying test escapes.** Not part of this plan, and not deferred:
the owner supplied the Codex source mid-execution, which led to establishing
that the `{}` stub in `~/.codex/auth.json` was written by **our own test
suite**, not by Codex.

`internal/provider/codex/auth_test.go`'s `fakeCodex` stub ran
`printf '{}\n' > "$HOME/.codex/auth.json"`, and
`TestClearCredentialRunsLogout` did not isolate `HOME`. Proven byte-identical
(`7b7d 0a`) to the reporting host's file. Fixed in commit `4eac448` by moving
the isolation into the helper, with `TestFakeCodexCannotEscapeItsSandbox` as the
guard.

A follow-up canary audit of the whole suite found a second escape:
`TestRunAllowsClientKeyWithTLS` calls the real `Run()` with
`config.Defaults()`, which reconciles the provider credential stores under
`$HOME` — rewriting `~/.config/goose/config.yaml` and tightening `~/.codex`,
`~/.grok` and `~/.config/mcremote` to 0700 on every run. Fixed in commit
`52db4bb` with a package-level `TestMain` isolating `HOME`, the `XDG_*`
variables, `CODEX_HOME` and `GROK_HOME`. Re-audit: canary home intact, one
accepted residual (the `.config/mcremote` directory mode, mcremote's own
directory, mode-only).

**This weakens this plan's motivating evidence without invalidating its
decision.** The exit-code probe really is broken — `codex login status` exits 0
for a signed-out home — and `RealityLoggedOut` really is unreachable, so the
parser and classification remain correct and worth landing. What is no longer
supported is the belief that any host legitimately reaches the "external store"
state: the `{}` shape that motivated `RealityExternal`, in MADR 0074 §15.13 and
again in MADR 0134, was self-inflicted. Whether `RealityExternal` and
`StateExternal` should exist at all is a question for the doc amendment the
owner has sequenced after this plan.

**Also found, not fixed:** `TestUnstableLiveDefersInsteadOfWedging` (MADR 0133,
Phase 3) fails under a loaded full-suite run while passing in isolation. Its
churn goroutine writes every 10 ms, and under load the file can settle long
enough for `stableObservation` to adopt it, breaking the manifest-unchanged
assertion. A genuine flake, reported rather than absorbed.

### Phase 3 — 2026-09-03, complete

`ObserveCredentialStore` now runs `codex doctor --json` and maps the report:
unrecognised → the config fallback (which yields `RealityUnknown` on a file
backend); backend not `File` → `RealityUnsupported`; backend `File` →
`RealityFileProtected` when usable, `RealityLoggedOut` when nothing is stored,
and a new **`RealityBroken`** when something is stored but unusable.

`RealityBroken` was not in the plan. It is what step 6's "not external" needs to
be *called* so callers can branch on it — the plan described the outcome without
naming the value. It is deliberately distinct from `RealityUnsupported`: the
file is the store and mcremote can protect it; what is wrong is the credential,
which is MADR 0133's escalation case.

"Nothing stored" is read structurally, not from the summary text: Codex omits
every `stored *` detail when there is nothing to describe, so
`HasStoredMaterialEvidence` keys off the presence of those details rather than
matching prose that carries no stability contract.

`RealityExternal` is retired — `grep -rn "RealityExternal" internal/` is empty.
`cliIsAuthenticated` and `fileHoldsUsableCredential` are gone with it.
`describeReality` gained a sentence for broken; `backupProjection` now overrides
the manifest only for `RealityUnsupported`, because reporting "unsupported" for
a broken credential would hide the honest `recovery_required`.
`DetectCredentialStore` survives as the no-probe fallback.

**A real bug found while wiring it.** `CredentialIsExternal` returned the
descriptive `ErrUnsupportedBackend` alongside `true`, and
`providerauth.credentialIsExternal` treats *any* error as "cannot tell" — so
keyring hosts escalated anyway. Caught by
`TestCredentialGuardPassesTheCodexBinary` failing with
`state = recovery_required, want external`. The observation's error is now
discarded there: for `RealityUnsupported` it is descriptive, not a failure to
observe, and `CheckBackend` is where that text reaches an operator.

**The old tests had to be rewritten, because they asserted the defect.**
`store_reality_test.go` contained `authJSON: {}, statusExit: 0, want:
RealityExternal` — the case that blessed it. They now drive a stub emitting the
Phase 1 fixtures via a control file the test can rewrite between calls, so the
cache-window test can change the CLI's answer. The stub exits **non-zero** for
doctor, as the real one does when it finds problems, so a regression to
trusting an exit code breaks every case.

**Verification.**

```text
go test ./internal/... ./cmd/... -count=1  -> 42 packages ok, 0 FAIL
make pre-add-check                         -> 717 file(s) clean
grep -rn "RealityExternal" internal/       -> (empty)
```

### Deviations (continued)

**2026-09-03 — a MADR 0133 defect surfaced mid-phase and was fixed under its
own record.** `TestUnstableLiveDefersInsteadOfWedging` failed about 1 run in 8.
I first called it a flaky test; that was wrong. `os.WriteFile` truncates before
writing, and an empty file's fingerprint is a *stable* value, so two reads that
both land in a truncate window agree and escalate. The owner chose to treat a
zero-length LIVE as unstable; amended into MADR 0133 and its plan, implemented
in `reconcile.go`, and now 0 failures in 12 runs.
`TestZeroLengthDefersButCorruptEscalates` pins the boundary: empty defers,
content-that-does-not-parse still escalates.

### Deviations (continued)

**2026-09-03 — CI (Windows) caught two portability defects Phase 3's local
verification could not see.** `make pre-add-check` and `go test ./...` were run
on macOS only; the push to `master` triggered `Go (windows/amd64)`, which
failed both `internal/daemon` and `internal/providerauth`:

```text
--- FAIL: TestCredentialGuardPassesTheCodexBinary (0.19s)
    state = recovery_required, want external: the guard must hand the adapter a binary, ...
--- FAIL: TestCommitBlocksOnTheProviderNativeLockFile/base_path_derives_the_provider's_lock_file
    Commit failed for the wrong reason: fsutil: lock ...auth.json.lock: lock busy for more
    than 200ms: The process cannot access the file because another process has locked ...
```

Both are test-fixture bugs, not classification bugs.

`TestCredentialGuardPassesTheCodexBinary` and
`TestCredentialGuardEscalatesABrokenCredential` (added in Phase 3, in
`internal/daemon/credentials_test.go`) wrote a raw `#!/bin/sh` stub and executed
it directly. Windows has no shebang mechanism — the codex package's own Phase 1
tests already established this pattern (`testexec.SkipIfNoPOSIXShell`,
documented at MADR 0116 P11) and my daemon tests simply didn't use it. Fixed by
switching to `testexec.WriteShellStub`, the existing helper that writes the
stub and skips the test on Windows in one call.

`TestCommitBlocksOnTheProviderNativeLockFile` (added in Phase 1 of MADR 0133,
predates this plan) asserted the failure text contained `"flock"` — the Unix
wrapper's word (`lock_unix.go`). Windows's `LockFileEx`-based implementation
(`lock_windows.go`) wraps the same failure as `"fsutil: lock %s: %w"`, which
never contains "flock". Fixed by asserting on `"lock"`, the substring both
wrappers share; re-run locally to confirm it still discriminates the
blocked/unblocked cases in the same test.

Both fixed in one commit. `go test ./internal/... ./cmd/... -count=1` (42
packages) and `make pre-add-check` (717 files) pass on macOS; the Windows-only
failure mode cannot be reproduced locally and is confirmed only by the next CI
run.

### Phases 4-5 — remaining

Phase 4's assertions landed with Phase 3 rather than after it, because the
existing tests encoded the old behaviour and could not be left failing while new
ones were added alongside. Steps 11-14 are covered by the rewritten
`store_reality_test.go` and step 15 by the two `internal/daemon` guard tests.
Still outstanding: running them against the previous commit to record the
fail-first output, and all of Phase 5 (verify on the reporting host).
