---
status: accepted
date: 2026-10-30
decision-makers: Project Owner
consulted: "0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md (D1 to D5, D11, D12, D14, D15 and its amendments to 2026-10-21); 0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md, amendment I (I-4); 0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md; 0137-MADR-prompt-to-first-token-latency-regression.md (the known-good engine pins); 0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md (D2); 0174-MADR-keep-dependencies-free-of-known-advisories.md; magic-git's scripts/tools/devenv/version_audit.py and standard.json, and the history of standard.json; 0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md (P0, P6, P7, P12 to P14, Deviations 20 and 21); this repository at 07ee60b2 with 0216's P2 staged"
informed: anyone who changes go.mod, pubspec.yaml, the Android build files, a workflow pin or a tool pin, or magic-git's devenv standard
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Update every pin to its current release, security fixes first

## Context and Problem Statement

On 2026-10-29 the owner asked: "i want to update all pins to current versions".

This repository pins versions in eleven places: the Go toolchain, Go modules, the owner's two
libraries, CI actions, CI tool versions, Node, Java, the Flutter SDK, Dart packages, the Android
build tools, and each agent engine's known-good version. Some pins are governed by records already:

* [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md):
  the toolchains, by a rule, D1;
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  the owner's libraries;
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  the engine pins.

Others have no record: actions, tool versions, Dart packages and Gradle.

This record takes one inventory of all of them against their publishers, decides what "current"
means for each, and puts the moves in order. One of them closes a high-severity advisory in the
shipped Android app.

On 2026-10-30 the owner added the host standard in magic-git: "we run 1.27.3, update the node
version/pin and ensure the python is latest 3.14 without advisories". The owner also drew the
scope's edge: "when i said to update all pins i did not mean for tooling (python) or pins outside
of the magic-cli-remote codebase itself, eg: node, etc, unless they have active advisories." So
the host standard and the host installs are in scope only where an advisory is active (D1, D11).

### The inventory [observed 2026-10-29 from each pin's source; `leftovers/pins_latest.py`, `leftovers/android_bounds.py`, `leftovers/engine_versions.py`, `flutter pub outdated --json`, `go list -m -u all`, and magic-git's `version_audit.py`]

**F1. Go toolchain: current.** `go.mod` declares `go 1.27.3`; go.dev's stable releases are
`go1.27.3` and `go1.26.10`. 0169 D2 reads 1.27.3. Nothing moves.

**F2. Go modules: three direct and many indirect are behind.**

| Module | Pinned | Newest |
| :--- | :--- | :--- |
| `github.com/caddyserver/certmagic` | v0.25.4 | v0.25.6 |
| `golang.org/x/mod` | v0.41.0 | v0.42.0 |
| `github.com/maccavelli/go-selfupdate-lib` | v1.22.3 | ~~v1.23.0~~ ~~v1.23.1 (F3, F3a)~~ **v1.24.0** (F3e) |
| `github.com/maccavelli/acp-go-sdk2` | v1.26.0 | ~~v1.26.0, current~~ ~~v1.27.0 (F3b)~~ ~~v1.28.0 (F3c)~~ ~~v1.31.1 (F3d; v1.30.0 came by 0224)~~ ~~v1.31.2 (F3e)~~ ~~v1.33.0 (F3f)~~ ~~v1.34.0 (F3g)~~ **v1.34.2** (F3h) |
| `github.com/maccavelli/go-llmprovider-sdk` | (not yet required; v1.27.0 since 0235) | **v1.28.0** (F3h) |
| `github.com/maccavelli/go-tui-lib/v2` | (not yet required; v2.7.0 since 0219) | ~~v2.9.0 (F3d)~~ **v2.12.0** (F3f) |

* `go list -m -u all` lists 33 indirect modules with a newer version. They include the AWS SDK used
  by `libdns/route53` (`aws-sdk-go-v2` v1.39.1 → v1.47.5, `service/route53` v1.58.3 → v1.70.6),
  `x/tools` v0.51.0 → v0.52.0, `miekg/dns`, `mholt/acmez/v3`, `go-viper/mapstructure/v2` and
  `pelletier/go-toml/v2`. Some, such as `stretchr/testify` and `yuin/goldmark`, are in the module
  graph but not in the build.
* `govulncheck` is clear, apart from the allowlisted `GO-2026-5932` (`x/crypto/openpgp`, required
  module only). OSV lists no fixed version for it (`introduced: 0`, no `fixed`), so no update can
  clear it.

**F3. `go-selfupdate-lib` v1.23.0 changes behaviour Magic can see.** 0207's I-4 predicted it. Its
`CHANGELOG.md` at the tag:

* `service.Supervised.Start` is `ErrNotInstalled` when the supervisor reports the service not
  installed, where it ran `Up`. Magic's runit, s6 and OpenRC backends run through `Supervised`
  (0212).
* launchd: `Start` on a missing property list is `ErrNotInstalled` at once. `Stop`'s `bootout` and
  its wait share one bound.
* systemd: `Start` of a `Type=notify` unit that exits before ready also wraps `ErrUnhealthy`.
* scm: `Start` on an ended context starts nothing.
* On Unix, the install resolves every name through the `os.Root` of the directory it locked.
* Added: pinned update plans (`Updater.Plan`, `Apply`, `ErrPlanStale`), the handoff carrying the
  plan, and `selfupdate/service/servicetest`.

Magic already treats `ErrNotInstalled` as "not installed" on the supervisor paths
(`internal/cli/service/supervisortool.go:209`, `:227`; `supervisorstate.go:94`, `:206`;
`setup.go:243`). Both `ci.yml` pins (`:743`, `:764`) name v1.22.3's commit, and
`TestWorkflowPinsMatchGoMod` ties them to `go.mod`.

**F3a. `go-selfupdate-lib` v1.23.1 is current, and adds nothing Magic can see.** *Observed
2026-10-30, after the inventory (`leftovers/gsl_peel.py`).*

* It is tagged 2026-10-30. Its annotated tag `b0c08a0e…` peels to commit
  `c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e`. The Go proxy lists it, and `go list -m -u` now offers
  it as the newest version.
* It is four commits past v1.23.0, with nothing behind. Only `go.mod`, `go.sum` and documents
  change. No file under `.github/` changes, so the two reusable workflows are v1.23.0's.
* Its `CHANGELOG.md`: it requires `golang.org/x/sys v0.49.0`, `x/term v0.47.0` and `x/mod v0.42.0`,
  and "No exported identifier and no behaviour a program can see changes: `make apicheck` reports
  `v1.23.1` compatible with `v1.23.0`".
* Its `go.mod` says `go 1.26.10`, below Magic's `go 1.27.3`.
* Magic already requires `x/sys` v0.49.0 and `x/term` v0.47.0, and D4 already moves `x/mod` to
  v0.42.0. So v1.23.1 moves no module that D4 would not.

F3's behaviour changes all come from v1.23.0, and v1.23.1 carries them unchanged.

**F3b. `acp-go-sdk2` v1.27.0 is current, and adds a package Magic does not import.** *Observed
2026-10-30, at PLAN P3 step 1 (`leftovers/release_diff.py`, and the release notes at the tag,
`docs/reports/0043-REPORT-release-notes-v1-27-0.md` in that repository).*

* It is tagged 2026-10-30T17:01Z, after the inventory. Its annotated tag `7c89dd4b…` peels to
  commit `913575f6c7fbf4c67ada357a3597293f43e09d2b`. P3's `go get -u ./...` took it.
* It is 13 commits past v1.26.0, with nothing behind. It adds one package, `remote` (ACP over
  Streamable HTTP and WebSocket), and `example/remote`. The release notes say `agentproc/`,
  `acptest/`, `clienthost/`, `internal/`, the schema and every generated file are unchanged, and
  that "nothing the root package, `agentproc`, `acptest` or `clienthost` sends or accepts changes".
* Its `make api` against v1.26.0: "0 incompatible, 11 compatible", each one in `remote`. `remote`
  is outside its compatibility promise.
* Its `go.mod` gains `github.com/coder/websocket v1.8.15`, "compiled only into a program that
  imports `remote`". Magic already requires `github.com/coder/websocket v1.8.15` directly
  (`go.mod:7` before P3), with the same two `go.sum` lines, so the requirement adds nothing here.
  The `go` lines are unchanged.
* 0216's design reads `acp-go-sdk2 v1.26.0`'s `agentproc` and `acptest`. Those are unchanged in
  v1.27.0, so 0216 is unaffected.

**F3c. `acp-go-sdk2` v1.28.0 is current, and changes nothing Magic sees.** *Observed 2026-10-30,
after P5 (`leftovers/release_diff.py`, and `docs/reports/0042-REPORT-release-notes-v1-28-0.md` at
the tag in that repository).*

* Tagged 2026-10-30T23:34Z. Its annotated tag `d376db9a…` peels to commit
  `b390bdf7d3046a7d28899190b7a15dbbbf18e8ff`, 11 commits past v1.27.0, with nothing behind.
* `make api` against v1.27.0: "0 incompatible, 3 compatible": `acp.ErrHandlersRunning`,
  `(*agentproc.Process).CloseContext` and `KillContext`. Nothing on the wire changes. `go.mod`,
  `go.sum`, the schema, `acptest/`, `clienthost/` and `remote/` are unchanged.
* Two behaviours change. `Process.Wait`'s error no longer repeats its cause for an **in-process**
  agent whose error already wraps `ErrKilled` or `ErrTerminated`; "a child's error … unchanged".
  And `ServeAgent`'s error gains `ErrHandlersRunning` when its 5 s bounds pass.
* Magic runs every agent as a child (`agentproc.Command`, `internal/provider/acpagent/acpagent.go:531`),
  tests the sentinels with `errors.Is` (`:644`), and calls no `ServeAgent` and no `InProcess`. Its
  one text check, `agentproc_test.go:247`, is of a child's error. So neither change reaches Magic.
