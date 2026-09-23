---
status: in-progress
date: 2026-09-22
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0167 — Contribute a notification overflow policy to `acp-go-sdk`

Implements [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
decisions **D1–D15**, closing findings **F1–F43**.

**The deliverable is a pull request against `coder/acp-go-sdk`, not a change to how this project
builds.** Our `go.mod` ends this plan byte-identical to how it started.

Three facts established while planning, recorded here rather than discovered mid-execution:

* **#40 already builds the whole options surface. [measured]** Its diff adds
  `ConnectionOption func(*connectionConfig)`, threads `opts ...ConnectionOption` through all three
  constructors, and ships a 153-line test — `4 files changed, 190 insertions(+)`. #50 independently
  exports `ErrNotificationQueueOverflow` **and** `ErrPeerDisconnected`. Our PR must compose with
  these, not restate them (MADR F26, D7).
* **Neither PR changes the overflow *policy*. [measured]** Both keep
  `shutdownReceive(errNotificationQueueOverflow)`. That is the gap (MADR F27).
* **Upstream's conventions differ from ours in one way that will cause a wrong "fix". [source]**
  Their `AGENTS.md` says *"Run `make fmt` or `gofumpt -w .`"* and *"Target Go 1.21 idioms"*, with
  tests named `TestType_Action` and table-driven where possible. **This repository forbids
  `gofumpt`** (`AGENTS.md`: "plain `gofmt`, *not* `gofumpt`"). Fork code follows *their* rules;
  our code follows ours. C4 exists because these two are easy to cross.

**Redaction.** The fork's owner account is written `<owner>` throughout, per this repository's
"Host identifiers never appear in records" rule.

## Goal

Observable end states:

1. A branch in the fork adds an overflow-policy option to `acp-go-sdk`, and
   **`go test ./...` passes with every pre-existing upstream test file unmodified** — including
   `TestConnectionFailsFastOnNotificationQueueOverflow` (`acp_test.go:836`), which guards the
   default.
2. `git diff --stat upstream/main -- acp_test.go connection_cancel_test.go
   connection_notification_barrier_test.go` shows **no changes to existing test files**.
3. With the new policy selected, `GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run
   StalledPump -count=20` shows the transport **surviving** — and with the policy unset it still
   fails, proving the default is unchanged.
4. This repository's `go.mod` and `go.sum` are unchanged: `git diff --exit-code go.mod go.sum` is
   clean at every commit boundary, and no commit in this plan contains a `replace`.
5. A PR description exists that states the protocol impact, the commands run, and the measured
   evidence — and has been shown to the owner before anything is posted.
6. `initialize` no longer goes through our raw path, and records 0038/0039/0081/0137 no longer
   assert the four things that are false at v0.13.5.

## Scope

### In scope (the only files any phase may touch)

**The fork (`<owner>/acp-go-sdk`, a separate repository) — the PR's content:**

* `connection.go` — the policy option and the overflow branch
* `client.go`, `agent.go` — only if #40's variadic plumbing is not already present on the base
* `connection_overflow_policy_test.go` *(new)*
* `client.go` + `client_connection_test.go` *(new, P7, separate branch for the accessor)*

**This repository — local fixes only, no build change:**

* `internal/provider/acpagent/acpagent.go` (P2)
* `internal/provider/acpagent/rewind_test.go` (P2)
* `internal/provider/acpagent/initialize_test.go` *(new, P2)*
* `docs/decisions/0167-MADR-*.md`, `docs/decisions/0167-PLAN-*.md`
* `docs/spec/0038-MADR-*.md`, `docs/spec/0039-MADR-*.md`, `docs/spec/0081-PLAN-*.md`,
  `docs/spec/0137-PLAN-*.md`, `docs/decisions/0166-MADR-*.md`

### Out of scope

* **`go.mod` / `go.sum` in this repository.** Not "avoid if possible" — **forbidden** (C3). The
  temporary `replace` P4 needs is created, used and reverted without ever being staged.
* **Deleting our `unsafe.Pointer` cast.** It depends on the accessor existing upstream, so it waits
  for P7 to merge (MADR D10). Removing it against a fork-only API would be the very coupling D1
  rejects.
* **Adopting the policy in our own code.** Even if the PR merges, switching our connection to the
  new policy is a later decision with its own trade (notification loss), not part of this plan.
* **The sequencer panics (MADR F13) and the 10 MiB frame cap (F14).** Both are candidate PRs; each
  needs its own evidence, and bundling them lowers the merge odds of all three.
* **Schema currency (F22), per-engine blast radius (0166 F3), switching SDKs, our own client.**

## Stability rule

**In the fork**, matching upstream's `AGENTS.md` rather than ours:

```bash
gofumpt -w .            # AGENTS.md asks for gofumpt; CI's treefmt enforces plain gofmt (F31). gofumpt is a superset, so this satisfies both.
make test               # go test ./... plus "all examples still build"
go test ./... -cover    # upstream asks that coverage not regress
go test -race ./...
make check              # treefmt --fail-on-change + README guard, as CI runs it
```

`mise install` provisions upstream's toolchain (`treefmt`, `gofumpt`, `golangci-lint`). If `mise`
or `treefmt` is not installed on this host, run the underlying `go test ./...` and record in the PR
exactly which commands were run and which were unavailable — **do not claim `make check` passed if
it was not run.** Installing a toolchain writes outside the repository and needs an explicit ask.

**In this repository**, unchanged from the house rule:

```bash
make pre-add-check && gofmt -l cmd internal && go vet ./...
go test ./internal/provider/acpagent/ && make race
make -n ci-windows && make ci-windows
git diff --exit-code go.mod go.sum        # C3, at every commit boundary
```

Commits: one per phase, message from the `prepare-commit-msg` hook via `git commit --no-edit`,
never `-m`. **Fork commit subjects must additionally satisfy upstream's rule** — imperative mood,
first line under ~72 characters. If the hook produces a subject that violates that, stop and ask:
do not hand-write the message, and do not push a non-conforming one.

**`git push` is not permitted by this plan.** P6 and P7 cannot complete without one, and both must
stop and ask in the turn they occur. Opening a PR is an outward-facing publication to a
third-party project: the text is shown and approved first, every time.

## Cross-cutting contracts

**C1 — No pre-existing upstream test file may be modified, and the default path must stay
byte-identical.** This is the PR's entire argument: it extends the maintainer's design instead of
reversing it (MADR D3). If a change requires editing `acp_test.go`, the design is wrong, not the
test.

**C2 — Every new assertion is observed failing before it is relied on**, against a deliberately
broken input, on a copy or scratch branch — never by dirtying the tree and running
`git checkout --`.

**C3 — This repository's `go.mod`/`go.sum` are never committed changed.** P4's `replace` is
created, used, and reverted within a single phase, and `git diff --exit-code go.mod go.sum` is run
before that phase's commit.

**C4 — Fork code follows upstream's conventions; our code follows ours.** `gofumpt` and Go 1.21
idioms and `TestType_Action` in the fork; plain `gofmt` and our own naming here. Crossing these is
the likeliest mechanical error in this plan.

**C5 — Amendments to existing records are additive.** Strike through or annotate; never rewrite
original rationale.

**C6 — Our runtime behaviour does not change.** 0166's containment stays exactly as built, and no
phase touches `session.go`'s delivery path.

**The contract most at risk is C1.** Once the policy exists, the fastest way to make the suite
green is to relax the one test that asserts the old behaviour — and doing so would destroy the only
argument the PR has. **The most likely *accidental* violation is C3**, because P4 genuinely needs a
`replace` on disk and `make pre-add-check` will not object to it; the phase therefore ends with an
explicit `git diff --exit-code` rather than trusting the gate.

## Dependency and delivery order

| Release | Phases | Why grouped |
| --- | --- | --- |
| **1 — our own house** | P1, P2 | No upstream dependency, no fork, no push. Pure gain; correct regardless of the PR's fate. |
| **2 — build the contribution** | P3, P4 | The patch and the evidence that distinguishes it from #40/#50. Fork only; nothing committed here. |
| **3 — submit** | P5, P6 | Text approved, then posted. Needs an explicit ask. |
| **4 — the small one, and close out** | P7, P8 | The accessor PR is independent and must not be blocked by the policy discussion. |

P2 depends on nothing. P4 depends on P3. P5 depends on P4's evidence. P7 depends on nothing but is
sequenced last so it cannot delay release 2.

## Implementation Steps

### P1 — Correct the four claims that are false at v0.13.5 (D10; closes F15, F16, F17, F19)

Docs only; no source file in this commit.

1. `docs/spec/0038-MADR-*` `:112-119` — annotate: `InitializeResponse.Meta` exists at
   `types_gen.go:2322`, so `_meta` is not discarded. `:157-161` — annotate: extension
   notifications *are* routed (`connection.go:582-587`).
2. `docs/spec/0039-MADR-*` `:254-256` and `:460-463` — the same two claims.
3. `docs/spec/0081-PLAN-*` `:444-448` — the open question is **answered** from source; no
   follow-up on "the SDK hook" is required.
4. ~~`docs/spec/0137-PLAN-*` — the `SetLogger` problem is a **data race**, not a construction race;
   upstream #57/#58 report it and #59 fixes it (D8).~~ **Replaced 2026-09-22 (deviation below):**
   `docs/spec/0137-PLAN-*` — add a note that upstream #57/#58 now report the race 0137 already
   diagnosed, and #59 fixes it with `atomic.Pointer`. 0137 was accurate; this is confirmation, not
   correction.
5. `docs/decisions/0166-MADR-*` — amendment: its deferred "upstream a patch" item is now decided by
   0167, and its fork/vendor item is **rejected as a goal** while retained as a contingency.

**Verification.** `git diff --numstat` shows additions only; zero deletions of original rationale
(C5). Re-read each cited line to confirm the annotation attaches to the sentence it corrects.

### P2 — Take `initialize` off the raw path (D10; closes F15)

Local, independent of upstream. This is our defect.

1. `acpagent.go:529` — replace `s.rawRequest(initCtx, "initialize", …)` with the SDK's public
   `conn.Initialize(initCtx, initReq)`.
2. Recover the vendor block from `initResp.Meta` by re-marshalling that `map[string]any` and
   decoding into the existing `grokInitializeMeta` (`:1040-1053`), which reads only `_meta`. Do not
   change `grokInitializeMeta`.
3. `rewind_test.go:472-476` — drop `initialize` from the allowed-unsafe set, leaving
   `session/set_model` and `session/resume`; the AST scan's minimum call-site count drops by one.
   **The cast itself stays** (out of scope, pending P7).
4. New `initialize_test.go` — assert `defaultAuthMethodId`, `agentVersion`,
   `modelState.currentModelId` and `availableModels` survive the typed round trip, from the existing
   grok `initialize` wire fixture.

**Verification, fail-first (C2).** Blank one field in the re-marshal step and confirm
`initialize_test.go` fails naming it; restore from an in-memory copy, never `git checkout`.
Temporarily re-add a raw `initialize` call in a *scratch copy* of `rewind_test.go`'s target and
confirm the AST guard fails. Then the house stability rule, including `git diff --exit-code go.mod
go.sum`.

### P3 — Fork: add the notification overflow policy (D2, D3, D4, D5, D6, D7, D14; closes F5, F23, F27, F31–F34, F38, F39)

Fork only. **Nothing in this repository changes in this phase.**

1. ~~Fetch #40 and branch from its head:
   `git fetch upstream pull/40/head:pr40 && git switch -c feat/notification-overflow-policy pr40`.
   If #40's base has drifted such that it no longer applies, fall back to a self-contained option
   type and say so in the PR — that is MADR open question 1, and the fallback is recorded, not
   improvised.~~ **Replaced 2026-09-22 (deviation below):** branch
   `feat/notification-overflow-policy` from `main` (`0845a3b`, `v0.13.5`), then
   `git cherry-pick -x a7af6cb 56c2c30` — #40's two queue commits only, authorship and messages
   preserved, each carrying `(cherry picked from commit …)`. #40's third commit, `107b384`
   (union-decode error context), is **excluded**. Verify the picked commits byte-for-byte against
   the originals before building on them.
2. Add the policy to #40's `connectionConfig`:

   ```go
   // NotificationOverflowPolicy selects what a Connection does with an inbound
   // notification that arrives when the notification queue is full.
   // More policies may be added in the future.
   type NotificationOverflowPolicy int

   const (
       // OverflowCloseConnection closes the connection when a notification cannot be
       // queued, failing in-flight requests with ErrNotificationQueueOverflow. This is
       // the default, and the behaviour of every release before this option existed.
       OverflowCloseConnection NotificationOverflowPolicy = iota
       // OverflowDropNewest discards the notification that could not be queued and
       // leaves the connection open. Delivered notifications keep their relative order;
       // the dropped one is never delivered and is not retried.
       OverflowDropNewest
   )

   func WithNotificationOverflowPolicy(p NotificationOverflowPolicy) ConnectionOption

   // fn runs on the reader goroutine and MUST NOT block; it must not call back into
   // the Connection. It does NOT receive params — see MADR F29.
   func WithNotificationDropHandler(fn func(method string, totalDropped uint64)) ConnectionOption

   // Observability that does not depend on a callback being installed (D5).
   // The sentinel is deliberately NOT exported here: #50 already does that, and
   // upstream's own test references the unexported name (C1, MADR F38).
   func (c *Connection) DroppedNotifications() uint64
   ```

   Zero value is `OverflowCloseConnection`, so an unset option is today's behaviour exactly
   (C1, D3). **Do not give the handler a return value and do not pass it `params`** — both were
   considered and withdrawn (MADR F28–F30); a reviewer asking for either should be answered with
   F30's migration argument, since adding a handler beside the enum later is additive.
3. In `receive()`'s `default:` branch, keep upstream's existing rollback untouched —
   `lastEnqueuedNotificationSeq--` and both invariant re-checks (`connection.go:435-445`) — and
   replace **only** the final log-and-`shutdownReceive` pair. On `OverflowDropNewest`: increment a
   counter, invoke the handler (never while holding `notifyMu`), log at `Warn`, and `continue` the
   read loop. Ordering and sequencing are untouched by construction (D4).
4. Do **not** add a blocking path and do **not** remove the bound (D6). Responses are dispatched
   inline on the reader goroutine (`connection.go:397`), so blocking deadlocks the connection;
   record that in a code comment so the next reader does not "simplify" it.
5. New `connection_overflow_policy_test.go`, named to upstream's convention (`tc := tc` on any
   captured row — the module is `go 1.21`, MADR F32; wait on `waitForNotificationBarrierDrain`,
   never `time.Sleep`, F33), table-driven where the cases share a fixture and
   (`TestConnection_NotificationOverflowPolicy…`): the default closes with the documented cause;
   `OverflowDropNewest` keeps `Done()` unfired across a burst well past the bound, reports drops
   through the handler, and leaves the barrier draining in order afterwards.

