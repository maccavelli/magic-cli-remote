---
status: in-progress
date: 2026-11-01
associated-madr: "0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement running OpenCode v2 over ACP with a lease-bound v2 HTTP control plane

Associated MADR: [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)

## Goal

The OpenCode provider drives opencode 2.2.3: sessions over `opencode acp` through an
`acpagent.Spec` (D1), and the surfaces ACP lacks through one lease-bound
`opencode serve --stdio --port 0` control plane (D2). A 1.x engine is refused by name (D3), every
dropped surface is named (D11), and the 1.x HTTP dialect is retired (D4) without kilo regressing.
The work breakdown is
[0220-REPORT-opencode-v2-parity-assessment.md](../reports/0220-REPORT-opencode-v2-parity-assessment.md)
§5 (A to J), against the source at `b44b7164ba` ("release: v2.2.3").

## Scope

* **In scope:** the MADR's D1 to D14 as amended on 2026-11-01, phase by phase below. Each phase
  names its files; a file outside the list is a deviation.
* **Not in scope:**
  * kilo's dialect, beyond D12's shared hardening;
  * new v2 features beyond parity (0220 §4's last row): each takes its own record;
  * share, the todo panel, LSP and formatter status (D10, D11), named and dropped;
  * revert, undo, unrevert and `!` shell, unless G5 shows the control plane can act on a session
    the ACP engine owns (D11);
  * 0208-PLAN P5 to P10, which wait for this PLAN by the owner's decision
    ([0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
    Q1).

## Constraints

* **C1 — One commit per phase.** The agent stages; the owner commits with `git commit --no-edit`.
* **C2 — Live runs only on the owner's word,** each named in its phase. No probe or test reads or
  writes the owner's OpenCode configuration, credentials or database, except the auth tests D13
  allows.
* **C2a — kilo's live runs use a free model only** (owner, 2026-11-01: out of kilo quota). Each
  runs as `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo`, stated in the
  command rather than left to the suite's default. Every kilo live test that prompts reads that
  variable through `liveModel()` (`internal/provider/kilo/live_test.go:22`); the tests that use
  kilo's default model (auth, catalog, surface) send no prompt. A run that would need a paid
  model, or a test that prompts without `liveModel()`, is a stop.
* **C3 — Every new test or gate is seen failing first,** on a scratch copy or against today's code,
  never by dirtying the tree.
* **C4 — Isolated home for every live run** (D13 as amended): `HOME`, `XDG_*`, `OPENCODE_DB`,
  `OPENCODE_CONFIG_DIR`, `OPENCODE_DISABLE_AUTOUPDATE`, `OPENCODE_DISABLE_FILEWATCHER`, and a
  distinct service port or no managed service (A4). *Amended 2026-11-01 (MADR amendment B, B1):*
  every credential-shaped variable (`*_API_KEY`, `*_TOKEN`, `*_SECRET`, `*_PASSWORD`) and
  `BASH_ENV` are also removed from the engine's environment; a run that needs a credential sets
  it alone.
* **C5 — The server password** (D2) is generated per launch, set only in the child's environment,
  and never in argv, a log, a capture or a file. A test asserts each.
* **C6 — Captures are read before they are committed,** values judged by shape, identifiers made
  placeholders, and the guard's redact preview run (0226-PLAN C4).
* **C7 — Hosts.** Process, path, signal and pipe changes pass on macOS, the Linux test host and
  the Windows test host (`make ci-windows`) before their phase is done.
* **C8 — The version is checked at each phase's start.** `opencode --version` must print
  `v2.2.3`; a moved binary is a stop.

## Implementation Steps

### P1 — Shared hardening in `httpagent` (D12; 0220 §5 A)

Files: `internal/provider/httpagent/provider.go`, `internal/provider/opencode/http.go` (the
health-version read), and their tests.

1. **Failing first:** a fake engine whose `/global/health` answers `200 text/html`, and one whose
   event stream answers `200 text/html`. Today both are accepted
   (`httpagent/provider.go:624`, `:770-771`). The tests require an error naming the content type
   and the body's first bytes, for the OpenCode and the kilo dialects.
2. JSON endpoints must answer `application/json`, the stream `text/event-stream`.
3. The health body must decode a version; an empty version no longer skips the minimum check
   (`opencode/http.go:699-724`).
4. Exit status 130 after SIGTERM, and 0 after a lease close, are orderly in `serverDied`
   (`provider.go:693-721`).
5. Verification: Go rules; `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo`
   (C2, C2a) shows kilo unchanged.

### P2 — acpagent extension hooks (D14; 0220 §5 B)

Files: `internal/provider/acpagent/acpagent.go` (Spec fields), `session.go`, `compaction.go`,
`extensions.go`, `conformance_test.go`, and new tests.

1. **Failing first:** recorded frames for `session_info_update._meta`, `usage_update.cost._meta`,
   `compaction_update._meta` and a child-session frame, through a Spec that registers a handler for
   each. Today each is declined or dropped (MADR A7).
2. `Spec` gains per-key `_meta` handlers and vendor notification methods, each opt-in. A Spec that
   registers none behaves exactly as today; grok's suites pass unchanged.
3. Frames for another session are passed to a registered child handler instead of dropped, only
   when the Spec opts in.
4. A conformance row per extension key, so a Spec cannot claim one silently.
5. Verification: Go rules; grok's unit, conformance and wire suites unchanged.

### P3 — The gates (MADR G1 to G6), live, each on the owner's word

Each runs under C4, records what it shows in the execution record, and pins what is stable as a
`live_opencode` test. Nothing in the tree changes in this phase except those tests and this plan.

* **G1** — one ACP turn on a free model: text and thinking, a tool call with a permission, a form
  elicitation, a subagent, usage, and a forced retry if one can be provoked. Frames captured under
  C6 into ~~`internal/provider/opencode/testdata/acp/2.2.3/`~~
  `internal/provider/opencode/testdata/wire/2.2.3/` (P2 execution record).
* **G2** — `session/load` and `session/resume` after a restart, rebuilding the transcript and cost.
* **G3** — the `--stdio` lease, the environment password and `opencode acp` on the Linux and
  Windows test hosts: exit codes, signals, pipes and the service port.
* **G4** — credential removal against a running ACP engine, with a throwaway key in an isolated
  home: does the engine keep the provider active? It confirms or retires D8's restart.
* **G5** — the control plane's `revert/stage|commit` and `session.shell` on a session the ACP engine
  owns. It decides whether D11's revert and shell deferral can be lifted.
* **G6** — the `session/new` result and `config_option_update` sizes on the phone wire with a real
  credential set (A2), measured against
  [0181-MADR-configurable-mcremote-message-size.md](0181-MADR-configurable-mcremote-message-size.md).

A gate whose result contradicts the MADR is a stop: amend the MADR, re-approve, continue.

*Added 2026-11-01, after MADR amendment B was settled.* The pins, in
`internal/provider/opencode/live_v2_gates_test.go` (new, `live_opencode`, under C4 as amended).
Each is seen failing first on a scratch copy with its key assertion inverted (C3).

* **G3:** `serve --stdio --port 0`:
  * one `{"url"}` ready line;
  * `/api/info` as JSON with a 2.x version, and `401` without the password;
  * a shell the server runs sees neither the password nor its variable names;
  * a lease close exits 0, and SIGTERM exits 130 on POSIX.

  `opencode acp`: `initialize` names version 2.x, and a lease close exits 0. No prompt.
* **G4:** after the control plane removes a throwaway key, the running engine still accepts an
  OpenRouter model, and a fresh engine lists none. The test is D8's reason, and fails when
  opencode clears it. No prompt. *Amended (P3 Deviation 1):* the running engine is left 20 s to
  settle first. A settled engine must not list the added provider, and selecting its model
  reloads the catalog.
* **G2:**
  * `session/resume` answers `configOptions` with ~~no update~~ no history replayed (P3
    Deviation 1), and refuses another `cwd` with `-32602` (no prompt);
  * `session/load` on a fresh engine replays the prompt as `user_message_chunk` and sends no
    `usage_update` (one free-model turn).
* **G5:** on an idle session the ACP engine owns, the control plane's `revert/stage` and
  `revert/commit` of a turn drop its messages, and `session.shell` runs its command. The engine
  sends no update for either (two free-model turns).
* **G6** is not pinned, because it needs the owner's keys. G1 is pinned through P4's unit tests on
  its capture.

### P4 — The OpenCode ACP Spec (D1, D3, D5, D6, D9; 0220 §5 C, G)

*Amended 2026-11-01 (MADR amendment C).* P4 starts after
[0208-PLAN-make-the-provider-layer-agent-neutral.md](0208-PLAN-make-the-provider-layer-agent-neutral.md)
P6.
* Every opencode handler is written in `internal/provider/opencode`, on `acpagent.Host`.
* `acpagent` gains only agent-neutral pieces: step 7's replayed prompts, and step 4's minimum
  version as a Spec field.
* Step 2's projected `_meta["opencode/child-session"]` and `_meta["opencode/compaction"]` are not
  sent under D1's capabilities, and are not handled.
* The tests of G1's recorded frames that P2 put in `acpagent` move here in 0208 P6c.

Files: `internal/provider/opencode/` (a new Spec file beside the old dialect, which stays live until
P7), `internal/provider/opencode/version.go`, `internal/provider/opencode/commandtable.go`,
`internal/command/conformance_test.go`, and tests.

1. The Spec, modelled on `internal/provider/grok/grok.go`: argv `opencode acp`, D1's
   `ClientCapabilities`, `opencode-login` as the only auth method, `model`/`mode`/`effort` config
   options, static modes and model seeds, one child per project directory (A5).
2. D5: `opencode/session/child_update` and `_meta["opencode/child-session"]` into the subagent
   panel, through P2's hooks. D6: `_meta["opencode/retry"]` as a notice or the limit card;
   `_meta["opencode/compaction"]` into compaction state.
3. D9: automatic permission choices select by kind, `allow_once` first, never `reject_*`.
4. D3: `MinVersion = "2.1.0"`, `KnownGoodVersion = "2.2.3"` (*named `ACPMinVersion` and
   `ACPKnownGoodVersion` until P7; P4 Deviation 1*), read from
   `initialize.agentInfo.version`; a 1.x engine is refused with the version found and
   `https://opencode.ai/v2/install`. A pin test reads G1's capture.
5. The command table: `goal` added, 1.x-only rows removed, KindNone reasons for D11's drops.
   *Amended 2026-11-01 (MADR amendment B, D11):* revert, unrevert and `!` shell are no longer
   drops; until P6 lands they are KindNone, and the reason names P6.
6. Unit tests on G1's recorded frames for each of the above, each seen failing first.
7. *Added 2026-11-01 (MADR amendment B, D1, B2):* in `internal/provider/acpagent/session.go`,
   while a session loads, a replayed `user_message_chunk` is emitted as a replayed user message,
   for every Spec. A live one is still skipped. Tested on G2's replay shape, seen failing first.
   grok's suites must pass.
8. *Added 2026-11-01 (D6, B3):* nothing is reported about cost at load. The total reappears with
   the next turn's `usage_update`. A unit test pins that a load emits no usage.

### P5 — The control plane (D2; 0220 §5 D)

Files: `internal/provider/opencode/control/` (new), and tests.

1. Launch `opencode serve --stdio --port 0` with a per-launch password in `OPENCODE_PASSWORD`;
   ready on one JSON stdout line, then `GET /api/info` answering JSON with a version; stdout drained
   after readiness.
2. Lease on stdin; shutdown closes stdin, then SIGTERM, then kill after a bounded wait; exits 0 and
   130 are orderly. *Amended 2026-11-01 (MADR amendment B, D2):* 130 on POSIX only; a Windows
   forced stop exits 1 and is not orderly.
3. Basic auth `opencode:<password>`; `location[directory]` and `x-opencode-directory` on every
   location-scoped call; `location.directory` in create bodies.
4. Starts on first use, stops after an idle period; a bounded retry on `503` from `GET /api/agent`
   (A3); never `GET /api/credential`.
5. Tests: C5's three assertions (argv, logs, captures), each seen failing on a planted leak; the
   lease on all three hosts (C7, G3).

### P6 — Features moved to the control plane (D7, D8, D10; 0220 §5 E)

Files: `internal/provider/opencode/auth.go`, `device_auth.go`, `upstream.go`, `model_surface.go`,
`workspace.go`, `diagnostics_surface.go`, `internal/provider/credstore/credstore.go`, and tests.

1. D7: the ACP `model` option, grouped into connected / not configured from `GET /api/provider`
   when the control plane is up; never the flat list (G6).
2. D8: phone auth through `integration.connect*` and `credential.*`; the ACP engine restarts at its
   next idle point after ~~a removal unless G4 retired that~~ an add or a removal (G4 confirmed
   it; MADR amendment B, D8); a removal is confirmed from the integration's connections, not the
   `DELETE` status; `auth.json` fingerprinting replaced by the integration's connections.
3. D10: workspace on `fs.list/read/find`; full-text search as the owner decides (below);
   diagnostics and VCS on `mcp`, `skill`, `plugin`, `vcs`, `vcs/status` and `session.diff`;
   LSP and formatter dropped by name.
4. *Added 2026-11-01 (MADR amendment B, D11, "Lift it"):* revert, unrevert and `!` shell go
   through the control plane:
   * `revert/stage`, `DELETE …/revert` and `revert/commit` for revert and unrevert;
   * `session.shell` for `!`;
   * on an idle session only, refused while a turn runs;
   * Magic emits `transcript_remove` for reverted messages and shows the shell's output itself,
     because the ACP engine sends no update for either (G5).

   The files are named when P6 starts.

### P7 — Retire the 1.x dialect, and the configuration (D4, D11; 0220 §5 F)

Files: `internal/provider/opencode/*` (the 1.x dialect: `http.go`, `lifecycle.go`, `resync.go`,
`parts.go`, `question.go`, `todo.go`, `session_ops.go`, `permission.go`, `discovery.go`, `control.go`
and their tests), `internal/provider/opencode/testdata/surface-1.18.21/`, `wire/1.18.26/`,
`internal/config/config.go`, `configs/config.example.yaml`, `internal/daemon/daemon.go`, and tests.

1. The provider registers the P4 Spec; the 1.x dialect's files are deleted, with their surface and
   wire fixtures.
2. `pure`, `session_tree` and `stream_coalesce_ms` (as the owner decides) fail validation naming the
   key and this record; `allow_remote_share` is dropped; the rest map onto `ACPProviderConfig`.
3. Kilo's tests and `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo` (C2,
   C2a) unchanged.

### P8 — Doctor, engines and docs (0220 §5 G, I)

Files: `internal/cli/doctor.go`, `internal/cli/engines.go`, `README.md`, `docs/guides/config.md`,
mobile docs' OpenCode rows, and tests.

1. `doctor` reads `/api/info` and `agentInfo.version`, stops reading `auth.json`, and reports the
   service-port collision (A4).
2. `mcremote engines` names ACP children and the control plane apart.
3. Every doc row describing OpenCode's capabilities states the v2 set and D11's drops.

### P9 — Acceptance

1. `make test`, `make race`, `make live-opencode` (C2) on macOS, the Linux test host and the
   Windows test host.
2. The MADR's Confirmation, item by item, cited. Every D11 drop listed with its reason.
3. 0217's F11 row and 0226's D4 note that the opencode pin moved here.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | An HTML `200` health or stream fails loudly, for opencode and kilo | D12 |
| V2 | A Spec without extension hooks behaves as today; one with them receives the declined keys | D14 |
| V3 | G1 to G6 run, recorded, and pinned where stable | gates |
| V4 | A session runs on `opencode acp` 2.2.3; a 1.x engine is refused by name | D1, D3 |
| V5 | The password never appears in argv, logs or captures; the lease holds on three hosts | D2 |
| V6 | Phone auth, catalog grouping, workspace and diagnostics work through the control plane | D7, D8, D10 |
| V7 | The 1.x dialect is gone; its keys fail validation by name; kilo unchanged | D4 |
| V8 | Every dropped surface is named with its reason | D11 |

## Rollout and Rollback

* **Rollout:** phase by phase. Until P7, the 1.x dialect stays registered, so a phase that fails
  leaves today's (already broken) behaviour, never a new failure.
* **Rollback:** revert the phase's commit. Reverting P7 restores the 1.x dialect, which works only
  with a 1.x engine.

## Owner decisions needed at approval

* **`stream_coalesce_ms`** (D4): retire with a validate error, or map onto acpagent's coalescing.
* **Workspace full-text search** (D10): mcremote's own bounded search under the session root, or
  dropped by name.
* **The live runs:** P1's and P7's `make live-kilo`, on a free model only (C2a); P3's G1 to G6 (G1, G2, G5 and G6 spend
  quota; G3 needs the Linux and Windows hosts; G4 needs only a throwaway key); P9's
  `make live-opencode` on three hosts.

