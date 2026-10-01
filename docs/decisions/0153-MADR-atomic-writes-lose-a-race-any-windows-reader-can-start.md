---
status: proposed
date: 2026-09-08
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Atomic writes fail on Windows whenever anything holds the destination open

## Context and Problem Statement

`fsutil.WriteFileAtomic` ends in `os.Rename(tmp, path)`. On POSIX that
succeeds regardless of who has the destination open — the rename replaces a
directory entry and existing readers keep their inode. Windows has no such
rule: `MoveFileEx` fails while any handle to the destination is open without
`FILE_SHARE_DELETE`, and Go returns that as a plain error the caller reports as
a write failure.

Eight production call sites go through this function, including the credential
store, the provider-auth transaction, the engine registry and the session
store. Any of them can fail because something — antivirus, Windows Search, a
backup agent, an editor, or mcremote's own concurrent reader — happened to have
the file open for a few hundred milliseconds.

This is W4 from the 2026-09-07 Windows debugging pass. The same pass listed W3
(the workspace validator accepting drive-absolute paths); this record closes
that hypothesis rather than opening one for it, because it does not reproduce.

### What was measured, not assumed

All on `33a7dfb`, Windows 11, Go 1.26.6, probed outside the repository against
the real `os.Rename`.

**A rename over an open destination fails, and the errno is not the obvious
one.**

```console
rename over a Go-opened handle:      err=... Access is denied.
rename over a share-read-only handle: err=... Access is denied.
  errors.Is(ERROR_SHARING_VIOLATION) = false
  errors.Is(ERROR_ACCESS_DENIED)     = true
  underlying errno                   = 5 (Access is denied.)
rename after the handle closes:      err=<nil>
```

Three things fall out of that, and the second is the one that changes the fix:

1. **It fails.** Both rows, on a file the process itself created moments before.
2. **It is `ERROR_ACCESS_DENIED` (5), not `ERROR_SHARING_VIOLATION` (32).** A
   retry predicate written from intuition would key on 32 and never fire.
3. **It clears the instant the handle closes.** The same rename succeeds on the
   next line, which is what makes a bounded retry the right shape rather than a
   way to paper over a permanent failure.

**A Go-opened handle is enough.** `os.Open` passes
`FILE_SHARE_READ|WRITE|DELETE`, the most permissive sharing Go offers, and the
first row shows the rename failing against it anyway. So this does not need a
hostile or unusual reader: one goroutine reading the credential file while
another writes it is sufficient, and a naive probe that opens the destination
with `os.Open` and sees a failure could easily be misread as *only* reproducing
under Go — it reproduces under everything.

**Eight production call sites, none retrying.**

```text
internal/auth/store.go:602              credential store
internal/procutil/registry.go:86        engine registry record
internal/providerauth/manifest.go:297   auth manifest
internal/providerauth/reconcile.go:336  live credential reconcile
internal/providerauth/store.go:162      provider credential store
internal/providerauth/transaction.go:468 credential transaction commit
internal/session/store.go:73            session store
internal/fsutil/atomic.go:50            the function itself
```

`writeFileAtomic` calls `ops.rename` once and returns
`fmt.Errorf("fsutil: rename: %w", err)` on failure. There is no retry in
`fsutil`, and no caller wraps one around it.

**W3 does not reproduce.** `pathWithinAny`
(`internal/provider/codex/execution.go:586`) was probed against every Windows
path form that could plausibly evade it, with root `C:\Users\mac\proj`:

| Case | `IsAbs` | within |
| --- | --- | --- |
| inside, exact case | true | **true** |
| inside, different case (`c:\users\...`) | true | **true** |
| inside, forward slashes | true | **true** |
| escape via `..` | true | false |
| sibling directory | true | false |
| other drive | true | false |
| `projX` — prefix of the root, not a child | true | false |
| extended-length `\\?\C:\...\proj\src` | true | false |
| extended-length with `..` escape | true | false |
| UNC `\\srv\share\...` | true | false |
| device namespace `\\.\C:\...` | true | false |
| rooted without a drive `\Users\...` | false | false |
| drive-relative `C:Users\...` | false | false |

