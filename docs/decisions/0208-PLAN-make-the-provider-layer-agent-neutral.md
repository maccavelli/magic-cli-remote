---
status: in-progress
date: 2026-10-21
associated-madr: "0208-MADR-make-the-provider-layer-agent-neutral.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement making the provider layer agent-neutral, pass-through first

Associated MADR: [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md)

## Goal

Magic forwards each agent CLI's own commands and output to the phone wherever the agent can run
them remotely, and rewrites as little as possible. Shared layers carry no vendor's behaviour, the
phone renders raw output with one set of heuristics, and a new agent CLI needs only its own package
and a registration line.

## Scope

* **In scope:** the MADR's D1–D13, in the order the owner chose (answer Q7): D11, D13, D9 and D10,
  D12, then D1 and D2, then D3, D6, D5 with D4, and D7.
* ~~**Gated:** P9 (D4 and D5) waits for the owner's answers to Q2 (default provider) and Q3
  (`codex_surface` overlap). No other phase depends on them.~~ *Answered 2026-10-21: gobble becomes
  the default provider, and API compatibility and native integration are design goals (the MADR's
  owner answers on Q2 and Q3). No phase is gated now.*
* **Not in scope:**
  * registering gobble, which is 0179's PLAN once P6 has landed (answer Q4);
  * 0181's configurable message size; P2 uses its setting if it has landed by then, and a constant
    beside `maxWSMessageBytes` if not;
  * live suites that spend provider quota. Each is named where it applies, and runs only on the
    owner's word for that run.

## Implementation Steps

### Rules for every phase

* **The phase's files are named at its start.** A file outside that list is a deviation: stop,
  prompt and record.
* **Every new test or gate is seen failing first,** on a scratch copy of the tree or against
  today's code before the change, never by dirtying the working tree. The execution record says
  what the failure looked like.
* **Fixtures come from committed wire captures** (`internal/provider/*/testdata/wire/`), redacted
  as `internal/wirecap` redacts. A capture is never edited to suit a test.
* **Each phase ends in one commit** that passes its verification. The agent stages; the owner
  commits.
* **Go:** `go test -race ./...`, `make pre-add-check FILES="…"` on every staged Go file,
  `make lint`, `make staticcheck`. **Dart:** `make preflight` (`flutter analyze`, `flutter test`,
  `dart format`).
* **Windows:** a phase that changes process, path or file handling also passes `make ci-windows`
  on the Windows test host before it is called done.
* ~~**API compatibility (owner, 2026-10-21).** A phase that changes the phone protocol adds, and
  never removes or repurposes, a field, event or request. Each addition is advertised as a server
  or client capability. The phase adds two tests, each seen failing on a planted break:~~
  * ~~an *older phone*: a protocol client that advertises none of the phase's new capabilities gets
    everything it got before, in the shapes it got it;~~
  * ~~an *older daemon*: the phone, against a fake server that advertises none of them, keeps its
    current behaviour.~~

  *Replaced 2026-10-21 by the owner's direction "greenfield, I am the only phone user" (the
  MADR's owner direction):* a phase that changes the phone protocol changes the daemon, the phone
  and `docs/guides/protocol-v2.md` in the same commit, and its tests cover the new shape only.
  Each agent CLI's published API is still used as published.

### P1 — Raw tool text on the ACP path, and one shared bound (D11, first half)

Files: `internal/provider/output/` (new), `internal/provider/acpagent/session.go` and its tests,
`internal/provider/opencode/http.go`, `internal/provider/opencode/parts.go`,
`internal/provider/opencode/workspace.go`, `internal/provider/kilo/session.go`, and their tests.

1. **Failing first.** A table test in `acpagent` feeds tool calls and updates whose content is a
   directory listing, an aligned table, a stack trace and an ANSI-coloured line, taken from the grok
   captures where they exist and written as fixtures where they do not. It asserts that each event's
   `Text` keeps the original newlines, indentation and escape bytes. On today's code it fails,
   because `summarizeToolContent` collapses whitespace (`session.go:2835`).
