---
status: proposed
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0151 — Make wirecap redaction match what the wire carries

Implements [0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md](0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md)
decisions D1–D7, closing findings F1–F8.

## Goal

1. A Windows home directory is redacted in the form it actually reaches the
   file — JSON-escaped — and a test proves it against real `json.Marshal`
   output.
2. POSIX redaction behaviour is byte-for-byte unchanged.
3. No committed fixture contains an email address or a Windows-shaped absolute
   path, and a test says so on every run.
4. The package states what its redaction does *not* cover.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/wirecap/wirecap.go` | P1 | escaped-form redaction (D1, D2), doc sentence (D7) |
| `internal/wirecap/wirecap_test.go` | P1, P3 | escaped rows (D3), fixture guard (D6) |
| `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl` | P2 | scrub account metadata (D4) |

### Out of scope

* **Structural redaction** (MADR option B). Frames are byte-verbatim by design,
  and codex's read pump keeps frames that do not unmarshal.
* **Rewriting git history** (D5). The email is already public through commit
  authorship; a force-push breaks every clone and every commit reference in the
  0137 records to remove a team UUID.
* **Recapturing any fixture.** P2 edits values in place; taking a fresh capture
  needs a live engine and is a different job.
* **The other two findings from the 2026-09-07 Windows pass** (W3 workspace
  validator, W4 `WriteFileAtomic` retry). Separate subjects.

## Stability rule

Every phase ends with:

```bash
go build ./... && go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
```

The cross-build is not required here — nothing in scope is platform-specific,
which is itself D2's point.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — frames stay byte-verbatim apart from redaction.** Every change is to the
needle `redact` searches for, never to how the frame is written. `Frame`'s
newline escaping and its write path are untouched.

**C2 — nothing is derived from the host.** No `runtime.GOOS`, no
`os.PathSeparator`, no `filepath.VolumeName` enters `redact`. This is MADR 0144's
root cause and D2 restates it; a host-dependent branch would make the new rows
untestable on the platform that is not running them.

**C3 — POSIX behaviour does not change.** The existing posix rows of
`TestRedactAbsoluteAndRelativeHome` must pass unmodified.

**C4 — P2 changes values, not shape.** The scrubbed fixture keeps its key set,
its frame count and its line count, so it still reproduces the engine's wire
*shape*, which is the reason it is committed.

**The contract most at risk is C3**, because the obvious implementation of D1
is to escape the needle in place rather than to add a second needle. Replacing
`c.home` with its escaped form would fix Windows and silently stop redacting
raw POSIX homes — and every posix row would still pass, because a POSIX home
has no backslash to escape and the two needles are identical there. The failure
would only appear on a raw Windows path, which is the case the current test
already covers, so it would look like a passing suite with one row deleted.

## Dependency and delivery order

P1 first: it is the proven defect and it is independent of everything else.

P2 before P3, and this ordering is load-bearing — P3's guard fails against the
current fixture, so landing it first would put the tree in a red state and
invite weakening the guard to get green. Scrub, then guard.

P4 last, so the sentence describes what shipped.

## Implementation Steps

### P1 — redact the escaped form (D1, D2, D3; closes F1, F2, F3)

In `redact`, build each needle in both forms. Sketch:

```go
func (c *Capture) redact(s string) string {
	if c.home == "" || c.home == "/" {
		return s
	}
	s = replaceBothForms(s, c.home, "/home/user")
	if rel := stripRoot(c.home); rel != "" && rel != c.home {
		s = replaceBothForms(s, rel, "home/user")
	}
	return s
}

