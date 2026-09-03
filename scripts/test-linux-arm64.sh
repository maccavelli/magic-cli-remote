#!/usr/bin/env bash
# Run the Go suite on native linux/arm64 from an Apple Silicon host.
#
# No emulation: on Apple Silicon the Linux VM is itself aarch64, so the
# container runs at native speed. If this ever starts needing QEMU, the host is
# not arm64 and the result is not the thing we wanted to test (MADR 0116 D18).
#
# This mirrors the ubuntu-24.04-arm CI leg exactly: same architecture, same cgo
# posture, same command. Race coverage for this code is the darwin gate, not
# this script (MADR 0116 D20) — -race would force CGO_ENABLED=1 off darwin.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ "$(uname -m)" != "arm64" ] && [ "$(uname -m)" != "aarch64" ]; then
    echo "host is $(uname -m), not arm64; results would be emulated" >&2
    exit 1
fi

if ! docker info >/dev/null 2>&1; then
    cat >&2 <<'MSG'
no docker daemon.

Start a native aarch64 one (free, Lima-based):
    colima start --arch aarch64 --vm-type vz

Docker Desktop with the default VM works too, as long as it is arm64.
MSG
    exit 1
fi

GO_IMAGE="${GO_IMAGE:-golang:1.26.6}"
echo "running the suite on native linux/arm64 in $GO_IMAGE …"

# -e CGO_ENABLED=0 is REQUIRED, not tidy: the golang images default to
# CGO_ENABLED=1, so inheriting the image default would silently break contract
# C7 and test a libc-backed net/os-user stack the shipped binary never uses.
exec docker run --rm --platform linux/arm64 \
    -v "$ROOT":/src -w /src \
    -e CGO_ENABLED=0 \
    -e GOFLAGS= \
    "$GO_IMAGE" \
    go test ./...