2. **The shared package.** `internal/provider/output` holds:
   * `MaxToolText = 8000`, today's cap shared by opencode, kilo and the phone;
   * `Clip(s string, max int) (string, bool)`, which trims trailing whitespace, cuts on the last
     line boundary at or before `max` (on a rune boundary when one line is longer than `max`),
     appends `"\n…[truncated]"`, and reports whether it cut;
   * `Line(s string, max int) string`, the one-line form for titles and error headlines: it joins
     a multi-line string's first non-empty line only, and never collapses the inside of a line.

   Unit tests cover a cut inside a long line, a multi-byte rune at the boundary, an exact fit, and
   empty input.
3. **ACP.** `summarizeToolContent` builds the body from content blocks joined by a blank line, with
   no whitespace collapse, bounded by `output.Clip(…, output.MaxToolText)`. The 400 and 600 caps
   go. The diff and terminal placeholders stay until P2. The raw-JSON fallback (no content blocks)
   is kept as it is today.
4. **opencode and kilo.** Their `clipBlock` and `maxToolOutputChars` copies are replaced by
   `output.Clip` and `output.MaxToolText`. `clip` for error text becomes `output.Line` plus
   `output.Clip`, so a multi-line error keeps its lines. Their existing truncation tests are
   updated for the line-boundary cut; each changed expectation is listed in the execution record.
5. **The source test.** A test in `internal/provider/output` fails if any non-test file under
   `internal/provider/` contains `strings.Join(strings.Fields(`. Seen failing on today's tree,
   which has three (`acpagent/session.go:2835`, `opencode/http.go:1827`, `kilo/session.go:927`).

Verification: the Go rules. Quota-free replays of the grok, opencode and kilo wire suites pass.

### P2 — Whole output on demand, diffs and terminal output (D11, second half)

Files: `internal/event/event.go`, `internal/protocol/messages.go`, `internal/session/` (an output
store and its tests), `internal/ws/server.go`, `internal/provider/acpagent/session.go`,
`docs/guides/protocol-v2.md`, and on the phone `lib/data/protocol/models.dart`,
`lib/data/ws/mcremote_client.dart`, `lib/features/chat/chat_bubble.dart` and their tests.

1. **Event fields, additive.** A tool event gains `truncated` (bool), and `diff`
   (`path`, `old_text`, `new_text`) when the agent sent a diff. ACP fills `diff` from
   `ToolCallContent.Diff`, and terminal output from the terminal registry Magic already keeps for
   the client-side terminal it runs.
2. **The output store.** The session manager keeps the unclipped text of each tool event, keyed by
   session and tool-call id, bounded per session by bytes (oldest evicted first). The per-session
   bound is a constant beside `maxWSMessageBytes`, or 0181's setting if it has landed.
3. **The request.** `session.tool_output {session_id, tool_call_id}` returns the whole text, or
   `not_found` once it was evicted. ~~It is advertised as a server capability, so an older phone
   never sends it.~~ *(Greenfield, 2026-10-21: the phone and daemon ship together.)*
4. **The phone.** A truncated tool card offers "Show all", which fetches the text. A diff renders
   from `diff`'s old and new text in P3's diff view; until P3 it shows the new text in monospace.
5. **Tests, each seen failing first:** a store eviction test, a protocol round trip, a Flutter
   widget test for "Show all" with a fake client, and an `acpagent` test that a diff block fills
   `diff`.

Verification: the Go and Dart rules. The protocol guide documents the fields and the request.

### P3 — One renderer for raw output (D13)

Files: `apps/mobile/lib/data/chat/raw_output.dart` (new classifier), `apps/mobile/lib/features/chat/ansi_text.dart`
(new), `apps/mobile/lib/features/chat/raw_output_view.dart` (new), `chat_bubble.dart`,
`apps/mobile/test/` (new tests and fixtures), and `scripts/extract-raw-output-fixtures.py` (new).

1. **Fixtures.** `scripts/extract-raw-output-fixtures.py` reads the committed wire captures of
   grok, opencode, kilo and codex, and writes each tool output and native-command reply it finds as
   a fixture file under `apps/mobile/test/fixtures/raw_output/<provider>/`, with an expected-class
   sidecar the author fills in once by reading it. The script is modular, standard library only,
   and reads no path outside the repository.
