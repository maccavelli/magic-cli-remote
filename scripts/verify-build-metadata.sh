#!/usr/bin/env bash
# verify-build-metadata.sh — assert build-tag and cgo policy across every
# published target (MADR 0059 D9, MADR 0116 D2/D21).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

build_one() {
  local goos="$1" goarch="$2" tags="$3" out="$4"
  local args=(-trimpath -o "$out")
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build "${args[@]}" ./cmd/mcremote
}

echo "Building probe binaries…"
build_one linux amd64 "netgo,osusergo" "$tmpdir/mcremote-linux"
build_one linux arm64 "netgo,osusergo" "$tmpdir/mcremote-linux-arm64"
build_one darwin arm64 "" "$tmpdir/mcremote-darwin"
build_one windows amd64 "" "$tmpdir/mcremote-windows-amd64.exe"
# NOTE: windows/arm64 is deliberately absent (MADR 0116 D19). An unbuilt target
# must not acquire a policy assertion that implies it ships.

check_has_tag() {
  local bin="$1" tag="$2"
  if ! go version -m "$bin" | grep -q "$tag"; then
    echo "error: $bin missing expected tag $tag" >&2
    go version -m "$bin" >&2 || true
    exit 1
  fi
}

check_lacks_tag() {
  local bin="$1" tag="$2"
  if go version -m "$bin" | grep -qE "(^|[[:space:]])${tag}([[:space:]]|$)"; then
    # go version -m lists build tags under a build line; match carefully.
    if go version -m "$bin" | grep -E '^\s+build\s+.*-tags=|^\s+build\s+tags=' | grep -q "$tag"; then
      echo "error: $bin unexpectedly has tag $tag" >&2
      go version -m "$bin" >&2 || true
      exit 1
    fi
  fi
}

echo "Checking Linux tags…"
for bin in "$tmpdir/mcremote-linux" "$tmpdir/mcremote-linux-arm64"; do
  meta_linux="$(go version -m "$bin")"
  echo "$meta_linux" | grep -q netgo || { echo "error: $bin missing netgo"; echo "$meta_linux"; exit 1; }
  echo "$meta_linux" | grep -q osusergo || { echo "error: $bin missing osusergo"; echo "$meta_linux"; exit 1; }
done

echo "Checking Darwin and Windows have no netgo/osusergo…"
for bin in "$tmpdir/mcremote-darwin" "$tmpdir/mcremote-windows-amd64.exe"; do
  meta="$(go version -m "$bin")"
  if echo "$meta" | grep -E 'build.*tags=.*netgo|build.*-tags=.*netgo' >/dev/null; then
    echo "error: $bin has netgo" >&2
    echo "$meta" >&2
    exit 1
  fi
  if echo "$meta" | grep -E 'build.*tags=.*osusergo|build.*-tags=.*osusergo' >/dev/null; then
    echo "error: $bin has osusergo" >&2
    echo "$meta" >&2
    exit 1
  fi
done

# MADR 0116 D21 / F22. The tag assertions above prove which net and user
# implementation is linked; this proves cgo is off at all. `go version -m` reads
# the binary's own build settings, so it is authoritative regardless of how the
# binary was produced.
echo "Checking every target is CGO_ENABLED=0…"
for bin in "$tmpdir"/mcremote-*; do
  if ! go version -m "$bin" | grep -q '^	build	CGO_ENABLED=0$'; then
    echo "error: $bin is not CGO_ENABLED=0" >&2
    go version -m "$bin" | grep -i cgo >&2 || true
    exit 1
  fi
done

echo "verify-build-metadata: OK"
