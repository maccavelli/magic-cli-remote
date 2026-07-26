#!/usr/bin/env bash
# Install a freshly built binary over a possibly-running `systemctl --user` unit.
#
# usage: install-binary.sh <src> <dest> [unit-name]
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
#   2. Restore the unit from a trap, on EVERY exit path — success, error, or
#      Ctrl-C. And restore it whenever the unit is *enabled*, not merely when
#      this run stopped it, so an install already stranded by an earlier
#      failure heals instead of staying dead.
set -euo pipefail

if [ $# -lt 2 ]; then
	echo "usage: $0 <src-binary> <dest-path> [systemd-user-unit]" >&2
	exit 2
fi

src=$1
dest=$2
unit=${3:-}

new="$dest.new.$$"
prev="$dest.prev.$$"
want_up=0

# systemctl_user runs a --user subcommand, or fails quietly where systemd is
# not the init system (macOS, containers, WSL without systemd).
systemctl_user() {
	command -v systemctl >/dev/null 2>&1 || return 1
	systemctl --user "$@"
}

unit_active() {
	[ -n "$unit" ] && systemctl_user is-active --quiet "$unit.service" 2>/dev/null
}

unit_enabled() {
	[ -n "$unit" ] && systemctl_user is-enabled --quiet "$unit.service" 2>/dev/null
}

cleanup() {
	rc=$?
	rm -f "$new" "$prev" 2>/dev/null || true
	if [ "$want_up" = 1 ] && ! unit_active; then
		echo "Starting $unit.service…"
		systemctl_user start "$unit.service" || true
	fi
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# 1. Stage first — nothing has been stopped yet if this fails.
install -m 755 "$src" "$new"

# 2. Record the intended end state, then get out of the binary's way.
if unit_active; then
	want_up=1
	echo "Stopping $unit.service for install…"
	systemctl_user stop "$unit.service" || true
elif unit_enabled; then
	# Enabled but down: most likely stranded by an install that failed between
	# its stop and its restart. Say so, then bring it back via the trap.
	want_up=1
	echo "$unit.service is enabled but stopped — will start it after the swap."
fi

# 3. Atomic swap.
if [ -e "$dest" ] || [ -L "$dest" ]; then
	mv -f "$dest" "$prev"
fi
mv -f "$new" "$dest"
rm -f "$prev"

echo "Installed $dest"