2. **The classifier.** `classifyRawOutput(String text) → RawOutputKind` is a pure function with
   these kinds, tried in order: `diff` (a unified diff header and hunk lines), `json` (it parses as
   a JSON object or array), `terminal` (an ANSI escape, box-drawing characters, two or more lines
   whose columns align on runs of spaces, or a majority of indented lines), `markdown`, and
   `plain`. It never takes a provider id.
3. **The views.**
   * `ansi_text.dart` turns SGR sequences into `TextSpan`s on the theme's palette and drops every
     other escape;
   * `raw_output_view.dart` renders each kind: monospace with horizontal scroll for `terminal`,
     pretty-printed and collapsible for `json`, added and removed lines for `diff`, the existing
     Markdown widget for `markdown`;
   * anything over `kAssistantShowMoreChars` collapses behind "Show more".
4. **Where it applies.** Tool card detail always goes through the classifier. Assistant messages
   stay Markdown, unless the classifier says `terminal` or `json`, which covers a forwarded
   command's preformatted reply.
5. **Fallback.** A kind whose renderer throws, or a classification the fixture marks wrong, falls
   back to plain monospace. Text is never hidden.
6. **Tests:** a table test over every fixture, with expected kinds; a planted misclassification
   (a fixture with a deliberately wrong sidecar) that must fail the suite; widget tests for each
   view, with fixed-width-safe assertions only.

Verification: the Dart rules.

### P4 — The agent's own command first, and silent commands detected (D9, D10)

Files: `internal/command/command.go`, `internal/command/specs.go`, `internal/session/commands.go`,
`internal/session/manager.go`, `internal/session/` (a silent-mark store, new), `internal/cli/doctor.go`,
the four providers' `commandtable.go`, the phone's composer, and their tests.

**P4a — Detection, with today's tables unchanged.**

1. When the daemon forwards a slash command (a `KindNative` row, or an agent command outside the
   canonical set), it notes the turn. If that turn completes with no assistant chunk, no thought,
   no tool event, no plan, no notice and no error, the command is marked silent.
2. **The key** is provider, agent version, host and command name (answer Q6). The agent version is
   the one the session already reports for its version pin; the host is the daemon's
   `os.Hostname()`. A mark whose host or version differs from the running daemon's is ignored.
3. **The store** is a JSON file in the daemon's state directory, written atomically
   (`internal/fsutil`), loaded at start.
4. **Effects:** a notice to the phone ("`/<name>` produced nothing over the remote connection"),
   the command's `remote_commands` entry marked unavailable with that note, and a `doctor` section
   listing the marks.
5. **Tests, each seen failing first:** a fixture ACP agent that answers one command with nothing
   and another with text; a changed agent version clears the mark; a mark from another host is
   ignored.

**P4b — Resolution order.**

1. `Resolve` gains the D9 order: when the agent advertises a command whose name, or the provider
   table's native alias, matches the canonical name, and it is not marked silent, the native
   command wins over a table `KindOp` row. Daemon commands (`help`, `new`, `sessions`, `clear`)
   never forward.
2. Resolver tests over all five providers' tables. Because opencode, kilo and codex advertise no
   canonical command (MADR N8), their resolution is unchanged, and the tests assert that.
3. **grok's rows.** Rows that only say "returns nothing here" are deleted after a `make live-grok`
   run, with the owner's quota approval, shows P4a marking each of those commands silent on the
   current grok release. Until that run, they stay.

Verification: the Go and Dart rules. The live run above is P4b's acceptance.

### P5 — Engine answers pass through as JSON (D12)

Files, per operation: the provider's `runtime.go` or `usage.go`, `internal/session/commands.go`,
`internal/session/tokencost.go`, and their tests.

One operation per commit, in this order: grok usage and billing (`acpagent/runtime.go`), codex
status and usage (`codex/runtime.go`), kilo usage (`kilo/usage.go`), opencode and kilo session
totals (`httpagent/runtime.go`), then `/context`.

