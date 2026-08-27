#!/usr/bin/env bash
# Prove the D21 artifact gate actually rejects a cgo-linked binary.
#
# A gate nobody has seen reject anything is not known to be a gate. This builds
# one deliberately cgo-linked binary, runs the same assertion the release job
# runs, and FAILS if the assertion passes.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# The exact predicate used by ci.yml and verify-build-metadata.sh.
is_cgo_free() {
    go version -m "$1" | grep -q '^	build	CGO_ENABLED=0$'
}

echo "building a deliberately cgo-linked binary…"
CGO_ENABLED=1 go build -o "$tmpdir/mcremote-cgo" ./cmd/mcremote

echo "building a pure-Go binary…"
CGO_ENABLED=0 go build -o "$tmpdir/mcremote-pure" ./cmd/mcremote

if is_cgo_free "$tmpdir/mcremote-cgo"; then
    echo "error: the cgo gate ACCEPTED a CGO_ENABLED=1 binary — it is not a gate" >&2
    go version -m "$tmpdir/mcremote-cgo" | grep -i cgo >&2 || true
    exit 1
fi
echo "ok: gate rejects a cgo binary"

if ! is_cgo_free "$tmpdir/mcremote-pure"; then
    echo "error: the cgo gate REJECTED a pure-Go binary — it is too strict" >&2
    go version -m "$tmpdir/mcremote-pure" | grep -i cgo >&2 || true
    exit 1
fi
echo "ok: gate accepts a pure-Go binary"

echo "cgo-gate-check: OK"
