---
status: proposed
date: 2026-10-21
decision-makers: Project Owner
consulted: "this repository at f26549de (Go under internal/ and cmd/, Dart under apps/mobile/lib); gobble-cli at 1cc1616 and its v0.1.1 release, read from code; MADR 0179, 0184 and 0186 and their PLANs"
informed: anyone who adds an agent CLI, changes internal/provider, internal/command, internal/session or internal/ws, or changes the phone's chat and settings screens
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Make the provider layer agent-neutral: declared capabilities, vendor code only in its vendor's package, one registration point

## Context and Problem Statement

On 2026-10-21, after an assessment of what native gobble support would take, the owner said:
"magic-cli-remote is too hardwired into grok. It needs to be more idempotent and optimized for the
full range of agent cli". This record reads "idempotent" as *agent-neutral*: the shared layers
behave the same whichever agent CLI is behind them, and a feature appears because the agent
offers it, never because of which vendor it is. The owner can correct that reading (Q1).

Magic runs five providers over three transports: grok over ACP (`internal/provider/acpagent`),
opencode and kilo over HTTP (`internal/provider/httpagent`), codex over its own app-server
protocol (`internal/provider/codex`), and `fake`. gobble would be the second ACP agent. The
standing parity rule is that no provider is primary and every one is equally optimized.

The question is what has to change so that adding or improving an agent CLI is a matter of that
agent's own package, and the phone and the shared daemon layers never need to know its name.

### Findings [measured 2026-10-21 on f26549de]

**N1 — The "generic" ACP adapter is grok's adapter.**

* `acpagent` names grok or its `x.ai` wire on 194 production lines. That is 47% of the 416 grok
  lines outside `internal/provider/grok`.
* Eight outbound vendor calls, in seven operations, are methods of the shared session, so they
  exist for every ACP agent:
  * `Compact` → `x.ai/compact_conversation` (`sessioncaps.go:245-253`);
  * `Rename` → `x.ai/session/rename` (`sessioncaps.go:256-268`);
  * `Fork` → `_x.ai/session/fork` (`fork.go:44`);
  * `UndoLast` → `x.ai/rewind/points`, then `x.ai/rewind/execute` (`rewind.go:96`, `:108`);
  * `RuntimeUsage` → `x.ai/session/usage` (`runtime.go:94`), and `RuntimeStatus` → `x.ai/billing`
    (`runtime.go:229`);
  * `confirmLimit` → `x.ai/billing`, run on **every** classified quota error of any ACP session
    (`quota.go:48`, called from `session.go:719`).
* Two incoming grok requests, `_x.ai/exit_plan_mode` and `_x.ai/ask_user_question`, are handled by
  the shared session for every agent (`extensions.go:29-30`, `:82`).
* Auth method selection knows only grok's ids, `cached_token` and `xai.api_key`
  (`authselect.go:17-41`).
* `sessioncaps.go:24` still says "This package is grok only."
* Two patterns already point the right way. Incoming grok notifications are registered per Spec
  (`internal/provider/grok/grok.go:104-116`). Model and thinking use the standard config option
  first and a grok fallback hook only when the agent offers none (0184 D6; `acpagent.go:148-163`).

**N2 — Capability is inferred from Go types, not declared.** `commandContext` decides which
operations a session has by type assertion (`internal/session/commands.go:66-80`). A Go type has
every method its adapter defines, so every ACP session reports compact, fork, undo, rename, usage
and status. The resolver then falls back to those operations (`internal/command/command.go:228-261`).
For an ACP agent other than grok, the phone shows `/compact` as available and the daemon sends
`_x.ai/compact_conversation`. This is 0179's F1, still open.

**N3 — Standard ACP data is dropped where grok never sent it.** Only ungrouped select options are
forwarded, and the option category is not (`session.go:2190-2235`). An agent with a grouped model
option, such as gobble (gobble-cli `acpserver/config.go:213-233`), reaches the phone with an empty model list.

**N4 — The phone branches on provider names.** Ten places in Dart compare the provider id to a
literal, deciding menus, plan mode, diagnostics, terminals and destructive-auth wording:
`chat_screen.dart:447`, `:2624`, `:2627`, `:2629`, `:2635`; `provider_detail_screen.dart:201`,
`:383`, `:579`, `:727`; `sessions_screen.dart:679`. The protocol names a vendor too: the server
capability is `codex_surface` (`internal/protocol/messages.go:199-275`).

