# MADR 0060: Local unsigned build and install on macOS and Linux

- **Status**: Accepted — D1–D8 implemented on master 2026-08-01. All macOS
  acceptance rows verified; the Linux row remains open pending a Linux host.
- **Date**: 2026-08-01
- **Deciders**: Project Owner
- **Scope**: `make install` for `mcremote` and `mcrelay` on a developer
  workstation — build flags, version stamping, binary placement, code
  signing, Gatekeeper, and service handoff. Covers Darwin and Linux hosts.
  Excludes distribution to third-party machines.
- **Related**:
  [0059-MADR-native-paths-and-linux-macos-parity.md](0059-MADR-native-paths-and-linux-macos-parity.md)
  (XDG layout, per-OS build tags),
  [0058-MADR-macos-launchd-service-hardening.md](0058-MADR-macos-launchd-service-hardening.md)
  (LaunchAgent design, no-sudo boundary).
- **Owner constraint**: installed binaries must run **without code signing** —
  no Apple Developer account, no Xcode signing identity, no notarization.

## Decision summary

`make install` already works on macOS. The requirement is met today: it builds
both binaries for `darwin/arm64`, installs them to `~/.local/bin`, and they
execute. No signing work is required, because the Go linker **already ad-hoc
signs** every `darwin/arm64` binary it produces, and locally built files never
carry a quarantine attribute for Gatekeeper to act on.

What this MADR changes is not the ability to install — it is the set of side
effects that make local installs unpleasant: local builds push git tags to a
public remote, every rebuild changes the binary's code identity, and one log
line prints an empty version. Four small fixes close those.

## Context

The daemon is developed on a macOS laptop and deployed on Linux. The owner
wants one command — `make install` — to produce running binaries on either
host, with no Apple developer identity involved. macOS is the constrained
side: since Apple Silicon, the kernel refuses to execute an arm64 binary that
carries **no** signature at all, which raises the question of whether an
unsigned local workflow is even possible.

It is, and the distinction that makes it possible is the one that this
document exists to record: **arm64 requires a signature, but not a trusted
one.** An ad-hoc signature — no certificate, no Team ID, no Apple involvement —
satisfies the loader. Gatekeeper, which is what actually demands Developer ID
and notarization, only adjudicates files carrying `com.apple.quarantine`, and
a file the compiler just wrote never has one.

## Research baseline

All findings below were verified on this host (macOS 15.5, arm64, Go 1.26.5)
rather than inferred from documentation.

### F1 — `make install` succeeds on macOS today

`MCREMOTE_VERSION_PUSH=0 make install` built and installed both binaries:

```
Building mcremote 0.6.0.3.ga1f0829 (darwin/arm64, cgo=0, tags=)
Installed /Users/<user>/.local/bin/mcremote
Installed /Users/<user>/.local/bin/mcrelay
```

`~/.local/bin` is the install target on both platforms (Makefile
`USER_BIN_DIR`), matching the `setup-service` default. It was already on
`PATH` on this host.

### F2 — Go ad-hoc signs `darwin/arm64` automatically

```
CodeDirectory v=20400 flags=0x20002(adhoc,linker-signed)
Signature=adhoc
TeamIdentifier=not set
```

`codesign --verify --strict` reports the signature **valid** for both
binaries. This is the Go linker's own signing, not a post-build `codesign`
step, and it needs no Apple tooling. The `-ldflags -s -w` strip happens at
link time, before signing, so it does not invalidate the signature.

### F3 — Gatekeeper rejects, and it does not matter

`spctl -a -vv -t execute` returns `rejected` for both binaries — correct and
expected, since neither carries Developer ID. Execution is unaffected:

- Neither installed binary has a `com.apple.quarantine` attribute.
- Both run from an interactive shell.
- Both run from a clean login shell with a scrubbed environment
  (`env -i … /bin/sh -lc 'mcremote version'`).

`spctl` reports what Gatekeeper *would* say if asked. Nothing asks it for a
locally compiled, non-quarantined binary invoked by path.

### F4 — Cross-compiled Darwin artifacts are signed too

The `darwin/arm64` binary from the v0.6.0 GitHub Release — cross-compiled on
`ubuntu-latest` — carries the same `adhoc,linker-signed` signature. Go's
signing is host-independent, so CI artifacts run on Apple Silicon without a
post-processing step on a Mac.

`darwin/amd64` carries **no** signature. This is fine: the signature
requirement is an arm64 kernel policy, and x86_64 macOS still executes
unsigned binaries.

