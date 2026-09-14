---
status: proposed
date: 2026-08-17
decision-makers: Project Owner (scope and acceptance)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Ship a Linux-only `curl | sh` bootstrap installer, and add `linux/arm64` to the release matrix

## Context and Problem Statement

Installing `mcremote`/`mcrelay` today requires cloning the repository and
running `make install`, which needs a Go toolchain. The published releases
carry ready-to-run binaries, but nothing turns a release asset into an
installed, running, service-managed daemon in one step. The goal is the
now-standard one-liner:

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

Scope is **Linux only**. macOS is deliberately excluded: its install path
depends on code-signing identity for durable TCC grants
([ops-macos-tcc.md](ops-macos-tcc.md), MADR 0069 D6), and distributing to
machines the operator does not own additionally requires a paid Developer ID
and notarization. None of that machinery exists on Linux — there is no
Gatekeeper, no quarantine attribute, no notary service, and no TCC — so the
Linux installer can ship now and macOS can be added later as one additional
branch without redesign.

The target set is the operator's own hosts (Ubuntu 26.04, Oracle Linux 9)
plus the environments a public one-liner will inevitably meet: Rocky/RHEL 9,
Debian, Alpine, WSL2, and containers.

### Assessment: what already exists

The binaries already do most of the work, which makes this a *bootstrap*
problem rather than an installer problem:

| Capability | Where it lives | Consequence for the script |
|---|---|---|
| Service unit generation, enable, start | `mcremote setup-service` (`internal/cli/setup_service.go`) | Script delegates; writes no unit files itself |
| `loginctl enable-linger`, with opt-out | `setup_service.go:85` (`--no-linger`) | Script delegates; linger is already handled |
| Graceful degradation with no `systemctl` | `internal/cli/service/setup.go` (runner is injectable, failures return a "finish manually with…" error) | Script does not need its own no-systemd branch to avoid crashing |
| Release discovery, checksum verify, atomic swap, service restart | `internal/update` (MADR 0065) | Script is needed **once**; `mcremote update` handles every subsequent upgrade |
| Stop → swap → start on install | `scripts/install-binary.sh` | Reusable shape; the script's own install step can be simpler |
| Default install dir `~/.local/bin` | `Makefile` (`USER_BIN_DIR`) | Script matches it, so `make install` and the script converge |

This is the decisive architectural fact: **the installer is a bootstrap, not
a package manager.** It must get one verified binary onto the host and then
hand off. Anything else — update logic, unit templating, linger — already
exists in Go, is already tested, and must not be reimplemented in shell where
it would silently drift.

### Assessment: the binaries are fully static

`Makefile:38` sets `CGO_ENABLED ?= 0`, and Linux builds add
`-tags netgo,osusergo` (`Makefile:32`) to pin pure-Go DNS and passwd
resolution. Verified against the published artifact:

```console
$ file mcremote-linux-amd64-0.12.0.1
ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, Go BuildID=…, stripped
```

**Statically linked** is the single most important portability fact in this
record. It means one `linux/<arch>` artifact runs unmodified on glibc
(Ubuntu, Oracle, Rocky, Debian) *and* musl (Alpine). There is no libc split,
no `-musl` build variant, no `ldd` probing, and no glibc-version floor. Most
installer complexity in comparable projects exists to solve a problem this
codebase does not have.

### Assessment: the release matrix is missing `linux/arm64`

`.github/workflows/ci.yml:161-163` builds exactly three platforms:

```text
linux/amd64
darwin/arm64
darwin/amd64
```

Confirmed against the current release `v0.12.0`, whose assets are
`{mcremote,mcrelay}-{darwin-amd64,darwin-arm64,linux-amd64}-0.12.0.1` plus
`SHA256SUMS-0.12.0.1` and the Android APK. There is **no `linux-arm64`
asset.**

A public installer that 404s on Raspberry Pi, Ampere, AWS Graviton, Oracle
Cloud's free ARM tier, and Apple-silicon Linux VMs is not a Linux installer.
Oracle Cloud's always-free tier is specifically ARM, which makes this
directly relevant to a host already running Oracle Linux. Because the build
is pure Go with `CGO_ENABLED=0`, adding the target is a matrix entry, not a
toolchain project.

### Assessment: asset names cannot be constructed, and this is verified

