# MADR 0069 — Implementation plan: macOS permissions

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: In progress. **P0 implemented 2026-08-04** (U1/U2:
  template gained explicit `codex.allow_full_access` + `grok.sandbox`;
  bidirectional parity test added and mutation-checked — it fails on
  exactly the 0069 F1 drift shape; README macOS Seatbelt note added;
  `go.yaml.in/yaml/v3` promoted to a direct dependency for the test).
  The operator step (P0.4) had already landed the same day:
  `allow_full_access: true` in the macOS host's live config + daemon
  kickstart — symptom resolved. **P1 implemented 2026-08-04** (U4 green:
  shared `provider.ResolveSessionCWD` with errno-preserving validation —
  all four providers, codex/acphttp `os.Getwd()` fallbacks removed;
  `agenterr.KindPermission` + `IsPermission`; fs-callback/terminal wraps
  attributing daemon-identity denials; `permission_denied` registered in
  the protocol error registry and documented in protocol-v1.md — the
  AST error-code lint enforces both. Note: all four spawn sites already
  used `%w`, so P1.3 reduced to the writeSessionErr mapping. The
  message-text classifier accepts bare "permission denied" only
  alongside a path-ish "/" so a user's tool-approval denial can never
  misclassify as an OS error). **P2 implemented 2026-08-04** (U5 green
  both stacks: guidance composed at the ws layer via a typed
  `provider.CWDPermissionError` carrying the path — providers stay
  GOOS-free as planned; FDA copy only for darwin + protected root, plain
  otherwise including Linux `~/Documents`; codex turn errors now run
  `agenterr.Classify` (previously never called — quota/rate classify as
  a bonus) with the mode-pointing hint under a confining sandbox only;
  phone renders `errorKind: permission` as the "Blocked by permissions"
  card with the daemon-composed message verbatim; suites at 723 mobile /
  Go clean). P3–P6 outstanding.
- **Date**: 2026-08-04
- **Scope**: Go daemon (`internal/agenterr`, `internal/provider/*`,
  `internal/ws`, `internal/cli`), build (`Makefile`,
  `scripts/install-binary.sh`), setup template, docs; one small
  phone-side error-copy surface. No wire-protocol changes.
- **Source**: [0069-MADR-macos-permissions-and-sandbox-parity.md](0069-MADR-macos-permissions-and-sandbox-parity.md)

---

## 0. Grounding — where every change lands

All seams verified 2026-08-04 (MADR F1–F7 carry the evidence):

| Seam | Location | Change |
| --- | --- | --- |
| Setup template codex block | `internal/cli/service/defaults_mcremote.yaml:90-101` | add `allow_full_access: false` (commented) + grok `sandbox` key (P0) |
| Example config (reference) | `configs/config.example.yaml:205-216` | parity source of truth; untouched |
| codex mode gate | `internal/provider/codex/mode.go:106-115` | unchanged (working as designed) |
| agenterr kinds | `internal/agenterr/agenterr.go` | new `KindPermission` (P1) |
| ACP fs callbacks | `internal/provider/acpagent/session.go:1933-1935`, `:1961-1966` | classify `fs.ErrPermission` (P1) |
| Spawn sites | `acpagent/acpagent.go:302`, `httpagent/provider.go:436`, `acphttp/provider.go:269`, `codex/provider.go:308` | `%w` the exec error (P1) |
| Session-error mapping | `internal/ws/server.go:1378-1403` (`writeSessionErr`) | map `fs.ErrPermission` → `permission_denied`; log at info (P1, P3) |
| Unlogged error write | `internal/ws/server.go:1852-1855` (`writeError`) | info logging (P3) |
| Terminal host | `internal/provider/acpagent/terminal.go:86-93` | classify create failure (P1) |
| cwd validators (stat-only, errno discarded) | `acpagent/acpagent.go:482-484`, `httpagent/session.go:385-387` | `%w` + corrected message (P1) |
| Missing cwd validation | `codex/session.go:131-137`, `acphttp/session.go:112-118` | add validation; kill `os.Getwd()` fallback (P1) |
| Engine stderr splice (Debug-only logs) | `httpagent/provider.go:484,516,947`, `acphttp/provider.go:305,325`, `codex/provider.go:359` | warn-log the spliced tail (P3) |
| Turn-error surface | `codex/session.go:1740-1752` | attach `errorKind` when classified (P2) |
| Phone error kinds | `apps/mobile/lib/data/chat/chat_models.dart:157`, `chat_bubble.dart:453` | render `permission` kind (P2) |
| goose modes | `internal/provider/goose/goose.go:23-28`, `:44` | `auto` dangerous, default `approve` (P4) |
| Probe/doctor | `internal/cli/` (new `doctor.go`), daemon startup | P5 |
| Signing | `Makefile:13-18` region, `scripts/install-binary.sh` | opt-in `MC_CODESIGN_IDENTITY` (P6) |
| Runbook | `docs/ops-macos-tcc.md` (new) | P6 |

