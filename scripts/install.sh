#!/bin/sh
# mcremote / mcrelay bootstrap installer — MADR 0097, extended to
# macOS by MADR 0104.
#
#   curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
#
# Bootstrap only: it places two verified binaries and hands off to
# `mcremote setup-service`. Every later upgrade is `mcremote update` (MADR
# 0065), so this script is run once per host and is not an upgrade path.
#
# Properties this script must keep:
#   * POSIX sh (Alpine /bin/sh is busybox ash). Verify: shellcheck -s sh
#   * never invokes sudo — everything lands under $HOME
#   * no GitHub API call, so the 60-req/hour per-IP anon limit cannot bite
#   * verifies SHA-256 by VALUE, not filename (SHA256SUMS lists versioned
#     names; we download unversioned aliases — see MADR 0097)
#   * the whole body lives in main(), invoked on the last line, so a
#     truncated download can never execute a partial script
#
# Exit codes: 0 ok  1 usage/unsupported  2 download or verify failed
#             3 binaries installed but service setup failed
set -eu

REPO_URL="https://github.com/maccavelli/magic-cli-remote/releases"
PRODUCTS="mcremote mcrelay"
# How long to wait for a service to settle before reporting what it is doing.
# Generous on purpose: a cold or loaded host can legitimately take several
# seconds, and calling a healthy-but-slow start a failure is its own defect.
# The override exists for the test harness, not as a documented flag.
SVC_WAIT_SECS="${MCREMOTE_SVC_WAIT_SECS:-15}"
TMP_DIR=""

# ---------------------------------------------------------------- utilities

