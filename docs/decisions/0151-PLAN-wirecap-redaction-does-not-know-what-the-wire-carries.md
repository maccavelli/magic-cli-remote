---
status: completed
date: 2026-10-29
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

## Amendment — 2026-09-07: the guard is an address shape, not a bare `@`

P3 specified "an `@` between two non-space runs", and MADR open question 2
chose that strict form on the reasoning that it is stricter and the reversal is
one line. Executing P2 produced the evidence that settles it the other way.

After the scrub, the grok fixture still contains ten `@`, and none is an
address:

| Occurrence | Count | What it is |
| --- | --- | --- |
| `@src/main.rs` | 10 | grok's file-reference prompt syntax, in help text |
| `@!.github/workflows` | 10 | the same syntax, negated form |
| `user@example.com` | 4 | P2's own placeholder |

**Corrected P3:** the guard matches an address shape, and allows the RFC 2606
documentation domains (`example.com`, `.org`, `.net`) so that P2's placeholder
does not trip it.

The strict form would have fired on ten legitimate lines of the one fixture
this guard exists for, and a guard that cries wolf on its own subject gets
weakened or deleted — which is a worse outcome than a narrower guard adopted
deliberately. It would also have rejected the placeholder P2 introduced, so the
two phases as written could not both have been satisfied.

**A4's verification command is corrected with it.** `grep -c '@' … # 0` cannot
hold and never could; the check is that no address outside a documentation
domain appears, which is what the guard asserts.

## Execution record — 2026-09-07

All four phases ran, one commit each, in the planned order.

| Phase | Commit | Result |
| --- | --- | --- |
| P1 | `12bebe6` | escaped-form redaction and the `json.Marshal` rows |
| P2 | `5f863a8` | four frames scrubbed, 247 lines before and after |
| P3 | `79d1de6` | fixture guard, both arms sabotage-tested |
| P4 | `6b78e3f` | the package comment says what redaction does not cover |

### Acceptance criteria

| # | Result |
| --- | --- |
| A1 | met — the escaped windows and UNC rows pass |
| A2 | met — all three original rows pass unmodified |
| A3 | met in substance; **the criterion's command was wrong** (see below) |
| A4 | met as amended — no address outside a documentation domain, no account id |
| A5 | met — the new rows failed before P1's change and passed after |
| A6 | met on both arms — the pre-scrub fixture and an injected `C:\Users\alice` each fail the guard |
| A7 | met — the guard fails if its glob matches fewer than five files |
| A8 | met — P4 |

### What the plan predicted incorrectly

**A3's command does not test A3.** It greps the package for `runtime.GOOS`,
`os.PathSeparator` and `filepath.VolumeName` and expects nothing. It prints six
matches, every one inside a comment explaining why those functions are *not*
used — comments MADR 0144's amendment left deliberately. The criterion holds
(no code consults the host) but the check cannot distinguish code from prose,
and a future reader running it verbatim would conclude the contract was broken.
A correct check would exclude comments or, more simply, be the test table
itself: C2's real guarantee is that no row needs a platform gate, and the table
demonstrates that on every run.

**P3's guard shape was wrong**, and P2 is what proved it. Recorded in the first
amendment above.