### F5 — Downloaded artifacts depend on the download tool

`gh release download` produced files with **no** quarantine attribute, and
they ran immediately. A browser download would set the attribute, and
Gatekeeper would then block the binary until it is cleared:

```bash
xattr -d com.apple.quarantine ./mcremote-darwin-arm64-*
```

This is the only case in the whole workflow where a manual signing-adjacent
step is required, and it never applies to `make install`.

### F6 — Builds are not reproducible, so code identity churns

Two consecutive builds pinned to the same `VERSION` produced **different**
binaries. Pinning `DATE` as well made them byte-identical, which isolates the
cause: the Makefile stamps `DATE := $(shell date -u …)` into `-ldflags` on
every invocation.

The consequence is not aesthetic. Every rebuild changes the binary's `CDHash`,
so macOS sees a *different program* each time. Anything that remembers a
decision keyed to code identity — Application Firewall approvals, TCC grants —
must re-ask after every `make install`.

The Application Firewall is currently **disabled** on this host, so the
symptom is dormant. It would surface the moment it is enabled, as a repeating
"accept incoming network connections?" prompt for a daemon that binds a port.

### F7 — Local builds push tags to a public remote

`scripts/next-build-version.sh` claims a build serial by creating
`build/<BASE>.<N>` and pushing it. Its default is to push whenever `origin`
exists and is writable — CI *and* laptop alike. A developer running
`make install` a few times a day silently appends tags to a public GitHub
repository.

With `MCREMOTE_VERSION_PUSH=0` the script falls back cleanly, emitting a
locally unique version with a commit suffix
(`0.6.0.3.ga1f0829`), which is exactly the right behavior for a local build.
It simply is not the default.

### F7a — CI never relies on the push default (grounding for D2)

`ci.yml:146` sets the variable explicitly on every run:

```yaml
MCREMOTE_VERSION_PUSH: ${{ github.ref_type == 'tag' && '1' || '0' }}
```

So CI pushes a build tag **only** on tag builds, and it never falls through to
`should_push`'s default. Changing that default therefore cannot alter CI
behavior — the blast radius is local builds only. `scripts/next-build-version_test.sh`
likewise exports `MCREMOTE_VERSION_PUSH=0`, so it does not exercise the default
path either.

The script's own header already documents the intended behavior as
`default: push in CI`. The implementation adds an undocumented third rule —
push locally whenever `origin` resolves — so D2 aligns the code with the
comment rather than changing a deliberate design.

### F8 — Two log lines lose their version string

```
Building mcremote 9.9.9 (darwin/arm64, cgo=0, tags=)…
Building mcrelay …
```

`Makefile:128` reads `echo "Building mcrelay $$VER…"`. With no delimiter
between the variable and the following `…`, the shell parses the multibyte
ellipsis as part of the variable name, resolves `$VER…` as unset, and prints
nothing. Confirmed directly:

```
$ VER=1.2.3 sh -c 'echo "A: [$VER…]"; echo "B: [${VER}…]"'
A: []
B: [1.2.3…]
```

Cosmetic only — the adjacent `-X main.version=$$VER ` is followed by a space
and expands correctly, and both binaries do report the right version.

An audit of all ten `$$VER` sites in the Makefile found **two** affected, not
one — every other site is followed by a space or `"`:

| Line | Target | Text |
|------|--------|------|
| 128 | `build` | `echo "Building mcrelay $$VER…"` |
| 384 | `run` | `echo "Running mcremote $$VER…"` |

### F8a — `make install` has no host/target guard

`install` depends on `build`, which honours `GOOS`/`GOARCH`. Nothing between
the two checks that the artifact matches the host, and
`scripts/install-binary.sh` performs a plain `install -m 755` with no
architecture check. So on a Mac:

```bash
make install GOOS=linux GOARCH=amd64
```

builds an ELF and writes it over `~/.local/bin/mcremote`, destroying a working
install. Verified that such a binary is `ELF 64-bit LSB executable, x86-64`
and that macOS refuses to execute it.

The failure is also **silent**: the install target's verification step is
`@"$(BIN)" version 2>/dev/null || true` (lines 180, 183, 189), which discards
both the error output and the exit status. The user sees `Installed …` and no
indication that the binary cannot run.

The README already states the intent — `make build GOOS=linux GOARCH=arm64
# cross-compile only (no install)` — but nothing enforces it.

