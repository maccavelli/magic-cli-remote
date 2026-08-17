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

S="$WORK/stub-s6";     mk_stubs "$S" x86_64 s6-svscan s6-svc
check "s6-svscan+s6-svc -> s6" "$(probe_init "$S")" "s6"

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

# ------------------------------------------------------------------ summary

printf '\n%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
