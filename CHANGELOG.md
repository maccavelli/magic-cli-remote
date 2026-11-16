# Changelog

All notable changes to Magic CLI Remote are recorded here: `mcremote`, `mcrelay`, the installers and service units, and
the phone app built with the same tag. An entry is a change a user, operator or the app can see. Each links the record
that explains it.

The format follows [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/). Versions are this repository's
`vX.Y.Z` tags. This file does not claim Semantic Versioning, which no record in this repository adopts.

## [Unreleased]

### Changed

- Example addresses in `--help` output, the phone's Host field hint and the guides were replaced with placeholders. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).

### Fixed

- The README no longer lists `_MAX_RETRIES` among the short `MCREMOTE_TLS_ROUTE53_*` names. Only the full name
  `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` works; the short spelling was silently ignored. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The mcrelay guide now says what the daemon does. A config file with any group or other permission bit is refused at
  load, not only a world-readable one, and a symlinked config path is normally refused too. `tls.mode: off` on a
  non-loopback bind is refused unless `--allow-plaintext` is given, instead of only warning. The hardware-validation
  runbook now tells an operator to expect a suspended socket to linger at most `limits.ws_read_deadline_seconds`
  (default 120 seconds), and sooner, about 45 seconds, when TCP keepalive sees the phone stop, not "within 60
  seconds"; and `resumed` is a field of the auth reply the phone receives, so it is looked for in the phone's log, not
  in the daemon's auth logs. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The protocol guide matches the daemon. `agent_sessions.list` is served by codex only: the ACP providers (grok, gobble,
  OpenCode) answer `unsupported` and kilo answers `agent_sessions_list_failed`. No provider emits `diagnostics_changed`,
  so a client must not wait for it. Kilo's live `session.diff` events are not turned into a notice; pull the diff with
  `session.diff`. `session.fork`, `session.revert`, `session.unrevert` and `session.diff` are listed per provider (fork:
  gobble, grok, codex, kilo; revert and unrevert: OpenCode, kilo, fake; diff: OpenCode, kilo, codex, fake), where the
  guide called them OpenCode features. An oversize `name` on `session.create` is cut to 256 bytes, not rejected with
  `bad_payload`, and the `aggregate` example no longer shows zero counts, which the daemon omits. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The configuration guide says how gobble picks its upstream. A session authenticates the first upstream, in gobble's
  order, that `gobble auth status --json` reports as usable with no reason, unless `providers.gobble.auth_method_id`
  pins one. A stored credential that carries a reason (gobble 0.5.0 gives one to an OAuth entry of an upstream with no
  login) is skipped, where the guide said the first upstream with a stored credential. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The Headscale guide creates pre-auth keys with the numeric user ID (`--user <user-id>`, from `headscale users list`),
  which v0.29 requires; the guide had passed a user name. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The phone app's README and architecture guide match the app. The profiling guide link resolves. The connect screen has
  **Paste URI / code / token**, not a host and token "under Advanced". The button is **Sign out**, not "Logout
  (Disconnect)". `make apk` writes only `app-release.apk`; `scripts/build-apk.sh` also copies it to `dist/`. Android and
  iOS refuse cleartext `ws://` in every build mode, and the app does not call `GET /v1/hello`. The emulator guide uses
  the key `listen.host: tailscale`, not `listen: tailscale`. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
- The CI build log for a release without Android signing secrets now prints the path
  `apps/mobile/docs/guides/ops-android-signing.md`, which exists, in place of the old `docs/guides/` path. See
  [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](docs/decisions/0248-PLAN-bring-the-docs-current-and-keep-them-so.md).

## [v0.26.0] - 2026-11-12

### Added

- The gobble provider. `mcremote` runs `gobble acp` for each gobble session and registers `gobble` beside the other
  providers. It is enabled by default and lists as not ready when the `gobble` executable is missing. Settings are the
  `providers.gobble.*` keys and the `MCREMOTE_PROVIDERS_GOBBLE_*` variables, and the example configs carry a commented
  block. gobble 0.4.0 is the supported minimum. The phone writes an API key through gobble's own CLI, never on a
  command line. Device sign-in from the phone is offered for the grok and kilo upstreams only, and the daemon gives up
  on a gobble that prints no device code within 30 seconds. An upstream that gobble reports with a reason (gobble
  0.5.0 does so for an OAuth entry of an upstream with no login) shows as not configured. `make live-gobble` is an opt-in live test.
  See [0179-MADR-gobble-native-acp-provider.md](docs/decisions/0179-MADR-gobble-native-acp-provider.md) and
  [0179-PLAN-gobble-native-acp-provider.md](docs/decisions/0179-PLAN-gobble-native-acp-provider.md).
- Usage-limit detail. An `error` event for a quota or rate limit can carry `limit_scope`, `limit_window_minutes`,
  `limit_provider` and `limit_code`, and the provider's own words with the time the limit lifts. The phone titles the
  card by what was hit ("5-hour limit reached", "Out of credits"), names the upstream, and says when a credits limit
  does not reset by itself. codex reports whichever of its two windows is full, and a notice when one reaches 90 percent.
  gobble's structured error data fills the same fields. Older clients ignore the new members. See
  [0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md](docs/decisions/0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md).
