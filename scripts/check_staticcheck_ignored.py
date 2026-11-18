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

  3. (0131-PLAN-replace-golint-with-golangci-lint.md, Q5 A) golangci-lint's staticcheck reads `//nolint`, not
     `//lint:ignore`, so each ignore is paired with a `//nolint:staticcheck` for the same site, and the pair is held
     both ways, so neither linter is silenced alone:
       - a `//lint:ignore` is followed by a covered line that ends in `//nolint:staticcheck // as the lint:ignore above`;
       - a `//lint:file-ignore` is paired with `//nolint:staticcheck // as the lint:file-ignore below` on the line
         directly above the file's `package` clause, which golangci-lint reads for the whole file;
       - every `//nolint:` naming staticcheck is one of those two forms, and no other.
  4. The counts are bounded: at most BOUND_NOLINT `//nolint:staticcheck` directives in the module, and at most
     BOUND_IGNORED ignored findings from staticcheck, so neither can grow without this file being edited, in a commit that
     names the record that allows it. A count below the bound passes and says so: lower the bound then.

Findings that are not ignored are staticcheck's own run's business; this script reads only the ignored.
Exit status is 1 iff a directive, a pairing, a bound or an ignored finding is outside the exception.
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
NOLINT_RE = re.compile(r"//nolint:([A-Za-z0-9_,-]+)(?:\s*//\s*(.*))?")
PACKAGE_RE = re.compile(r"^package\s+\w+")
# The recorded bounds (0131-PLAN, P3): nine `//nolint:staticcheck` directives, which are the seven line directives and the two
# file directives of internal/provider/acpagent, and the 56 findings staticcheck reports ignored on each GOOS. Raising either
# needs a record that allows it.
BOUND_NOLINT = 9
BOUND_IGNORED = 56


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


def names_staticcheck(m: re.Match | None) -> bool:
    return bool(m) and "staticcheck" in m.group(1).split(",")


def bad_pairs() -> tuple[list[str], int]:
    """Rule 3: each //lint:ignore and //lint:file-ignore paired with its //nolint:staticcheck, and no other //nolint:staticcheck.
    Returns the problems and the number of //nolint:staticcheck directives."""
    files = subprocess.run(["git", "ls-files", "*.go"], capture_output=True, text=True, check=True).stdout.split()
    bad, count = [], 0
    for path in files:
        try:
            lines = open(path, encoding="utf-8").read().splitlines()
        except OSError:
            continue
        pkg = next((i for i, l in enumerate(lines) if PACKAGE_RE.match(l)), None)
        file_ignored = any(DIRECTIVE_RE.search(l) and DIRECTIVE_RE.search(l).group(1) == "file-ignore" for l in lines)
        for i, line in enumerate(lines):
            n = i + 1
            d = DIRECTIVE_RE.search(line)
            if d and d.group(1) == "ignore":
                nxt = lines[i + 1] if i + 1 < len(lines) else ""
                m = NOLINT_RE.search(nxt)
                if not names_staticcheck(m) or "lint:ignore above" not in (m.group(2) or ""):
                    bad.append(f"{path}:{n + 1}: the line under the //lint:ignore at line {n} does not end in "
                               f"`//nolint:staticcheck // as the lint:ignore above`")
            if d and d.group(1) == "file-ignore":
                above = lines[pkg - 1] if pkg else ""
                m = NOLINT_RE.search(above)
                if pkg is None or not names_staticcheck(m) or "lint:file-ignore below" not in (m.group(2) or ""):
                    bad.append(f"{path}:{n}: the //lint:file-ignore has no `//nolint:staticcheck // as the lint:file-ignore below` "
                               f"on the line above the package clause")
            m = NOLINT_RE.search(line)
            if not names_staticcheck(m) or d:
                continue
            count += 1
            if pkg is not None and i == pkg - 1:
                if not file_ignored:
                    bad.append(f"{path}:{n}: //nolint:staticcheck above the package clause, with no //lint:file-ignore in the file")
                continue
            above = lines[i - 1] if i else ""
            prev = DIRECTIVE_RE.search(above)
            if not prev or prev.group(1) != "ignore":
                bad.append(f"{path}:{n}: //nolint:staticcheck with no //lint:ignore on the line above: {line.strip()}")
    return bad, count


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
    pairs, nolints = bad_pairs()
    ignored, allowed = bad_ignored(sys.argv[1])
    bounds = []
    if nolints > BOUND_NOLINT:
        bounds.append(f"{nolints} `//nolint:staticcheck` directives, over the recorded bound of {BOUND_NOLINT}")
    if allowed > BOUND_IGNORED:
        bounds.append(f"{allowed} ignored staticcheck findings, over the recorded bound of {BOUND_IGNORED}")
    for b in directives:
        print(f"lint directive outside MADR 0207 amendment H (D45): {b}")
    for b in pairs:
        print(f"unpaired suppression (0131-PLAN, Q5 A): {b}")
    for b in ignored:
        print(f"ignored finding outside MADR 0207 amendment H (D45): {b}")
    for b in bounds:
        print(f"suppression bound exceeded (0131-PLAN, Q5 A): {b}")
    if directives or pairs or ignored or bounds:
        return 1
    note = "" if (nolints, allowed) == (BOUND_NOLINT, BOUND_IGNORED) else " (below the recorded bound: lower it)"
    print(f"check_staticcheck_ignored: {allowed} ignored finding(s), each an acp-go-sdk2 unstable-schema name; "
          f"{nolints} //nolint:staticcheck directive(s), each paired{note}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