### F9 — Linux needs nothing further

Linux has no signature requirement and no Gatekeeper. The Makefile already
selects `netgo,osusergo` for `GOOS=linux` and no tags for Darwin (MADR 0059
D9), and `scripts/install-binary.sh` already detects systemd, LaunchAgent, or
neither, staging the new binary before stopping anything. The install path is
functionally identical on both platforms; only F6 and F7 apply to both, and
they are host-agnostic.

### F10 — Prior art in the sibling Flutter project

`magic-git/build_macos.sh` solves the harder version of this problem for a
`.app` bundle: it vendors a throwaway Flutter SDK, offers `--unsigned` (ad-hoc)
builds, and strips quarantine on install. Its hard-won lesson —
`ENABLE_HARDENED_RUNTIME` must stay **off** while ad-hoc signed, because
hardened-runtime library validation compares Team IDs and an ad-hoc signature
has none, so `dyld` refuses to load the embedded framework — **does not apply
here.** That failure needs an embedded framework to reject. `mcremote` and
`mcrelay` are statically linked pure-Go binaries with no embedded libraries,
no bundle, and no entitlements. The trap is worth knowing and worth not
copying.

## Gap and risk assessment

| # | Gap | Severity | Platform |
|---|-----|----------|----------|
| G1 | Local builds push `build/*` tags to a public remote by default | **High** — pollutes a shared ledger from a laptop | Both |
| G2 | `DATE` makes every build unique, churning code identity | Medium — dormant while the firewall is off | macOS |
| G3 | `mcrelay` build log prints an empty version | Low — cosmetic | Both |
| G4 | Quarantine removal for browser-downloaded artifacts is undocumented | Low — does not affect `make install` | macOS |
| G5 | `~/.local/bin` on `PATH` is assumed, never checked | Low | Both |
| G6 | `make install GOOS=…` overwrites a working install with a foreign-arch binary, and the failure is swallowed by `\|\| true` | **High** — destroys a working install silently | Both |

No gap blocks the owner requirement. `make install` works unsigned on macOS
today.

## Decisions

**D1 — Keep ad-hoc linker signing. Do not adopt Developer ID for local
installs.** The Go toolchain already satisfies the arm64 loader with no Apple
account, and Gatekeeper never adjudicates a locally built binary. Developer ID
and notarization buy nothing for a binary that is compiled and executed on the
same machine. Revisit only if binaries are distributed to Macs that did not
build them.

**D2 — Default local builds to no tag push; keep pushing in CI.** Drop
`should_push`'s final `git remote get-url` fallback so a tag is pushed only
when `CI`/`GITHUB_ACTIONS` is set or `MCREMOTE_VERSION_PUSH=1` is explicit.
The remote ledger stays authoritative for CI builds; laptops use the offline
claim path that already exists and already produces collision-safe versions.
Per F7a this cannot affect CI, which sets the variable explicitly, and it
makes the code match its own documented contract. Fixes G1.

**D3 — Make builds reproducible by default.** Derive `DATE` from the commit
timestamp rather than wall-clock time:

```make
DATE ?= $(shell git log -1 --format=%cI 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
```

Identical source then yields an identical binary and a stable `CDHash`, so
firewall and TCC decisions survive rebuilds. Honour `SOURCE_DATE_EPOCH` if
present. Fixes G2.

**D4 — Brace the variable where an ellipsis follows it.** `${VER}` instead of
`$VER` at `Makefile:128` (`build`) and `Makefile:384` (`run`) — the two sites
identified in F8. Leave the other eight alone; they are already delimited.
Fixes G3.

**D8 — Refuse to install a binary the host cannot run, and stop hiding the
failure.** Add a guard to the `install` target: when the resolved
`GOOS`/`GOARCH` differ from the host's, fail with a message pointing at
`make build` for cross-compilation, before `install-binary.sh` is reached.
Separately, drop `|| true` from the post-install version check so an
unrunnable binary reports loudly rather than printing `Installed` and moving
on. Fixes G6.

This is the one decision here that prevents data loss rather than annoyance:
the current behavior replaces a working daemon binary with an unrunnable one
and says nothing.

**D5 — Document, do not automate, quarantine removal.** Add the `xattr -d`
one-liner to the README's download instructions. Do not have `make install`
strip quarantine: it never encounters a quarantined file, and a build tool
that clears security attributes as a matter of course is a bad habit to
encode. Fixes G4.

