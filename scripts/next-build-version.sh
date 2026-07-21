#!/usr/bin/env bash
# Allocate the next build version BASE.N (e.g. 0.2.1.7) with global uniqueness.
#
# Source of truth: git tags  build/<BASE>.<N>  (e.g. build/0.2.1.7)
# Release tags remain  v0.2.1  (three-part); build tags never use the v-prefix.
#
# Algorithm:
#   1. Resolve BASE from the latest release tag (vX.Y.Z), unless passed in.
#   2. Fetch remote build/* tags (best-effort).
#   3. Take max N from remote+local build tags and local .build-counter cache.
#   4. N=max+1; create lightweight tag build/BASE.N; push with CAS retry.
#   5. On successful claim, update .build-counter and print BASE.N.
#   6. If push is impossible (offline / no write), still emit a version but append
#      a uniqueness suffix so two offline builders cannot silently collide.
#
# Env:
#   BUILD_COUNTER_FILE     cache path (default: $repo/.build-counter)
#   MCREMOTE_VERSION_PUSH  1=always try push, 0=never push (default: push in CI)
#   MCREMOTE_VERSION_TAG   0=skip creating tags (tests only)
#   MCREMOTE_VERSION_REMOTE  remote name (default: origin)
#   MCREMOTE_BUILD_N_MIN   floor for N before increment
#   CI / GITHUB_ACTIONS     imply push when MCREMOTE_VERSION_PUSH unset
#
# Usage:
#   scripts/next-build-version.sh [base] [counter-file]
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
base_arg="${1:-}"
file="${2:-${BUILD_COUNTER_FILE:-$root/.build-counter}}"
remote="${MCREMOTE_VERSION_REMOTE:-origin}"
do_tag="${MCREMOTE_VERSION_TAG:-1}"
max_attempts="${MCREMOTE_VERSION_ATTEMPTS:-20}"

log() { printf 'next-build-version: %s\n' "$*" >&2; }

# --- resolve base release (vX.Y.Z only; ignore build/* and dirty describe) ---
resolve_base() {
  if [[ -n "$base_arg" ]]; then
    printf '%s' "${base_arg#v}"
    return
  fi
  local t
  # Prefer explicit three-part release tags (v0.2.1), highest by version sort.
  t="$(git -C "$root" tag -l 'v*.*.*' 2>/dev/null \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
    | sort -V \
    | tail -1 || true)"
  if [[ -z "$t" ]]; then
    t="$(git -C "$root" describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null || true)"
  fi
  t="${t#v}"
  # Strip accidental fourth segment if someone tagged v0.2.1.3 as a release.
  if [[ "$t" =~ ^([0-9]+\.[0-9]+\.[0-9]+) ]]; then
    t="${BASH_REMATCH[1]}"
  fi
  if [[ -z "$t" ]]; then
    t="0.0.0"
  fi
  printf '%s' "$t"
}

# Highest N among tags matching build/<base>.*
max_n_from_tags() {
  local b="$1"
  local prefix="build/${b}."
  local max=0 n tag
  while IFS= read -r tag; do
    [[ -z "$tag" ]] && continue
    n="${tag#"$prefix"}"
    if [[ "$n" =~ ^[0-9]+$ ]] && (( n > max )); then
      max=$n
    fi
  done < <(git -C "$root" tag -l "${prefix}*" 2>/dev/null || true)
  printf '%s' "$max"
}

max_n_from_file() {
  local b="$1"
  if [[ ! -f "$file" ]]; then
    printf '0'
    return
  fi
  local prev_base prev_n
  read -r prev_base prev_n <"$file" || true
  if [[ "${prev_base:-}" == "$b" && "${prev_n:-}" =~ ^[0-9]+$ ]]; then
    printf '%s' "$prev_n"
  else
    printf '0'
  fi
}

max_int() {
  local a="$1" b="$2"
  if (( a > b )); then printf '%s' "$a"; else printf '%s' "$b"; fi
}

