# Implement Linux + macOS curl bootstrap installer

Associated MADR: [0104-MADR-installer-linux-and-macos.md](0104-MADR-installer-linux-and-macos.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

## Goal

The same one-liner installs verified `mcremote` and `mcrelay` binaries on
Linux **and** macOS, sets up the native rootless service (systemd-user or
launchd LaunchAgent), and reports only what it actually achieved.

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

Linux behaviour that 0097 / 0099 / 0100 already verified does not change.
macOS is no longer a hard error.

## Scope

**In**

* `.github/workflows/ci.yml` — publish Darwin unversioned aliases the
  same way Linux aliases are published.
* `scripts/install.sh` — OS detection, asset names, launchd backend,
  stop-before-swap, teardown wait, quarantine clear, honest summary /
  uninstall / usage.
* `scripts/install_test.sh` — invert the Darwin rejection; add Darwin
  download/verify/launchd/uninstall cases; keep every Linux assertion.
* Operator docs: `README.md` (install section + "macOS and Windows"),
  `docs/ops-linux-install.md` (platform table), `docs/ops-macos-tcc.md`
  (one-liner is now a valid way to place the unsigned binary).

**Out**

* Developer ID, notarization, ad-hoc `codesign` in the installer.
* LaunchDaemons, sudo, linger-on-macOS.
* Windows.
* Turning hosted `macos-15` CI back on.
* Reimplementing `setup-service` or plist templating in shell.
* Changing `mcremote update` (0103). Re-running the one-liner stays a
  safe but unblessed upgrade.

## Prerequisites and locked decisions

Locked by the MADR; restated so this plan is executable without
re-deriving them:

| Decision | Value |
|---|---|
| Architecture | 0097 A2 + B1 + C2, plus Darwin as a first-class OS |
| Asset discovery | `${product}-${OS}-${ARCH}` unversioned alias; no API |
| Checksum | By hash value against the versioned `SHA256SUMS` line |
| Service | Delegate to `$INSTALL_DIR/mcremote setup-service` |
| Darwin init | `launchd-agent` if `launchctl` exists; Homebrew supervisors lose |
| Swap order | `svc_note_active` → stop → `wait_for_teardown` → install → start |
| Darwin persistence claim | Session-bound. Never "enabled at boot" |
| Privilege | None. Never `sudo` |
| Shell | POSIX `sh`. No `[[`, no `pipefail` |
| Signing | None. Print the FDA / `MC_CODESIGN_IDENTITY` advisory |
| Quarantine | Best-effort `xattr -d com.apple.quarantine` |

Commit after each phase. No `-m`; `git commit --no-edit`. Run
`sh scripts/install_test.sh` and `shellcheck -s sh scripts/install.sh`
before every commit that touches the script. No Go files change, so
`make pre-add-check` is not required unless a later discovery forces a
Go edit (none is planned).

---

## Phase 1 — Release aliases for Darwin

### 1.1 Publish Darwin aliases next to the Linux ones

`.github/workflows/ci.yml`, the alias loop at lines 624–631. Replace
the Linux-only glob and delete the "macOS is out of scope" comment.

Current:

```sh
          # Linux only: macOS installs are out of scope for 0097, and darwin
          # aliases would advertise support that does not exist.
          for f in mcremote-linux-*-"${VER}" mcrelay-linux-*-"${VER}"; do
            [ -f "$f" ] || continue
            alias_name="${f%-${VER}}"
            cp -f "$f" "$alias_name"
            ASSETS+=("$alias_name")
          done
```

New:

```sh
          # Unversioned aliases so install.sh can build a URL from uname
          # alone (MADR 0097 A2, extended to Darwin by MADR 0104). Copies
          # of the versioned assets, made AFTER SHA256SUMS-${VER} is
          # written, and NOT added to that manifest.
          for f in mcremote-*-"${VER}" mcrelay-*-"${VER}"; do
            [ -f "$f" ] || continue
            case "$f" in
              SHA256SUMS*|*.apk) continue ;;
            esac
            alias_name="${f%-${VER}}"
            cp -f "$f" "$alias_name"
            ASSETS+=("$alias_name")
          done
```

The broader glob is safe: the only `mcremote-*` / `mcrelay-*` files in
that directory at this point are the versioned Go binaries just
renamed. The APK is `magic-cli-remote-…apk`. `SHA256SUMS-${VER}` does
not match the glob.

Do **not** add the aliases to `SHA256SUMS`. The installer still
discovers `RESOLVED_VER` from the versioned filename.

### 1.2 Verify (this phase)

```sh
# The glob expansion is the thing that can go wrong. Recreate the
# rename+alias steps against a fake dist dir:
mkdir -p /tmp/alias-probe && cd /tmp/alias-probe
VER=0.13.9.1
for p in mcremote mcrelay; do
  for os in linux darwin; do
    for arch in amd64 arm64; do
      : > "${p}-${os}-${arch}-${VER}"
    done
  done
done
# run the new loop by hand; expect eight aliases, zero versioned
# files destroyed, and no SHA256SUMS alias.
```

No tagged release happens in this phase. Darwin aliases appear on the
next tag that includes this commit. Until then Phase 2+ is exercised
with `MC_TEST_BASE_URL` pointing at a local fake release.

**Commit** this phase.

---

## Phase 2 — OS detection and asset names

All edits in `scripts/install.sh` unless noted.

### 2.1 Header and usage

* Line 2 comment: drop "Linux bootstrap". Say
  `mcremote / mcrelay bootstrap installer — MADR 0097, extended to
  macOS by MADR 0104.`
* `usage()` (`install.sh:676`): title becomes
  `mcremote Linux / macOS installer`. Mention launchd next to systemd
  in the `--force-service` blurb.

### 2.2 `detect_arch`

Replace the Linux-only gate. Set both `OS` and `ARCH`.

```sh
detect_arch() {
    uname_s=$(uname -s)
    case "$uname_s" in
        Linux)  OS=linux ;;
        Darwin) OS=darwin ;;
        *)
            die 1 "this installer supports Linux and macOS only (found $uname_s).
Windows is not a supported host; use WSL2."
            ;;
    esac

    uname_m=$(uname -m)
    case "$uname_m" in
        x86_64|amd64)  ARCH=amd64 ;;
        aarch64|arm64) ARCH=arm64 ;;
        armv6l|armv7l|armhf)
            die 1 "32-bit ARM ($uname_m) is not published; only amd64 and arm64 are built." ;;
        *)
            die 1 "unsupported architecture $uname_m; only amd64 and arm64 are published." ;;
    esac
}
```

### 2.3 `download_all` / `verify_and_resolve`

Every hardcoded `linux` in those two functions becomes `${OS}`:

* grep: `"  ${_p}-${OS}-${ARCH}-[0-9]"`
* missing-entry error: `"${_p}-${OS}-${ARCH}"`
* `RESOLVED_VER=${_name#"${_p}-${OS}-${ARCH}-"}`
* fetch URL: `"$URL_DIR/$p-$OS-$ARCH"`

`vlog` in `main` already prints `arch=`. Add `os=$OS` next to it
(`install.sh:736`). Dry-run (`install.sh:751`) prints `os:` as well
as `arch:`.

### 2.4 Tests — invert Darwin rejection, parameterise the fake release

`scripts/install_test.sh`:

* File header: drop "PATH is replaced… `uname` always reports Linux".
  The suite now drives both. Default cases stay Linux via `mk_stubs`.
* `mk_release` grows an OS argument. Keep the existing two-arg call
  working so the 40+ Linux cases do not all change:

  ```sh
  mk_release() { # $1 = dir, $2 = arch, $3 = os (default linux), $4 = "corrupt"
      _os=${3:-linux}
      _corrupt=${4:-}
      # if someone still passes the old "corrupt" in $3:
      case "$3" in corrupt) _os=linux; _corrupt=corrupt ;; esac
      …
      printf … > "$rel/$p-$_os-$2"
      printf '%s  %s-%s-%s-%s\n' "$(sha_of …)" "$p" "$_os" "$2" "$VER"
  }
  ```

  Update the one existing `mk_release "$R" "$ARCH" corrupt` call to
  `mk_release "$R" "$ARCH" linux corrupt`.
* Replace section `3. non-Linux rejected` with:

  | Case | Want |
  |---|---|
  | stub `uname -s=Darwin -m=arm64`, Darwin release, `--dry-run --verbose` | exit 0, `arch: arm64`, `os: darwin` (or `os=darwin`), **no** "Linux only" |
  | stub `uname -s=Darwin -m=x86_64`, Darwin release, dry-run | exit 0, `arch: amd64` |
  | stub `uname -s=Windows_NT` (or `FreeBSD`) | exit 1, message names Linux and macOS |

* Add a Darwin checksum-mismatch case (`mk_release … darwin corrupt`)
  that exits 2 and leaves a pre-existing binary untouched — same
  contract as the Linux corrupt case at lines 149–154.

* Dry-run verbose on Linux must still print `arch:` and must now
  print `os: linux` (or `os=linux`). Update any assertion that
  assumed the old dry-run format if one exists.

### 2.5 Verify

```sh
sh -n scripts/install.sh
shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
```

All previous Linux assertions green. New Darwin dry-run and
FreeBSD/Windows rejection green.

**Commit** this phase.

---

## Phase 3 — launchd as a first-class init, stop-before-swap

This is the load-bearing phase. Get the ordering wrong and the first
real Mac upgrade dies with `ETXTBSY` or `Bootstrap failed: 5`.

### 3.1 `detect_init` — Darwin first

At the top of `detect_init`, before any `systemctl` probe:

```sh
    if [ "${OS:-}" = darwin ]; then
        INIT_PID1=launchd
        if have launchctl; then
            INIT=launchd-agent
        else
            INIT=none
        fi
        return
    fi
```

Linux probing below this is byte-identical to today. Homebrew `runit`
/ `s6` / a user-installed `systemctl` on a Mac must not take this
branch.

### 3.2 launchd helpers

Add next to the existing `svc_*` functions. Labels match
`internal/cli/service/control.go:213-215`:
`com.magiccliremote.<product>`.

```sh
launchd_label() { # $1 = product
    printf 'com.magiccliremote.%s' "$1"
}

launchd_plist() { # $1 = product
    printf '%s/Library/LaunchAgents/%s.plist' "$HOME" "$(launchd_label "$1")"
}

launchd_target() { # $1 = product
    printf 'gui/%s/%s' "$(id -u)" "$(launchd_label "$1")"
}

# bootout is async. install-binary.sh:104-117. 10s is enough; the
# installer already waits up to SVC_WAIT_SECS on the start side.
wait_for_teardown() { # $1 = product
    [ "$INIT" = launchd-agent ] || return 0
    _t=0
    while [ "$_t" -lt 10 ]; do
        launchctl print "$(launchd_target "$1")" >/dev/null 2>&1 || return 0
        sleep 1
        _t=$((_t+1))
    done
    warn "$1 did not finish tearing down; continuing"
}
```

### 3.3 `svc_is_active` / start / stop

Extend the three `case "$INIT"` switches.

**`svc_is_active`** — same rules as `install-binary.sh:83-99` and
`control.go:18-38`:

```sh
        launchd-agent)
            _out=$(launchctl print "$(launchd_target "$1")" 2>/dev/null) || return 1
            printf '%s' "$_out" | grep -qE 'state = (running|waiting)' && return 0
            printf '%s' "$_out" | grep -qE 'state = (exited|not running)' && return 1
            printf '%s' "$_out" | grep -Eq 'pid = [1-9]'
            ;;
```

**`svc_stop_one`**:

```sh
        launchd-agent)
            launchctl bootout "$(launchd_target "$1")" >/dev/null 2>&1 || true
            wait_for_teardown "$1"
            ;;
```

**`svc_start_one`** — do not reimplement bootstrap-vs-kickstart in
two places. Call the binary that already has the 0072 order
(`control.go:74-103`):

```sh
        launchd-agent)
            # Best-effort restart of an already-defined agent. First
            # install goes through setup-service, not here.
            "$INSTALL_DIR/$1" --help >/dev/null 2>&1 || return 1
            # Prefer the control path inside the just-installed binary
            # by kicking the documented verbs. If the binary is a test
            # stub, fall back to launchctl.
            if [ -f "$(launchd_plist "$1")" ]; then
                launchctl bootstrap "gui/$(id -u)" "$(launchd_plist "$1")" >/dev/null 2>&1 || true
                launchctl kickstart -k "$(launchd_target "$1")" >/dev/null 2>&1
            else
                return 1
            fi
            ;;
```

Keep this launchctl fallback. The test harness installs shell stubs,
not real `mcremote`, so `setup-service` is stubbed separately (3.7).
Do not call a stub as if it knew `setup-service` unless the test
stub implements it.

### 3.4 `svc_refresh_units`

Today (`install.sh:411-418`) only walks
`${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/*.service`. Also
refresh a Darwin plist when it exists:

```sh
        if [ -f "$(launchd_plist "$_rp")" ]; then
            "$INSTALL_DIR/$_rp" setup-service --refresh 2>&1 | sed 's/^/  /' || true
        fi
```

Keep the systemd walk. A Darwin host will not have those files; a
Linux host will not have the plists. Both loops are cheap and
keep the function OS-agnostic.

### 3.5 `svc_launchd` — sibling of `svc_systemd`

Mirror `svc_systemd` (`install.sh:420-484`) with these substitutions:

* Existing definition: `[ -f "$(launchd_plist mcremote)" ]`
* Refresh + restart the `SVC_ACTIVE_LIST` (already populated,
  see 3.6). Confirm with `svc_confirm mcremote "supervised-session"`
  — **not** `"supervised+boot"`.
* First install: `"$INSTALL_DIR/mcremote" setup-service` with the
  same `--force` plumbing. `--no-linger` may still be passed; the
  binary ignores it on Darwin.
* `--with-relay-service` calls `"$INSTALL_DIR/mcrelay" setup-service`
  and `svc_confirm mcrelay "supervised-session"`.

Wire it in `setup_service`:

```sh
        systemd-user)   svc_systemd  || { svc_restore; return 3; } ;;
        launchd-agent)  svc_launchd  || { svc_restore; return 3; } ;;
        runit)          svc_runit ;;
        …
```

`svc_settle` already has a generic (non-systemd) sustained-active
path. `launchd-agent` uses that. Do not invent an `NRestarts`
equivalent; launchd does not have one.

### 3.6 Stop-before-swap

`main` currently:

```
download_all
install_binaries
setup_service          # notes, stops, starts
```

Change to:

```
download_all
svc_note_active        # while old processes are still observable
svc_stop_if_running    # includes wait_for_teardown per product
install_binaries
clear_quarantine
setup_service          # must NOT call svc_note_active / svc_stop_if_running again
```

`setup_service` today starts with `svc_note_active; svc_stop_if_running`.
Remove those two lines from `setup_service`. After this change they
live only in `main` (and in `do_uninstall`, which keeps its own
note+stop so uninstall still works as a standalone path).

`--no-service` still skips `setup_service` entirely. It must **not**
skip the pre-swap stop: replacing a running Darwin binary without
stopping it is the `ETXTBSY` case. So the note+stop in `main` runs
even when `NO_SERVICE=1`.

`--dry-run` exits before `download_all`; no change.

`clear_quarantine`:

```sh
clear_quarantine() {
    have xattr || return 0
    for _p in $PRODUCTS; do
        [ -f "$INSTALL_DIR/$_p" ] || continue
        xattr -d com.apple.quarantine "$INSTALL_DIR/$_p" 2>/dev/null || true
    done
}
```

No-op on Linux. No-op when the xattr is absent.

### 3.7 Tests

Add a `mk_stubs_darwin` (or a third argument to `mk_stubs`) that
writes `uname -s=Darwin` and a `launchctl` stub.

`launchctl` stub, driven by files so each case can choose a state:

```sh
# $STUB/launchctl
# understands: print | bootout | bootstrap | kickstart | enable
# print reads $STUB/launchctl.state (running|exited|missing)
# every invocation appends argv to $STUB/launchctl.log
```

`mcremote` / `mcrelay` in a Darwin *service* test cannot be the tiny
`echo` stubs from `mk_release` if `svc_launchd` calls
`setup-service`. Two options, pick the second (simpler, matches how
Linux systemd tests work — they stub `systemctl`, not `mcremote`):

* `svc_launchd` first-install path: if the installed `mcremote` does
  not understand `setup-service` (test stub), write a marker plist
  ourselves only when `MC_TEST_BASE_URL` is set **or**
* Better: the Darwin service tests install a `mcremote` stub that
  implements `setup-service` by touching
  `$HOME/Library/LaunchAgents/com.magiccliremote.mcremote.plist` and
  exiting 0, and `--refresh` the same way. Put that stub in the
  fake release so `install_binaries` places it.

New assertions (section `3b. Darwin launchd`):

| # | Case | Want |
|---|---|---|
| D1 | Darwin + `launchctl` + healthy `state = running` after setup | exit 0; summary contains `LaunchAgent`; summary does **not** contain `enabled at boot` |
| D2 | Darwin, no `launchctl` | exit 0; binaries installed; `INIT=none` / "no supported service manager" |
| D3 | Darwin upgrade, plist already present, launchctl log shows `bootout` **before** any `bootstrap`/`kickstart` | order is stop → swap → start |
| D4 | Darwin `--uninstall` with `state = running` | `bootout` logged, plist gone, binaries gone |
| D5 | Homebrew `sv`/`runsvdir` present on a Darwin stub | `INIT` still `launchd-agent` (dry-run `--verbose` prints it) |
| D6 | `xattr` stub that records `-d com.apple.quarantine` | invoked on both binaries (or skipped cleanly if we do not install an `xattr` stub — then assert the installer still exits 0) |

Linux systemd tests (15–17, upgrade, uninstall) must stay green. The
`setup_service` note+stop move is the risk: those tests currently
rely on `setup_service` doing the note. After 3.6, `main` does it
before `install_binaries`. That is still before `svc_systemd` runs,
and the stub `systemctl is-active` still reports active, so
`SVC_ACTIVE_LIST` still populates. Confirm by running the suite,
do not reason it green.

### 3.8 Verify

```sh
sh -n scripts/install.sh
shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
```

**Commit** this phase.

---

## Phase 4 — Summary, advisories, uninstall, docs

### 4.1 `summary`

Add a `supervised-session` Darwin-specific line only if needed.
`supervised-session` already exists (`install.sh:628-630`) and is
what `svc_launchd` confirms with. Tighten the text so a LaunchAgent
is not described as "your supervisor":

```
        supervised-session)
            if [ "$INIT" = launchd-agent ]; then
                log "service:  running as a LaunchAgent (session-bound; stops on logout)"
                log "at boot:  NOT configured — keep a login session for an always-on Mac"
                log "check:    launchctl print gui/\$(id -u)/com.magiccliremote.mcremote"
            else
                # existing runit/s6 session text
            fi
            ;;
```

Leave `supervised+boot` exclusive to systemd-user.

### 4.2 `advisories`

After the existing WSL / AppArmor blocks, add a Darwin block that
always runs when `OS=darwin`:

```
        Full Disk Access is not granted by this installer.
        System Settings → Privacy & Security → Full Disk Access
        → add $INSTALL_DIR/mcremote → restart the LaunchAgent:
            launchctl kickstart -k gui/$(id -u)/com.magiccliremote.mcremote
        Unsigned upgrades drop that grant. To keep it, rebuild signed:
            make install MC_CODESIGN_IDENTITY='Apple Development: you (TEAMID)'
        See docs/ops-macos-tcc.md.
```

Do not mention AppArmor, linger, `su`, or `XDG_RUNTIME_DIR` on Darwin.
Those blocks already no-op because `/proc` is unreadable and
`INIT` is not `systemd-broken`.

### 4.3 `do_uninstall`

After the existing systemd disable/rm and before deleting binaries,
boot out and delete Darwin plists:

```sh
    for p in $PRODUCTS; do
        if have launchctl; then
            launchctl bootout "$(launchd_target "$p")" >/dev/null 2>&1 || true
            wait_for_teardown "$p"
        fi
        rm -f "$(launchd_plist "$p")" 2>/dev/null || true
    done
```

`have launchctl` is false on Linux; the `rm -f` of a path under
`$HOME/Library/LaunchAgents` is harmless if the directory does not
exist (`rm -f` on a missing file is 0; `launchd_plist` does not
`mkdir`).

Keep the systemd / runit / s6 / openrc cleanup. A Darwin host will
no-op those commands (`systemctl` missing, dirs empty).

### 4.4 Docs

`README.md`:

* Heading `## Install on Linux` → `## Install on Linux and macOS`.
* Step 5: mention launchd LaunchAgent on macOS, linger only on Linux.
* Replace `### macOS and Windows` (`README.md:126-132`). macOS is
  supported. Keep the FDA / signing pointer. Windows stays
  "use WSL2".

`docs/ops-linux-install.md`:

* Title can stay (it is the Linux runbook). Change
  "macOS is deliberately out of scope" (`:66-67`) to a pointer:
  macOS uses the same `install.sh`; service is launchd; see
  `ops-macos-tcc.md` for FDA.
* Add `launchd-agent` to the service-backend table:
  supervised yes, starts at boot **no** (login session).

`docs/ops-macos-tcc.md`:

* In "Granting Full Disk Access", note that
  `curl …/install.sh | sh` now places `~/.local/bin/mcremote` and
  does not grant FDA. The grant steps are unchanged.

Do not rewrite 0097. 0104 supersedes its scope; historical rationale
stays.

### 4.5 Verify

```sh
sh -n scripts/install.sh
shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
```

**Commit** this phase.

---

## Phase 5 — Live acceptance on this Mac, Linux regression

Stub tests cannot catch `ETXTBSY`, async `bootout`, or a real
`setup-service` plist. This phase is the 0097 lesson: the first
real host finds the pre-existing-state bugs.

### 5.1 Linux regression (offline, this repo)

```sh
sh scripts/install_test.sh
shellcheck -s sh scripts/install.sh
```

Must be all-green. If a Linux assertion flipped, fix it here, do not
carry it into the Mac run.

### 5.2 Darwin live, binaries only, no service

Stage a local fake release of the **real** Darwin binaries so this
does not depend on a not-yet-tagged alias:

```sh
make build
STAGE=$HOME/tmp/mc-0104-rel/latest/download
mkdir -p "$STAGE"
cp bin/mcremote "$STAGE/mcremote-darwin-arm64"
cp bin/mcrelay  "$STAGE/mcrelay-darwin-arm64"
# SHA256SUMS must list VERSIONED names (what verify_and_resolve matches):
VER=$(./bin/mcremote version | awk '{print $2}')
shasum -a 256 "$STAGE/mcremote-darwin-arm64" \
  | awk -v v="$VER" '{print $1"  mcremote-darwin-arm64-"v}' > "$STAGE/SHA256SUMS"
shasum -a 256 "$STAGE/mcrelay-darwin-arm64" \
  | awk -v v="$VER" '{print $1"  mcrelay-darwin-arm64-"v}' >> "$STAGE/SHA256SUMS"

# Point at a throwaway prefix so this cannot touch ~/.local/bin.
DEST=$HOME/tmp/mc-0104-bin
sh scripts/install.sh --dir "$DEST" --no-service \
  # MC_TEST_BASE_URL must be the release root, not …/latest/download
```

The installer joins `$BASE_URL/latest/download`, so:

```sh
export MC_TEST_BASE_URL=$HOME/tmp/mc-0104-rel
sh scripts/install.sh --dir "$DEST" --no-service --verbose
```

Accept:

* exit 0
* `$DEST/mcremote` and `$DEST/mcrelay` executable
* `file` reports Mach-O
* `"$DEST/mcremote" version` equals `$VER`
* no `~/Library/LaunchAgents/com.magiccliremote.mcremote.plist`
  created (because `--no-service`)

### 5.3 Darwin live, LaunchAgent, against a disposable label

Default `setup-service` writes `com.magiccliremote.mcremote`. That
is this machine's production agent. **Do not** point `--dir` at
`~/.local/bin` and do not run without `--no-service` against the
real label in the first attempt.

Two safe options; use the first:

1. Run `setup-service` yourself on the staged binary with
   `--unit-name mcremote-0104` (maps to
   `com.magiccliremote.mcremote-0104`) to prove the binary's launchd
   path, then
2. Only if the owner explicitly wants the real agent recycled: run
   the installer against `~/.local/bin` without `--no-service`.

This plan's acceptance for 5.3 is option 1 plus a **dry** installer
run that prints `init: launchd-agent`. Full one-liner-against-the-
production-agent is an owner action, recorded in the MADR
Confirmation when it happens.

```sh
# Prove detect_init + dry-run on the real Mac:
MC_TEST_BASE_URL=$HOME/tmp/mc-0104-rel \
  sh scripts/install.sh --dry-run --verbose --dir "$DEST"
# must print os=darwin (or os: darwin), arch=arm64, init=launchd-agent

"$DEST/mcremote" setup-service --unit-name mcremote-0104 --no-start
# plist exists:
test -f "$HOME/Library/LaunchAgents/com.magiccliremote.mcremote-0104.plist"
# clean up:
"$DEST/mcremote" setup-service --unit-name mcremote-0104 --remove
```

### 5.4 Record

Append a short "Phase 5 live verification" note to
`0104-MADR-installer-linux-and-macos.md` More Information, same
shape as 0097's verification table. Flip the MADR `status` to
`accepted` only after the owner has signed off and the production
one-liner has been seen to work on this Mac **or** the owner
accepts the disposable-label evidence as enough.

**Commit** the verification note (and any bugfixes this phase
found) as the last commit.

---

## Verification

| Check | Command | Pass |
|---|---|---|
| POSIX parse | `sh -n scripts/install.sh` | exit 0 |
| shellcheck | `shellcheck -s sh scripts/install.sh` | no findings |
| Offline suite | `sh scripts/install_test.sh` | all previous Linux assertions + new Darwin ones |
| No bashisms | existing suite lines 299–301 | no `[[`, no `pipefail`, no `sudo` |
| Truncation guard | `tail -1 scripts/install.sh` | `main "$@"` |
| Linux still Linux | dry-run on a Linux stub | `os: linux`, systemd path unchanged |
| Darwin accepted | dry-run on a Darwin stub | exit 0, `os: darwin`, not "Linux only" |
| Darwin launchd | D1–D5 in Phase 3.7 | LaunchAgent text, no "enabled at boot" |
| Live Darwin binary | Phase 5.2 | Mach-O in `$DEST`, version matches |
| Live Darwin plist | Phase 5.3 | disposable label writes and `--remove`s |

## Rollout and Rollback

**Rollout**

* Phases 1–4 land on `master` as four commits. They are inert for
  published one-liners until the next tag: GitHub still serves the
  previous `install.sh` from `releases/latest`.
* The next release tag publishes the new `install.sh` **and** the
  Darwin aliases together (Phase 1 and the script ride in the same
  tree). Do not tag a tree that has the new script but not the
  Darwin alias loop — a Mac would then 404
  `mcremote-darwin-arm64`.
* Pre-0104 published `install.sh` assets keep rejecting Darwin.
  That is expected. Operators on an old pin need to drop
  `MCREMOTE_VERSION` or download `install.sh` from the new tag.

**Rollback**

* Revert the four commits. The next tag after a revert goes back to
  Linux-only aliases + Linux-only `detect_arch`. Already-installed
  Macs keep their binaries and LaunchAgent; they just cannot
  re-run the one-liner.
* Do not delete Darwin aliases from an already-published release
  (`gh release upload --clobber` of a smaller set does not remove
  old assets). If a bad alias ever ships, upload a corrected copy
  over it; do not try to unpublish.

## Acceptance criteria

Taken from the MADR Confirmation, restated as a checklist:

* [x] `sh scripts/install_test.sh` green on this Mac (Linux stubs +
      Darwin stubs). 116 passed, 0 failed (2026-08-19).
* [x] `shellcheck -s sh scripts/install.sh` clean.
* [x] Linux one-liner behaviour unchanged: systemd, runit, s6,
      checksum, uninstall, `--force-service`, `--with-relay-service`
      (stub suite; no Linux assertion flipped).
* [x] Darwin dry-run no longer prints "Linux only".
* [x] Darwin install places both binaries (live 5.2). Stub D1
      delegates to `setup-service`, reports a LaunchAgent, never
      "enabled at boot". Production one-liner against `~/.local/bin`
      not run.
* [x] Darwin upgrade stop-before-start order: stub D3. Live 5.2
      did boot out the production label before the throwaway swap
      (label-global stop; see MADR Phase 5 note). Full production
      upgrade not run.
* [x] Darwin uninstall: stub D4. Live 5.3 `--remove` of disposable
      label `mcremote-0104` deleted the plist and left production
      alone.
* [x] FDA advisory printed on Darwin (live 5.2). No linger /
      AppArmor / `su` story.
* [ ] Next tag will publish four Darwin aliases as byte copies of
      the versioned artifacts.
* [x] Phase 5 live evidence recorded in the MADR. Status stays
      `proposed` pending owner sign-off.

## Risks

| Risk | Why it is real | Mitigation |
|---|---|---|
| `setup_service` note+stop move breaks Linux upgrade tests | Those tests were written against note-inside-`setup_service` | Run the full suite after 3.6 before writing more tests |
| `mk_release` signature change silently corrupts Linux cases | `$3` used to mean `corrupt` | Keep the `case "$3" in corrupt)` compatibility branch; update the one existing call |
| Real `bootout` needs a longer wait than 10s | `install-binary.sh` waits 10s (100 × 100ms) | Same budget, in 1s steps. If Phase 5 sees a leftover label, raise it |
| Operator runs Phase 5.3 against the production label | This machine's daemon | Disposable `--unit-name mcremote-0104` is mandatory in 5.3 |
| Next tag ships new `install.sh` without the alias loop | Mac 404s | Phase 1 is committed first and called out in Rollout as a pairing requirement |
| Homebrew `systemctl` on a Mac | Would have selected `systemd-broken` before this plan's Darwin-first probe | Darwin-first `return` in `detect_init` |
