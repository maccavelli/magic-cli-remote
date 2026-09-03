#!/usr/bin/env bash
# Build a sideloadable release APK (arm64) on low-RAM hosts.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/apps/mobile"

export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
export ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-$ANDROID_HOME}"
export PATH="$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/platform-tools:$PATH"
export JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}"

# Stop leftover daemons that may still hold huge heaps from prior failed builds.
cd android
./gradlew --stop >/dev/null 2>&1 || true
cd ..

flutter pub get
# Release identity is the immutable strict tag (MADR 0005): there is no BASE.N
# build serial to allocate any more. BUILD_NAME comes from the tag CI passes in,
# and BUILD_NUMBER stays on the CI run number so versionCode keeps increasing
# monotonically across releases. Android update behaviour is unchanged.
BUILD_NAME="${BUILD_NAME:-${VERSION:-}}"
if [[ -z "$BUILD_NAME" ]]; then
  BASE="$(git -C "$ROOT" tag -l 'v*.*.*' 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 | sed 's/^v//' || true)"
  BUILD_NAME="${BASE:-0.0.0}"
fi
BUILD_NAME="${BUILD_NAME#v}"
BUILD_NUMBER="${BUILD_NUMBER:-1}"

if [[ "$BUILD_NAME" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$BUILD_NUMBER" =~ ^[0-9]+$ ]]; then
  flutter build apk --release --target-platform android-arm64 --build-name="$BUILD_NAME" --build-number="$BUILD_NUMBER"
else
  flutter build apk --release --target-platform android-arm64
fi

APK="build/app/outputs/flutter-apk/app-release.apk"
if [[ ! -f "$APK" ]]; then
  echo "error: APK not found at $APK" >&2
  exit 1
fi

# Fail closed: refuse to stage debug/profile as a "release" APK.
export GRADLE_METADATA="${GRADLE_METADATA:-$ROOT/apps/mobile/build/app/outputs/apk/release/output-metadata.json}"
if [[ ! -f "$GRADLE_METADATA" ]]; then
  unset GRADLE_METADATA
fi
"$ROOT/scripts/assert-flutter-release-apk.sh" "$APK"

OUT_DIR="$ROOT/dist"
mkdir -p "$OUT_DIR"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
DEST="$OUT_DIR/magic-cli-remote-${STAMP}-arm64.apk"
cp -f "$APK" "$DEST"
cp -f "$APK" "$OUT_DIR/magic-cli-remote-latest-arm64.apk"

echo
echo "✓ APK ready:"
ls -lh "$DEST" "$OUT_DIR/magic-cli-remote-latest-arm64.apk"
echo
echo "Sideload:"
echo "  adb install -r $DEST"
echo "  # or copy to phone and open the file"
