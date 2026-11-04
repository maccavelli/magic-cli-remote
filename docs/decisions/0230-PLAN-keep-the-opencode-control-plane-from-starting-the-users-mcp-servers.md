---
status: complete
date: 2026-11-03
associated-madr: "0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement: keep the OpenCode control plane from starting the user's MCP servers

Associated MADR: [0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md](0230-MADR-keep-the-opencode-control-plane-from-starting-the-users-mcp-servers.md)

## Goal

The control plane (`internal/provider/opencode/control`) starts no MCP server the user
configured, whatever added it. The engine's servers are unchanged, and everything the control
plane serves still answers. This lands before 0214's P7 registers the provider.

## Scope

* **In:**
  * `internal/provider/opencode/control/control.go`: the policy file, and the control plane's
    environment;
  * `control_test.go`: unit tests, and the fake engine's launch log extended;
  * a new live-tagged `live_mcp_test.go` in the same package;
  * this pair;
  * a dated note in `0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md`.
* **Out:**
  * the engine (`opencode acp`), whose environment is not touched;
  * MCP status for v2 diagnostics, still omitted (0214 D10 as amended);
  * user plugins' `setup` running in the control plane (MADR D5);
  * `acpagent`, httpagent, and every other provider.

## Constraints

* C1. Ask before spending provider quota. Nothing here sends a prompt.
* C2. Process and environment changes pass on macOS, the Linux test host and the Windows test host
  (0214 PLAN C7).
* C3. `opencode --version` prints `v2.2.3` at the phase's start (0214 PLAN C8).
* C4. Every new test is seen failing on a scratch copy, never the working tree.
* C5. The policy file holds only MADR D1's statement, and no path, name or secret. Nothing writes a
  user's config.

## Implementation Steps

### P0 — Before the phase (read only)

1. C3, and `git status` clean apart from untracked drafts.
2. Re-read `control.go` `start` and `stop`, and confirm that `cmd.Env` is still
   `os.Environ()` followed by `cfg.Env`. Go's `os/exec` keeps the last value of a duplicated key.

### P1 — The policy, and its tests

1. **The file.**
   * On the first start, `Plane` creates a private directory (`os.MkdirTemp`, mode 0700) and
     writes `opencode-control-plane.json` in it (mode 0600) with exactly
     `{"policies":[{"action":"mcp.use","resource":"*","effect":"deny"}]}`.
   * Later starts rewrite the same file. `Close` removes the directory.
   * A directory left behind by a crash holds only that statement, so it leaks nothing.
2. **The environment** is built by a pure function. It takes the environment the child would
   otherwise get and the file's path, and returns the environment plus an optional warning:
   * neither `OPENCODE_CONFIG` nor `OPENCODE_CONFIG_CONTENT` set: add `OPENCODE_CONFIG=<file>`;
   * `OPENCODE_CONFIG` set: keep it, and set `OPENCODE_CONFIG_CONTENT` to the policy document;
   * `OPENCODE_CONFIG_CONTENT` also set, as a JSON object: decode it, append the statement to its
     `policies` (creating the key when absent), and re-encode. Every other key is kept;
   * `OPENCODE_CONFIG_CONTENT` set to anything else: leave both alone, and return a warning that
     names this MADR and says the user's MCP servers will start in the control plane too.

   `start` logs the warning once, at `Warn`.
3. **Tests** (`control_test.go`):
   * The fake serve's launch log records `OPENCODE_CONFIG` and `OPENCODE_CONFIG_CONTENT`, and the
     named file's content.
   * `TestControlPlaneDeniesMCP`: by default, `OPENCODE_CONFIG` names a file whose content is
     exactly the policy, and the content variable is unset.
   * `TestControlPlaneKeepsTheOperatorsConfig`: with an operator `OPENCODE_CONFIG`, that file is
     still named and the policy is in the content. With an operator JSON object in the content, its
     keys are kept and `policies` ends with the statement.
   * `TestControlPlaneWarnsWhenItCannotDeny`: with content that is not a JSON object, the
     content is passed through unchanged and the warning is logged.
   * `TestPolicyFileIsRemovedOnClose`.
   * A source test: no production file under `internal/provider` outside `control/` names
     `OPENCODE_CONFIG`. The engine's environment is never given the policy.