The obvious installer approach uses GitHub's convenience redirect, which
needs no API call and is not rate-limited:

```text
https://github.com/OWNER/REPO/releases/latest/download/<asset-name>
```

It requires the **exact** asset filename. This repository's names embed the
full build version — `mcremote-linux-amd64-0.12.0.1` — which the script
cannot know in advance. `internal/update/github.go:85` does not guess it
either; it prefix-matches `<product>-<goos>-<goarch>-` against the release
JSON precisely because the suffix is unknowable.

Verified live:

```console
$ curl -sI https://github.com/maccavelli/magic-cli-remote/releases/latest
HTTP/2 302
location: https://github.com/maccavelli/magic-cli-remote/releases/tag/v0.12.0

$ curl -sI -L -o /dev/null -w '%{http_code}\n' \
    https://github.com/…/releases/latest/download/mcremote-linux-amd64-0.11.2.1
404
```

The tag redirect works; the asset URL 404s because the version in the name no
longer matches the latest release. This is the central mechanical problem the
decision must solve.

The fallback — querying `api.github.com/repos/…/releases/latest` and parsing
asset names — works, but GitHub's documented primary rate limit for
unauthenticated requests is **60 requests per hour, scoped to the originating
IP address** (5,000/hr authenticated). That is per-IP, so it is shared by
everyone behind a corporate NAT, a CI runner pool, or a university network,
and it fails at exactly the moment an install is most visible. It also forces
JSON parsing in POSIX shell.

### Assessment: what is genuinely environment-specific

Static binaries erase the libc problem, so the remaining variance is entirely
about **service management** and one sandboxing wrinkle:

| Environment | Init | libc | Notes |
|---|---|---|---|
| Ubuntu 26.04 | systemd | glibc | Target host. AppArmor userns restriction, below |
| Oracle Linux 9 / Rocky 9 | systemd | glibc | RHEL 9 family; SELinux enforcing by default |
| Debian | systemd | glibc | Baseline case |
| Alpine | **OpenRC** | **musl** | No `systemctl`, no `loginctl`; `/bin/sh` is busybox `ash` |
| WSL2 | systemd **opt-in** | glibc | Requires `systemd=true` in `/etc/wsl.conf` + `wsl --shutdown` |
| WSL1 | none | glibc | No service management; upstream installers reject it outright |
| Containers | usually none | either | PID 1 is the app; no user bus |

Three consequences:

1. **`systemd --user` is not universal.** Alpine has no systemd at all.
   WSL2 has it only when explicitly enabled — per Microsoft's documentation
   it is off by default and needs `[boot] systemd=true` plus a full
   `wsl --shutdown`, and older inbox WSL builds cannot do it. Containers
   generally have no user bus. The script must treat "no service manager" as
   an ordinary outcome, not a failure.
2. **Linger is required, not optional, for the real use case.** A systemd
   *user* service is bound to the session unless lingering is enabled; the
   daemon's whole purpose is to keep running after the operator disconnects.
   `setup-service` already runs `loginctl enable-linger` and exposes
   `--no-linger`, and README:197 documents it. A related trap: entering a
   session via `su` rather than a login path skips `pam_systemd`, so
   `XDG_RUNTIME_DIR` is unset and every `systemctl --user` call fails with a
   confusing bus error. The script should detect and explain this rather
   than surface the raw failure.
2a. **Rootless boot persistence is a systemd-specific capability.** This is
   the load-bearing constraint for non-systemd support, and it is structural
   rather than an implementation gap: for a user service to start at boot,
   *something running as root at boot* must start it. systemd solves this
   with `loginctl enable-linger`, which starts the per-user manager at boot
   without a login session. OpenRC's native user services are experimental
   (introduced 0.60) and the Gentoo documentation records that OpenRC
   "always requires admin privileges to interact with a daemon", explicitly
   recommending runit's `runsvdir` for user-level supervision instead.
   runit and s6 *do* supervise rootlessly and well — but the user's
   `runsvdir`/`s6-svscan` must itself be started, and starting it at boot
   again requires root. A rootless installer can therefore deliver
   supervision-and-restart on these systems, but **cannot** deliver
   start-at-boot without one root action. The installer must state which of
   the two it achieved rather than implying persistence it did not set up.
   A tempting universal fallback — a `@reboot` crontab entry — is not
   dependable either: Alpine's busybox `crond` has long-standing gaps in
   `@reboot`/named-time support for non-root users.

