---
status: accepted
date: 2026-10-31
decision-makers: Project Owner
consulted: "acp-go-sdk2 v1.28.0 (connection.go: defaultMaxMessageSize, readBufferSize, readLine, receive; remote/client.go); this repository at 0f6d27a6: internal/provider/codex/transport.go and transport_p3_test.go, internal/provider/acpagent/acpagent.go, internal/relay/config.go and server.go, internal/relayhost/client.go and deadline_test.go, internal/config/config.go, apps/mobile/lib/data/ws/relay_transport.dart, the configs and docs that state the defaults"
informed: operators of mcrelay and of mcremote hosts that use it
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Raise the Codex transport and mcrelay reader limits to 10 MiB, matching acp-go-sdk2

## Context and Problem Statement

acp-go-sdk2, the SDK Magic's ACP providers run on, reads a message of up to **10 MiB**
(`connection.go:26`, `defaultMaxMessageSize = 10 * 1024 * 1024`; `remote/client.go:13` the same).
Magic's ACP reader states that bound: `maxACPMessageSize = 10 << 20`
(`internal/provider/acpagent/acpagent.go:1144`). httpagent's SSE line cap is 10 MiB as well
(`maxSSELine`, `internal/provider/httpagent/provider.go:833`).

Two readers are still at **1 MiB**. The owner decided on 2026-10-31 to raise both to 10 MiB, to
match the SDK: "both, the codex jsonl transport and also the limit set in mcrelay."

**The Codex transport.** `internal/provider/codex/transport.go:17`,
`maxTransportFrameBytes = 1 << 20`. One constant bounds every Codex transport, in both directions:

* the stdio JSONL reader: `bufio.NewReaderSize(reader, maxTransportFrameBytes+2)` (`:37`), with a
  longer line drained and answered with an error (`:71-77`);
* the three WebSocket transports' `conn.SetReadLimit(maxTransportFrameBytes)` (`:118`, `:161`,
  `:178`);
* both `Send` methods' refusal of a longer frame (`:53-55`, `:183-185`).

The JSONL reader's buffer is the limit itself, allocated when the transport is made. The SDK does
not do that: `receive` reads through a 128 KiB `bufio.Reader` (`readBufferSize = 128 << 10`,
`connection.go:30`, `:1320`), and `readLine` (`:1297-1317`) appends `ReadSlice` chunks until the
newline, so a line costs what it holds, up to the limit. Raising the constant alone would make each
Codex stdio transport allocate 10 MiB up front. Two places make one (`provider.go:687`,
`conn.go:132`).

**mcrelay.** `internal/relay/config.go:57`, `DefaultLimits().MaxMessageBytes = 1 << 20`. It is the
splice's read limit, on the phone's and the host tunnel's WebSocket (`server.go:773`, `:901-902`).
An operator can set `limits.max_message_bytes` up to its 16 MiB ceiling (`config.go:18`). The
pre-auth control plane has its own 64 KiB limit and does not change (`ControlReadLimitBytes`,
`server.go:546`, `:665`, `:797`).

Its partner on the host is the daemon's tunnel bridge. `internal/relayhost/client.go:28`,
`bridgeWSReadLimit = 1 << 20`, used when `relay.max_frame_bytes` is 0 (`:285-288`). The code
(`client.go:50`, `config.go:107`) and the guide (`config-mcrelay.md:207-209`) say it must match
the relay's limit. Nothing ties the two defaults together but those comments.

### What the limits actually see

* **F1 — Relay messages are one socket read each.** The splice copies a message at a time through a
  pooled 32 KiB buffer (`spliceCopyBufSize`, `server.go:81`; the loop at `:949-971`). The host bridge writes each TCP read of up to 32 KiB as
  one message (`client.go:318-327`), and streams each message it reads into TCP (`:342-352`). The
  phone writes each chunk its loopback socket gives it as one message
  (`apps/mobile/lib/data/ws/relay_transport.dart:353-357`). Neither side writes a message near 1 MiB, so the relay limit is
  headroom. Raising it changes no current traffic. A relay and a daemon on different versions
  therefore keep working together.
* **F2 — Neither relay path buffers a whole message.** Both stream through `Reader` and `Writer`.
  A higher limit lets a longer message through; it does not allocate one. The process's 512 MiB
  soft heap ceiling (`ops-mcrelay.md`) is not moved by this change.
* **F3 — The phone protocol's own 1 MiB is a different limit.** The daemon's WebSocket server
  (`maxWSMessageBytes`, `internal/ws/server.go:310`), its outbound cap (`:3608`), the negotiated
  `max_frame_bytes`, and the phone's `kMaxClientFrameBytes` bound protocol messages inside the TLS
  stream, not relay messages. They are not part of this decision.
* **F4 — Codex frames are protocol messages.** A Codex app-server line, or WebSocket message, is
  one JSON-RPC message, the same unit the SDK's and httpagent's 10 MiB bound. No Codex message over
  1 MiB has been observed or recorded. The change is for parity with the SDK, not for a failure
  seen.

## Decision Drivers

* **D-1 — One bound for an agent message across Magic's providers**: the SDK's 10 MiB (owner).
* **D-2 — The relay and the host bridge cannot drift apart.**
* **D-3 — Memory follows the message, not the limit**, as in the SDK.
* **D-4 — A small change**, with the existing behaviour for an oversized message kept.

## Considered Options

* **A — Raise both to 10 MiB. The Codex JSONL reader reads as the SDK does. The host bridge's
  default is the relay's default, by reference.**
* **B — Raise the two constants and the relay default only.**
* **C — Raise only Codex's limit.**