4. **The live pin** (`live_mcp_test.go`, `//go:build live_opencode`):
   * An isolated home, built as `live_v2_gates_test.go`'s `newV2Home` builds one, including the
     Windows profile variables.
   * The global `opencode.json` declares a local MCP server, and a global plugin (`plugins/*.js`)
     adds a second.
   * Both commands are this test binary, re-run through a `FAKE_MCP_MARKER` branch in
     `TestMain` that appends a tag and exits.
   * **The control:** a plain `opencode serve --stdio` with the same home, given one
     `GET /api/integration`, must write both markers. Otherwise the test fails as proving
     nothing.
   * **The case:** `control.New` with that home in `Config.Env` and the real binary. It runs
     `GET /api/integration` (no directory, as P6b) and `GET /api/integration`, `/api/skill` and
     `/api/plugin` in a project directory (as P6d). Each must answer 200, and no marker may appear
     within 5 s of the last call.
   * It sends no prompt.
5. **The deliberate breaks**, each on a scratch copy and each failing on an assertion:
   * the policy not added;
   * the file holding another statement;
   * the operator's `OPENCODE_CONFIG` replaced;
   * an operator content object's other keys dropped;
   * the statement prepended rather than appended;
   * the warning not logged;
   * the file left after `Close`;
   * the live case with the policy removed: it must fail on the markers.
6. A dated note in the 0214 PLAN: P6d Deviation 1's MCP decision is carried by this pair.

### P2 — Gates, hosts, staging

1. `go test -race -count=1 ./...`; `make pre-add-check FILES=…`; `make lint`;
   `make staticcheck`; `make vet-tags`; `make check-records`.
2. C2:
   * the race suite on the Linux test host, and `make ci-windows` on the Windows test host, on
     HEAD plus the changed files;
   * the live pin alone on all three hosts:
     `go test -tags live_opencode -count=1 -run TestLiveV2ControlPlaneStartsNoMCPServers ./internal/provider/opencode/control/`.

     It sends no prompt, but it still runs only on the owner's word, as every live run does.
3. The identifier scan and `github-disclosure.py redact` before staging. Stage the Go files, this
   pair and the 0214 PLAN. The owner commits.

## Verification

### Acceptance criteria (mapped to the MADR's Confirmation)

| Criterion | Evidence |
| --- | --- |
| The control plane is started with D1's policy, by `OPENCODE_CONFIG` by default | `TestControlPlaneDeniesMCP` |
| An operator's config is kept, and the policy rides in the content | `TestControlPlaneKeepsTheOperatorsConfig` |
| An unextendable content is passed through, with a warning | `TestControlPlaneWarnsWhenItCannotDeny` |
| The engine's environment never carries the policy | the source test |
| No user MCP server starts in the control plane, on 2.2.3, on three hosts | `TestLiveV2ControlPlaneStartsNoMCPServers`, with its control half passing |
| Every new test was seen failing | P1 step 5's record |

## Rollout and Rollback

* **Rollout.** Nothing registers the v2 provider until 0214's P7, so this reaches no phone before
  then. At P7, the live pin joins the checks run at each engine pin.
* **Rollback.** Revert the commit. The control plane then starts the user's MCP servers again,
  which is 2.2.3's behaviour today.
* **If a later OpenCode changes policy evaluation or the load order,** the live pin fails at the
  next pin. That calls for an amendment of this pair, not a quiet change.

## Execution record

### P0–P2 — 2026-11-03 (approved by the owner: "proceed with accepting 0230 madr, approved to execute the plan")

**P0.**
* C3: `opencode --version` printed `v2.2.3`, at `d52c99a6`.
* The tree held only the untracked 0228, 0229 and 0230 drafts.
* `start` still builds `cmd.Env` as `os.Environ()` then `cfg.Env`.

**What changed** (`internal/provider/opencode/control/control.go`):

* **`writePolicy`.**
  * On the first start, a private directory (`os.MkdirTemp`, `mcremote-opencode-control-*`).
  * In it, `opencode-control-plane.json` (0600), holding exactly MADR D1's statement. It is
    rewritten at every start.
  * `Close` removes the directory.
* **`denyMCP`, a pure function** over the environment the child would otherwise get:
  * no `OPENCODE_CONFIG` (unset or empty): it is set to the file;
  * an operator `OPENCODE_CONFIG`: kept, with the policy in `OPENCODE_CONFIG_CONTENT`;
  * an operator JSON object there: decoded with `UseNumber`, the statement appended to
    `policies`, every other key kept;
  * anything else there: left alone, and `start` logs a warning naming MADR D2.

  Names match without regard to case on Windows only.
