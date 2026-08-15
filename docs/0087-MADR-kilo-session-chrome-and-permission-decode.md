---
status: accepted
date: 2026-08-14
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Filter Kilo session chrome on the live wire and decode durable permission frames

## Context and Problem Statement

The Kilo CLI provider shipped in
[0075](./0075-MADR-kilo-cli-provider.md) and was audited in
[0076](./0076-MADR-kilo-debug-pass.md) against **kilo 7.4.20**. On this
host the installed engine is now **7.4.22** (`KnownGoodVersion` is still
7.4.20). A prompted Kilo session on the phone spewed “Initializing
session” as assistant text, leaked file-diff notices on every snapshot
tick, and could not run terminal commands such as `grep` even with the
synthetic **auto** mode armed.

The dialect already *meant* to drop transient lifecycle parts (0075
§2.4) and to auto-approve `permission.asked` (0044 / kilo `mode.go`).
Those contracts failed against the live 7.4.20–7.4.22 wire. This record
asks: **which Kilo SSE shapes are engine chrome versus conversation, and
how must `DecodeFrame` / auto-approve tolerate 7.4.22’s dual
`properties`/`data` envelope so a bash ask is not silently dropped?**

Scope is `internal/provider/kilo` only (session/event translation,
`DecodeFrame`, `handleSessionDiff`). No protocol change, no mobile
change, no `httpagent` interface change.

## Decision Drivers

* A prompted session must show the model’s reply, not engine spinner
  chrome or workspace snapshot patches.
* Auto mode (MADR 0044) must actually unblock tools. On kilo 7.4.22’s
  `code` agent, `bash *` is **ask** and there is no native `grep` tool,
  so grep is a bash permission.
* httpagent drops any SSE frame whose decoded session id is empty
  (`provider.go` SSE pump: `ok && sid != ""`). A permission frame that
  decodes without a sid never reaches `emitPermissionAsk`.
* Wire-shape decisions must be pinned to captured or live-probed
  frames. CLI forks change silently (AGENTS.md).
* Do not swallow real assistant text. Chrome filters must be
  kilo-specific and fail closed only on markers we have seen.
* Keep the un-gated loopback posture (0075 D5 / PD1). Do not add Basic
  auth to every engine call just to reach one new endpoint.

## Considered Options

* Option 1: Tolerate both live wire shapes in the kilo dialect
* Option 2: Pin or refuse engines newer than 7.4.20
* Option 3: Drive auto mode through `POST /permission/allow-everything`
* Option 4: Treat `session.diff` as a first-class structured chat event

## Decision Outcome

Chosen option: **"Option 1: Tolerate both live wire shapes in the kilo
dialect"**, because the three user-visible failures are decode/filter
bugs against frames kilo already emits, not missing product features.
The 7.4.20 spike already contained the dotted metadata key the 0075
filter missed; 7.4.22 added a durable `data` envelope that httpagent
would drop. Fixing the dialect restores the 0075/0044 contracts without
a version gate or a new auth header.

* Implementation Plan:
  [0087-PLAN-kilo-session-chrome-and-permission-decode.md](./0087-PLAN-kilo-session-chrome-and-permission-decode.md)

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | Chrome filter matches the **live** markers: `synthetic: true`, `ignored: true`, `metadata["kilocode.lifecycle"] == "transient"` (dotted key), the nested `metadata.kilocode.lifecycle` shape 0075 assumed, part types `snapshot` / `patch` / `step-start` / `step-finish` / `compaction` / `retry`, and a last-resort text heuristic for braille-prefixed “Initializing snapshot/session…”. Tool parts are never chrome. Apply the same filter on live `message.part.updated` **and** on `Replay`. |
| **D2** | `session.diff` is snapshot chrome, not a transcript notice. The user-facing pull path stays `httpSession.Diff` (`session_ops.go`). Do not emit `TypeNotice` (or the raw `patch`) on the SSE. |
| **D3** | `DecodeFrame` accepts `properties` **or** `data` (wrapped `payload.*` and bare). `properties` wins when both are present. An empty/null blob is skipped so a durable `permission.asked` still yields a session id. |
| **D4** | Ignore 7.4.22 ticker/chrome events that are not conversation: `session.next.synthetic`, `file.edited`, `session.next.text.started/ended`, `session.next.reasoning.started/ended`, `session.next.step.*`. Do **not** subscribe `session.next.text.delta` yet — 7.4.22 still dual-emits `message.part.delta` on the probed stream; handling both would double assistant text. |
| **D5** | Auto-approve stays per-permission `{reply:"once"}` (0044 D4.4). Do **not** switch auto mode to `POST /permission/allow-everything`. That route returned **401 Basic** on 7.4.22 even with `KILO_SERVER_PASSWORD` unset; `-u kilo:` (empty password) was required. Sending Basic on the un-gated engine is a 0075 D5 change and is out of scope. |
| **D6** | **Superseded by [0088](./0088-MADR-kilo-7.4.22-surface-parity.md).** Leaving the pin at 7.4.20 is not acceptable: `KnownGoodVersion` must move to the installed current release once its surface is probed. 0088 is that probe and pin. |