## The owner's answers

*Answered by the owner, 2026-11-01:*

* The PLAN: "Approve, ask per gate", whose option read "Start P1 and P2 now (no quota except P1's
  make live-kilo). Stop before each of G1-G6 and P9's live runs for your word". So P1's
  `make live-kilo` runs within P1 (on the free model, C2a); G1 to G6, P7's `make live-kilo` and
  P9's live runs each wait for the owner's word. *Corrected 2026-11-01: this entry first said every
  other named live run waits, which narrowed the option the owner chose.*
* `stream_coalesce_ms`: "Retire it" (P7 step 2).
* Full-text search: dropped by name in P6, with issue
  [#34](https://github.com/maccavelli/magic-cli-remote/issues/34) opened for the research and a
  MADR/PLAN pair of its own.
* Kilo: "i am out of kilo quota, so it needs to use a free model". Added as C2a.
* The gates, 2026-11-01:
  * G2, G3, G4 and G5 were chosen to run.
  * G6: "my opencode api-key is exported into my shell env. capture it to a file but do NOT read
    it into the session or echo it into the session."
* MADR amendment B, 2026-11-01:
  * replayed prompts: "All Specs" (P4 step 7);
  * cost at load: "Next turn" (P4 step 8);
  * revert and shell: "Lift it" (P6 step 4);
  * the rest: "Accept all" (C4, P5 step 2, P6 step 2, D7).
* At P4's start, 2026-11-01: "no, we need to keep provider specific code OUT of acpagent not in
  it. we need to peel grok out of there as part of getting ready to adopt support for gobble."
  Then "0208 P6 now", "Amend 0208 and 0214" and "Neutralize in P6" (MADR amendment C; P4's note).

## Execution record

### P1 — 2026-11-01: shared hardening in `httpagent`

C8: `opencode --version` printed `v2.2.3`, at `13834e36`.

**What changed.**

* `httpagent/provider.go`:
  * A `200` health answer passes only as JSON naming the engine's version (`checkHealthBody`),
    and anything else ends the start at once, killing the engine. Before, any `200` was health.
  * The event stream must answer `text/event-stream`.
  * An API answer that is decoded must be JSON (`application/json` or a `+json` type). An answer
    that is not decoded is not checked.
  * Each refusal names the request, the content type it got, the one it wanted, and up to 120 bytes
    of the body (`contentTypeError`).
  * The death monitor logs exits 0 and 130 as "engine stopped" at Info, and anything else as
    before. Sessions fail either way (`orderlyExit`, `exitCode`).
* `opencode/http.go`: `CheckMinVersion` refuses an engine that reported no version when
  session-tree mode is on, where it used to pass. The kill switch still allows it.
* Tests: `httpagent/contenttype_test.go` (new) covers the health probe through the real
  `startServer` (an HTML page, and JSON without a version, from a helper engine), the event stream,
  the API, and the exit classification. `supervise_wiring_test.go`'s helper engine now answers
  JSON with a version. `opencode/version_test.go`'s "unknown version must not block" became the
  refusal, with the kill-switch case kept.
* The kilo and OpenCode 1.x dialects use the same `/global/health` and `/global/event` paths, so
  the tests' one stub dialect with those paths covers both. No kilo or OpenCode unit fake needed a
  change for the API check; the existing fakes already set JSON content types where they are
  decoded.

**Tests, each seen failing.** On `HEAD` the new file does not compile (it calls the new helpers).
On scratch copies of the change, one check undone each (`p1_plants.py` in the session
scratchpad):

| Plant | Fails |
| --- | --- |
| the health body left unchecked | both `TestStartServerRefuses…` tests: "startServer accepted an HTML page as health", "…a health answer that names no version" |
| the stream's content type unchecked | `TestEventStreamRefusesAnHTMLAnswer`: "the event stream accepted an HTML page" |
| the API's content type unchecked | `TestAPIRefusesAnAnswerThatIsNotJSON`, for `text/html` and `text/plain` |
| 130 not orderly | `TestOrderlyExit`: "orderlyExit(exit 130) = false, want true" |
| opencode passing an unknown version | `TestOnHealthyAndVersionGate`: "an engine that reported no version passed the minimum check" |

The first time the full suite ran on the change, `TestEngineTeardownSupervisesTheDescendantTree` and
`TestOnHealthyAndVersionGate` failed, as the new rules require; both were updated as above.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 45 packages ok |
| `make pre-add-check FILES=…` (five Go files) | "5 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags` | exit 0 each |
| the Linux test host, `go test -race -count=1 ./...` (C7) | exit 0, 45 packages ok, no failure or race line |
| the Windows test host, `make ci-windows` (C7) | exit 0, "ALL SELECTED CHECKS PASSED", 45 packages ok |
| `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo` (C2a) | exit 0; 185 passed, 0 failed, 4 skipped, 97.7 s; every turn on `openrouter/openrouter/free` |

The four skips: three need an opt-in variable (`MCREMOTE_KILO_ENGINE`, `MCREMOTE_LIVE_AUTH_WRITE`,
`MCREMOTE_RAW_OUTPUT_DIR`). The fourth, `TestLivePermissionRoundTrip`, is model-dependent: "model
completed the turn without using a tool; no permission was requested". It passed in 0226 R2's run
on the same model.

**Not done:** 0220 §5 A's optional fourth item (passing a secret to an engine through its
environment) belongs to the control plane, P5.

**Files for this commit:** `internal/provider/httpagent/provider.go`,
`internal/provider/httpagent/contenttype_test.go` (new),
`internal/provider/httpagent/supervise_wiring_test.go`, `internal/provider/opencode/http.go`,
`internal/provider/opencode/version_test.go`, and this plan.

*The owner committed P1 as `72abd42f` and pushed it.*

### P2 Deviation 1, 2026-11-01 — P2's premises, read against the source; G1 first. The owner decided: "Narrow P2" and "Run G1 first"

- **Found** before any P2 change, reading opencode's ACP source at `b44b7164ba` with D1's client
  capabilities: three of the four keys P2 was to hook never reach Magic. Child output arrives as
  the extension notification `opencode/session/child_update`, which `Spec.ExtensionNotifications`
  already routes, not as frames for another session; cost arrives in the standard
  `usage_update.cost`; compaction arrives as standard `compaction_update` frames. The one gap is
  the retry marker, `_meta["opencode/retry"]` in `session_info_update` (declined at
  `session.go:1971-1973`) and in the prompt result (not read). The MADR's A7 and D14 are corrected
  with the citations ("A7 and D14, corrected 2026-11-01").
- **Found:** P2 step 1 calls for recorded frames, and no opencode ACP recording exists before G1.
- **Resolution, scope:** P2 builds per-Spec, opt-in handlers for keys in `session_info_update._meta`
  and in the prompt result's `_meta`, with a conformance row per key; steps 1 to 4 are narrowed to
  that. The other-session routing and the cost and compaction hooks are not built.
- **Resolution, order:** G1 runs now, ahead of P2, on the owner's choice ("Ask for G1 now … and
  build P2's tests from its recording"), and P2's tests are built from its recording. G1 runs from
  a scratch-only harness in a scratch copy of the tree; its frames are read under C6 before any is
  committed. The rest of P3 stays where it is.
- **Not taken:** building all four hooks; folding P2 into P4; frames transcribed from source.
- **The MADR is amended** (A7, D14).

### P2 Deviation 2, 2026-11-01 — G1's recording: child updates never reach acpagent, and no retry. The owner decided: "SDK receiving middleware" and "Transcribe from source"

- **G1, run 2026-11-01** (scratch-only harness `g1/zz_g1capture_test.go` in a scratch copy of the
  tree, removed; isolated home per C4; `opencode v2.2.3`; D1's client capabilities with
  `_meta["opencode/child-session-updates"]`). Runs 1 and 2: every prompt answered `-32603` "Add
  credits to continue, or switch to a free model": the session's default model was a paid one, and
  the harness's `StartOptions.Model` was not applied (below). Run 3 set
  `opencode/deepseek-v4.1-flash-free` through `SetModel`: four turns, each `end_turn`, 53 frames:
  text and thinking chunks, three `tool_call`s with eight updates, one `elicitation/create` form
  (answered), four `usage_update`s, four prompt results with `usage` and `_meta: {}`, and seven
  `opencode/session/child_update` notifications.
- **Found, child updates:** the harness registered `opencode/session/child_update` in
  `Spec.ExtensionNotifications` and counted 0 calls for 7 frames. acp-go-sdk2 hands a method to the
  extension handler only when its name starts with `_` (`extensions.go:181-197` at v1.31.1);
  opencode's has none, so the generated dispatch answers it as an unknown method and the
  notification is dropped. Deviation 1's statement that `Spec.ExtensionNotifications` already
  routes child updates was wrong.
- **Found, retry:** no retry occurred; every `_meta` was `{}`, and none can be provoked on demand.
- **Found, for P4:** acpagent sends a start model only through grok's paths (`_meta`, `ModelArgs`;
  `acpagent.go:915-930`), so the opencode Spec must set it with `session/set_config_option`; and
  "build" mode asked no permission for `ls -la`.