log()  { printf '%s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$2" >&2; exit "$1"; }
vlog() { [ "${VERBOSE:-0}" = 1 ] && printf '  %s\n' "$*" >&2 || true; }

# shellcheck disable=SC2329  # invoked indirectly via trap
cleanup() { [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && rm -rf "$TMP_DIR" || true; }

have() { command -v "$1" >/dev/null 2>&1; }

# fetch <url-or-path> <dest>. Plain paths are supported so the test harness
# can point MC_TEST_BASE_URL at a directory and exercise this offline.
fetch() {
    case "$1" in
        /*) [ -f "$1" ] || return 1; cp -f "$1" "$2" ;;
        *)
            if have curl; then
                curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
            else
                wget -q -O "$2" "$1"
            fi
            ;;
    esac
}

sha256_of() {
    if have sha256sum; then
        sha256sum "$1" | awk '{print $1}'
    elif have shasum; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    fi
}

# ---------------------------------------------------------------- detection

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
        x86_64|amd64)   ARCH=amd64 ;;
        aarch64|arm64)  ARCH=arm64 ;;
        armv6l|armv7l|armhf)
            die 1 "32-bit ARM ($uname_m) is not published; only amd64 and arm64 are built." ;;
        *)
            die 1 "unsupported architecture $uname_m; only amd64 and arm64 are published." ;;
    esac

    if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ]; then
        die 1 "darwin/amd64 is retired and is not published after v0.14.10 (MADR 0120).
Intel Macs have no supported current build; Rosetta cannot run darwin/arm64 on Intel.
v0.14.10 was the last release to carry darwin/amd64; install it manually if needed."
    fi
}

# native | wsl1 | wsl2 | container
detect_environment() {
    # Test seam, same convention as MC_TEST_BASE_URL: WSL is identified from a
    # kernel file that cannot be stubbed through PATH, so the advisory routing
    # would otherwise only be testable on a real Windows host.
    _osr="${MC_TEST_OSRELEASE:-/proc/sys/kernel/osrelease}"
    osrel=""
    [ -r "$_osr" ] && osrel=$(cat "$_osr")

    case "$osrel" in
        *WSL2*|*wsl2*) ENVIRONMENT=wsl2; return ;;
        *Microsoft*)   ENVIRONMENT=wsl1; return ;;
        *microsoft*)   ENVIRONMENT=wsl2; return ;;
    esac

    if [ -f /.dockerenv ] || [ -f /run/.containerenv ]; then
        ENVIRONMENT=container; return
    fi
    if [ -r /proc/1/cgroup ] && grep -qE 'docker|lxc|kubepods' /proc/1/cgroup 2>/dev/null; then
        ENVIRONMENT=container; return
    fi
    ENVIRONMENT=native
}

# systemd-user | systemd-broken | runit | s6 | openrc-user | openrc-system
# | launchd-agent | none
#
# Deliberately does not branch on /proc/1/comm: that reports the SYSTEM init,
# which says nothing about whether a rootless user manager is usable — the
# only question that matters here. It is recorded for diagnosis only.
# Darwin is launchd-first: Homebrew runit/s6/systemctl must not win.
detect_init() {
    if [ "${OS:-}" = darwin ]; then
        INIT_PID1=launchd
        if have launchctl; then
            INIT=launchd-agent
        else
            INIT=none
        fi
        return
    fi

    # Test seam, as for MC_TEST_OSRELEASE: /proc/1/comm cannot be stubbed via
    # PATH, and the advisory below now depends on it.
    _p1="${MC_TEST_PID1COMM:-/proc/1/comm}"
    INIT_PID1=$( [ -r "$_p1" ] && cat "$_p1" 2>/dev/null || echo unknown )

    if have systemctl; then
        if [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "${XDG_RUNTIME_DIR:-/nonexistent}" ] &&
           systemctl --user is-system-running >/dev/null 2>&1 ||
           { [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -d "${XDG_RUNTIME_DIR:-/nonexistent}" ] &&
             systemctl --user show-environment >/dev/null 2>&1; }; then
            INIT=systemd-user
        else
            INIT=systemd-broken
        fi
        return
    fi

    if have runsvdir && have sv;         then INIT=runit;         return; fi
    # s6-svstat is required, not optional: svc_is_active depends on it, so a
    # partial s6 install must degrade to `none` rather than to a backend whose
    # liveness probe cannot work.
    if have s6-svscan && have s6-svc && have s6-svstat; then INIT=s6; return; fi
    if have rc-service; then
        if rc-service --user --help >/dev/null 2>&1; then
            INIT=openrc-user
        else
            INIT=openrc-system
        fi
        return
    fi
    INIT=none
}

# ------------------------------------------------------------- resolve/fetch

# Verify by hash VALUE. SHA256SUMS lists versioned filenames
# (mcremote-linux-amd64-0.12.0.1) while we download the unversioned alias, so
# `sha256sum -c` would fail on the NAME. Matching the versioned manifest line
# also yields the resolved version with no API call.
verify_and_resolve() {
    _p=$1
    _line=$(grep -E "  ${_p}-${OS}-${ARCH}-[0-9]" "$TMP_DIR/SHA256SUMS" | head -1) || true
    [ -n "$_line" ] || die 2 "no checksum entry for ${_p}-${OS}-${ARCH} in SHA256SUMS"

    _want=$(printf '%s\n' "$_line" | awk '{print $1}')
    _name=$(printf '%s\n' "$_line" | awk '{print $NF}')
    _got=$(sha256_of "$TMP_DIR/$_p")

    if [ "$_want" != "$_got" ]; then
        die 2 "checksum mismatch for $_p
  expected $_want
  got      $_got
Nothing was installed."
    fi
    RESOLVED_VER=${_name#"${_p}-${OS}-${ARCH}-"}
    # Convention C5 (MADR 0116 F17): the extension comes LAST, so a Windows
    # manifest line yields "0.14.10.1.exe" without this. install.sh refuses to
    # run on Windows, but the manifest format is shared with install.ps1 and
    # with update/github.go, and all three must parse it the same way.
    RESOLVED_VER=${RESOLVED_VER%.exe}
    vlog "$_p verified, version $RESOLVED_VER"
}

download_all() {
    if [ -n "${PIN_VERSION:-}" ]; then
        URL_DIR="$BASE_URL/download/v$PIN_VERSION"
    else
        URL_DIR="$BASE_URL/latest/download"
    fi
    vlog "source $URL_DIR"

    fetch "$URL_DIR/SHA256SUMS" "$TMP_DIR/SHA256SUMS" ||
        die 2 "could not download SHA256SUMS from $URL_DIR
If you pinned a version, check that the release exists and carries the
unversioned alias assets (releases before MADR 0097 do not)."

    for p in $PRODUCTS; do
        fetch "$URL_DIR/$p-$OS-$ARCH" "$TMP_DIR/$p" ||
            die 2 "could not download $p-$OS-$ARCH from $URL_DIR"
        verify_and_resolve "$p"
    done
}

install_binaries() {
    mkdir -p "$INSTALL_DIR" || die 1 "cannot create $INSTALL_DIR"
    for p in $PRODUCTS; do
        chmod 0755 "$TMP_DIR/$p"
        mv -f "$TMP_DIR/$p" "$INSTALL_DIR/$p" || die 2 "cannot install $p to $INSTALL_DIR"
        log "installed $INSTALL_DIR/$p"
    done

    _reported=$("$INSTALL_DIR/mcremote" version 2>/dev/null | awk '{print $2}') || _reported=""
    if [ -n "$_reported" ] && [ -n "${RESOLVED_VER:-}" ] && [ "$_reported" != "$RESOLVED_VER" ]; then
        warn "installed binary reports '$_reported' but the manifest said '$RESOLVED_VER'"
    fi
}

# curl does not normally set com.apple.quarantine; browsers and some helper
# downloaders do. Missing xattr, or no such attribute, is a no-op.
clear_quarantine() {
    have xattr || return 0
    for _p in $PRODUCTS; do
        [ -f "$INSTALL_DIR/$_p" ] || continue
        xattr -d com.apple.quarantine "$INSTALL_DIR/$_p" 2>/dev/null || true
    done
}

check_path() {
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            log ""
            log "note: $INSTALL_DIR is not on your PATH. Add it with:"
            log "    export PATH=\"\$PATH:$INSTALL_DIR\""
            ;;
    esac
}

# --------------------------------------------------------- service backends

# Service-directory locations. Computed once in main() so that
# svc_stop_if_running can reference them before the matching backend has run —
# under `set -u` a bare reference to an unset variable is fatal.
svc_paths() {
    RUNIT_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/runit/service"
    S6_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/s6/service"
    RCDIR="${XDG_CONFIG_HOME:-$HOME/.config}/rc"
}

launchd_label() { # $1 = product
    printf 'com.magiccliremote.%s' "$1"
}

launchd_plist() { # $1 = product
    printf '%s/Library/LaunchAgents/%s.plist' "$HOME" "$(launchd_label "$1")"
}

launchd_target() { # $1 = product
    printf 'gui/%s/%s' "$(id -u)" "$(launchd_label "$1")"
}

# Absolute path for comparison. Parent must exist for a relative $1;
# after main() mkdir -p, INSTALL_DIR is already absolute.
svc_abs_file() { # $1 = path
    case "$1" in
        /*) printf '%s' "$1" ;;
        *)
            _d=$(dirname -- "$1")
            _b=$(basename -- "$1")
            if _p=$(CDPATH='' cd -- "$_d" && pwd); then
                printf '%s/%s' "$_p" "$_b"
            else
                printf '%s' "$1"
            fi
            ;;
    esac
}

# Supervisor-configured executable for $1, or empty if unknown.
# Unknown → svc_targets_install_dir treats as overlap (stop / teardown)
# so Darwin ETXTBSY protection and stub D3 stay intact (MADR 0104 E3).
svc_program_path() { # $1 = product
    case "${INIT:-}" in
        launchd-agent)
            _out=$(launchctl print "$(launchd_target "$1")" 2>/dev/null) || _out=""
            _p=$(printf '%s\n' "$_out" | awk -F ' = ' '/^[ \t]*program = / {
                p=$2; sub(/^[ \t]+/, "", p); sub(/[ \t]+$/, "", p)
                print p; exit
            }')
            if [ -n "$_p" ]; then printf '%s\n' "$_p"; return 0; fi
            _plist=$(launchd_plist "$1")
            if [ ! -f "$_plist" ] || ! have plutil; then
                return 0
            fi
            plutil -extract ProgramArguments.0 raw "$_plist" 2>/dev/null || true
            ;;
        systemd-user)
            _u="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$1.service"
            [ -f "$_u" ] || return 0
            awk -F= '/^ExecStart=/ {
                v=$2; for (i=3;i<=NF;i++) v=v "=" $i
                if (v ~ /^"/) { sub(/^"/,"",v); sub(/".*/,"",v) }
                else { sub(/[ \t].*/,"",v) }
                print v; exit
            }' "$_u"
            ;;
        runit)
            _f="${RUNIT_DIR:-}/$1/run"
            [ -f "$_f" ] || return 0
            awk '{ for (i=1;i<=NF;i++) if ($i=="exec") {
                p=$(i+1); gsub(/"/,"",p); print p; exit
            } }' "$_f"
            ;;
        s6)
            _f="${S6_DIR:-}/$1/run"
            [ -f "$_f" ] || return 0
            awk '{ for (i=1;i<=NF;i++) if ($i=="exec") {
                p=$(i+1); gsub(/"/,"",p); print p; exit
            } }' "$_f"
            ;;
        openrc-user)
            _f="${RCDIR:-}/init.d/$1"
            [ -f "$_f" ] || return 0
            awk -F= '/^command=/ { gsub(/"/,"",$2); print $2; exit }' "$_f"
            ;;
    esac
}

