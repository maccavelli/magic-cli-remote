#!/bin/sh
# Tests for scripts/install.sh (MADR 0097 / PLAN Phase 5).
#
# Fully offline and host-independent: a fake release directory is served via
# MC_TEST_BASE_URL, and PATH is replaced with a stub directory whose `uname`
# always reports Linux. The suite therefore produces identical results on a
# macOS workstation and a Linux CI runner.
#
#   sh scripts/install_test.sh
set -eu

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
INSTALLER="$HERE/install.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; [ $# -gt 1 ] && printf '       %s\n' "$2"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1" "missing [$3] in: $2" ;; esac; }

sha_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
    else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# ---------------------------------------------------------- fake release dir

VER=9.9.9.1
mk_release() { # $1 = dir, $2 = arch, $3 = "corrupt" to poison mcremote
    rel="$1/latest/download"; mkdir -p "$rel"
    for p in mcremote mcrelay; do
        printf '#!/bin/sh\necho "%s %s"\n' "$p" "$VER" > "$rel/$p-linux-$2"
        chmod 0755 "$rel/$p-linux-$2"
    done
    : > "$rel/SHA256SUMS"
    for p in mcremote mcrelay; do
        printf '%s  %s-linux-%s-%s\n' "$(sha_of "$rel/$p-linux-$2")" "$p" "$2" "$VER" >> "$rel/SHA256SUMS"
    done
    if [ "${3:-}" = corrupt ]; then
        printf '#!/bin/sh\necho tampered\n' > "$rel/mcremote-linux-$2"
        chmod 0755 "$rel/mcremote-linux-$2"
    fi
}

# PATH is replaced wholesale with this dir, so detect_init sees exactly the
# tools named here and nothing the host happens to have.
mk_stubs() { # $1 = dir, $2 = uname -m value, rest = extra stub tool names
    d="$1"; um="$2"; shift 2; mkdir -p "$d"
    for t in "$@"; do printf '#!/bin/sh\nexit 0\n' > "$d/$t"; chmod 0755 "$d/$t"; done
    for t in sh mktemp mkdir mv rm chmod cat grep awk cp id pgrep tail head find wc tr sed; do
        p=$(command -v "$t" 2>/dev/null) || continue
        [ -e "$d/$t" ] || ln -sf "$p" "$d/$t"
    done
    for t in sha256sum shasum openssl; do
        p=$(command -v "$t" 2>/dev/null) || continue
        [ -e "$d/$t" ] || ln -sf "$p" "$d/$t"
    done
    # Written last and never symlinked: writing through a symlink would try to
    # overwrite the real /usr/bin/uname.
    # shellcheck disable=SC2016  # $1 must reach the stub literally
    printf '#!/bin/sh\ncase "$1" in -m) echo %s ;; -s) echo Linux ;; *) echo Linux ;; esac\n' \
        "$um" > "$d/uname"
    chmod 0755 "$d/uname"
}

# run_installer <path> <base-url|-> <install-dir> [args...]
#
# PATH is taken as a positional argument, not a prefix assignment: in POSIX sh
# an assignment prefixed to a FUNCTION call persists in the caller's shell,
# which silently leaked the stub PATH into the rest of the suite.
run_installer() {
    _rp="$1"; _url="$2"; _dir="$3"; shift 3
    ( set +e
      # Pre-existing latent bug: the init probe asserts systemd-broken for a
      # host with systemctl but no user bus, yet XDG_RUNTIME_DIR was inherited
      # from the developer's own session (/run/user/1000 on any systemd box),
      # so detect_init returned systemd-user and the case failed on real
      # workstations while passing in a container. The suite must not depend on
      # the environment it is run from.
      unset XDG_RUNTIME_DIR
      [ -n "$_rp" ] && { PATH="$_rp"; export PATH; }
      [ "$_url" != "-" ] && { MC_TEST_BASE_URL="$_url"; export MC_TEST_BASE_URL; }
      MCREMOTE_INSTALL_DIR="$_dir"; export MCREMOTE_INSTALL_DIR
      "$INSTALLER" "$@" >"$WORK/out" 2>"$WORK/err"
      echo $? > "$WORK/rc" )
    RC=$(cat "$WORK/rc"); OUT=$(cat "$WORK/out" "$WORK/err" 2>/dev/null || true)
}

# ------------------------------------------------------- 1,2 arch mapping

