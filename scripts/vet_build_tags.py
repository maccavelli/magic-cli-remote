#!/usr/bin/env python3
"""Type-check every opt-in build tag: `go vet -tags <tag> ./...` once per tag (MADR 0225 D8).

Usage: vet_build_tags.py   (run from the repository root; GOOS is inherited)

`go vet ./...` and `go test ./...` never compile a file behind a tag such as `live_codex`, so such a
file can stop compiling and nothing notices until the next live run. That is how 0225's D2 helper
broke three codex live suites behind a green CI run (0225 MADR F9 to F11).

The tags are read from the `//go:build` lines of the tracked Go files, so a new tag is checked
without editing this script. Platform and toolchain names are left out: the per-OS jobs and
`make staticcheck`'s GOOS passes already build those. `ignore` is left out too, because it marks
files that are never meant to build with the rest.

Exit status is 1 iff `go vet` fails under any tag.
"""

from __future__ import annotations

import re
import subprocess
import sys

IDENT_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_.]*")
NOT_OPT_IN = {"unix", "cgo", "gc", "gccgo", "ignore"}


def platform_names() -> set[str]:
    """Every GOOS and GOARCH the toolchain knows, from `go tool dist list`."""
    out = subprocess.run(["go", "tool", "dist", "list"], capture_output=True, text=True, check=True).stdout
    names = set()
    for pair in out.split():
        names.update(pair.split("/"))
    return names


def constraint_lines(path: str) -> list[str]:
    """The `//go:build` lines above a file's package clause."""
    found = []
    try:
        with open(path, encoding="utf-8") as fh:
            for line in fh:
                if line.startswith("package "):
                    break
                if line.startswith("//go:build "):
                    found.append(line[len("//go:build "):])
    except OSError:
        pass
    return found


def opt_in_tags() -> list[str]:
    files = subprocess.run(["git", "ls-files", "*.go"], capture_output=True, text=True, check=True).stdout.split()
    excluded = NOT_OPT_IN | platform_names()
    tags = set()
    for path in files:
        for expr in constraint_lines(path):
            for name in IDENT_RE.findall(expr):
                if name not in excluded and not name.startswith("go1."):
                    tags.add(name)
    return sorted(tags)


def main() -> int:
    tags = opt_in_tags()
    print(f"vet-tags: {len(tags)} opt-in tag(s): {', '.join(tags)}")
    failed = []
    for tag in tags:
        r = subprocess.run(["go", "vet", "-tags", tag, "./..."], capture_output=True, text=True)
        if r.returncode == 0:
            print(f"vet-tags: {tag}: ok")
            continue
        failed.append(tag)
        print(f"vet-tags: {tag}: FAILED")
        for line in (r.stdout + r.stderr).splitlines():
            print(f"    {line}")
    if failed:
        print(f"vet-tags: go vet failed under {len(failed)} tag(s): {', '.join(failed)}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