Notable non-facts (checked): no `runtime.GOOS` anywhere in providers —
none is added by this plan (classification is platform-neutral; only
*copy* mentions macOS, chosen at the daemon which knows `GOOS`);
`dangerous` and `errorKind` already exist on the wire — no protocol
work; the live-host binary identity is `a.out`/adhoc (MADR F7), so P6
has a measurable before/after.

## P0 — Template parity (D1, D2)

1. `defaults_mcremote.yaml`: add the full commented codex block
   (`allow_full_access: false` with the 0048/0069 context comment) and
   grok's `sandbox` key, mirroring `config.example.yaml` values.
2. Parity test (new `internal/cli/service/template_parity_test.go`):
   parse both YAMLs; for each provider block assert the template's keys
   ⊆ example's keys **and** every key present in both carries the same
   default. Fails on future drift (U1).
3. README codex section: document `allow_full_access`, its default, and
   the Seatbelt consequence of leaving it off (out-of-workspace EPERM in
   auto/default modes — MADR F5).
4. Record (docs only): the macOS host's live config gained
   `allow_full_access: true` on 2026-08-04 (operator action per D2, done
   during review). No code ships it.

**Tests:** U1; extend `internal/provider/codex` mode-filter test to run
the same assertions via config parsed from *both* YAML files (U2).

## P1 — EPERM classification core (D4.1–D4.4)

1. `internal/agenterr`: add `KindPermission`; `Classify` recognizes
   `errors.Is(err, fs.ErrPermission)` (covers `EPERM` and `EACCES` from
   `*os.PathError`/`*exec.Error` chains).
2. ACP fs callbacks (`acpagent/session.go:1933`, `:1961`): wrap before
   returning over JSON-RPC — message keeps the path, gains the kind.
3. Spawn sites (four providers): wrap exec errors with `%w` so the
   errno survives to `writeSessionErr`; `writeSessionErr` maps
   `fs.ErrPermission` → stable code `permission_denied`.
4. Terminal create (`terminal.go:91-93`): same wrap.
5. Validators: `if st, err := os.Stat(cwd); err != nil` branches — EPERM
   → typed permission error; `ENOENT`/`ENOTDIR` → corrected literal
   messages ("does not exist" / "not a directory"); always `%w`.
6. Validation parity: codex and acphttp validate cwd identically to
   acpagent (empty → home, never `os.Getwd()` — the acpagent comment at
   `acpagent.go:464-465` becomes true everywhere).

**Tests:** U4 — injected-statFn/exec-error unit tests per seam; mapping
test for `writeSessionErr`; mid-session surfaces get the kind for free
via existing `Classify` call sites (assert one).

## P2 — Disambiguation copy + phone rendering (D4.5)

