---
status: complete
date: 2026-11-04
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
  ~~*Amended 2026-11-04 (P9 Deviation 2):* `npm_config_cache`, in any case, is also pointed into
  the isolated home.~~ *Withdrawn the same day: the cache was not the cause, and the owner chose
  "Revert it" (P9 Deviation 2, corrected).*
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

  `opencode acp`: `initialize` names version 2.x, and a lease close exits 0. No prompt. *Annotated
  2026-11-04 (P9 Deviation 1): the pin never asserted the version; `opencode --version` was the only
  check. P9 adds it.*
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
*Added 2026-11-04 (P8 Deviation 1):* `internal/provider/acpagent/acpagent.go`,
`docs/guides/protocol-v1.md`, `docs/guides/chat-performance.md` and `docs/architecture.md`. The
mobile docs carry no OpenCode capability row.

1. `doctor` reads ~~`/api/info` and `agentInfo.version`~~ the binary's `--version` (P8 Deviation
   1), stops reading `auth.json`~~, and reports the service-port collision (A4)~~ (G3 showed Magic
   cannot collide; P8 Deviation 1).
2. `mcremote engines` names ACP children and the control plane apart. *Amended (P8 Deviation 1):*
   acpagent records the agents it starts, so `engines` can list them.
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

*The owner committed P4 as `98a62681`.*

### P5 Deviation 1, 2026-11-02 — the fixture guard on the Linux test host. The owner decided: "Fix it inside P5"

C8: `opencode --version` printed `v2.2.3`, at `98a62681`.

- **Found** by C7's run of the whole suite on the Linux test host, with P5's package written and
  passing there. One test outside P5 failed: `internal/wirecap`'s
  `TestCommittedFixturesCarryNoIdentifiers`, on
  `internal/provider/opencode/testdata/wire/2.2.3/frames.jsonl` lines 2 and 4, "this host's name,
  unredacted (MADR 0151 F9)".
  * It is not a leak. The guard fails on the running host's name anywhere in a fixture
    (`internal/wirecap/fixtures_test.go:80-101`, MADR 0151 D10). The Linux test host's short name
    is an ordinary word. It occurs, six times on each line, inside three public model ids in G1's
    catalog: one from opencode's own models, one from kilo's and one from OpenRouter's.
  * It predates P5: the fixture is `f15af43e` (0214 P2), and no phase since ran the whole suite
    on that host. It passes on macOS, where G1 was captured.
  * The Windows test host passed `make ci-windows` in the same run.
- **Resolution:** the guard checks a fixture against the running host's name only when that host
  captured it.
  * A capture host is identified by a random key kept in the user's config directory, never by its
    name: a hash of a name that is a dictionary word would name it.
  * `wirecap` writes the key's tag (`capture_host`) beside the frames it captures, when the key
    exists. It never creates the key on its own, so a test that turns capture on leaves nothing in
    the operator's config directory.
  * A fixture's `meta.json` carries `capture_host`. The guard applies the host-name rule when the
    fixture's tag is this host's, or when the fixture has none, which keeps today's rule for every
    other fixture.
  * The 2.2.3 fixture gets this Mac's tag, where G1 captured it. The key is created on this Mac
    once, for that.
  * `wirecap`'s redaction is unchanged (the owner's option): a capture taken on that host still
    writes those model ids with `<host>`.
- **Files added to P5's scope:** `internal/wirecap/wirecap.go`, `internal/wirecap/fixtures_test.go`
  and a new test for the tag, and `internal/provider/opencode/testdata/wire/2.2.3/meta.json`. MADR
  0151 gets an amendment for D10.

### P5 — 2026-11-02: the control plane

**What changed.**

* **`internal/provider/opencode/control` (new), `Plane`.** Nothing registers or calls it yet;
  P6 does.
  * **Launch (step 1).**
    * `serve --stdio --port 0` through `launch.Command`, in its own process group under a job
      object, registered in the engine registry like httpagent's engine.
    * A 24-byte random password per launch, only in the child's `OPENCODE_PASSWORD`.
    * Ready means one `{"url"}` line on stdout, then `/api/info` answering JSON with a version at
      or above the floor, which the caller sets. stdout is drained after the ready line.
  * **Lifetime (step 2).** Close ends the lease (stdin); after 5 s, SIGTERM; after 5 s more, the
    kill. Exit 0, and 130 on POSIX only, are logged as orderly; anything else, a Windows forced
    stop included, as an exit.
  * **Requests (step 3).**
    * HTTP Basic `opencode:<password>`.
    * A directory travels as `location[directory]` and `x-opencode-directory`; `WithLocation`
      adds `location.directory` to a create body.
    * An answer with a body must be `application/json`, or the error names the content type and
      the first bytes (D12).
  * **On demand (step 4).**
    * It starts on first use and stops after 10 idle minutes; a child that died is replaced on the
      next use.
    * `GET /api/agent` is retried on 503 up to 6 times, the wait doubling from 250 ms.
    * `GET /api/credential` is refused before it is sent (`ErrCredentialRead`).
  * **Captures.** The engine's answers go to `wirecap` as `opencode-control`, so P6 can record the
    v2 HTTP surface; a request, and so its password, is never captured.
* **The fixture guard (Deviation 1).**
  * `wirecap.HostTag` is the hash of a random key in the user's config directory.
    * `wirecap.For` writes it beside a capture as `capture_host` when the key exists, and never
      creates the key.
    * `EnvHostKey` points the key elsewhere, for tests.
  * `TestCommittedFixturesCarryNoIdentifiers` checks the running host's name in a fixture captured
    on this host, or in one that names no capture host.
  * The 2.2.3 fixture names this Mac. Its key was created once, with `HostTag(true)` run from a
    scratch copy of the module; it is the only file written outside the tree.

**Tests, each seen failing.** The fake engine is the control package's test binary, re-run as
`serve --stdio`; it is Go, so it runs on all three hosts. `p6/plant_p5.py` holds 19 deliberate
breaks and `p6/plant_p5b.py` seven, in the session scratchpad, on scratch copies with a 90 s bound,
so a hang counts as a failure.

| Break | Fails |
| --- | --- |
| the password in argv | `TestPasswordIsNotInArgv` (C5) |
| the Authorization header logged | `TestPasswordIsNotLogged`: "the log carries the Authorization header" (C5) |
| the password captured | `TestPasswordIsNotCaptured`: "a capture carries the password" (C5) |
| no password on a request | `TestRequestsCarryTheirPasswordAndLocation`: "/api/info answered 401" |
| no `location[directory]`; no `x-opencode-directory`; no `location.directory` | `TestRequestsCarryTheirPasswordAndLocation`, each naming the missing one |
| credentials read | `TestNeverReadsCredentials`: "err <nil>, want ErrCredentialRead" |
| no 503 retry; one retry too many | `TestAgentsRetry503`: "after two 503s: 503, want 200"; "7 agent requests, want 6" |
| any ready line, any answer, any version accepted | `TestStartRefusesWhatIsNotTheEngine`, `TestAnswerMustBeJSON` |
| 130 orderly on the wrong hosts | `TestOrderly`: "Orderly(130) = false on darwin, want true"; `TestIgnoredLeaseIsTerminated` |
| the lease not closed first | `TestLeaseCloseIsOrderly`: "want an orderly stop with 0" (the child got SIGTERM: 130) |
| no terminate after the lease | `TestIgnoredTerminateIsKilled`: "Close did not return" |
| no idle stop | `TestIdleStopsAndNextUseRestarts` |
| a dead child reused | `TestRestartsAfterTheChildDies`: "connection refused" |
| stdout not drained after the ready line | `TestStdoutIsDrainedAfterReady`: "a child that wrote 1 MiB to stdout stalled" |
| the host rule applied whatever the tag; skipped on the capture host | `TestHostRuleAppliesOnTheCaptureHost` |
| a capture makes the key; writes no tag | `TestCaptureWritesTheTagOnlyWhenAKeyExists` |
| asking without create makes the key; a key others can read | `TestHostTagIsAKeyNotAName` |
| the tagged 2.2.3 fixture carrying this Mac's name | `TestCommittedFixturesCarryNoIdentifiers`: "line 54: this host's name, unredacted" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 5 changed and new files | exit 0, 47 packages ok; before Deviation 1, the one failure above |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (the 4 Go files) | "4 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done, and why.**

* **No live run.** G3's live pins already hold the real `serve --stdio` lease on three hosts;
  this phase's lease tests run on the fake. P9's `make live-opencode` runs the real one.
* **Not wired.** Nothing calls `Plane` until P6.
* **The redaction side of Deviation 1 is unchanged**, as the owner chose: a capture taken on the
  Linux test host would still write those model ids with `<host>`.

P5 is complete with this commit. P6, the features on the control plane, is next.

**Files for this commit:** `internal/provider/opencode/control/control.go` and
`control_test.go` (new); `internal/wirecap/wirecap.go` and `fixtures_test.go`;
`internal/provider/opencode/testdata/wire/2.2.3/meta.json`; MADR 0151; and this plan.

*The owner committed P5 as `254279ac`.*

### P6 Deviation 1, 2026-11-02 — P6's files are the 1.x dialect's; P6 is split. The owner decided: "Approve, start P6a"

C8: `opencode --version` printed `v2.2.3`, at `254279ac`.

- **Found** before the first write. Every file P6 names is the 1.x dialect's, which P7 deletes:
  * `auth.go`, `device_auth.go`, `upstream.go`, `workspace.go` and `diagnostics_surface.go` are
    methods of `httpDialect` and `httpSession` on 1.x routes (`/auth/…`, `/file`, `/find`,
    `/skill`, `/lsp`);
  * `credstore.go` is the `auth.json` store kilo still uses. opencode v2 keeps credentials in its
    database (0220 §5 E), so P6 does not touch it.
- **What the ACP session lacks.** acpagent's session implements none of `WorkspaceSession`,
  `ShellSession`, `RevertSession` or `DiffSession`. `Diagnostics` reports MCP only. No Spec hook
  reaches any of them. P6's v2 features need agent-neutral operations in acpagent first, as
  amendment C did for P4.
- **Two further facts:**
  * **No native message ids on an ACP session.** acpagent stamps no `NativeMessageID`, which
    `transcript_remove` works by (`internal/session/manager.go:335-343`). ACP's prompt request
    carries no client message id in acp-go-sdk2 v1.31.1, and opencode echoes no
    `user_message_chunk` during a live turn (G1 recorded none). Its agent chunks do carry
    `messageId`.
  * **D8's restart has no mechanism.** acpagent runs one process per session. "Restart the ACP
    engine at its next idle point" means each live session's process, reattached with
    `session/load`, and nothing in acpagent does that today.
