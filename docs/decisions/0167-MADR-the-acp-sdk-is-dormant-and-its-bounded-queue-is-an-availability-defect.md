---
status: accepted
date: 2026-09-22
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# MADR 0167: `acp-go-sdk` tears the transport down on a full notification queue — contribute an overflow policy upstream

## Context and Problem Statement

Every ACP-speaking provider in this repository talks to its engine through one third-party
module: `github.com/coder/acp-go-sdk v0.13.5` (`go.mod:7`). That module is 5,804 non-test
lines of our own code's dependency surface (`internal/provider/acpagent`, 45 files), and it
owns the JSON-RPC transport, the read loop, the notification ordering guarantees and the
generated protocol types.

MADR 0166 established that a stalled notification consumer takes the **entire engine
transport** down, that no client-side change can prevent it at v0.13.5, and that the permanent
fix is upstream. It deferred three things by name: upstreaming a patch, forking/vendoring the
SDK, and reducing the per-engine blast radius.

**This record decides the first of those, and its purpose is a contribution rather than a
workaround.** The question is not "what do we carry instead" — it is *what is the right
enhancement to send upstream, such that a dormant single-maintainer project might actually merge
it, and every consumer of this SDK benefits rather than only us?* A private fork would resolve our
symptom and leave the defect in place for everyone else; that is explicitly not the goal here, and
Option E is rejected on exactly those grounds.

The answer turned out to be broader than the one defect. Fourteen distinct limitations are
already recorded across this repository's decision records, and reading the SDK source
directly — which no previous record did, they all cite it second-hand — showed that four of
our own recorded claims are **wrong at v0.13.5**, one latent failure mode is unrecorded, and
the defect 0166 traced is one instance of a design the upstream project has deliberately
chosen and locked with a test.

### What was measured, not assumed

Everything below was produced this session against `v0.13.5` at commit `0845a3bb`, in a
local fork whose `upstream` remote points at `coder/acp-go-sdk` (push URL disabled). Claims
are marked **[measured]** (a command was run and its output read), **[source]** (read
directly in the SDK at `v0.13.5`), **[inferred]** (reasoned from source, not executed), or
**[second-hand]** (reported by delegated research and not independently re-verified here).

