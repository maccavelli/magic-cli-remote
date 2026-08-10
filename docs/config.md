# mcremote configuration

## Locations (XDG)

Linux **and** macOS use the [XDG Base Directory
Specification](https://specifications.freedesktop.org/basedir-spec/0.8/) for
product paths (MADR 0059). macOS does **not** use
`~/Library/Application Support` for config/data. Relative `$XDG_*` values are
ignored with a diagnostic; product env overrides (`MCREMOTE_CONFIG`,
`MCREMOTE_DATA_DIR`) must be absolute. CLI path flags may be relative to CWD.
Relative YAML filesystem fields resolve against the directory containing the
loaded config file. There is no `${HOME}` / tilde expansion in YAML.

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcremote` or `~/.config/mcremote` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcremote` or `~/.local/share/mcremote` |
| State dir | `$XDG_STATE_HOME/mcremote` or `~/.local/state/mcremote` |
| Cache dir | `$XDG_CACHE_HOME/mcremote` or `~/.cache/mcremote` (disposable) |
| Runtime dir | under `$XDG_RUNTIME_DIR/mcremote/<instance-key>` (or secure fallback) |
| Admin socket | `<runtime_dir>/admin.sock` (not under data dir) |
| Engine registry | `<state_dir>/instances/<instance-key>/engines/` |
| Devices | `<data_dir>/devices.json` (mode `0600`) |
| Pair codes | `<data_dir>/pair_codes.json` (mode `0600`) |
| TLS certificate (selfsigned) | `<data_dir>/tls.crt` (mode `0600`) |
| TLS private key (selfsigned) | `<data_dir>/tls.key` (mode `0600`) |
| ACME storage (letsencrypt) | `<data_dir>/acme/` (durable — not cache) |
| User unit (Linux) | `~/.config/systemd/user/mcremote.service` |
| LaunchAgent (macOS) | `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` |
| LaunchAgent logs (macOS) | `~/Library/Logs/mcremote/` (stdio only) |

Inspect the effective layout (no mutation):

```text
mcremote paths
mcremote paths --json
```

Override config path: `--config /path/to.yaml` or absolute `MCREMOTE_CONFIG`.

`mcremote setup-service` writes a **default** `config.yaml` into the config dir
when missing (0600, never overwrites an existing file) and bakes that path into
the unit’s `ExecStart`. The service exports the same absolute `XDG_*` roots the
daemon resolves.

### Platform constraints (not silent failures)

| Constraint | Linux | macOS |
|------------|-------|-------|
| Survive logout | Optional `loginctl enable-linger` | User LaunchAgent ends at logout |
| Privileged ports 80/443 | Capabilities / proxy / DNS-01 | Proxy / redirection / DNS-01 |

## Precedence

1. CLI flags (`--listen-host`, …)  
2. Environment (`MCREMOTE_*`)  
3. Config file  
4. Built-in defaults  

## Defaults

Values match `config.Defaults()` in `internal/config/config.go`. Keep
`configs/*.yaml` in sync when keys change.

| Key | Default |
|-----|---------|
| `listen.host` | `127.0.0.1` (the `Defaults()` value; mesh launch paths set `tailscale`) |
| `listen.port` | `7531` |
| `tls.mode` | *(empty — `letsencrypt` when `tls.letsencrypt.domains` + `email` are set, else `selfsigned`)* |
| `tls.enabled` | `true` (legacy switch; `false` == `tls.mode: off`) |
| `tls.cert_file` / `tls.key_file` | *(empty — generate and manage automatically; `selfsigned` only)* |
| `tls.letsencrypt.domains` | *(empty)* |
| `tls.letsencrypt.email` | *(empty)* |
| `tls.letsencrypt.directory_url` | *(empty — Let's Encrypt production)* |
| `tls.letsencrypt.staging` | `false` |
| `tls.letsencrypt.cache_dir` | *(empty — `<data_dir>/acme`)* |
| `tls.letsencrypt.route53.hosted_zone_id` | *(empty — discovered from the zone name)* |
| `tls.letsencrypt.route53.region` | *(empty — `AWS_REGION`)* |
| `tls.letsencrypt.route53.profile` | *(empty — `AWS_PROFILE`)* |
| `tls.letsencrypt.route53.max_retries` | `0` (AWS SDK default) |
| `log.level` | `info` |
| `log.format` | `text` |
| `data_dir` | *(empty — XDG data home)* |
| `auth.require_device_token` | `true` |
| `auth.require_client_key` | `true` — tokens bound to the device's enrolled TLS client key (ADR 0005); keyless legacy devices must re-pair |
| `auth.allowed_origins` | `[]` — browser Origin allowlist for the WS upgrade; empty is the secure baseline (native clients + same-origin accepted, cross-origin rejected). Never `"*"` |
| `providers.fake.enabled` | `false` (dev/smoke only) |
| `providers.grok.enabled` | `true` |
| `providers.grok.bin` | `grok` |
| `providers.grok.args` | `[]` — empty uses built-in `agent --no-leader stdio` (+ `-m MODEL` when set) |
| `providers.grok.always_approve` | `false` |
| `providers.grok.default_cwd` | *(empty — sessions start in the daemon user's home directory)* |
| `providers.grok.model` | *(empty)* |
| `providers.grok.reasoning_effort` | *(empty — pass `--reasoning-effort <EFFORT>` to `grok agent` when non-empty, e.g. `low`, `medium`, `high`)* |
| `providers.grok.permission_mode` | **`default`** — passed as `--permission-mode <MODE>`. Valid: `default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`; empty inherits grok's own config. Rejected at load if unrecognised. **Process-wide and launch-scoped**: applies to every grok session, changing it needs an engine restart. Distinct from the per-session `auto` **mode** in the app's mode menu, which is daemon-enforced (MADR 0049). See the note below on why this is pinned. |
| `providers.grok.sandbox` | *(empty — grok's own default)* — OS-level sandbox profile (`--sandbox <PROFILE>`). Built-ins: `off`, `workspace`, `devbox`, `read-only`, `strict`. Any other value is treated as a custom profile name and resolved by grok from `~/.grok/sandbox.toml` or `.grok/sandbox.toml`; grok errors clearly if it is missing. Also settable via the `GROK_SANDBOX` env var. See the sandbox note below. |
| `providers.grok.allowed_tools` | `[]` — whitelist of built-in tool names (`--tools a,b`). **Measured no-op for remote sessions** — see the note below |
| `providers.grok.disallowed_tools` | `[]` — blacklist of built-in tool names (`--disallowed-tools a,b`). **Measured no-op for remote sessions** |
| `providers.grok.allow_rules` | `[]` — persistent permission allow rules (`--allow <rule>`). **Measured no-op for remote sessions** |
| `providers.grok.deny_rules` | `[]` — persistent permission deny rules (`--deny <rule>`). **Measured no-op for remote sessions** |
| `providers.grok.no_subagents` | `false` — disable subagent spawning (`--no-subagents`) |
| `providers.grok.disable_web_search` | `false` — disable built-in web search (`--disable-web-search`) |
| `providers.grok.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.grok.prewarm` | `true` — keep one spare initialized agent (Phase 4.2); disable if memory is tight |
| `providers.grok.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.grok.stream_coalesce_ms` | `80` — hold assistant/thought text this long so it ships as one event instead of one per model token (MADR 0024 / 0057). `0` = one event per token; max `1000` |
| `providers.grok.fs_roots` | `[]` — confine the agent's `fs/read_text_file` & `fs/write_text_file` callbacks to these roots (plus the session cwd). Empty = unrestricted. Defense-in-depth/audit only: the agent has terminal access as the same user, so this is not a sandbox |
| `providers.grok.auth_method_id` | *(empty)* — ACP auth method to invoke if the agent reports it needs authentication |
| `providers.grok.mcp_servers` | `[]` — MCP servers advertised to the agent (config-file only; not env/flags) |
| `providers.goose.enabled` | `true` — pick Goose per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.goose.bin` | `goose` |
| `providers.goose.always_approve` | `false` |
| `providers.goose.default_cwd` | *(empty — sessions start in the daemon user's home directory)* |
| `providers.goose.model` | *(empty — Goose's own default)* |
| `providers.goose.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.goose.prewarm` | `false` — when `true`, boot the shared `goose serve` engine at daemon start; default lazy-boots on first use |
| `providers.goose.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.goose.stream_coalesce_ms` | `80` — same coalescing as other providers (MADR 0024). `0` = one event per token; max `1000` |
| `providers.goose.auth_method_id` | *(empty)* — ACP auth method if advertised at initialize |
| `providers.goose.with_builtins` | `[]` — named Goose built-ins to enable on the shared engine (typed list, not free-form argv; no duplicates/empty entries) |
| `providers.goose.mcp_servers` | `[]` — MCP servers (config-file only) |
| `providers.opencode.enabled` | `true` — pick OpenCode per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.opencode.bin` | `opencode` |
| `providers.opencode.always_approve` | `false` |
| `providers.opencode.default_cwd` | *(empty — sessions start in the daemon user's home directory)* |
| `providers.opencode.model` | *(empty — OpenCode's own default; pin e.g. `opencode/deepseek-v4-flash-free` or `anthropic/claude-haiku-4-5`)* |
| `providers.opencode.pure` | `false` — run `opencode serve` with `--pure` (without loading external third-party plugins) |
| `providers.opencode.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.opencode.prewarm` | `true` — boot the shared `opencode serve` engine at daemon start so the first session create is instant. `false` boots it lazily on first use (~3–5s) and holds no idle engine (~250MB) |
| `providers.opencode.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.opencode.stream_coalesce_ms` | `80` — hold assistant/thought text this long so it ships as one event instead of one per model token (MADR 0024), capping mid-stream updates at ~12/s. The first chunk of a reply and the tail before any control event are never delayed, so time-to-first-token and end-of-turn latency are unchanged. `0` = one event per token (pre-0024 behaviour); max `1000` |
| `providers.opencode.session_tree` | `true` — multi-agent session-tree demux (child aliases, tree-idle EndTurn, child fan-in; MADR 0020 KD11). `false` = exact pre-0020 kill switch (parent-only). When `true`, OpenCode must report version **≥ 1.18.0** on `/global/health` (KD10) or session create fails |
| `providers.codex.enabled` | `true` — pick Codex per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.codex.bin` | `codex` |
| `providers.codex.always_approve` | `false` |
| `providers.codex.default_cwd` | *(empty — sessions start in the daemon user's home directory)* |
| `providers.codex.model` | *(empty — Codex's own default from `~/.codex/config.toml`; pin e.g. `gpt-5.6-terra`)* |
| `providers.codex.permission_timeout_seconds` | `900` (`0` = wait forever) — longer than other providers because Codex sandboxed tools may run for minutes |
| `providers.codex.prewarm` | `false` — boot the shared `codex app-server` engine at daemon start so the first session create skips the ~500ms cold start. `true` pre-warms; `false` boots lazily on first use |
| `providers.codex.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off). Match other providers; `0` left phones looking frozen during multi-minute tools after a WS blip (MADR 0072 D1) |
| `providers.codex.stream_coalesce_ms` | `80` — same coalescing as other providers (MADR 0024). `0` = one event per token; max `1000` |
| `providers.codex.approval_policy` | *(empty — mcremote `default` session mode: `on-request`)*. Valid: `untrusted`, `on-request`, `never`. Empty with empty sandbox seeds the normal mode pair (MADR 0047); `never` alone is repaired to auto (`never` + `workspace-write`). Set **both** fields to pin a custom pair |
| `providers.codex.sandbox_mode` | *(empty — mcremote `default` session mode: `workspace-write`)*. Valid: `read-only`, `workspace-write`, `danger-full-access`. See approval_policy; both empty → default mode, not silent engine-file inheritance for remote sessions |
| `providers.codex.allow_full_access` | `false` — advertise the `full-access` session mode (no approval prompts **and** no sandbox). Opt-in; see [MADR 0044](./0044-MADR-auto-approve-modes.md) D5 |
| `providers.codex.sandbox_broken_policy` | `warn` (default) — when the daemon's workspace-write probe fails (Linux userns/bwrap): `warn` = notice only; `require_full_access` = seed full-access (needs `allow_full_access: true`) or fail create; `refuse` = fail create. See [MADR 0048](./0048-MADR-codex-sandbox-namespace.md) |
| `providers.kilo.enabled` | `true` — default-on since MADR 0075 acceptance (2026-08-10); set `false` per host to drop it from the phone's new-session provider menu. Known-good CLI: **kilo 7.4.20** (`npm i -g @kilocode/cli` or `brew install Kilo-Org/tap/kilo`) |
| `providers.kilo.bin` | `kilo` |
| `providers.kilo.always_approve` | `false` |
| `providers.kilo.default_cwd` | *(empty — sessions start in the daemon user's home directory)* |
| `providers.kilo.model` | *(empty — Kilo's own default, which is Gateway-auth-state-dependent: `kilo-auto/free` logged out, `kilo-auto/balanced` with a Gateway session)*. Pin `providerID/modelID` split on the **first** slash — Kilo model ids may contain slashes: `kilo/kilo-auto/free`, `openrouter/openrouter/free`, `kilo/~anthropic/claude-sonnet-4-5` |
| `providers.kilo.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.kilo.prewarm` | `true` — boot the shared `kilo serve` engine at daemon start so the first session create is instant; `false` boots lazily on first use (Bun-class cold start) |
| `providers.kilo.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.kilo.stream_coalesce_ms` | `80` — same coalescing as other providers (MADR 0024). `0` = one event per token; max `1000` |
| `providers.kilo.session_tree` | `false` — stays off until child-session SSE fixtures prove tree demux on the kilo fork (MADR 0075 Q7); flip is config-only once proven |
| `providers.kilo.pure` | `false` — run `kilo serve` with `--pure` (without loading external third-party plugins) |
| `headscale.control_url` | `http://localhost:8080` |
| `limits.max_ws_clients` | `8` (simultaneous WebSocket clients; `0` falls back to default 8 via `Resolved()`) |
| `limits.max_live_sessions` | `16` (concurrent live agent sessions; `0` falls back to default 16) |
| `limits.ws_read_deadline_seconds` | `120` — rolling authenticated-WS read deadline (floor 15; `0` → 120). Advertised in v2 caps (MADR 0068 / 0072 D2) |
| `limits.ws_resume_window_seconds` | `120` — v2 resume-token validity after issue (`0` → 120) |
| `relay.url` | *(empty — outbound mcrelay disabled)* |
| `relay.host_id` | *(empty)* — public id for join routing (`hid=` in pair URI) |
| `relay.secret` | *(empty)* — registration secret (min 16); prefer env. Required for **serve** registration only; `pair` can advertise url+host_id without the secret in-process |
| `relay.insecure_skip_verify` | `false` — skip TLS verify of **mcrelay** only (dev) |
| `pair.advertise_host` | *(empty — auto-detect: Tailscale IPv4, else loopback)* — host (or host:port) advertised in the pair QR/URI. A bare host inherits `listen.port`. Ignored in `letsencrypt` mode (the ACME domain is used); `mcremote pair --host` overrides per run |
| `receipts.enabled` | `false` — signed receipts for permission decisions are opt-in (MADR 0077). See `docs/receipts.md` |
| `receipts.allow_patterns` | `[]` — shell-glob patterns (`*`, `?`, `[set]`) matched against `"<tool_name> <detail>"`; a match triggers a device-signed, hash-chained receipt for that decision |
| `receipts.deny_patterns` | `[]` — same syntax; a match here wins over `allow_patterns` on the same decision |
| `receipts.handoffs` | `true` — when receipts are enabled, sign a receipt for each device-to-device session handoff (release + claim, MADR 0078). Only consulted when `receipts.enabled`. The handoff feature is always available; this gates only its attestation. |

### Grok tool allow/deny lists do not apply to remote sessions

`allowed_tools`, `disallowed_tools`, `allow_rules` and `deny_rules` are
accepted on grok's command line and the agent starts cleanly with them, but
they have **no observable effect on sessions driven over ACP**
(`grok agent stdio`), which is how mcremote runs grok.

Measured against grok **0.2.114** on 2026-07-29, asking the agent to write a
file in each configuration:

| config | result |
|---|---|
| control (no tool policy) | writes the file |
| `disallowed_tools: [Write]` | writes the file |
| `allowed_tools: [Read]` | writes the file |
| `deny_rules: [Write]` / `[Write(*)]` | writes the file |

Repeated under a prompting mode (`permission_mode: default`), all four
configurations produced an identical permission request — the deny rules did
not short-circuit it. grok's own reference marks `--tools`/`--disallowed-tools`
as headless-only, which is consistent with this.

Use `permission_mode`, the per-session `auto` mode, or `sandbox` for policy
that actually binds remote sessions. These four keys are retained because they
are harmless and may become effective in a later grok; re-measure before
relying on them. Tracked in
[MADR 0050](./0050-MADR-grok-cli-surface-drift.md) §4.

### Grok sandbox profiles

`providers.grok.sandbox` maps to grok's own `--sandbox <PROFILE>`, an OS-level
filesystem and network containment layer independent of the permission mode.
Built-in profiles: `off` (grok's default), `workspace`, `devbox`, `read-only`,
`strict`. Anything else is a custom profile name grok resolves from
`~/.grok/sandbox.toml` or `.grok/sandbox.toml`.

Leave it empty to inherit grok's own configuration. The daemon does not
enum-validate the value beyond documenting the built-ins — custom profiles are
a supported grok feature, and a hard-coded enum would break the day grok adds
one. An unknown name surfaces grok's own error:

```text
sandbox profile resolve failed: Custom sandbox profile 'x' not found.
Define it in ~/.grok/sandbox.toml or .grok/sandbox.toml
```

On Linux hosts where AppArmor restricts unprivileged user namespaces, a
containment profile may hit the same wall as the codex sandbox — see the
section below and [MADR 0048](./0048-MADR-codex-sandbox-namespace.md).

### Grok permission mode is pinned, not inherited

`providers.grok.permission_mode` defaults to **`default`** rather than empty.

Empty means "whatever this host's grok resolves to" — `~/.grok/config.toml`,
project config, or (since grok 0.2.102) **fleet-wide remote config when no
local setting exists**. The daemon cannot see any of those, yet it advertises
session modes and a `dangerous` flag as though it knows the session's approval
posture. On a host that resolves to something permissive, the phone shows a
mode chip for a policy nobody set and the agent never asks for anything.

Pinning `default` makes grok ask, which is what gives the mode chip, the
`dangerous` flag and the per-session `auto` mode (MADR 0049) their meaning.

**Upgrade note — this is a behaviour change.** Grok sessions on hosts that were
silently permissive will start prompting for approvals. To keep the previous
behaviour, choose one:

- `providers.grok.permission_mode: bypassPermissions` — process-wide, no prompts;
- switch the session to the **`auto`** mode from the app's mode menu — the
  supported per-session answer, gated behind a confirmation;
- `providers.grok.permission_mode: ""` — inherit grok's own config again, with
  the caveats above.

Background: [MADR 0050](./0050-MADR-grok-cli-surface-drift.md) D3.

### Codex sandbox: unprivileged user namespaces (Ubuntu 24.04+)

Codex runs every sandboxed shell command and `apply_patch` through
**bubblewrap**, which needs an unprivileged user namespace. On Ubuntu 24.04+
AppArmor restricts those by default, and the symptom is that the agent looks
healthy but cannot write:

```text
bwrap: No permissions to create a new namespace, likely because the kernel
does not allow non-privileged user namespaces.
```

The mode chip still says `default` or `auto`, the policy is correct on the
wire, and every edit fails. Only `danger-full-access` (no sandbox) works.

Check the host — and note that the obvious check is wrong:

```bash
unshare -Ur true    # fails ⇒ restricted. THIS is the discriminating check
bwrap --unshare-user --ro-bind / / true    # passes even when broken — do not use
```

`bwrap` ships its own AppArmor profiles so it never transitions into the
restrictive one; codex fails because it reaches bwrap through node, which does.
Confirm with `sudo dmesg | grep -i 'apparmor.*DENIED.*unprivileged_userns'`.

To fix:

```bash
sh scripts/bwrap-apparmor-fix.sh
# the daemon and its agent children inherit the old policy until restarted
systemctl --user restart mcremote
```

That sets `kernel.apparmor_restrict_unprivileged_userns=0` and persists it to
`/etc/sysctl.d/60-mcremote-userns.conf`. **It is a host-wide security
loosening** — it restores pre-24.04 behaviour where any unprivileged user can
create user namespaces — so decide deliberately on a shared machine. Undo:

```bash
sudo rm /etc/sysctl.d/60-mcremote-userns.conf
sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=1
```

Granting the capability back inside the AppArmor profile (a
`local/unprivileged_userns` override) does **not** work: the profile's
`audit deny capability,` wins, because in AppArmor deny always beats allow.
Background and the narrower per-binary-profile alternative:
[MADR 0048](./0048-MADR-codex-sandbox-namespace.md) §2.1.1–§2.1.2.

If you cannot change host policy, set `providers.codex.allow_full_access: true`
and use the `full-access` session mode — auto-approve with no sandbox, so only
on a machine where that is acceptable. For a daemon-side escape hatch when the
probe fails every time:

```yaml
providers:
  codex:
    allow_full_access: true
    sandbox_broken_policy: require_full_access  # or refuse to block create
```

Prefer fixing the host over permanent full-access on shared machines. The
daemon probes at engine start and emits a session notice under `warn` (default)
so the phone is not left with a silent write-failure loop.

### `listen.host: tailscale`

`listen.host` accepts the sentinel **`tailscale`**. At startup the daemon
replaces it with this host's Tailscale IPv4 (`tailscale ip -4`), so the listener
binds the mesh interface only and nothing else on the machine's networks can
reach 7531.

It **fails closed**: if no Tailscale IPv4 can be found, `serve` exits with an
error naming the fix. It never falls back to `0.0.0.0`.

`0.0.0.0` remains available as an explicit opt-in for serving clients that are
not on the tailnet; the daemon logs a warning when it is used. It is no longer
the default anywhere.

> **Note:** The shipped mesh configs (`configs/config.mesh-grok.yaml`,
> `config.prod.example.yaml`) and `scripts/start-mcremote-grok.sh` set
> `listen.host` / `--listen-host` to **`tailscale`**. Interactive `serve`
> without flags or a config file still defaults to `127.0.0.1`.
> `setup-service` does **not** bake listen flags into the unit unless you pass
> `--listen-host` / `--listen-port` — the unit then follows config.yaml.

## Environment variables

All use the `MCREMOTE_` prefix. Nested YAML keys use underscores.

| Variable | Maps to | Description |
|----------|---------|-------------|
| `MCREMOTE_CONFIG` | config file path | Explicit config YAML path |
| `MCREMOTE_LISTEN_HOST` | `listen.host` | Bind address; `tailscale` binds the tailnet IPv4 only |
| `MCREMOTE_LISTEN_PORT` | `listen.port` | Bind port (1–65535) |
| `MCREMOTE_LOG_LEVEL` | `log.level` | `debug` \| `info` \| `warn` \| `error` |
| `MCREMOTE_LOG_FORMAT` | `log.format` | `text` \| `json` |
| `MCREMOTE_DATA_DIR` | `data_dir` | Devices, pair codes, session meta |
| `MCREMOTE_AUTH_REQUIRE_DEVICE_TOKEN` | `auth.require_device_token` | Require device token on WebSocket |
| `MCREMOTE_AUTH_REQUIRE_CLIENT_KEY` | `auth.require_client_key` | Require enrolled TLS client key |
| `MCREMOTE_AUTH_ALLOWED_ORIGINS` | `auth.allowed_origins` | Comma-separated browser Origin allowlist |
| `MCREMOTE_TLS_ENABLED` | `tls.enabled` | Serve HTTPS/WSS (`true`/`false`) |
| `MCREMOTE_TLS_CERT_FILE` | `tls.cert_file` | Operator-managed certificate (with key file) |
| `MCREMOTE_TLS_KEY_FILE` | `tls.key_file` | Operator-managed private key (with cert file) |
| `MCREMOTE_TLS_MODE` | `tls.mode` | `letsencrypt` \| `selfsigned` \| `off` |
| `MCREMOTE_TLS_DOMAINS` | `tls.letsencrypt.domains` | Comma-separated DNS names to request |
| `MCREMOTE_TLS_EMAIL` | `tls.letsencrypt.email` | ACME account contact |
| `MCREMOTE_TLS_ACME_DIRECTORY_URL` | `tls.letsencrypt.directory_url` | ACME directory URL |
| `MCREMOTE_TLS_ACME_STAGING` | `tls.letsencrypt.staging` | Use the Let's Encrypt staging CA |
| `MCREMOTE_TLS_ACME_CACHE_DIR` | `tls.letsencrypt.cache_dir` | ACME storage dir |
| `MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID` | `tls.letsencrypt.route53.hosted_zone_id` | Route 53 hosted zone ID |
| `MCREMOTE_TLS_ROUTE53_REGION` | `tls.letsencrypt.route53.region` | AWS region |
| `MCREMOTE_TLS_ROUTE53_PROFILE` | `tls.letsencrypt.route53.profile` | AWS shared-config profile |
| `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` | `tls.letsencrypt.route53.max_retries` | AWS API max retries (`0` = SDK default). Unlike the three above, this key has **no** short `MCREMOTE_TLS_ROUTE53_*` alias — it is reachable only by its full path. The short spelling is silently ignored. |
| `MCREMOTE_PROVIDERS_GROK_ENABLED` | `providers.grok.enabled` | Enable Grok provider (`true`/`false`) |
| `MCREMOTE_PROVIDERS_GROK_BIN` | `providers.grok.bin` | Grok executable path |
| `MCREMOTE_PROVIDERS_GROK_ALWAYS_APPROVE` | `providers.grok.always_approve` | Auto-approve Grok tool requests |
| `MCREMOTE_PROVIDERS_GROK_DEFAULT_CWD` | `providers.grok.default_cwd` | Grok fallback session CWD |
| `MCREMOTE_PROVIDERS_GROK_MODEL` | `providers.grok.model` | Grok model override |
| `MCREMOTE_PROVIDERS_GROK_REASONING_EFFORT` | `providers.grok.reasoning_effort` | Grok reasoning effort level |
| `MCREMOTE_PROVIDERS_GROK_PERMISSION_MODE` | `providers.grok.permission_mode` | Grok permission mode (`default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`) |
| `MCREMOTE_PROVIDERS_GROK_NO_SUBAGENTS` | `providers.grok.no_subagents` | Disable subagent spawning (`true`/`false`) |
| `MCREMOTE_PROVIDERS_GROK_DISABLE_WEB_SEARCH` | `providers.grok.disable_web_search` | Disable built-in web search (`true`/`false`) |
| `MCREMOTE_PROVIDERS_GROK_PERMISSION_TIMEOUT_SECONDS` | `providers.grok.permission_timeout_seconds` | Grok permission decision timeout seconds |
| `MCREMOTE_PROVIDERS_GROK_PREWARM` | `providers.grok.prewarm` | Pre-warm Grok agent instance |
| `MCREMOTE_PROVIDERS_GROK_TURN_STALL_NOTICE_SECONDS` | `providers.grok.turn_stall_notice_seconds` | Grok turn stall notice threshold |
| `MCREMOTE_PROVIDERS_GROK_STREAM_COALESCE_MS` | `providers.grok.stream_coalesce_ms` | Grok stream coalescing window (ms) |
| `MCREMOTE_PROVIDERS_GROK_SANDBOX` | `providers.grok.sandbox` | Grok OS-level sandbox profile |
| `MCREMOTE_PROVIDERS_GROK_AUTH_METHOD_ID` | `providers.grok.auth_method_id` | Grok ACP auth method id |
| `MCREMOTE_PROVIDERS_GOOSE_ENABLED` | `providers.goose.enabled` | Enable Goose provider |
| `MCREMOTE_PROVIDERS_GOOSE_BIN` | `providers.goose.bin` | Goose executable path |
| `MCREMOTE_PROVIDERS_GOOSE_ALWAYS_APPROVE` | `providers.goose.always_approve` | Auto-approve Goose tool requests |
| `MCREMOTE_PROVIDERS_GOOSE_DEFAULT_CWD` | `providers.goose.default_cwd` | Goose fallback session CWD |
| `MCREMOTE_PROVIDERS_GOOSE_MODEL` | `providers.goose.model` | Goose model override |
| `MCREMOTE_PROVIDERS_GOOSE_PERMISSION_TIMEOUT_SECONDS` | `providers.goose.permission_timeout_seconds` | Goose permission timeout seconds |
| `MCREMOTE_PROVIDERS_GOOSE_PREWARM` | `providers.goose.prewarm` | Goose prewarm (runtime still lazy-boots) |
| `MCREMOTE_PROVIDERS_GOOSE_TURN_STALL_NOTICE_SECONDS` | `providers.goose.turn_stall_notice_seconds` | Goose turn stall notice threshold |
| `MCREMOTE_PROVIDERS_GOOSE_STREAM_COALESCE_MS` | `providers.goose.stream_coalesce_ms` | Goose stream coalescing window (ms) |
| `MCREMOTE_PROVIDERS_GOOSE_AUTH_METHOD_ID` | `providers.goose.auth_method_id` | Goose ACP auth method id |
| `MCREMOTE_PROVIDERS_OPENCODE_ENABLED` | `providers.opencode.enabled` | Enable OpenCode provider |
| `MCREMOTE_PROVIDERS_OPENCODE_BIN` | `providers.opencode.bin` | OpenCode executable path |
| `MCREMOTE_PROVIDERS_OPENCODE_ALWAYS_APPROVE` | `providers.opencode.always_approve` | Auto-approve OpenCode tool requests |
| `MCREMOTE_PROVIDERS_OPENCODE_MODEL` | `providers.opencode.model` | OpenCode model override |
| `MCREMOTE_PROVIDERS_OPENCODE_PERMISSION_TIMEOUT_SECONDS` | `providers.opencode.permission_timeout_seconds` | OpenCode permission timeout seconds |
| `MCREMOTE_PROVIDERS_OPENCODE_PREWARM` | `providers.opencode.prewarm` | Pre-warm OpenCode serve engine |
| `MCREMOTE_PROVIDERS_OPENCODE_TURN_STALL_NOTICE_SECONDS` | `providers.opencode.turn_stall_notice_seconds` | OpenCode turn stall notice threshold |
| `MCREMOTE_PROVIDERS_OPENCODE_SESSION_TREE` | `providers.opencode.session_tree` | OpenCode multi-agent session tree demux |
| `MCREMOTE_PROVIDERS_OPENCODE_STREAM_COALESCE_MS` | `providers.opencode.stream_coalesce_ms` | OpenCode stream coalescing window (ms) |
| `MCREMOTE_PROVIDERS_OPENCODE_PURE` | `providers.opencode.pure` | OpenCode `--pure` flag |
| `MCREMOTE_PROVIDERS_CODEX_ENABLED` | `providers.codex.enabled` | Enable Codex provider |
| `MCREMOTE_PROVIDERS_CODEX_BIN` | `providers.codex.bin` | Codex executable path |
| `MCREMOTE_PROVIDERS_CODEX_ALWAYS_APPROVE` | `providers.codex.always_approve` | Auto-approve Codex tool requests |
| `MCREMOTE_PROVIDERS_CODEX_MODEL` | `providers.codex.model` | Codex model override |
| `MCREMOTE_PROVIDERS_CODEX_PERMISSION_TIMEOUT_SECONDS` | `providers.codex.permission_timeout_seconds` | Codex permission timeout seconds |
| `MCREMOTE_PROVIDERS_CODEX_PREWARM` | `providers.codex.prewarm` | Pre-warm Codex app-server engine |
| `MCREMOTE_PROVIDERS_CODEX_TURN_STALL_NOTICE_SECONDS` | `providers.codex.turn_stall_notice_seconds` | Codex turn stall notice threshold |
| `MCREMOTE_PROVIDERS_CODEX_STREAM_COALESCE_MS` | `providers.codex.stream_coalesce_ms` | Codex stream coalescing window (ms) |
| `MCREMOTE_PROVIDERS_CODEX_APPROVAL_POLICY` | `providers.codex.approval_policy` | Codex approval policy (`untrusted`, `on-request`, `never`) |
| `MCREMOTE_PROVIDERS_CODEX_SANDBOX_MODE` | `providers.codex.sandbox_mode` | Codex sandbox mode (`read-only`, `workspace-write`, `danger-full-access`) |
| `MCREMOTE_PROVIDERS_CODEX_ALLOW_FULL_ACCESS` | `providers.codex.allow_full_access` | Advertise full-access session mode (`true`/`false`) |
| `MCREMOTE_PROVIDERS_KILO_ENABLED` | `providers.kilo.enabled` | Enable the Kilo provider (`true`/`false`) |
| `MCREMOTE_PROVIDERS_KILO_MODEL` | `providers.kilo.model` | Kilo model (`providerID/modelID`, first-slash split) |
| `MCREMOTE_PROVIDERS_KILO_SESSION_TREE` | `providers.kilo.session_tree` | Kilo session-tree demux (`true`/`false`) |
| `MCREMOTE_PROVIDERS_KILO_PURE` | `providers.kilo.pure` | Kilo `--pure` flag |
| `MCREMOTE_HEADSCALE_CONTROL_URL` | `headscale.control_url` | Headscale control URL |
| `MCREMOTE_LIMITS_MAX_WS_CLIENTS` | `limits.max_ws_clients` | Max simultaneous WebSocket connections |
| `MCREMOTE_LIMITS_MAX_LIVE_SESSIONS` | `limits.max_live_sessions` | Max concurrent provider sessions |
| `MCREMOTE_PAIR_ADVERTISE_HOST` | `pair.advertise_host` | Host (or host:port) advertised in the pair QR/code, overriding auto-detection. Ignored in `letsencrypt` mode, where the primary ACME domain is used |
| `MCREMOTE_PAIR_HOST` | `pair.advertise_host` | Legacy alias for `MCREMOTE_PAIR_ADVERTISE_HOST` (same key) |
| `MCREMOTE_RELAY_URL` | `relay.url` | mcrelay base URL (`wss://…`) |
| `MCREMOTE_RELAY_HOST_ID` | `relay.host_id` | Public host registration id |
| `MCREMOTE_RELAY_SECRET` | `relay.secret` | Registration secret (min 16 chars) |
| `MCREMOTE_RELAY_INSECURE_SKIP_VERIFY` | `relay.insecure_skip_verify` | Skip relay TLS verify (dev only) |
| `MCREMOTE_RECEIPTS_ENABLED` | `receipts.enabled` | Enable signed receipts for permission decisions (`true`/`false`) |
| `MCREMOTE_RECEIPTS_ALLOW_PATTERNS` | `receipts.allow_patterns` | Comma-separated glob patterns; a match triggers a receipt |
| `MCREMOTE_RECEIPTS_DENY_PATTERNS` | `receipts.deny_patterns` | Comma-separated glob patterns; a match wins over `allow_patterns` |
| `MCREMOTE_RECEIPTS_HANDOFFS` | `receipts.handoffs` | Sign a receipt for each session handoff when receipts are enabled (`true`/`false`) |

AWS credentials for the DNS-01 solver are **not** mcremote settings: the
`route53` provider reads the standard chain (`AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`, `AWS_PROFILE`, or an instance
role). See [iam-route53-acme.md](iam-route53-acme.md) for the IAM policy.

Viper also accepts automatic env for other keys using `MCREMOTE_` + uppercased path with `_` (e.g. `MCREMOTE_PROVIDERS_GROK_BIN`, `MCREMOTE_PROVIDERS_GROK_PREWARM`, `MCREMOTE_AUTH_ALLOWED_ORIGINS`). Prefer the explicit table above for production.

### Examples

```bash
export MCREMOTE_LISTEN_HOST=tailscale   # or an explicit address; 0.0.0.0 opts out of tailnet-only
export MCREMOTE_LISTEN_PORT=7531
export MCREMOTE_LOG_LEVEL=debug
export MCREMOTE_LOG_FORMAT=json
export MCREMOTE_DATA_DIR=/var/lib/mcremote
export MCREMOTE_CONFIG=/etc/mcremote/config.yaml
export MCREMOTE_PAIR_ADVERTISE_HOST=100.64.0.1:7531   # pair QR host; selfsigned mode (== config pair.advertise_host)

# Let's Encrypt (DNS-01 via Route 53)
export MCREMOTE_TLS_DOMAINS=devbox.ts.lallygag.net
export MCREMOTE_TLS_EMAIL=ops@lallygag.net
export MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID=Z0123456789ABCDEFGHIJ
export MCREMOTE_TLS_ROUTE53_REGION=us-east-1
export MCREMOTE_TLS_ACME_STAGING=true      # drop once staging succeeds
```

## TLS modes

| `tls.mode` | Certificate | Phone trust | When |
|------------|-------------|-------------|------|
| `letsencrypt` | ACME DNS-01 via Route 53, auto-renewed by certmagic | Platform chain validation **or** recovery pin (`fp=` + `mode=letsencrypt`). The pin is the self-signed fallback leaf, not the ACME leaf — see [protocol-v1.md](protocol-v1.md) | Default once a domain + email are configured |
| `selfsigned` | Long-lived leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint **only** (`fp=` + `mode=selfsigned`) | Mesh IPs with no public DNS; also the automatic fallback if ACME fails |
| `off` | none | n/a — plaintext `ws://` (`mode=off`, no `fp`) | Only behind another TLS terminator |

Only DNS-01 is implemented for mcremote. Daemon nodes are often mesh-only and
their MagicDNS names are not in public DNS, so an ACME validator cannot reach
them for HTTP-01 or TLS-ALPN-01. IAM / zone setup:
[iam-route53-acme.md](iam-route53-acme.md). Wire trust rules:
[protocol-v1.md](protocol-v1.md) (transport security). Product overview:
[README.md](../README.md#tls).

## CLI flags

Long options always use **two dashes** (`--flag`). Help is `--help` or `-h`. Version is `--version` or `mcremote version`.

### Global (all commands)

| Flag | Env / config | Description |
|------|----------------|-------------|
| `--config` | `MCREMOTE_CONFIG` | Config file path |
| `--log-level` | `MCREMOTE_LOG_LEVEL` | Log level |
| `--log-format` | `MCREMOTE_LOG_FORMAT` | Log format |
| `--help` / `-h` | | Help |
| `--version` | | Version (root) |
| `--setup-service` | | Root alias: install + enable + start user systemd unit |

### `mcremote serve`

| Flag | Description |
|------|-------------|
| `--listen-host` | Override `listen.host` |
| `--listen-port` | Override `listen.port` |
| `--data-dir` | Override `data_dir` |
| `--tls` | Legacy on/off switch; `--tls=false` == `--tls-mode off` |
| `--tls-mode` | `letsencrypt` \| `selfsigned` \| `off` |
| `--tls-domain` | DNS name to request (repeatable / comma-separated); first is advertised to phones |
| `--tls-email` | ACME account email |
| `--tls-acme-directory` | ACME directory URL |
| `--tls-acme-staging` | Use the Let's Encrypt staging CA |
| `--tls-route53-zone-id` | Route 53 hosted zone ID |
| `--tls-route53-region` | AWS region |
| `--tls-route53-profile` | AWS shared-config profile |
| `--relay-url` | mcrelay base URL (`wss://…`); env `MCREMOTE_RELAY_URL` |
| `--relay-host-id` | Public host id for registration; env `MCREMOTE_RELAY_HOST_ID` |
| `--relay-secret` | Registration secret (min 16); env `MCREMOTE_RELAY_SECRET` |

When `relay.url` is set, `mcremote pair` adds `relay=` and `hid=` to the pair URI
(secret is never on the QR). See [0015](0015-MADR-mcrelay-transport-security.md).

### `mcremote engines`

| Flag | Description |
|------|-------------|
| `--reap` | Stop every engine whose owning daemon is gone |

### `mcremote receipts list` / `receipts verify` / `receipts show`

Inspect and verify signed permission-decision receipts (MADR 0077, opt-in —
see [docs/receipts.md](receipts.md)).

| Flag | Description |
|------|-------------|
| `--data-dir` | Data directory (overrides config) — where `receipts/<device_id>.jsonl` and `devices.json` live |
| `--device` | Device id: optional filter on `list`; **required** on `verify` and `show` |
| `--permission` | Permission id (`show` only, required): which decision to decode |

`verify` exits non-zero on a broken chain (reporting the exact 1-indexed
line), so it is safe to use in an audit script. Both `verify` and `show`
resolve the daemon's own signing key the same way `mcremote pair` resolves
the advertised fingerprint (`EnsureCerts` on the resolved data dir), so
`receipt-unavailable` markers verify against the real key that produced them.
The device's key comes from its live `devices.json` record, falling back to
the key archived beside the chain (`receipts/<device_id>.spki`) — so a
**revoked** device's chain still verifies (see
[docs/receipts.md](receipts.md#revoked-devices)).

### `mcremote pair` / `pair code` / `pair create`

| Flag | Description |
|------|-------------|
| `--name` | Device label (default `device`) |
| `--qr` | Print terminal QR (default on TTY) |
| `--host` | Advertise host:port in QR/URI. Overrides `pair.advertise_host` for this run. Default: the primary ACME domain in `letsencrypt` mode; else `pair.advertise_host`, then the Tailscale IPv4 (`selfsigned`) |
| `--ttl` | Pair **code** lifetime (default `5m`; `pair code` / bare `pair` only) |
| `--data-dir` | Data directory for devices / pair codes |

### `mcremote pair list` / `pair revoke`

| Flag | Description |
|------|-------------|
| `--data-dir` | Data directory |

`pair revoke` takes one positional argument: device id or name.

### `mcremote pair prune`

| Flag | Description |
|------|-------------|
| `--keyless` | Prune devices with no enrolled client key |
| `--stale` | Prune devices unused for at least this duration (e.g. `2160h` for 90d) |
| `--data-dir` | Data directory |

At least one of `--keyless` or `--stale` is required.

### `mcremote setup-service` / `mcremote --setup-service`

| Platform | Definition | Enable / start | Linger |
|----------|------------|----------------|--------|
| Linux | `~/.config/systemd/user/mcremote.service` from `internal/cli/service/mcremote.user.service.tmpl` | `systemctl --user` | `loginctl enable-linger` (default) |
| macOS | `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` (LaunchAgent) | `launchctl` `gui/$UID` | **None** (session-bound; no sudo) |

Examples: `deploy/systemd/mcremote.user.service`, `deploy/systemd/mcremote.service`,
`deploy/launchd/com.magiccliremote.mcremote.plist`. See MADR/PLAN 0058.

| Flag | Default | Description |
|------|---------|-------------|
| `--setup-service` | false | Root flag alias for this command |
| `--unit-name` | `mcremote` | Linux unit name; macOS maps to `com.magiccliremote.<name>` Label |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | Serve path only (never copies the binary; use `make install`) |
| `--service-config` | | Config path embedded in service (else `--config`) |
| `--data-dir` | | Passed to `serve` |
| `--listen-host` | *(empty — follow config)* | Baked into the service only when set; `"tailscale"` binds the tailnet IPv4 only |
| `--listen-port` | `0` (follow config) | Baked into the service only when non-zero |
| `--working-directory` | `$HOME` | Working directory |
| `--env` | | Extra `KEY=VALUE` (repeatable) |
| `--print-only` | false | Print unit/plist to stdout only |
| `--force` | false | Overwrite existing definition |
| `--no-enable` | false | Skip enable |
| `--no-start` | false | Skip start/restart |
| `--no-linger` | false | Linux: skip linger. macOS: no effect |
| `--remove` | false | Stop, disable, and delete the service definition |

### `mcremote update` / `mcrelay update` (MADR 0065)

User-initiated upgrade from GitHub Releases: discover latest, download the
matching binary + `SHA256SUMS-<VER>`, verify SHA-256, swap into place, restart
the user service when one is active.

| Flag | Description |
|------|-------------|
| `--check` | Report only. Exit `0` if up to date, `10` if an update is available, `1` on error |
| `--yes` | Skip the confirmation prompt |
| `--force` | Allow updating a local dev-suffixed build (e.g. `0.7.0.1.gdeadbeef`) |

Optional env: `GITHUB_TOKEN` (API rate limits), `MC_CODESIGN_IDENTITY` (macOS
re-sign after download so TCC/FDA grants survive — see
[ops-macos-tcc.md](ops-macos-tcc.md)).

### Unit file options (embedded user template)

| Directive | Value / notes |
|-----------|----------------|
| `Type` | `simple` |
| `WorkingDirectory` | installer's home (override with `--working-directory`) |
| `ExecStart` | `<binary> serve` plus optional `--config`, `--data-dir`, `--listen-host`, `--listen-port`, `--log-level`, `--log-format` |
| `Restart` | `always` (not `on-failure`) |
| `RestartSec` | `2` |
| `TimeoutStopSec` | `45` |
| `KillMode` / `KillSignal` | `control-group` / `SIGTERM` |
| `Environment` | `HOME`, `USER`, `LOGNAME`, `PATH`, `XDG_*` (+ optional `--env` extras) |
| Hardening | `NoNewPrivileges`, `PrivateTmp`, `RestrictSUIDSGID`, `LockPersonality`, `RestrictRealtime`, `ProtectKernelTunables`, `ProtectControlGroups`, `SystemCallArchitectures=native`, `LimitNOFILE=65536` |
| `WantedBy` | `default.target` |

## Examples

- Dev: [configs/config.example.yaml](../configs/config.example.yaml)
- Prod-oriented: [configs/config.prod.example.yaml](../configs/config.prod.example.yaml)
- Mesh + Grok: [configs/config.mesh-grok.yaml](../configs/config.mesh-grok.yaml)
