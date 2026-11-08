# Record status and docs accuracy audit, 2026-11

<!-- markdownlint-disable MD013 -->

Started 2026-11-08, at `master` `cb68b744`. This report records what the audit of
[0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)
found and corrected. It decides nothing: each correction cites its evidence in the record it changed, and here.
Its sections are filled phase by phase, as
[0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md)
runs. This version covers P1, the open records; P2, the `completed` spelling; P3, the entry points; and P4, the
install, configuration and operations guides with the Go standards.

## Rules

The rules of [0200-REPORT-record-status-audit.md](0200-REPORT-record-status-audit.md), with the owner's answers of
2026-11-08:

- **MADR `proposed` → `accepted`** when its PLAN ran and the change is on `master`, or the owner's approval is on
  record. Where the acceptance was never written down, it is marked accepted with a note that it is inferred (Q2:
  "Keep the 0200 rule").
- **PLAN → `complete`** only when the code, or the PLAN's own record, shows every step landed. **PLAN →
  `in-progress`** when some did, with what remains named.
- **Additive only.** A record with front matter has its `status` and `date` changed and gains a closing "Status note
  — 2026-11-08". Nothing earlier is rewritten. Every note links here.
- A record that is open on purpose and reads accurately is left as it is.

## P1 — the open records

### Method

- A script listed every MADR and PLAN whose front-matter status is `proposed` or `in-progress`: 53 files, the 51 the
  MADR counted plus 0239's own pair. For each, it gathered the last headings, the last "Not done" text, and the
  commits since 2026-10-18 that touched it or named it.
- Eighteen records were checked by one read-only helper pass, against the code, git history, citing records and CI.
  Its load-bearing claims were spot-checked against the files before any edit: 0211's recorded approval, `ci.yml`'s
  path filters, the repository's rulesets, 0208's "stays `proposed`" sentence, `internal/config/load.go:310-318`, and
  0217's remaining dotfiles step.
- The rest were checked directly: `grep` for the symbols, files, keys and targets each names, and `git log` since
  0200's audit.
- No test was run, and no provider quota was spent.

### Corrected

| Record | Change | Evidence |
| --- | --- | --- |
| 0211-MADR | `proposed` → `accepted` | its PLAN records the owner's approval; `88d37d1c` |
| 0090-MADR | `proposed` → `accepted` (inferred) | D1–D5 with the template parity test; D6 through 0201 D23 (`84011354`) |
| 0208-MADR | `proposed` → `accepted` | the owner's answer, 2026-11-08 ("Accept it now"); "Proceed" already on record; PLAN P1–P8 run |
| 0203-PLAN | `in-progress` → `complete` | V3: `de29a288` ran only Docs (`42490295468`); `74ad525e` ran CI, Docs and the ledger. V4: the `v0.25.1` tag ran full CI (`42493767398`). V5: scheduled ledger `42393302902` |
| 0089-MADR | `proposed` kept; **D1 superseded in part** by 0126 F3 | the owner's answer, 2026-11-08; `84876c6b` removed `WAKE_LOCK`; the manifest strips it today |
| 0130-MADR | `proposed` → `accepted` (inferred) | its decision, option A, was to investigate first; its PLAN is `complete` and its 2026-09-02 amendment records the investigation and the parking. Found by the Confirmation 2 check; 0200 had listed it as parked |

### Status kept; stale text corrected by a note