# 0 = this prefix owns the definition (stop / refresh / teardown).
# Empty program path → overlap, because a missed Darwin parse would
# otherwise swap a running Mach-O and hit ETXTBSY.
svc_targets_install_dir() { # $1 = product
    _prog=$(svc_program_path "$1")
    [ -z "$_prog" ] && return 0
    _want=$(svc_abs_file "$INSTALL_DIR/$1")
    [ "$_prog" = "$_want" ]
}

# A product with no definition is not ours to boot out. Unknown-overlap
# only applies when a unit/plist/run script exists (D4 empty plist).
svc_definition_exists() { # $1 = product
    case "${INIT:-}" in
        launchd-agent) [ -f "$(launchd_plist "$1")" ] ;;
        systemd-user)  [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$1.service" ] ;;
        runit)         [ -d "${RUNIT_DIR:-}/$1" ] ;;
        s6)            [ -d "${S6_DIR:-}/$1" ] ;;
        openrc-user)   [ -f "${RCDIR:-}/init.d/$1" ] ;;
        *)             return 1 ;;
    esac
}

# First-install setup-service. --binary is per product: a shared "$@"
# with mcremote's path would make mcrelay write the wrong ExecStart.
svc_run_setup() { # $1 = product
    _prod=$1
    set -- setup-service --binary "$INSTALL_DIR/$_prod"
    [ "${NO_LINGER:-0}" = 1 ] && set -- "$@" --no-linger
    [ "${FORCE_SERVICE:-0}" = 1 ] && set -- "$@" --force
    "$INSTALL_DIR/$_prod" "$@"
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

svc_is_active() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user is-active "$1" >/dev/null 2>&1 ;;
        runit)        SVDIR="$RUNIT_DIR" sv status "$1" >/dev/null 2>&1 ;;
        # s6-svc has no -l option: it exits 100 with a usage error, so this
        # probe was permanently false and neither uninstall nor upgrade ever
        # cycled the daemon — it survived on a (deleted) inode. s6-svstat is
        # the status tool. Match on "^up " rather than exit status: s6-svstat
        # exits 0 for a DOWN service too, which would swap a permanently-false
        # probe for a permanently-true one. MADR 0099 F1.
        s6)           s6-svstat "$S6_DIR/$1" 2>/dev/null | grep -q '^up ' ;;
        openrc-user)  rc-service --user "$1" status >/dev/null 2>&1 ;;
        launchd-agent)
            _out=$(launchctl print "$(launchd_target "$1")" 2>/dev/null) || return 1
            printf '%s' "$_out" | grep -qE 'state = (running|waiting)' && return 0
            printf '%s' "$_out" | grep -qE 'state = (exited|not running)' && return 1
            printf '%s' "$_out" | grep -Eq 'pid = [1-9]'
            ;;
        *) return 1 ;;
    esac
}