1. The operation returns the engine's response body, as the engine sent it, in a structured-result
   event the phone renders with P3's JSON view (answer Q5). No labels are added.
   * The first commit of this phase adds the structured-result event, in the daemon and the phone
     together.
   * ~~A phone that has not advertised that capability gets the same body as pretty-printed JSON in
     a fenced code block, in the text event it already renders. It never gets nothing.~~ *Dropped
     2026-10-21: greenfield, one phone user.*
2. `/context` has no engine answer: Magic computes it from usage events. It is emitted as the JSON
   of the usage record Magic already holds, with the same pass-through rendering. Where the agent
   advertises its own context command, P4's resolution forwards that instead.
3. The operation's formatter and its prose tests are deleted. A fixture test asserts that the
   event carries the captured engine body unchanged.
4. Each provider's live check of the operation needs the owner's quota approval.

Verification: the Go rules per commit.

### P6 — Declared capabilities, and vendor code in its vendor's package (D1, D2)

Files: `internal/provider/provider.go`, `internal/session/commands.go`, every adapter's session type,
`internal/provider/acpagent/` (session operations, extensions, quota, auth selection) and
`internal/provider/grok/`, with their tests.

1. **Failing first.** A fixture ACP Spec with no operations: a recorded connection across
   `/compact`, `/fork`, `/undo`, `/thinking high`, a phone fork, a phone rename and a classified
   quota error shows `_x.ai/*` frames on today's code. The test asserts none.
2. `provider.Session.Capabilities()`; `commandContext` reads it; each adapter declares honestly
   (MADR D1).
3. `acpagent.Spec.Ops` holds grok's seven operations, moved into `internal/provider/grok`
   unchanged on the wire, and `ConfirmLimit`. The incoming `_x.ai/exit_plan_mode` and
   `_x.ai/ask_user_question` handlers move to the grok Spec's extension map. Auth selection
   becomes a Spec policy, with grok's current order.
4. The source test: no `x.ai` or `_gobble/` string in `acpagent`'s non-test files. Seen failing on
   today's tree.
5. grok's unit, conformance and wire suites pass unchanged. `make live-grok` at acceptance, with
   the owner's quota approval.

Verification: the Go rules, and `make ci-windows` on the Windows test host.

### P7 — Standard ACP config options (D3)

Files: `internal/provider/acpagent/session.go`, `session_meta.go`, `acpagent.go`, `internal/event/event.go`,
the phone's config-option rendering, and their tests.

