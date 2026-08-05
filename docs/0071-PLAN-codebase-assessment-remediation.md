# MADR 0071 — Implementation plan: non-update remediation

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: In progress (implementation train).
- **Date**: 2026-08-05
- **Source**: [0071-MADR-codebase-assessment.md](0071-MADR-codebase-assessment.md)
- **Scope**: F1–F4, F6 software; F5 is ops/hardware only. **No update
  automation** (0065 owns that).
- **Non-goals**: 0065; APNs; full 0029/0039; hardware gates.

---

## Phase map

| Phase | Findings | Work |
| --- | --- | --- |
| **P0** | F1, F2 | Linux-only sandbox probe; async on Linux; `/bin/sh`; template key |
| **P1** | F3, F4 | Resume map expiry purge; empty-catch `debugPrint` floor (non-teardown) |
| **P2** | F6 | Skip empty-ring history under force; document optional discard metrics |
| **P3** | F5 | Docs close-out: plan/MADR status; hardware remains ops |

Commit per phase; no push until review.

---

## P0 — Sandbox probe + template (F1, F2)

1. `probeSandboxHealth` / `runSandboxProbe`: if `runtime.GOOS != "linux"`, set
   health `OK: true`, reason `ok`, detail that userns probe is Linux-only;
   do not exec codex sandbox.
2. On Linux: run probe in a **background** goroutine after engine ready
   (do not block `startEngine`); keep 8s timeout; use `/bin/sh` not `bash`.
3. `defaults_mcremote.yaml`: commented `sandbox_broken_policy: warn` next to
   `allow_full_access`.
4. Tests: non-linux short-circuit (or inject GOOS via test seam if needed —
   pure function `sandboxProbeApplicable(goos string)`); probe still classifies
   fixtures; fake-bin tests unchanged for linux path.

## P1 — Resume GC + catch floor (F3, F4)

1. `resumeStore.issue` / `validate`: under lock, drop expired entries
   (and optionally cap map if huge — purge-only is enough).
2. Tests: issue tokens with fake `now`, advance, issue another device → old
   expired gone.
3. Mobile: remaining non-teardown `catch (_) {}` in settings/chat/sessions
   get `debugPrint` + comment `// best-effort: …` per 0070 convention.
   Do **not** change relay_transport / socket teardown catches.

## P2 — History skip polish (F6)

1. `SessionSynchronizer`: if bounds for id has `latest == 0` (and no gap),
   skip history even when `force` — empty host ring has nothing to fetch.
2. Test: epoch force with empty latest skips historyCalls.
3. `docs/standards/mobile/dart.md`: one line that discard metrics remain
   optional (no code counter required).

## P3 — Close-out (F5)

1. 0071 MADR status → software remediation complete; F5 still hardware.
2. This plan Status → Implemented for P0–P2; P3 docs done.

---

## Acceptance

- `go test ./internal/provider/codex/ ./internal/ws/ -count=1`
- `flutter test` for session_synchronizer + analyze on touched dart
- `make pre-add-check` on Go files before stage
