#!/usr/bin/env bash
# Self-check for install-binary.sh.
#
# The regression: `make install` stopped mcremote, skipped its own restart, and
# exited 0 — leaving the machine with no daemon until someone noticed. launchd
# never failed to start it; it was never asked to.
#
# The cause was a race, and the racing window is launchd's, not ours: after
# `bootout` returns, `launchctl print` keeps reporting
#
#     state = SIGTERMed
#     pid   = 86498
#
# for as long as the service takes to exit (mcremote closes sessions and agent
# engines first, so ~140ms and up). The old `unit_active` counted any pid as
# "up", so the restart guard read a corpse as a healthy service and skipped
# `launchctl bootstrap` entirely.
#
# `launchctl` is stubbed rather than driven for real: a live LaunchAgent cannot
# reproduce this deterministically — `bootout` happens to block there, which
# hides the window — and a test that only passes when the timing cooperates is
# worse than none. The stub replays the exact sequence the incident's launchd
# log recorded.
#
# HERMETIC, and it must stay so (MADR 0170 F1/F2, D2). The first version of
# this test stubbed launchctl and HOME but left the real systemctl on PATH.
# `systemctl --user` ignores HOME — it reaches the real user manager through
# XDG_RUNTIME_DIR — so on a Linux host with a healthy manager the script under
# test stopped and restarted the developer's LIVE mcremote.service, and the
# test's verdict depended on the host (it passed on macOS and on a degraded
# manager, failed on a healthy one). Every run below therefore gets a PATH made
# ONLY of a stub directory, XDG_RUNTIME_DIR points into the temp dir, and the
# D-Bus address is unset; `assert_hermetic` refuses to run otherwise.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/install-binary.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

label="com.magiccliremote.mcremote"
unit="mcremote"