1. Grouped selects are forwarded with their groups, and every option with its category. A fixture
   with a grouped model option (gobble's shape) fails first on today's code, which forwards no
   values.
2. A `ListModels` built from the `model`-category option, for any Spec without its own.
3. `Spec.DangerousModeIDs`, gating the phone's confirmation like the synthetic `auto` mode.

Verification: the Go and Dart rules.

### P8 — One provider descriptor (D6)

Files: `internal/provider/registry.go`, each provider package, `internal/config/`,
`internal/daemon/daemon.go`, `internal/cli/doctor.go`, `internal/config/prewarm_write.go`, and their
tests.

1. `provider.Descriptor` and registration, one provider per commit, starting with `fake`.
2. ~~The config file's keys and environment variables do not change.~~ *Greenfield (2026-10-21):*
   the keys may change where a descriptor makes them simpler. Whatever shape lands, a test loads
   the documented example config and every documented environment variable through it, and is seen
   failing on a planted wrong key. If the shape changes, the owner's live config is migrated with a
   backup, only with the owner's go-ahead for that file.
3. The daemon, `doctor`, prewarm and validation iterate descriptors. At the end, `daemon.go` reads
   no `cfg.Providers.<Name>` field (74 today).

Verification: the Go rules, and `make ci-windows`.

### P9 — The daemon and the phone act on capabilities (D5, D4)

Files: `internal/ws/server.go`, `internal/session/manager.go`, `internal/protocol/messages.go`,
`internal/config/` (the default order), `docs/guides/config.md`, `docs/guides/protocol-v2.md`, the
ten Dart call sites in MADR N4, and their tests.

1. ~~Does not start until the owner answers Q2 and Q3. Their answers are recorded here before step
   2.~~ *Answered 2026-10-21; see Scope.*
2. **The default provider (answer Q2).**
   * `default_provider` in the config, and a built-in default order held as config data:
     `gobble`, then the rest in registration order. *(~~then `grok`~~ dropped 2026-10-21:
     greenfield.)*
   * `defaultProviderID` (`internal/ws/server.go:3500-3520`) walks that order and picks the first
     ready provider. It compares no provider id to a constant.
   * Tests: with gobble ready it is chosen; with gobble absent, the first ready provider in
     registration order is; with `default_provider` set, that provider is, whatever is ready
     first. Seen failing first on today's grok-first rule for the gobble case.
   * codex's approvals reviewer becomes a capability the codex session declares.
3. Session capabilities and resolved command mechanisms in the session payload. The ten Dart
   branches become capability checks.
4. **The neutral name for `codex_surface`.** ~~The daemon sends both the neutral field and
   `codex_surface`, and accepts both. The phone reads the neutral one and falls back to the old
   one. Removing `codex_surface` is not part of this PLAN.~~ *Greenfield (2026-10-21):* renamed
   outright in the daemon, the phone and the protocol guide, in one commit.
5. Source tests, each seen failing first: no Go comparison of a provider id to a constant outside
   the allowed packages; no Dart comparison of a provider id to a literal under `lib/`.

Verification: the Go and Dart rules.

### P10 — One conformance suite (D7)

Files: `internal/provider/conformance/` (new), and the fixture ACP agent from P6.

1. The suite runs over every registered descriptor with fixtures, and over the fixture agent:
   every available command executes; no foreign vendor method on the wire; declared capabilities
   match the session's answers.
2. Adding the fixture agent as a provider touches only its package and the registration list. The
   execution record shows the diff.

Verification: the Go rules.

## Verification

* Each phase's own verification, recorded with its output in the execution record.
* At the end: the MADR's Confirmation and the amendment's Confirmation, each item checked and
  cited.
* Provider live suites run only with the owner's approval for each run. Until a phase's named live
  run passes, that phase is not complete.

## Rollout and Rollback

* Each phase is one commit, revertible on its own.
* ~~Protocol changes are additive and advertised as capabilities. An older phone keeps working
  against a newer daemon, and a newer phone against an older daemon. Nothing in this PLAN removes a
  protocol field (answer Q3).~~ *Greenfield (2026-10-21):* the daemon and the phone change
  together, so a rollback reverts both, and the phone is rebuilt from the same commit.
* ~~The config file format never changes, so rolling a binary back needs no config change.~~ A
  phase that changes the config shape keeps the backup it took, and a rollback restores it.
* P4's silent-mark file is ignored by older binaries, and can be deleted to clear every mark.

## Execution record

### Approval, 2026-10-21

The owner said "Look at this as greenfield. I am the only phone user", then "Proceed". The
greenfield direction is recorded in the MADR and struck through above. P1 does not touch the phone
protocol or the config, so the direction does not change it. P1 starts on this approval.

### P1 Deviation 1, 2026-10-21 — four more files. The owner decided: "Add the 4 files"

- **Found** before any code change, while mapping step 4. opencode's and kilo's `clip`
  (`opencode/http.go:1826-1832`, `kilo/session.go:926-932`) collapses whitespace, so step 5's source
  test requires changing it. It is called from files outside P1's list, and six of those calls
  carry agent error or notice text that would lose its lines:
  `opencode/lifecycle.go:134` and `:175`, `opencode/resync.go:268`, `kilo/lifecycle.go:134` and
  `:175`, `kilo/resync.go:266`.
- **Resolution:** `internal/provider/opencode/lifecycle.go`, `internal/provider/opencode/resync.go`,
  `internal/provider/kilo/lifecycle.go` and `internal/provider/kilo/resync.go` join P1's files. Only
  those six sites change, to the line-preserving clip. `clip` itself becomes the one-line form for
  metadata (ids, titles, branch names), cut on a rune boundary where today it cuts on a byte.
- **Not taken:** splitting P1 into P1 and a P1b with the same end state.
- **The MADR is unchanged:** no decision or asserted fact moved.

### P1, 2026-10-21: complete (staged for the owner's commit)

**Step 1, failing first.** `TestToolTextKeepsItsLayout` (`acpagent/tooltext_test.go`) sends five
layouts, as both `tool_call` and `tool_call_update`: a directory listing, an aligned table, a Go
stack trace, an indented first line and ANSI-coloured `go test` lines. They are written by hand,
because grok's committed captures (`grok/testdata/wire/1.0.13`, `1.0.50`) hold no tool frames. On
the code before the change, all ten cases failed. One example:

```text
Text = "total 16 drwxr-xr-x 4 user staff 128 Oct 21 10:00 . drwxr-xr-x 9 user staff 288 Oct 21 09:58 .. -rw-r--r-- 1 user staff 1024 Oct 21 10:00 main.go"
want  "total 16\ndrwxr-xr-x  4 user  staff   128 Oct 21 10:00 .\ndrwxr-xr-x  9 user  staff   288 Oct 21 09:58 ..\n-rw-r--r--  1 user  staff  1024 Oct 21 10:00 main.go"
```

**Step 5, failing first.** `TestNoAdapterCollapsesWhitespace` (`output/source_test.go`) was written
before any adapter change. It failed on exactly the three sites the step names:

```text
collapses whitespace: ../acpagent/session.go:2835
collapses whitespace: ../kilo/session.go:927
collapses whitespace: ../opencode/http.go:1827
```

**Step 2.** `internal/provider/output`:

* `MaxToolText = 8000`, carrying the MADR 0034 D2 rationale from the deleted constants;
* `Truncated`;
* `Clip`, which cuts on the last line break at or before the bound. When that break would discard
  more than half the budget, it cuts on a rune boundary instead.
* `Line`, the first non-empty line, rune-safe, with `…` when cut or when more lines followed.

`TestClip` and `TestLine` cover empty input, an exact fit, inner spacing, a break at the bound, an
early break, one long line, a multi-byte rune at the boundary and no limit.

**Step 3, ACP.**

* `summarizeToolContent` is now `output.Clip` over a new `toolText`: content blocks joined by a
  blank line, only leading blank lines and trailing whitespace removed.
* Tool calls and updates use `output.MaxToolText`; the 400 and 600 caps are gone.
* The permission sheet keeps its 400 cap, now line-preserving.
* The one-line auto-approval audit row (`maxApprovalDetail`, 120) uses `output.Line`.
* The diff and terminal placeholders and the raw-JSON fallback are unchanged, as the step says.

**Step 4, opencode and kilo (with Deviation 1).**

* `clipBlock` and `maxToolOutputChars` are deleted from both.
* `clip` is now `output.Line`, for metadata.
* A new `clipText` (`output.Clip`) takes tool output, tool errors, unclassified errors, the
  sub-agent error notice, the retry notice, workspace match text and `shortJSON`.
* **Changed test expectations: none.** Two tests only renamed what they call:
  * `http_delta_test.go`: `clipBlock` → `clipText`, `maxToolOutputChars` → `output.MaxToolText`;
    the test is renamed `TestToolOutputVisibilityAndClipText`;
  * `parts_test.go`: `maxToolOutputChars` → `output.MaxToolText`.

  Every existing assertion held unchanged.

**Verification.**

* `go test -race -count=1 ./...`: exit 0, every package `ok`, including the grok, opencode, kilo
  and codex wire-replay suites.
* `make pre-add-check` on the 15 changed and new Go files: "15 file(s) clean (gofmt, golint,
  govulncheck)".
* `make lint`: exit 0. `make staticcheck`: exit 0 for linux, darwin and windows.
* `make ci-windows` was not run: P1 changes no process, path or file handling.
* No provider quota was spent.

**Found, not in P1's scope.** Two whitespace collapses remain outside `internal/provider`:

* `internal/auth/store.go:49` normalizes a paired device's name. It is not agent output.
* `internal/agenterr/agenterr.go:725` flattens error text before classifying it. Whether a
  classified message keeps the agent's lines is a question for P5, where engine answers pass
  through; P1's gate covers `internal/provider` only, as written.
