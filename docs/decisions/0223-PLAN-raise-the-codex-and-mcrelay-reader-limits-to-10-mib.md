---
status: in-progress
date: 2026-10-31
associated-madr: "0223-MADR-raise-the-codex-and-mcrelay-reader-limits-to-10-mib.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement raising the Codex transport and mcrelay reader limits to 10 MiB

Associated MADR: [0223-MADR-raise-the-codex-and-mcrelay-reader-limits-to-10-mib.md](0223-MADR-raise-the-codex-and-mcrelay-reader-limits-to-10-mib.md)

## Goal

Every Codex transport reads and sends a message of up to 10 MiB, and the stdio reader holds only
what a line needs (MADR D1, D2). mcrelay's default message limit is 10 MiB, and the host bridge's
default is the relay's by reference (D3, D4). Every place that states either default says 10 MiB
(D5).

## Scope

**Code:**

* `internal/provider/codex/transport.go`: the constant, and the JSONL reader (D1, D2);
* `internal/relay/config.go`: `DefaultLimits().MaxMessageBytes` and its comment (D3);
* `internal/relayhost/client.go`: the bridge default and its comments (D4);
* `internal/config/config.go:106-108`: the `MaxFrameBytes` comment (D5).

**New tests:**

* `internal/provider/codex/transport_limit_test.go`;
* `internal/relay/limit_default_test.go`;
* `internal/relayhost/bridge_default_test.go`.

**Configs and docs (D5):**

* `configs/mcrelay.example.yaml`, `internal/cli/service/defaults_mcrelay.yaml`, and
  `deploy/systemd/mcrelay.user.service`;
* the `max_frame_bytes` comments in `internal/cli/service/defaults_mcremote.yaml`,
  `configs/config.example.yaml`, `configs/config.prod.example.yaml` and
  `configs/config.mesh-grok.yaml`;
* `docs/guides/config-mcrelay.md`, `docs/guides/config.md`, and `README.md`'s relay-limits row.

**Records:** this pair, and `docs/README.md`'s two index rows for it.

**Not in scope:** the phone protocol's 1 MiB, Codex's oversized-line behaviour, and the other
bounds the MADR's "Not decided here" names. The 0222 pair, untracked in the tree, is not this
pair's and is left alone.

## Constraints

* **C1 — One commit for the change**, after this pair's own commit. The agent stages; the owner
  commits with `git commit --no-edit`.
* **C2 — The owner's word comes first** for the Linux and Windows test-host runs (S4). Asked for
  at approval.
* **C3 — No push without an explicit ask in the same turn.** `leftovers/guard_preview.py` before
  staging, `leftovers/guard_range.py` before asking the owner to push.
* **C4 — No provider quota.** No live-tagged Codex test runs.
* **C5 — Every child process goes through `procutil.Command`** (`TestNoBareSpawnOutsideProcutil`).
  The tests here start none.

## Implementation Steps

### P0 — Before the first write (read only)

1. `git status --short`: only this pair, and work that is not this plan's, which is left alone.
   Record `HEAD`.
2. The MADR's line references still hold: `transport.go:17`, `:37`, `:53-55`, `:71-77`, `:118`,
   `:161`, `:178`, `:183-185`; `relay/config.go:57`; `relayhost/client.go:28`, `:285-288`.

### S1 — The tests, seen failing first

Written into the tree, then run on a scratch copy of `HEAD` plus the three new test files only
(`leftovers/tree_copy.py`), before any code changes. Each test and what it must show there:

1. `transport_limit_test.go`:
   * `TestJSONLTransportReadsTwoMiB`: a 2 MiB line is read intact. **Red:** "frame exceeds
     1048576 bytes".
   * `TestJSONLTransportRefusesOverLimitAndContinues`: a line of `maxTransportFrameBytes+1` bytes is
     refused with "frame exceeds", and the next line is read intact. It passes at `HEAD`, as a
     guard. It is shown failing on a plant copy whose reader does not drain the long line.
   * `TestJSONLTransportReaderIs128KiB`: the transport's `bufio.Reader.Size()` is `128 << 10`.
     **Red:** `1048578`.
   * `TestJSONLTransportSendsTwoMiB`: `Send` of a 2 MiB frame succeeds. **Red:** "frame exceeds".
   * `TestWebSocketTransportReadsTwoMiB`: over an `httptest` WebSocket server, a 2 MiB message is
     read intact. **Red:** a read-limit error.
2. `limit_default_test.go`:
   * `TestDefaultMaxMessageBytesIsTenMiB`. **Red:** `1048576`.
   * `TestSpliceCarriesTwoMiBUnderDefaults`: as `preauth_limit_test.go`'s post-auth echo, with a
     2 MiB payload and `DefaultLimits()`. **Red:** the splice closes, and the echo read fails.
