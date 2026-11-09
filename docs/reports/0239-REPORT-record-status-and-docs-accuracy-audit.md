# Record status and docs accuracy audit, 2026-11

<!-- markdownlint-disable MD013 -->

Started 2026-11-08, at `master` `cb68b744`. This report records what the audit of
[0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)
found and corrected. It decides nothing: each correction cites its evidence in the record it changed, and here.
Its sections were filled phase by phase, as
[0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md)
ran:

- P1, the open records;
- P2, the `completed` spelling;
- P3, the entry points;
- P4, the install, configuration and operations guides, with the Go standards;
- P5, the protocol and client guides;
- P6, the mobile tree's guides;
- P7, the close-out: every doc's result in one table, and the findings in one list.

The audit is complete.

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
  *Resolved 2026-11-08 by
  [0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md](../decisions/0240-MADR-make-the-shipped-user-units-match-the-templates-and-gate-them.md)
  and its PLAN: both directives removed and the units gated against their templates (`6178ad32`), the guides' note
  (`ffbda60f`), and the host scope corrected (`31e21118`, `1ff7fe58`). "Every host" was too broad. The 218 happens
  where the user manager cannot create a user namespace with capabilities, which includes stock Ubuntu 24.04 and
  later (0240-MADR A1). The `config-mcrelay.md` row above that reads "most; `MCRELAY_LIMITS_MAX_CONNS` is not" was
  reverted to "every" by 0240-MADR D6, when the unit gained that variable.*
- **Unverified, left as written:** `ops-hardware-validation.md` F6's "expect ≤60 s" lingering bound (a device pass's
  expected result; it may share the old 60 s deadline), and `ops-codex-contract.md`'s "an identical 164 methods",
  which may be an older capture's count.

## P5 — protocol and client guides

`protocol-v1.md`, `protocol-v2.md`, `receipts.md`, `chat-performance.md`.

### Method

