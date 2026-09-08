---
status: proposed
date: 2026-09-07
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# wirecap redacts a home path, but the wire carries JSON — and more than a home path

## Context and Problem Statement

`internal/wirecap` writes an engine's raw frames to `frames.jsonl` so a version
pin can cite evidence that the wire shape was re-checked. Those files are
committed: five of them live under `internal/provider/*/testdata/wire/`, in a
repository that is public.

Its one privacy control is `redact`, which replaces the operator's home
directory with `/home/user` in two forms — absolute, and with the root stripped.
Two problems, found while grounding a Windows debugging-pass finding:

1. **The frames are JSON, and JSON escapes a backslash.** A Windows home
   reaches the file as `C:\\Users\\alice`, which matches neither replacement.
   Redaction is a complete no-op on the platform this repository is currently
   developed on.
2. **A home directory is not the only identifying thing an engine sends.** The
   committed grok fixture carries the operator's email address and vendor
   account identifiers. `redact` has no concept of them, so it did not miss
   them — it was never asked.

The second is realised, in public, today. The first is latent only because
every fixture so far was captured on a POSIX host.

### What was measured, not assumed

All on `def36a6`, Windows 11, Go 1.26.6.

**The JSON-escaped form defeats both replacements.** Probed by copying
`wirecap.go` outside the repository and calling the real `redact`:

```console
$ go test -run Windows -v
=== RUN   TestJSONEscapedWindowsHomeSurvivesRedaction
    frame on the wire: {"cwd":"C:\\Users\\alice\\proj"}
    after redact:      {"cwd":"C:\\Users\\alice\\proj"}
    LEAK: username survives redaction
--- FAIL: TestJSONEscapedWindowsHomeSurvivesRedaction
=== RUN   TestRawWindowsHomeIsRedacted
    after redact:      cwd=/home/user\proj
--- PASS: TestRawWindowsHomeIsRedacted
```

The second case is what `wirecap_test.go:106` already pins. The first is the
form that actually reaches the file.

**Every capture site records JSON.** `wire.Frame` is called at five places,
and each hands it a JSON document:

```text
acphttp/conn.go:82    json.Marshal(jsonRPCRequest{...})
acphttp/conn.go:97    the HTTP response body of POST /acp
acphttp/ws.go:207     a websocket frame
codex/conn.go:280     one JSON-RPC line, unmarshalled on the next statement
httpagent/provider.go:774  the payload of an SSE `data:` line
```

There is no call site where the bytes are not JSON, so the escaped form is not
an edge case — it is the only form a Windows path can take.