* **The package comment** says why the child is started with the policy.
* Nothing outside `control/` changed, so the engine's environment is untouched.

**Tests** (`control_test.go`; `live_mcp_test.go`, new):

* The fake serve's launch log records `OPENCODE_CONFIG`, `OPENCODE_CONFIG_CONTENT` and the named
  file's content. `TestMain` gains a `FAKE_MCP_MARKER` branch, so the test binary can be a user's
  MCP server.
* Unit tests:
  * `TestControlPlaneDeniesMCP`;
  * `TestControlPlaneKeepsTheOperatorsConfig`;
  * `TestControlPlaneWarnsWhenItCannotDeny`;
  * `TestDenyMCPMatchesNamesAsTheOSDoes`;
  * `TestPolicyFileIsRemovedOnClose`;
  * `TestOnlyTheControlPlaneSetsOpencodeConfig`, a source test over `internal/provider`.
* `TestLiveV2ControlPlaneStartsNoMCPServers`, on an isolated home, with the test's own `HOME` so
  the real home is never a location:
  * the global config declares a server, and a global plugin adds another;
  * **its control half:** a plain `opencode serve` must start both after one integration list;
  * **its case:** `control.New` must start neither, across the auth and project routes, with the
    probe plugin listed.

**Each seen failing** (`mcp/plant_0230.py` in the session scratchpad, 10 deliberate breaks on
scratch copies):

| Break | Fails |
| --- | --- |
| the policy not added | `TestControlPlaneDeniesMCP`: "launch config "" (file ""), content """ |
| the file holding another statement | `TestControlPlaneDeniesMCP`: "file "{"policies":[]}"" |
| the operator's `OPENCODE_CONFIG` replaced | `TestControlPlaneKeepsTheOperatorsConfig`: "unexpected end of JSON input" (no inline policy) |
| the operator's other keys dropped; the statement prepended | `TestControlPlaneKeepsTheOperatorsConfig`: "want the operator's keys kept and the policy appended" |
| the warning not logged | `TestControlPlaneWarnsWhenItCannotDeny`: "no warning logged" |
| the names never folded | `TestDenyMCPMatchesNamesAsTheOSDoes` |
| the file left after `Close` | `TestPolicyFileIsRemovedOnClose`: "the policy directory: <nil>; want it gone" |
| another file naming `OPENCODE_CONFIG` | `TestOnlyTheControlPlaneSetsOpencodeConfig`: "../../opencode/acp.go names OPENCODE_CONFIG" |
| live: the policy not added | `TestLiveV2ControlPlaneStartsNoMCPServers`: "the control plane started map[config:true plugin:true]" |

The first version of "the operator's `OPENCODE_CONFIG` replaced" left a variable unused and did
not compile. It was rewritten. It was not a fault in a test.

**Gates.**

| Gate | Result |
| --- | --- |
| `go test -race -count=1 ./...` | exit 0, 47 packages ok, no race |
| the live pin, this Mac | PASS, 8.6 s |
| the Linux test host: `go test -race -count=1 ./...`, then the live pin (C2) | exit 0, 47 packages ok; PASS, 11.2 s; `go1.27.3` |
| the Windows test host: `make ci-windows`, then the live pin (C2) | exit 0, "ALL SELECTED CHECKS PASSED", 47 packages ok; PASS, 7.2 s |
| `make pre-add-check FILES=…` (the 3 Go files) | "3 file(s) clean (gofmt, golint, govulncheck)" |
| `make lint`; `make staticcheck`; `make vet-tags`; `make check-records` | exit 0 each |

The live pin sends no prompt and reaches no network. It ran on the owner's approval of this plan.

**Not done:**
* The plugin-setup finding (MADR D5) is not addressed, as decided.
* MCP status for v2 diagnostics stays omitted.
* The pin joins the checks run at each engine pin from 0214's P7, which is not yet run.

Every acceptance criterion holds, so this plan is complete with the owner's commit.

**Files for this commit:**
* `internal/provider/opencode/control/control.go`, `control_test.go`, and `live_mcp_test.go` (new);
* this pair;
* the 0214 PLAN's dated note.
