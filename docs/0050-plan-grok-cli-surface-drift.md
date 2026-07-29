# MADR 0050 — Implementation plan: grok CLI surface drift

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0050](./0050-MADR-grok-cli-surface-drift.md). Read that
first: it carries the measurements, the seven broken options, and decisions
D1–D5. This is the build order, keyed to source as of `4e8e7e3` and to grok
**0.2.114**.

- **Status**: Implementation-ready
- **Date**: 2026-07-29
- **Scope**: `internal/provider/grok`, `internal/provider/acpagent/config.go`,
  `internal/config`, `docs/config.md`. No protocol change, no mobile change.
- **Standards**: `/home/mac/standards/go` — `testing.md` (read first: D2 turns
  on the difference between asserting our own argv and executing the binary)

---

## 0. Goal, non-goals, ground rules

### Goal

No `providers.grok.*` option can prevent grok from starting; `permission_mode`
works and defaults to `default`; grok's sandbox is exposed; and a live test
fails the next time grok relocates a flag.

### Non-goals

- `--agents`, `--agent`, `--rules`, `--system-prompt-override`, `--max-turns`,
  memory flags, `/auto` command mapping. Recorded in MADR §4 as a follow-up.
- Changing MADR 0049's auto mode. It is correct — §2 of the MADR proves it —
  and this plan only makes it reachable.
- Runtime `grok --help` parsing (rejected, MADR D2).

### Ground rules

1. **One phase → one commit.** Do not push unless asked.
2. **Go gate before `git add`**: `make pre-add-check FILES="..."`.
3. **Commit without `-m`** — the hook writes the message.
4. **Measure, don't infer.** Every claim about grok's CLI in this plan came
   from executing `grok`; keep that standard. `grok agent --help` and
   `grok --help` disagree about which flags exist — trust the binary.
5. Live tests need grok in `PATH`; they skip otherwise and are **not** a CI
   gate.

### Verified baseline facts (do not rediscover)

Measured on grok 0.2.114, 2026-07-29:

| Claim | Evidence |
|---|---|
| Seven flags rejected after `agent`, accepted before it | MADR §1 table; `error: unexpected argument '--permission-mode' found` |
| `grok agent` accepts only `--reauth -m --reasoning-effort --always-approve --agent-profile --plugin-dir --leader --no-leader --leader-socket --debug --debug-file --grok-ws-* --xai-api-base-url --cli-chat-proxy-base-url` | `grok agent --help` |
| `grok agent stdio` accepts only `--debug --debug-file --leader-socket` | `grok agent stdio --help` |
| grok prompts via ACP under `default`/`acceptEdits`/`dontAsk`; not under absent/`auto`/`bypassPermissions` | MADR §2 matrix |
| Auto armed suppresses a real prompt **and** the write succeeds | MADR §2 second table |
| `--sandbox` accepts `off|workspace|devbox|read-only|strict`; unknown → `sandbox profile resolve failed: Custom sandbox profile 'x' not found` | direct probe |
| `grok models` reports only `grok-4.5` on this host | direct probe |
| Existing `grok_test.go` asserts our own argv, so it cannot catch this class | `TestSpecModelArgs`, `TestSpecModelArgsPolicyFlags` |

---

## 1. Phase order

```text
A  argv placement fix + unit tests updated
B  live argv contract pin (the test that would have caught this)
C  permission_mode default = "default" + config/docs + release note
D  --sandbox support
E  measure --tools/--disallowed-tools on agent stdio; document or withdraw
F  verification
```

A must land first — every later phase depends on grok starting.

---

## Phase A — Put global flags before the subcommand

**Files:** `internal/provider/grok/grok.go`, `internal/provider/grok/grok_test.go`

### Steps