printf '\n1-2. architecture mapping\n'
for pair in "x86_64 amd64" "amd64 amd64" "aarch64 arm64" "arm64 arm64"; do
    m=${pair% *}; want=${pair#* }
    S="$WORK/stub-arch-$m"; mk_stubs "$S" "$m"
    R="$WORK/rel-$want"; [ -d "$R" ] || mk_release "$R" "$want"
    run_installer "$S" "$R" "$WORK/bin-arch-$m" --dry-run --verbose
    check "uname -m=$m maps to $want" "$(printf '%s\n' "$OUT" | grep -c "arch: *$want")" 1
done

for m in armv7l armv6l armhf; do
    S="$WORK/stub-$m"; mk_stubs "$S" "$m"
    run_installer "$S" - "$WORK/bin-$m" --dry-run
    check "$m rejected (exit 1)" "$RC" 1
    contains "  $m message names 32-bit ARM" "$OUT" "32-bit ARM"
done

for m in i686 riscv64; do
    S="$WORK/stub-$m"; mk_stubs "$S" "$m"
    run_installer "$S" - "$WORK/bin-$m" --dry-run
    check "$m rejected (exit 1)" "$RC" 1
    contains "  $m message names the arch" "$OUT" "unsupported architecture"
done

# ---------------------------------------------------------------- 3 non-Linux

printf '\n3. non-Linux rejected\n'
S="$WORK/stub-darwin"; mk_stubs "$S" arm64
# shellcheck disable=SC2016  # $1 must reach the stub literally
printf '#!/bin/sh\ncase "$1" in -s) echo Darwin ;; -m) echo arm64 ;; esac\n' > "$S/uname"
chmod 0755 "$S/uname"
run_installer "$S" - "$WORK/bin-darwin" --dry-run
check "Darwin rejected (exit 1)" "$RC" 1
contains "  message points at Linux-only scope" "$OUT" "Linux only"

# Everything below runs on a stubbed Linux/amd64 host.
ARCH=amd64
BASE="$WORK/stub-base"; mk_stubs "$BASE" x86_64

# ------------------------------------------------------- 4-7 download/verify

printf '\n4-7. download and verification\n'

R="$WORK/rel-nomatch"; mk_release "$R" "$ARCH"
grep -v mcremote "$R/latest/download/SHA256SUMS" > "$R/latest/download/S.tmp"
mv "$R/latest/download/S.tmp" "$R/latest/download/SHA256SUMS"
D="$WORK/bin-nomatch"
run_installer "$BASE" "$R" "$D" --no-service
check "missing manifest entry exits 2" "$RC" 2
check "  nothing installed" "$( [ -e "$D/mcremote" ] && echo yes || echo no )" no

R="$WORK/rel-corrupt"; mk_release "$R" "$ARCH" corrupt
D="$WORK/bin-corrupt"; mkdir -p "$D"; printf 'PREEXISTING\n' > "$D/mcremote"
run_installer "$BASE" "$R" "$D" --no-service
check "checksum mismatch exits 2" "$RC" 2
check "  existing install untouched" "$(cat "$D/mcremote")" "PREEXISTING"
check "  no temp dir left behind" "$(find "$D" -maxdepth 1 -name '.mcinstall.*' | wc -l | tr -d ' ')" 0

R="$WORK/rel-ok"; mk_release "$R" "$ARCH"; D="$WORK/bin-ok"
run_installer "$BASE" "$R" "$D" --no-service
check "valid digest installs (exit 0)" "$RC" 0
check "  mcremote installed executable" "$( [ -x "$D/mcremote" ] && echo yes || echo no )" yes
check "  mcrelay installed executable"  "$( [ -x "$D/mcrelay" ]  && echo yes || echo no )" yes
contains "  resolved version reported" "$OUT" "$VER"

R="$WORK/rel-alias"; mk_release "$R" "$ARCH"
printf 'deadbeef  mcremote-linux-%s\n' "$ARCH" >> "$R/latest/download/SHA256SUMS"
D="$WORK/bin-alias"
run_installer "$BASE" "$R" "$D" --no-service
check "alias line does not shadow the versioned line" "$RC" 0

run_installer "$BASE" "$R" "$D" --no-service
check "re-run is idempotent (exit 0)" "$RC" 0
check "  no temp dir left behind" "$(find "$D" -maxdepth 1 -name '.mcinstall.*' | wc -l | tr -d ' ')" 0

# ------------------------------------------------------------ 8-10 init probe

printf '\n8-10. init detection\n'
# A local base URL is used so preflight does not demand curl/wget: the stub
# PATH deliberately contains no downloader, and the probe is about init
# detection, not fetching.
probe_init() { # $1 = stub dir
    run_installer "$1" "$WORK/rel-ok" "$WORK/bin-probe" --dry-run --verbose
    printf '%s\n' "$OUT" | sed -n 's/.*init: *\([a-z0-9-]*\).*/\1/p' | head -1
}

S="$WORK/stub-none";   mk_stubs "$S" x86_64
check "no service manager -> none" "$(probe_init "$S")" "none"

S="$WORK/stub-broken"; mk_stubs "$S" x86_64 systemctl
check "systemctl without XDG_RUNTIME_DIR -> systemd-broken" "$(probe_init "$S")" "systemd-broken"

S="$WORK/stub-runit";  mk_stubs "$S" x86_64 runsvdir sv
check "runsvdir+sv and no systemd -> runit" "$(probe_init "$S")" "runit"

S="$WORK/stub-s6";     mk_stubs "$S" x86_64 s6-svscan s6-svc s6-svstat
check "s6-svscan+s6-svc+s6-svstat -> s6" "$(probe_init "$S")" "s6"

# 20 — a partial s6 install must not select a backend whose liveness probe
# cannot work (0099 F1): s6-svstat is what svc_is_active depends on.
S="$WORK/stub-s6-partial"; mk_stubs "$S" x86_64 s6-svscan s6-svc
check "20 s6 without s6-svstat -> none" "$(probe_init "$S")" "none"

# Two OpenRC stubs: native user services are experimental upstream, so the
# probe must distinguish a build that supports --user from one that does not.
S="$WORK/stub-openrc-sys"; mk_stubs "$S" x86_64
# shellcheck disable=SC2016  # $1 must reach the stub literally
printf '#!/bin/sh\ncase "$1" in --user) exit 1 ;; esac\nexit 0\n' > "$S/rc-service"
chmod 0755 "$S/rc-service"
check "rc-service rejecting --user -> openrc-system" "$(probe_init "$S")" "openrc-system"

S="$WORK/stub-openrc-usr"; mk_stubs "$S" x86_64
printf '#!/bin/sh\nexit 0\n' > "$S/rc-service"
chmod 0755 "$S/rc-service"
check "rc-service accepting --user -> openrc-user" "$(probe_init "$S")" "openrc-user"

# The runit backend must write a real, executable run script.
R="$WORK/rel-runit"; mk_release "$R" "$ARCH"
S="$WORK/stub-runit2"; mk_stubs "$S" x86_64 runsvdir sv
D="$WORK/bin-runit"; H="$WORK/home-runit"; mkdir -p "$H"
( set +e
  PATH="$S"; export PATH
  HOME="$H"; export HOME
  XDG_DATA_HOME="$H/.local/share"; export XDG_DATA_HOME
  MC_TEST_BASE_URL="$R"; export MC_TEST_BASE_URL
  MCREMOTE_INSTALL_DIR="$D"; export MCREMOTE_INSTALL_DIR
  "$INSTALLER" >"$WORK/out" 2>"$WORK/err"; echo $? > "$WORK/rc" )
RUNF="$H/.local/share/runit/service/mcremote/run"
check "runit backend writes a run script" "$( [ -f "$RUNF" ] && echo yes || echo no )" yes
check "  run script is executable" "$( [ -x "$RUNF" ] && echo yes || echo no )" yes
check "  run script exec's the daemon" "$(grep -c 'exec .*mcremote.* serve' "$RUNF" 2>/dev/null || echo 0)" 1
contains "  reports supervision without boot persistence" "$(cat "$WORK/out" "$WORK/err")" "at boot"

# Upgrade path: an existing unit must be kept and the service restarted, not
# treated as a setup failure. Regression test for the wonder host, where a
# failed setup-service left a previously-running daemon stopped.
R="$WORK/rel-upg"; mk_release "$R" "$ARCH"
S="$WORK/stub-systemd"; mk_stubs "$S" x86_64 systemctl loginctl
D="$WORK/bin-upg"; H="$WORK/home-upg"
mkdir -p "$H/.config/systemd/user" "$H/run"
printf '# existing unit with different content\n' > "$H/.config/systemd/user/mcremote.service"
( set +e
  PATH="$S"; export PATH
  HOME="$H"; export HOME
  XDG_CONFIG_HOME="$H/.config"; export XDG_CONFIG_HOME
  XDG_RUNTIME_DIR="$H/run"; export XDG_RUNTIME_DIR
  MC_TEST_BASE_URL="$R"; export MC_TEST_BASE_URL
  MCREMOTE_INSTALL_DIR="$D"; export MCREMOTE_INSTALL_DIR
  "$INSTALLER" >"$WORK/out" 2>"$WORK/err"; echo $? > "$WORK/rc" )
UPG_RC=$(cat "$WORK/rc"); UPG_OUT=$(cat "$WORK/out" "$WORK/err")
check "upgrade over an existing unit exits 0" "$UPG_RC" 0
contains "  existing unit is kept" "$UPG_OUT" "existing unit"
# The stub systemctl reports both products active, so both must be restarted.
# This is the relay-host case: install_binaries replaces BOTH binaries, so a
# service we do not cycle keeps running old code on its old inode.
contains "  mcremote restarted" "$UPG_OUT" "mcremote"
contains "  mcrelay also restarted" "$UPG_OUT" "mcrelay"
check "  unit was NOT rewritten" "$(head -1 "$H/.config/systemd/user/mcremote.service")" "# existing unit with different content"

# Uninstall must stop running daemons before deleting their binaries. A prior
# refactor made svc_stop_if_running depend on a list that uninstall never
# populated, so it removed the binaries and left the daemon alive on a deleted
# inode. The stub systemctl logs its arguments so we can assert the stop.
R="$WORK/rel-uni"; mk_release "$R" "$ARCH"
S="$WORK/stub-uni"; mk_stubs "$S" x86_64 loginctl
D="$WORK/bin-uni"; H="$WORK/home-uni"; mkdir -p "$H/.config/systemd/user" "$H/run"
# shellcheck disable=SC2016  # $@ and the log var must reach the stub literally
printf '#!/bin/sh\necho "$@" >> "$MC_SYSTEMCTL_LOG"\nexit 0\n' > "$S/systemctl"
chmod 0755 "$S/systemctl"
mkdir -p "$D"; : > "$D/mcremote"; : > "$D/mcrelay"
printf 'unit\n' > "$H/.config/systemd/user/mcremote.service"
( set +e
  PATH="$S"; export PATH
  HOME="$H"; export HOME
  XDG_CONFIG_HOME="$H/.config"; export XDG_CONFIG_HOME
  XDG_RUNTIME_DIR="$H/run"; export XDG_RUNTIME_DIR
  MC_SYSTEMCTL_LOG="$WORK/sysctl.log"; export MC_SYSTEMCTL_LOG
  MC_TEST_BASE_URL="$R"; export MC_TEST_BASE_URL
  MCREMOTE_INSTALL_DIR="$D"; export MCREMOTE_INSTALL_DIR
  "$INSTALLER" --uninstall >"$WORK/out" 2>"$WORK/err"; echo $? > "$WORK/rc" )
check "uninstall exits 0" "$(cat "$WORK/rc")" 0
check "  binaries removed" "$( [ -e "$D/mcremote" ] || [ -e "$D/mcrelay" ] && echo present || echo gone )" gone
check "  unit removed" "$( [ -e "$H/.config/systemd/user/mcremote.service" ] && echo present || echo gone )" gone
# The stub records full argv, e.g. "--user stop mcremote", so match unanchored.
check "  stopped mcremote before deleting" "$(grep -c 'stop mcremote' "$WORK/sysctl.log" 2>/dev/null || true)" 1
check "  stopped mcrelay before deleting" "$(grep -c 'stop mcrelay' "$WORK/sysctl.log" 2>/dev/null || true)" 1

# --------------------------------------------------------- 11-14 flags/shape

printf '\n11-14. flags and script shape\n'
R="$WORK/rel-pin"; mk_release "$R" "$ARCH"
run_installer "$BASE" "$R" "$WORK/bin-pin" --dry-run --verbose --version 1.2.3
contains "pinned version uses download/vX.Y.Z" "$OUT" "download/v1.2.3"

D="$WORK/bin-dry"
run_installer "$BASE" "$R" "$D" --dry-run
check "dry-run writes nothing" "$( [ -e "$D/mcremote" ] && echo yes || echo no )" no

check "last line is the truncation guard" "$(tail -1 "$INSTALLER")" 'main "$@"'
sh -n "$INSTALLER" 2>"$WORK/parse" && PARSE=0 || PARSE=1
check "POSIX parse (sh -n)" "$PARSE" 0
check "no bashisms: no [[" "$(grep -c '\[\[' "$INSTALLER" || true)" 0
check "no bashisms: no pipefail" "$(grep -c 'pipefail' "$INSTALLER" || true)" 0
check "never invokes sudo" "$(grep -cE '^[[:space:]]*sudo ' "$INSTALLER" || true)" 0
check "help exits 0" "$( ( "$INSTALLER" --help >/dev/null 2>&1; echo $? ) )" 0
run_installer "$BASE" - "$WORK/bin-bogus" --bogus-flag
check "unknown flag exits 1" "$RC" 1

# ------------------------------------- 15-17 service state verification (0099)

printf '\n15-17. service state verification (MADR 0099 F5)\n'

# A stub systemctl that (a) satisfies detect_init so the systemd-user backend is
# selected, and (b) reports a chosen unit state back to the verification gate.
#   $1 = dir  $2 = ActiveState  $3 = SubState  $4 = NRestarts
# A stub whose unit always reads ActiveState=active but whose NRestarts climbs
# on every call — i.e. a Type=simple service that is respawning constantly and
# is `active` at each instant we happen to look. This is the shape that defeated
# the first version of the gate on a real host (MADR 0099).
mk_systemctl_flapping() { # $1 = dir
    cat > "$1/nrestarts" <<'CNT'
0
CNT
    cat > "$1/systemctl" <<STUB
#!/bin/sh
case " \$* " in
  *" is-system-running "*|*" show-environment "*) exit 0 ;;
esac
for a in "\$@"; do
  case "\$a" in
    --property=ActiveState) echo active; exit 0 ;;
    --property=SubState)    echo running; exit 0 ;;
    --property=NRestarts)
        n=\$(cat "$1/nrestarts" 2>/dev/null || echo 0)
        n=\$((n+1)); echo "\$n" > "$1/nrestarts"; echo "\$n"; exit 0 ;;
  esac