1. Daemon picks the copy (the phone stays dumb): where a
   `permission_denied` is emitted with session context available, attach
   `errorKind: "permission"` and compose the message server-side:
   - codex session with sandbox policy `workspace-write`/`read-only` →
     "…this mode sandboxes the agent to the workspace. Switch modes, or
     enable `allow_full_access` on the host." (suggestive wording);
   - `GOOS == darwin` and the path is under a protected root
     (`~/Documents`, `~/Desktop`, `~/Downloads`, iCloud Drive prefix) →
     Full Disk Access guidance naming `mcremote` and System Settings;
   - otherwise plain permission copy.
2. Phone: `chat_models.dart` parses `errorKind: permission`;
   `chat_bubble.dart` renders it like `quota`/`rate_limit` (distinct
   presentation, full message shown). No path logic client-side.

**Tests:** U5 — go unit for the three copy branches (context injected);
dart unit for kind parse + bubble rendering.

## P3 — Observability (D7)

1. `writeSessionErr` and `writeError` log at `info`: code, clipped
   message (same clip as the wire), session id, provider when known.
2. Engine-stderr splice sites log the spliced tail at `warn` at the
   moment they embed it in a returned error (bounded by the existing
   20-line tail).
3. No new log levels, no additional payload beyond what the phone
   already receives.

**Tests:** U7 — log-capture unit asserting one line per emitted error
and warn-tail on a synthetic engine start failure.

## P4 — Goose mode honesty (D3)

1. `goose.go`: `auto` gains `Dangerous: true`; `DefaultModeID` →
   `"approve"`.
2. Confirm the 0049 dangerous-mode confirmation flow fires for goose on
   the phone (existing machinery; no new UI).
3. `docs/0044-MADR-auto-approve-modes.md`: annotate the "goose left
   untouched" item as decided by 0069 D3.

**Tests:** U3 — go unit (mode table); dart widget test extending the
0049 suite with a goose-shaped mode list.

## P5 — Probe + doctor (D5)

1. Startup probe (daemon): stat `~/Downloads` (exists on every macOS
   account); `EPERM` → one `warn` line "macOS Full Disk Access not
   granted; sessions under protected folders will fail — see
   ops-macos-tcc.md". Non-darwin: compiled out (this is the one
   permitted `GOOS` check, in daemon startup, not providers).
2. `mcremote doctor` (new subcommand): section "macOS privacy" — probe
   result, grant instructions, `tccutil reset` recovery, unified-log
   predicate for confirming TCC denials. Runs the same probe code.
3. Never a hard failure; probe result not cached (re-evaluated per run).

**Tests:** U6 — probe unit with injected statFn (EPERM-despite-exists /
absent / ok); doctor golden output.

## P6 — Identity stability + runbook (D6)

1. `Makefile`: when `MC_CODESIGN_IDENTITY` is set, after build:
   `codesign -f -s "$MC_CODESIGN_IDENTITY" -i com.magiccliremote.mcremote
   --options runtime=no` (no hardened runtime — it breaks nothing but
   buys nothing here) with an embedded `__info_plist`
   (`-ldflags -linkmode=external` not required; use
   `-sectcreate __TEXT __info_plist` via `CGO`-free `-ldflags` extldflags
   only when external linking is already in play — otherwise sign with
   explicit `-i` identifier, which suffices for a stable DR with a
   certificate anchor). Unset ⇒ byte-identical current path.
2. `scripts/install-binary.sh`: preserve the signature (no strip/copy
   that invalidates it; `install`+`mv` already safe — add a
   `codesign --verify` post-install check when the env var is set).
3. `docs/ops-macos-tcc.md` (new): grant-keying model, the
   upgrade-revocation trap unsigned, granting FDA to a bare binary,
   `tccutil reset SystemPolicyAllFiles`, log predicate, and the
   LaunchAgent-not-Terminal attribution warning (MADR F7).
4. Cross-references: 0065 PLAN note (update flow must re-sign with the
   local identity when configured, or document re-grant); 0060 MADR
   amendment note (Apple-Development middle path, optional).

