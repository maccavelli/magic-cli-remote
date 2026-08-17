#!/bin/sh
# mcremote / mcrelay Linux bootstrap installer — MADR 0097.
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
    [ "$uname_s" = Linux ] || die 1 "this installer supports Linux only (found $uname_s).
macOS installs need code-signing for durable Full Disk Access grants and are
not covered here; build from source with 'make install' instead."

    uname_m=$(uname -m)
    case "$uname_m" in
        x86_64|amd64)   ARCH=amd64 ;;
        aarch64|arm64)  ARCH=arm64 ;;
        armv6l|armv7l|armhf)
            die 1 "32-bit ARM ($uname_m) is not published; only amd64 and arm64 are built." ;;
        *)
            die 1 "unsupported architecture $uname_m; only amd64 and arm64 are published." ;;
    esac
}

# native | wsl1 | wsl2 | container
detect_environment() {
    osrel=""
    [ -r /proc/sys/kernel/osrelease ] && osrel=$(cat /proc/sys/kernel/osrelease)

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

# systemd-user | systemd-broken | runit | s6 | openrc-user | openrc-system | none
#
# Deliberately does not branch on /proc/1/comm: that reports the SYSTEM init,
# which says nothing about whether a rootless user manager is usable — the
# only question that matters here. It is recorded for diagnosis only.
detect_init() {
    INIT_PID1=$( [ -r /proc/1/comm ] && cat /proc/1/comm 2>/dev/null || echo unknown )

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
    if have s6-svscan && have s6-svc;    then INIT=s6;            return; fi
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
    _line=$(grep -E "  ${_p}-linux-${ARCH}-[0-9]" "$TMP_DIR/SHA256SUMS" | head -1) || true
    [ -n "$_line" ] || die 2 "no checksum entry for ${_p}-linux-${ARCH} in SHA256SUMS"

    _want=$(printf '%s\n' "$_line" | awk '{print $1}')
    _name=$(printf '%s\n' "$_line" | awk '{print $NF}')
    _got=$(sha256_of "$TMP_DIR/$_p")

    if [ "$_want" != "$_got" ]; then
        die 2 "checksum mismatch for $_p
  expected $_want
  got      $_got
Nothing was installed."
    fi
    RESOLVED_VER=${_name#"${_p}-linux-${ARCH}-"}
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
        fetch "$URL_DIR/$p-linux-$ARCH" "$TMP_DIR/$p" ||
            die 2 "could not download $p-linux-$ARCH from $URL_DIR"
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

svc_is_active() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user is-active "$1" >/dev/null 2>&1 ;;
        runit)        SVDIR="$RUNIT_DIR" sv status "$1" >/dev/null 2>&1 ;;
        s6)           s6-svc -l "$S6_DIR/$1" >/dev/null 2>&1 ;;
        openrc-user)  rc-service --user "$1" status >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

svc_start_one() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user start "$1" >/dev/null 2>&1 ;;
        runit)        SVDIR="$RUNIT_DIR" sv up "$1" >/dev/null 2>&1 ;;
        s6)           s6-svc -u "$S6_DIR/$1" >/dev/null 2>&1 ;;
        openrc-user)  rc-service --user "$1" start >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

svc_stop_one() { # $1 = product
    case "$INIT" in
        systemd-user) systemctl --user stop "$1" >/dev/null 2>&1 || true ;;
        runit)        SVDIR="$RUNIT_DIR" sv down "$1" >/dev/null 2>&1 || true ;;
        s6)           s6-svc -d "$S6_DIR/$1" >/dev/null 2>&1 || true ;;
        openrc-user)  rc-service --user "$1" stop >/dev/null 2>&1 || true ;;
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
            SVC_ACTIVE_LIST="$SVC_ACTIVE_LIST $_sp"
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
            warn "start it with: systemctl --user start $_sp"
        fi
    done
    [ "$_restored" -gt 0 ] && log "restarted:$SVC_ACTIVE_LIST" || true
}