- A provider's `auth` block says when the agent cannot switch its active upstream (`upstream_switch_unsupported`). The
  daemon refuses `provider.set_active_upstream` for such an agent with `unsupported`, and the phone hides the "Active
  upstream" control. An older daemon never sends the member, so an older phone keeps the control. See
  [0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](docs/decisions/0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md).
- The host's credential backup state on provider status (`backup_state`, `recovery_available`), and a "Credential
  backup" row on the phone's provider screen. It shows whether a backup can be restored with `mcremote auth-recovery`
  on the host. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- Five error codes for sign-in outcomes that used to arrive as `credential_failed`: `provider_auth_busy`,
  `credential_conflict`, `unsupported_backend`, `recovery_required` and `too_many_sign_ins`. A device-flow result also
  carries `state` and `retryable`. The phone's sign-in sheet words each state separately and shows a retry hint when a
  retry can work. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- A device code now expires when its provider says it does (15 minutes for codex, 600 seconds for grok). The flow ends
  as `expired`, not `cancelled`, and the result reports `expires_in`. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).

### Changed

- Catalog sign-in methods for the kilo and opencode providers are named `<upstream>:<label slug>`, not
  `<upstream>:<index>`, so a method keeps its id when the catalog reorders. An id in the old index form is refused as
  unsupported. Every `oauth` entry in a catalog is offered as device sign-in, and the label-based guess at browser
  sign-in is gone. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- The error text of `mcremote update` and `mcrelay update` names `selfupdate:` once, not twice, wherever a refusal wraps
  an error of the update library. A refused confirmation, run without a terminal and without `--yes`, read
  `update failed: selfupdate: mcremote: selfupdate: pass --yes to apply without prompting: selfupdate: confirmation required`
  and now reads `update failed: selfupdate: mcremote: pass --yes to apply without prompting: confirmation required`. The
  same holds for the other refusals, such as "replacing a local build requires --force" and "target ... is outside
  allowed roots". The words after the prefix are unchanged, but anything that matches the old stderr line whole no
  longer matches. The library's own notes also list reworded text for the symlink refusal and a few others. See
  [0217-MADR-update-every-pin-to-its-current-release.md](docs/decisions/0217-MADR-update-every-pin-to-its-current-release.md).

### Fixed

- The shipped mcrelay user unit (`deploy/systemd/mcrelay.user.service`) no longer sets `PrivateDevices` or
  `RestrictNamespaces`. On a host that restricts user namespaces, as stock Ubuntu 24.04 and later does, the old unit
  restarted in a loop with `status=218/CAPABILITIES`. Both user units also drop the `GOCACHE` and `GOTMPDIR` overrides
  under `/cache`, which broke agents' Go commands where `/cache` is not writable, and gain `USER`, `LOGNAME`,
  `XDG_STATE_HOME` and `XDG_RUNTIME_DIR` to match the templates. If you copied an older unit, delete those lines or
  replace the file with `mcrelay setup-service --force`; the steps are in
  [docs/guides/ops-mcrelay.md](docs/guides/ops-mcrelay.md). See
  [0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md](docs/decisions/0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md).
- The error codes `session_cancel_timeout` and `not_found` reached the wire without being registered. Both are now in
  the registered set and documented in `protocol-v1.md`. See
  [0241-MADR-register-every-sent-error-code.md](docs/decisions/0241-MADR-register-every-sent-error-code.md).
- A grok `auth.json` that exists but holds no credential no longer reads as signed in. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- Signing codex out, or writing its key, from the phone now goes through the credential coordinator. The revoke runs
  against a private copy of the credential, where it used to find no token in the daemon's home after the logout was
  recorded. See [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- A data race, and a send on a closed channel, in the update stream of a device sign-in that finished while an update
  was still being forwarded. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- The binaries build with Go 1.27.4, which fixes GO-2026-6859, a `net/http` HTTP/1 server crash that `govulncheck`
  reported as reachable from this module. See
  [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](docs/decisions/0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md).

### Removed

- The `confirm_destructive` member of `provider.start_auth`. The phone no longer sends it, and the protocol guide says
  `confirm_required` now covers only codex thread deletions and unsandboxed execution actions. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).
- The `keyring_managed` error code and its phone text, which nothing produced; codex's older device sign-in; and the
  phone's codex sign-out confirmation dialog. codex's owned device flow is the only codex device sign-in. See
  [0074-MADR-remote-provider-auth-from-phone.md](docs/decisions/0074-MADR-remote-provider-auth-from-phone.md).

## Older releases

This file starts at v0.26.0. For earlier releases, see this repository's
[releases](https://github.com/maccavelli/magic-cli-remote/releases) and
[tags](https://github.com/maccavelli/magic-cli-remote/tags), and its records, indexed in
[docs/README.md](docs/README.md).

[Unreleased]: https://github.com/maccavelli/magic-cli-remote/compare/v0.26.0...HEAD
[v0.26.0]: https://github.com/maccavelli/magic-cli-remote/releases/tag/v0.26.0