- **The reference checker:** 225 unresolved before. 217 of them were dotted names the checker reads as config keys,
  which in these guides are wire types and payload paths. A second script (`msgtypes.py`) resolved each one as a
  string literal in non-test Go or Dart source. It was seen failing first on a planted `session.frobnicate_result`.
  Two were left over: `header.payload.signature` (the JWS notation) and `providers.opencode.allow_remote_share`,
  which the guide says was removed. The other eight were prose (`mcremote session`) or marked history
  (`codex 0.145.0`, OpenCode's `ToolStateCompleted`). `internal/receipt.Store` exists (`store.go:31`).
- **The reverse direction:** a script (`undocumented_types.py`) listed every `Type*` wire constant in
  `internal/protocol` that neither protocol guide mentions. It was seen failing first on a planted name. One of 100
  was missing: `oauth.device_flow_update`.
- **The error codes:** a script (`errcodes.py`) compared `ErrorCodes()` with the reference section. It was seen
  failing first on a planted code. 18 of 88 registered codes were outside the reference that claims to list every
  code.
- **The read-through:** `receipts.md`, `chat-performance.md` and `protocol-v2.md` were read in full against the code.
  `protocol-v1.md` (2,584 lines) was read in full by one read-only helper (C5), and each of its 17 findings was
  checked against the cited lines before any edit.
- **The anchors:** the known dead `receipts.md:197` anchor. The resolver was seen catching a planted dead same-file
  anchor, since P5 adds same-file links.

### Corrections, `protocol-v1.md`

| Was | Now | Evidence |
| --- | --- | --- |
| read deadline 60 s | `limits.ws_read_deadline_seconds`, default 120 s, at least 15 | `internal/config/config.go:568, 595` |
| "No connection replacement" on v1 | every successful `auth` closes the device's older sockets with 4001 `replaced`, unless device tokens are off | `internal/ws/server.go:1440, 1444-1482` |
| a `since_seq` below the ring is silently truncated on v1 | every `session.history_result` carries `first_seq`/`latest_seq` | `internal/ws/server.go:1928-1932` |
| the durable transcript is "the same 800-event tail" | bounded by the same 32 MiB budget | `internal/session/store.go:302, 352-356` |
| admin socket at `$data_dir/admin.sock` | `<runtime_dir>/admin.sock`, as `config.md` says | `internal/appdirs/paths.go:99-101` |
| clipped at `maxToolOutputChars = 8000` | `output.MaxToolShown = 8000` | `internal/provider/output/output.go:26` |
| six idempotent mutations | adds `session.set_collaboration_mode` | `internal/ws/server.go:1149-1155` |
| `keyring_managed` as an error code | registered, never sent as a code; it arrives as a method `reason` | `internal/protocol/messages.go:783`; no emit site |
| `unknown_provider`: "outside the five agents" | four | `internal/config/prewarm_write.go:28` |
| `auth_error` and `pair_error` lists | add `already_authed`; add `bad_version` and `bad_payload` | `internal/ws/server.go:1283, 1291-1293, 3568` |
| `permission_denied`, `unavailable`: one meaning each | also the Codex-surface refusal, and the missing-codex-provider answer | `internal/ws/codex_handlers.go:123, 135, 166` |
| the reference "lists every code" | names the 18 codes described with their operations, with links | `errcodes.py`: 88 of 88 after |
| `projects.list` returns at most 100 projects, sorted | no provider implements discovery today: OpenCode answers `unsupported`, Kilo `projects_list_failed` | `internal/provider/httpagent/provider.go:339-351`; no `ListProjectsLive` implementation |
| `agents.list` and `session.create.agent` are OpenCode's | Kilo's: only Kilo implements the catalog and validates the agent | `internal/provider/kilo/catalog_live.go:16`, `kilo/mode.go:95-105`; acpagent never reads the agent |
| `commands.list`: "OpenCode maps engine `GET /command`" | Kilo does; the ACP providers advertise theirs on `available_commands` | `internal/provider/kilo/command.go:27`; `internal/ws/server.go:3370` |
| `thinking_level`: grok at spawn only, OpenCode ignores it | the ACP providers send it in `session/new` `_meta` and change it mid-session | `internal/provider/acpagent/acpagent.go:1067-1079`; MADR 0106 |
| slash commands: grok `/model` restarts, `/compact` unavailable, `/thinking` spawn-only; OpenCode `/model` by HTTP, `/goal` unavailable, `/thinking` absent | grok and OpenCode `/model` in place; `/compact` and `/goal` forwarded; `/thinking` mid-session (OpenCode while the model has variants); the `remote_commands` example uses grok's `/diff` | `internal/provider/grok/commandtable.go:13-45`; `internal/provider/opencode/commandtable.go:16-42` |
| the OAuth table | adds `oauth.device_flow_update`, `provider_id` and `upstream_id?` on `oauth.device_flow`, and the owned path's `state`, `retryable`, `backup_state`, `recovery_available` on the result | `internal/protocol/messages.go:915-963`; `internal/ws/provider_auth_owned.go:103-150` |

The helper also checked these clean: envelope and frame caps, auth and pairing apart from the lists above, the
session lifecycle numbers, history paging, tool-output bounds, workspace and shell bounds, provider-auth catalog
paging, the catalog cache, the Codex caps, receipts, `devices.list`, `session.rename`, the self-signed certificate,
`/healthz` and `/v1/hello`.

### Corrections, the other three guides

| File | Was | Now | Evidence |
| --- | --- | --- | --- |
| `receipts.md` | link to `config.md#locations-xdg` | `config.md#locations` | `config.md` heading `## Locations` |
| `chat-performance.md` | `kMaxTranscriptItems` 800 | 4,000 | `apps/mobile/lib/data/chat/chat_models.dart:67` |
| `chat-performance.md` | host `historyBufferCap` 800-event ring | `historyBudgetBytes`, 32 MiB per session | `internal/session/manager.go:52` (MADR 0138) |
| `chat-performance.md` | client history `limit: 800` | `kHistoryFetchLimit` 200, at most `kHistoryMaxPages` 32 per walk | `chat_models.dart:75, 80` |
| `chat-performance.md` | "client cap (800) can exceed the host ring (500)"; prefer `limit: 500` | the two budgets are independent; the phone is the tighter cap | `chat_models.dart:60-73`; `manager.go:41-52` |
| `chat-performance.md` | long-stream plain-text path above `kMaxStreamingMarkdownChars` | one `MarkdownBody` engine for stream and finalize | `apps/mobile/lib/features/chat/chat_bubble.dart:1121-1125`; mobile MADR 0056, Phase 7 |
| `chat-performance.md` | throttle "120 / 200 / 320 ms" | per frame; 120 ms above 4k, 200 ms above 16k | `chat_bubble.dart:1033-1037`; `chat_models.dart:101-102` |
| `chat-performance.md` | expanded detail max height ~240 | 200 | `chat_bubble.dart:1627` |
| `protocol-v2.md` | caps example `read_deadline_ms: 60000` | 120000, the default | `internal/config/config.go:568` |

`receipts.md` was otherwise checked against `internal/receipt`, `internal/cli/receipts.go`, the `receipts` cap and the
phone's screen, and is accurate. The rest of `protocol-v2.md` matches: the frame cap, ping cadence, resume window,
`history_ring` and `history_budget_bytes`, TCP keepalive, close codes 4001 and 1013, the Codex surface limits and
timeouts, the relay's first-envelope deadline and sweep, and the client's 60 s clamp.

### Doc-versus-code differences (Q3)

- **Prompt attachments** (`protocol-v1.md`). The guide said the daemon rejects non-canonical base64, an unsafe
  `filename`, a mismatched MIME family and oversized fields, and rejects a whole prompt whose block the model does not
  take, because "silently dropping a block would leave the agent a prompt referring to an attachment it never
  received". No code validates any of it: `Filename` is only copied (`internal/ws/server.go:2037`). On the ACP path,
  a block the agent did not advertise is dropped with a warning (`internal/provider/acpagent/session.go:598-627`).
  The guide now says what the code does. Whether the daemon should validate is the owner's call (below).
- **Project discovery, the agent catalog and the command catalog** described OpenCode's 1.x HTTP routes. OpenCode
  now runs over ACP (0208), and the guide now names Kilo, the one provider with those routes.

### Findings for the owner, from P5

- **Attachment validation is documented as a safety property and is absent.** The old text gave a reason for
  rejecting rather than dropping. Today an ACP provider drops an unadvertised block, and no field is validated. A
  decision record would settle whether to restore the documented behaviour.
- **`session_cancel_timeout` is declared in `internal/protocol/errors.go:49` and sent
  (`internal/ws/server.go:2126`), but missing from `ErrorCodes()`.** The registry the guide's tests check against
  does not contain it, so neither test can notice if its documentation goes. Code, not this audit's.
  *Resolved 2026-11-09 by
  [0241-MADR-register-every-sent-error-code.md](../decisions/0241-MADR-register-every-sent-error-code.md) and its
  PLAN: `session_cancel_timeout` registered, with `not_found`, a second sent code that was neither registered nor
  documented, and two checks that fail on a declared or helper-returned code left out of the registry
  (`711201e9`).*
- **Unverified, left as written:** `chat-performance.md`'s "Host dedup" row (`usage_update` only on change,
  `session_status: running` only on transition). No dedup site was found by search, and none was ruled out.