# svc_settle <product> — MADR 0099 F5.
#
# The installer used to report "running, and enabled at boot" whenever the
# setup command exited 0. But `systemctl --user enable --now` returns 0 once the
# start job is ISSUED, not once the unit is RUNNING, so a unit that died
# instantly still reported success. This turns the claim into a measurement.
#
# Why NRestarts and not just ActiveState: with Restart=always a crash-looping
# unit sits in activating/auto-restart forever and NEVER reaches `failed`, so
# waiting for `failed` waits for a state that never arrives. NRestarts is the
# only field that separates "still coming up" from "coming up over and over".
#
#   0 = confirmed running
#   1 = still coming up when the window closed (slow, not necessarily broken)
#   2 = confirmed not running (failed, or restart-looping)
svc_settle() {
    _sp=$1; _i=0; _ok=0

    # Baseline the auto-restart counter. Any increase while we watch means the
    # unit is cycling, whatever ActiveState happens to read at that instant.
    # (`systemctl start/restart` does not bump NRestarts; only automatic
    # restarts do, so a deliberate restart on the upgrade path is not counted.)
    _nr0=$(systemctl --user show "$_sp" --property=NRestarts --value 2>/dev/null || echo 0)
    case "$_nr0" in ''|*[!0-9]*) _nr0=0 ;; esac

    while [ "$_i" -lt "$SVC_WAIT_SECS" ]; do
        case "$INIT" in
            systemd-user)
                _as=$(systemctl --user show "$_sp" --property=ActiveState --value 2>/dev/null || echo unknown)
                _ss=$(systemctl --user show "$_sp" --property=SubState    --value 2>/dev/null || echo unknown)
                _nr=$(systemctl --user show "$_sp" --property=NRestarts   --value 2>/dev/null || echo 0)
                case "$_nr" in ''|*[!0-9]*) _nr=0 ;; esac

                # Two independent loop signals, because a unit can be caught at
                # either end of the cycle:
                #   - it restarted while we watched (started looping just now);
                #   - it is sitting in auto-restart having already looped
                #     (was looping before we arrived, counter static).
                [ "$_nr" -gt "$_nr0" ] && return 2
                if [ "$_ss" = auto-restart ] && [ "$_nr" -ge 2 ]; then return 2; fi
                [ "$_as" = failed ] && return 2

                # `active` must be SUSTAINED, not instantaneous. With
                # Type=simple systemd reports active the moment the process is
                # forked — before it can fail — so a service that dies on
                # startup is briefly indistinguishable from a healthy one.
                # Returning on the first sighting reported a crash-looping
                # daemon as running; measured on a real host, see MADR 0099.
                if [ "$_as" = active ]; then
                    _ok=$((_ok+1))
                    [ "$_ok" -ge 3 ] && return 0
                else
                    _ok=0
                fi
                ;;
            *)
                if svc_is_active "$_sp"; then
                    _ok=$((_ok+1))
                    [ "$_ok" -ge 2 ] && return 0
                else
                    _ok=0
                fi
                ;;
        esac
        _i=$((_i+1))
        sleep 1
    done
    return 1
}

# Print the backend's own diagnostic. A generic "it failed" is not actionable;
# the journal line naming the bind error or the capability fault is.
svc_diagnose() { # $1 = product
    case "$INIT" in
        systemd-user)
            log "  journalctl --user -u $1 -n 20 --no-pager"
            journalctl --user -u "$1" -n 8 --no-pager 2>/dev/null | sed 's/^/  | /' >&2 || true ;;
        s6)    log "  s6-svstat $S6_DIR/$1" ;;
        runit) log "  SVDIR=$RUNIT_DIR sv status $1" ;;
        launchd-agent) log "  launchctl print $(launchd_target "$1")" ;;
    esac
}

# svc_confirm <product> — apply the gate and translate to SERVICE_RESULT.
# $1 = product, $2 = the result the backend intended to deliver on success.
# Returns 1 when the service is confirmed NOT running, so callers can fail.
svc_confirm() {
    svc_settle "$1"; _rc=$?
    case "$_rc" in
        0) SERVICE_RESULT="$2"; return 0 ;;
        1) SERVICE_RESULT=starting
           warn "$1 has not reported active yet after ${SVC_WAIT_SECS}s"
           return 0 ;;
        *) SERVICE_RESULT=failed
           warn "$1 is not running after ${SVC_WAIT_SECS}s"
           svc_diagnose "$1"
           return 1 ;;
    esac
}

svc_start_one() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user start "$1" >/dev/null 2>&1 ;;
        runit)        SVDIR="$RUNIT_DIR" sv up "$1" >/dev/null 2>&1 ;;
        s6)           s6-svc -u "$S6_DIR/$1" >/dev/null 2>&1 ;;
        openrc-user)  rc-service --user "$1" start >/dev/null 2>&1 ;;
        launchd-agent)
            # Best-effort restart of an already-defined agent. First
            # install goes through setup-service, not here. Test stubs
            # implement setup-service; this path talks to launchctl
            # directly so a stub binary is enough.
            if [ -f "$(launchd_plist "$1")" ]; then
                launchctl bootstrap "gui/$(id -u)" "$(launchd_plist "$1")" >/dev/null 2>&1 || true
                launchctl kickstart -k "$(launchd_target "$1")" >/dev/null 2>&1
            else
                return 1
            fi
            ;;
        *) return 1 ;;
    esac
}

svc_stop_one() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user stop "$1" >/dev/null 2>&1 || true ;;
        runit)        SVDIR="$RUNIT_DIR" sv down "$1" >/dev/null 2>&1 || true ;;
        s6)           s6-svc -d "$S6_DIR/$1" >/dev/null 2>&1 || true ;;
        openrc-user)  rc-service --user "$1" stop >/dev/null 2>&1 || true ;;
        launchd-agent)
            launchctl bootout "$(launchd_target "$1")" >/dev/null 2>&1 || true
            wait_for_teardown "$1"
            ;;
    esac
}