**No committed fixture carries a Windows path.** All five contain `/home/user`
and zero occurrences of `C:\\`. The ten escaped backslashes in the grok fixture
are `Ctrl+\` in help text, not paths. Every fixture was captured on a POSIX
host, which is why nothing has leaked by this route yet.

**The grok fixture carries account metadata.** Counting keys across all five:

| Fixture | `email` | `team_id` |
| --- | --- | --- |
| `grok/testdata/wire/1.0.13/frames.jsonl` | 4 | 4 |
| the other four | 0 | 0 |

The frame is an `initialize` result whose `_meta` object carries the operator's
email address, an account `team_id` UUID, `auth_mode`, `subscription_tier`, and
several account flags. No token, API key, secret, or `Authorization` header
appears in any fixture — checked by key name across all five.

It landed on 2026-09-03 in `b0fecd9`, and again in `fc5b75e` the same day.

**The email is already public by another route.** Every commit in this
repository carries the same address in its author metadata (`git log -1
--format=%ae`). The fixture therefore adds no new fact about *who*; what it
adds is the vendor account context — team UUID, subscription tier, auth mode —
tied to that identity.

### Findings

**F1 — `redact` is a no-op against a JSON-escaped Windows home.** Neither
`strings.ReplaceAll(s, c.home, ...)` nor the `stripRoot` form matches
`C:\\Users\\alice`, because both hold single separators and the wire holds
doubled ones. Proven above.

**F2 — POSIX is unaffected.** `encoding/json` does not escape `/`, so a POSIX
home appears on the wire exactly as `redact` expects. This is a Windows-only
defect, and the reason it has stayed invisible is that the fixtures predate the
Windows host.

**F3 — the defect is in the only form that matters.** All five capture sites
record JSON (evidence above), so on Windows there is no path through this
package where redaction works.

**F4 — F1 is latent, not realised.** No committed fixture contains a Windows
path. The exposure begins the first time a fixture is captured on this host,
which is now the active development machine.

**F5 — the grok fixture leaks account metadata, and this is realised.** Four
frames carry the operator's email, team UUID, subscription tier and auth mode,
public since 2026-09-03.

**F6 — F5 is a smaller disclosure than it first appears, and saying so is part
of assessing it.** The email is already in every commit's author metadata, so
it is not newly exposed. The team UUID and subscription tier are. No credential
of any kind is present.

**F7 — this is the third instance of one class, not a new bug.** The class is
"the redaction rule does not match what the wire actually carries":

| When | Instance | Found by |
| --- | --- | --- |
| MADR 0137 Phase 1 | the stripped form (`Users/alice`) survived | grepping a fixture that had already been "redacted" |
| MADR 0144 amendment | the host's separator made the stripped form unreachable on Windows | this package's own new test |
| now | JSON escaping defeats both forms | grounding a Windows debugging-pass finding |

Each was found by looking at the output rather than the rule. That is the
pattern, and it is why F8 matters more than F1.

**F8 — `redact` protects one value, and the record has never said so.** It
replaces the home directory. It has no concept of an email address, an account
identifier, a hostname, or a token, and nothing in the package documents that
limit — the package comment says frames are "byte-verbatim apart from the
home-path redaction", which is accurate but reads as reassurance. F5 is what
that gap looks like in practice.

## Decision Drivers

* The fixtures are committed to a public repository, so a miss is not
  recoverable by editing the working tree.
* Frames must stay byte-verbatim: their whole purpose is to reproduce an
  engine's own bytes for a version pin.
* MADR 0144's lesson stands — redaction is string surgery on bytes another
  process produced, so nothing may be derived from the host running the
  capture.
* The active development machine is now Windows, so F1 stops being latent at
  the next capture.
* Anything asserted about a public repository's contents needs the owner's
  decision, not the agent's.

## Considered Options

* **A — Escape-aware redaction, a fixture scrub, and a committed-fixture
  guard** (chosen)
* **B — Redact structurally: unmarshal each frame, walk it, re-marshal**
* **C — Fix F1 only; treat F5 as acceptable**
* **D — Stop committing fixtures**

## Decision Outcome

Chosen option: **A**, because it fixes the proven defect, removes the realised
disclosure, and adds the only thing that would have caught either one — a check
on the artefact rather than on the rule.

### The decisions

**D1 — redact the JSON-escaped form as well as the raw one.** For each of the
two forms `redact` already builds, also replace the form with every backslash
doubled. This is a string transform on the needle, not on the frame, so the
frame stays byte-verbatim.

**D2 — derive nothing from the host.** The doubling is applied unconditionally,
not under `runtime.GOOS == "windows"`. A POSIX home contains no backslash, so
the extra needle is identical to the original and the extra replacement is a
no-op — which keeps the behaviour testable on either platform and honours the
constraint 0144's amendment established.

**D3 — the test table gains the escaped rows.** `TestRedactAbsoluteAndRelativeHome`
grows cases whose input is real `json.Marshal` output rather than a hand-written
string, so the test's fixture cannot drift from what the encoder actually
produces.

**D4 — scrub the account metadata from the committed grok fixture.** Replace
the `_meta` values with inert placeholders, in place, in a normal commit.

**D5 — do not rewrite git history.** The email is already public through commit
authorship (F6), so a force-push would not un-publish the identity; it would
break every clone and every commit reference in the 0137 records to remove a
team UUID and a subscription tier. Recorded as a decision rather than an
omission, because "scrub a leak" reflexively suggests rewriting history and the
reason not to here is specific.

**D6 — add a guard on the committed fixtures themselves.** A test that reads
every `testdata/wire/*/frames.jsonl` and fails on an email address or a
Windows-shaped absolute path. Both prior instances of this class (F7) were
found by looking at a fixture; this makes that a test instead of a habit.

**D7 — say in the package comment what redaction does not cover.** One
sentence: it replaces the home directory and nothing else, so a capture may
carry any other identifier the engine sends.

### Consequences

* Good: the Windows capture path stops being a no-op before the first Windows
  fixture is taken (F4's deadline).
* Good: D6 catches the next instance of F7's class in CI rather than by
  inspection, and it catches instances redaction was never designed to handle.
* Good: D4 removes the only realised disclosure, and it is a test fixture, so
  nothing depends on the values.
* Neutral: `redact` gains two more `ReplaceAll` calls on a path that runs only
  when capture is enabled, which is never in a shipped daemon.
* Bad: D6 is a keyword guard. It will catch an email address and a `C:\` path
  and will not catch an identifier nobody thought of, which is exactly the
  failure mode F7 describes. It narrows the class; it does not close it.
* Bad: D5 leaves a team UUID and a subscription tier in the repository's
  history permanently. That is the cost of not rewriting, stated rather than
  glossed.

### Confirmation

```bash
# 1. The escaped form is redacted (D1, D3):
go test ./internal/wirecap/ -run TestRedact -count=1 -v

# 2. Unchanged on POSIX-shaped input (D2) — same command, the posix rows.

# 3. No fixture carries an email or a Windows path (D4, D6):
go test ./internal/wirecap/ -run TestCommittedFixtures -count=1 -v
grep -c '@' internal/provider/*/testdata/wire/*/frames.jsonl   # expect 0 matches

# 4. Frames stay byte-verbatim apart from redaction:
go test ./internal/wirecap/ -count=1
```

## Pros and Cons of the Options

### A — Escape-aware redaction, fixture scrub, committed-fixture guard (chosen)

* Good, because it fixes the proven defect with a two-line change to the needle
  and no change to how frames are written.
* Good, because the guard checks the artefact that actually ships, which is
  where all three instances of this class were found.
* Good, because it separates the latent defect (F1) from the realised one (F5)
  and does not let a green test suite imply the second was handled.
* Neutral, because the guard's keyword list will need extending; that is
  visible work rather than silent rot.
* Bad, because it leaves redaction as substring replacement, so a home
  directory that appears in some third encoding would evade it again.

### B — Redact structurally: unmarshal, walk, re-marshal

* Good, because it would handle every encoding of a path uniformly, and could
  redact by *key* (`email`, `team_id`) rather than by value — which is the only
  approach that would have caught F5 automatically.
* Bad, and decisively, because it destroys the package's purpose: frames are
  byte-verbatim so a fixture reproduces the engine's own bytes. Re-marshalling
  re-orders keys, changes number formatting and drops unparseable frames — and
  codex's read pump explicitly tolerates frames that do not unmarshal
  (`conn.go:282`), which are exactly the frames a wire fixture most wants to
  keep.
* This option's strongest argument survives rejection and is worth recording:
  redacting by key is the only design that would have prevented F5 rather than
  cleaned up after it. If a second account-metadata leak appears, that is the
  evidence to revisit this — applied at capture time to a *copy*, with the
  verbatim frame still written.

### C — Fix F1 only; treat F5 as acceptable

* Good, because F6 is real: the email adds nothing that commit authorship does
  not already publish.
* Bad, because the team UUID and subscription tier are not published elsewhere,
  and "it is already partly public" is not a reason to leave the rest.
* Bad, because it leaves the fixture as a standing counter-example to the
  package's own claim about what it protects.

### D — Stop committing fixtures

* Good, because it removes the exposure class entirely.
* Bad, because the fixtures exist to let a version pin cite evidence
  (MADR 0137 Phase 1); removing them removes the reason the package exists.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| `redact` misses the JSON-escaped Windows home | probe against the real `redact` body, output in *What was measured* |
| The raw Windows form *is* redacted | same probe, second case; `wirecap_test.go:106` |
| Every capture site records JSON | `acphttp/conn.go:82,97`, `acphttp/ws.go:207`, `codex/conn.go:280`, `httpagent/provider.go:774` |
| `encoding/json` does not escape `/` | probe output shows `C:\\` doubled and `/home/user` untouched |
| No committed fixture holds a Windows path | `grep -o 'C:\\\\'` across all five: zero |
| The ten escaped backslashes are `Ctrl+\` | context grep on the grok fixture |
| grok fixture holds 4 `email` and 4 `team_id` | key-name count across all five fixtures |
| No credential of any kind in any fixture | key-name search for `token`, `api_key`, `secret`, `authorization`, `access_token`, `refresh_token`: zero |
| Fixture landed 2026-09-03 | `git log -- internal/provider/grok/testdata/wire/1.0.13/frames.jsonl` → `b0fecd9`, `fc5b75e` |
| The email is already in commit authorship | `git log -1 --format=%ae` |
| The repository is public | `gh repo view --json visibility` → `PUBLIC` |
| Two prior instances of the same class | MADR 0137 Phase 1; MADR 0144 amendment |

### Related records

* **MADR 0137** — created `wirecap` and the fixture practice; its Phase 1 found
  the first instance of F7's class.
* **MADR 0144** — added this package's tests, and its amendment found the
  second instance. Its root-cause paragraph (redaction must not consult the
  host) is D2's direct ancestor.
* **MADR 0150** — the Windows debugging pass this finding came from, recorded
  there as W1 and deliberately left out of that record's scope.

### Open questions for the plan

1. **Is `team_id` worth the scrub on its own?** D4 assumes yes. If the owner
   judges a vendor team UUID and subscription tier as uninteresting, P2 can be
   dropped and D6 narrowed to emails and paths — the fix for F1 does not depend
   on it.
2. **Should the guard fail on any `@`, or on an address shape?** Any `@` is
   stricter and will catch a mention in prose; an address regex is precise and
   will miss an obfuscated one. The plan picks the strict form; it is a
   one-line reversal.
3. **Do the other four fixtures carry anything the keyword guard will not
   name?** They were read only by key name, not line by line. **[unverified]**