| # | Observation | Method |
| --- | --- | --- |
| 1 | `v0.13.5` is the newest of 12 tags; `releases/latest` = `v0.13.5`, published `2026-06-02T14:24:21Z` | **[measured]** `git tag --sort=-v:refname`; `gh api repos/coder/acp-go-sdk/releases/latest` |
| 2 | `upstream/main` is `0845a3b`, dated `2026-06-02`, and **0 commits ahead** of `v0.13.5` | **[measured]** `git rev-list --count v0.13.5..upstream/main` → `0` |
| 3 | 32 commits total; 19 from one author, 8 from dependabot, 5 from 4 others | **[measured]** `git shortlog -sn upstream/main` |
| 4 | PRs #40, #50, #59 are all `OPEN` + `MERGEABLE` + `BLOCKED` | **[measured]** `gh pr view --json mergeable,mergeStateStatus` |
| 5 | Issues #57 and #58 (logger data race) are both `OPEN` | **[measured]** `gh issue view` |
| 6 | 30 GitHub issues/PRs across all repos match `"notification queue overflow" acp` | **[measured]** `gh api search/issues` |
| 7 | The overflow path: non-blocking send, `default` → `shutdownReceive`, reader returns | **[source]** `connection.go:431-449` |
| 8 | The teardown is asserted by upstream's own test | **[source]** `acp_test.go:836` `TestConnectionFailsFastOnNotificationQueueOverflow` |
| 9 | No configuration surface: all three constructors are `(handler, io.Writer, io.Reader)`; no options type exists | **[source]** `connection.go:94`, `client.go:16`, `agent.go:21` |
| 10 | `defaultMaxQueuedNotifications = 1024` is unexported and used directly in the `make(chan …)` | **[source]** `connection.go:19`, `:108` |
| 11 | The producer holds `notifyMu` across the enqueue; the consumer must re-acquire it before it can dequeue again | **[source]** `connection.go:428-433` vs `:499` |
| 12 | `waitNotificationsUpTo` blocks a **response** until prior notifications are **processed** | **[source]** `connection.go:773-799` |
| 13 | The same file **drops** on a full queue for `$/cancel_request` | **[source]** `connection.go:722`, `:730` (`"dropping $/cancel_request due to full queue"`) |
| 14 | 10 `panic()` sites, all in the notification sequencer; **0 at `v0.11.7`, 10 at `v0.12.0`** | **[measured]** `git show v0.11.7:connection.go` etc., counted per tag |
| 15 | `bufio.Scanner` line cap is 10 MiB; a larger frame ends the connection | **[source]** `connection.go:369`, `:374`, `:455-459` |
| 16 | No exported error sentinels; peer-disconnect is `NewInternalError(map[string]any{…})` | **[source]** `connection.go:741` |
| 17 | `Connection` has `Done()` and **no** `Close()`; it never closes `c.r`/`c.w` | **[source]** `connection.go:945` |
| 18 | `SetLogger` is an unsynchronized field write; 4 goroutines start before the constructor returns | **[source]** `connection.go:80`, `:111-119`, `:125`, `:127` |
| 19 | `InitializeResponse` **has** `Meta map[string]any` with `json:"_meta,omitempty"` | **[source]** `types_gen.go:2322` |
| 20 | Extension **notifications** reach `HandleExtensionMethod` | **[source]** `client.go:19` → `connection.go:582-587` → `extensions.go:53-67` |
| 21 | `SendRequest[T]` is exported; `ClientSideConnection.conn` is private with no accessor | **[source]** `connection.go:625`; `client.go:13-15` |
| 22 | `SessionUsageUpdate` = `{Meta, Cost, SessionUpdate, Size, Used}` — no cache fields | **[source]** `types_gen.go:5243` |
| 23 | `NewSessionRequest` = `{Meta, AdditionalDirectories, Cwd, McpServers}` — no `mode` | **[source]** `types_gen.go:3236` |
| 24 | The SDK contains **no** model type at all (`^type \w*[Mm]odel\w* struct` → none) | **[measured]** regex over `types_gen.go` |
| 25 | The bundled ACP schema never mentions backpressure, flow control, dropping or queueing: **0 of 488** descriptions across 129 definitions | **[measured]** `scratchpad/schema_notify.py` over `schema/schema.json` |
| 26 | `x/tools` states in-code that bounding a bidirectional JSON-RPC inbound queue is not generally possible | **[measured]** `curl raw.githubusercontent.com/golang/tools/master/internal/jsonrpc2_v2/conn.go` |
| 27 | SDK `go.mod` declares `go 1.21`; this project declares `go 1.26.6` | **[measured]** both files |
| 28 | With a **null** notification handler the transport is still lost **3/5** under `GOMAXPROCS=1`, with `session_faulted=0/5` | **[measured]** MADR 0166 amendment, `0166-MADR:216-238` |
| 29 | Inbound **responses** are handled **inline on the reader goroutine**; inbound requests get their own goroutine | **[source]** `connection.go:397` vs `:411-421` |
| 30 | `$/cancel_request` is handled synchronously ahead of the switch, deliberately outside notification ordering | **[source]** `connection.go:388-393` |
| 31 | Go's only teardown-on-bound is explicit anti-DoS at a bound of **10000**: *"to prevent memory exhaustion attacks"*, *"assume they are trying to make us run out of memory"* | **[measured]** `curl raw.githubusercontent.com/golang/net/master/http2/server.go` |
| 32 | `mise.toml` pins Go `1.26.3`, `gofumpt 0.10.0`, `golangci-lint 2.12.2`, `treefmt 2.5.0`; `treefmt.toml` formats Go with **`gofmt`**; `make check` = `treefmt` + `git diff --exit-code`; `make test` = `go test ./...` + `go build ./example/...`; neither `go vet` nor `golangci-lint` runs in CI | **[source]** `mise.toml`, `treefmt.toml`, `Makefile`, `.github/workflows/ci.yaml` |
| 33 | Baselines on unmodified `v0.13.5`: `gofmt -l` and `gofumpt -l` empty; `go vet` → 12 × `unreachable code`, all in `types_gen.go`; `staticcheck` and `golangci-lint` → the same 2 × `S1016` at `acp_test.go:1306,1405` | **[measured]** runs this session; `scratchpad/baseline-*.log` |
| 34 | A `go 1.21` module: three closures over one range loop observe `[3 3 3]`; under `go 1.22`, `[1 2 3]` — same go1.26.6 toolchain | **[measured]** `scratchpad/loopvar_proof.py` |
| 35 | 36 of 49 tests are `TestType_Action`; `waitForNotificationBarrierDrain` at `connection_notification_barrier_test.go:91`; a `time.Sleep(50ms)` wait at `acp_test.go:724`; `t.Parallel` in 3 files, none in `acp_test.go` | **[measured]** counted over `*_test.go` |
| 36 | `connection.go` log calls: `Error` 9, `Debug` 2, `Info` 1, `Warn` 0; no `sync.Once`/rate-limit pattern in library code | **[measured]** counted |
| 37 | Fork-PR CI runs of 2026-08-14/17 (#40's branch among them): `conclusion=failure` with `total_count=0` jobs; runs of 2026-08-29/09-01: `action_required`; scheduled `main` runs succeed | **[measured]** `gh run list`; `gh api …/actions/runs/32064390541/jobs` |
| 38 | PR #8, 2026-02-19: *"bounded queue with explicit overflow handling"*, *"risky for a library default"*; CHANGES_REQUESTED: *"fail fast with explicit connection cancellation and cause"*; Codex P1 on the blocking draft 2025-12-09; merged 2026-02-24 | **[measured]** `gh api repos/coder/acp-go-sdk/pulls/8/{comments,reviews}` |
| 39 | Maintainer's last comment `2026-06-01T14:24:51Z`, last review comment `2026-05-15`; others commenting through `2026-09-04`; **12** open PRs, **4** open issues; oldest open PR #40 (`2026-05-14`) | **[measured]** `gh api …/issues/comments`, `gh pr list`, `gh issue list` |
| 40 | `schema-v1.23.0` published `2026-09-18`, 17 `schema-v*` tags in the last 60 releases; Rust `jsonrpc.rs` `mpsc::unbounded()` × 4; TS `connection.ts` unchecked `queue.push`; spec-repo code search: backpressure 1 / "flow control" 0 / "slow consumer" 0 / windowing 0 / notification 116 | **[measured]** `gh api`, `curl` raw sources, `gh api search/code` |

### Findings

**F1 — The dependency is dormant, not merely stable.** `v0.13.5` is both the newest release
and the head of `main`, published `2026-06-02`; `upstream/main` is 0 commits ahead of the tag
(obs. 1, 2). As of this record's date that is **3 months and 20 days** with no commit. `go get
-u` would not move us. Consequence: "wait for upstream" is not a plan with a date attached.

**F2 — Bus factor one, and review is the bottleneck rather than disagreement.** 19 of 32
commits come from a single author (obs. 3), and the three PRs that would fix our two most
serious problems are all simultaneously `OPEN`, `MERGEABLE` and `BLOCKED` (obs. 4) — i.e.
they are rebasable and uncontested, and nothing merges them. **[second-hand]** research adds
that the first-time-contributor CI gate was never approved on them, so they have never even
been allowed to run, and that the maintainer's last comment on any thread was `2026-06-01`.
Consequence: the fixes we need are written, and the blocker is attention, not engineering.

**F3 — The teardown is deliberate, specified in review, and locked by upstream's own test.**
`acp_test.go:836` is literally named `TestConnectionFailsFastOnNotificationQueueOverflow` and
asserts the cancellation cause (obs. 8). **[second-hand]** the design was requested by the
maintainer during review of PR #8 — an unbounded queue was rejected as "unbounded memory
growth … risky for a library default", and the review summary asked for "a **bounded**
notification queue … On overflow, **fail fast** with explicit connection cancellation and
cause". Consequence: **this must not be reported upstream as a bug.** The only viable upstream
ask is an *escape hatch*, which is exactly what PRs #40 and #50 propose.

**F4 — `1024` is an unargued number.** The constant has been `1024` since at least `v0.11.7`
(obs. 14's tag walk covered the same lines). **[second-hand]** the contributor who
implemented the bound explicitly asked the maintainer what a sensible default should be and
whether it should be configurable, and received no answer. Consequence: there is no design
rationale to respect when we change it — only the bounded-vs-unbounded principle of F3.

**F5 — Depth is not the lever, and neither is our handler's cost.** 0166 measured that a
notification handler which does **nothing** still loses the transport 3 in 5 runs under
`GOMAXPROCS=1`, while `session_faulted=0/5` (obs. 28). The reader outruns the consumer
because the consumer is not scheduled, not because the consumer is slow. Consequence: raising
1024 to 8192 moves a threshold; it does not remove a failure mode. This is the single most
important finding for choosing *what* to patch.

**F6 — There is no configuration surface at all.** Not merely "no queue-depth option": the
package exposes no `ConnectionOption`, no `Config`, and no variadic parameter on any of the
three constructors (obs. 9), and the constant is unexported (obs. 10). Consequence: F5's fix
is unreachable from a consumer. This is the API-level confirmation of 0166 F9, which had
inferred it from the constant alone.

**F7 — "Just block the reader" is not available, for two independent reasons — and the second
is the real one.**

*Mechanism 1 (lock inversion) **[inferred]*** — the producer holds `notifyMu` across the
enqueue (`connection.go:428-433`), while the consumer must re-acquire `notifyMu` after
`handleInbound` (`:499`) before it can loop back and dequeue (obs. 11). With the queue full and
the consumer inside a handler, the producer would block on the send holding the lock, and the
consumer would then block acquiring it. Reasoned from source, **not executed**. This one is
also *fixable* by releasing the lock before the send (F8).

*Mechanism 2 (head-of-line blocking of responses) **[source]*** — this one is not fixable by
reordering locks. Inbound **responses** are dispatched **inline on the reader goroutine**
(`c.handleResponse(&msg)`, `connection.go:397`), while inbound requests are handed to their own
goroutine (`:411-421`) (obs. 29). A reader blocked on a queue send therefore stops delivering
**responses to our own outbound requests**. If anything on the notification-consumer path is
waiting for such a response, the connection deadlocks outright. This is the hazard upstream's
reviewer predicted when a blocking send was first proposed, and the class of failure already
seen and fixed once in this codebase (upstream #9, "client hangs after sending permission
response") **[second-hand]**.

Consequence: blocking the reader is unsafe on a multiplexed bidirectional stream, which is
exactly what `x/tools` says in code (F11). State this plainly so that a later reader does not
"simplify" the fix back into a blocking send.

**F8 — But a correct fix is tractable, because there is exactly one producer.** Only the
`receive` goroutine enqueues, and the SDK's own panic text asserts it:
`panic("notification sequence advanced while receive goroutine was queueing")`
(`connection.go:437`). Consequence: the sequence counter does not need the mutex to *order*
enqueues, only to publish state to `waitNotificationsUpTo`. Assigning the sequence under the
lock, releasing it, then enqueueing preserves ordering without holding a lock across a
blocking operation.

**F9 — A slow notification handler also delays our own RPC responses.**
`waitNotificationsUpTo` (`connection.go:773-799`) blocks a response until every notification
enqueued before it has been *processed*, not merely received (obs. 12). Consequence: the
single-consumer design couples notification-handling latency to request latency, so the
blast radius of a slow handler is wider than 0166 described — it is not only "the transport
may die", it is "every in-flight request on this connection is delayed".

**F10 — The SDK's own overflow policy is internally inconsistent.** The same file drops on a
full queue for `$/cancel_request` — `"dropping $/cancel_request due to full queue"` at
`connection.go:730`, guarded at `:722` against the same `1024` bound (obs. 13) — while
tearing the connection down for notifications. Consequence: drop-on-full is already an
accepted policy inside this codebase for one message class, which materially strengthens an
upstream argument for an escape hatch on the other.

**F11 — The Go ecosystem has converged on the opposite design, and says why in code.**
`x/tools/internal/jsonrpc2_v2/conn.go` (mirrored in the official MCP Go SDK) states: *"we
cannot in general limit the size of the handler queue: we have to read every response that
comes in on the wire … and in order to get to that response we have to read all of the
requests that came in ahead of it"* (obs. 26, verified verbatim). **[second-hand]** research
surveyed nine comparable Go JSON-RPC/ACP libraries and found four designs — block the reader,
unbounded goroutines, unbounded queue with a serial consumer, and bounded-queue-plus-teardown
— with `coder/acp-go-sdk` the **only** instance of the fourth, and both official ACP reference
SDKs (Rust, TypeScript) unbounded. Consequence: an unbounded inbound queue with a serial
consumer is the ecosystem-normal choice, not an exotic one.

**F12 — The protocol neither permits nor forbids any of this.** Across the bundled schema,
**0 of 488** descriptions in 129 definitions mention backpressure, flow control, dropping or
queue capacity (obs. 25). **[second-hand]** the same search over the specification repository
returns zero files for `flow control`, `windowing`, `credit`, `back-pressure` and `slow
consumer`, with the single `backpressure` hit sitting in a *rejected-alternatives* list of a
draft transport RFD. Consequence: there is no delivery guarantee to violate and no drop
licence to claim; the policy is the library's to justify, and therefore ours to choose in a
fork. It also means we must not describe the SDK's response barrier as spec compliance.

**F13 — A library that panics takes our daemon with it, and this is new.** There are 10
`panic()` calls in `connection.go`, all in the notification sequencer, and the tag walk shows
**0 at `v0.11.7` and 10 at `v0.12.0`** (obs. 14) — they arrived with the ordering barrier.
Two sit directly in the overflow path (`:437`, `:443`). Consequence: an invariant violation
in a third-party library is an unrecoverable crash of a long-running daemon. No `recover`
exists on that goroutine, and none of our records had noticed this.

**F14 — Two further latent failure modes are unrecorded anywhere in this repository.** A
single JSON-RPC frame larger than 10 MiB ends the connection through `scanner.Err()`
(obs. 15), and the package exports **no** error sentinels, so peer-disconnect must be
recognised from a `-32603` whose reason is buried in a `data` map (obs. 16). Consequence: the
first is a plausible cause of an unexplained disconnect on a very large `session/update`
frame and we have never looked for it; the second is why our own error handling reaches for
structured-then-string matching (`session.go:2458-2476`).

**F15 — `InitializeResponse` does carry `_meta`, so one use of our `unsafe` cast is
unnecessary.** Records `0038:112-119`, `0039-MADR:254-256` and `0039-PLAN:65-68` state that
the typed response has no top-level `Meta` and that `_meta` is discarded. At `v0.13.5` the
field exists (obs. 19), and our `grokInitializeMeta` (`acpagent.go:1040-1053`) reads **only**
`_meta`. Consequence: `initialize` can use the SDK's public typed `Initialize` plus
`initResp.Meta`, removing it from the unsafe path. This is our bug, not the SDK's.

**F16 — Extension notifications are routed, which closes an open question from 0081.**
`client.go:19` installs `handleWithExtensions` as *the* handler; `handleInbound` invokes it
**before** testing `req.ID == nil` (`connection.go:582-584`); and the notification branch
deliberately tolerates `-32601` for `_`-prefixed methods with the comment *"Per ACP, unknown
extension notifications should be ignored"* (`:585-587`) (obs. 20). Records `0038:157-161`
and `0039-MADR:460-463` say the SDK "drops these silently"; that was true of an earlier
version and is false at `v0.13.5`. Consequence: `0081-PLAN:444-448` left SDK routing of
extension notifications as an explicitly unconfirmed claim. It is now confirmed, from source.

**F17 — Two of the three methods on the unsafe path genuinely need it; one does not.**
`ClientSideConnection` exposes `Initialize` (`client_gen.go:226`) and `ResumeSession`, so
`rewind_test.go:469-476`'s claim that the SDK "does not model" them is wrong. But the reasons
differ per method: `initialize`'s payload is under `_meta` (F15, removable);
`session/resume`'s is a **top-level `models` object** that `ResumeSessionResponse` has no
field for (obs. 19's sibling read: `{Meta, ConfigOptions, Modes}`); and `session/set_model`
does not exist in the SDK in any form (obs. 24). Consequence: the unsafe surface goes from
three methods to two — not to one, and not to zero.

**F18 — The `unsafe` cast is removable outright by a one-line upstream change.**
`SendRequest[T any](c *Connection, …)` is already exported (obs. 21), and `CallExtension` is
literally `SendRequest[json.RawMessage](c.conn, …)` gated behind a `strings.HasPrefix(method,
"_")` check (`extensions.go:89-94`, `:23-31`). The only thing forcing
`*(**acp.Connection)(unsafe.Pointer(c))` (`session.go:2579-2582`) is that `conn` is private
with no accessor. Consequence: `func (c *ClientSideConnection) Connection() *Connection`
deletes our most fragile construct — one that is sound today only because `conn` happens to
be the first field (`client.go:13-15`), and that a field reorder upstream would silently
break.

**F19 — The logger defect is a data race, not a construction race, and the fix is already
written.** `SetLogger` is a bare field assignment (`connection.go:125`) read by
`loggerOrDefault` (`:127`) from goroutines started inside the constructor (`:111-119`), with
no mutex on `logger` (`:80`) (obs. 18). Our own comment at `acpagent.go:496-501` describes it
as a construction-time race; it is an unsynchronized concurrent access by the memory model.
Two independent upstream issues report it (#57, #58) and PR #59 fixes it with
`atomic.Pointer[slog.Logger]` and no API change (obs. 4, 5). Consequence: the cost we
currently pay — SDK protocol diagnostics going to `slog.Default()` without session fields
(`acpagent.go:507-511`) — is recoverable.

**F20 — We are not an edge case.** 30 GitHub issues and PRs across all public repositories
match `"notification queue overflow" acp` (obs. 6), including closed fixes in unrelated
projects. **[second-hand]** research characterised four independent downstream mitigations,
three of which converge on the same answer — make the handler unable to block and do the work
elsewhere — which is what our parked-overflow design (`session.go:1473-1520`) already does;
and one project pins a patched fork behind a `go.mod replace` today. Consequence: a fork is a
trodden path here, and our existing handler design is already the recommended half of the fix.

**F21 — Three of our recorded claims check out exactly, line numbers included.**
`SessionUsageUpdate` has no cache fields at `types_gen.go:5243` (obs. 22), `NewSessionRequest`
has no `mode` at `:3236` (obs. 23), and the SDK has no model concept at all (obs. 24).
Consequence: L5, L3a and L3b in the inventory stand unmodified, and the vendor `_x.ai`
channels built on them (`xaiusage.go`, `thinking.go`) remain necessary.

**F22 — The generated types are ten schema releases stale. [second-hand]** The SDK's
`schema/version` file reads `0.13.5`; research reports the specification's stable line is at
`schema-v1.23.0` (2026-09-18) with a `v2` draft alongside, and that the two PRs which would
re-point the generator are themselves stuck. Consequence: forking inherits a generator whose
download URLs are reported broken, so schema currency is a *separate* problem from the
availability defect and must not be bundled into the same decision.

**F23 — Go sanctions teardown-on-bound only as an attack mitigation against a peer, never as a
response to a slow local consumer.** The single instance of the pattern in Go-team code is
`x/net/http2`, and the code says why: `maxQueuedControlFrames = 10000`, commented *"the maximum
number of control frames … that will be queued for writing before the connection is closed
**to prevent memory exhaustion attacks**"*, with the teardown site reading *"If the peer is
causing us to generate a lot of control frames, but not reading them from us, **assume they are
trying to make us run out of memory**"* (obs. 31, verified verbatim). `coder/acp-go-sdk` applies
the same remedy at **1024**, to a well-behaved consumer falling behind on a legitimate burst.
**[second-hand]** research adds two Go-team verdicts on the general question — the enumerated
responses to a consumer falling behind are *drop, summarise, or escalate*, and closing the
connection appears on neither list; one argues directly that it is *"better to drop some logs
when the buffer gets full, rather than dropping the entire program state"*, which is precisely
what a teardown does to every session on our engine. Consequence: this asymmetry is the
strongest single argument available for the upstream ask, because it is Go's own code with the
rationale written in the comment rather than an appeal to our own inconvenience.

**F24 — There is a fifth design we should not omit: bounded, blocking, but partitioned by
message class. [second-hand]** `grpc-go` bounds its control buffer and **blocks its reader**
when it fills — safely, because the classes that could self-deadlock are exempt, and the bound
is documented and tunable (default 100, range 1–10000, via an environment variable described as
*"an escape hatch to increase the throttling limit if unforeseen issues arise"*). That design
squares F7's hazard with F25's objection, and `acp-go-sdk` already has an embryonic version of
it: `$/cancel_request` is handled synchronously ahead of the switch *"so cancellations take
effect immediately and do not participate in notification ordering"* (obs. 30). Consequence: a
partitioned bounded design is the most defensible *upstream* proposal, though it is more work
than this project should undertake unilaterally — see Option G.

**F25 — The honest tension in choosing "unbounded". [second-hand]** Go's general guidance is
*against* unbounded channels, because an unbounded buffer destroys backpressure, and the
reference the Go team endorsed on this topic argues that a bigger buffer *"makes failures more
rare, but makes their magnitude worse"*. That objection is real, and it is the same one that
produced this bound (F3). It is answered here rather than dismissed: bounding is unsafe on this
specific stream shape (F7 mechanism 2), which is the exception `x/tools` documents in code
(F11); and backpressure does not vanish, it moves to the only component that can apply it
correctly — our handler, which returns in O(1) and parks at an explicit cap. Consequence: this
record must not present unbounded as uncomplicatedly correct. It is correct *here*, conditional
on our handler keeping its contract, and D3's consequences say so.

**F26 — The options surface we would need already exists in an open PR. [measured]** PR #40's
diff adds `ConnectionOption func(*connectionConfig)`, threads `opts ...ConnectionOption` through
all three constructors, and ships a 153-line test — `4 files changed, 190 insertions(+)`. PR #50
independently exports **both** `ErrNotificationQueueOverflow` and `ErrPeerDisconnected`
(`errors.go`, +17) alongside its own `WithMaxQueuedNotifications`. Consequence: proposing a
capacity option or exported sentinels would duplicate two open PRs, which is poor contribution
etiquette and adds nothing.

**F27 — Neither open PR changes what happens when the queue fills. [measured]** Both #40 and #50
keep the `default:` branch's `shutdownReceive(errNotificationQueueOverflow)`; they let a caller
choose *when* the transport dies, not *whether*. Combined with F5 — a null handler still loses the
transport 3/5 — this is the unclaimed gap: the **policy**, not the capacity. Consequence: an
overflow policy is the one contribution here that is both novel and sufficient, and it composes
with #40 rather than competing with it.

**F28 — Go's precedent for this exact trigger is unanimous, and it is an enum or a fixed policy —
never a decision callback. [second-hand, with three spot-checks measured]** A survey of 19 Go
libraries with a bounded buffer found **zero** whose overflow or loss callback returns a decision;
every one returns nothing (`jaeger onDroppedItem func(item any)`, `zerolog Alerter func(missed
int)`, `nats ErrHandler`, `zap SamplerHook`, `golang-lru EvictCallback`, `bigcache
OnRemoveWithReason`, …). Where the choice *is* configurable it is an enum or a bool —
`k8s.io/apimachinery/pkg/watch.FullChannelBehavior` (`WaitIfChannelFull` / `DropIfChannelFull`),
`prometheus/promhttp.HandlerErrorHandling`, `m3 OnFullStrategy`. Three checks I ran myself:

* **Google's Go style guide states the rule outright** — *"An enumerated option should accept an
  enumerated constant"*, and *"binary settings should accept a boolean"*. **[measured]**
* **OpenTelemetry Go, the closest analogue, uses a bool and a metric** — *"If the queue gets full
  it drops the spans. Use BlockOnQueueFull to change this behavior"*, with `BlockOnQueueFull bool`
  and `WithBlocking()`; loss is surfaced by an unexported counter, a debug log and an experimental
  metric, **never a user callback**. **[measured]**
* **The same library uses a decision-returning interface for *sampling*** —
  `ShouldSample(SamplingParameters) SamplingResult` with `SamplingDecision`/`Drop`, carrying the
  comment *"DO NOT CHANGE: any modification will not be backwards compatible"*. **[measured]**

Consequence: Go reaches for a decision callback when the decision is a **semantic** one the
consumer owns (which certificate to trust, which trace to keep), and for **resource-pressure**
events it uses a fixed policy or an enum plus a notification-only observer. The same team, in the
same package, drew that line exactly where this record now draws it.

**F29 — The appeal of a decision callback for *our* use case is illusory, and the appeal is the
footgun.** The argument for handing the callback `params json.RawMessage` was that we already
extract `SessionId` from notification params (`session.go:1576`) and fault per session
(`session.go:1448-1457`), so we could attribute lost output to one session instead of warning
across all of them. But the overflow site is on the **reader goroutine** (`connection.go:429`), so
doing that means parsing JSON on the read loop at the precise moment the queue is overflowing —
deepening the backlog that caused the drop. The capability we wanted is the capability we must not
use. Consequence: once that is removed, what our use case actually needs is only *that* drops
happened and *how many* — which a notification-only handler and a counter supply. **The
per-method distinction remains genuinely desirable** (F30), but not at this call site.

**F30 — The per-method severity argument is real, and is answered by migration order rather than
by API shape.** Notification loss in ACP is not uniform: on the client side the only notification
is `session/update` (a visible gap), but on the **agent** side it is `session/cancel`
(`agent_gen.go:291`) — losing one means the agent keeps working on an abandoned turn. The unstable
schema grows this to a `document/did*` family (`agent_gen.go:25`,
`UnstableDidChangeDocument(...) error`), which is high-frequency coalescible editor traffic sitting
in the same queue as the cancellation that must never be dropped. A single enum cannot express
"coalesce `didChange`, never drop `cancel`". Consequence: this is a reason to keep the door open,
not to start there — **adding a handler beside an enum later is additive; starting with a frozen
func signature forecloses the enum.** The stdlib has twice paid the reverse cost
(`net.Dialer.Control` needing `ControlContext`; `tls.VerifyPeerCertificate` needing
`VerifyConnection`).

**F31 — Upstream's real gates, measured, and what "clean" means against them.** `mise.toml` pins
**Go 1.26.3**, `gofumpt 0.10.0`, `golangci-lint 2.12.2`, `treefmt 2.5.0`. `make check` is
`treefmt` followed by `git diff --exit-code`; `treefmt.toml`'s Go formatter is **plain `gofmt -w`**,
so CI enforces `gofmt`, while `AGENTS.md` asks contributors for `gofumpt` — a strict superset, so
running `gofumpt` satisfies both. `make test` is `go test ./...` plus `go build ./example/...`.
**Neither `go vet` nor `golangci-lint` runs in CI.** Baselines on unmodified `v0.13.5`
**[measured]**: `gofmt -l` and `gofumpt -l` empty; `go vet` reports 12 pre-existing `unreachable
code` findings, all in generated `types_gen.go`; `staticcheck` and `golangci-lint` (v2 defaults)
each report the same 2 pre-existing `S1016` findings in `acp_test.go:1306,1405`. Consequence:
"clean" for the PR means **no new finding over those baselines**, and the PR must not "fix" the
pre-existing ones — that is unrelated churn in someone else's diff.

**F32 — `go 1.21` in `go.mod` gives per-loop variable semantics, proven, not recalled.** A
three-closure range loop compiled with `go 1.21` observed `[3 3 3]`; with `go 1.22`, `[1 2 3]` —
on the same Go 1.26.6 toolchain **[measured]**. Consequence: any table-driven test in the patch
that captures the row in a closure or goroutine needs `tc := tc`; and the patch may use nothing
newer than 1.21 (`atomic.Uint64` is 1.19, `log/slog` and `slices` are 1.21; range-over-int and
range-over-func are not available).

**F33 — Upstream's test conventions, counted rather than assumed.** 36 of 49 tests use the
`TestType_Action` form `AGENTS.md` asks for; 13 do not, including the overflow test itself. A
deterministic barrier helper exists — `waitForNotificationBarrierDrain(t, c, timeout)` at
`connection_notification_barrier_test.go:91` — and the existing overflow test
(`acp_test.go:836-881`: `io.Pipe`, a blocked first handler, fill the queue, one extra) is the
fixture shape to reuse. `TestConnectionHandlesNotifications` (`acp_test.go:724`) waits with
`time.Sleep(50 * time.Millisecond)` — a pattern that exists upstream and must **not** be copied.
`t.Parallel` is used in 3 files, never in `acp_test.go`. Consequence: name new tests
`TestConnection_…`, reuse the overflow fixture, wait on the barrier helper, and do not add
`t.Parallel` to a pipe-driven connection test.

**F34 — Logging conventions fix the drop log level.** `connection.go` uses `Error` 9 times,
`Debug` 2, `Info` 1 and **`Warn` never**; the in-house drop-on-full at `:730` logs at `Debug`; and
no once-only or rate-limited logging pattern exists anywhere in library code **[source]**.
Consequence: log each drop at `Debug` with `method`, `capacity`, `queued` and `dropped_total`,
leave the `Error` on the close path untouched, and rely on the handler and counter for
observability — a per-drop `Warn` under a 4,096-notification burst would itself be a log flood on
the reader goroutine.

**F35 — No fork PR has ever produced a CI signal on this repository, and ours will not either.**
All four recent fork-PR runs (`configurable-notification-queue` = #40, `chore/schema-1.20.0`,
`fix/schema-release-tag`, `ci/schema-update-automation`; 2026-08-14/17) are `conclusion=failure`
with **`total_count=0` jobs**, and the 2026-08-29/09-01 runs are `action_required`
**[measured]**; scheduled `main` runs succeed. A run that fails with zero jobs failed before any
step could run. **[inferred]** the cause is the workflow's runner selection —
`runs-on: ${{ github.repository_owner == 'coder' && 'depot-ubuntu-latest' || 'ubuntu-latest' }}`
(`ci.yaml`) — which resolves to a Depot runner for fork PRs too, and a fork PR cannot be scheduled
on it; the exact error is not retrievable (logs gone, no check-run annotation), so this stays
inferred. Consequence, which holds either way: **a green check is unattainable for our PR
regardless of its quality**, so the PR body must carry the local verification transcripts that CI
would otherwise provide; and this is also why #40, #50 and #59 have never had a signal — not
because they are wrong.

**F36 — No README change.** `README.md` is regenerated by `mdsh` (`Makefile`, `README.md:
schema/version` target) and `make check` diffs it; it documents no constructor options today; #40
did not touch it. Consequence: godoc is the documentation surface for this option, and touching
the README without `treefmt` available locally risks a formatting diff we cannot verify.

**F37 — The maintainer's own words, read from the PR #8 thread. [measured]** 2026-02-19, review
comment on `connection.go:66`: *"This introduces unbounded memory growth, which is risky for a
library **default**. Can we switch to a bounded queue with **explicit overflow handling**?"* Same
day, CHANGES_REQUESTED summary: *"Use a **bounded** notification queue instead of an unbounded
one. On overflow, **fail fast with explicit connection cancellation and cause** (no fallback to
concurrent handling)."* Earlier, 2025-12-09, the Codex reviewer flagged the blocking-send draft
P1: *"Notification backlog can deadlock reader."* PR #8 merged 2026-02-24. Consequence: F3 is now
measured rather than second-hand, and the framing improves — the maintainer asked for *explicit
overflow handling* and objected to unbounded growth *as a default*. An overflow-policy option with
the default preserved is his own request, met literally.

**F38 — Exporting the sentinel here would duplicate #50 and conflict with it.** #50 already adds
`var ErrNotificationQueueOverflow` in `errors.go` and keeps the unexported name as an alias
**[measured]**. Upstream's overflow test references `errNotificationQueueOverflow` by its
unexported name (`acp_test.go:875`), so the internal name must survive in any case (C1).
Consequence: D5 as first written contradicted D7; this PR does **not** export the sentinel and
instead credits #50 for it.

**F39 — `OverflowDropNewest` is safe for the response barrier, proven from source — with one
semantic caveat that must be documented.** `handleResponse` reads the watermark
(`lastEnqueuedNotificationSeq`) under `notifyMu` (`connection.go:530-537`) **on the reader
goroutine**; the overflow rollback (`lastEnqueuedNotificationSeq--`, `:439`) runs under the same
lock on the same goroutine before the loop continues. With a single reader, no response can be
handled between a failed enqueue and its rollback, so no watermark ever names a dropped sequence
number; the next notification reuses it, so `processNotifications`' contiguity check
(`queued.seq == completedNotificationSeq+1`, `:500-503`) holds by construction. **The caveat:**
the pre-response barrier (`waitNotificationsUpTo`, `:773`) guarantees that *enqueued*
notifications are processed before a response is returned — a dropped notification was never
enqueued, so `SendRequest` may return although a notification that preceded the response on the
wire was discarded. Consequence: this is inherent in any drop policy and must be stated in
`OverflowDropNewest`'s doc comment; it is also the honest answer to a reviewer who asks "what does
the barrier mean now".

**F40 — Our own consumer, evaluated against the chosen shape.** We build the connection with
`acp.NewClientSideConnection(s, stdin, s.wire.TeeReader(stdout))` (`acpagent.go:494`); we have a
generic per-session `TypeNotice` event (`event.go:42`) to surface "output may be incomplete"; our
`deliver` is O(1) and parks at `controlOverflowCap = 512`. With `OverflowDropNewest`, a handler
call arrives on the reader goroutine; the correct consumer behaviour is to record a counter and
raise the notice from our own goroutine — never to parse `params`. Consequence: our use case is
fully served without the callback receiving `params`, which independently confirms F29; adoption
stays out of scope (D13).

**F41 — Reference SDKs are unbounded, and the spec repository is silent — both measured now.**
Rust `rust-sdk` `jsonrpc.rs`: `mpsc::unbounded()` four times; TypeScript `typescript-sdk`
`connection.ts`: `private readonly queue: Message[] = []` with an unchecked `push`. GitHub code
search over `agentclientprotocol/agent-client-protocol`: `backpressure` 1 file, `"flow control"`
0, `"slow consumer"` 0, `windowing` 0 — against `notification` in 116 files as the control.
Consequence: F11's reference-SDK claim and F12's spec-silence claim are no longer second-hand.

**F42 — Maintenance state, measured to the day.** Maintainer's most recent comment anywhere:
`2026-06-01T14:24:51Z` (issue #44); most recent review comment: `2026-05-15` (on #40). Other
people kept commenting through `2026-09-04`. **12 open PRs, 4 open issues**; the oldest open PR
is #40 (`2026-05-14`). The protocol is at `schema-v1.23.0` (`2026-09-18`) with **17** `schema-v*`
releases among the last 60, against the SDK's `schema/version` of `0.13.5`. Consequence: F1, F2
and F22 are measured; nothing in them was overstated.

**F43 — The remaining cited authorities, verified by identifier.** Go-team comments fetched by
comment ID: rsc on golang/go#20352 (`2017-06-21`, *"The limited capacity of channels is an
important source of backpressure… If one goroutine falls sufficiently behind, you usually want to
take some action in response, not just queue its messages forever"*), bcmills on #27935
(`2018-10-01`, *"unbounded queues are not usually what you want in a well-behaved program"*),
ianlancetaylor on #20352 (`2018-02-13`). `grpc-go` `internal/transport/controlbuf.go`:
`maxQueuedControlBufferItems`, `throttle()`, and `ControlBufferThrottleLimit` from env with
default `100`, range `1–10000`. #59's diff: `logger atomic.Pointer[slog.Logger]`, `Store`/`Load`,
plus `connection_logger_test.go`. Consequence: F19, F23 and F24 are measured.


## Decision Drivers

* **The deliverable is an upstream contribution.** The objective is a merged enhancement that
  helps every consumer of this SDK, not a private build. A fork that only we run is a failure of
  this record's purpose even if it fixes our symptom.
* **Merge probability is a first-class design constraint.** With a dormant, single-maintainer
  project (F1, F2), a large or contentious diff is indistinguishable from no fix at all. Small,
  backward-compatible and uncontroversial beats theoretically ideal.
* **Do not contradict the maintainer's stated requirement.** The bound exists because he asked for
  bounded memory and fail-fast overflow (F3). A proposal that reverses that decision re-opens a
  settled argument and will stall exactly as the unbounded attempt did.
* **Do not duplicate open work.** #40 and #50 already cover the options surface, capacity and
  exported sentinels (F26). Our contribution must compose with them and credit them.
* **Default behaviour must not change.** Anything that alters existing consumers' behaviour, or
  that requires editing upstream's tests, raises review cost and risk (F27, and upstream's
  `acp_test.go:836`).
* **Loss must never be silent.** Whatever the new policy does, a consumer has to be able to tell
  it happened; an invisible gap in a streamed turn is worse than a visible one.
* **Our own defects are ours to fix now.** Four of our records are wrong and one `unsafe` cast is
  unnecessary (F15–F19). None of that depends on upstream, so none of it waits for upstream.

## Considered Options

* **A — Contribute a notification **overflow policy** layered on #40's `ConnectionOption`
  surface, defaulting to today's exact behaviour.** (chosen)
* **B — Contribute an unbounded notification queue,** matching `x/tools` and the official MCP Go
  SDK.
* **C — Contribute a partitioned bounded queue that blocks the reader,** `grpc-go` style, moving
  response dispatch off the reader goroutine first.
* **D — Re-propose configurable capacity** (a third variant of #40/#50).
* **E — Carry a patched fork in our own build via `go.mod replace`,** and treat upstream as
  optional.
* **F — Contribute nothing; keep 0166's containment and accept the blast radius.**

## Decision Outcome

Chosen: **Option A — an overflow *policy* option, stacked on PR #40, with the existing
fail-fast behaviour as the default.**

The shape is forced by four findings acting together. F27 says capacity is already claimed and
does not fix anything; F5 says only a change of policy removes the failure mode; F3 says the
maintainer requires memory to stay bounded; and F7 mechanism 2 says the reader must never block.
A bounded queue whose overflow behaviour is *chosen by the consumer* is the only design that
satisfies all four at once — and, unlike every alternative, it needs **no edit to upstream's
existing tests**, because the default path is untouched.

Option E is explicitly rejected as the *goal*, though the fork remains the development vehicle:
a private patch fixes one consumer and leaves the defect in place for everyone else, which is the
opposite of this record's purpose.

The honest cost of Option A is that it makes loss possible where today there is none — see D4 and
the Consequences. That trade is defensible only because the alternative is not "no loss", it is
losing every session on the connection (F5, 0166 F3).

### The decisions

**D1 — The deliverable is an upstream pull request. Our build does not change.** `go.mod` keeps
`github.com/coder/acp-go-sdk v0.13.5` with **no `replace` directive** committed at any point.
0166's containment stays exactly as it is, so if the PR is never merged nothing has regressed and
nothing has been abandoned half-done.

**D2 — The enhancement is a notification overflow *policy*, not a capacity knob — and its shape is
an enumerated option, not a decision callback (F28, F29).** Layered on the `ConnectionOption` type
PR #40 introduces (F26):

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
    // leaves the connection open. Delivered notifications keep their relative
    // order; the dropped one is never delivered and is not retried.
    OverflowDropNewest
)

func WithNotificationOverflowPolicy(p NotificationOverflowPolicy) ConnectionOption

// WithNotificationDropHandler registers fn, called once per dropped notification
// under OverflowDropNewest. It runs on the connection's reader goroutine and MUST
// NOT block: while it runs nothing is read, which deepens the backlog that caused
// the drop. It must not call back into the Connection.
func WithNotificationDropHandler(fn func(method string, totalDropped uint64)) ConnectionOption
```

Two naming decisions, both taken from precedent rather than taste: the constant is named for its
**effect** (`OverflowCloseConnection`) not its attitude (`OverflowFailFast`), matching
`WaitIfChannelFull` and `OnFullStrategy`; and it is `OverflowDropNewest` rather than
`OverflowDrop`, which reserves `DropOldest` and `Coalesce` for later and is accurate about what the
code does — the arriving notification is discarded, because upstream's existing sequence rollback
(`connection.go:435-445`) makes exactly that cheap and correct.

**Semantics fixed by this record, so they are not improvised in review:**

* Any `NotificationOverflowPolicy` value other than `OverflowDropNewest` behaves as
  `OverflowCloseConnection`. No panic, no error — matching `WithMaxQueuedNotifications`, which
  silently ignores `n <= 0` (#40).
* The handler is invoked only for an actual drop, only under `OverflowDropNewest`, **after**
  `notifyMu` is released (the existing branch already unlocks before it logs, `connection.go:445`),
  and synchronously on the reader goroutine. A handler that panics is not recovered — the SDK
  recovers no handler panics anywhere, and `kafka-go` documents the same choice.
* Each drop is logged at `Debug` with `method`, `capacity`, `queued` and `dropped_total` (F34).
  The `Error` on the close path is unchanged.
* `OverflowDropNewest`'s doc comment states two things a caller must know: the barrier caveat
  (F39) and that on an agent-side connection the notification being dropped may be
  `session/cancel` (F30).
* Extension (`_`-prefixed) notifications are subject to the same policy; nothing is special-cased.

**The handler deliberately does not receive `params`, and does not return a decision.** Both were
considered and rejected: see F29 for why handing over `params` invites JSON parsing on the reader
goroutine mid-overflow, and F30 for why a decision callback would freeze a signature and foreclose
the cheaper migration.

**D3 — The default must be byte-identical to today, and upstream's existing overflow test must
pass unmodified.** `TestConnectionFailsFastOnNotificationQueueOverflow` (`acp_test.go:836`) is the
guard on the maintainer's chosen default; a contribution that has to rewrite it is arguing with
him rather than extending his design. Keeping it green is the single strongest signal the PR can
carry.

**D4 — Implement the non-fatal policy as "drop the newest notification", reusing upstream's own
rollback path.** The existing `default:` branch already decrements
`lastEnqueuedNotificationSeq` and re-checks the completed-sequence invariant
(`connection.go:435-445`) before tearing down. The policy change replaces only the final two
statements — the log and `shutdownReceive` — with a counter, a callback and `continue`. Ordering,
sequencing and the barrier are untouched by construction, which is what makes the diff small
enough to review. Dropping the *newest* preserves a contiguous processed prefix and needs no
eviction from the middle of the queue.

**D5 — The library must not let a consumer choose lossiness without choosing observability.** Two
mechanisms in this PR, and one deliberately delegated:

1. `WithNotificationDropHandler`, called once per drop (D2);
2. `func (c *Connection) DroppedNotifications() uint64` — an atomic read, matching the existing
   `nextID atomic.Uint64` field style, so a consumer that wants no callback can still poll;
3. **not** an exported `ErrNotificationQueueOverflow`: #50 already does exactly that (F38), and
   duplicating it would both violate D7 and create a merge conflict with a PR we want to see land.
   The PR body credits #50 for it.

`OverflowDropNewest`'s doc comment must say that selecting it obliges the caller to install a
handler or read the counter. This is `promhttp`'s discipline — its `HandlerOpts` documents that
"errors are logged regardless of the configured ErrorHandling provided ErrorLog is not nil" — and
it is the specific failing of `watch.Broadcaster`, which pairs a correct enum with **zero**
instrumentation on the drop path. The SDK already has a logger surface (`SetLogger`,
`connection.go:125`) and already logs this event, so the floor is not silence.

**D6 — Do not propose unbounded, and do not propose blocking.** Unbounded reverses the
maintainer's explicit requirement (F3) and was already rejected once in this codebase's history;
blocking deadlocks the connection because responses are dispatched inline on the reader goroutine
(F7 mechanism 2). Both are recorded in Considered Options with their real arguments so a future
reader does not mistake omission for ignorance.

**D7 — Stack on #40, credit it, and do not duplicate it or #50.** The PR branches from #40's head
and says so; if #40 merges first it rebases to a small diff, and if the maintainer prefers a single
change we offer to carry both. Comment on #40 and #50 pointing at the policy gap (F27) rather than
opening a competing capacity PR.

**D8 — Endorse #59 for the logger race; do not write a rival fix.** Two issues (#57, #58) and one
ready PR already exist, and the PR's approach — `atomic.Pointer[slog.Logger]`, no API change — is
the right one (F19). Independent confirmation is worth more than a duplicate patch.

**D9 — Submit the `ClientSideConnection.Connection()` accessor as its own small PR.** It is
genuinely unclaimed, it is one line plus a test, and it removes the need for *any* consumer to do
what we currently do with `unsafe.Pointer` (F18). Keep it separate so it cannot be held up by the
policy discussion.

**D10 — Fix our own defects now, independently of upstream.** Correct the four false claims
(F15–F19) and take `initialize` off the raw path (F15). Neither waits on anyone. **Our
`unsafe.Pointer` cast stays until D9 is merged** — deleting it depends on upstream API, so it is
sequenced after, not before.

**D11 — The fork is a development vehicle only.** Branches, upstream's test suite, and a
*temporary, never-committed* `replace` for local verification. Its value is that it lets us prove
the patch against our real workload before proposing it — which is the difference between a
credible PR and a plausible one.

**D12 — Prove the patch against our own reproduction before submitting.** The PR must be able to
state that the policy was exercised by a real consumer under the conditions that produce the
failure (`GOMAXPROCS=1`, a starved consumer), not only by a synthetic unit test. This is the
evidence #40 and #50 lack, and the reason our contribution can succeed where theirs stalled.

**D13 — Out of scope:** the sequencer panics (F13) and the 10 MiB frame cap (F14), both worth
separate PRs later; schema currency (F22); per-engine blast radius (0166 F3); switching SDKs;
writing our own client.

**D14 — The patch complies with upstream's standards as measured, not as remembered (F31–F34,
F36).** Concretely: `gofumpt -l .` empty (which also satisfies the CI-enforced `gofmt`); nothing
newer than Go 1.21, with `tc := tc` wherever a table row is captured (F32); tests named
`TestConnection_…`, built on the existing overflow fixture, waiting on
`waitForNotificationBarrierDrain` and never on `time.Sleep` (F33); **no new finding** from
`go vet`, `staticcheck` or `golangci-lint` over the recorded baselines, and no edits to the
pre-existing ones (F31); `make test` green, examples included; no README change (F36); imperative
commit subject under ~72 characters; a PR body that states protocol impact ("none"), generated-file
impact ("none") and the commands run, per `AGENTS.md`.

**D15 — The PR body carries the evidence CI cannot (F35).** Because no fork PR can produce a green
check on this repository, the PR includes verbatim transcripts of `make test`, `go test -race
./...`, `gofumpt -l .`, the vet/staticcheck/golangci baseline diffs, and the D12 reproduction in
both directions — and states plainly which upstream commands (`make check` via `treefmt`) were
**not** run locally if the tool is unavailable, rather than implying they were. This is also the
argument to make on #40/#50/#59: their lack of a CI signal is the repository's gate, not their
code.

### Consequences

* Good, because a merged policy option fixes the defect for every consumer of this SDK, including
  the four downstream projects that hit it independently (F20), rather than for us alone.
* Good, because our build is unchanged and therefore cannot regress: D1 means this record carries
  no runtime risk at all until upstream acts.
* Good, because the diff is small, additive, default-preserving and leaves upstream's tests
  untouched — the four properties most correlated with a stalled project merging something.
* Good, because D9 and D10 deliver real local improvements that are independent of the PR's fate.
* Neutral, because 0166's retired assertion stays retired. The property only becomes assertable
  if the policy merges *and* we adopt it, which is a later decision, not this one.
* Bad, because we remain exposed to the defect in the meantime. D1 accepts 0166's blast radius for
  as long as review takes, and F1/F2 say that may be indefinitely.
* Bad, because the chosen policy makes notification loss possible for consumers who opt in, and
  ACP gives no delivery guarantee to lean on either way (F12). D5 is the mitigation and is not a
  cure: a dropped `session/update` chunk is a visible gap in a turn.
* Bad, because stacking on #40 couples our PR's fate to another stalled PR. D7's fallback — offer
  to carry both — is the answer, but it is extra work we do not control.
* Bad, because `OverflowDropNewest` weakens what the response barrier means: it still guarantees
  that every *enqueued* notification is processed before a response is returned, but a dropped
  notification was never enqueued, so a response can arrive although a notification that preceded
  it on the wire was discarded (F39). This is inherent to any drop policy and is documented rather
  than hidden; a consumer for whom that ordering is load-bearing keeps the default.
* Bad, because the PR will never show a green check on upstream (F35), so its credibility rests
  entirely on the transcripts in its body (D15) — which is more work per PR and easier to get
  wrong than pointing at a CI run.

### Confirmation

```bash
# D3 — the load-bearing check: upstream's own suite, entirely unmodified.
cd <fork> && git diff --stat upstream/main -- acp_test.go   # expect: no changes to existing tests
go test ./...                                               # expect: all pass, including :836
go test -race ./...

# D3 — the default path is byte-identical: no option set means today's behaviour.
go test ./... -run TestConnectionFailsFastOnNotificationQueueOverflow -count=5

# D4 — the new policy must be seen to work AND to be off by default.
go test ./... -run TestNotificationOverflowPolicy -v

# D12 — proven against our real reproduction, with a temporary uncommitted replace.
GOMAXPROCS=1 go test ./internal/provider/acpagent/ -run StalledPump -count=20

# D1 — and then proven that our committed tree never carried the replace.
git -C . diff --exit-code go.mod go.sum                     # expect: clean
grep -n 'replace' go.mod                                    # expect: no match

# D10 — local fixes stand on their own.
grep -n 'rawRequest(initCtx, "initialize"' internal/provider/acpagent/acpagent.go   # expect: none
make pre-add-check && make race && make ci-windows

# D14 — standards compliance, measured against the recorded baselines (F31).
cd <fork>
gofumpt -l .                                   # expect: empty (superset of the CI-enforced gofmt)
go vet ./... 2>&1 | grep -v 'types_gen.go'      # expect: empty (12 pre-existing generated-code hits excluded)
staticcheck ./... 2>&1 | grep -v 'acp_test.go:1306\|acp_test.go:1405'   # expect: empty
golangci-lint run ./... 2>&1 | grep -c 'S1016'  # expect: 2, unchanged from baseline
make test                                      # go test ./... + examples build
git log -1 --format=%s | awk '{ print length }' # expect: < 72
grep -n 'tc := tc' connection_overflow_policy_test.go   # expect: present if any row is captured (F32)
grep -n 'time.Sleep' connection_overflow_policy_test.go # expect: none (F33)
```

The criterion most likely to be quietly skipped is **D12** — proving the patch against our own
starved-consumer reproduction rather than only against a unit test. It is the one piece of evidence
that distinguishes this PR from the two already sitting unmerged, and it is also the only one that
requires the temporary `replace` and is therefore the easiest to leave out.

## Pros and Cons of the Options

### A — Overflow policy layered on #40, default unchanged (chosen)

* Good, because it fills the one gap neither open PR addresses (F27) while composing with both.
* Good, because the default path is untouched, so upstream's existing tests pass unmodified —
  the clearest possible signal that the change extends the maintainer's design rather than
  reversing it (D3).
* Good, because it honours the stated memory requirement (F3): the queue stays bounded under every
  policy, so the argument that killed the unbounded attempt does not apply.
* Good, because the implementation reuses upstream's own invariant-rollback code, so the diff is
  roughly two statements plus an option, a counter and tests (D4).
* Good, because dropping is on the sanctioned list of responses to a consumer falling behind,
  where closing the connection is not (F23).
* Bad, because it introduces the possibility of silent loss, mitigated but not removed by D5.
* Bad, because it is opt-in: consumers who never set the option keep the defect, so the fix's
  reach depends on adoption as well as merging.
* Bad, because stacking on a stalled PR inherits its stall (D7).

### B — Contribute an unbounded queue

* Good, because it removes the failure mode outright rather than offering an alternative to it,
  and it is what the Go ecosystem does for this exact problem shape, with an in-code rationale to
  cite (F11).
* Good, because it needs no option surface and no policy vocabulary.
* Bad, because it directly reverses the maintainer's explicit review decision (F3), and the
  identical proposal was already rejected in this codebase's history for the identical reason.
* Bad, because it deletes `defaultMaxQueuedNotifications` and `errNotificationQueueOverflow`,
  which makes upstream's `acp_test.go:836` fail to compile — a PR that breaks the maintainer's
  test is arguing, not contributing.
* Bad, because Go's general guidance is against unbounded channels (F25), so the reviewer has a
  ready and legitimate objection.

### C — Partitioned bounded queue that blocks the reader (`grpc-go` shape)

* Good, because it is the only option that preserves real backpressure, answering F25 rather than
  working around it, and it has shipped prior art with a documented tunable (F24).
* Good, because it would make blocking safe *properly*, by moving response dispatch off the reader
  goroutine — fixing the structural cause rather than its symptom.
* Bad, because it is a structural rewrite of someone else's concurrency design, submitted to a
  project whose CI has not been approved to run on contributor PRs for a month (F2). Review cost
  is the binding constraint and this maximises it.
* Bad, because getting the exemption wrong converts an overflow into a deadlock — a strictly worse
  failure, and one that this codebase has already experienced once (upstream #9).
* Neutral, because it stays the right long-term shape to advocate if the maintainer ever engages;
  choosing A does not foreclose it.

### D — Re-propose configurable capacity

* Good, because it is trivially small and obviously backward compatible.
* Bad, because it is a third copy of #40 and #50 (F26), which is discourteous and would reasonably
  be closed as a duplicate.
* Bad, because it does not fix anything: a bigger bound moves the threshold, and F5 shows the
  failure survives it.

### E — Carry a patched fork in our build

* Good, because it fixes our symptom on our schedule, with no dependency on review.
* Good, because the fork already exists and the mechanism is proven: a remote `replace` compiles
  and runs against it without touching its `go.mod`. **[measured]**
* Bad, because it does not serve the purpose of this record: the defect stays in place for every
  other consumer, and our patch stops being reviewed by anyone.
* Bad, because it creates a supply-chain surface and a standing rebase obligation, and once our
  code depends on fork-only API the `replace` is no longer trivially reversible.
* Neutral, because it remains the contingency if the PR is rejected on grounds we accept — and the
  fork stays as the development vehicle regardless (D11).

### F — Contribute nothing; keep containment

* Good, because 0166 already made the failure survivable and tested, at zero further cost.
* Bad, because it leaves a known, reproducible availability defect in a dependency that four other
  projects have independently hit (F20), when the fix is small and we have already done the
  analysis.
* Bad, because it wastes the strongest asset we have: a reproduction with measured evidence
  (F5, obs. 28) that the existing PRs lack.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| `v0.13.5` is newest; published 2026-06-02 | `gh api repos/coder/acp-go-sdk/releases/latest`; `git tag --sort=-v:refname` |
| `main` is 0 commits ahead of the tag | `git rev-list --count v0.13.5..upstream/main` → `0` |
| 32 commits, 19 by one author | `git shortlog -sn upstream/main` |
| #40, #50, #59 OPEN + MERGEABLE + BLOCKED | `gh pr view --json mergeable,mergeStateStatus` |
| #57, #58 OPEN (logger race) | `gh issue view` |
| 30 matches for `"notification queue overflow" acp` | `gh api search/issues` |
| Overflow → `shutdownReceive`, reader returns | `connection.go:431-449` |
| Teardown is test-locked | `acp_test.go:836` |
| Maintainer specified bounded+fail-fast in PR #8 review | **[second-hand]** delegated research |
| `1024` default was never argued | **[second-hand]** delegated research (PR #8 thread) |
| No options type on any constructor | `connection.go:94`, `client.go:16`, `agent.go:21` |
| Unexported constant used directly | `connection.go:19`, `:108` |
| Null handler still loses transport 3/5 | `0166-MADR:216-238` |
| Producer holds `notifyMu` across enqueue | `connection.go:428-433` |
| Consumer re-acquires `notifyMu` before dequeue | `connection.go:499` |
| Single producer asserted by upstream | `connection.go:437` panic text |
| Response barrier waits for *processing* | `connection.go:773-799` |
| `$/cancel_request` drops on full queue | `connection.go:722`, `:730` |
| Bounding a bidirectional queue is unsound | `x/tools/internal/jsonrpc2_v2/conn.go`, verified verbatim by `curl` |
| Responses dispatched inline on the reader goroutine | `connection.go:397` vs `:411-421` |
| `$/cancel_request` handled outside notification ordering | `connection.go:388-393` |
| Go's teardown-on-bound is anti-DoS at 10000 | `x/net/http2/server.go`, verified verbatim by `curl` |
| Go-team verdicts: drop / summarise / escalate, not teardown | **[second-hand]** delegated research (golang/go#20352, #27935) |
| `grpc-go` bounds + blocks with exempt classes and a tunable | **[second-hand]** delegated research (`internal/transport/controlbuf.go`) |
| Bigger buffers make failures rarer but worse | **[second-hand]** delegated research (reference endorsed in golang/go#20352) |
| Upstream #9 was a real nested-request deadlock | **[second-hand]** delegated research |
| Reference Rust/TS SDKs are unbounded | **[second-hand]** delegated research |
| Schema silent on flow control (0/488) | `scratchpad/schema_notify.py` over `schema/schema.json` |
| Spec repo silent on flow control | **[second-hand]** delegated research |
| 10 panics; 0 at `v0.11.7`, 10 at `v0.12.0` | `git show <tag>:connection.go`, counted per tag |
| 10 MiB line cap ends the connection | `connection.go:369`, `:374`, `:455-459` |
| No exported sentinels; disconnect via `data` map | `connection.go:741` |
| No `Close()`; `Done()` only | `connection.go:945` |
| `SetLogger` unsynchronized vs 4 constructor goroutines | `connection.go:80`, `:111-119`, `:125`, `:127` |
| `InitializeResponse.Meta` exists | `types_gen.go:2322` |
| Our init reader consumes only `_meta` | `acpagent.go:1040-1053` |
| Extension notifications reach the handler | `client.go:19`; `connection.go:582-587`; `extensions.go:53-67` |
| `SendRequest[T]` exported; `conn` private, no accessor | `connection.go:625`; `client.go:13-15` |
| `CallExtension` is `SendRequest` behind a prefix check | `extensions.go:89-94`, `:23-31` |
| Our unsafe cast and its field-order dependency | `session.go:2572-2582`, `:2588-2597` |
| `Initialize`/`ResumeSession` are modelled | `client_gen.go:226` and sibling |
| No model type exists in the SDK | regex over `types_gen.go` → none |
| `SessionUsageUpdate` has no cache fields | `types_gen.go:5243` |
| `NewSessionRequest` has no `mode` | `types_gen.go:3236` |
| `session/resume` vendor payload is top-level `models` | `thinking.go:21-27` vs `ResumeSessionResponse` |
| Our buffer 256 / cap 512 | `acpagent.go:454`; `session.go:1480` |
| SDK `go 1.21` vs our `go 1.26.6` | both `go.mod` files |
| Schema ~10 releases stale | **[second-hand]** delegated research |
| Four downstream mitigation patterns; one ships a `replace` | **[second-hand]** delegated research |
| CI toolchain pins; `make check` = treefmt + `git diff --exit-code`; treefmt formats with `gofmt` | `mise.toml`, `Makefile`, `treefmt.toml` |
| vet/staticcheck/golangci/gofumpt baselines on unmodified `v0.13.5` | `scratchpad/baseline-*.log`, run this session |
| `go 1.21` ⇒ per-loop loop variables (`[3 3 3]` vs `[1 2 3]`) | `scratchpad/loopvar_proof.py`, run this session |
| 36/49 tests use `TestType_Action`; barrier helper at `:91`; sleep-based wait at `acp_test.go:724` | counted over `*_test.go` |
| Log levels: Error 9 / Debug 2 / Info 1 / Warn 0; drop-on-full logs Debug | `connection.go`, counted; `:730` |
| Fork-PR runs fail with `total_count=0` jobs; `runs-on` selects a Depot runner for owner `coder` | `gh api …/actions/runs/32064390541/jobs`; `ci.yaml` |
| README is mdsh-generated and diffed by `make check`; #40 did not touch it | `Makefile`, `gh pr diff 40 --stat` |
| Maintainer asked for "explicit overflow handling" and objected to unbounded growth "as a default" | `gh api repos/coder/acp-go-sdk/pulls/8/comments`, 2026-02-19 |
| #50 exports the sentinel with an unexported alias | `gh pr diff 50` |
| Watermark read on reader goroutine under `notifyMu`; rollback on same goroutine, same lock | `connection.go:530-537`, `:428-445` |
| Our connection construction and generic per-session notice event | `acpagent.go:494`; `event.go:42` |
| Rust `mpsc::unbounded()` ×4; TS unchecked `queue.push` | `curl` raw `rust-sdk` `jsonrpc.rs`, `typescript-sdk` `connection.ts` |
| Spec repo: backpressure 1 / flow control 0 / slow consumer 0 / windowing 0 vs notification 116 | `gh api search/code` |
| Maintainer last comment 2026-06-01; 12 open PRs; 4 open issues; oldest #40 | `gh api …/issues/comments`, `gh pr list`, `gh issue list` |
| `schema-v1.23.0` on 2026-09-18; 17 `schema-v*` in last 60 releases | `gh api repos/agentclientprotocol/agent-client-protocol/releases` |
| rsc / bcmills / ianlancetaylor comments, by comment ID | `gh api repos/golang/go/issues/comments/{310200463,425910003,365438524}` |
| `grpc-go` throttle: default 100, range 1–10000 | `curl` raw `controlbuf.go`, `envconfig.go` |
| #59 uses `atomic.Pointer[slog.Logger]` | `gh pr diff 59` |

### Assessment pass — 2026-09-22

A second reading of the record against the SDK source, upstream's toolchain and the live
repository state. Thirteen findings were added (F31–F43). Of the claims previously marked
**[second-hand]**, the following are now **[measured]** or **[source]**: F1/F2 (dormancy, open
items, last comment), F3 (the maintainer's bounded/fail-fast request — his words are quoted in
F37), F11's reference-SDK claim, F12's spec-silence claim, F19 (#59's approach), F22 (schema
staleness), F23 (Go-team verdicts, by comment ID), F24 (`grpc-go`). Still second-hand and marked
as such: the 19-library API survey behind F28 beyond its three spot checks, the four downstream
mitigation write-ups behind F20 beyond the existence of the issues, and the detailed release
history in F1. One inference is recorded as such because its evidence is gone: the mechanism
behind fork-PR runs failing with zero jobs (F35).

Two decisions changed as a result: D5 no longer exports the sentinel (F38), and D2 now fixes the
runtime semantics a reviewer would otherwise have to guess. Two were added: D14 (standards
compliance against measured baselines) and D15 (evidence in the PR body, because CI cannot supply
it).

### Related records

* **MADR/PLAN 0166** — the transport teardown. This record closes its two deferred items
  (upstream a patch; fork/vendor) and supersedes nothing: 0166 D1–D7 were correct for
  `v0.13.5` unpatched. If Option A is executed, 0166's own consequence — *"if `acp-go-sdk`
  later makes the reader block or the depth configurable, this test will under-assert"* —
  becomes live, by our own hand rather than upstream's.
* **MADR 0138** — F5 promised transport survival; 0166 amended it as unachievable. Option A
  makes it achievable again and will need a third, additive amendment.
* **MADR 0165** — the lock and assertion flakes; the source of the discipline that a check
  must be seen to fail, which this record's Confirmation depends on.
* **MADR 0137** — the wire-capture tee and the `SetLogger` accommodation (F19).
* **MADR 0038 / 0039 / 0081** — the records carrying the three claims corrected by F15, F16
  and F17.
* **MADR 0029** — the standing decision to keep the SDK a direct, unhidden dependency, which
  a `replace` directive must be reconciled with in the plan.

### Open questions for the plan

1. **Stack on #40, or stand alone?** Stacking keeps the diff honest and credits prior work
   (D7), but inherits #40's stall and makes our PR unmergeable until it moves. A self-contained
   PR that introduces a minimal `ConnectionOption` itself is mergeable alone but overlaps #40.
   The plan must pick one and state the fallback.
2. ~~**What exactly does the option look like?**~~ **Answered 2026-09-22 (F28, F29, F30, D2):** an
   enumerated option plus a notification-only handler, a counter accessor and the exported
   sentinel. A decision-returning callback was the initial proposal and was **withdrawn** — it has
   zero precedent in Go for this trigger across 19 libraries surveyed, it contradicts the style
   guide's rule that an enumerated option takes an enumerated constant, and the reason it looked
   attractive for us is the reason it is harmful (F29).
3. ~~**Drop newest or drop oldest?**~~ **Answered:** newest, because upstream's existing rollback
   at `connection.go:435-445` already makes discarding the arriving notification correct and cheap,
   and dropping from the middle of the queue would move the sequencing invariants — the one part of
   this code that must not move. `OverflowDropOldest` stays available as a later enum constant.
4. **Can the new policy test be made to fail on demand?** If the option can be wired wrongly and
   the test still passes, it proves nothing — this is the usual fail-first requirement, and it
   applies to the *option plumbing* as much as to the behaviour.
5. **Does D12's evidence require the temporary `replace`, and how is it kept out of every
   commit?** This is the one step that touches our repository's build files, and the one most
   likely to be committed by accident.
6. **How long do we wait before reconsidering Option E?** F1/F2 suggest review may never come.
   The plan should not assume an answer, but it should say what observation would reopen the
   question rather than leaving it to drift.
7. **Is the 10 MiB frame cap (F14) reachable in our traffic?** Unmeasured. If a large
   `session/update` can exceed it, it is a second availability defect hiding behind the first,
   and a second candidate PR.