# Which daemons were running before we touched anything.
#
# Both products are checked, not just mcremote. A relay host runs mcrelay as a
# service too, and install_binaries replaces BOTH binaries: on Linux a rename
# over a running executable leaves the process on its old inode, so a service
# we neither stop nor restart keeps executing the OLD code while the on-disk
# binary reports the new version. Silent staleness is worse than a crash.
#
# Note this is independent of --with-relay-service, which governs whether we
# CREATE a relay service, not whether we manage one that already exists.
svc_note_active() {
    SVC_ACTIVE_LIST=""
    for _sp in $PRODUCTS; do
        if svc_is_active "$_sp"; then
            if svc_targets_install_dir "$_sp"; then
                SVC_ACTIVE_LIST="$SVC_ACTIVE_LIST $_sp"
            else
                vlog "not stopping $_sp: program=$(svc_program_path "$_sp") want=$INSTALL_DIR/$_sp"
            fi
        fi
    done
    [ -n "$SVC_ACTIVE_LIST" ] && vlog "running services:$SVC_ACTIVE_LIST" || true
}

svc_stop_if_running() {
    for _sp in $SVC_ACTIVE_LIST; do
        svc_stop_one "$_sp"
    done
}

# Restart everything we stopped. Used both on the happy upgrade path and as
# the failure safety net.
svc_restore() {
    _restored=0
    for _sp in $SVC_ACTIVE_LIST; do
        if svc_start_one "$_sp"; then
            _restored=$((_restored+1))
        else
            warn "$_sp was running before this install and could not be restarted"
            case "$INIT" in
                launchd-agent) warn "start it with: launchctl kickstart -k $(launchd_target "$_sp")" ;;
                *) warn "start it with: systemctl --user start $_sp" ;;
            esac
        fi
    done
    [ "$_restored" -gt 0 ] && log "restarted:$SVC_ACTIVE_LIST" || true
}

# Re-render each installed unit from the binary just installed, keeping the
# options baked into it (MADR 0100). Output is indented under the installer's
# own lines; a non-zero exit is ignored, including the case where the installed
# binary predates --refresh.
svc_refresh_units() {
    _rud="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
    for _rp in mcremote mcrelay; do
        [ -x "$INSTALL_DIR/$_rp" ] || continue
        if [ -f "$_rud/$_rp.service" ] && svc_targets_install_dir "$_rp"; then
            "$INSTALL_DIR/$_rp" setup-service --refresh 2>&1 | sed 's/^/  /' || true
        fi
        if [ -f "$(launchd_plist "$_rp")" ] && svc_targets_install_dir "$_rp"; then
            "$INSTALL_DIR/$_rp" setup-service --refresh 2>&1 | sed 's/^/  /' || true
        fi
    done
}

svc_systemd() {
    _unit="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mcremote.service"

    # Upgrade path. A unit already exists, so this is a re-install, not a
    # bootstrap: do not rewrite wholesale a unit the operator may have
    # customised. `setup-service --force` would, and treating its refusal as a
    # failure would leave a previously-running daemon stopped.
    #
    # `--refresh` (MADR 0100) is the middle ground this branch was missing: it
    # re-renders the unit from the NEW binary's template while keeping the
    # options baked into the old one, and rewrites only a unit setup-service
    # wrote and can reproduce. Without it a release whose fix lives in the unit
    # template — 0099 F4a, a relay unit that could not start on any host —
    # installs its binary here and leaves the broken unit in place.
    if [ -f "$_unit" ] && ! svc_targets_install_dir mcremote; then
        log "existing unit execs $(svc_program_path mcremote); not recycling it for $INSTALL_DIR"
        SERVICE_RESULT=foreign
        return 0
    fi

    if [ -f "$_unit" ] && [ "${FORCE_SERVICE:-0}" != 1 ]; then
        # Before the restart, so the refreshed unit is what starts. Never fatal:
        # a refusal just means the unit is kept, which is the pre-0100 behaviour.
        svc_refresh_units
        # Restart every daemon we stopped, not just mcremote — a relay host
        # runs mcrelay as a service too, and leaving it down (or worse, up on
        # a stale inode) is the failure this branch exists to prevent.
        _failed=""
        for _sp in $SVC_ACTIVE_LIST; do
            svc_start_one "$_sp" || _failed="$_failed $_sp"
        done
        if [ -z "$_failed" ]; then
            # Gate even the upgrade path: `svc_start_one` returning 0 means the
            # start was accepted, not that the daemon survived it (0099 F5).
            svc_confirm mcremote "supervised+boot" || return 1
            if [ -n "$SVC_ACTIVE_LIST" ]; then
                log "existing unit(s) kept; restarted on the new binary:$SVC_ACTIVE_LIST"
            else
                log "existing unit kept; no service was running to restart"
            fi
            return 0
        fi
        warn "existing unit present but these failed to start:$_failed"
        SERVICE_RESULT=failed
        return 1
    fi

    if ! svc_run_setup mcremote; then
        warn "mcremote setup-service failed; binaries are installed"
        SERVICE_RESULT=failed
        return 1
    fi
    svc_confirm mcremote "supervised+boot" || return 1

    if [ "${WITH_RELAY_SERVICE:-0}" = 1 ]; then
        if svc_run_setup mcrelay; then
            # A relay that cannot start must not be reported as set up, but it
            # also must not discard a healthy mcremote: downgrade, do not fail.
            if ! svc_confirm mcrelay "supervised+boot"; then
                warn "mcremote is running; mcrelay is not"
                SERVICE_RESULT=failed
                return 1
            fi
        else
            warn "mcrelay setup-service failed"
        fi
    fi
}

