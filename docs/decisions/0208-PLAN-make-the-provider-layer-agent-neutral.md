---
status: in-progress
date: 2026-11-04
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

### P4c — The silent-command test's fake no longer races its close (owner's decision, 2026-10-28; approved and done)

*Added 2026-10-28 from
[0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md)
C0 Deviation 2. The owner chose "Fix under 0208, first (Recommended)": its own commit, before
C0's.*

**Why.** `TestSilentCommandIsMarked` races about once in 300 `-race` runs on the Linux test host,
at `f2e5aa43` and after. P4a's fake session (`internal/session/silent_test.go`) sends a turn's
events from a goroutine without its mutex (`:73-86`), ending with an `idle` status. The test waits
only for the turn's `turn_complete`, so the cleanup's `Close` (`:90-98`) can close the channel
while that last send is still running. The race detector reports `closechan` at `:95` against
`chansend` at `:85`. If the close came first, the send would panic.

**Files:** `internal/session/silent_test.go`; this plan.

1. **Reproduce first,** on a scratch copy of `HEAD`: a delay before the last send makes the close
   come first, and the unchanged test must fail.
2. **The fix, test only.**
   * The fake's turn goroutine sends with `select` on a `done` channel.
   * `Close` marks the session closed, closes `done`, waits for any turn goroutine to return, and
     only then closes `events`.
   * `Prompt` refuses to start a turn on a closed session, so nothing is added to the wait once
     `Close` has begun.
   * The events a turn sends, and their order, are unchanged.
3. **Seen to pass where it failed:** step 1's copy with the fix.
4. **Repeated runs:**
   `go test -race -count=300 -run '^TestSilentCommandIsMarked$' ./internal/session/`, on the Linux
   test host, in a scratch copy of `HEAD` with the fix. This also shows whether the one "timed out
   waiting for /quiet turn" seen in C0's 300 runs is a separate fault.
5. **Gates:** `make pre-add-check FILES=internal/session/silent_test.go`, `go test -race
   ./internal/session/` here, `go test -race ./...` on the Linux test host on the same copy,
   `make check-records`, and the guard's redact preview.

### P5 — Engine answers pass through as JSON (D12)

Files, per operation: the provider's `runtime.go` or `usage.go`, `internal/session/commands.go`,
`internal/session/tokencost.go`, and their tests.

One operation per commit, in this order: grok usage and billing (`acpagent/runtime.go`), codex
status and usage (`codex/runtime.go`), kilo usage (`kilo/usage.go`), opencode and kilo session
totals (`httpagent/runtime.go`), then `/context`.
*Restated 2026-11-04 (P5 Deviation 5):* after grok's and codex's commits, kilo `/status`
(`kilo/runtime.go`), kilo `/usage` (`httpagent/runtime.go`), then `/context`. opencode has
neither operation since 0214.

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

1. ~~**Failing first.** A fixture ACP Spec with no operations: a recorded connection across
   `/compact`, `/fork`, `/undo`, `/thinking high`, a phone fork, a phone rename and a classified
   quota error shows `_x.ai/*` frames on today's code. The test asserts none.~~ *Replaced
   2026-11-02 by P6d's step 1 (P6d Deviation 1).*
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

*Amended 2026-11-01 (MADR amendment "a Spec-facing surface, and no provider in `acpagent`").* P6
runs now, ahead of P5's remainder. It is executed as P6a to P6d below. Steps 1 and 2 are P6d,
step 3 is P6a and P6b, and step 4 is P6c. Each sub-phase is one commit, and each needs the owner's
word to start.

#### P6a — `Host`, and every hook takes it

Files: `internal/provider/acpagent/` (new `host.go`; `acpagent.go`, `extensions.go`,
`session.go`), `internal/provider/grok/grok.go`, and their tests.

1. **Failing first.** A Spec in an external test package (`acpagent_test`) registers a vendor
   notification, a `_meta` key and an extension request. Through `Host`, it puts a subagent in the
   panel, posts a notice, ends a turn with a limit card and a retry time, and answers a request.
   Before `Host` exists the test does not compile. With `Host` declared and its methods stubbed,
   each assertion fails.
2. `Host`, the method set the MADR's findings list. `*session` implements it.
   * `ExtensionNotificationHandler`, `MetaHandler`, the model and thinking fallbacks
     (`RawCaller` retired) and `Spec.Ops` take a `Host`.
   * New: `Spec.ExtensionRequests`, `Spec.NewSessionState` with `Host.SpecState()`, and the
     initialize `_meta` hook.
3. grok's handlers are adapted in place to take `Host`, still in `acpagent`, so this commit
   changes signatures, not behaviour.
4. grok's unit, conformance and wire suites pass unchanged.

#### P6b — grok out of `acpagent`

Files: `internal/provider/acpagent/` (`xaiusage.go`, `xaicompaction.go`, `xaiextensions.go`,
`subagents.go`, `extensions.go`, `fork.go`, `rewind.go`, `runtime.go`, `sessioncaps.go`,
`quota.go`, `authselect.go`, `acpagent.go`), `internal/provider/grok/` (new files for the moved
code), and the tests that move with them.

