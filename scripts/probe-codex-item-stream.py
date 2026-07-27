#!/usr/bin/env python3
"""
Phase 0 probe for MADR 0035.

Drives `codex app-server --listen stdio://` through the item-stream shapes
that phase 2's allowlist, phase 8's plan panel, and D9's rate-limit card
all need to be reconciled against.

Outputs docs/codex-spike-0.145.0/item-stream.json with the captured shapes.
"""

import json
import os
import select
import subprocess
import sys
import time
from pathlib import Path

REPO = Path("/home/mac/gitrepos/magic-cli-remote")
OUT_DIR = REPO / "docs" / "codex-spike-0.145.0"
OUT_PATH = OUT_DIR / "item-stream.json"
CODEX_BIN = os.environ.get("CODEX_BIN", "codex")
APP_SERVER = [CODEX_BIN, "app-server", "--listen", "stdio://",
              "-c", 'approval_policy="never"',
              "-c", 'sandbox_mode="read-only"']


def log(*a):
    print("[probe]", *a, file=sys.stderr, flush=True)


class AppServer:
    def __init__(self):
        self.proc = subprocess.Popen(
            APP_SERVER,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            bufsize=0,
        )
        self._id = 0
        self._buf = b""
        self._notifications = []
        self._responses = {}
        self._stderr_lines = []

    def send(self, method, params=None):
        self._id += 1
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        msg["id"] = self._id
        line = json.dumps(msg) + "\n"
        self.proc.stdin.write(line.encode())
        self.proc.stdin.flush()
        return self._id

    def notify(self, method, params=None):
        # JSON-RPC notification: no id, do not expect a response.
        msg = {"method": method}
        if params is not None:
            msg["params"] = params
        line = json.dumps(msg) + "\n"
        self.proc.stdin.write(line.encode())
        self.proc.stdin.flush()

    def drain_into(self, events_target):
        """Read available frames into the running list."""
        while True:
            r, _, _ = select.select([self.proc.stdout], [], [], 0.5)
            if not r:
                return
            chunk = self.proc.stdout.read(4096)
            if not chunk:
                return
            self._buf += chunk
            while b"\n" in self._buf:
                line, self._buf = self._buf.split(b"\n", 1)
                line = line.strip()
                if not line:
                    continue
                try:
                    frame = json.loads(line)
                except json.JSONDecodeError as e:
                    log("bad json:", e, line[:200])
                    continue
                if "method" in frame and "id" in frame:
                    # server request
                    events_target.append({"kind": "server_request", "data": frame})
                elif "method" in frame:
                    events_target.append({"kind": "notification", "data": frame})
                elif "id" in frame:
                    events_target.append({"kind": "response", "data": frame})

    def wait_response(self, req_id, timeout=60.0, events_target=None):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.drain_into(events_target or self._notifications)
            for ev in events_target or self._notifications:
                if ev["kind"] == "response" and ev["data"].get("id") == req_id:
                    return ev["data"]
            time.sleep(0.05)
        raise TimeoutError(f"no response for id {req_id}")

    def kill(self):
        try:
            self.proc.stdin.close()
        except Exception:
            pass
        try:
            self.proc.kill()
        except Exception:
            pass
        try:
            out, err = self.proc.communicate(timeout=2)
        except Exception:
            out, err = b"", b""
        # capture stderr
        self._stderr_lines = (err or b"").decode(errors="replace").splitlines()[-20:]
        return out, err