**Tests:** U8 — live-tagged: with a certificate present, two builds with
a source change between them produce the same identifier and an
anchor-based designated requirement (`codesign -d -r-` parse); without
the env var, `codesign -dv` still reports adhoc/`a.out` (unchanged).

## File checklist

| File | Phases |
| --- | --- |
| `internal/cli/service/defaults_mcremote.yaml` | P0 |
| `internal/cli/service/template_parity_test.go` (new) | P0 |
| `README.md` | P0, P6 |
| `internal/agenterr/agenterr.go` | P1 |
| `internal/provider/acpagent/session.go` | P1 |
| `internal/provider/acpagent/acpagent.go` | P1 |
| `internal/provider/acpagent/terminal.go` | P1 |
| `internal/provider/httpagent/provider.go` | P1, P3 |
| `internal/provider/httpagent/session.go` | P1 |
| `internal/provider/acphttp/provider.go` | P1, P3 |
| `internal/provider/acphttp/session.go` | P1 |
| `internal/provider/codex/provider.go` | P1, P3 |
| `internal/provider/codex/session.go` | P1, P2 |
| `internal/ws/server.go` | P1, P2, P3 |
| `apps/mobile/lib/data/chat/chat_models.dart` | P2 |
| `apps/mobile/lib/features/chat/chat_bubble.dart` | P2 |
| `internal/provider/goose/goose.go` | P4 |
| `docs/0044-MADR-auto-approve-modes.md` | P4 |
| `internal/cli/doctor.go` (new), daemon startup | P5 |
| `Makefile`, `scripts/install-binary.sh` | P6 |
| `docs/ops-macos-tcc.md` (new) | P6 |
| `docs/0065-PLAN-update-automation.md`, `docs/0060-MADR-local-unsigned-build-and-install.md` | P6 |

## Verification map (MADR → plan)

| MADR ID | Plan phase | Kind |
| --- | --- | --- |
| U1, U2 | P0 | go unit |
| U4 | P1 | go unit |
| U5 | P2 | go unit + dart unit |
| U7 | P3 | go unit |
| U3 | P4 | go unit + dart widget |
| U6 | P5 | go unit |
| U8 | P6 | live-tagged + manual |
| G1 (+ Q2/Q3/Q4/Q5 checks) | after P6 | ops walkthrough on this host |

## Edge cases held by design (for review)

- **A classified EPERM inside agent prose** (tool text, command cards)
  stays verbatim — the daemon only classifies errors it constructs
  (MADR Rejected). The copy split can therefore be *absent* on some
  surfaces; that is accepted, not a bug.
- **Sandbox copy on Linux**: the codex-sandbox branch of D4.5 keys on
  the session's sandbox policy, not GOOS — a Linux bwrap denial gets
  the same "switch modes" hint, which is equally correct there.
- **Probe false-negative**: an FDA-granted daemon probing `~/Downloads`
  succeeds even though a *specific* protected path may still fail
  (iCloud, external volumes) — the runbook says the probe is a
  lower-bound signal, not a guarantee.
- **Signed binary, unsigned agents**: agent CLIs keep their own
  identities; because children inherit the daemon's TCC responsibility
  (MADR F7), the daemon's grant is what matters for spawned work — but
  independently launched engines (none today) would not be covered.
- **Goose default flip on existing sessions**: mode is per-session
  state; existing sessions keep whatever they had. Only new sessions
  start in `approve`.

## Sequencing and commits

House rules: one commit per phase (`git commit --no-edit`), suites green
before each commit (`go vet && go test ./...` + the mobile trio for
P2/P4), no pushes until review. Order P0 → P6: P0 is the reported
symptom's durable fix; P1 before P2 (copy needs the kind), P2 before P3
only by convention (independent in fact); P4 anywhere but kept after the
error work so its dart tests don't collide; P5 needs P1's classifier;
P6 last — it changes the build and wants everything else already green.
The G1 ops walkthrough (with Q2–Q5 probes from the MADR) runs on this
host after P6 and closes the MADR's open questions in one session.