## Consequences

* Good, because the 0075 transient-part rule now matches the frames kilo
  actually sends, so the initializing spinner cannot become assistant
  bubbles.
* Good, because workspace snapshot diffs stop flooding the chat while
  the explicit Diff RPC still works.
* Good, because a durable `permission.asked` keeps its session id and
  can be auto-approved, which is what unblocks bash/grep in auto mode.
* Bad, because the dialect now encodes two metadata shapes and two
  envelopes. A third shape still needs a code change.
* Bad, because D5 leaves `allow-everything` unused; kilo’s own TUI auto
  flag may be richer than per-permission `once`.
* Neutral, because `session.next.*` tool/text streams are recorded and
  ignored rather than mapped. Revisit when a live turn proves
  `message.part.*` is gone.
* Neutral, because the 7.4.20 pin is retired by 0088; this record only
  owns the chrome/decode fixes (D1–D5).

## Pros and Cons of the Options

### Option 1: Tolerate both live wire shapes in the kilo dialect (Chosen)

Widen the chrome filter and `DecodeFrame` so 7.4.20 spike frames and
7.4.22 OpenAPI durable frames both work.

* Good, because it fixes the three reported symptoms without a protocol
  or mobile change.
* Good, because the 7.4.20 spike fixture already proves the dotted key
  is not a 7.4.22-only surprise.
* Bad, because two shapes must stay tested forever.
* Neutral, because opencode’s dialect is untouched (it still uses
  `properties` only).

### Option 2: Pin or refuse engines newer than 7.4.20

Fail Start / log-and-refuse when `/global/health` is not 7.4.20.

* Good, because the dialect stays exactly as 0075 probed it.
* Bad, because this host (and any brew/npm user) is already on 7.4.22;
  the product would be unusable until they downgrade.
* Bad, because the dotted-key filter bug is already present on 7.4.20
  (`sse-or.raw`). A pin would not have stopped the initializing leak.

### Option 3: Drive auto mode through `POST /permission/allow-everything`

Arming auto would POST `{enable:true, sessionID}` so the engine never
raises bash asks.

* Good, because it matches kilo’s own “allow everything” control and
  would cover permission types we do not yet decode.
* Bad, because on 7.4.22 the route is 401 unless Basic `kilo:` is sent,
  even with no server password. That is a 0075 D5 policy change.
* Bad, because per-permission `once` already works once the frame is
  not dropped (D3). allow-everything is not required to unblock grep.
* Neutral, because `POST /permission/{id}/reply` still returns 404
  (not 401) without Basic — the existing reply path is not gated.

### Option 4: Treat `session.diff` as a first-class structured chat event

Map SSE `session.diff` onto a new protocol event or a tool card.

* Good, because a user who wants a live dirty-tree view would see it.
* Bad, because kilo fires the event as snapshot chrome with full
  `patch` text, often repeatedly. That is the leak.
* Bad, because the pull path (`GET /session/{id}/diff` via
  `httpSession.Diff`) already exists for the session-actions menu.
* Neutral, because a future structured diff card can be added without
  un-silencing the SSE notice.

## Confirmation