3. `bridge_default_test.go`:
   * `TestBridgeDefaultIsRelayDefault`: the limit `relayhost` uses for `MaxFrameBytes` 0 equals
     `relay.DefaultLimits().MaxMessageBytes`. It passes at `HEAD`, where both are 1 MiB. It is shown
     failing on a plant copy that raises only the relay's default.
   * `TestBridgeCarriesTwoMiBByDefault`: as `deadline_test.go`'s frame-cap test, with a 2 MiB frame
     and the default limit. **Red:** the frame is cut short.

The output goes to `leftovers/0223-s1-red.log`, and the plants' to `leftovers/0223-s1-plants.log`.
Every red is the stated failure, and not a compile error.

### S2 — The change

1. `transport.go`:
   * `const maxTransportFrameBytes = 10 << 20`, commented: acp-go-sdk2's `defaultMaxMessageSize`,
     MADR 0223 D1.
   * `const jsonlReadBufferSize = 128 << 10`, commented: the SDK's `readBufferSize`.
   * `newJSONLTransport` uses `bufio.NewReaderSize(reader, jsonlReadBufferSize)`.
   * `Read` appends `ReadSlice` chunks until the newline. Past `maxTransportFrameBytes` it stops
     appending, keeps reading through the newline, and returns the existing error. It trims `\n`,
     then `\r`, as now. The `readMu` locking stays.
2. `relay/config.go:57`: `MaxMessageBytes: 10 << 20, // 10 MiB, acp-go-sdk2's message bound (MADR 0223 D3)`.
3. `relayhost/client.go`: ~~the `bridgeWSReadLimit` constant goes. Line 287 takes
   `int64(relay.DefaultLimits().MaxMessageBytes)`.~~ *As done:* `bridgeWSReadLimit` becomes a
   package `var` set to `int64(relay.DefaultLimits().MaxMessageBytes)`, so S1's tests compile
   against `HEAD` (see the execution record). The comments at `:27` and `:49-51` say the default
   is the relay's. `deadline_test.go`'s two uses move to the same expression, or to a small
   unexported helper both use.
4. `config.go:106-108`: "0 = the relay default (10 MiB)".
5. The configs and docs in Scope: `1048576` becomes `10485760`, "1 MiB" becomes "10 MiB" where it
   names this default, and the bridge note says "raised above 10 MiB". The template-parity test
   (`internal/cli/service/template_parity_test.go`) passes with `defaults_mcrelay.yaml` changed.
6. `gofmt` on every changed Go file. `git diff --stat` shows only Scope's files.

### S3 — Gates on this Mac

1. The new tests in the tree: all pass.
2. `make pre-add-check` on the changed and new Go files.
3. `leftovers/vet_goos.py`: darwin, windows and linux.
4. `make staticcheck` and `make vulncheck`.
5. `make race`: every package `ok`, with no FAIL, panic or DATA RACE line.
6. `leftovers/nonascii_scan.py` on the new Go files.
7. `make check-records` and `make markdownlint-docs`.

### S4 — Gates on the test hosts (C2)

`leftovers/pins_hosts.py` with `PINS_PHASE=0223`, naming the changed and new files:
`go test -race ./...` on the Linux test host, and `make ci-windows` on the Windows test host.

### S5 — Records, staging, and CI

1. This plan's execution record: each step's output.
2. `leftovers/guard_preview.py`. Stage Scope's files. `check_records.py --check-all` on the staged
   tree. The owner commits.
3. `make check-libs` on `HEAD`. `leftovers/guard_range.py` before asking the owner to push.
4. After the push: every Go job and the Docs workflow pass. Record the run ids.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | S1's red tests failed as stated at `HEAD`, the two guards failed on their plants, and all pass in the tree | D1 to D4 |
| V2 | `maxTransportFrameBytes` is `10 << 20`, the JSONL reader is 128 KiB, and an over-limit line is still an error followed by the next line | D1, D2 |
| V3 | `DefaultLimits().MaxMessageBytes` is `10 << 20`, and the bridge default is that value by reference | D3, D4 |
| V4 | No file in Scope still states 1 MiB for these two defaults | D5 |
| V5 | S3's and S4's gates, and CI on the push, pass | all |

## Rollout and Rollback

* **Rollout:** the next release of `mcremote` and `mcrelay`. Relay and daemon versions can be
  mixed in either order, since relay messages are one socket read each (MADR F1). A relay whose
  config sets `max_message_bytes: 1048576` keeps it until its operator changes it.
