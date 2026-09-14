# PLAN 0060: Local unsigned build and install — implementation

- **Status**: Implemented on master 2026-08-01 (steps 1–6; step 7 pending a Linux host)
- **Date**: 2026-08-01
- **MADR**: [0060-MADR-local-unsigned-build-and-install.md](0060-MADR-local-unsigned-build-and-install.md)
- **Scope**: `Makefile`, `scripts/next-build-version.sh`,
  `scripts/next-build-version_test.sh`, `README.md`. No Go source changes.
- **Nothing here alters signing.** The MADR's central finding is that the
  unsigned local install already works (D1). Every task below removes a side
  effect or a footgun.

## Ordering rationale

D8 goes first because it is the only defect that destroys working state: a
single `make install GOOS=linux` silently replaces a functioning daemon binary
with one the host cannot execute. D2 is next because every day it stays in
place, local builds keep appending tags to a public remote. The rest are
quality fixes in descending blast radius.

| Step | Decision | Files | Risk | Verified by |
|------|----------|-------|------|-------------|
| 1 | D8 | `Makefile` | Low | T1, T2 |
| 2 | D2 | `next-build-version.sh`, `_test.sh` | Low | T3, T4 |
| 3 | D4 | `Makefile` | None | T5 |
| 4 | D3 | `Makefile` | Low | T6 |
| 5 | D6 | `Makefile` | None | T7 |
| 6 | D5 | `README.md` | None | review |
| 7 | — | Linux host | — | T8 |

---

## Step 1 — D8: refuse cross-target installs, surface failures

**Problem.** `install` depends on `build`, which honours `GOOS`/`GOARCH`;
neither the target nor `scripts/install-binary.sh` checks the artifact against
the host. The post-install check `@"$(BIN)" version 2>/dev/null || true`
(lines 180, 183, 189) then discards both stderr and the exit status.

**Change 1a — host guard.** The Makefile already computes the host in
`UNAME_S`/`UNAME_M` before defaulting `GOOS`/`GOARCH`. Capture those defaults
under separate names at that point (`HOST_GOOS`, `HOST_GOARCH`) so the guard
compares against the host rather than against the possibly-overridden target.
Add a `.PHONY` prerequisite to `install` and `install-relay`:

```make
check-host-target:
	@if [ "$(GOOS)" != "$(HOST_GOOS)" ] || [ "$(GOARCH)" != "$(HOST_GOARCH)" ]; then \
		echo "refusing to install $(GOOS)/$(GOARCH) on $(HOST_GOOS)/$(HOST_GOARCH)." >&2; \
		echo "cross-compile with: make build GOOS=$(GOOS) GOARCH=$(GOARCH)" >&2; \
		exit 1; \
	fi
```

Order matters: the guard must run **before** `build`, so a rejected install
does not spend a minute compiling first.

`install: check-host-target build` does **not** guarantee that. GNU make is
free to run prerequisites concurrently under `-j`, so the compile could start
alongside the guard. Depend on the guard alone and recurse for the build:

```make
install: check-host-target
	@$(MAKE) build
	@mkdir -p "$(USER_BIN_DIR)"
	...
```

Apply the same treatment to `install-relay`, which has the identical shape
(`install-relay: build-relay`, then `install-binary.sh`, then a
`|| true` version check).

**Change 1b — stop swallowing failures.** Replace all three occurrences of
`2>/dev/null || true` on the post-install version check with a plain
invocation, so a binary that cannot execute fails the target.

Keep `|| true` nowhere in this path. If a freshly installed binary cannot
print its own version, the install did not succeed.

**Do not** add an architecture check inside `install-binary.sh`. That script's
contract is "swap a staged file safely around a running service"; target
validation belongs to the caller that chose the target.

### Acceptance

- **T1** — `make install GOOS=linux GOARCH=amd64` on macOS exits non-zero,
  prints the guidance line, performs no compilation, and leaves
  `~/.local/bin/mcremote` byte-identical to before the run.
- **T2** — plain `make install` on the host still succeeds end to end.

---

## Step 2 — D2: stop pushing build tags from local builds

**Problem.** `should_push()` ends with a fallback that pushes whenever
`origin` resolves, so laptop builds append `build/*` tags to a public
repository.

**Change.** Delete the trailing fallback, leaving the explicit-variable cases
and the CI check:

```sh
should_push() {
  case "${MCREMOTE_VERSION_PUSH:-}" in
    0|false|no|NO) return 1 ;;
    1|true|yes|YES) return 0 ;;
  esac
  # Default: push only in CI — the remote is the shared ledger. Local builds
  # take the offline claim path, which already appends a commit suffix so two
  # developers cannot silently ship the same BASE.N.
  [[ "${CI:-}" == "true" || "${GITHUB_ACTIONS:-}" == "true" ]]
}
```

**Safety.** Per MADR F7a, `ci.yml:146` sets `MCREMOTE_VERSION_PUSH` explicitly
on every run (`github.ref_type == 'tag' && '1' || '0'`), so CI never reaches
the default. The existing test exports `MCREMOTE_VERSION_PUSH=0` and is
likewise unaffected. This change also makes the code match the contract its
own header already documents.

