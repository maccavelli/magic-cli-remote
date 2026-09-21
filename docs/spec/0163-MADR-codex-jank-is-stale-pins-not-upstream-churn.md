---
status: proposed
date: 2026-09-21
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The Codex provider's jank is stale pins and a dead drift gate, not upstream churn

## Context and Problem Statement

Codex is the provider with the most defects, the most workarounds and the worst
reputation for predictability. The owner asked for one audit covering four things
at once: our provider code, the newly installed Codex CLI driven as a live
binary, the upstream sources at the matching tag, and what the release makes
newly possible.

The premise going in was that Codex churns faster than we can track. **That
premise is wrong, and correcting it reframes everything else.** Between the
version our contract pins (0.149.1) and the version now installed (0.155.1) the
JSON-RPC surface removed nothing, renamed nothing, retyped nothing and added no
required field; all 191 pinned `required_params` arrays still hold; `initialize`
is byte-identical. What actually hurts is on our side: the one mechanism built to
detect drift **cannot pass on any host** and so is never run, three version pins
inside the package disagree with each other and with the binary in use, and the
capability list the phone is told is six releases old.

The second theme is Windows. Two of the workarounds we carry are still
necessary, one is now obsolete, and the sandbox mechanism that locked the owner's
`config.toml` on 2026-09-19 has a fix upstream whose absence at 0.149.1 explains
the incident **in precisely our process topology**.

This record is an audit. It states what was measured, what is broken, what is
newly available and what should be decided. A PLAN follows on approval.

### What was measured, not assumed

Everything below was run on the owner's Windows host (MAC420, Windows 11
10.0.26200) on 2026-09-20 and 2026-09-21, against the binary our daemon actually
drives.

**The version delta is not the commit range it looks like.** `rust-v0.149.1` and
`rust-v0.155.1` have **no merge base** in the owner's clone: 0.155.1's history is
a *grafted* 266-commit history rooted at `ac192cd79` (2026-09-06), while
0.149.1's tip is 2026-08-23 with 9581 reachable commits. A `git log
rust-v0.149.1..rust-v0.155.1` therefore shows only 2026-09-06 → 09-18 and
silently omits the 2026-08-23 → 09-06 half — which is where much of the Windows
sandbox and config work lives. **Ground truth is the tree-to-tree diff**
(`git diff rust-v0.149.1 rust-v0.155.1 -- <path>`), which is immune to the graft;
commit attribution for 2026-09-04 → 09-06 is simply unavailable in this clone and
is marked **[GAP]** below. Tag depths: 0.149.1 = 9581, 0.153.4 = 10135,
0.154.0 = 48, 0.155.1 = 266.

**A related trap: committer dates lie here.** One config key's commit is dated
2026-08-21, before the 0.149.1 tag, yet the key is absent at 0.149.1 and present
at 0.153.4. Attribute by tag bisection (`git ls-tree` / `git grep` at each tag),
never by date.

**Probe 1 — there are two Codex installs on this host, at different versions.**

| Path | `--version` | Driven by |
| --- | --- | --- |
| `%APPDATA%\npm\codex.cmd` → vendored `codex.exe` | **`codex-cli 0.155.1`** | **our daemon** |
| `%LOCALAPPDATA%\OpenAI\Codex\bin\12219cbfbcbddde7\codex.exe` | **`codex-cli 0.154.0-alpha.6.2`** | the Codex desktop app |

Confirmed from the live process tree: `mcremote.exe serve … → cmd.exe /d /s /v:off
/c ""…\npm\codex.cmd" "app-server" "--listen" "stdio://"" → node bin\codex.js →`
the npm-vendored `codex.exe`. The desktop app runs the older managed binary with
`-c features.code_mode_host=true`.

**Probe 2 — the source tree the owner pulled is not the installed version.**
`~/gitrepos/codex` sat on `ac192cd7` (2026-09-06), `behind 750`, with two modified
TUI snapshots. The audit used a detached worktree at `rust-v0.155.1`
(`be2951ea3`, 2026-09-18). `core.longpaths` had to be enabled first: Codex's
`apply-patch` fixtures exceed `MAX_PATH` on Windows.

**Probe 3 — our drift gate fails today.**

```text
$ go test -tags live_codex_contract ./internal/provider/codex/ -run TestLiveContract
    live_contract_test.go:44: installed stable schema differs from the 0.149.1 manifest
--- FAIL: TestLiveContractNoModelTurn (4.14s)
```

**Probe 4 — the protocol delta, derived three independent ways** that agree
exactly: by exporting the installed binary's schemas, by reading the Rust macro
definitions at the tag, and by decompressing the committed schema blobs at both
tags. Extracted method sets are byte-identical across methods.

```text
codex app-server generate-json-schema --out <DIR>                 # stable
codex app-server generate-json-schema --experimental --out <DIR>  # experimental
```

| Surface | kind | 0.149.1 | 0.155.1 | added | removed |
| --- | --- | --- | --- | --- | --- |
| stable | client_requests | 95 | 102 | 7 | **0** |
| stable | server_notifications | 75 | 82 | 7 | **0** |
| stable | server_requests | 10 | 10 | 0 | **0** |
| experimental | client_requests | 150 | 164 | 14 | **0** |
| experimental | server_notifications | 75 | 82 | 7 | **0** |
| experimental | server_requests | 11 | 11 | 0 | **0** |

Schema definitions grew 940 → 1021 (stable) and 1170 → 1288 (experimental) with
zero removals. Method literals live at `oneOf[].properties.method.enum[0]` —
`enum`, not `const`; a `const`-based extractor silently returns zero methods.

