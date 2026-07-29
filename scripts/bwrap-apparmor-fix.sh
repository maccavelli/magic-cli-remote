#!/bin/sh
# Allow capabilities inside the unprivileged_userns AppArmor profile, so
# bubblewrap can build its sandbox for agent processes.
#
# Why this is needed (MADR 0048): the mcremote daemon runs confined by the
# `unprivileged_userns` profile, and its agent children (codex, grok) inherit
# it. Codex reaches bwrap through node, which transitions into that profile
# when it creates a user namespace; the profile denies capabilities, so bwrap
# fails with "No permissions to create a new namespace" and every workspace
# write in `auto` mode fails with it.
#
# Verify confinement with:  aa-status | grep unprivileged_userns
#
# This LOOSENS a security boundary: it grants capabilities to unprivileged
# user namespaces host-wide. Undo with:
#   sudo rm /etc/apparmor.d/local/unprivileged_userns
#   sudo apparmor_parser -r /etc/apparmor.d/unprivileged_userns
set -eu

PROFILE="/etc/apparmor.d/unprivileged_userns"
LOCAL="/etc/apparmor.d/local/unprivileged_userns"
RULE="allow capability,"

# The check that matters. It must run *inside* the profile: an unconfined shell
# is not what breaks, and bare bwrap has its own permissive profiles (`bwrap`,
# `unpriv_bwrap`), so testing bwrap directly from a terminal passes whether or
# not this fix is applied — the previous version of this script did exactly
# that and could only ever report success.
probe() {
    aa-exec -p unprivileged_userns -- \
        bwrap --unshare-user --ro-bind / / true >/dev/null 2>&1
}

for tool in aa-exec apparmor_parser bwrap; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "error: $tool not found; is apparmor/bubblewrap installed?" >&2
        exit 2
    }
done
[ -f "$PROFILE" ] || {
    echo "error: $PROFILE not found; nothing to override" >&2
    exit 2
}

if probe; then
    echo "Already working — bwrap can create a namespace under the profile."
    echo "Nothing to do."
    exit 0
fi
echo "Confirmed: bwrap is blocked inside the unprivileged_userns profile."

echo "Writing override to $LOCAL"
printf '%s\n' "$RULE" | sudo tee "$LOCAL" >/dev/null

echo "Reloading $PROFILE"
sudo apparmor_parser -r "$PROFILE"

echo "Verifying inside the profile"
if probe; then
    echo "OK — bwrap works under unprivileged_userns."
    echo
    echo "Agent sessions already running were confined by the old policy;"
    echo "restart the daemon so they pick this up:"
    echo "    systemctl --user restart mcremote"
    exit 0
fi

# Show the real error rather than swallowing it: the remaining causes are a
# different AppArmor stack or the sysctl, and the message distinguishes them.
echo "FAIL — still blocked. Diagnostic output:" >&2
aa-exec -p unprivileged_userns -- bwrap --unshare-user --ro-bind / / true 2>&1 |
    sed 's/^/    /' >&2
echo >&2
echo "Check: sysctl kernel.apparmor_restrict_unprivileged_userns" >&2
echo "       sudo aa-status | grep unprivileged_userns" >&2
exit 1