done
exit 0
STUB
    chmod 0755 "$1/systemctl"
}

mk_systemctl() {
    cat > "$1/systemctl" <<STUB
#!/bin/sh
case " \$* " in
  *" is-system-running "*|*" show-environment "*) exit 0 ;;
esac
for a in "\$@"; do
  case "\$a" in
    --property=ActiveState) echo "$2"; exit 0 ;;
    --property=SubState)    echo "$3"; exit 0 ;;
    --property=NRestarts)   echo "$4"; exit 0 ;;
  esac
done
case " \$* " in
  *" is-active "*) [ "$2" = active ] || exit 3 ;;
esac
exit 0
STUB
    chmod 0755 "$1/systemctl"
}

# Dedicated runner: the systemd-user path needs XDG_RUNTIME_DIR and a short
# settle window. Kept separate from run_installer so the existing cases keep
# their exact environment.
# XDG_CONFIG_HOME and XDG_DATA_HOME must be overridden too, not just HOME:
# svc_systemd resolves the unit as ${XDG_CONFIG_HOME:-$HOME/.config}/..., so a
# developer whose own machine has mcremote installed would otherwise have the
# suite read their REAL unit file and silently take the upgrade branch.
run_svc() { # $1 = stub dir, $2 = install dir, $3 = home dir, rest = installer args
    _s="$1"; _d="$2"; _h="$3"; shift 3
    mkdir -p "$WORK/xdg" "$_h/.config" "$_h/.local/share"
    ( set +e
      PATH="$_s"; export PATH
      MC_TEST_BASE_URL="$WORK/rel-svc"; export MC_TEST_BASE_URL
      MCREMOTE_INSTALL_DIR="$_d"; export MCREMOTE_INSTALL_DIR
      XDG_RUNTIME_DIR="$WORK/xdg"; export XDG_RUNTIME_DIR
      # Must allow at least the 3 consecutive `active` samples the gate now
      # requires, while staying short enough not to pad the suite.
      MCREMOTE_SVC_WAIT_SECS=5; export MCREMOTE_SVC_WAIT_SECS
      HOME="$_h"; export HOME
      XDG_CONFIG_HOME="$_h/.config"; export XDG_CONFIG_HOME
      XDG_DATA_HOME="$_h/.local/share"; export XDG_DATA_HOME
      "$INSTALLER" "$@" >"$WORK/out" 2>"$WORK/err"
      echo $? > "$WORK/rc" )
    RC=$(cat "$WORK/rc"); OUT=$(cat "$WORK/out" "$WORK/err" 2>/dev/null || true)
}

