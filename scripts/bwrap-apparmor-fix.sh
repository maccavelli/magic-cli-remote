#!/bin/sh
# Let bubblewrap build its sandbox, so codex `auto`/`workspace-write` can write.
#
# Problem (MADR 0048): on Ubuntu with AppArmor's unprivileged-userns restriction
# enabled, a process that creates a user namespace is transitioned into the
# `unprivileged_userns` profile. That profile denies CAP_SYS_ADMIN, which bwrap
# needs to set up its mounts:
#
#   apparmor="DENIED" operation="capable" profile="unprivileged_userns" \
#     comm="bwrap" capability=21 capname="sys_admin"
#
# The mcremote daemon runs under that profile already (check with
# `aa-status | grep unprivileged_userns`), and its agent children inherit it,
# so every workspace write in auto mode fails.
#
# Why not a local profile override: /etc/apparmor.d/unprivileged_userns contains
# `audit deny capability,`, and in AppArmor an explicit deny always beats a
# later allow — precedence is not order-based. Dropping `allow capability,` into
# local/unprivileged_userns is therefore inert. An earlier version of this
# script did exactly that and reported success; it never worked.
#
# What this does instead: turns off the restriction so nothing transitions into
# that profile. Verified against codex-cli 0.145.0.
#
# SECURITY: this is a host-wide loosening — it restores pre-24.04 behaviour,
# where any unprivileged user can create user namespaces. Undo with:
#   sudo rm /etc/sysctl.d/60-mcremote-userns.conf
#   sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=1
set -eu

KEY="kernel.apparmor_restrict_unprivileged_userns"
DROPIN="/etc/sysctl.d/60-mcremote-userns.conf"

# The check that matters, and it is fiddly — two obvious probes are both wrong:
#
#   bwrap --unshare-user ...          passes either way. bwrap ships its own
#                                     AppArmor profiles (`bwrap`, `unpriv_bwrap`)
#                                     so it never transitions into the
#                                     restrictive one. Codex fails because it
#                                     reaches bwrap through node, which does.
#   aa-exec -p unprivileged_userns    fails either way, by design: it forces the
#                                     profile, and the profile denies
#                                     CAP_SYS_ADMIN even after this fix. The fix
#                                     stops anything *entering* the profile.
#
# `unshare -Ur` is an unprofiled binary creating a user namespace and claiming
# capabilities in it — the same shape as node→bwrap — and it tracks the sysctl
# exactly (verified both directions against codex-cli 0.145.0).
probe() {
    unshare -Ur true >/dev/null 2>&1
}

for tool in unshare sysctl; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "error: $tool not found" >&2
        exit 2
    }
done

sysctl -n "$KEY" >/dev/null 2>&1 ||
    echo "note: $KEY absent on this kernel; the restriction may not apply here."

if probe; then
    echo "Already working — unprivileged user namespaces are usable."
    echo "Nothing to do."
    exit 0
fi
echo "Confirmed: unprivileged user namespaces are restricted."
echo "Recent denial, if any:"
(dmesg 2>/dev/null || sudo dmesg 2>/dev/null || true) |
    grep -i 'apparmor="DENIED".*unprivileged_userns' | tail -1 | sed 's/^/    /'

echo "Applying $KEY=0 for this boot"
sudo sysctl -w "$KEY=0" >/dev/null

echo "Persisting to $DROPIN"
printf '# mcremote: allow bubblewrap to sandbox agent processes (MADR 0048)\n%s = 0\n' \
    "$KEY" | sudo tee "$DROPIN" >/dev/null

echo "Verifying"
if probe; then
    echo "OK — unprivileged user namespaces are usable."
    echo
    echo "Agent sessions already running were confined by the old policy;"
    echo "restart the daemon so they pick this up:"
    echo "    systemctl --user restart mcremote"
    exit 0
fi

echo "FAIL — still blocked. Diagnostic:" >&2
unshare -Ur true 2>&1 | sed 's/^/    /' >&2
echo >&2
echo "Check: sysctl $KEY" >&2
echo "       sudo dmesg | grep -i 'apparmor.*DENIED'" >&2
exit 1