**D6 — Warn once when `~/.local/bin` is absent from `PATH`.** A non-fatal
notice at the end of `install`, on both platforms. Fixes G5.

**D7 — No hardened runtime, no entitlements, no bundle.** These binaries are
CLI executables. Hardened runtime exists to constrain bundled apps loading
external code; adopting it here would add the Team-ID failure mode described
in F10 while protecting nothing.

## Acceptance matrix

| Check | Expected | Verified |
|-------|----------|----------|
| `make install` on macOS arm64 | Both binaries installed to `~/.local/bin` | ✅ F1 |
| Installed binary executes | `mcremote version` from a clean login shell | ✅ F3 |
| Signature valid | `codesign --verify --strict` passes | ✅ F2 |
| No Apple account needed | No Xcode identity, no notarization | ✅ F2 |
| CI Darwin artifacts run on arm64 | ad-hoc signed when cross-compiled | ✅ F4 |
| `make install` on Linux | systemd-aware swap, `netgo,osusergo` | ⬜ untested — needs a Linux host |
| Local build leaves remote untouched | No `build/*` tag pushed | ✅ T3/T4 |
| Rebuild is byte-identical | Same source → same `CDHash` | ✅ T6 |
| Cross-target install refused | `make install GOOS=linux` fails before writing | ✅ T1, incl. under `-j4` |
| Broken install is visible | Post-install version check fails loudly | ✅ T2 |
| Both build lines show a version | `mcremote` and `mcrelay` | ✅ T5 |
| `PATH` notice | Non-fatal when install dir is off `PATH` | ✅ T7 |

## Consequences

Local installs on macOS work with no signing infrastructure, and that
conclusion is stable — it rests on the Go linker's ad-hoc signing and on
Gatekeeper's quarantine scoping, neither of which is a workaround.

After D2, a laptop build no longer participates in the global build serial. Two
developers can produce `0.6.0.4` independently; the commit suffix on the
offline claim keeps the strings distinguishable. This is the correct trade:
the ledger exists to give CI artifacts a monotonic identity, and a local
debug binary does not need one.

After D3, the version string stops encoding build time. Anyone debugging from
a version string alone loses "when was this compiled" and keeps "what commit
is this" — which is the question that actually matters, and `-dirty` still
marks a modified tree.

The unsigned posture has a hard boundary: these binaries will **not** run on
another Mac that downloads them through a browser without clearing quarantine
first, and they will never pass `spctl`. Distributing to machines the owner
does not control requires Developer ID and notarization, and is out of scope
here.

## Rejected alternatives

**Sign with a self-signed certificate.** Adds keychain setup and a signing
step to every build, and buys nothing: a self-signed identity is no more
trusted by Gatekeeper than an ad-hoc signature, and the loader is already
satisfied.

**Adopt Developer ID + notarization for local installs.** Requires a paid
Apple Developer account and a network round-trip to Apple's notary service on
every build. Solves a distribution problem the owner does not have.

**Install to `/usr/local/bin` via `sudo`.** Contradicts the no-sudo boundary
established in MADR 0058, and `~/.local/bin` is already on `PATH` and already
the `setup-service` default.

**Have `make install` strip quarantine defensively.** It would be a no-op in
every case the target actually encounters. See D5.

**Vendor a Go toolchain the way `magic-git` vendors Flutter.** Justified there
because a macOS `.app` build needs a specific Flutter SDK, Xcode, and
CocoaPods. A Go build needs `go` and nothing else.

## Implementation order

Sequenced in [0060-PLAN-local-unsigned-build-and-install.md](0060-PLAN-local-unsigned-build-and-install.md).
Summary: **D8** first (prevents destroying a working install), then **D2**
(stops remote pollution), then **D4**, **D3**, **D6**, **D5**, and finally a
Linux host check to close the untested acceptance rows.

## Sources consulted

- Live verification on macOS 15.5 arm64: `codesign -dv`, `codesign --verify
  --strict`, `spctl -a -vv -t execute`, `xattr -p com.apple.quarantine`,
  `lipo -archs`, clean-environment execution.
- v0.6.0 GitHub Release artifacts (cross-compiled on `ubuntu-latest`).
- `Makefile`, `scripts/install-binary.sh`, `scripts/next-build-version.sh`.
- `magic-git/build_macos.sh` — ad-hoc signing and hardened-runtime prior art.
- MADR 0058 (LaunchAgent, no-sudo), MADR 0059 D9 (per-OS build tags).
