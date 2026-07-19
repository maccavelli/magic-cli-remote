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
# arm64-only: phones in the field; avoids multi-ABI memory pressure.
flutter build apk --release --target-platform android-arm64

APK="build/app/outputs/flutter-apk/app-release.apk"
if [[ ! -f "$APK" ]]; then
  echo "error: APK not found at $APK" >&2
  exit 1
fi

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
