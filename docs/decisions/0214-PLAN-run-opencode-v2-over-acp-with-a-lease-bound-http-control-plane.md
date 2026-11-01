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
  distinct service port or no managed service (A4).
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
  C6 into `internal/provider/opencode/testdata/acp/2.2.3/`.
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

### P4 — The OpenCode ACP Spec (D1, D3, D5, D6, D9; 0220 §5 C, G)

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
4. D3: `MinVersion = "2.1.0"`, `KnownGoodVersion = "2.2.3"`, read from
   `initialize.agentInfo.version`; a 1.x engine is refused with the version found and
   `https://opencode.ai/v2/install`. A pin test reads G1's capture.
5. The command table: `goal` added, 1.x-only rows removed, KindNone reasons for D11's drops.
6. Unit tests on G1's recorded frames for each of the above, each seen failing first.

### P5 — The control plane (D2; 0220 §5 D)

Files: `internal/provider/opencode/control/` (new), and tests.

1. Launch `opencode serve --stdio --port 0` with a per-launch password in `OPENCODE_PASSWORD`;
   ready on one JSON stdout line, then `GET /api/info` answering JSON with a version; stdout drained
   after readiness.
2. Lease on stdin; shutdown closes stdin, then SIGTERM, then kill after a bounded wait; exits 0 and
   130 are orderly.
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
   next idle point after a removal unless G4 retired that; `auth.json` fingerprinting replaced by
   the integration's connections.
3. D10: workspace on `fs.list/read/find`; full-text search as the owner decides (below);
   diagnostics and VCS on `mcp`, `skill`, `plugin`, `vcs`, `vcs/status` and `session.diff`;
   LSP and formatter dropped by name.

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

* The PLAN: "Approve, ask per gate". P1 and P2 start; each of G1 to G6, and every other named live
  run, waits for the owner's word at its turn.
* `stream_coalesce_ms`: "Retire it" (P7 step 2).
* Full-text search: dropped by name in P6, with issue
  [#34](https://github.com/maccavelli/magic-cli-remote/issues/34) opened for the research and a
  MADR/PLAN pair of its own.
* Kilo: "i am out of kilo quota, so it needs to use a free model". Added as C2a.

## Execution record

Not started.
