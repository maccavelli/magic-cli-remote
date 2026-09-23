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

1. Fetch #40 and branch from its head:
   `git fetch upstream pull/40/head:pr40 && git switch -c feat/notification-overflow-policy pr40`.
   If #40's base has drifted such that it no longer applies, fall back to a self-contained option
   type and say so in the PR — that is MADR open question 1, and the fallback is recorded, not
   improvised.
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