mk_release "$WORK/rel-svc" "$ARCH"

# 15 — THE CONTRACT (MADR 0097 section 4.0): a backend whose liveness probe says
# the unit is NOT running must never yield a "running, and enabled at boot"
# claim. This assertion fails against v0.13.4 by design; that is the point.
S="$WORK/stub-svc-loop"; mk_stubs "$S" x86_64
mk_systemctl "$S" activating auto-restart 6
run_svc "$S" "$WORK/bin-svc-loop" "$WORK/home-loop"
case "$OUT" in
    *"running, and enabled at boot"*)
        bad "15 crash-looping unit must not be reported as running" \
            "summary claimed boot-persistent supervision for a unit in auto-restart" ;;
    *) ok "15 crash-looping unit is not reported as running" ;;
esac
check "15b crash loop exits 3" "$RC" 3
contains "15c crash loop names the failure" "$OUT" "FAILED to start"

# 16 — slow but healthy: still activating when the window closed. Not an error;
# reporting it as one would turn a loaded host into a false alarm.
S="$WORK/stub-svc-slow"; mk_stubs "$S" x86_64
mk_systemctl "$S" activating start 0
run_svc "$S" "$WORK/bin-svc-slow" "$WORK/home-slow"
contains "16 slow start reports 'starting'" "$OUT" "starting"
check "16b slow start still exits 0" "$RC" 0