## Decision Outcome

Chosen option: "A", because it is the owner's decision (D-1) carried out so that it holds: B
allocates 10 MiB per Codex stdio transport (D-3) and leaves the bridge's default tied to the
relay's by a comment (D-2); C leaves the relay out, which the owner named.

### The decisions

* **D1 — `maxTransportFrameBytes = 10 << 20`.** It keeps bounding every Codex transport in both
  directions. Its comment names the SDK's default as the reason.
* **D2 — The Codex JSONL reader reads as the SDK does.** A 128 KiB `bufio.Reader`, the SDK's
  `readBufferSize`. Each `Read` appends `ReadSlice` chunks until the newline, and stops keeping them
  once the line passes the limit. An oversized line is still drained through its newline, and still
  answered with the error "codex transport frame exceeds %d bytes". The SDK instead skips the line
  and goes on, which this decision does not adopt (D-4). A `\r\n` ending is still trimmed.
* **D3 — mcrelay's default `max_message_bytes` is 10 MiB.** `DefaultLimits().MaxMessageBytes =
  10 << 20`. The 16 MiB ceiling, the 64 KiB control limit, and an operator's explicit setting are
  unchanged.
* **D4 — The host bridge's default is the relay's.** `relayhost` takes its default from
  `relay.DefaultLimits().MaxMessageBytes`, which it already imports the package for, instead of a
  constant of its own. `relay.max_frame_bytes: 0` then always means "the relay default". The
  daemon's validation range for a set value, 4096 to 16 MiB, is unchanged.
* **D5 — What states the defaults says 10 MiB.** The code comments, `configs/mcrelay.example.yaml`,
  `internal/cli/service/defaults_mcrelay.yaml` (with its template-parity test), the commented
  `Environment=` line in `deploy/systemd/mcrelay.user.service`, the `max_frame_bytes` comments in
  `defaults_mcremote.yaml` and the three `configs/config*.yaml`, `docs/guides/config-mcrelay.md`,
  `docs/guides/config.md`, and `README.md`'s relay-limits row. The guide's bridge note then reads
  "raised above 10 MiB".

  *Amended 2026-10-31 (PLAN Deviation 1).* Nine places, in comments, a test and a guide, gave the
  relay as the reason for a 1 MiB budget: catalog caps in `internal/ws`, `opencode` and `kilo`,
  `docs/guides/protocol-v1.md`, and the relay's own pre-auth test. That reason is wrong after D3.
  The bound they defend is the daemon's 1 MiB protocol frame limit (F3), which this decision does
  not change. On the owner's decision, each now names that limit. The test constant
  `relayFrameLimit` becomes `protocolFrameLimit`, with the same value.

### Consequences

* Good, because a Codex message between 1 and 10 MiB is read rather than failing the transport, as
  it already is for ACP and httpagent.
* Good, because the Codex stdio reader holds 128 KiB until a line needs more, where today it holds
  1 MiB from the start.
* Good, because the relay and bridge defaults can no longer disagree (D4).
* Neutral, because relay traffic does not change: its messages are one socket read each (F1).
* Bad, because a relay installed from the service template keeps its explicit `1048576` until its
  operator edits it. The template's own default moves; an installed copy does not.
* Bad, because a Codex peer can now make the daemon hold up to 10 MiB for one line, where it was
  1 MiB. That is the SDK's bound for every ACP agent already.

### Confirmation

* New tests, each seen failing first on a scratch copy of the tree without the change:
  * Codex JSONL: a 2 MiB line is read intact. A line one byte over the limit is refused with
    "frame exceeds", and the next line is read. A 2 MiB `Send` is accepted.
  * Codex WebSocket: a 2 MiB message is read intact.
  * Codex JSONL's buffer is 128 KiB (`bufio.Reader.Size`).
  * relay: `DefaultLimits().MaxMessageBytes` is `10 << 20`, and a 2 MiB message splices phone to
    host under the defaults.
  * relayhost: a 2 MiB frame crosses the bridge with `MaxFrameBytes` 0. The bridge's default
    equals `relay.DefaultLimits().MaxMessageBytes`.
* The existing tests pass unchanged, `TestTransportFrameBoundsAndEOF` and the template-parity test
  among them.
* `make pre-add-check`, `go vet` on three systems, `make staticcheck`, `make vulncheck`,
  `make race`, and the Linux and Windows test hosts pass.

## Pros and Cons of the Options

### A — Both to 10 MiB, SDK-style reader, bridge by reference

* Good, because it meets D-1 to D-4.
* Bad, because it touches more than two constants: the reader, and the files in D5.

### B — Constants and relay default only

* Good, because it is the smallest diff.
* Bad, because each Codex stdio transport allocates 10 MiB at start (D-3).
* Bad, because the bridge default stays a second constant that must be kept equal by hand (D-2).

### C — Codex only

* Good, because the relay is left alone, and F1 says it does not need the room.
* Bad, because the owner named the relay as well.

## More Information

### Not decided here

* The phone protocol's 1 MiB (F3).
* Codex's handling of an oversized line: error, as now, rather than the SDK's skip-and-continue.
* opencode's `maxPromptFrameBytes`, and Codex's 1 MiB terminal replay window
  (`maxTerminalReplayBytes`): different bounds, on what Magic sends or keeps.

### Related records

* [0223-PLAN-raise-the-codex-and-mcrelay-reader-limits-to-10-mib.md](0223-PLAN-raise-the-codex-and-mcrelay-reader-limits-to-10-mib.md),
  this decision's plan.
