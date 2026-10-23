---
status: in-progress
date: 2026-10-22
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

P1 was committed by the owner after the records commit `e86d9b5c`.

### P2 Deviation 1, 2026-10-21 — the design pinned, and the file list amended. The owner decided: "Approve amended list"

- **Found** at P2's start, before any code: the step list did not say where the full text lives
  while the phone gets a bounded copy, and the file list missed the files that decides.
- **The design:**
  - **Two bounds** in `internal/provider/output`. `MaxToolText` becomes what an adapter hands the
    daemon, 256 KiB: a fetched text must fit one 1 MiB WebSocket message (`maxWSMessageBytes`)
    even if JSON escaping quadruples it. A new `MaxToolShown`, 8,000, is what an event carries.
    opencode's and kilo's adapter code is unchanged, because it already names `MaxToolText`.
  - **The manager clips centrally.** In `pump`, before a tool event enters the history ring, the
    manager stores its full text and diffs, clips them to `MaxToolShown`, and sets `truncated`.
    That makes the bound "one shared function" (MADR D11).
  - **The store** keeps the full text and diffs per session, keyed by tool-call id, at most
    8 MiB per session with the oldest evicted. It lives on the session's entry, so it goes when
    the session does.
  - **`diffs` is a list** (`path`, `old_text`, `new_text`), because one ACP tool call can carry
    several diff blocks. The PLAN's step 1 said `diff`.
  - **Terminal content** replaces the `terminal <id>` placeholder with the output Magic already
    buffers for that terminal (`acpagent/terminal.go`, `limitedBuffer.Snapshot`).
- **Files added to P2:** `internal/provider/output/output.go` and `output_test.go`;
  `internal/session/tooloutput.go` and `tooloutput_test.go` (new);
  `internal/provider/opencode/http_delta_test.go` and `parts_test.go`, which pin today's 8,000
  adapter cap; a new test file in `internal/ws`; and on the phone `lib/data/chat/chat_models.dart`,
  `lib/data/chat/transcript_reducer.dart` and a new Flutter test.
- **Not taken:** moving diffs and terminal content to a separate P2b.
- **The MADR is unchanged:** D11 already says the text is kept whole for the phone to fetch.

### P2 Deviation 2, 2026-10-21 — `retention.go` counts diffs. The owner decided: "Add retention.go"

- **Found** while adding `Diffs` to the event: `internal/event/retention.go`'s `Bytes` sizes every
  owned string and slice for the history ring's budget, and warns that "adding a field the budget
  does not count is how a budget silently stops bounding anything".
- **Resolution:** `Bytes` counts each diff's struct header, path, old text and new text.
  `internal/event/retention.go` and its test file, for a new `TestBytesCountsToolDiffs`, join P2's
  files.
- **Not taken:** stopping P2. No other route keeps the budget honest.

### P2 Deviation 3, 2026-10-21 — three files for the request. The owner decided: "Approve all three"

- **Found** by tracing the existing `session.diagnostics` request end to end:
  - every async request needs a deadline in `internal/protocol/op_timeouts.json`, enforced by
    `internal/ws/op_timeout_test.go` and `apps/mobile/test/op_timeout_ladder_test.dart`;
  - requests are documented in `docs/guides/protocol-v1.md`. `protocol-v2.md`, which the step names,
    covers negotiation and transport only;
  - tool cards (`chat_bubble.dart`) reach neither the session id nor the client. The pane that
    builds them, `transcript_pane.dart`, has both, and already passes `onUserAction` down.
- **Resolution:**
  - `session.tool_output` takes 30 s in `op_timeouts.json`, like `session.diagnostics`;
  - it is documented in `protocol-v1.md`, which replaces `protocol-v2.md` in P2's files;
  - `transcript_pane.dart` passes a fetch callback to the tool card, one optional parameter.
- **Not taken:** stopping P2.

### P2 Deviation 4, 2026-10-22 — the error code is registered. The owner decided: "Add errors.go"

- **Found** by `TestWSErrorCodesAreRegistered`: "unregistered error code tool_output_not_found at
  server.go:3515:44 — add it to protocol.ErrorCodes() and document it in
  docs/guides/protocol-v1.md". No registered code means "this tool has nothing to fetch";
  `session_not_live` would mislead, because the session is live when an output was evicted.