**N5 — The daemon branches on provider names.** `defaultProviderID` prefers a ready grok, then
fake, then the first ready provider (`internal/ws/server.go:3500-3520`). The session manager
special-cases codex's approvals reviewer (`internal/session/manager.go:1499`, `:1505`).

**N6 — Registering a provider touches many files.** Configuration is one typed struct per provider
(`internal/config/config.go:449-454`, structs at `:458-680`), and `internal/daemon/daemon.go` reads
those fields 74 times. 0179 D8 lists eight places a new provider must be added: the ID, the config
struct and its environment variables, prewarm's known IDs, the daemon, `doctor`, the conformance
lists, the phone's icon manifest and screens, and the protocol guides.

**Not a finding: name counts alone.** Outside its own package grok is named on 416 production
lines, codex on 425, opencode on 314 and kilo on 139. Most of those are registration, config and
each vendor's own phone surface. The coupling that matters is N1–N6: shared code that *behaves*
differently by vendor, or that assumes one vendor's wire.

## Decision Drivers

* Parity: no provider is primary, and every one is equally optimized and hardened. *(2026-10-21:
  gobble becomes the default selection; parity of support is unchanged. See the owner answers
  on Q2.)*
* *(Added 2026-10-21 by the owner.)* API compatibility: ~~the phone protocol stays backward
  compatible, and~~ each agent's published API is used as published. *(Narrowed the same day:
  greenfield, one phone user; see the owner direction below.)*
