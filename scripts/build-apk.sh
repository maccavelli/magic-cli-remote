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
# Grab the dynamic version (e.g. 0.2.3.6) and split it
VER="$("$ROOT/scripts/next-build-version.sh" | tail -1)"
# Full four-part version, matching `make apk` and CI (MADR 0128 D2). This script
# previously split it to three parts, and `make apk` inherited that when it was
# taught to stamp at all — so both local paths disagreed with CI's stated intent
# that the APK and the binaries in a release carry the same version.
BUILD_NAME="$VER"
BUILD_NUMBER="${VER##*.}"

if [[ "$BUILD_NAME" =~ ^[0-9]+\.[0-9]+\.[0-9]+(\.[0-9]+)?$ && "$BUILD_NUMBER" =~ ^[0-9]+$ ]]; then
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
