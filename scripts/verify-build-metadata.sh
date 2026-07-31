#!/usr/bin/env bash
# verify-build-metadata.sh — assert Darwin/Linux build-tag policy (MADR 0059 D9).
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
build_one darwin arm64 "" "$tmpdir/mcremote-darwin"
build_one darwin amd64 "" "$tmpdir/mcremote-darwin-amd64"

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
meta_linux="$(go version -m "$tmpdir/mcremote-linux")"
echo "$meta_linux" | grep -q netgo || { echo "error: linux missing netgo"; echo "$meta_linux"; exit 1; }
echo "$meta_linux" | grep -q osusergo || { echo "error: linux missing osusergo"; echo "$meta_linux"; exit 1; }

echo "Checking Darwin has no netgo/osusergo…"
for bin in "$tmpdir/mcremote-darwin" "$tmpdir/mcremote-darwin-amd64"; do
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

echo "verify-build-metadata: OK"