- **Resolution, child updates:** P2 also delivers a Spec's vendor notification methods whose names
  lack `_` through acp-go-sdk2 v1.31.1's `WithReceivingMiddleware`: a registered method goes to
  the Spec's handler, everything else passes through untouched, and a Spec that registers no such
  method installs no middleware. Its test uses G1's recorded `child_update` frames.
- **Resolution, retry:** the retry tests use frames transcribed from opencode's source
  (`packages/cli/src/acp/translate.ts:166`, `:265`, `:275` at `b44b7164ba`), each marked as from
  source; a later recording replaces them when one occurs.
- **The capture:** G1's run-3 frames, read under C6, go to
  ~~`internal/provider/opencode/testdata/acp/2.2.3/`~~ `internal/provider/opencode/testdata/wire/2.2.3/`
  as P2's fixture (moved; P2 execution record).
- **Not taken:** changing acp-go-sdk2 first; dropping the child-updates capability; more turns to
  provoke a retry.
- **The MADR is amended** (A7 and D14 again); 0207's amendment L notes the middleware's adoption.

### P2 — 2026-11-01: acpagent extension hooks, as narrowed

**What changed.**

* `acpagent.go`:
  * `Spec.MetaHandlers map[string]MetaHandler`, carried onto the session at start beside
    `ExtensionNotifications`.
  * `clientConnOptions` appends `acp.WithReceivingMiddleware(s.vendorNotifications)` only when the
    Spec registered a notification method whose name lacks `_`. Every other Spec's connection,
    grok's included, has the four options it had.