## P6 — the mobile tree's guides

`apps/mobile/README.md`, `apps/mobile/docs/guides/*.md` (four guides) and
`apps/mobile/docs/guides/standards/mobile/*.md` (seven standards).

### Method

- **The reference checker:** 21 unresolved, all in the earlier classes: filenames read as keys (`poster.png`,
  `build.gradle.kts`), Dart and Riverpod API names (`ref.watch`, `context.mounted`), Flutter and SDK flags
  (`--analyze-size`, `--jdk-dir`), a build output path, and prose. The application id
  `com.maccavelli.magic_cli_remote` matches `android/app/build.gradle.kts:46, 58`. Anchors: none dead.
- **The read-through:** all twelve files in full, against `apps/mobile`, its build files, `ci.yml` and the daemon.
- **The standards' concrete claims:** a script (`p6_claims.py`) checked 28 of them: build versions, manifest flags,
  entitlements, and named files and symbols. It was seen failing first on a planted dependency. All 28 match,
  including the absence of `UIBackgroundModes`.

### Corrections

| File | Was | Now | Evidence |
| --- | --- | --- | --- |
| `apps/mobile/README.md` | providers grok, opencode, codex, fake | adds kilo | `internal/provider/registered/registered.go:20-26` |
| `apps/mobile/README.md` | connect by code, QR or token, with no condition | a self-signed daemon needs the QR or the pasted pair URI, which carry the fingerprint; a typed code or bare token works only with a publicly trusted certificate | `apps/mobile/lib/features/connect/connect_screen.dart:604-611, 872-905`; `lib/data/ws/mcremote_client.dart:359-376`; `internal/pairuri/pairuri.go:125` |
| `apps/mobile/README.md` | emulator: "Token: Scan QR or paste `mcr_…`" | paste the `Pair URI:` line, or scan; a bare token carries no fingerprint | the same; `internal/cli/pair.go:167` |
| `apps/mobile/README.md` | "iOS / Web: Not enabled" | iOS implemented, verified on the Simulator only | mobile `0067-MADR-ios-port.md` status; `ops-ios-signing.md` |
| `apps/mobile/README.md` | "Signed with debug keys for now" | debug key unless `key.properties` exists; tagged releases signed with the upload key | `android/app/build.gradle.kts:13-31`; `ci.yml` "Provision release signing" |
| `apps/mobile/README.md` | "This machine currently has no Android emulator"; "Android SDK is already installed at `$HOME/Android/Sdk`"; "a headless AWS box" | general wording | one host's state, written as the guide's |
| `apps/mobile/README.md` | layout without `theme/` | adds `theme/`, and `settings` under `features/` | `apps/mobile/lib/` |
| `ops-android-emulator.md` | "the QR is the only way in" | pasting the pair URI also carries the fingerprint; the QR route tests the scanner | `connect_screen.dart:872-905` (the paste button dates from `0bb7cf87`, before the guide) |
| `mobile-profiling.md` | Flutter 3.47.2 / Dart 3.13.2 | 3.47.9 / 3.13.7, the CI pin | `ci.yml:33`; `flutter --version` |
| `mobile-profiling.md` | two links labelled `apps/mobile/README.md` that pointed at `standards/mobile/README.md` | `../../README.md` | the link targets |
| `standards/mobile/architecture.md` | `go_router ^17.3.0` | `^18.0.0` | `apps/mobile/pubspec.yaml:19` |
| `ops-android-signing.md` | the APK job is "tag-only" | on a tag or a manual dispatch | `ci.yml:465` |