# 17 — healthy: unchanged from v0.13.4.
S="$WORK/stub-svc-ok"; mk_stubs "$S" x86_64
mk_systemctl "$S" active running 0
run_svc "$S" "$WORK/bin-svc-ok" "$WORK/home-ok"
contains "17 active unit reports boot persistence" "$OUT" "running, and enabled at boot"
check "17b healthy path exits 0" "$RC" 0

# ------------------------------------------ 18-19 s6 liveness probe (0099 F1)

printf '\n18-19. s6 liveness probe (MADR 0099 F1)\n'

# svc_is_active is not directly callable, so drive it through --uninstall, which
# calls svc_note_active -> svc_is_active and logs what it found.
s6_probe_says() { # $1 = s6-svstat output line -> echoes "active" | "inactive"
    _d="$WORK/s6-$2"; _h="$WORK/home-s6-$2"
    S="$WORK/stub-s6-$2"; mk_stubs "$S" x86_64 s6-svscan s6-svc
    printf '#!/bin/sh\necho "%s"\n' "$1" > "$S/s6-svstat"; chmod 0755 "$S/s6-svstat"
    mkdir -p "$_h/.local/share/s6/service/mcremote" "$_d"
    printf '#!/bin/sh\nexit 0\n' > "$_d/mcremote"; chmod 0755 "$_d/mcremote"
    ( set +e
      PATH="$S"; export PATH
      MCREMOTE_INSTALL_DIR="$_d"; export MCREMOTE_INSTALL_DIR
      HOME="$_h"; export HOME
      XDG_DATA_HOME="$_h/.local/share"; export XDG_DATA_HOME
      XDG_CONFIG_HOME="$_h/.config"; export XDG_CONFIG_HOME
      unset XDG_RUNTIME_DIR
      "$INSTALLER" --uninstall --verbose >"$WORK/out" 2>"$WORK/err" ) || true
    if grep -q 'running services:.*mcremote' "$WORK/out" "$WORK/err" 2>/dev/null
    then echo active; else echo inactive; fi
}