* `extensions.go`:
  * `vendorNotifications`: a notification, never a request, whose method lacks `_` and is
    registered goes to its handler. Requests, `_` names and unregistered methods pass to the SDK's
    dispatch unchanged, so a request under a registered name is still answered `-32601`, and an
    unregistered vendor notification is still dropped.
  * `MetaSource` (`session_info_update`, `prompt_result`), `MetaHandler`, and `handleMeta`, which
    passes each registered key's value, as sent, to its handler in key order. A key cleared with
    `null` arrives as `null`.
* `session.go`:
  * `session_info_update._meta`: registered keys go to their handlers. The update is declined, as
    every `_meta` was before, only when a key has no handler. A `_meta`-only update sets no title.
  * The prompt result's `_meta`: registered keys go to their handlers on the turn goroutine, after
    a successful answer and before the usage report and `turn_complete`. A key with no handler is
    not read, as before.
* Tests:
  * `vendorext_test.go` (new): G1's seven `child_update` frames reach the handler with their
    params. A request under that name gets `-32601` and no call. An unregistered vendor
    notification is dropped. A `_` name is delivered once. A Spec with no vendor name gets no
    middleware.
  * `metaext_test.go` (new): the retry marker, scheduled and cleared, reaches its handler and is
    not declined. An unregistered key is declined, with handlers for other keys and with none. The
    prompt result's marker reaches its handler before `turn_complete`. G1's four recorded prompt
    results, each `_meta: {}`, call none. The retry frames are transcribed from `translate.ts:166`,
    `:265` and `:275` at `b44b7164ba`, and are marked in the file as from source. Their values are
    illustrative; their shape is `translate.ts:268-272` with `SessionStructuredError`
    (`packages/client/src/promise/generated/types.ts:114-120`).
  * `conformance_test.go`: `TestExtensionKeysConform`, step 4's rows. There is one row per key a
    Spec can register: the `child_update` notification, `session_info_update._meta` `opencode/retry`,
    and prompt-result `_meta` `opencode/retry`. Each row is driven over a connection with
    production's options, names its evidence (recorded or transcribed), and must reach the
    registered handler exactly once. A key gets a row before a Spec registers it.
  * `wireagent_test.go`: `newWireSessionFor` carries the Spec's notification and `_meta` handlers
    onto the session, as the provider's start does, before the connection is built.

