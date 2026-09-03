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
allow="${MC_MANIFEST_ALLOW:-$root/apps/mobile/android/manifest-surface.allow}"

# The merged manifest is found by SEARCHING the release intermediates, not by
# hard-coding a path. That path is an AGP implementation detail and it moved:
# the previous default named the directory "processReleaseManifest", but the
# app-level task under AGP 9 is :app:processReleaseMainManifest, so the gate
# could never find the file and failed every tag build. A search survives the
# next rename; an exact path guarantees another silent outage.
#
# Ambiguity fails closed. Two candidate manifests mean the gate cannot know
# which one ships, and guessing is how a surface check becomes a rubber stamp.
# Search the APP module's intermediates only, and only the output of the main
# manifest-merge task. Observed layout under AGP 9:
#   build/app/intermediates/merged_manifest/release/processReleaseMainManifest/AndroidManifest.xml
# Note "merged_manifest" is SINGULAR here; AGP 8 used "merged_manifests". The
# sibling outputReleaseAppLinkSettings/AndroidManifest.xml is a decoy, and every
# plugin has its own copy under build/<plugin>/ — matching on the MainManifest
# task excludes both without depending on the directory name.
manifest_root="${MC_MANIFEST_ROOT:-$root/apps/mobile/build/app/intermediates}"

if [ "$#" -ge 1 ] && [ -n "${1:-}" ]; then
  manifest="$1"
else
  matches="$(find "$manifest_root" -type f -name AndroidManifest.xml -path '*release*' -path '*MainManifest*' 2>/dev/null | sort || true)"
  count="$(printf '%s' "$matches" | grep -c . || true)"
  case "$count" in
    1) manifest="$matches" ;;
    0)
      echo "assert-android-manifest-surface: merged manifest not found under:" >&2
      echo "  $manifest_root" >&2
      # Never fail blind again. Show the candidates that DO exist so the next
      # AGP layout change is diagnosable from the failure itself rather than
      # from another round of tag-and-guess.
      echo "candidate AndroidManifest.xml files under apps/mobile (max 40):" >&2
      find "$root/apps/mobile" -type f -name AndroidManifest.xml 2>/dev/null |
        grep -iE "intermediates|manifest" | head -40 | sed "s|^$root/||; s|^|  |" >&2 || true
      echo "intermediates subdirectories that exist:" >&2
      find "$root/apps/mobile/build" -maxdepth 3 -type d -name "*manifest*" 2>/dev/null |
        head -20 | sed "s|^$root/||; s|^|  |" >&2 || true
      echo "Generate it first, e.g.:" >&2
      echo "  cd apps/mobile && flutter build apk --config-only --release --target-platform android-arm64" >&2
      echo "  cd apps/mobile/android && ./gradlew :app:processReleaseMainManifest" >&2
      exit 2
      ;;
    *)
      echo "assert-android-manifest-surface: ambiguous merged manifests under:" >&2
      echo "  $manifest_root" >&2
      printf '%s\n' "$matches" | sed 's/^/  /' >&2
      echo "Refusing to guess which manifest ships." >&2
      exit 2
      ;;
  esac
fi

if [ ! -f "$manifest" ]; then
  echo "assert-android-manifest-surface: merged manifest not found:" >&2
  echo "  $manifest" >&2
  exit 2
fi
echo "assert-android-manifest-surface: reading $manifest" >&2
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
