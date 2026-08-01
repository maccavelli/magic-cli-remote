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
		# Non-zero PID in launchctl print means running; best-effort.
		launchctl print "${domain}/${label}" 2>/dev/null | grep -q 'state = running' ||
			launchctl print "${domain}/${label}" 2>/dev/null | grep -Eq 'pid = [1-9]'
		;;
	*) return 1 ;;
	esac
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
	if [ "$want_up" = 1 ] && ! unit_active; then
		start_service
	fi
	exit "$rc"
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