svc_launchd() {
    _plist=$(launchd_plist mcremote)

    # Upgrade path. A plist already exists, so this is a re-install: do not
    # rewrite wholesale a LaunchAgent the operator may have customised.
    # --refresh (MADR 0100) re-renders from the NEW binary's template while
    # keeping the options baked into the old one.
    if [ -f "$_plist" ] && ! svc_targets_install_dir mcremote; then
        log "existing LaunchAgent execs $(svc_program_path mcremote); not recycling it for $INSTALL_DIR"
        SERVICE_RESULT=foreign
        return 0
    fi

    if [ -f "$_plist" ] && [ "${FORCE_SERVICE:-0}" != 1 ]; then
        svc_refresh_units
        _failed=""
        for _sp in $SVC_ACTIVE_LIST; do
            svc_start_one "$_sp" || _failed="$_failed $_sp"
        done
        if [ -z "$_failed" ]; then
            svc_confirm mcremote "supervised-session" || return 1
            if [ -n "$SVC_ACTIVE_LIST" ]; then
                log "existing LaunchAgent(s) kept; restarted on the new binary:$SVC_ACTIVE_LIST"
            else
                log "existing LaunchAgent kept; no service was running to restart"
            fi
            return 0
        fi
        warn "existing LaunchAgent present but these failed to start:$_failed"
        SERVICE_RESULT=failed
        return 1
    fi

    if ! svc_run_setup mcremote; then
        warn "mcremote setup-service failed; binaries are installed"
        SERVICE_RESULT=failed
        return 1
    fi
    svc_confirm mcremote "supervised-session" || return 1

    if [ "${WITH_RELAY_SERVICE:-0}" = 1 ]; then
        if svc_run_setup mcrelay; then
            if ! svc_confirm mcrelay "supervised-session"; then
                warn "mcremote is running; mcrelay is not"
                SERVICE_RESULT=failed
                return 1
            fi
        else
            warn "mcrelay setup-service failed"
        fi
    fi
}

# runit/s6 share a service-directory shape: a directory holding an executable
# `run` script that exec's the daemon.
write_run_script() {
    mkdir -p "$1/mcremote"
    printf '#!/bin/sh\nexec "%s/mcremote" serve 2>&1\n' "$INSTALL_DIR" > "$1/mcremote/run"
    chmod 0755 "$1/mcremote/run"
}

svc_runit() {
    write_run_script "$RUNIT_DIR"
    log "wrote $RUNIT_DIR/mcremote/run"

    if pgrep -u "$(id -u)" -f "runsvdir .*$RUNIT_DIR" >/dev/null 2>&1; then
        SERVICE_RESULT="supervised"
        log "an existing runsvdir is supervising $RUNIT_DIR; mcremote will start shortly"
    else
        runsvdir "$RUNIT_DIR" >/dev/null 2>&1 &
        SERVICE_RESULT="supervised-session"
        log "started runsvdir for this session (pid $!)"
    fi
    log "control it with: SVDIR=$RUNIT_DIR sv status|up|down mcremote"
}

svc_s6() {
    write_run_script "$S6_DIR"
    log "wrote $S6_DIR/mcremote/run"

    if pgrep -u "$(id -u)" -f "s6-svscan .*$S6_DIR" >/dev/null 2>&1; then
        SERVICE_RESULT="supervised"
    else
        s6-svscan "$S6_DIR" >/dev/null 2>&1 &
        SERVICE_RESULT="supervised-session"
        log "started s6-svscan for this session (pid $!)"
    fi
    log "control it with: s6-svc -u|-d $S6_DIR/mcremote"
}

# Experimental upstream (OpenRC 0.60+), so failure here is non-fatal by design.
svc_openrc_user() {
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
    log "wrote $RCDIR/init.d/mcremote"

    if rc-service --user mcremote start >/dev/null 2>&1; then
        SERVICE_RESULT="supervised"
    else
        warn "OpenRC user service did not start (this feature is experimental upstream)"
        SERVICE_RESULT=none
    fi
}

setup_service() {
    if [ "${NO_SERVICE:-0}" = 1 ]; then
        SERVICE_RESULT=skipped
        return 0
    fi
    # Note+stop happen in main() before install_binaries so Darwin does not
    # hit ETXTBSY (MADR 0104). Do not re-probe here: the daemons are already
    # down and SVC_ACTIVE_LIST would come back empty.
    case "$INIT" in
        systemd-user)   svc_systemd  || { svc_restore; return 3; } ;;
        launchd-agent)  svc_launchd  || { svc_restore; return 3; } ;;
        runit)          svc_runit ;;
        s6)             svc_s6 ;;
        openrc-user)    svc_openrc_user ;;
        *)              SERVICE_RESULT=none ;;
    esac
    return 0
}

# ------------------------------------------------------------------ reports