check "18 s6-svstat reporting up -> active" "$(s6_probe_says 'up (pid 1) 3 seconds' up)" "active"
check "19 s6-svstat reporting down -> inactive" "$(s6_probe_says 'down 0 seconds' down)" "inactive"

# ----------------------------------------- 21-22 WSL advisory routing (0099 F6)

printf '\n21-22. WSL advisory routing (MADR 0099 F6)\n'

# WSL detection reads /proc/sys/kernel/osrelease, which cannot be stubbed via
# PATH, so drive detect_environment through a fake proc file instead.
wsl_advice() { # $1 = osrelease contents -> installer output
    _t="$WORK/wsl-$2"; mkdir -p "$_t"
    printf '%s\n' "$1" > "$_t/osrelease"
    S="$WORK/stub-wsl-$2"; mk_stubs "$S" x86_64 systemctl
    ( set +e
      PATH="$S"; export PATH
      MC_TEST_BASE_URL="$WORK/rel-svc"; export MC_TEST_BASE_URL
      MCREMOTE_INSTALL_DIR="$WORK/bin-wsl-$2"; export MCREMOTE_INSTALL_DIR
      HOME="$WORK/home-wsl-$2"; export HOME
      XDG_CONFIG_HOME="$WORK/home-wsl-$2/.config"; export XDG_CONFIG_HOME
      unset XDG_RUNTIME_DIR
      MC_TEST_OSRELEASE="$_t/osrelease"; export MC_TEST_OSRELEASE
      "$INSTALLER" >"$WORK/out" 2>"$WORK/err" ) || true
    cat "$WORK/out" "$WORK/err" 2>/dev/null || true
}

OUT=$(wsl_advice "5.15.167.4-microsoft-standard-WSL2" wsl2)
contains "21 WSL2 gets the wsl.conf remedy" "$OUT" "systemd=true"
case "$OUT" in
    *pam_systemd*) bad "21b WSL2 must not get the su/pam_systemd advisory" \
                       "the su diagnosis is false on WSL: XDG_RUNTIME_DIR is set there" ;;
    *) ok "21b WSL2 does not get the su/pam_systemd advisory" ;;
esac

