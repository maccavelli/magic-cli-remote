#!/usr/bin/env bash
# Agent gate for the git commit message rule in AGENTS.md:
# Do NOT pass a commit message (-m / --message / -F). The global prepare-commit-msg
# hook populates the message automatically.

set -uo pipefail

payload="$(cat)"

read -r -d '' pyprog <<'PY'
import json, re, sys

payload = sys.argv[1]
try:
    data = json.loads(payload)
except Exception:
    sys.exit(0)

cmd = (data.get("tool_input") or {}).get("command") or ""

# Match git commit with -m, -M, --message, or -F
if re.search(r"\bgit\s+(?:-[^\s]+\s+)*commit\b.*?(?:-m|-M|--message|-F)\b", cmd):
    print("MATCH")
PY

result="$(python3 -c "$pyprog" "$payload")"
if [ "$result" = "MATCH" ]; then
  {
    echo "Blocked: Do NOT pass a commit message (-m, -M, --message, or -F) when running 'git commit' (AGENTS.md)."
    echo "A global git prepare-commit-msg hook populates commit messages automatically."
    echo "Run 'git commit' without a message argument."
  } >&2
  exit 2
fi
exit 0