**Test addition.** `scripts/next-build-version_test.sh` currently pins
`MCREMOTE_VERSION_PUSH=0` globally, so the default is untested. Add one case
that unsets the variable, unsets `CI`/`GITHUB_ACTIONS`, and asserts no push
was attempted against a throwaway remote — otherwise a future edit can
reintroduce the local push with nothing to catch it.

### Acceptance

- **T3** — with `MCREMOTE_VERSION_PUSH` unset and `CI` unset, `make build`
  emits an offline-claim version and `git ls-remote --tags origin` shows no
  new `build/*` tag.
- **T4** — with `CI=true`, push behavior is unchanged; the allocator test
  suite passes.

---

## Step 3 — D4: brace `${VER}` before the ellipsis

**Change.** Two sites only — `Makefile:128` (`build`) and `Makefile:384`
(`run`):

```make
echo "Building mcrelay ${VER}…"; \
echo "Running mcremote ${VER}…"; \
```

In the recipe these are written `$${VER}`. The remaining eight `$$VER` sites
are followed by a space or `"` and must not be touched.

**Cause, for the commit message.** The shell treats the leading byte of the
multibyte `…` as part of the variable name, resolves the unset `VER…`, and
prints nothing. Reproduce with
`VER=1.2.3 sh -c 'echo "[$VER…]"; echo "[${VER}…]"'`.

### Acceptance

- **T5** — `make build VERSION=9.9.9` prints a version on **both** the
  `mcremote` and `mcrelay` lines.

---

## Step 4 — D3: reproducible builds

**Change.** Replace the wall-clock stamp at `Makefile:13`:

```make
DATE ?= $(shell git log -1 --format=%cI 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
```

Honour `SOURCE_DATE_EPOCH` when set, for downstream reproducible-build
tooling. `DATE` feeds five `-ldflags` sites (126, 130, 143, 160, 386); the
single assignment covers all of them.

**Safety.** `scripts/verify-build-metadata.sh` inspects build **tags** via
`go version -m`, never the date, so it is unaffected. `main.date` is a plain
stamped variable in `cmd/*/main.go` with no consumer that parses it.

**Known trade.** The version string stops carrying build time. It keeps the
commit, and `-dirty` still marks a modified tree — which is the question that
matters when debugging from a version string.

### Acceptance

- **T6** — two consecutive `make build VERSION=9.9.9` runs produce
  byte-identical binaries (`shasum -a 256`) and an unchanged `CDHash`
  (`codesign -dvvv`). This is the check that was failing in MADR F6.

---

## Step 5 — D6: warn when `~/.local/bin` is off `PATH`

**Change.** A non-fatal notice at the end of `install`, both platforms:

```make
@case ":$$PATH:" in \
	*":$(USER_BIN_DIR):"*) ;; \
	*) echo "note: $(USER_BIN_DIR) is not on PATH; add it to run mcremote by name" ;; \
esac
```

Warn, never fail — the install itself succeeded, and the daemon's LaunchAgent
and systemd unit both invoke the binary by absolute path, so an absent `PATH`
entry affects interactive use only.

### Acceptance

- **T7** — `env PATH=/usr/bin:/bin make install` prints the notice and still
  exits zero.

---

## Step 6 — D5: document quarantine removal

**Change.** In the README section covering downloaded release binaries, note
that a browser download sets `com.apple.quarantine` and Gatekeeper will block
the binary until it is cleared:

```bash
xattr -d com.apple.quarantine ./mcremote-darwin-arm64-*
```

State the scope explicitly: this applies to **downloaded** artifacts only.
`make install` never produces a quarantined file, and the install path must
not strip attributes on its own behalf (MADR D5).

Worth one sentence nearby: `gh release download` does **not** set the
attribute, so the step is unnecessary when fetching through the CLI.

---

## Step 7 — Linux verification

Two acceptance rows in the MADR remain unverified because this pass ran on
macOS only. On a Linux host, confirm:

- **T8** — `make install` builds with `netgo,osusergo`, installs both
  binaries, and `install-binary.sh` takes the systemd branch: a running
  `mcremote.service` is stopped, swapped, and restarted; an enabled-but-stopped
  unit is healed.

Then flip the MADR acceptance matrix rows and move its status from
**Proposed** to **Accepted**.

---

## Out of scope

- **Developer ID / notarization** (MADR D1, rejected alternatives). Only
  required to distribute to Macs that did not build the binary.
- **Hardened runtime or entitlements** (MADR D7). These are CLI binaries with
  no bundle and no embedded frameworks.
- **Vendoring a Go toolchain** the way `magic-git` vendors Flutter. A Go build
  needs `go` and nothing else.
- **Windows.** The Makefile resolves `GOOS=windows` and a `.exe` suffix, but
  no service integration or install path is designed or tested for it.

## Commit sequence

One commit per step, each independently revertable:

1. `fix(make): refuse cross-target install and surface a broken install`
2. `fix(build): push build tags only from CI`
3. `fix(make): brace ${VER} before the ellipsis in two log lines`
4. `build(make): derive DATE from the commit for reproducible builds`
5. `feat(make): note when the install dir is off PATH`
6. `docs(readme): quarantine removal for downloaded macOS binaries`