* **Rollback:** revert the commit. Nothing is stored, and nothing on the wire changes shape.

## Owner decisions needed at approval

* **Q1 — The host runs (C2):** S4 runs on the Linux and Windows test hosts, in scratch trees.

## Not done by this plan

* The phone protocol's 1 MiB bound (MADR F3).
* Codex's skip-and-continue for an oversized line, as the SDK does.
* A live Codex run with a message over 1 MiB (C4). The unit tests stand in for it.

## Execution record

**Approval, 2026-10-31.** The pair was committed as `722dffa7`. The owner then wrote "proceed,
approved, yes to both hosts": the MADR is accepted, this plan is approved, and Q1 is answered yes.

**P0.** `HEAD` `722dffa7`. `git status --short` held this pair's acceptance edits, the untracked
0222 pair, and `docs/README.md`'s index rows for 0221 and 0222. None of the last three is this
plan's, and they are left alone. Every line reference in P0 step 2 held.

**S1, red** (`leftovers/tree_copy.py 0223-red`: `HEAD` plus the three new test files only;
`leftovers/0223-s1-red.log`). Exit 1. Each test failed as stated, and none failed to compile:

* `TestJSONLTransportReadsTwoMiB`: "codex transport frame exceeds 1048576 bytes".
* `TestJSONLTransportReaderIs128KiB`: "JSONL read buffer is 1048578 bytes, want 131072".
* `TestJSONLTransportSendsTwoMiB`: "codex transport frame exceeds 1048576 bytes".
* `TestWebSocketTransportReadsTwoMiB`: "websocket: message too big: read limited at 1048577
  bytes".
* `TestDefaultMaxMessageBytesIsTenMiB`: "DefaultLimits().MaxMessageBytes = 1048576, want
  10485760".
* `TestSpliceCarriesTwoMiBUnderDefaults`: "received close frame: status = StatusMessageTooBig and
  reason = \"read limited at 1048577 bytes\"".
* `TestBridgeCarriesTwoMiBByDefault`: "1048577 bytes crossed, want 2097152 intact".
* The two guards passed at `HEAD`, as S1 says they must: `TestJSONLTransportRefusesOverLimitAndContinues`
  and `TestBridgeDefaultIsRelayDefault`.

**S1, the guards' plants** (`leftovers/0223_plants.py`, each a copy of the changed tree with one
asserted edit, removed afterwards).

* **First run** (`leftovers/0223-s1-plants.log`): the bridge plant, a fixed `1 << 20` default,
  failed `TestBridgeDefaultIsRelayDefault`, "bridge default 1048576, relay default 10485760". The
  drain plant, which returns as soon as a line passes the bound, was **not caught**. The test's
  line was one byte over the bound, so the chunk that crossed it also held the newline, and there
  was nothing left to drain.
* **The test was corrected.** It now runs two cases: one byte over, and 256 KiB over, a read buffer
  and more before the newline.
* **Second run** (`leftovers/0223-s1-plants-2.log`): both plants failed their tests. The drain
  plant gave "line after the over-limit one = \"xxxx…\", want the next line". The corrected test
  passes at `HEAD`, both cases (`leftovers/0223-s1-red-guard.log`, a clean copy of `HEAD` plus that
  file). Both red copies were removed.

**S2.**

* `transport.go`:
  * `maxTransportFrameBytes = 10 << 20`, and `jsonlReadBufferSize = 128 << 10`, each commented
    with its SDK counterpart;
  * `newJSONLTransport` uses the 128 KiB buffer;
  * `Read` follows the SDK's `readLine`. Over the bound it keeps reading through the newline,
    then returns the existing error. A partial last line before EOF still returns the EOF, and the
    returned slice is still a copy.
* `relay/config.go:57`: `10 << 20`.
* `relayhost/client.go` (*step 3, as done*): `bridgeWSReadLimit` stays as a name, now a package
  `var` set to `int64(relay.DefaultLimits().MaxMessageBytes)`, rather than being removed. It is the
  "small unexported helper" step 3 allows. Keeping the name let S1's tests compile against
  `HEAD`, and `deadline_test.go` needs no change. The `MaxFrameBytes` comment says 10 MiB.
* `config/config.go:106-108`: "0 = 10 MiB (the relay default, MADR 0223)".
* `leftovers/0223_d5_edits.py`: the ten files of D5, 12 edits, each asserted once. Then Deviation 1.
* `gofmt -l` on the 14 changed Go files: none.
* Green (`leftovers/0223-s2-green.log`): `go test -count=1` on `codex`, `relay`, `relayhost`, `ws`,
  `opencode`, `kilo`, `config` and `cli/service` (the template-parity test): all `ok`. The nine new
  tests and their three neighbours, verbose (`leftovers/0223-s2-green-v.log`): 12 PASS, 0 FAIL.