- **Resolution:** `internal/protocol/errors.go` declares `ErrToolOutputNotFound` and lists it in
  `ErrorCodes()`. The handler uses the constant, and protocol-v1.md documents it.
- **Not taken:** stopping P2.

### P2, 2026-10-22: complete (staged for the owner's commit)

**What changed.**

* **Bounds.** `output.MaxToolText` is 256 KiB, what an adapter hands the daemon. `MaxToolShown`,
  8,000, is what an event carries. `TestBoundsLeaveRoomToFetch` pins that what an event shows is
  below what the daemon keeps, and that the kept text fits 1 MiB at four bytes of JSON per byte.
* **The event.** `Truncated` and `Diffs []ToolDiff` (`path`, `old_text`, `new_text`).
  `retention.go` counts diffs (Deviation 2).
* **The manager.** `cutToolEventLocked` runs in `pump` before `appendHistoryLocked`. It clips text
  with `output.Clip` and each diff side only when it is over the bound, so a file's text is never
  trimmed. When it cuts, it stores the whole and sets `Truncated`. An event carrying new content
  that needs no cut drops a stale stored output; a status-only update leaves it.
  `Manager.ToolOutput` authorizes the device without claiming the session.
* **The store** (`internal/session/tooloutput.go`): 8 MiB per session, oldest tool evicted first.
  The newest output is never evicted, even alone over budget.
