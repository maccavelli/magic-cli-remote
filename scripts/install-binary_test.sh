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
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/install-binary.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

label="com.magiccliremote.mcremote"
# detect_service only takes the LaunchAgent path when the plist exists.
fake_home="$tmp/home"
mkdir -p "$fake_home/Library/LaunchAgents"
touch "$fake_home/Library/LaunchAgents/${label}.plist"

# A launchctl that reproduces the incident:
#   * bootout returns immediately, before the service is gone
#   * print then reports SIGTERMed WITH a live pid (the trap in the window)
#   * only after DYING_PRINTS calls does the service disappear
mkdir -p "$tmp/bin"
cat >"$tmp/bin/launchctl" <<'EOF'
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
chmod +x "$tmp/bin/launchctl"

# `sleep` is stubbed so wait_for_teardown's poll does not add real seconds.
cat >"$tmp/bin/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$tmp/bin/sleep"

run_install() {
	rm -rf "$tmp/state"
	mkdir -p "$tmp/state"
	: >"$tmp/calls"
	printf '#!/bin/sh\nexit 0\n' >"$tmp/payload"
	chmod +x "$tmp/payload"
	printf '#!/bin/sh\nexit 1\n' >"$tmp/dest"
	env PATH="$tmp/bin:$PATH" HOME="$fake_home" \
		LAUNCHCTL_STATE="$tmp/state" LAUNCHCTL_CALLS="$tmp/calls" \
		DYING_PRINTS="${1:-6}" \
		"$script" "$tmp/payload" "$tmp/dest" mcremote >"$tmp/out" 2>&1
}

fail() {
	echo "FAIL $1"
	echo "--- install output ---"
	cat "$tmp/out" 2>/dev/null || true
	echo "--- launchctl calls ---"
	cat "$tmp/calls" 2>/dev/null || true
	exit 1
}

# 1. The regression itself: the service is mid-teardown when the trap checks,
#    and the install must still bring it back.
run_install 6 || fail "install-binary.sh exited non-zero"
grep -q '^bootout$' "$tmp/calls" || fail "the service was never stopped"
grep -q '^bootstrap$' "$tmp/calls" ||
	fail "no bootstrap: the restart was skipped while the service was dying \
(this is the regression — the install exits 0 with the daemon down)"
[ -f "$tmp/state/up" ] || fail "the service was left down after install"

# 2. A service that lingers longer than the teardown poll must still be
#    started, not silently abandoned.
run_install 999 || fail "install-binary.sh exited non-zero on a slow teardown"
grep -q '^bootstrap$' "$tmp/calls" ||
	fail "a slow teardown must not cancel the restart"

# 3. The binary swap still happened.
[ -x "$tmp/dest" ] || fail "destination binary missing after install"
cmp -s "$tmp/payload" "$tmp/dest" || fail "destination was not replaced"

echo "ok install-binary.sh restarts a service that is still dying when the trap runs"