**Probe 5 — the handshake, measured live.** `codex app-server --stdio`,
newline-delimited JSON, no `Content-Length`:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"…","version":"0.0.1"}}}
→ {"id":1,"result":{"userAgent":"…/0.155.1 (Windows 10.0.26200; x86_64) …",
                    "codexHome":"…","platformFamily":"windows","platformOs":"windows"}}
```

`initialize` needs **no auth** — it succeeded against a home reporting `Not
logged in`. The response has exactly four fields and **no capabilities object**:
negotiation is one-way. The reply omits `"jsonrpc"`. A
`remoteControl/status/changed` notification arrives unsolicited immediately after.

**Probe 6 — `experimentalApi` is the runtime gate, measured both ways.** With
`experimentalApi: false`, `collaborationMode/list` → `-32600
"collaborationMode/list requires experimentalApi capability"`; with `true`, it
returns the `Plan` and `Default` modes.

**Probe 7 — what we send.** The 59 client methods our non-test code sends,
checked against the 0.155.1 export: **45 stable, 13 experimental-only, 1 declared
nowhere** (`gitDiffToRemote`).

**Probe 8 — the notification routing gap.** 82 declared, 75 routed, 7 dropped,
0 stale routes.

**Probe 9 — transcript item coverage.** 19 `ThreadItem` variants; `items.go`
names 18. Missing: `functionCallOutput`.

**Probe 10 — four decode risks checked against our code.** We never decode
`call_id` (now nullable upstream) and never read Codex's SQLite state DB (table
renamed upstream) — both safe. A retired model id (`gpt-5.4-mini`) survives only
in a synthetic test fixture. But we do **not** read the new approval `kind`
field (F22).

### Findings

#### The contract machinery

**F1 — The drift gate is red-by-construction, so nothing detects drift.**
`live_contract_test.go:43-48` compares the installed schema to the embedded
0.149.1 manifest with `reflect.DeepEqual`, and `:62-64` requires the binary's
SHA-256 to equal a pinned literal. Any addition anywhere fails it (probe 3), so it
cannot pass unless the host runs exactly 0.149.1 with that exact binary. A gate
that cannot go green is a gate nobody runs — which is why the seven unrouted
notifications and the missing item variant were found by this audit and not by CI.

**F2 — Re-pinning requires editing a test file, not just setting the documented
env vars.** The four `CODEX_CONTRACT_*` / `CODEX_SOURCE_*` variables are
necessary but not sufficient: `CodexVersion` (`contract_generate_test.go:30`),
`BinarySHA256` (`:31`), the upstream `Commit` (`:45`) and three output paths
(`:53-55`) are hardcoded, and the `implemented` set is a hardcoded switch
(`:118-132`). That is why re-pinning has not happened in six releases.

**F3 — Three version pins disagree, and the phone is told the oldest.** Embedded
contract 0.149.1 (`contract.go:121-128`); `KnownGoodVersion` plus the wire and
doctor fixtures 0.152.1 (`version.go:17`); an older fixture set 0.147.0. The
capability list advertised to the phone comes from the embedded manifest
(`capabilities.go:250-269` → `internal/ws/liveness.go:177-178`), so the mobile
client is told what 0.149.1 could do while the daemon drives 0.155.1. The two
baseline files do not even share a commit: `manifest.json` is `rust-v0.149.1`
while `source-watch-manifest.json` pins `6143217c…`, which is `rust-v0.150.0`.

**F4 — Our manifest mislabels experimental notifications as stable, because
Codex's own exporter does.** `filter_experimental_schema` prunes experimental
client methods, server methods and *fields*, but never experimental
**notifications** — verified in `export.rs`, whose body calls
`prune_experimental_methods` for `EXPERIMENTAL_CLIENT_METHODS` and
`EXPERIMENTAL_SERVER_METHODS` only. Both bundles therefore carry all 82
notifications while the runtime does suppress the experimental ones
(`app-server/src/transport.rs:117-123`). Our manifest consequently marks as
`stable` notifications that will never arrive without the opt-in, including
`thread/queue/changed`, `project/changed`, `thread/realtime/sdp`,
`thread/environment/connected` and `turn/moderationMetadata`. The true split at
0.155.1 is **62 stable / 22 experimental**, not 82/82.

**F5 — The `implemented` classification is provably stale in one direction.** All
21 server-request entries are `typed_deferred`, yet `session.go:1768-1786`
implements six and explicitly rejects three; the generator's `implemented` switch
never lists a server request.

#### Upstream compatibility — the reassuring part

**F6 — The upgrade is backward compatible, measured three ways.** Zero methods
removed or renamed, zero params or results retyped, **zero new required fields
anywhere**, all 191 `required_params` and every `required_result` in our pinned
fixtures unchanged, every schema type name still resolving, `initialize`
byte-identical, JSON-RPC error-code set unchanged
(`-32000, -32001, -32004, -32010, -32020, -32021, -32022, -32600..-32603`). No
production reference in our Go code names a method that does not exist at
0.155.1. **We can re-pin with very low regression risk; the work is opt-in
migration, not repair.**

**F7 — The one wire-breaking change does not touch us.**
`BedrockSetupParams` lost its `AccessKeys` variant (experimental); access-key
setup moved to `account/login/start`. Irrelevant unless we drive Bedrock.

**F8 — Two relaxations, both in our favour.**
`FunctionCallOutputResponseItem.call_id` left `required` and became nullable
(verified: we never decode it), and `PermissionsRequestApprovalParams.cwd` moved
from `AbsolutePathBuf` to `LegacyAppPathString`, which accepts any UTF-8 string
without validating it — a win for Windows path spellings.

**F9 — 21 new methods, and four promotions that are ours to take.** New: 7 stable
client requests, 14 experimental client requests, 7 notifications. Promoted
experimental → **stable**: `thread/items/list`, `thread/turns/list`,
`thread/revert` and the `thread/reverted` notification, plus the fields
`thread/fork.excludeTurns`, `thread/resume.excludeTurns`,
`thread/resume.itemsBackwardsCursor`, `thread/resume.turnsBackwardsCursor` and
`thread.historyMode`.

**F10 — The experimental opt-in is still load-bearing, for 13 of our methods.**
`thread/settings/update`, `thread/search`, `collaborationMode/list`,
`thread/backgroundTerminals/{list,terminate,clean}`,
`environment/{add,status,info}` and `process/{spawn,writeStdin,resizePty,kill}`
remain experimental-only. The opt-in cannot simply be dropped; what it buys is now
enumerable.

**F11 — The experimental negotiation costs a whole process, and its trigger is a
regex.** `provider.go:827-841` reaps the engine and launches a second process when
`initialize` with `experimentalApi: true` is rejected. Rejection is recognised by
`collaboration.go:65-77`: `data.capability == "experimentalApi"`, **or** a
`(?i)experimental` regex against the message, **or** the same regex against the
raw error data. Measured at 0.155.1 the real message is `"<method> requires
experimentalApi capability"`, gated at `message_processor.rs:970-974`. A reworded
error flips the whole experimental surface, and because the server never echoes
what it accepted (probe 5) a failed opt-in is undetectable except by attempting a
gated call.

