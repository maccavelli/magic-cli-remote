#!/usr/bin/env python3
"""Run the pinned golangci-lint over every build this repository ships (MADR 0131, its PLAN's P1).

Usage: lint.py [--pin] [package-dir ...]   (run from anywhere inside the repository)

The one implementation of the repository's lint rule, called from `make lint`, from CI and from
scripts/go-precheck.sh, so the three cannot drift. golangci-lint is v2.15.0, the fleet's pin, and PIN
below is the only place that says so: CI installs `...@v$(python3 scripts/lint.py --pin)`.

With no package directory it lints `./...`. With directories (the pre-add gate passes the packages that
hold the staged files) it lints just those; golangci-lint analyses packages, not files, so a narrower
argument would report different findings than `make lint` and the two would drift.

It runs once per GOOS CI builds for (the host's, linux, darwin and windows), with CGO_ENABLED=0 as the
release and the CI native lanes build, and once more for linux with every opt-in build tag (the same tags
scripts/vet_build_tags.py vets under). A host-only run misses `_windows.go` and tagged files: on this tree
that is 18 and 8 findings that the host's own run does not show (0131 MADR, update of 2026-11-18).

Env:
  GOLANGCI_LINT=<path>   the golangci-lint binary (default: PATH, then $(go env GOPATH)/bin)

Exit status: 0 every pass clean, 1 a pass reported findings or failed, 2 the tool is missing or is not the pin.
"""

from __future__ import annotations

import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path

PIN = "2.15.0"
CONFIG = ".golangci.yml"
CI_GOOS = ("linux", "darwin", "windows")
INSTALL = f"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v{PIN}"
HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))


def host_goos() -> str:
    out = subprocess.run(["go", "env", "GOOS"], capture_output=True, text=True)
    return out.stdout.strip() if out.returncode == 0 and out.stdout.strip() else platform.system().lower()


def find_binary() -> str | None:
    env = os.environ.get("GOLANGCI_LINT", "").strip()
    if env:
        return env if shutil.which(env) or os.access(env, os.X_OK) else None
    found = shutil.which("golangci-lint")
    if found:
        return found
    gp = subprocess.run(["go", "env", "GOPATH"], capture_output=True, text=True).stdout.strip()
    for name in ("golangci-lint", "golangci-lint.exe"):
        cand = os.path.join(gp, "bin", name) if gp else ""
        if cand and os.access(cand, os.X_OK):
            return cand
    return None


def version_of(binary: str) -> str:
    out = subprocess.run([binary, "version"], capture_output=True, text=True)
    m = re.search(r"has version v?(\d+\.\d+\.\d+)", out.stdout + out.stderr)
    return m.group(1) if m else ""


def passes(host: str) -> list[tuple[str, str]]:
    """(label, GOOS) for each GOOS pass: the host's first, then each GOOS CI builds for."""
    goos_list = []
    for g in (host, *CI_GOOS):
        if g not in goos_list:
            goos_list.append(g)
    return [(f"GOOS={g}", g) for g in goos_list]


def main(argv: list[str]) -> int:
    if argv and argv[0] == "--pin":
        print(PIN)
        return 0
    root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True)
    if root.returncode != 0:
        print("lint: not inside a git repository", file=sys.stderr)
        return 1
    os.chdir(root.stdout.strip())
    from vet_build_tags import opt_in_tags  # the tag discovery `make vet-tags` uses

    binary = find_binary()
    if binary is None:
        print(f"lint: golangci-lint not found (GOLANGCI_LINT, PATH, GOPATH/bin).\n  install: {INSTALL}", file=sys.stderr)
        return 2
    got = version_of(binary)
    if got != PIN:
        print(f"lint: {binary} is v{got or '?'}, the repository pins v{PIN}.\n  install: {INSTALL}", file=sys.stderr)
        return 2

    targets = [d if d.startswith("./") else f"./{d}" for d in argv if d not in (".", "./")] or ["./..."]
    # A directory under testdata, or one with its own go.mod, is not a package of this module.
    keep = []
    for t in targets:
        parts = Path(t).parts
        if t != "./..." and ("testdata" in parts or (Path(t) / "go.mod").exists()):
            continue
        keep.append(t)
    if not keep:
        print("lint: no package of this module among the arguments; nothing to lint.")
        return 0

    host = host_goos()
    runs: list[tuple[str, dict[str, str], list[str]]] = []
    for label, goos in passes(host):
        runs.append((label, {"GOOS": goos}, []))
    tags = opt_in_tags()
    if tags:
        runs.append((f"GOOS=linux, every opt-in tag ({', '.join(tags)})", {"GOOS": "linux"}, ["--build-tags", ",".join(tags)]))

    rc = 0
    for label, extra_env, extra_args in runs:
        env = {**os.environ, "CGO_ENABLED": "0", **extra_env}
        cmd = [binary, "run", "--config", CONFIG, *extra_args, *keep]
        print(f"golangci-lint v{PIN} {label}", flush=True)
        r = subprocess.run(cmd, env=env)
        if r.returncode != 0:
            print(f"lint: FAILED: {label} (exit {r.returncode})", file=sys.stderr)
            rc = 1
    if rc == 0:
        print(f"lint: clean on {len(runs)} pass(es).")
    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