1. Rewrite `defaultArgs` so the vector is
   `grok <globals…> agent --no-leader stdio`:

   ```go
   // grok's own flags are global: they belong before the `agent` subcommand.
   // `grok agent` accepts only a small set (see its --help) and rejects the
   // rest outright — putting them after `agent` made seven config options
   // fail session start (MADR 0050 D1).
   func defaultArgs(cfg Config) []string {
       var args []string
       if cfg.Model != "" {
           args = append(args, "-m", cfg.Model)
       }
       if cfg.ReasoningEffort != "" {
           args = append(args, "--reasoning-effort", cfg.ReasoningEffort)
       }
       if cfg.AlwaysApprove {
           args = append(args, "--always-approve")
       }
       if cfg.PermissionMode != "" {
           args = append(args, "--permission-mode", cfg.PermissionMode)
       }
       if len(cfg.AllowedTools) > 0 {
           args = append(args, "--tools", strings.Join(cfg.AllowedTools, ","))
       }
       if len(cfg.DisallowedTools) > 0 {
           args = append(args, "--disallowed-tools",
               strings.Join(cfg.DisallowedTools, ","))
       }
       for _, r := range cfg.AllowRules {
           args = append(args, "--allow", r)
       }
       for _, r := range cfg.DenyRules {
           args = append(args, "--deny", r)
       }
       if cfg.NoSubagents {
           args = append(args, "--no-subagents")
       }
       if cfg.DisableWebSearch {
           args = append(args, "--disable-web-search")
       }
       return append(args, "agent", "--no-leader", "stdio")
   }
   ```

   `--always-approve`, `-m` and `--reasoning-effort` are valid in both
   positions; they move too so there is one rule rather than an exception list.

2. Update `TestSpecModelArgs` and `TestSpecModelArgsPolicyFlags` for the new
   order, and add an assertion that pins the *shape*, not just the contents:
   every element before the literal `"agent"` is a global flag, and the tail is
   exactly `["agent", "--no-leader", "stdio"]`. State in a comment that these
   tests cannot catch grok moving a flag — that is Phase B's job.

### Acceptance

`go test ./internal/provider/grok/` green; `go build ./...` clean.

---

## Phase B — Pin the argv contract against the real binary

**Files:** new `internal/provider/grok/live_argv_test.go` (build tag `live_grok`)

This is the test whose absence let the drift ship.

### Steps

Spawn the real binary once per configured option and assert it *starts* rather
than rejecting the vector. Feed `/dev/null` on stdin and give it a short
deadline — an ACP server that stays up is a pass; `error: unexpected argument`
is the failure.

```go
//go:build live_grok

// Pins the argv contract against the installed grok. The unit tests assert the
// vector we build, which is precisely what was wrong in MADR 0050: every flag
// was placed where `grok agent` rejects it, and no test noticed because none
// executed the binary. When grok next relocates a flag, this fails.
func TestLiveGrokAcceptsEveryConfiguredFlag(t *testing.T) {
    cases := []struct{ name string; cfg grok.Config }{
        {"model", grok.Config{Model: "grok-4.5"}},
        {"reasoningEffort", grok.Config{ReasoningEffort: "low"}},
        {"alwaysApprove", grok.Config{AlwaysApprove: true}},
        {"permissionMode", grok.Config{PermissionMode: "default"}},
        {"allowedTools", grok.Config{AllowedTools: []string{"Bash"}}},
        {"disallowedTools", grok.Config{DisallowedTools: []string{"Bash"}}},
        {"allowRules", grok.Config{AllowRules: []string{"Bash"}}},
        {"denyRules", grok.Config{DenyRules: []string{"Bash"}}},
        {"noSubagents", grok.Config{NoSubagents: true}},
        {"disableWebSearch", grok.Config{DisableWebSearch: true}},
        {"sandbox", grok.Config{Sandbox: "workspace"}}, // after Phase D
    }
    // For each: p := grok.New(cfg); p.Start(ctx, …); assert no start error,
    // then Close. A rejected flag surfaces as a start failure.
}
```

Prefer driving `provider.Start` over hand-rolling `exec.Command`: it exercises
the same path production uses, including `--no-leader` and the ACP handshake.

### Acceptance

Green with grok installed. **Verify it bites**: revert Phase A locally, confirm
the seven flag cases fail, restore.

---

## Phase C — `permission_mode` defaults to `default`

**Files:** `internal/config/config.go`, `internal/config/load.go`,
`docs/config.md`, `README` grok section if it names defaults

### Steps