**The capture moved.** G1's frames were first written to `testdata/acp/2.2.3/`, which
`internal/wirecap`'s `TestCommittedFixturesCarryNoIdentifiers` does not cover. It globs
`provider/*/testdata/wire/*/frames.jsonl`. They now sit in `testdata/wire/2.2.3/`
(`frames.jsonl`, 53 frames; `meta.json`), beside the kilo and codex captures, so that guard covers
them. Read under C6, the capture had the operator's account name in 7 places: the shell's startup
errors and the owner column of an `ls -la` tool result. Each is now `<user>`. The isolated home
was already written as `/home/user` by `internal/wirecap`.

**Tests, each seen failing.**

* Against the code before the change, `TestVendorNotificationReachesItsSpecHandler` failed with
  "the handler was called 0 time(s), want 7". The SDK logged `-32601` for each of the seven
  frames. `TestNoVendorMethodInstallsNoMiddleware` failed with "vendor: 4 options, want 5".
* With `MetaHandlers` declared but not dispatched, `TestRetryMetaReachesItsHandler/scheduled` and
  `/cleared` failed with "the handler was called 0 time(s), want 1".
  `TestPromptResultMetaReachesItsHandler` failed with "…0 time(s) before turn_complete, want 1".
* The rows that pin today's behaviour passed before and after: the request answered `-32601`, the
  unregistered vendor notification, the `_` name delivered once, the unregistered `_meta` key
  declined, and G1's empty `_meta`.
* `TestExtensionKeysConform` was written after the code. On scratch copies of the tree, one
  dispatch point undone each (`p2/plant.py` in the session scratchpad), exactly one row failed:

| Plant | Fails |
| --- | --- |
| no middleware | `notification opencode/session/child_update`: "called 0 time(s), want 1 (recorded: G1, opencode 2.2.3 (testdata/wire/2.2.3))" |
| no `session_info_update` dispatch | `session_info_update._meta opencode/retry`: "called 0 time(s), want 1 (transcribed: opencode translate.ts:275 at b44b7164ba)" |
| no prompt-result dispatch | `prompt result _meta opencode/retry`: "called 0 time(s), want 1 (transcribed: opencode translate.ts:166 at b44b7164ba)" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 45 packages ok, `acpagent` and `grok` among them, no failure or race line |
| `make pre-add-check FILES=…` (seven Go files) | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

grok's unit, conformance and wire suites ran unchanged in the race run (step 5). The test hosts
were not run: the change adds no process, path, signal or build-tag behaviour (C7 applies to those).

**Not done:**

* No Spec registers a handler yet. The opencode Spec's retry handler and child panel are P4.
* G1 pinned no `live_opencode` test. P3 pins what is stable when the rest of its gates run.
* No retry has been recorded. The transcribed frames stay until one is.