**S3** (logs `leftovers/0223-s3-*.log`):

* `make pre-add-check` on the 14 Go files: "14 file(s) clean (gofmt, golint, govulncheck)".
* `leftovers/vet_goos.py`: darwin/arm64, windows/amd64 and linux/amd64, exit 0 each.
* `make staticcheck`: exit 0, "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name"
  for each system.
* `make vulncheck`: "vulncheck: clear; allowlisted required-module findings: GO-2026-5932".
* `leftovers/nonascii_scan.py`: none in the three new test files or in `transport.go`. In the
  added lines of the other edited files, only three em dashes that were already on the re-worded
  lines.
* `make check-records`: exit 0. `make markdownlint-docs`: 57 files, "0 issues in 0 files".
* `make race`: exit 0, 45 `ok` packages, and no FAIL, panic or DATA RACE line
  (`leftovers/0223-s3-race.log`).

**S4** (`PINS_PHASE=0223 leftovers/pins_hosts.py`, the 20 changed and new code, test and config
files named, since its default would miss the three untracked tests):

* Linux test host, go1.27.3 linux/amd64: `go test -race -count=1 ./...` exit 0, 45 `ok` lines.
* Windows test host, go1.27.3 windows/amd64: the dry run, then `make ci-windows` exit 0,
  "ci-windows-local: ALL SELECTED CHECKS PASSED", 45 `ok` lines.
* Both scratch trees removed, and their absence checked.

**S5.**

* `leftovers/guard_preview.py` before staging: exit 0, "would edit 0 file(s), 0 path(s) need a
  rename by hand". `make check-records`: exit 0.
* Staged: the four code files, the three new tests, Deviation 1's eight files, D5's ten files, and
  this pair. `docs/README.md` is not staged: this pair's rows went in with `722dffa7`, and its
  remaining diff is the 0221 and 0222 rows, which are not this plan's.
* `check_records.py --check-all` on the staged tree (`git checkout-index` into a scratch
  directory, removed afterwards): exit 0, no output.

### Deviations

**Deviation 1, 2026-10-31: comments and a guide name the relay as the 1 MiB cap (S2 step 5).**

* **Found.** After S2's code and D5 edits, a sweep of tracked files outside `docs/decisions` and
  `docs/reports` for "1 MiB" and `1048576` found nine places that give the relay as the reason for
  a 1 MiB budget:
  * `internal/ws/server.go:2862`: the models.list cap exists because "the relay caps a message at
    1 MiB (internal/relay/config.go)";
  * `internal/ws/models_cap_test.go:49`, `:73-76`: a test constant `relayFrameLimit = 1 << 20`;
  * `internal/provider/opencode/http.go:416`, `:425`, `internal/provider/kilo/catalog_live.go:233`,
    `internal/provider/opencode/catalog_size_test.go:104` and
    `internal/provider/opencode/live_catalog_test.go:22`;
  * `docs/guides/protocol-v1.md:412`;
  * `internal/relay/preauth_limit_test.go:91`: "under the 1 MiB splice limit".

  The budgets they defend still hold: the daemon's own protocol limits, `maxWSMessageBytes` and
  `maxOutboundFrameBytes` (`internal/ws/server.go:310`, `:3608`), are 1 MiB (MADR F3). Only the
  reason they name is wrong after D3, and the relay never carried a protocol frame whole
  (MADR F1). None is in Scope.
* **Options put to the owner.** Re-attribute all nine (recommended); fix only the relay's own test
  comment; leave all and record them.
* **Decision.** "Re-attribute all nine (Recommended)". Comment and doc wording only. Each names the
  daemon's 1 MiB protocol frame limit. `relayFrameLimit` is renamed `protocolFrameLimit`, with its
  value unchanged. `preauth_limit_test.go:91` says 10 MiB. No behaviour changes, and the existing
  tests are the check.
* **Files added to Scope.** The eight files above.
* **Done** by `leftovers/0223_dev1_edits.py`, which asserts each old text once. A wider sweep
  afterwards (`relay` within 40 characters of `1 MiB`, `1048576` or `1 << 20`) found one more line
  of the same kind, `models_cap_test.go:12` ("The relay caps one message at 1 MiB"), in a file
  already listed. It was re-attributed under the same decision. The sweep then matched only the
  new `10485760` values.
* **Records changed.** MADR D5 (amended). Here: this entry.