* *(Added 2026-10-21 by the owner.)* Native integration: gobble's whole surface is first-class.
* 0023's rule: a command the phone shows as available must execute.
* Standards first: where ACP (or any agent's documented protocol) defines a mechanism, use it;
  a vendor method is a fallback behind a hook in that vendor's package (0184 D6).
* Adding an agent CLI should be one package plus its registration, with no edits to the phone's
  logic or the shared session layers.
* No regression for grok, opencode, kilo or codex: their tables, live suites and wire captures
  stay green.
* Every new gate is seen failing first.

## Considered Options

* **A.** Declared capabilities, vendor code only in its vendor's package, standards-first ACP,
  capability-driven phone, one provider descriptor
* **B.** 0179's D1–D3 only: move grok's operations into its Spec and add strict fallback for ACP
* **C.** A separate adapter per ACP agent, copied from `acpagent`
* **D.** Keep the structure, and register each new agent with workarounds

## Decision Outcome

Chosen option: **A**, because it removes the cause rather than one symptom. B fixes the ACP
adapter but leaves the phone, the daemon and registration vendor-aware, so the next agent CLI
meets the same walls. C duplicates hardened transport code. D accumulates the debt this record
exists to pay.

The decision has eight parts.

**D1 — Sessions declare their capabilities.**

* `provider.Session` gains `Capabilities() provider.Capabilities`, a value listing the operations
  the session supports now (compact, fork, undo, rename, usage, status, diff, model, thinking,
  execution, steer, and so on).
* `commandContext` and every other availability check read that value. A type assertion stays
  only as the way to *call* an operation the session declared.
* Each adapter declares honestly:
  * `acpagent` from the Spec's operation hooks (D2) and what `initialize` negotiated;
  * `httpagent` and `codex` from what their engines report, or statically where an engine's
    surface is fixed;
  * `fake` from its configuration.
* With honest declarations, the resolver's fallback to a spec default is safe: a default operation
  is available only when the session declared it. 0179 D3's per-provider strict flag is therefore
  not needed, and is not built.

**D2 — Vendor behaviour lives only in its vendor's package.**

* `acpagent.Spec` gains an operations value (0179 D1's `SessionOps`, extended with
  `ConfirmLimit`). A nil field means the operation is not offered.
* grok's seven operations (N1) move to `internal/provider/grok` as that Spec's operations,
  unchanged on the wire.
* The incoming `_x.ai/exit_plan_mode` and `_x.ai/ask_user_question` handlers are registered per
  Spec, as grok's notifications already are.
* Auth method selection becomes a Spec policy. grok's Spec carries today's preference order.
* After this, `acpagent` names no vendor method. A source test fails if a vendor wire name
  (`x.ai`, `_gobble/`, and any later vendor prefix) appears in `acpagent`'s production code.

**D3 — ACP features follow the standard first.**

* Config options forward every select shape, grouped and ungrouped, with the option's category.
* The model catalog and the current model come from the option of category `model`, the thinking
  level from `thought_level`. grok's `_meta` harvesting stays as its fallback hook (0184 D6).
* `Spec.DangerousModeIDs` marks agent modes that need the phone's confirmation gate (0179 D7).
* Standard session updates already mapped by 0187–0192 stay as they are.

**D4 — The phone gates on capabilities, never on provider ids.**

* Each session's capabilities (D1) and each resolved command's mechanism (0179 D4) reach the phone
  in the session payload.
* The ten Dart branches in N4 are replaced by capability checks. A Dart test fails if
  `lib/` compares a provider id to a literal.
* `codex_surface` gains a vendor-neutral name for the execution surface it describes. The old
  field stays accepted for one protocol version, so an older phone keeps working (Q3).
* Provider-specific *content* (icons, display names, help text) stays data the daemon sends or the
  icon manifest holds. Only behaviour is forbidden to branch on the id.

**D5 — The daemon branches on capabilities, never on provider ids.**

* The default provider is configuration (`default_provider`). When unset, it is the first ready
  provider in the configured order (Q2).
* codex's approvals-reviewer handling in the session manager becomes a capability the codex
  session declares.
* A Go test fails if code outside `internal/provider/<id>/`, the registry and the config loader
  compares a provider id to a constant.

**D6 — A provider registers through one descriptor.**

* Each provider package exports a `provider.Descriptor`: its ID, display name, config section and
  its decoder, defaults and validation, constructor, prewarm, `doctor` probe, auth adapter and icon
  key.
* The daemon, `doctor`, prewarm, config validation and the conformance suite iterate the
  registered descriptors.
* The config file format does not change: `providers.<id>` keeps its keys and environment
  variables. Only where the Go struct lives changes.
* Adding an agent CLI is then its package and one line in the registration list.

**D7 — One conformance suite for every provider.**

* A provider-neutral suite runs against every registered provider with fixtures, and against a
  fixture ACP agent that offers no vendor extension at all:
  * every command the phone sees as available executes;
  * a recorded connection carries no other vendor's method;
  * declared capabilities match what the session actually answers.
* Each provider's live suite stays its own, and spends quota only on the owner's word.

**D8 — 0179 narrows to gobble.** This record takes over 0179's D1–D3 and D7 as general
decisions. 0179, when rewritten for gobble (0186 Q2), keeps only gobble's Spec, its `_gobble/*`
operations, its auth hooks, its command table and its registration gates, and depends on this
record.

### Consequences

* Good, because a new agent CLI needs its own package and a descriptor, and nothing in the phone
  or the shared session layers.
* Good, because N2's leak closes for every ACP agent at its cause: nothing is available unless it
  was declared.
* Good, because grok keeps every behaviour it has today, owned by grok's package.
* Good, because the phone's behaviour becomes testable from capability fixtures without naming a
  vendor.
* Neutral, because the config file and the existing protocol fields stay compatible; only Go
  types move and protocol fields are added.
* Bad, because it touches the provider interface, every adapter, the resolver, the session
  manager, the WebSocket protocol and the phone. It must land in phases, each green on its own.
* Bad, because declared capabilities can drift from behaviour. D7's suite is the guard, and it is
  only as good as its fixtures.

### Confirmation

* A resolver test: a fixture ACP Spec with no operations reports none, and `/compact`, `/fork`,
  `/undo`, `/thinking`, a phone fork and a phone rename send no `_x.ai/*` frame. Seen failing on
  today's code first.
* The source tests of D2, D4 and D5 are each seen failing on today's tree, run on a scratch copy.
* A grouped config option fixture reaches the phone with its values and category.
* grok's, opencode's, kilo's and codex's unit, conformance and wire suites pass unchanged. Their
  live suites pass at acceptance, with the owner's approval for quota.
* Adding the fixture ACP agent as a provider touches only its package and the registration list.

## Pros and Cons of the Options

### A — Declared capabilities, vendor code in its package, standards first, capability-driven phone, one descriptor

* Good, because every vendor assumption gets one home, and the shared layers can be tested
  against a neutral fixture.
* Good, because it reuses patterns already in the tree: per-Spec notification handlers and 0184
  D6's standard-first hooks.
* Bad, because it is the largest change, across Go and Dart.

### B — 0179's D1–D3 only

* Good, because it is contained to `acpagent`, the resolver and `commandContext`.
* Neutral, because it is the first phase of A anyway.
* Bad, because the phone's ten id branches, the daemon's default and codex branches, and the
  eight-place registration stay.

### C — A separate adapter per ACP agent

* Good, because grok's code is untouched.
* Bad, because it copies transport, supervision and teardown code that 0166 and 0167 hardened, and
  every fix then lands twice.
* Bad, because the phone and the daemon stay vendor-aware.

### D — Keep the structure, register with workarounds

* Good, because it costs nothing now.
* Bad, because each agent adds more id branches and another copy of the registration checklist.
* Bad, because N2 stays a live defect for every ACP agent after grok.

## More Information

### Questions for the owner

1. **The reading of "idempotent".** This record takes it as agent-neutral behaviour. If you meant
   something else as well, such as operations that are safe to repeat (reconnect, retry, resume),
   that is a separate decision. *Answered 2026-10-21; see the amendment below, which governs.*
2. **The default provider.** Configuration with "first ready in configured order" as the default
   changes today's grok preference when `default_provider` is unset. Keep grok first in the
   default order, or make the order purely the operator's?
3. **Protocol compatibility.** One protocol version of overlap for `codex_surface`, or a protocol
   version bump that phones must meet?
4. **Order against gobble.** Register gobble after D1 and D2 (the safety part), or after all of
   D1–D7?

### Suggested phases (the PLAN fixes them)

1. D1 and D2 in Go, with the failing-first resolver and source tests. Nothing visible changes.
2. D3: config options, categories, dangerous modes.
3. D6: descriptors, provider by provider, the config format unchanged.
4. D5 and D4: daemon and protocol, then the phone, with Flutter tests and `dart format`.
5. D7's suite, then gobble under the rewritten 0179.

### Related records

* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) (proposed):
  its D1–D3 and D7 are generalized here (D8).
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  D6's standard-first hooks are the pattern D2 and D3 extend.
* [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md):
  Q2 (0179 renamed for gobble) and Q3 (client capabilities per Spec, already built at
  `acpagent.go:141-146`).