3. **AppArmor restricted user namespaces (Ubuntu 24.04+, so also 26.04).**
   `scripts/apparmor/unprivileged_userns.local` and
   `scripts/bwrap-apparmor-fix.sh` exist for this (MADR 0048). It does not
   affect installing or running `mcremote`; it breaks **bubblewrap
   sandboxing inside the agent CLIs the daemon spawns**, surfacing later as
   a confusing sandbox failure. The remedy needs `sudo`, so it cannot live
   in a piped-to-shell path.

### Assessment: privilege

Comparable installers (Ollama) create a system user, write to
`/etc/systemd/system`, and require `sudo` throughout. This project does not
need any of that: binaries go to `~/.local/bin` and the unit is a
`systemd --user` service. **The installer requires no root.** That removes
the largest security objection to `curl | sh` — a piped script that never
escalates cannot silently modify the system — and it is a deliberate design
property worth preserving.

## Decision Drivers

* One-line install must work unattended on the common Linux environments,
  and fail with an actionable message everywhere else.
* No root. A piped installer that escalates is a materially different risk.
* No reimplementation of logic that already exists in Go and is tested.
* Integrity must be verified, not assumed from TLS alone.
* No dependence on a rate-limited API in the default path.
* The script is a bootstrap; `mcremote update` owns every later upgrade.
* Failure modes must be legible — a 404 mid-pipe is not a diagnosis.

## Considered Options

**Asset discovery**

* **A1 — Query the GitHub API and parse asset names in shell**
* **A2 — Publish stable-named alias assets and use the `latest/download` redirect**
* **A3 — Hybrid: alias first, API fallback**

**Installer scope**

* **B1 — Bootstrap only: fetch, verify, install, delegate to `setup-service`**
* **B2 — Full installer: shell-side unit templating, linger, and update handling**

**Architecture coverage**

* **C1 — Ship `linux/amd64` only, fail clearly on arm64**
* **C2 — Add `linux/arm64` to the release matrix now**

## Decision Outcome

Chosen: **A2 (stable aliases) + B1 (bootstrap only) + C2 (add `linux/arm64`)**.

**A2** because it removes the rate limit, the JSON parsing, and the
dependency on `jq` in one move. The release job already stages artifacts; it
additionally copies each Linux binary to an unversioned name
(`mcremote-linux-amd64`, `mcremote-linux-arm64`, and the `mcrelay`
equivalents) plus a stable `SHA256SUMS`, and attaches both. The versioned
assets remain untouched, so `internal/update`'s prefix matching
(`github.go:85`) keeps working unchanged and release auditability is
preserved. The installer then uses a URL it can construct without knowing any
version:

```sh
https://github.com/maccavelli/magic-cli-remote/releases/latest/download/mcremote-linux-${ARCH}
```

Version pinning uses the `releases/download/v<TAG>/` form. Note that a tag
does **not** determine the asset name: `v0.12.0` ships
`mcremote-linux-amd64-0.12.0.1`, and the trailing build serial (`.1`) is not
derivable from the tag. Pinning therefore also depends on the alias existing
per-release (`releases/download/v0.12.0/mcremote-linux-amd64`), which the
alias step provides for every release, not only the latest.

**Aliases break naive checksum verification, and the fix is load-bearing.**
The published `SHA256SUMS-<VER>` lists *versioned* filenames only — verified
against `v0.12.0`:

```text
f99b62c594ff…  mcremote-linux-amd64-0.12.0.1
a8ca6c85d827…  mcrelay-linux-amd64-0.12.0.1
```

So `sha256sum -c` against a file downloaded under the alias name
`mcremote-linux-amd64` fails on the name, not the content. The installer must
therefore verify **by hash value, not by filename**: select the manifest line
whose filename matches `mcremote-linux-<arch>-*`, extract the expected digest,
compute the digest of the downloaded file, and compare the two strings. This
has a useful side effect — the matched manifest line reveals the exact
resolved version (`0.12.0.1`) with no API call, which the script prints and
can cross-check against `mcremote version` after install.

**B1** because every capability a "full" installer would add already exists
in the binary, tested, on the other side of one `exec`. Shell reimplementation
would drift from the Go implementation and would have to be maintained against
six environments. The script's entire job is: detect, resolve, download,
verify, place, delegate.

