#!/usr/bin/env bash
# Prove ci.yml's unversioned-alias loop handles an extension-bearing asset.
#
# MADR 0116 F17: the pre-0116 glob was `mcremote-*-"${VER}"`, which requires the
# filename to END in ${VER}. A Windows asset ends in .exe, so it never matched
# and no alias was published — a 404 for any installer building a URL from the
# platform alone. This fixture pins the corrected loop.
set -euo pipefail

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
cd "$tmpdir"

VER="0.14.10.1"
: > "mcremote-linux-amd64-${VER}"
: > "mcremote-linux-arm64-${VER}"
: > "mcremote-darwin-arm64-${VER}"
: > "mcremote-windows-amd64-${VER}.exe"
: > "mcrelay-windows-amd64-${VER}.exe"
: > "SHA256SUMS-${VER}"

# --- the loop, copied from ci.yml ---
for f in mcremote-*-"${VER}"* mcrelay-*-"${VER}"*; do
    [ -f "$f" ] || continue
    case "$f" in
        SHA256SUMS*|*.apk) continue ;;
    esac
    base="${f%%-"${VER}"*}"
    ext="${f#*-"${VER}"}"
    cp -f "$f" "${base}${ext}"
done
# --- end copied loop ---

fail=0
for want in mcremote-linux-amd64 mcremote-linux-arm64 mcremote-darwin-arm64 \
            mcremote-windows-amd64.exe mcrelay-windows-amd64.exe; do
    if [ -f "$want" ]; then
        echo "ok: alias $want"
    else
        echo "error: missing alias $want" >&2
        fail=1
    fi
done

# The manifest must NOT acquire an alias.
if [ -f "SHA256SUMS" ]; then
    echo "error: SHA256SUMS was aliased; it must stay versioned-only" >&2
    fail=1
fi

[ "$fail" -eq 0 ] || exit 1
echo "alias-loop-check: OK"
