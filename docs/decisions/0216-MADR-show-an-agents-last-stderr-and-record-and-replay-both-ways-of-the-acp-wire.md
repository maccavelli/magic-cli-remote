---
status: accepted
date: 2026-10-29
decision-makers: Project Owner
consulted: "0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md, amendment I (I-6, D50, question 16); 0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md with its amendments of 2026-10-28 and 2026-10-29; 0196-MADR-remove-the-development-account-name-and-guard-home-paths.md; 0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md, C3 Deviations 3 and 4; acp-go-sdk2 v1.26.0's agentproc and acptest packages, read from the module cache; this repository at 0e84b1f0"
informed: anyone who changes internal/wirecap, the acpagent process path, or a committed wire fixture
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Show an agent's last stderr when it dies, and record and replay both ways of the ACP wire

## Context and Problem Statement

[0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
amendment I, pinned `acp-go-sdk2 v1.26.0`. Its I-6 lists what that release offers and Magic either
wrote for itself or lacks. Its D50 left three of those items undecided. The owner set their order,
in question 16: "Stderr, capture, replay (Recommended)".

1. **The stderr tail.** When an ACP agent dies, Magic says only that its process exited, not what
   the agent last wrote to stderr.
2. **A capture of both ways.** `wirecap` records only what the agent sends. It does not record
   Magic's requests, or Magic's answers to the agent's requests.
3. **Replay.** Replaying a recorded agent against Magic needs both ways of the wire, so it waits on
   item 2.

D50 said each item takes its own MADR. On 2026-10-29 the owner instead asked for one: "scope and
spec 1 and 4. research the codebase. ground in facts. write the detailed and comprehensive madr for
review". Here "1" is the stderr tail and "4" is the other two items. They are kept together because
they share one mechanism, `agentproc`'s taps and caps on the agent's three streams, and because
item 3 depends on item 2.

This record decides all three. Each is carried out by its own PLAN, and nothing is implemented until
the owner approves a PLAN.

### What Magic does today [observed at `0e84b1f0`]

**One provider runs on this path.** `acpagent` is the transport of every ACP agent Magic runs:
`grok` today, and `gobble` once it registers under
[0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md).
Only `internal/provider/grok/grok.go` builds an `acpagent` provider at this commit.

**The agent's stderr.**

* `spawnAgent` sets `cmd.Stderr` to a `slogWriter` (`acpagent.go:494-501`). It logs each line at
  debug level, prefixed `<agent>-stderr`, and caps a line with no newline at 4096 bytes
  (`acpagent.go:973-1014`).
* Each complete line also goes to `noteEngineLogLine` (`session.go:825-847`). A line that
  `agenterr.IsLimit` matches during a turn cancels the turn and becomes the turn's error text. So
  some stderr text already reaches the phone today, but only a provider-limit line.
* Nothing keeps stderr after that. The C3 live grok run of 2026-10-28 logged no `grok-stderr` line,
  because the live tests discard debug logs. So what grok writes to stderr when it dies has never
  been observed.

**The death message** (`acpagent.go:591-616`).

* The exit watcher calls `proc.Wait()` and builds `"<agent> process exited"`, or
  `"<agent> process exited: <err>"` when Wait returned an error. `signalDisconnected` sends it once,
  as a `TypeError` event followed by a `disconnected` status (`session.go:1276-1296`). An agent Magic
  closed sends nothing, because `s.closed` is set first.
* That event goes to the session's history (`manager.go:1647`). The manager saves the history to
  disk (`saveHistory`, `manager.go:3854-3873`; `Store.SaveHistory`, `store.go:290`), and it reaches
  every attached phone.
* The phone clips an error item to 300 characters (`apps/mobile/lib/data/chat/transcript_reducer.dart:253-264`).
* `TestSpawnedAgentDeathIsOneDisconnect` (`agentproc_test.go:205-258`) requires the message to
  contain `"testagent process exited: "` and not `"agent killed"`.

**A failure while starting.**

* When `initialize` or `authenticate` fails, `spawnAgent` calls `killTree` (`acpagent.go:526-532`)
  and returns `initializeError(err)` or an `acp authenticate (<id>)` error (`acpagent.go:543-577`).
* None of these carries anything the agent said. An agent that writes "not logged in" to stderr and
  exits before it answers `initialize` shows up as `acp initialize: …` with the connection's error.
* A failed session create reaches the phone through `writeSessionErr`, clipped to 300 bytes
  (`ws/server.go:2134-2177`). Its comment says "the phone needs the headline, the journal has the
  rest".

**Secrets the agent is given.**

* The daemon copies each configured MCP server's `Headers` into `acpagent.McpServer`
  (`daemon/daemon.go:800-819`). `buildMcpServers` sends them to the agent in `session/new` and
  `session/load` (`acpagent.go:650-679`). An `Authorization` header is the usual way to
  authenticate to an MCP server over HTTP.
* The agent inherits the daemon's environment. grok reads `XAI_API_KEY` from it as a credential
  (`grok/auth.go:80`, `:123`).

**The wire capture** (`internal/wirecap`).

* `For(provider)` returns nil unless `MCREMOTE_WIRE_CAPTURE_DIR` is set. Otherwise it opens
  `<dir>/<provider>/frames.jsonl` with `O_APPEND` (`wirecap.go:58-74`). Every session's `Capture`
  opens the same file, so concurrent sessions' frames interleave in it.
* `Frame` writes one line per frame, verbatim apart from redaction (`wirecap.go:100-113`).
  `redact` replaces the capturing host's name with `<host>` and the home directory with
  `/home/user` or `home/user`, each both raw and JSON-escaped (`wirecap.go:147-184`; 0151 D1, D8).
* `acpagent` passes only `OutputTap: s.wire.Tap()` (`acpagent.go:513-519`). `Command.InputTap`
  exists, and has since before `v1.25.0`, but Magic does not use it (0207 I-6).
* `closeCapture` warns when the output tap dropped bytes (`acpagent.go:626-636`).
* `codex` (`codex/conn.go:139`, `:280`) and `httpagent`, the transport kilo and opencode use
  (`httpagent/provider.go:145`, `:793`), each call `Frame` on what they read. Neither goes through
  `agentproc`.

**The committed fixtures.**

* There are six `frames.jsonl` fixtures: grok 1.0.13, 1.0.50 and 1.0.70, codex 0.152.1, kilo 7.5.6
  and opencode 1.18.26.
* Ten test files besides the guard read them as raw frames, one JSON-RPC message per line (for example
  `initialize_test.go:22`, `xaicost_test.go:63`, `grok/version_test.go:18-19`).
* `TestCommittedFixturesCarryNoIdentifiers` globs `testdata/wire/*/frames.jsonl` and requires at
  least 4 matches (`wirecap/fixtures_test.go:64-75`). It fails on an email address, a Windows
  absolute path and the running host's name. Nothing in the tree fails on a bare account name:
  0196 R5 says its guard "covers home paths only", and its consequences name "a bare word" as not
  caught.
* The 1.0.70 fixture holds a `terminal/create` whose command is `/bin/bash -lc "ls -la && … git
  status -sb && git log -5 …"`. The answers to that request, which would be the command's output,
  are Magic's, so they are not in the fixture. With both ways captured they would be.

**C3's gap.** The SDK-gaps plan's Deviation 3 could not tell a `session/load` answer from a
`session/resume` answer, because Magic's requests were not in the capture
([0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md),
C3 Deviation 3). It got only "the shape of" a load answer.

**The tests' fake agent.** `newWireSessionFor` (`wireagent_test.go:55-83`) builds a `session` on a
real `acp.ClientSideConnection` whose peer is a hand-scripted `wireAgent`, with production's
connection options (`clientConnOptions(nil, s)`). No process is started.

### What `acp-go-sdk2 v1.26.0` offers [read from the module cache, and probed]

**The stderr tail** (`agentproc/agentproc.go:72-88`, `:207-211`, `:386-422`; `agentproc/stderr.go`).

* `Command.StderrTail` (bytes) and `Command.StderrTailLines` (lines) keep the end of the agent's
  stderr. A cap of zero means no cap, and the tail starts at the beginning of a line.
* Start then puts a writer of its own in front of `Cmd.Stderr`, which forwards every byte. So
  Magic's `slogWriter` would still see all of stderr.
* `Process.StderrTail()` returns the tail, which is final once Wait has returned.
* `JoinStderrTail` joins a `*StderrError{Tail}` to Wait's error, but only when that error is
  non-nil and the tail is not empty.

**Taps of both ways** (`agentproc.go:54-70`, `:493-506`).

* `InputTap` gets a copy of what the connection writes to the agent, as `OutputTap` does of what
  the agent writes.
* The two streams have no order between them.
* `DroppedTapBytes()` returns the bytes each tap missed, for output and for input.

**`Transcript`** (`agentproc/transcript.go`). It writes both taps to one writer, one
`{"direction":"sent"|"received","frame":…}` object per line. That is the form `acptest.ReadRecords`
reads. Redaction is left to the caller, which wraps the writer.

**Replay** (`acptest/replay.go:21-49`, `:345-374`).

* `ReplayAgent` plays a recording's agent side to a client.
* Notifications and the agent's own requests are sent in order, the requests under fresh ids
  (`"replay-1"` and on). The k-th request of a method is answered with the recorded answer to the
  k-th recorded request of that method.
* A request beyond those recorded is answered with -32603. Serve returns an error that lists how
  the client departed from the recording.
* `ConnectReplay(tb, records, client, opts...)` returns a client connection and checks for
  departures when the test ends.
* `planReplay` refuses a recorded answer whose id no Sent record asked (`replay.go:102-106`). So
  none of today's one-way fixtures can be replayed.

**Checks** (`acptest/golden.go:41`, `:91`; `recorder.go:126-141`).

* `ValidateGolden` checks every record as a protocol message of its direction.
* `Golden` compares records with a file, each frame re-encoded into canonical form with sorted keys,
  and `ACPTEST_UPDATE=1` rewrites the file.
* `agentproc.InProcess` (`inprocess.go:42-64`) runs a serve function such as `ReplayAgent.Serve` as
  an agent and returns the same `*agentproc.Process` that a child process gets.

**What the probe showed.** It was a scratch module outside the tree, not committed, built against
the pinned `v1.26.0`.

* A frame `{"jsonrpc":"2.0", "id":1,  "result":{"text":"<host> & aé 1.50e2","n":1.50e2}}`
  written through `Transcript` came out as
  `{"direction":"received","frame":{"jsonrpc":"2.0","id":1,"result":{"text":"<host> & aé 1.50e2","n":1.50e2}}}`.
  `Transcript` drops whitespace between tokens and escapes `<`, `>` and `&`. It keeps the number as
  written.
* A line that is not JSON was written as a JSON string. A line with no newline when `Close` ran was
  dropped.
* `/bin/sh -c 'for i in 1 … 12; do echo "line $i" >&2; done; exit 3'`, with caps of 4096 bytes and
  5 lines and `JoinStderrTail`, gave the Wait error `"exit status 3\nagentproc: the agent's standard
  error ended: line 8\nline 9\nline 10\nline 11\nline 12"`. That is six lines, and `errors.As` finds
  the `*StderrError`.
* `echo "only line" >&2; exit 0` gave a nil error, so no tail was joined. `StderrTail()` still
  returned `"only line\n"`.
* `exit 4` gave `"exit status 4"`, with an empty tail.

## Decision Drivers

* **An operator should see why an agent died, where they see that it died:** on the phone, and in
  the session's history.
* **No secrets in source, `.env`, CLI args or logs.** The session history saved to disk is a log in
  practice. Text the agent wrote must not carry a value Magic knows to be secret into it.
* **The committed fixtures are public** (0151's first driver). A miss can't be undone by editing
  the working tree.
* **0151 requires byte-verbatim frames:** "their whole purpose is to reproduce an engine's own
  bytes". The other 0151 rules stand too: derive nothing from the host (D2), check the artefact and
  not only the rule (D6), and say what redaction does not cover (D7).
* **Magic's half of the wire carries more than the agent's half.** It holds MCP header values, file
  contents, and terminal output: `ls -la` lists the account name as each file's owner.
* **A replay must not act on the host.** A recorded `terminal/create` played to Magic runs a real
  command.
* **Existing readers and tests keep working:** ten fixture readers, the guard, and the
  death-message test.
* **Provider quota is spent only with the owner's word.** A two-way fixture needs one live run.
* **Cross-platform.** The capture's redaction, the tail and the guard behave the same on darwin,
  linux and windows.

## Considered Options

### Item 1, the stderr tail

* **1A.** Set `JoinStderrTail`, and report Wait's error text as it is.
* **1B.** Set the SDK's caps. Magic writes the tail into its own messages: an unexpected exit of any
  status, and a failure while starting. It removes the secret values it knows first.
* **1C.** Write the tail to the daemon's log at warn level only. The phone's message stays as it is.
* **1D.** Change nothing.

### Item 2, a capture of both ways

* **2A.** Use the SDK's `agentproc.Transcript` as the capture's writer, with redaction wrapping it.
* **2B.** `wirecap` writes the same record form itself, with each frame verbatim, to one file per
  agent process. `frames.jsonl` stays exactly as it is.
* **2C.** Replace `frames.jsonl` with the record form for ACP agents, and change its readers.
* **2D.** Keep capturing the agent's output only.

### Item 3, replay

* **3A.** Replay tests in `acpagent` through `acptest.ConnectReplay`, on a session built as
  `newWireSessionFor` builds one.
* **3B.** Replay through `agentproc.InProcess` and `ReplayAgent.Serve`, by giving `spawnAgent` a
  seam for how it starts the agent, so that the exit watcher and the taps run too.
* **3C.** Magic's own replayer, built on `wireAgent`.
* **3D.** No replay. Keep the hand-written wire tests.

## Decision Outcome

Chosen options: **1B**, **2B** and **3A**.

* **1B**, because it is the only option that covers the two deaths an operator meets most: an agent
  that exits with status 0, which `JoinStderrTail` passes over (probe), and an agent that dies
  before it answers `initialize`. It also removes the secrets Magic knows before the text is saved.
* **2B**, because it gives the SDK's record form, which `acptest` reads, without giving up 0151's
  verbatim frames, which `Transcript` re-encodes (probe). It also leaves every existing reader of
  `frames.jsonl` untouched.
* **3A**, because it needs no product change. It reuses the session construction and connection
  options the wire tests already trust.

### The decisions

#### Item 1: the stderr tail

* **D1. Keep the tail.** `spawnAgent` sets ~~`StderrTail: 1024` and `StderrTailLines: 5`~~
  `StderrTail: 4096` and `StderrTailLines: 10` on its `agentproc.Command`, and leaves
  `JoinStderrTail` unset. `slogWriter` stays as `Cmd.Stderr`, and the SDK forwards every byte to it,
  so the debug log and the limit scan are unchanged.
  * *Owner's answer, 2026-10-29 (question 2):* "10 lines, 4KiB and re-assess the phone parser to
    allow more history and effective use of the additional bounded memory. ensure the memory is
    bounded, just expanded to 4KiB to be able to capture more data for troubleshooting when needed."
  * **The memory stays bounded.** The byte cap bounds what the tail holds (`agentproc.go:76-77`).
    After each write the SDK's writer keeps at most 4096 bytes, and it lets go of a backing array
    more than twice its size (`stderr.go:41-82`). That is one such buffer per agent process,
    whatever the agent writes. The message built from it is at most its first line plus 4096 bytes.
  * The phone side is D20.
* **D2. The death message carries the tail.**
  * When the exit watcher reports an exit (`acpagent.go:611-615`) and `proc.StderrTail()` is not
    empty after removal (D4), the message becomes the present first line, then `"\nlast stderr:\n"`,
    then the tail with its final newline trimmed.
  * The first line does not change. This holds for every exit status, 0 included.
  * An agent with no stderr gets the message it gets today.
* **D3. A failure while starting carries the tail.**
  * The errors `spawnAgent` returns after `killTree`: `initializeError`, both
    `acp authenticate (<id>)` errors, and the auth-method selection error. Each wraps the tail the
    same way, after `killTree` has returned. `Kill` returns Wait's error, so the tail is final then.
  * The wrapped error keeps its `%w` chain.
  * ~~`writeSessionErr` still clips to 300 bytes for the phone.~~ `writeSessionErr` clips its
    message to 300 bytes (`ws/server.go:2165-2170`), which would cut a start-up tail off before the
    phone ever sees it. For an error that carries a tail, the clip applies to the text before
    `"\nlast stderr:\n"`, and the tail follows it whole, within D1's bound. Every other error is
    clipped as today. The PLAN also checks where the full text is logged, and records it.
* **D4. Remove the secrets Magic knows from the tail.** Before the tail is used:
  * each value of every configured MCP server's `Headers` is replaced with `<redacted>`;
  * so is the value of each credential environment variable the provider's `Spec` names. This is a
    new `Spec` field. grok names `XAI_API_KEY`.
  * A value shorter than 8 bytes is not replaced. It would be replaced inside other words, and is
    not a credential's shape.
  * Removal runs on the tail only. The debug log stays as it is today.
  * This is not a guarantee. An agent can print a secret Magic never held. The code comment says
    so, as 0151 D7 does for `wirecap`.
* **D5. Tests.** Each is seen failing on a scratch copy first.
  * They use the test binary's helper agent (`agentproc_test.go:31-40`), with a new mode that writes
    lines to stderr and exits with a status the test chooses.
  * Exit status 3 with fifteen lines: the message has the last ten, and its first line still
    matches `TestSpawnedAgentDeathIsOneDisconnect`.
  * Exit status 0 with stderr: the message has the tail.
  * No stderr: the message is unchanged.
  * A configured header value and a named environment variable's value, written to stderr, come out
    as `<redacted>`.
  * A 64 KiB line, and 1 MiB of short lines: at most 4096 bytes are kept, and the message is no
    longer than its first line plus 4096 bytes.
  * An exit before the `initialize` answer: the returned error has the tail, and `writeSessionErr`
    passes it on with only the text before it clipped.
  * The tests run on darwin, linux and windows. The helper is the test binary, so they need no
    shell.
* **D20. The phone shows a long error collapsed, and expands it on a tap.** *Added 2026-10-29 from
  the owner's answer: "Collapsed, tap to expand (Recommended)".*
  * Today the chat clips every error item to 300 characters as it is reduced
    (`transcript_reducer.dart:253-264`, `_clip` at `:1129`), so the rest never reaches the
    transcript.
  * From now on, the reducer keeps the error's full text up to a bound of 8 KiB. That is D1's 4096
    bytes, the longest first line D3 lets through, and margin. Anything beyond the bound is clipped
    with an ellipsis, as now. So the phone's memory per item stays bounded, at a larger size.
  * The error item shows its first line and a two-line preview. A tap expands it to the full text,
    in a monospace, selectable block. A tap again collapses it. An error of three lines or fewer
    shows as it does today, with no control.
  * It covers every error item, not only agent deaths, since the reducer cannot tell them apart.
    `errorKind` and `retryAt` keep their present rendering.
  * Widget tests: collapsed by default; a tap expands and another collapses; the bound clips a
    longer text; a short error has no control. Each is seen failing first on a scratch copy. They
    assert on structure, not on text width, since the test font is fixed-width.
  * `dart format`, `flutter analyze` and `flutter test` (`make preflight`) gate it.

#### Item 2: a capture of both ways

* **D6. One two-way file per agent process.**
  * When capture is on, `wirecap` gives each ACP session a second file:
    `<dir>/<provider>/transcripts/<UTC start time>-<sequence>.jsonl`.
  * It is in the `acptest` record form: one
    `{"direction":"sent"|"received","frame":<frame>}` object per line.
  * One file per process means a recording is one connection, which a replay needs. The shared,
    interleaved `frames.jsonl` cannot give that.
  * `frames.jsonl` keeps its name, its form and its writer, so 0151's rules and every existing
    reader stay as they are.
* **D7. Frames are written verbatim.**
  * `wirecap` writes the record line itself. When the redacted frame is valid JSON it is inserted as
    it is, and otherwise it is written as a JSON string. `json.Unmarshal` into `acptest.Record`
    keeps a `json.RawMessage` byte for byte, so `ReadRecords` reads the file.
  * The SDK's `Transcript` is not used, because it re-encodes each frame (probe).
  * A line with no newline when the capture closes is written as a string, not dropped: the end of a
    stream is what a capture of a death most needs.
* **D8. Both taps.**
  * `acpagent` sets `InputTap` to the capture's sent side.
  * The output tap writes each frame to `frames.jsonl` as now, and to the transcript as received.
  * `closeCapture` also warns on dropped input bytes, naming each direction.
* **D9. Redaction on the sent side.** Both sides of the transcript get 0151's home and host rules.
  On top of those:
  * the configured MCP header values, and the named credential environment values, become
    `<redacted>` (as D4);
  * the running account's name becomes `<user>`. It is replaced in both forms, under 0151's 4-byte
    floor (`minHostName`), with nothing derived from the host beyond `os/user`, as 0151 D2 allows
    for the home.
  * An account name shorter than the floor can't be replaced safely. The capture then writes no
    transcript, says so once at warn level, and keeps writing `frames.jsonl`.
* **D10. The guard reads the transcripts.**
  * `TestCommittedFixturesCarryNoIdentifiers` also globs `testdata/wire/*/transcripts/*.jsonl` and
    applies its checks to the decoded frame of each record.
  * It also fails on the running account's name, as D10 of 0151's amendment does for the host's.
  * The existing floor of 4 `frames.jsonl` fixtures stays. A floor for transcripts arrives with the
    first committed one.
  * Each new check is seen failing on a scratch copy.
* **D11. The package comment says what the sent side carries.** File contents (`fs/read_text_file`
  answers), terminal output (`terminal/output` answers), prompt text and MCP configuration. None of
  it is redacted beyond D9, so a transcript bound for the repository is read in full and curated,
  as 0151 F8 requires of frames.
* **D12. Codex and httpagent are unchanged.**
  * Their transports don't go through `agentproc`.
  * codex writes its own requests through its app-server connection.
  * httpagent's requests are HTTP calls, not frames on a stream.
  * A two-way capture of either is its own record, if one is wanted.

#### Item 3: replay

* **D13. Replay tests live in `acpagent` and use `acptest.ConnectReplay`.**
  * A helper builds a `session` the way `newWireSessionFor` does, and connects it with
    `ConnectReplay(t, records, s, clientConnOptions(nil, s)...)`.
  * The records come from a committed transcript.
  * A departure from the recording fails the test, through `ConnectReplay`'s own cleanup.
* **D14. A replay never acts on the host.**
  * The replay helper refuses, before it connects, a recording that holds an agent request for
    `terminal/create` or `fs/write_text_file`.
  * The session under test runs with `FSRoots` and its cwd set to `t.TempDir()`, so a recorded
    `fs/read_text_file` outside them is refused.
  * Each refusal is seen failing on a planted recording.
* **D15. The first replay tests.**
  * One recorded grok connection: `initialize`, `session/new`, one `session/prompt` streamed to
    `end_turn`, and grok's `_x.ai/session_notification` usage. The test asserts the events Magic
    emits: assistant text, usage, and `turn_complete`.
  * `ValidateGolden` over every committed transcript. A record grok sends that the SDK's types
    refuse is a finding for the PLAN to record, not something to skip.
  * The `session/load` against `session/resume` question from C3 Deviation 3 is answered from the
    transcript's sent side when the capture holds a reload.
* **D16. Magic's requests are not compared byte for byte yet.** A golden of the sent side would
  differ from run to run: the session cwd is a temp directory, `clientInfo.version` is the build's,
  and request ids depend on order. `ConnectReplay`'s departure check already fails a request the
  recording does not hold. A golden waits for a record that needs it.
* **D17. One live run supplies the first transcript.**
  * It is a `make live-grok` run with capture on, which needs the owner's word for the quota when
    its PLAN runs.
  * The last run took 596 s of the target's 600 s (C3, step 1). ~~So the PLAN either narrows `-run`
    to the tests D15 needs or waits on the timeout's own record.~~ The owner's answer, 2026-10-29:
    "Fix the timeout first", in "Own MADR/PLAN pair (Recommended)". So D21 comes first, and the
    capture is the full `make live-grok` suite once D21 has landed. This record does not change the
    timeout.
  * The transcript is committed under the grok version that produced it, beside that version's
    `frames.jsonl` when one exists. The known-good pin (0137) is not moved by this record.

#### Records and order

* **D18. Three PLANs, in the owner's order.** Each is presented for approval on its own:
  * `0216-PLAN-show-an-agents-last-stderr-when-it-dies.md` (D1 to D5, and D20's phone change);
  * `0216-PLAN-record-both-ways-of-the-acp-wire.md` (D6 to D12);
  * `0216-PLAN-replay-recorded-acp-connections.md` (D13 to D17).

  The first does not depend on the others. The third needs the second's committed transcript, and
  D21's record before its live run.
* **D21. The `make live-grok` timeout is its own MADR and PLAN pair, and it lands before D17's
  capture run.** *Owner's answer, 2026-10-29: "Own MADR/PLAN pair (Recommended)".* It is a
  live-test and `Makefile` concern, not stderr, capture or replay, so it takes the next free number
  from `scripts/check_records.py --next`. The replay PLAN cites it as a prerequisite by full
  filename once it exists.
* **D19. The records this touches.**
  * 0207's D50 gets a dated annotation, keeping its text: carried by this record, as one MADR
    with three PLANs, as the owner asked on 2026-10-29 (owner's answer: "Annotate D50 in 0207
    (Recommended)").
  * 0151 gains a short amendment saying D6 to D11 extend its rules to the sent side. Its decisions
    stand.
  * `docs/README.md`'s index is regenerated with `scripts/check_records.py --write-index`.

### Consequences

* Good, because an agent that dies, or that fails to start, says what it last wrote, on the phone
  and in the saved history. That includes an exit of status 0.
* Good, because a value Magic knows to be secret does not reach the saved history through the tail
  or a capture.
* Good, because a two-way recording answers questions a one-way one can't, such as C3's load or
  resume, and lets tests run Magic's whole session path over real engine frames.
* Good, because replay fails a test when Magic starts sending a request the recorded engine never
  saw.
* Good, because `frames.jsonl`, its readers and 0151's verbatim rule are unchanged.
* Neutral, because the death message gains up to ten lines and 4096 bytes. The phone shows it
  collapsed and expands it on a tap (D20), and the saved history keeps all of it.
* Neutral, because every long error on the phone, not only an agent's death, now collapses with an
  expand control, and keeps up to 8 KiB where it kept 300 characters.
* Neutral, because `Spec` gains a field naming credential environment variables. Only grok sets it.
* Bad, because stderr text written by another program now reaches the phone and the saved history.
  D4 removes what Magic knows, and cannot remove what it doesn't.
* Bad, because a transcript carries file contents and command output, which no pattern redacts.
  Curation by reading stays the control, as for frames today.
* Bad, because the account-name redaction is string surgery on a short word. A name of 4 to 6
  letters can occur inside ordinary words, so a transcript may come out with `<user>` inside them.
  That garbles the text but leaks nothing, and the PLAN's curation reads every line.
* Bad, because the first transcript costs one live grok run, and waits for D21's timeout record.
* Bad, because a replay checks Magic against what one engine version sent once. It does not check
  the engine, and timing is not reproduced.

### Confirmation

* **Item 1.** D5's tests pass on darwin, linux and windows, and each was seen failing on a scratch
  copy. `TestSpawnedAgentDeathIsOneDisconnect` passes unchanged. D20's widget tests pass under
  `make preflight`, each seen failing first. On the Android emulator, an agent death's error shows
  collapsed and expands to its tail.
* **Item 2.**
  * A capture of a helper-agent session writes a transcript whose records `acptest.ReadRecords`
    reads, with both directions present.
  * Its frames are byte-equal to what the tap received, apart from redaction.
  * `frames.jsonl` is byte-equal to the capture today's code writes.
  * D9's needles are replaced in both forms. D10's guard fails on each planted shape.
* **Item 3.**
  * D15's replay test passes against the committed transcript, and fails when a planted extra
    request is sent.
  * D14's refusals fail on planted recordings.
  * `ValidateGolden` runs over every committed transcript, with its findings recorded.
* **Everywhere.** `make test`, `make race`, `make staticcheck` (exceptions unchanged at 56),
  `make check-records`, `make markdownlint-docs`, the guard's redact preview, and `make ci-windows`
  on the Windows test host.

## Pros and Cons of the Options

### 1A: `JoinStderrTail`, the error as it is

* Good, because it is two fields, and `errors.As` finds the `*StderrError`.
* Bad, because an exit of status 0 gets no tail (probe). An agent that gives up and exits cleanly
  mid-session is a death Magic did not ask for.
* Bad, because the start-up path does not go through the watcher's message, so the commonest case,
  an agent that cannot start, is still not covered.
* Bad, because the SDK's wording ("agentproc: the agent's standard error ended:") reaches the phone
  as Magic's text, and nothing is removed from the tail.

### 1B: the SDK's caps, Magic's own messages (chosen)

* Good, because it covers every unexpected exit and every failure while starting.
* Good, because the first line, which tests and the phone's 300-character clip see first, is
  unchanged.
* Good, because the secrets Magic holds are removed before the text is saved.
* Bad, because `Spec` gains a field, and Magic owns a small formatting and removal function.

### 1C: the daemon's log only

* Good, because no stderr text reaches the phone.
* Bad, because the operator learns why the agent died only by reading the host's log, which the
  phone exists to avoid.
* Bad, because a warn-level line of another program's text in the rolling log is the same exposure
  as the saved history, with less benefit.

### 1D: nothing

* Good, because nothing changes.
* Bad, because a dead agent stays a bare "process exited", and a failed start loses everything the
  agent said.

### 2A: the SDK's `Transcript`

* Good, because it is the SDK's own writer, and `acptest` reads what it writes.
* Bad, and decisively: it re-encodes every frame, dropping whitespace and escaping `<`, `>` and
  `&` (probe). That breaks 0151's verbatim rule, and turns a `<host>` placeholder written before it
  into `<host>`.
* Bad, because a line with no newline at the end is dropped (probe), and the end of a dying agent's
  output is what a capture most wants.

### 2B: `wirecap`'s own records, one file per process (chosen)

* Good, because frames stay verbatim, and `ReadRecords` still reads them.
* Good, because each file is one connection, which `ReplayAgent` needs and the shared
  `frames.jsonl` cannot give.
* Good, because `frames.jsonl` and its readers are unchanged.
* Bad, because a capture run writes received frames twice.
* Bad, because `wirecap` owns a record writer the SDK also has.

### 2C: replace `frames.jsonl`

* Good, because there is one capture file and one form.
* Bad, because it changes ten readers and three grok fixtures, while codex, kilo and opencode
  keep the old form. Fixtures would then come in two forms under one name.
* Bad, because concurrent sessions still interleave in one file.

### 2D: output only

* Good, because nothing new is exposed.
* Bad, because C3's question stays unanswerable, and replay is impossible (`planReplay` refuses a
  one-way recording).

### 3A: `ConnectReplay` on a test-built session (chosen)

* Good, because it needs no product change, and uses production's connection options.
* Good, because a departure fails the test with the SDK's own message.
* Bad, because the exit watcher, the taps and `spawnAgent`'s handshake are not exercised. Those stay
  with the helper-agent tests.

### 3B: `InProcess` through a seam in `spawnAgent`

* Good, because the whole process path would run against a recording.
* Bad, because it adds a production seam for tests, on the path that owns the agent's process
  lifetime. 0207 D10 moved that path into `agentproc` to stop Magic owning it.
* Neutral, because 3A can grow into this later if a test needs the watcher.

### 3C: Magic's own replayer on `wireAgent`

* Good, because it would fit the existing helper's idiom.
* Bad, because it rewrites what `acptest.ReplayAgent` already does and tests, against the SDK's own
  record form.

### 3D: no replay

* Good, because there is no new test machinery.
* Bad, because real engine frames reach Magic's session code only through unit tests that decode one
  frame at a time.

## More Information

### Open questions for the owner

1. **D4 and D9's removal list.** Is "the configured MCP header values and the credential
   environment variables `Spec` names" the right set? Or should the tail and the transcripts also
   lose the home directory? The phone is the operator's own, but the saved history is a file on the
   host.
2. **D1's caps.** 5 lines and 1024 bytes are proposed. The phone shows 300 characters of the whole
   message.
3. **D9's short account name.** For a name under 4 bytes, the proposal is to write no transcript and
   warn. The alternative is to write it unredacted, which leaves the leak to curation.
4. **D17's live run.** Should the capture run narrow `-run` to D15's tests, or wait for the
   `make live-grok` timeout's own record?
5. **One MADR for three items** replaces D50's "each takes its own MADR", as the owner asked. Is it
   right to record that in 0207 as D19 says?

*Answered 2026-10-29.* The owner was asked seven questions, each with three options and one
recommended. The answers, verbatim:

* **1, the removal list:** "Known secrets only (Recommended)". D4 and D9 stand as written. The home
  directory is not removed from the tail.
* **2, the caps:** "10 lines, 4KiB and re-assess the phone parser to allow more history and
  effective use of the additional bounded memory. ensure the memory is bounded, just expanded to
  4KiB to be able to capture more data for troubleshooting when needed." D1 now says 4096 bytes and
  10 lines, and states the bound. D3 no longer lets the daemon's 300-byte clip cut the tail. D5's
  tests now use the new caps. D20 is added.
* **3, a short account name:** "Skip transcript, warn (Recommended)". D9 stands.
* **4, the live run:** "Fix the timeout first". D17 now runs the full suite after the timeout's
  record, D21.
* **5, recording it in 0207:** "Annotate D50 in 0207 (Recommended)". D19 now says so.
* **The phone's view of a long error:** "Collapsed, tap to expand (Recommended)". This is D20.
* **Where the timeout fix is recorded:** "Own MADR/PLAN pair (Recommended)". This is D21.

One correction to how that sixth question was asked. It cited `settings_store.dart:1089` as a
second clip of chat errors. That line bounds a stored record of a secure-storage failure, which is
not an error event, so D20 does not touch it. The only clip of the error the phone shows is
`transcript_reducer.dart:259`. The question also left out the daemon's own clip of a failed
create, at `ws/server.go:2165-2170`, which D3 now handles.

~~The record stays `proposed` until the owner approves it.~~ *Accepted 2026-10-29:* "accept the
madr".

### Evidence index

* `internal/provider/acpagent/acpagent.go:494-519`, `:526-577`, `:591-636`, `:650-679`, `:973-1014`.
* `internal/provider/acpagent/session.go:825-847`, `:1276-1296`.
* `internal/session/manager.go:1647`, `:3854-3873`; `internal/session/store.go:290`.
* `internal/ws/server.go:2134-2177`; `apps/mobile/lib/data/chat/transcript_reducer.dart:253-264`,
  `:1129`.
* acp-go-sdk2 v1.26.0 `agentproc/stderr.go:41-82`: the tail's bound.
* `internal/daemon/daemon.go:800-819`; `internal/provider/grok/auth.go:80`, `:123`.
* `internal/wirecap/wirecap.go`; `internal/wirecap/fixtures_test.go:64-108`.
* `internal/provider/acpagent/agentproc_test.go:31-40`, `:205-258`; `wireagent_test.go:55-83`.
* `internal/provider/grok/testdata/wire/1.0.70/frames.jsonl`, its `terminal/create` frame.
* acp-go-sdk2 v1.26.0: `agentproc/agentproc.go`, `agentproc/stderr.go`, `agentproc/transcript.go`,
  `agentproc/inprocess.go`, `acptest/records.go`, `acptest/replay.go`, `acptest/golden.go`,
  `acptest/recorder.go`.
* The probe: a scratch Go module outside the tree, against the pinned v1.26.0. Its output is quoted
  in full above.

### Related records

* [0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md](0151-MADR-wirecap-redaction-does-not-know-what-the-wire-carries.md):
  the rules D6 to D11 extend.
* [0196-MADR-remove-the-development-account-name-and-guard-home-paths.md](0196-MADR-remove-the-development-account-name-and-guard-home-paths.md):
  R5, the bare account name no guard catches.
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  I-6, D50 and question 16.
* [0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md](0207-PLAN-close-the-acp-go-sdk2-v1-20-0-gaps.md):
  C3 Deviations 3 and 4.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  the wire capture and the known-good pins.
* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md):
  gobble, the next agent on this path.
* `apps/mobile/docs/decisions/0042-MADR-android-app-remediation.md`, D8: the phone's 300-character
  error clip, which D20 raises.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
  `proposed`: OpenCode sessions on `acpagent`.

## Amendment — 2026-10-29 (A): the stderr tail for every supported agent

*D22 to D30 ~~are proposed~~ decided by the owner, 2026-10-29 (questions 8 to 11, below). D31
~~is proposed, and is decided with the approval of its plan~~ accepted the same day: "accept d31".* When the owner accepted this record, they asked: "also, assess how best
to make this compatible with all supported agents, including gobble-cli." D1 to D5 cover
`acpagent` only, which is grok's transport today and gobble's next. This amendment records how the
other supported agents handle stderr today, what a Go agent such as gobble writes to it, and what
would give every agent the same behaviour. It changes one fact D1 rests on: that 10 lines is enough
to hold a fatal message.

### Facts [observed 2026-10-29 at `0e84b1f0`, gobble-cli read at its local `ccd8aa8`]

* **A-1. The supported agents run on three transports.**

  | Agent | Transport | Process | stderr today | Start-up failure | Death while running |
  | :--- | :--- | :--- | :--- | :--- | :--- |
  | grok | `acpagent` | one child per session, through `agentproc` | `slogWriter`: debug log, limit scan (`acpagent.go:494-501`) | no tail (`acpagent.go:543-577`) | `"grok process exited: <err>"`, no tail (`:611-615`) |
  | gobble (after 0208 P6 and 0179's PLAN) | `acpagent` | as grok | as grok | as grok | as grok |
  | codex | `codex` app-server connection | one engine shared by its sessions, `os/exec` and `procutil` | `lineRing{max: 20}`: debug log, limit scan (`codex/provider.go:601-608`) | `"initialize: <err>; stderr:\n<tail>"`, logged by `provider.LogStderrTail` (`:873-883`) | each session gets `"engine lost"`, no tail (`codex/session.go:2127-2147`). The log has `"engine exited"` and the error, no tail (`provider.go:1040`) |
  | kilo, opencode | `httpagent` | one server shared by its sessions, `os/exec` and `procutil` | its own `lineRing{max: 20}`: debug log only (`httpagent/provider.go:543-545`, `:1206-1261`) | `"…; recent stderr:\n<tail>"` on a failed health wait or an early exit, logged by `LogStderrTail` (`:603-650`) | each session gets `"<bin> server exited"`, no exit error, no tail (`httpagent/session.go:1360-1364`) |

* **A-2. Two of the three already put stderr on the phone at start-up.** MADR 0069 D7 is why
  `provider.LogStderrTail` exists: "the phone could display text the operator could not grep"
  (`internal/provider/cwd.go:12-25`). That is the same text D3 proposes for `acpagent`, in two
  different wordings (`"; stderr:\n"` and `"; recent stderr:\n"`). `writeSessionErr` clips both to
  300 bytes, so today's tails mostly don't reach the phone either.
* **A-3. The two `lineRing` copies are bounded in lines, not bytes.**
  * Both keep 20 lines. Each caps a line with no newline at 4096 bytes, but a line that does end in
    a newline is kept whole, however long (`codex/provider.go:1335-1365`,
    `httpagent/provider.go:1218-1239`).
  * So one 1 MiB line followed by a newline is held whole, up to 20 times. `LogStderrTail`'s comment
    "the tail is already bounded (lineRing, 20 lines)" is true of lines only.
  * The two copies differ in one way: codex's calls `onLine` for the limit scan, and httpagent's
    has no `onLine`.
* **A-4. gobble writes to stderr only when something goes wrong.**
  * Under `gobble acp`, gobble sets its logger's stderr to nil, because "stdio is the protocol"
    (`gobble-cli/internal/cli/main.go:82-83`). Its logs go to `<state>/logs/gobble.log` (`:246-250`).
  * What does reach stderr: a command's fatal error, printed as `"Error: <message>"` by `exitCode`
    (`:116-132`, `output.go:63`); the `"Warning: no log file: …"` line (`main.go:247`, `:255`); and
    anything the Go runtime writes, a panic above all.
  * gobble's credential messages name a variable, "never a value" (`internal/auth/resolve.go:60-62`).
* **A-5. A Go panic puts its message first, and 10 lines do not reach it.**
  * A scratch program built with `-trimpath` panicked at a chosen stack depth
    (`panicprobe_run.py`, outside the tree):

    | Depth | Lines | Bytes | Line holding `panic:` | In the last 10 lines? |
    | :--- | :--- | :--- | :--- | :--- |
    | 0 | 6 | 147 | 1 | yes |
    | 3 | 12 | 283 | 1 | no |
    | 8 | 22 | 506 | 1 | no |

  * At depth 3 the panic is already 12 lines, and D1's 10-line cap keeps only stack frames. The
    whole panic is under 600 bytes, so a byte bound of 4096 would keep all of it. It is the line cap
    that loses the message.
  * Node's uncaught-error output also puts its message before the stack frames. kilo and opencode
    run on a JavaScript runtime, so the same holds for them. This is from the runtime's documented
    form, not probed here.
* **A-6. Credential values the agents hold.**
  * grok: `XAI_API_KEY` (`grok/auth.go:80`).
  * opencode: six variables (`opencode/auth.go:21-28`: `OPENROUTER_API_KEY`, `HF_TOKEN`,
    `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GROQ_API_KEY`, `XAI_API_KEY`).
  * codex: its key is in `auth.json` under `OPENAI_API_KEY` (`codex/adapter.go:130-136`).
  * kilo: none found by name in Magic's code.
  * gobble: one variable per provider, named by its own catalog (`resolve.go:81-84`). So no fixed
    list in Magic would stay complete.
  * Every agent inherits the daemon's environment: `os.Environ()` for codex and httpagent
    (`codex/provider.go:569`, `httpagent/provider.go:534`), and `launch.Command`'s default for
    `acpagent`.
  * MCP header values are `acpagent`-only: `acpAgentConfig` is the only converter that copies them
    (`daemon/daemon.go:796-822`).
* **A-7. The phone renders every agent's errors the same way.** All three transports emit a
  `TypeError` event. The reducer clips it (`transcript_reducer.dart:253-264`), and `chat_bubble.dart`
  renders an item with no `errorKind` as one centred line of text (`:542-565`). Items with an
  `errorKind` get their own cards: limit and server (`_LimitNotice`, whose detail is
  `maxLines: 3`, `:902-912`), permission, and auth. So D20 reaches every agent's death message,
  since none of them sets an `errorKind`.
* **A-8. Tests that pin today's text.**
  * `TestStartServerBailsWhenEngineExitsImmediately` requires `"exited during startup"`
    (`httpagent/provider_test.go:97-117`).
  * `TestLineRingKeepsTail` requires `"b\nc\nd"` from `lineRing{max: 3}`
    (`httpagent/session_test.go:450-457`).
  * `TestSpawnedAgentDeathIsOneDisconnect` requires `"testagent process exited: "`.
  * `handleUnexpectedEngineExit` already has a test seam that takes an `engineAttempt`
    (`codex/managed_daemon_p3_test.go:158`).
  * No test pins `"engine lost"` or `"server exited"` text.
  * The reducer's surrogate-pair test builds `'x' * 299` against the 300 cap
    (`transcript_reducer_test.dart:1202-1233`). It would pass without clipping anything once the
    cap rises, unless it is rebuilt on the new constant.
* **A-9. 0214, proposed, would move OpenCode's sessions onto `acpagent`.** They would then get the
  `acpagent` path, and any server it still starts keeps httpagent's.

### The decisions this amendment proposes

* **D22. One behaviour for every supported agent.** grok, gobble, codex, kilo and opencode each:
  * keep a bounded tail of their engine's stderr;
  * add it to a failure while starting, and to the message every session gets when the engine dies
    while running;
  * remove the same known secrets from it;
  * and go through the same error type, which `writeSessionErr` and the phone handle.

  The agent-specific part is only which credential values it contributes (D27).
* **D23. One tail writer, in a new package, `internal/provider/stderrtail`.** It holds:
  * `Writer`: the bounded tail, D24's caps, starting at a line's beginning, and keeping the end of a
    last line longer than the byte cap. It forwards every byte to the writer it wraps, and is safe
    for concurrent use.
  * `Scrub(tail, secrets)`: D4 and D27's removal, of each value of at least 8 bytes, raw and
    JSON-escaped.
  * `Error{Err, Tail}`: `Error()` is `Err.Error()`, then `"\nlast stderr:\n"`, then the tail.
    `Unwrap()` returns `Err`.
  * `Message(headline, tail)`, for an event that is not an error value.

  `acpagent` sets this `Writer` as `Cmd.Stderr` around `slogWriter`, and leaves the SDK's
  `StderrTail` caps unset. That amends D1's "on its `agentproc.Command`". `os/exec` copies stderr
  on a goroutine that `Cmd.Wait`, and so `agentproc`'s `Wait` and `Kill`, wait for, bounded by the
  `WaitDelay` that `agentproc` sets (`agentproc.go:164-168`, `:212-217`). So the tail is final at
  the same points as the SDK's.
* **D24. The byte cap governs; the line cap only backs it up.** Amends D1's 10 lines.
  `stderrtail` keeps 4096 bytes and at most 64 lines. A-5 shows that 10 lines lose a Go panic's
  message at depth 3, while 4096 bytes hold a 22-line panic eight times over. The memory bound is
  unchanged: 4096 bytes per engine process.
* **D25. codex and httpagent drop their rings.** Each `lineRing` keeps its debug logging and, in
  codex, `onLine`. Its `ring`, `max` and `tail()` go: the `stderrtail.Writer` in front of it is the
  tail. That removes A-3's unbounded growth. `TestLineRingKeepsTail` moves to `stderrtail` as a
  table test of the caps. `LogStderrTail` stays, and its comment states the byte bound.
* **D26. Every death message names the exit, and carries the tail.**
  * acpagent: D2, unchanged.
  * codex: `engineLost` gets a message. `handleUnexpectedEngineExit` builds
    `"engine lost: codex exited: <exitErr>"` with the tail, and passes it to every session.
  * httpagent: `serverDied` gets a message. The death monitor builds
    `"<bin> server exited: <err>"` with the tail.
  * The first line stays as the phone shows it today, plus the exit error. The `"engine exited"`
    warnings in both logs also log the scrubbed tail through `LogStderrTail`.
* **D27. The secrets each agent contributes.**
  * **Every agent:** the value of each variable in the environment the engine was started with whose
    name ends in `_API_KEY`, `_TOKEN`, `_SECRET` or `_PASSWORD`. Magic holds these values: they are
    its own environment, passed down. This covers gobble's per-provider variables without Magic
    listing them (A-6).
  * **acpagent:** also the configured MCP header values (D4), and a new `Spec.CredentialEnv` for a
    name that doesn't match the pattern. grok sets nothing extra, since `XAI_API_KEY` matches. The
    gobble Spec in 0179's PLAN sets what its catalog needs.
  * **codex, httpagent:** the pattern only. opencode's six variables and codex's `OPENAI_API_KEY`
    all match it.

  D4's 8-byte floor applies, and the code comment says the list cannot be complete.
* **D28. Start-up failures use `stderrtail.Error` everywhere.**
  * codex's `"; stderr:\n"` and httpagent's `"; recent stderr:\n"` become the one separator.
  * Each message's text before the tail stays as it is. `"exited during startup"` and
    `"did not become healthy"` keep `TestStartServerBailsWhenEngineExitsImmediately` passing.
  * `writeSessionErr` (D3) finds the tail with `errors.As`, not by its wording.
* **D29. Tests for every transport.** Each is seen failing first on a scratch copy.
  * `stderrtail`: the caps (a 64 KiB line, 1 MiB of short lines, 70 short lines), the line-start
    rule, concurrent writes under `-race`, `Scrub`'s floor and both forms, and `Error`'s chain and
    text.
  * acpagent: D5's tests, plus a Go-panic-shaped stderr whose `panic:` line survives.
  * codex: `handleUnexpectedEngineExit` with an `engineAttempt` whose stderr holds a tail; every
    session's error carries it.
  * httpagent: the death monitor, through `TestHelperProcessExitsNonZero`'s helper writing to
    stderr; the early-exit start error carries a scrubbed tail.
  * ws: `writeSessionErr` keeps a `stderrtail.Error`'s tail after a 300-byte headline.
  * phone: D20's.
* **D30. gobble needs nothing beyond its Spec.**
  * It reaches Magic through `acpagent`, so it inherits D2, D3, D20, D23 and D24.
  * What it writes to stderr, an `"Error: …"` line or a panic, fits D24's bound whole (A-4, A-5).
  * Its Spec sets `CredentialEnv` only for a name D27's pattern misses.
  * No change to gobble-cli is needed. If its own logging ever moves back onto stderr, D24's byte
    bound still holds, and only the last 4096 bytes reach the phone.
* **D31. The OS notification shows the first line only.**
  * Today an `error` event's whole text becomes the notification's body, as
    `"<session label> · <error>"` (`notification_coordinator.dart:233-243`,
    `notification_service.dart:386-391`). With D24 that would put up to 4 KiB of stack frames in a
    system notification.
  * The body takes the error's first line, clipped to 300 characters, the bound the chat used
    before D20. The full text stays one tap away, in the chat (D20).

### Consequences of the amendment

* Good, because all five agents report a death and a failed start the same way, with the same
  bound and the same removal.
* Good, because a Go or JavaScript fatal message survives the tail, where 10 lines would drop it.
* Good, because the two `lineRing` copies stop holding unbounded lines.
* Good, because gobble's per-provider keys are covered without a list Magic would have to keep up.
* Neutral, because codex's and httpagent's start-up wording changes from `"; stderr:"` and
  `"; recent stderr:"` to the shared separator. No test pins those words.
* Bad, because the name pattern misses a credential in a variable with another name. `CredentialEnv`
  covers a known one, and the code comment says the list is not complete.
* Bad, because `acpagent` no longer uses the SDK's tail, so Magic owns about 60 lines that the SDK
  also has. The reason is that codex and httpagent can't use the SDK's, and one writer gives one
  test table.

### Questions this amendment adds

* **8. D24's caps.** 4096 bytes and 64 lines (proposed), or D1's 10 lines with each runtime's fatal
  line kept as well, or D1 as it is?
* **9. D23's writer.** One `stderrtail.Writer` for every transport (proposed), or the SDK's caps for
  `acpagent` and `stderrtail` for the others, or `acpagent` only?
* **10. D27's source.** The name pattern over the engine's environment plus named extras
  (proposed), or named lists per agent only, or MCP header values only?
* **11. D26 and D28's wording.** The shared separator, and the exit error in every death message
  (proposed), or the shared error type with each transport keeping its wording, or `acpagent` only?

*Answered 2026-10-29:*

* **8.** "4 KiB, 64-line backstop (Recommended)". D24 stands, and amends D1's 10 lines.
* **9.** "One shared writer (Recommended)". D23 stands, and amends D1's use of the SDK's caps.
* **10.** "Name pattern + extras (Recommended)". D27 stands.
* **11.** "Shared separator + exit error (Recommended)". D26 and D28 stand.

### Evidence index of the amendment

* `internal/provider/codex/provider.go:569`, `:601-608`, `:873-883`, `:1003-1065`, `:1322-1410`;
  `codex/session.go:2127-2147`; `codex/adapter.go:130-136`;
  `codex/managed_daemon_p3_test.go:158`.
* `internal/provider/httpagent/provider.go:508-726`, `:1206-1261`; `httpagent/session.go:1360-1364`;
  `httpagent/provider_test.go:61-117`; `httpagent/session_test.go:450-457`.
* `internal/provider/cwd.go:12-25`; `internal/provider/opencode/auth.go:18-28`;
  `internal/daemon/daemon.go:796-822`.
* `apps/mobile/lib/features/chat/chat_bubble.dart:516-565`, `:825-919`;
  `apps/mobile/lib/data/chat/transcript_reducer.dart:1125-1136`;
  `apps/mobile/test/transcript_reducer_test.dart:1173-1233`.
* gobble-cli, at its local `ccd8aa8` (origin's HEAD is `6ee20140`): `internal/cli/main.go:72-132`,
  `:227-264`; `internal/cli/output.go:63-66`; `internal/auth/resolve.go:60-87`.
* The panic probe: a `-trimpath` scratch build outside the tree, with its output tabled under A-5.
