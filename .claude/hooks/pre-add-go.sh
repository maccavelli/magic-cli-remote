#!/usr/bin/env bash
# Agent gate for the pre-add rule in AGENTS.md: no Go file gets staged until
# gofmt, golint and govulncheck are clean.
#
# Registered as a PreToolUse hook on Bash in .claude/settings.json. Reads the
# tool call as JSON on stdin, and blocks (exit 2, message on stderr) when the
# command would stage Go files that do not pass scripts/go-precheck.sh.
#
# It deliberately gates `git add` rather than `git commit`: by commit time the
# work is already staged, and the point of the rule is that what lands in the
# index is correct. The git pre-commit hook is the backstop.
set -uo pipefail

payload="$(cat)"

# The command being run, and the Go paths it would stage. A `git add` with no
# explicit paths (-A, ., a directory) resolves to the changed Go files under
# those paths; anything with no Go file in scope is allowed through untouched.
read -r -d '' pyprog <<'PY'
import json, os, re, subprocess, sys

payload = sys.argv[1]
try:
    data = json.loads(payload)
except Exception:
    sys.exit(0)
# Claude Code uses tool_input (snake_case); Grok uses toolInput (camelCase).
tool_input = data.get("tool_input") or data.get("toolInput") or {}
if not isinstance(tool_input, dict):
    tool_input = {}
cmd = tool_input.get("command") or ""

# Every `git add`/`git stage` in the command line (they chain with && and ;).
adds = re.findall(r"\bgit\s+(?:-[^\s]+\s+)*(?:add|stage)\b([^&|;\n]*)", cmd)
if not adds:
    sys.exit(0)

def changed_go(prefixes):
    out = subprocess.run(["git", "status", "--porcelain", "--untracked-files=all"],
                         capture_output=True, text=True).stdout
    files = []
    for line in out.splitlines():
        path = line[3:].strip().strip('"')
        if " -> " in path:            # rename
            path = path.split(" -> ", 1)[1]
        if not path.endswith(".go"):
            continue
        if not prefixes or any(path == p or path.startswith(p.rstrip("/") + "/") for p in prefixes):
            files.append(path)
    return files

targets, broad_prefixes = [], []
for args in adds:
    for tok in args.split():
        if tok.startswith("-"):
            if tok in ("-A", "--all", "-u", "--update"):
                broad_prefixes.append("")   # whole tree
            continue
        tok = tok.strip("'\"")
        if tok in (".", "./", ":/"):
            broad_prefixes.append("")
        elif os.path.isdir(tok):
            broad_prefixes.append(tok)
        elif tok.endswith(".go"):
            targets.append(tok)

if broad_prefixes:
    prefixes = [] if "" in broad_prefixes else broad_prefixes
    targets.extend(changed_go(prefixes))

seen, uniq = set(), []
for t in targets:
    if t not in seen and os.path.isfile(t):
        seen.add(t)
        uniq.append(t)
print("\n".join(uniq))
PY

mapfile -t go_files < <(python3 -c "$pyprog" "$payload")
[ "${#go_files[@]}" -eq 0 ] && exit 0

root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
if ! output="$("$root/scripts/go-precheck.sh" "${go_files[@]}" 2>&1)"; then
  reason="$(printf '%s\n' \
    "Blocked: Go files must pass gofmt, golint and govulncheck before they are staged (AGENTS.md)." \
    "" \
    "$output" \
    "" \
    "Fix the reported issues (start with 'make fmt'), then re-run the git add.")"
  # Grok PreToolUse honors JSON deny on stdout; Claude Code uses exit 2 + stderr.
  printf '%s\n' "{\"decision\":\"deny\",\"reason\":$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$reason")}"
  echo "$reason" >&2
  exit 2
fi
exit 0