| Record | Correction |
| --- | --- |
| 0090-PLAN | tasks 3.1 and 3.2 landed through 0201 D23; task 4.2 no longer applies (0208 P8). Remaining: `config.md`'s `MCREMOTE_LIMITS_*` rows (3.3) and `README.md`'s kilo version, 7.4.20 against the 7.8.18 pin (4.1). Both fall in this audit's guide phases |
| 0143-PLAN | `Go (linux/arm64)` already retries through the shared matrix step (`ci.yml:265`); `Go (test; build on tag)` is now `Go (test)`. Remaining: Phase 3 for `Go (test)` and both Flutter jobs, Phase 4, Phase 5 |
| 0163-PLAN | only the manual phone pass remains; the codex pin has since moved under 0226 and 0236 |
| 0169-PLAN | remaining: D7's git on the Linux test host and WSL, and D3's Flutter lockstep (magic-git's pair). A6 is overtaken by 0217 D2 |
| 0217-PLAN | remaining: the owner's dotfiles commit on the Linux test host (owner: "Not yet", 2026-11-08). A5 reads as amended by D4's siblings-only rule (`spf13/pflag` held) |

### Closed on the owner's check, after P1's commit

- **0227-MADR** `proposed` → `accepted`, and **0227-PLAN** `in-progress` → `complete`. The owner ran the
  Confirmation's check headless on 2026-11-08, at `39b83ba9`, one run per tool in its default permission mode. In
  Claude Code, OpenCode and gobble the read-only turn (`git status`, `git log -1`, `check_records.py --next`, a read
  of `AGENTS.md`) ran without a denial. The control, `git add --dry-run AGENTS.md`, was denied by each:
  `permission_denials` in Claude Code, "auto-rejecting" in OpenCode, "declined" in gobble.

### Checked and left as they are

Each reads accurately against the tree:

- **Not implemented, proposals:** 0113, 0117, 0121, 0131, 0135, 0146, 0157 and 0181 (MADR and PLAN where both exist);
  0213 (MADR and PLAN), 0221, 0228-PLAN, 0229 and 0233. Spot checks: no `LoadPurpose` (0117), `closeMatching` still
  purges in place of closing (0146), no rolling-log sink (0157), no application message limit (0181), no
  golangci-lint target (0131), no `OverrideRecordDir` (0233), `SkillInfo` unchanged (0229), no `ListPlugins` (0228).
- **Open by the record's own rule:** 0095-MADR (acceptance held for a device check).
- **`in-progress` and accurate:** 0089-PLAN (Phases 0 and 3–5), 0109-PLAN expand (P8–P17 not started), 0123, 0125,
  0132, 0133, 0134, 0136, 0145, 0159 (P12, `--env-file`), 0208-PLAN (P9, P10), 0216 (P3–P7), 0219 (V3, waiting on
  `/vN` support in go-shared).
- **`proposed` and accurate:** 0109-PLAN retire (waiting on the owner), 0179-PLAN (P0–P6, amended 2026-11-08), 0205-PLAN
  (not executed), and 0239's own pair.

### Findings for the owner

- **0205's tag ruleset was never created.** 0205-MADR chose N2, "do not sign; add a tag ruleset that limits `v*` tags
  to the owner". The repository has one ruleset, `Protect` (`20181285`), which targets branches. The PLAN's
  execution record is empty. Its status, `proposed`, is accurate; the decision's one action is not done.
- **Eight proposals have been idle since August or September 2026:** 0113, 0117, 0121, 0131, 0135, 0146, 0157 and
  0181. None was started. Each could stay as backlog, or be closed as `rejected` or deferred by the owner.
- **0213 is partly overtaken.** Its markdownlint-cli2 pin (0.23.2) is behind 0217 D6's 0.24.0, and 0203's
  `docs.yml` already carries a home-path step. Its portable disclosure check and parity test are not built.
- **0227's configuration landed in an unrelated commit,** `254279ac` ("feat(opencode): add control plane and fixture
  host-tag guard"), not under 0227's own.
- **gobble ignores one line of `.gobble/settings.yaml`.** Every gobble run warns
  `permissions.allowBypass: only the global settings file can set this` (line 4). The project file's
  `allowBypass: false` has no effect. It does no harm, since `false` is gobble's default, but removing the line is a
  change to that file under an amendment of 0227.

## P2 — the `completed` spelling

On the owner's answer to Q1 ("Normalize to complete"), the front-matter line `status: completed` became `status:
complete` in 31 PLANs, with no note: 0087, 0088, 0091, 0098, 0115, 0118, 0119, 0120, 0124, 0142, 0147–0156, 0160,
0161, 0162, 0164, 0165, 0166, 0168, 0171, 0172, 0173 and 0182. No status changed meaning.

**Left as `completed`:** `0122-PLAN-deterministic-goose-file-log-tail-attach.md`.
[0160-MADR-remove-goose-cli-support.md](../decisions/0160-MADR-remove-goose-cli-support.md) D9 freezes the status
lines of goose-only PLANs, and the owner chose to keep it that way (PLAN P2 Deviation 1).

## P3 — the entry points

`README.md`, `docs/README.md`, `docs/architecture.md`, `apps/mobile/docs/README.md` and
`apps/mobile/docs/architecture.md`.

### Method

- **The reference checker** (`docref.py`, scratchpad) extracts eight kinds of reference and resolves each against the
  tree: a repository path, a `make` target, an `mcremote`/`mcrelay` subcommand, a `--flag`, a dotted config key, an
  `MCREMOTE_*`/`MCRELAY_*` variable, a backticked Go identifier, and a tool version against its pin. It was seen
  failing on one planted bad reference of each kind.
- **The anchor resolver** (`anchors.py`) checks every relative link's `#anchor` against GitHub's heading slugs. It was
  seen failing on a planted dead same-file anchor and a planted dead cross-file anchor. `make check-records` already
  checks each link's file (PLAN P3 Deviation 1).
- Each unresolved reference was read in context. Then each doc was read through against the code, for the README
  with one read-only helper pass whose claims were spot-checked against the files before any edit.

### Checker results

87 unresolved references before the edits, 85 after. Each remaining one is in a class that is not an error:

- **Protocol message types** read as config keys: `session.create`, `models.list`, `pair.claim` and 20 more, each
  confirmed in `internal/protocol`.
- **English after a binary's name:** "mcrelay does", "mcrelay supports" and similar.
- **Record titles in the generated index:** "grok 1.0.3", "Flutter 3.47.2", "make docs".
- **Installer options** (`--no-hooks`, `MCREMOTE_NO_HOOKS`, `MCREMOTE_INSTALL_DIR`): defined in go-selfupdate-lib's
  installer template at the pinned commit, not in this repository.
- **Variables viper derives from a key** (`MCREMOTE_AUTH_ALLOWED_ORIGINS`, `MCREMOTE_PROVIDERS_GROK_SANDBOX`): each key
  exists.
- **History stated as history:** "measured on grok 1.0.3", "`scripts/next-build-version.sh` are gone".

The anchor resolver found one dead anchor in the whole doc set, `docs/guides/receipts.md:197` to
`config.md#locations-xdg`, which P5 fixes.

### Corrections, `README.md`

Each states what the code does today. The evidence column names the code that decided it.

| Section | Was | Now | Evidence |
| --- | --- | --- | --- |
| Binaries | mcremote: "protocol v1" | "protocol v1/v2" | `internal/protocol/messages.go` |
| Upgrading later | agent-run update under runit, s6, OpenRC "refuses" | hands off to a detached process; OpenRC refuses only when the update shares the service's cgroup | `internal/updateclient/client.go:245-277, 355-360` |
| Requirements | setup-service: systemd, launchd, Task Scheduler | adds runit, s6 and OpenRC 0.60+ | `internal/cli/service/detect.go`; the README's own table |
| Requirements | "Dart ≥ 3.13.2 (CI pins Flutter 3.47.2)" | CI pins 3.47.9; `pubspec.yaml` requires Dart `^3.12.2` | `ci.yml:33`; `apps/mobile/pubspec.yaml:12` |
| Quick start | `make build VERSION=1.2.3` | `VERSION=v1.2.3 BUILD_KIND=release`; without `BUILD_KIND` the build is `local` | `Makefile:10, 49` |
| Quick start | "Short `-h` is help only" | `update` also takes `-y` | go-selfupdate-lib `selfupdate/cli/flags.go:78` |
| Service installation | waits `MCREMOTE_SVC_WAIT_SECS` "(default 15)" | 60 s, 75 s on runit/s6/OpenRC; the variable must exceed the 10 s settle window | `internal/cli/service/settle.go:28-60` |
| Device pairing | host order "Tailscale / advertise_host / loopback" | ACME domain, `pair.advertise_host`, explicit `listen.host`, Tailscale IPv4, loopback | `internal/cli/pair.go:540-558, 622-633` |
| CLI reference | no `doctor` or `update` | both added, synopsis and table | `internal/cli/root.go:83-84` |
| setup-service flags | no `--from-installer` | row added | `internal/cli/setup_service.go:102-109` |
| YAML surface | `relay` without `max_frame_bytes`; `limits` with 2 of 8 keys; no `receipts` row | all three completed | `internal/config/config.go:72-157` |
| Provider: Grok | `model`: "live 1.0.5 floor is grok-4.6, grok-4.5" | live catalog, else the saved one, else go-llmprovider-sdk's list | `internal/provider/grok/grok.go:21-41`; 0235 |
| Provider: Grok | "`xhigh` is grok-4.6 only" | each live model advertises its levels | the live catalog decides |
| Provider: OpenCode | 1.x keys "ignored with a warning" | a config that sets one fails to load and names it | `internal/provider/opencode/section.go:11-14` |
| Provider: Kilo | "Disabled by default … until acceptance" | enabled by default | `internal/provider/kilo/section.go:45` |
| Provider: Kilo | "kilo 7.4.20" | "kilo 7.8.18" | `internal/provider/kilo/version.go:25` |
| Provider: Kilo | credentials "host-side only until … 0074" | the phone adds a key or starts an engine-run device sign-in | `internal/provider/kilo/auth.go`, `device_auth.go` |
| mcrelay CLI | no `update`; setup-service "systemd / launchd" | `update` added; the same backends as mcremote | `internal/relay/cli.go:80-84` |
| Mobile companion | emulator: "opt-out of the `tailscale` default" | the default is `127.0.0.1` | `internal/config/config.go:544` |
| Mobile companion, Documentation | link text `docs/guides/ops-android-signing.md`, `docs/guides/mobile-profiling.md`, `docs/decisions/0067-MADR-ios-port.md` | the `apps/mobile/docs/…` paths the links point to | the link targets |
| CI / CD | job table: `go`, `flutter`, `android-apk`, `release` | `go`, `go-native`, `live-supervisors`, `flutter`, `flutter-windows`, `android-apk`, `build`, `publish`; no `release` | `ci.yml:42-783` |
| CI / CD | `go` job: "version-allocator tests"; builds binaries on a tag | its real steps; go-selfupdate-lib's `build` job makes the binaries | `ci.yml:42-186, 750-759` |
| CI / CD | "Flutter 3.47.2"; `android-apk` "tag only" | 3.47.9; tag or manual dispatch | `ci.yml:33, 465` |
| CI / CD | build tags "CI enforces" for binaries | local `make` builds carry them; release binaries are cgo-free with no tags | `scripts/verify-build-metadata.sh`; the `build` workflow |
| macOS binary | `./mcremote-darwin-arm64-*` | `./mcremote-darwin-arm64` | go-selfupdate-lib's canonical asset names |
| Headless smoke | the snippet as written | adds that `auth.require_client_key` defaults to `true`, and `fake` is off by default | `internal/config/config.go:557`; `scripts/smoke-protocol/main.go:51` |
| Development | `make lint  # golangci-lint` | `# golint` | `Makefile:607-608` |
| Development | check-libs: two libraries | three, with go-llmprovider-sdk | `check_libs.json` |
| Development | grok and kilo: "no make shorthand yet" | `make live-grok`, `make live-kilo` | `Makefile:381, 394` |
| Repository layout | `ws/` "protocol-v1"; no `protocol/`, `certs/`, `receipt/`, `updateclient/`; `scripts/` "hooks" | v1/v2; the four packages added; "records and CI checks" | the tree |
| Architecture & key decisions | 0074 "(proposed; not yet implemented)" | implemented; P22's live run outstanding | 0074's status line |
| Architecture & key decisions | 0065 and 0020 with no note | 0065 superseded for the host update path; 0020 is the 1.x provider 0214 replaced | their status lines; 0214's Related records |

### Corrections, other entry points

| File | Was | Now | Evidence |
| --- | --- | --- | --- |
| `apps/mobile/docs/architecture.md` | "Flutter pin: **3.47.5**" | 3.47.9, CI's `FLUTTER_VERSION` | `ci.yml:33` |

`docs/architecture.md`, `docs/README.md` and `apps/mobile/docs/README.md` were checked and are unchanged: the receipts
path, the Windows Known Folders, the packages named, the endpoints and the "I want to…" rows all match the tree.

### Doc-versus-code differences (Q3)

Places where the doc described behaviour the code does not have, rewritten to the code:

- the kilo provider's default (`enabled: true`, not `false`);
- the OpenCode 1.x keys (refused, not ignored with a warning);
- the agent-run update under runit, s6 and OpenRC (hands off, not refuses);
- the setup-service start wait (60 s or 75 s, not 15 s);
- the pair-QR host order;
- the CI job set and what each job builds.

None looks like the code being the one that is wrong; each matches a later record (0208 P8, 0212, 0207 D42, 0203,
0054).

### Not covered by P3

The unnumbered docs added by PLAN P3 Deviation 2 (the coding standards and `apps/mobile/README.md`) are P4's and P6's.

## P4 — install, configuration and operations guides, and the Go standards

`config.md`, `config-mcrelay.md`, `ops-linux-install.md`, `ops-windows-install.md`, `ops-macos-tcc.md`,
`ops-mcrelay.md`, `ops-credential-recovery.md`, `ops-hardware-validation.md`, `headscale.md`, `iam-route53-acme.md`,
`ops-codex-contract.md`, and the nine files of `docs/guides/standards/go/`.

### Method

P3's checker and resolver, then a read-through of each guide against the code. Two read-only helper passes, one at a
time, read the eleven guides in full; their load-bearing claims were spot-checked against the files before any edit.
The Go standards were read directly. go-selfupdate-lib's installer templates and service backends were read at the
pinned commit, and libdns/route53 v1.6.2 in the module cache.

Checker: 219 unresolved references before, 215 after, all in P3's non-error classes, plus provider keys whose middle
segment is a section id (`providers.opencode.bin`), AWS and Windows API names, and the `ci-windows` targets, which
live in `make/ci-windows.mk`. Anchors: none dead in these files.

### Corrections that would have misconfigured a host

| File | Was | Now | Evidence |
| --- | --- | --- | --- |
| `ops-mcrelay.md` | mcremote's `relay: registration_secret:` | `relay.secret` (or `MCREMOTE_RELAY_SECRET`) | `internal/config/config.go:99`. Non-provider sections decode leniently, so the wrong key was ignored and the host never registered |
| `iam-route53-acme.md` | `route53:` beside `letsencrypt:` | nested under `letsencrypt:` | `internal/config/config.go:264`. `tls.route53` was ignored, so the zone ID and profile were dropped |
| `iam-route53-acme.md` | IAM policy grants `route53:ListHostedZones` | removed; `ListHostedZonesByName` is the only listing call | libdns/route53 v1.6.2 `client.go:298, 344, 395, 418` |

### Other corrections

| File | Was | Now | Evidence |
| --- | --- | --- | --- |
| `config.md` | grok `args`: empty uses `--no-auto-update agent --no-leader stdio`; "always gets `--no-auto-update`" | the key-driven global flags, then that argv; a non-empty `args` replaces all of it | `internal/provider/grok/grok.go:225-247`; `acpagent.go:291` |
| `config.md` | grok `reasoning_effort`: which grok model takes which level | each live model advertises its levels | the live catalog decides |
| `config.md` | kilo known-good 7.4.22 | 7.8.18 | `internal/provider/kilo/version.go:25` |
| `config.md` | `pair.advertise_host` empty: Tailscale IPv4, else loopback; `--host` default likewise | follows the bind; Tailscale only for an empty, `tailscale` or wildcard bind | `internal/cli/pair.go:621-635` |
| `config.md` | no `--copy` row; `--permission` "Permission id" | `--copy` added; "or handoff nonce" | `internal/cli/pair.go:199, 225`; `receipts.go:273` |
| `config.md`, `config-mcrelay.md` | Route 53 `max_retries` `0` = "SDK default" | the provider's default, five attempts | libdns/route53 `client.go:142-154` |
| `config.md` | env table without the `limits` keys beyond two | six `MCREMOTE_LIMITS_*` rows added (closes 0090-PLAN task 3.3) | `internal/config/env_limits_test.go` |
| `config.md` | a macOS log row stranded outside its table | a sentence | layout |
| `config-mcrelay.md` | hardening "plus `PrivateDevices` and `RestrictNamespaces`" | both deliberately absent | `internal/cli/service/mcrelay.user.service.tmpl:73-82` (MADR 0099 F4a) |
| `config-mcrelay.md` | every variable listed in the deploy unit | most; `MCRELAY_LIMITS_MAX_CONNS` is not | `deploy/systemd/mcrelay.user.service` |
| `ops-mcrelay.md` | outer TLS "files or ACME HTTP-01" | adds DNS-01 | `internal/relay/tls.go:56-125` |
| `headscale.md` | `tailscale` "the default in every shipped launch path" | set by the mesh config and start script; the seed config keeps `127.0.0.1` | `configs/config.mesh-grok.yaml:12`; `defaults_mcremote.yaml` |
| `headscale.md` | connect to `ws://…` | `wss://…`, TLS on by default | `internal/config/config.go:328-334` |
| `iam-route53-acme.md` | citations by line number of `headscale.md` (stale) | by section | `headscale.md` §3 |
| `ops-linux-install.md` | an older release is refused with exit `2` | exit `1` before any download when its assets are not found; `2` after a failed version check | go-selfupdate-lib `install.sh:350-356, 503-505` |
| `ops-linux-install.md` | binaries built with `-tags netgo,osusergo` | release binaries cgo-free with no tags; `make build` adds the tags | `scripts/verify-build-metadata.sh:4-6` |
| `ops-linux-install.md` | CI runs runit and s6 "on every push" | pushes to `master`, tags and pull requests, except Markdown-only changes | `ci.yml:8-14` |
| `ops-macos-tcc.md` | agents "claude/opencode/codex" | codex, grok, kilo, opencode | `internal/provider/registered/registered.go:20-23` |
| `ops-macos-tcc.md` | signing keeps the grant across updates; unsigned builds "re-ask" | `update` re-signs only with `MC_CODESIGN_IDENTITY` set; unsigned builds lose the grant, usually silently | `internal/updateclient/client.go:360-365`; `internal/cli/service/advisories.go:60` |
| `ops-credential-recovery.md` | "Exactly two are retained" | at most two | `internal/providerauth/manifest.go:94-95` |
| `ops-credential-recovery.md` | output has "timestamps"; exit codes 0, 2, 3 | state and `recovery_available`; adds exit `1` | `internal/cli/auth_recovery.go:33-41, 79-82, 144-147` |
| `ops-hardware-validation.md` | the read deadline is 60 s, reset only by the app ping | `limits.ws_read_deadline_seconds`, default 120 s; on v2 any data or transport pong extends it | `internal/config/config.go:565-568`; `internal/ws/server.go:690-695` |
| `ops-hardware-validation.md` | C4: a `pair create` token pairs as is | needs `auth.require_client_key: false` | `internal/config/config.go:557`; `internal/ws/server.go:1504-1531` |
| `ops-hardware-validation.md` | C5: `pair prune` clears it | `pair prune --stale <duration>`; a bare `prune` fails | `internal/cli/pair.go:305-310` |
| `standards/go/core.md` | the Makefile stamps `main.version`, `main.commit`, `main.date` | go-selfupdate-lib `buildinfo` (version and build kind); no date | `Makefile:1-23`; `cmd/mcremote/main.go` |
| `standards/go/core.md`, `README.md` | `netgo,osusergo` are release-build defaults | Linux `make` builds only; release binaries carry no tags | `Makefile:94-110`; `verify-build-metadata.sh:4-6` |
| `standards/go/cli.md` | five subcommands; `main.go` does "version injection"; `-h` the only short flag | ten subcommands; handoff environment then execute; `update`'s `-y` | `internal/cli/root.go:77-86`; `cmd/mcremote/main.go` |
| `standards/go/config.md` | no word on provider sections | per-provider strict sections, and their variables | `internal/config/sections.go` |
| `standards/go/testing.md` | live tags `live_grok`, `live_opencode` | adds `live_codex` (and its parts) and `live_kilo` | the `//go:build` lines |

`ops-windows-install.md` and `ops-codex-contract.md` were checked in full and are unchanged. The other Go standards
(`concurrency`, `logging`, `network`, `session`) match the code.

### Doc-versus-code differences (Q3)

The two silent misconfigurations above, and the read-deadline description, are places where a guide described
something the code does not do. None looks like the code being wrong.

### Findings for the owner, from P4

- **`deploy/systemd/mcrelay.user.service` still sets `PrivateDevices=true` and `RestrictNamespaces=true`** (lines
  131-132). MADR 0099 F4a removed both from the embedded template because they make a user unit fail to start on
  every host (`218/CAPABILITIES`). The guides point readers at this file as the manual unit. It is a code change, not
  this audit's.
- **Unverified, left as written:** `ops-hardware-validation.md` F6's "expect ≤60 s" lingering bound (a device pass's
  expected result; it may share the old 60 s deadline), and `ops-codex-contract.md`'s "an identical 164 methods",
  which may be an older capture's count.
