#!/usr/bin/env bash
# Assert an Android APK was produced by Flutter/Android *release* mode.
#
# Used by CI tag builds and local `make apk` / scripts/build-apk.sh so we never
# publish debug or profile artifacts as a release.
#
# Checks (fail closed):
#   1. File exists and is a ZIP/APK
#   2. Basename is not *debug* / *profile* (e.g. app-debug.apk, app-profile.apk)
#   3. Contains AOT libapp.so (release/profile); rejects JIT debug snapshots
#   4. Does not contain vm_snapshot_data / isolate_snapshot_data (debug)
#   5. If aapt is available: android:debuggable must not be true
#      (rejects profile builds, which keep debuggable for DevTools).
#      Set ASSERT_REQUIRE_AAPT=1 to fail when aapt is missing (CI android-apk job).
#   6. Optional: GRADLE_METADATA=path/to/output-metadata.json → variantName=release
#
# Usage:
#   scripts/assert-flutter-release-apk.sh path/to.apk
#   GRADLE_METADATA=apps/mobile/build/app/outputs/apk/release/output-metadata.json \
#     scripts/assert-flutter-release-apk.sh apps/mobile/build/app/outputs/flutter-apk/app-release.apk
set -euo pipefail

usage() {
  echo "usage: $0 <apk-path>" >&2
  exit 2
}

[[ $# -eq 1 ]] || usage
APK=$1

die() {
  echo "error: $*" >&2
  exit 1
}

info() {
  echo "assert-flutter-release-apk: $*"
}

[[ -f "$APK" ]] || die "APK not found: $APK"
[[ -s "$APK" ]] || die "APK is empty: $APK"

# ZIP local file header magic (APK is a ZIP).
header=$(od -An -tx1 -N4 "$APK" | tr -d ' \n')
[[ "$header" == "504b0304" ]] || die "not a ZIP/APK (bad magic): $APK"

base=$(basename "$APK")
# Reject Flutter's mode-named outputs even if someone re-stages under dist/.
if [[ "$base" =~ [Dd]ebug ]] || [[ "$base" =~ [Pp]rofile ]]; then
  die "basename looks like a non-release build: $base (need release mode)"
fi

# Listing once for content checks.
listing=$(unzip -Z1 "$APK" 2>/dev/null) || die "cannot list APK (corrupt?): $APK"

echo "$listing" | grep -q 'lib/.*/libapp\.so$' \
  || die "missing lib/*/libapp.so (AOT). Debug/JIT builds lack this; expected Flutter release APK"

if echo "$listing" | grep -qE 'assets/flutter_assets/(vm_snapshot_data|isolate_snapshot_data)$'; then
  die "found Dart JIT snapshot assets (vm_snapshot_data/isolate_snapshot_data) — this is a debug build"
fi

# Optional Gradle metadata written next to assembleRelease output.
if [[ -n "${GRADLE_METADATA:-}" ]]; then
  [[ -f "$GRADLE_METADATA" ]] || die "GRADLE_METADATA not found: $GRADLE_METADATA"
  if command -v python3 >/dev/null 2>&1; then
    variant=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("variantName",""))' "$GRADLE_METADATA")
  else
    # Minimal fallback: require the literal "release" variant field.
    variant=$(grep -o '"variantName"[[:space:]]*:[[:space:]]*"[^"]*"' "$GRADLE_METADATA" | head -1 | sed 's/.*"\([^"]*\)"$/\1/')
  fi
  [[ "$variant" == "release" ]] || die "Gradle variantName is '$variant' (want release) in $GRADLE_METADATA"
  info "Gradle variantName=release ($GRADLE_METADATA)"
fi

find_aapt() {
  if command -v aapt >/dev/null 2>&1; then
    command -v aapt
    return 0
  fi
  local root="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  if [[ -n "$root" && -d "$root/build-tools" ]]; then
    # Prefer newest build-tools.
    local candidate
    candidate=$(ls -1d "$root"/build-tools/*/aapt 2>/dev/null | sort -V | tail -1 || true)
    if [[ -n "$candidate" && -x "$candidate" ]]; then
      echo "$candidate"
      return 0
    fi
  fi
  return 1
}

aapt_bin=""
if aapt_bin=$(find_aapt); then
  # aapt dump xmltree prints android:debuggable=(type 0x12)0xffffffff when true.
  xml=$("$aapt_bin" dump xmltree "$APK" AndroidManifest.xml 2>/dev/null) \
    || die "aapt failed to dump AndroidManifest.xml from $APK"
  if echo "$xml" | grep -E 'android:debuggable\(0x0101000f\)=\(type 0x12\)0xffffffff' >/dev/null; then
    die "android:debuggable=true — debug or profile APK, not a release build"
  fi
  # Also reject explicit true in badging if present.
  if "$aapt_bin" dump badging "$APK" 2>/dev/null | grep -qi 'application-debuggable'; then
    die "aapt badging reports application-debuggable"
  fi
  info "aapt: android:debuggable is not set (release-compatible)"
else
  # Profile vs release both use AOT (libapp.so); only debuggable separates them.
  # The android-apk job sets ASSERT_REQUIRE_AAPT=1 (SDK present). Publish jobs
  # re-check content without aapt; they already consumed an aapt-gated artifact.
  if [[ "${ASSERT_REQUIRE_AAPT:-}" == "1" ]]; then
    die "aapt not found (set ANDROID_HOME or install build-tools). Cannot distinguish profile from release without it."
  fi
  info "aapt not found — skipped debuggable check (set ASSERT_REQUIRE_AAPT=1 to require it)"
fi

info "OK release-mode APK: $APK ($(du -h "$APK" | awk '{print $1}'))"