**Verification.**
* **C1, the load-bearing check:** `git diff --stat upstream/main -- acp_test.go
  connection_cancel_test.go connection_notification_barrier_test.go` is **empty**, and
  `go test ./...` passes with `TestConnectionFailsFastOnNotificationQueueOverflow` green.
* **Fail-first (C2):** wire the option so it is read but never applied, and confirm the
  drop-policy test fails; then invert it so the policy is always on, and confirm the *default* test
  fails. Both directions, because an option that silently does nothing and an option that silently
  applies to everyone are different bugs and only two runs separate them.
* Upstream's stability rule in full, and record which of `make check` / `mise` actually ran.

### P4 — Prove it against our own reproduction, without changing our build (D1, D11, D12; closes F5)

The evidence #40 and #50 lack, and the only phase that touches this repository's build files.

1. Add `replace github.com/coder/acp-go-sdk => ../acp-go-sdk` to `go.mod` — **temporary, never
   staged** (C3).
2. Point our connection at `OverflowDropNewest` *in a scratch edit* and run
   `GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run StalledPump -count=20`, using 0166's
   fixture. Record the transcript: the transport must survive where it previously died.
3. Re-run with the policy unset: the failure must reappear, proving the default is unchanged (D3).
4. Record the drop counts observed, so the PR can state the cost honestly rather than only the
   benefit.
