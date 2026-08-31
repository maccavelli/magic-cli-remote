---
status: proposed
date: 2026-08-31
associated-madr: "0126-MADR-codex-purge-skips-local-teardown.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0126 — Close before purge, and bound the engine call

Implements [0126-MADR-codex-purge-skips-local-teardown.md](0126-MADR-codex-purge-skips-local-teardown.md)
decisions D1–D6, closing findings F1–F7.

## Goal

Ending a codex session returns promptly, releases every local resource, and
says so when the engine-side delete did not happen.

Finish line:

* the manager closes locally before purging, for every provider;
* the provider-side purge runs detached, under a ceiling well below the 60s
  RPC budget;
* a purge failure reaches the caller instead of a log line;
* no goroutine is left selecting on a purged codex session's `done`;
* neither timeout in the ladder changed.

## Scope

### In scope (the only files any phase may touch)

* `internal/session/manager.go` — `closeMatching` only: the close/purge
  sequence, the bounded context, and the error return
* `internal/provider/codex/session.go` — `Purge` only
* `internal/provider/provider.go` — the `PurgeSession` doc comment (D5)
* `internal/session/*_test.go`, `internal/provider/codex/*_test.go` — tests

### Out of scope

* **Both timeouts** (D6). The 60s daemon budget and the phone's 60s + 10s
  margin are a deliberate ladder from MADR 0095 F9. A diff in
  `asyncOpTimeout` or `opTimeoutFor` is a failed plan — the temptation is real,
  because raising one makes the symptom quieter without fixing anything.
* **`httpagent` and `acphttp` `Purge`.** Their internal `Close` and 15s budget
  become redundant once D1/D2 land, not wrong. Removing them is MADR open
  question 2 and is deliberately not decided here.
* **The phone.** `chat_screen.dart`'s cancel-then-delete flow and its
  confirm-against-`session.list` fallback (MADR 0094 D7) stay exactly as they
  are. They are what currently rescues the user, and they will simply stop
  being needed.
* **A sweep for other ctx-passthrough calls** — MADR open question 3. Named,
  not done.

## Stability rule

Every phase ends with:

```bash
go build ./... && go vet ./...
go test ./internal/session/ ./internal/provider/... ./internal/ws/
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Local `gofmt -l` is unusable on this host** (stale CRLF checkout). CI's Gofmt
step is the authority; do not run `make fmt`.

**Run `go test -race ./internal/session/` at least once in P1.** The change
alters what runs while `m.mu` is not held and adds a second call into provider
teardown; a race here would be a worse bug than the one being fixed.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — Neither timeout moves.** D6. `asyncOpTimeout` and `opTimeoutFor` are
untouched.

**C2 — `Close` must stay idempotent, and that is verified, not assumed.** D1
makes every `PurgeSession` provider close twice — once from the manager, once
inside its own `Purge`. All three guard on `s.closed`, but a test must prove
it rather than a reading of the source.

**C3 — Local release never depends on an engine call.** The ordering is close
first, then purge. A provider whose engine never answers must still end up
fully released locally.

**C4 — No new hang.** Every provider-side call on this path is bounded. If a
phase adds an unbounded call it has recreated the defect.

**C3 is the one at risk.** It is tempting to purge first — the engine-side
delete is the "real" work and closing first feels like giving up on it. But
close-then-purge is the whole point: the reported hang is local release waiting
on a remote call.

## Dependency and delivery order

P1 (manager) → P2 (codex) → P3 (interface doc). P1 alone fixes the reported
symptom; P2 makes codex's `Purge` correct when called directly rather than only
via the manager; P3 writes down the obligation so the next implementer does not
re-derive it.

## Implementation Steps

### P1 — Close before purge, bounded and reported (D1, D2, D4; closes F1–F6)

`internal/session/manager.go`, `closeMatching` only.

Replace the either/or with: always `e.sess.Close(ctx)`, then — when the session
implements `PurgeSession` and `purge` is set — the engine-side purge under
`context.WithoutCancel` with its own ceiling.

Answer MADR open question 1 in the commit: pick the ceiling and say why. It
must be at least the 15s the two peers already budget and well under the 60s
RPC allowance, so the daemon returns its own error rather than being pre-empted
by the phone (MADR 0095 F9).

D4 lands here: a purge failure is returned, not just logged. The local session
is gone regardless — that is not in doubt — so the error must say specifically
that the engine-side remnant survived, not imply the delete failed wholesale.

**Verification:**

```bash
go test ./internal/session/
go test -race ./internal/session/
```

New cases, using a fake session:

* `Close` runs before `Purge`, in that order, on delete;
* a `Purge` that blocks past the ceiling returns within it, and the session is
  still fully closed and removed from the store;
* that failure reaches the caller;
* a soft close (`purge=false`) still never calls `Purge` — 0095's rule that
  resume depends on that state surviving.

### P2 — Codex's Purge stops being the odd one out (D3; closes F1, F2 at source)

`internal/provider/codex/session.go`, `Purge` only.

Give it the shape its peers already have: local `Close` first, then the engine
call detached with its own bound. After P1 the manager already guarantees both,
so this is about `Purge` being correct when called directly — the same reason
`httpagent` and `acphttp` do it.

**Verification:** a codex test through the existing session fakes asserting
that `Purge` closes locally even when the engine call fails, and that it
returns within its own bound when the engine never answers. `s.done` must be
closed in both cases — that is F4, and it is the assertion that proves the
goroutine leak is gone.

### P3 — Write the obligation down (D5; closes F7)

`internal/provider/provider.go`, the `PurgeSession` doc comment.

State that the manager performs local close and that `Purge` is the
engine-side remnant only; that it must not rely on the caller's deadline; and
that it must be safe to call after `Close`. One implementer missed all three by
inference.

**Verification:** none beyond `go vet`. This is a comment, and its value is
that the next reader does not have to diff three implementations to learn the
contract.

## Verification (whole plan)

```bash
go build ./... && go vet ./...
go test ./internal/session/ ./internal/provider/... ./internal/ws/
go test -race ./internal/session/
git diff --stat internal/ws/server.go apps/mobile/    # empty (C1)
```

```text
unit tests              -> ordering, bounding, and error propagation
owner, on macOS         -> delete a codex session; returns promptly
owner, engine wedged    -> returns within the ceiling and says the engine kept the thread
```

### Acceptance criteria

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | The manager closes locally before purging, every provider | D1, C3 |
| A2 | The engine-side purge is detached and bounded by the manager | D2, C4 |
| A3 | A purge failure reaches the caller | D4, F6 |
| A4 | `s.done` is closed on a purged codex session | F4 |
| A5 | Pending permission/question asks are cancelled on delete | F5 |
| A6 | Soft close still never calls `Purge` | 0095 |
| A7 | Double `Close` is proven safe, not assumed | C2 |
| A8 | Neither timeout changed | D6, C1 |
| A9 | `PurgeSession` documents the obligation | D5, F7 |
| A10 | Owner confirms a codex delete returns promptly | — |

**A6 is the one to guard.** The change is "always close, then maybe purge", and
the fastest way to write that wrong is to make purge unconditional too. Soft
close exists so resume can rely on engine-side state surviving; a delete-shaped
close would destroy sessions the user only backgrounded, and no test of the
reported bug would notice.

A10 is the one that cannot be faked: everything else runs against fakes on a
Windows host, and the report came from a Mac.

## Rollout and Rollback

No protocol change, no persisted-format change, no phone change. Each phase
reverts independently. The user-visible effect is that ending a session stops
hanging; the invisible one is that purged codex sessions stop leaking
goroutines.

## Deferred (named, so they are not mistaken for oversights)

* **Whether `httpagent` and `acphttp` should drop their now-redundant internal
  `Close`** — MADR open question 2. Keeping them makes each `Purge` correct
  standalone; removing them puts the obligation in exactly one place. Genuinely
  balanced, and not worth deciding inside a bug fix.
* **A sweep for other calls passing the caller's context straight to
  `sendRequest`** — MADR open question 3. The same shape produces the same
  hang anywhere it appears on a user-facing path. This record found it in
  purge and did not look further.
* **Whether `thread/delete` is answerable mid-turn** — MADR open question 4. If
  codex defers it until the turn ends, the ceiling is doing the real work and
  the daemon should probably cancel before purging, as the phone already does.
  Needs a live codex to answer, so it cannot be settled here.