**C2** because a Linux installer that excludes ARM is not credible in 2026,
the operator's own cloud tier is ARM, and `CGO_ENABLED=0` makes it a
one-line matrix change with no toolchain cost.

### Consequences

* Good, because the default path makes **zero API calls** and is therefore
  immune to the 60/hr per-IP limit that would break shared-egress networks.
* Good, because static linking means one artifact per architecture covers
  glibc and musl alike — Alpine needs no special build, only special *service*
  handling.
* Good, because requiring no root keeps the blast radius of a piped script to
  the invoking user's home directory.
* Good, because `mcremote update` already exists, so the script is run once
  per host and never becomes an upgrade path that must be maintained.
* Good, because ARM hosts become installable, including Oracle Cloud's
  always-free tier.
* Neutral, because alias assets roughly double the Linux asset count in each
  release; they are small, and the versioned names remain authoritative.
* Bad, because two names now point at the same bytes, so the release job must
  generate the aliases from the same files it checksums — generating them
  separately would allow the alias and the versioned artifact to diverge.
* Bad, because the script cannot fix the AppArmor userns restriction (needs
  `sudo`), so on Ubuntu 24.04+ it can only detect and instruct; an operator
  who ignores the notice will hit agent sandbox failures later, far from the
  install.
* Bad, because Alpine and container installs end without a running service.
  This is honest rather than broken, but it means "installed" and "running"
  are not the same outcome, and the script must say which one it achieved.

### Confirmation

The decision is satisfied when, on a clean host:

* `curl … | sh` installs both binaries to `~/.local/bin` and exits non-zero
  with a single actionable line on any unsupported platform.
* The downloaded artifact's SHA-256 is verified against the published
  `SHA256SUMS` **before** the binary is placed, using `sha256sum` or, where
  absent, `shasum -a 256` / `openssl dgst -sha256`.
* On systemd hosts, `systemctl --user is-active mcremote` reports active and
  `loginctl show-user "$USER" --property=Linger` reports `Linger=yes`.
* On Alpine/WSL1/containers, the script exits **0** having installed the
  binaries, printing how to run the daemon manually.
* `mcremote version` matches the release the installer resolved.
* Re-running the script is idempotent and does not duplicate units.
* A deliberately corrupted download fails the checksum step and installs
  nothing.

Test coverage lives in `scripts/install_test.sh` alongside the existing
`scripts/install-binary_test.sh` and must cover: arch mapping
(`x86_64`/`aarch64`/`armv7l`), unsupported-platform exit, checksum mismatch,
missing-`systemctl` degradation, and pinned-version resolution.

## Pros and Cons of the Options

### A1 — Query the GitHub API, parse assets in shell

* Good, because it needs no CI change and mirrors what `internal/update`
  already does.
* Good, because it always reflects reality — no alias can go stale.
* Bad, because unauthenticated callers get 60 requests/hour **per IP**, so
  one NAT'd office or CI pool exhausts it for everyone behind that address.
* Bad, because it means parsing JSON with `grep`/`sed` in POSIX shell, or
  taking a hard dependency on `jq`, which is absent from minimal images.

### A2 — Stable alias assets + `latest/download` redirect (chosen)

* Good, because the URL is constructible from `uname` alone: no API, no rate
  limit, no JSON, no `jq`.
* Good, because it is the pattern the ecosystem already relies on, so it is
  well-trodden and cache-friendly.
* Neutral, because it requires a small, one-time release-job change.
* Bad, because it introduces a second name for identical bytes, which must be
  produced from the same source files as the checksums to stay honest.

### A3 — Hybrid

* Good, because it survives a missing alias.
* Bad, because the fallback runs precisely when something is already wrong,
  and a rarely-exercised path in an installer is a path that does not work.
* Bad, because it carries both implementations' complexity permanently.

### B1 — Bootstrap only (chosen)

* Good, because it reuses tested Go for units, linger, and updates.
* Good, because the script stays small enough to audit before piping to `sh`
  — itself a security property.
* Bad, because failures inside `setup-service` surface through the script,
  so its error messages must stay legible when wrapped.

### B2 — Full shell installer

* Good, because it could run without the binary being executable (irrelevant
  here — the binary must run anyway).
* Bad, because it duplicates `setup-service` and `internal/update` in shell
  and guarantees drift.