# Real tools the script under test and this harness use, linked into each stub
# dir so PATH can be that dir alone.
link_tools() { # $1 = dir
	local t p
	for t in bash sh env install mv rm cat grep seq id mkdir chmod touch cmp printf; do
		p=$(command -v "$t" 2>/dev/null) || continue
		case "$p" in /*) ln -sf "$p" "$1/$t" ;; esac
	done
	# `sleep` is stubbed so the polls do not add real seconds.
	printf '#!/bin/sh\nexit 0\n' >"$1/sleep"
	chmod +x "$1/sleep"
}

# ---------------------------------------------------------------- darwin stubs
darwin_bin="$tmp/bin-darwin"
mkdir -p "$darwin_bin"
link_tools "$darwin_bin"

# detect_service only takes the LaunchAgent path when the plist exists.
fake_home="$tmp/home"
mkdir -p "$fake_home/Library/LaunchAgents"
touch "$fake_home/Library/LaunchAgents/${label}.plist"

# A launchctl that reproduces the incident:
#   * bootout returns immediately, before the service is gone
#   * print then reports SIGTERMed WITH a live pid (the trap in the window)
#   * only after DYING_PRINTS calls does the service disappear
cat >"$darwin_bin/launchctl" <<'EOF'
#!/usr/bin/env bash
state="$LAUNCHCTL_STATE"
log="$LAUNCHCTL_CALLS"
echo "$1" >>"$log"
case "$1" in
print)
	if [ -f "$state/up" ]; then
		printf '\tstate = running\n\tpid = 222\n'
		exit 0
	fi
	if [ -f "$state/booted_out" ]; then
		n=$(cat "$state/prints" 2>/dev/null || echo 0)
		n=$((n + 1))
		echo "$n" >"$state/prints"
		if [ "$n" -le "${DYING_PRINTS:-6}" ]; then
			# The window: signalled, not yet reaped.
			printf '\tstate = SIGTERMed\n\tpid = 111\n'
			exit 0
		fi
		exit 1 # finally removed from the domain
	fi
	printf '\tstate = running\n\tpid = 111\n'
	;;
bootout)
	touch "$state/booted_out"
	rm -f "$state/up"
	;;
bootstrap) touch "$state/up" ;;
kickstart) touch "$state/up" ;;
enable) ;;
esac
exit 0
EOF
chmod +x "$darwin_bin/launchctl"

# ----------------------------------------------------------------- linux stubs
linux_bin="$tmp/bin-linux"
mkdir -p "$linux_bin"
link_tools "$linux_bin"

# A systemd user manager: MANAGER_STATE is what `is-system-running` prints
# (running exits 0; degraded, like any other state, exits 1 — measured on
# systemd 255 and 259). The unit's state is two files, active and enabled.
cat >"$linux_bin/systemctl" <<'EOF'
#!/usr/bin/env bash
state="$SYSTEMCTL_STATE"
[ "$1" = --user ] || { echo "stub systemctl: only --user is expected" >&2; exit 90; }
shift
echo "$1" >>"$SYSTEMCTL_CALLS"
case "$1" in
is-system-running)
	echo "$MANAGER_STATE"
	[ "$MANAGER_STATE" = running ]
	;;
show-environment) echo "HOME=$HOME" ;;
is-active) [ -f "$state/active" ] || exit 3 ;;
is-enabled) [ -f "$state/enabled" ] || exit 1 ;;
stop) rm -f "$state/active" ;;
start) touch "$state/active" ;;
*) echo "stub systemctl: unexpected $1" >&2; exit 91 ;;
esac
EOF
chmod +x "$linux_bin/systemctl"

# The PATH each run gets. The guard below checks these same variables, so
# widening a run's PATH trips it before anything runs.
darwin_path="$darwin_bin"
linux_path="$linux_bin"

# Refuse to run unless each run's PATH can reach only the stubs. A service
# manager found anywhere else would be the host's real one.
assert_hermetic() { # $1 = the PATH a run will use, $2 = its stub dir
	local t p
	for t in systemctl launchctl; do
		p=$(PATH="$1" command -v "$t" 2>/dev/null) || continue
		case "$p" in
		"$2"/*) ;;
		*)
			echo "FATAL not hermetic: $t resolves to $p, outside $2" >&2
			exit 2
			;;
		esac
	done
}
assert_hermetic "$darwin_path" "$darwin_bin"
assert_hermetic "$linux_path" "$linux_bin"

prepare() {
	rm -rf "$tmp/state"
	mkdir -p "$tmp/state"
	: >"$tmp/calls"
	printf '#!/bin/sh\nexit 0\n' >"$tmp/payload"
	chmod +x "$tmp/payload"
	printf '#!/bin/sh\nexit 1\n' >"$tmp/dest"
}

run_darwin() { # $1 = DYING_PRINTS
	prepare
	env -i PATH="$darwin_path" HOME="$fake_home" \
		LAUNCHCTL_STATE="$tmp/state" LAUNCHCTL_CALLS="$tmp/calls" \
		DYING_PRINTS="${1:-6}" \
		"$darwin_bin/bash" "$script" "$tmp/payload" "$tmp/dest" "$unit" >"$tmp/out" 2>&1
}

run_linux() { # $1 = manager state, $2 = active|inactive, $3 = enabled|disabled
	prepare
	[ "$2" = active ] && touch "$tmp/state/active"
	[ "$3" = enabled ] && touch "$tmp/state/enabled"
	mkdir -p "$tmp/run"
	env -i PATH="$linux_path" HOME="$tmp/home-linux" XDG_RUNTIME_DIR="$tmp/run" \
		SYSTEMCTL_STATE="$tmp/state" SYSTEMCTL_CALLS="$tmp/calls" MANAGER_STATE="$1" \
		"$linux_bin/bash" "$script" "$tmp/payload" "$tmp/dest" "$unit" >"$tmp/out" 2>&1
}

fail() {
	echo "FAIL $1"
	echo "--- install output ---"
	cat "$tmp/out" 2>/dev/null || true
	echo "--- service-manager calls ---"
	cat "$tmp/calls" 2>/dev/null || true
	exit 1
}

called() { grep -qx "$1" "$tmp/calls"; }

swapped() {
	[ -x "$tmp/dest" ] || fail "$1: destination binary missing after install"
	cmp -s "$tmp/payload" "$tmp/dest" || fail "$1: destination was not replaced"
}

# ------------------------------------------------------------------- darwin

# D1. The regression itself: the service is mid-teardown when the trap checks,
#     and the install must still bring it back.
run_darwin 6 || fail "D1 install-binary.sh exited non-zero"
called bootout || fail "D1 the service was never stopped"
called bootstrap ||
	fail "D1 no bootstrap: the restart was skipped while the service was dying \
(this is the regression — the install exits 0 with the daemon down)"
[ -f "$tmp/state/up" ] || fail "D1 the service was left down after install"
swapped D1

# D2. A service that lingers longer than the teardown poll must still be
#     started, not silently abandoned.
run_darwin 999 || fail "D2 install-binary.sh exited non-zero on a slow teardown"
called bootstrap || fail "D2 a slow teardown must not cancel the restart"
swapped D2

# -------------------------------------------------------------------- linux

# L1. Healthy manager, running unit: stopped for the swap, started after.
run_linux running active enabled || fail "L1 install-binary.sh exited non-zero"
called stop || fail "L1 a running unit was not stopped"
called start || fail "L1 a running unit was not started again"
[ -f "$tmp/state/active" ] || fail "L1 the unit was left down after install"
swapped L1

# L2. A DEGRADED manager (one failed unrelated user unit) is still a working
#     manager. The old detection required is-system-running to exit 0, so the
#     install swapped the binary, left the old process running, and exited 0
#     (MADR 0170 F3, driven on a real degraded manager).
run_linux degraded active enabled || fail "L2 install-binary.sh exited non-zero"
called stop || fail "L2 degraded manager: the running unit was not stopped (F13)"
called start || fail "L2 degraded manager: the unit was not restarted (F13)"
[ -f "$tmp/state/active" ] || fail "L2 the unit was left down after install"
swapped L2

# L3. Enabled but stopped (stranded by an earlier failed install): healed.
run_linux running inactive enabled || fail "L3 install-binary.sh exited non-zero"
called stop && fail "L3 a stopped unit must not be stopped again"
called start || fail "L3 an enabled, stopped unit was not healed"
swapped L3

# L4. Disabled and stopped: left alone.
run_linux running inactive disabled || fail "L4 install-binary.sh exited non-zero"
called stop && fail "L4 a disabled unit must not be stopped"
called start && fail "L4 a disabled unit must not be started"
swapped L4

echo "ok install-binary.sh: darwin D1-D2 and linux L1-L4 (hermetic: stub-only PATH)"
