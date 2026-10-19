# Linux install runbook (`install.sh`)

<!-- markdownlint-disable MD013 -->

Decision record: [MADR 0097](../decisions/0097-MADR-linux-curl-installer.md); implementation
plan: [0097-PLAN](../decisions/0097-PLAN-linux-curl-installer.md).

```bash
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

The installer is a **bootstrap**, not a package manager. It places two verified
binaries and hands off to `mcremote setup-service`. Upgrades are
`mcremote update` ([MADR 0065](../decisions/0065-MADR-update-automation.md),
[0100](../decisions/0100-MADR-update-unit-refresh-and-daemon-reload.md) — see
[Updating](#updating)), so this script is run once per host.

What it guarantees:

- **No root.** Everything lands under `$HOME`; the script never calls `sudo`.
- **Verified.** Each binary's SHA-256 is compared against the published
  `SHA256SUMS` *before* anything is installed. A mismatch leaves an existing
  installation untouched.
- **No GitHub API call**, so the 60-requests/hour per-IP anonymous limit
  cannot break an install behind shared egress.
- **Atomic.** Downloads are staged inside the install directory and renamed
  into place, so a partial download can never become a live binary.

## Options

| Flag | Environment variable | Default |
|---|---|---|
| `--version X.Y.Z` | `MCREMOTE_VERSION` | latest release |
| `--dir PATH` | `MCREMOTE_INSTALL_DIR` | `~/.local/bin` |
| `--no-service` | `MCREMOTE_NO_SERVICE=1` | unset |
| `--with-relay-service` | — | unset (relay binary installed, no service) |
| `--no-linger` | — | unset |
| `--dry-run`, `--verbose`, `--uninstall`, `--help` | — | — |

Piped invocation cannot pass flags, so use the environment form:

```bash
curl -fsSL <url>/install.sh | MCREMOTE_NO_SERVICE=1 sh
```

Exit codes: `0` success · `1` usage/unsupported platform · `2` download or
checksum failure (nothing installed) · `3` binaries installed but the service
did not start.

**What the service line means.** The installer waits for the service to settle
and reports what it observed, never what it merely requested:

| Line | Meaning | Exit |
|---|---|---|
| `running, and enabled at boot` / `supervised…` | confirmed running | 0 |
| `starting — not yet confirmed running` | still coming up when the wait window closed; check with the printed command | 0 |
| `FAILED to start` | confirmed not running; the backend's own diagnostic is printed beneath it | 3 |

## Supported platforms

`linux/amd64` and `linux/arm64`. The binaries are built with `CGO_ENABLED=0`
and `-tags netgo,osusergo`, so they are **fully static** — one artifact per
architecture runs on glibc (Ubuntu, Oracle, Rocky, Debian) and musl (Alpine)
alike. 32-bit ARM, i686 and riscv64 are not published and are rejected by name.

The same `install.sh` also installs on Apple Silicon macOS (`darwin/arm64`).
Intel Macs are not supported; see
[MADR 0120](../decisions/0120-MADR-retire-the-darwin-amd64-target.md). The service there is
a launchd LaunchAgent, not systemd; Full Disk Access is a separate grant, see
[ops-macos-tcc.md](ops-macos-tcc.md).

## Service backends

The installer probes for a *rootless* service manager. What each can deliver:

| Detected | Supervised (restart on crash) | Starts at boot | Notes |
|---|---|---|---|
| `systemd-user` | yes | **yes** | `setup-service` + `loginctl enable-linger` |
| `launchd-agent` | yes | **no** (login session) | macOS LaunchAgent in `~/Library/LaunchAgents` |
| `runit` | yes | only if `runsvdir` is started at boot | run script in `~/.local/share/runit/service` |
| `s6` | yes | only if `s6-svscan` is started at boot | run script in `~/.local/share/s6/service` |
| `openrc-user` | best effort | unverified | experimental upstream (OpenRC 0.60+) |
| `openrc-system`, `sysvinit` | no | no | needs root; not attempted |
| `none`, `wsl1`, `container` | no | no | binaries installed, run manually |

**Why only systemd gets boot persistence.** For a user service to start at
boot, something running as root at boot must start it. systemd provides
exactly that through `loginctl enable-linger`. runit and s6 supervise
rootlessly and correctly, but their `runsvdir`/`s6-svscan` must itself be
started, and starting *that* at boot is a root action. macOS LaunchAgents
are session-bound: they stop on logout, and there is no user-level linger.
The installer therefore reports which of the two capabilities it actually
achieved and never claims persistence it did not configure.

**`setup-service` on runit, s6 and OpenRC.** `mcremote setup-service` and
`mcrelay setup-service` now set up these supervisors themselves, with the
same definitions as the systemd unit, and `update` restarts them
([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
PLAN S5b and S5f). Its wording for what it achieved is the installer's:

- **runit and s6, session only.** If no `runsvdir` or `s6-svscan` of yours
  watches the service directory, `setup-service` starts one, detached from the
  terminal, and prints `At boot: NOT configured`. That supervisor runs until
  the machine restarts. After a reboot nothing starts it, and the daemon stays
  down until you run `setup-service` again or start the supervisor yourself.
  To make it permanent, start your supervisor at boot, which is a root action.
- **OpenRC user services** need OpenRC 0.60 or later. If your OpenRC user
  session is not running, `setup-service` starts it, and prints that it starts
  at boot only if `pam_openrc` starts user sessions at login. The service is
  added to the user `default` runlevel.
- **`openrc-system`** (OpenRC older than 0.60), **`systemd-broken`** and
  **`none`:** `setup-service` writes nothing, prints the likely cause and how
  to run the daemon yourself, and exits 0. On `openrc-system` it also prints
  the root steps for a system service that runs the daemon as you.

`setup-service` confirms the start the way the installer does, and
`MCREMOTE_SVC_WAIT_SECS` bounds the wait for both. `MCREMOTE_NO_LINGER`,
`MCREMOTE_FORCE_SERVICE` and `MCREMOTE_WITH_RELAY_SERVICE` also act on
`setup-service` (`1` on; unset or `0` off; anything else refused). See
[config.md](config.md#mcremote-setup-service--mcremote---setup-service).

Control commands per backend:

```sh
# systemd
systemctl --user status|restart|stop mcremote
loginctl show-user "$USER" --property=Linger      # expect Linger=yes

