#!/usr/bin/env bash
# Diff the shipped Android permission / exported-component surface against a
# committed allowlist.
#
# MADR 0126 D4. Nothing else in this repository reads the MERGED manifest:
# `flutter analyze` and every Dart test are blind to it, and `lintVitalRelease`
# is a manifest-structure gate, not a surface gate. That is how the app came to
# ship WAKE_LOCK, RECEIVE_BOOT_COMPLETED, VIBRATE and an exported plugin
# receiver that appear nowhere in this repository (0126 F3) — every one of them
# injected by a plugin's own manifest at merge time.
#
# Fails on ADDITIONS and on REMOVALS. A removal matters too: losing
# POST_NOTIFICATIONS to a plugin bump would silently break alerts.
#
# The allowlist is edited by a human with a reason (0126 C2). Never regenerate
# it to clear a red build — that turns the gate into a rubber stamp.
#
# Usage:
#   scripts/assert-android-manifest-surface.sh [merged-AndroidManifest.xml]
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
default_manifest="$root/apps/mobile/build/app/intermediates/merged_manifests/release/processReleaseManifest/AndroidManifest.xml"
manifest="${1:-$default_manifest}"
allow="${MC_MANIFEST_ALLOW:-$root/apps/mobile/android/manifest-surface.allow}"

if [ ! -f "$manifest" ]; then
  echo "assert-android-manifest-surface: merged manifest not found:" >&2
  echo "  $manifest" >&2
  echo "Generate it first, e.g.:" >&2
  echo "  cd apps/mobile && flutter build apk --config-only --release --target-platform android-arm64" >&2
  echo "  cd apps/mobile/android && ./gradlew :app:processReleaseManifest" >&2
  exit 2
fi
if [ ! -f "$allow" ]; then
  echo "assert-android-manifest-surface: allowlist not found: $allow" >&2
  exit 2
fi

actual="$(python3 - "$manifest" <<'PY'
import sys, xml.etree.ElementTree as ET
A = '{http://schemas.android.com/apk/res/android}'
root = ET.parse(sys.argv[1]).getroot()
out = []
for e in root.findall('uses-permission'):
    out.append('permission %s' % e.get(A + 'name'))
app = root.find('application')
if app is not None:
    for tag in ('activity', 'activity-alias', 'service', 'receiver', 'provider'):
        for e in app.findall(tag):
            if e.get(A + 'exported') == 'true':
                out.append('exported %s %s %s' % (
                    tag, e.get(A + 'name'), e.get(A + 'permission') or '-'))
print('\n'.join(sorted(set(out))))
PY
)"

expected="$(grep -vE '^\s*(#|$)' "$allow" | sort -u)"

if ! diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") \
     --label "allowlist ($allow)" --label "merged manifest"; then
  echo >&2
  echo "assert-android-manifest-surface: FAIL — the shipped Android surface does" >&2
  echo "not match the allowlist (MADR 0126 D4)." >&2
  echo >&2
  echo "A '+' line is a permission or exported component a plugin added without" >&2
  echo "review. A '-' line is one that disappeared — check nothing depended on it." >&2
  echo "Decide, then edit the allowlist deliberately. Do NOT regenerate it." >&2
  exit 1
fi

echo "assert-android-manifest-surface: OK surface matches the allowlist"
