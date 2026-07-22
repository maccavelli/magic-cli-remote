# MADR 0013: Daemon audit remediation — decisions & deferral register

- **Status**: Accepted
- **Date**: 2026-07-22
- **Deciders**: Project Owner
- **Remediates**: [MADR 0012 — mcremote Go daemon assessment & action plan](0012-mcremote-daemon-assessment-action-plan.md)
- **Purpose**: A durable record of *how* the 2026-07-22 audit findings were resolved, the non-obvious judgment calls made along the way, and — most importantly — the register of items deliberately **deferred or declined** with their rationale, so we can return to them without re-deriving the reasoning.

## Context

The 2026-07-22 audit (MADR 0012, working finding-list `H1–H6 / M1–M17 / Low`)
surveyed the daemon for residual bugs, concurrency hazards, and hardening gaps.
It found **no P0** (no default-config RCE or auth bypass). Remediation was shipped
in reviewed batches, each finding paired with a regression test verified to **fail
against the buggy code and pass against the fix** ("fail-on-buggy"), with the full
tree kept green under `-race`.

## Commit map

| Batch | Findings | Commit |
|-------|----------|--------|
| WS Origin check + allowlist | M1 | `7b2dd09` |
| CLI/config traps | M2, M15, M16, M17 | `4f7a64f` |
| Provider control-event unify + lifecycle | H5, H6, provider fidelity | `0c6de31` |
| Provider stream cluster | M7–M14 | `8ca9c16` |
| Session-zombie on connection death | **M11** | `c4c952f` |
| FS confinement/audit + cert-renewal recovery | **M3, M5** | `cbba953` |
| Low sweep + `OutputByteLimit` clamp | M6 (already fixed) + Low | `22dcea0` |
| Session persist-race cluster (prior) | H1, H2, M4 | earlier history |

## Key decisions (the non-obvious calls)

### D1 — M11: fix the zombie with a `conn.Done()` watcher, **not** an internal spool

The ACP SDK (`coder/acp-go-sdk`) runs **all** `session/update` notifications on a
**single** goroutine; our blocking control-event `deliver` can wedge it, and once
the SDK's inbound queue overflows (1024) it tears down the *connection* but **not**
the agent process. Nothing watched `conn.Done()`, so the session zombied (process
alive, no `disconnected` event, manager entry live forever).

The deferral note had proposed an unbounded internal spool to decouple the SDK
goroutine. We **rejected** that: grounded reading showed the WS layer is
non-blocking (a slow phone is dropped, not waited on) and the manager pump has no
unbounded blocking, so the trigger is narrow; and the spool would rework the
delicate coalesce-on-backpressure code for a milder symptom. The `conn.Done()`
watcher (`session.watchConnClose` → `signalDisconnected`, funneled with the
existing `cmd.Wait` watcher) fixes the actual severe outcome for **every**
connection-death cause, is minimal, and is deterministically testable. A bounded
spool remains a documented *future* option, not needed now.

### D2 — M3: audit + **opt-in** confinement, **not** a cwd jail

The agent's `fs/read_text_file` / `fs/write_text_file` callbacks had no
containment. A hard cwd-jail is **security theater here**: the agent runs as the
same user and has terminal access (`CreateTerminal`), so it can reach any path
regardless — a jail only breaks legitimate absolute-path reads. Chosen instead:

- **Always** emit a `tool_call` (`kind:"fs"`) audit event per fs callback.
- **Opt-in** `Config.FSRoots` (default empty = unrestricted); when set, paths must
  resolve — after symlink evaluation — within a root or the session cwd.

Documented in code/`docs/config.md` as defense-in-depth + audit, **not** a sandbox.

### D3 — M5: promote a matching orphan `cert.new`, don't delete it

A crash between the key-rename and cert-rename of `writePairAtomic` left the new
key live, old cert live, and new cert as `cert.new`. The old `cleanupOrphanNew`
**deleted** that orphan → mismatched key/cert → `Ensure` refused to re-key →
daemon wouldn't start. Fix: if an orphan `.new` loads against the surviving live
counterpart, **promote** it (finish the interrupted install); only genuinely stale
orphans are removed.

### D4 — device names sanitized at the store layer

Names arrive from both WS pairing and the CLI, so `auth.Store.CreateWithClientKey`
(not just the WS handler) strips control/ANSI-ESC bytes, rune-safe truncates, and
**deflects UUID-shaped names** so a name can never collide with a device id in
`Revoke`; `Revoke` also prefers an exact id match (protects legacy records).

### D5 — pre-auth slot exhaustion → evict oldest **unauthenticated** client

Bounded per-client queues already stop slow-client stalls; the residual was idle
pre-auth sockets holding the `maxClients` pool for up to 30s (tailnet-gated). At
capacity we now evict the oldest *unauthenticated* client (never an authed one) and
`CloseNow` it; when every slot is authed, that's genuine capacity and we refuse.

## Deferred / declined register (revisit only on decision)

| Item | Decision | Rationale |
|------|----------|-----------|
| **H4** — SSE reconnect resync (turns stuck "running") | **Deferred (feature)** | Needs a resync protocol; product-scoped, not a quick fix |
| **os.Environ** to agent/terminal children | **Won't-fix** | Coding agents need the env (PATH/HOME/toolchains); "secret" filtering is undefinable + high-breakage, and the agent is same-user with terminal access (same logic as D2) |
| **0o644** agent-written files | **Keep** | Correct for agent-written source/project files; `0600` breaks build tools / other readers |
| Windows `filelock_other` no-op + non-unix `KillProcessGroup` partial kill | **Deferred** | Daemon targets Linux (systemd/Tailscale/Headscale); untested syscall code is riskier than a documented no-op |
| `session.list` re-reads every `meta.json` under the mutex; closed sessions accumulate | **Deferred (structural)** | Needs a retention/GC *policy* (product decision) or an in-memory cache; a wrong GC deletes user history |
| `coalesced` map "unbounded" | **No change** | Already bounded — only ever holds 2 keys (assistant/thought chunk types) |
| Fake-provider contract divergences | **Already fixed** | Fake errors on concurrent Prompt, emits `turn_complete` on cancel, blocks control via `event.IsControl` |
| opencode model-key tests don't call `Create`/`Prompt` | **Deferred (test-only)** | Coverage gap for an already-fixed bug class; low value |
| `writeSessionErr` 300-char raw error | **Keep** | Goes only to **authenticated** (trusted) peers; the audit's leak concern was unauthenticated peers, which is fixed |
| `providers.*.args` env binding | **Skipped** | `[]string` viper env binding is fragile/error-prone |

## Consequences

- New opt-in config surface: `providers.{grok,opencode}.fs_roots` (default empty).
- New cross-process locks: cert generation (`certs`), and a **timeout** on the auth
  device-store flock (a wedged holder can no longer stall all authentication).
- Graceful shutdown now closes hijacked WS conns and no longer races itself into a
  non-graceful `exit 1`.
- Open follow-ups are the **Deferred** rows above; H4 and the `session.list`
  retention policy are the two most likely to warrant a dedicated decision next.

## Verification

Every logic fix carries a fail-on-buggy regression test; full tree green under
`go test ./... -race`. Cross-platform/timing-only fixes (cert lock race, shutdown
race) are code-review-verified rather than backed by flaky timing tests, noted as
such at their sites.