svc_systemd() {
    _unit="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/mcremote.service"

    # Upgrade path. A unit already exists, so this is a re-install, not a
    # bootstrap: restart the existing service rather than rewriting a unit the
    # operator may have customised. `setup-service` refuses to overwrite
    # differing content without --force, and treating that refusal as a
    # failure would leave a previously-running daemon stopped. Same division
    # of labour as scripts/install-binary.sh, which swaps binaries and cycles
    # the service without touching the unit.
    if [ -f "$_unit" ] && [ "${FORCE_SERVICE:-0}" != 1 ]; then
        # Restart every daemon we stopped, not just mcremote — a relay host
        # runs mcrelay as a service too, and leaving it down (or worse, up on
        # a stale inode) is the failure this branch exists to prevent.
        _failed=""
        for _sp in $SVC_ACTIVE_LIST; do
            svc_start_one "$_sp" || _failed="$_failed $_sp"
        done
        if [ -z "$_failed" ]; then
            SERVICE_RESULT="supervised+boot"
            if [ -n "$SVC_ACTIVE_LIST" ]; then
                log "existing unit(s) kept; restarted on the new binary:$SVC_ACTIVE_LIST"
            else
                log "existing unit kept; no service was running to restart"
            fi
            log "refresh the unit itself with: mcremote setup-service --force"
            return 0
        fi
        warn "existing unit present but these failed to start:$_failed"
        SERVICE_RESULT=failed
        return 1
    fi

    set -- setup-service
    [ "${NO_LINGER:-0}" = 1 ] && set -- "$@" --no-linger
    [ "${FORCE_SERVICE:-0}" = 1 ] && set -- "$@" --force
    if ! "$INSTALL_DIR/mcremote" "$@"; then
        warn "mcremote setup-service failed; binaries are installed"
        SERVICE_RESULT=failed
        return 1
    fi
    if [ "${WITH_RELAY_SERVICE:-0}" = 1 ]; then
        "$INSTALL_DIR/mcrelay" "$@" || warn "mcrelay setup-service failed"
    fi
    SERVICE_RESULT="supervised+boot"
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
    svc_note_active
    svc_stop_if_running
    case "$INIT" in
        systemd-user)   svc_systemd || { svc_restore; return 3; } ;;
        runit)          svc_runit ;;
        s6)             svc_s6 ;;
        openrc-user)    svc_openrc_user ;;
        *)              SERVICE_RESULT=none ;;
    esac
    return 0
}

# ------------------------------------------------------------------ reports

advisories() {
    if [ "$INIT" = systemd-broken ]; then
        log ""
        log "systemctl is present but the user bus is unreachable."
        log "This usually means the session was entered with 'su', which skips"
        log "pam_systemd and leaves XDG_RUNTIME_DIR unset. Reconnect with ssh,"
        log "or use: machinectl shell $(id -un)@"
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
            log "service:  supervised for this login session only"
            log "at boot:  NOT configured — arrange for your supervisor to start at boot" ;;
        skipped)
            log "service:  skipped (--no-service)"
            log "run it:   $INSTALL_DIR/mcremote serve" ;;
        failed)
            log "service:  setup FAILED — the binaries are installed and usable"
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
    log "stopping any running service…"
    svc_stop_if_running
    systemctl --user disable mcremote >/dev/null 2>&1 || true
    rm -f "$HOME/.config/systemd/user/mcremote.service" \
          "$HOME/.config/systemd/user/mcrelay.service" 2>/dev/null || true
    systemctl --user daemon-reload >/dev/null 2>&1 || true
    rm -rf "$RUNIT_DIR/mcremote" "$S6_DIR/mcremote" "$RCDIR/init.d/mcremote" 2>/dev/null || true
    for p in $PRODUCTS; do
        rm -f "$INSTALL_DIR/$p" && log "removed $INSTALL_DIR/$p"
    done
    log "uninstalled. Config and data under ~/.config and ~/.local/share were left in place."
}

usage() {
    cat >&2 <<'EOF'
mcremote Linux installer

  install.sh [--version X.Y.Z] [--dir PATH] [--no-service]
             [--with-relay-service] [--no-linger] [--force-service]
             [--dry-run] [--verbose] [--uninstall] [--help]

  --force-service   rewrite an existing systemd unit (setup-service --force);
                    without it an existing unit is kept and the service is
                    simply restarted on the new binary

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
    vlog "arch=$ARCH env=$ENVIRONMENT init=$INIT (pid1=$INIT_PID1)"

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
    TMP_DIR=$(mktemp -d "$INSTALL_DIR/.mcinstall.XXXXXX") || die 1 "cannot create a temp dir in $INSTALL_DIR"
    trap cleanup EXIT INT TERM

    download_all
    install_binaries
    check_path

    svc_rc=0
    setup_service || svc_rc=$?

    advisories
    summary

    [ "$svc_rc" = 0 ] || exit 3
    exit 0
}

main "$@"
