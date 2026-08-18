# Implement the Linux `curl | sh` bootstrap installer and `linux/arm64` releases

Associated MADR: [0097-MADR-linux-curl-installer.md](0097-MADR-linux-curl-installer.md)

## Goal

A single copy-paste line installs verified `mcremote` and `mcrelay` binaries on
any mainstream Linux host, sets up service management where the host can
support it, and reports precisely what it achieved where it cannot — with no
root, no toolchain, and no GitHub API call in the default path.

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

## Scope

**In:** `linux/amd64` and `linux/arm64` release artifacts; unversioned alias
assets; `scripts/install.sh`; `scripts/install_test.sh`; init-system probing
with supported backends for systemd-user, runit, s6, and OpenRC user
services; README/docs updates.

**Out:** macOS (needs its own record — signing, TCC, notarization); Windows;
`linux/386`, `armv7`, `riscv64`; system-wide (root) installation; packaging
(deb/rpm/apk); the AppArmor userns remediation itself (needs `sudo`, so the
installer only detects and instructs).

## Prerequisites and locked decisions

Locked by the MADR; restated so this plan is executable without re-deriving them:

| Decision | Value |
|---|---|
| Asset discovery | Unversioned aliases + `releases/latest/download/…`; **no API call** in the default path |
| Checksum verification | **By hash value, not filename** (`SHA256SUMS` lists versioned names only) |
| Installer scope | Bootstrap only; `mcremote update` owns all later upgrades |
| Install dir | `~/.local/bin` (matches `Makefile` `USER_BIN_DIR`) |
| Privilege | None. The script must never invoke `sudo` |
| Shell dialect | POSIX `sh` (Alpine's `/bin/sh` is busybox `ash`) — no bashisms |
| Architectures | `amd64`, `arm64` |
| Relay service | Binary installed; **no** service unless `--with-relay-service` |

---

## Phase 1 — Release pipeline: add `linux/arm64` and alias assets

### 1.1 Add the architecture

`.github/workflows/ci.yml`, the `PLATFORMS` block (currently lines 161–163):

```diff
           PLATFORMS="$PLATFORMS linux/amd64"
+          PLATFORMS="$PLATFORMS linux/arm64"
           PLATFORMS="$PLATFORMS darwin/arm64"
           PLATFORMS="$PLATFORMS darwin/amd64"
```

No other build change is required: `Makefile:38` already sets
`CGO_ENABLED ?= 0` and the Linux tag set `netgo,osusergo` is selected by
`GOOS`, so `GOARCH=arm64` cross-compiles with the existing recipe.

**Verify locally before touching CI** (proves the toolchain, not the workflow):

```sh
make build GOOS=linux GOARCH=arm64
file bin/mcremote    # must report: ELF 64-bit LSB executable, ARM aarch64, statically linked
```

### 1.2 Generate alias copies from the checksummed files

The aliases must be produced from **the same files** the checksum step
hashes, in the same job, after `SHA256SUMS` is written. Generating them
elsewhere would allow an alias and its versioned original to diverge.

In the release job, after the existing `SHA256SUMS-<VER>` generation and
before upload:

```sh
# Unversioned aliases so the installer can build a URL without knowing VER.
# Copies, not symlinks: GitHub release assets do not preserve symlinks.
for f in dist/mcremote-linux-* dist/mcrelay-linux-*; do
  case "$f" in *SHA256SUMS*) continue ;; esac
  base=$(basename "$f")
  # strip the trailing -<VER>: mcremote-linux-arm64-0.12.0.1 -> mcremote-linux-arm64
  alias_name=$(printf '%s\n' "$base" | sed -E 's/-[0-9]+\.[0-9]+\.[0-9]+(\.[0-9]+)?$//')
  [ "$alias_name" != "$base" ] || { echo "error: could not derive alias from $base" >&2; exit 1; }
  cp -f "$f" "dist/$alias_name"
done
cp -f "dist/SHA256SUMS-$VER" dist/SHA256SUMS
```

Then extend the release upload glob so `dist/mcremote-linux-*`,
`dist/mcrelay-linux-*`, and `dist/SHA256SUMS` are all attached.

**Deliberately:** aliases are Linux-only. macOS is out of scope, and adding
darwin aliases would imply installer support that does not exist.

**Deliberately:** `SHA256SUMS` (alias) is a byte copy of `SHA256SUMS-<VER>`
and therefore still lists **versioned** filenames. That is correct and
required — it is what lets the installer discover the resolved version. Do
not "fix" it by rewriting the names to aliases.

### 1.2a Attach the installer itself as a release asset

The one-liner points at a release asset, not at a branch:

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

Add to the same upload step:

```sh
cp -f scripts/install.sh dist/install.sh
```

This is not merely about a stable URL. Serving from
`raw.githubusercontent.com/…/main/…` allows the script to **drift ahead of
the released assets**: change the asset naming convention in CI and the
script on `main` immediately expects names the current `latest` release does
not carry, breaking the one-liner for everyone until the next release — a
breakage invisible in local testing, because the working tree is consistent
with itself. Shipping the script as a release asset makes script and assets
version together by construction: the script attached to release N is exactly
the script that knows how to fetch release N.

Two operational notes:

* **Hotfixes do not require a new tag.** Replace the asset in place with
  `gh release upload <tag> scripts/install.sh --clobber`. This preserves the
  only real advantage branch-hosting had.
* **`latest` resolves to the newest non-prerelease.** Marking a release as a
  prerelease leaves the installer URL on the previous stable release —
  usually the desired behaviour, but it must be known rather than discovered.

Because the script is fetched from the same release as the binaries, it
should default to `latest/download/` for assets (not a hardcoded tag), so a
copy fetched today keeps working against the release it came from.

### 1.3 Acceptance for Phase 1

Cut a release, then:

```sh
R=https://github.com/maccavelli/magic-cli-remote/releases/latest/download
for a in mcremote-linux-amd64 mcremote-linux-arm64 mcrelay-linux-amd64 mcrelay-linux-arm64 SHA256SUMS install.sh; do
  printf '%-28s %s\n' "$a" "$(curl -sIL -o /dev/null -w '%{http_code}' "$R/$a")"
done   # all must print 200
```

And prove alias/original identity:

```sh
curl -fsSL "$R/mcremote-linux-amd64" | sha256sum
grep 'mcremote-linux-amd64-' <(curl -fsSL "$R/SHA256SUMS")   # digests must match
```

---

## Phase 2 — Deterministic platform probe

The script must classify the host with no ambiguity and no network access.
This phase defines the contract; Phase 3 implements it.

### 2.1 OS and architecture

```sh
uname -s   # must be "Linux"; anything else → exit 1 with the macOS/Windows note
uname -m   # map, then fail closed
```

| `uname -m` | `ARCH` |
|---|---|
| `x86_64`, `amd64` | `amd64` |
| `aarch64`, `arm64` | `arm64` |
| anything else | **exit 1**: `unsupported architecture <val>; only amd64 and arm64 are published` |

Explicitly reject `armv6l`/`armv7l` by name with "32-bit ARM is not
published" so Raspberry Pi OS 32-bit gets a real answer rather than a 404.

### 2.2 Environment classification

Evaluated in this order; first match wins:

| Probe | Test | Classification |
|---|---|---|
| WSL1 | `/proc/sys/kernel/osrelease` contains `Microsoft` (capital M, no `WSL2`) | `wsl1` |
| WSL2 | `/proc/sys/kernel/osrelease` contains `WSL2` or `microsoft` | `wsl2` |
| Container | `/.dockerenv` or `/run/.containerenv` exists, or `/proc/1/cgroup` matches `docker\|lxc\|kubepods` | `container` |
| Otherwise | — | `native` |

### 2.3 Init/supervisor probe

This is the part MADR 0097 previously hand-waved. Evaluated in order; first
match wins. `INIT` is the resulting backend id.

| Order | Probe (exact) | `INIT` |
|---|---|---|
| 1 | `command -v systemctl` **and** `systemctl --user is-system-running` exits 0/1 (running or degraded) **and** `${XDG_RUNTIME_DIR:-}` is set and writable | `systemd-user` |
| 2 | `command -v systemctl` succeeds but check 1 fails | `systemd-broken` |
| 3 | `command -v runsvdir` **and** `command -v sv` | `runit` |
| 4 | `command -v s6-svscan` **and** `command -v s6-svc` | `s6` |
| 5 | `command -v rc-service` **and** `rc-service --user --help` exits 0 | `openrc-user` |
| 6 | `command -v rc-service` (no `--user`) | `openrc-system` |
| 7 | none of the above | `none` |

Record `/proc/1/comm` verbatim in verbose output for diagnosis. Do **not**
branch on it: it reports the *system* init, which says nothing about whether
a rootless user manager is usable — the distinction that actually matters here.

`systemd-broken` is a separate class on purpose. Its most common cause is
entering the session via `su`, which bypasses `pam_systemd` and leaves
`XDG_RUNTIME_DIR` unset, so every `systemctl --user` call fails with an
opaque D-Bus error. The script must name that cause rather than surface the
raw failure.

---

## Phase 3 — `scripts/install.sh`

### 3.1 Structural requirements

1. **Whole body inside `main()`, invoked on the final line.** A truncated
   download must never execute a partial script. Last line is exactly
   `main "$@"`.
2. `#!/bin/sh` and `set -eu`. No `set -o pipefail` (not POSIX). No arrays, no
   `[[`, no `local` outside functions that declare it portably.
3. `trap 'rm -rf "$TMPDIR_MC"' EXIT INT TERM` with
   `TMPDIR_MC=$(mktemp -d)` — never a predictable `/tmp` path.
4. Every message to stderr; only machine-readable output (if any) to stdout.
5. Exit codes: `0` success, `1` usage/unsupported, `2` download/verify
   failure, `3` service-setup failure after a successful binary install.

### 3.2 Flags and environment

| Flag | Env | Default | Effect |
|---|---|---|---|
| `--version X.Y.Z` | `MCREMOTE_VERSION` | latest | Pin to `releases/download/vX.Y.Z/` |
| `--dir PATH` | `MCREMOTE_INSTALL_DIR` | `~/.local/bin` | Install location |
| `--no-service` | `MCREMOTE_NO_SERVICE=1` | unset | Install binaries only |
| `--with-relay-service` | — | unset | Also set up `mcrelay` |
| `--no-linger` | — | unset | Pass through to `setup-service` |
| `--dry-run` | — | unset | Print every action; touch nothing |
| `--verbose` | — | unset | Probe results and URLs |
| `--uninstall` | — | unset | Stop service, remove unit, remove binaries |

Piped invocation cannot take arguments after `| sh`, so **every flag must
have an environment-variable equivalent**, and the documented form for piped
use is `curl … | MCREMOTE_NO_SERVICE=1 sh`.

### 3.3 Preflight

```sh
need curl OR wget          # prefer curl -fsSL; fall back to wget -qO-
need sha256sum OR shasum OR openssl
need uname mkdir mv chmod grep sed
```

Fail with the full list of what is missing, not the first one. On Alpine
minimal, `curl` is frequently absent while `wget` (busybox) is present — the
`wget` path must therefore be a real, tested branch, not a courtesy.

### 3.4 Resolve, download, verify (the exact algorithm)

```
BASE = https://github.com/maccavelli/magic-cli-remote/releases
if PIN:  URL_DIR = $BASE/download/v$PIN
else:    URL_DIR = $BASE/latest/download

fetch $URL_DIR/SHA256SUMS            -> $TMP/SHA256SUMS
fetch $URL_DIR/mcremote-linux-$ARCH  -> $TMP/mcremote
fetch $URL_DIR/mcrelay-linux-$ARCH   -> $TMP/mcrelay

for each product:
  line = grep -E "  ${product}-linux-${ARCH}-[0-9]" $TMP/SHA256SUMS | head -1
  [ -n "$line" ]                      || fail 2 "no checksum entry for ${product}-linux-${ARCH}"
  want = field 1 of $line
  got  = sha256_of($TMP/$product)
  [ "$want" = "$got" ]                || fail 2 "checksum mismatch for $product"
  RESOLVED_VER = field 2 of $line, with "${product}-linux-${ARCH}-" stripped
```

Notes that make this deterministic:

* Match on the **versioned** name pattern with a trailing `-[0-9]`, so the
  alias line (if one is ever added) cannot shadow the versioned entry.
* Compare digests **as strings**. Do not use `sha256sum -c`: the manifest
  names do not match the alias filenames on disk, and `-c` would fail on the
  name. This is the defect verified in the MADR.
* `sha256_of` selects `sha256sum` → `shasum -a 256` → `openssl dgst -sha256`,
  normalising each to the bare hex digest (lowercase, whitespace stripped).
* `RESOLVED_VER` is printed and later cross-checked against
  `mcremote version`; a mismatch means the alias and manifest disagree and is
  a hard failure.

### 3.5 Install atomically and upgrade-safely

```sh
mkdir -p "$INSTALL_DIR"
chmod 0755 "$TMP/mcremote" "$TMP/mcrelay"

# If a service is currently running the old binary, stop it before replacing.
service_stop_if_running   # backend-aware; no-op when INIT=none

# Atomic within the same filesystem; never write directly to the live path.
mv -f "$TMP/mcremote" "$INSTALL_DIR/mcremote"
mv -f "$TMP/mcrelay"  "$INSTALL_DIR/mcrelay"
```

`mv` across filesystems is not atomic, so `TMPDIR_MC` is created **inside
`$INSTALL_DIR`** (`mktemp -d "$INSTALL_DIR/.mcinstall.XXXXXX"`) rather than
`/tmp`, guaranteeing a same-filesystem rename. The `EXIT` trap removes it.

Then verify what landed:

```sh
"$INSTALL_DIR/mcremote" version    # must contain $RESOLVED_VER
```

**PATH:** if `$INSTALL_DIR` is not in `$PATH`, print the exact `export` line
for the user's shell. Do **not** edit profile files — `~/.local/bin` is on
`PATH` by default on most modern distributions, and silent profile edits are
a common source of surprise.

### 3.6 Service dispatch

Dispatch on `$INIT` from §2.3. Each backend is specified in Phase 4. In all
cases the binary install has already succeeded, so a service failure exits
`3`, never `1` — "installed but not supervised" must be distinguishable from
"nothing happened".

### 3.7 Post-install advisories

* **AppArmor userns** (`native`/`wsl2` only): if
  `/proc/sys/kernel/apparmor_restrict_unprivileged_userns` reads `1`, print
  that agent-CLI sandboxing (codex/bubblewrap) will fail and point at
  `scripts/bwrap-apparmor-fix.sh` and MADR 0048. Needs `sudo`, so it is
  reported and never executed.
* **WSL2 without systemd**: print the `/etc/wsl.conf` `[boot] systemd=true`
  remedy plus `wsl.exe --shutdown`, per Microsoft's documentation.
* **`systemd-broken`**: explain the `su`/`pam_systemd`/`XDG_RUNTIME_DIR`
  cause and suggest reconnecting with `ssh` or `machinectl shell`.

---

## Phase 4 — Service backends, including systems without systemd

### 4.0 The constraint this phase must not paper over

For a user service to start **at boot**, something running as root at boot
must start it. systemd provides exactly that via `loginctl enable-linger`.
runit and s6 supervise rootlessly and restart-on-crash correctly, but their
`runsvdir`/`s6-svscan` must itself be started, and starting *that* at boot
requires one root action. OpenRC's user services are experimental
(OpenRC 0.60+) and its own documentation records that OpenRC "always requires
admin privileges to interact with a daemon".

Therefore every backend below is classified by the two capabilities it can
actually deliver, and the script must print which it achieved:

* **supervised** — restarted on crash, runs after the invoking shell exits
* **boot-persistent** — starts automatically after reboot

Never claim boot persistence that was not configured.

### 4.A `systemd-user` — full support (delegate)

```sh
"$INSTALL_DIR/mcremote" setup-service ${NO_LINGER:+--no-linger}
[ -n "$WITH_RELAY" ] && "$INSTALL_DIR/mcrelay" setup-service ${NO_LINGER:+--no-linger}
```

Delegates to existing, tested Go (`internal/cli/service`), which writes the
unit, runs `daemon-reload`, `enable --now`, and `loginctl enable-linger`.
Capabilities: **supervised + boot-persistent**.

Verify:

```sh
systemctl --user is-active mcremote                       # active
loginctl show-user "$USER" --property=Linger              # Linger=yes
```

`service_stop_if_running` = `systemctl --user stop mcremote 2>/dev/null || true`.

### 4.B `runit` — supervised, boot persistence needs one root step

Generate a service directory in the user's own scan directory. The layout is
minimal and stable: a directory containing an executable `run` script.

```sh
SVDIR_USER="${XDG_DATA_HOME:-$HOME/.local/share}/runit/service"
mkdir -p "$SVDIR_USER/mcremote"
cat > "$SVDIR_USER/mcremote/run" <<EOF
#!/bin/sh
exec "$INSTALL_DIR/mcremote" serve 2>&1
EOF
chmod 0755 "$SVDIR_USER/mcremote/run"
```

If a user `runsvdir` is already supervising that directory, the service
starts within seconds and no further action is needed. Detect with:

```sh
pgrep -u "$(id -u)" -f "runsvdir .*$SVDIR_USER" >/dev/null 2>&1
```

If not running, start it for this session and print the boot instruction:

```sh
runsvdir "$SVDIR_USER" >/dev/null 2>&1 &
```

Control commands to report: `SVDIR=$SVDIR_USER sv status|up|down mcremote`.
Capabilities: **supervised**; boot-persistent only if the operator arranges
for `runsvdir` to start at boot (root, one time).
`service_stop_if_running` = `SVDIR=$SVDIR_USER sv down mcremote || true`.

### 4.C `s6` — supervised, same persistence caveat

Structurally identical to runit; s6 reads the same "directory containing an
executable `run`" shape.

```sh
S6DIR="${XDG_DATA_HOME:-$HOME/.local/share}/s6/service"
mkdir -p "$S6DIR/mcremote"
printf '#!/bin/sh\nexec "%s/mcremote" serve 2>&1\n' "$INSTALL_DIR" > "$S6DIR/mcremote/run"
chmod 0755 "$S6DIR/mcremote/run"
```

Start under an existing user `s6-svscan` if one is supervising `$S6DIR`;
otherwise start `s6-svscan "$S6DIR" &` for the session and print the boot
instruction. Control: `s6-svc -u|-d "$S6DIR/mcremote"`.
Capabilities: **supervised**.

### 4.D `openrc-user` — best effort, probed not assumed

Only entered when `rc-service --user --help` exits 0, because native user
services are experimental and version-dependent. Write the service script to
the user config root OpenRC uses (`${XDG_CONFIG_HOME:-$HOME/.config}/rc`),
then attempt enable/start and **treat failure as non-fatal**:

```sh
RCDIR="${XDG_CONFIG_HOME:-$HOME/.config}/rc"
mkdir -p "$RCDIR/init.d"
cat > "$RCDIR/init.d/mcremote" <<EOF
#!/sbin/openrc-run
name="mcremote"
command="$INSTALL_DIR/mcremote"
command_args="serve"
command_background=true
pidfile="\${XDG_RUNTIME_DIR:-/tmp}/mcremote.pid"
EOF
chmod 0755 "$RCDIR/init.d/mcremote"
rc-service --user mcremote start || warn "OpenRC user service start failed; run mcremote serve manually"
```

Because this path is experimental upstream, acceptance is "does not break the
install", not "must work". If it fails, fall through to the 4.F messaging.
Capabilities: **supervised** where it works; boot persistence unverified.

### 4.E `openrc-system`, `sysvinit` — no rootless option

Do not write to `/etc/init.d`; that requires root and the installer must not
escalate. Install binaries, print the foreground command and a ready-to-use
init script path the operator can install themselves with `sudo`.
Capabilities: **neither**.

### 4.F `none`, `systemd-broken`, `wsl1`, `container` — honest exit

Install binaries, exit **0**, and print exactly what was and was not done:

```text
Installed:   ~/.local/bin/mcremote 0.12.0.1
             ~/.local/bin/mcrelay  0.12.0.1
Service:     not configured (no supported service manager detected)
Run it now:  ~/.local/bin/mcremote serve
Background:  nohup ~/.local/bin/mcremote serve >~/.local/state/mcremote.log 2>&1 &
```

Exit 0 is deliberate: on Alpine, WSL1 and containers, "binaries installed, no
supervisor available" is a correct and complete outcome, not an error. The
distinction lives in the report, not the exit code.

### 4.G Backend capability summary (must match the script's output strings)

**Reported capability is measured, not assumed** (MADR 0099 F5). Since v0.13.5
the summary reflects the state the backend's own liveness probe reported, not
merely that the setup command exited 0. Three outcomes are possible for any
backend in the table below:

| Reported | Meaning | Exit |
|---|---|---|
| the row's capability | the daemon was confirmed running | 0 |
| `starting` | not yet active when the window closed — slow, not necessarily broken | **0** |
| `failed` | confirmed not running (dead, or restart-looping), with the backend's diagnostic | **3** |

`starting` exits 0 deliberately: reporting a loaded-but-healthy host as broken
would be a defect in the opposite direction.


| `INIT` | Supervised | Boot-persistent | Root needed for full function |
|---|---|---|---|
| `systemd-user` | yes | yes (linger) | no |
| `runit` | yes | only if `runsvdir` starts at boot | yes, once |
| `s6` | yes | only if `s6-svscan` starts at boot | yes, once |
| `openrc-user` | best effort | unverified | unknown |
| `openrc-system` / `sysvinit` | no | no | yes |
| `none` / `systemd-broken` / `wsl1` / `container` | no | no | n/a |

---

## Phase 5 — `scripts/install_test.sh`

Sits beside the existing `scripts/install-binary_test.sh` and follows its
conventions. The script must be structured so every unit below is testable
without network access: put resolution, verification, and probing in
functions, and allow `MC_TEST_BASE_URL` to point at a local directory.

Required cases:

| # | Case | Expectation |
|---|---|---|
| 1 | `uname -m` = `x86_64`, `amd64`, `aarch64`, `arm64` | maps to `amd64`/`arm64` |
| 2 | `uname -m` = `armv7l`, `i686`, `riscv64` | exit 1, message names the arch |
| 3 | `uname -s` = `Darwin` | exit 1, points at the macOS note |
| 4 | Manifest lacks the product line | exit 2, no file installed |
| 5 | Digest mismatch (flip one byte) | exit 2, **destination unchanged** |
| 6 | Valid digest | installs, `RESOLVED_VER` extracted correctly |
| 7 | Alias line present in manifest | versioned line still selected |
| 8 | `systemctl` absent | `INIT=none`, exit 0, service-not-configured message |
| 9 | `systemctl` present, `XDG_RUNTIME_DIR` unset | `INIT=systemd-broken`, `su` cause explained |
| 10 | `runsvdir`+`sv` present, no systemd | `INIT=runit`, run script created and executable |
| 11 | Pinned `--version` | URL uses `download/vX.Y.Z`, not `latest` |
| 12 | `--dry-run` | no file created anywhere |
| 13 | Re-run over an existing install | idempotent; no duplicate unit; binary replaced |
| 14 | Truncated script | `main` never invoked (assert by grepping last line) |

Case 5 is the most important: a failed verification must leave the previous
installation untouched.

**Container matrix** (manual or CI, `docker run --rm -v $PWD:/w -w /w <img> sh install_test.sh`):
`ubuntu:26.04`, `oraclelinux:9`, `rockylinux:9`, `debian:13`, `alpine:3.22`.
Note that containers report `INIT=container`, so they exercise the install and
verify paths, not the systemd path — systemd coverage requires a VM or a
`systemd`-enabled container and is validated on the operator's own hosts.

---

## Phase 6 — Documentation

1. **README**: install section leading with the one-liner, the
   environment-variable form for piped flags, and a link to the manual
   `make install` path for developers.
2. **`docs/ops-linux-install.md`** (new): the backend capability table from
   §4.G, the AppArmor advisory, the WSL2 systemd enablement steps, the `su`/
   `XDG_RUNTIME_DIR` trap, and uninstall.
3. Cross-link MADR 0048 (AppArmor) and 0065 (`mcremote update` owns upgrades,
   so the installer is run once per host).

---

## Verification

Per-host acceptance. Every row must be executed before this plan is
considered complete; record actual output, not expectations.

Phases 1–6 are **implemented and released** (v0.13.4). What follows is the
running record of which acceptance rows have actually been executed on real
systems, kept current so the untested surface stays visible.

Status as of **2026-08-18 (v0.13.4)**, after the MADR 0098 ephemeral-cloud
sweep. ✅ passed · ⚠️ passed but exposed a defect · ❌ failed · 🚫 blocked.

**All 12 previously-outstanding rows have now been executed on real hosts**
(one blocked, recorded as such). The sweep found **seven findings, two of them
HIGH** — every one of them in a row this table had marked untested, which is
the strongest possible argument for having run it. Full evidence and write-ups:
[0098-MADR](0098-MADR-ephemeral-cloud-install-verification.md) ·
[0098-PLAN](0098-PLAN-ephemeral-cloud-install-verification.md).

**All defects are now fixed and re-verified** — see
[MADR 0099](0099-MADR-installer-service-state-verification.md) and
[0099-findings-reverification.md](0099-findings-reverification.md). Fixed in
v0.13.5, except F5 which needed a second pass in **v0.13.6** after the
re-verification sweep found the first gate could still be fooled by a
`Type=simple` unit that is `active` for an instant before dying. **F8** was
found afterwards while closing the last blocked row, and is fixed on `master`
awaiting a release.

Defects found, and where they were fixed:

| Ref | Severity | Summary |
|---|---|---|
| F1 | **HIGH** | `svc_is_active` uses `s6-svc -l` (not a valid option) → uninstall/upgrade never stop an s6-supervised daemon; it survives on a `(deleted)` inode |
| F4 | **HIGH** | `--with-relay-service` produces a unit that can never start — `218/CAPABILITIES` from `PrivateDevices`+`RestrictNamespaces`, then a config the binary refuses |
| F5 | MEDIUM | Installer reports `supervised+boot` without verifying the unit reached `active`; observed twice from unrelated causes |
| F6 | MEDIUM | WSL hosts classify `systemd-broken` and get the irrelevant `su`/`pam_systemd` advisory, whose stated cause is factually false there |
| F2 | — | `openrc-user` unreachable on stock Alpine (no elogind → no `XDG_RUNTIME_DIR`); backend is correct, keep it |

| # | Host / case | Expect `INIT` | Result |
|---|---|---|---|
| ✅ | Lima VM, Ubuntu 26.04 **aarch64**, clean host — *fresh install* | `systemd-user` | exit 0; unit created, enabled, `Linger=yes`; daemon listening, first-run self-signed cert |
| ✅ | arm64 binary executes | — | `ELF ARM aarch64`, reports `0.13.3.1` |
| ✅ | `wonder`, Ubuntu 26.04 amd64 — *upgrade, mcremote only* | `systemd-user` | exit 0; existing unit preserved; AppArmor advisory printed |
| ✅ | `awsutility`, Ubuntu 26.04 amd64 — *upgrade, both daemons* | `systemd-user` | exit 0; both restarted; `/proc/<pid>/exe` shows no `(deleted)`; relay re-registered 3 hosts |
| ✅ | Idempotent re-run | `systemd-user` | one unit; no temp dirs; exit 0 |
| ✅ | `--uninstall` | `systemd-user` | service stopped, process gone, binaries + unit removed |
| ✅ | `alpine:3.22` container — musl | `container` | same amd64 binary runs unmodified; exit 0 |
| ✅ | Alpine + runit | `runit` | run script created and executable; reports supervision, not boot persistence |
| ✅ | `ubuntu:26.04` container | `container` | exit 0; binaries execute |
| ✅ | Test suite on oraclelinux:9 / rockylinux:9 / debian:13 | — | 57/57 (suite only — **not** a real systemd install) |
| ✅ | **WSL2, systemd enabled** — WS2025 + `m8i.xlarge` nested virt, WSL 2.7.11 | `systemd-user` | `env=wsl2 init=systemd-user`; `supervised+boot`; `is-active`=active, `Linger=yes`, daemon PID 321 **inside** WSL |
| ✅ | **WSL2, systemd disabled** (`[boot] systemd=false`) | `systemd-broken` (not `none`) | **Fixed in v0.13.5, re-verified on WSL 2.7.11.** Only the wsl.conf remedy prints; `grep -c pam_systemd` = 0. Expectation column corrected: `detect_init` returns on `have systemctl`, which Ubuntu-on-WSL always ships |
| ✅ | **WSL1** | `systemd-broken` (not `none`) | `env=wsl1` matched from osrelease `4.4.0-26100-Microsoft`; only the "upgrade to WSL2" advisory prints; exit 0. **Fixed in v0.13.5, re-verified** |
| ✅ | **Rocky 9.8 as a real host** (OL9 substituted, see MADR 0098) | `systemd-user` | `getenforce`=**Enforcing**; `ausearch -m avc -ts recent` → **`<no matches>`**; dmesg clean; context `unconfined_u:object_r:gconf_home_t:s0`; unit active and **listening on 127.0.0.1:7531**. **Open question 1 retired** — `restorecon` not required |
| ✅ | **`--with-relay-service`** on a virgin host | `systemd-user` | **Was wholly broken in v0.13.4** (`218/CAPABILITIES`, then a config the daemon refuses). **Fixed in v0.13.5 and re-verified:** both units `active`, mcrelay `NRestarts=0 Result=success`, `/healthz` → `{"ok":true}` on `127.0.0.1:8443`, zero CAPABILITIES faults |
| ✅ | **s6 backend on a real `s6-svscan`** (Alpine 3.23.5) | `s6` | Detection, run script and supervision correct. The `s6-svc -l` defect (probe always false → orphaned daemon on a `(deleted)` inode) is **fixed in v0.13.5 and re-verified**: daemon confirmed live via `/proc/<pid>/exe`, then gone after `--uninstall` |
| ✅ | **`openrc-user` on real OpenRC 0.63** (Alpine 3.23.5) | `openrc-system` | **Do not delete the backend.** OpenRC 0.63 has `-U, --user`; the probe fails only because stock Alpine ships no elogind so `XDG_RUNTIME_DIR` is unset (`exit 1` unset → `exit 0` when set). Falling through to `openrc-system` is correct. **Open question 2 answered** — 0098 F2 |
| ✅ | **`openrc-system`** (Alpine) / **non-systemd PID 1** | `openrc-system` · `systemd-broken` | `openrc-system` passes on Alpine: `SERVICE_RESULT=none`, exit **0**, `nohup` line per §4.F. The sysvinit half was initially **blocked** (Debian converted, but every SSH session hung) and is now **closed without a VM**: `detect_init` never branches on PID 1, so any non-systemd PID 1 takes an identical path — reproduced with `unshare -pf --mount-proc`, giving `init=systemd-broken (pid1=sh)`. That exposed **F8** (the `su` advisory firing on hosts that never used `su`), now fixed |
| ✅ | **`MCREMOTE_VERSION` pin against real releases** | — | `--version 0.13.3` → `releases/download/v0.13.3`, installs `0.13.3.1` (a downgrade). `--version 0.12.0` (pre-alias) → exit **2**, 404 on `SHA256SUMS`, correct "releases before MADR 0097" guidance, **existing install untouched** |
| ✅ | **`wget` fallback, no `curl`** (Alpine amd64 + aarch64) | — | busybox `wget -qO- \| sh` followed GitHub's redirect to `objects.githubusercontent.com` over TLS; SHA-256 verified; exit 0; `0.13.4.1` |
| ✅ | **Checksum failure on a real host, over real HTTPS** | — | S3 mirror, one byte flipped, run against a host with a **working install**: exit **2**, both digests printed, `Nothing was installed.`, binary digest **unchanged**, service still active, no `.mcinstall.*` residue. Control with clean mirror: exit 0 |
| ✅ | **arm64 on cloud hardware** — Graviton3 `c7g.medium` + `t4g.small` | `systemd-user` | Ubuntu 26.04 arm64: `supervised+boot`, `Linger=yes`, `ELF 64-bit LSB executable, ARM aarch64, statically linked`. Upgrade left **no `(deleted)` inode** (contrast with s6). Alpine aarch64: static-on-musl confirmed |

**Do not treat a green suite as sufficient.** Every one of the three real
environments tested so far surfaced a defect the 57-assertion suite missed,
and all three were **pre-existing-state** bugs — an already-installed unit, an
already-running second daemon, an already-running daemon at uninstall. The
harness always starts from nothing; real hosts do not. Expect the outstanding
rows to behave the same way, and run them on real systems.

Cross-cutting checks on every host:

```sh
mcremote version                 # equals RESOLVED_VER printed by the installer
sh -n scripts/install.sh         # POSIX parse check
shellcheck -s sh scripts/install.sh
tail -1 scripts/install.sh       # exactly: main "$@"
```

Run `shellcheck -s sh` (not `-s bash`) so bashisms are caught rather than
tolerated — the Alpine path depends on it.

---

## Rollout and Rollback

**Rollout order is fixed** and each step is independently revertible:

1. Phase 1 (arm64 + aliases) and cut a release. This is additive: existing
   versioned assets and `internal/update` are untouched, so a bad alias step
   cannot break `mcremote update`.
2. Verify Phase 1 acceptance (§1.3) against the real release **before**
   writing the installer. The installer's core assumption is that the alias
   URLs resolve; proving that first avoids debugging two layers at once.
3. Land `install.sh` + tests, unadvertised.
4. Verify the matrix above.
5. Only then add the one-liner to the README — that line is the public
   commitment, and it should be made last. *(Done in v0.13.3.)* Note the
   ordering dependency:
   the one-liner cannot work until a release has been cut that carries
   `install.sh` as an asset, so step 3 must precede a release, and that
   release must precede step 5.

**Rollback:**

* Installer: remove the README line; the script is inert if nobody runs it.
* Aliases: stop attaching them. `internal/update` never referenced them, so
  nothing regresses; only already-published aliases remain, harmlessly.
* `linux/arm64`: remove the matrix line. No consumer depends on it until the
  installer advertises it.
* A user's install: `install.sh --uninstall`, or manually
  `systemctl --user disable --now mcremote`, remove
  `~/.config/systemd/user/mcremote.service`, delete the two binaries.

**Irreversible boundary:** none. Nothing here rewrites published artifacts,
moves tags, or requires root on any host.

---

## Open questions

1. **SELinux on Oracle Linux 9 / Rocky 9** — a `systemd --user` unit running
   an unlabelled binary from `$HOME` is expected to work, but this is
   assumption, not measurement. It is called out as an acceptance row rather
   than asserted as fact; if it fails, the remedy (`restorecon`, or a
   `~/.local/bin` file context) belongs in `ops-linux-install.md`.
2. **OpenRC user services** — behaviour varies by OpenRC version and the
   feature is upstream-experimental. Phase 4.D is written as probe-and-degrade
   for that reason. If it proves unreliable on Alpine 3.22, delete the
   backend rather than carrying a path that half-works.
3. **First-run configuration** — this plan installs and starts the daemon; it
   does not pair a device or write config. Whether the installer should print
   the pairing next step, or invoke `mcremote pair`, is unresolved and
   deliberately out of scope here.
4. ~~**Script hosting**~~ — **Resolved: the installer ships as a release
   asset.** See §1.2a.