**Files for this commit:** `internal/provider/acpagent/acpagent.go`, `extensions.go`, `session.go`,
`conformance_test.go`, `wireagent_test.go`, `vendorext_test.go` (new), `metaext_test.go` (new);
`internal/provider/opencode/testdata/wire/2.2.3/` (new); this plan; the MADR; and
[0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
(amendment L).

*The owner committed P2 as `f15af43e`.*

### P3 — 2026-11-01: gates G2 to G6

C8: `opencode --version` printed `v2.2.3` here, and on the Linux and Windows test hosts.

**The owner's word.** The owner chose G2, G3, G4 and G5. For G6 the owner wrote: "my opencode
api-key is exported into my shell env. capture it to a file but do NOT read it into the session
or echo it into the session." Two variables matched, `OPENCODE_API_KEY` and
`OPENCODE_ZEN_API_KEY`; only their names and lengths were printed. A script wrote their values to
a mode-0600 file in the session scratchpad, and G6's harness read them from there into its own
environment. The file was deleted after G6. A scan of every G2 to G6 log and capture, and of the
committed G1 fixture, for each key (whole, and its first and last 12 characters) found 0.

**Found first: G1 inherited the operator's credentials** (MADR amendment B, B1). G1's harness set
the isolated-home variables and removed nothing, so its engine inherited opencode's keys and
`BASH_ENV`. G2 to G6 removed every `*_API_KEY`, `*_TOKEN`, `*_SECRET` and `*_PASSWORD` variable (14
on this host) and `BASH_ENV`. G6 then set the two keys alone.

**How each ran.** Every harness ran in an isolated home and was scratch-only. Go harnesses ran
under a build tag, in a scratch copy of the tree that was removed afterwards, as G1's did.
Python harnesses spoke JSON-RPC and HTTP directly. Nothing in the tree changed.

**G2 — `session/load` and `session/resume`** (`g2/zz_g2capture_test.go`, `g2/run_g2.py`).

* Phase A ran two turns on a new session, "apple" then "banana", and closed the engine. Each turn
  sent two `usage_update`s. Each carried `cost {amount: 0, currency: "USD"}`, so Magic's
  `SessionCost` was 0 USD.
* Phase B, on a fresh engine:
  * `session/load` took 1.21 s;
  * opencode replayed `user_message_chunk` ×2 and `agent_message_chunk` ×2, with the original
    message ids, and no `usage_update`;
  * Magic emitted 2 replayed `assistant_message_chunk` events and no `user_message`;
  * a third turn named both earlier answers.
* Resume, on a fresh engine:
  * `session/resume` answered with `configOptions` only: model (8 options, the free model
    current), effort, mode;
  * ~~it sent no update within 8 s;~~ it replayed no history (P3 Deviation 1: the harness
    dropped messages read with an answer, and `available_commands_update` and
    `config_option_update` do follow);
  * the same request with another `cwd` got `-32602`.

**G3 — the lease, the password and `opencode acp`** (`g3/g3_probe.py`, copied to each test host,
run there, and removed).

| Check | macOS | Linux test host | Windows test host |
| --- | --- | --- | --- |
| launcher | binary | binary | `.exe` |
| ready line | `{"url"}`, 0.20 s | `{"url"}`, 0.81 s | `{"url"}`, 0.17 s |
| `/api/info` | 200 JSON, 2.2.3; 401 without the password | same | same |
| a shell the server runs sees the password or its variable names | no | no | no |
| lease close (stdin) | exit 0, 0.04 s | exit 0, 0.22 s | exit 0, 0.07 s |
| terminate | 130 | 130 | 1 |
| SIGINT | 130 | 130 | — |
| `acp` initialize / session/new | 0.30 / 0.37 s | 0.94 / 1.48 s | 0.27 / 0.39 s |
| `acp` lease close / terminate | 0 / 130 | 0 / 130 | 0 / 1 |
| descendants alive after any stop | 0 | 0 | 0 |
| service-registration files in the home | 0 | 0 | 0 |

**G4 — credential removal under a running engine** (`g4/run_g4.py`). There was one `opencode
acp` engine and one control plane on the same home. The throwaway key was random and never used
in a request.

* Before: new sessions listed 8 models, and the control plane's OpenRouter was `404`.
* After `POST /api/credential` (`200`; OpenRouter activation `auto`):
  * 95 s later, the running engine's new session still listed 16 models and no OpenRouter model;
  * `set_config_option` to `openrouter/openai/gpt-4o-mini` was accepted, and the list became 367.
* After `DELETE /api/credential/<id>` (`204`; a second `DELETE` also `204`; activation still
  `auto`):
  * 95 s later, the running engine's new session listed 367 models, 351 of them OpenRouter's;
  * selecting an OpenRouter model was accepted, in a new session and in the earlier one;
  * a fresh engine on the same home listed 16 models and no OpenRouter model.
* The first run's harness read the credential id from the wrong level, and its `DELETE` removed
  nothing. Only the second run is recorded.

**G5 — revert and shell through the control plane** (`g5/run_g5.py`). The project was a git
repository. Two ACP turns wrote `note.txt` as "one", then "one two". All control-plane calls were
made with the session idle.

| Control-plane call | Status | `note.txt` | Messages | ACP updates |
| --- | --- | --- | --- | --- |
| `revert/stage` at turn 2, files | 200 `{files, messageID, snapshot}` | "one" | 11 | none |
| `DELETE …/revert` | 204 | "one two" | 12 | none |
| `revert/stage` again | 200 | "one" | 12 | none |
| `revert/commit` | 204 | "one" | 6 (turn 2 gone) | none |
| `session.shell` `echo g5shell > shell.txt` | 204 | — | 7 (a `shell` message) | none |

A third ACP turn listed the first request, the shell command and itself, and not turn 2. The
first run staged at turn 1, because the message list is newest first. Every message and the file
went, and the third turn named only the shell command.

**G6 — catalog size on the phone wire** (`g6/zz_g6capture_test.go`, `g6/run_g6.py`). These are
Magic's `session_config` events for a new session, encoded as JSON:

| Credentials | Event | Models | Providers | Size |
| --- | --- | --- | --- | --- |
| none | 1 | 8 | opencode | 1,030 bytes |
| none | 2 | 16 | opencode, ollama | 1,864 bytes |
| the owner's two keys | 1 | 81 | opencode 43, opencode-go 30, ollama 8 | 5,932 bytes |

The ACP frames were 1,188 bytes (`session/new` result) and 2,160 bytes (`config_option_update`)
with no credential, and 6,309 bytes with the keys. The limits are the daemon's 1 MiB read limit
and the relay's 10 MiB.

**Stop (P3's rule).** G2 contradicts the MADR's statement that acpagent handles every standard
kind, and G2 to G6 change D1, D2, D6, D7, D8, D11 and D13. The MADR's amendment B records the
facts and proposes the changes. P3 waits for the owner's choices before its `live_opencode`
pins, and before P4.

**Not done:**

* No `live_opencode` test is pinned yet. Which facts to pin depends on amendment B.
* No G2 to G6 capture is committed. They hold the owner's catalog (G6) and nothing P4 reads
  that G1's fixture does not.
* No retry was provoked.
* Revert during a running turn was not tried.

*The owner settled amendment B on 2026-11-01 ("The owner's answers").*

### P3 Deviation 1, 2026-11-01 — two pins disagreed with the gates. The owner decided: "Investigate further"

- **Found, resume:** the first run of `TestLiveV2ResumeReplaysNothing` saw
  `available_commands_update` and `config_option_update` follow `session/resume`, where G2
  recorded none. G2's Python harness returned on finding the answer and dropped the rest of that
  read. Its "no update" and its resume timing were artifacts. G4's and G5's harnesses read
  results only from answers, or from separate reads, so their findings stand.
- **Found, G4:** across all runs, the running engine's view after a removal varied. Python runs
  kept the provider 3 of 3 times. The first Go pin kept it 2 of 6 times and dropped it within
  1–10 s in the other 4.
- **Investigated:** opencode's source at `b44b7164ba` gives the mechanism (MADR amendment B, B6
  as corrected). An engine left 20 s to settle reflected neither the add (3 of 3 runs) nor the
  removal (3 of 3).
- **Resolution:**
  * B4 and B6 are corrected in the MADR. D8 is unchanged.
  * The resume pin asserts that no history is replayed (`TestLiveV2ResumeReplaysNoHistory`).
  * The G4 pin settles the engine first, asserts that the settled engine does not list the
    added provider, and reaches it by a selection that misses the cache.
- **Not taken:** dropping the G4 pin; asserting either outcome on an unsettled engine.

### P3 — 2026-11-01: the pins

`internal/provider/opencode/live_v2_gates_test.go` (new, build tag `live_opencode`) holds six
tests. Each starts `opencode acp` or `opencode serve --stdio` itself, under C4 as amended, and none
uses Magic's provider code. Run with `go test -tags live_opencode ./internal/provider/opencode/
-run LiveV2 -count=1 -v`:

| Test | Gate | Prompts |
| --- | --- | --- |
| `TestLiveV2ServeStdioLease` | G3 | none |
| `TestLiveV2AcpLease` | G3 | none |
| `TestLiveV2RemovedCredentialStaysInRunningEngine` | G4 | none |
| `TestLiveV2ResumeReplaysNoHistory` | G2 | none |
| `TestLiveV2LoadReplaysPromptsAndNoUsage` | G2 | one, free model |
| `TestLiveV2RevertAndShellFromControlPlane` | G5 | two, free model |

**Runs on macOS.** All six passed in one run (54.7 s). The G4 pin, settled, passed 3 of 3 in
one more run.

**Seen failing.** On scratch copies, one key assertion inverted each (`p3/plant.py` in the session
scratchpad), each test failed alone:

| Test | Inverted | Failure |
| --- | --- | --- |
| `ServeStdioLease` | `401` without the password | "/api/info without the password = 401, want 401" |
| `AcpLease` | lease close exits 0 | "lease close: exit 0, want 0" |
| `RemovedCredentialStaysInRunningEngine` | the running engine keeps the removed provider | "the running engine refused … after the removal … <nil>" |
| `ResumeReplaysNoHistory` | another `cwd` is `-32602` | "resume with another cwd: Invalid params: … does not belong to cwd …, want -32602 (A5)" |
| `LoadReplaysPromptsAndNoUsage` | the prompt is replayed once | "load replayed 1 user_message_chunk with the prompt …" |
| `RevertAndShellFromControlPlane` | commit drops the second turn | "after revert/commit the first turn kept true, the second kept false" |

**Gates.**

| Gate | Result |
| --- | --- |
| `make pre-add-check FILES=…` (the new file) | "1 file(s) clean (gofmt, golint, govulncheck)" |
| `make vet-tags`; `make staticcheck`; `make lint` | exit 0 each |

**Not done:**

* The pins have not run on the Linux and Windows test hosts. That is P9's `make live-opencode` on
  three hosts. G3's host facts are in the gate record above.
* `make live-opencode` still runs the 1.x dialect's live tests, which P7 retires.

P3 is complete with this commit.

**Files for this commit:** this plan, the MADR, and
`internal/provider/opencode/live_v2_gates_test.go`.

*The owner committed P3 as `3f793e6f`. 0208 P6, which P4 waits for, ended with `85b1447f`.*

### P4 Deviation 1, 2026-11-02 — the version constants, and the starting model. The owner decided: "New constants; P7 renames" and "acpagent, for every Spec"

C8: `opencode --version` printed `v2.2.3`, at `85b1447f`.

- **Found** before the first write:
  * **The version constants are the 1.x dialect's.** Step 4 sets `MinVersion` and
    `KnownGoodVersion` in `internal/provider/opencode/version.go`. The 1.x HTTP dialect, registered
    until P7, gates its engines on the same two constants (`http.go:677-724`). About twenty of its
    unit tests pin the 1.18 floor or fake 1.18.x engines (`version_test.go:14-58` among them).
    Changed as written, the dialect would refuse every engine it can drive, against this PLAN's
    rollout.
  * **A starting model or thinking level is never applied through config options.** acpagent
    records the requested values (`acpagent/acpagent.go:949-961`), and grok applies them through
    `_meta`. For an agent whose model and level are standard config options, nothing sends them,
    and the agent's current values then replace the request (`acpagent/session_meta.go:54-69`). A
    model chosen at create, or the configured `model`, would be silently ignored on opencode.
- **Resolution:**
  * `version.go` gains the Spec's own `ACPMinVersion = "2.1.0"` and
    `ACPKnownGoodVersion = "2.2.3"`. `MinVersion` and `KnownGoodVersion` stay the 1.x dialect's.
    P7, which deletes the dialect, renames the two.
  * acpagent, for every Spec: after `session/new`, a requested model or thinking level that
    differs from the agent's current `model` or `thought_level` option is set through standard
    `session/set_config_option`, and the answer is recorded. A refusal keeps the agent's value and
    posts a notice; the session still starts.
- **Also within P4 as written:**
  * acpagent's minimum-version field (MADR amendment C) comes with a second field for the upgrade
    link, so the refusal can name `https://opencode.ai/v2/install`.
  * Step 3's choice by kind already exists in acpagent (MADR 0207 amendment H). P4 pins it on
    opencode's options.
  * A5's one child per directory already holds: acpagent runs one process per session and claims
    a spare only for a matching directory (`acpagent.go:831`).
  * opencode needs no auth handling from the Spec: with no safe method named, acpagent does not
    call `authenticate`, and the engine uses its own credential store.
- **Files added to P4's scope:** `internal/provider/acpagent/acpagent.go`, `session_meta.go`,
  `version.go`, and their tests. The MADR records the third acpagent piece under amendment C.

### P4 Deviation 2, 2026-11-02 — modes, and the start timeout. The owner decided: "Config option first" and "Spec field, opencode 120 s"

- **Found** while building the Spec, after Deviation 1's acpagent pieces landed in the tree:
  * **`/plan` is never confirmed.** opencode publishes its mode only as a `mode`-category config
    option, with no ACP modes (G1's `session/new` result). Its `session/set_mode` changes the
    selection and sends nothing back: no `current_mode_update` and no `config_option_update`
    (opencode `packages/cli/src/acp/sessions.ts:184-190` and `service.ts:233-237`, at
    `b44b7164ba`). acpagent sends `session/set_mode` and waits for the agent's confirmation
    (`acpagent/session.go:878-899`), so Magic would keep showing build mode. grok has no `mode`
    option: the `"category":"mode"` entries in its fixtures are inside its `x.ai` `_meta` model
    list.
  * **Cold starts exceed the start bound.** A6 measured a cold `initialize` at 43.7 s and
    `session/new` at about 45 s. acpagent bounds each with a fixed 30 s (`acpagent.go:40`, used at
    `:641` and `:979`), and no Spec can size it as D1 (amended) asks.
- **Resolution:**
  * `SetMode` sets a `mode`-category select option, when the agent has one, through
    `session/set_config_option`. Its answer carries the options: Magic records them and reports the
    new mode. Otherwise `session/set_mode`, as today. A config update that changes the mode
    option's value reports the new mode too.
  * `Spec.StartTimeout` bounds `initialize` and `session/new`; zero keeps 30 s. opencode's Spec
    sets 120 s.
- **Files added to P4's scope:** `internal/provider/acpagent/session.go`, already step 7's, for
  `SetMode`. The MADR's amendment C note lists both pieces.

### P4 — 2026-11-02: the OpenCode ACP Spec

**What changed in `acpagent`, for every Spec.**

* **Replayed prompts (step 7).** While a session loads, a `user_message_chunk` is a replayed
  prompt: chunks sharing a message id make one replayed user message. It is released when another
  update arrives, or just before the load ends, so it is still marked replay. A live chunk is still
  skipped.
* **No cost at load (step 8).** Nothing new: the replay carries no usage, and none is reported. A
  test pins it.
* **The floor (step 4).** `Spec.MinVersion` and `Spec.UpgradeURL`. An engine below the floor, or
  one that names no version at `initialize`, fails Start with the version found, the floor and the
  link. A prerelease of the floor is below it. An empty floor starts any engine, as before.
* **The starting model and level (Deviation 1).** After `session/new`, a requested value that
  differs from the agent's current `model` or `thought_level` option is set through
  `session/set_config_option`. The start's `session_config` event shows the recorded answer. A
  refusal keeps the agent's value and posts a notice.
* **Modes through a `mode` option (Deviation 2).** `SetMode`, and the arming of auto, use a
  `mode`-category option when the agent has one, and report the mode from its answer. A
  `config_option_update` that moves that option reports the new mode.
* **The start bound (Deviation 2).** `Spec.StartTimeout`; zero keeps 30 s.

**What changed in `internal/provider/opencode`.**

* **`acp.go`:** the Spec, and `NewACP`, which nothing registers until P7.
  * argv `opencode acp`;
  * acpagent's client capabilities, plus `_meta["opencode/child-session-updates"]`;
  * `ACPMinVersion` 2.1.0, `ACPKnownGoodVersion` 2.2.3, `https://opencode.ai/v2/install`;
  * a 120 s start bound;
  * static modes `build` and `plan`, with synthesized auto;
  * seven free models from G1's catalog as the fallback.
  * No auth hook and no safe auth method: acpagent then never calls `authenticate`.
* **`acp_extensions.go`:**
  * `opencode/session/child_update` → the subagent panel (D5). The status maps to running,
    completed or failed (from failed or interrupted). An update only marks the child running; its
    output stays out of the parent's transcript.
  * `_meta["opencode/retry"]` (D6), by the 1.x dialect's rule: a quota error or a back-off of
    `agenterr.LongBackoffMin` or more ends the turn with the limit card and opencode's retry time.
    A short retry is a notice, except on a prompt result, where the turn is already over. A cleared
    marker says nothing.
* **`commandtable.go`:** `acpCommandTable` (step 5).
  * opencode's own `compact`, `goal` and `review` forward.
  * `model`, `thinking` and `context` are operations.
  * `diff`, `undo` and `redo` are unavailable, naming P6; `fork` is unavailable.
  * The rest have reasons.
* **`version.go`:** `ACPMinVersion`, `ACPKnownGoodVersion`, `ACPUpgradeURL` (Deviation 1).
* **`internal/command/conformance_test.go`:** the ACP Spec joins the four provider lists.

**Not changed:** the 1.x dialect and its version constants, and their tests. D9's choice by kind
already existed (MADR 0207 amendment H); it is pinned here on opencode's options.

**Tests, each seen failing** (`p6/plant_p4.py` in the session scratchpad, 22 deliberate breaks on
scratch copies; the acpagent tests also failed first on the tree before their change):

| Break | Fails |
| --- | --- |
| replayed prompts skipped again | `TestLoadReplaysThePrompts`: "replayed prompts = [], want [\"write note.txt\" \"and again\"]"; `TestSessionLoadEmitsTheLastReplayedPrompt` |
| the last prompt not released before the load ends | `TestSessionLoadEmitsTheLastReplayedPrompt`: "the replayed prompt was not emitted by the load" |
| one prompt's chunks not merged | `TestLoadReplaysThePrompts`: `["write note" ".txt" "and again"]` |
| a load reports usage | `TestLoadReplaysThePrompts`: "a load reported usage" |
| the floor not enforced | `TestMinVersionRefusesAnOlderEngine`, `TestACPSpecRefusesA1xEngine`: "Start accepted the engine" |
| no version accepted | both: the error "does not name \"no version\"" |
| the starting model not applied | `TestStartAppliesTheRequestedModelAndLevel`: "set_config_option calls = \"\", want \"model=m2,effort=high\""; `TestACPStartAppliesTheRequestedModel` |
| the start shows session/new's options | the same: "the start's session_config shows model \"m1\", want \"m2\"" |
| a refusal says nothing | the same: "notice \"\" does not name \"m2\"" |
| the Spec's start bound ignored | `TestStartTimeoutBoundsTheHandshake`: "Start gave up after 5.002s; the Spec bounds initialize at 200ms" |
| modes through `session/set_mode` again | `TestModeIsSetThroughTheModeOption`: "reported modes [], want plan last"; `TestArmingAutoUsesTheModeOption`; `TestACPPlanSetsTheModeOption` |
| a moved mode option not reported | `TestModeOptionUpdateReportsTheMode`: "reported modes [], want build then plan" |
| the child-session flag not advertised | `TestACPSpecAdvertisesD1sCapabilities` |
| a completed child shown running | `TestACPChildUpdatesFillTheSubagentPanel` |
| an interrupted child not failed | `TestACPChildFailedOrInterruptedIsFailed` |
| a child's output in the parent's transcript | `TestACPChildUpdatesFillTheSubagentPanel`: "the child's output reached the parent's transcript" |
| a long back-off shown as a notice | `TestACPRetryLongEndsTheTurnWithTheLimitCard`: "the long back-off did not cancel the turn" |
| a short retry says nothing | `TestACPRetryShortIsANotice`: "notices = [], want one naming the retry" |
| a retry notice after the turn ended | `TestACPRetryOnAPromptResultSaysNothingShort`: "a retry notice after the turn ended: \"Retrying in 5s (attempt 3): Rate limited\"" |
| opencode's `goal` not forwarded | `TestACPCommandTableOnG1sCommands` |
| a row missing from the ACP table | `TestProvidersDeclareEveryCanonicalCommand/opencode#01`: "opencode declares no mapping for /ps" |
| auto choosing `allow_always` first | `TestACPAutoAllowsOnceByKind`: "auto answered {Outcome:selected OptionID:always}, want once selected" |

`TestACPRetryOnAPromptResultSaysNothingShort` first passed against its break: it waited for the
turn's end with `acptest.Next`, which discards the events before it. It now keeps every event from
the prompt on, and fails as shown.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 46 packages ok, no race |
| `make pre-add-check FILES=…` (the 14 changed and new Go files) | "14 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done, and why.**

* **No live run.** Nothing registers the Spec until P7, and P9's `make live-opencode` is the live
  acceptance. `/plan` through the mode option, the starting model, the start bound and the
  replayed prompts are tested on recorded and source-transcribed shapes only.
* **G1 saw no retry and no permission.** The retry frames are transcribed from opencode's source,
  as in P2, and the permission options from `packages/cli/src/acp/permission.ts:39-43`.
* **Seen, not changed:** the phone's generic config-option control (`SetConfigOption`) ignores
  the agent's answer, so a mode or model chosen there is shown only when the agent next sends its
  options. opencode answers with them but sends no update. It is outside P4.
* **C7's three hosts:** P4 changes no process, path, signal or pipe code, so it ran on macOS
  only.

P4 is complete with this commit. P5, the control plane, is next.

**Files for this commit:** `internal/provider/acpagent/acpagent.go`, `session.go`,
`session_meta.go`, `version.go`, and the tests `version_test.go`, `loadreplay_test.go`,
`startoptions_test.go` and `modeoption_test.go` (new); `internal/provider/opencode/acp.go`,
`acp_extensions.go` and `acp_spec_test.go` (new), `commandtable.go`, `version.go`;
`internal/command/conformance_test.go`; the MADR; and this plan.