// replaceBothForms replaces needle as written and as JSON encodes it. The
// frames are JSON, so a Windows separator reaches the file doubled; a POSIX
// needle contains no backslash, making the second replacement identical to
// the first and therefore a no-op (MADR 0151 D2).
func replaceBothForms(s, needle, with string) string {
	s = strings.ReplaceAll(s, needle, with)
	if esc := strings.ReplaceAll(needle, `\`, `\\`); esc != needle {
		s = strings.ReplaceAll(s, esc, with)
	}
	return s
}
```

The raw replacement runs first deliberately: it is the form the existing tests
pin, and running it first means the escaped pass can only ever match what the
raw pass did not.

Add rows to `TestRedactAbsoluteAndRelativeHome` whose input comes from
`json.Marshal`, not from a hand-written literal — a windows home, a UNC home,
and a posix home that must come back identical to today's result.

**Verification.**

```bash
go test ./internal/wirecap/ -run TestRedact -count=1 -v
```

The new windows-escaped row must fail before the `replaceBothForms` change and
pass after. Run it in that order; a row that never failed proves nothing (this
is A5, and it is the criterion most likely to be skipped).

### P2 — scrub the committed grok fixture (D4; closes F5)

`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`, four frames whose
`_meta` carries `email`, `team_id`, `auth_mode`, `subscription_tier` and
account flags. Replace the identifying values with inert placeholders —
`"user@example.com"`, a zero UUID — and leave every key, every other value, and
the line count alone (C4).

**Verification.**

```bash
grep -c '@' internal/provider/grok/testdata/wire/1.0.13/frames.jsonl   # 0
git diff --numstat -- internal/provider/grok/testdata/wire/1.0.13/frames.jsonl
go test ./internal/provider/grok/ -count=1
```

The `numstat` line count added must equal the count removed: four frames
changed, no frame added or dropped.

### P3 — guard the committed fixtures (D6; closes F7, F8)

A test in `internal/wirecap` that globs
`../provider/*/testdata/wire/*/frames.jsonl`, reads each, and fails on:

* an `@` between two non-space runs (an address shape, deliberately strict —
  MADR open question 2);
* a drive-letter absolute path in either raw or escaped form (`C:\` / `C:\\`).

It must also assert its own reach — that it found at least five files — so a
glob that silently stops matching cannot turn the guard into a no-op. That
failure mode is MADR 0147 D11's lesson and it applies verbatim here.

**Verification.**

```bash
go test ./internal/wirecap/ -run TestCommittedFixtures -count=1 -v
git stash && go test ./internal/wirecap/ -run TestCommittedFixtures -count=1; git stash pop
```

The second run is against the un-scrubbed fixture and must fail. Without it the
guard is asserting that a file it may not have read contains nothing.

### P4 — say what redaction does not cover (D7; closes F8)

One sentence in the package comment: redaction replaces the operator's home
directory and nothing else, so a capture may carry any other identifier the
engine sends — an email address, an account id, a hostname. Point at 0151.

**Verification.** `gofmt -l`; the sentence names no mechanism that does not
exist.

## Verification (whole plan)

```bash
go test ./internal/wirecap/ -count=1 -v
go test ./... && go test -race ./...
grep -rc '@' internal/provider/*/testdata/wire/*/frames.jsonl
grep -rn 'runtime.GOOS\|os.PathSeparator\|filepath.VolumeName' internal/wirecap/
```

The last command must print nothing (C2).

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | A JSON-escaped Windows home is redacted | D1, F1 |
| A2 | Posix rows of the existing table pass unmodified | D2, C3 |
| A3 | No `runtime.GOOS` / `os.PathSeparator` / `filepath.VolumeName` in the package | D2, C2 |
| A4 | The grok fixture contains no email and no account id | D4, F5 |
| A5 | The new escaped row fails before P1's change and passes after | D3 |
| A6 | The fixture guard fails against the un-scrubbed fixture | D6 |
| A7 | The guard asserts it read at least five files | D6 |
| A8 | The package comment states redaction's limit | D7, F8 |

**A5 and A6 are the two most likely to be skipped**, and for the same reason:
both require deliberately running a new test against old content to watch it
fail, and once the fix is in, the opportunity is gone. A5 without its failing
run would be asserting that `ReplaceAll` works. A6 without its failing run
would be asserting that a glob returns files.

**A2 is the one most likely to be satisfied accidentally**, per the C3 trap:
the wrong implementation passes every posix row.

## Rollout and Rollback

Test-and-fixture changes plus a four-line function. No protocol, no wire
format, no user-visible surface, and the redaction path runs only when
`MCREMOTE_WIRE_CAPTURE_DIR` is set — never in a shipped daemon.

Each phase reverts independently. P2 is the only phase that changes a committed
artefact rather than code; reverting it restores the fixture byte-for-byte and
would then fail P3's guard, which is the correct coupling.

## Deferred (named, so they are not mistaken for oversights)

* **Redacting by key rather than by value** (MADR option B's surviving
  argument). It is the only design that would have prevented F5 rather than
  cleaned up after it, and it stays rejected here only because frames must be
  byte-verbatim. If a second account-metadata leak appears, that is the
  evidence to build it — over a copy, with the verbatim frame still written.
* **Reading the other four fixtures line by line.** They were checked by key
  name only, so an identifier under an unexpected key would have been missed.
  MADR open question 3 records this as **[unverified]**; P3's guard covers the
  two shapes that are known to matter, not the general case.
* **A recapture of the grok fixture.** P2 edits values in place. A capture
  taken on a scrubbed account would be cleaner, needs a live engine, and would
  change the fixture's shape — which is the thing the version pin cites.
* **W3 and W4 from the 2026-09-07 Windows pass.** The workspace validator
  accepting drive-absolute paths, and `WriteFileAtomic` having no retry for
  sharing violations. Both are Windows-correctness, neither is privacy, and
  neither shares a file with this record.