#### Defects in our code that the delta exposes

**F12 — Seven declared notifications are silently dropped, and they are exactly
the seven new ones.** `notificationRouteUnknown` is the zero value, so anything
absent from the 75-entry table in `routing.go:22-98` is discarded with a Debug log
at `provider.go:1230-1232`. Two of them —
`modelProvider/authRecoveryStarted` and `…Completed` — **already have handlers**
at `session.go:1717-1756` and are **stable** at 0.155.1; the test covering them
(`authrecovery_test.go:28-51`) calls `handleNotification` directly and bypasses
routing, so it is green against dead code. This is MADR 0137 F9's failure mode
recurring: the handler landed, the route did not, nothing noticed.

**F13 — One transcript item type is dropped.** `functionCallOutput` is 1 of 19
`ThreadItem` variants and appears in neither `itemsRenderedAsTools` nor
`knownNonToolItems` (`items.go:15-33`), so `session.go:1498-1504` counts it as
unknown and logs at debug. It degrades gracefully; the item never reaches the
transcript. `item_test.go:15-33` still enumerates the 0.145.0 probe list.

**F14 — `gitDiffToRemote` is live on the wire and invisible to every schema.**
`export.rs:75-76` declares `V1_CLIENT_REQUEST_METHODS = ["getConversationSummary",
"gitDiffToRemote", "getAuthStatus"]` and strips them from the bundles
(`:1429-1430`), while the server still dispatches
`ClientRequest::GitDiffToRemote` (`message_processor.rs:1773`). So our call at
`diff.go:57` is valid and our contract can never see it, which is why it is
absent from the inventory. Sibling worth noting: `getAuthStatus` returns
`{authMethod, authToken, requiresOpenaiAuth}` (`protocol/v1.rs:202-206`,
dispatched at `:1751`).

**F15 — A single `-32601` permanently disables a feature for the engine's
lifetime.** `diff.go:57-63` latches `eng.diffUnavailable`;
`fast_personality.go:145-159` and `collaboration.go:168-182` do the same for
thread settings and collaboration modes. `capabilities.go:232-241` has **no
re-enable path**, so one transient error degrades that engine generation until the
process is replaced.

**F16 — Purge still has no local teardown.** `session.go:921-930` is
`thread/delete` and nothing else: no cancellation of pending permissions, no
`close(s.done)`, no removal from `p.sessions`, no detach from the caller's
deadline; `session/manager.go` calls `Purge` instead of `Close`. MADR 0146 records
the live symptom — a 60 s daemon hang, a 70 s phone timeout — and is still
`status: proposed`. Two other providers do it correctly.

**F17 — The jank is documented, not marked.** The package carries almost no
`TODO`/`FIXME`/`HACK` markers; the workarounds are long justifying comments, each
naming the version it was observed against (0.145.0 through 0.152.1). The debt is
invisible to any tooling that greps for markers.

**F18 — Live coverage has large holes that line up with the jank.** Nothing live
exercises the `unix_ws` or `ws` transports or either WS auth mode (we sign an
HS256 JWT with `iss: mcremote`, `aud: codex-app-server` that nothing has confirmed
Codex accepts); engine replacement and reconnect; any real approval round trip
(all six hardcoded decision vocabularies are fixture-only); the 1195-line
execution surface; `config/batchWrite`, which writes the user's `config.toml`;
history paging; `gitDiffToRemote`; or Purge. On Windows only 2 of the 4 declared
transports are even available.

#### What changed upstream that we must act on