* **ACP.** Tool events fill `Diffs` from diff blocks, and a terminal block carries that terminal's
  buffered output (`terminalOutput`, through the package's existing `get` and `Snapshot`), falling
  back to `terminal <id>`. `terminal.go` is unchanged.
* **The request.** `session.tool_output` → `session.tool_output_result`, 30 s in
  `op_timeouts.json`, error `tool_output_not_found` (Deviations 3 and 4), documented in
  `protocol-v1.md` with the event's `text`, `diffs` and `truncated` fields.
* **The phone.**
  * `SessionEvent` parses `truncated` and `diffs`. `ToolDiff` and `ToolOutput` are new.
  * `ChatItem` carries and caches `toolTruncated` and `diffs`, which the reducer passes through.
  * `McremoteClient.toolOutput` sends the request.
  * A cut tool card shows "Show all", which opens a sheet with the whole output and a copy button.
    The pane passes the fetcher down.
  * Until P3, diffs show as each new text under its path.
* **Also fixed, in the reducer and the tile:** both trimmed a tool's text, which removed the first
  line's indentation P1 now carries. They now remove only leading blank lines and trailing
  whitespace.

**Failing first, each seen before its fix:**

| Test | Failure before the fix |
| :--- | :--- |
| `TestBytesCountsToolDiffs` (counting removed) | `diffs add 0 bytes, want 124` |
| `TestToolOutputsEvictOldestFirst`, `…KeepsAnOversizedNewest` (store stubbed) | `a = 0 bytes, false; want it kept whole`; `the newest output evicted itself` |
| `TestManagerCutsToolEventsAndKeepsTheWhole` | `long event: truncated=false, 17000 bytes; want it cut to 8000` |
| `TestToolDiffsAndTerminalOutputPassThrough` | `Diffs = []`; `Text = "diff /src/a.go\n\ndiff /src/new.go\n\nterminal term-1"` |
| `TestWSSessionToolOutput` (route removed) | `want session.tool_output_result, got error: … unknown_type` |
| opencode's two cap tests | `long output missing truncation marker`, once the adapter bound moved to 256 KiB |
| Flutter "Show all" | `Found 0 widgets with text "Show all"` |
| Flutter reducer and cache (fields removed) | `Expected: true, Actual: false` |

The two opencode tests keep their assertions, the adapter's cap and its marker. Their inputs now
exceed the new 256 KiB bound. `TestWSErrorCodesAreRegistered` caught the unregistered code
(Deviation 4).

**Verification.**

* `go test -race -count=1 ./...`: no failures.
* `make pre-add-check` on the 16 changed Go files: "16 file(s) clean (gofmt, golint, govulncheck)".
* `make lint` and `make staticcheck` (linux, darwin, windows): exit 0.
* `make preflight`: "No issues found!", "All tests passed!" (1,484 tests), formatting 0 changed.
* `protocol-v1.md`: markdownlint 0 issues.
* `make ci-windows` was not run: P2 changes no process, path or file handling. No provider quota
  was spent.

**Not done in P2.** Diffs render as diffs in P3. The 256 KiB adapter bound and 8 MiB store are
constants until 0181's configurable message size lands.

P2 was committed by the owner.

### P3 Deviation 1, 2026-10-22 — real captures first, and `chat_screen.dart`. The owner decided: "Live captures first" and "Add chat_screen.dart"

- **Found** at P3's start, before any code:
  - **Step 1 had no input.** A read-only scan of the five committed captures
    (`internal/provider/{grok,opencode,kilo,codex}/testdata/wire/*/frames.jsonl`) found no
    multi-line or escape-bearing string of 20 characters or more: no ANSI, table, box drawing,
    JSON, diff or Markdown block. They are short smoke sessions. The MADR amendment's Confirmation
    asks for fixtures "each from a real capture".
  - **One import site.** `chat_bubble.dart` is `part of 'chat_screen.dart'`, so the new renderer
    libraries must be imported in `chat_screen.dart`, which was not on the list.
- **Resolution, a new step P3.0 before P3's code:**
  1. **One live test per provider,** `internal/provider/{grok,opencode,kilo,codex}/live_rawoutput_test.go`,
     under each package's existing live build tag. It does nothing unless
     `MCREMOTE_RAW_OUTPUT_DIR` is set, so routine `make live-*` runs spend nothing on it.
  2. **What each run does.** It starts the provider with auto-approve in a temporary workspace: a
     small git repository with one modified file, a JSON file, and a script that prints coloured
     and box-drawn lines. It sends one prompt asking the agent to run, one at a time, `ls -la`,
     `git diff`, `cat data.json`, the script, and a command that prints an aligned table, and then
     to answer with a short Markdown summary. It writes every tool event's text and diffs, and
     the assistant's reply, as Magic received them, to `$MCREMOTE_RAW_OUTPUT_DIR/<provider>/`.
  3. **The script reads those dumps,** not the wire frames: `scripts/extract-raw-output-fixtures.py`
     copies each candidate to `apps/mobile/test/fixtures/raw_output/<provider>/` with a sidecar
     naming its expected kind. Each candidate is read for identifiers before it is staged, as
     wirecap's own documentation requires.
  4. **Quota.** One prompt per provider, four runs, each only with the owner's approval for that
     run. A provider that is not installed or signed in is skipped and recorded.
  5. `apps/mobile/lib/features/chat/chat_screen.dart` gains import lines for the new libraries,
     and nothing else.
- **Not taken:** fixtures written by hand, with or without a later live step.
- **The MADR is unchanged:** its "each from a real capture" now holds as written.
- **Also found, not P3's:** the committed codex capture
  (`internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`, line 2) carries
  `"serverName":"<host>"`, a real machine name in a public repository (redacted here).
  It is reported to the owner; nothing here changes it.

### P3.0, 2026-10-22: the live captures

The owner approved four runs, one prompt each: "grok, opencode, kilo, codex".

| Provider | Engine on this host | Result |
| :--- | :--- | :--- |
| grok | (pin unchanged) | PASS, 6 samples, 17.6 s |
| opencode | v2.0.32, pin 1.18.26 | **FAIL before any prompt**: `POST /session` answered HTTP 405, and `GET /provider` answered HTML. No quota was spent. |
| kilo | 7.8.12, pin 7.5.6 | PASS, 6 samples, 54.2 s; Magic warned of the pin |
| codex | 0.163.0, pin 0.152.1 | PASS, 6 samples, 22.6 s; Magic warned of the pin |

`scripts/extract-raw-output-fixtures.py` wrote 18 fixtures, 6 per provider, after redacting the
account name, home directory and host name. Every fixture was read in full before staging. A scan
for the account name, home paths, host names and the e-mail domain found none. The expected kinds
were written by hand after reading. The one judgment call is codex's `cat data.json`, expected as
`terminal`, because Magic's appended exit-code line makes it invalid JSON (below).

**Found by the captures, none of them P3's to fix:**

1. **opencode 2.x breaks Magic's opencode provider on this host.** The installed engine is a major
   version past the pin, and session creation fails with HTTP 405. Phone sessions on opencode are
   likely failing here today.
2. **Magic writes text into codex's tool output.** The codex adapter appends
   `Exit code N · M ms` to a command's output (`internal/provider/codex/items.go:299-311`). That is
   composed text inside agent output, which D12 retires. It belongs to P5, or to its own step.
3. **The committed codex capture names this machine.** Its `serverName` value is this host's name
   (`hostname -s`), and it has been in a pushed fixture since the capture was committed.
4. **kilo and codex run newer than their pins on this host** (7.8.12 against 7.5.6, and 0.163.0
   against 0.152.1). Magic warns and carries on, as designed.

Also seen: grok colours `ls` output with ANSI escapes, kilo's reply leaks a `</think>` tag, and grok
joins its between-tool sentences without a separator. All three are kept in the fixtures as
received.

### P3, 2026-10-22: complete (staged for the owner's commit)

**What changed.**

* **`lib/data/chat/raw_output.dart`.** `classifyRawOutput` tries, in order:
  * `diff`: a hunk header and a file header;
  * `json`: the whole text parses as an object or array;
  * terminal, strong signs: an escape or box drawing;
  * `markdown`;
  * terminal, weak signs: two or more lines with aligned columns, or most of three or more lines
    indented;
  * `plain`.

  Trying the weak terminal signs after Markdown's refines the step's order, so a Markdown reply
  with double spaces stays Markdown. A test pins it. The file also has `prettyJson`, and
  `unifiedDiff`, a longest-common-subsequence line diff with three lines of context. It is coarse
  above 4,000,000 compared line pairs.
* **`lib/features/chat/ansi_text.dart`.**
  * `ansiSpan` turns SGR codes into spans: bold, dim, italic, underline, the 16 colours,
    256-colour and true colour.
  * `stripAnsi` removes CSI, OSC and two-character escapes.
  * `AnsiPalette.of` draws the colours from the theme, with red as its error colour.
* **`lib/features/chat/raw_output_view.dart`.** `RawOutputView` renders each kind:
  * terminal text in monospace, with colours and sideways scroll;
  * JSON pretty-printed;
  * diff lines marked by colour and tint;
  * Markdown through `MarkdownBody`;
  * plain as it came.

  Text over the limit collapses behind "Show more". A renderer that throws falls back to plain
  monospace.
* **`chat_bubble.dart`.**
  * Tool card detail and the "Show all" sheet render through `RawOutputView`.
  * Diffs are built as unified diffs from their old and new text.
  * A finished assistant reply that classifies as terminal or JSON renders through it as well. A
    streaming reply stays Markdown.
* **`chat_screen.dart`:** two import lines, and nothing else (Deviation 1).

**Failing first, each seen before its fix:**

| Test | Failure before the fix |
| :--- | :--- |
| fixture table (stub classifier) | all 18 fixtures: `got plain, want …` |
| planted misclassification | `got plain, want diff`, then `got json, want diff` once classifying |
| Markdown with double spaces | `Expected: markdown, Actual: plain` |
| `unifiedDiff`, both cases (output deliberately broken) | both failed; restored |
| ANSI, three cases (stub) | escapes left in place; no styles |
| `RawOutputView`, seven cases (stub) | all seven failed |

The first `unifiedDiff` run failed on the test, not the code: the expectation put added lines
before removed ones, where `git diff` puts removed lines first. The expectation was corrected to
git's order, and a deliberate break of the code then failed both cases.

**Verification.**

* `make preflight`: "No issues found!", "All tests passed!" (1,501, 17 new), formatting 0 changed.
* `make pre-add-check` on the four capture tests: "4 file(s) clean".
* `go test ./internal/provider/...`: 13 packages ok, unchanged.
* `go vet` under each live tag compiles each capture test.

**Not done in P3.** Findings 1–4 above. The renderer covers the kinds the captures showed; a new
kind, or a misclassification, becomes a fixture through the same capture test and script.

P3 was committed by the owner as `ea20b47b`.

### P4 Deviation 1, 2026-10-22 — opencode and kilo report their version. The owner decided: "Add httpagent/provider.go"

**Found, before any P4 edit.** A silent mark is keyed by the agent's version, so an upgrade clears
it (D10; P4a step 2). Where that version comes from:

* the acpagent providers (grok, gobble) and codex expose `EngineVersion()` on the provider
  (`acpagent/version.go:60`, `codex/version.go:83`);
* opencode and kilo keep it on their HTTP dialect (`opencode/http.go:727`, `kilo/version.go:55`),
  and `httpagent.Provider` does not expose it.

Without the provider exposing it, their marks would carry an empty version that no upgrade
clears.

**Decision.** `internal/provider/httpagent/provider.go` joins P4's files. It gains
`EngineVersion()`, which returns the dialect's when the dialect has one, and "" otherwise.

**Also settled while reading, with no file outside the list.** The marks file sits beside the
session store, in `data_dir`. The store's root is `data_dir/sessions` (`session/store.go:80-86`),
so the manager derives the directory, and `doctor` reads `cfg.DataDir`. `daemon.go` does not
change.

### P4a, 2026-10-22: detection, with today's tables unchanged

**What was done.**

1. **`internal/session/silent.go` (new).**
   - **`SilentMark`:** provider, version, host, command, and when it was marked.
   - **`LoadSilentMarks(dataDir)`** for `doctor`, and **`SilentMarksPath`**.
   - **`silentMarks`,** the daemon's live set. It is loaded at start and written with
     `fsutil.WriteFileAtomic` (mode `0600`) to `data_dir/silent-commands.json`. `commands` and
     `add` match on provider, version and host, so a mark from another version or host never
     applies.
   - **`armSilentWatch` and `noteSilent`** (manager methods), and **`breaksSilence`**.
   - **The host** is `os.Hostname()`, through a variable the tests replace.
2. **`manager.go`.**
   - `Prompt` notes the agent command a prompt carries: the native name after a translation, or
     the agent's own command. It arms the watch just before the turn starts.
   - The pump follows the watched turn. A `turn_complete` with nothing but bookkeeping before it,
     and not cancelled, marks the command. An `error` ends the watch unmarked.
   - The marking runs after the delivery lock is released. It writes the file, sends the notice,
     and re-advertises.
   - `clearPromptStart` drops the watch when the prompt fails.
3. **`command.go`.**
   - **`SessionState.Silent`.** A native mapping to a silent command is unavailable, whether
     declared, the spec's default, or advertised and undeclared.
   - **A new frozen reason,** `ReasonSilent`: "it produced nothing over the remote connection".
   - **`commands.go`** fills `Silent` for the provider's current version.
4. **`httpagent/provider.go`.** `EngineVersion()` passes the dialect's through (deviation 1).
5. **`doctor`** gains "silent agent commands (each applies to its host and agent version only)".
   It lists every mark on disk, sorted, or "none", or "could not be read".
6. **The notice** is "“/<name>” produced nothing over the remote connection. It is marked
   unavailable here for this agent version."

**Three choices made while building it, each narrower than or the same as the step's text.**

- **What breaks the silence is everything but bookkeeping.** Bookkeeping means the status, the
  echoed prompt, `usage_update`, `available_commands`, `remote_commands`,
  `session_capabilities` and `diagnostics_changed`.
  - The step listed assistant text, thoughts, tools, plans, notices and errors.
  - The wider rule marks fewer commands. A command whose only effect is a mode switch, a config
    change, a title or a goal is not silent; `TestCommandsWithOutputAreNotMarked` covers the mode
    switch.
- **Only a command sent to an idle session is judged.** A prompt that queues behind a running
  turn cannot be told apart from that turn's output, so it is not judged at all.
- **Agent commands outside the canonical set get the notice and the `doctor` line, but no
  `remote_commands` change.** `remote_commands` lists only the canonical vocabulary, so those
  commands have no entry to mark. P4b's resolution order is where a mark changes what is sent.

**Tests, each seen failing first.**

- `internal/session/silent_test.go`, with a scripted provider: `/quiet` and `/loop` end with
  bookkeeping only, `/loud` replies, and `/modeonly` only switches the mode.
  - `TestSilentCommandIsMarked`: the mark is `sil/1.0/host-a/quiet`, and the notice is sent.
  - `TestCommandsWithOutputAreNotMarked`: neither `/loud` nor `/modeonly` is marked.
  - `TestSilentMarkMakesTheCanonicalCommandUnavailable`: `/loop` becomes unavailable with
    `ReasonSilent`, and is available again once the agent's version changes.
  - `TestMarkFromAnotherHostIsIgnored`: another host's mark for `/loop` does not apply.
- `command_test.go`, `TestSilentNativeCommandIsUnavailable`: declared, default and advertised
  native mappings.
- `doctor_test.go`, `TestRenderSilentDoctor`.

**Fail-first, on scratch copies of the module (`p4a/plants.py`), final run.**

```text
== control: no plant: PASS (as expected)
== control: command and doctor: PASS (as expected)
== the pump never marks: FAIL (as expected)
   silent_test.go:204: timed out waiting for silent notice
== every event is bookkeeping: FAIL (as expected)
   silent_test.go:225: a command with output was reported silent
== marks ignore the version: FAIL (as expected)
   silent_test.go:248: timed out waiting for /loop available on a new version
== marks ignore the host: FAIL (as expected)
   silent_test.go:267: /loop was made unavailable by another host's mark: {… Available:false Reason:it produced nothing over the remote connection}
== resolution ignores marks: FAIL (as expected)
   command_test.go:352: /context with /session-info silent: available=true reason="", want unavailable, "it produced nothing over the remote connection"
== doctor drops the version: FAIL (as expected)
   doctor_test.go:217: marks missing "1.0.62":
```

- **One plant was redone.** Its first form, removing the marking call, left two variables unused,
  so the build failed rather than the test. It now keeps the variables read, and the test fails on
  the missing notice.
- **One case of the resolution plant passed.** It removed the silent check from `available` only,
  and the "advertised, undeclared" case has a check of its own in `Resolve`, so that case still
  passed. The two cases that go through `available` failed.

**Verification, 2026-10-22, go1.27.3.**

```text
macOS (darwin/arm64):
  vet-darwin, vet-linux, vet-windows: exit 0
  make test: exit 0; 43 ok, 0 FAIL lines
  make race: exit 0; 43 ok, 0 FAIL lines
  make lint: exit 0; make staticcheck: exit 0
  make pre-add-check FILES="(the nine P4a Go files)": 9 file(s) clean (gofmt, golint, govulncheck)
Windows test host (the phase writes a file), a fresh clone of d1f96902 with the P4a diff, new files included:
  make ci-windows: exit 0 in 64s; 44 ok, 0 FAIL lines
```

- **No Dart file changed.** The phone already renders `remote_commands`' `available` and
  `reason`.
- **P4a is staged as a commit of its own.** P4b changes resolution, and its acceptance needs a
  `make live-grok` run, which waits for the owner's quota word. Keeping them apart lets each be
  reverted alone.

**Not done in P4a.** P4b: the resolution order, the resolver tests over the five providers'
tables, and grok's rows, which wait for a live run.

P4a was committed by the owner as `214cc35c`. The owner approved P4b's `make live-grok` run on
2026-10-22 ("approved for the live run").

### P4b Deviation 1, 2026-10-22 — grok's `/compact` compacts, so Magic shows it. The owner decided: "Show grok's compaction"

**Found, before any P4b row change, in grok's source.** The owner pointed to the current sources
in `~/gitrepos` (grok's is `grok-build`, at `2bdd1d6a`, synced 2026-09-29). Paths below are under
its `crates/codegen/xai-grok-shell/src/`.

| Command | What grok does over ACP | What Magic saw |
| :--- | :--- | :--- |
| `/context` | ends the turn with nothing (`session/acp_session_impl/slash_exec.rs:74`, `ContextInfo => ok_end_turn(0, None)`) | nothing: silent, and the `context` → `session-info` row stays |
| `/compact` | **compacts** (`slash_exec.rs:16-19`, `run_compact`), then sends `auto_compact_completed` alone, with `summary_preview: None` (`session/compaction.rs:598-641`). A failure returns the prompt's error instead. The automatic path sends `auto_compact_started` first, then `_completed` or `_failed` (`compaction.rs:2327-2384`), and `_cancelled` only for an automatic compaction (`compaction.rs:644-652`). Shapes: `extensions/notification.rs:526-605`. All go out on `x.ai/session_notification` (`session/acp_session_impl/updates.rs:1060-1066`). | nothing: `HandleXAISessionNotification` returns at `u.SubagentID == ""` for every variant it does not name (`acpagent/subagents.go`) |
| `/feedback` | bare, answers `Usage: /feedback <text>` as slash-command output and submits nothing (`slash_exec.rs:922-926`) | the reply: not silent, so the caveat's example is stale |
| `/always-approve` | toggles auto-approve; its `YoloToggled` event reaches the TUI and telemetry, not ACP | nothing: silent, correctly |

*Corrected while implementing, the same day.* This entry first named `_x.ai/session/update` and
`HandleXAISessionUpdate`. That is the session/load replay channel. grok's live updates use
`_x.ai/session_notification` (`updates.rs:1060-1066`), so the mapping is in that handler.
Replayed compactions on `_x.ai/session/update` are still declined, which is not changed here.

As written, step 3 would let P4a mark `/compact` silent and then delete its row, hiding a command
that works.

**Decision.** P4b gains these steps, before step 3's deletions:

1. **grok's compaction becomes Magic's compaction event.** The four `auto_compact_*` updates
   become `TypeCompaction` events (MADR 0190 D1's shape).
   - `started` opens a compaction under an ID of Magic's, since grok sends none.
   - `completed` closes it, with `summary_preview` as its summary, grok's own text. A
     `completed` with none open, which is what `/compact` sends, is a whole compaction of its own.
   - `failed` closes it with grok's `error`, and `cancelled` with status cancelled.
   - Nothing of Magic's own wording is added (D11).
2. **grok's `compact` row becomes `KindNative`, `compact`.** `/compact` then forwards to grok's
   own command (D9), as `goal` and `loop` do. A row is kept because the conformance test requires
   every canonical command to be declared.
3. **The live run** checks that `/compact` shows its compaction and is not marked, that
   `/always-approve` is marked, and that `/feedback` is not. The caveat is then rewritten, or
   deleted, to match what the run shows.

**Files added to P4.**

- `internal/provider/acpagent/subagents.go`, for the dispatch (first written as `xaiusage.go`;
  see the correction above), and `session.go` for the fold's two fields.
- A new `internal/provider/acpagent/xaicompaction.go` and its test.
- A new live test in `internal/provider/grok/`, `live_silent_test.go`, under `live_grok`.
- `internal/session/live_command_test.go` (`live_grok`). It asserted that `/compact` is refused.

### P4b, 2026-10-22: the agent's own command first, and grok's compaction shown

**What was built.**

- **D9's order** is in `internal/command/command.go` `Resolve`. Daemon-only commands (`help`,
  `new`, `sessions`, `clear`) return `KindDaemon` before anything else is read. A `KindOp` row
  yields to `KindNative` with the canonical name when the agent advertises that name and it is
  not marked silent. The undeclared-name comment no longer cites grok's `/compact`.
- **grok's compaction** (deviation 1). `HandleXAISessionNotification` sends the four
  `auto_compact_*` variants to `onXAICompaction` (`acpagent/xaicompaction.go`), which folds them
  through the session's existing `compactionFold` and emits `TypeCompaction`. IDs are `xai-N`.
  The fold needs no lock: the SDK runs every notification, extension ones included, through one
  ordered queue (`acp-go-sdk2@v1.20.0 connection.go:1399-1406`).
- **grok's table.** `compact` is now `{KindNative, "compact"}`. No other grok row only said
  "returns nothing here", so step 3 deleted nothing. The remaining `KindNone` rows (`diff`,
  `undo`, `redo`, and others) say grok has no such operation over ACP. They are not silence
  claims.
- **grok's caveat** is kept as written ("Some of grok's own commands only work in its terminal
  UI and return nothing here."), because the run below shows `/always-approve` doing exactly
  that. Its code comment no longer names `/feedback`, which answers.

**Tests, each seen failing first.** A scratch script copied the tree (without `.git` and
`apps`), broke one thing per plant, ran the guarding test, and restored. All ten failed as
expected, and the restored copy passed:

| Plant | Failed |
| :--- | :--- |
| D9 branch removed | `TestAgentCommandComesBeforeATableOp` |
| D9 ignores silent marks | `TestAgentCommandComesBeforeATableOp` |
| D9 fires on any agent command | `TestNonCanonicalAgentCommandsChangeNothing` |
| daemon commands forward | `TestDaemonCommandsNeverForward` |
| grok `compact` row back to `KindNone` | `TestGrokCompactIsGroksOwn` |
| grok `diff` note lost | `TestAgentAdvertisesAndHelp` |
| `auto_compact_*` dispatch dropped | `TestXAIManualCompactShowsACompaction` |
| compaction never closed | `TestXAICompactionsAreNumbered` |
| `summary_preview` ignored | `TestXAIAutoCompactFoldsIntoOneCompaction/completed` |
| `error` ignored | `TestXAIAutoCompactFoldsIntoOneCompaction/failed` |

- `internal/command/resolve_order_test.go` (new) runs the D9 assertions over all five
  providers' tables: 43 `KindOp` rows, each checked advertised, advertised-but-silent, and
  not advertised. It also checks daemon-only commands over the five tables and a nil one, and
  that non-canonical agent commands leave `ResolveAll` unchanged on every table (N8).
- `internal/provider/acpagent/xaicompaction_test.go` (new) builds frames shaped like grok's
  serde output.
- `internal/session/commands_internal_test.go` asserted grok's old `/compact` note as "the
  provider's own words". It now asserts grok's `/diff` note, which is the same check on a row
  that still carries one.

**The live run** (`make live-grok`, approved by the owner, grok 1.0.62): 104 PASS, 1 FAIL,
1 SKIP (`TestLiveRawOutputSamples`, which needs `MCREMOTE_RAW_OUTPUT_DIR`).

`TestLiveGrokSilentCommandsThroughTheDaemon` passed, through `session.Manager` with a real store:

```text
Reply with the single word: ready: assistant "ready"
/compact: compaction id="xai-1" status="completed" summary="" error=""
/always-approve: notice "“/always-approve” produced nothing over the remote connection. It is marked unavailable here for this agent version."
/feedback: assistant "Usage: /feedback <text>"
mark: provider=grok version=1.0.62 command=always-approve
```

So the compaction reached the phone before the turn ended, and `/compact` was not marked. The
ordering was an assumption from grok's source (the notification is queued before `run_compact`
returns), and this run is its evidence.

The one FAIL was `TestLiveGrokCommandTableMatchesReality`
(`internal/provider/grok/live_command_test.go:63`), which pinned `compact` as `KindNone`. It
pins grok's command table, which is P4 scope, so it was updated. `compact` joined the commands
the table forwards to, so the test checks that grok still advertises it, and it asserts the row
is `{KindNative, "compact"}`. Its `/compact` prompt was removed, because the silent-commands
test above checks the same command with a turn before it. **That updated test has not been
re-run live**, and neither has `internal/session/live_command_test.go`, which `make live-grok`
does not cover. Each costs one more grok session.

**Gates.** `go vet ./...` for darwin/arm64, linux/amd64 and windows/amd64, `make test`,
`make race`, `make lint` and `make staticcheck`: each exit 0 with no FAIL lines. `go vet
-tags live_grok` is clean for `internal/provider/grok` and `internal/session`. `make
pre-add-check` is clean on all eleven Go files.

**Not done in P4b.**

- Replayed compactions. On session/load, grok replays them on `_x.ai/session/update`, and
  `HandleXAISessionUpdate` still declines them.
- `docs/guides/protocol-v1.md:2279` still uses the old grok `/compact` reason in an example
  payload. The guide is outside P4's files.
- The phone's composer, which P4's file list names. Nothing in P4b needed it: a mark reaches the
  phone as an unavailable `remote_commands` entry, which it already renders.