5. Revert the `replace`, the scratch edit and `go.sum`; run `git diff --exit-code go.mod go.sum` and
   `git status --porcelain` and confirm both are clean **before** anything is committed.

**Verification.** The two transcripts from steps 2 and 3, quoted verbatim in the PR and in P8.
`git diff --exit-code go.mod go.sum` clean. This phase commits nothing to this repository unless
step 4's numbers land in a doc, in which case that doc is the only file staged.

### P5 — Conform to upstream's conventions and draft the PR (D7, D8, D14, D15; closes F35, F36, F37)

No pushing. Produce text and check conformance.

1. Re-read the fork's `AGENTS.md` before writing anything and confirm: imperative subject under
   ~72 chars, `gofumpt` clean, tests named `TestType_Action`, fixtures under `testdata/` if any,
   examples still building (`make test`).
2. Draft the PR description covering what upstream asks for — protocol impact ("none: the wire
   format and default behaviour are unchanged"), schema/generated updates ("none"), and the
   commands run. Lead with the gap rather than the grievance:
   * that #40/#50 make the bound configurable but leave the policy fixed (F27);
   * that a **null** handler still loses the transport 3/5 under `GOMAXPROCS=1`, so capacity alone
     does not remove the failure (F5, obs. 28);
   * that Go reserves teardown-on-bound for attack mitigation at 10000 (`x/net/http2`), not for a
     slow local consumer at 1024 (F23);
   * that this same file already **drops** on a full queue for `$/cancel_request` (F10);
   * that the default is untouched and upstream's own tests pass unmodified (C1);
   * the maintainer's own framing from PR #8 — a bounded queue *"with explicit overflow handling"*,
     unbounded growth objected to *"as a default"* (F37) — met literally.
   Do **not** call the teardown a bug (MADR F3).

   Then the transcripts D15 requires, verbatim, because CI cannot supply them (F35): `make test`;
   `go test -race ./...`; `gofumpt -l .` (empty); `go vet ./...` filtered to non-generated files
   (empty); `staticcheck` and `golangci-lint` showing **only** the two pre-existing `S1016`
   findings (F31); and the P4 reproduction in both directions with drop counts. State explicitly
   that `make check` (`treefmt`) was not run if `treefmt` is unavailable locally — never imply it
   was.
3. Draft the shorter comments for #40 and #50 (point at the policy gap, offer the branch) and for
   #57/#58/#59 (independent confirmation of the race; we are not submitting a rival fix — D8).
4. **Present all four texts to the owner.** Nothing is posted in this phase.

**Verification.** A conformance checklist against the fork's `AGENTS.md`, each item marked with the
command that proved it or explicitly marked not-run. Any claim in the PR body that cannot be traced
to a transcript from P3 or P4 is removed before it is shown.

### P6 — Submit the policy PR (D7; **requires an explicit ask**)

1. Push `feat/notification-overflow-policy` to the fork. **Explicit ask required.**
2. Open the PR against `coder/acp-go-sdk` with the approved text. **Explicit ask required**, and
   the approved text is used verbatim.
3. Post the approved comments on #40, #50, #57, #58, #59.
4. Record every URL.

**Verification.** Posted text matches what was approved, character for character. URLs recorded in
P8. **The PR will not get a green check, and that is not ours to fix (MADR F35):** every fork-PR
run on this repository since August has either sat at `action_required` or failed with zero jobs
before any step ran, while `main` passes. P8 records the run's actual state verbatim rather than
reporting "red"; the evidence the reviewer needs is in the PR body (D15).

### P7 — The `Connection()` accessor PR (D9; closes F18; **requires an explicit ask**)

Deliberately last and deliberately separate, so a one-line change cannot be held hostage by the
policy discussion.

1. Fork branch `feat/expose-connection` from `upstream/main` (**not** from the policy branch).
2. `client.go`: `func (c *ClientSideConnection) Connection() *Connection { return c.conn }`, plus
   the symmetric `AgentSideConnection` method, with doc comments explaining the use case — reaching
   the exported `SendRequest[T]` for a standard method the SDK does not model.
3. New test asserting the accessor returns the live connection and that `SendRequest` through it
   reaches the wire.
4. PR body: state that consumers currently do this with `unsafe.Pointer` because `CallExtension`
   refuses non-`_` methods, and that the accessor removes the need — without naming our repository
   as the only consumer, since `session/set_model` affects anyone talking to an agent that models
   it.

**Verification.** Upstream's stability rule. Fail-first: a test that the accessor is non-nil and
identical to the connection the constructor built — broken by returning a fresh `Connection`, which
must fail. **Our own `unsafe` cast is not touched** (out of scope until this merges).

### P8 — Execution record (closes the plan)

Append `## Execution record (YYYY-MM-DD)` here and `## Observed — execution results` to the MADR:
which phases ran; the P4 transcripts in both directions; the drop counts; whether #40's branch
still applied cleanly (open question 1); which upstream commands could not be run on this host; the
PR URLs; and what the plan predicted incorrectly.

## Verification (whole plan)

```bash
# C1 — the PR's whole argument: upstream's existing tests, untouched and green.
cd ../acp-go-sdk
git diff --stat upstream/main -- acp_test.go connection_cancel_test.go connection_notification_barrier_test.go
go test ./... && go test -race ./... && go test ./... -cover
gofumpt -l .                      # THEIR formatter (C4); expect empty

# D3 — default unchanged.
go test ./... -run TestConnectionFailsFastOnNotificationQueueOverflow -count=5

# D12 — the evidence, with a temporary replace that is reverted before committing.
cd ../magic-cli-remote
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run StalledPump -count=20

# C3/D1 — our build is untouched.
git diff --exit-code go.mod go.sum
grep -n '^replace' go.mod         # expect: no match

# House gates.
make pre-add-check && gofmt -l cmd internal && go vet ./... && make race
make -n ci-windows && make ci-windows
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | No pre-existing upstream test file is modified by the policy branch | D3, C1 |
| A2 | `go test ./...` in the fork passes, `TestConnectionFailsFastOnNotificationQueueOverflow` included | D3 |
| A3 | Unset option ⇒ behaviour byte-identical to `v0.13.5`, proven by that test at `-count=5` | D3 |
| A4 | `OverflowDropNewest` keeps the connection alive across a burst past the bound | D2, D4 |
| A5 | Drops are reported through the handler and counted; none are silent | D5 |
| A6 | The policy test was seen to fail **both** ways: option ignored, and option always-on | C2 |
| A7 | Upstream's existing rollback code is reused, not rewritten; sequencing untouched | D4 |
| A8 | No blocking path and no removal of the bound appears in the diff | D6 |
| A9 | `GOMAXPROCS=1 … StalledPump -count=20` survives with the policy and fails without it | D12 |
| A10 | Drop counts from that run are recorded, so the PR states the cost as well as the benefit | D5, D12 |
| A11 | `git diff --exit-code go.mod go.sum` clean; no commit contains a `replace` | D1, C3 |
| A12 | Fork code is `gofumpt`-clean and Go 1.21-compatible; our code is plain-`gofmt`-clean | C4 |
| A13 | PR body states protocol impact, generated-file impact, and the commands actually run — with unavailable ones marked unavailable | D7 |
| A14 | The branch is stacked on #40 and credits it, or the fallback is recorded with its reason | D7 |
| A15 | #40, #50, #57, #58, #59 each carry an approved comment; no new duplicate issue was opened | D7, D8 |
| A16 | `initialize` is off the raw path and its vendor fields still arrive; the cast is untouched | D10 |
| A17 | Records 0038/0039/0081/0137/0166 amended additively — additions only | D10, C5 |

**The criterion most likely to be quietly dropped is A9**, and after it A6. A9 is the only evidence
that separates this PR from the two already sitting unmerged, and it is also the only one requiring
the temporary `replace` — so it is simultaneously the most valuable and the most awkward. A6 is the
usual trap in a different costume: an option that is read but never applied produces a green suite,
and only the second, inverted run distinguishes that from a working one.

## Rollout and Rollback

**Rollout.** There is nothing to roll out here: D1 means this project's behaviour is unchanged, so
release 2 carries no runtime risk at all. The only outward-facing steps are P6 and P7, and both are
gated on an explicit ask with the text approved first. Live-tagged runs are not required by this
plan — the reproduction in P4 is a unit-level fixture, not a token-spending live turn.

**Rollback.** Releases 1 and 4 are ordinary reverts. Release 2 has nothing to roll back: the work
lives on fork branches that this project does not consume. Release 3 is the only step that cannot
be un-done unilaterally — a published PR and five comments exist once posted. That is a reason to
approve the text carefully, not a reason to hesitate: a withdrawn PR is a normal event, and closing
it with a short explanation is the rollback.

**If the PR is rejected**, the MADR's Option E becomes live as a contingency and needs its own
approval — the fork already exists and the mechanism is proven, so that pivot is cheap. Rejection
on grounds we accept (for example, the maintainer prefers Option C's partitioned design) should be
recorded as an amendment, not treated as a defeat: it would mean the design conversation finally
happened, which is more than #40 and #50 achieved in 131 and 105 days.

## Deferred (named, so they are not mistaken for oversights)

* **Adopting the policy in our own build.** Waits because it trades a dead transport for a visible
  gap in a turn, which is a product decision and needs its own record — and because until the PR
  merges, adopting it would mean the `replace` D1 forbids.
* **Deleting our `unsafe.Pointer` cast.** Waits for P7 to merge. Removing it earlier would couple
  our build to a fork-only API, which is the coupling this plan exists to avoid.
* **A PR for the sequencer panics (F13).** Waits because it needs its own evidence — ten sites, most
  unreachable through the public API — and because three concurrent PRs to a dormant project
  compete with each other for the same scarce review.
* **A PR for the 10 MiB frame cap (F14).** Waits on measurement: nothing in our traffic has been
  shown to approach it, and a bigger constant with no failing test behind it is not a contribution.
* **Option C, the partitioned bounded queue (F24).** Waits because it is a structural rewrite of
  someone else's concurrency design; it is what to advocate if the maintainer engages, and P3's
  branch is the vehicle for that conversation.
* **Schema currency (F22)** and **per-engine blast radius (0166 F3).** Unchanged by this plan,
  each needing its own pair.

## Deviation — 2026-09-22: P1 step 4 corrected a record that was already correct

**Found.** P1 step 4 instructed correcting `0137-PLAN` to call the `SetLogger` problem a data race
"not a construction race". On reading the passage before editing it, `0137-PLAN:1619` already
reads *"A data race on the ACP SDK's connection logger"*, quotes the `-race` detector trace
(`:1623-1624`, `loggerOrDefault` read vs `SetLogger` write), and explains the unsynchronised field
write (`:1634`). The code comment at `acpagent.go:496-502` says the same. Pre-existing and
accurate; confirmed by reading the committed text, which no phase of this plan had touched. The
error originated in MADR 0167 F19, which asserted our account was a mischaracterisation without
re-reading 0137.

**Decision (owner, 2026-09-22).** Replace step 4 with an additive note recording upstream #57/#58/#59
as confirmation; amend MADR F19 additively to withdraw the claim. No files added to scope.

**Also found in scope.** `0038-MADR:113` additionally claims *"`NewSessionRequest` does not model
`_meta` either"*; at v0.13.5 `NewSessionRequest` has `Meta` (`types_gen.go:3236`). Annotated
together with step 1's `InitializeResponse` correction — same passage, same phase.

## Deviation — 2026-09-22: P2's wiring was untested, before and after

**Found.** P2's fail-first run (C2) mutated the production call site to
`decodeGrokInitializeMeta(nil)`, discarding grok's vendor block, and the whole
`internal/provider/acpagent` package still passed. The same mutation against unmodified HEAD (the
old `json.Unmarshal(rawInit, &initMeta)` blanked, in a separate worktree) also passed: the gap is
**pre-existing** — no unit test has ever checked that `initialize` populates `engineModelID`, the
model catalog or the reported engine version. The only end-to-end coverage is
`grok/live_initializemeta_test.go` (`-tags live_grok`), not verified to bite. A16's "its vendor
fields still arrive" was therefore unproven, on the very line P2 rewrote.

**Decision (owner, 2026-09-22).** Extract the post-`initialize` handling into
`applyInitializeResponse`, called from `spawnAgent`, and unit-test it from the grok fixture. No
files added to scope (`acpagent.go` and `initialize_test.go` are already P2's). Residual, stated:
deleting the single call line would still pass; everything behind it is guarded.

## Deviation — 2026-09-22: P2's race gate failed on an unrelated, load-sensitive relay test

**Found.** P2's stability rule failed at `make race`: `internal/relayhost`
`TestEnvelopeVersionRejected` (`deadline_test.go:130`) timed out in `websocket.Dial` against its
5 s context; in the same run `internal/provider/acpagent` passed. Pre-existing and independent of
P2: `acpagent` is absent from `relayhost`'s dependency graph including test deps
(`go list -deps -test`), and the file was last changed in `d11e938` (plan 0115). 25/25 reruns under
`-race` pass; a loopback dial normally takes 1–3 ms. Instrumenting a scratch copy showed the
test's assertion is genuine (`ReadEnvelope` rejects `v:99` immediately, context unexpired) but
every run spends 5.00 s in the deferred `conn.Close`: the server handler never reads, so the close
handshake waits out the library timeout.

**Decision (owner, 2026-09-22).** Fix it for real inside this plan, before P2 commits, rather than
defer it to 0115. **File added to scope:** `internal/relayhost/deadline_test.go` (and any sibling
test file in `internal/relayhost` shown to share the defect, each named in the execution record).
The fix must be shown to change the failure rate under reproduced load, not merely to pass.

## Deviation — 2026-09-22: #40 is three commits, and one of them is not about queues

**Found.** Before branching, `git log main..pr40` showed PR #40
(<https://github.com/coder/acp-go-sdk/pull/40>, opened 2026-05-14 by Alvaro Saurin, GitHub
**@inercia**, head `inercia:configurable-notification-queue` at `107b384`) is **three** commits,
all authored by Alvaro Saurin:

| commit | authored | subject | files |
| --- | --- | --- | --- |
| `a7af6cb8babe78b74d71240e6a79a7b4a8547d29` | 2026-05-14T16:31:43+02:00 | Add configurable notification queue size via ConnectionOption | `agent.go`, `client.go`, `connection.go`, `connection_queue_size_test.go` (+190/−8) |
| `56c2c30ca894d2fecaeb1db086b613677b4be3d6` | 2026-05-15T09:41:17+02:00 | Remove unused fmt import and dummy reference in test | `connection_queue_size_test.go` (−2) |
| `107b384c8140ca27a5dfe37d6234bc12e848b9ec` | 2026-08-17T22:10:31+02:00 | Improve union decode error context | `cmd/generate/internal/emit/types.go`, `errors.go`, `errors_test.go`, `types_gen.go` (+315/−232) |

The third is a generator and error-context change unrelated to the notification queue. #40's base
is `192e108`, **4 commits behind `main`**, predating the Nix→mise migration (`3091984`) against
whose `mise.toml`/`treefmt.toml`/`Makefile` MADR F31 was measured. P3 step 1 as written would have
put a 456-line generated-code change into our PR, on a pre-mise tree. The error originated in MADR
F26, which quoted the first commit's stat as the PR's.

**Verified before deciding.** On a scratch worktree, `a7af6cb` + `56c2c30` cherry-pick cleanly onto
`main` (4 files, +188/−8), `go build ./...` succeeds and `go test ./...` passes. The global
`prepare-commit-msg` hook does **not** rewrite a cherry-picked message: author and author date are
preserved, and `-x` appends the provenance line.

**Decision (owner, 2026-09-22).** Branch from `main`, cherry-pick #40's two queue commits with `-x`,
exclude `107b384`, and cite authorship in full in the PR, in its commits and in these records. No
files added to scope. The PR must state which of #40's commits it includes and which it excludes,
and why — so it cannot be read as appropriating #40 or as silently dropping part of it.

## Execution record — release 1 (2026-09-22)

**Ran:** status flip + pair (`89ef291`), P1 (`0072ec1`), P2 (this commit). Three deviations, each
recorded above with the owner's decision.

**P1.** Seven records annotated; `git diff --numstat` showed **zero** deletions in every older
record. The only removed lines were in 0167's own PLAN and MADR — the two struck-through
passages — and each was verified present inside its `~~ ~~` markers. A first run changed two
files' final newline (my script normalised EOF); caught by the numstat check and restored
byte-for-byte before commit.

**P2, as planned.** `initialize` uses the typed `conn.Initialize`, which is
`SendRequest[InitializeResponse]` over the same connection — the wire frame is unchanged, and
`InitializeRequest.Validate()` returns nil. `initialize` is out of the guard's allowed-unsafe set,
whose comment now states the real reason the other two remain (MADR F17). The guard's floor of 6
call sites was **left alone**: the step said the minimum "drops by one", but it is a sanity floor,
not an exact count (13 sites → 12).

**P2, from its deviation.** The post-initialize handling moved verbatim into
`Provider.applyInitializeResponse`, with the one local `spawnAgent` still reads (`advertised`)
rebound from `s.advertisedAuth`, the identical slice.

**Fail-first (C2), all on scratch worktrees, the real tree never modified:**

| mutation | result |
| --- | --- |
| decoder drops `defaultAuthMethodId` | FAIL — `typed: {DefaultAuthMethodID: …}` vs `raw: {DefaultAuthMethodID:cached_token …}` |
| decoder returns nothing | FAIL — equivalence assertion |
| `initialize` put back on the raw path | FAIL — `rawRequest("initialize") takes the unsafe raw-connection cast…` |
| call site passes `nil` meta (the gap) | before the fix: **whole package green**, and green at HEAD too; after: FAIL — `engineModelID = "", want "grok-4.6"`, `no model catalog recorded` |
| `spawnAgent` never calls the handler | **passes** — the residual stated in the deviation, now measured |

**The relay test (third deviation).** `internal/relayhost` package time **16.95 s → 2.15 s**
(`-race`), from two defects fixed in the places shown to have them: two handlers now answer the
close handshake (`answerClose`); the two tests' 5 s contexts became a named `hangGuard`. The third
parking handler (`TestRegisterExchangeDeadline`) is unchanged by design. Proven three ways:
both assertions still fail when the product code is broken (`v:99 envelope must be rejected…`;
`limit=4096: 5120 bytes crossed…`); a 6 s injected handshake reproduces the `make race` failure
message verbatim on the original file (`failed to WebSocket dial: … context deadline exceeded`)
and passes on the fixed one; reverting `answerClose` alone restores the 5.00 s waits.

**What the plan got wrong.** P1 step 4 targeted a record that was already correct, a claim that
entered MADR 0167 from an inventory's paraphrase. P2's verification list checked the decoder and the
guard but not the wiring, which was untested before this plan existed. Neither was visible without
reading the actual record or breaking the actual call site — both were found only by doing so.

**Same-class candidates, not touched (outside the deviation's scope):** handlers parking on
`r.Context().Done()` in `internal/provider/codex/auth_p3_test.go:31`,
`internal/provider/httpagent/supervise_wiring_test.go:127` and
`internal/relay/server_lifecycle_test.go:114` — not measured, so not claimed to share the defect.

**Gates at commit:** `pre-add-check`, `gofmt -l`, `go vet ./...`, `acpagent` tests, `make race`
(0 FAIL lines; `relayhost` 2.03 s), `make -n ci-windows` showing the live guard, `make ci-windows`,
`git diff --exit-code go.mod go.sum` — all exit 0.

## Execution record — release 2 (2026-09-22)

**Ran:** P3 and P4. Fork branch `feat/notification-overflow-policy`, three commits on `main`
(`0845a3b`, `v0.13.5`):

| commit | author | subject |
| --- | --- | --- |
| `10bc9f5` | Alvaro Saurin (@inercia) | Add configurable notification queue size via ConnectionOption — cherry-picked from `a7af6cb` |
| `52b5723` | Alvaro Saurin (@inercia) | Remove unused fmt import and dummy reference in test — cherry-picked from `56c2c30` |
| `eb6e808` | this project | feat(connection): support dropping notifications on queue overflow |

Nothing is pushed. This repository's only change in release 2 is this record and the deviation
committed before it (`03fe631`).

**Provenance, verified rather than assumed.** A script compared each picked commit with its
original: author name, email and date identical; message identical plus exactly one
`(cherry picked from commit <sha>)` line; `git patch-id --stable` identical; `107b384` neither an
ancestor of the branch nor referenced by it. The verifier was then run against deliberately
swapped pairs and failed on author date, message and patch-id — so it can fail. The global
`prepare-commit-msg` hook was shown on a scratch worktree not to rewrite cherry-picked messages.

**Baseline before any change** (`main` + #40's two commits): `gofmt -l`/`gofumpt -l` empty;
`go vet` 12, `staticcheck` 2, `golangci-lint` 2 findings — exactly MADR F31's counts on plain
`main`, so #40 added none; `make test` and `go test -race ./...` pass; coverage 29.1%.
My first gate script compared findings as a *set*, which collapsed the 12 identical vet findings
into one and would have hidden a new duplicate. It now compares counts; a negative test adding one
duplicate finding reports `NEW=1` where the set version reports `NEW=0`.

**P3, as built.** `connection.go` +92, `client.go` +6, `agent.go` +6, all additions — the existing
overflow branch, upstream's rollback and its fail-fast tail are byte-identical; the policy check
sits in front of the tail. `NotificationOverflowPolicy` (`OverflowCloseConnection` zero value,
`OverflowDropNewest`), `WithNotificationOverflowPolicy`, `WithNotificationDropHandler`,
`DroppedNotifications`. Drops log at `Debug` (F34). Receiver names follow the file (`c`, 17 of 19
uses). The sentinel is not exported (F38).

**One addition the plan did not list, recorded here:** `DroppedNotifications` forwarders on
`ClientSideConnection` and `AgentSideConnection`. Without them D5's counter would be unreachable
for nearly every consumer, since both types keep their `*Connection` private — the gap F18
describes. Both files were already in P3's scope.

**Tests** (`connection_overflow_policy_test.go`, 6 tests, `TestType_Action` names, `tc := tc`
per F32): the default closes, with no option, with `OverflowCloseConnection` explicitly, and with
an undefined value; `OverflowDropNewest` keeps the connection open, reports each drop with its
running total, delivers queued notifications in order, never delivers dropped ones, and still
delivers a notification sent after the backlog clears (showing released sequence numbers are
reused cleanly); the handler is optional; both forwarders work. `-race`, 5 runs: all pass.

**Fail-first (C2), scratch worktree, seven mutations, all fail:**

| mutation | failed with |
| --- | --- |
| option read but never applied | `write of notification 6 blocked: the connection stopped reading` |
| policy always on | `connection stayed open after overflow; the default must close it` — **and upstream's own** `TestConnectionFailsFastOnNotificationQueueOverflow` |
| handler never called | `drop 1 was never reported` |
| counter not incremented | `drop 2 reported ("test/notify", 1), want ("test/notify", 2)` |
| reader stops after a drop | `write of notification 6 blocked: the connection stopped reading` |
| drop skips upstream's rollback (F39) | `notification barrier did not drain: completed=5 enqueued=8` |
| client forwarder broken | `ClientSideConnection.DroppedNotifications() = 0, want 7` |

The first run of this set **hung** on "reader stops": the test's pipe writes had no bound, so a
stopped reader blocked the next write until go test's 10-minute timeout. A test that hangs on the
regression it guards is not doing its job; writes are now bounded and fail within 5 s. The harness
also gained `-timeout=90s` so a hang cannot hide as a long run.

**Gates after:** `gofumpt`/`gofmt` clean; vet 12 / staticcheck 2 / golangci 2 — **NEW=0** in each;
`make test` and `-race` pass; coverage **29.1% → 29.4%**.

**P4, method strengthened.** Instead of a temporary `replace` in this repository's `go.mod` that
must be reverted before commit, P4 ran in a scratch worktree whose `go.mod` replaced the SDK with
the fork (`go list -m` confirmed `=> …/acp-go-sdk`). The real tree was never touched, and was
re-verified afterwards: `go.mod`/`go.sum` unchanged, status clean. 20 runs per cell, `GOMAXPROCS=1`,
1,224 `tool_call` frames at a stalled consumer:

| handler | policy | transport survived | frames written | dropped |
| --- | --- | --- | --- | --- |
| null | off | 7/20 | 1025–1224 | 0 |
| null | **on** | **20/20** | 1224 | 0–199 |
| real | off | 2/20 | 1025–1224 | 0 |
| real | **on** | **20/20** | 1224 | 0–199 |

The defect reproduces as MADR 0166 measured it (null handler: 13/20 lost, against 0166's 3/5; the
1025 minimum is 0166 F8's 1,024 queued + 1 in flight), and the policy removes it in both variants.
The cost is stated with it: up to 199 notifications dropped in a run, the maximum being exactly
1,224 − 1,025.

**What the plan got wrong.**
* **P3 step 1** stacked on #40's head, which carries an unrelated 456-line commit; F26 had quoted
  one commit's stat as the PR's (deviation above).
* **D14's confirmation `grep … time.Sleep → none` was over-broad.** It conflated a fixed sleep
  followed by an assertion (the pattern F33 warns against, `acp_test.go:724`) with polling a
  condition until a deadline — which upstream's own `waitForNotificationBarrierDrain` does with
  `time.Sleep(time.Millisecond)`. The handler-less test polls the counter the same way, because
  with no handler installed there is no event to wait on. No fixed sleep is used as a wait.
* **C3's mechanism** (a temporary `replace` in the real `go.mod`) was weaker than necessary; the
  worktree method above makes a stray `replace` impossible rather than merely checked-for.

**Carried to release 3:** the PR body must state the cherry-picks and the exclusion of `107b384`
explicitly, credit Alvaro Saurin (@inercia) for the options surface, carry the transcripts above
because no fork PR can get a CI signal (F35), and say plainly that `make check` was not run
locally (`treefmt`/`mise` are not installed on this host).

## Amendment — 2026-09-22: release 5, adopt the fork (MADR D16–D20)

The owner decided to build against the fork (MADR amendment of 2026-09-22). This section adds the
work; everything above is unchanged.

### Contracts changed by this amendment

* **C3** (`go.mod`/`go.sum` never committed changed) is superseded **for P10 only**. The single
  permitted change is the `replace` line and its `go.sum` entries, targeting a remote module at an
  immutable tag. A filesystem `replace` stays forbidden, now enforced by P13's guard.
* **C6** (our runtime behaviour does not change) is superseded by P10–P11, deliberately. 0166's
  containment stays in place underneath as the fallback.

### Scope added (the only files release 5 may touch)

**Fork:** the annotated tag `v0.13.6-mcr.1` on the verified branch head; the branch and tag
published to `<owner>/acp-go-sdk`.

**This repository:** `go.mod`, `go.sum`; `internal/provider/acpagent/acpagent.go`;
`internal/provider/acpagent/session.go`; `internal/provider/acpagent/stalledpump_test.go`;
`internal/provider/acpagent/droppednotice_test.go` *(new)*; `internal/modguard_test.go`
*(new, P13)*; `docs/decisions/0166-MADR-*.md`; `docs/spec/0138-MADR-*.md`; this pair.

### P9 — Tag and publish the fork (D16; **explicit ask required**)

1. Annotate `v0.13.6-mcr.1` on `eb6e808`, after re-running `verify_picks.py` and the fork gates
   against that exact commit.
2. Push the branch and the tag to `<owner>/acp-go-sdk`. **Explicit ask in the turn it happens.**
3. From a clean scratch module, `go list -m github.com/<owner>/acp-go-sdk@v0.13.6-mcr.1` must
   resolve **through the module proxy**, which proves CI and other machines can fetch it.

### P10 — Replace and opt in (D16, D17)

1. `go.mod`: add the `replace`, with a comment naming this record and the upstream PR it stands in
   for; `go mod tidy`; commit `go.sum`.
2. `acpagent.go:494`: pass `acp.WithNotificationOverflowPolicy(acp.OverflowDropNewest)`. No drop
   handler.
3. Gates: `make pre-add-check` (govulncheck runs over the replaced module), `make race`,
   `make ci-windows`.

### P11 — Per-turn loss notice (D18)

1. A `droppedCount func() uint64` seam on the session, defaulting to `s.conn.DroppedNotifications`,
   so the logic is testable without a live transport.
2. Snapshot it when the turn starts. After `submitPrompt` returns (`session.go:443`), if it rose,
   emit one `TypeNotice` naming the count, ahead of every exit path's `TypeTurnComplete`.
3. `droppednotice_test.go`: a turn during which the counter rises gets exactly one notice, emitted
   before `TypeTurnComplete`, on the done, cancelled and errored paths; a turn without drops gets
   none.
   **Fail-first (C2):** emit after `TypeTurnComplete`, never emit, and emit on no-drop turns — each
   must fail.

### P12 — Reinstate transport survival (D19)

1. `stalledpump_test.go`: `TestACPConnectionSurvivesAStalledPump` with the option set, beside the
   containment test (which stays).
2. **Red first:** the same test with the option removed, 20 runs under `GOMAXPROCS=1`, must fail
   at least once; then **green:** 20/20 with it.
3. Additive amendments to 0166 (its D1 retirement is lifted, with evidence) and 0138 (F5's promise
   is met again).

### P13 — Guard, exit path, record (D20)

1. `internal/modguard_test.go` parses `go.mod`. It fails if any `replace` targets a filesystem
   path, or if the acp-go-sdk replace's version is not a tag of the form `vX.Y.Z-mcr.N`. It must
   be seen failing on a `=> ../acp-go-sdk` fixture and on a pseudo-version fixture.
2. Record the exit procedure here: when upstream merges, delete the `replace`, bump the version,
   adapt the call site — one commit.
3. Execution record.

### Acceptance criteria added

| # | Criterion | MADR |
| --- | --- | --- |
| A18 | The tag resolves through the module proxy from a clean module | D16 |
| A19 | `go list -m github.com/coder/acp-go-sdk` shows `=> github.com/<owner>/acp-go-sdk v0.13.6-mcr.1` | D16 |
| A20 | Import paths unchanged in every Go file | D16, C4 |
| A21 | The client connection is constructed with `OverflowDropNewest` and no drop handler | D17, F29 |
| A22 | A turn with drops gets exactly one notice before `TypeTurnComplete`, on all three exit paths; a turn without drops gets none — each seen failing first | D18 |
| A23 | Transport survival: red with the option removed (≥1/20), green with it (20/20), `GOMAXPROCS=1` | D19 |
| A24 | The guard fails on a filesystem `replace` and on a pseudo-version | D20 |
| A25 | `make pre-add-check`, `make race`, `make ci-windows` pass with the `replace` in place | stability rule |
| A26 | 0166 and 0138 amended additively | D19, C5 |

**The criterion most likely to be skipped is A23's red half.** Once the option is in the code, the
survival test is green, and the only way to prove it can go red is to remove the option on a
scratch copy. It is the same trap as A2, one release later.

### Sequencing against release 3

Release 5 needs P9's push, and P6 needs the same branch pushed. The branch should be pushed once,
serving both, which is why P9 and P6 share their first step. Release 5 does not depend on the PR
being opened, merged or answered — that independence is the point of the owner's decision.