OUT=$(wsl_advice "4.4.0-26100-Microsoft" wsl1)
contains "22 WSL1 gets the upgrade-to-WSL2 advice" "$OUT" "set-version"
case "$OUT" in
    *pam_systemd*) bad "22b WSL1 must not get the su/pam_systemd advisory" "" ;;
    *) ok "22b WSL1 does not get the su/pam_systemd advisory" ;;
esac

# The advisory is still correct — and still shown — on a genuine non-WSL host.
S="$WORK/stub-native-broken"; mk_stubs "$S" x86_64 systemctl
run_installer "$S" "$WORK/rel-svc" "$WORK/bin-native-broken"
contains "22c native systemd-broken still explains su/pam_systemd" "$OUT" "pam_systemd"

# ------------------------------- 23 transient-active crash loop (MADR 0099)

printf '\n23. sustained-active requirement (MADR 0099)\n'

# With Type=simple, systemd reports `active` the moment the process is forked,
# so a daemon that dies on startup looks healthy at any single instant. The
# gate must require active to be SUSTAINED and must treat a climbing restart
# counter as decisive. Found on a real host after the first fix shipped.
S="$WORK/stub-svc-flap"; mk_stubs "$S" x86_64
mk_systemctl_flapping "$S"
run_svc "$S" "$WORK/bin-svc-flap" "$WORK/home-flap"
case "$OUT" in
    *"running, and enabled at boot"*)
        bad "23 respawning unit must not be reported as running" \
            "ActiveState=active at every sample, but NRestarts kept climbing" ;;
    *) ok "23 respawning unit is not reported as running" ;;
esac
check "23b respawning unit exits 3" "$RC" 3

# ------------------------ 24-25 systemd-broken cause attribution (0099 F8)

printf '\n24-25. systemd-broken cause attribution (MADR 0099 F8)\n'

# The advisory must name the actual cause. Two distinct situations produce
# systemd-broken, and the remedy differs:
#   PID 1 IS systemd     -> probably entered via su; reconnecting helps.
#   PID 1 is NOT systemd -> there is no user manager at all; reconnecting
#                           cannot help, and the su story is simply false.
broken_advice() { # $1 = /proc/1/comm contents, $2 = tag
    _t="$WORK/p1-$2"; mkdir -p "$_t"; printf '%s\n' "$1" > "$_t/comm"
    S="$WORK/stub-p1-$2"; mk_stubs "$S" x86_64 systemctl
    ( set +e
      PATH="$S"; export PATH
      MC_TEST_BASE_URL="$WORK/rel-svc"; export MC_TEST_BASE_URL
      MCREMOTE_INSTALL_DIR="$WORK/bin-p1-$2"; export MCREMOTE_INSTALL_DIR
      HOME="$WORK/home-p1-$2"; export HOME
      XDG_CONFIG_HOME="$WORK/home-p1-$2/.config"; export XDG_CONFIG_HOME
      unset XDG_RUNTIME_DIR
      MC_TEST_PID1COMM="$_t/comm"; export MC_TEST_PID1COMM
      "$INSTALLER" >"$WORK/out" 2>"$WORK/err" ) || true
    cat "$WORK/out" "$WORK/err" 2>/dev/null || true
}

OUT=$(broken_advice sysvinit sysv)
contains "24 non-systemd PID 1 is named in the advisory" "$OUT" "PID 1 on this host is 'sysvinit'"
case "$OUT" in
    *pam_systemd*) bad "24b non-systemd PID 1 must not be blamed on su" \
                       "no su was involved and reconnecting cannot create a user manager" ;;
    *) ok "24b non-systemd PID 1 is not blamed on su" ;;
esac

OUT=$(broken_advice systemd sd)
contains "25 systemd PID 1 still gets the su explanation" "$OUT" "pam_systemd"
case "$OUT" in
    *"PID 1 on this host is"*) bad "25b systemd PID 1 must not get the wrong-init text" "" ;;
    *) ok "25b systemd PID 1 does not get the wrong-init text" ;;
esac

# ------------------------------------------ 26-27 unit refresh on upgrade (0100)

printf '\n26-27. upgrade refreshes the unit (MADR 0100)\n'

# `update` and this installer both used to swap binaries and leave the unit
# alone, so a release whose fix lives in the unit template could not deliver it
# (0099 F4a: a relay unit that could not start on any host). The upgrade branch
# now runs `setup-service --refresh` on the NEW binary, before the restart --
# after it, the daemon would keep running the old definition until something
# else restarted it.
R="$WORK/rel-refresh"; rel="$R/latest/download"; mkdir -p "$rel"
for p in mcremote mcrelay; do
    # Release binaries that log the arguments they are invoked with.
    cat > "$rel/$p-linux-$ARCH" <<STUB
