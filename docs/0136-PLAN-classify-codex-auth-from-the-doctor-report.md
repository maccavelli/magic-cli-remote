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

Not started. `status: proposed` — awaiting approval to execute.