* Bad, because unit templating, linger, and restart semantics would need
  independent testing across six environments.

### C1 — `linux/amd64` only

* Good, because it is zero work.
* Bad, because ARM hosts — Raspberry Pi, Graviton, Ampere, Oracle Cloud's
  free tier — get a 404 partway through an install.
* Bad, because the operator's own Oracle Cloud tier is ARM.

### C2 — Add `linux/arm64` (chosen)

* Good, because `CGO_ENABLED=0` pure-Go cross-compilation makes it a matrix
  entry.
* Good, because it doubles realistic Linux reach.
* Neutral, because it adds one more artifact pair per release.
* Bad, because it is built but not routinely exercised on real ARM hardware;
  the release job can only prove it links, not that it runs.

## More Information

### Verification status (as of 2026-08-17, v0.13.4)

Implemented and released. `scripts/install_test.sh` carries **57 assertions**,
green on the macOS workstation and in `ubuntu:26.04`, `oraclelinux:9`,
`rockylinux:9`, `debian:13`, and `alpine:3.22`. `shellcheck -s sh` is clean on
both scripts.

**Proven on real machines:**

| # | Case | Where | Outcome |
|---|---|---|---|
| 1 | Fresh install — unit created, enabled, lingering set | Lima VM, Ubuntu 26.04 **aarch64** | exit 0; daemon listening, self-signed cert generated first run |
| 2 | `linux/arm64` binary actually executes | same VM | `ELF ARM aarch64`, reports `0.13.3.1` |
| 3 | Upgrade, mcremote-only service | `wonder`, Ubuntu 26.04 amd64 | exit 0; existing unit preserved, service restarted |
| 4 | Upgrade, **both** daemons as services | `awsutility`, Ubuntu 26.04 amd64 | exit 0; both restarted; `/proc/<pid>/exe` confirmed no stale inode; relay re-registered 3 hosts |
| 5 | Idempotent re-run | VM + `awsutility` | exit 0; one unit; no temp dirs |
| 6 | `--uninstall` | VM | service stopped, process gone, binaries and unit removed |
| 7 | musl portability | `alpine:3.22` container | the *same* amd64 binary ran unmodified |
| 8 | runit backend | `alpine:3.22` + runit | run script created; reported supervision without claiming boot persistence |
| 9 | Release pipeline | v0.13.2–v0.13.4 | all six alias URLs 200; alias digests match the versioned manifest lines |

**Not yet verified.** Listed so the gap is visible rather than assumed. Rough
priority order:

| Case | Why it matters | Status |
|---|---|---|
| **WSL2 with systemd enabled** | Advertised as supported; whole `systemd-user` path unexercised there | untested |
| **WSL2 without systemd** | Advisory text and exit-0 behaviour never seen on a real WSL host | untested |
| **WSL1** | Rejection/advisory path | untested |
| **SELinux enforcing (Oracle 9 / Rocky 9)** | Containers do not exercise SELinux or a real user unit; open question 2 below | untested |
| **`--with-relay-service`** | Creating a relay service from scratch has never run; `awsutility` already had one | untested |
| **s6 backend** | Stub-tested only; never run against a real `s6-svscan` | untested |
| **`openrc-user` backend** | Stub-tested only; experimental upstream. Delete the backend rather than carry a half-working path if it fails | untested |
| **`openrc-system` / sysvinit messaging** | Message correctness on a real host | untested |
| **`MCREMOTE_VERSION` pin against a real release** | Only the URL shape is asserted in the harness | untested |
| **`wget` fallback with no `curl`** | Real branch for minimal images | untested |
| **Checksum failure on a real host** | Harness-verified only | untested |
| **arm64 on cloud/SBC hardware** | Proven under Apple Virtualization; Graviton / Ampere / Raspberry Pi unproven | partial |

### Live testing found what the suite could not

Three defects reached real hosts despite a green suite, and each was found by
the *first* run in a new environment:

1. **`wonder`** — an existing unit made `setup-service` refuse to overwrite;
   the installer had already stopped the daemon and left it down. Fixed by
   keeping the existing unit and restarting, plus a restore-on-failure path.
2. **`awsutility`** — only `mcremote` was cycled while *both* binaries were
   replaced, so `mcrelay` would have kept running old code on its old inode
   while reporting the new version. Fixed by tracking every active service.