* `CloseContext` and `KillContext`, a close bounded against a client handler that ignores its
  context, are available and not adopted here (0207 D53's rule).

**F3d. On 2026-11-01, two of Magic's three libraries have newer tags, and neither changes what
Magic uses.** *Observed 2026-11-01 (`libs/latest.py`, `libs/tui_apidiff.py` in the session
scratchpad; each library's release notes and `git diff` at its tags).*

| Module | Pinned | Newest tag | Peeled commit, date |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.30.0 (0224) | **v1.31.1** (v1.31.0 between) | `151b6e8b…`, 2026-11-01 |
| `go-selfupdate-lib` | v1.23.1 | v1.23.1, current | (no change) |
| `go-tui-lib/v2` | v2.7.0 (0219) | **v2.9.0** (v2.7.1, v2.8.0 between) | `16349ecd…`, 2026-11-01 |

* **`acp-go-sdk2` v1.31.0** adds sending and receiving middleware (`WithSendingMiddleware`,
  `WithReceivingMiddleware`, `Middleware`, `RequestIDFromContext`, `ErrSendingMiddleware`) and an
  observer for what the connection handles itself (`WithObserver`, `Observation`,
  `ObservationKind` and six kinds). Its `make api` against v1.30.0: "0 incompatible, 14
  compatible". A connection that registers neither sees no change on the wire and no added
  allocation. **v1.31.1** changes nine comment lines in `observer.go`: "0 incompatible, 0
  compatible". The schema stays 1.25.0, and `go.mod`/`go.sum` are unchanged across both
  (`docs/reports/0046-REPORT-release-notes-v1-31-{0,1}.md` at the tag). Magic registers no
  middleware and no observer, and calls none of the new names. Adopting them is 0207's amendment
  L.
* **`go-tui-lib` v2.7.1 to v2.9.0** (16 commits) add terminal view facts, a message catalogue,
  opt-outs, a `Linker`, OSC 8 link bounds in `safetext`, and `inline`/`transcript` reprinting. Its
  `go.mod` is unchanged. Magic imports three packages (`internal/modguard_test.go:216-271`):
  * `safetext`: apidiff shows no change. Its one behaviour change, removing an OSC 8 whose body is
    over 2,048 bytes, applies under `KeepSGR`; Magic calls `safetext.Clean(s, safetext.Line())`
    (`internal/cli/termtext.go:9`, `internal/auth/store.go:39`), and `Line()` is
    `Rules{Flatten: true}`, without `KeepSGR`.
  * `termcap`: apidiff shows only compatible additions (six `Caps` fields, `Messages`,
    `WithReportCatalog` and the `Msg…` catalogue).
  * `termsvc`: apidiff shows only compatible additions (`Linker`, `WithBodyCells`,
    `WithCopyRoute`, `WithTitleCells`, `WithoutBell`). `PrepareCopy` changed order, so a program's
    own clipboard is no longer held to `MaxCopyBytes`; Magic does not call it. `CopyPlan` and
    `CopySequence`, which `internal/cli/pair.go:398-409` calls, are unchanged.
* **`go-selfupdate-lib`** is at its newest tag. Its checkout has records after v1.23.1 (its 0040)
  and no newer tag.
* **gobble** pins `acp-go-sdk2 v1.28.0`. 0184 D12's shared pin is needed only before
  `live-gobble`, so Magic moving ahead changes nothing for it now.
* **`go.yaml.in/yaml/v3`**, found behind at PLAN P3c step 2 (Deviation 17): v3.0.5 → v3.0.6, one
  commit that quotes a literal `<<` string on encode. Dated 2026-10-13, so P3 should have taken it;
  why it did not is not known.

**F3e. On 2026-11-03, two of Magic's three libraries have newer tags. Neither changes an API, and
neither changes anything Magic asserts.** *Observed 2026-11-03 (`git ls-remote` of each repository;
each library's release notes, `CHANGELOG.md` and `git diff` at its tags; `go list -m -u` on
`beb31c3b`).*

| Module | Pinned | Newest tag | Peeled commit, date |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.31.1 (F3d) | **v1.31.2** | `69eff662…`, 2026-11-03 |
| `go-selfupdate-lib` | v1.23.1 | **v1.24.0** | `74ab5dd3…`, 2026-11-02 |
| `go-tui-lib/v2` | v2.9.0 (F3d) | v2.9.0, current | (no change) |

* `go list -m -u` lists exactly these two as the direct modules behind. Neither repository has a
  newer major version on the Go proxy.
* **`acp-go-sdk2` v1.31.2** fixes ten connection-runtime defects, its 0049 R1 to R10
  (`docs/reports/0049-REPORT-release-notes-v1-31-2.md` at the tag in that repository).
  * Its `make api` against v1.31.1: "0 incompatible, 0 compatible".
  * `go.mod`, `go.sum`, the schema (1.25.0), `.github/`, `acptest/`, `agentproc/`, `clienthost/`
    and `remote/` are unchanged.
  * Magic's client connections (`clientConnOptions`, `internal/provider/acpagent/acpagent.go:1223-1236`)
    set `WithNotificationOverflowPolicy(OverflowBlock)`, `WithInboundValidation`, `WithLogger`,
    `WithMaxMessageSize(10 << 20)` and, for a Spec with vendor notifications,
    `WithReceivingMiddleware` (0214 D14).
  * **Three fixes reach those connections.**
    * **R1.** The reader no longer writes its own error answers itself. It queues them for a
      writer goroutine, and a peer that leaves more than 1,024 unread closes the connection.
      Magic's reader answers only an invalid or oversized line, and its agents read their input,
      so the change removes a deadlock Magic could only meet against an agent that stops reading.
    * **R7.** A line ending `\r\n` exactly at `WithMaxMessageSize` is no longer refused.
    * **R9.** A request whose context ended before it was written no longer sends
      `$/cancel_request`. Magic cancels prompts by context, so it no longer sends that stray
      cancel in that race.
  * **The other seven do not reach Magic.**
    * R2 binds an **agent-side** receiving middleware. Magic's middleware is client-side, and its
      two agent-side connections are test agents with none (`stderrtail_test.go:81`,
      `agentproc_test.go:62`).
    * R3 concerns a notification handler that sends on **another** connection. R4 concerns
      `ServeAgent`, which Magic does not call.
    * Magic sets neither of R5 and R6's options, and registers no sending middleware (R8).
    * R10 is documentation.
* **`go-selfupdate-lib` v1.24.0** adds `String`, `GoString`, `Format` and `LogValue` on
  `service.Command`, `service.HandOff`, `service.ExecOptions` and `ghattest.Options`. `fmt` and
  `log/slog` now print each `Env` value as `<redacted>`, `Command.Stdin` as its length, and the
  function fields as `<set>` or `<nil>`. `encoding/json` is unchanged (`CHANGELOG.md` at the tag,
  and its 0040 E1).
  * `go.mod`, `go.sum` and `.github/` are unchanged from v1.23.1, so the two reusable workflows
    `ci.yml` calls are v1.23.1's.
  * Magic builds `Command`, `HandOff` and `ExecOptions` (`internal/cli/service/supervisortool.go:152`,
    `internal/updateclient/client.go:394`, `managed.go:88`). No production code formats one, and no
    test compares a formatted one; tests compare the values. So the change reaches only output
    Magic does not produce today, and makes it safer if it ever does.
  * `ci.yml`'s two `uses:` pins (`:756`, `:777`) name v1.23.1's commit `c24f5919…`, and
    `TestWorkflowPinsMatchGoMod` (`internal/updateclient/spec_test.go:138`) ties them to `go.mod`.
    They move to `74ab5dd3…` with it.
  * The new methods need no adoption. They apply wherever a value is formatted (0207 D53's rule
    has nothing to adopt here).
* **`go-tui-lib`** is at its newest tag. Its checkout has 14 untagged commits past v2.9.0: the
  0009 streaming engine (`stream`, `frame`, `inputfilter`, `streamtea`). Nothing untagged is
  pinned.
* **gobble** pins `acp-go-sdk2 v1.31.1`. As in F3d, 0184 D12's shared pin is needed only before
  `live-gobble`.
  * *Added 2026-11-03, at PLAN P3d.* The owner: "gobble is also moving to current acp-go-sdk2".
    gobble's move is gobble-cli's own record and commit, not this one. Once both land, the two
    repositories share `v1.31.2` again.

**F3f. On 2026-11-04, two of Magic's three libraries have newer tags. Neither changes an API
Magic calls, and Magic's tests pass on both.** *Observed 2026-11-04 (`git ls-remote` of each
repository; each library's release notes and `git diff` at its tags; `go list -m -u` and go-shared's
`check_libs.py` on `e2d6493b`; a scratch clone with `go-tui-lib/v2` moved).*

| Module | Pinned | Newest tag | Peeled commit, date |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.31.2 (F3e) | **v1.33.0** (v1.32.0 between) | `6e04da6e…`, 2026-11-04 |
| `go-selfupdate-lib` | v1.24.0 (F3e) | v1.24.0, current | (no change) |
| `go-tui-lib/v2` | v2.9.0 (F3d) | **v2.12.0** (v2.10.0, v2.11.0 between) | `bbfd52fa…`, 2026-11-04 |

* `go list -m -u` lists these two as the only direct sibling modules behind. Indirect modules
  are behind too, and the owner chose to leave them (D4's amendment of this date).
* **`acp-go-sdk2` v1.32.0** fixes nine defects of the SDK surface, its 0049 S1 to S9
  (`docs/reports/0049-REPORT-release-notes-v1-32-0.md` at the tag). `make api` against v1.31.2:
  "0 incompatible, 26 compatible". Most of the fixes refuse a value the SDK used to send.
  `go.mod`, `go.sum`, the schema (1.25.0) and `agentproc/` are unchanged.
  * **S5, a boolean `session/set_config_option`,** is sent only by a client that advertises
    `session.configOptions.boolean`. Magic sends one (`internal/provider/acpagent/session.go:1192`)
    and advertises it (`acpagent.go:1325`), so nothing changes.
  * **S8, an unadvertised auth method,** is refused before it is sent, with an error wrapping
    `acp.ErrUnadvertisedAuthMethod`. Magic's auth selection picks from the advertised list. Only
    the configured `auth_method_id` branch (`acpagent.go:737`) sends an id it did not check. An id
    the agent never advertised failed there before, with the agent's error, and now fails with the
    SDK's. Either way, the session start fails and names the id.
    * *Corrected 2026-11-06.* "Magic's auth selection picks from the advertised list" was wrong
      for one branch, and so was "Only the configured `auth_method_id` branch".
    * By 0085's D2 step 4, when a key was present and neither preferred method was advertised,
      the selection sent the key method `xai.api_key` "even when that id was not advertised". S8
      refuses that before sending.
    * So from `a115e40e`, a cold grok host whose only credential is `XAI_API_KEY` could not start
      a session. Proven in
      [0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md](0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md)
      F4 and F5.
    * That MADR's D2 removed the branch, in `a499b13f`. F3g's statement is unaffected: v1.33.0
      already refused.
  * **S6, closed enumerations,** are checked on send. Magic's production code sends none of them.
    Its test agents return an empty `PromptResponse` only together with an error, which is
    answered as that error (`lifecycle_test.go:170`, `prewarm_rearm_test.go:97,136`,
    `queue_test.go:62`), and its tests pass at v1.33.0.
  * **S6 on receipt:** a `tool_call`'s unknown `kind` or `status` now reads as empty, where the
    unknown string was kept before. **S7:** `AuthMethod`'s `"type":"agent"` decodes into `Agent`,
    where it was kept in `Unknown`. Magic reads neither field's raw unknown value.
  * S1, S2, S3, S4 and S9 concern an agent's handlers, `ToolCallUpdate` construction, the unstable
    methods, the timelines and unlisted prompt-block types. Magic is a client, builds no
    `ToolCallUpdate`, calls no unstable method, uses neither timeline, and sends only listed block
    types.
* **`acp-go-sdk2` v1.33.0** fixes nine subpackage defects, its 0049 P1 to P9
  (`docs/reports/0049-REPORT-release-notes-v1-33-0.md`). `make api` against v1.32.0: "0
  incompatible, 2 compatible", both in `remote`, which Magic does not import. The root package,
  `go.mod`, `go.sum` and the schema are unchanged.
  * **P7 reaches Magic.** `agentproc.LineTap` now guards its held line with a mutex, so a `Close`
    from another goroutine no longer races a `Write`. Magic's wire capture builds its tap with
    `LineTap` (`internal/wirecap/wirecap.go:271`).
  * The other eight concern `remote`, `clienthost` and `Transcript`, which Magic does not use.
* **`go-tui-lib` v2.10.0 to v2.12.0** add the streaming engine (`stream`, `frame`,
  `inputfilter`, `streamtea`) and `Printer.Printed` reprinting. Magic imports `safetext`, `termcap`
  and `termsvc`. Those three, the packages they import from the library (`glyph`, `msgcat`,
  `internal/enum`, `internal/sanitize`), and the library's `go.mod` and `go.sum` are unchanged:
  `git diff --stat v2.9.0 v2.12.0` over them is empty. So the move changes no code Magic builds.
* **The checks before the move**, none of which wrote the tree:
  * `check_libs.py` on `e2d6493b`, `acp-go-sdk2` at v1.33.0: "0 failing checks". Get and tidy,
    version, modules, build, vet on three GOOS, test, api, govulncheck and advisories were each ok.
  * `check_libs.py` cannot check `go-tui-lib`, because it refuses a `/v2` module path. A scratch
    clone with `go-tui-lib/v2` moved to v2.12.0: get, tidy and build ok, `go vet` of
    `internal/auth` and `internal/cli` on three GOOS ok, and their tests ok.
* **gobble** pins `acp-go-sdk2 v1.31.2`. Its 0035 PLAN's Deviation 23 (proposed, not executed)
  takes v1.32.0. Its `check_libs.py` at v1.33.0 fails only two tests, `TestErrors` and
  `TestAuthenticate`, on S8's refusal, which that deviation already plans to fix. gobble's move is
  gobble-cli's own record and commit. Until it lands, Magic is ahead. As in F3d, 0184 D12's shared
  pin is needed only before `live-gobble`.

**F3g. On 2026-11-05, `acp-go-sdk2` has two newer tags, v1.33.1 and v1.34.0. The first fixes a
race at the notification queue's limit that Magic's `OverflowBlock` connections can meet. The
second adds two exported helpers and changes `authenticate`'s refusal text. Neither changes the
outcome of any `authenticate` call Magic makes.** *Observed 2026-11-05 (`git ls-remote --tags` of
each sibling repository, twice, because v1.34.0 was tagged between the two reads; `go list -m -u` on
`1c27206b`; `docs/reports/0049-REPORT-release-notes-v1-33-1.md` and
`docs/decisions/0050-MADR-auth-method-ids-and-the-prompt-result-for-issue-14.md` at the tags;
`diff -r` of the module trees in the module cache).*

| Module | Pinned | Newest tag | Peeled commit, tag time (UTC) |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.33.0 (F3f) | **v1.34.0** (v1.33.1 between) | `3013065d…`, 2026-11-06 02:09 |
| `go-selfupdate-lib` | v1.24.0 (F3e) | v1.24.0, current | (no change) |
| `go-tui-lib/v2` | v2.12.0 (F3f) | v2.12.0, current | (no change) |

Magic requires no other sibling module, directly or indirectly (`go.mod`, `go.sum`).

* **`go.mod` is the same at both tags.** Each tag's `/go.mod` hash equals v1.33.0's, so the
  library's requirements are unchanged, and so is the schema (1.25.0).
* **v1.33.1, no API change.** Its own `make api` against v1.33.0: "0 incompatible, 0 compatible".
* **v1.33.1's fix, its R11:** a notification is now admitted by a count of those received and not
  completed, up to `n + 1`, or `2n + 1` on `OverflowBlock`'s read-on path. Before, it was admitted
  by whether a send to the queue's channel succeeded. That made the limit one notification lower
  whenever the notification loop had not yet taken the head of the queue.
  * **It reaches Magic.** `clientConnOptions` names `OverflowBlock` and takes the default queue of
    1024 (`internal/provider/acpagent/acpagent.go:1269-1275`). Under that policy, a burst that
    reached `2n` while the client's `Initialize` was in flight could close the connection one
    notification early, with "connection ended before response". At 1024 that is rare, but the
    outcome depended on goroutine scheduling, and now it does not.
  * **Nothing is refused earlier** than before. The channel gains one slot.
  * The stalled-pump tests flood `sdkNotificationQueueDepth + 200` frames
    (`stalledpump_test.go:152,273,347`), so one slot more does not change what they assert. Magic
    matches neither error the fix changes (`git grep` for `ErrNotificationQueueOverflow` and the
    error's text: no match).
* **v1.33.1's other Go changes:**
  * `agentproc` gains an unexported test seam, `attachTree`, for `proctree.Attach`. `Start`
    behaves as before;
  * `types_gen.go` loses an unexported method that nothing called, `ConfigOptionUpdate.gates`;
  * the rest are the library's tests, tooling and records.
* **v1.34.0, by its 0050 MADR** (issue #14 there). It is a minor release: its D9 plans
  `make api` against v1.33.1 as "0 incompatible, 2 compatible". The tree has no release-notes
  report for it. Its Go changes are `helpers.go`, `types_gen.go` and `doc.go`, besides tests:
  * **two new exported helpers:** `AuthMethod.ID()`, and `AuthenticateMethodIDs(methods)`, the
    list of ids `authenticate` accepts. That list is the gate's own, generated in one function;
  * **the gate counts an `Unknown` entry only by a string `"id"`** (its D1). Before, an entry
    without one let `methodId: "unknown"` through. Now that id is refused;
  * **the refusal names the accepted ids:** `…: methodId "x": <ErrUnadvertisedAuthMethod's
    text>; authenticate accepts "a", "b"`, or `… accepts none`. `errors.Is` still holds;
  * a godoc note that `PromptResponse.StopReason` has no default, and the package doc names
    `ErrUnadvertisedAuthMethod`.
* **What v1.34.0 changes for Magic: the refusal's text only.**
  * Magic authenticates either an id `selectACPAuthMethod` picks, from its own list of `Agent`
    entries' ids (`acpagent.go:1480-1486`) or the spec's key method, or the configured
    `auth_method_id` (`acpagent.go:736-742`). An `Agent` id that no terminal entry names was
    accepted before and is accepted now. A terminal id meets `ErrTerminalAuthMethod` first, as
    before. An unadvertised id was refused before sending from v1.32.0 on (F3f, S8), and still
    is.
  * So on the configured-id branch, a session start that failed still fails, and its error
    (`acp authenticate (<id>): …`) now ends with the accepted ids. No Magic test matches that
    text (`git grep` for the sentinel and its text: no match).
  * Magic sends no `"unknown"` id, and builds no `PromptResponse` outside its test agents, which
    F3f already covers.
  * **Not taken here:** the 0050 MADR's F10 reads Magic's own list of advertised ids as a copy of
    the gate with a different rule, and offers `AuthenticateMethodIDs` in its place. Adopting it is
    a source change, which D5's rule ("Adopting its new APIs … is not a pin") leaves to a record
    of its own.
* **gobble** pins `acp-go-sdk2 v1.33.1` on disk (gobble-cli's `go.mod`, read 2026-11-05). After
  this move, Magic is one minor ahead. gobble's move is gobble-cli's own record and commit. As in
  F3d, 0184 D12's shared pin is needed only before `live-gobble`.

**F3h. On 2026-11-07, two of Magic's four sibling libraries have newer tags. Neither changes a
library requirement or an API Magic calls. One fix reaches Magic's ACP client.** *Observed
2026-11-07 (`git ls-remote --tags` of each sibling repository; `go list -m @latest` through the
proxy and `GOPROXY=direct`; each release's notes on GitHub; `go mod download -json` and `diff -r`
of the module trees in the module cache).*

| Module | Pinned | Newest tag | Peeled commit, tag time (UTC) |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.34.0 (F3g) | **v1.34.2** (v1.34.1 between) | `4c94d2ec…`, 2026-11-07 23:37 |
| `go-llmprovider-sdk` | v1.27.0 (0235) | **v1.28.0** | `e98c2483…`, 2026-11-07 22:45 |
| `go-selfupdate-lib` | v1.24.0 (F3e) | v1.24.0, current | (no change) |
| `go-tui-lib/v2` | v2.12.0 (F3f) | v2.12.0, current | (no change) |

* **`go-llmprovider-sdk` is a sibling library now.** F3g said Magic requires no other sibling
  module. From `2666dc26`, by
  [0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md)
  D5, it also requires `go-llmprovider-sdk`. So this record's rule for sibling libraries covers it
  too.
  * *Added 2026-11-07, at PLAN P3g (Deviation 23).* 0235 did not add it to `check_libs.json`, so
    `make check-libs` never checked it. P3g adds it, with `./internal/provider/grok/...` as its
    packages.
* **Neither `go.mod` changed.** For each library, the new tag's `GoModSum` equals the pinned
  tag's: `Iz5f3g3L…` for `acp-go-sdk2` and `dE+W7oDy…` for `go-llmprovider-sdk`. So no
  transitive requirement moves.
* **`acp-go-sdk2` v1.34.1, its 0049 G14.** The schema stays 1.25.0. Its notes say `make api`
  reports no change against v1.34.0.
  * **The Go change** is in `types_gen.go`, the one non-test, non-tooling file that differs from
    v1.34.0 besides `README.md`. `skipInvalid` now skips an array item that is the literal `null`.
    Before, `encoding/json` decoded that item to its zero value. Validation, which runs on every
    received message, then failed, and the whole message was refused.
  * **It reaches Magic.** Magic's client connections set `WithInboundValidation`, receive
    `session/update`, read `initResp.AuthMethods` (`internal/provider/acpagent/acpagent.go:1515-1520`),
    and list sessions with `acp.AllSessions` (`sessioncaps.go:74`). Before this fix:
    * an agent's `session/update` with a `null` in a tool call's `content` or `locations` was
      refused, and Magic's handler never saw the update;
    * an `initialize` answer with a `null` in `authMethods` failed `Initialize`, so the session
      did not start;
    * a `session/list` answer with a `null` in `additionalDirectories` failed the listing.

    Now each `null` item is dropped, and the rest of the message is accepted. No engine Magic runs
    is known to send such an item, so this removes a failure Magic has not been seen to meet.
  * **Sending is unchanged.** Magic's tests of a `null` patch field (`patchfields_test.go`) send
    a whole field as `null`, not a `null` item, and `skipInvalid` still reads a `null` array as no
    items.
* **`acp-go-sdk2` v1.34.2, the generator's fixes** (G1 to G13, G15 to G37). Its notes say the
  exported API, the generated files, the schema, the `go` lines and the requirements are
  v1.34.1's. `diff -r` agrees: from v1.34.0 to v1.34.2, the only changed `.go` file outside tests
  and `cmd/` is `types_gen.go`, with G14's change. Magic does not build `cmd/generate`.
* **`go-llmprovider-sdk` v1.28.0, its 0071 MADR.**
  * **Added:** `catalog.Cache.Close`, which stops and waits for a cache's background fetches and
    then refuses calls given the cache; and `auth.OAuthSession.Wait`.
  * **Fixed:** two background listing starts now count their fetch under the lock. Concurrent
    `List` calls of one key no longer send a second listing request (its 0053 D4).
  * **What changes:** `catalog/cache.go`, `discovery.go`, `listing_cache.go`, `lookup.go`,
    `model_metadata.go`, `auth/oauth_session.go`, `internal/transport/probe.go`, and a comment in
    `command_token.go`.
  * **None of it reaches Magic.** Magic's only use of the SDK is `catalog.Static`,
    `catalog.SortByRank`, `catalog.LabelName` and `llmprovider.ProviderGrok`, in
    `internal/provider/grok`. `TestSDKIsImportedOnlyByGrok` holds it to those
    (`internal/provider/grok/sdk_boundary_test.go`). The first three live in
    `catalog/models_catalog.go` and `catalog/version_reading.go`, which are byte-for-byte the same
    at both tags. Magic builds no `Cache`, calls no `List`, and uses no `OAuthSession`. The changed
    files still compile into Magic, because they are in the `catalog` package. At package level
    they add only three variables: `errCacheClosed`, an error value, and two test hooks,
    `listingStartHook` and `listingClaimHook`, which are nil until a test sets them. They add no
    `init`.
  * **Not taken here:** `Cache.Close` and `OAuthSession.Wait`. Magic has no cache to close
    (D5's rule: "Adopting its new APIs … is not a pin").
* **gobble** pins `acp-go-sdk2 v1.34.0` and `go-llmprovider-sdk v1.27.0` (gobble-cli's `go.mod`,
  read 2026-11-07). After this move, Magic is one patch ahead on the first and one minor ahead on
  the second. gobble's move is gobble-cli's own record and commit. As in F3d, 0184 D12's shared
  pin is needed only before `live-gobble`.

**F3i. On 2026-11-10, three of Magic's five sibling modules have newer tags. None changes a
library requirement. Two of them change a behaviour that can reach Magic, and none breaks anything
Magic asserts.** *Observed 2026-11-10 (`git ls-remote --tags` of each sibling repository; `go list
-m -json @latest` with `GOPROXY=direct`; `go mod download -json` and a file-by-file comparison of
the module trees in the module cache; each library's `CHANGELOG.md` or release notes at the new
tag; `gh api …/compare` for `go-selfupdate-lib`'s `.github/`).*

| Module | Pinned | Newest tag | Peeled commit |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.34.2 (F3h) | **v1.35.0** | `644cfe4e…` (tag object `8e991669…`), tagged 2026-11-10 |
| `go-llmprovider-sdk` | v1.28.0 (F3h) | **v1.29.0** | `4e338da5…`, dated 2026-11-10 in its changelog |
| `go-selfupdate-lib` | v1.24.0 (F3e) | **v1.26.0** (v1.25.0 between) | `7c677842…`, dated 2026-11-09 in its changelog |
| `go-tui-lib/v2` | v2.12.0 (F3f) | v2.12.0, current | (no change) |
| `gobble-cli/acpext` | v0.3.0 (0243) | v0.3.0, current | (no change) |

* **Five sibling modules now.** F3h counted four. `gobble-cli/acpext` came with
  [0243-PLAN-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md](0243-PLAN-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md)'s
  P4, at v0.3.0, which is its newest tag. No sibling repository has a newer major version: the
  highest tag per major is `acp-go-sdk2` v1.34.2, `go-llmprovider-sdk` v1.29.0, `go-selfupdate-lib`
  v1.26.0, `go-tui-lib` v2.12.0 (no v3), and `gobble-cli` `acpext/v0.3.0`.
* **The owner's word on `acp-go-sdk2`:** "one sdk, acp-go-sdk2 is currently building and will be
  finished with CI, tagged v1.35.0 in a few minutes when i get it tagged." The tag appeared
  10 minutes 38 seconds into the wait (`wait_tag.py` in the session scratchpad).
* **No library's `go.mod` changed.** For each, the new tag's `GoModSum` equals the pinned tag's:
  `Iz5f3g3L…` for `acp-go-sdk2`, `dE+W7oDy…` for `go-llmprovider-sdk` and `DIqb0hzm…` for
  `go-selfupdate-lib`. So no transitive requirement moves.
* **`acp-go-sdk2` v1.35.0, its 0052 MADR** (`docs/reports/0052-REPORT-release-notes-v1-35-0.md` at
  the tag in that repository). The schema stays 1.25.0. `go.mod`, `go.sum`, `.github/`,
  `unstable/`, `agentproc/` and `acptest/` are unchanged. Its `make api` against v1.34.2: "0
  incompatible, 7 compatible", all in the root package.
  * **Added:** `ToolCallStream`, which lets an **agent** send a running tool call's whole state at
    most every 250 ms, as replace-whole `tool_call_update`s. Also `StartToolCallStream`, its two
    options, and two errors.
  * **Changed, D3:** on the **agent** side, a prompt turn's unfinished streams are written before
    its `session/prompt` answer. "An agent that starts no stream sees no change."
  * **Changed, D6:** `WithMaxMessageSize`'s default goes from 10 MiB to 32 MiB. "An explicit
    option is unchanged."
  * **Moved:** `clienthost`'s terminal output buffer, to `internal/tailbuf`. `clienthost`'s API and
    behaviour are unchanged.
* **What `acp-go-sdk2` v1.35.0 changes for Magic: nothing it builds or asserts.**
  * **D6 does not reach Magic's connections.** `clientConnOptions` sets
    `acp.WithMaxMessageSize(maxACPMessageSize)` (`internal/provider/acpagent/acpagent.go:1346`),
    and `maxACPMessageSize` is `10 << 20` (`:1357`). So an agent's line over 10 MiB is still
    refused, as before. Only Magic's test agents' agent-side connections take the new default. They
    send nothing near 10 MiB.
  * **D3 and `ToolCallStream` are agent-side.** Magic is a client, its test agents start no
    stream, and it imports no `clienthost` (`git grep` for `clienthost`, `StartToolCall` and
    `ToolCallTimeline`: no match).
  * **What could reach Magic later:** an agent that adopts `ToolCallStream`, such as gobble once
    its tools get an output hook (the 0052 MADR's F15, F19). Magic would then receive many
    replace-whole `tool_call_update`s for one call. How Magic and the phone handle them is a
    question for the record that registers that agent, not for this pin.
  * **A fact in that MADR about Magic is wrong.** Its decision drivers say Magic's connections use
    `OverflowDropNewest`. They use `OverflowBlock` (F3e, and `clientConnOptions`). It doesn't
    change that MADR's decision, which holds for a client that drops and for one that blocks.
    Correcting it is the SDK repository's job.
* **`go-llmprovider-sdk` v1.29.0, its 0072 MADR.**
  * **Added:** `APIError.ResetAt`, the instant a limit lifts; `APIError.Limit`, of the new type
    `Limit`, which holds what the service said about the limit; and `ClassifyStreamFailureBody`.
  * **Fixed:** an OpenCode `BudgetLimitError` and `CreditLimitExceeded` are
    `ErrQuotaExhausted` and not retried. A Gemini per-day quota reported inside a stream is
    `ErrQuotaExhausted`. A failure inside a stream carries the delay its error states.
  * **What changes:** in the root package, `api_error.go` and the new `api_limit.go`. Also
    `internal/transport`, the four `internal/wire` readers, and `providers/gemini/interactions.go`.
    No file under `catalog/` changes.
  * **What reaches Magic: two package-level regular expressions, and nothing else.** Magic uses
    `catalog.Static`, `catalog.SortByRank`, `catalog.LabelName` and `llmprovider.ProviderGrok`,
    in `internal/provider/grok` only (`TestSDKIsImportedOnlyByGrok`). The first three are
    unchanged. `ProviderGrok` is in the root package, so `api_error.go` and `api_limit.go` compile
    into Magic. At package level they add `tryAgainIn` and `hoursName`, two `regexp.MustCompile`
    values that v1.28.0 did not have. They are compiled once, at program start, and add no
    `init`. Magic builds no `APIError` and reads none, so `ResetAt`, `Limit` and the
    reclassifications do not reach it.
  * **Not taken here:** `APIError.ResetAt` and `Limit`. They are the SDK's own counterpart to
    `agenterr.Limit` (0243). Magic has no HTTP-API provider that returns an `APIError` (D5's
    rule: "Adopting its new APIs … is not a pin").
    * *Added 2026-11-10.* Using them is the subject of issue #38 in this repository. The detail
      reaches Magic through gobble, which maps it into `acpext`'s `ErrorData` (gobble-cli#15,
      open). It is 0243's topic, the usage-limit notices, and not this record's. The issue's
      text-path entries (`creditlimitexceeded`, `budgetlimiterror`) do not depend on the pin,
      and its `windowMinutes` item waits on a newer `acpext` tag.
* **`go-selfupdate-lib` v1.25.0, its 0041 MADR (issue #9 there).**
  * **Fixed:** a program started through a symlink to its binary is refused on Linux, as it
    already was on macOS and Windows. On Linux `os.Executable` returns the resolved path, so the
    library now checks the path in `os.Args[0]` too. "A Linux program that updated through a link
    before is now refused."
  * **Added:** `ErrSymlinkInvocation` and the `failed` event class `symlink-invocation`. The
    refusal's message names the link and its target.
  * It also made `CleanupPending` and `KeptBackups` fail for a program started through a link.
    v1.26.0 fixes that.
* **`go-selfupdate-lib` v1.26.0, its 0041 MADR's amendment A1 and its 0042 MADR.**
  * **Added:** `SymlinkInvocationError` and `SymlinkSource`. Four sentinels:
    `ErrSpecialModeBits`, `ErrNoAllowedRoot`, `service.ErrBinaryMismatch` and
    `taskscheduler.ErrLogonType`. Two `failed` event classes, `special-mode-bits` and
    `no-allowed-root`.
  * **Changed:** the symlink refusal's text, and ten other run-time messages, which no longer name
    a Go identifier. Nine of them now wrap a sentinel.
  * **Fixed:** `CleanupPending` and `KeptBackups` no longer fail for a program started through a
    link (issue #10).
  * `.github/` is unchanged from v1.24.0: 10 commits ahead, 0 behind, 49 files, none under
    `.github/`. So the two reusable workflows `ci.yml` calls are v1.24.0's.
* **What `go-selfupdate-lib` changes for Magic: the Linux symlink refusal.**
  * **It reaches Magic.** In production, `updateclient.New` sets no `TargetPolicy`, because
    `Options.TargetPath` is set only in tests (`internal/updateclient/client.go:64-66`, `:121-126`).
    So the library finds the target itself, and since v1.25.0 it also checks `os.Args[0]`
    (`selfupdate/target.go:45-48` at v1.26.0). On Linux, `mcremote update` or `mcrelay update`
    started through a symlink to the binary is now refused, where it updated before.
  * **No Magic install creates such a link.** `make install` and `scripts/install-binary.sh`
    write a regular file (`install -m 755`, then `mv -f`). The systemd unit's `ExecStart` is the
    installed binary at `~/.local/bin`, or `os.Executable` with symlinks resolved
    (`internal/cli/service/setup.go:1030-1045`). So the refusal reaches only a user who made a
    link themselves. macOS and Windows already refused it, so the three platforms now agree.
  * **Nothing Magic reads changes meaning.** Magic matches no library message text and no
    `failed` event class (`git grep` for the changed texts, the new sentinels and the classes:
    no match outside Magic's own fake errors). Its reporter is `selfupdate.DiscardReporter()`
    (`client.go:153`). It calls neither `CleanupPending` nor `KeptBackups`.
  * **`ci.yml`'s two `uses:` pins** (`:756`, `:777`) name v1.24.0's commit `74ab5dd3…`, and
    `TestWorkflowPinsMatchGoMod` (`internal/updateclient/spec_test.go`) ties them to `go.mod`.
    They move to `7c677842dface4b14b65b2e8b46aa0a7543bed13` with it.
  * **Not taken here:** the new sentinels and `SymlinkInvocationError`. Magic's update command
    could name the remedy for a symlink refusal from them. That is a source change, and D5's rule
    leaves it to a record of its own.
* **gobble** pins `acp-go-sdk2 v1.34.2`, `go-llmprovider-sdk v1.28.0`, `go-selfupdate-lib v1.25.0`,
  `go-tui-lib/v2 v2.12.0` and `acpext v0.2.0` (gobble-cli's `go.mod` on GitHub, read 2026-11-10).
  gobble's move is gobble-cli's own record and commit. As in F3d, 0184 D12's shared pin is needed
  only before `live-gobble`.
  * The owner, 2026-11-10: "i am also pinning gobble to latest now in its own repo". Magic
    requires no gobble-cli module but `acpext`, so gobble's move changes no Magic pin unless it
    comes with a newer `acpext` tag. The PLAN re-reads `acpext`'s tags before it moves anything.

**F3j. After P3h's commit, `go-selfupdate-lib` has two newer tags.** They were found by `make
check-libs` on the owner's commit `912b9aa1` (PLAN, P3h's execution record). Both were tagged on
2026-11-10, after P3h step 1:

| Tag | Peeled commit | What changed (its CHANGELOG) |
| --- | --- | --- |
| v1.26.1 | `9fc9dd7265c305caf995083663dd0df5b2a0ad39` | "An error names the package once." A wrapped sentinel no longer repeats `selfupdate:`, so a refused confirmation now ends "…: confirmation required". "A sentinel printed alone keeps its prefix, and `errors.Is` and `errors.As` match as before." |
| v1.26.2 | `e62d77cddaf264cc6ec7bbc4f39b90322542f752` | "A managed install's failure names its phrase once." `errors.Is` still matches `ErrManagedInstall` and each cause. |

* **Requirements and workflows are unchanged.** v1.26.2's `GoModSum` is `DIqb0hzm…`, the same as
  v1.26.0's (`go mod download -json`). Its `.github/` tree is identical to v1.26.0's (`diff -rq`
  reports nothing). check-libs' api check reports "ok", so the change is compatible.
* **What reaches Magic.** check-libs' test run at v1.26.2 failed two tests; every other package
  passed.
  * `TestWorkflowPinsMatchGoMod` fails because `ci.yml`'s two pins still say v1.26.0. They move
    with `go.mod`, as they did in P3h.
  * `TestUpdateHandOffNeedsYes` (`internal/updateclient/command_test.go:357`) fails at `:364`. It
    looks in stderr for `selfupdate.ErrConfirmationRequired.Error()`, "selfupdate: confirmation
    required". v1.26.2 prints "selfupdate: service: an update from inside the service runs
    detached and cannot ask; pass --yes: confirmation required"
    (`selfupdate/service/handoff.go:199`). The refusal and its exit status 1 are unchanged; only
    the text the test matches is.
  * No other Magic code matches text the library produces. A `git grep` for `"selfupdate: ` in
    `internal` and `cmd` finds only that assertion and fakes that tests build as inputs
    (`launchd_sequence_test.go:146`, `:192`; `settle_test.go:93`; `result_print_test.go:140`;
    `managed_test.go:521`).
* **Moving to v1.26.2 therefore changes one test assertion.** That is not D5's "adopting its new
  APIs": the test keeps asserting the same refusal, by text the library now prints. Because the
  move changes a test, the PLAN proposes it as its own phase (P3i) for the owner's approval.

**F3k. On 2026-11-11, `acp-go-sdk2` has a newer tag, v1.36.0. It moves the ACP schema and adds one
gate on the wire, which Magic never reaches. This move is tied to gobble's.** *Observed 2026-11-11,
read-only: a local clone of `acp-go-sdk2` (`git rev-list`, `git diff` and `git show` between the
tags, and its `docs/reports/0054-REPORT-release-notes-v1-36-0.md` at the tag); this repository at
`14200b6e` (`git grep`); a local clone of gobble-cli (`git show <ref>:go.mod`). Nothing was built
and no module was downloaded for this finding. Two items below come from an audit the
cross-repository coordinator of the auth-contract work ran the same day. They were not re-run
here, and each is marked "relayed".*

| Module | Pinned | Newest tag | Peeled commit |
| :--- | :--- | :--- | :--- |
| `acp-go-sdk2` | v1.35.0 (F3i) | **v1.36.0** | `3be7af61…` (tag object `0e68b2b9…`), dated 2026-11-11 |

The other four sibling modules were not re-read for this finding. P3j's P0 re-reads them.

* **The owner's ruling, 2026-11-11,** relayed by the coordinator; none of the owner's wording was
  relayed, so none is quoted. Magic moves from v1.35.0 to v1.36.0, together with gobble-cli, before
  gobble's `v0.6.0`. gobble-cli's `main` moves under gobble-cli's own record 0054, which was being
  drafted that day and was not in its tree when this was written, so it is named by number.
  `v0.6.0` is the gobble release that must carry the same tag
  (`0179-MADR-gobble-native-acp-provider.md` A10).
* **This is not F3d's case.** F3d to F3i each say that 0184 D12's shared pin "is needed only
  before `live-gobble`", and Magic moved ahead of gobble on that footing. Since 2026-11-11 the shared pin
  is a recorded gate that is knowingly unmet: Magic has pinned v1.35.0 since `912b9aa1`, gobble's
  releases `v0.4.0` and `v0.5.0` pin v1.34.2, and only gobble-cli's `main` (`17d1605`) pins
  v1.35.0. So this move does not go ahead alone. It is one half of a pair.
* **Fifteen commits** (`git rev-list --count v1.35.0..v1.36.0`). Two of that repository's records
  carry the change: its 0054 MADR (schema 1.31.0 and client system prompts) and its 0055 MADR (the
  Go pin).
* **Its `go.mod` changed, and its requirements did not.** `go 1.26.10` becomes `go 1.26.11`, and
  `toolchain go1.27.3` becomes `toolchain go1.27.4`. Its `go.sum` is identical at both tags.
  * Magic's `go.mod` says `go 1.27.4` with no `toolchain` line, which is above the new floor. So
    Magic's `go` line should not move and no `toolchain` line should appear *(inferred from the
    two files; P3j step 2 checks it)*.
  * Unlike F3i and F3j, the new tag's `/go.mod` hash differs from the pinned tag's, because the
    file changed. So `go.sum`'s `/go.mod` line for the module changes as well as its `h1:` line.
* **The API.** Its release notes: `make api` against v1.35.0, "0 incompatible, 57 compatible", 11
  in the root package, all for the client system prompt, and 46 in package `unstable`, which is
  "reported and not gated".
  * Of the root package's files that are not tests, only the generated `types_gen.go`,
    `agent_gen.go` and `client_gen.go` changed (`git diff --name-only`).
  * `connection.go` and `middleware.go` are byte-identical at the two tags (`git diff --quiet`,
    exit 0). So
    `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`'s cites of
    v1.35.0 hold at v1.36.0: `acp.RequestIDFromContext` (`middleware.go:133-155`, `:192`) in its
    D8, and `connection.go:23`, `:2668-2694` and `:2494-2518` in its table's row T7.
* **The schema pin moves from 1.25.0 to 1.31.0.** From its release notes: the names package `acp`
  took from the unstable schema at 1.25.0 "keep that shape"; the new keyword
  `x-deserialize-default-on-null` marks 14 stable arrays and objects, the decoders "already read
  `null` that way at all 14, so nothing changes on the wire"; 134 doc comments change "wording and
  links, and nothing else". Package `unstable` gains 46 generated names that package `acp` does
  not dispatch.
* **One change on the wire: the system prompt's gates.** Its release notes: "Only a `session/new`,
  `session/load` or `session/resume` that carries `systemPrompt`. `v1.35.0`'s types had no such
  field. An agent built on this release that does not advertise the capability answers such a
  request `-32602`. A request without the field is handled as before." On the client side, "A
  typed method returns the error and writes nothing": a client cannot send a prompt the agent did
  not advertise.
* **Magic never sends `systemPrompt`.** As an ACP client it builds a session request in three
  places, and none sets the field:
  * `session/new`, in `sessionOpenNew`: `Cwd`, `McpServers` and `Meta`
    (`internal/provider/acpagent/session_meta.go:67-73`);
  * `session/load`: `Cwd`, `McpServers`, `SessionId` and `Meta`
    (`internal/provider/acpagent/acpagent.go:1148-1153`);
  * grok's raw `session/resume`, a read-back after `session/set_model`: `sessionId` and `cwd`
    (`internal/provider/grok/fallback.go:58-59`, its body at `:72-74`).

  At `14200b6e`, `git grep -i systemprompt` over tracked and untracked files finds the word in
  three earlier decision records (0038, 0081 and 0106) and in no Go, Dart, YAML or script file.
  So no request Magic sends changes, and the gate is not reached with any agent.
* **Magic's test agents.** `acptest`'s agent is scripted and is not built on the SDK's agent side.
  `test/supervisors/probeagent` is. At v1.36.0 it would answer a `systemPrompt` request `-32602`,
  and nothing sends it one.
* **What could reach Magic later.** Sending a system prompt is a source change, and D5's rule
  leaves it to a record of its own ("Adopting its new APIs … is not a pin"). That record has to
  read the agent's `sessionCapabilities.systemPrompt` first, or every agent that does not
  advertise it fails to start. P3j adds a test that holds "Magic sends none", so the change
  cannot arrive unnoticed.
  *(2026-11-11, later the same day: the owner ruled that gobble, at `v0.6.0`, will advertise
  `sessionCapabilities.systemPrompt` and honour a client's system prompt (gobble-cli
  `0054-MADR-go-1-27-4-and-sibling-pin-refresh.md`, as the coordinator relayed it; that record was
  being edited the same day, so what it finally says is gobble-cli's). Three things follow here.
  First, gobble's `initialize` answer changes at `v0.6.0`: at `0.4.0` its `sessionCapabilities` are
  `additionalDirectories`, `close`, `list` and `resume` (the first frame of
  `internal/provider/gobble/testdata/wire/0.4.0/frames.jsonl`). That change is covered by the fixture
  recapture of `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`,
  at its gate G2 and its run L4, not here, and P3j's step 4 still finds no fixture changed, since
  P3j moves no gobble release. Second, the daemon still sends no `systemPrompt`, so the advertised
  capability changes no request Magic makes, and P3j's two tests that pin that stay as written (they
  run against the `acptest` agent, whose capabilities each test sets). Third, sending a client system prompt from the phone is not in scope of this record or of 0246;
  it stays a source change for a record of its own, which reads the agent's capability first, as
  above.)*
* **Deprecations.** `Deprecated:` occurs 278 times in that repository's non-test Go files at
  v1.34.2, at v1.35.0 and at v1.36.0 (`git grep -c`, summed). That is a count, and not a
  comparison of the sets. So `make staticcheck`'s "56 ignored finding(s), each an acp-go-sdk2
  unstable-schema name" should not grow *(inferred)*, and P3j compares the names, not the number.
* **Relayed, and not re-run here:**
  * a scratch `git archive` of `bcde74ab`, with a `replace` onto the v1.36.0 tree, built, vetted
    and passed its tests in `internal/provider/acpagent`, `acpagent/acptest`, `acpcommon`,
    `gobble`, `wirecap` and `relay`, as the coordinator named them;
  * a probe of request-scoped elicitation inside `authenticate` passed on both tags. 0246's D8
    rests on that behaviour.
* **gobble-cli was built against this tree by the SDK's own check.** Its release notes, "The
  consumers": gobble-cli at `d6c10a5`, unpacked into a scratch directory with a `replace` onto the
  v1.36.0 tree, "builds, vets and passes its tests with no other change". That bears on gobble's
  own move, which is gobble-cli's record and commit.
* **Alternatives to moving now.** The ruling, as relayed, decides the move and names no option it
  declined, so none is recorded as declined. Two exist:
  * stay on v1.35.0 until after the auth cut-over. gobble's `v0.6.0` would then be cut on v1.35.0,
    or 0246's gate G2 would stop on a mismatch;
  * move in the same commit as 0246's `authcontract` pin. That plan's P1 step 1 calls any other
    requirement that moves a deviation, and gobble's release would wait for Magic's cut-over to
    start.

**F4. CI actions: four of eight are behind.** Each is pinned by commit, with a version comment.

| Action | Pinned | Newest release, peeled commit |
| :--- | :--- | :--- |
| `actions/checkout` | v7.0.1 `3d3c42e5` | v7.0.1, current |
| `actions/setup-go` | v7.0.0 `b7ad1dad` | v7.0.0, current |
| `actions/setup-node` | v7.0.0 `82076278` | **v7.1.0** `949feb24` (2026-10-08) |
| `actions/setup-java` | v6.0.0 `dd06d9cb` | **v6.1.0** `b843ebf9` (2026-10-14) |
| `actions/upload-artifact` | v7.0.1 `043fb46d` | **v7.0.2** `cf430e03` (2026-10-07) |
| `actions/download-artifact` | `3e5f45b2`, commented `# v8`; that commit is v8.0.1 | **v8.0.2** `9000827c` (2026-10-07) |
| `subosito/flutter-action` | `1a449444`, commented `# v2` | v2.23.0, the same commit |
| `nick-fields/retry` | v4.0.0 `ad984534` | v4.0.0, current |

**F5. CI tool pins.**

* **`govulncheck` is behind:** `golang.org/x/vuln` v1.7.0, at `ci.yml:127` and
  `scripts/vulncheck.sh:31`. The newest is v1.9.0.
* **`staticcheck` is current:** `STATICCHECK_VERSION ?= v0.8.2` (`Makefile:601`).
* **`golint` floats, with nothing to float to:** `go-precheck.sh:46` installs `@latest`, and the
  module's only version is the pseudo-version of 2024-11-12.
* **`markdownlint-cli2` floats.** `make markdownlint-docs` runs `npx markdownlint-cli2`
  (`Makefile:315`) with no version, so each CI run takes npm's newest: 0.24.0 today. 0169 D15
  installed 0.23.2 on hosts. CI has never had a pin.

**F6. Node is behind 0169's own rule.**

* CI's `NODE_VERSION` is `"24"` (`ci.yml:29`, `docs.yml:22`), and `.node-version` and `.nvmrc` say
  `24`.
* 0169 D4: "From **2026-10-28**, when Node 26 becomes Active LTS, the standard moves to the newest
  26.x". nodejs.org lists `v26.13.0 (Lithium)` as the newest LTS. Node 24's newest is v24.22.2.
* The audit flags `24.21.0` with a later security release in its line, `v24.22.1`. CI's `"24"`
  resolves to the newest 24.x at run time, so CI itself is not on 24.21.0.

**F7. The Flutter SDK has a high-severity advisory, and the shipped Android app is affected.**

* CI's `FLUTTER_VERSION` is `"3.47.5"` (`ci.yml:33`), with Dart 3.13.4. The newest stable is
  **3.47.9**, with Dart 3.13.7, released 2026-10-29.
* `dart-lang/sdk` advisory **GHSA-frr6-h895-5pxp** (high, published 2026-10-23): eight eMudhra
  intermediate CA certificates were disclosed as compromised. `dart:io` on Android and Windows
  validates chains with its compiled-in BoringSSL and does not read the OS blocklist. So apps
  built with Flutter before **3.47.8**, or Dart before 3.13.7, accept certificates forged under
  those CAs. Fixed in Flutter 3.47.8 and Dart 3.13.7. iOS and macOS are not affected.
* **Where the phone is exposed.** Read from the code:
  * **Not exposed:** a self-signed daemon. `SecurityContext(withTrustedRoots: false)`, and only a
    fingerprint match is accepted (`apps/mobile/lib/data/ws/mcremote_client.dart:138-147`, `:290`).
  * **Exposed:** a Let's Encrypt-mode daemon with no fingerprint, which "keeps doing plain chain
    validation" (`mcremote_client.dart:290`, `:968`).
  * **Exposed:** the relay's outer connection, a default `HttpClient()` (`relay_transport.dart:92`).
  * **Exposed:** the in-app update check, a default `http.Client()` to `api.github.com`
    (`apps/mobile/lib/data/update/app_update.dart:13-16`).
* 0169 D3 moves Flutter "in lockstep with magic-git's `build_macos.sh` pin and goldens". That is a
  second repository.

**F8. Dart packages: 15 direct dependencies are behind, two by a major.**

* `flutter pub outdated --json` at the 3.47.5 SDK reports no advisory, retraction or
  discontinuation.
* 13 of the 15 move within their constraints. Examples: `go_router` 18.0.0 → 18.1.2,
  `flutter_local_notifications` 22.3.0 → 22.3.3, `mobile_scanner` 7.4.0 → 7.4.2, `speech_to_text`
  7.4.0 → 7.5.0, `url_launcher` 6.3.2 → 6.3.3.
* Two are new majors:
  * `cupertino_icons` 1.0.9 → **2.0.0**;
  * `flutter_secure_storage` 10.3.1 → **11.2.0**, which holds the client identity and the pinned
    fingerprints (0066's subject).
* 65 transitive packages are behind as well.

**F8a. What `flutter_secure_storage` 11 changes, and why the app's store survives it.** From its
`CHANGELOG.md` on `develop`, read 2026-10-29:

* **11.0.0** (2026-08-06), breaking: "items deprecated in v10 have been removed. Any data saved
  using deprecated algorithms or features will be unusable after this upgrade."
  * Removed: `KeyCipherAlgorithm.RSA_ECB_PKCS1Padding`, `StorageCipherAlgorithm.AES_CBC_PKCS7Padding`,
    and `AndroidOptions`' `encryptedSharedPreferences` and `sharedPreferencesName`.
  * `minSdk` rises to 24; 11.1.0 then uses `flutter.compileSdkVersion` in place of a pinned 37.
* **11.1.0** adds `checkUpgradeStatus()`, "to report data lost on a direct major upgrade".
  **11.2.0** fixes Android biometric and namespace recovery, and scopes `deleteAll` to the key
  prefix.
* **Every value the app has stored was written with v11's ciphers:**
  * the app's first commit (`719401b6`, 2026-07-19) already locked `flutter_secure_storage`
    10.3.1;
  * that is after 10.1.0 "fixed default Android key cipher not aligning with the Flutter default",
    and after 10.2.0 made `RSA_ECB_OAEPwithSHA_256andMGF1Padding` and `AES_GCM_NoPadding` the only
    non-deprecated defaults. 10.0.0 had already made them the defaults.
* **The app uses nothing v11 removed.** Its only options are `AndroidOptions(resetOnError: true)` and
  an `IOSOptions` (`apps/mobile/lib/data/local/settings_store.dart:61-66`).
* **The remaining risk is `resetOnError`.** If v11 failed to decrypt an existing value, the plugin
  would reset the store silently (`settings_store.dart:326`), and the phone would lose its client
  identity and pinned fingerprints. The emulator check in D7 is what shows it does not.

**F8b. `cupertino_icons` 2.0.0** "updates minimum supported SDK version to Flutter 3.44/Dart 3.12"
and "migrates to use `cupertino_ui` instead of `flutter/cupertino.dart`" (its `CHANGELOG.md` in
`flutter/packages`). The app uses no `CupertinoIcons` glyph (`grep -rl "CupertinoIcons\." lib`: no
match). So the move changes no rendering. ~~It adds `cupertino_ui` as a transitive package.~~
*Corrected 2026-10-30, at PLAN P1 step 4:* `cupertino_ui` is already in the graph, at 1.0.1
(`flutter pub get` on 3.47.9: "cupertino_ui 1.0.1 (1.1.5 available)"). The move only moves its
version.

**F9. The Android build tools are bounded by Flutter, not by their publishers.**

| Tool | Pinned | Publisher's newest | Flutter 3.47's known maximum (`flutter_tools/lib/src/android/gradle_utils.dart`) | Newest inside it |
| :--- | :--- | :--- | :--- | :--- |
| Gradle (`gradle-wrapper.properties`) | 9.1.0 | 9.8.2 | `maxKnownAndSupportedGradleVersion = '9.3.1'` (`:85`) | 9.3.1 |
| Android Gradle Plugin (`settings.gradle.kts:22`) | 9.0.1 | 9.5.0 | `maxKnownAndSupportedAgpVersion = '9.2'` (`:98`); `maxKnownAgpVersionWithFullKotlinSupport = '9.1.0'` (`:104`) | 9.2.1, or 9.1.1 for full Kotlin support |
| Kotlin Gradle plugin (`settings.gradle.kts:23`) | 2.3.20 | 2.4.21 | `maxKnownAndSupportedKgpVersion = '2.4.0'` (`:91`) | 2.4.0 (2.4.10 to 2.4.21 are past it) |

Flutter's own templates use Gradle 9.3.1, AGP 9.1.0 and Kotlin 2.4.0 (`:36`, `:44`, `:52`). The
bounds were read from the 3.47.5 SDK on this Mac. A patch release seldom moves them, and the PLAN
re-reads them at 3.47.9.

**F10. Java.**

* CI runs `java-version: "25"` (`ci.yml:474`) through `setup-java`, which takes the newest 25.x.
  0169 D5 chose Temurin 25 LTS, and 0168 D2's bytecode target of 17 stands
  (`app/build.gradle.kts:49-50`, `:106`).
* The audit reports the standard's 25.0.4.1 as stale against 25.0.5, and an OpenJDK advisory of
  2026-10-24 "affects 25.0.5.0.2 and earlier". So the newest Temurin is still affected, and there
  is no fixed build to move to yet.

**F11. Every agent engine's known-good pin is behind what is installed.**

| Engine | `KnownGoodVersion` | Installed here (`--version`) | npm's newest |
| :--- | :--- | :--- | :--- |
| grok (`grok/version.go:16`) | 1.0.13 | 1.0.70 | (not on npm) |
| codex (`codex/version.go:17`) | 0.152.1 | 0.166.0 | 0.166.0 |
| kilo (`kilo/version.go:22`) | 7.5.6 | 7.8.18 | 7.8.18 |
| opencode (`opencode/version.go:29`) | 1.18.26 | **v2.2.2** | `opencode-ai` 1.19.5 |

* A pin is the version whose wire shapes the provider's tests were checked against, and it backs a
  committed fixture (0137 Phase 3). A mismatch only warns.
* Moving one means re-reading those shapes against the new version, which takes a live run of
  that provider: quota. grok 1.0.70 already has a committed capture (0207 SDK-gaps PLAN, C3).
* opencode 2.x is the subject of
  [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
  proposed.
* *Note, 2026-11-04:* opencode's pin moved under
  [0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md). `KnownGoodVersion`
  is 2.2.3 and `MinVersion` 2.1.0 (`internal/provider/opencode/version.go`): set at its P4, and
  made the only pins at P7, when the 1.x dialect and its 1.18.26 pin were retired. Its P9 ran the
  live suite on the three hosts. 0214 was accepted on 2026-11-01.

**F12. The host standard in magic-git is stale against 0169's own amendments.**

* `standard.json` still says Go 1.27.1, Node 24.21.0, Flutter 3.47.5 and Java 25.0.4.1.
* `version_audit.py` reports 8 STALE and 4 ADVISORY against it. Besides F6, F7 and F10, it flags
  Python 3.14.7 (11 advisories fixed by 3.14.10), Git for Windows, gh and glab.
* Those are host installs and a file in another repository (0169 P14). This repository pins none
  of them.
* **Its history stops at 2026-09-30.** magic-git's log for `standard.json` has two commits,
  `74c7380` (2026-09-29) and `4f400c9` (2026-09-30, gh 2.102.0). 0169's P12 step 3 (Go 1.27.2)
  and P14 step 4 (Go 1.27.3) each left the Go move "for the owner to commit there", and neither
  reached the file. The file has no floor key, and the audit reads none: 0169 D2's floor lives in
  prose only.
* **The audit, row by row, with an advisory or without** [run 2026-10-30 against the file and
  against a scratch copy with the candidates below; `leftovers/standard_candidate.py`]:

  | Row | Standard | Advisory active on it | Candidate | The candidate's audit |
  | :--- | :--- | :--- | :--- | :--- |
  | Go | 1.27.1 | **yes:** "16 advisories fixed by 1.27.3", GO-2026-6599 to GO-2026-6809 | 1.27.3 | no row |
  | Node | 24.21.0 | **yes:** "1 later security releases in its line: v24.22.1" | 26.13.0 | no row |
  | Python | 3.14.7 | **yes:** "11 advisories fixed by 3.14.10" | 3.14.10 | `STALE 3.14.10 -> 3.15.2` only (F15) |
  | Flutter | 3.47.5 | **yes:** GHSA-frr6-h895-5pxp (F7) | (magic-git's pair, D11) | — |
  | Java | 25.0.4.1 | yes, with no fixed build (F10) | none exists | — |
  | Git for Windows | 2.56.0.windows.1 | no | — | STALE only |
  | gh | 2.102.0 | no | — | STALE only |
  | glab | 1.120.0 | no | — | STALE only |

**F13. Go on the hosts is already 1.27.3.** 0169's P13 and P14 records, and its Deviation 21: this
Mac, the Linux and Windows test hosts, and the WSL distro all report `go1.27.3`, with
`GOTOOLCHAIN=go1.27.3`. This Mac, read 2026-10-30: `go version go1.27.3 darwin/arm64`. Only the
standard's row is behind.

**F14. Node on the hosts.**

* nodejs.org's index, 2026-10-30: `v26.13.0` (2026-10-28, LTS "Lithium"); `v26.12.1` (2026-10-21) is
  flagged `security: true`. 0169 D4 moved the standard to the newest 26.x from 2026-10-28.
* **This Mac resolves Homebrew's `node` 26.10.0** (`/opt/homebrew/Cellar/node/26.10.0_1`), not the
  `node@24` 24.21.0 that 0169's P6a linked, which is still installed. 26.10.0 predates 26.12.1's
  security release, so the advisory is active here. Homebrew's stable `node` is 26.13.0.
* **The other hosts** run 24.21.0, as 0169's P6a left them:
  * ~~the Linux test host from `~/sdk/node-v24.21.0`, on PATH through the dotfiles' `00-paths.sh` and
    the `mcremote` drop-in;~~ *Corrected 2026-10-30, at PLAN P5c (Deviation 12):* on the Linux
    test host, the login PATH names `~/sdk/node-v26.10.0` (created 2026-09-21), through an owner's
    uncommitted edit to the dotfiles' `00-paths.sh:27`. The `mcremote` drop-in, `path.conf:22`, still
    names `~/sdk/node-v24.21.0`, so the daemon runs 24.21.0;
  * WSL from `~/sdk/node-v24.21.0`, through `~/.config/devenv.sh`;
  * the Windows test host from the `node-v24.21.0-x64.msi` install.

  *Corrected 2026-10-30, at PLAN P5c (Deviation 12):* WSL's `devenv.sh` named
  `~/sdk/node-v26.10.0`, and the Windows test host's MSI install was v26.10.0. Each is the owner's
  earlier move.

  The PLAN re-reads each before it moves anything.
* npm global packages live under each host's own prefix (for example `markdownlint-cli2` under
  `~/.local`, 0169 D15). They are not inside the Node directory, so they survive the swap. A package
  with a native addon would need rebuilding for Node 26's ABI. That is why the PLAN lists them, and
  runs each one's `--version` before and after.

**F15. Python 3.14.10 has no advisory, and the audit fails on any held line.**

* endoflife.date: 3.14's newest is **3.14.10** (2026-10-25), supported until 2027-10-01 and secure
  until 2030-10-31. 3.15 is out, at 3.15.2.
* OSV, queried by each release tag's peeled commit (`leftovers/python_osv.py`, the audit's own
  method), counts records, aliases included:

  | Release | Tag commit | OSV records |
  | :--- | :--- | :--- |
  | 3.14.7 | `823f0323ee6e` | 19 |
  | 3.14.8 | `8e6e75d9102e` | 8 |
  | 3.14.9 | `ead82c07db1f` | 2 (PSF-2026-45, PSF-2026-46) |
  | **3.14.10** | `b703b2c5e704` | **0** |

* **This Mac's `python3` is Homebrew's `python@3.14` 3.14.8** (3.14.7 also installed). Homebrew's
  stable is 3.14.10. By 0169's records (P0, P7, Deviation 20), the Linux test host and WSL run
  python-build-standalone 3.14.7 from `~/sdk/python-3.14.7`, with `~/default-venv` built on it, and
  the Windows test host runs the `py` install manager's 3.14.7. The PLAN re-reads each.
  *Corrected 2026-10-30, at PLAN P5c:* this Mac's `python@3.14` is 3.14.10 by then. On the Linux
  test host the owner installed `~/sdk/python-3.14.10` on 2026-10-30 and rebuilt `~/default-venv` on
  it (Deviation 12); `python-3.14.7` is still there. WSL is the same, and the Windows test host's
  `py` install manager already has 3.14.10.
* **0169 D1 would take 3.15.** Its rule is "the newest stable patch of the newest line". The owner
  holds 3.14.
* **The audit cannot express a held line.** `s_python` (`version_audit.py:569-578`) compares the
  standard with the newest supported cycle of every Python line. `STALE` is in `FAIL_STATES`
  (`:46`), and `exit_code` returns 1 on any fail state (`:61-64`). So with the standard on 3.14.10,
  the audit reports `STALE standard python 3.14.10 -> 3.15.2` and exits 1, on every run, for as
  long as the hold stands.
* A feed that cannot be reached is an `ERROR` row (`:144-146`), and exits 2. So "no advisory row"
  is a real answer, and not an outage read as clean.
* This repository pins no Python. No workflow uses `setup-python`, and there is no
  `.python-version`.

## Decision Drivers

* **A published advisory against a shipped artifact is fixed first.** 0169 D10: "Exposures are
  remediated first, ahead of every other change." F7 is one.
* **"Current" means 0169 D1's rule, applied to every pin:** the newest stable release that is
  supported and has no advisory, *and* is inside what its consumer supports. Flutter bounds Gradle,
  AGP and Kotlin (F9).
* **A version move is adopted only after this repository's gates pass on it** (0169 D1). That
  includes the Linux and Windows test hosts, the APK build, and the live supervisors job for F3.
* **A behaviour change is a decision, not a bump.** F3, the two majors in F8, and the engine pins in
  F11 each change something Magic relies on.
* **Provider quota is spent only with the owner's word** (F11).
* **Outside this codebase, only an active advisory moves a pin** (the owner, 2026-10-30). Host
  installs and magic-git's standard are outside it.
* **What another product gates is changed under that product's record** (F7's lockstep with
  magic-git's goldens). magic-git's devenv tooling has no record of its own: 0169 wrote it, so
  this record changes it the way 0169 did.
* **Each move is reversible on its own,** so one that fails a gate does not hold the rest.

## Considered Options

* **A — Everything to its publisher's newest in one pass,** majors and engine pins included.
* **B — Tiered by risk** (chosen):
  1. the advisory fix;
  2. in-range moves that change no behaviour;
  3. the moves that do, each a decision of its own;
  4. engine pins with their own live runs.
* **C — The advisory fix only;** the rest waits for the next audit.
* **D — Nothing.**

## Decision Outcome

Chosen option: **"B — Tiered by risk"**, because:

* it ships the one fix that matters to users now (F7) without waiting on anything;
* it moves every other pin to current under the same rule (D1);
* and it keeps each behaviour change visible as its own decision, which A buries in a single diff
  and C and D leave undone.

### The decisions (accepted 2026-10-30)

* **D1 — "Current" is 0169 D1 for every pin.** That is the newest stable release that is supported,
  has no advisory affecting it, is inside what its consumer supports, and passes this repository's
  gates. A pin with an affected newest release and no fix (F2's `GO-2026-5932`, F10's JDK) stays
  where it is, and is named in the record.
  * **Scope, the owner's, 2026-10-30.** The rule moves every pin in this codebase. A pin outside it
    moves only while an advisory is active on it. That covers the host installs, magic-git's
    `standard.json`, and developer tooling such as Python. When it does move, it moves to the
    version the rule gives, or to the version the owner holds (D13).
  * By F12's table, that is Go, Node and Python (D11, D14). Flutter moves under magic-git's own
    pair (D2). Java has no fix (D9). Git for Windows, gh and glab have no advisory, and stay.
* **D2 — Flutter 3.47.9 with Dart 3.13.7, first, in a commit of its own (F7).**
  * `FLUTTER_VERSION: "3.47.9"`.
  * `pubspec.yaml`'s `sdk: ^3.12.2` is left alone: it is a floor, and 3.13.7 satisfies it.
  * This Mac's Flutter moves to 3.47.9 for the gates.
  * ~~The next release's APK is built with it. A release is what reaches users, so the PLAN asks
    the owner whether to cut one at once.~~ *Owner's answer, 2026-10-29: "First, then cut a release
    (Recommended)".* A release is cut straight after this phase's commit, so the fixed APK reaches
    users. The agent prepares it, and the owner pushes and tags, each on an explicit ask.
  * magic-git's `build_macos.sh` pin and goldens (0169 D3's lockstep) are listed for that
    repository, and not changed from here. *Owner's answer, 2026-10-30: "magic-git's own pair
    (Recommended)".* That pair is also where `standard.json`'s Flutter row and the other hosts'
    Flutter move, with the pin. Until then, the audit reports this Mac's Flutter as drift
    from the standard. ~~magic-git's `CLAUDE.md` says to use its vendored `.flutter-sdk` whenever
    `flutter` on PATH disagrees with `build_macos.sh`, so this Mac's move leaves its goldens
    alone.~~ *Corrected 2026-10-30, while writing the PLAN:* this Mac has no `.flutter-sdk` in
    magic-git (`.flutter-sdk/bin/flutter`: no such file). `build_macos.sh` uses the Flutter on PATH
    when its `git describe` tag matches the pin (`:158-167`), and that is Homebrew's 3.47.5 today.
    After this phase, PATH gives 3.47.9. magic-git's `CLAUDE.md` then says to use the vendored SDK,
    which `build_macos.sh` clones (`:175-178`, `git clone --depth 1 --branch 3.47.5`, gitignored at
    `.gitignore:53`). So magic-git's goldens are safe only on that SDK, and phase 5 clones it before
    running magic-git's gates.
* **D3 — Node 26, as 0169 D4 already decided (F6).**
  * `NODE_VERSION: "26"` in `ci.yml` and `docs.yml`, and `26` in `.node-version` and `.nvmrc`.
  * The only thing CI runs on Node is `markdownlint-cli2` (D6).
* **D4 — Go modules to their newest, with `go-selfupdate-lib` as D5 says (F2).**
  * `go get -u ./...`, then `go mod tidy`. That moves `certmagic`, `x/mod` and every indirect
    module in the build. ~~`acp-go-sdk2` is already current.~~ *Amended 2026-10-30, at PLAN P3
    step 1 (Deviation 7), by the owner: "Take v1.27.0".* It also moves `acp-go-sdk2` to v1.27.0
    (F3b). *Amended again 2026-10-30 (PLAN Deviation 14), by the owner: "acp-go-sdk2 just got a new
    tagged release. we need to update the pin."* Then to v1.28.0 (F3c). *Amended again 2026-11-01
    (PLAN Deviation 16), by the owner: "assess acp-go-sdk2 and go-selfupdate-lib, and go-tui-lib.
    pin the current tagged versions."* Then `acp-go-sdk2` to v1.31.1 and `go-tui-lib/v2` to
    v2.9.0; `go-selfupdate-lib` is already current (F3d). *Amended again 2026-11-04 (PLAN
    Deviation 19), by the owner: "We need to update pins for magic-cli-remote and gobble-cli",
    then "Sibling libs only".* Then `acp-go-sdk2` to v1.33.0 and `go-tui-lib/v2` to v2.12.0
    (F3f). The indirect modules `go list -m -u` shows behind stay where they are. *Amended again
    2026-11-05 (PLAN Deviation 20), by the owner: "pin the latest go-acp-sdk2", then "we need to
    ensure the sibling repo dependencies are all pinned to latest. acp-go-sdk2 just updated".*
    Then `acp-go-sdk2` to v1.34.0 (F3g). The other two sibling libraries are current. *Amended
    again 2026-11-07 (PLAN Deviation 22), by the owner: "go-llmprovider-sdk, acp-go-sdk2 both
    have new releases. we need to pin them."* Then `acp-go-sdk2` to v1.34.2 and
    `go-llmprovider-sdk` to v1.28.0 (F3h). `go-selfupdate-lib` and `go-tui-lib/v2` are current.
    *Amended again 2026-11-10 (PLAN Deviation 25), by the owner: "we need to refresh the sibling
    lib/sdk pins they have updated tagged releases. ensure these pins are pinning current releases
    for these dependencies."* Then `go-llmprovider-sdk` to v1.29.0, `go-selfupdate-lib` to
    v1.26.0 with both `ci.yml` pins, and `acp-go-sdk2` to v1.35.0, tagged the same day (F3i).
    `go-tui-lib/v2` and `gobble-cli/acpext` are current. *Amended again 2026-11-10 (PLAN
    Deviation 27), by the owner: "Run p3i".* Then `go-selfupdate-lib` to v1.26.2, with both
    `ci.yml` pins and one test assertion (F3j). `gobble-cli/acpext` v0.4.0 is moved under
    `0179-MADR-gobble-native-acp-provider.md` A9, not here.
    *Amended again 2026-11-11 (PLAN Deviation 29), by the owner's ruling of that day, relayed by
    the cross-repository coordinator of the auth-contract work with none of the owner's wording
    to quote.* Then `acp-go-sdk2` to v1.36.0, together with gobble-cli's own move and before
    gobble's `v0.6.0` (F3k). The move is decided. *Its phase, P3j, was approved by the owner on
    2026-11-11 and ran that day; the PLAN's "P3j, executed" records it.*
  * `make vulncheck` must stay "clear" apart from `GO-2026-5932`.
* **D5 — `go-selfupdate-lib` ~~v1.23.0~~ v1.23.1, as an amendment to 0207 (F3, F3a).**
  *Amended 2026-10-30 by the owner, before phase 3: "pin go-selfupdate-lib to current v1.23.1".*
  v1.23.1 was tagged after the inventory, and is its current release (F3a).
  * `go.mod`, `go.sum`, and both `ci.yml` pins move to ~~v1.23.0's~~ v1.23.1's commit, with its
    comment.
  * Its behaviour changes are checked by:
    * Magic's service tests;
    * the runit/s6 live supervisors job;
    * the Windows test host's `make ci-windows`;
    * and a read of every `Start` caller for the new `ErrNotInstalled` and `ErrUnhealthy` returns.
  * Adopting its new APIs (`Plan` and `Apply`, `servicetest` for 0212's backends) is not a pin. It
    waits for a record of its own.
* **D6 — CI actions and tool pins to current (F4, F5).**
  * Actions, by peeled commit, each with its exact version comment:
    * `setup-node` v7.1.0 `949feb24…`;
    * `setup-java` v6.1.0 `b843ebf9…`;
    * `upload-artifact` v7.0.2 `cf430e03…`;
    * `download-artifact` v8.0.2 `9000827c…`;
    * `flutter-action`'s comment becomes `# v2.23.0`, on the same commit.
  * `govulncheck` v1.9.0, in `ci.yml:127` and `scripts/vulncheck.sh:31` together.
  * `markdownlint-cli2` gets a pin: `npx --yes markdownlint-cli2@0.24.0` in `Makefile:315`. Its run
    then no longer depends on the day it happens.
  * `staticcheck` v0.8.2 and `golint` are current, and stay.
* **D7 — Dart packages to their newest, the two majors included (F8, F8a, F8b).** *Owner's
  answer, 2026-10-29: "Both now".*
  * `flutter pub upgrade`: the 13 in-range moves, and the transitive packages they allow.
    `pubspec.yaml`'s floors are not raised for those.
  * ~~`cupertino_icons` 2 and `flutter_secure_storage` 11 are not taken here.~~ Both majors are
    taken. `pubspec.yaml` says `cupertino_icons: ^2.0.0` and `flutter_secure_storage: ^11.2.0`, then
    `flutter pub upgrade --major-versions` is limited to those two.
  * `flutter_secure_storage` 11 holds the client identity and the pinned fingerprints (0066). F8a is
    why no migration code is needed. The emulator proves it:
    1. pair the AVD using an APK built from the commit before this phase;
    2. install this phase's APK over it, keeping the app's data;
    3. the app reconnects with no re-pair;
    4. the client identity's public key is unchanged, from the app's diagnostics;
    5. the pinned fingerprint is unchanged.

    A re-pair, or a changed key, is a stop, not a workaround.
* **D8 — Android build tools to the newest inside Flutter 3.47.9's known range (F9):**
  * Gradle **9.3.1**;
  * AGP **9.1.1**, the newest with Flutter's full Kotlin support. 9.2.1 is supported but past
    `maxKnownAgpVersionWithFullKotlinSupport`. *Owner's answer, 2026-10-29: "AGP 9.1.1
    (Recommended)";*
  * Kotlin Gradle plugin **2.4.0**.

  The PLAN re-reads the bounds in the 3.47.9 SDK, and the build must show no Flutter version
  warning. The bytecode target stays 17 (0168 D2).
* **D9 — Java: no change (F10).** `"25"` already takes the newest Temurin 25. The 2026-10-24
  advisory has no fixed build yet. The record names it, and 0169's audit flags the fix when it
  ships.
* **D10 — Engine pins: each its own decision, with its own live run (F11).** *Owner's answer,
  2026-10-29: "Each its own record (Recommended)".*
  * This record moves none of them. It lists them, with the installed versions.
  * Each move is 0137's procedure: a capture, the wire-shape tests re-read, and the pin moved.
  * grok 1.0.70's capture already exists. opencode 2.x waits on 0214.
* ~~**D11 — The host standard is magic-git's to update (F12).** The audit's output is quoted here.
  `standard.json` and the host installs are 0169 P14's, in that repository.~~ *Replaced
  2026-10-30, at the owner's ask: "spec and scope these in".*
* **D11 — The host standard's rows with an active advisory move here (F12, D1's scope).** In
  magic-git's `scripts/tools/devenv/standard.json`:
  * `go` 1.27.1 → **1.27.3** (F13). The hosts are already there, so the row catches up with
    them;
  * `node` 24.21.0 → **26.13.0**, `line` `"24"` → `"26"`, the note rewritten for 26 (F14,
    0169 D4);
  * `python` 3.14.7 → **3.14.10**, held on the 3.14 line (D13, F15);
  * each moved row's `verified` set to the day it is checked, and its note made true. Go's note
    keeps "Every binary in GOBIN and ~/go/bin is built by this version", which F13's hosts already
    satisfy.

  Not moved here:
  * `flutter`, by magic-git's own pair (D2);
  * `java`, which has no fix (D9);
  * `git` (Git for Windows), `gh` and `glab`, which have no advisory (D1's scope).

  The file's `record` field keeps naming 0169 D11, whose rule it still is. The moved rows' notes
  cite this record. 0169's P12 step 3 and P14 step 4, which never reached the file (F12), are
  closed by this, and noted in 0169's PLAN by pointer.
* **D12 — One PLAN, in five phases, each its own commit:**
  1. D2;
  2. D3 and D6;
  3. D4 and D5;
  4. D7 and D8;
  5. D11, D13 and D14: the host standard, the audit's hold, and the hosts. *Added 2026-10-30.*
     * Its commits are the owner's in magic-git (`standard.json`, `version_audit.py`, the devenv
       `README.md`) and in the dotfiles repository (`00-paths.sh` and the drop-in), and this
       PLAN's execution record here.
     * It touches no file of this repository's product, so it can run at any point. It runs
       last, so the D2 release is not held for it.

  Phases 1 to 4 are gated on macOS, the Linux and Windows test hosts, and, where they touch the
  app, the APK build and the emulator. Phase 5 is gated by the audit over every host's probe, and
  by magic-git's own pre-staging gates. The owner's word is asked for those runs.
  * 0216's stderr plan continues afterwards. Its P2 is staged and is committed first.
  * D5 also amends 0207. D2, D3, D11 and D13 are noted in 0169, by pointer: D13 because it
    departs from 0169 D1 for Python.
* **D13 — Python is held on 3.14 at the owner's word, and the audit learns what a hold is (F15).**
  *Owner's answers, 2026-10-30: "ensure the python is latest 3.14 without advisories", and "Held
  line in audit (Recommended)".*
  * This departs from 0169 D1 for Python alone: "the newest line" would be 3.15.
  * `standard.json`'s `python` gains `"held_line": "3.14"` and a `"held_reason"` that cites this
    record. Its existing `line` already says `3.14`.
  * `version_audit.py`'s `s_python`, when `held_line` is set:
    * judges staleness against the newest patch of the held line, not of the newest line. A newer
      3.14 patch is still `STALE`, and still fails;
    * reports a newer line as a new state, `HELD`, which names the line and its newest patch
      (`3.14 held; newest line 3.15 (3.15.2)`). `HELD` is not in `FAIL_STATES`, so it does not
      change the exit status;
    * reports `UNSUPPORTED`, a fail, once the held line is past its end of life, so a hold cannot
      outlive its line's security support;
    * leaves the advisory check as it is: OSV against the standard's version, then the held line's
      newest patch as the fix candidate (`newest_python`, `:428-431`, is already per line);
    * fails with `ERROR` when `held_line` disagrees with `line` or with the standard's version.
      A typo then cannot hold a line nobody chose.
  * Hosts are still judged against the standard's version, as drift. The hold changes only how
    the standard is judged against the publishers.
  * The devenv `README.md` documents `held_line` and `HELD`.
  * The other toolchains do not get a hold. Python is the only line the owner has held, and a
    general mechanism would be a decision nobody has made.
* **D14 — The hosts move with their rows (F13 to F15).** Each host is changed the way 0169 changed
  it, with a dated backup first and the old version left installed until nothing needs it (0169
  C4):
  * **Go:** none moves. Each host's `go version` is read, and must say `go1.27.3`.
    *Amended 2026-10-30, at PLAN P5d (Deviation 13), by the owner: "Fix the Go rows in P5".* The
    audit over the hosts found every Go bin directory's tools built with go1.27.2 (`DRIFT`), and
    the kept go1.27.2 installs, with go1.27.1 on the Windows test host, carrying advisories
    (`ADVISORY` GO-2026-6809; 16 on go1.27.1). Those advisories bring them into scope (D1's rule).
    So on each host every tool not built with go1.27.3 is rebuilt with it, at the module version
    it already records (`go install <path>@<version>`), and the old Go installs are removed once
    nothing on the host names them.
  * **Node 26.13.0,** each archive or installer checked against nodejs.org's `SHASUMS256.txt`:
    * this Mac: `brew upgrade node`;
    * the Linux test host: `~/sdk/node-v26.13.0`, with PATH swapped in the dotfiles'
      `00-paths.sh` and in the `mcremote` drop-in, then `systemctl --user daemon-reload`. The
      owner commits the dotfiles change there;
    * WSL: `~/sdk/node-v26.13.0`, with PATH swapped in `~/.config/devenv.sh`;
    * the Windows test host: `node-v26.13.0-x64.msi`.

    Each host's npm global packages are listed before and after, and each one's `--version`
    must answer. `--version` spends no provider quota.
  * **Python 3.14.10:**
    * this Mac: `brew upgrade python@3.14`;
    * the Linux test host and WSL: python-build-standalone's 3.14.10 `install_only` archive, checked
      against its GitHub asset digest, in `~/sdk/python-3.14.10`. `~/default-venv` is rebuilt on it
      from a saved `pip freeze`, and the old one is kept as a backup until that passes (0169 P0
      step 4);
    * the Windows test host: `py install --update 3.14`.

    Distribution system Pythons stay the distribution's (0169 D6).
  * A host whose install does not verify is left on its old version, and the PLAN stops for the
    owner.
  * Homebrew's `node` formula follows Node's newest release line, odd lines included. When it
    moves past 26, this Mac's `node` drifts from the standard, and the audit reports it.

### Consequences

* Good, because the shipped app stops accepting certificates forged under the compromised CAs.
  That covers its Let's Encrypt-mode, relay and update-check connections (F7).
* Good, because every pin ends current under one stated rule, and the ones that cannot move (D1,
  D9, D10) are named rather than forgotten.
* Good, because `markdownlint-cli2` stops floating in CI (D6).
* Good, because the Android build stays inside what Flutter knows (D8), rather than on versions
  Flutter would warn about.
* Neutral, because `go get -u ./...` moves 33 indirect modules that no one chose individually. The
  gates and `govulncheck` are what check them, as for any tidy.
* Bad, because `go-selfupdate-lib` v1.23.0 changes how `Start` fails for a service that isn't
  installed, on launchd and on every supervised backend. Magic's handling exists, but the live
  job and the Windows host are what prove it.
* Bad, because the four engine pins stay behind after this record, by decision. Each needs a live
  run this record does not do.
* Bad, because `flutter_secure_storage` 11 can reset the phone's store if it cannot read it
  (`resetOnError`). F8a argues that it can read it, and D7's emulator check is the proof.
* ~~Bad, because Flutter's lockstep with magic-git (D2) and the stale host standard (D11) are left to
  another repository.~~ *Superseded 2026-10-30 by D11's replacement.*
* Good, because no host and no standard row is left with an advisory that has a fix: Go's 16,
  Node's security release and Python's 11 are closed (D11, D14).
* Good, because the audit can pass again while Python is held. A newer 3.14 patch, an advisory,
  or the held line's end of life still fails it (D13).
* Bad, because Flutter's lockstep with magic-git (D2) waits on magic-git's own pair. Until then,
  the standard's Flutter row keeps its advisory, and the other hosts' local Flutter builds keep
  accepting the compromised CAs. Released APKs are built in CI, which D2 fixes.
* Bad, because a hold is an exception to 0169 D1 that the audit no longer flags as stale. Only the
  record, and the `HELD` row on every run, keep it visible.
* Bad, because phase 5 changes four hosts and two other repositories. Each change has a backup and
  a rollback, but the dotfiles and magic-git commits are the owner's to make.
* Neutral, because Git for Windows, gh and glab stay `STALE` in the audit by the owner's scope.
  The audit still exits 1 for them until a later move, or until one of them has an advisory.

### Confirmation

* **D2:** `flutter --version` on this Mac gives 3.47.9 and Dart 3.13.7. `make preflight` passes.
  CI's Flutter jobs run 3.47.9. A dispatched `Android APK` run builds.
* **D3:** CI's `setup-node` resolves 26.x, and `make markdownlint-docs` passes on it.
* **D4, D5:** `go list -m -u` shows no newer version for a direct module. `make vulncheck` is
  "clear". `TestWorkflowPinsMatchGoMod` passes. `make test`, `make race`, the runit/s6 live job,
  the Linux test host, and `make ci-windows` on the Windows test host all pass.
* **D6:** every `uses:` names a peeled release commit with its exact version.
  `make markdownlint-docs` prints `markdownlint-cli2 v0.24.0`.
* **D7, D8:** `flutter pub outdated` shows no direct dependency behind. `flutter build apk` shows
  no AGP, Gradle or Kotlin warning. On the emulator, a pairing made with the previous APK survives
  the upgrade, with the same client key and pinned fingerprint.

  *Amended 2026-10-31 (PLAN Deviation 15).* CI's APK job, on the dispatch after P4 (run
  41210038095), shows AGP 9's deprecation of `android.builtInKotlin=false` and
  `android.newDsl=false`. It shows the old `android {}` DSL warning for this app and for seven
  plugins, and Gradle's JDK restricted-method warning. P1's tag run, before D8, shows the same
  warnings. The local `flutter build apk` did not print them, so the line above was not true of
  CI. On the owner's decision, a new MADR and PLAN move this app's build to built-in Kotlin.
  A probe in scratch copies showed Flutter 3.47.9's Gradle plugin fails to apply under
  `android.newDsl=true`, so the new DSL, and the old-DSL warnings, wait on Flutter. The plugins'
  own warnings wait on their maintainers. D7 and D8 are unchanged; this check stays open until
  that pair lands.
* ~~After all four phases, magic-git's `version_audit.py` is run again, and every flag it still
  raises is one D9, D10 or D11 names.~~
* **D13:** the held-line logic is seen failing first, on scratch copies of the standard, never in
  magic-git's tree:
  * a held standard behind the line's newest patch gives `STALE`;
  * `--today` past 3.14's end of life gives `UNSUPPORTED`;
  * a `held_line` that disagrees with `line` gives `ERROR`;
  * a standard with no `held_line` still gives `STALE ... -> 3.15.2`, as today;
  * the real standard gives `HELD`, and exit 0 when nothing else fails.

  The audit still runs under macOS's system Python 3.9 (its own floor, and magic-git's
  `dependencies.py`).
* **D11, D14:** after phase 5, the audit over a fresh probe of every host, against the moved
  `standard.json`, reports:
  * no Go, Node or Python row except `HELD`;
  * no `ADVISORY` row except Java's (D9) and Flutter's (magic-git's pair);
  * `STALE` only for Flutter, Java, Git for Windows, gh and glab.

  Each of those is a row this record names. On each host, `go version` gives 1.27.3, `node
  --version` v26.13.0, and the developer `python3` (`python` on Windows) 3.14.10. Every npm global
  package's `--version` answers as before.

## Pros and Cons of the Options

### A — Everything at once

* Good, because it is one change, and nothing stays behind.
* Bad, because the advisory fix waits on the riskiest move in the set.
* Bad, because the secure-storage major, the self-update library's behaviour change and four engine
  pins arrive in one diff, and a failure cannot be traced to one of them.
* Bad, because the engine pins need live runs (quota) and wire re-reads, and Gradle past Flutter's
  known range is what Flutter warns against.

### B — Tiered by risk (chosen)

* Good, because the fix that matters ships first.
* Good, because each behaviour change keeps its own decision.
* Bad, because four commits and three later records are more work than one pass.

### C — The advisory fix only

* Good, because it is the smallest safe change.
* Bad, because the owner asked for every pin, and Node is already behind 0169's own date (F6).

### D — Nothing

* Bad, because the shipped app stays exposed (F7).

## More Information

### Open questions for the owner

1. **The order.** D2, Flutter, first in a commit of its own, before 0216's P3 resumes? And is a
   release with the fixed APK cut straight after?
2. **D7's two Dart majors.** Leave both for records of their own (proposed); take `cupertino_icons` 2
   now and leave secure storage; or take both now?
3. **D8's AGP.** 9.1.1, the newest with Flutter's full Kotlin support (proposed), or 9.2.1, the
   newest Flutter supports at all?
4. **D10's engine pins.** Leave each to its own record (proposed); move grok's to 1.0.70 now, on its
   existing capture; or schedule live runs for all four?

*Answered 2026-10-29:*

1. "First, then cut a release (Recommended)": D2.
2. "Both now": D7, with F8a and F8b researched after the answer.
3. "AGP 9.1.1 (Recommended)": D8.
4. "Each its own record (Recommended)": D10.

*Asked and answered 2026-10-30, after the owner scoped in the host standard:*

1. **How the standard holds Python on 3.14,** when `STALE` fails the audit (F15). "Held line in
   audit (Recommended)": D13.
2. **Git for Windows, gh and glab,** stale with no advisory. The owner did not pick an option, and
   answered: "when i said to update all pins i did not mean for tooling (python) or pins outside of
   the magic-cli-remote codebase itself, eg: node, etc, unless they have active advisories." Read as
   D1's scope rule. So they stay, and Go, Node and Python move because each has an active
   advisory (F12).
3. **The standard's Flutter row and magic-git's pin.** "magic-git's own pair (Recommended)": D2.

~~The record stays `proposed` until the owner approves it.~~ *Accepted 2026-10-30: "committed,
proceed", in answer to "Approve the MADR and I'll write the five-phase PLAN".* The plan is
[0217-PLAN-update-every-pin-to-its-current-release.md](0217-PLAN-update-every-pin-to-its-current-release.md).

### Evidence index

* `leftovers/pins_latest.py` and `pins_latest.json`: go.dev, nodejs.org, the Flutter release feed,
  the GitHub API for each action (latest release and peeled commit), the Go module proxy, npm,
  Gradle, Google's Maven and Maven Central, and Adoptium.
* `leftovers/android_bounds.py`: the AGP, KGP and Gradle patch releases. Flutter's bounds come from
  `/opt/homebrew/share/flutter/packages/flutter_tools/lib/src/android/gradle_utils.dart` at 3.47.5.
* `leftovers/engine_versions.py`: each engine's `--version`, and npm.
* `flutter pub outdated --json`, summarised by `leftovers/pub_outdated_summary.py`.
* `go list -m -u all`; OSV `GET /v1/vulns/GO-2026-5932`.
* `gh api repos/dart-lang/sdk/security-advisories/GHSA-frr6-h895-5pxp`.
* `go-selfupdate-lib`'s `CHANGELOG.md` at `v1.23.0`, from the contents API.
* `leftovers/gsl_peel.py v1.23.0 v1.23.1`, 2026-10-30 (F3a): both tags peeled through `gh api`,
  the compare between them, `CHANGELOG.md` at `v1.23.1`, and `go list -m -versions`. Also
  `go.mod` at `v1.23.1` from the contents API, and `go list -m -u` for `x/sys`, `x/term`, `x/mod`
  and `go-selfupdate-lib` in this repository.
* `leftovers/release_diff.py maccavelli/acp-go-sdk2 v1.26.0 v1.27.0`, 2026-10-30 (F3b): both
  tags peeled, the compare between them, and its `go.mod` and `go.sum` patch. Also that
  repository's `docs/reports/0043-REPORT-release-notes-v1-27-0.md` at `v1.27.0`, from the contents
  API.
* magic-git `scripts/tools/devenv/version_audit.py` against its `standard.json`, run read-only on
  2026-10-29: "4 ADVISORY, 1 ERROR, 1 REVIEW, 8 STALE". The ERROR is that this Mac has no probe
  documents.
* `leftovers/standard_candidate.py`, 2026-10-30. It copies `standard.json` to the scratchpad with
  Go 1.27.3, Node 26.13.0 and Python 3.14.10, and runs the audit on the file and on the copy:
  * the file: "4 ADVISORY, 1 ERROR, 1 REVIEW, 8 STALE; exit 1";
  * the copy: "1 ADVISORY, 1 ERROR, 1 REVIEW, 6 STALE; exit 1". Its one advisory is Java's, and
    its Python row is `STALE 3.14.10 -> 3.15.2`.
* `leftovers/python_osv.py`, 2026-10-30: each CPython tag's commit through `gh api`, then OSV
  `POST /v1/query` by commit (F15's table).
* endoflife.date `GET /api/python/3.14.json`: `latest` 3.14.10, `latestReleaseDate` 2026-10-25,
  `eol` 2030-10-31.
* nodejs.org `dist/index.json` (F14), and `dist/v26.13.0/SHASUMS256.txt`, which lists
  `node-v26.13.0-x64.msi` (`0e5ee99c…`), `-linux-x64.tar.xz` (`d28a96af…`) and `-linux-x64.tar.gz`
  (`58485538…`).
* python-build-standalone's release `20261026` carries
  `cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-install_only.tar.gz`.
* `leftovers/mac_toolchains.py`, 2026-10-30: this Mac's `go`, `node` and `python3` in a login
  shell, and `brew list --versions` / `brew info` for `node`, `node@24` and `python@3.14`.
* magic-git, read only: `git log` of `standard.json`, and `version_audit.py` at `:46`, `:61-69`,
  `:144-146`, `:285-295`, `:428-437` and `:569-578`.

### Related records

* [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md): D1's rule, D3, D4, D6, D10, D11, D15. D13 departs from D1 for Python.
* [0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md): how each host was installed (P0, P6, P7), and the standard's Go row that P12 and P14 left for the owner (F12).
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md): I-4, and the amendment D5 adds.
* [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md): the backends F3's `Supervised` change reaches.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md): the engine pins (D10).
* [0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](../../apps/mobile/docs/decisions/0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md), in the mobile tree: D2, the bytecode target.
* [0174-MADR-keep-dependencies-free-of-known-advisories.md](0174-MADR-keep-dependencies-free-of-known-advisories.md): the advisory gate D4 keeps.
* [0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md](0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md): its plan, which resumes after this one.
* [0066-MADR-secure-storage-upgrade-resilience.md](0066-MADR-secure-storage-upgrade-resilience.md):
  the storage the `flutter_secure_storage` major touches.