should_push() {
  case "${MCREMOTE_VERSION_PUSH:-}" in
    0|false|no|NO) return 1 ;;
    1|true|yes|YES) return 0 ;;
  esac
  # Default: push in CI so the remote is the shared ledger.
  if [[ "${CI:-}" == "true" || "${GITHUB_ACTIONS:-}" == "true" ]]; then
    return 0
  fi
  # Local: try push when origin exists (best-effort global uniqueness).
  git -C "$root" remote get-url "$remote" >/dev/null 2>&1
}

fetch_build_tags() {
  if ! git -C "$root" remote get-url "$remote" >/dev/null 2>&1; then
    return 0
  fi
  # Narrow fetch of build tags AND release tags: without the v* refspec a
  # machine that never runs `git fetch --tags` keeps stamping builds against a
  # stale base after someone else cuts a release. Fall back to --tags.
  if git -C "$root" fetch "$remote" \
    'refs/tags/build/*:refs/tags/build/*' \
    'refs/tags/v*:refs/tags/v*' --force 2>/dev/null; then
    return 0
  fi
  git -C "$root" fetch "$remote" --tags --force 2>/dev/null || true
}

create_local_tag() {
  local tag="$1"
  if [[ "$do_tag" != "1" ]]; then
    return 0
  fi
  # Reclaim if a dangling same-name tag points elsewhere (retry path).
  if git -C "$root" rev-parse -q --verify "refs/tags/$tag" >/dev/null 2>&1; then
    git -C "$root" tag -d "$tag" >/dev/null 2>&1 || true
  fi
  git -C "$root" tag "$tag" HEAD
}

push_tag() {
  local tag="$1"
  git -C "$root" push "$remote" "refs/tags/$tag:refs/tags/$tag" 2>/dev/null
}

# --- main ---
# Fetch before resolving the base so a release cut elsewhere is picked up by
# THIS build, not the next one.
fetch_build_tags

base="$(resolve_base)"
log "base=$base"

claimed=0
ver=""
attempt=0
n=0

while (( attempt < max_attempts )); do
  attempt=$((attempt + 1))

  n_tags="$(max_n_from_tags "$base")"
  n_file="$(max_n_from_file "$base")"
  n="$(max_int "$n_tags" "$n_file")"
  if [[ -n "${MCREMOTE_BUILD_N_MIN:-}" && "$n" -lt "${MCREMOTE_BUILD_N_MIN}" ]]; then
    n="${MCREMOTE_BUILD_N_MIN}"
  fi
  n=$((n + 1))
  ver="${base}.${n}"
  tag="build/${ver}"

  if [[ "$do_tag" != "1" ]]; then
    claimed=1
    break
  fi

  create_local_tag "$tag"

  if should_push; then
    if push_tag "$tag"; then
      log "claimed $tag (pushed to $remote)"
      claimed=1
      break
    fi
    log "push rejected for $tag (contention or no write); retrying…"
    git -C "$root" tag -d "$tag" >/dev/null 2>&1 || true
    fetch_build_tags
    sleep "0.$(( RANDOM % 5 ))"
    continue
  fi

  # No push: local tag + file is best-effort only.
  log "no remote push (offline or MCREMOTE_VERSION_PUSH=0); using local claim $tag"
  claimed=0
  break
done

if [[ -z "$ver" ]]; then
  log "failed to allocate version"
  exit 1
fi

# Offline / no-push uniqueness: avoid two machines silently shipping the same BASE.N.
if (( claimed == 0 )) && [[ "$do_tag" == "1" ]]; then
  uniq=""
  if [[ -n "${GITHUB_RUN_ID:-}" ]]; then
    uniq="ci${GITHUB_RUN_ID}"
  else
    uniq="g$(git -C "$root" rev-parse --short HEAD 2>/dev/null || echo local)"
  fi
  ver="${ver}.${uniq}"
  log "unique offline version: $ver"
fi

printf '%s %s\n' "$base" "$n" >"$file"
printf '%s\n' "$ver"