3. **VM** — `--uninstall` deleted the binaries without stopping the daemons,
   a regression from fix 2 (`svc_stop_if_running` iterates a list that
   uninstall never populated).

The pattern is that all three were **pre-existing-state** bugs: the harness
starts from nothing, real hosts do not. That is the argument for working
through the untested rows above on real systems rather than treating a green
suite as sufficient.

### Reference implementations reviewed

**Ollama** (`ollama/scripts/install.sh`) is the closest analogue — a Go
daemon distributed by one-liner with service setup. Techniques adopted:
wrapping the whole script in a `main()` invoked on the final line, so a
**truncated download cannot execute a partial script**; `uname -m` → arch
mapping with an explicit error on unsupported values; `systemctl
is-system-running` to decide whether service setup is even possible; WSL2
detection via the `*icrosoft*WSL2` kernel-version string with WSL1 rejected
outright; version pinning through an environment variable; `trap`-based
temp-directory cleanup; and pre-validating required utilities before doing
any work. Deliberately **not** adopted: its system-wide install, dedicated
service user, `sudo` escalation, and GPU/driver branches — none apply to a
rootless user service.

**rustup** and **uv** informed the PATH handling: both write to shell profile
files when the install directory is not already on `PATH`. This record takes
the more conservative line of *reporting* rather than editing profiles, since
`~/.local/bin` is on `PATH` by default on most modern distributions and
silent profile edits are a common source of surprise.

### Notes and open items

* **`.local/bin` on `PATH`** is standard on Ubuntu/Debian via `~/.profile`
  and on systemd systems generally, but is not guaranteed — notably in
  minimal containers and some RHEL-family shells. Report, do not edit.
* **SELinux** is enforcing by default on Oracle Linux 9 and Rocky 9. A
  `systemd --user` unit running a binary from the user's home is normal and
  expected to work unlabelled, but this has not been verified on those hosts
  and should be part of acceptance.
* **`mcrelay`** is installed as a binary but gets **no** service by default,
  matching current practice on the operator's macOS host. A
  `--with-relay-service` flag covers the other case.
* **Hosting**: the installer ships as a **release asset**, fetched from
  `releases/latest/download/install.sh` — the same alias mechanism used for
  the binaries. This gives a permanent URL with no per-release documentation
  edit, and, more importantly, versions the script together with the assets
  it knows how to fetch. Branch hosting (`raw.githubusercontent.com/…/main/`)
  was rejected because it lets the script drift ahead of the published
  release: an asset-naming change on `main` breaks the public one-liner until
  the next release, and does so invisibly, since a local working tree is
  always self-consistent. Hotfixes remain possible without a new tag via
  `gh release upload <tag> scripts/install.sh --clobber`. Note that `latest`
  resolves to the newest **non-prerelease** release. Payload integrity comes
  from the checksum step regardless of where the script is hosted.
* **macOS** is out of scope here and needs its own record: the durable-TCC
  path requires `MC_CODESIGN_IDENTITY` (0069 D6), and clean installs on
  machines the operator does not own require paid Developer ID plus
  notarization with `--timestamp`. Note that `curl` does **not** set
  `com.apple.quarantine`, so terminal-installed binaries are not
  Gatekeeper-assessed today — the real macOS motivation is durable TCC grants
  for *other* users, not install-time prompts.
* **Windows** is out of scope entirely; WSL2 is covered as a Linux
  environment.

### Sources

* [GitHub REST API rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api) — 60 req/hr unauthenticated, per IP; 5,000/hr authenticated
* [REST API endpoints for release assets](https://docs.github.com/en/rest/releases/assets) — asset download redirects to a short-lived signed URL
* [Use systemd to manage Linux services with WSL](https://learn.microsoft.com/en-us/windows/wsl/systemd) — `[boot] systemd=true`, `wsl --shutdown`, version requirements
* [Advanced settings configuration in WSL](https://learn.microsoft.com/en-us/windows/wsl/wsl-config) — `wsl.conf` semantics
* [ollama/scripts/install.sh](https://github.com/ollama/ollama/blob/main/scripts/install.sh) — reference installer
* [The rustup book — Installation](https://rust-lang.github.io/rustup/installation/index.html) — one-liner and PATH conventions
* [Curl to shell isn't so bad](https://www.arp242.net/curl-to-sh.html) — threat-model discussion for piped installers