The rest of `ops-android-signing.md` matches: the four secrets, both step names, the pinned-digest variable, the
canonical-repo check, the gitignore lines and the app's strings. `ops-ios-signing.md` is unchanged. `android.md`'s "until service-owned connection (MADR 0056 H-5b)" stands: 0056 still
defers H-5b.

### Findings for the owner, from P6

- **A rule the code does not follow:** `standards/mobile/architecture.md` says to use `Notifier` / `AsyncNotifier`
  with `AsyncValue.guard()` for async state. Nothing in `apps/mobile/lib` uses `AsyncNotifier` or
  `AsyncValue.guard`. A standard is a rule, not a description, so it is left as written; whether to keep the rule is
  the owner's call.

## P7 — close-out

### Every unnumbered doc

The 24 docs of the MADR's inventory and the 17 its amendment added: 41 docs. "Checker, unresolved now" is the
reference checker's count on the doc as committed. Every one was read in context in its phase, and each phase's
section above names the classes it fell into: wire types and payload paths read as config keys, filenames, provider
keys, Dart and Flutter API names, external tool flags, prose, and marked history. None is a stale reference.
"Changed" means a 0239 commit edited the doc. "No, checked" means it was read in full against the tree and found
accurate. Three of them, `README.md`, `ops-mcrelay.md` and `config-mcrelay.md`, were also edited by 0240.

