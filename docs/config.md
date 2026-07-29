# mcremote configuration

## Locations (XDG)

| Item | Path |
|------|------|
| Config dir | `$XDG_CONFIG_HOME/mcremote` or `~/.config/mcremote` |
| Config file | `config.yaml` inside config dir |
| Data dir | `$XDG_DATA_HOME/mcremote` or `~/.local/share/mcremote` |
| Devices | `<data_dir>/devices.json` (mode `0600`) |
| Pair codes | `<data_dir>/pair_codes.json` (mode `0600`) |
| TLS certificate (selfsigned) | `<data_dir>/tls.crt` (mode `0600`) |
| TLS private key (selfsigned) | `<data_dir>/tls.key` (mode `0600`) |
| ACME storage (letsencrypt) | `<data_dir>/acme/` (certmagic: account key + issued certs) |
| User unit | `~/.config/systemd/user/mcremote.service` |

Override config path: `--config /path/to.yaml` or `MCREMOTE_CONFIG`.

`mcremote setup-service` writes a **default** `config.yaml` into the config dir
when missing (0600, never overwrites an existing file) and bakes that path into
the unit’s `ExecStart`.

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
| `tls.mode` | _(empty — `letsencrypt` when `tls.letsencrypt.domains` + `email` are set, else `selfsigned`)_ |
| `tls.enabled` | `true` (legacy switch; `false` == `tls.mode: off`) |
| `tls.cert_file` / `tls.key_file` | _(empty — generate and manage automatically; `selfsigned` only)_ |
| `tls.letsencrypt.domains` | _(empty)_ |
| `tls.letsencrypt.email` | _(empty)_ |
| `tls.letsencrypt.directory_url` | _(empty — Let's Encrypt production)_ |
| `tls.letsencrypt.staging` | `false` |
| `tls.letsencrypt.cache_dir` | _(empty — `<data_dir>/acme`)_ |
| `tls.letsencrypt.route53.hosted_zone_id` | _(empty — discovered from the zone name)_ |
| `tls.letsencrypt.route53.region` | _(empty — `AWS_REGION`)_ |
| `tls.letsencrypt.route53.profile` | _(empty — `AWS_PROFILE`)_ |
| `tls.letsencrypt.route53.max_retries` | `0` (AWS SDK default) |
| `log.level` | `info` |
| `log.format` | `text` |
| `data_dir` | _(empty — XDG data home)_ |
| `auth.require_device_token` | `true` |
| `auth.require_client_key` | `true` — tokens bound to the device's enrolled TLS client key (ADR 0005); keyless legacy devices must re-pair |
| `auth.allowed_origins` | `[]` — browser Origin allowlist for the WS upgrade; empty is the secure baseline (native clients + same-origin accepted, cross-origin rejected). Never `"*"` |
| `providers.fake.enabled` | `false` (dev/smoke only) |
| `providers.grok.enabled` | `true` |
| `providers.grok.bin` | `grok` |
| `providers.grok.args` | `[]` — empty uses built-in `agent --no-leader stdio` (+ `-m MODEL` when set) |
| `providers.grok.always_approve` | `false` |
| `providers.grok.default_cwd` | _(empty — sessions start in the daemon user's home directory)_ |
| `providers.grok.model` | _(empty)_ |
| `providers.grok.reasoning_effort` | _(empty — pass `--reasoning-effort <EFFORT>` to `grok agent` when non-empty, e.g. `low`, `medium`, `high`)_ |
| `providers.grok.permission_mode` | **`default`** — passed as `--permission-mode <MODE>`. Valid: `default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`; empty inherits grok's own config. Rejected at load if unrecognised. **Process-wide and launch-scoped**: applies to every grok session, changing it needs an engine restart. Distinct from the per-session `auto` **mode** in the app's mode menu, which is daemon-enforced (MADR 0049). See the note below on why this is pinned. |
| `providers.grok.sandbox` | _(empty — grok's own default)_ — OS-level sandbox profile (`--sandbox <PROFILE>`). Built-ins: `off`, `workspace`, `devbox`, `read-only`, `strict`. Any other value is treated as a custom profile name and resolved by grok from `~/.grok/sandbox.toml` or `.grok/sandbox.toml`; grok errors clearly if it is missing. Also settable via the `GROK_SANDBOX` env var. See the sandbox note below. |
| `providers.grok.allowed_tools` | `[]` — whitelist of built-in tool names (`--tools a,b`) |
| `providers.grok.disallowed_tools` | `[]` — blacklist of built-in tool names (`--disallowed-tools a,b`) |
| `providers.grok.allow_rules` | `[]` — persistent permission allow rules (`--allow <rule>`) |
| `providers.grok.deny_rules` | `[]` — persistent permission deny rules (`--deny <rule>`) |
| `providers.grok.no_subagents` | `false` — disable subagent spawning (`--no-subagents`) |
| `providers.grok.disable_web_search` | `false` — disable built-in web search (`--disable-web-search`) |
| `providers.grok.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.grok.prewarm` | `true` — keep one spare initialized agent (Phase 4.2); disable if memory is tight |
| `providers.grok.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.grok.fs_roots` | `[]` — confine the agent's `fs/read_text_file` & `fs/write_text_file` callbacks to these roots (plus the session cwd). Empty = unrestricted. Defense-in-depth/audit only: the agent has terminal access as the same user, so this is not a sandbox |
| `providers.opencode.enabled` | `true` — pick OpenCode per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.opencode.bin` | `opencode` |
| `providers.opencode.always_approve` | `false` |
| `providers.opencode.default_cwd` | _(empty — sessions start in the daemon user's home directory)_ |
| `providers.opencode.model` | _(empty — OpenCode's own default; pin e.g. `opencode/deepseek-v4-flash-free` or `anthropic/claude-haiku-4-5`)_ |
| `providers.opencode.pure` | `false` — run `opencode serve` with `--pure` (without loading external third-party plugins) |
| `providers.opencode.permission_timeout_seconds` | `120` (`0` = wait forever) |
| `providers.opencode.prewarm` | `true` — boot the shared `opencode serve` engine at daemon start so the first session create is instant. `false` boots it lazily on first use (~3–5s) and holds no idle engine (~250MB) |
| `providers.opencode.turn_stall_notice_seconds` | `120` — notice when a running turn goes silent (`0` = off) |
| `providers.opencode.stream_coalesce_ms` | `80` — hold assistant/thought text this long so it ships as one event instead of one per model token (MADR 0024), capping mid-stream updates at ~12/s. The first chunk of a reply and the tail before any control event are never delayed, so time-to-first-token and end-of-turn latency are unchanged. `0` = one event per token (pre-0024 behaviour); max `1000` |
| `providers.goose.stream_coalesce_ms` | `80` — same coalescing as `providers.opencode.stream_coalesce_ms` (MADR 0024), for the goose ACP-over-WebSocket transport. `0` = one event per token; max `1000` |
| `providers.opencode.session_tree` | `true` — multi-agent session-tree demux (child aliases, tree-idle EndTurn, child fan-in; MADR 0020 KD11). `false` = exact pre-0020 kill switch (parent-only). When `true`, OpenCode must report version **≥ 1.18.0** on `/global/health` (KD10) or session create fails |
| `providers.codex.enabled` | `true` — pick Codex per session from the phone's new-session provider menu; harmless when the binary is absent (listed as not ready) |
| `providers.codex.bin` | `codex` |
| `providers.codex.always_approve` | `false` |
| `providers.codex.default_cwd` | _(empty — sessions start in the daemon user's home directory)_ |
| `providers.codex.model` | _(empty — Codex's own default from `~/.codex/config.toml`; pin e.g. `gpt-5.6-terra`)_ |
| `providers.codex.permission_timeout_seconds` | `900` (`0` = wait forever) — longer than other providers because Codex sandboxed tools may run for minutes |
| `providers.codex.prewarm` | `false` — boot the shared `codex app-server` engine at daemon start so the first session create skips the ~500ms cold start. `true` pre-warms; `false` boots lazily on first use |
| `providers.codex.turn_stall_notice_seconds` | `0` — notice when a running turn goes silent (`0` = off) |
| `providers.codex.stream_coalesce_ms` | `80` — same coalescing as other providers (MADR 0024). `0` = one event per token; max `1000` |
| `providers.codex.approval_policy` | _(empty — mcremote `default` session mode: `on-request`)_. Valid: `untrusted`, `on-request`, `never`. Empty with empty sandbox seeds the normal mode pair (MADR 0047); `never` alone is repaired to auto (`never` + `workspace-write`). Set **both** fields to pin a custom pair |
| `providers.codex.sandbox_mode` | _(empty — mcremote `default` session mode: `workspace-write`)_. Valid: `read-only`, `workspace-write`, `danger-full-access`. See approval_policy; both empty → default mode, not silent engine-file inheritance for remote sessions |
| `headscale.control_url` | `http://localhost:8080` |
| `limits.max_ws_clients` | `8` (simultaneous WebSocket clients; `0` falls back to default 8 via `Resolved()`) |
| `limits.max_live_sessions` | `16` (concurrent live agent sessions; `0` falls back to default 16) |
| `relay.url` | _(empty — outbound mcrelay disabled)_ |
| `relay.host_id` | _(empty)_ — public id for join routing (`hid=` in pair URI) |
| `relay.secret` | _(empty)_ — registration secret (min 16); prefer env |
| `relay.insecure_skip_verify` | `false` — skip TLS verify of **mcrelay** only (dev) |
| `pair.advertise_host` | _(empty — auto-detect: Tailscale IPv4, else loopback)_ — host (or host:port) advertised in the pair QR/URI. A bare host inherits `listen.port`. Ignored in `letsencrypt` mode (the ACME domain is used); `mcremote pair --host` overrides per run |

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
on a machine where that is acceptable.

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
| `MCREMOTE_TLS_ROUTE53_MAX_RETRIES` | `tls.letsencrypt.route53.max_retries` | AWS API max retries (`0` = SDK default) |
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
| `MCREMOTE_PROVIDERS_GOOSE_ENABLED` | `providers.goose.enabled` | Enable Goose provider |
| `MCREMOTE_PROVIDERS_GOOSE_BIN` | `providers.goose.bin` | Goose executable path |
| `MCREMOTE_PROVIDERS_GOOSE_ALWAYS_APPROVE` | `providers.goose.always_approve` | Auto-approve Goose tool requests |
| `MCREMOTE_PROVIDERS_GOOSE_MODEL` | `providers.goose.model` | Goose model override |
| `MCREMOTE_PROVIDERS_GOOSE_PERMISSION_TIMEOUT_SECONDS` | `providers.goose.permission_timeout_seconds` | Goose permission timeout seconds |
| `MCREMOTE_PROVIDERS_GOOSE_STREAM_COALESCE_MS` | `providers.goose.stream_coalesce_ms` | Goose stream coalescing window (ms) |
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
| `MCREMOTE_HEADSCALE_CONTROL_URL` | `headscale.control_url` | Headscale control URL |
| `MCREMOTE_LIMITS_MAX_WS_CLIENTS` | `limits.max_ws_clients` | Max simultaneous WebSocket connections |
| `MCREMOTE_LIMITS_MAX_LIVE_SESSIONS` | `limits.max_live_sessions` | Max concurrent provider sessions |
| `MCREMOTE_PAIR_ADVERTISE_HOST` | `pair.advertise_host` | Host (or host:port) advertised in the pair QR/code, overriding auto-detection. Ignored in `letsencrypt` mode, where the primary ACME domain is used |
| `MCREMOTE_PAIR_HOST` | `pair.advertise_host` | Legacy alias for `MCREMOTE_PAIR_ADVERTISE_HOST` (same key) |
| `MCREMOTE_RELAY_URL` | `relay.url` | mcrelay base URL (`wss://…`) |
| `MCREMOTE_RELAY_HOST_ID` | `relay.host_id` | Public host registration id |
| `MCREMOTE_RELAY_SECRET` | `relay.secret` | Registration secret (min 16 chars) |
| `MCREMOTE_RELAY_INSECURE_SKIP_VERIFY` | `relay.insecure_skip_verify` | Skip relay TLS verify (dev only) |

AWS credentials for the DNS-01 solver are **not** mcremote settings: the
`route53` provider reads the standard chain (`AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`, `AWS_PROFILE`, or an instance
role). See [tls-letsencrypt.md](tls-letsencrypt.md) for the IAM policy.

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
| `letsencrypt` | ACME DNS-01 via Route 53, auto-renewed by certmagic | Platform trust store — **no** `fp=` in the pair QR | Default once a domain + email are configured |
| `selfsigned` | Long-lived leaf in `<data_dir>/tls.{crt,key}` | SHA-256 fingerprint pinned from the pair QR (`fp=`) | Mesh IPs with no public DNS; also the automatic fallback if ACME fails |
| `off` | none | n/a — plaintext `ws://` | Only behind another TLS terminator |

Only DNS-01 is implemented. Daemon nodes are mesh-only and their MagicDNS
names are not in public DNS, so an ACME validator can never reach them for
HTTP-01 or TLS-ALPN-01. Full setup: [tls-letsencrypt.md](tls-letsencrypt.md).

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
(secret is never on the QR). See [0015](0015-mcrelay-transport-security.md).

### `mcremote engines`

| Flag | Description |
|------|-------------|
| `--reap` | Stop every engine whose owning daemon is gone |

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

Source of truth for the unit body: `internal/cli/service/mcremote.user.service.tmpl`.
Example copy for manual install: `deploy/systemd/mcremote.user.service`.
System-wide example: `deploy/systemd/mcremote.service`.
macOS example: `deploy/launchd/com.magiccliremote.mcremote.plist`.

| Flag | Default | Description |
|------|---------|-------------|
| `--setup-service` | false | Root flag alias for this command |
| `--unit-name` | `mcremote` | Unit name without `.service` |
| `--binary` | `~/.local/bin/mcremote` if present, else this executable | `ExecStart` path only (never copies the binary; use `make install`) |
| `--service-config` | | Config path embedded in unit (else `--config`) |
| `--data-dir` | | Passed to `serve` |
| `--listen-host` | _(empty — follow config)_ | Baked into the unit only when set; `"tailscale"` binds the tailnet IPv4 only |
| `--listen-port` | `0` (follow config) | Baked into the unit only when non-zero |
| `--working-directory` | `$HOME` | systemd `WorkingDirectory` |
| `--env` | | Extra `KEY=VALUE` (repeatable) |
| `--print-only` | false | Print unit to stdout only |
| `--force` | false | Overwrite existing unit |
| `--no-enable` | false | Skip `systemctl --user enable` |
| `--no-start` | false | Skip start/restart |
| `--no-linger` | false | Skip `loginctl enable-linger` |
| `--remove` | false | Stop, disable, and delete the unit (inverse of setup) |

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