1. Change the shipped default for `providers.grok.permission_mode` from `""`
   to `"default"`, and validate against grok's enum
   (`default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`) —
   mirroring `validApprovalPolicy`/`validSandboxMode` for codex. Empty stays
   legal and means "inherit grok's own config".
2. `docs/config.md`: document the enum, the new default, what empty means, and
   **why** the default changed — with an empty value the daemon cannot know the
   session's approval posture (grok resolves it from `~/.grok/config.toml`,
   project config, or fleet-wide remote config since 0.2.102), so the mode chip
   and the `dangerous` flag describe a policy nobody set.
3. Document the opt-out for operators who want today's silent behaviour:
   `permission_mode: bypassPermissions`, or the per-session `auto` mode
   (MADR 0049), which is the supported answer.
4. Release note: **behaviour change on upgrade** — grok sessions that never
   prompted will start prompting.

### Acceptance

`go test ./internal/config/`; a fresh config yields
`--permission-mode default`; a bogus value is rejected at load with a clear
message.

---

## Phase D — Expose grok's sandbox

**Files:** `internal/provider/acpagent/config.go`, `internal/provider/grok/grok.go`,
`internal/config/*`, `docs/config.md`

### Steps

1. Add `Sandbox string` to `acpagent.Config` beside `PermissionMode`, doc'd as
   grok-specific today.
2. Emit `--sandbox <PROFILE>` in `defaultArgs` when non-empty (globals block).
3. Config key `providers.grok.sandbox`. Validate the five built-ins
   (`off`, `workspace`, `devbox`, `read-only`, `strict`) and **accept anything
   else** as a custom profile name — grok resolves those from
   `~/.grok/sandbox.toml` / `.grok/sandbox.toml` and fails with a clear message
   if absent. Do not hard-code an enum that breaks the day grok adds a profile.
4. `docs/config.md`: list the built-ins, note the custom-profile path and the
   `GROK_SANDBOX` env var, and cross-reference MADR 0048 — this is grok's
   analogue of the codex sandbox story, and on AppArmor-restricted hosts a
   containment profile may hit the same user-namespace wall.

### Acceptance

Unit test for argv; live case added to Phase B's table; an unknown profile
surfaces grok's own error rather than a generic start failure.

---

## Phase E — Are `--tools`/`--disallowed-tools` real for ACP?

**Files:** `docs/config.md`, possibly `internal/config/config.go`

The cheat sheet documents both as **headless-only**. If they are inert for
`agent stdio`, describing them as tool allow/deny lists is a lie of the same
family as the one this MADR is about.

### Steps

1. Measure. Start a session with `AllowedTools: ["Read"]` and prompt for a
   shell command; then `DisallowedTools: ["Bash"]` and prompt likewise. Compare
   against an unrestricted control. Record the outcome the way MADR §2 does.
2. If effective: document precisely which tool names are accepted.
3. If inert: mark both keys as **no-op for ACP sessions** in `docs/config.md`
   with the measurement date and grok version, and open a follow-up to either
   remove them or route tool policy through `--allow`/`--deny` (which are
   permission rules, not headless-only).

### Acceptance

`docs/config.md` states measured behaviour, not the CLI's summary line.

---

## Phase F — Verification

1. `make pre-add-check FILES="…"` on every touched `.go`.
2. `go build ./... && go test ./internal/...`.
3. `go test -tags live_grok ./internal/provider/grok/ -count=1` — full live
   suite, including MADR 0049's mode tests and Phase B's argv pin.
4. **Re-run the MADR §2 discrimination pair** with `permission_mode: default`:
   unarmed must prompt and block; armed must not prompt and must complete the
   write. This is the end-to-end proof that auto works, and it only became
   possible after Phase A.
5. `make preflight`.
6. Update MADR 0050 Status, and add a line to MADR 0049 §2 recording that its
   mechanism is now proven end-to-end rather than merely plumbed.
7. Manual smoke from the phone: a grok session prompts for a write under the
   new default; arming `auto` stops the prompts and the edit lands; switching
   back to `default` restores prompting.

Done means: no grok config option can break startup, `permission_mode` and
`sandbox` work, auto is proven, and the argv contract has a test that will
notice the next drift.