| Doc | Phase | Checker, unresolved now | Changed |
| --- | --- | ---: | --- |
| `README.md` | P3 | 50 | yes |
| `apps/mobile/docs/README.md` | P3 | 0 | no, checked |
| `apps/mobile/docs/architecture.md` | P3 | 2 | yes |
| `docs/README.md` | P3 | 26 | yes |
| `docs/architecture.md` | P3 | 7 | no, checked |
| `docs/guides/config-mcrelay.md` | P4 | 7 | yes |
| `docs/guides/config.md` | P4 | 80 | yes |
| `docs/guides/headscale.md` | P4 | 16 | yes |
| `docs/guides/iam-route53-acme.md` | P4 | 54 | yes |
| `docs/guides/ops-codex-contract.md` | P4 | 4 | no, checked |
| `docs/guides/ops-credential-recovery.md` | P4 | 3 | yes |
| `docs/guides/ops-hardware-validation.md` | P4 | 1 | yes |
| `docs/guides/ops-linux-install.md` | P4 | 18 | yes |
| `docs/guides/ops-macos-tcc.md` | P4 | 3 | yes |
| `docs/guides/ops-mcrelay.md` | P4 | 12 | yes |
| `docs/guides/ops-windows-install.md` | P4 | 16 | no, checked |
| `docs/guides/standards/go/README.md` | P4 | 2 | yes |
| `docs/guides/standards/go/cli.md` | P4 | 0 | yes |
| `docs/guides/standards/go/concurrency.md` | P4 | 0 | no, checked |
| `docs/guides/standards/go/config.md` | P4 | 0 | yes |
| `docs/guides/standards/go/core.md` | P4 | 1 | yes |
| `docs/guides/standards/go/logging.md` | P4 | 0 | no, checked |
| `docs/guides/standards/go/network.md` | P4 | 0 | no, checked |
| `docs/guides/standards/go/session.md` | P4 | 0 | no, checked |
| `docs/guides/standards/go/testing.md` | P4 | 0 | yes |
| `docs/guides/chat-performance.md` | P5 | 0 | yes |
| `docs/guides/protocol-v1.md` | P5 | 214 | yes |
| `docs/guides/protocol-v2.md` | P5 | 12 | yes |
| `docs/guides/receipts.md` | P5 | 4 | yes |
| `apps/mobile/README.md` | P6 | 2 | yes |
| `apps/mobile/docs/guides/mobile-profiling.md` | P6 | 2 | yes |
| `apps/mobile/docs/guides/ops-android-emulator.md` | P6 | 4 | yes |
| `apps/mobile/docs/guides/ops-android-signing.md` | P6 | 1 | yes |
| `apps/mobile/docs/guides/ops-ios-signing.md` | P6 | 1 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/README.md` | P6 | 1 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/android.md` | P6 | 4 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/architecture.md` | P6 | 3 | yes |
| `apps/mobile/docs/guides/standards/mobile/dart.md` | P6 | 2 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/flutter.md` | P6 | 1 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/ios.md` | P6 | 0 | no, checked |
| `apps/mobile/docs/guides/standards/mobile/networking.md` | P6 | 0 | no, checked |

`docs/README.md`'s records table was regenerated by `check_records.py --write-index` in each phase that changed a
status, and never edited by hand. Its "I want to…" rows were checked in P3.

### Records closed at the close-out

- **0090-PLAN** `in-progress` → `complete`. Its last two tasks landed in this audit: 4.1, the README's kilo version
  (`39b83ba9`, P3), and 3.3, `config.md`'s `MCREMOTE_LIMITS_*` rows (`d493b32a`, P4).

### Findings for the owner, all phases

Nothing below was changed by this audit. Each is in its phase's section with the evidence.

| Finding | Phase | Kind |
| --- | --- | --- |
| 0205's tag ruleset was never created; the decision's one action is not done | P1 | a decision not carried out |
| eight proposals idle since August or September (0113, 0117, 0121, 0131, 0135, 0146, 0157, 0181) | P1 | backlog to keep or close |
| 0213 is partly overtaken by 0217 D6 and 0203 | P1 | a record to amend or close |
| 0227's configuration landed in an unrelated commit (`254279ac`) | P1 | history, no action |
| `.gobble/settings.yaml` line 4 (`allowBypass`) has no effect in a project file | P1 | a config change under an amendment of 0227 |
| `deploy/systemd/mcrelay.user.service` kept the two directives 0099 F4a removed | P4 | **resolved by 0240** |
| `ops-hardware-validation.md` F6's "≤60 s" bound and `ops-codex-contract.md`'s "164 methods" | P4 | unverified, left as written |
| prompt attachments: the guide promised validation and whole-prompt rejection that the code does not do | P5 | doc-versus-code; a decision on whether to validate |
| `session_cancel_timeout` is sent but missing from `ErrorCodes()` | P5 | **resolved by 0241** |
| `chat-performance.md`'s "Host dedup" row | P5 | unverified, left as written |
| the mobile architecture standard prescribes `AsyncNotifier` with `AsyncValue.guard()`, which no code uses | P6 | a rule to keep or drop |