No form is accepted that should be rejected. The prefix case (`projX`) is
correct, so there is no string-prefix bug; case-insensitivity works, so a
Windows path is not rejected for its casing; and `filepath.Clean` collapses
`..` even under a `\\?\` prefix (`\\?\C:\Users\mac\proj\..\secret` →
`\\?\C:\Users\mac\secret`), so the extended-length form cannot smuggle an
escape.

### Findings

**F1 — `os.Rename` fails on Windows while anything holds the destination
open.** Measured above. On POSIX the same call succeeds, which is why the code
was written without a retry and why no test caught it.

**F2 — the error is `ERROR_ACCESS_DENIED` (5).** Not `ERROR_SHARING_VIOLATION`
(32), which is what the name of this failure mode suggests. Any fix must key on
what Windows actually returns.

**F3 — the failure is transient.** The rename succeeds immediately once the
handle closes, so the condition a retry waits out genuinely ends.

**F4 — `ERROR_ACCESS_DENIED` is ambiguous.** It is also the permanent error for
a genuine permission failure or a destination that is a directory. A retry
keyed on it will therefore also retry errors that will never succeed, spending
its budget before returning the same error. That is a real cost of the chosen
fix and it is accepted below rather than hidden.

**F5 — the blast radius is the daemon's durable state.** Eight call sites:
credentials, the auth transaction commit, the engine registry, the session
store. A credential write that fails mid-transaction is the worst of them,
because `providerauth/transaction.go` is the path that exists to make credential
swaps atomic.

**F6 — W3 is closed, not deferred.** The probe table above shows the validator
correct on every containment question. The only anomaly is a false *negative*:
an extended-length path that genuinely is inside the root is reported outside
it. That direction cannot grant access, and no client is known to send that
form.

**F7 — `fsutil` already has the file layout this fix needs.** `lock_unix.go` /
`lock_windows.go` and `syncdir_unix.go` / `syncdir_windows.go` establish the
per-platform split, so a Windows-only predicate needs no new convention and no
`runtime.GOOS`.

## Decision Drivers

* The failure is silent in testing and only appears on the platform with the
  least coverage, so it will not be found by the suite.
* The affected writes are the daemon's durable state; a spurious failure is a
  real user-visible error, not a log line.
* MADR 0144's lesson applies: no behaviour may be derived from the host at
  runtime — the platform split belongs in the build tags.
* A retry must not turn a permanent failure into a slow permanent failure of
  unbounded length.

## Considered Options

* **A — Bounded retry around the rename, with a per-platform predicate**
  (chosen)
* **B — Retry every step of `writeFileAtomic`**
* **C — Use the Windows `ReplaceFile` API instead of `MoveFileEx`**
* **D — Do nothing; the window is small**

## Decision Outcome

Chosen option: **A**. It addresses the measured cause, it is confined to one
call, and it degrades to exactly today's behaviour on POSIX and on a permanent
error.

### The decisions

**D1 — retry the rename, bounded, on a retryable error only.** Up to five
attempts with a short backoff. On the final failure return the original error
unchanged, so the caller sees what it sees today.

**D2 — the predicate is per-platform, chosen at build time.**
`retryableRenameErr(error) bool` lives in `rename_windows.go` and
`rename_other.go`. The non-Windows implementation returns `false`
unconditionally, which makes POSIX behaviour identical to today by
construction, not by test. No `runtime.GOOS` anywhere (MADR 0144).

**D3 — the predicate matches `ERROR_ACCESS_DENIED` and
`ERROR_SHARING_VIOLATION`.** The first is what was measured (F2); the second is
what the documentation describes for this condition and what a different
sharing mode or filesystem may return. Matching both costs nothing.

**D4 — the budget is ~150 ms total** (10, 20, 40, 80 ms). Long enough to
outlast a scanner touching a small file, short enough that a genuinely
permanent `ACCESS_DENIED` (F4) costs a caller a sixth of a second before
failing exactly as it does today. **[unverified]** how long a real antivirus
hold lasts on this host; the budget is chosen from the transient nature of the
failure, not from a measurement of the holder.

**D5 — the retry is exercised through the existing `fileOps` seam.**
`writeFileAtomic` already takes an injectable `rename`; a test supplies one that
fails N times then succeeds, and another that fails permanently. No production
call site changes.

**D6 — record W3 as closed here; do not open a record for it.** The probe table
is the evidence, and it belongs somewhere findable rather than in a session
transcript.

### Consequences

* Good: the daemon stops reporting a credential or session write as failed
  because something else was reading the file for 50 ms.
* Good: POSIX is unchanged by construction — the predicate is a compile-time
  `false`.
* Good: W3 is answered with a table rather than left as an open worry.
* Neutral: `fsutil` gains two small files, matching a split it already uses
  twice.
* Bad: a genuinely permanent `ERROR_ACCESS_DENIED` now takes ~150 ms to report
  instead of returning at once (F4). This is the accepted cost of D3; the
  alternative is keying on an errno Windows does not return.
* Bad: the retry hides contention rather than removing it. If a real
  reader/writer race exists inside the daemon, this makes it less visible. That
  is a reason to prefer failing loudly *after* the budget, which D1 does, over
  retrying indefinitely.

### Confirmation

```bash
# 1. POSIX is a compile-time no-op:
grep -n 'return false' internal/fsutil/rename_other.go

