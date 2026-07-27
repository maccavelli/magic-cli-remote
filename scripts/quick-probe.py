#!/usr/bin/env python3
"""Quick probe to see actual item/started wire shape in codex 0.145.0."""
import json
import os
import select
import subprocess
import sys
import time
from pathlib import Path

REPO = Path("/home/mac/gitrepos/magic-cli-remote")
CODEX_BIN = os.environ.get("CODEX_BIN", "codex")
APP_SERVER = [CODEX_BIN, "app-server", "--listen", "stdio://",
              "-c", 'approval_policy="never"', "-c", 'sandbox_mode="read-only"']


def log(*a):
    print("[quick]", *a, file=sys.stderr, flush=True)


class AppServer:
    def __init__(self):
        self.proc = subprocess.Popen(
            APP_SERVER, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0,
        )
        self._id = 0
        self._buf = b""

    def send(self, method, params=None):
        self._id += 1
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        msg["id"] = self._id
        self.proc.stdin.write((json.dumps(msg) + "\n").encode())
        self.proc.stdin.flush()
        return self._id

    def notify(self, method, params=None):
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        self.proc.stdin.write((json.dumps(msg) + "\n").encode())
        self.proc.stdin.flush()

    def drain(self, all_events):
        while True:
            r, _, _ = select.select([self.proc.stdout], [], [], 0.3)
            if not r:
                return
            chunk = self.proc.stdout.read(65536)
            if not chunk:
                return
            self._buf += chunk
            while b"\n" in self._buf:
                line, self._buf = self._buf.split(b"\n", 1)
                line = line.strip()
                if not line:
                    continue
                try:
                    f = json.loads(line)
                except Exception:
                    continue
                all_events.append(f)

    def wait_response(self, rid, all_events, timeout=30.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.drain(all_events)
            for f in all_events:
                if f.get("id") == rid:
                    return f
            time.sleep(0.05)
        raise TimeoutError(f"id {rid}")

    def wait_turn_done(self, all_events, timeout=180):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.drain(all_events)
            for f in all_events:
                if f.get("method") == "turn/completed":
                    return
            time.sleep(0.2)
        log("turn did not complete within timeout")


def main():
    s = AppServer()
    all_events = []
    init_id = s.send("initialize", {
        "clientInfo": {"name": "mcremote-quick", "title": "MADR 0035 quick", "version": "0.0.0"},
        "capabilities": {"experimentalApi": False},
    })
    s.wait_response(init_id, all_events=all_events)
    s.notify("initialized")

    t = s.wait_response(
        s.send("thread/start", {"cwd": str(REPO), "approvalPolicy": "never", "sandbox": "read-only"}),
        all_events=all_events,
    )
    thread_id = t["result"]["thread"]["id"]
    log("thread:", thread_id)

    turn_id = s.send("turn/start", {
        "threadId": thread_id,
        "input": [{"type": "text", "text": "Run `echo probe` and tell me the output."}],
    })
    s.wait_response(turn_id, all_events=all_events)
    s.wait_turn_done(all_events, timeout=120)

    out_dir = REPO / "docs" / "codex-spike-0.145.0"
    out_dir.mkdir(parents=True, exist_ok=True)
    out = out_dir / "item-stream-raw.jsonl"
    with out.open("w") as f:
        for f0 in all_events:
            f.write(json.dumps(f0) + "\n")

    log("== item/started shapes ==")
    n = 0
    for f in all_events:
        if f.get("method") == "item/started":
            log(json.dumps(f, indent=2)[:2000])
            n += 1
            if n >= 4:
                break

    log("== item/completed shapes ==")
    n = 0
    for f in all_events:
        if f.get("method") == "item/completed":
            log(json.dumps(f, indent=2)[:2000])
            n += 1
            if n >= 4:
                break

    log("== turn/completed ==")
    for f in all_events:
        if f.get("method") == "turn/completed":
            log(json.dumps(f, indent=2)[:2000])
            break

    log("== notification methods seen ==")
    methods = sorted({f.get("method") for f in all_events if f.get("method")})
    for m in methods:
        log(" ", m)

    s.proc.kill()
    try:
        s.proc.wait(timeout=3)
    except Exception:
        pass


if __name__ == "__main__":
    main()
