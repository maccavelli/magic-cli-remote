# MADR 0023: A canonical slash-command vocabulary across agent CLIs

- **Status**: **Accepted** — implemented on `master` (2026-07-25).
- **Date**: 2026-07-25
- **Deciders**: Project Owner (scope, client advertisement, phasing); Implementer
  (registry/providers/daemon/mobile)
- **Related**:
  - [protocol-v1.md](./protocol-v1.md) — canonical commands, `remote_commands`
    event, `commands.list`
  - [MADR 0022](./0022-MADR-plan-mode-parity.md) — session modes, `/plan`, `/mode`
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — OpenCode agents, commands,
    fork/revert/diff
  - [agent_cli_slash_commands_matrix.md](./agent_cli_slash_commands_matrix.md) —
    the survey this corrects

**Verified against** (live, not inferred): grok **0.2.112** over ACP stdio,
OpenCode **1.18.5** over HTTP, goose **1.44.0** over ACP, on 2026-07-25.

---

## Context

A survey proposed that four CLIs (OpenCode, Goose, Grok, Codex) all "support and
advertise" the same eight core slash commands, and that standardising on them
"guarantees 100% compatibility". Probing the three CLIs installed here showed the
premise is wrong in both directions:

| claim | what the CLIs actually do |
|---|---|
| all four advertise the same 8 | the *advertised* intersection of grok ∩ goose is `{compact, goal}`; OpenCode advertises 4 project commands and none of the 8 |
| `/help` is universal | no CLI advertises it (grok's `/user:help` is a user-authored skill) |
| `/plan` is universal | none advertises it; goose has no plan mode at all — its modes are permission modes (`auto`, `approve`, `smart_approve`, `chat`) |
| `/clear`, `/model`, `/sessions` advertised | not by grok or OpenCode; goose advertises `clear` only |
| ACP `session/available_commands_update` | not a method — it is a `session/update` variant. ACP standardises `session/{new,load,list,resume,fork,close,delete,cancel,prompt,set_mode,set_config_option}`: no compact, model or plan |

The load-bearing error is subtler than the missing rows. **A command an agent
advertises is not a command it executes.** grok advertises `/compact`,
`/context` and `/always-approve` over ACP; sending any of them returns *zero*
`session/update` frames and `totalTokens: 0`. In the same session
`/session-info` returned a full markdown report and an ordinary prompt streamed
normally. Grok's `available_commands` is its **TUI's** catalog, only partly
reachable over the protocol.

Our own code had the matching flaws. The vocabulary was duplicated and
hand-synced (`BuiltinCommands` server-side, never sent over the wire; a second
hardcoded list plus a `hasModes` gate in `chat_screen.dart`). Availability came
from ad-hoc heuristics: `agentAdvertises` trusted the agent's catalog, while
`helpText` hardcoded a grok caveat naming `/context` and `/compact` as
"terminal-only" — i.e. the daemon simultaneously believed both that those
commands worked and that they did not. And the commands the survey is really
about — `/compact`, `/context`, `/sessions`, `/goal`, `/diff`, `/undo` — were
reachable from nowhere, though the endpoints or ops existed.

## Decision

**One canonical vocabulary, declared per provider, resolved per session, and
advertised to clients.**

1. **`internal/command` is the canonical resource.** `Specs` is the vocabulary
   (name, args, description, aliases, default mechanism). A provider declares a
   `Table` mapping each canonical name to a `Mapping`: `KindDaemon` (the daemon
   does it), `KindMode` (a session mode), `KindOp` (a provider capability),
   `KindNative` (forward a command the agent really executes — not necessarily
   the same name) or `KindNone` **with a reason a user can read**. `Resolve`
   combines a table with what a live session reports and picks the mechanism;
   `ResolveAll` feeds `/help` and the advertised list. One function answers
   "what can this session run", so the router, the help text and the client
   cannot disagree.

2. **A provider's table beats what the agent advertises.** This is the rule the
   package exists for, and grok's inert `/compact` is why. Advertisement is only
   consulted where a table says nothing at all — there, an agent advertising the
   exact canonical name owns it, which preserves the soft-builtin behaviour
   MADR 0022 introduced for `/plan`.

3. **Capabilities are proven by interfaces, not claims.** A `KindOp` mapping
   resolves only if the live session implements the matching optional interface
   (`CompactSession`, `ModelSession`, `UndoSession`, `RevertSession`,
   `DiffSession`) or, for `/context`, has actually reported usage. A provider
   cannot advertise an op its transport does not implement.

4. **Tables live with their provider** — `acpagent.Spec.Commands` for ACP
   agents, a dialect hook for HTTP ones — and a conformance test asserts every
   registered provider declares every canonical command. Adding a CLI means
   stating, per command, how it works or why it cannot.

5. **The daemon advertises the resolved list** (`remote_commands`), including
   unavailable commands with their reasons, at create and whenever resolution
   changes. The Flutter composer renders that list; its hardcoded lists survive
   only as a fallback for an older daemon.

### What this made possible

- **`/compact`** on OpenCode via `POST /session/{id}/summarize` (the v2
  `/api/session/{id}/compact` route answers 503 "not available yet" on 1.18.5).
- **`/context`** everywhere the daemon has a usage report, which now includes
  OpenCode: assistant messages carry `tokens`, and the context window comes from
  the `/provider` catalog `AfterBoot` already fetches. On grok it is forwarded as
  `/session-info`, the command that does work.
- **`/model` without losing the conversation** on OpenCode
  (`POST /api/session/{id}/model`, ModelRef `{providerID, id}`). grok keeps the
  relaunch path, which the resolution ladder falls back to automatically.
- **`/undo`, `/redo`, `/diff`, `/sessions`** — the first three over the revert
  and diff routes (undo resolves the last user message itself, since revert needs
  a message id the daemon never sees), the last from the daemon's own registry.

### Consequences

- `/help` now lists what works *and* why the rest does not, in the provider's own
  words. The wrong "terminal-only" caveat is gone; grok keeps a truthful
  session-wide note about the rest of its TUI catalog.
- Clients stop guessing. The Go/Dart duplication is gone in the direction that
  matters: the daemon is the source of truth and the Dart list is dead weight
  kept only for old daemons.
- `remote_commands` is a control event and is recorded in the history ring, so a
  cold client rebuilds autocomplete from replay. It is re-emitted only when the
  resolved list changes (typically 1–3 times per session).
- `commands.list` gained an optional `session_id`; without one, session-dependent
  commands are listed disabled rather than presented as working.
- Fixed alongside: `hydrateFromCache` now preserves `remoteCommands` too — the
  same class of bug as MADR 0022's mode-strip wipe, since the list arrives before
  any chat item and the cache stores none of it.

### Known limitations

- `/context` needs one usage report before it can answer; until then it says so.
  grok never sends `usage_update`, which is why its mapping is native.
- `/compact` is unavailable on grok. Its shell compacts only in the TUI, and no
  ACP call or `_x.ai` extension exposes it.
- The per-provider tables are *statements about a CLI version*. That is what the
  live tests are for (below): a CLI update that changes behaviour fails a test
  instead of going quiet.
- Non-canonical commands an agent advertises are still forwarded blindly, so
  grok's other TUI-only commands still answer with silence. A per-provider deny
  list for those is possible but was not worth the maintenance here; the `/help`
  caveat warns instead.

### Amendments

**MADR 0043 (2026-07-28) — `/model`.** Two changes, both inside the framework
rather than around it:

- **goose moves from `KindDaemon` to `KindOp` + `OpSetModel`.** The daemon
  mapping meant relaunch, and relaunch costs the conversation. Goose can switch
  model in place through ACP `session/set_config_option`; nothing read that
  capability back until 0043 D6. The table entry was a statement about what
  goose could do, and it was wrong.
- **Bare `/model` is answered by a client-side picker.** With no argument the
  client fetches a session-scoped `models.list` and, on confirm, submits the
  ordinary `/model <id>` text. The daemon executes exactly the command it always
  has — the picker composes the argument, it does not bypass resolution. It is
  gated on `remote_commands` reporting `/model` available, so the rule that the
  daemon decides availability is intact: when it says no, the text goes through
  and the user gets the daemon's explanation rather than a picker for a command
  that cannot run.

### Alternatives rejected

- **Trusting `available_commands` and standardising on the survey's eight.** It
  would have shipped `/compact` and `/context` on grok as commands that silently
  do nothing — the exact failure the probe found.
- **Per-provider slash-command handling inside each provider.** Duplicates the
  routing, and gives no place to record *why* something is unavailable.
- **A capability bitmask on the provider.** Coarser than a per-command mapping
  and cannot express "same idea, different command name" (`/context` →
  `/session-info`), which turned out to be the common case.
- **Resolving on the client.** The client would need the provider tables, the
  session's capabilities and the ACP semantics — everything the daemon already
  has. It is also how the duplicated Dart gate got out of step in the first place.

## Adding a provider (the checklist)

1. Implement the transport (`acpagent.Spec` or an `httpagent` dialect).
2. Declare a `command.Table` — `acpagent.Spec.Commands`, or `CommandTable()` on
   the dialect. Every canonical command needs an entry; the conformance test
   fails otherwise. Write `KindNone` reasons in words a user reads.
3. **Probe, don't assume.** For each command you map `KindNative`, send it and
   confirm output actually comes back. For `KindOp`, implement the matching
   optional interface (`provider.CompactSession`, …) — resolution checks for it.
4. Add a `CommandCaveat` if the agent has a session-wide quirk worth a line in
   `/help`.
5. Add live-tagged tests pinning what you probed, following
   `internal/provider/*/live_command_test.go`.

## Verification

- Unit: resolution ladder, degradation and precedence (`internal/command`);
  cross-provider conformance; OpenCode summarize/model/undo request shapes and
  token→usage translation; daemon dispatch for every canonical kind on the fake
  provider; `/help` agreeing with the router; advertisement dedupe; Flutter
  reducer, hydrate and composer rendering.
- Live:
  - `go test -tags live_grok ./internal/provider/grok/ -run Command -count=1` —
    `/session-info` answers, `/compact` does not, and the table matches.
  - `go test -tags live_opencode ./internal/provider/opencode/ -run Command -count=1` —
    summarize accepted, model switched in place, usage reported.
  - `go test -tags live_grok ./internal/session/ -run TestLiveCanonical -count=1` —
    end to end through the daemon: `/context` forwards to `/session-info` and
    returns text; `/compact` reports grok's recorded reason.
