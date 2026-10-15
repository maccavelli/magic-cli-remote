#!/usr/bin/env python3
"""Load macOS Gatekeeper's first-run queue, to reproduce MADR 0193's flakes.

macOS holds the first run of every newly written executable while Gatekeeper
scans it, and scans queue behind each other. A test whose stub was not warmed
waits in that queue inside its own time bound (MADR 0193 E1-E8). This script
keeps the queue busy: each worker writes a new `#!/bin/sh` script and runs it,
in a loop, so a test run alongside it sees the delays a busy host produces.

Usage:
    python3 scripts/gatekeeper_load.py --workers 12 --seconds 600
    python3 scripts/gatekeeper_load.py --workers 40      # until interrupted

Run `go test` in another shell while it runs. On Linux it only spawns shells,
which start in milliseconds, so it reproduces nothing there (MADR 0193 E9).
Scripts go under one temporary directory, removed on exit.
"""

from __future__ import annotations

import argparse
import os
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--workers", type=int, default=12, help="concurrent writers (default 12)")
    ap.add_argument("--seconds", type=float, default=0, help="run time; 0 runs until interrupted")
    args = ap.parse_args()
    if args.workers < 1:
        ap.error("--workers must be at least 1")

    base = tempfile.mkdtemp(prefix="gatekeeper-load-")
    stop = threading.Event()
    count = 0
    lock = threading.Lock()

    def work() -> None:
        nonlocal count
        while not stop.is_set():
            d = tempfile.mkdtemp(dir=base)
            path = os.path.join(d, "s")
            with open(path, "w", encoding="utf-8") as f:
                f.write("#!/bin/sh\nexit 0\n")
            os.chmod(path, 0o700)
            subprocess.run([path], check=False)
            shutil.rmtree(d, ignore_errors=True)
            with lock:
                count += 1

    def on_signal(_signum: int, _frame: object) -> None:
        stop.set()

    signal.signal(signal.SIGINT, on_signal)
    signal.signal(signal.SIGTERM, on_signal)

    start = time.monotonic()
    threads = [threading.Thread(target=work, daemon=True) for _ in range(args.workers)]
    for th in threads:
        th.start()
    try:
        while not stop.is_set():
            if args.seconds and time.monotonic() - start >= args.seconds:
                stop.set()
            stop.wait(0.2)
    finally:
        for th in threads:
            th.join()
        shutil.rmtree(base, ignore_errors=True)
    elapsed = time.monotonic() - start
    rate = count / elapsed if elapsed > 0 else 0.0
    print(f"gatekeeper_load: {count} fresh runs in {elapsed:.0f}s with {args.workers} workers "
          f"({rate:.1f}/s)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