**Two of the three edits in this plan were made with a blanket string
replacement, and both were corrupted by it.** P1's `replaceBothForms` was
written by a script whose escaping collapsed `` `\` `` and `` `\` `` into the
same string, producing a function that compiled, read correctly, and did
nothing — `esc` always equalled `needle`, so the escaped branch never ran. The
test caught it immediately, which is the only reason A5's ordering mattered
twice over. The doc comment in the same edit lost its doubled separators the
same way. This is the third instance in one session of a blanket replace
damaging text that mentions both sides of a change (MADR 0150 P2's `killTree`,
MADR 0152's name flip). The pattern is now well enough evidenced to state as a
rule: when old and new strings both appear in the surrounding prose, a blanket
replace is the wrong tool, and a compiler is not a sufficient check on one.

### Verification at completion

`gofmt` clean; `go test ./...` and `go test -race ./...` green; the Windows
gate reports `ALL SELECTED CHECKS PASSED`.

## Amendment — 2026-10-28: P5, the host's name (MADR amendment of 2026-10-28, D8 to D10)

*Approved by the owner, 2026-10-28: "Fix under 0151, first (Recommended)". Its own commit, before
the commit of 0207's C3.*

### P5 — redact the host's name, scrub two fixtures, guard

**Files:** `internal/wirecap/wirecap.go`, `wirecap_test.go`, `fixtures_test.go`;
`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`;
`internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`; this plan; the MADR.

1. **Tests first,** each seen failing on a scratch copy:
   - `redact` replaces a host name, as written and JSON-escaped, and its short form, with
     `<host>`; it leaves a name shorter than four characters alone, and redacts with no home set;
   - `TestCommittedFixturesCarryNoIdentifiers` fails on a fixture holding the running host's name.
2. **Code** as tested: `Capture.host`, set in `For`; `redact` gains the replacement.
3. **The fixtures:** the name replaced with `<host>`, five occurrences, and no other byte changed
   (`git diff --word-diff`).
4. **Gates:** `make pre-add-check FILES=…`, `make test`, `make race`, `go vet` for the three targets,
   `make check-records`, the guard's redact preview, and `go test ./internal/provider/grok/
   ./internal/provider/codex/ ./internal/provider/acpagent/`, whose tests read these fixtures.

### P5 execution — 2026-10-28

**Step 1, the tests.**

- In `wirecap_test.go`:
  - `TestRedactHostName`: a host name and its FQDN, as written and lowercased, are replaced with
    `<host>`, four times in one JSON frame, the full name before the short one; a two-letter name
    is left alone; an unconfigured capture changes nothing;
  - `TestForRecordsTheHostName`: `For` records `hostNames` of the running host.
- `fixtures_test.go`: `TestCommittedFixturesCarryNoIdentifiers` also fails on the running host's
  name, naming the line and not the value.

**The guard's first run was on the real fixtures,** before the scrub. It failed on exactly the
five occurrences: "line 2: this host's name, unredacted (MADR 0151 F9)" in codex 0.152.1, and
lines 1, 20, 101 and 179 in grok 1.0.13.

**Step 2, the code** (`wirecap.go`): `Capture.hosts`, set in `For` from `os.Hostname()` through
`hostNames`, which returns the full name, the short one and each lowercased, longest first, none
under four characters. `redact` replaces them with `<host>` before the home, whatever the home.
The package comment now says redaction covers the home and the host's name, and nothing else.

**Step 3, the fixtures** (`leftovers/p5_scrub.py`): the name replaced with `<host>`, 4 times in
grok 1.0.13 and once in codex 0.152.1. The script asserted the counts, that the name was then
absent, and that no `<host>` was there before. `git diff --numstat` shows `4 4` and `1 1`: those
lines and no others.

**Seen failing** (`leftovers/p5_plants.py`; scratch copies of the working tree):

| Plant | The failure |
| :--- | :--- |
| `redact` without the host loop | "the host's name leaked after redact: {…\"hostname\":\"Build-Box-7\"…}" |
| no lowercase forms | "… \"lower\":\"build-box-7.corp.example\" …" |
| the short name replaced first | "… \"fqdn\":\"<host>.corp.example\" …" |
| no length floor | "hostNames(\"ab\") = [\"ab\"], want none" |
| `For` without the host | "hosts = 0 name(s), want hostNames of this host, 2" |
| the name put back into grok 1.0.13's first line | "line 1: this host's name, unredacted (MADR 0151 F9)" |
| none (the control) | exit 0 |

**Step 4, the gates:**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (the three Go files) | "3 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0 |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64 | exit 0, each |
| `make test`, `make race` | exit 0; 44 packages ok |
| `go test -race ./internal/wirecap/ ./internal/provider/grok/ ./internal/provider/codex/ ./internal/provider/acpagent/` | ok, each; the tests that read the two fixtures pass with `<host>` |
| `go test -race -count=1 ./...` on the Linux test host, in a scratch copy removed afterwards | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs`; the guard's redact preview | exit 0; "0 issues"; "would edit 0 file(s)" |

**Files for this commit:** `internal/wirecap/wirecap.go`, `wirecap_test.go`, `fixtures_test.go`;
`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`;
`internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`; the MADR; this plan.

**Not done, by D5:** the old values stay in git history, in `b0fecd9f`, `fc5b75ec` and the commits
that added and touched codex 0.152.1's capture. Nothing is force-pushed.

P5 is complete, and the plan is `completed` again.

## Amendment — 2026-10-29: P6, the drive-path check reads a line by its encoding (MADR amendment of 2026-10-29, D11)