* [0023-MADR-canonical-slash-commands.md](0023-MADR-canonical-slash-commands.md): the rule that
  an available command executes.
* [0179-REPORT-gobble-native-acp-integration-assessment.md](../reports/0179-REPORT-gobble-native-acp-integration-assessment.md):
  gobble's registration gates.

## Amendment — 2026-10-21: the owner's answer to Q1, pass-through first

**The answer, verbatim:** "Idempotent means wherever possible we pass through cli commands to the
phone and rewrite as little as possible. Improve the heuristics of the harness to be able to handle
the raw command output of any supported agentic cli instead of having to write our own hardcoded
duplicates."

The record stays `proposed`. D1–D8 stand, because pass-through still needs honest capabilities
(D1), vendor code in its vendor's package (D2) and a capability-driven phone (D4). This amendment
adds the governing principle and D9–D13. Where it and the text above disagree, this amendment
governs.

### Further findings [measured 2026-10-21 on f26549de]

**N7 — Forwarding already exists, and silence is handled by hand.**

*Note, 2026-10-22 (the PLAN's P4b deviation 1).* grok's current source shows its `/compact` does
compact over ACP. It reports that only as `auto_compact_completed` on its
`_x.ai/session_notification` channel, which Magic dropped, so Magic saw silence. `/context` is the silent one. P4b shows grok's compaction and
forwards `/compact`. The paragraph below states what was observed against grok 1.0.5.

* A `KindNative` mapping sends the agent's own slash command as prompt text, and the reply streams
  back as the agent's message.
* Agent commands outside the canonical set are already forwarded as they are.
* What a forwarded command does over the remote protocol is recorded by hand. grok's table notes
  that `/compact` and `/context` "produced zero session/update frames and zero tokens", and maps
  `context` to grok's `/session-info` instead (`internal/provider/grok/commandtable.go:5-29`). Its
  caveat says the TUI-only commands "answer with silence" (`:72-76`). Nothing detects silence at
  run time.

**N8 — Pass-through has a hard limit: many CLIs' slash commands live in their terminal UI.**

* codex's app-server protocol carries no slash commands; every codex row is an operation Magic
  drives (`internal/provider/codex/commandtable.go:5-50`).
* opencode "advertises no canonical command of its own — `GET /command` returns only project
  commands and skills" (`internal/provider/opencode/commandtable.go:7-11`). kilo shares that
  engine.
* grok advertises its TUI's catalog over ACP, and some of it is silent remotely (N7).
* gobble executes every command it advertises, by design.
* So "forward the agent's command" is possible for grok's working commands, gobble's commands, and
  opencode's and kilo's project commands and skills. For codex's set and opencode's built-ins,
  Magic is the client and has to call the engine's API. What it can stop doing there is rewrite the
  engine's answer.

**N9 — Raw tool output is flattened on the ACP path, and kept on the HTTP path.**

* `acpagent` collapses all whitespace in a tool card's text with `strings.Fields`, then cuts it to
  400 runes for a call and 600 for an update. A diff becomes `diff <path>` and terminal output
  `terminal <id>` (`internal/provider/acpagent/session.go:1809`, `:1833`, `:2807-2837`). A
  directory listing, a table or a stack trace arrives as one line.
* opencode and kilo keep line structure and cap tool output at 8,000 characters, "aligned with the
  client's" cap (`internal/provider/opencode/http.go:1841-1859`;
  `internal/provider/kilo/session.go:941-959`). The same function is copied in both packages.