1. The code the MADR's findings list moves to `internal/provider/grok`, on `Host`:
   * the notification handlers and the cost and compaction state (into the Spec's session state);
   * the two incoming requests (into `Spec.ExtensionRequests`);
   * the seven operations and `ConfirmLimit` (into `Spec.Ops`);
   * auth selection (a Spec policy, in today's order);
   * the initialize `_meta` decoding.
2. Their tests move with them, unchanged in what they assert.
3. grok's wire fixtures (`testdata/wire/1.0.13`, `1.0.70`) replay with unchanged results.
4. `make live-grok` at acceptance, on the owner's quota word.

#### P6c — `acpagent` names no provider

Files: `internal/provider/acpagent/` (comments, and the tests from 0214 P2), a new source test, and
`internal/provider/opencode/` (the tests that move there).

1. **Failing first.** The source test (MADR D2, as amended) fails on the tree before P6c, listing
   each hit.
2. Comments are rewritten without provider names. 0214 P2's tests use neutral vendor names and a
   fixture under `acpagent/testdata/`. The tests of opencode's recorded frames move to
   `internal/provider/opencode`, for 0214 P4 to build on.

#### P6d — declared capabilities (D1)

This is steps 1 and 2 above, after P6c, so the fixture Spec offers no vendor operation at all.

*Amended 2026-11-02 (P6d Deviation 1).* Files: `internal/provider/provider.go`,
`internal/session/commands.go` and `manager.go`, each adapter's session (`acpagent`, `httpagent`,
`codex`, `fake`), and their tests.

1. **Failing first.** A fixture ACP Spec through `acptest`, with no operations, no config
   options and no fallbacks. Its command table maps `/compact`, `/fork`, `/undo`, `/usage`,
   `/status`, `/thinking` and `/model` to operations, as a Spec author might before writing the
   hooks:
   * `/compact`, `/fork`, `/undo`, `/usage`, `/status` and `/thinking` resolve unavailable;
   * `/model` resolves to the daemon's relaunch, not the agent's in-place switch;
   * running each, a phone fork and a phone rename sends the agent no request after
     `session/new`.
   It fails on the tree before P6d, which reports the operations available.
2. `provider.Capability`, one constant per operation, and `provider.Capabilities`, a set of them.
   `provider.Session` gains `Capabilities()`. `commandContext` reads it. A type assertion stays
   only to call an operation the session declared.
3. Each adapter declares:
   * `acpagent` from `Spec.Ops`, the model and thinking fallbacks, and its current config
     options;
   * `httpagent` and `codex` statically, with codex's fast tier and personality read live as
     today;
   * `fake` everything it implements.
4. `manager.go`: the phone's rename, fork, revert and diff check the declaration before the call.
   A session config event re-resolves the commands.
5. A test per adapter that each declared capability's interface is implemented.

Verification for each sub-phase: the Go rules; `make ci-windows` on the Windows test host for P6b,
because grok's code moves; and `make vet-tags`.

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
   no `cfg.Providers.<Name>` field (74 today). *(65 on `02463a8a`; P8 Deviation 1.)*

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

### P4c, 2026-10-28: the silent-command test's fake no longer races its close

**Step 1, reproduced** on a scratch copy of `HEAD` (`f2e5aa43`; `leftovers/p4c.py` in the session
scratchpad). With a 100 ms delay before the turn's last send, `go test -race -run
'^TestSilentCommandIsMarked$' ./internal/session/` failed: one "WARNING: DATA RACE" with the
recorded frames (`silent_test.go:96`, `:86`, moved one line by the plant), then "panic: send on
closed channel" at `:86`.

**Step 2, the fix,** in `internal/session/silent_test.go` alone:

* `silentSession` gains `done` and `turns`, with a comment citing this phase.
* A turn's goroutine sends through `send`, a `select` on `events` and `done`.
* `Prompt` adds to `turns` under the mutex, and refuses a closed session.
* `Close` marks the session closed and closes `done` under the mutex, waits on `turns`, then
  closes `events`.
* The events a turn sends, and their order, are unchanged.

**Step 3.** The same delay on a copy carrying the fix: exit 0, no race and no panic. The fixed
copy without the delay: exit 0.

**Step 4, the Linux test host,** on a scratch copy of `HEAD` with only this file changed, as the
commit holds it (`leftovers/p4c_linux.py`; the copy was removed and its absence checked):
`go test -race -count=300 -timeout=40m -run '^TestSilentCommandIsMarked$' ./internal/session/`
gave exit 0, "ok … internal/session 21.659s", with no race, failure, panic or timeout line. The
one "timed out waiting for /quiet turn" in the 0207 C0 run did not recur in 300 runs. It is not
pursued further here.

**Step 5, the gates:**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=internal/session/silent_test.go` | "1 file(s) clean (gofmt, golint, govulncheck)" |
| `go test -race -count=1 ./internal/session/`, here | exit 0 |
| `go test -race -count=1 ./...` on the Linux test host, the same copy | exit 0, 44 packages ok |
| `make check-records`; the guard's redact preview | exit 0; "would edit 0 file(s)" |

* **The first Linux full run** failed `TestTrackedFilesUseOnlyPlaceholderHomes`: "no git index …
  the guard cannot read the tracked files". My copy was not a git tree. Rerun after `git init` and
  `git add -A` in the copy: exit 0, as above.

**Files for this commit:** `internal/session/silent_test.go`, this plan.

### P5 Deviation 1, 2026-11-01 — the event's files, and a live capture. The owner decided: "Add the files" and "Capture with no turn"

The owner said "do P5" (tracked as GitHub issue #33). Two gaps were found while naming the phase's
files, before any change.

- **Found, files.** Step 1 adds the structured-result event "in the daemon and the phone
  together", but P5's file list names only each provider's runtime or usage file,
  `internal/session/commands.go`, `internal/session/tokencost.go` and their tests. The shared
  interface returns text (`provider.RuntimeSession`, `internal/provider/provider.go:163-168`), the
  event model has no such event, and the phone has no row for it.
- **Found, capture.** Step 3's fixture needs grok's `_x.ai/session/usage` and `_x.ai/billing`
  responses from a committed capture. None of the three grok captures (1.0.13, 1.0.50, 1.0.70)
  holds either response, by method or by body field.
- **Resolution, files.** The first P5 commit adds `internal/provider/provider.go`,
  `internal/event/event.go`, `docs/guides/protocol-v1.md` (where event fields are documented), and
  on the phone `lib/data/protocol/models.dart`, `lib/data/chat/chat_models.dart`,
  `lib/data/chat/transcript_reducer.dart`, `lib/features/chat/chat_bubble.dart` and their tests.
  The interface changes for every adapter in that commit, so `codex/runtime.go`,
  `httpagent/runtime.go` and `kilo/runtime.go` (already in P5's list, for later commits) change
  signature only: they return their current text unchanged until their own commit.
- **Resolution, capture.** The two existing quota-free live tests
  (`internal/provider/acpagent/live_runtime_test.go`) are run against grok 1.0.70 with no turn and
  with `MCREMOTE_WIRE_CAPTURE_DIR` set to a scratch directory. The two response frames are added
  verbatim to the 1.0.70 capture, redacted as `internal/wirecap` redacts, and checked with the
  disclosure guard's preview. No model tokens are spent; billing makes one authenticated account
  request. The usage body therefore shows zero turns.
- **Not taken:** a separate P5a for the event alone (same files, same end state); a capture after
  one turn (spends model quota).
- **The MADR is unchanged:** no decision or asserted fact moved.

### P5 Deviation 2, 2026-11-01 — `retention.go` classes the event. The owner decided: "Add retention.go"

- **Found** while mapping the event, before any change. Two existing tests bind every event type
  and every string field: `TestClassOfIsExhaustive` (`internal/event/retention_test.go:78`) fails
  for a type with no explicit class in `ClassOf`, and `TestBytesTracksStructGrowth` (`:156`) fails
  for a string field `Bytes` does not count.
- **Resolution:** `internal/event/retention.go` and its test join the first P5 commit.
  `engine_result` is classed as telemetry, as the `notice` it replaces is, and `ResultMethod` is
  counted in `Bytes`.
- **Not taken:** classing it as content, which keeps it in history longer than the notice it
  replaces.
- **The MADR is unchanged.**

### P5, first commit, 2026-11-01: the `engine_result` event, and grok's `/usage` and `/status`

**The capture (Deviation 1).**
`MCREMOTE_WIRE_CAPTURE_DIR=<scratch> go test -tags live_grok -run 'TestLiveGrok(SessionUsage|Billing)'
./internal/provider/acpagent/ -count=1 -v`, against grok 1.0.70, before any source change:
exit 0, both tests PASS, no prompt sent. The usage answer and the billing answer were appended
verbatim to `internal/provider/grok/testdata/wire/1.0.70/frames.jsonl` (16 frames to 18), each
chosen by its result's shape (`p5_append_capture.py` in the session scratchpad), and
`meta.json`'s count and note say so. The usage ledger is empty (`numTurns` 0, no cost field).
The billing answer carries `"creditUsagePercent":28.0`, which a decode and re-encode would write
as `28`.

**What changed.**

* `provider.RuntimeSession` returns `provider.Result` (`Method`, `Body`, `Text`) instead of a
  string. `Body` is the engine's answer, never decoded and re-encoded.
* `event.TypeEngineResult` (`engine_result`) carries the body in `Text` and the method in
  `ResultMethod`. It is a control type, like `notice`, and is retained as telemetry (Deviation 2).
* `internal/session`'s `cmdRuntime` emits an `engine_result` when the result has a body, and a
  notice with Magic's text when it does not.
* grok: `RuntimeUsage` and `RuntimeStatus` return `_x.ai/session/usage` and `_x.ai/billing` as
  grok sent them. An engine that cannot answer (an older build, an API-key install behind grok's
  auth gate) still gets Magic's one-line reason as a notice: it is not the engine's answer, so D12
  does not apply to it.
* **Deleted with the formatter (step 3):** `formatSessionUsage`, `formatUSDTicks`,
  `formatBilling`, `sessionUsageResponse`, `promptUsage`, `promptUsageModel`, and the tests
  `TestSessionUsageDecodesFlattenedTotals`, `TestUsageNeverReportsAnAbsentCostAsFree`,
  `TestRuntimeUsageReadsTheLedger` and `TestRuntimeStatusReadsBilling`.
* **Kept:** the billing types, `billingPeriod`, `billingDate` and `formatCents`. `quota.go`'s
  `confirmLimit` still decodes billing to annotate a quota error, and that moves in P6, not here.
  The live billing test still decodes strictly for the same reason. The live usage test now
  asserts that `RuntimeUsage` returns the installed binary's answer unchanged.
* codex and httpagent: signature only. They return their text unchanged in `Text` until their own
  commits.
* The phone: `SessionEvent.resultMethod`; a system `ChatItem` with `resultMethod`, kept in the
  transcript cache; the reducer appends the body untrimmed; `_EngineResult` shows the method and
  the body through `RawOutputView`, which renders JSON pretty-printed and collapsible.
* `docs/guides/protocol-v1.md` lists `engine_result`, documents `result_method`, and has a
  section for the event.

**Tests, each seen failing.** On a scratch copy of `HEAD` with only the new tests added, none
builds: the Go tests fail to compile against the string interface, and the Dart test reports "No
named parameter with the name 'resultMethod'". On scratch copies of the change, one plant each
(`p5_plants.py`, `p5_plant_content.py`):

| Plant | Fails |
| --- | --- |
| grok's body decoded and re-encoded in `extensionResult` | `TestUsagePassesGrokAnswerThrough`, `TestStatusPassesGrokBillingThrough` |
| `emitResult` sends the body as a notice, as before | `TestUsageSendsTheEngineAnswerUnchanged` |
| `engine_result` left out of `ClassOf` | `TestClassOfIsExhaustive` |
| `engine_result` classed as content | `TestEngineResultIsRetainedLikeANotice`: "engine_result is content, notice is telemetry" |
| the bubble draws it as a plain system line | "an engine result renders as its JSON under its method" |
| the reducer ignores `engine_result` | "engine_result keeps the engine body byte for byte", and the widget test |

`TestEventTypesAreDocumented` failed for real before the protocol guide was updated:
`event type "engine_result" is emitted but missing`.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 45 packages ok |
| `go vet` with each of `live_grok`, `live_kilo`, `live_opencode` | exit 0 |
| `make pre-add-check FILES=…` (the 13 Go files) | "13 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck` | exit 0; exit 0, 56 ignored findings, each an acp-go-sdk2 unstable-schema name |
| `make preflight` | dart format "230 files (0 changed)"; flutter analyze "No issues found!"; flutter test "+1506 ~3: All tests passed!" |
| `make markdownlint-docs`; `make check-records` | 0 issues in 57 files; exit 0 |
| the guard's redact preview | "would edit 0 file(s)" |

**Not done in this commit.**

* **Step 4, the live check of grok's operations.** The capture run used the live tests as they
  were. The rewritten live usage assertion has not run against grok. It spends no model tokens,
  but it waits for the owner's word.
* **The rest of P5:** codex status and usage, kilo usage, opencode and kilo session totals, then
  `/context`, one commit each.
* **Found, out of scope:** `go vet -tags live_codex ./internal/provider/codex/` fails on `HEAD`
  (`liveEngine redeclared`, `live_helpers_test.go:13` against `collaboration_test.go:451`). The
  0225 commit `85e10fb0` added a second `liveEngine`, so the codex live suites do not build. This
  belongs to [0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md](0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md),
  and waits for that record's amendment and the owner's approval.

**Files for this commit:** `internal/provider/provider.go`, `internal/event/event.go`,
`internal/event/event_test.go`, `internal/event/retention.go`, `internal/event/retention_test.go`,
`internal/session/commands.go`, `internal/session/engineresult_test.go` (new),
`internal/provider/acpagent/runtime.go`, `internal/provider/acpagent/runtime_test.go`,
`internal/provider/acpagent/live_runtime_test.go`, `internal/provider/codex/runtime.go`,
`internal/provider/codex/runtime_p4_test.go`, `internal/provider/httpagent/runtime.go`,
`internal/provider/grok/testdata/wire/1.0.70/frames.jsonl` and `meta.json`,
`docs/guides/protocol-v1.md`, `apps/mobile/lib/data/protocol/models.dart`,
`apps/mobile/lib/data/chat/chat_models.dart`, `apps/mobile/lib/data/chat/transcript_reducer.dart`,
`apps/mobile/lib/features/chat/chat_bubble.dart`, `apps/mobile/test/engine_result_test.dart`
(new), this plan.

*The owner committed this as `ad588bb0`. Before P5 continued, the owner chose to fix the codex
live-build break first: amendment A of
[0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md](0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md),
committed and pushed as `b85cfec3`.*

### P5 Deviation 3, 2026-11-01 — codex has no single answer to pass through. The owner decided: "Approve the mapping" and "One short turn"

- **Found** while naming the codex commit's files, before any change. codex's `/status` and `/usage`
  call nothing: they format `RuntimeSnapshot`, which Magic assembles from cached reads
  (`account/read`, `account/rateLimits/read`, `account/usage/read` and three more, in
  `RefreshRuntime`) and from notifications (`internal/provider/codex/runtime.go:432-466`). No
  engine body answers either command.
- **Found, the schema.** codex 0.166.0's export (`generate-json-schema`, read during the issue #31
  assessment) has `account/usage/read` take an optional `threadId`, "read estimated usage for this
  thread instead of account-wide token activity", answering `threadUsage` with per-model tokens and
  cost: the counterpart of grok's session ledger. `account/rateLimits/read` answers the account's
  windows, credits and plan: the counterpart of grok's billing. Its answer carries `accountId`.
- **Found, the capture.** The one codex capture (0.152.1) holds only an
  `account/rateLimits/updated` notification. codex's `KnownGoodVersion` is 0.152.1, and 0.166.0 is
  installed: issue #31's re-pin, which this deviation does not do.
- **Resolution, the mapping.** `/usage` calls `account/usage/read` with the session's thread id,
  and `/status` calls `account/rateLimits/read`. Each is gated on the engine's capability, as
  `RefreshRuntime`'s reads are, and its body passes through unchanged. A missing capability or a
  failed call is a notice saying why. `RuntimeSnapshot` and the codex surface it feeds do not
  change.
- **Resolution, the capture.** codex 0.166.0, run live with `MCREMOTE_WIRE_CAPTURE_DIR` in a
  scratch directory, through a scratch-only test in a scratch copy of the tree: start a thread,
  one short turn, then both reads. The two answers go to a new
  `internal/provider/codex/testdata/wire/0.166.0/` (`frames.jsonl`, `meta.json`), verbatim except
  that `accountId`'s value is replaced by a placeholder, as the grok 1.0.13 capture carries a zero
  UUID (MADR 0151 F5). The owner chose one turn, so `threadUsage` carries real figures; it spends a
  little codex quota.
- **Files for the codex commit:** `internal/provider/codex/runtime.go`, its tests, the new capture
  directory, this plan.
- **Not taken:** `/status` only, with `/usage` deferred to the re-pin; skipping codex in P5.
- **The MADR is unchanged:** D12 already covers it.

### P5 Deviation 4, 2026-11-01 — codex's thread usage read answers null. The owner decided: "Last tokenUsage notice"

- **Found** in Deviation 3's capture, before any source change. codex 0.166.0, one turn
  (`end_turn`), both capabilities advertised. `account/usage/read` with the thread's id answered
  199 bytes, every field `null`, `threadUsage` included: the schema gives it only "when a thread
  was requested and its billing route is available", and the account is on a ChatGPT plan. The
  same turn pushed one `thread/tokenUsage/updated` carrying `total`, `last` and
  `modelContextWindow` (16,858 tokens, 258,400 window). `account/rateLimits/read` answered 1,540
  bytes; besides `accountId`, each reset credit carries a 53-character `id`.
- **Resolution:** codex `/usage` returns the session's latest `thread/tokenUsage/updated` params,
  exactly as codex pushed them, with that method as the result's method. Before any is received,
  it is a notice that codex has reported no usage yet. `/status` stays Deviation 3's
  `account/rateLimits/read`. The committed frames are the `thread/tokenUsage/updated` notification
  and the rate-limits answer. In the rate-limits answer, `accountId` and each credit `id` are
  replaced by placeholders; the credits' titles and descriptions are generic text and stay.
- **Files added to the codex commit:** `internal/provider/codex/session.go`, which keeps the raw
  params where it already reads the notification.
- **Not taken:** passing the null thread read through; the read with a fallback to the
  notification, which puts two sources behind one command.
- **The MADR is unchanged.**

### P5, codex commit, 2026-11-01: codex `/status` and `/usage`

**The capture (Deviations 3 and 4).** codex-cli 0.166.0, from a scratch-only test in a scratch copy
of the tree (`p5codex/run_capture.py` in the session scratchpad; the copy was removed), with
`MCREMOTE_WIRE_CAPTURE_DIR` set: one turn ("Reply with exactly 'hello'"), `end_turn`; both
capabilities advertised; `account/usage/read` 181 bytes and `account/rateLimits/read` 1,521 bytes,
no error. The bodies were judged by shape before any value was shown (`scan.py`, `idshape.py`):
`accountId` is a UUID and each credit `id` a 20-character prefix and 32 hex digits.
`write_capture.py` wrote `internal/provider/codex/testdata/wire/0.166.0/` with two frames in
capture order, each count asserted: the `thread/tokenUsage/updated` notification, verbatim, and the
rate-limits answer, verbatim except `<account-id>`, `<credit-id-1>` and `<credit-id-2>`. The guard's
redact preview: "would edit 0 file(s)".

**What changed.**

* `session.go`: `lastTokenUsage` keeps the latest `thread/tokenUsage/updated` params as codex
  pushed them, under `s.mu`, before Magic decodes them. The decode and its `usage_update` event are
  unchanged.
* `runtime.go`: `RuntimeStatus` sends `account/rateLimits/read` (gated on
  `CapabilityAccountRateLimits`, bounded at 15 s) and returns codex's answer unchanged.
  `RuntimeUsage` returns `lastTokenUsage`. A missing capability, a stopped engine, a failed read or
  no report yet is one line saying why.
* **Deleted with the formatter (step 3):** both snapshot sentences, and the text assertions of
  `TestStatusAndUsageCommandsAreMappedAndReadable`, which keeps its command-table check as
  `TestStatusAndUsageCommandsAreMapped`. `RuntimeSnapshot` and the codex surface it feeds are
  unchanged.
* `runtime_passthrough_test.go` (new): `TestUsagePassesCodexTokenUsageThrough`,
  `TestStatusPassesCodexRateLimitsThrough` (a fake engine over a pipe answering with the captured
  result) and `TestStatusWithoutTheCapabilityIsALine` (a running engine without the capability
  must not be asked).

**Tests, each seen failing** (`p5codex/plants.py`). On a `git archive` copy of `b85cfec3` with only
the tests and the capture added, all three fail: "body = null … text = \"Codex usage is
unavailable.\""; "codex was never asked for account/rateLimits/read"; and the snapshot sentence
where a refusal was wanted. On copies of the change, one plant each:

| Plant | Fails |
| --- | --- |
| the rate-limits answer decoded and re-encoded | `TestStatusPassesCodexRateLimitsThrough` (keys reordered, `<` escaped) |
| the capability gate removed | `TestStatusWithoutTheCapabilityIsALine` (the read was sent) |
| the session keeps a re-marshalled map, not codex's params | `TestUsagePassesCodexTokenUsageThrough` |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 45 packages ok |
| the four new and changed tests, `-race -count=3` | ok |
| `make pre-add-check FILES=…` (the four Go files) | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0; exit 0, 56 ignored SDK names; exit 0, eight tags ok |
| `make check-records`; the guard's redact preview | exit 0; "would edit 0 file(s)" |

**Not done:** the live check of codex's operations (step 4), which needs the owner's word. The
capture run exercised both reads on 0.166.0, but not through the new `RuntimeStatus`.

**Also in this commit:** the close of
[0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md](0225-PLAN-fix-the-two-codex-tests-that-raced-in-ci.md)
amendment A (its A4 and A5, and `status: complete`), after CI on `b85cfec3`.

**Files for this commit:** `internal/provider/codex/runtime.go`, `internal/provider/codex/session.go`,
`internal/provider/codex/runtime_p4_test.go`, `internal/provider/codex/runtime_passthrough_test.go`
(new), `internal/provider/codex/testdata/wire/0.166.0/frames.jsonl` and `meta.json` (new), this
plan, and the 0225 plan.

### P6 brought forward, 2026-11-01 — a Spec-facing surface first. The owner decided: "0208 P6 now", "Amend 0208 and 0214", "Neutralize in P6"

- **Found:** 0214 P4 was about to add opencode's handlers to `acpagent`, because a Spec's hooks
  receive the unexported `*session` and cannot act on the session from another package. The owner
  stopped it: "no, we need to keep provider specific code OUT of acpagent not in it. we need to
  peel grok out of there as part of getting ready to adopt support for gobble."
- **Decision:** the MADR's amendment of 2026-11-01. `acpagent` exports `Host`; every hook takes
  it; grok moves to its package; `acpagent` names no provider. P6 runs now as P6a to P6d. P5's
  remainder (kilo usage, opencode and kilo totals, `/context`, and the live checks of step 4)
  follows 0214.
- **Files added to P6's scope:** `internal/provider/acpagent/host.go` (new); in P6c,
  `internal/provider/opencode/` for the tests that move there.

*The owner committed the amendment as `94bd486b`.*

### P6a, 2026-11-01: `Host`, and every hook takes it

**What changed.**

* `acpagent/host.go` (new) defines `Host`, which `*session` implements. Its methods:
  * identity: `AgentSessionID`, `CWD`, `Log`, `Config`, `Loading`, `SpecState`;
  * the wire: `RawRequest`, and `CallExtension`, which takes the full wire name and keeps the
    timeout and the method-not-found mapping of the retired `callAgentExtension`;
  * events: `Emit`, `Decline`, `Notice`, `NoteSubagent`, `ReportUsage`, `UpdateCompaction`;
  * limits: `NoteLimitText` (the stderr classifier's path), and `EndTurnWithLimit` (a limit
    reported structurally, with its retry time);
  * state `acpagent` holds: `UpdateMCPStatus`, `SetModelCatalog`;
  * round trips: `AwaitPermission`, `AwaitAnswers`.
* `Spec` gains `ExtensionRequests`, `NewSessionState`, `InitializeMeta` and `Ops` (`Fork`,
  `Compact`, `Rename`, `UndoLast`, `RuntimeUsage`, `RuntimeStatus`, `ConfirmLimit`).
  `ExtensionNotificationHandler`, `MetaHandler` and the two fallbacks take a `Host`, and
  `RawCaller` is gone. `bindSpec` gives a session what its Spec registers, in `Start` and in the
  wire-test helper.
* Dispatch:
  * `HandleExtensionMethod` serves the Spec's request and notification maps; the two grok names
    are no longer in a `switch`.
  * The session's `Fork`, `Compact`, `Rename`, `UndoLast`, `RuntimeUsage` and `RuntimeStatus`
    call the Spec's `Ops`.
  * The limit card's check calls `Ops.ConfirmLimit`.
  * `applyInitializeResponse` reads `_meta` through `Spec.InitializeMeta`.
  * A retry time from `EndTurnWithLimit` fills the card when the text names none.
* grok's code, still in `acpagent`, now uses only `Host`:
  * the notification handlers;
  * `HandleGrokExitPlanMode` and `HandleGrokAskUserQuestion`;
  * `GrokFork`, `GrokCompact`, `GrokRename`, `GrokUndoLast`, `GrokRuntimeUsage`,
    `GrokRuntimeStatus` and `GrokConfirmLimit`;
  * `GrokInitializeMeta`.

  Its cost ledger and compaction counter moved from the session into `GrokSessionState`. The
  session's `turnXAIUsage` is now `turnUsageReported`, set by `Host.ReportUsage`.
  `internal/provider/grok/grok.go` registers all of it. P6b moves this code to the grok package
  unchanged.
* Tests:
  * new: `host_test.go` (package `acpagent_test`, the fixture Spec), `export_test.go` (its wire
    harness), and `groktest_test.go` (grok's acpagent-side hooks, for the grok tests here,
    which cannot import the grok package);
  * set-up only: the handler literals take `Host`, the grok tests' sessions are bound to grok's
    hooks, and grok's `fakeRawCaller` embeds `acpagent.Host`.
  * The two source scans (`rawpath_test.go`, and `rewind_test.go`'s underscore check) follow the
    vendor calls to the callers of `CallExtension`, `RawRequest` and `extensionResult`. They
    still assert that every vendor method is a literal with its leading `_`.

**Differences from before, all small.**

* A Spec with no `Ops` answers fork, compaction, rename and undo with
  `provider.ErrNotImplemented` ("this agent offers no …"), and `/usage` and `/status` with one
  line. Before, every ACP Spec sent grok's `x.ai` methods. grok is the only ACP Spec registered,
  so no user sees a change (MADR D2: a nil operation is not offered).
* grok's billing-probe log lines carry `agent_session_id` instead of Magic's session id, which
  `Host` does not expose.
* An extension call's error names the method with its leading `_`, as the wire spells it.

**Tests, each seen failing** (`p6/plant_p6a.py` in the session scratchpad, on scratch copies):

| Plant | Fails |
| --- | --- |
| HEAD, with the fixture tests added | does not compile: "unknown field NewSessionState in struct literal of type acpagent.Spec", "undefined: acpagent.Host" |
| `NoteSubagent` stubbed | `TestHostDrivesTheSubagentPanel`: "no subagents event" |
| `Notice` stubbed | `TestHostPostsANotice`: "no notice event" |
| extension requests not dispatched | `TestHostAnswersAnExtensionRequestWithSpecState`: "answer 1 = null (code -32601)" |
| `SpecState` stubbed | the same test, by a panic on the fixture's state assertion |
| `EndTurnWithLimit` stubbed | `TestHostEndsATurnWithALimitAndItsRetryTime`: "no turn_complete event" |
| the retry time dropped | the same test: "retry at 0001-01-01 …, want 2026-11-01 12:00:05" |
| `_x.ai/billing` written without its `_` | both scans: "GrokConfirmLimit sends the standard method \"x.ai/billing\" untyped", and "CallExtension(\"x.ai/billing\") takes the standard-method raw path" |
| `_x.ai/session/usage` written without its `_` | both scans, the second as "extensionResult(\"x.ai/session/usage\") sends a vendor extension without its leading underscore" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 45 packages ok, `acpagent` and `grok` among them, no race |
| `make pre-add-check FILES=…` (the 36 changed Go files) | "36 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0 each |

grok's unit, conformance and wire suites pass (step 4). Their assertions are unchanged; only their
set-up changed, as listed above.

**Not done here:** moving grok's code (P6b), auth selection as a Spec policy (P6b), the source
test and comments (P6c), and declared capabilities (P6d).

**Files for this commit:** `internal/provider/acpagent/` (`host.go` new; `acpagent.go`,
`extensions.go`, `fork.go`, `quota.go`, `rewind.go`, `runtime.go`, `session.go`,
`sessioncaps.go`, `subagents.go`, `xaicompaction.go`, `xaiextensions.go`, `xaiusage.go`; tests
`host_test.go`, `export_test.go`, `groktest_test.go` new, and 18 changed),
`internal/provider/grok/grok.go`, `fallback.go`, `fallback_test.go`, and this plan.

*The owner committed P6a as `83dd5548`.*

### P6b Deviation 1, 2026-11-01 — grok's tests need a harness outside `acpagent`. The owner decided: "Fixture agent + StartOn"

- **Found** before any P6b change: 68 of `acpagent`'s tests exercise grok's code, mostly the
  rewind, runtime and extension tests.
  * They build sessions with `acpagent`-internal helpers: a scripted ACP agent on pipes
    (`newScriptedSession`, 15 tests), bare sessions (`newQueueTestSession`, `newExtSession`), and
    event drains.
  * Seven of them read session fields.
  * Moved to `internal/provider/grok`, they can reach none of that. grok's package cannot be
    imported into `acpagent`'s tests either, because it imports `acpagent`.
  * Step 2 ("their tests move with them") names no harness.
- **Resolution:**
  * `internal/provider/acpagent/acptest` (new): a fixture ACP agent on in-memory pipes. It
    answers each method from a script, records what it is sent, and can send notifications and
    requests.
  * `Provider.StartOn` (new, in `acpagent.go`) runs `Start`'s handshake and `session/new` or
    `session/load` on a connection it is given, instead of on a spawned process. `Start` and
    `StartOn` share one path after the connection exists.
  * grok's tests run real sessions through these. Their assertions stay; their set-up changes;
    the seven field reads become checks on events or on the session's own getters.
  * P10's conformance suite reuses the fixture agent, which P10 already names as "the fixture
    ACP agent from P6".
- **Not taken:** a fake `Host` for unit tests, which would restate every event assertion as an
  assertion on `Host` calls; and both together.
- **Files added to P6b's scope:** `internal/provider/acpagent/acptest/` (new), and
  `internal/provider/acpagent/acpagent.go` for `StartOn`.
- **The MADR** gets a note, because `StartOn` is a new exported entry point.

### P6b, 2026-11-01: grok out of `acpagent`

**What changed.**

* **The fixture agent and `StartOn`** (Deviation 1).
  * `acpagent/acptest/acptest.go` (new) is the agent end of a connection on in-memory pipes.
    * It answers `initialize`, `authenticate`, `session/new` and `session/load`, and any method
      the test scripts; anything else gets `-32601`.
    * It holds `session/prompt`, and any method named in `Hold`, for the test to answer.
    * It records every message, sends notifications and requests, and returns the client's
      answers.
  * `acpagent.go`: `spawnAgent` and `Start` are split into `newAgentSession`, `handshake` and
    `openSession`, unchanged in behaviour.
  * `Provider.StartOn` runs `handshake` and `openSession` on a connection it is given.
* **grok's code moved** to four new files in `internal/provider/grok`, renamed to that package's
  unexported names (`handleSessionNotification`, `forkSession`, `confirmLimit`, and so on):
  * `notifications.go`: the session notification and replay handlers, the cost ledger and
    compaction numbering (`sessionState`), and the MCP and model-catalog handlers;
  * `initialize.go`: the initialize `_meta` decoding and the model catalog;
  * `requests.go`: plan approval and the question form;
  * `ops.go`: fork, rewind, the usage and billing operations, compaction, rename and the
    billing check, with the billing types. It carries private copies of `truncateRunes` and
    `sanitizeUserFacingErr`, which `acpagent` keeps private.

  Comment blocks describing grok's rewind and runtime surfaces moved with them.
  `acpagent/xaicompaction.go` and `xaiextensions.go` are gone, and what was left of `xaiusage.go`
  (the turn's own usage) is `turnusage.go`.
* **The check that the move kept the code:** each of the 66 moved declarations, read with Go's
  parser before and after, is equal once the renames and the `acpagent.` qualifiers are undone.
* **Auth selection is a Spec policy** (step 1).
  * `Spec.AuthPreference` gives the order to try advertised, safe methods, and
    `Spec.KeyAuthMethodID` the method used when a key exists.
  * grok's Spec carries today's order: `cached_token`, then `xai.api_key`, with `xai.api_key` as
    its key method.
  * `TestSelectACPAuthMethod` keeps its table, with neutral method names.
* **Tests** (step 2). 62 tests changed package, from 13 `acpagent` test files to 7 in grok.
  * They run real sessions through the fixture agent: `grok/session_test.go` builds them.
  * A test that passed a session to grok's handler still does, as an `acpagent.Host`.
  * Where a test set the session's private fields, it now does one of three things:
    * runs a real turn: grok's panel clear at turn end, the two `retry_state` tests, and the
      billing check's limit card;
    * passes a `Host` that replaces one answer: the replayed cost (`Loading` and the agent id),
      the decline record, and the missing-id checks of undo and fork;
    * asks the session's own getter: `MCPStatus`.

    What each test asserts is unchanged.
  * Moved whole: the two grok live tests. They now start grok with grok's own Spec, on the same
    argv, because `/usage`, `/status` and undo run through its `Ops`.
  * Stayed in `acpagent`:
    * the tests of `acpagent`'s own behaviour, on neutral hooks instead of grok's functions: MCP
      status, the turn-usage interplay, the middleware's absence, fork's own checks, the
      non-blocking limit funnel (a recording `ConfirmLimit`), and the "no decline" form (a form
      from `Host.AwaitAnswers`);
    * the two transport tests, on neutral vendor names. `TestCallAgentExtensionPrefixesTheMethod`
      is now `TestCallExtensionSendsTheNameAsGiven`, since `CallExtension` has taken the full
      name since P6a.
  * Split: `TestApplyInitializeResponseRecordsWhatGrokReported` became
    `TestApplyInitializeResponseRecordsWhatTheSpecRead` (`acpagent`, a neutral hook) and
    `TestInitializeMetaReadsWhatGrokReported` (grok, grok's real response).
  * New in grok: `TestAGrokQuestionCannotBeDeclined`, which was a subtest of `acpagent`'s
    elicitation test.
  * The underscore scan moved to `grok/scan_test.go`, where grok's vendor calls are, and still
    allows only grok's two fallbacks a standard method. The standard-methods scan names its
    wrappers with their package (`grok.extensionResult`).
  * Two `acpagent` test helpers lost their last caller and are gone (`callExtension`,
    `scriptedAgent.paramsFor`).
* **Test names, before and after**, across both packages: 395 → 397. Two names are gone (the
  split, the rename), four are new, none is duplicated.
* **No `x.ai`, xAI or grok is named in `acpagent`'s production code** outside comments. The
  comments are P6c's.

**Tests, each seen failing** (`p6/plant_p6b.py` in the session scratchpad, on scratch copies):

| Plant | Fails |
| --- | --- |
| HEAD, with `acptest` added | does not compile: "p.StartOn undefined" |
| `StartOn` without its handshake | `TestStartOnRunsTheHandshake`, `TestStartOnAuthenticatesWithTheSpecsMethod`: "initialize has not completed" |
| the initialize hook not applied | `TestApplyInitializeResponseRecordsWhatTheSpecRead` ("InitializeMeta saw map[]"), `TestStartOnRunsTheHandshake` |
| grok's initialize hook dropping the version | `TestInitializeMetaReadsWhatGrokReported`: "Version = \"\", want \"1.0.13\"" |
| auth selection ignoring the Spec's preference | `TestSelectACPAuthMethod`: "got \"\", want \"vendor.token\"", and two more cases |
| a grok vendor call without its `_` | both scans: "grok.confirmLimit sends the standard method \"x.ai/billing\" untyped", and "CallExtension(\"x.ai/billing\") takes the standard-method raw path" |
| the fixture agent answering a prompt instead of holding it | `TestAHeldPromptIsAnsweredByTheTest`: "no session/prompt within 5s" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 46 packages ok (`acptest` is new), no race |
| `make pre-add-check FILES=…` (the 39 changed and new Go files) | "39 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0 each (staticcheck after the two unused helpers went) |
| `go vet -tags live_grok ./internal/provider/grok/` | exit 0 |
| the Windows test host, `make ci-windows` (HEAD plus the 51 changed, new and deleted files) | exit 0, "ALL SELECTED CHECKS PASSED", 46 packages ok |

**The live run** (step 4; `make live-grok`, the owner's word "1", run before P6b's commit; grok
1.0.70, the pin):

* `make live-grok` ended at the target's `-timeout 600s` with 158 PASS, 1 SKIP
  (`TestLiveRawOutputSamples`, which needs `MCREMOTE_RAW_OUTPUT_DIR`) and no FAIL.
  `TestLiveGrokSessionMetaDiscrimination` was 2 min 9 s in, and ten tests had not started.
* The three tests that moved here passed: `TestLiveGrokRewindPointsDecodeStrictly` (6.00 s),
  `TestLiveGrokSessionUsageDecodesStrictly` (3.31 s), `TestLiveGrokBillingDecodesStrictly`
  (4.00 s).
* The eleven left were run alone with the same flags, and all passed (342.7 s).
  `TestLiveGrokSessionMetaDiscrimination` took 284.38 s; the others took 0.00 to 19.09 s.
* In all: 169 PASS, 1 SKIP, no FAIL.
* **Found:** the suite no longer fits the target's 600 s. 0184's run took 485.9 s for 144 tests,
  and this one about 943 s across the two runs.
  * The two longest tests, `TestLiveGrokSessionMetaDiscrimination` (284.38 s) and
    `TestLiveGrokAutoDiscriminationPair` (153.73 s), are model turns; nothing P6b moved is among
    them.
  * The `Makefile` is not in P6's scope, so the target is unchanged; the owner decides.

**Not done:** comments naming grok in `acpagent`, and the source test, are P6c. The
`live-grok` timeout above.

*The owner committed P6b as `b110cb50`.*

### P6b Deviation 2, 2026-11-01 — the live grok suite outgrew its timeout. The owner decided: "split the suite and change the timeout to 900 sec"

- **Found** at P6b's live run (above): `make live-grok` needs about 943 s, and the target allows
  600 s. The two discrimination tests, which drive several model turns each, took 438 s of it.
- **Resolution:**
  * `make live-grok` runs two targets, each with `-timeout 900s`:
    * `live-grok-discrimination`: `TestLiveGrokSessionMetaDiscrimination` and
      `TestLiveGrokAutoDiscriminationPair`, about 438 s;
    * `live-grok-rest`: every other grok live test, about 505 s.
  * One regex, `^TestLiveGrok\w*Discrimination\w*$`, selects the first part with `-run` and the
    second with `-skip`, so every test runs in exactly one part.
  * The `make live-grok` that AGENTS.md names runs both.
- **Files added to P6's scope:** `Makefile`.
- **Checked without spending quota:**
  * `make -n live-grok` prints the two `go test` commands, each with `-timeout 900s`, one with
    `-run` and one with `-skip` on the regex.
  * A check (`p6/split_check.py` in the session scratchpad) reads the regex from the `Makefile`
    and applies it to `go test -list` of the live suite: "170 tests: 2 in
    live-grok-discrimination, 168 in live-grok-rest", the two being the discrimination tests.
  * On a copy whose regex names no test, it fails: "the first part selects [], want
    ['TestLiveGrokAutoDiscriminationPair', 'TestLiveGrokSessionMetaDiscrimination']".
  * `go test -list` with the regex lists the same two tests. `-list` does not apply `-skip`, so
    the second part rests on `-skip` matching as `-run` does, which the check mirrors.
- **Not run:** the split targets live. The suite passed in full at P6b, and a run spends quota.

*The owner committed the split as `d0b06f90`.*

### P6c, 2026-11-02: `acpagent` names no provider

**What changed.**

* **The source test** is `acpagent/neutral_test.go`, `TestNoProviderIsNamedInAcpagent`.
  * It walks `acpagent`'s non-test `.go` files, its subpackages included and `testdata` left
    out.
  * It fails on any line that names `grok`, `opencode`, `kilo`, `codex`, `gobble`, `x.ai` or
    `xai`, in any case, in code or in a comment, reporting the file, the line and its text.
  * It fails if it scans fewer than ten files.
* **The comments**, in 9 files (`acpagent.go`, `config.go`, `extensions.go`, `session.go`,
  `sessioncaps.go`, `terminal.go`, `thinking.go`, `turnusage.go`, `version.go`).
  * Each was rewritten to say what the code does for any agent, keeping its MADR citation.
  * The `callAgentExtension` comment, orphaned since P6a removed that function, is gone;
    `Host.CallExtension`'s comment covers it.
  * The grok facts these comments carried now sit beside grok's code in
    `internal/provider/grok/grok.go`: the values `--permission-mode` and `--sandbox` take, why
    the grok.com auth method is never pinned, and why grok sets no `CredentialEnv`.
* **0214 P2's tests are neutral:**
  * `vendorext_test.go`, `metaext_test.go` and the conformance rows use `vendor/…` names;
  * they read `acpagent/testdata/vendor/` (new), a hand-written fixture of three child-session
    notifications and two prompt results, shaped like G1's frames, with a `meta.json` saying so;
  * their counts follow that fixture: 3 notifications where G1 has 7, and 2 prompt results where
    it has 4;
  * `TestG1PromptResultsCallNoMetaHandler` is now `TestEmptyPromptResultMetaCallsNoHandler`.
* **opencode's frames are tested in its package.**
  * `internal/provider/opencode/acp_frames_test.go` (new) holds three tests:
    `TestG1ChildUpdatesReachTheHandler` (G1's seven), `TestG1PromptResultsCallNoMetaHandler`
    (G1's four) and `TestRetryMarkerReachesItsHandler` (the transcribed retry frames, with their
    source citations).
  * They run through `acptest` on a Spec registering opencode's names. P4's Spec builds on them.

**Tests, each seen failing.**

* The source test on the tree before P6c: 81 hits in the 9 files, 62 of them "grok".
* On scratch copies (`p6/plant_p6c.py` in the session scratchpad):

| Plant | Fails |
| --- | --- |
| a provider named in a comment of `acptest/acptest.go` | `TestNoProviderIsNamedInAcpagent`: "acptest/acptest.go:26 names \"grok\"" |
| vendor notifications not delivered | `TestVendorNotificationReachesItsSpecHandler` ("called 0 time(s), want 3") and opencode's `TestG1ChildUpdatesReachTheHandler` ("called 0 time(s), want 7") |
| `_meta` keys not dispatched | `TestRetryMetaReachesItsHandler` (both cases), `TestPromptResultMetaReachesItsHandler`, and opencode's `TestRetryMarkerReachesItsHandler` ("called 0 time(s), want 3") |

  A first form of the third plant removed the call outright and failed to compile; it was
  replaced by one that keeps the call and never makes it.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 46 packages ok, no race |
| `make pre-add-check FILES=…` (the 15 changed and new Go files) | "15 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done, and for the owner.**

* The MADR's amendment says `acpagent`'s tests "use neutral vendor names and fixtures of their
  own"; P6c's step 2 names only 0214 P2's tests.
* Thirty other `acpagent` test files still name a provider: 51 comment lines and 41 code lines,
  most of the code lines model ids such as `grok-4.6` used as test data.
* Two of them read another provider's directory:
  * `rawpath_test.go` scans grok's code on purpose, to keep standard methods to grok's two
    fallbacks;
  * `sessiontitle_test.go` reads grok's 1.0.13 wire fixture.

**Files for this commit:** `internal/provider/acpagent/` (`acpagent.go`, `config.go`,
`extensions.go`, `session.go`, `sessioncaps.go`, `terminal.go`, `thinking.go`, `turnusage.go`,
`version.go`; `neutral_test.go` and `testdata/vendor/` new; `vendorext_test.go`,
`metaext_test.go`, `conformance_test.go`), `internal/provider/grok/grok.go`,
`internal/provider/opencode/acp_frames_test.go` (new), and this plan.

*The owner committed P6c as `90f62f17`.*

### P6c Deviation 1, 2026-11-02 — the rest of `acpagent`'s tests. The owner decided: "add the 30 files to the plan"

- **Found** at P6c (above): thirty `acpagent` test files still name a provider, against the
  MADR amendment's "its tests use neutral vendor names and fixtures of their own".
- **Resolution, as a second P6c commit:**
  * The thirty files are made neutral. Comments say what the test pins for any agent; test data
    (model ids, provider ids, method names) uses neutral values. What each test asserts about
    `acpagent` is unchanged.
  * A test that reads another provider's directory moves to that provider's package with what
    it reads:
    * `rawpath_test.go`'s scan of grok's fallbacks goes to grok, and `acpagent`'s copy scans
      `acpagent` only;
    * `sessiontitle_test.go`'s use of grok's wire fixture goes to grok, unless the test pins
      only `acpagent` behaviour, in which case it gets a neutral fixture.
  * `TestNoProviderIsNamedInAcpagent` covers `acpagent`'s test files too, all but itself, so
    the rule holds from here on. It is seen failing on the tree before this commit.
- **Files added to P6c's scope:** the thirty `acpagent` test files, and grok test files for what
  moves.

### P6c, second commit, 2026-11-02: `acpagent`'s tests name no provider

**What changed.**

* **`TestNoProviderIsNamedInAcpagent`** now scans `acpagent`'s test files as well, all but
  `neutral_test.go`, which holds the names. On the tree before this commit it failed with 92
  hits in 30 files.
* **The thirty files:**
  * comments rewritten to say what each test pins for any agent, keeping their MADR citations;
  * test data made neutral: model ids `model-a`, `model-a-fast` and `model-b`; provider ids
    `vendor`; methods `_vendor/…`; and the auth method `vendor.browser`.
  * Three tests renamed: `TestTurnUsageIsNotCountedBesideXAIUsage` →
    `TestTurnUsageIsNotCountedBesideTheSpecsOwn`, `TestReplayedXAIUsageDoesNotSilenceTheTurn` →
    `TestReplayedSpecUsageDoesNotSilenceTheTurn`, and `TestEmitClassifiedTurnErrorGrok401` →
    `TestEmitClassifiedTurnErrorAuth401`.
* **The tests that read grok's data split in two.** acpagent keeps the behaviour, on neutral data;
  grok keeps its recorded data, on real sessions through `acptest`.
  * **Standard methods.** `TestStandardMethodsAreTypedOutsideGrokFallbacks` now lives in
    `grok/rawpath_test.go` and scans grok's code, allowing its two fallbacks. `acpagent`'s copy,
    `TestStandardMethodsAreTypedInAcpagent`, scans `acpagent` and allows none.
  * **Session titles.** `TestSessionInfoUpdateTitleIsAGeneratedSessionTitle` reads two titles
    added to `acpagent/testdata/vendor/` (now 7 frames). grok's copy, in
    `grok/behaviour_test.go`, replays grok 1.0.13's two titles.
  * **The 401.** `TestEmitClassifiedTurnErrorAuth401` uses a neutral payload. grok's
    `TestEmitClassifiedTurnErrorGrok401`, in `grok/behaviour_test.go`, answers a real turn with
    grok 1.0.0's captured payload.
* **`acptest`** gains `RespondErrorData`, so a test can answer with an error carrying `data`.

**Tests, each seen failing** (`p6/plant_p6c2.py` in the session scratchpad, on scratch copies):

| Plant | Fails |
| --- | --- |
| a provider named in an `acpagent` test comment | `TestNoProviderIsNamedInAcpagent`: "purge_test.go:72 names \"grok\"" |
| an untyped standard call added to `acpagent` | `TestStandardMethodsAreTypedInAcpagent`: "acpagent.plantedRaw sends the standard method \"session/set_model\" untyped" |
| grok's fork sent untyped as `session/fork` | grok's `TestStandardMethodsAreTypedOutsideGrokFallbacks` and `TestNoExtensionMethodIsWrittenWithoutItsUnderscore` |
| a title not marked generated | both copies of `TestSessionInfoUpdateTitleIsAGeneratedSessionTitle` |
| the classified kind dropped from the error event | `TestEmitClassifiedTurnErrorAuth401` and `TestEmitClassifiedTurnErrorGrok401`: "error_kind=\"\"" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 46 packages ok, no race |
| `make pre-add-check FILES=…` (the 34 changed and new Go files) | "34 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

P6c is complete with this commit. P6d, the declared capabilities, is next.

**Files for this commit:** the thirty `acpagent` test files, `acpagent/neutral_test.go`,
`acpagent/acptest/acptest.go`, `acpagent/testdata/vendor/` (`frames.jsonl`, `meta.json`),
`grok/behaviour_test.go` and `grok/rawpath_test.go` (new), and this plan.

*The owner committed this as `84c39872`.*

### P6d Deviation 1, 2026-11-02 — the failing-first test, and `manager.go`. The owner decided: "Replace test, add manager.go"

- **Found** before the first write. Step 1's assertion (no `_x.ai/*` frame from a Spec with no
  operations) already holds on `84c39872`, so it cannot be seen failing:
  * `acpagent`'s operations return "not offered" when the Spec's hook is nil, before the wire
    (`acpagent/sessioncaps.go:177`, `:185`; `fork.go:15`; `rewind.go`; `runtime.go`);
  * P6c's source test keeps `x.ai` out of `acpagent`.
- What does fail is D1's subject. `internal/session/commands.go:66-80` reads availability from Go
  types, and `acpagent`'s session has `Compact`, `Fork`, `UndoLast`, `RuntimeUsage`,
  `RuntimeStatus`, `SetModel` and `SetThinkingLevel` whatever its Spec offers. A Spec with no
  operations shows all of them available.
- Two of D1's checks are outside the files listed:
  * the phone's rename, fork, revert and diff are availability checks by type assertion
    (`internal/session/manager.go:2885`, `:3244`, `:3302`, `:3343`);
  * commands are re-resolved on agent commands, modes, collaboration modes, the first usage and
    `/model` (`manager.go:1495-1565`), but not on a config option update, which is where an ACP
    session's model and thinking capabilities change.
- **Resolution:** step 1 is replaced by P6d's step 1 (above), and `manager.go` joins P6d's
  files. One commit. The MADR's Confirmation is amended to match ("Added at P6d").
- **Files added to P6's scope:** `internal/session/manager.go`.
- **Added the same day, before the first code write. The owner decided: "Add the 3 ws tests".**
  `Capabilities()` on `provider.Session` must be implemented by every session type. Besides the
  four adapters, 24 test doubles in `internal/session`, one in `internal/provider` and three in
  `internal/ws` implement it.
  The `internal/ws` three (`receipt_e2e_test.go`, `setconfig_async_test.go`,
  `tool_output_test.go`) are not tests of P6d's files. Each gains the method, declaring nothing,
  and asserts nothing new. The files join P6d's scope.

### P6d, 2026-11-02: declared capabilities

**What changed.**

* **`provider.go`.**
  * `Capability`: one constant per operation, each documented with the interface that performs
    it.
  * `Capabilities`: a set of them, with `Has` and `NewCapabilities`.
  * `Implements(s, c)`: whether `s` has `c`'s interface.
  * `Session` gains `Capabilities()`.
* **The adapters declare.**
  * **`acpagent`:** from `Spec.Ops` (compact, fork, rename, undo, usage, status); a model or
    thinking switch when the agent has the `model` or `thought_level` config option, or the Spec
    a fallback. The answer changes with the agent's config options.
  * **`httpagent`:** what the dialect's session implements, plus `/status` and `/usage`, which
    every dialect answers.
  * **`codex`:** its fixed surface. The Fast tier and personality are read from the current
    model, as before. The approvals reviewer stays static, as step 3 says, although the engine
    reports whether it can write the setting.
  * **`fake`:** the six operations it implements.
* **`commands.go`.**
  * `commandContext` reads the declaration through `opCapabilities`, in place of nineteen type
    assertions.
  * `undoNotice` offers `/redo` only when revert is declared. It was an availability check by
    type, in a file already in P6d's scope.
* **`manager.go`.**
  * Rename, fork, revert, unrevert and diff check the declaration before the call.
  * A session config event re-resolves the commands.
* **Test doubles.** 24 in `internal/session`, one in `internal/provider` and three in
  `internal/ws` declare what they define (Deviation 1).

**What it changes for today's providers: nothing visible.**
* grok's Spec sets all six operations and both fallbacks, so it declares what its type
  implemented.
* codex and fake declare what they implement.
* opencode's and kilo's dialect sessions implement every optional operation, except kilo's
  thinking switch, which kilo's table already marks unavailable.
* What changes is any ACP Spec that offers fewer operations: opencode over ACP (0214 P4), and
  gobble.

**Tests, each seen failing** (`p6/plant_p6d.py` in the session scratchpad, on scratch copies):

| Test | Fails on |
| --- | --- |
| `session.TestUndeclaredOperationsAreUnavailable` (step 1) | the tree before P6d: "/compact resolves available ({Kind:op Op:compact …}); the session declared no such operation", the same for `/fork`, `/undo`, `/usage`, `/status` and `/thinking`, and "/model resolves to {Kind:op Op:set_model …}, want the daemon's relaunch" |
| the same | `commandContext` inferring from types again (`\|\| provider.Implements(sess, c)`): the same six lines |
| the same | the phone rename gate dropped, with `acpagent`'s `Rename` sending `_vendor/session/rename` when its hook is nil: "the agent was sent \"_vendor/session/rename\"; an undeclared operation reached the wire" |
| `session.TestSessionConfigReresolvesCommands` | no re-resolution on a session config event: "/thinking was not advertised available after the agent's config options added it" |
| `session.TestUndoNoticeOffersRedoOnlyWhenThereIsOne` (existing) | `undoNotice` reading undo instead of revert: "it offers /redo to a session that cannot redo" |
| `acpagent.TestCapabilitiesFollowTheSpecAndTheAgent` | undo not declared when `Ops.UndoLast` is set: "want \"undo\" among them"; the `thought_level` option ignored: "declared map[set_model:true], want exactly [set_model set_thinking_level]" |
| `httpagent.TestCapabilitiesFollowTheDialect` | fork declared whatever the dialect: "declared map[fork:true status:true usage:true], want exactly [status usage]" |
| `codex.TestCapabilitiesAreTheFixedSurfaceAndTheModels` | the Fast tier no longer read: `gpt-5.6-sol` missing `service_tier`; `Implements` mapping diff to `UndoSession`: "declares \"diff\" without the interface that performs it" |
| `fake.TestCapabilitiesAreWhatTheFakeImplements` | fork declared: "declares \"fork\" without the interface that performs it" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 46 packages ok, no race |
| `make pre-add-check FILES=…` (the 36 changed and new Go files) | "36 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done.**
* No grok test pins that grok's session declares its operations. Its Spec sets all six and both
  fallbacks (P6b), so by its code it declares every operation its type implements. A pin would
  be a new grok test file, outside P6d's files.
* `make ci-windows` was not run: the PLAN asks it only of P6b.
* No live suite was run.

P6d is complete with this commit, and with it P6. 0214 P4, opencode's ACP Spec, is next.

**Files for this commit:** `internal/provider/provider.go` and `provider_test.go`;
`acpagent/sessioncaps.go`; `httpagent/session.go`; `codex/session.go`; `fake/fake.go`;
`internal/session/commands.go` and `manager.go`; the new `session/capabilities_internal_test.go`,
`acpagent/capabilities_test.go`, `httpagent/capabilities_test.go`,
`codex/declared_capabilities_test.go` and `fake/capabilities_test.go`; the test doubles in 21
`internal/session` and three `internal/ws` test files; the MADR; and this plan.

### P5 Deviation 5, 2026-11-04 — the remainder, restated after 0214. The owner decided: "Approve, do P5", "GET /session/{id}" and "Yes, capture with no turn"

P5's remainder was deferred until 0214 was done ("P6 brought forward", above). 0214 is complete
(`7788da48`). Read again at the restart (master `a3a29a6c`; kilo `7.8.18`):

* **P5's order names files that no longer hold the operations.**
  * "kilo usage (`kilo/usage.go`)": that file now holds only per-turn usage events (`emitUsage`).
  * kilo's `/usage` is `httpagent`'s `RuntimeUsage`: `GET /session/{id}`, rendered by
    `formatSessionTotals` (`httpagent/runtime.go:128-160`).
  * kilo's `/status` is its dialect's `RuntimeStatus`: `GET /kilocode/provider-usage`, rendered by
    `formatProviderUsage` (`kilo/runtime.go:23-57`).
  * "opencode and kilo session totals": opencode no longer uses `httpagent` (0214 P7), and its
    `/status` and `/usage` are `KindNone` (`opencode/commandtable.go`).
* **`/context`** is still Magic's line from its last usage report (`session/commands.go:499`).
* **No capture holds either kilo answer.** `kilo/testdata/wire/7.8.18/` is the `/global/event`
  stream only: httpagent's capture records SSE frames (`httpagent/provider.go:816`), not HTTP
  answers.

**Decided.**

* **"Approve, do P5".** The remainder is three commits, in this order:
  1. kilo `/status`: the answer of `GET /kilocode/provider-usage`, unchanged, in `engine_result`.
     `formatProviderUsage` and `windowSummary` and their prose tests go. `providerUsage`, which
     confirms a limit (`quota.go`), stays.
  2. kilo `/usage`: the answer of `GET /session/{id}`, unchanged. `formatSessionTotals` and its
     prose tests go. A session with no engine id keeps its notice: there is no engine answer.
  3. `/context`: the JSON of the usage record Magic holds (D12 step 2), with the same rendering.
     Magic's line goes.
* **"GET /session/{id}".** The call Magic already makes; no new route is relied on. The phone gets
  the whole session object.
* **"Yes, capture with no turn".** One capture on this host from kilo 7.8.18 with the owner's kilo
  credentials, and no prompt:
  * `kilo serve` on loopback, as the dialect starts it;
  * a session created in a scratch directory;
  * the two answers saved raw, then the session deleted.

  The bodies are read, identifiers replaced with placeholders, and the guard's preview run, before
  they are copied into `kilo/testdata/wire/7.8.18/`.
* **Each commit's files** are named at its start, as the rules say. Step 4's live checks stay on the
  owner's word.

### P5, kilo `/status` commit, 2026-11-04: kilo's plan usage as kilo sent it

**The capture** (Deviation 5; `p5r/capture_kilo.py` in the session scratchpad).
* kilo `7.8.18`, a private `kilo serve` on loopback, with the owner's credentials and no prompt.
* `POST /session` and `GET /session/{id}` answered `200` JSON; `GET /kilocode/provider-usage`
  answered `200` `{"items":[], "generatedAt":…}`: the account has no plan windows. The session was
  deleted (`200`) and the server stopped (exit 0).
* `provider-usage.json` carries no identifier, and is copied unchanged into
  `kilo/testdata/wire/7.8.18/`; `meta.json` describes it. The session answer waits for the `/usage`
  commit, redacted there.

**What changed.**
* `httpagent.RuntimeDialect.RuntimeStatus` returns a `provider.Result`.
  * `runtimeStatus` passes a dialect's answer through unchanged.
  * A failed read is still a line naming the failure, and an empty answer still the
    "publishes no plan usage" line, since `cmdRuntime` would show an error as nothing.
* kilo's `RuntimeStatus` reads `GET /kilocode/provider-usage` into a `json.RawMessage`, and returns
  it with `Method: "GET /kilocode/provider-usage"`.
* `formatProviderUsage` and `windowSummary` and their three prose tests are deleted.
  `providerUsage`, which confirms a limit after a failed turn (`quota.go`), stays.
* The phone needs nothing new: `emitResult` sends a body as `engine_result`, which P3's JSON view
  renders (P5's first commit).

**Tests, each seen failing** (`p5r/plant_status.py`, on scratch copies):

| Break | Fails |
| --- | --- |
| kilo's answer returned as text | `TestStatusPassesKiloProviderUsageThrough`: `body = null`, and the text holds the answer |
| another method named | the same: `method = "GET /plan"` |
| a second engine call | the same: `engine calls = [GET /kilocode/provider-usage GET /session]` |
| the engine's error swallowed | `TestStatusReturnsTheEnginesError`: `err = <nil>` |
| httpagent rewriting the answer | `TestStatusReportsADialectFailureAsText`: "want the dialect's answer unchanged" |

`TestStatusWithoutARuntimeDialectStillAnswers` and the failure and blank cases of
`TestStatusReportsADialectFailureAsText` are restated for the `Result`, asserting what they did.

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (4 Go files) | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| `make lint`; `make staticcheck` | exit 0 |

**Not done:**
* The live check (step 4): this commit's answer came from the live engine at the capture, but no
  `make live-kilo` ran.
* kilo `/usage` and `/context` are the next two commits.

**Files for this commit:**
* `internal/provider/httpagent/runtime.go` and `runtime_test.go`;
* `internal/provider/kilo/runtime.go` and `runtime_test.go`;
* `internal/provider/kilo/testdata/wire/7.8.18/provider-usage.json` (new) and `meta.json`;
* this plan.

*The owner committed the kilo `/status` commit as `376f063b`.*

### P5, kilo `/usage` commit, 2026-11-04: the session object as kilo sent it

**The capture** is the kilo `/status` commit's: `GET /session/{id}` on a new session with no turn.
`p5r/redact_session.py` (session scratchpad) put it into `kilo/testdata/wire/7.8.18/session.json`.
It replaced the macOS per-user temp root, in `directory` and `path`, with `/var/folders/<id>/<user>/T`
(the stream capture's rule), and changed nothing else. `meta.json` describes it.

**What changed** (`internal/provider/httpagent/runtime.go`).
* `RuntimeUsage` passes `GET /session/{id}` through unchanged, with `Method: "GET /session/<id>"`,
  through a new `runtimeUsage` seam that takes the `API`, as `runtimeStatus` does.
* A session with no engine id, and a failed read, are still a line of text: there is no engine
  answer to pass.
* `sessionTotals`, `fetchSessionTotals` and `formatSessionTotals` are deleted. Nothing else read
  them. So are their tests:
  * `TestSessionTotalsDecodeFromTheSessionObject`;
  * `TestFormatSessionTotalsStatesSubCentCosts`;
  * `TestLivePayloadsFromBothEnginesDecodeIdentically`, with their hand-trimmed payloads.
* `TestSessionUsageReadsTheSessionObjectAndNothingElse` is restated as
  `TestUsagePassesTheSessionObjectThrough`. It reads the capture from kilo's testdata, and still
  allows exactly one engine call.

**Tests, each seen failing** (`p5r/plant_usage.py`, on scratch copies):

| Break | Fails |
| --- | --- |
| the answer returned as text | `TestUsagePassesTheSessionObjectThrough`: `body = null`, and the text holds the answer |
| `model-usage` read as well | the same: `engine calls = [GET /session/ses_… GET /session/ses_…/model-usage]` |
| the method without the id | the same: `method = "GET /session"` |
| the engine called with no session id | `TestUsageWithoutAnEngineAnswerIsText`: "a session with no engine id must not call the engine" |
| a failed read returned as nothing | the same: `usage = {Method: Body:null Text:}` |

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (2 Go files) | "2 file(s) clean (gofmt, golint, govulncheck)" |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0 |

**Not done:**
* The live check (step 4), as for `/status`.
* `/context` is the next commit.

**Files for this commit:**
* `internal/provider/httpagent/runtime.go` and `runtime_test.go`;
* `internal/provider/kilo/testdata/wire/7.8.18/session.json` (new) and `meta.json`;
* this plan.

*The owner committed the kilo `/usage` commit as `b12a8274`.*

### P5 Deviation 6, 2026-11-04 — `/context` and 0189 D2's session cost. The owner decided: "Record plus kept cost"

Found while naming the `/context` commit's files, before any change:

* **Step 2 says** `/context` is "the JSON of the usage record Magic already holds".
* **[0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)
  D2 says** `/context` appends the session cost, and its Confirmation requires "the cost suffix when
  known and none when unknown". `session/sessioncost_test.go:116-154` pins it.
* **The two differ.** The record Magic holds is `entry.lastUsage`, the latest report as it arrived.
  A report without a cost leaves `meta.SessionCost`, the last known total, unchanged
  (`session/manager.go:1600-1609`). So the latest record can lack a cost Magic still knows.
* Today's line also names the model and the mode. Neither is part of the usage record.
* The phone already keeps the last known cost from usage events (`transcript_reducer.dart:117`).

**Decided: "Record plus kept cost".**
* `/context` sends Magic's usage record (`event.Usage`) as JSON, in an `engine_result` named
  `usage_update`, the event the record came from.
* Its `session_cost` is the kept total when the latest report has none. So 0189 D2 holds: the cost
  when known, none when unknown.
* 0189 is amended: D2's `/context` bullet and its Confirmation line now name the JSON field, not
  the line's suffix.
* The model and the mode leave `/context`; the phone shows both elsewhere.
* **Files for the commit:** `internal/session/commands.go`, `commands_internal_test.go`,
  `sessioncost_test.go` and `canonical_test.go`; the 0189 MADR; this plan.

### P5, `/context` commit, 2026-11-04: Magic's usage record as JSON

**What changed** (`internal/session/commands.go`).
* `cmdContext` sends a copy of `entry.lastUsage` as JSON, through `emitResult`, as an
  `engine_result` named `usage_update`. Its `session_cost` is `meta.SessionCost`, the kept total,
  when the latest report carried none (Deviation 6).
* Before any report it still says "No context report yet": there is no record to send.
* The model and the mode leave `/context`. `formatSessionCost` and `formatCount` are deleted; nothing
  else used them. `TestFormatCount` goes with them.
* Tests restated:
  * 0189's Confirmation, `TestContextReportsTheSessionCost`, decodes the record and checks
    `session_cost`: none, a cost, a free one, another currency, and a new case kept through a report
    without one.
  * `TestCanonicalOpCommands`' `/context` row becomes `TestCanonicalContextIsTheUsageRecord`.
  * `TestContextBeforeAnyReportSaysSo` is new.

**Tests, each seen failing** (`p5r/plant_context.py`, on scratch copies):

| Break | Fails |
| --- | --- |
| the kept cost not merged | `TestContextReportsTheSessionCost/kept_through_a_report_without_one`: `session_cost = <nil>, want &{Amount:1.5 Currency:USD}` |
| the record sent as a notice | `TestContextReportsTheSessionCost/none`: "no engine_result was sent" |
| another method named | the same: `method = "context", want usage_update` |
| an empty record sent before any report | `TestContextBeforeAnyReportSaysSo`: `/context sent "{}" before any usage report` |
| the same method break, through the fake provider | `TestCanonicalContextIsTheUsageRecord`: "event engine_result not observed" |

A first version of the first break deleted the merge outright, which left `cost` unused and did not
compile. It was redone as `_ = cost`.

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (4 Go files) | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0 |

**Where P5 stands.** Every operation P5 names passes the engine's answer through, or Magic's own
record where no engine answers:
* grok's and codex's `/usage` and `/status`;
* kilo's `/status` and `/usage`;
* `/context`.

The Verification section says a phase is not complete until its named live run passes. **Step 4's
live checks have not run:**
* `make live-grok` (both parts);
* `make live-codex`;
* `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo`.

Each waits for the owner's quota word. Until then P5 is done in code, but not complete.

**Files for this commit:**
* `internal/session/commands.go`, `commands_internal_test.go`, `sessioncost_test.go` and
  `canonical_test.go`;
* `docs/decisions/0189-MADR-carry-the-agent-session-cost-as-a-session-total.md`;
* this plan.

*The owner committed the `/context` commit as `8f8e32ed`.*

### P5, the live checks, 2026-11-04 (step 4)

On the owner's word, "run all live test, you have approval", on master `8f8e32ed` (this host):

| Run | Result |
| --- | --- |
| `make live-grok` (both parts) | exit 0, 230 PASS. `TestLiveGrokSessionUsageDecodesStrictly` and `TestLiveGrokBillingDecodesStrictly` passed. One skip, `TestLiveRawOutputSamples`, which needs `MCREMOTE_RAW_OUTPUT_DIR` |
| `make live-codex` | exit 0, 540 PASS. Two skips, each needing an opt-in variable: `TestGenerateContractManifest` (`CODEX_CONTRACT_GENERATE`) and `TestLiveRawOutputSamples` |
| `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo` | exit 0, 205 PASS. Three skips, each needing an opt-in variable |
| `TestLiveProviderUsageAnswersTheShapeWeParse`, against a private `kilo serve` (`p5live/run_provider_usage.py`) | exit 0: `plans=0`, the capture's shape |

**Not run:**
* `TestLiveKiloTogetherAIKeyRoundTrip`: it writes a key into the owner's kilo credential store
  (`MCREMOTE_LIVE_AUTH_WRITE`). It has nothing to do with P5.
* The raw-output and contract-generation samples, which are opt-in capture runs.

**Not covered:** no codex live test calls `/status` or `/usage` through the provider. The codex
commit's capture was taken live, and its fixture test replays it. The live suite passing says the
engine still speaks the shapes the suite checks, not that these two operations were driven end to
end.

**P5 is complete.** Every operation it names passes the engine's answer through, or Magic's own
record where no engine answers. Its live checks passed. P7 is next; P6 was done ahead of P5's
remainder.

### P7 Deviation 1, 2026-11-04 — step 2 exists already, and the protocol guide. The owner decided: "Met by the opt-in hook" and "Add protocol-v1.md"

Read at P7's start (master `8f8e32ed` plus P5's staged live record):

* **Step 1 is open.**
  * `emitConfigOptions` (`acpagent/session.go:2369-2395`) walks `Options.Ungrouped` alone, so a
    grouped select reaches the phone with no values.
  * It forwards no category: `event.ConfigOption` has no field for one (`event/event.go:580-594`).
* **Step 2 exists.** 0214 P6c added `Spec.ModelsFromOption`, which builds the provider's catalog
  from the `model` option, grouped values included (`acpagent/modelcatalog.go:21-39`; its tests
  `TestModelOptionBecomesTheCatalog`, `TestModelOptionsKeepTheAgentsGroups`).
  * opencode sets it, and gobble's 0179 PLAN P1 step 3 does.
  * Making it every Spec's default would change how grok's catalog is built, from its initialize
    `_meta`.
* **Step 3 is open.** No `Spec.DangerousModeIDs`. Only the synthetic `auto` is marked `Dangerous`
  (`session.go:2245-2256`), which is what makes the phone confirm.
* **Out of the file list.** `session_config` is documented in `docs/guides/protocol-v1.md:1872`.
  The greenfield rule (Rules for every phase) changes the daemon, the phone and the guide together.

**Decided.**
* **"Met by the opt-in hook".** Step 2 is recorded as met by `Spec.ModelsFromOption`; no code
  changes for it.
* **"Add protocol-v1.md".** The guide documents the new fields in the same commit.
* **P7's files:**
  * `internal/provider/acpagent/session.go` and `acpagent.go`, and their tests;
  * `internal/event/event.go`;
  * `apps/mobile/lib/data/protocol/models.dart` and `apps/mobile/lib/features/chat/chat_screen.dart`,
    the phone's config-option rendering, and their tests;
  * `docs/guides/protocol-v1.md`;
  * this plan.

  `session_meta.go`, which the PLAN lists, is not needed.

### P7 Deviation 2, 2026-11-04 — the retention budget. The owner decided: "Also count value strings"

Found while adding the fields, before any change:

* **The budget counts strings unevenly.** `event.Bytes` (`internal/event/retention.go:186-190`)
  counts each config option's `id`, `name`, `description`, `kind` and `current_value` by length,
  but each value only by its struct's size. So a value's `id` and `name`, and now its `group`, are
  not counted.
* **Nothing would catch it.** `TestBytesTracksStructGrowth` fills `Event`'s own string fields, not
  the ones nested in a config option.
* `retention.go` is not in P7's list.

**Decided: "Also count value strings".**
* `Bytes` counts the option's new `category`, and each value's `id`, `name` and `group`, by length.
* The test is seen failing on each left out.
* **Files added to P7:** `internal/event/retention.go` and its test.

### P7 — 2026-11-04: standard ACP config options, and dangerous modes

**What changed.**
* **`internal/event/event.go`.** `ConfigOption` gains `Category` (`category`), and
  `ConfigOptionValue` gains `Group` (`group`), each omitted when empty.
* **`internal/provider/acpagent/session.go`.**
  * `emitConfigOptions` forwards every select's values through `selectValues`: ungrouped ones as
    before, then each grouped one with its group's name, or the group's id when the agent named
    none.
  * Each select and boolean option carries its category (`categoryOf`).
  * `advertisedModes` marks the Spec's `DangerousModeIDs` dangerous (`dangerousModes`), without
    regard to case, on a copy of the list: a Spec's static list is shared by every session. Both
    mode paths, the agent's and the static fallback, go through it.
* **`internal/provider/acpagent/acpagent.go`.** `Spec.DangerousModeIDs`. The agent enforces what the
  mode does. Magic marks it dangerous so the phone confirms it, as it confirms the synthetic `auto`.
* **`internal/event/retention.go`** (Deviation 2). `Bytes` counts an option's `category`, and each
  value's `id`, `name` and `group`.
* **The phone.**
  * `models.dart`: `ConfigOption.category` and `ConfigOptionValue.group`, parsed and part of
    equality, so a change to either is applied.
  * `chat_screen.dart`: the config sheet's picker catalog comes from a new top-level
    `configValuesCatalog`. It puts each value in its group, which the picker already renders as
    headers.
  * A value listed in two groups (a `recent` group and its provider's) is two rows. Rows are not
    keyed by id, so this is safe, and both show selected.
* **`docs/guides/protocol-v1.md`.** The `session_config` event documents `category` and a value's
  `group`.
* **Step 2** is met by 0214's `Spec.ModelsFromOption` (Deviation 1); no code.

**Tests, each seen failing** (`p7r/plant_go.py`, `p7r/plant_dart.py`, on scratch copies; the phone's
held only `lib`, `test`, `.dart_tool`, `assets` and the pubspec files):

| Break | Fails |
| --- | --- |
| grouped values skipped | `TestGroupedConfigOptionReachesThePhone`: `model values = []` |
| the group not set | the same: every value with `Group:` empty |
| no fallback to the group's id | the same: `openai/gpt-z` with no group |
| the category dropped | the same: `category ""` |
| `DangerousModeIDs` not applied | `TestDangerousModeIDsMarkTheAgentsModes`: bypass `Dangerous:false` |
| matched with case | the same: the static `Bypass` not marked |
| marking the shared static list | the same: "marking wrote through to the Spec's shared static list" |
| `Bytes` without the category; without a value's id, name or group | `TestBytesCountsConfigOptionStrings`: "adds 0 bytes, want 13", once for each |
| the phone not parsing `group`; not parsing `category` | `config_option_groups_test.dart`: `Actual: ['', '', '', '']`; `Actual: ''` |
| the phone's equality ignoring `category`; ignoring `group` | the same file: "a changed category or group is a change": `Actual: <true>`, once for each |
| the picker catalog dropping the group | the same file: `Actual: ['/anthropic/claude-x', …]` |

`TestNoDangerousModeIDsLeavesModesAlone` pins that a Spec without the field advertises the agent's
modes unchanged.

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (6 Go files) | "6 file(s) clean (gofmt, golint, govulncheck)" |
| `dart format` on the three Dart files | applied; the formatter changed only this phase's lines |
| `make preflight` | exit 0; 47 packages ok, no race; 0 of 231 files changed by `dart format`; no analyzer issues; 1513 phone tests passed |

The first `make preflight` failed `TestNoProviderIsNamedInAcpagent`: the new test's fixture comment
named the agent whose shape it follows (`configgroups_test.go:14`). The comment was made neutral, and
the second run is the one above.

**The MADR's Confirmation:** "a grouped config option fixture reaches the phone with its values and
category" is met: `TestGroupedConfigOptionReachesThePhone`, and the phone's parse and render tests.

**Not done:**
* No live run: P7 names none, and no Spec sets `DangerousModeIDs` yet. 0179 P3 sets gobble's
  `bypass`.
* The fake provider's config options carry no category; it was not changed.

P7 is complete with this commit. P8, one provider descriptor, is next.

**Files for this commit:**
* `internal/event/event.go`, `retention.go` and `retention_test.go`;
* `internal/provider/acpagent/acpagent.go` and `session.go`, and `configgroups_test.go` (new);
* `apps/mobile/lib/data/protocol/models.dart`, `apps/mobile/lib/features/chat/chat_screen.dart` and
  `apps/mobile/test/config_option_groups_test.dart` (new);
* `docs/guides/protocol-v1.md`;
* this plan, which also carries P5's live checks.

### P8 Deviation 1, 2026-11-04 — where the sections live, strict keys, and the files that agree. The owner decided: "Provider pkgs, explicit list", "Add them all", "Defer to P9", "Fail the load by name", "Re-measure on 1.0.70 first", "New pair after P8" and "Add them"

**Found** (the MADR's amendment of 2026-11-04 has the evidence):
* Step 1 did not say where a section lives or how the loader learns it. D6's auth adapter is in a
  file the phase did not list.
* Step 3's count was 74; `daemon.go` has 65 named provider reads on `02463a8a`.
* Asked whether keys may change, the owner directed that the templates, the generated config and
  the guide agree with the code, retired keys removed, and every key commented.

**Decided:**
* Each provider package owns its section; a registration list returns the descriptors;
  `config.Load` takes their sections through its options.
* Sections are strict: an unknown key, provider id or `MCREMOTE_PROVIDERS_*` variable fails the
  load by name. The retired-key warnings are deleted with the section that carried them.
* The icon key moves to P9.
* No provider key is renamed. grok's four filter keys are measured on grok 1.0.70 before grok's
  commit (a live run, approved by this answer), and kept with a pin or removed.
* The example configs, the seed and the guide change with each section, and a test loads them
  through the sections.
* Keys outside `providers.*` go to a new pair after P8.
* The owner's live config is backed up and migrated in the commit that changes its shape, after
  the owner is asked by file.

**Files added to P8:**
* `internal/provider/descriptor.go` (new), and a registration-list package (new);
* `internal/provider/prewarm.go` and `internal/daemon/credentials.go`;
* the `config.Load` callers `internal/cli/serve.go`, `paths.go`, `health.go` and `pair.go`;
* the tests that read named provider fields: `internal/daemon/daemon_test.go`,
  `prewarm_boot_test.go`, `shutdown_unix_test.go`, `notify_unix_test.go`,
  `internal/cli/service/template_parity_test.go`, and those in `internal/config/`;
* `configs/config.example.yaml`, `configs/config.prod.example.yaml`,
  `internal/cli/service/defaults_mcremote.yaml` and `docs/guides/config.md`;
* a superseded-by note in `0160-MADR-remove-goose-cli-support.md` and in
  `0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md`, each in the
  commit that deletes its warning.

**Commits, in order:**
1. `fake`: the descriptor, the registration list, the strict-section loader, and the test that
   loads the config files; fake's section moves.
2. `grok`, after the measurement: its section, its auth adapter, and the four keys' outcome.
3. `opencode`: its section; the 1.x and `transport` warnings go.
4. `codex`: its section and its auth adapter.
5. `kilo`: its section; the goose warning goes, an unknown provider id fails, and `daemon.go` reads
   no named provider field.

### P8 Deviation 2, 2026-11-04 — two more files carry provider keys. The owner decided: "Add both"

**Found:**
* `configs/config.mesh-grok.yaml` is a shipped mcremote config, which this phase's new test loads.
* `README.md` has a `providers.*` key table, and its codex row already lacks `transport` and seven
  more keys.

**Decided:** both join P8, and each provider's commit keeps them in step with its section.

### P8, `fake` commit, 2026-11-04: the descriptor, the registration list, and strict sections

**What changed.**
* **`internal/config/sections.go`** (new).
  * `ProviderSection` is a section a provider's package owns. `ProviderSectionSpec` names one and
    builds it holding its defaults.
  * `config.Load` takes the specs in `LoadOptions.Providers`. It gives each leaf key its default,
    and so its `MCREMOTE_PROVIDERS_<ID>_<KEY>` variable, except a field tagged `env:"-"`. It decodes
    each section into `Config.Providers.Sections`, then runs the section's `Validate`.
  * A key the section lacks fails the load and is named, whether it is in the file, in a nested
    table, or in a variable. A variable for a config-file-only key fails the same way.
* **`internal/provider/descriptor.go`** (new). `provider.Descriptor` carries the ID, display name,
  `DevOnly`, `Section` and `New`. `SectionIn` returns a Config's section, or the defaults when a
  Config was not built by `config.Load`.
* **`internal/provider/registered`** (new). `Descriptors()` is the registration list, and
  `Sections()` its specs. It lists `fake` only.
* **`internal/provider/fake/section.go`** (new): `providers.fake` and fake's descriptor.
  `FakeProviderConfig`, its default and its viper default are gone from `internal/config`.
* **`internal/daemon/daemon.go`.** `registerDescribed` builds each enabled described provider, for
  both `Run` and `InspectProviders`. The dev-only warning and the "no provider is ready" count come
  from the list. The four other providers are unchanged.
* **`internal/cli`.** All five `config.Load` callers (`serve.go`, `paths.go`, `health.go`,
  `pair.go`, `doctor.go`) pass `registered.Sections()`.
* **The shipped configs.** All four carry the same section comment and a comment on `enabled`.
  The prod example and the seed had neither.
* **Tests.** `config_test.go`, `notify_unix_test.go` and `shutdown_unix_test.go` set fake through
  its section.

**Tests, each seen failing** (`p8/plant_c1.py`, on a scratch copy):

| Break | Fails |
| --- | --- |
| no section defaults | `TestASectionLoadsFromItsDefaultsFileAndEnv`: `Enabled:false … Max:0` |
| unknown keys ignored | `TestASectionRefusesWhatItDoesNotHave/a_key_the_section_lacks`: `Load error = <nil>` |
| nested tables not walked | the same, `/a_key_in_a_table_the_section_lacks` |
| a variable naming no key accepted | the same, `/a_variable_naming_no_key` |
| a variable for a config-file-only key accepted | the same, `/a_variable_for_a_config-file-only_key` |
| the section's `Validate` not run | the same, `/the_section's_own_validation` |
| `enable: true` planted in the example config's `fake` | `TestTheShippedConfigFilesLoadThroughTheSections`: "providers.fake has no key enable" |
| `MCREMOTE_PROVIDERS_FAKE_ENABLE` planted in the guide | `TestEveryDocumentedProviderVariableNamesAKey`: "names no providers.fake key" |
| `registerDescribed` ignoring `IsEnabled` | `TestADescribedProviderRegistersWhenItsSectionIsEnabled`: "fake is registered from its defaults" |
| `fake` missing from the list | the same: `unknown provider "fake"` |

The first run of the plant script broke the variable-name check by deleting its `case`, which
made the next case index `leaves[-1]` and panic. It was planted again with the case kept and its
report removed, and failed on the assertion.

The first `make ci-windows` failed `TestTheShippedConfigFilesLoadThroughTheSections` on all four
files: "contains a credential and is readable by" a group the host's temp directory grants. The
shipped configs carry `relay.secret`, and a Windows temp file inherits its directory's ACL whatever
its mode (MADR 0155). The test now writes into a directory made private with
`appdirs.EnsurePrivateDir`, as `internal/config/configperm_test.go` does. The second run is the
one below, and the plants were run again against the changed test.

The documented-variable test checks only registered providers; the guide documents no fake
variable, so it checks none yet and grows with each section.

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (18 Go files) | "18 file(s) clean (gofmt, golint, govulncheck)" |
| `go test -race ./...` | 48 packages ok |
| `make lint staticcheck vet-tags` | exit 0 |
| `make ci-windows` on the Windows test host | all selected checks passed (A2 to A6, 48 packages), on the second run |
| the owner's live config, loaded by a scratch build (`mcremote paths`) | exit 0: no migration |

**Not done here:** the other four providers' sections and the unknown-provider-id check (commits 2
to 5); the guide's note on strict sections, which lands when every section is strict.

**Files for this commit:**
* `internal/config/sections.go` and `sections_test.go` (new), `config.go`, `load.go` and
  `config_test.go`;
* `internal/provider/descriptor.go`, `internal/provider/fake/section.go`, and
  `internal/provider/registered/registered.go` and `configfiles_test.go` (all new);
* `internal/daemon/daemon.go`, `daemon_test.go`, `notify_unix_test.go` and `shutdown_unix_test.go`;
* `internal/cli/serve.go`, `paths.go`, `health.go`, `pair.go` and `doctor.go`;
* `configs/config.example.yaml`, `config.prod.example.yaml` and `config.mesh-grok.yaml`, and
  `internal/cli/service/defaults_mcremote.yaml`;
* this plan and the MADR's amendment of 2026-11-04.

Next: grok's commit, after the four filter keys are measured on grok 1.0.70.

### P8 Deviation 3, 2026-11-04 — cli tests read the host's config. The owner decided: "Make them hermetic" and "Migrate both"

**Found.** The first `make ci-windows` of grok's commit failed 13 tests in `internal/cli`, each with
"providers.grok has no key allow_rules, allowed_tools, deny_rules, disallowed_tools":
`TestPairListShowsKeyColumn` (`pair_test.go:388`), eight `TestReceipts*` tests and four
`TestAuthRecovery*` tests. They pass `--data-dir` and no config, so `config.Load` read the
test host's own config, and the Windows test host's still had the four keys. This predates P8: these
tests have always loaded the host's config, and grok's strict section exposed it. On this host they
had passed only because the owner's config had just been migrated.

**Decided:**
* `hermeticConfig` (`receipts_test.go`) points `MCREMOTE_CONFIG` at an empty file for the test.
  `receiptsFixture`, `runAuthRecovery` and `TestPairListShowsKeyColumn` call it.
* The Linux and Windows test hosts' configs are backed up and migrated, as the owner's was.

**Files added to P8:** `internal/cli/receipts_test.go`, `auth_recovery_test.go` and `pair_test.go`.

### P8, `grok` commit, 2026-11-04: grok's section, its auth adapter, and the four filter keys removed

**The measurement** (owner: "Re-measure on 1.0.70 first") is in the MADR's note of this date: no key
changed the outcome, so all four are removed.

**What changed.**
* **`internal/config`.**
  * `AgentCommon` (new, `config.go`) holds the keys every agent has: `enabled`, `bin`,
    `always_approve`, `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm` and
    `turn_stall_notice_seconds`. It gives a section `IsEnabled`, `Agent` and `ValidateAgent`.
    `ACPProviderConfig` embeds it squashed, and marks `args`, `fs_roots` and `mcp_servers`
    config-file only.
  * `AgentSection` (`sections.go`) is a section that embeds it. `finalizePaths` resolves each one's
    `default_cwd`, and the prewarm toggle (`prewarm_write.go`) reaches it.
  * `GrokProviderConfig`, grok's defaults, viper defaults and validation are gone, with
    `validGrokPermissionMode`. `ValidateACPProvider` and `MaxStreamCoalesceMs` are exported for
    sections to use. A stray comment line above `ValidateACPProvider` was removed when golint read
    it as the exported function's doc.
  * A section's `Validate` errors are no longer wrapped: a section names its keys in full,
    `providers.<id>.<key>`, as `Config.Validate` does.
* **`internal/provider/grok/section.go`** (new): `providers.grok`, its defaults (the pinned
  `permission_mode` and its reason moved with it), validation, the mapping to grok's provider
  config, and grok's descriptor, whose auth adapter makes the daemon build the coordinated provider.
* **`internal/provider/acpagent/fromconfig.go`** (new). `ConfigFrom` is the daemon's
  `acpAgentConfig`, moved so every ACP section shares it. Its tests moved with it.
* **The four filter keys.** They are gone from `acpagent.Config`, grok's argv and `ModelArgs`,
  `grok_test.go`, `live_argv_test.go`, and `live_policyflags_test.go`'s flag list.
  `live_toolpolicy_test.go` (new) is the pin.
* **`internal/provider/descriptor.go`.** `Descriptor.Credentials`, and `Build.Version`,
  `Build.Credentials` and `Build.Busy`. `prewarm.go` exports `Warm`.
* **`internal/daemon`.**
  * `registerDescribed` builds each described provider with its coordinator and live-session count.
    It warns when the binary is missing, and warms it when its section asks.
  * `credentialAdapters` (`credentials.go`) builds the guard's adapters from the descriptors, plus
    codex until codex's commit. `newCredentialGuard` takes them.
  * `prewarmPlan` reads described sections.
  * `grokConfig` and `acpAgentConfig` are gone.
* **`internal/cli/doctor.go`.** `engineBins` takes a described agent's binary from its section.
* **The config files.** All four shipped configs carry one identical `providers.grok` block, every
  key commented, and a note that the four keys are gone. The guide's four rows became one "Removed"
  row. Its note now records both measurements and the pin. `README.md`'s key table and grok table
  dropped the keys.

**Tests, each seen failing** (`p8/plant_c2.py` with `p8/plant_c1_harness.py`, on scratch copies):

| Break | Fails |
| --- | --- |
| `allowed_tools` back in grok's section | `TestSectionRefusesTheRemovedFilterKeys/allowed_tools` |
| no `ValidateAgent` call | `TestSectionValidate/negative_stall` |
| no `stream_coalesce_ms` range check | `TestSectionValidate/stream_coalesce_too_long` |
| no `reasoning_effort` check | `TestSectionValidate/whitespace_reasoning_effort` |
| no `ValidateACPProvider` call | `TestSectionValidate/an_MCP_server_without_a_url` |
| no `permission_mode` check | `TestSectionValidate/unknown_permission_mode` |
| `permission_mode` defaulting empty | `TestSectionDefaults` |
| `sandbox` not copied to the provider | `TestSectionReachesTheProvider` |
| the coordinator ignored | `TestDescriptorCoordinatesWhenGivenACoordinator` |
| `AgentCommon` not squashed | `TestSectionLoadsThroughConfig` |
| `fs_roots` settable from a variable | `TestACPListKeysAreConfigFileOnly/MCREMOTE_PROVIDERS_ACP_FS_ROOTS` |
| an MCP `stdio` transport accepted | `TestACPMCPTransportValidation` |
| adapters built for a disabled provider | `TestCredentialAdaptersFollowTheSections` |
| grok missing from the list | the same: "grok's defaults enable it, but it has no credential adapter" |
| a disabled agent prewarmed | `TestPrewarmPlanReadsADescribedAgentsSection` |
| the prewarm toggle missing the section | `TestPrewarmTogglesADescribedAgentsSection` |
| doctor ignoring a described agent's binary | `TestEngineBinsReadADescribedAgentsSection` |
| `default_cwd` not resolved for a section | `TestSectionDefaultCWDIsResolvedAgainstTheConfigFile` |
| `deny_rules: []` planted in the seed | `TestTheShippedConfigFilesLoadThroughTheSections`: "providers.grok has no key deny_rules" |
| `MCREMOTE_PROVIDERS_GROK_ALLOWED_TOOLS` planted in the guide | `TestEveryDocumentedProviderVariableNamesAKey` |
| the pin's `deny` case expecting a prompt | the live pin, one turn: `TestLiveGrokToolPolicyFlagsStillHaveNoEffect/deny`: "no prompt under --deny: grok may now honour it over ACP" |

Deviation 3's fix was seen working (`p8/plant_c2_hermetic.py`). With a host config holding
`providers.grok.allowed_tools`, 13 tests failed without `hermeticConfig` and none failed with it.

The shipped-config test caught the templates before they were edited: all four configs failed with
"providers.grok has no key allow_rules, allowed_tools, deny_rules, disallowed_tools".

**My mistakes, each fixed before a gate ran:**
* An over-wide cut in `config_test.go` deleted five unrelated tests, from
  `TestResolveListenHostTailscaleSentinel` to `TestLoadWithoutTransportKeySucceeds`. They were
  restored from `HEAD` (`p8/restore_c2_cfgtests.py`), and the file's diff is grok's lines only.
* Grok's section test cleared `MCREMOTE_*` after it had set its own variable, and failed. Clearing
  now comes first.

**Live.**

| Run | Result |
| --- | --- |
| the measurement (`p8/grok_probe_live_test.go`, scratch only, never committed) | six configurations, table in the MADR |
| `TestLiveGrokToolPolicyFlagsStillHaveNoEffect` | passed: `disallowed-tools`, `tools` and `deny` each wrote `probe.txt` with 0 prompts; `allow` prompted once and wrote nothing |
| `make live-grok` (both parts, which include the pin and `TestLiveGrokArgvAcceptsEveryConfiguredFlag`) | passed: both parts ok (356 s and 542 s), 248 passes, 0 failures, 1 opt-in skip (`TestLiveRawOutputSamples`) |

**The live configs.** This host's, the Linux test host's and the Windows test host's each held the
four keys, one line each. Each was copied to `config.yaml.bak-0208-p8` beside it, and those lines
were deleted (`p8/migrate_live_grok.py`, then `p8/migrate_grok_keys.py` over ssh; no value
printed). Each then loaded with the installed build, and with this commit's code (`mcremote paths`).

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (33 Go files) | "33 file(s) clean (gofmt, golint, govulncheck)", after the stray comment above |
| `go test -race ./...` | 48 packages ok |
| `make lint staticcheck vet-tags` | exit 0 |
| `make ci-windows` on the Windows test host | all selected checks passed (A2 to A6, 48 packages), on the second run; `go run ./cmd/mcremote paths` there loaded the migrated config, exit 0 |

**Not done here:**
* ~~`internal/cli/auth_recovery.go:80-83` still lists grok's and codex's credential adapters by
  hand. It is outside P8's list.~~ *Done in this commit: P8 Deviation 4.*
* ~~`TestGrokClearMethodsAreIndependent` (`methods_test.go`, unchanged) starts the installed grok to
  read its model catalog, outside the live tag. This was seen in the logs and predates P8.~~ *Done
  in this commit: P8 Deviation 4.*

**Files for this commit:**
* `internal/config/config.go`, `load.go`, `prewarm_write.go` and `sections.go`, and their tests
  `acp_config_test.go`, `config_test.go`, `prewarm_write_test.go`, `retired_goose_test.go`,
  `sections_test.go` and `viper_defaults_test.go`;
* `internal/provider/descriptor.go`, `prewarm.go` and `registered/registered.go`;
* `internal/provider/acpagent/config.go`, and `fromconfig.go` and `fromconfig_test.go` (new);
* `internal/provider/grok/grok.go`, `grok_test.go`, `live_argv_test.go` and
  `live_policyflags_test.go`, and `section.go`, `section_test.go` and `live_toolpolicy_test.go` (new);
* `internal/daemon/daemon.go`, `credentials.go`, and `daemon_test.go`, `credentials_test.go`,
  `notify_unix_test.go`, `shutdown_unix_test.go` and `prewarm_boot_test.go`;
* `internal/cli/doctor.go`, `doctor_engines_test.go`, `receipts_test.go`, `auth_recovery_test.go`,
  `pair_test.go` and `service/template_parity_test.go`;
* the four shipped configs, `docs/guides/config.md` and `README.md`;
* this plan and the MADR's note.

Next: opencode's commit, in which the 1.x and `transport` warnings go.

### P8 Deviation 4, 2026-11-04 — the grok commit's two leftovers. The owner decided: "address these now"

**Found** (the grok record's "Not done"):
* `internal/cli/auth_recovery.go` built its own adapters: codex's and grok's, by hand, whether
  enabled or not, and codex's without the configured binary. Its comment says it builds "the
  same adapters … `serve` would", and MADR 0134 makes codex's binary required. The error text named
  the two providers by hand too.
* `TestGrokClearMethodsAreIndependent`'s first subtest built grok's provider on `Bin: "grok"`.
  Clearing the API key resolves the credential model through `ListModels`, which on a ready provider
  spawns the agent to read its catalog. The unit test started this host's installed grok 1.0.70,
  which signed in. It was the only grok unit test in `go test -v` that reached the binary.

**Changed** (in the grok commit):
* `internal/daemon/credentials.go`: `credentialAdapters` is exported as `CredentialAdapters(cfg,
  enabledOnly)`. serve passes `true`. `auth-recovery` passes `false`, so a provider disabled since
  its credential was stored can still be inspected and recovered, as before.
* `internal/cli/auth_recovery.go` builds its coordinators from `daemon.CredentialAdapters`. Codex's
  adapter now gets the configured binary, as serve's does. The "unknown provider" error lists the
  providers it has.
* `internal/cli/auth_recovery_test.go`: `runAuthRecoveryWith` writes the test's own providers
  section, and `TestAuthRecoveryReachesADisabledProvider` is new.
* `internal/provider/grok/methods_test.go`: the subtest's provider names a binary that does not
  exist, so it is not ready and uses grok's static catalog.

**Seen failing and seen working** (`p8/plant_dev4.py`, on scratch copies):

| Break or experiment | Result |
| --- | --- |
| `auth-recovery` passing `enabledOnly` true | `TestAuthRecoveryReachesADisabledProvider`: "status with both disabled = \"\", want both listed" |
| a descriptor's adapter only while enabled | `TestCredentialAdaptersFollowTheSections`: "auth-recovery has no adapter for a disabled grok" |
| codex's adapter only while enabled | the same: "no adapter for a disabled codex" |
| the grok subtest as it was | passes, and logs "spawning a short-lived agent to read the model catalog" |
| the grok subtest as fixed | passes, and starts no agent |
| a recording stub configured as codex's binary | all `TestAuthRecovery*` pass, and the stub never runs |

**My mistake.** I first gave the `auth-recovery` tests a codex binary that did not exist, on the
assumption that the configured binary would now run. The stub experiment showed no test path runs
it, so that change and its comment were dropped before any gate.

**Gates.** `make pre-add-check` on the six changed Go files: "6 file(s) clean". `go test -race ./...`:
48 packages ok. `make lint staticcheck vet-tags`: exit 0. `make ci-windows` on the Windows test host:
all selected checks passed (A2 to A6, 48 packages).

**Files added to P8:** `internal/cli/auth_recovery.go` and `internal/provider/grok/methods_test.go`.
