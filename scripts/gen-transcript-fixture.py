"""Build a large, realistically scaffolded transcript fixture for device testing.

Usage:
    python3 scripts/gen-transcript-fixture.py <session-id> <out.json> <turns>

Written for MADR 0138 criterion 4 / MADR 0141 F2, where a 900-turn fixture
showed the app opening on turn 291 of 900 because the forward history walk is
bounded at 32 pages x 200 events.

Two things about the output are load-bearing, both learned by getting them
wrong:

  * The container is {"events": [...]}. A bare JSON array is accepted by every
    JSON tool and *silently discarded* by Store.LoadHistory (MADR 0141 F3).
  * The store reads <dataDir>/sessions/<id>/history.json. Seeding one level
    higher produces the same silent empty result, and nothing distinguishes the
    two.

Validate before seeding. Do not trust a fixture that has not been through
Store.LoadHistory.

Each turn reproduces the 22-event shape observed in a real grok session
(32da1cc5, seq 2976-2997):
  user_message, session_status, thought x2, available_commands,
  tool_call, tool_call_update x2, thought x3, available_commands,
  tool_call, tool_call_update x2, thought x2,
  assistant_message_chunk x3, available_commands, turn_complete

Every turn is labelled "TURN n of N" in both its user and assistant text, so a
screenshot says exactly which turn is on screen.
"""
import json, sys, datetime, uuid

SID = sys.argv[1]
OUT = sys.argv[2]
TURNS = int(sys.argv[3])
AGENT = "01a06dca-1d08-7121-861f-c54b9e0127a6"

CMDS = [
    {"name": "compact", "description": "Compress conversation history to save context window",
     "hint": "optional context about what to preserve"},
    {"name": "always-approve", "description": "Toggle always-approve mode (skip all permission prompts)"},
    {"name": "plan", "description": "Enter plan mode"},
    {"name": "model", "description": "Switch model", "hint": "model id"},
    {"name": "undo", "description": "Undo the last turn"},
    {"name": "status", "description": "Show runtime status"},
]

t0 = datetime.datetime(2026, 8, 1, 9, 0, 0, tzinfo=datetime.timezone.utc)
events, seq, clock = [], 0, t0

def add(typ, **kw):
    global seq, clock
    seq += 1
    clock += datetime.timedelta(milliseconds=850)
    e = {"type": typ, "session_id": SID,
         "timestamp": clock.strftime("%Y-%m-%dT%H:%M:%S.%fZ"), "seq": seq}
    e.update(kw)
    events.append(e)

for n in range(1, TURNS + 1):
    tag = f"TURN {n} of {TURNS}"
    add("user_message", text=f"{tag} - please summarise the build log and push if clean",
        agent_session_id=AGENT)
    add("session_status", status="running", agent_session_id=AGENT)
    add("thought_chunk", text="The")
    add("thought_chunk", text=f" user is on {tag}. I will check the tree, then report. "
                              "No -m on commit; the hook writes the message.")
    add("available_commands", commands=CMDS, agent_session_id=AGENT)

    for call in (1, 2):
        tid = f"call-{uuid.uuid5(uuid.NAMESPACE_OID, f'{n}-{call}')}-{n}"
        add("tool_call", tool_id=tid, tool_name="run_terminal_command", agent_session_id=AGENT)
        add("tool_call_update", text=f"{tag}: step {call} - inspect working tree",
            tool_id=tid, tool_name="Execute `git status --short`", tool_kind="execute",
            agent_session_id=AGENT)
        add("tool_call_update", status="completed",
            text=f"## master...origin/master [ahead {n}] --- clean ---",
            tool_id=tid, tool_name="Execute `git status --short`", agent_session_id=AGENT)
        if call == 1:
            add("thought_chunk", text="The")
            add("thought_chunk", text=f" tree is clean at {tag}. Nothing to stage.")
            add("thought_chunk", text=" Proceeding to push.\n")
            add("available_commands", commands=CMDS, agent_session_id=AGENT)

    add("thought_chunk", text="Done")
    add("thought_chunk", text=f". {tag} completed; summarising concisely.")
    add("assistant_message_chunk", text="The")
    add("assistant_message_chunk",
        text=f" working tree was clean at **{tag}**, so there was nothing new to commit. ")
    add("assistant_message_chunk", text=f"All {n} local commits are on `origin/master`.")
    add("available_commands", commands=CMDS, agent_session_id=AGENT)
    add("turn_complete", status="end_turn", agent_session_id=AGENT, stop_reason="end_turn")

with open(OUT, "w") as f:
    json.dump({"events": events}, f)

import os
print(f"turns={TURNS} events={len(events)} last_seq={events[-1]['seq']} "
      f"bytes={os.path.getsize(OUT):,} ({os.path.getsize(OUT)/1e6:.1f} MB)")
print(f"events/turn={len(events)//TURNS}")