* Both still flatten error text with `clip` (`opencode/http.go:1393`, `:1585`, `:1826-1832`).
* codex's command-output path was not measured for this amendment.

**N10 — The phone can show preformatted text, but has no heuristics for raw CLI output.**

* Assistant text renders as Markdown, with code-block chrome and copy (`chat_bubble.dart:1193-1270`).
  Tool detail renders as selectable monospace, capped at `kMaxExpandedDetailChars` = 8,000
  (`chat_bubble.dart:1360-1440`; `data/chat/chat_models.dart:95`).
* Nothing in the app recognizes ANSI escapes, aligned columns, box-drawing tables, JSON or unified
  diffs. The only ANSI handling in the repository strips escapes from auth CLI output
  (`internal/providerauth/cli.go:18-24`).

**N11 — Magic writes its own output for twenty operations.** `runCanonical` dispatches twenty
operations (`internal/session/commands.go:210`, the switch at `:248-287`). Their text comes from Magic's own formatters (for example
`formatSessionUsage`, `formatBilling`, `formatSessionTotals`, `formatSessionCost`). These live in
`codex/runtime.go`, `acpagent/runtime.go`, `httpagent/runtime.go`, `kilo/usage.go` and
`session/tokencost.go`, five files of 1,243 lines that hold the engine calls as well as the
formatting. The formatting share was not measured separately.

### The principle

**Magic forwards the agent's own command and the agent's own output wherever it can, and rewrites
as little as possible.** Magic's own code is for what only the daemon can do (sessions, help,
pairing, transport) and for engines whose commands exist only in their terminal UI. Even there, the
engine's answer reaches the phone as the engine gave it.

### Further decisions

**D9 — Resolution prefers the agent's own command.**

* For each canonical command, the order is:
  1. the agent's own command, forwarded verbatim, when the agent advertises it and it is not known
     to be silent remotely (D10);
  2. the engine's own operation, when the engine has no remote command for it, with its answer
     passed through (D12);
  3. Magic's own implementation, only for daemon concerns (`help`, `new`, `sessions`, `clear`).
* A canonical name is an alias for the agent's command, not a replacement. The phone lists the
  agent's commands under the agent's own names, and the canonical names map onto them.