# macOS launchd
launchctl print gui/$(id -u)/com.magiccliremote.mcremote
launchctl kickstart -k gui/$(id -u)/com.magiccliremote.mcremote

# runit
SVDIR=~/.local/share/runit/service sv status|up|down mcremote

# s6
s6-svstat ~/.local/share/s6/service/mcremote      # status ("up (pid N) …")
s6-svc -u|-d ~/.local/share/s6/service/mcremote   # bring up / take down

# OpenRC user service (setup-service only)
rc-service --user mcremote status|restart|stop
```

## Environment-specific notes

**WSL2** — systemd is opt-in. Add to `/etc/wsl.conf`:

```ini
[boot]
systemd=true
```

then run `wsl.exe --shutdown` from Windows and reopen the distro. WSL1 has no
service manager at all; upgrade with `wsl.exe --set-version <distro> 2`.

**`systemd-broken`** — `systemctl` exists but the user bus is unreachable.
Almost always caused by entering the session with `su`, which skips
`pam_systemd` and leaves `XDG_RUNTIME_DIR` unset, so every `systemctl --user`
call fails with an opaque D-Bus error. Reconnect over `ssh`, or use
`machinectl shell $(id -un)@`.

**`mcrelay` binds loopback by default.** `--with-relay-service` provisions
`listen.host: 127.0.0.1`, because a public bind with no TLS is refused at
startup and would leave the unit crash-looping (MADR 0099 F4b). To expose the
relay, set `listen.host` and `tls.mode=letsencrypt|files` together.

**`~/.local/bin` not on `PATH`** — standard on Ubuntu/Debian via `~/.profile`,
but not guaranteed in minimal containers or some RHEL-family shells. The
installer reports this rather than editing profile files.

**AppArmor restricted user namespaces (Ubuntu 24.04+)** — does not affect
`mcremote` itself, but breaks bubblewrap sandboxing inside the agent CLIs the
daemon spawns, which surfaces later as a confusing sandbox failure. The
installer detects and reports it; the remedy needs `sudo`:

```sh
sudo sh scripts/bwrap-apparmor-fix.sh    # see MADR 0048
```

**SELinux (Oracle Linux 9, Rocky 9)** — a `systemd --user` unit running a
binary from `$HOME` is expected to work unlabelled. If it does not, check
`ausearch -m avc -ts recent` and consider `restorecon -Rv ~/.local/bin`.

## Updating

After the first install, upgrades are the daemon's own job:

```sh
mcremote update          # or: mcrelay update
```

Since [MADR 0100](../decisions/0100-MADR-update-unit-refresh-and-daemon-reload.md) an update
does four things, in this order:

1. download the release asset and verify it against `SHA256SUMS`;
2. stop the service, if one is installed and running;
3. swap the binary, then **reconcile the service definition** by running
   `<new binary> setup-service --refresh` — the running process is the *old*
   binary and carries the *old* template, so only the newly installed one can
   render what the release ships;
4. `systemctl --user daemon-reload`, then start, then confirm it is active.

`mcremote update` only cycles `mcremote`'s service. `mcrelay update` only
cycles `mcrelay`'s. If that product has no service definition, the command
still replaces the binary and does not start a service. If the definition
exists but the process is down, the update starts it when it is enabled
(MADR 0103).

The steps above are systemd's. On a runit, s6 or OpenRC user service that
`setup-service` set up, `update` stops it through its supervisor, swaps and
refreshes the run script the same way, starts it, and confirms it with
`setup-service`'s verified start. A runit or s6 service with a `down` file,
or an OpenRC service outside the `default` runlevel, is disabled: if it was
stopped, it stays stopped.

The refresh reports one of four outcomes and never fails the update:

| Verdict | Meaning |
|---|---|
| `unchanged` | the installed definition already matches this release |
| `refreshed` | rewritten from the new template; the old file is kept as `<unit>.prev` |
| `kept` | not rewritten, with the reason — a hand-edited unit, or one this binary did not write |
| `none` | no service definition is installed; only the binary was replaced |

What a refresh preserves: every option baked in at setup time
(`--listen-port`, `--service-config`, `--data-dir`, `--env`), the unit's own
`Environment=PATH=` and `XDG_*` values, the `0600` mode of a unit carrying
`--env` secrets, and every drop-in under `<unit>.d/`. It re-renders only what the
template itself changed.

Two limitations worth knowing:

- **The behaviour arrives with the release that installs it.** The parent process
  in an update is the previous binary, so a host running a pre-0100 release gets
  the refresh from its *next* update. To apply a unit fix immediately, re-run
  `curl … | sh` (the installer refreshes on upgrade) or run
  `mcremote setup-service --refresh` by hand.
- **PATH is pinned, not re-derived.** If a release adds a directory to the
  service `PATH`, the refresh keeps your unit's existing value and says which
  entries it did not apply. `mcremote setup-service --force` re-derives them —
  at the cost of resetting every baked option to its default.

If no service is installed at all — `--no-service`, or a host with nothing to
supervise — `update` replaces the binary and exits 0 instead of failing on a
service that was never there. Before PLAN S5f this was also true of a
`runit`, `s6` or `openrc` host, whose service `update` could not cycle.

## Uninstall

```sh
mcremote setup-service --uninstall
sh install.sh --uninstall
```

`setup-service --uninstall` stops and removes every `mcremote` and `mcrelay`
service definition, on every service manager, whose program is in the same
folder as the `mcremote` running it. A definition that runs a binary from
another folder is reported and left. It covers what `setup-service` set up,
including an OpenRC user service.

`install.sh --uninstall` stops and disables the service, removes the unit or
run directory, and deletes both binaries. Configuration and data under
`~/.config` and `~/.local/share` are left in place deliberately.

## Testing

`scripts/install_test.sh` is offline and host-independent — it stubs `uname`
and replaces `PATH`, so it produces identical results on a macOS workstation
and a Linux runner:

```sh
sh scripts/install_test.sh
shellcheck -s sh scripts/install.sh          # must be clean; -s sh, not bash
docker run --rm -v "$PWD:/w" -w /w alpine:3.22 sh scripts/install_test.sh
```

Use `-s sh` rather than `-s bash`: Alpine's `/bin/sh` is busybox `ash`, and the
installer must stay POSIX.