advisories() {
    # Not on WSL. There the cause is the distro's systemd boot setting, and the
    # su/pam_systemd story is wrong on every count: no su is involved, and
    # XDG_RUNTIME_DIR is SET (measured /mnt/wslg/runtime-dir on WSL2), so the
    # stated cause is factually false on the host being shown it. The WSL
    # advisories below say the right thing on their own. MADR 0099 F6.
    if [ "$INIT" = systemd-broken ] &&
       [ "$ENVIRONMENT" != wsl1 ] && [ "$ENVIRONMENT" != wsl2 ]; then
        log ""
        log "systemctl is present but the user bus is unreachable."
        # Distinguish the two causes rather than asserting the common one.
        # INIT_PID1 was already being collected and thrown away; on a host whose
        # PID 1 is not systemd the `su` story is simply false, and "reconnect
        # with ssh" cannot help because there is no user manager to reach.
        # MADR 0099 F8.
        if [ "$INIT_PID1" != systemd ] && [ "$INIT_PID1" != unknown ]; then
            log "PID 1 on this host is '$INIT_PID1', not systemd, so there is no"
            log "user manager to connect to. systemd user services are not"
            log "available here — run the daemon under this host's own"
            log "supervisor, or in the foreground."
        else
            log "This usually means the session was entered with 'su', which skips"
            log "pam_systemd and leaves XDG_RUNTIME_DIR unset. Reconnect with ssh,"
            log "or use: machinectl shell $(id -un)@"
        fi
    fi

    if [ "$ENVIRONMENT" = wsl2 ] && [ "$INIT" != systemd-user ]; then
        log ""
        log "WSL2 without systemd. Enable it by adding to /etc/wsl.conf:"
        log "    [boot]"
        log "    systemd=true"
        log "then run 'wsl.exe --shutdown' from Windows and reopen the distro."
    fi

    if [ "$ENVIRONMENT" = wsl1 ]; then
        log ""
        log "WSL1 has no service manager. Upgrade the distro to WSL2 for"
        log "background operation: wsl.exe --set-version <distro> 2"
    fi

    _ap=/proc/sys/kernel/apparmor_restrict_unprivileged_userns
    if [ -r "$_ap" ] && [ "$(cat "$_ap" 2>/dev/null)" = 1 ]; then
        log ""
        log "note: AppArmor restricts unprivileged user namespaces on this host."
        log "mcremote itself is unaffected, but agent CLI sandboxing (codex /"
        log "bubblewrap) will fail. Remedy (needs sudo, see MADR 0048):"
        log "    sudo sh scripts/bwrap-apparmor-fix.sh"
    fi

    if [ "${OS:-}" = darwin ]; then
        log ""
        log "Full Disk Access is not granted by this installer."
        log "System Settings → Privacy & Security → Full Disk Access"
        log "    → add $INSTALL_DIR/mcremote → restart the LaunchAgent:"
        log "    launchctl kickstart -k gui/\$(id -u)/com.magiccliremote.mcremote"
        log "Unsigned upgrades drop that grant. To keep it, rebuild signed:"
        log "    make install MC_CODESIGN_IDENTITY='Apple Development: you (TEAMID)'"
        log "See docs/ops-macos-tcc.md."
    fi
}

summary() {
    log ""
    log "mcremote/mcrelay ${RESOLVED_VER:-unknown} installed to $INSTALL_DIR"
    case "${SERVICE_RESULT:-none}" in
        "supervised+boot")
            log "service:  running, and enabled at boot (systemd user unit + linger)"
            if [ -n "${SVC_ACTIVE_LIST:-}" ]; then
                log "restarted:${SVC_ACTIVE_LIST}"
            fi
            log "check:    systemctl --user status mcremote" ;;
        supervised)
            log "service:  supervised, restarts on crash"
            log "at boot:  NOT configured — your supervisor must be started at boot" ;;
        supervised-session)
            if [ "$INIT" = launchd-agent ]; then
                log "service:  running as a LaunchAgent (session-bound; stops on logout)"
                log "at boot:  NOT configured — keep a login session for an always-on Mac"
                log "check:    launchctl print gui/\$(id -u)/com.magiccliremote.mcremote"
            else
                log "service:  supervised for this login session only"
                log "at boot:  NOT configured — arrange for your supervisor to start at boot"
            fi
            ;;
        skipped)
            log "service:  skipped (--no-service)"
            log "run it:   $INSTALL_DIR/mcremote serve" ;;
        foreign)
            log "service:  not modified — existing definition execs a different binary"
            log "binaries: $INSTALL_DIR  (this run did not stop or refresh the running agent)"
            ;;
        starting)
            log "service:  starting — not yet confirmed running"
            log "check:    systemctl --user status mcremote" ;;
        failed)
            log "service:  FAILED to start — the binaries are installed and usable"
            log "run it:   $INSTALL_DIR/mcremote serve" ;;
        *)
            log "service:  not configured (no supported service manager detected: $INIT)"
            log "run it:   $INSTALL_DIR/mcremote serve"
            log "background:"
            log "          nohup $INSTALL_DIR/mcremote serve >\$HOME/mcremote.log 2>&1 &" ;;
    esac
}

do_uninstall() {
    detect_init
    svc_paths
    if [ -d "$INSTALL_DIR" ]; then
        INSTALL_DIR=$(CDPATH='' cd -- "$INSTALL_DIR" && pwd) || true
    fi
    # svc_stop_if_running works from the list svc_note_active builds; without
    # this call the list is empty and uninstall would delete the binaries out
    # from under a still-running daemon, leaving it alive on a deleted inode.
    svc_note_active
    log "stopping any running service…"
    svc_stop_if_running

    _ud="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
    for p in $PRODUCTS; do
        if ! svc_definition_exists "$p"; then
            continue
        fi
        if svc_targets_install_dir "$p"; then
            systemctl --user disable "$p" >/dev/null 2>&1 || true
            rm -f "$_ud/$p.service" 2>/dev/null || true
            if have launchctl; then
                launchctl bootout "$(launchd_target "$p")" >/dev/null 2>&1 || true
                wait_for_teardown "$p"
            fi
            rm -f "$(launchd_plist "$p")" 2>/dev/null || true
            # ${var:?} so an empty path can never make this rm -rf "/…"
            rm -rf "${RUNIT_DIR:?}/$p" "${S6_DIR:?}/$p" "${RCDIR:?}/init.d/$p" 2>/dev/null || true
        else
            log "leaving $p service (program=$(svc_program_path "$p"); this prefix is $INSTALL_DIR/$p)"
        fi
    done
    systemctl --user daemon-reload >/dev/null 2>&1 || true
    for p in $PRODUCTS; do
        rm -f "$INSTALL_DIR/$p" && log "removed $INSTALL_DIR/$p"
    done
    log "uninstalled. Config and data under ~/.config and ~/.local/share were left in place."
}