# 2. No host-derived behaviour:
grep -rn 'runtime.GOOS' internal/fsutil/    # expect none

# 3. The retry runs, and gives up:
go test ./internal/fsutil/ -run TestRename -count=1 -v

# 4. Nothing else changed:
go test ./... && go test -race ./...
```

## Pros and Cons of the Options

### A — Bounded retry around the rename, per-platform predicate (chosen)

* Good, because it targets the one call that was measured to fail.
* Good, because the POSIX path is unchanged by construction rather than by
  assertion.
* Good, because the existing `fileOps` seam makes both the retry and the
  give-up testable without touching a real filesystem race.
* Bad, because `ERROR_ACCESS_DENIED` is ambiguous, so a permanent failure pays
  the full budget (F4).

### B — Retry every step of `writeFileAtomic`

* Good, because `CreateTemp` and `Chmod` can hit the same sharing conditions.
* Bad, because only the rename was measured to fail, and the temp file has a
  unique name no other process is holding — retrying its creation is
  speculation. Widening a fix past its evidence is how a small change acquires
  a large blast radius.

### C — Use `ReplaceFile` instead of `MoveFileEx`

* Good, because `ReplaceFile` preserves the destination's ACLs and attributes,
  which on Windows is a genuine improvement over a rename that creates a file
  inheriting the parent's DACL.
* Bad, because it does not solve this problem: `ReplaceFile` also fails while
  the destination is open, so a retry is still required, and it would need
  `os.Rename` replaced with a hand-written syscall path on one platform.
* Its ACL argument is real and is left on the record: if the DACL a replaced
  file inherits ever matters (MADR 0116 D4 puts the access control on the parent
  directory, so today it does not), this is the option to revisit.

### D — Do nothing; the window is small

* Good, because the window genuinely is small, and no user has reported it.
* Bad, because the affected writes are credentials and session state, the
  platform is the one with the least coverage, and the measurement shows the
  daemon's own reader is enough to trigger it. "Not yet observed" on a platform
  nobody runs the suite on is not evidence of rarity.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Rename fails over an open destination | probe output, *What was measured* |
| The errno is 5, not 32 | same probe, `errors.Is` lines |
| A Go-opened handle is enough | same probe, first row |
| The failure clears when the handle closes | same probe, last row |
| Eight call sites, none retrying | `grep -rn 'WriteFileAtomic(' internal/` |
| `writeFileAtomic` renames once | `internal/fsutil/atomic.go:108` |
| W3's validator is correct on every containment case | probe table, *What was measured* |
| `Clean` collapses `..` under `\\?\` | same probe, final line |
| `fsutil` already splits per platform | `lock_windows.go`, `syncdir_windows.go` |

### Related records

* **MADR 0116** — the Windows platform layer. D4 puts access control on the
  parent directory's DACL, which is why option C's ACL argument does not bite
  today.
* **MADR 0144** — established that this codebase's path handling must not
  derive behaviour from the host at runtime. D2 follows it.
* **MADR 0151** — the previous finding from the same Windows pass; its
  out-of-scope section named W3 and W4 as separate subjects. W4 is this record;
  W3 is closed by F6.

### Open questions for the plan

1. **Is five attempts over ~150 ms the right budget?** Chosen from the shape of
   the failure, not from a measurement of how long a real scanner holds a file.
   **[unverified]**
2. **Should the give-up path log?** The error already reaches the caller. A log
   line would make contention visible rather than merely survivable, which the
   Consequences section notes is otherwise lost.