def main():
    log("starting codex app-server")
    s = AppServer()
    events = []

    # initialize
    init_id = s.send("initialize", {
        "clientInfo": {"name": "mcremote-probe", "title": "MADR 0035 phase 0", "version": "0.0.0"},
        "capabilities": {"experimentalApi": False},
    })
    s.wait_response(init_id, timeout=30, events_target=events)
    s.notify("initialized")

    # thread/start with read-only sandbox (v2 schema: sandbox is a string)
    tid_resp = s.wait_response(
        s.send("thread/start", {"cwd": str(REPO),
                                "approvalPolicy": "never",
                                "sandbox": "read-only"}),
        timeout=30, events_target=events
    )
    if tid_resp.get("error"):
        log("thread/start failed:", tid_resp["error"])
    s.kill()

    # Save all raw notifications for shape analysis
    raw_path = OUT_DIR / "item-stream-raw.jsonl"
    with raw_path.open("w") as f:
        for ev in events:
            if ev["kind"] == "notification":
                f.write(json.dumps(ev["data"]) + "\n")
        sys.exit(1)
    thread_id = tid_resp["result"]["thread"]["id"]
    log("thread:", thread_id)

    # ---------- Scenario A: plain chat (no tools) ----------
    log("A: plain chat")
    a_turn = s.send("turn/start", {
        "threadId": thread_id,
        "input": [{"type": "text", "text": "Reply with exactly: ok. No tools, no markdown."}],
    })
    s.wait_response(a_turn, timeout=60, events_target=events)
    s.drain_into(events)
    time.sleep(1.0)

    # ---------- Scenario B: shell command ----------
    log("B: shell command")
    b_turn = s.send("turn/start", {
        "threadId": thread_id,
        "input": [{"type": "text", "text": "Run `echo codex-probe-marker` with the bash tool. Then reply with the result."}],
    })
    s.wait_response(b_turn, timeout=120, events_target=events)
    s.drain_into(events)
    time.sleep(1.0)

    # ---------- Scenario C: file edit ----------
    log("C: file edit")
    c_turn = s.send("turn/start", {
        "threadId": thread_id,
        "input": [{"type": "text", "text": "Read /etc/hostname and tell me its contents using the read tool, no shell."}],
    })
    s.wait_response(c_turn, timeout=120, events_target=events)
    s.drain_into(events)
    time.sleep(1.0)

    # ---------- Scenario D: multi-step plan ----------
    log("D: multi-step plan")
    d_turn = s.send("turn/start", {
        "threadId": thread_id,
        "input": [{"type": "text", "text":
            "Make a plan to: 1) run `pwd`, 2) list the files in the current dir, 3) read AGENTS.md. "
            "Then execute it. Use the plan tool if available."
        }],
    })
    s.wait_response(d_turn, timeout=180, events_target=events)
    s.drain_into(events)
    time.sleep(2.0)

    s.kill()

    # ---------- Summarize ----------
    notif_by_method = {}
    for ev in events:
        if ev["kind"] == "notification":
            m = ev["data"]["method"]
            notif_by_method.setdefault(m, []).append(ev["data"])

    item_started = notif_by_method.get("item/started", [])
    item_completed = notif_by_method.get("item/completed", [])

    # Dump first 3 raw frames per method for shape discovery
    raw_samples = {}
    for m, lst in notif_by_method.items():
        raw_samples[m] = [x for x in lst[:3]]

    # v2 schema: itemType may be on the nested "item" object instead of params.
    def detect_item_type(p):
        params = p.get("params", {}) or {}
        if params.get("itemType"):
            return params["itemType"]
        item = params.get("item")
        if isinstance(item, dict):
            if item.get("type"):
                return item["type"]
            if item.get("itemType"):
                return item["itemType"]
        return None

    item_types_seen = sorted({detect_item_type(p) for p in item_started})
    item_shapes = {}
    for p in item_started:
        t = detect_item_type(p)
        if t and t not in item_shapes:
            item_shapes[t] = {
                "first_params_keys": sorted(p.get("params", {}).keys()),
                "first_item_keys": sorted(p.get("params", {}).get("item", {}).keys()) if isinstance(p.get("params", {}).get("item"), dict) else None,
                "first_item": p.get("params", {}).get("item"),
            }

    completed_shapes = {}
    for p in item_completed:
        t = detect_item_type(p)
        if t and t not in completed_shapes:
            completed_shapes[t] = {
                "first_params_keys": sorted(p.get("params", {}).keys()),
                "first_item_keys": sorted(p.get("params", {}).get("item", {}).keys()) if isinstance(p.get("params", {}).get("item"), dict) else None,
            }

    turn_completed = notif_by_method.get("turn/completed", [])
    turn_status_enum = []
    for p in turn_completed:
        st = p.get("params", {}).get("turn", {}).get("status")
        if st and st not in turn_status_enum:
            turn_status_enum.append(st)

    rate_limits = notif_by_method.get("account/rateLimits/updated", [])
    rate_limit_shape = None
    if rate_limits:
        rate_limit_shape = {
            "first_params_keys": sorted(rate_limits[0].get("params", {}).keys()),
            "first_params": rate_limits[0].get("params"),
        }

    mcp_status = notif_by_method.get("mcpServer/startupStatus/updated", [])
    mcp_shape = None
    if mcp_status:
        mcp_shape = {
            "first_params_keys": sorted(mcp_status[0].get("params", {}).keys()),
            "first_params": mcp_status[0].get("params"),
        }

    # lifecycle invariant: for every item type that started, did it complete?
    started_ids = {p.get("params", {}).get("itemId"): p.get("params", {}).get("itemType")
                   for p in item_started}
    completed_ids = {p.get("params", {}).get("itemId") for p in item_completed}
    unclosed = [(iid, t) for iid, t in started_ids.items() if iid and iid not in completed_ids]

    plan_items = [p for p in item_started if p.get("params", {}).get("itemType") == "plan"]
    plan_item_shape = None
    if plan_items:
        plan_item_shape = {
            "first_params_keys": sorted(plan_items[0].get("params", {}).keys()),
            "first_item": plan_items[0].get("params", {}).get("item"),
        }
    plan_update_notifs = [k for k in notif_by_method.keys() if "plan" in k.lower() and k != "item/started"]

    summary = {
        "cli_version": "0.145.0",
        "captured_at_unix": int(time.time()),
        "thread_id": thread_id,
        "event_count": len(events),
        "notification_methods_seen": sorted(notif_by_method.keys()),
        "item_types_started": item_types_seen,
        "item_shapes": item_shapes,
        "item_completed_shapes": completed_shapes,
        "lifecycle_invariant": {
            "started_count": len(started_ids),
            "completed_count": len(item_completed),
            "unclosed_items": unclosed,
        },
        "plan": {
            "item_observed": bool(plan_items),
            "item_shape": plan_item_shape,
            "notification_methods_relating_to_plan": plan_update_notifs,
        },
        "turn_status_enum_observed": turn_status_enum,
        "rate_limits": {
            "count": len(rate_limits),
            "shape": rate_limit_shape,
        },
        "mcp_startup_status": {
            "count": len(mcp_status),
            "shape": mcp_shape,
        },
        "all_notifications_raw_count": {m: len(v) for m, v in notif_by_method.items()},
    }

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    OUT_PATH.write_text(json.dumps(summary, indent=2))
    log("wrote", OUT_PATH)
    log("item types:", item_types_seen)
    log("unclosed items:", unclosed)
    log("plan items seen:", bool(plan_items))
    log("rate-limits seen:", bool(rate_limits))
    log("mcp startup seen:", bool(mcp_status))


if __name__ == "__main__":
    main()