usage() {
    cat >&2 <<'EOF'
mcremote Linux / macOS installer

  install.sh [--version X.Y.Z] [--dir PATH] [--no-service]
             [--with-relay-service] [--no-linger] [--force-service]
             [--dry-run] [--verbose] [--uninstall] [--help]

  --force-service   rewrite an existing systemd unit or launchd plist
                    (setup-service --force); without it an existing
                    definition is kept and the service is simply restarted
                    on the new binary

Piped invocation cannot take flags after `| sh`, so use the environment
equivalents instead:

  MCREMOTE_VERSION      same as --version
  MCREMOTE_INSTALL_DIR  same as --dir           (default ~/.local/bin)
  MCREMOTE_NO_SERVICE=1 same as --no-service

  curl -fsSL <url>/install.sh | MCREMOTE_NO_SERVICE=1 sh
EOF
}

# --------------------------------------------------------------------- main

main() {
    INSTALL_DIR="${MCREMOTE_INSTALL_DIR:-$HOME/.local/bin}"
    PIN_VERSION="${MCREMOTE_VERSION:-}"
    NO_SERVICE="${MCREMOTE_NO_SERVICE:-0}"
    BASE_URL="${MC_TEST_BASE_URL:-$REPO_URL}"
    WITH_RELAY_SERVICE=0; NO_LINGER=0; DRY_RUN=0; VERBOSE=0; UNINSTALL=0
    FORCE_SERVICE=0; SVC_ACTIVE_LIST=""
    RESOLVED_VER=""; SERVICE_RESULT=none

    while [ $# -gt 0 ]; do
        case "$1" in
            --version) PIN_VERSION="${2:?--version needs a value}"; shift 2 ;;
            --dir)     INSTALL_DIR="${2:?--dir needs a value}"; shift 2 ;;
            --no-service)         NO_SERVICE=1; shift ;;
            --with-relay-service) WITH_RELAY_SERVICE=1; shift ;;
            --no-linger)          NO_LINGER=1; shift ;;
            --force-service)      FORCE_SERVICE=1; shift ;;
            --dry-run)            DRY_RUN=1; shift ;;
            --verbose|-v)         VERBOSE=1; shift ;;
            --uninstall)          UNINSTALL=1; shift ;;
            --help|-h)            usage; exit 0 ;;
            *) usage; die 1 "unknown option: $1" ;;
        esac
    done

    PIN_VERSION="${PIN_VERSION#v}"

    detect_arch
    detect_environment

    if [ "$UNINSTALL" = 1 ]; then
        do_uninstall
        exit 0
    fi

    detect_init
    svc_paths
    vlog "os=$OS arch=$ARCH env=$ENVIRONMENT init=$INIT (pid1=$INIT_PID1)"

    # Preflight: report EVERY missing tool, not just the first.
    missing=""
    case "$BASE_URL" in
        /*) ;;  # local path (test harness) — no downloader needed
        *) have curl || have wget || missing="$missing curl-or-wget" ;;
    esac
    have sha256sum || have shasum || have openssl || missing="$missing sha256sum-or-shasum-or-openssl"
    have awk  || missing="$missing awk"
    have grep || missing="$missing grep"
    [ -z "$missing" ] || die 1 "missing required tools:$missing"

    if [ "$DRY_RUN" = 1 ]; then
        log "dry run — nothing will be written"
        log "  os:          $OS"
        log "  arch:        $ARCH"
        log "  environment: $ENVIRONMENT"
        log "  init:        $INIT (pid1=$INIT_PID1)"
        log "  install dir: $INSTALL_DIR"
        if [ -n "$PIN_VERSION" ]; then
            log "  source:      $BASE_URL/download/v$PIN_VERSION"
        else
            log "  source:      $BASE_URL/latest/download"
        fi
        exit 0
    fi

    # Temp dir lives INSIDE the install dir so the final mv is a
    # same-filesystem rename (atomic). /tmp is often a different filesystem.
    mkdir -p "$INSTALL_DIR" || die 1 "cannot create $INSTALL_DIR"
    INSTALL_DIR=$(CDPATH='' cd -- "$INSTALL_DIR" && pwd) ||
        die 1 "cannot resolve $INSTALL_DIR"
    TMP_DIR=$(mktemp -d "$INSTALL_DIR/.mcinstall.XXXXXX") || die 1 "cannot create a temp dir in $INSTALL_DIR"
    trap cleanup EXIT INT TERM

    download_all
    # Stop before the swap so Darwin does not hit ETXTBSY, and so launchd
    # finishes tearing down before we bootstrap again (MADR 0104). Note the
    # active set first: after the stop, a re-probe would see nothing.
    # Runs even with --no-service — replacing a running binary still needs
    # the process out of the way.
    svc_note_active
    svc_stop_if_running
    install_binaries
    clear_quarantine
    check_path

    svc_rc=0
    setup_service || svc_rc=$?

    advisories
    summary

    [ "$svc_rc" = 0 ] || exit 3
    exit 0
}

main "$@"