#!/bin/sh
echo "$p \$*" >> "\$MC_ARGS_LOG"
exit 0
STUB
    chmod 0755 "$rel/$p-linux-$ARCH"
done
: > "$rel/SHA256SUMS"
for p in mcremote mcrelay; do
    printf '%s  %s-linux-%s-%s\n' "$(sha_of "$rel/$p-linux-$ARCH")" "$p" "$ARCH" "$VER" >> "$rel/SHA256SUMS"
done

S="$WORK/stub-refresh"; mk_stubs "$S" x86_64 loginctl
cat > "$S/systemctl" <<'STUB'
#!/bin/sh
echo "systemctl $*" >> "$MC_ARGS_LOG"
case " $* " in
  *" is-system-running "*|*" show-environment "*) exit 0 ;;
esac
for a in "$@"; do
  case "$a" in
    --property=ActiveState) echo active; exit 0 ;;
    --property=SubState)    echo running; exit 0 ;;
    --property=NRestarts)   echo 0; exit 0 ;;
  esac
done
exit 0
STUB
chmod 0755 "$S/systemctl"

D="$WORK/bin-refresh"; H="$WORK/home-refresh"
mkdir -p "$H/.config/systemd/user" "$H/run" "$D"
printf '# existing unit\n' > "$H/.config/systemd/user/mcremote.service"
printf '# existing unit\n' > "$H/.config/systemd/user/mcrelay.service"
LOG="$WORK/args-refresh"; : > "$LOG"
( set +e
  PATH="$S"; export PATH
  HOME="$H"; export HOME
  XDG_CONFIG_HOME="$H/.config"; export XDG_CONFIG_HOME
  XDG_RUNTIME_DIR="$H/run"; export XDG_RUNTIME_DIR
  MC_TEST_BASE_URL="$R"; export MC_TEST_BASE_URL
  MCREMOTE_INSTALL_DIR="$D"; export MCREMOTE_INSTALL_DIR
  MC_ARGS_LOG="$LOG"; export MC_ARGS_LOG
  "$INSTALLER" >"$WORK/out" 2>"$WORK/err"; echo $? > "$WORK/rc" )
check "26 upgrade with refresh exits 0" "$(cat "$WORK/rc")" 0
check "26b mcremote unit refresh attempted" \
      "$(grep -c '^mcremote setup-service --refresh$' "$LOG")" 1
check "26c mcrelay unit refresh attempted too" \
      "$(grep -c '^mcrelay setup-service --refresh$' "$LOG")" 1
# The installer itself must still not rewrite the unit: --refresh decides that,
# and this fixture's unit carries no managed-by header, so it is kept.
check "26d installer does not rewrite the unit itself" \
      "$(head -1 "$H/.config/systemd/user/mcremote.service")" "# existing unit"

REF_IDX=$(grep -n 'setup-service --refresh' "$LOG" | head -1 | cut -d: -f1)
START_IDX=$(grep -n 'systemctl --user start' "$LOG" | head -1 | cut -d: -f1)
if [ -n "$REF_IDX" ] && [ -n "$START_IDX" ] && [ "$REF_IDX" -lt "$START_IDX" ]; then
    ok "27 refresh runs before the restart"
else
    bad "27 refresh runs before the restart" "refresh=$REF_IDX start=$START_IDX in $LOG"
fi

# A product without a unit must not be refreshed: there is nothing to refresh,
# and invoking the binary would only add noise to the upgrade output.
D2="$WORK/bin-refresh2"; H2="$WORK/home-refresh2"
mkdir -p "$H2/.config/systemd/user" "$H2/run" "$D2"
printf '# existing unit\n' > "$H2/.config/systemd/user/mcremote.service"
LOG2="$WORK/args-refresh2"; : > "$LOG2"
( set +e
  PATH="$S"; export PATH
  HOME="$H2"; export HOME
  XDG_CONFIG_HOME="$H2/.config"; export XDG_CONFIG_HOME
  XDG_RUNTIME_DIR="$H2/run"; export XDG_RUNTIME_DIR
  MC_TEST_BASE_URL="$R"; export MC_TEST_BASE_URL
  MCREMOTE_INSTALL_DIR="$D2"; export MCREMOTE_INSTALL_DIR
  MC_ARGS_LOG="$LOG2"; export MC_ARGS_LOG
  "$INSTALLER" >"$WORK/out" 2>"$WORK/err" )
check "27b no mcrelay unit means no mcrelay refresh" \
      "$(grep -c '^mcrelay setup-service --refresh$' "$LOG2")" 0

# ------------------------------------------------------------------ summary

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
