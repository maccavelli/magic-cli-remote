# Linux install runbook

<!-- markdownlint-disable MD013 -->

Decision records: [MADR 0097](../decisions/0097-MADR-linux-curl-installer.md) (the
`curl | sh` install), and
[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
which replaced the hand-written script with one go-selfupdate-lib generates for each release.

```bash
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

The installer is a **bootstrap**, not a package manager. It places two verified
binaries, then runs `mcremote setup-service --from-installer` and
`mcrelay setup-service --from-installer`, which set up or refresh the service.
Upgrades are `mcremote update` ([MADR 0065](../decisions/0065-MADR-update-automation.md),
[0100](../decisions/0100-MADR-update-unit-refresh-and-daemon-reload.md) — see
[Updating](#updating)), so this script is run once per host.

What it guarantees:

- **No root.** Everything lands under `$HOME`; the script never calls `sudo`,
  and refuses to run as root unless given `--allow-root`.
- **Verified.** Each binary's SHA-256 is compared against the published
  `SHA256SUMS` *before* anything is installed. A mismatch leaves an existing
  installation untouched.
- **HTTPS only.** It downloads with `curl`, or with a `wget` that has
  `--https-only`. BusyBox `wget` has no such option and is refused, so an
  Alpine-style host needs `curl` installed first.
- **No GitHub API call**, so the 60-requests/hour per-IP anonymous limit
  cannot break an install behind shared egress.
- **Atomic.** Downloads are staged inside the install directory and renamed
  into place, so a partial download can never become a live binary. Each new
  binary must then report the release's version (`v<X.Y.Z> (release)`), or the
  previous ones are put back. The previous binary is kept as `<name>.prev`.

## Options

| Flag | Environment variable | Default |
|---|---|---|
| `--version TAG` | `MCREMOTE_VERSION` | the installer's own release |
| `--dir DIR` | `MCREMOTE_INSTALL_DIR` | `~/.local/bin` |
| `--product NAME` (repeatable) | — | both `mcremote` and `mcrelay` |
| `--no-hooks` | `MCREMOTE_NO_HOOKS=1` | unset: run `setup-service --from-installer` |
| `--verify-attestation` | — | unset; also verifies each download's attestation with `gh` |
| `--allow-root` | — | unset: refuse to run as root |
| `--force` | — | unset; with `--uninstall` only: delete the binaries even when a service cannot be removed |
| `--dry-run`, `--uninstall`, `--help` | — | — |

On the piped one-liner, flags go after `sh -s --`:

```bash
curl -fsSL <url>/install.sh | sh -s -- --no-hooks
```

**The service options are environment variables.** The installer passes its
environment to `setup-service`, which reads these (`1` on; unset or `0` off):

| Variable | Effect |
|---|---|
| `MCREMOTE_WITH_RELAY_SERVICE=1` | also set up `mcrelay` as a service, beside `mcremote`'s |
| `MCREMOTE_NO_LINGER=1` | skip `loginctl enable-linger` |
| `MCREMOTE_FORCE_SERVICE=1` | overwrite an existing service definition from defaults |
| `MCREMOTE_SVC_WAIT_SECS` | seconds to wait for the service to confirm it is running (default `15`) |

```bash
curl -fsSL <url>/install.sh | MCREMOTE_WITH_RELAY_SERVICE=1 sh
```

**Renamed from the earlier hand-written installer:**

| Before | Now |
|---|---|
| `--no-service`, `MCREMOTE_NO_SERVICE=1` | `--no-hooks`, `MCREMOTE_NO_HOOKS=1` |
| `--with-relay-service` | `MCREMOTE_WITH_RELAY_SERVICE=1` |
| `--no-linger` | `MCREMOTE_NO_LINGER=1` |
| `--force-service` | `MCREMOTE_FORCE_SERVICE=1` |
| `--verbose` | removed |
| `--version X.Y.Z` | `--version vX.Y.Z`; a version without its `v` still works |

`--no-hooks` skips `setup-service` entirely: no service is set up on a first
install, and on a second one the running daemon keeps running the previous
binary until you restart it.

**Installing an older release.** `--version` installs another release with
this installer's steps. A release published before this installer reports its
version in an older form, so it is refused: the installer puts back what was
there and exits `2`. Install such a release with its own installer, from
`https://github.com/maccavelli/magic-cli-remote/releases/download/<tag>/install.sh`.

Exit codes: `0` success · `1` usage error, unsupported platform or missing
tool, or with `--uninstall` a service that could not be removed (nothing
deleted) · `2` download, checksum or version check failed (nothing changed, or
the previous binaries restored) · `3` binaries installed, but `setup-service`
failed; or with `--uninstall`, deleted with `--force` although a service could
not be removed, or a binary that could not be deleted.

**What the service lines mean.** `setup-service` waits for the service to settle
and reports what it observed, never what it merely requested:

| Line | Meaning | Installer exit |
|---|---|---|
| `Running:           yes, confirmed after the start` | confirmed running | 0 |
| `Running:           not yet confirmed — still starting when the wait ended`, and a `warning:` | still coming up when the wait window closed; check with the printed command | 0 |
| `<name> is not running after setup: <why>. The definition is installed; see why with:` | confirmed not running; the backend's own diagnostic is printed beneath it | 3 |

**What a second install does.** Each product's `setup-service --from-installer`
looks for that product's service definition:

- **None:** for `mcremote`, a first install, as plain `setup-service`. For
  `mcrelay`, nothing: it prints that no relay service is set up, and how to
  set one up.
- **One that runs a binary from this folder:** it is refreshed as `update`
  refreshes it, keeping the options baked into it (see [Updating](#updating)).
  Then a running service is restarted on the new binary, a stopped one that is
  enabled is started, and a disabled one is left stopped. Each start is
  confirmed as above.
- **One that runs a binary from another folder:** it is reported and left
  alone, even with `MCREMOTE_FORCE_SERVICE=1`.
- **`MCREMOTE_FORCE_SERVICE=1`** makes a definition this folder owns a full
  `setup-service --force`, which resets every baked option to its default.

## Supported platforms

`linux/amd64` and `linux/arm64`. The binaries are built with `CGO_ENABLED=0`
and `-tags netgo,osusergo`, so they are **fully static** — one artifact per
architecture runs on glibc (Ubuntu, Oracle, Rocky, Debian) and musl (Alpine)
alike. 32-bit ARM, i686 and riscv64 are not published, and the installer
refuses them, naming the platforms the release ships.

The same `install.sh` also installs on Apple Silicon macOS (`darwin/arm64`).
Intel Macs are not supported; see
[MADR 0120](../decisions/0120-MADR-retire-the-darwin-amd64-target.md). The service there is
a launchd LaunchAgent, not systemd; Full Disk Access is a separate grant, see
[ops-macos-tcc.md](ops-macos-tcc.md).

## Service backends

`setup-service` probes for a *rootless* service manager. What each can deliver:

| Detected | Supervised (restart on crash) | Starts at boot | Notes |
|---|---|---|---|
| `systemd-user` | yes | **yes** | `setup-service` + `loginctl enable-linger` |
| `launchd-agent` | yes | **no** (login session) | macOS LaunchAgent in `~/Library/LaunchAgents` |
| `runit` | yes | only if `runsvdir` is started at boot | run script in `~/.local/share/runit/service` |
| `s6` | yes | only if `s6-svscan` is started at boot | run script in `~/.local/share/s6/service` |
| `openrc-user` | yes | only if `pam_openrc` starts user sessions | OpenRC 0.60+, user `default` runlevel |
| `openrc-system`, `systemd-broken`, `none` | no | no | nothing written; prints how to run the daemon yourself |

**Why only systemd gets boot persistence.** For a user service to start at
boot, something running as root at boot must start it. systemd provides
exactly that through `loginctl enable-linger`. runit and s6 supervise
rootlessly and correctly, but their `runsvdir`/`s6-svscan` must itself be
started, and starting *that* at boot is a root action. macOS LaunchAgents
are session-bound: they stop on logout, and there is no user-level linger.
`setup-service` therefore reports which of the two capabilities it actually
achieved and never claims persistence it did not configure.

**`setup-service` on runit, s6 and OpenRC.** `mcremote setup-service` and
`mcrelay setup-service` set up these supervisors themselves, with the
same definitions as the systemd unit, and `update` restarts them
([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
PLAN S5b and S5f). What it reports:

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

`MCREMOTE_SVC_WAIT_SECS` bounds the verified start. `MCREMOTE_NO_LINGER`,
`MCREMOTE_FORCE_SERVICE` and `MCREMOTE_WITH_RELAY_SERVICE` act on
`setup-service` whether the installer runs it or you do (`1` on; unset or `0`
off; anything else refused). See
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

# OpenRC user service
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

**Alpine and other BusyBox hosts** — install `curl` before the one-liner
(`apk add curl`). BusyBox `wget` cannot refuse a downgrade to plain HTTP, so
the installer will not use it.

**`mcrelay` binds loopback by default.** `MCREMOTE_WITH_RELAY_SERVICE=1`
provisions `listen.host: 127.0.0.1`, because a public bind with no TLS is
refused at startup and would leave the unit crash-looping (MADR 0099 F4b). To
expose the relay, set `listen.host` and `tls.mode=letsencrypt|files` together.

**`~/.local/bin` not on `PATH`** — standard on Ubuntu/Debian via `~/.profile`,
but not guaranteed in minimal containers or some RHEL-family shells. The
installer prints the `export PATH=…` line to add rather than editing profile
files.

**AppArmor restricted user namespaces (Ubuntu 24.04+)** — does not affect
`mcremote` itself, but breaks bubblewrap sandboxing inside the agent CLIs the
daemon spawns, which surfaces later as a confusing sandbox failure. When
`/proc/sys/kernel/apparmor_restrict_unprivileged_userns` reads `1`,
`mcremote setup-service --from-installer` prints a notice after its service
step. The remedy needs `sudo` and is host-wide: see
[config.md](config.md#codex-sandbox-unprivileged-user-namespaces-ubuntu-2404)
and MADR 0048. From a clone of this repository:

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
does five things, in this order:

1. download the release asset, verify it against `SHA256SUMS`, check that it is an
   executable for this platform, and run its `version`;
2. swap the binary while the service still runs, and run the installed binary's
   `version`: if that fails, the previous binary goes back and the service is never
   stopped
   ([0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](../decisions/0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md),
   D33, D34);
3. stop the service, if one is installed and running;
4. **reconcile the service definition** by running
   `<new binary> setup-service --refresh` — the running process is the *old*
   binary and carries the *old* template, so only the newly installed one can
   render what the release ships;
5. `systemctl --user daemon-reload`, then start, then confirm it is active.

`mcremote update` only cycles `mcremote`'s service. `mcrelay update` only
cycles `mcrelay`'s. If that product has no service definition, the command
still replaces the binary and does not start a service. If the definition
exists but the process is down, the update starts it when it is enabled
(MADR 0103).

The steps above are systemd's. On a runit, s6 or OpenRC user service that
`setup-service` set up, `update` swaps the binary first in the same way, stops the
service through its supervisor, refreshes the run script, starts it, and confirms it with
`setup-service`'s verified start. A runit or s6 service with a `down` file,
or an OpenRC service outside the `default` runlevel, is disabled: if it was
stopped, it stays stopped.

**Updating from a phone session.** An agent can run the update for you, but it
runs inside the service it updates, so it must pass `--yes`: `mcremote update
--yes`. Under systemd the update hands itself off to the service manager, as a
transient unit `<unit>-selfupdate-<id>.service`, and returns at once with
`update handed off: …`. The daemon then restarts on the new release, which ends
the session. `mcremote doctor` then shows the handoff's outcome: its ID, when it
started and finished, its exit code, and any error. Under
runit, s6 and OpenRC there is no handoff yet: the update refuses from inside the
service and leaves it running, so update from a shell instead
([MADR 0210](../decisions/0210-MADR-hand-off-agent-run-updates-outside-the-service.md)).

The refresh reports one of four outcomes and never fails the update:

| Verdict | Meaning |
|---|---|
| `unchanged` | the installed definition already matches this release |
| `refreshed` | rewritten from the new template; the old file is kept as `<unit>.prev` |
| `kept` | not rewritten, with the reason — a hand-edited unit, or one this binary did not write |
| `none` | no service definition is installed; only the binary was replaced |

The kept `<unit>.prev` can hold your `--env` values. `setup-service --remove`
and `--uninstall` delete it with the definition and print its path (MADR 0210
D9).

What a refresh preserves: every option baked in at setup time
(`--listen-port`, `--service-config`, `--data-dir`, `--env`), the unit's own
`Environment=PATH=` and `XDG_*` values, the `0600` mode of a unit carrying
`--env` secrets, and every drop-in under `<unit>.d/`. It re-renders only what the
template itself changed.

Two limitations worth knowing:

- **The behaviour arrives with the release that installs it.** The parent process
  in an update is the previous binary, so a host running a pre-0100 release gets
  the refresh from its *next* update. To apply a unit fix immediately, re-run
  `curl … | sh` (its `setup-service --from-installer` refreshes and restarts) or
  run `mcremote setup-service --refresh` by hand.
- **PATH is pinned, not re-derived.** If a release adds a directory to the
  service `PATH`, the refresh keeps your unit's existing value and says which
  entries it did not apply. `mcremote setup-service --force` re-derives them —
  at the cost of resetting every baked option to its default.

If no service is installed at all — `--no-hooks`, or a host with nothing to
supervise — `update` replaces the binary and exits 0 instead of failing on a
service that was never there. Before PLAN S5f this was also true of a
`runit`, `s6` or `openrc` host, whose service `update` could not cycle.

## Uninstall

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh -s -- --uninstall
```

**What it does.** For each product it removes, the installer first runs that
product's own `setup-service --uninstall --from-installer`, its
`before_uninstall` hook
([0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](../decisions/0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
amendment S9). Then it deletes the binaries and their `.prev` copies from
`~/.local/bin`, or from the `--dir` you pass it.

- **The hook removes only its own product's services**, on every service
  manager, whose program is in the same folder. So
  `--product mcrelay --uninstall` leaves `mcremote`'s service. A definition
  that runs a binary from another folder is reported and left.
- **A service that cannot be removed stops the uninstall:** nothing is deleted,
  and the installer exits `1`. `--force` deletes the binaries anyway, and exits
  `3`.
- **`--dry-run`** lists the hooks it would run, as `would run <product> <args>`,
  and the binaries it would remove.
- **Configuration and data** under `~/.config` and `~/.local/share` are left in
  place deliberately.

`mcremote setup-service --uninstall`, run by hand, still removes every
`mcremote` and `mcrelay` service from its folder at once.

**An install made by v0.21.0 or earlier.** The hook runs the installed binary,
and such a binary cannot run it: v0.21.0's refuses `--from-installer` with
`--uninstall`, and older ones have no `--uninstall`. So the uninstall stops
with exit `1` and deletes nothing. Remove the services by hand, then delete the
binaries without the hooks:

```sh
mcremote setup-service --uninstall      # v0.21.0; before it: mcremote setup-service --remove, and mcrelay's if it has one
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh -s -- --uninstall --no-hooks
```

## Testing

The installer is go-selfupdate-lib's template, and its tests live in that
repository. This repository pins what it feeds the template: the `installer`
block of `internal/updateclient/selfupdate-release.json`, checked by
`TestReleaseSpecInstaller` in `internal/updateclient/spec_test.go`, and
`setup-service --from-installer`, tested in `internal/cli/service`.

To see what a release's installer would do on a host without changing
anything:

```sh
curl -fsSL <url>/install.sh | sh -s -- --dry-run
```
