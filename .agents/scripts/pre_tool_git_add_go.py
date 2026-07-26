#!/usr/bin/env python3
"""Codex/Open Agents adapter for the repository's Claude pre-add Go gate."""

import json
import subprocess
import sys
from pathlib import Path


def command_from(payload):
    tool_input = payload.get("tool_input") or {}
    if isinstance(tool_input.get("command"), str):
        return tool_input["command"]

    tool_call = payload.get("toolCall") or {}
    args = tool_call.get("args") or {}
    for name in ("command", "CommandLine", "cmd"):
        if isinstance(args.get(name), str):
            return args[name]

    for container in (payload.get("input") or {}, payload.get("args") or {}):
        for name in ("command", "CommandLine", "cmd"):
            if isinstance(container.get(name), str):
                return container[name]
    return ""


def main():
    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError:
        return

    command = command_from(payload)
    if not command:
        return

    root = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        check=False,
    ).stdout.strip()
    if not root:
        return

    gate = Path(root) / ".claude" / "hooks" / "pre-add-go.sh"
    result = subprocess.run(
        [str(gate)],
        input=json.dumps({"tool_input": {"command": command}}),
        capture_output=True,
        text=True,
        cwd=root,
        check=False,
    )
    if result.returncode == 2:
        reason = result.stderr.strip() or "Go pre-add checks failed."
        print(json.dumps({"decision": "block", "reason": reason}))


if __name__ == "__main__":
    main()