* Rows in a provider's table exist only where the agent's name differs from the canonical one
  (grok's `context` → `session-info`), or where the engine has no remote command (N8).

**D10 — Silent commands are detected, not catalogued by hand.**

* When a forwarded command ends its turn with no session update and no output, the daemon records
  it as silent, keyed by provider, agent version and command name. It then:
  * tells the phone the command produced nothing remotely;
  * marks it unavailable for that agent version, so the composer stops offering it;
  * clears the mark when the agent version changes.
* The marks persist in the daemon's state directory, and `doctor` lists them.
* Hand-written "returns nothing here" rows and caveats (N7) are deleted once detection covers them.

**D11 — Raw output is carried end to end.**

* No adapter collapses whitespace in, or replaces, an agent's output. Each tool event carries:
  * a short title for the card's collapsed row, built separately;
  * the agent's text as it came, with line breaks, indentation and escape sequences;
  * a diff's path, old text and new text, when the agent sent a diff;
  * a terminal's output, when the agent sent it.
* One bound applies to every provider, set from the configured message size
  ([0181-MADR-configurable-mcremote-message-size.md](0181-MADR-configurable-mcremote-message-size.md)).
  It is applied by one shared function, not by a copy per adapter. Text over the bound is cut on a
  line boundary, marked as truncated, and kept whole in the session's history for the phone to
  fetch.
* The ACP path moves to this first (N9); opencode's and kilo's `clipBlock` copies become the shared
  function.

**D12 — The engine's answer passes through; Magic adds labels, not prose.**

* When Magic drives an engine operation (D9 step 2), the adapter returns the engine's result as it
  came: text as text, structured data as structured data.
* The phone renders structured results generically (D13). An adapter may supply field labels and
  units. It does not compose sentences.
* The per-vendor formatters in N11 are retired as each operation moves.

**D13 — The phone renders raw output with one set of heuristics for every agent.**

* A pure classifier decides how to show a block of text:
  * Markdown, the default for assistant messages;
  * preformatted terminal text, when it has ANSI escapes, box-drawing characters, aligned columns
    or mostly-indented lines. It is shown in monospace, with ANSI colours mapped onto the theme and
    other escapes removed;
  * JSON, pretty-printed and collapsible;
  * a unified diff, shown as a diff;
  * anything long, collapsed behind "Show more".
* The classifier never asks which provider sent the text.
* Its tests are fixtures cut from real wire captures of every provider (`internal/wirecap`),
  redacted. A new agent's output joins the fixtures; it does not get new rendering code.

### Consequences of the amendment

* Good, because each agent's own commands and output reach the phone, so an agent's improvement
  shows up without a Magic release.
* Good, because the vendor formatters in N11's five files and the hand-kept "silent" rows go.
* Good, because one renderer, tested on every provider's real output, replaces per-screen special
  cases.
* Neutral, because codex and opencode's built-ins still need Magic to call their APIs (N8). Their
  answers pass through, but the calls stay.
* Bad, because raw output is larger than summaries. The single bound (D11) and fetch-on-demand
  from history are the guard.
* Bad, because heuristics misclassify. A wrong guess must fall back to plain monospace, never
  hide text, and every misclassification found becomes a fixture.

### Confirmation of the amendment

* A table test over captured tool output from grok, opencode, kilo and codex shows line structure
  kept end to end. It is first seen failing on the ACP path.
* A fixture agent whose forwarded command ends silently is marked silent, the phone is told, and a
  changed agent version clears the mark.
* The classifier's fixtures cover ANSI, aligned columns, box drawing, JSON, a unified diff and
  plain Markdown, each from a real capture. A planted misclassification fails the suite.
* A source test fails if an adapter calls `strings.Fields` on agent output.

### Questions this amendment adds

* **Q5 — Structured results.** D12 lets an adapter label fields. Should the phone show raw engine
  JSON when no labels exist, or should every structured result need labels before it ships?
* **Q6 — Silent-mark persistence.** Per agent version, as D10 says, or per agent version and host?
* **Q7 — Phase order.** Suggested: D11's ACP fix and the shared bound first, since they are small
  and visible; then D13's renderer; then D9 and D10; then D12 operation by operation; with D1 and
  D2 before gobble is registered.

### Owner answers, 2026-10-21

The owner answered: "Q5 raw json, Q6 version and host, Q7 as recommended". These govern the text
above where they differ.

* **Q5 — raw JSON.** A structured engine result reaches the phone as the engine's JSON, and the
  phone shows it with D13's JSON rendering. Labels are not needed for a result to ship. An adapter
  adds none unless a later record asks for them.
* **Q6 — per agent version and host.** D10's silent marks are keyed by provider, agent version,
  host and command name. A mark made on one host never hides a command on another, even when the
  state directory is copied or restored there.
* **Q7 — the suggested order.** D11, then D13, then D9 and D10, then D12 one operation at a time.
  D1 and D2 land before gobble is registered. This also answers Q4: gobble waits for D1 and D2,
  not for all of D1–D7.
* **Still open:** Q2 (the default provider) and Q3 (protocol overlap for `codex_surface`). Only the
  D4 and D5 phases depend on them. *Answered later the same day; see below.*

### Owner answers, 2026-10-21 (Q2, Q3, and two design goals)

The owner answered: "Gobble will become the default provider. Api compatibility and native
integration are design goals". These govern the text above where they differ.

* **Q2 — gobble is the default provider.**
  * D5's built-in default order starts with gobble. It is data in the config defaults, not a branch
    in code, so D5's rule against comparing provider ids still holds.
  * Until gobble is registered (0179's PLAN, after this PLAN's P6), the built-in order is gobble,
    then grok, then the rest in registration order. A host without gobble therefore keeps today's
    grok preference, and the change arrives with gobble rather than before it. *This interim
    order is this record's reading of "will become"; the owner can correct it.*
  * `default_provider` in the config still overrides the built-in order.
  * Being the default selects gobble when the operator picks nothing. It does not change the parity
    rule for how well every provider is supported.
* **Q3 and the first design goal — API compatibility.**
  * Magic's phone protocol stays backward compatible. A change is additive and advertised as a
    capability. An older phone keeps working against a newer daemon, and a newer phone against an
    older daemon.
  * `codex_surface` gains its neutral name beside the old one. The old field is still accepted and
    still sent. Removing it takes its own record and a protocol version bump.
  * Where this record replaces Magic's text with the engine's own output (D12), a phone that has not
    advertised the new rendering still gets the output as text it already shows.
  * Agent-side, compatibility means the standard APIs each agent CLI publishes (ACP's schema,
    opencode's and kilo's HTTP API, codex's app-server protocol), used as published (D3, 0184 D6).
* **The second design goal — native integration.**
  * gobble is the native agent of this platform. Its whole published surface (its commands, its
    `_gobble/*` operations, steer and follow-up, its todos) is integrated as first-class, through
    the agent-neutral mechanisms this record builds, not through special cases in shared code.
  * D1 and D2 (P6) remain the precondition for registering it. Nothing else in this record waits
    for gobble, and gobble's registration waits for nothing beyond P6.

### Owner direction, 2026-10-21: greenfield, one phone user

The owner said: "Look at this as greenfield. I am the only phone user." Then: "Proceed". This
governs the two answers above where they differ.

* **API compatibility means the agents' APIs, not old phones.** The daemon and the phone ship
  together to one user, so the phone protocol may change in step with the daemon. There are no
  overlap fields, no fallbacks for a phone that lacks a new capability, and no old-phone or
  old-daemon compatibility tests. Each agent CLI's published API is still used as published.
* **`codex_surface` is renamed outright** to its neutral name, in the daemon and the phone in one
  change.
* **D12's structured results** go to the phone as raw JSON only. The text fallback for an older
  phone is dropped.
* **The default order is gobble, then registration order.** The interim grok-second order is
  dropped: until gobble is registered, the first ready provider is the default. `default_provider`
  still overrides.
* **D6's config format may change** where a provider descriptor makes it simpler. The phase that
  changes it migrates the owner's config with a backup, and only with the owner's go-ahead for that
  live file.
* **Design is judged on the target, not on the path from today.** Where today's structure is in the
  way of D1–D13, it is replaced rather than adapted.

**A fact this record stated too early.** D11 sets the bound "from the configured message size" of
[0181-MADR-configurable-mcremote-message-size.md](0181-MADR-configurable-mcremote-message-size.md).
0181 is `proposed` and not built: the WebSocket limit is the constant `maxWSMessageBytes`, 1 MiB
(`internal/ws/server.go:310`). Until 0181 lands, the shared bound is a constant beside that limit.
The first phase keeps today's 8,000-character cap, which opencode, kilo and the phone already share
(`opencode/http.go:1844`, `kilo/session.go:944`, `chat_models.dart:95`), so nothing grows before
the phone can page longer text.