*Approved by the owner, 2026-10-29: "option 2, but determine the best way to ensure the guard will
work with a raw windows path". Its own commit, before the commit of 0207's C3.*

### P6 — the drive-path check reads a line by its encoding

**Files:** `internal/wirecap/fixtures_test.go`; this plan; the MADR.

1. **Test first,** seen failing on a scratch copy: `TestWindowsPathByEncoding`.
   - Hits: a JSON line holding a path as `json.Marshal` writes it; a JSON line holding one escaped
     as `\u005c`; a line that is not JSON holding a raw path; and one holding an escaped path.
   - No hit: a JSON line whose text has a colon followed by a newline and a tab.
2. **Code** as tested: `windowsPath(line)` searches a JSON line with `escapedDrive`, and any other
   line with `driveAbsolute`, unchanged. The guard calls it.
3. **Plants,** each on a scratch copy:
   - D6's pattern for every line;
   - the escaped pattern for every line;
   - the escaped pattern without `\u005c`;
   - the guard calling `driveAbsolute` again, with grok 1.0.70's capture present.
4. **Gates:** `make pre-add-check FILES=internal/wirecap/fixtures_test.go`, `make staticcheck`,
   `go vet` for the three targets, `make test`, `make race`, `make check-records`,
   `make markdownlint-docs`, and the guard's redact preview. They run with grok 1.0.70's capture in
   the working tree, which this commit does not include.

### P6 execution — 2026-10-29

**Measured first** (`leftovers/p6_measure.py`): every line of the five committed fixtures, and of
grok 1.0.70's capture, is JSON. D6's pattern matches nothing in the five. In grok 1.0.70's capture
it matches four times, `e:\n` twice on each of lines 10 and 12. The escaped pattern matches nothing
in any of the six.

**Steps 1 and 2, the test and the code** (`fixtures_test.go`):

- `escapedDrive`, `[A-Za-z]:(\\\\|\\u005[cC])[A-Za-z]`; `driveAbsolute` is unchanged, and its
  comment says what it misreads in a JSON line.
- `windowsPath(line)` searches a line that `json.Valid` accepts with `escapedDrive`, and any other
  line with `driveAbsolute`. `TestCommittedFixturesCarryNoIdentifiers` calls it.
- `TestWindowsPathByEncoding`, five cases: a path as `json.Marshal` writes it, one escaped as
  `\u005c`, a raw path in a line that is not JSON, and an escaped one there, are each found; a
  JSON line holding "In scope:", a newline, "Out of scope:" and a tab is not.

**Step 3, seen failing** (`leftovers/p6_plants.py`; scratch copies of the working tree, grok
1.0.70's capture included):

| Plant | The failure |
| :--- | :--- |
| D6's pattern for every line | "windowsPath({\"v\":\"In scope:\\nOut of scope:\\tx\"}) = \"e:\\\\n\", want a path: false", and the guard's "line 10" and "line 12" on grok 1.0.70 |
| the escaped pattern for every line | "windowsPath(stderr: C:\\Users\\alice\\proj) = \"\", want a path: true" |
| the escaped pattern without `\u005c` | "windowsPath({\"v\":\"C:\\u005cUsers\\u005calice\"}) = \"\", want a path: true" |
| the guard calling `driveAbsolute` again | "line 10: unredacted Windows path starting \"e:\\\\n\" (MADR 0151 F1)", and line 12 |
| none (the control) | exit 0 |

**Step 4, the gates** (`leftovers/p6_gates.py`), with grok 1.0.70's capture in the working tree:

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=internal/wirecap/fixtures_test.go` | "1 file(s) clean (gofmt, golint, govulncheck)" |
| `make staticcheck` | exit 0; "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name" |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64, with `CGO_ENABLED=0` | exit 0, each. The first run left `CGO_ENABLED` unset, and linux/amd64's vet failed on cgo against the macOS SDK; the earlier phases' gate scripts set it |
| `make test`, `make race` | exit 0; 44 packages ok |
| `make check-records`, `make markdownlint-docs`; the guard's redact preview | exit 0; "0 issues"; "would edit 0 file(s)" |

No run on the Linux test host: the change is one test file, and `json.Valid` and `regexp` behave
the same on every GOOS.

**Files for this commit:** `internal/wirecap/fixtures_test.go`; the MADR; this plan. Not grok
1.0.70's capture, which is C3's.

P6 is complete, and the plan is `completed` again.