**F19 — Paginated history is now the default for durable threads, and full
hydration is deprecated.** `thread/start` defaults non-ephemeral threads to
`historyMode: "paginated"` (#40677), and `thread/read` with `includeTurns: true`
now emits a `deprecationNotice` on **every call** (#40676), as does
`thread/resume`/`thread/fork` requesting full history. Verified advisory: the
notice is sent *after* the full response is computed, so data still returns today
— but `threads.go:930` is on a removal path. Combined with F9's promotions this is
one migration, not two.

**F20 — The managed app-server daemon now supports Windows.** The
`app-server-daemon` README moved from *"The current daemon implementation is
Unix-only … does not yet support Windows lifecycle management"* to *"The daemon
supports Linux, macOS, and Windows"*, with new `backend/windows.rs`,
`backend/pid_windows.rs`, `uds/src/windows_peer.rs`, `windows_security.rs` and
`windows_socket_validation.rs` **[GAP]**. Our `managed_daemon_proxy` refusal on
Windows (`config.go:116-118`, `launch.go:46-48`) is therefore **obsolete as a
platform limit**. Three gotchas from the README: a non-elevated terminal whose
host permits detached children; the control socket must fit the **108-byte
AF_UNIX limit including terminator** (so a short `CODEX_HOME`); and *"per-client
environment isolation is not provided"*.

**F21 — Windows app-server shutdown moved from a file to a socket.** `#43308`
deleted the `daemon_signal` file-watch branch from `shutdown_signal()`;
`CODEX_DAEMON_SHUTDOWN_FILE` now has exactly one non-test caller. The new contract
(`app-server-transport/src/transport/unix_socket.rs:140-156`) is a `/daemon/shutdown`
handshake on the control socket with a PID echo that must match exactly, and it
returns **403 "unmanaged server"** unless the process was launched with
`CODEX_DAEMON_SHUTDOWN_SOCKET`. If we launch the app-server ourselves without it,
our only stops remain console Ctrl-C (if it has a console) or `TerminateProcess`.
The `windowsSandbox/*` RPC schemas are byte-identical between tags.

**F22 — We would mis-render a stdin-injection approval as a command approval.**
`CommandExecutionRequestApprovalParams` gained
`kind: CommandExecutionApprovalKind` (`command | writeStdin`, defaulting to
`command` "for older servers"), because input to an already-escalated terminal now
requires approval (#40978). Verified: our code never reads that field — the only
`writeStdin` references are the unrelated `process/writeStdin` execution method
(`execution.go:698`). So the phone would present "approve this command" when the
real question is "allow typing into a running privileged shell". This is a
consent-integrity defect, not a cosmetic one.

**F23 — Two new error signals the phone should distinguish.** `CodexErrorInfo`
gained `rateLimitExceeded` distinct from `usageLimitExceeded` (#44492 maps
`insufficient_quota`, `credit_balance_exhausted` and the spend/usage-limit codes
to quota, keeping 429/`slow_down` as retryable), and `TurnError` gained
`misalignment` → `{errorType, detailedExplanation, steer: {message}}`, where
`steer.message` is *"Instruction to submit as the next turn's user input if
continuation is confirmed"* — a structured, one-tap-resumable refusal.

**F24 — Automatic daemon updates default to ON and will restart the server under
a connected client.** First check at a hard-coded 5-minute delay, then hourly; a
`Busy` outcome retries in **50 ms** rather than waiting for idle; there is **no
in-band notification**, only a disconnect. The kill switch lives in
`$CODEX_HOME/app-server-daemon/settings.json`
(`{"updater":{"autoUpdateEnabled":false}}`), is re-read before every wait, and
must be **on disk** — the updater ignores `-c` overrides, and no CLI subcommand
writes it. Related: the new `thread_unload_delay_secs` (default **60 s**, requires
a server restart) is *"Seconds a thread must have no subscribers and no activity
before app-server unloads it"* — i.e. exactly how long a backgrounded phone may be
away.

**F25 — Codex fixed the `timeoutMs` gap we work around.**
`ThreadShellCommandParams` gained `timeoutMs`, defaulting to one hour, where zero
means an immediate timeout rather than unlimited (#41384).

**F26 — Retired model ids.** `gpt-5.2` and `gpt-5.4-mini` were removed from the
bundled catalog (#44250); `base_instructions` was removed from it too. Verified:
we pin neither in production — `gpt-5.4-mini` survives only in a synthetic test
fixture (`thinking_test.go:36,92`).

**F27 — Our documentation source upstream was deleted.**
`codex-rs/app-server/README.md` went from 2570 lines to 115 (#43421), taking with
it the documented transport, lifecycle, approval, backpressure and `-32001`
contracts. The entire `docs/` tree is **byte-identical** between the two tags
(same tree hash), so it documents none of this delta. The authoritative sources
are now `codex-rs/core/config.schema.json`, the schema bundles, and the Rust
structs. Any future bump must generate from those, never from prose.

#### Windows, the sandbox, and the 2026-09-19 incident

**F28 — The ACL bug that best explains the owner's incident is fixed, and it was
triggered by exactly our topology.** At 0.149.1 the sandbox setup derived the
principal to grant from the environment:
`real_user: std::env::var("USERNAME").unwrap_or_else(|_| "Administrators")`. At
0.155.1 it comes from the OS token —
`real_user: current_account_name()?` via `GetUserNameExW`, with the comment
*"Sandbox launchers can filter USERNAME"*. `real_user` is the principal granted on
`.sandbox`, `.sandbox-secrets` and `.sandbox-bin`, and `.sandbox-bin` is locked
`DaclInheritance::Protected`. **A launcher that does not export `USERNAME` — a
service, or a relay-spawned app-server, which is our topology — therefore ACL'd
those directories for the literal string `"Administrators"` and not for the real
user**, after which a non-admin can neither read nor re-ACL that tree. That is
the shape of "applied ACLs it could not undo". Two further changes in the same
commit: ACL application moved out of the elevated helper into the unelevated
refresh, so *the identity that can apply a deny is now the identity that can
revoke it*; and `ensure_runtime_tree_readable` additively repairs missing
read/execute on every refresh.

**F29 — Nothing in Codex ever names `config.toml` as a deny target, so the deny
came from a glob or a managed file.** `resolve_windows_deny_read_paths` derives
the deny set from the permission profile's unreadable roots and globs; a tree-wide
grep finds no default deny for `CODEX_HOME`, `config.toml` or `auth.json`, and
none of the shipped profiles (`read_only`, `workspace_write`,
`danger-full-access`) carries denies. The only machine-injected denies come from
`[filesystem] deny_read` in `%ProgramData%\OpenAI\Codex\requirements.toml`. **Two
places to audit**, and one of them is exposed: `C:\ProgramData` is
world-writable-ish by default, so a non-admin can pre-create
`C:\ProgramData\OpenAI\Codex` and plant a `requirements.toml` — at 0.155.1 that is
*measured and reported* by a telemetry probe (#44284), **not prevented**.

**F30 — An upgrade will not clean up ACEs orphaned by the old code.** The only
record of applied denies is `$CODEX_HOME\.sandbox\deny_read_acl_state.json`;
nothing else scans for stale denies, `revoke_ace` is fire-and-forget, and there is
no repair subcommand — `clean_up_packaged_windows_sandbox` has exactly one caller,
the MSIX uninstall event. **If that state file is deleted or emptied, every deny
ACE it recorded is orphaned forever.** The owner's file was
`{"principals": {}}`, which is precisely that state. Related hazard, unchanged: a
deny entry naming a non-existent path **creates a directory there**.

**F31 — Keep the job-object workaround; Codex still has no console-signal
story.** `JOB_OBJECT_LIMIT_BREAKAWAY_OK` remains the default, so
`CREATE_BREAKAWAY_FROM_JOB` grandchildren still escape, and a tree-wide grep at
0.155.1 finds **no** `SetConsoleCtrlHandler`, `GenerateConsoleCtrlEvent`,
`CTRL_BREAK_EVENT`, `CTRL_C_EVENT` or `CREATE_NEW_PROCESS_GROUP` anywhere. The new
SIGTERM handler and 45 s shutdown watchdog are `cfg(unix)`. One real gain: a
control-pipe disconnect now kills the whole job instead of orphaning it (#40808).
Also note sandboxed non-TTY children now get `CREATE_NO_WINDOW` unconditionally —
no console flashes, and no console for a signal to arrive on.

**F32 — Device login is still destructive; keep our guard.**
`codex-rs/cli/src/login.rs:335` still calls `clear_existing_auth_before_login()`
→ `logout_with_revoke()` **before** the device flow starts; the only diff to
`login.rs` in the range is Bedrock-related, and `device_code_auth.rs` does not
appear in the tree diff at all. Our sidecar backup, `confirmDestructive` gate and
isolated-home approach all stay. (Note the superseded destructive path is still
the live one in production — `StartOwnedDeviceAuth` requires `NewCoordinated`,
which production construction does not use.)

**F33 — There is no "disable" for the Windows sandbox, only an omission.**
`[windows] sandbox = "elevated" | "unelevated"` with no `"disabled"` value;
resolution falls back to features, and `from_features` returns `Disabled` only
when neither feature is on. So the off switch is: omit `[windows] sandbox` and
leave `experimental_windows_sandbox` / `elevated_windows_sandbox` (both
`Removed` stage, default false) off. No `CODEX_*` env var disables it. Scoping is
via permission profiles. A new `windows_sandbox_service` feature
(`UnderDevelopment`, default false) backs a new `CodexSandboxService` + named pipe
that removes the interactive UAC prompt but not the privilege requirement.

**F34 — Managed policy is partly advisory, and we must not present it as
enforced.** In `TryFrom<ConfigRequirementsWithSources> for ConfigRequirements`
(`config/src/config_requirements.rs` ~`:1678-1683`),
`allow_browser_and_computer_use: _`, `browser_use: _` and `in_app_browser: _` are
destructured and **discarded**, while `application` and
`additional_developer_instructions` are bound and do reach the runtime. So Codex
will **not** stop a client doing browser or computer use that an administrator
denied — a client that surfaces those capabilities must enforce the restriction
itself. Enforced, by contrast: approval policies, approvals reviewers, sandbox
modes, permission profiles, web-search modes, and
`additional_developer_instructions`, which is injected as its own
`role() == "developer"` message that **our client cannot suppress** (cap 10,000
tokens, exceeding it is a load error).

**F35 — Four managed-policy fields are sent but described by no generated
type.** `allowedApprovalsReviewers`, `hooks`, `network` and `application` carry
`#[experimental("configRequirements/read.<field>")]`
(`protocol/v2/config.rs:416,431,434,436`), so the schema and TS exports omit them
while the request itself is not experimental-gated. Anything generated from the
schema silently loses admin-managed hooks and network policy — including
`network.header_injections`, where admin-injected HTTP headers live. We hand-write
our structs so we are not bitten today; our contract inventory understates the
response, and future codegen would regress.

## Decision Drivers

* **A gate that cannot pass is worse than no gate.** It costs maintenance,
  reports nothing, and provides false assurance that drift is monitored.
* **Consent integrity outranks features.** An approval prompt that misdescribes
  what is being approved (F22) is the most serious item in this record, and the
  cheapest to fix.
* **Windows is the primary host and the least covered.** Two of four transports
  are unavailable there, and every sandbox surface in this audit is Windows-only.
* **Prefer leaning on upstream to carrying our own defensive code** — but only
  where the fix is measured, not announced.
* **Do not advertise capabilities we cannot back.** The phone is currently told a
  six-release-old capability list.
* **An audit must not become a rewrite.** 26,812 lines across 116 files in the
  codex package alone; the response has to be ordered by risk, not by appetite.

## Considered Options

* **A — Re-pin, fix the cheap defects, and stage the new surface (chosen).**
* **B — Full uptake now**: attachments, timeline, `userVerification`, the managed
  Windows daemon, realtime voice, in one release.
* **C — Freeze**: keep the 0.149.1 pin, document the drift, change nothing.
* **D — Abandon the generated contract** and hand-maintain the method inventory.

## Decision Outcome

Chosen: **Option A**, executed in the order below. The ordering is the decision:
consent integrity first, then the machinery that makes drift visible, then the
defects that machinery has already found, then migrations, then staged uptake.

### The decisions

* **D1 — Handle `CommandExecutionApprovalKind` before anything else here ships.**
  Read `kind` on `item/commandExecution/requestApproval`, default it to `command`
  for older servers as the schema prescribes, and render `writeStdin` as what it
  is: input into an already-escalated terminal. Closes **F22**.
* **D2 — Re-pin the contract to 0.155.1, and make re-pinning a command rather
  than a code edit.** `CodexVersion`, `BinarySHA256`, the upstream `Commit` and
  the output directory become inputs (flags or env), not literals in a test body;
  the `implemented` set is derived from the code that sends and routes, not from a
  hand-maintained switch. Closes **F2**; makes **F3** mechanically preventable.
* **D3 — Replace the exact-match gate with a two-mode drift check.** The default
  mode fails only on **breaking** drift — a removed or renamed method, a newly
  required field, a stable→experimental demotion, or a method we reference that no
  longer exists — and reports additions as warnings with a machine-readable diff.
  An exact-match mode remains, used deliberately at release pinning. Closes
  **F1**.
* **D4 — Notification stability comes from the `#[experimental]` marker, not the
  schema bundle.** The generator reads `protocol/common.rs` (or an equivalent
  authoritative source) for notification stability, because Codex's own exporter
  does not prune experimental notifications. Closes **F4**.
* **D5 — Route every declared notification, and test through the router.** Add
  the seven missing routes; `modelProvider/authRecoveryStarted`/`Completed`
  become live (both are stable) and surface "reconnecting your account" instead of
  a stalled turn; the covering test goes through `routeNotification` rather than
  calling `handleNotification` directly. A future unrouted-but-declared
  notification must fail a test, not produce a Debug line. Closes **F12**, and
  closes MADR 0137 F9 properly.
* **D6 — Add `functionCallOutput` to the item registries** and refresh
  `item_test.go`'s enumeration to the installed version. Closes **F13**.
* **D7 — Migrate history to the paginated contract and drop the opt-in for it.**
  `thread/items/list`, `thread/turns/list` and `thread/revert` are stable now:
  make them the primary path, stop calling `thread/read` with `includeTurns:
  true`, adopt `excludeTurns` plus the two backwards cursors on
  resume/fork, and treat `historyMode: paginated` as the default it now is. This
  also retires the per-call `deprecationNotice` we currently earn. Closes **F19**;
  takes the promotions in **F9**. Note this is the same migration as MADR 0141's
  unreachable paging — it should be finished here, not re-planned.
* **D8 — Distinguish the two new error signals on the phone.**
  `rateLimitExceeded` ("you are out of credit") must not read as
  `usageLimitExceeded` ("back off and retry"), and `TurnError.misalignment` should
  render `detailedExplanation` with its `steer.message` offered as a one-tap
  continuation. Closes **F23**.
* **D9 — Keep the experimental opt-in, and make its detection exact.** 13 of our
  methods still require it (**F10**), so the negotiation stays; but the rejection
  test stops being a `(?i)experimental` regex over free text and keys on the
  structured `data.capability` field, with the message match retained only as a
  logged fallback. Whether the second process can be avoided is left to the PLAN.
  Bounds **F11**.
* **D10 — `-32601` must stop being permanent.** A capability disabled by a
  method-not-found or invalid-params response is re-probed at the next natural
  boundary rather than staying off for the engine's lifetime. Closes **F15**.
* **D11 — Windows sandbox hygiene, four parts.** (a) Export `USERNAME`
  explicitly in the environment we give the engine, as defence in depth against
  the `"Administrators"` fallback even though 0.155.1 fixes it (**F28**). (b)
  Never call `windowsSandbox/setupStart`; use `windowsSandbox/readiness` when we
  need to know. (c) Document the repair path, because Codex has none: orphaned
  deny ACEs must be stripped with `icacls`, and `deny_read_acl_state.json` being
  empty means nothing will ever revoke them (**F30**). (d) Audit the two deny
  sources — the permission profile's globs and
  `%ProgramData%\OpenAI\Codex\requirements.toml` — and check the ACL on that
  directory, since Codex measures but does not prevent a standard user
  pre-creating it (**F29**).
* **D12 — Treat the managed daemon on Windows as newly *possible*, not newly
  *enabled*.** Retire the platform refusal as a statement about Codex
  (`config.go:116-118` is now factually wrong), but keep the transport off by
  default until it has live coverage, and record its three constraints in the
  code that builds it: non-elevated launch with detached children permitted, the
  108-byte AF_UNIX path limit, and no per-client environment isolation.
  Bounds **F20**.
* **D13 — If we ever manage a daemon, its settings are written to disk before
  bootstrap.** `{"updater":{"autoUpdateEnabled":false}}` at minimum, because
  automatic updates otherwise restart the server under a connected phone with no
  in-band notice and a 50 ms retry that does not wait for idle; and
  `thread_unload_delay_secs` must be chosen deliberately, since its 60 s default
  is how long a backgrounded phone may be away. Closes **F24**.
* **D14 — Lean on upstream where the fix is measured.** Pass `timeoutMs` on
  `thread/shellCommand` instead of only our own timeout (**F25**); surface the new
  `McpServerStatus.runtimeStatus`/`toolsError` so an unhealthy MCP server can be
  explained; rely on the shared `Retry-After` handling rather than adding more
  hand-rolled backoff.
* **D15 — Keep, and annotate with evidence, the workarounds upstream has not
  fixed.** Device login is still destructive (**F32**), grandchildren still escape
  the job object and Codex has no console-signal mechanism at all (**F31**), and
  `model/list` still rejects a missing `params` object. Each keeps its comment,
  updated to say "still true at 0.155.1" with the evidence, so the next audit does
  not re-derive it.
* **D16 — Advertise the negotiated surface, not the embedded manifest.** What the
  phone is told a provider can do must derive from the binary in use and the
  capabilities actually negotiated, with the manifest as the schema of record
  rather than the answer. Closes the user-visible half of **F3**.
* **D17 — Never present advisory managed policy as enforced.** If we surface
  browser or computer-use capability at all, we enforce an administrator's denial
  ourselves, because Codex discards those three requirements (**F34**); and we
  state plainly that `additional_developer_instructions` cannot be suppressed by
  us.
* **D18 — Binary identity records which install answered.** Two Codex installs
  at different versions coexist on the reference host (**probe 1**), so the
  resolved path is part of the identity we log and pin, not just the version and
  SHA.
* **D19 — Generate from schemas, never from prose.** Codex deleted the
  app-server README's protocol documentation and its `docs/` tree is frozen
  (**F27**); the contract pipeline's inputs are the schema bundles and
  `config.schema.json`. Where a source checkout is used, it must be at the tag of
  the installed binary — the schema blobs are committed, so a stale tree yields a
  stale contract silently.

### Consequences

* Good, because the expensive half is already done: the surface is measured, the
  delta is known to be non-breaking, and every defect below is small and local.
* Good, because D1 and D5–D6 are hours of work against user-visible correctness,
  and D3 converts a dead gate into a signal that can run in CI.
* Good, because three findings (F22, F28, F29) are safety-relevant rather than
  cosmetic, and none of them was known before this audit.
* Bad, because D7 touches the transcript and resume paths — the two places where a
  regression is most visible to the owner — and MADR 0141 shows this migration has
  stalled once already.
* Bad, because re-pinning to 0.155.1 pins to a version that is *already* behind:
  `rust-v0.156.0-alpha.13` exists upstream. D3 exists so that being behind is
  visible and tolerable rather than fatal.
* Neutral, because the large new surfaces (attachments, timeline, voice,
  `userVerification`) are deliberately deferred; this record names them so the
  deferral is a decision.

### Confirmation

```text
go test ./internal/provider/codex/            approval `kind` decoded; functionCallOutput registered
go test ./internal/provider/codex/            every declared notification has a route, asserted via routeNotification
make live-codex-contract                      passes on 0.155.1; fails on a removal/rename/new-required-field
make live-codex                               engine start, thread lifecycle, both sandbox param shapes
make ci-windows && make race                  green
phone, manually                               a writeStdin approval reads as terminal input, not as a command
phone, manually                               out-of-credit reads differently from rate-limited
```

## Pros and Cons of the Options

### A — Re-pin, fix the cheap defects, stage the new surface (chosen)

* Good, because it fixes the consent defect and the silent-drop defects in days,
  not weeks.
* Good, because it makes drift visible permanently, which is the only reason this
  audit was needed at all.
* Good, because the measured non-breaking delta means re-pinning carries low risk.
* Neutral, because it leaves the highest-value new features unbuilt for now.
* Bad, because it still requires touching history paging, the riskiest area.

### B — Full uptake now

* Good, because `thread/attachment/*` would let the phone pin context without us
  inventing storage, and `turn/settings/update` would let a live turn be
  retargeted — both genuinely new product capability.
* Bad, because it multiplies a transcript-path migration by four new subsystems
  while the drift gate is still dead, so a regression would be attributed by
  guesswork.
* Bad, because `userVerification` is a project in its own right (device-bound
  P-256 credentials, challenge signing, failure taxonomy), not a flag.

### C — Freeze

* Good, because it is free and cannot regress anything this week.
* Bad, because the consent defect (F22) and the dropped notifications persist,
  and both are live today.
* Bad, because the deprecation path under `thread/read` means freezing chooses a
  future forced migration over a chosen one.

### D — Abandon the generated contract

* Good, because it removes the machinery whose brittleness caused this situation.
* Bad, because the machinery is not what failed — its pinning discipline did. The
  generated inventory is what made this audit precise, and hand-maintenance is
  exactly how the 75-vs-82 notification gap arose.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Two installs, different versions | measured: `%APPDATA%\npm\codex.cmd` → `0.155.1`; `%LOCALAPPDATA%\OpenAI\Codex\bin\…\codex.exe` → `0.154.0-alpha.6.2` |
| Our daemon drives the npm one | measured: live process tree, `mcremote.exe → cmd.exe /d /s /v:off /c … codex.cmd app-server --listen stdio://` |
| The owner's checkout is not the installed version | measured: `ac192cd7`, `behind 750`; worktree created at `rust-v0.155.1` = `be2951ea3` |
| No merge base; the commit range is half the delta | measured: `git merge-base` exits 1; tag depths 9581 vs 266 |
| The drift gate fails today | measured: `live_contract_test.go:44`, FAIL in 4.14 s |
| Zero removals / renames / new required fields | measured three ways: binary schema export, Rust macro inventory, committed `.zst` blobs at both tags |
| 191 pinned `required_params` unchanged | measured against `testdata/0.149.1/fixtures.json` |
| Surface counts 95→102 / 75→82 / 10 / 150→164 / 11 | measured, `oneOf[].properties.method.enum[0]` |
| `initialize` byte-identical, no auth needed, no capabilities echo | measured live + `protocol/v1.rs:27-80` diff empty |
| `experimentalApi` is the runtime gate | measured both ways: `-32600 "… requires experimentalApi capability"` vs a populated result |
| 45 stable / 13 experimental / 1 undeclared of the 59 we send | measured against the export |
| 82 declared notifications, 75 routed, 7 dropped, 0 stale | measured against `routing.go` |
| Exporter never prunes experimental notifications | measured: `filter_experimental_schema` body in `export.rs` |
| `gitDiffToRemote` is v1, stripped from bundles, still dispatched | measured: `export.rs:75-76`, `:1429-1430`; `message_processor.rs:1773` |
| `functionCallOutput` unhandled | measured: 19 `ThreadItem` variants, 18 named in `items.go` |
| Approval `kind` unread by us | measured: no `ApprovalKind` reference; only `process/writeStdin` at `execution.go:698` |
| We never decode `call_id`; never read the SQLite DB | measured: zero non-test hits |
| `gpt-5.4-mini` only in a test fixture | measured: `thinking_test.go:36,92` |
| Paginated is the new durable default; full hydration deprecated | source: `thread_processor.rs` history-mode default (#40677); notices (#40676) |
| Promotions to stable | source: #40673; confirmed by the stability diff |
| Windows managed daemon supported | source: `app-server-daemon/README.md` diff, 0.149.1 vs 0.155.1 |
| Windows shutdown moved file→socket, 403 without the env var | source: #43308; `unix_socket.rs:140-156`; `app-server/src/lib.rs:750-758` |
| `USERNAME` → `"Administrators"` fallback, and its fix | source: `setup.rs:351`/`:1097` before-and-after |
| No default deny names `config.toml` | source: `deny_read_resolver.rs:32`, `permission_profile_catalog.rs`; tree-wide grep |
| `%ProgramData%` squatting measured, not prevented | source: #44284, `config/src/loader/windows.rs` probe docstring |
| No console-signal mechanism anywhere in Codex | measured: tree-wide grep at 0.155.1 finds none of the five Win32 console-signal symbols |
| Device login still destructive | source: `cli/src/login.rs:335`; `device_code_auth.rs` absent from the tree diff |
| Auto-update ON, 5 min then hourly, 50 ms busy retry | source: `app-server-daemon` update loop + `settings.rs` defaults |
| `thread_unload_delay_secs` default 60 s | source: `config/src/config_toml.rs:322-325` |
| Browser/computer-use requirements discarded | measured: `config_requirements.rs` ~`:1678-1683`, `: _` bindings |
| Four managed fields experimental-gated yet sent | measured: `protocol/v2/config.rs:416,431,434,436` |
| app-server README 2570→115; `docs/` byte-identical | source: #43421; identical `docs/` tree hash at both tags |

### Related records

Cited rather than restated: **0028** (the foundational Codex provider record),
**0035**/**0036** (item-stream fidelity, vocabularies), **0043** (model catalog,
the `data`/`params` bug), **0044**/**0047** (modes and the two sandbox param
shapes), **0048** (sandbox namespace failure), **0051** (transcript noise,
sub-agents), **0052** (thinking levels), **0074**/**0133**/**0134**/**0135**/
**0136** (credentials, reality, manifest refusal), **0080**/**0109** (app-server
parity and the contract manifest this record audits), **0137** (latency, wire
fixtures, the 83-vs-75 notification gap), **0138** (history budget, the codex
append tool lane), **0141** (unreachable history paging — D7 finishes it),
**0146** (Purge teardown, still `proposed` — F16), **0150** (the Windows
tree-kill guarantee), **0151** (wirecap redaction), **0159** (the `.cmd` shim
argv guard our engine spawn depends on).

### Open questions for the plan

1. **Codex now ships its own remote control.** `codex remote-control
   start|stop|pair --json`, `app-server daemon bootstrap --remote-control`, a WS
   listener with `capability-token` / `signed-bearer-token` auth and JWT
   issuer/audience/clock-skew validation, plus `remoteControl/*` methods and
   client revocation. That overlaps this product's core. Is it a competitor to
   route around, a transport to adopt, or an integration to expose? This is an
   owner-level product question, and it is the most consequential thing in the
   audit that is not a defect.
2. **Is the second process in the experimental negotiation avoidable?** The
   server never echoes accepted capabilities, so a single-process probe needs
   either a cheap gated call or acceptance that we always opt in and handle
   `-32600` per call.
3. **Can `getAuthStatus` replace part of the 1.4 s `codex doctor --json` probe?**
   It returns `{authMethod, authToken, requiresOpenaiAuth}`, but whether it
   distinguishes signed-out from broken-external-store is **[unverified]**, and it
   can return a token we would rather not request.
4. **Did our own engine spawn trigger the 2026-09-19 ACL damage?** F28 gives the
   mechanism and our topology matches it, but the causal link is
   **[unverified]** — the sandbox log shows a setup at 10:02:18 with
   `cwd=C:\Users\macsm\gitrepos` and we do not call `setupStart`. Worth settling,
   because if a session start can trigger provisioning, D11 needs to be stronger.
5. **On-disk formats are undetermined.** `codex-rs/thread-store` moved ~9100
   lines including a rollout-migration rewrite. No `CODEX_HOME` entry was removed,
   but whether `auth.json`, the credential store or rollout files changed shape is
   unknown. Anything of ours that reads a session file directly deserves its own
   pass before we trust it.
6. **Attribution for 2026-09-04 → 09-06 is unavailable in this clone** [GAP],
   covering the whole `windows-sandbox-service` crate, the Windows daemon backend
   files, the `mcp-server` deletion and several config keys. An unshallowed clone
   would close it if we ever need the PR history.
