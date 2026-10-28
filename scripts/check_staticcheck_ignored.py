#!/usr/bin/env python3
"""Bound staticcheck's ignore directives to the one exception MADR 0207 amendment H allows (D45).

Usage: check_staticcheck_ignored.py <staticcheck binary>   (run from the repository root; GOOS is inherited)

The exception: an acp-go-sdk2 declaration whose deprecation is only its unstable-schema notice may be
used under `//lint:ignore SA1019` or `//lint:file-ignore SA1019`. A file-wide directive hides every SA1019
in its file, so the directive alone cannot hold that bound (the SDK-gaps PLAN's C0 Deviation 1). This
script holds it:

  1. Every `//lint:ignore` and `//lint:file-ignore` in a tracked Go file names exactly SA1019, and its
     reason names "0207 amendment H".
  2. `staticcheck -show-ignored -f json ./...` reports no ignored finding other than SA1019 on an
     acp-go-sdk2 declaration carrying the unstable-schema notice.

Findings that are not ignored are staticcheck's own run's business; this script reads only the ignored.
Exit status is 1 iff a directive or an ignored finding is outside the exception.
"""

from __future__ import annotations

import json
import re
import subprocess
import sys

DIRECTIVE_RE = re.compile(r"//lint:(ignore|file-ignore)\s+(\S+)\s*(.*)")
REASON = "0207 amendment H"
SDK = "github.com/maccavelli/acp-go-sdk2."
NOTICE = "this declaration comes from the unstable ACP schema"


def bad_directives() -> list[str]:
    files = subprocess.run(["git", "ls-files", "*.go"], capture_output=True, text=True, check=True).stdout.split()
    bad = []
    for path in files:
        try:
            lines = open(path, encoding="utf-8").read().splitlines()
        except OSError:
            continue
        for n, line in enumerate(lines, 1):
            m = DIRECTIVE_RE.search(line)
            if m and (m.group(2) != "SA1019" or REASON not in m.group(3)):
                bad.append(f"{path}:{n}: {line.strip()}")
    return bad


def bad_ignored(staticcheck: str) -> tuple[list[str], int]:
    r = subprocess.run([staticcheck, "-show-ignored", "-f", "json", "./..."], capture_output=True, text=True)
    bad, allowed = [], 0
    for line in r.stdout.splitlines():
        try:
            rec = json.loads(line)
        except json.JSONDecodeError:
            bad.append(f"unreadable staticcheck output: {line[:200]}")
            continue
        if rec.get("severity") != "ignored":
            continue
        msg = rec.get("message", "")
        loc = rec.get("location", {})
        # "github.com/…/acp-go-sdk2.Name is deprecated: …" or "(github.com/…/acp-go-sdk2.Type).Field is …"
        subject = msg.split(" is deprecated", 1)[0].lstrip("(")
        if rec.get("code") == "SA1019" and subject.startswith(SDK) and NOTICE in msg:
            allowed += 1
            continue
        bad.append(f"{loc.get('file')}:{loc.get('line')}: ignored {rec.get('code')}: {msg[:200]}")
    if r.returncode not in (0, 1) or (not r.stdout and r.stderr.strip()):
        bad.append(f"staticcheck -show-ignored exited {r.returncode}: {r.stderr.strip()[:300]}")
    return bad, allowed


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__.strip().splitlines()[2], file=sys.stderr)
        return 2
    directives = bad_directives()
    ignored, allowed = bad_ignored(sys.argv[1])
    for b in directives:
        print(f"lint directive outside MADR 0207 amendment H (D45): {b}")
    for b in ignored:
        print(f"ignored finding outside MADR 0207 amendment H (D45): {b}")
    if directives or ignored:
        return 1
    print(f"check_staticcheck_ignored: {allowed} ignored finding(s), each an acp-go-sdk2 unstable-schema name")
    return 0


if __name__ == "__main__":
    sys.exit(main())
