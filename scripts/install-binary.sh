#!/usr/bin/env bash
# Install a freshly built binary over a possibly-running service.
#
# usage: install-binary.sh <src> <dest> [service-name]
#
# service-name is the bare product name (mcremote / mcrelay). On Linux it is
# the systemd --user unit basename; on macOS it maps to
# com.magiccliremote.<name> LaunchAgent.
#
# Avoids ETXTBSY ("text file busy") on a running binary by staging to a temp
# path and renaming into place, and keeps the unit's up/down state intact
# across the swap.
#
# Two ordering rules matter more than they look, both learned the hard way —
# the previous inline recipe stopped mcremote and then died before its restart,
# and re-running `make install` could not recover because it only restarted a
# unit it had stopped in that same run (so a stopped unit stayed stopped):
#
#   1. Stage the new binary BEFORE stopping anything. If the build output is
#      missing, the destination is unwritable, or the disk is full, the daemon
#      never goes down at all.
#   2. Restore the service from a trap, on EVERY exit path — success, error, or
#      Ctrl-C. And restore it whenever the service is *enabled*, not merely when
#      this run stopped it, so an install already stranded by an earlier
#      failure heals instead of staying dead.
set -euo pipefail

if [ $# -lt 2 ]; then
	echo "usage: $0 <src-binary> <dest-path> [service-name]" >&2
	exit 2
fi

src=$1
dest=$2
unit=${3:-}

new="$dest.new.$$"
prev="$dest.prev.$$"
want_up=0
# linux | darwin-agent | none
svc_kind=none
label=""
plist=""
domain=""

# Map bare product name to launchd Label.
launchd_label() {
	case "$1" in
	mcremote) echo "com.magiccliremote.mcremote" ;;
	mcrelay) echo "com.magiccliremote.mcrelay" ;;
	com.*) echo "$1" ;;
	*) echo "com.magiccliremote.$1" ;;
	esac
}

systemctl_user() {
	command -v systemctl >/dev/null 2>&1 || return 1
	systemctl --user "$@"
}

detect_service() {
	[ -n "$unit" ] || return 0
	if command -v systemctl >/dev/null 2>&1 && systemctl --user is-system-running >/dev/null 2>&1; then
		svc_kind=linux
		return 0
	fi
	# macOS / no systemd: LaunchAgent only (never sudo / LaunchDaemons).
	if command -v launchctl >/dev/null 2>&1; then
		label=$(launchd_label "$unit")
		plist="${HOME}/Library/LaunchAgents/${label}.plist"
		if [ -f "$plist" ]; then
			domain="gui/$(id -u)"
			svc_kind=darwin-agent
		fi
	fi
}

unit_active() {
	case "$svc_kind" in
	linux)
		systemctl_user is-active --quiet "$unit.service" 2>/dev/null
		;;
	darwin-agent)
		# A service that has been SIGTERMed but has not exited yet is NOT
		# active for our purposes: launchd keeps reporting its pid for up to
		# 45s ("scheduling cleanup in 45 sec"), and mcremote takes well over
		# 100ms to go down because it closes sessions and agent engines first.
		# Reading that pid as "still up" is what made the restart get skipped —
		# the trap checked 37ms after bootout, saw the dying pid, and returned
		# without ever calling launchctl bootstrap. The install then exited 0
		# with the daemon dead.
		local out
		out=$(launchctl print "${domain}/${label}" 2>/dev/null) || return 1
		printf '%s' "$out" | grep -qE 'state = (running|waiting)' && return 0
		# No usable state line: fall back to a pid, but only when the service
		# has not been told to stop.
		printf '%s' "$out" | grep -qE 'state = (exited|not running)' && return 1
		printf '%s' "$out" | grep -Eq 'pid = [1-9]'
		;;
	*) return 1 ;;
	esac
}

# Block until launchd has finished tearing the service down.
#
# bootout is asynchronous. Bootstrapping into a domain that is still releasing
# the label fails ("Bootstrap failed: 5: Input/output error"), and because every
# launchctl call here is best-effort that failure would be swallowed too.
wait_for_teardown() {
	[ "$svc_kind" = darwin-agent ] || return 0
	local i
	for i in $(seq 1 100); do
		launchctl print "${domain}/${label}" >/dev/null 2>&1 || return 0
		sleep 0.1
	done
	echo "warning: ${label} did not finish tearing down; starting anyway" >&2
}

# Give the service a moment to come up before declaring the install broken.
# launchd returns from `kickstart` before the process is reported running.
wait_for_up() {
	local i
	for i in $(seq 1 50); do
		unit_active && return 0
		sleep 0.1
	done
	return 1
}

unit_enabled() {
	case "$svc_kind" in
	linux)
		systemctl_user is-enabled --quiet "$unit.service" 2>/dev/null
		;;
	darwin-agent)
		# Plist present implies we manage it; treat as enabled for heal-on-install.
		[ -f "$plist" ]
		;;
	*) return 1 ;;
	esac
}

stop_service() {
	case "$svc_kind" in
	linux)
		echo "Stopping ${unit}.service for install..."
		systemctl_user stop "$unit.service" || true
		;;
	darwin-agent)
		echo "Stopping LaunchAgent ${label} for install..."
		launchctl bootout "${domain}/${label}" 2>/dev/null || true
		;;
	esac
}

start_service() {
	case "$svc_kind" in
	linux)
		echo "Starting ${unit}.service..."
		systemctl_user start "$unit.service" || true
		;;
	darwin-agent)
		# Brace ${label}: bash 5.3 + set -u treats "$label…" (Unicode
		# ellipsis U+2026 glued to the name) as the identifier "label…",
		# which is unbound → install dies in the EXIT trap after the swap.
		echo "Starting LaunchAgent ${label}..."
		launchctl enable "${domain}/${label}" 2>/dev/null || true
		launchctl bootstrap "${domain}" "$plist" 2>/dev/null || true
		launchctl kickstart -k "${domain}/${label}" 2>/dev/null || true
		;;
	esac
}

cleanup() {
	rc=$?
	rm -f "$new" "$prev" 2>/dev/null || true
	if [ "$want_up" = 1 ]; then
		# Unconditional: the old guard (`&& ! unit_active`) was a race against
		# a shutdown this script had just initiated, and losing it meant the
		# install silently left the daemon down. Starting a service that is
		# somehow already up is harmless — `kickstart -k` restarts it, which is
		# what a binary swap wants anyway.
		wait_for_teardown
		start_service
		if ! wait_for_up; then
			echo "warning: ${unit} did not come back up after install" >&2
			echo "         start it with: $(restart_hint)" >&2
		fi
	fi
	exit "$rc"
}

restart_hint() {
	case "$svc_kind" in
	linux) echo "systemctl --user start $unit.service" ;;
	darwin-agent) echo "launchctl bootstrap ${domain} ${plist}" ;;
	*) echo "(no service manager detected)" ;;
	esac
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

detect_service

# 1. Stage first — nothing has been stopped yet if this fails.
install -m 755 "$src" "$new"

# 2. Record the intended end state, then get out of the binary's way.
if unit_active; then
	want_up=1
	stop_service
elif unit_enabled; then
	# Enabled but down: most likely stranded by an install that failed between
	# its stop and its restart. Say so, then bring it back via the trap.
	want_up=1
	echo "$unit is enabled but stopped — will start it after the swap."
fi

# 3. Atomic swap.
if [ -e "$dest" ] || [ -L "$dest" ]; then
	mv -f "$dest" "$prev"
fi
mv -f "$new" "$dest"
rm -f "$prev"

echo "Installed $dest"
