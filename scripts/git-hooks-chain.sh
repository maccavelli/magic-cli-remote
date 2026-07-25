#!/usr/bin/env bash
# Global hook shim: run a repository's own hook of the same name.
#
# Why this exists: a machine-wide `core.hooksPath` makes Git look *only* there,
# so every repository's .git/hooks is silently ignored — including the one
# `make install-hooks` installs. Nothing errors; the checks just never run.
#
# Installed as ~/.config/git/hooks/<hook-name> (see `make install-hooks`).
#
# Note it resolves the repo hook through `--git-dir`, not
# `git rev-parse --git-path hooks`: that one *honours* core.hooksPath and would
# therefore point back at this shim.
set -uo pipefail

hook_name="$(basename "$0")"
git_dir="$(git rev-parse --git-dir 2>/dev/null)" || exit 0
local_hook="$git_dir/hooks/$hook_name"

[ -x "$local_hook" ] || exit 0

# Never chain into ourselves.
self="$(readlink -f "$0" 2>/dev/null || echo "$0")"
target="$(readlink -f "$local_hook" 2>/dev/null || echo "$local_hook")"
[ "$self" = "$target" ] && exit 0

exec "$local_hook" "$@"