- **Proposed:** P6 runs as five sub-phases. Each is one commit, and each needs the owner's word
  to start.
  * **P6a — acpagent, agent-neutral.**
    * `Spec.Ops` gains workspace (list, read, search), shell, revert and unrevert, diff, skill
      refresh, and a diagnostics extension beside MCP. The session implements each interface
      through them, returning "not offered" when nil.
    * Capabilities: workspace, shell and skill refresh join `provider.Capability`, declared from
      the Ops. The manager's workspace and shell checks read the declaration, as P6d did for
      rename, fork, revert and diff (0208 D1).
    * `Host.Busy()`, so a Spec can refuse an operation while a turn runs.
    * The ACP `messageId` of agent and thought chunks, and of replayed prompts, becomes the
      event's `NativeMessageID`.
    * `Provider.RestartSessionsWhenIdle()`: each live session, at its next idle point, closes its
      agent process and reattaches it with `session/load`, keeping Magic's session.
    * Files: `internal/provider/acpagent/` (`acpagent.go`, `host.go`, `session.go`, and new files
      for the operations and the restart), `internal/provider/provider.go`,
      `internal/session/manager.go`, and their tests.
  * **P6b — auth on the control plane (D8).**
    * The Spec's auth hooks, on `control.Plane`: status and the catalog from
      `/api/integration`; key connects through `connect/key`; OAuth through `connect/oauth` and
      its status; command connects through `connect/command`.
    * A removal deletes the integration's credential by its id, read from `integration.get`
      (never `credential.list`), and is confirmed by reading the integration again.
    * An add or a removal calls `RestartSessionsWhenIdle`.
    * Files: `internal/provider/opencode/acp.go` and a new `acp_auth.go`, and tests.
  * **P6c — the catalog (D7).** `Spec.ListModels`: the ACP `model` option's values, grouped
    connected or not configured from `GET /api/provider` while the control plane runs. Files:
    `acp.go`, a new `acp_catalog.go`, and tests.
  * **P6d — workspace, diagnostics, VCS and diff (D10).**
    * Workspace on `fs.list`, `fs.read` and `fs.find`; full-text search dropped by name (#34).
    * Diagnostics on `mcp`, `skill`, `plugin`, `vcs` and `vcs/status`; diff on `session.diff`.
    * LSP and formatter dropped by name.
    * Files: `acp.go`, a new `acp_surfaces.go`, and tests.
  * **P6e — revert, unrevert and `!` shell (D11, lifted).**
    * `revert/stage`, `DELETE …/revert`, `revert/commit` and `session.shell`, on an idle session
      only. Magic shows the shell's output, and emits `transcript_remove` for the reverted
      messages.
    * A live prompt has no native id (above). How its row is removed on a revert is decided at
      P6e's start, with the evidence then in hand.
    * Files are named at P6e's start.
  * The control plane is passed to the Spec's hooks through a small interface, so each
    sub-phase's tests use a fake rather than a process.
- **Not part of P6:** the 1.x dialect's files, deleted in P7; `credstore.go`, which kilo keeps.
- **Added at P6a's start, before its first code write. The owner decided: "Add httpagent and
  fake".** The new capabilities have to be declared by every session that has the operation, or
  the manager's checks would take away what it offers today. httpagent already reports workspace,
  skill refresh and shell (operator policy included) to the phone through `session_capabilities`
  (`internal/provider/httpagent/session.go:393-407`); fake implements all three.
  `internal/provider/httpagent/session.go` and `internal/provider/fake/fake.go`, and their tests,
  join P6a's files.
- **Added during P6a, 2026-11-03. The owner decided: "Restate on the declaration".**
  * grok's `TestGrokImplementsUndoButNotRevert` (`internal/provider/grok/ops_test.go:196-204`)
    asserted that grok's session type lacks `RevertSession`.
  * acpagent's session type now implements it for every Spec, and a session without
    `Ops.Revert` does not declare it (0208 D1).
  * The test is restated on the declaration: grok declares undo and not revert, and `Revert`
    answers "not offered" without reaching the agent. The property is unchanged.
  * `internal/provider/grok/ops_test.go` joins P6a's files.

### P6a — 2026-11-03: acpagent, agent-neutral

**What changed.**

* **`Spec.Ops` gains eight operations** (`host.go`): `ListWorkspace`, `ReadWorkspace`,
  `SearchWorkspace`, `Shell`, `Revert`, `Unrevert`, `Diff`, `RefreshSkills`, plus `Diagnostics`.
  * The session implements `WorkspaceSession`, `ShellSession`, `RevertSession`, `DiffSession` and
    `SkillRefreshSession` through them (new `surfaces.go`). Without the hook, each answers "not
    offered".
  * Workspace counts as offered only with all three parts, and revert only with unrevert.
  * `Diagnostics` is the Spec's answer, with MCP from the session's own list unless the answer
    carries it.
* **Declarations.**
  * `provider.CapWorkspace`, `CapShell` and `CapSkillRefresh` are new.
  * acpagent declares the five operations from its Ops, and tells the phone workspace, skill
    refresh and shell through `session_capabilities`.
  * httpagent declares the three new ones from what it already told the phone, the operator's
    shell policy included; fake declares all three.
  * The manager's workspace, shell and skill-refresh checks read the declaration.
* **`Host.Busy()`:** whether a turn is running.
* **Native message ids.** The ACP `messageId` of agent and thought chunks, and of a replayed prompt,
  is the event's `NativeMessageID`. The manager's replay deduplication does not read it, so
  nothing else changes.
* **The idle restart** (new `restart.go`).
  * The provider tracks its open sessions. `RestartSessionsWhenIdle(notice)` restarts each one:
    an idle one at once, a busy one when its turn and any queued prompt end.
  * A restart posts the notice, reports the session disconnected with no error, and ends its
    agent. The manager then closes it as it does any ended agent, and resume reattaches it with
    `session/load`.
  * The warm spare is discarded, and so is a spare that was warming across the restart.
* **grok:** `TestGrokImplementsUndoButNotRevert` is restated on the declaration (above).

**Tests, each seen failing** (`p6/plant_p6a.py` in the session scratchpad, 21 deliberate breaks on
scratch copies):

| Break | Fails |
| --- | --- |
| httpagent drops workspace; ignores the operator's shell policy | `TestCapabilitiesFollowTheDialect`, the all-operations and the shell-forbidden cases |
| fake drops shell; `Implements` maps shell to `ForkSession` | `TestCapabilitiesAreWhatTheFakeImplements`: "want \"shell\" among them"; "declares \"shell\" without the interface that performs it" |
| a partial workspace declared; revert declared without unrevert | `TestSurfacesFollowTheSpecsOps/part_of_workspace,_revert_without_unrevert` |
| a missing shell hook answers success; the phone told shell is there with no hook | `TestSurfacesFollowTheSpecsOps/none_set`: "operation 3: <nil>, want not offered"; "shell true; want all false" |
| the session's MCP replaces the Spec's; the Spec's diagnostics ignored | `TestDiagnosticsJoinTheSpecsAndMCP` |
| `Busy` never true | `TestBusyIsTheTurn`: "not busy while the prompt is held" |
| no native id on the answer; on a replayed prompt | `TestNativeMessageIDs` |
| a busy session restarted under its turn; a waiting restart never run; a restart reported as an error; a closed session kept | `TestRestartSessionsWhenIdle`, each naming its case ("the provider tracks 3 live sessions, want the 2 not closed" for the last) |
| the spare kept; a warming spare kept | `TestRestartDiscardsTheSpare` |
| the manager's shell gate dropped, with a shell that reaches the agent | `TestUndeclaredOperationsAreUnavailable`: "the agent was sent \"_vendor/shell\"" |
| revert declared for every Spec | grok's `TestGrokImplementsUndoButNotRevert`: "the grok session declares revert" |

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 16 changed and new files | exit 0, 47 packages ok |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (the 16 Go files) | "16 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done:**
* Nothing calls the new operations or the restart yet; P6b to P6e do, for opencode.
* No live run: grok's Spec sets none of the new operations, so its behaviour is unchanged.

P6a is complete with this commit. P6b, auth on the control plane, is next.

**Files for this commit:** `internal/provider/acpagent/acpagent.go`, `host.go`, `session.go`,
`sessioncaps.go`, `surfaces.go` and `restart.go` (new), and the tests `restart_test.go` and
`surfaces_test.go` (new); `internal/provider/provider.go`; `internal/provider/httpagent/session.go`
and `capabilities_test.go`; `internal/provider/fake/fake.go` and `capabilities_test.go`;
`internal/provider/grok/ops_test.go`; `internal/session/manager.go` and
`capabilities_internal_test.go`; the MADR; and this plan.

### P6b Deviation 1, 2026-11-03 — several credentials per integration, no command methods, and a narrow device hook. The owner decided: "Replace, clear all", "Drop by name", "Not in P6b" and "Widen the hook"

C8: `opencode --version` printed `v2.2.3`, at `6b1d3b84`.

Found before the first code write. The evidence is the opencode source at `b44b7164ba` and one
read-only probe: `opencode serve --stdio` on an isolated home with no credential, reading
`GET /api/integration` and `GET /api/provider`.

* **An integration can hold several credentials. P6b assumed one.**
  * `connect/key` always creates a new credential, labelled with the integration's name and a
    number when that is taken, and makes it the active one. It never replaces an older one
    (`packages/core/src/integration.ts`, `createCredential`; `packages/core/src/credential.ts`,
    `create`).
  * `integration.get` lists the active credential first (`resolveConnections`).
  * Removing the active credential makes the newest remaining one active (`credential.ts`,
    `remove`). A removal of the active credential alone could leave the provider running on an
    older key.
  * **Decided: "Replace, clear all".**
    * An add stores the new key and confirms it is the active connection. Then it removes the
      integration's older credentials of the same kind (key or OAuth), and confirms they are gone.
    * A removal deletes every stored credential of the integration, then confirms none remains.
    * A key supplied only through an environment variable cannot be removed. The removal says the
      provider is still configured through it, naming the variable.
    * This is what 1.x's `PUT` and `DELETE /auth/{id}` did. A key added in a terminal is replaced
      by a phone re-key of the same kind.
* **No integration offers a command method on 2.2.3.**
  * The probe listed 237 integrations in 34.7 KB. Method types: key 235, environment variable 236,
    OAuth 10, external 4, command 0. Every key form's fields are strings (15 fields).
  * **Decided: "Drop by name".** Command connects are not built in P6b.
  * A method of a type the phone cannot drive is listed as unavailable with the reason
    `host_oauth`, so the phone never offers a button that cannot work. That covers a command
    method if one appears, and the 4 external methods: AWS profile, Google Cloud credentials, the
    Azure CLI, and the Claude Code login.
* **`activate` has no phone operation.** MADR D8 lists it; P6b's steps do not.
  * Switching the active upstream means choosing the model vendor, which is P6c's catalog.
  * With "Replace, clear all", a provider never holds two phone-added credentials of the same kind.
  * **Decided: "Not in P6b".** A provider with both a key and an OAuth sign-in uses whichever was
    added last.
* **acpagent's device-flow hook drops the method and the form answers.**
  * `Provider.StartDeviceAuth` passes the Spec only the upstream and the confirmation
    (`internal/provider/acpagent/acpagent.go:358-362`).
  * opencode needs both. openai offers three OAuth methods, and GitHub Copilot's form asks for a
    deployment type and an enterprise URL.
  * grok is the only Spec that sets the hook (`internal/provider/grok/grok.go:66`).
  * **Decided: "Widen the hook".** `Spec.StartDeviceAuth` also takes the method id and the inputs,
    and acpagent passes them through. grok's `startDeviceAuth` accepts and ignores them, so its
    behaviour is unchanged. `internal/provider/acpagent/acpagent.go` and
    `internal/provider/grok/device_auth.go` join P6b's files, with a pass-through test.
* **Unchanged by these decisions, and following existing decisions:**
  * An OAuth attempt is classified when it starts, from its URL and instructions, as MADR 0074 D7
    does.
    * A browser redirect to the host's loopback is refused, as it is today.
    * So is an attempt in `code` mode, which needs a pasted code the phone has no field for.
    * Either refused attempt is cancelled on the control plane.
  * The flow waits on the attempt's own status (`connect/oauth/{attempt}`), not on a poll of the
    configured set, and cancels the attempt when the phone cancels.
  * The status the phone reads on every provider list is small:
    * the integrations with a connection;
    * those whose sign-in needs more than a bare key (OAuth, external, or a key form).

    The full list is the catalog.
  * `opencodeAuthFingerprint`'s `auth.json` read is not used. The integration's connections
    replace it (D8).
* **Added before the first code write, 2026-11-03. The owner decided: "List first, and pin it".**
  * **Found.** Until plugins have activated in a location, `integration.get` and the connect routes
    answer `404 IntegrationNotFoundError` for an integration that exists.
    * Only `integration.list` waits for activation
      (`packages/server/src/handlers/integration.ts`: `yield* Plugin.awaitActivation` in list, and
      in no other route).
    * Probes on isolated homes, with a made-up key and no provider contacted:
      * The first get or connect in a location not used before answered 404 in 8 of 17 tries.
        The next call answered normally.
      * Naming a new location directory each time, on two engines (one probed at once, one after
        3 s): the first `integration.get` answered 404 in 10 of 10, and the second 200. The
        engine's own location, used when a call names none, answered 404 once, to a connect sent
        as the engine became ready.
      * With a list call first in the same location, get and connect answered normally in 6 of 6,
        with a location and without one.
  * **Decided.** Every auth operation reads `GET /api/integration` in its location first. Status
    and the catalog use that answer. An add or a removal reads it before `integration.get` or
    a connect.
    * A fake-engine test fails if get or connect comes first.
    * A live-tagged pin records the 404 before activation and the answer after a list, so a
      changed engine shows up. It uses an isolated home and a made-up key, and spends no provider
      quota.
    * `internal/provider/opencode/live_acp_auth_test.go` (new) joins P6b's files.
  * The other probe answers the code relies on:
    * a connect answers 204;
    * a key form answer on a method with no form answers 400 `InvalidRequestError` with the message
      "Key method does not accept a form answer";
    * an unknown OAuth method answers 500 with no body;
    * `DELETE /api/credential/{id}` answers 204 for an id that does not exist, as B6 recorded.

### P6b — 2026-11-03: auth on the control plane

**What changed.**

* **`acp_auth.go` (new)** implements the Spec's auth hooks on the control plane, reached through a
  small `controlPlane` interface.
  * **Order.** Every operation lists the integrations first, the call that waits for plugin
    activation.
    * The list runs apart from its caller and is bounded at 3 minutes. A cold start that outlasts
      a short caller, such as the provider list's 20 s, therefore finishes and serves the next
      call.
    * Calls name no location, so they use the control plane's own.
  * **Status** lists the configured integrations and those whose sign-in is more than a bare key.
    **The catalog** lists every integration. Both leave out an MCP server's sign-in.
  * **Methods.**
    * A key method is `<id>:key`. 1.x's `<id>:api` is still accepted on a write.
    * An OAuth method is `<id>:oauth:<method>`, typed as a browser flow when its id or label says
      so, otherwise as a device flow.
    * External methods, and any other type, are listed as unavailable with `host_oauth`. So is a
      form with a field that is not a string.
    * A form's string fields become the phone's inputs, as a select when they have fixed options.
      A hidden field is left to its default; a default otherwise shows as the placeholder.
    * Whether a method holds a credential is known for the key, and for OAuth only when the
      integration has one OAuth method: the connection does not say which method stored it.
  * **An add** (`SetCredential`):
    * reads the integration;
    * sends the key once to `connect/key`, with the form answer when one is given, leaving out
      empty values;
    * confirms the new credential is the active connection, or answers
      `ErrCredentialNotAccepted`;
    * removes the older keys, and confirms they are gone.
  * **A removal** (`ClearCredential`) deletes every stored credential and confirms none remains.
    A key from the environment is named in the error.
  * **OAuth** (`StartDeviceAuth`):
    * `connect/oauth` with the method and the answer;
    * a `code`-mode or loopback-browser attempt is refused and cancelled;
    * the wait follows the attempt's status, and on completion adopts the credential as an add
      does;
    * a cancelled wait cancels the attempt.
  * **Restart.** Any change the engine accepted restarts the provider's open sessions at their
    next idle point, with the notice "A provider credential changed; the agent restarts to apply
    it."
* **`acp.go`.** `NewACP` builds a control plane from the configured binary, with the version floor
  and the upgrade link, and wires the hooks. The restart closes over the provider it builds.
* **acpagent** passes the method id and a copy of the inputs to `Spec.StartDeviceAuth`. grok's hook
  ignores them.

**Tests, each seen failing** (`p6b/plant_p6b.py` in the session scratchpad, 30 deliberate breaks on
scratch copies):

| Break | Fails |
| --- | --- |
| no list before get | `TestACPAuthListsFirst`: "opencode GET /api/integration/vendor: 404 IntegrationNotFoundError" |
| status lists every bare key; lists an MCP sign-in | `TestACPAuthStatusIsSmall`: "status lists [… plain]", "[… mcp-server …]" |
| the catalog lists an MCP sign-in | `TestACPAuthCatalogListsEveryProvider` |
| an add keeps the older key; removes every kind; sends an empty answer | `TestACPSetCredentialReplacesTheOlderKey` |
| an add restarts nothing | `TestACPSetCredentialReplacesTheOlderKey` ("restarts [], want one") and `TestACPProviderWiresAuth` |
| an add not checked active; its removal not confirmed; any method taken | `TestACPSetCredentialChecksTheEngine`, its three cases |
| a removal deletes only the active one; says nothing of the env; is not confirmed; restarts with nothing removed | `TestACPClearCredentialRemovesEvery`, its three cases |
| a code-mode attempt kept; a refused attempt not cancelled; a cancelled wait leaves the attempt; a completed sign-in not adopted; a failed one reported done; the first of two methods taken | `TestACPDeviceAuth`, each case |
| a hidden field shown; a number field offered; an external method offered; which OAuth stored it claimed known; a browser method shown as a device | `TestACPAuthStatusIsSmall` |
| `newACP` restarts nothing | `TestACPProviderWiresAuth`: "no notice event within 5s" |
| acpagent drops the method; passes the caller's inputs | `TestStartDeviceAuthPassesTheMethodAndInputs` |
| the live pin's cold location listed first | `TestLiveV2IntegrationsWaitForActivation`: "the first get in a new location answered 200" |

Two breaks were rewritten before they took. "An add not checked active" first left part of the
condition in force. "Any method taken" first did not compile. Neither was a fault in a test.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 9 changed and new files | exit 0, 47 packages ok |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `go test -tags live_opencode -run LiveV2IntegrationsWaitForActivation` (opencode 2.2.3, isolated home, made-up key) | exit 0 |
| `make pre-add-check FILES=…` (the 7 Go files) | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done:**
* Nothing registers the provider until P7, so no phone has used these hooks.
* `activate` and command connects are not built (Deviation 1).
* The control plane is not closed at shutdown. It stops after 10 idle minutes, and its lease ends
  with Magic. P7, which registers the provider, owns shutdown.
* `SetActiveUpstream` is not offered. Choosing the model vendor is P6c's catalog.
* No OAuth flow was run live: it would contact a vendor.

P6b is complete with this commit. P6c, the catalog, is next.

**Files for this commit:** `internal/provider/opencode/acp.go`, `acp_auth.go`, `acp_auth_test.go`
and `live_acp_auth_test.go` (the last three new); `internal/provider/acpagent/acpagent.go` and
`credential_test.go`; `internal/provider/grok/device_auth.go`; the MADR; and this plan.

### P6c Deviation 1, 2026-11-03 — a configured key in `/api/provider`, no drill-down in acpagent, and model ids only from a session. The owner decided: "Never read them", "Agent-neutral in acpagent" and "Latest session's option"

C8: `opencode --version` printed `v2.2.3`, at `5ca425da`.

Found before the first code write. The evidence is the opencode source at `b44b7164ba` and probes on
an isolated home with two made-up keys: one stored through `connect/key`, one written in
`opencode.json` as a provider option. No prompt was sent and no provider was contacted.

* **`/api/provider` and `/api/model` return a configured key.**
  * The key in `opencode.json` came back in plain text from `GET /api/provider`,
    `/api/provider/mistral` and `/api/model`, in the provider's or model's `settings`.
  * The stored key came back from none of them.
  * `GET /api/integration`, `/api/integration/{id}` and ACP `session/new` returned neither key.
  * The control plane writes every response body to its wire capture (`control.go`, `send`).
  * D7 and P6c took connected-or-not from `/api/provider`. They no longer need to:
    * The ACP `model` option lists exactly the usable providers' models. In the probe it listed
      379: openrouter 351, mistral 20 and opencode 8, the same three providers `/api/provider`
      listed.
    * mistral was usable through its `opencode.json` key alone, with no stored credential.
    * `/api/integration` names every vendor.
  * **Decided: "Never read them".**
    * A vendor is connected when the model option lists its models.
    * Its name, and the vendors not configured, come from `/api/integration`.
    * The control plane refuses `/api/provider` and `/api/model`, and anything under them, as it
      refuses `/api/credential`.
    * `internal/provider/opencode/control/control.go` and `control_test.go` join P6c's files.
* **acpagent has no drill-down.**
  * It implements neither `provider.ModelProviderCatalog` (`ListModelProviders`, `ListModelsFor`)
    nor `provider.ModelCatalogSession`.
  * Without them the daemon gives the phone one implicit vendor and a flat list
    (`internal/ws/server.go:2927-2960`).
  * An open session's /model picker gets the provider-wide list, which for opencode is the 7 static
    models.
  * **Decided: "Agent-neutral in acpagent".**
    * A Spec hook builds the vendor step from the model list.
    * acpagent implements `ListModelsFor` and the session's own catalog by filtering models by
      vendor, for every Spec.
    * A Spec without the hook answers with one implicit vendor, as the daemon does today, so grok
      is unchanged.
    * `internal/provider/acpagent/acpagent.go`, `session_meta.go`, a new `modelcatalog.go` and
      their tests join P6c's files.
* **Model ids come only from a session.**
  * opencode sends its `model` option at `session/new`, and names no model at `initialize`, so
    nothing reaches acpagent's provider-wide catalog.
  * The create-session picker always shows the 7 static models. To show them, acpagent spawns an
    agent that answers nothing.
  * **Decided: "Latest session's option".**
    * With a Spec hook set, acpagent keeps the latest session's `model` option values as the
      provider catalog, and each later report replaces them.
    * The static list is shown only before any session has run. opencode's `Spec.ListModels`
      answers empty, so no agent is spawned to learn nothing.
    * A credential change restarts the sessions (P6b), and they report again.
    * grok sets no such hook.
* `Spec.ListModels` therefore no longer reads `GET /api/provider`, as P6c first said. Its files are
  `internal/provider/opencode/acp.go`, a new `acp_catalog.go`, and their tests, with the files
  above.

### P6c — 2026-11-03: the catalog

**What changed.**

* **The control plane refuses** `/api/provider` and `/api/model`, and any path under them, by any
  method, before anything starts. A path that only shares the prefix, such as `/api/providers`, is
  not refused (`ErrProviderRoute`, `control.go`).
* **acpagent, for every Spec** (new `modelcatalog.go`):
  * **`Spec.ModelsFromOption`.** When a session records its config options (at `session/new`,
    `session/load`, after `session/set_config_option`, or on `config_option_update`), its `model`
    option's values become the provider's catalog, through the hook, and replace the last.
    * The values arrive as picker options: the value, the name, and the agent's group when it
      groups them.
    * The catalog's default is the option's current value.
  * **`Spec.ModelProviders`** builds the vendor step. The provider implements
    `ListModelProviders` and `ListModelsFor` (one vendor's models, by group). The session
    implements `ModelCatalog`: the vendor step, or its own option's models of its current model's
    vendor.
  * **A Spec with no `ModelProviders`** answers one implicit vendor, its own ID, connected, and
    `ListModelsFor` of that ID is the provider's list: what the daemon answered before. Its
    session reports no catalog of its own, so the daemon answers with the provider's. grok sets
    neither hook.
* **opencode** (new `acp_catalog.go`, wired in `acp.go`):
  * A model's vendor is its value's prefix (`openrouter/…`), and its label drops the vendor that
    its name repeats.
  * The vendor step lists:
    * the connected vendors, in the order the models list them, each with its model count;
    * then every other integration, by name, as not configured.

    Names come from `/api/integration`, and MCP sign-ins are left out. If the integration list
    cannot be read, the connected vendors are listed by id.
  * `Spec.ListModels` answers empty. Before any session has reported, the picker shows the static
    list, and no agent is spawned.

**Tests, each seen failing** (`p6c/plant_p6c.py` in the session scratchpad, 20 deliberate breaks on
scratch copies):

| Break | Fails |
| --- | --- |
| provider routes not refused; a prefix refused as a route | `TestNeverCallsProviderRoutes`: "GET /api/provider: err <nil>, want ErrProviderRoute"; "underRoute matches a route that only shares the prefix" |
| the option never kept; kept past the hook; kept without the hook | `TestModelOptionBecomesTheCatalog` ("without the hook: [a/two], want the static list unchanged" for the last) |
| grouped values dropped | `TestModelOptionsKeepTheAgentsGroups` |
| one vendor's models unfiltered; another vendor's default kept; the session's models unfiltered | `TestModelProvidersDrillDown` |
| no hook: no vendor; a session catalog anyway; every vendor the one | `TestNoModelProvidersKeepsOneVendor` |
| the vendor kept in the label | `TestACPModelsFromOptionGroupsByVendor`, `TestACPProviderCatalog` |
| an MCP server listed; not configured left unsorted; connected shown not configured; the provider route read | `TestACPModelProvidersGroupsConnected` ("sent GET /api/provider" for the last) |
| no engine, no vendors | `TestACPModelProvidersWithoutTheEngine` |
| `newACP` keeps no option; builds no vendor step | `TestACPProviderCatalog` |

Three breaks did not compile at first and were rewritten: "no hook, no vendor", and the two that
left an import unused. None was a fault in a test.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 11 changed and new files | exit 0, 47 packages ok |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (the 9 Go files) | "9 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done:**
* Nothing registers the provider until P7, so no phone has seen this catalog.
* The catalog is the latest session's. Sessions in projects whose `opencode.json` adds different
  providers replace each other's lists.
* A vendor that is not configured lists no models. The ACP option names none, and `/api/model`,
  which would, is refused.
* No live run. The option's shape is G1's recording and this deviation's probe, and the code reads
  no new route.

P6c is complete with this commit. P6d, workspace, diagnostics, VCS and diff, is next.

**Files for this commit:** `internal/provider/opencode/control/control.go` and `control_test.go`;
`internal/provider/acpagent/acpagent.go`, `session_meta.go`, and `modelcatalog.go` and
`modelcatalog_test.go` (new); `internal/provider/opencode/acp.go`, and `acp_catalog.go` and
`acp_catalog_test.go` (new); the MADR; and this plan.

### P6d Deviation 1, 2026-11-03 — `fs.read` answers raw bytes, plugins and skill refresh need records of their own, the control plane starts its own MCP servers, and `session.diff` is one turn. The owner decided: "Raw request, uncaptured", a plugins MADR, a skill-refresh MADR, "Own record; P6d omits MCP" and "Whole session"

C8: `opencode --version` printed `v2.2.3`, at `beb31c3b` and again at `020aa379`.

Found before the first code write. The evidence:

* the opencode source at `b44b7164ba`;
* probes on isolated homes, sending no prompt (`p6d/probe_surfaces.py`, `p6d/probe_mcp_spawn.py`
  and `skills/probe_*.py` in the session scratchpad):
  * the project was a git repository with a committed file changed since, an untracked file, and
    a symlink out of the project;
  * `opencode.json` declared a remote MCP server with a made-up bearer token, and a local one with
    a made-up secret in its environment.

No route echoed either made-up secret.

* **The workspace routes reach outside the project.**
  * `fs.list?path=.` answers `{location, data:[{path, type}]}`. Paths are relative to the location,
    and a directory's ends in `/`.
  * With an absolute path or `..`, it listed the directory above the project, the database
    included.
  * `fs.find?query=…&limit=…` answers `data:[{path, type}]`.
  * As planned, the 1.x guards run before any request (`normalizeWorkspacePath`,
    `checkNoSymlinkComponents`), and every returned path is re-checked (`validateReturnedPath`). P7
    keeps those helpers when it retires `workspace.go`.
* **`fs.read` answers raw bytes.**
  * `GET /api/fs/read/README.md` answered 200 `text/markdown` with the file's bytes.
  * A symlink, a missing file and a directory each answered 404 `FileNotFoundError`.
  * The control plane refuses any answer that is not JSON (D12).
  * **Decided: "Raw request, uncaptured".**
    * `control.Request` gains a raw mode: no JSON requirement and a read bound the caller sets.
      The body never enters the wire capture; only its size is recorded.
    * The 1.x content rules still apply: NUL or invalid UTF-8 is `binary_content`, and more than
      262,144 bytes is `result_too_large`, never a partial file.
    * `internal/provider/opencode/control/control.go` and `control_test.go` join P6d's files.
* **Plugins.**
  * `/api/plugin` was in P6d's list.
  * The owner: "i do want a fully spec's, scoped, hardened, functional, and flexible plugins
    section … draft a detailed madr."
  * **Decided:** plugins are `0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md`
    (proposed). P6d reads no plugin route.
* **Skill refresh.**
  * The owner: "this is core functionality. evaluate the best way to implement this and write an
    madr."
  * **Decided:** skill refresh is `0229-MADR-make-skill-discovery-freshness-and-refresh-agent-neutral.md`
    (proposed). P6d sets no `Ops.RefreshSkills`.
  * P6d's skill list keeps name and description only, as 1.x does.
  * The first `/api/skill` in a new location answered 2 of its 3 built-in skills. P6d therefore
    reads it after `GET /api/integration` in the same location: the route that waits for plugin
    activation (P6b Deviation 1), and the order 0229 D8 asks for.
* **The control plane starts the user's local MCP servers.**
  * A location's boot connects every configured MCP server in the background
    (`packages/core/src/mcp/index.ts:660-688`). v2 has no switch for MCP as a whole; a server is
    disabled only by name.
  * Probed with a local MCP server whose command only writes `<role> <pid>` to a file:
    * after the engine's `session/new`: `engine 80308`;
    * after the control plane's `GET /api/integration`: `engine 80308`, `control-plane 80547`.
  * So since P6b, the first call each auth operation makes starts a second copy of each local MCP
    server, in a process that runs for up to 10 idle minutes. `/api/mcp` on the control plane
    reports those copies, not the engine's servers.
  * **Decided: "Own record; P6d omits MCP".**
    * P6d's diagnostics carry no MCP state from the control plane, which is said where the Spec
      builds them, as for LSP and formatter.
    * The duplicate start is decided in a MADR/PLAN of its own before P7 registers the provider.
      Nothing registers it until then, so no phone has met it.
* **`session.diff` is one turn.**
  * With no `from`, it diffs "the turn of the newest user message"; `to` extends the range
    through a later turn (`packages/protocol/src/groups/session.ts:519-542`).
  * Magic's `/diff` and the phone's diff button send no message id
    (`internal/session/commands.go:661`; `chat_screen.dart:1599`). On 1.x that means the whole
    session.
  * `GET /api/session/{id}/message` takes `type`, `order` and `limit`
    (`packages/protocol/src/groups/message.ts:23-42`). A user message is `{id, type:"user", …}`
    (`packages/schema/src/session-message.ts:77-84`). Gate G5 read this route live.
  * **Decided: "Whole session".**
    * P6d reads the first and the newest user message (`type=user`, `order=asc` and `desc`,
      `limit=1`), then `session.diff` `from` the first `to` the newest.
    * A session with no user message is "No file changes".
    * A message id, when one is sent, is passed as `from` alone.
* **Unchanged from P6d as planned:** full-text search answers that it is not available, naming
  #34; LSP and formatter status are dropped by name; and every call carries the session's
  directory (`Directory: h.CWD()`).

P6d's files are `internal/provider/opencode/acp.go`, a new `acp_surfaces.go` and its test, and
`internal/provider/opencode/control/control.go` and `control_test.go`.

### P6d — 2026-11-03: workspace, diagnostics, VCS and diff

**What changed.**

* **The control plane's raw mode** (`control.go`). A request with `Raw` and a positive `MaxBytes`:
  * is sent with `Accept: */*`;
  * takes the answer in any content type;
  * reads at most `MaxBytes`, and one byte more to set `Response.Truncated`.

  The wire capture gets a line naming the route, status, size and truncation, never the bytes. A
  raw request with no bound is refused before anything starts. `Response` also carries the
  answer's content type.
* **opencode** (new `acp_surfaces.go`, wired in `acp.go` as `Spec.Ops`). Every call is in the
  session's directory.
  * **Workspace.**
    * The path is checked before any request, by the 1.x guards: an absolute path, a URI or `..`
      is refused, and so is a path through a symlink.
    * `fs.list` paths are re-checked, and one that leaves the root is dropped. Directories come
      first, at most 200 entries.
    * `fs.read` is a raw request bounded at 262,144 bytes. A longer file is `result_too_large`,
      and NUL or invalid UTF-8 is `binary_content`.
    * `fs.find` returns files only (a directory is browsed, not read), with `limit` 100. A full
      page is marked truncated.
    * Text search answers that v2 has no route, naming #34, and sends nothing.
  * **Errors.**
    * An answer outside 2xx becomes an error naming the route, status and engine tag, never the
      engine's message, which can name a host path.
    * `FileNotFoundError` is `invalid_path`.
  * **Diagnostics.**
    * The branch and default branch from `/api/vcs`.
    * An aggregate change count from `/api/vcs/status`: file names are not decoded.
    * Skills by name and description, read after `GET /api/integration` in the same location.
      When activation fails, the section is left out, unread.
    * No MCP, LSP, formatter or plugin route is read. The session's own MCP list fills the MCP
      section (P6a).
  * **Diff.**
    * With no message id: the first and the newest user message
      (`/api/session/{id}/message?type=user&order=asc|desc&limit=1`), then `session.diff` `from`
      the first `to` the newest.
    * No user message is "No file changes", and no diff is asked for.
    * A message id is `from` alone.
    * The summary is 1.x's, at most 20 files, then "…and N more", marked truncated.
  * `Ops.RefreshSkills` stays unset (0229). The session declares workspace and diff, and not
    skill refresh.

**Tests, each seen failing** (`p6d/plant_p6d.py` in the session scratchpad, 26 deliberate breaks
on scratch copies, each failing on an assertion; none failed to compile):

| Break | Fails |
| --- | --- |
| raw answer JSON-checked | `TestRawAnswerSkipsTheJSONCheck`: "answered "text/markdown", not application/json" |
| raw answer not cut | `TestRawAnswerIsBounded`: "MaxBytes 7: "# notes\n", truncated false" |
| raw request without a bound | `TestRawNeedsABound`: "a raw request with no MaxBytes was sent"; "1 launch(es); want none" |
| raw body captured | `TestRawBodyIsNotCaptured`: "a capture carries the raw file's bytes" |
| no symlink check; no path normalization | `TestACPWorkspaceRefusesPathsOutsideTheRoot`: "sent [/api/fs/list /api/fs/read/link …]"; "sent [… /api/fs/read/.. …]" |
| returned paths unchecked; no directory sent | `TestACPWorkspaceListKeepsOnlyPathsInside` (`TestACPDiagnostics` too, for the second) |
| read not raw; a cut file shown; NUL shown as text | `TestACPWorkspaceReadIsRawAndBounded` |
| the engine's message kept; a missing file not `invalid_path` | `TestACPWorkspaceErrorsCarryNoEngineText`: "500 UnknownError: failed at /Users/secret/project" for the first |
| text search sent as file search | `TestACPTextSearchNamesTheIssue`: "sent [/api/fs/find]" |
| directories kept as matches; a full page not marked | `TestACPFileSearch` |
| skills read before activation | `TestACPDiagnostics`: "want the integration list before the skill list"; `TestACPDiagnosticsWithoutActivation` |
| the control plane's MCP read; a changed file's name kept | `TestACPDiagnostics`: "read /api/mcp"; "diagnostics carry a path or content" |
| the newest turn only | `TestACPDiffIsTheWholeSession`: "diff query map[], want from msg_1 to msg_3" |
| more than 20 files not marked; a diff with no prompt asked for | `TestACPDiffBounds` |
| the message id ignored | `TestACPDiffOfOneMessage` |
| `newACP` offers no workspace; no diagnostics; no diff | `TestACPProviderOffersTheSurfaces` |

The paths shown under `/Users/secret` are the tests' own made-up engine messages.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 5 changed and new Go files | exit 0, 47 packages ok, `go1.27.3` |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (the 5 Go files) | "5 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

**Not done:**
* Nothing registers the provider until P7, so no phone has used these surfaces.
* The duplicate MCP start (Deviation 1) waits for its own MADR/PLAN, before P7.
* Text search (#34), LSP and formatter status, plugins (0228) and skill refresh (0229) are not
  served.
* No live run. The routes' shapes are this phase's probes and the source at `b44b7164ba`. The
  message list's shape is from the source and G5's live read.

P6d is complete with this commit. P6e, revert, unrevert and the `!` shell, is next.

**Files for this commit:** `internal/provider/opencode/control/control.go` and `control_test.go`;
`internal/provider/opencode/acp.go`, and `acp_surfaces.go` and `acp_surfaces_test.go` (new); the
MADR; and this plan.

### Note, 2026-11-03 — the duplicate MCP start is carried by 0230

P6d Deviation 1's MCP decision, "Own record; P6d omits MCP", is
[0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md](0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md)
and its PLAN.
* The control plane is started with a policy denying every MCP server, in a file of its own
  named by `OPENCODE_CONFIG`.
* The engine is untouched.
* `TestLiveV2ControlPlaneStartsNoMCPServers` pins it, and joins the checks run at each engine pin.

It lands before P7.

### P6e Deviation 1, 2026-11-03 — how an undone turn leaves the transcript, who makes it permanent, the command table, the shell's policy, and live turns. The owner decided: "Range by Magic's seq", "Magic, before the prompt", "Fold into P6e" and "Yes, free model"

C8: `opencode --version` printed `v2.2.3`, at `3489184b`.

P6 Deviation 1 left two things to P6e's start: how a reverted live prompt's row is removed, and
P6e's files. The evidence, from the opencode source at `b44b7164ba`, Magic's code, and one probe
on an isolated home with no prompt (`p6e/probe_shell.py` in the session scratchpad):

* **Revert is staged, then committed** (`packages/core/src/session/revert.ts`; `session/session.ts`).
  * `revert/stage {messageID, files}` moves a boundary and restores the files the assistant
    messages after it changed.
  * `DELETE …/revert` restores them and clears the boundary.
  * `revert/commit` makes it permanent.
  * A process commits a staged revert itself when it admits the next prompt
    (`session.ts:168-173`).
  * Each route's busy check is `execution.isActive` in the process that receives it. In the
    control plane that is never the engine's turn, so Magic refuses while a turn runs
    (`Host.Busy()`, D11 as amended).
* **What `/undo` and `/redo` mean.**
  * `/undo` is `UndoSession.UndoLast`, and `/redo` is `RevertSession.Unrevert`, "Restored the
    undone turn" (`internal/session/commands.go:290-292`, `:710-726`).
  * So `/undo` only stages, and the undo becomes permanent at the next prompt, as on 1.x.
  * The phone has no message-picking revert, deliberately (MADR 0042 D10;
    `mcremote_client.dart:3907-3921`).
* **Rows of an undone turn carry no common id.**
  * Today a row is removed only by its native message id, on the daemon
    (`manager.go:335`, `:618-639`) and the phone (`transcript_reducer.dart:195-196`).
  * On v2:
    * a live prompt's row has no id (P6 Deviation 1);
    * agent text carries the assistant message's id (`packages/cli/src/acp/translate.ts:301`);
    * a thought carries an id derived from it (`:310`);
    * a tool row carries none.
  * Removal by id would leave the turn's prompt, thoughts and tools on screen.
* **The shell** (probed through the control plane, on a session it created):
  * `POST …/shell {id, command}` answers 204 when the command has finished: 2.1 s for a
    two-second command.
  * `GET …/message/{id}` then holds `{type:"shell", status, exit, output{output, size, truncated}}`.
    A failing command was `exit: 1`, with its stderr in the output. Twenty thousand lines came back
    whole, 129 KB, `truncated: false`.
  * The id must start `msg_`, or the call answers 400.
* **The command table.** v2's `acpCommandTable` still names `/diff`, `/undo` and `/redo`
  `reasonControlPlane` (`internal/provider/opencode/commandtable.go`). `/diff` has been served since
  P6d, so typing it says "not available" while the button works: a P6d leftover.
* **The shell's operator policy.**
  * acpagent declares the shell whenever `Ops.Shell` is set (`sessioncaps.go`) and has no policy.
  * 1.x and httpagent honour `providers.opencode.allow_remote_shell`, off by default
    (MADR 0112 A9; `internal/config/config.go:595-602`).
  * Without the same gate, v2 would offer remote command execution unasked.

**Decided.**

* **"Range by Magic's seq".**
  * A Spec reports an undone turn's removal as a count of turns:
    `transcript_remove` with `RemoveTurns`.
  * The manager turns that into the sequence range from the n-th newest user row to the newest
    row. It drops every row in that range except notices and tombstones, and records the
    tombstone with `RemoveFromSeq` and `RemoveToSeq`.
  * The phone's transcript items keep the sequence number of the event that made them, and a
    range tombstone removes the items in it. Items stored before this have none, and are never
    matched.
  * The fields are additive. An older phone keeps the rows until it replays history.
* **"Magic, before the prompt".**
  * acpagent gains `Ops.BeforePrompt`, agent-neutral, run in `beginTurn` once the turn is
    claimed and before the user row is emitted. An error refuses the prompt and leaves the
    staged undo.
  * opencode's hook, when an undo is staged:
    * `revert/commit` through the control plane;
    * then the removal, as many turns as the undo spans;
    * then the prompt.
* **What is undone.**
  * `/undo` stages at the newest user message not already behind the boundary, so a second
    `/undo` reaches one turn further.
  * The count is the user messages from the boundary on.
  * `/undo` across a compaction is refused, naming why. A compaction is a turn Magic shows with a
    user row and the engine records without a user message, so the two counts would differ.
  * `/redo` clears the staged revert (`DELETE …/revert`).
  * `Ops.Revert` with a message id stages at that message, for the protocol's `session.revert`.
* **The shell.**
  * `POST …/shell` with a `msg_` id Magic makes, then that message read back.
  * The output is shown as a `bash` tool call and its completion, `execute` kind, through the
    manager's existing cut for the phone. The engine's output is read up to 1 MiB.
  * The shell is declared only when `acpagent.Config.AllowRemoteShell` is set, which P7 feeds from
    `providers.opencode.allow_remote_shell`. acpagent's capability gate reads it, for every Spec.
  * The command is checked and bounded as on 1.x (`validShellCommand`, `shellTimeout`, 30 minutes;
    P7 keeps both).
  * The control plane's client had one 2-minute timeout for every request, and the shell route
    answers only when the command ends. So `control.Request` gains a per-request `Timeout`, with
    2 minutes still the default. `control.go` and `control_test.go` join P6e's files.
* **"Fold into P6e".** `/diff`, `/undo` and `/redo` map to `OpDiff`, `OpUndo` and `OpRedo` in v2's
  table.
* **"Yes, free model".**
  * A live-tagged test on an isolated home, on `opencode/deepseek-v4.1-flash-free`:
    * two turns that write a file;
    * `/undo`, `/redo`, `/undo`, then a third turn that makes it permanent;
    * then a shell command.
  * It checks the file, the engine's message list and Magic's history.
  * It runs once here, on this decision.

**P6e's files:**
* `internal/event/event.go`;
* `internal/session/manager.go`;
* `internal/provider/acpagent/` (`host.go`, `config.go`, `session.go`, `sessioncaps.go`, and their
  tests);
* `internal/provider/opencode/acp.go`, `commandtable.go`, and a new `acp_revert.go` with its test and
  a live test;
* `internal/provider/opencode/control/control.go` and `control_test.go`;
* `apps/mobile/lib/data/protocol/models.dart`, `apps/mobile/lib/data/chat/chat_models.dart`,
  `transcript_reducer.dart`, and their tests;
* `docs/guides/protocol-v1.md`, where the event is documented;
* the MADR;
* this plan.

### P6e Deviation 2, 2026-11-03 — undo restored no file live: the project was reached through a symlink. The owner decided: "Diagnose with 2 more turns", then "Real path for opencode v2"

* **Found** by the live test (P6e Deviation 1, "Yes, free model"), on the free model:
  * **Run 1** (a project that was not a git repository): the model wrote `note.txt` twice, and
    `/undo` restored nothing. OpenCode records a turn's files only in a git repository
    (`packages/core/src/snapshot.ts:114`), so the test's project became one. The owner allowed one
    re-run ("Re-run once").
  * **Run 2** (a git project): still nothing restored, and the stage answered no files.
  * **The owner chose "Diagnose with 2 more turns".** A probe drove `opencode acp` directly, with
    Magic's client capabilities, in an isolated git project under the session scratchpad
    (`p6e/probe_undo_files.py`). The engine had recorded each writing step's start snapshot and
    file by 0 s. The stage restored `note.txt` (`banana` → `apple`), and the clear put it back. So
    neither timing nor client writes was the cause.
  * **The cause, with no model turn** (`p6e/probe_symlink_snapshot.py`):
    * The test's directory is a temporary directory, which on macOS is reached through `/var`, a
      symlink to `/private/var`.
    * OpenCode resolves the project's worktree to its real path and requires the location to be
      inside it (`snapshot.ts`, `scope`). A location reached through a symlink fails that check.
      `capture` then logs and returns nothing.
    * Same git project, a stage forcing a capture: by its real path, 0 failures logged; through a
      symlink, `failed to capture snapshot … Location is outside the project`, and the stage still
      answered 200.
    * So any session whose directory is reached through a symlink gets an undo that removes the
      turn and silently restores no files.
  * **Upstream:** this is anomalyco/opencode#54454, "Snapshots fail for Locations opened through
    symlinked paths", closed on 2026-11-04 after 2.2.3. No second issue was filed.
* **Decided: "Real path for opencode v2".**
  * acpagent gains an agent-neutral `Spec.RealCWD`. With it, `resolveSessionCWD` resolves the
    session's directory through symlinks (`filepath.EvalSymlinks`) before the engine,
    `session/new` and the control plane see it.
  * The warm spare's directory comes from the same function, so the two still compare alike.
  * opencode's Spec sets it. Every other Spec is unchanged.
  * `/undo`'s message says how many files were restored, or that none were recorded and why, from
    the stage's `files`.
  * `internal/provider/acpagent/acpagent.go` joins P6e's files.

### P6e — 2026-11-03: undo, redo, revert and the `!` shell

**What changed.**

* **The event** (`internal/event/event.go`). `transcript_remove` gains:
  * `RemoveTurns`, which a provider sets;
  * `RemoveFromSeq` and `RemoveToSeq`, which the manager sets and clients read.
* **The manager** (`internal/session/manager.go`, `removeTurnsLocked`). A removal of n turns:
  * deletes every history row from the n-th newest user row to the newest row, except notices and
    tombstones;
  * keeps the tombstone, carrying the range;
  * with no user row, removes nothing and carries no range.
* **acpagent, for every Spec.**
  * `Ops.BeforePrompt`, run in `beginTurn` once the turn is claimed and before the user row. An
    error refuses the prompt and frees the turn; for a drained queue entry, it shows the error and
    keeps draining.
  * `Config.AllowRemoteShell` gates the shell's declaration, its `session_capabilities` flag and
    the call itself (`shellOffered`).
  * `Spec.RealCWD` (Deviation 2).
* **The control plane:** `Request.Timeout`, with the client's fixed 2-minute timeout moved to a
  per-request deadline in `send`.
* **opencode** (new `acp_revert.go`, wired in `acp.go`).
  * **`/undo`** (`Ops.UndoLast`):
    * refused while a turn runs;
    * stages a revert, files included, at the newest user message not already undone, refusing
      one that would reach past a compaction;
    * counts the turns, and says how many files it restored.
  * **`/redo`** (`Ops.Unrevert`) clears the staged revert.
  * **`session.revert`** (`Ops.Revert`) stages at a named recent prompt, and refuses a part.
  * **`Ops.BeforePrompt`**: with an undo staged, `revert/commit`, then a `transcript_remove` of
    that many turns. A failed commit refuses the prompt and names `/redo`.
  * **`Ops.Shell`:**
    * checked and bounded as on 1.x, and refused while a turn runs;
    * `POST …/shell` with a `msg_` id and `shellTimeout`;
    * that message is read back and shown as a `bash` tool call and its completion: failed on a
      non-zero exit, with the exit code, and the output cut at 1 MiB.
    * The command is never logged.
  * `NewSessionState` holds each session's undo; `RealCWD` is set.
  * `commandtable.go`: `/diff`, `/undo` and `/redo` map to `OpDiff`, `OpUndo` and `OpRedo`, and the
    `reasonControlPlane` note is gone.
* **The phone.**
  * `SessionEvent` reads `remove_from_seq` and `remove_to_seq`.
  * `ChatItem.hostSeq` (cached when non-zero) is the sequence number of the event that made the
    item, stamped by `applySessionEvent` on every item an event creates.
  * A range tombstone removes the items in the range, except system lines, and rebuilds the tool
    index.
* **`docs/guides/protocol-v1.md`** documents the range form.

**Tests restated** (each property changed by Deviation 1's decisions, as P6a restated grok's):

* `TestSurfacesFollowTheSpecsOps`:
  * "all set" now permits the shell;
  * a new subtest pins that the hook alone is not enough.
* `TestACPCommandTableOnG1sCommands`: `/diff`, `/undo` and `/redo` resolve to their operations,
  where they resolved to "not available until the control plane".

**Tests, each seen failing** (`p6e/plant_p6e.py` and `p6e/plant_realcwd.py` in the session
scratchpad). 29 deliberate breaks on scratch copies, each failing on an assertion: 24 Go and 5
Dart. The Dart copy took `lib`, `test`, `assets`, the pubspec files and `.dart_tool` only.

| Break | Fails |
| --- | --- |
| the range never removed; notices removed too; always one turn | `TestRemoveTurnsTakesTheNewestTurns` ("two turns from seq 3, want 1" for the last) |
| the shell without the operator's permission | `TestSurfacesFollowTheSpecsOps/shell_without_the_operator's_permission` |
| a failing hook ignored; the turn kept after a refusal; the hook after the user row | `TestBeforePromptRunsAheadOfThePrompt` |
| a second undo stages the same turn; staged without the files; the restored files not counted | `TestACPUndoStagesTheNewestTurn` (and `TestUndoTextSaysWhatWasRestored` for the last) |
| past a compaction allowed; a running turn not refused | `TestACPUndoRefusals` (and the redo and shell refusals for the second) |
| redo keeps the undo | `TestACPRedoClears` |
| the undo never committed; one turn removed whatever the undo spans | `TestACPBeforePromptMakesTheUndoPermanent` |
| the shell on the default bound; a non-zero exit shown as success; a long output not cut | `TestACPShell` ("a long output: 1048586 bytes" for the last) |
| an invalid command run | `TestACPShellRefusals` |
| `/undo` still not available | `TestACPCommandTableOnG1sCommands` |
| `newACP` without the undo state | `TestACPProviderOffersUndoAndShell`: "the session has no undo state" |
| a request's Timeout ignored | `TestRequestTimeoutBoundsOneRequest`: "after 424ms; want the deadline" |
| the RealCWD flag ignored; the OpenCode Spec without it | `TestRealCWDResolvesSymlinks`; `TestACPSpecWantsTheRealPath` |
| items not stamped; the range ignored; system lines removed too; the tool index not rebuilt; the host seq not cached | the reducer's `range tombstone` group ("Expected: 'running' Actual: 'failed'" for the tool index: a removed tool's late update hit the new card) |

"The hook after the user row" failed on the refusal check, because the break also swallowed the
hook's error, so the ordering assertion was not the one that caught it. The ordering is pinned by
the same test's passing run. The first Dart run did not build: the copy lacked `assets`, which was
added. Neither was a fault in a test.

**Live.**

| Run | Turns | Result |
| --- | --- | --- |
| 1 | 2 | the model wrote `note.txt` twice; `/undo` restored nothing: the project was not a git repository (Deviation 2) |
| 2 | 2 | a git project; still nothing restored: a symlinked path (Deviation 2) |
| probe | 2 | `opencode acp` directly in a real path: files recorded by 0 s; the stage restored `note.txt` |
| 3 | 3 | with `RealCWD`, on the unchanged `/var` temporary directory: every assertion passed, as follows |
| shell | 0 | `TestLiveV2Shell`, split from run 3, passed |

Run 3 in full:
* `/undo` restored `apple` ("restored 1 file"), `/redo` brought back `banana`, and `/undo`
  restored `apple` again.
* The third prompt emitted one removal of one turn, `note.txt` stayed `apple`, and the engine
  kept two prompts.
* Its last check, the shell, failed only on the test's isolation: the test process still held
  `BASH_ENV`, which leaked the developer's shell start-up errors into the output. The shell
  ran and completed.
* The shell was split into `TestLiveV2Shell`, which sends no prompt, with `BASH_ENV` blanked. It
  passed: a `bash` call with the command, completed with `apple`.
* The restructured undo test was not re-run, to spend no further turns. Its assertions are run
  3's, unchanged.

Eleven free-model turns in all, on `opencode/deepseek-v4.1-flash-free`, each on the owner's word.

**Gates.**

| Gate | Result |
| --- | --- |
| `make preflight` (records, docs lint, gofmt, vet on every tag, staticcheck, race, release build, `dart format`, `flutter analyze`, `flutter test`) | exit 0; 0 files changed by `dart format`; no analyzer issues; 1510 phone tests passed |
| `go test -race -count=1 ./...`, after Deviation 2 | exit 0, 47 packages ok |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus the 22 changed and new files | exit 0, 47 packages ok, `go1.27.3` |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (18 Go files) | "18 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records`; `make markdownlint-docs` | exit 0 each |

The race run before Deviation 2's comment fix failed `TestNoProviderIsNamedInAcpagent`: the
`RealCWD` comment in acpagent named the vendor. The comment was reworded and the guard passes.

**Not done:**
* Nothing registers the provider until P7, so no phone has used these.
* A user prompt's row is removed by range only on a phone that knows the new fields. An older
  phone keeps the rows until it replays history.
* Undo state is the session's own. If the agent restarts with an undo staged (for example after a
  credential change), Magic forgets it. The engine then commits it at the next prompt, and the
  undone turn's rows stay on screen.
* An undo across a compaction is refused, not supported.
* `session.revert` reaches only one of the last 200 prompts.
* `SessionEvent.withText` carries neither the native ids, `replace` nor `artifact`, against its
  own comment. That predates P6e and is untouched here; P6e needed only `seq`, which it carries.

P6e is complete with this commit. P7, registering the provider, is next. 0230's MCP fix has
landed, as P7 required.

**Files for this commit:**
* `internal/event/event.go`;
* `internal/session/manager.go` and `manager_identity_test.go`;
* `internal/provider/acpagent/acpagent.go`, `config.go`, `host.go`, `session.go`,
  `sessioncaps.go`, `surfaces.go` and `surfaces_test.go`;
* `internal/provider/opencode/acp.go`, `commandtable.go`, `acp_spec_test.go`, and `acp_revert.go`,
  `acp_revert_test.go` and `live_acp_revert_test.go` (new);
* `internal/provider/opencode/control/control.go` and `control_test.go`;
* `apps/mobile/lib/data/protocol/models.dart`, `lib/data/chat/chat_models.dart`,
  `lib/data/chat/transcript_reducer.dart`, and `test/transcript_reducer_test.dart`;
* `docs/guides/protocol-v1.md`;
* the MADR;
* this plan.

### P7 Deviation 1, 2026-11-03 — the dialect is larger than P7's list, and P7 inherited five obligations. The owner decided: "acpagent Spec hook" and "Yes, free model"

C8: `opencode --version` printed `v2.2.3`, at `c5242302`.

Found before the first write:

* **What the v2 Spec still uses from the dialect.** On a scratch copy, every opencode file but the
  v2 ones (`acp*.go`, `commandtable.go`, `version.go`) was deleted and the package built
  (`p7/what_v2_needs.py`). 22 files went, and the build named 23 missing names:
  * the workspace guards and bounds from `workspace.go`: `normalizeWorkspacePath`,
    `checkNoSymlinkComponents`, `validateReturnedPath`, `sortMatches`, the four `errWorkspace*`
    errors, the `maxWorkspace*` bounds and `fileSearchLimit`;
  * the diagnostics bounds: `maxDiagnosticSkills`, `maxSkillNameLen` and `maxSkillDescriptionLen`
    from `diagnostics_surface.go`, and `maxDiagnosticTextLen` from `session_ops.go`;
  * the shell's `validShellCommand` and `shellTimeout` from `control.go`;
  * `clip` and `firstNonEmpty` from `http.go`;
  * `commandtable.go`'s 1.x table (`httpDialect`), which goes with the dialect.
* **P7's list names 10 dialect files; the dialect is 22.** It also has `artifact.go`,
  `attachment.go`, `auth.go`, `command.go`, `device_auth.go`, `diagnostics_surface.go`, `mode.go`,
  `model_surface.go`, `thinking.go`, `upstream.go`, `usage.go` and `workspace.go`. P6 Deviation 1
  moved P6's dialect files out of P6, and they were never named for deletion.
* **The 1.x tests and fixtures.**
  * Every opencode test but the v2 ones is a test of the dialect.
  * The fixtures `surface-1.18.21`, `wire/1.18.26`, `sse`, `opencode-spike-1.18.5` and the three
    `1.18.16` JSON files are the dialect's. `wire/2.2.3` is G1's and stays.
  * Outside the package, `internal/command/conformance_test.go` and `resolve_order_test.go`,
    `internal/provider/auth_conformance_test.go` and `internal/session/commands_internal_test.go`
    construct the 1.x provider.
* **Five obligations earlier phases gave P7.**
  * Keep the helpers above (P6d).
  * Rename `ACPMinVersion`, `ACPKnownGoodVersion` and `ACPUpgradeURL` (P4 Deviation 1).
  * Feed `acpagent.Config.AllowRemoteShell` from `providers.opencode.allow_remote_shell`
    (P6e Deviation 1).
  * Keep `validShellCommand` and `shellTimeout` (P6e).
  * Own the control plane's shutdown (P6b).
* **The shutdown has no path.** At exit the daemon calls `Shutdown()` on each provider
  (`internal/daemon/daemon.go:413-419`). acpagent's releases only the warm spare
  (`acpagent.go`, `Shutdown`), so the control plane would end only when its lease does.
* **The configuration** (`internal/config/config.go`):
  * `OpencodeProviderConfig` is the dialect's.
  * `providers.opencode.transport`'s refusal says OpenCode "always uses one shared
    `opencode serve` engine", which P7 makes untrue.
  * `pure`, `session_tree` and `stream_coalesce_ms` have defaults in `load.go`. Left there, a
    retired key would look set in every configuration and always fail validation.

**Decided.**

* **The helpers move** to a new v2 file, `acp_limits.go`, verbatim with their comments. Their tests
  move to `acp_limits_test.go`. Every other dialect file, test and 1.x fixture is deleted.
* **"acpagent Spec hook".** acpagent gains an agent-neutral `Spec.OnShutdown`, which `Shutdown`
  calls after releasing the spare. opencode's closes the control plane.
* **The rest follows the PLAN as approved.**
  * The renames.
  * `OpencodeProviderConfig` becomes `ACPProviderConfig` plus `allow_remote_shell`, as grok's
    `ACPProviderConfig` is.
  * ~~`pure`, `session_tree` and `stream_coalesce_ms` fail validation naming the key and this record,
    and have no default.~~ *Superseded by P7 Deviation 2: they have no default, and the load warns
    about them and ignores them.*
  * `allow_remote_share` is dropped.
  * The `transport` refusal names v2's model.
  * The daemon registers the Spec, warms it when `prewarm` is on, and the external tests construct
    it.
* **"Yes, free model".** `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo` runs
  once at P7's end.
* Stored sessions created on a 1.x engine resume only if v2 can load them. That is v2's migration,
  not Magic's. A failure surfaces as any resume failure does, and P9's acceptance records what
  happens.

P7's files are its list and the files above, `internal/provider/acpagent/acpagent.go` (and a
test), `internal/config/load.go` and its tests, `internal/daemon/daemon.go` and its tests, and
`configs/config.example.yaml`.

### P7 Deviation 2, 2026-11-03 — every installed configuration carries the retired keys. The owner decided: "Warn and ignore any value"

Found after step 2's validation was written:

* **setup-service seeds them.** It writes the embedded `internal/cli/service/defaults_mcremote.yaml`
  when no configuration exists (`ensureDefaultConfig`, `internal/cli/service/setup.go`). Its
  opencode section has `stream_coalesce_ms: 80`, `session_tree: true` and `pure: false`, and so
  does the development host's installed configuration. With the refusal, every host setup-service
  provisioned would fail to start after the upgrade until the lines were removed by hand.
* **The tests showed it.** `go test ./internal/cli/...` failed 13 CLI tests with
  "providers.opencode.pure is no longer supported". Those tests load the host's live configuration;
  that gap was already there and is not P7's.
* **The templates.** `TestTemplatesSpellEveryConfigKey`
  (`internal/cli/service/template_parity_test.go`) failed: `defaults_mcremote.yaml`,
  `config.example.yaml` and `config.prod.example.yaml` lack the shared ACP keys `args`, `fs_roots`,
  `auth_method_id` and `mcp_servers`. `configs/config.mesh-grok.yaml` also ships the retired keys.
* **The precedent.** MADR 0160 D3 met the same case for goose, and reports the block without failing
  the load (`internal/config/retired_goose.go`).

**Decided.**

* **"Warn and ignore any value".** `pure`, `session_tree` and `stream_coalesce_ms` under
  `providers.opencode` are ignored, whether in the file or as `MCREMOTE_PROVIDERS_OPENCODE_*`. The
  load logs a warning and adds a `retired_opencode_1x_keys` diagnostic naming each one and this
  record, as `noteRetiredGoose` does.
  * This supersedes step 2's and Deviation 1's "fail validation". The three `Retired*` fields go;
    `RetiredTransport` keeps its refusal.
  * A configuration that set `pure: true`, `session_tree: false` or another `stream_coalesce_ms`
    now gets v2's behaviour, and the warning is its only notice. The owner accepted that.
  * `allow_remote_share` stays dropped with no warning, as D4 says.
* **Files added to P7:** `internal/config/retired_opencode.go` and its test,
  `internal/cli/service/defaults_mcremote.yaml`, `configs/config.prod.example.yaml` and
  `configs/config.mesh-grok.yaml`. In all four shipped YAMLs the retired keys and
  `allow_remote_share` go, the shared ACP keys are spelled, and the 1.x comments are restated for
  v2.

### P7 — 2026-11-03: the 1.x dialect retired, and the configuration

**What changed.**

* **The dialect is gone** (`internal/provider/opencode`): 100 files.
  * 22 sources: `artifact`, `attachment`, `auth`, `command`, `control`, `device_auth`,
    `diagnostics_surface`, `discovery`, `http`, `lifecycle`, `mode`, `model_surface`, `parts`,
    `permission`, `question`, `resync`, `session_ops`, `thinking`, `todo`, `upstream`, `usage` and
    `workspace`.
  * 50 tests: every opencode test but the v2 ones.
  * 28 fixture files: `config-providers-1.18.16.json`, `provider-1.18.16.json`,
    `provider-auth-1.18.16.json`, `opencode-spike-1.18.5`, `sse`, `surface-1.18.21` and
    `wire/1.18.26`. `wire/2.2.3` stays.
* **What v2 kept** moved, unchanged, to `acp_limits.go`: the workspace bounds, errors and guards, the
  diagnostics bounds, `validShellCommand` and `shellTimeout`, `clip` and `firstNonEmpty`. Their
  tests moved to `acp_limits_test.go`. `textSearchCap`, which nothing used, did not.
* **`version.go`** holds only v2's `MinVersion` (`2.1.0`), `KnownGoodVersion` (`2.2.3`) and
  `UpgradeURL`, the renamed `ACP*` constants (P4 Deviation 1).
* **`commandtable.go`** holds only the v2 table.
* **Shutdown.** acpagent's `Spec.OnShutdown` runs after `Shutdown` releases the spare. opencode's
  closes the control plane.
* **The configuration** (`internal/config`).
  * `OpencodeProviderConfig` is `ACPProviderConfig` plus `allow_remote_shell`. `RetiredTransport`
    keeps its refusal, which now names v2's model.
  * `retired_opencode.go` warns about `pure`, `session_tree` and `stream_coalesce_ms`, in the file
    or the environment, and ignores them (Deviation 2).
  * `load.go` has no default for them and none for `allow_remote_share`; `auth_method_id` gains one.
  * `viper_defaults_test.go` lists `args`, `fs_roots` and `mcp_servers` as list keys, as grok's
    are.
* **The shipped YAMLs** (`defaults_mcremote.yaml`, `config.example.yaml`,
  `config.prod.example.yaml`, `config.mesh-grok.yaml`) are restated for v2 (Deviation 2). The mesh
  example's pinned model is now v2's `opencode/deepseek-v4.1-flash-free`.
* **The daemon** (`internal/daemon/daemon.go`).
  * `opencodeConfig` is `acpAgentConfig` plus `AllowRemoteShell`.
  * `InspectProviders` and `Run` register `opencode.NewACPWithLogger`, and prewarm calls
    `EnsureWarm`.
* **Outside the package**, `internal/command`'s conformance and resolve-order tests,
  `internal/provider/auth_conformance_test.go` and `internal/session/commands_internal_test.go`
  construct the v2 provider. Where a list held both, the 1.x entry was dropped.
* `make live-opencode` now runs only the v2 live tests.

**Tests restated.**

* `TestOpencodeRemotePolicyReachesTheProvider`: the shell alone, and the shared keys with it.
* `TestRemoteMutationPolicy*` (config): the shell alone; a leftover `allow_remote_share` is
  ignored.
* `TestHelpTextModesAndProviderCaveat` checked OpenCode's own reason for `/goal`, which was the 1.x
  table's. v2 has `/goal` natively, so the check is OpenCode's own reason for `/loop`.
* `TestProviderAuthInterfaceConformance`: opencode is the v2 provider, with the same interfaces.

**Tests, each seen failing** (`p7/plant_p7.py` in the session scratchpad). 10 deliberate breaks
on scratch copies, each failing on an assertion:

| Break | Fails |
| --- | --- |
| the retired keys never noted | `TestRetiredOpencodeKeyWarnsWhateverItsValue` |
| the environment not checked | `TestRetiredOpencodeEnvWarns` |
| `OnShutdown` never called | `TestShutdownRunsTheSpecHookOnce`: "ran 0 times, want 1" |
| `OnShutdown` before the provider is closed | `TestShutdownRunsTheSpecHookOnce`: "ran before the provider was marked closed" |
| opencode's hook not wired | `TestACPShutdownClosesTheControlPlane`: "closed 0 times, want 1" |
| `control.Plane.Close` returning an error | `TestControlPlaneHasTheShutdownMethod` |
| the daemon registering something else | `TestOpencodeRegistersTheV2Spec` |
| the shell policy not mapped | `TestOpencodeRemotePolicyReachesTheProvider`: "shell=true reached the provider as false" |
| the seed lacking `fs_roots` | `TestTemplatesSpellEveryConfigKey`: "defaults_mcremote.yaml missing providers.opencode.fs_roots" |
| `/loop`'s reason not OpenCode's | `TestHelpTextModesAndProviderCaveat` |

**Gates.**

| Gate | Result |
| --- | --- |
| C8, `opencode --version` | `v2.2.3` |
| `make preflight` (records, docs lint, gofmt, tidy, vet on every tag, staticcheck, race, release build, `dart format`, `flutter analyze`, `flutter test`) | exit 0; 48 packages ok; 0 files changed by `dart format`; no analyzer issues; 1510 phone tests passed |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus P7's 121 changed and deleted files and its 6 new ones | exit 0, 47 packages ok, `go1.27.3` |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| `make pre-add-check FILES=…` (21 Go files) | "21 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make check-records`; `make markdownlint-docs` | exit 0 each |
| `MCREMOTE_LIVE_KILO_MODEL=openrouter/openrouter/free make live-kilo` (C2, C2a) | exit 0; 186 passed, 3 skipped |

The first `make preflight` failed `TestHelpTextModesAndProviderCaveat`, which was then restated as
above. The first host run started before that fix; it was stopped, its test process and tree on the
Linux test host were removed and their absence checked, and the run was repeated.

**Not done:**
* No phone has run a session on the registered provider. P9's acceptance does.
* Stored sessions made on a 1.x engine resume only if v2 loads them (Deviation 1); P9 records what
  happens.
* `allow_remote_share` is dropped with no warning, as D4 says.
* `docs/guides/config.md` and the other docs still describe the 1.x keys. P8 restates them.
* The CLI tests that load the host's live configuration still do. That gap predates P7.

P7 is complete with this commit. P8, doctor, engines and docs, is next.

**Files for this commit:**
* `internal/provider/opencode/`: the 100 deleted files; `acp_limits.go`, `acp_limits_test.go`
  and `acp_shutdown_test.go` (new); `acp.go`, `acp_spec_test.go`, `commandtable.go` and
  `version.go`;
* `internal/provider/acpagent/acpagent.go` and `shutdown_test.go` (new);
* `internal/config/config.go`, `load.go`, `config_test.go`, `viper_defaults_test.go`, and
  `retired_opencode.go` and `retired_opencode_test.go` (new);
* `internal/daemon/daemon.go` and `daemon_test.go`;
* `internal/command/conformance_test.go` and `resolve_order_test.go`,
  `internal/provider/auth_conformance_test.go`, `internal/session/commands_internal_test.go`;
* `internal/cli/service/defaults_mcremote.yaml`, `configs/config.example.yaml`,
  `configs/config.prod.example.yaml` and `configs/config.mesh-grok.yaml`;
* the MADR;
* this plan.

### P8 Deviation 1, 2026-11-04 — doctor cannot read the version without starting the engine, Magic cannot collide on the service port, engines cannot see an ACP child, and three more docs describe 1.x. The owner decided: "opencode --version", "Drop it, cite G3", "Register ACP children" and "Add all three to P8"

Found at P8's start, against steps 1 to 3 (C8: `opencode --version` printed `v2.2.3`):

* **The version (step 1).**
  * `/api/info` needs a running `opencode serve --stdio`, whose password exists only in the
    daemon's child (C5).
  * `agentInfo.version` needs an `opencode acp` to answer `initialize`, which took 6.7 s and
    43.7 s cold (A6).
  * Doctor is a lookup that starts no engine
    ([0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md)
    D32; `internal/cli/doctor.go:181-183`).
  * Without a version check, doctor reports a 1.x binary as ready. A session still refuses it by
    name (D3).
* **The service port (step 1).**
  * G3's table recorded no service-registration files from `serve --stdio` or `acp`, on any host
    (P3 record).
  * 0220 met the collision only through `auth list`, `models` and `debug agents`
    ([0220-REPORT-opencode-v2-parity-assessment.md](../reports/0220-REPORT-opencode-v2-parity-assessment.md)
    §2.2). Magic runs none of them (`internal/provider/opencode`).
  * Doctor would have nothing to detect.
* **`auth.json` (step 1).** Doctor is the last reader of opencode's `auth.json`
  (`doctor.go:116-124`), and v2 keeps credentials in its database (MADR, "Credentials across
  processes"). Step 1 already removes the read.
* **engines (step 2).**
  * acpagent writes no engine registry record for the agents it starts (`spawnAgent`,
    `internal/provider/acpagent/acpagent.go`), so `mcremote engines` cannot list an `opencode acp`
    child.
  * The control plane records itself as `opencode-control` (`control/control.go:648-654`), but
    `engines` prints no provider.
* **The docs (step 3).**
  * Besides `README.md` and `docs/guides/config.md`, three living documents describe the 1.x
    dialect:
    * `docs/guides/protocol-v1.md`, in about ten 1.18.21 passages;
    * `docs/guides/chat-performance.md`, in the `stream_coalesce_ms` row;
    * `docs/architecture.md`, in two `opencode serve` lines.
  * The mobile docs carry no OpenCode capability row (`apps/mobile/README.md`,
    `apps/mobile/docs/architecture.md`), so they need no change.
  * Records keep their history.

**Decided.**

* **"opencode --version".**
  * Doctor runs `<bin> --version` through `launch.Command`, bounded at 10 s, and compares the answer
    with `MinVersion`.
  * Below it, the engine is reported too old, with `UpgradeURL`. An answer that names no version is
    reported as unknown.
  * This replaces step 1's `/api/info` and `agentInfo.version`. It amends 0201 D32's "no engine
    started" for OpenCode: the query starts no server and sends no prompt.
* **"Drop it, cite G3".** Doctor gets no service-port check.
* **"Register ACP children".**
  * acpagent records every agent it starts in the engine registry, under its Spec id. It removes the
    record when the agent exits, or when a failed start kills it.
  * A failed write ends the start, as httpagent's does.
  * `mcremote engines` prints each record's provider. An ACP agent whose daemon died is then reaped
    like any recorded engine.
  * This holds for every Spec, grok's agents too, and acpagent names no vendor.
* **"Add all three to P8".** They are restated for v2 with `README.md` and `docs/guides/config.md`.
* **Files added to P8:** `internal/provider/acpagent/acpagent.go` and its test,
  `docs/guides/protocol-v1.md`, `docs/guides/chat-performance.md` and `docs/architecture.md`.
  acpagent's change is a process change, so C7's host runs apply to it.

### P8 — 2026-11-04: doctor, engines and docs

**What changed.**

* **acpagent records its agents** (`internal/provider/acpagent/acpagent.go`, `spawnAgent`).
  * Once agentproc has started the agent, it writes an engine registry record: the Spec's id as the
    provider, and the agent's PID as PID and process group.
  * The exit watcher removes it after `Wait`. A start that fails before the watcher exists removes
    it in `killTree`.
  * A record that cannot be written kills the agent and fails the start, naming the Spec.
  * With no registry configured, as in unit tests, nothing is written.
* **`mcremote engines`** (`internal/cli/engines.go`).
  * The table gains a PROVIDER column, from each record. An ACP agent shows as `opencode` or
    `grok`, the control plane as `opencode-control`, and kilo's and codex's engines as before.
  * A record with no provider, and a Linux process found by its environment alone, shows as
    `(unrecorded)`.
  * The row building and the table are `recordRows` and `renderEngineRows`, so they can be tested.
    The help text names what each provider value means.
* **doctor** (`internal/cli/doctor.go`).
  * opencode's `auth.json` is no longer read or reported.
  * For an engine with a floor (OpenCode alone, `MinVersion` and `UpgradeURL`), doctor runs
    `<bin> --version` through `launch.Command`, bounded at 10 s, with a 1 s `WaitDelay`.
  * It reads the first SemVer word. Below the floor, the line says "too old" with the floor and the
    install link. An unreadable or versionless answer is reported as such.
  * The section's title is "agent engines (no engine is started)".
  * Run here, doctor printed `opencode: ready <path> (v2.2.3)`, and no opencode credentials row.
* **The docs** (step 3, as amended). Each restates OpenCode for v2 and D11's drops, and names the
  retired keys as ignored.
  * `README.md`:
    * the product surface and the architecture sketch;
    * the `engines` row and the provider matrix (kilo is no longer described as OpenCode's
      architecture);
    * the remote shell policy, without `allow_remote_share`;
    * the YAML surface's opencode keys, and stream coalescing (fixed at 80 ms for OpenCode);
    * the OpenCode section: the process model, what v2 does not offer, the model pin and
      `prewarm`'s cold start;
    * thinking (OpenCode's effort option), and the `engines` section.
  * `docs/guides/config.md`:
    * the opencode defaults rows: `args`, `fs_roots`, `auth_method_id` and `mcp_servers` added; the
      retired keys and `allow_remote_share` named as ignored;
    * the env map: the 1.x variables gone, `AUTH_METHOD_ID` added;
    * the remote shell control, without share;
    * what remains engine-owned (no LSP or formatter, and no skill refresh yet).
  * `docs/guides/protocol-v1.md`:
    * the catalog's engines; the 1.18.21 variant note, in the past tense; the queue paragraph's
      paths;
    * the reference tool-status mapping, now kilo's, since `opencode/http.go` is gone;
    * the shell's output, read back through the control plane (G5);
    * sharing, offered by no provider today;
    * diagnostics on v2, and `diagnostics_changed`, which only kilo emits;
    * skill refresh, not yet on v2;
    * text search, unavailable on v2 (#34); the viewer and VCS notes;
    * artifacts, which no provider emits today.
  * `docs/guides/chat-performance.md`: the coalescing row.
  * `docs/architecture.md`: the engine line and the providers paragraph.

**Tests, each seen failing** (`p8/plant_p8.py` in the session scratchpad). 10 deliberate breaks
on scratch copies, each failing on an assertion:

| Break | Fails |
| --- | --- |
| the record kept after the agent exits | `TestSpawnedAgentIsRecordedUntilItExits`: "the record outlived the agent" |
| the record not under the Spec's id | `TestSpawnedAgentIsRecordedUntilItExits`: recorded as provider `acp`, want `testagent` |
| the record kept after a failed start | `TestFailedStartDropsTheRecord`: "a failed start left its record" |
| the agent not killed when its record cannot be written | `TestUnwritableRegistryEndsTheStart`: "the agent was left running" |
| engines printing no provider | `TestEnginesNamesEachProvider`: `acp-agent` listed as provider `-` |
| no fallback for a record without one | `TestEnginesNamesEachProvider`: `old-record` listed with an empty provider |
| doctor not asking OpenCode's version | `TestDoctorChecksTheOpenCodeVersion`: opencode's line lacks `(v2.2.3)` |
| the floor compared the wrong way | `TestDoctorChecksTheOpenCodeVersion`: opencode's line lacks `ready` |
| the first word taken as the version | `TestVersionInReadsWhatTheBinaryPrints`: `opencode 2.1.0` read as `opencode` |
| doctor reading opencode's `auth.json` again | `TestDoctorReadsNoOpenCodeAuthFile`: "doctor reported OpenCode's leftover auth.json" |

`TestDoctorReportsEachEnginesBinaryAndReadiness` passes no floors, and is otherwise unchanged.

**Gates.**

| Gate | Result |
| --- | --- |
| C8, `opencode --version` | `v2.2.3` |
| `make pre-add-check FILES=…` (7 Go files) | "7 file(s) clean (gofmt, golint, govulncheck)" |
| `make markdownlint-docs check-records` | exit 0; "0 issues in 0 files" over 57 files |
| `make preflight` (records, docs lint, gofmt, tidy, vet on every tag, staticcheck, race, release build, `dart format`, `flutter analyze`, `flutter test`) | exit 0; 47 packages ok; 0 of 230 files changed by `dart format`; no analyzer issues; 1510 phone tests passed |
| the Linux test host, `go test -race -count=1 ./...` (C7), HEAD plus P8's files | exit 0, 47 packages ok, `go1.27.3` |
| the Windows test host, `make ci-windows` (C7), the same tree | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok |
| the disclosure guard's redact preview; an identifier scan of P8's added lines | "would edit 0 file(s)"; 0 hits |

No live run: P8 needed none.

**Not done:**
* **No live engine.** No phone has seen the new `engines` rows. `make live-opencode` is P9's.
* **grok's agents are now recorded too.** That is the agent-neutral half of the owner's choice. The
  unit tests cover it, and no grok live run was made.
* **0201 is not edited.**
  [0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md)
  keeps D32 as written. The exception for OpenCode's version is recorded in the MADR's P8 section.
* **Unchanged:**
  * the records, which keep their history;
  * the mobile docs, which carry no OpenCode capability row;
  * `docs/README.md`, since P8 adds no record.
* **The CLI tests that load the host's live configuration still do.** That gap predates P7.

P8 is complete with this commit. P9, acceptance, is next.

**Files for this commit:**
* `internal/provider/acpagent/acpagent.go` and `registry_test.go` (new);
* `internal/cli/doctor.go`, `doctor_test.go`, `doctor_engines_test.go`, `engines.go` and
  `engines_test.go`;
* `README.md`, `docs/guides/config.md`, `docs/guides/protocol-v1.md`,
  `docs/guides/chat-performance.md` and `docs/architecture.md`;
* the MADR;
* this plan.

### P9 Deviation 1, 2026-11-04 — the free limit on Linux, an answer that differs there, the Windows host's cgo setting and a held database, missing pins, and drops never named. The owner decided: "Use a paid model with your key", "Stream it over ssh stdin", "Let me list the catalog first", "Probe Linux first", "Fix the test, unset CGO for the run", "Name them in P9" and "Add the pins in P9"

Found in step 1's runs, on HEAD `11728543` (C8: `v2.2.3` on all three hosts), and in step 2's audit.
The owner's word for the live runs: "Yes, all three hosts", and "Run it too" for the control plane's
MCP pin (`control/live_mcp_test.go`), which `make live-opencode` does not cover.

* **macOS: every run passed.** `make test` and `make race` (47 packages each); `make
  live-opencode`, all 9 live tests; the MCP pin.
* **The Linux test host.**
  * `make test`, `make race` (47 packages each) and the MCP pin passed.
  * **The free limit.** `TestLiveV2LoadReplaysPromptsAndNoUsage` and
    `TestLiveV2RevertAndShellFromControlPlane` were answered "Free usage limit reached. Connect a
    provider or subscribe to OpenCode Go to keep going, or switch models." `TestLiveV2UndoAndRedo`'s
    first turn wrote nothing within 3 s, most likely the same limit.
  * **A different answer.** `TestLiveV2IntegrationsWaitForActivation` (`live_acp_auth_test.go:60`):
    the first `GET /api/integration/openrouter` in a new location answered `200`, where macOS and
    Windows answered `404`. The product's list-first order works either way.
* **The Windows test host.**
  * **`make test` and `make race`** failed `TestReleaseBuildReportsTheTag` and
    `TestLocalBuildReportsLocal`: "refusing to build a release binary with CGO_ENABLED=1".
    * The host's environment sets `CGO_ENABLED=1`, and the Makefile's `CGO_ENABLED ?= 0` yields to it
      (`Makefile:40`, `:268-274`).
    * `make ci-windows` sets `CGO_ENABLED=0` itself (`scripts/ci-windows-local.ps1:112`), which is
      why it passed at every phase. The failure predates 0214.
  * **Two live tests failed their cleanup.** `TestLiveV2UndoAndRedo` and `TestLiveV2Shell` passed
    every assertion, then failed: "TempDir RemoveAll cleanup: … opencode.db: The process cannot
    access the file because it is being used by another process."
    * `startLiveV2Session` (`live_acp_revert_test.go:79-86`) closes the session but never shuts the
      provider down, so its control plane keeps the database open.
    * Windows cannot delete an open file. `control.Plane.Close` waits for the child to exit
      (`control/control.go:451-478`, `:767-776`).
  * The other live tests and the MCP pin passed.
* **The audit (step 2).**
  * **Pins the Confirmation asks for and nothing asserts:** the help surface, and `initialize`'s
    capabilities. `initialize`'s version is not asserted either: `startV2Acp` only calls it, and
    `newV2Home` checks `opencode --version` (P3's text is annotated). `session/new`'s options are
    read by G4's pin.
  * **Drops no 0214 record names:**
    * **rename:** the Spec sets no `Ops.Rename`, so the phone is told "session … does not support
      rename" (`internal/session/manager.go:2919-2922`). 0220's matrix lists it as the control
      plane's (`PATCH /api/session/{id}`);
    * **artifacts:** the v2 provider emits no `artifact` event; 1.x's `artifact.go` went at P7, and
      0220's matrix has no row for it;
    * **fork:** only the command table names it, "Magic does not fork OpenCode v2 sessions yet".
* **A credential for Linux.** That host has no OpenCode key; this host's shell has one. The pins
  use the free model alone (`v2FreeModel`), and `newV2Home` drops every `*_API_KEY` (C4).

**Decided.**

* **"Use a paid model with your key"**, **"Stream it over ssh stdin"** and **"Let me list the
  catalog first".**
  * The pins take their model from `MCREMOTE_LIVE_OPENCODE_MODEL`, the free model by default, as
    kilo's take `MCREMOTE_LIVE_KILO_MODEL` (C2a).
  * `MCREMOTE_LIVE_OPENCODE_CREDENTIAL` names the one credential variable a run keeps, as C4 allows
    ("a run that needs a credential sets it alone"). Every other credential variable is still
    removed.
  * The key goes from this host's environment to the Linux run over ssh's stdin, read into the
    remote test process's environment. It is never in argv, a file or a log, and never read into
    the session.
  * The model is the owner's pick from a no-prompt catalog listing in an isolated home with the key.
  * Linux's three prompting tests are rerun on it.
* **"Probe Linux first".** A no-prompt probe on the Linux host finds why the first get answers
  `200`, before the pin is touched.
* **"Fix the test, unset CGO for the run".**
  * `startLiveV2Session` shuts the provider down at cleanup, after the session closes. The two tests
    are rerun on Windows.
  * Windows' `make test` and `make race` are rerun with `CGO_ENABLED` removed from that run's
    environment, as the other two hosts have it. `make race` still gets cgo, the toolchain default
    there with gcc on the path.
* **"Name them in P9".** rename, artifacts and fork join D11's named drops (the MADR, added at P9),
  with their reasons. Restoring any of them takes a record of its own.
* **"Add the pins in P9".** No prompt and no quota:
  * `opencode --help` lists `acp` and `serve`;
  * `initialize` names version 2.x in `agentInfo`;
  * it reports D1's capabilities: `loadSession`, image prompts and one auth method.

  Each is seen failing on a scratch copy, then run on all three hosts.
* **Files added to P9:** `internal/provider/opencode/live_v2_gates_test.go` and
  `live_acp_revert_test.go`, and `live_acp_auth_test.go` if the probe says so;
  [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
  and
  [0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
  (step 3).

### P9 Deviation 2, 2026-11-04 — ~~the Linux test host's npm cache reaches the isolated home~~ the first integration get races plugin activation. The owner decided: ~~"Isolate the npm cache"~~ "Pin the fact, not the race" and "Revert it" (corrected below)

The probe ("Probe Linux first", Deviation 1) sent no prompt. On the Linux test host,
`TestLiveV2IntegrationsWaitForActivation` ran twice each way, on HEAD plus the P9 pins so far:

| The run's environment | The first get in a new location | The pin |
| --- | --- | --- |
| as the host has it, `npm_config_cache` set | `200`, twice | failed, twice |
| `npm_config_cache` removed (`env -u`) | `404`, twice | passed, twice |

* ~~The host sets `npm_config_cache`, and C4's recipe did not override it. The engine therefore
  shared an npm cache with the real home, so its plugins were installed and active at once.~~
* ~~macOS and Windows set no such variable, so plugins install into the empty home first, and the
  first get waits for them.~~ *Both struck: later runs disproved them (correction below).*
* The product is unaffected: acp_auth.go lists first either way.

~~**Decided: "Isolate the npm cache".** `newV2Home` points `npm_config_cache` (any case) into the
isolated home, and C4 is amended to name it. The pin stays as written. The as-is run above is its
failure; the same run after the change is its pass.~~ *Superseded by the correction below.*

### P9 Deviation 2, corrected, 2026-11-04 — the cold get is a race, not the npm cache. The owner decided: "Pin the fact, not the race" and "Revert it"

With the change made, the same pin on the Linux test host still answered `200`. Two more runs, no
prompt:

| The run's environment | The first get |
| --- | --- |
| the harness pointing `npm_config_cache` into the isolated home | `200` |
| `npm_config_cache` removed, then the harness pointing it there | `200`, then `404` |

* One command in one environment answered both. The first get races plugin activation in the new
  location; the variable is not the cause, and the two `404`s before were the race's other outcome.
* P6b's fact stands as recorded: the get answers `404` *until* plugins have activated there. A
  `200` means activation won. The pin assumed its first request always loses.
* The product is unaffected: acp_auth.go lists first, whatever the first get would answer.

**Decided.**

* **"Pin the fact, not the race".** The cold get may answer `404` `IntegrationNotFoundError` or
  `200` with the integration; anything else fails. The rest of the pin is unchanged. This narrows
  one assertion to what OpenCode guarantees.
* **"Revert it".** The `npm_config_cache` change and its C4 amendment are withdrawn.
* **Found in the same Windows run, and fixed under "Fix the test, unset CGO for the run":**
  `startLiveV2Session` set every inherited variable with `t.Setenv`. Run from Git Bash rather than
  through `make`, Windows passes the per-drive working directories as variables named `=C:` and the
  like, which cannot be set ("setenv: The parameter is incorrect"). Those, with no name, are
  skipped.

### P9 — 2026-11-04: acceptance

**What changed** (tests only, and three records).

* `internal/provider/opencode/live_v2_gates_test.go`:
  * `liveV2Model` reads `MCREMOTE_LIVE_OPENCODE_MODEL`, the free model by default. The two
    prompting pins in this file use it.
  * `strippedCredential` keeps the one variable `MCREMOTE_LIVE_OPENCODE_CREDENTIAL` names; every
    other credential-shaped variable and `BASH_ENV` is still removed (C4).
  * `startV2Acp` keeps `initialize`'s answer.
  * New pins, no prompt: `TestLiveV2HelpListsAcpAndServe` and
    `TestLiveV2InitializeNamesV2AndD1Capabilities` (Deviation 1).
* `internal/provider/opencode/live_acp_revert_test.go`:
  * `startLiveV2Session` shuts the provider down at cleanup, after the session closes, and skips
    the variables with no name that Windows sets (`=C:`).
  * It uses `liveV2Model` and `strippedCredential`.
* `internal/provider/opencode/live_acp_auth_test.go`: the cold get may answer `404`
  `IntegrationNotFoundError` or `200` with the integration (Deviation 2, corrected).
* Step 3: dated notes in
  [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
  F11 and
  [0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md](0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md)
  ("Note, 2026-11-04: D4's pin moved under 0214"). No committed line was reworded.

**Tests, each seen failing.**

| Break or evidence | Fails |
| --- | --- |
| `--version` asked instead of `--help` (`p9/plant_p9.py`) | `TestLiveV2HelpListsAcpAndServe`: lists no `acp`, no `serve` |
| the version floor at `3.` | `TestLiveV2InitializeNamesV2AndD1Capabilities`: `agentInfo.version` 2.2.3, want 2.x |
| `loadSession` read from another field | the same: loadSession false |
| another auth method id | the same: auth methods `opencode-login`, want it alone |
| both accepted cold-get answers made wrong (`p9/plant_p9b.py`) | `TestLiveV2IntegrationsWaitForActivation`: answered 404 `IntegrationNotFoundError` |
| HEAD's harness on the Windows test host (Deviation 1) | `TestLiveV2UndoAndRedo`, `TestLiveV2Shell`: `opencode.db` in use at cleanup |
| the first harness fix, run from Git Bash on Windows (Deviation 2, corrected) | the same two: "setenv: The parameter is incorrect" |

**Gates.** C8 printed `v2.2.3` on all three hosts. HEAD is `11728543`; "the final tree" is HEAD plus
the three test files above.

| Host | Run | Result |
| --- | --- | --- |
| macOS | `make test`; `make race` (HEAD) | exit 0; 47 packages ok each |
| macOS | `make live-opencode` (HEAD), and the control plane's MCP pin | exit 0; all 9 live tests passed; the MCP pin passed |
| macOS | every live test but the prompting three (the final tree) | 8 passed, the two new pins included |
| the Linux test host | `make test`; `make race` (HEAD) | exit 0; 47 packages ok each |
| the Linux test host | `make live-opencode` (HEAD); the MCP pin | 4 failed (Deviation 1); the MCP pin passed |
| the Linux test host | every live test but the prompting three (the final tree), and the cold get three more times | exit 0; 8 passed; 3 passed |
| the Linux test host | the prompting three on `opencode/deepseek-v4.1-flash`, the key streamed over ssh's stdin | exit 0; `TestLiveV2UndoAndRedo`, `TestLiveV2LoadReplaysPromptsAndNoUsage` and `TestLiveV2RevertAndShellFromControlPlane` passed |
| the Windows test host | `make test`; `make race` (HEAD) | failed: the host's `CGO_ENABLED=1` (Deviation 1) |
| the Windows test host | `env -u CGO_ENABLED make test`; `env -u CGO_ENABLED make race` | exit 0; 46 packages ok each |
| the Windows test host | `make test`; `make race`, after the host's `CGO_ENABLED=1` was removed | exit 0; 46 packages ok each; the run's environment had no `CGO_ENABLED` |
| the Windows test host | `make live-opencode` (HEAD); the MCP pin | 7 of 9 live tests passed, the prompting load and revert pins among them; undo and shell failed their cleanup; the MCP pin passed |
| the Windows test host | every live test but the load and revert pins (the final tree) | exit 0; 9 passed, undo and shell among them |
| this host | `make pre-add-check FILES=…` (3 files); `make lint`; `make check-records`; `make markdownlint-docs` | exit 0 each; "3 file(s) clean" |

Quota: free-model turns on macOS (6) and the Windows test host (9). On the Linux test host the free
model was refused, and 6 paid turns ran on the owner's key.

**The MADR's Confirmation, item by item.**

* **`live_opencode` tests for the no-prompt probe facts.**
  * The help surface: `TestLiveV2HelpListsAcpAndServe` (new).
  * The ready line, `401` without auth and JSON `/api/info`: `TestLiveV2ServeStdioLease`.
  * The location parameter: the same test's `POST /api/session` with `location.directory`, and
    `TestLiveV2ResumeReplaysNoHistory`'s `-32602` for another `cwd`.
  * The `initialize` capabilities: `TestLiveV2InitializeNamesV2AndD1Capabilities` (new).
  * The `session/new` options: `TestLiveV2RemovedCredentialStaysInRunningEngine` reads the model
    option.
* **G1 to G6.**
  * G1 through unit tests on its capture (`TestG1ChildUpdatesReachTheHandler`,
    `TestRetryMarkerReachesItsHandler`, `TestACPCommandTableOnG1sCommands`).
  * G2: `TestLiveV2ResumeReplaysNoHistory`, `TestLiveV2LoadReplaysPromptsAndNoUsage`.
  * G3: `TestLiveV2ServeStdioLease`, `TestLiveV2AcpLease`.
  * G4: `TestLiveV2RemovedCredentialStaysInRunningEngine`.
  * G5: `TestLiveV2RevertAndShellFromControlPlane`.
  * **G6 is not pinned:** it needs the owner's keys (P3).
* **Each seen failing first:** the P3 record for the earlier pins, and the table above for P9's.
* **Unit tests on recorded frames.**
  * Child updates: `TestACPChildUpdatesFillTheSubagentPanel`.
  * Retry `_meta`: `TestACPRetryShortIsANotice`, `TestACPRetryLongEndsTheTurnWithTheLimitCard`.
  * Compaction `_meta`: not sent under D1's capabilities and not handled (amendment C). The standard
    `compaction_update` is `TestACompactionArrivesOverTheWire`.
  * Permission choice by kind: `TestACPAutoAllowsOnceByKind`, `TestAutoApprovalPicksByKindOnly`.
  * Catalog grouping: `TestACPModelsFromOptionGroupsByVendor`, `TestACPModelProvidersGroupsConnected`.
* **The content-type tests against an HTML `200`:** `TestStartServerRefusesAnHTMLHealthAnswer` and
  `TestEventStreamRefusesAnHTMLAnswer`, on kilo's dialect paths. OpenCode's HTTP dialect is gone
  (P7), and its control plane refuses an answer that is not JSON (P6d).
* **`make test`, `make race` and `make live-opencode` on three hosts:** the gates above, with the
  splits and the paid model that Deviation 1 records.
* **Every dropped surface, with its reason (D11):**

| Surface | Why it is not offered on v2 | Where decided |
| --- | --- | --- |
| share links, and `allow_remote_share` | v2 has no share API | D11; D4 |
| the todo panel | experimental in v2, with no read API | D11 |
| LSP and formatter status | no v2 route | D10 |
| workspace text search | no v2 route ([#34](https://github.com/maccavelli/magic-cli-remote/issues/34)) | the owner's answers; P6d |
| plugins | a record of its own, `0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md` (proposed) | P6d Deviation 1 |
| skill refresh | a record of its own, `0229-MADR-make-skill-discovery-freshness-and-refresh-agent-neutral.md` (proposed) | P6d Deviation 1 |
| MCP state from the control plane | the control plane starts no MCP server; the session's own list is shown | P6d Deviation 1; [0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md](0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md) |
| `/status`, `/usage` | OpenCode exposes neither | P4's command table |
| rename | no ACP method, and the control plane's route is not called | P9 Deviation 1 |
| artifacts | the v2 provider emits none | P9 Deviation 1 |
| fork | `session/fork` is not called | P9 Deviation 1 |

  Revert, undo, unrevert and the `!` shell are not drops: G5 lifted their deferral (amendment B),
  and P6e built them.

**Not done:**
* G6 is not pinned (P3).
* **Runs not repeated on the final tree:**
  * macOS's undo pin, after the Shutdown cleanup was added; it passed at HEAD.
  * The Windows test host's load and revert pins, which `startLiveV2Session` does not reach; they
    passed at HEAD.
  * The Linux test host's prompting three ran on the tree that still had the withdrawn
    `npm_config_cache` change. That change did not reach the prompting paths.
* **The Windows test host's `CGO_ENABLED=1` is removed** (owner: "remove it and re-run them").
  * It was a persistent User environment variable, left from enabling `-race` there (MADR 0160
    records it). `-race` does not need it: Go turns cgo on itself where gcc is on the path.
  * Plain `make test` and `make race` then passed (the gates above). The repository was not changed
    for it.
  * Go's own env file on that host (`go env -w`) also held `CGO_ENABLED=1`; `go env -u` removed it
    (owner: "fix this and disable it"). Go still turns cgo on there by itself, gcc being on the
    path, so `make race` gets it. The owner chose "Leave it unset" over `go env -w
    CGO_ENABLED=0`, which would stop `make race` passing on that host.
* **`make live-opencode`** still omits the control plane's package, and its Makefile comment still
  says "Live OpenCode HTTP suite". Neither is P9's to change.
* rename, artifacts and fork stay dropped until records of their own.

P9 is complete with this commit, and so is this plan. 0208-PLAN P5 to P10 no longer wait on it.

**Files for this commit:**
* `internal/provider/opencode/live_v2_gates_test.go`, `live_acp_revert_test.go` and
  `live_acp_auth_test.go`;
* `docs/decisions/0217-MADR-update-every-pin-to-its-current-release.md` and
  `0226-MADR-re-pin-the-agent-clis-to-the-installed-releases.md`;
* the MADR;
* this plan.
