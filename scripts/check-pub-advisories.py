#!/usr/bin/env python3
"""Check every hosted package in the Flutter app's pubspec.lock against OSV (MADR 0174 D5).

    python3 scripts/check-pub-advisories.py [--lock apps/mobile/pubspec.lock]

One OSV query batch (ecosystem Pub) covers every package pub.dev hosts. SDK, path and git
packages are skipped, since they have no pub.dev version to match.

Exit: 0 no advisories · 1 at least one, each listed as package, version and advisory ID ·
2 OSV could not be reached, or the lock file could not be read (a warning: `make preflight`
does not fail offline).
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent.parent
OSV_BATCH = "https://api.osv.dev/v1/querybatch"

# A pubspec.lock package block: two-space name, then four-space fields, of which only
# `source` and `version` matter here.
BLOCK = re.compile(r"^  ([A-Za-z0-9_]+):\n((?:    .*\n)+)", re.M)


def hosted_packages(lock: str) -> list[tuple[str, str]]:
    out = []
    for name, body in BLOCK.findall(lock):
        source = re.search(r"^    source: (\S+)$", body, re.M)
        version = re.search(r'^    version: "([^"]+)"$', body, re.M)
        if source and version and source.group(1) == "hosted":
            out.append((name, version.group(1)))
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--lock", type=Path, default=ROOT / "apps/mobile/pubspec.lock")
    args = ap.parse_args()
    try:
        pkgs = hosted_packages(args.lock.read_text(encoding="utf-8"))
    except OSError as e:
        print(f"pub-advisories: cannot read {args.lock}: {e}", file=sys.stderr)
        return 2
    if not pkgs:
        print(f"pub-advisories: no hosted packages found in {args.lock}", file=sys.stderr)
        return 2
    body = {"queries": [{"package": {"ecosystem": "Pub", "name": n}, "version": v} for n, v in pkgs]}
    req = urllib.request.Request(OSV_BATCH, data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json", "User-Agent": "check-pub-advisories"})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            results = json.loads(r.read())["results"]
    except (urllib.error.URLError, OSError, ValueError, KeyError) as e:
        print(f"pub-advisories: could not query OSV ({e}); skipped.", file=sys.stderr)
        return 2
    if len(results) != len(pkgs):
        print(f"pub-advisories: OSV returned {len(results)} results for {len(pkgs)} packages; skipped.",
              file=sys.stderr)
        return 2
    hits = [(n, v, sorted(x["id"] for x in r.get("vulns", []))) for (n, v), r in zip(pkgs, results) if r.get("vulns")]
    for name, version, ids in hits:
        print(f"pub-advisories: {name} {version}: {', '.join(ids)}", file=sys.stderr)
    if hits:
        return 1
    print(f"pub-advisories: {len(pkgs)} hosted packages, no advisories.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