* Unit tests in `internal/provider/kilo`:
  * `TestTransientLifecyclePartsFiltered` — nested object, live dotted
    key + `synthetic`, and metadata-less “Initializing session” text.
  * `TestInitializingSpinnerDoesNotRepeat` — three spinner ticks emit
    zero assistant chunks.
  * `TestSessionDiffDoesNotEmitNotice` — a frame with `file` + `patch`
    emits nothing.
  * `TestDecodeFrameDurableDataEnvelope` — wrapped
    `payload.data` permission.asked yields sid + a normalizable ask.
  * `TestReplaySkipsInitializingChrome` — resume replay drops the
    spinner part and keeps real text.
  * `TestIgnoredKiloEventsEmitNothing` includes
    `session.next.synthetic` and `file.edited`.
* `go test -race ./internal/provider/kilo/` green (2026-08-14).
* `make pre-add-check FILES=` over the touched kilo files clean
  (govulncheck offline-warn only).
* Live probe 2026-08-14 against `kilo serve` 7.4.22 (loopback,
  `--pure`): health `{"healthy":true,"version":"7.4.22"}`;
  `/doc` lists both `EventPermissionAsked.properties` and
  durable `PermissionAsked.data`; `code` agent has `bash * ask` and no
  native grep; `POST /permission/allow-everything` is 401 without
  Basic, 200 with `-u kilo:`; `POST /permission/{id}/reply` is 404
  without Basic (not gated). A live tool turn was **not** completed:
  `kilo-auto/free` returned `PAID_MODEL_AUTH_REQUIRED`. Existing
  `live_kilo` permission tests remain the acceptance pin when a
  working model is available.

## More Information

### Probe evidence (this host, 2026-08-14)

| Item | Result |
| --- | --- |
| Binary | `/home/mac/.local/bin/kilo` → 7.4.22 |
| 0075 pin | `KnownGoodVersion = "7.4.20"` |
| 7.4.20 spike (still authoritative for the spinner) | `docs/kilo-spike-7.4.20/sse-or.raw` lines 31–45: `text` is `⠋/⠙/⠹ Initializing snapshot…`, `synthetic: true`, `metadata: {"kilocode.lifecycle":"transient"}` |
| 0075 filter as shipped | nested `metadata.kilocode.lifecycle` only — **does not match** the spike |
| 0075 test as shipped | asserted the **wrong** nested shape (`session_test.go` before this change) |
| 7.4.22 OpenAPI | 256 paths (spike had 243). New part types: `snapshot`, `patch`, `step-start`, `step-finish`. New events: `session.next.synthetic`, `session.next.tool.*`, `session.next.shell.*`, `file.edited` |
| `session.diff` item | `SnapshotFileDiff`: `file`, `patch`, `before`, `after`, `additions`, `deletions`, `status` |
| `code` agent permissions | `bash * ask` (twice in the merged ruleset); native `grep` **absent** on `code` (present as allow on `ask` / `plan` / `orchestrator`) |
| Auto mode | still the synthetic id `auto` → agent `code` + `SetAutoApprove(true)` (0044). No engine agent named `auto`. |

### Why the spinner repeated

`emitTextCatchUp` emits the tail when the new snapshot is not a prefix
of the last streamed text. Each spinner frame changes only the leading
braille character (`⠋` → `⠙` → `⠹`), so the common prefix is empty and
the full “Initializing …” line is emitted again.

### Why grep died in auto mode

1. The model must use bash (`code` has no grep tool).
2. `bash *` is ask, so the engine emits `permission.asked`.
3. If that frame is the durable `{payload:{type, data}}` shape,
   pre-D3 `DecodeFrame` left `props` empty → `sessionIDOf` "" →
   httpagent dropped the line.
4. Auto-approve never ran. The agent stayed blocked.

Per-permission reply itself is fine (404 for a fake id, not 401).

### Related

* [0075-MADR-kilo-cli-provider.md](./0075-MADR-kilo-cli-provider.md) §2.4
  (transient filter as specified — nested key, now known to be incomplete)
* [0076-MADR-kilo-debug-pass.md](./0076-MADR-kilo-debug-pass.md) (session
  loop was “byte-correct vs opencode”; this bug was in the kilo-only
  filter and in 7.4.22 envelope drift)
* [0044-MADR-auto-approve-modes.md](./0044-MADR-auto-approve-modes.md)
* Spike: [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/)
* Code: `internal/provider/kilo/{session,dialect,command}.go`
