---
status: in-progress
date: 2026-08-27
associated-madr: "0116-MADR-windows-and-linux-arm64-build-targets.md"
owner: [Project Owner]
target-milestone: "Windows amd64/arm64 + linux/arm64 build targets"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Implement the Windows and linux/arm64 build targets

Associated MADR: [0116-MADR-windows-and-linux-arm64-build-targets.md](0116-MADR-windows-and-linux-arm64-build-targets.md)

## Goal

Make `mcremote` and `mcrelay` build, test and run on `windows/amd64` with
**no shims and no silently-absent invariants**, close the `linux/arm64`
verification gap, and give `appdirs` a native per-OS root provider under one
written idempotency contract — implementing decisions D1–D19 and closing
findings F1–F21.

**`windows/arm64` is out of scope** (MADR D19, owner decision 2026-08-27). It
costs this plan nothing to leave out: every file below is `//go:build windows`,
which says nothing about `GOARCH`, so the deferral removes matrix lines and no
code.

The finish line is mechanical, not editorial:

* `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` exits 0 with **no
  file named `*probe*` or `*shim*` in the tree**;
* a `windows-latest` CI job runs `CGO_ENABLED=0 go test ./...` green;
* `make verify-build-metadata` proves the tag policy on all **five** targets;
* every Windows behaviour that cannot be made safe **returns a typed error**
  rather than degrading.

## Scope

### In scope (the only files any phase may touch)

**New per-platform files (created):**

* `internal/appdirs/`: `roots_unix.go`, `roots_windows.go`, `ensure_windows.go`,
  `runtime_windows.go`, `sockpath_unix.go`, `sockpath_windows.go`
* `internal/fsutil/`: `lock_windows.go`, `syncdir_unix.go`, `syncdir_windows.go`
* `internal/auth/filelock_windows.go`
* `internal/certs/filelock_windows.go`
* `internal/admin/`: `owner_unix.go`, `owner_windows.go`
* `internal/procutil/`: `procutil_windows.go`, `owner_windows.go`,
  `starttoken_windows.go`
* `internal/cli/`: `signals_unix.go`, `signals_windows.go`
* `internal/relay/signals_unix.go`, `internal/relay/signals_windows.go`
* `internal/provider/launch/` (new package): `launch.go`, `launch_unix.go`,
  `launch_windows.go`, plus tests
* `internal/cli/service/`: `setup_windows.go`, `control_windows.go`,
  `schtasks_windows.go`
* `scripts/install.ps1`
* `docs/ops-windows-install.md`

**Existing files edited:**

* `internal/appdirs/`: `roots.go`, `paths.go`, `product.go`, `ensure_unix.go`,
  `runtime_unix.go`, and their `*_test.go`
* `internal/fsutil/`: `atomic.go`, `lock_unix.go`, `atomic_test.go`
* `internal/admin/admin.go`, `admin_test.go`
* `internal/procutil/`: `procutil_other.go`, `owner_other.go`,
  `starttoken_other.go`, `registry.go`, and tests
* `internal/auth/filelock_other.go`, `internal/certs/filelock_other.go`
* `internal/cli/serve.go`, `internal/relay/cli.go`
* `internal/update/`: `swap.go`, `github.go`, `run.go`, and tests
* `internal/cli/service/`: `setup.go`, `control.go`
* `internal/provider/`: `codex/provider.go`, `acpagent/acpagent.go`,
  `acphttp/provider.go`, `httpagent/provider.go` — **only** the
  `exec.LookPath` → `launch.Resolve` substitution
* `Makefile`, `.github/workflows/ci.yml`,
  `scripts/verify-build-metadata.sh`, `scripts/install.sh`
* `README.md`, `docs/config.md`, `docs/config-mcrelay.md`, `docs/ops-mcrelay.md`

### Scope expansion recorded 2026-08-27

An earlier draft of this plan fenced off `ci.yml`'s existing `go` job
entirely. The owner's A + B decision on MADR open question 0 brings it in:
**P8 adds one step** — a `CGO_ENABLED=0 go test ./...` pass — to that job, and
changes nothing else about it. The existing `go test -race ./...` step is
**not** edited, reordered, or renamed.

### Out of scope

**`windows/arm64` — the whole target.** Not built, not published, not tested
(MADR D19). No `PLATFORMS` entry, no `verify-build-metadata` case, no
`windows-11-arm` CI job. The re-entry recipe lives in D19 and in this plan's
Deferred section; nothing in P1–P10 needs to change to enable it later.

Authenticode certificate **procurement** (D14 wires the hook; buying the cert
is an owner action). MSI/winget packaging. Live provider suites on Windows
(D16 fixes Windows at Tier 2). Any change to envelope JSON shapes, wire error
codes, or the pairing protocol. The Flutter app. Codex `unix_ws` /
`managed_daemon_proxy` transports (D15 — they stay refused). `internal/tcc`.
`internal/session/store.go`'s `safeDir` path-traversal semantics beyond the
one Windows test added in P3.

Anything discovered mid-execution outside this list **stops and waits** per
`AGENTS.md` and the global plan-deviation rule.

## Stability rule

Every phase ends with, in order:

```bash
make pre-add-check                      # on that phase's staged Go files
gofmt -l cmd internal                   # must print nothing
go vet ./...                            # host platform
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./...
CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build ./...
CGO_ENABLED=0 go test -race -count=1 ./...   # host (darwin): race WITH cgo off
```

`CGO_ENABLED=0` on the test line is not decoration — it is contract C7. Darwin
is the only platform where `-race` and cgo-off coexist
(`cmd/go/internal/work/init.go:194–204`), so the owner's Mac is where this
plan's race coverage lives.

then **one commit** (`git commit --no-edit` — the repo hook writes the
message; never pass `-m`). **No `git push` at any point in this plan.**

The four cross-compile lines above are the phase gate that makes this plan
deterministic: from P1 onward, **no phase may leave any of the five targets
un-buildable.** P1 is the only phase permitted to start with a red Windows
build, because it is the phase that turns it green.

## Cross-cutting contracts

Every phase must honour these. They are the reason the port does not become a
pile of `if runtime.GOOS == "windows"`.

**C1 — Build tags, not runtime branches.** Platform divergence lives in
`_unix.go` / `_windows.go` files behind an identical exported signature.
`runtime.GOOS` comparisons are added **only** where an existing seam already
uses one (`cli/service`'s `installOS`, `ws/permission_copy.go`), so tests can
drive the other platform's branch on this host.

**C2 — The idempotency contract (D4), verbatim, in every implementation's doc
comment:**

> Idempotent and converging. Creates the directory and any parents if absent;
> if present, verifies owner and access and repairs access to the private
> state; returns an error rather than repairing when the leaf is a symlink,
> reparse point, or not owned by the current principal. A second call on a
> converged directory performs no writes.

**C3 — Fail closed, never degrade.** A Windows path that cannot establish the
security property returns an error. The three no-op fallbacks that violate
this today (`auth/filelock_other.go`, `certs/filelock_other.go`,
`procutil/procutil_other.go`) get real Windows implementations; the `!unix`
files survive only for genuinely unsupported platforms (js/wasm, plan9) and
their doc comments are narrowed to say exactly that.

**C4 — `Resolve` stays pure.** `appdirs.Resolve` keeps its
*"Pure: no filesystem side effects"* guarantee (`paths.go:29–31`) and stays
OS-agnostic. All platform divergence is in `SystemRoots`, which returns
`Roots`. Tests inject `Roots` and therefore test **both** layouts on **any**
host — this is what makes the Windows path layout testable on the owner's Mac.

**C5 — One asset-naming convention (F17).** Written once as
`<product>-<goos>-<goarch>[-<VER>][<ext>]`, extension always last. Three
parsers must agree: `ci.yml`'s alias loop, `scripts/install.sh`'s
`verify_and_resolve`, and `update/github.go`'s `AssetFor`. P8 fixes all three
together and adds a test to each that can be traced to the same convention.

**C6 — Exported API is additive.** No existing exported signature in
`appdirs`, `fsutil`, `admin` or `procutil` changes. Callers
(`daemon/daemon.go:86`, `daemon/certs.go:29`, `procutil/registry.go:78`,
`relay/fileconfig.go:803`, `admin/admin.go:81–88`, four `providerauth` sites)
are **not edited** in P1–P3. If a phase finds it must change one, that is a
deviation — stop and prompt.

## Where each target is actually executed

Cross-compiling proves a target links. Only running it proves it works. This
plan therefore fixes, up front, where every one of the five targets is
*executed* — because MADR F17 is exactly the bug that "we cross-compile it, so
it's covered" produces.

All entries run `CGO_ENABLED=0` (C7), with one sanctioned exception: the
`ubuntu-latest` race pass kept by MADR D20a. The `-race` column is where the
cgo constraint bites.

| Target | CI execution | Local execution | `-race` |
| --- | --- | --- | --- |
| `linux/amd64` | `ubuntu-latest` — **two passes**: `-race` (cgo) and `CGO_ENABLED=0` | — | yes, on the cgo pass (MADR D20a) |
| `linux/arm64` | `ubuntu-24.04-arm` | Mac, native aarch64 VM | no — forces cgo |
| `darwin/arm64` | — (cross-built only) | **owner's Mac** | **yes** — cgo-off exemption |
| `darwin/amd64` | — (cross-built only) | — | n/a |
| `windows/amd64` | `windows-latest` | owner's Windows laptop | no — forces cgo |

`windows/arm64` appears nowhere in that table on purpose: MADR D19 takes it out
of scope. It was the only candidate target that could run neither the race
detector (F19) nor a local acceptance pass (D18), so dropping it removes the
matrix's weakest leg rather than any coverage the other five provide.

Three rules fall out of this table and bind every phase:

1. **No emulation.** QEMU is not introduced anywhere. On the owner's Apple
   Silicon Mac a `linux/arm64` container runs natively via
   Virtualization.framework, and in CI `ubuntu-24.04-arm` is real arm64
   hardware. A grep gate in P8 enforces this.
2. **The Windows laptop is never a runner.** This repository is public;
   GitHub's own guidance is that self-hosted runners should almost never serve
   a public repo, because any fork PR can execute code on them and they are
   non-ephemeral by default (F20). The laptop runs
   `scripts/acceptance-windows.ps1` by hand, and that script says so in its
   header.
3. **Every target that ships is executed somewhere.** With `windows/arm64` out
   of scope there is no published artifact whose only evidence is a successful
   cross-compile — which is the exact failure mode that let F17 sit unnoticed.
4. **Race coverage lives on darwin and on the one sanctioned `ubuntu-latest`
   pass.** Darwin is the only platform where `-race` and `CGO_ENABLED=0`
   coexist, and it is the per-phase gate; `ubuntu-latest` adds Linux/amd64
   race coverage at the cost of cgo in that test binary alone (MADR D20a).
   The remaining blind spot is a race that manifests only under `linux/arm64`
   or `windows/amd64` scheduling.

**C7 — `CGO_ENABLED=0` everywhere, builds and tests (MADR D20), and
**asserted on every shipped artifact** (MADR D21). Every command this plan
adds — build, vet, test, container run, CI step — sets `CGO_ENABLED=0`
explicitly rather than inheriting it, and **no binary is published until
`go version -m` confirms `build CGO_ENABLED=0` on the file itself.** The
artifact assertion is the load-bearing half: the recipe can be bypassed, the
artifact check cannot. Two consequences that
must not be worked around:

* **`-race` is used on darwin only.** Off darwin it forces `CGO_ENABLED=1`
  (`go: -race requires cgo`, reproduced for `linux/arm64` and
  `windows/amd64`). The `ubuntu-24.04-arm` and `windows-latest` legs therefore
  run plain `go test`. Adding `-race` back to those legs is not a fix, it is a
  violation of C7.
* **Container runs must set it explicitly.** The `golang` images default to
  `CGO_ENABLED=1`, so `scripts/test-linux-arm64.sh` passes
  `-e CGO_ENABLED=0`; inheriting the image default would silently break the
  contract.

**The one sanctioned cgo-enabled command in the repository** is `ci.yml`'s
existing `go test -race ./...` on `ubuntu-latest`. Per MADR **D20a** (owner
decision 2026-08-27, options A + B) it is **kept**, and P8 adds a second pass
beside it:

```yaml
- name: Test (race, all packages)          # existing, unchanged
  run: go test -race ./...                 # implies CGO_ENABLED=1
- name: Test (cgo-free, all packages)      # added by P8
  run: CGO_ENABLED=0 go test ./...
```

Two rules follow, and they are the ones a later phase is most likely to get
wrong:

* **Exactly one `-race` invocation may exist in `.github/workflows/`.** Not
  zero — deleting it loses the project's only automated Linux race coverage.
  Not two — a second would be a new cgo dependency. P8 installs a count-based
  gate rather than a line-number one, because adding the cgo-free step moves
  every line below it.
* **Test-binary cgo never reaches an artifact.** D21's assertion runs on the
  published files, so how the race test binary is linked cannot affect what
  ships. That separation is what makes keeping the race pass safe.

## Dependency and delivery order

```text
P1 ── P2 ── P3 ─┬─ P4 ── P5
                ├─ P6
                └─ P7
                      └── P8 ── P9 ── P10
```

* **P1** (appdirs native roots) unblocks everything — it is what makes the
  tree compile for Windows.
* **P2** (durability + locking) must precede P3, because P3's admin DACL
  reuses P1's security-descriptor helper and P2's lock.
* **P3** (admin control plane) closes the security hole; P4–P7 are independent
  of each other and may be reordered by the owner without breaking anything.
* **P8** (packaging) must follow P1–P3 so CI has something green to build.
* **P9** (Windows background execution) depends on P8's CI lane to prove
  itself. It was gated on MADR open questions 2 and 3; both were answered
  2026-08-27, so it is now unblocked and carries **no one-way door** — the
  per-user model leaves D3's path table untouched.
* **P10** (docs, coverage, final regression) is last by definition.

**P1–P3 + P8 is the minimum shippable Windows target.** P4–P7 and P9 improve
it; P9 can still be deferred to a follow-up number without leaving the tree
inconsistent, because `cli/service` already returns a clean
`service control unsupported on windows` (F11). It is no longer *blocked*,
only optional.

---

## Implementation Steps

### P1 — `appdirs`: native roots, idempotent ensure, and the end of the shims (F1, F4)

**Outcome.** `SystemRoots` returns Known-Folder roots on Windows and today's
XDG roots on Unix, from one pure `Resolve`. `EnsurePrivateDir`,
`ValidateRuntimeDir` and `CheckSocketPathLength` exist on every platform under
contract C2. The three `undefined:` errors in `appdirs` are gone.

**Files.** `internal/appdirs/roots.go` (edit), `roots_unix.go` (create),
`roots_windows.go` (create), `ensure_unix.go` (edit — doc only),
`ensure_windows.go` (create), `runtime_unix.go` (split),
`runtime_windows.go` (create), `sockpath_unix.go` (create),
`sockpath_windows.go` (create), `paths.go` (edit), `product.go` (edit — doc),
`paths_test.go` (edit), `roots_windows_test.go` (create),
`ensure_unix_test.go` (edit — rename symbols only).

**Steps.**

1. **Split `SystemRoots`.** In `roots.go`, keep `Roots`, `Diagnostic` and the
   `xdgOr` helper. Replace the body of `SystemRoots` with a dispatch to an
   unexported `systemRoots(product Product) (Roots, []Diagnostic, error)`
   defined per platform. Move today's implementation verbatim into
   `roots_unix.go` (`//go:build unix`) — **including** `resolveRuntimeHome`
   and its `os.Getuid()` / `/run/user` logic, which becomes Unix-only and
   therefore correct.

2. **Delete the unconditional `Logs` line.** `roots.go:66` currently sets
   `Logs: filepath.Join(home, "Library", "Logs")` on every platform. It moves
   into `roots_unix.go` guarded by `runtime.GOOS == "darwin"`, and `Logs` is
   `""` on Linux. `Resolve` already tolerates an empty `Logs`
   (`paths.go:57`), so `Paths.LogDir` becomes empty on Linux — which is what
   the field's own doc (*"LaunchAgent stdio directory on Darwin"*) always
   said it meant.

3. **Create `roots_windows.go`** (`//go:build windows`):

   ```go
   //go:build windows

   package appdirs

   import (
       "fmt"
       "os"
       "path/filepath"

       "golang.org/x/sys/windows"
   )

   // knownFolder is a test seam: production reads the real Known Folder, and
   // roots_windows_test.go substitutes a temp dir.
   var knownFolder = func(id *windows.KNOWNFOLDERID) (string, error) {
       return windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
   }

   // systemRoots resolves the Windows layout from Known Folders (MADR 0116 D3).
   //
   // Roaming vs local is deliberate: config is the only thing a user wants to
   // follow them onto another machine, so it lives under RoamingAppData
   // (%AppData%). Data, state, cache, logs and the runtime dir are
   // machine-specific and must NOT roam — a session store or an admin socket
   // replicated to a second machine is at best useless and at worst confusing,
   // so they live under LocalAppData. This mirrors what os.UserConfigDir and
   // os.UserCacheDir already choose (os/file.go:504-512, 557-565).
   func systemRoots(product Product) (Roots, []Diagnostic, error) {
       home, err := os.UserHomeDir()
       if err != nil {
           return Roots{}, nil, fmt.Errorf("home dir: %w", err)
       }
       roaming, err := knownFolder(windows.FOLDERID_RoamingAppData)
       if err != nil {
           return Roots{}, nil, fmt.Errorf("known folder RoamingAppData: %w", err)
       }
       local, err := knownFolder(windows.FOLDERID_LocalAppData)
       if err != nil {
           return Roots{}, nil, fmt.Errorf("known folder LocalAppData: %w", err)
       }
       base := filepath.Join(local, product.Name)
       return Roots{
           Home:        filepath.Clean(home),
           ConfigHome:  filepath.Clean(roaming),
           DataHome:    filepath.Clean(local),
           StateHome:   filepath.Join(base, "State"),
           CacheHome:   filepath.Join(base, "Cache"),
           RuntimeHome: filepath.Join(base, "Runtime"),
           Temp:        filepath.Clean(os.TempDir()),
           Logs:        filepath.Join(base, "Logs"),
       }, nil, nil
   }
   ```

   **No `os.Getuid()` appears anywhere in this file.** The runtime root is
   per-user because `FOLDERID_LocalAppData` already is — which is precisely
   how F4's `mcremote-runtime--1` collision is removed rather than patched.

   Note the `StateHome`/`CacheHome`/`RuntimeHome` values already include the
   product leaf, and `Resolve` joins `product.Name` again
   (`paths.go:52–55`), producing `…\mcremote\State\mcremote`. **Fix in
   `Resolve`, not here** — see step 5.

4. **Case-fold the instance key on Windows.** `InstanceKey`
   (`paths.go:124`) hashes the cleaned data dir. NTFS is case-insensitive, so
   `C:\Users\X` and `c:\users\x` are one directory but two keys today. Add to
   `paths.go`:

   ```go
   // instanceKeyInput normalizes dataDir before hashing. On a case-insensitive
   // filesystem two spellings of one directory must produce one instance key,
   // or a daemon started from a differently-cased path silently gets its own
   // runtime dir and engine registry (MADR 0116 D3).
   func instanceKeyInput(dataDir string) string {
       if runtime.GOOS == "windows" {
           return strings.ToLower(filepath.Clean(dataDir))
       }
       return filepath.Clean(dataDir)
   }
   ```

   and call it from `InstanceKey`. This is the one sanctioned `runtime.GOOS`
   under C1: the behaviour must be testable from the Unix host, and a build
   tag would make `TestInstanceKeyCaseFolding` unrunnable on the owner's Mac.

5. **Stop double-joining the product leaf.**

   > **Deviation 2026-08-27 (P1 execution).** ~~The step below specified a
   > `joinProduct(root, name)` helper keyed on
   > `filepath.Base(filepath.Clean(root)) == name`.~~ **That predicate is
   > wrong and was replaced.** The Windows `StateHome` is
   > `%LocalAppData%\mcremote\State`, whose base is `State`, not `mcremote` —
   > the product name is an *ancestor*. The predicate never fired and
   > `Resolve` appended the leaf twice
   > (`...\mcremote\State\mcremote`), caught by
   > `TestResolveDoesNotDoubleJoinProductLeaf`.
   >
   > **Resolution (owner, 2026-08-27):** add **`Roots.ProductScoped bool`** —
   > the platform that builds the roots declares the shape instead of
   > `Resolve` inferring it. A widened path-element heuristic was rejected: it
   > false-positives for a Linux user whose home is `/home/mcremote` and would
   > silently relocate `StateDir`. Files added to this step's scope:
   > `internal/appdirs/roots.go` (the new field). MADR D3 amended to match.

   In `Resolve`, `StateDir`, `CacheDir` and `RuntimeBase` join `product.Name`
   onto the corresponding root. Gate that join on the new flag:

   ```go
   // ProductScoped reports that StateHome, CacheHome and RuntimeHome are
   // already product-specific directories, so Resolve must not append the
   // product leaf again (MADR 0116 D3).
   ProductScoped bool
   ```

   ```go
   func (r Roots) joinProduct(root, name string) string {
       if r.ProductScoped {
           return filepath.Clean(root)
       }
       return filepath.Join(root, name)
   }
   ```

   Apply to `StateDir` and `CacheDir`. `RuntimeBase` already has a special
   case for the temp-fallback shape (`paths.go:66–71`); keep that `if` and use
   `roots.joinProduct` in its else branch. **`ConfigDir`, `DataDir` and
   `LogDir` are unchanged** — their Windows roots (`%AppData%`,
   `%LocalAppData%`) are genuinely *not* product-scoped, so they must always
   join.

6. **Create `ensure_windows.go`** implementing `EnsurePrivateDir` under
   contract C2. "Private" on Windows is an explicit DACL:

   ```go
   //go:build windows

   package appdirs

   import (
       "fmt"
       "os"
       "path/filepath"

       "golang.org/x/sys/windows"
   )

   // privateDACL grants the owner and SYSTEM full control and nobody else,
   // with inheritance disabled ("P" = SDDL_PROTECTED). It is the Windows
   // equivalent of mode 0700: Administrators is deliberately omitted, matching
   // the Unix rule that only the owning uid may read the daemon's secrets.
   const privateDACL = "D:P(A;OICI;FA;;;OW)(A;OICI;FA;;;SY)"

   // EnsurePrivateDir creates dir (and parents) as a user-owned private
   // directory.
   //
   // Idempotent and converging. Creates the directory and any parents if
   // absent; if present, verifies owner and access and repairs access to the
   // private state; returns an error rather than repairing when the leaf is a
   // symlink, reparse point, or not owned by the current principal. A second
   // call on a converged directory performs no writes.
   func EnsurePrivateDir(dir string) error { /* … */ }
   ```

   Implementation shape, in order:

   * reject empty and non-absolute (`filepath.IsAbs`) — identical to
     `ensure_unix.go:20–24`, so the error strings match across platforms;
   * `os.MkdirAll(dir, 0o700)` (the mode is ignored on Windows but keeps one
     code shape);
   * `os.Lstat`; reject `fi.Mode()&os.ModeSymlink != 0` **and**
     `fi.Mode()&os.ModeIrregular != 0` — the latter is how Go surfaces a
     non-symlink reparse point (`os/types_windows.go:222–227`), and is the
     Windows analogue of the existing symlink refusal;
   * read the current owner with
     `windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)`
     and compare to the process token's user SID
     (`windows.OpenCurrentProcessToken` → `Token.GetTokenUser`); mismatch is a
     **hard error**, never a repair;
   * parse `privateDACL` once into an `*ACL` —
     `windows.SecurityDescriptorFromString(privateDACL)` then `sd.DACL()`
     (`security_windows.go:1452`, `1245`) — because
     `SetNamedSecurityInfo(objectName, objectType, securityInformation,
     owner, group, dacl, sacl *ACL)` (`zsyscall_windows.go:1274`) takes a
     parsed ACL, not an SDDL string. Cache it in a `sync.OnceValues` so the
     parse happens once per process;
   * read the existing DACL the same way and compare its SDDL round-trip
     (`sd.String()`) against `privateDACL`; **only if it differs**, call
     `SetNamedSecurityInfo` with
     `windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION`
     (the `PROTECTED_` bit is what makes the `"P"` in the SDDL stick by
     severing inheritance). This comparison is what makes the second call
     perform no writes — the C2 no-op requirement;
   * re-read and verify, mirroring `ensure_unix.go:52–61`'s re-check.

7. **Split `runtime_unix.go`.** `ValidateRuntimeDir` stays there. Move
   `MaxUnixSocketPathLen` and `CheckSocketPathLength` into `sockpath_unix.go`
   (`//go:build unix`, unchanged bodies). Create `sockpath_windows.go`:

   ```go
   //go:build windows

   package appdirs

   import "fmt"

   // maxWindowsSockaddrUn is the usable sun_path length in the Windows
   // AF_UNIX sockaddr_un (108 bytes including the trailing NUL). Windows does
   // not export a RawSockaddrUnix through x/sys, so the constant is stated
   // here rather than derived, and pinned by TestMaxUnixSocketPathLenWindows.
   const maxWindowsSockaddrUn = 107

   func MaxUnixSocketPathLen() int { return maxWindowsSockaddrUn }

   func CheckSocketPathLength(path string) error { /* same shape as unix */ }
   ```

   Create `runtime_windows.go` with `ValidateRuntimeDir` reusing the same
   owner + DACL check as step 6 (extract a shared unexported
   `checkPrivateDir(dir string) error` so the two cannot drift).

8. **Path-length diagnostic (MADR D3, owner decision 2026-08-27).** In
   `roots_windows.go`, after building `Roots`, append a `Diagnostic` when the
   longest path the layout will produce approaches `MAX_PATH`. Reuse the
   existing channel — `SystemRoots` already returns `[]Diagnostic` — with a
   new code `windows_path_length`:

   ```go
   // Go's fixLongPath (os/path_windows.go:100-105) transparently applies the
   // \\?\ prefix below 248 bytes, so the daemon's own os calls keep working
   // past MAX_PATH. Provider CLIs get no such help, which is the real
   // exposure -- so warn and proceed rather than refuse (MADR 0116 D3).
   // Contrast CheckSocketPathLength, which DOES refuse: an over-long
   // sun_path cannot work at all.
   ```

   Budget the check against the deepest leaf `Resolve` will build (the
   instance-keyed runtime dir plus `admin.sock`), not against the root, or the
   warning fires too late to be useful.

9. **Tests.** In `paths_test.go`, add `TestInstanceKeyCaseFolding`
   (Windows-only assertion via `runtime.GOOS`, skipped elsewhere with an
   explicit reason) and `TestResolveDoesNotDoubleJoinProductLeaf` — the
   latter runs **on every host** by injecting a `Roots` whose `StateHome`
   already ends in `mcremote`, which is exactly why C4 matters. Create
   `roots_windows_test.go` (`//go:build windows`) driving `systemRoots`
   through the `knownFolder` seam and asserting the D3 table.
   Rename `ensure_unix_test.go`'s helper `stringsRepeat` → `strings.Repeat`
   (it predates the stdlib usage and is dead weight).

**Verification.**

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./internal/appdirs/
CGO_ENABLED=0 go test -race -count=1 ./internal/appdirs/
# The whole-tree Windows build is still red here — admin and fsutil are P2/P3.
# It must fail ONLY on those two packages:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./... 2>&1 \
  | grep -E '^internal/' | grep -v -E '^internal/(admin|providerauth)/' \
  && { echo "unexpected package failing"; exit 1; } || true
# No uid logic may survive in shared appdirs code:
! grep -n 'os.Getuid()' internal/appdirs/roots.go internal/appdirs/paths.go
# D3: the path-length policy is a diagnostic, never a refusal.
grep -q 'windows_path_length' internal/appdirs/roots_windows.go
! grep -n 'path.*too long' internal/appdirs/roots_windows.go
```

Plus the stability rule, **except** the whole-tree Windows lines, which are
waived for P1 only and re-armed from P2 onward.

---

### P2 — `fsutil`: honest durability and real cross-process locks (F5, F12)

**Outcome.** `WriteFileAtomic` succeeds on Windows. `WithLock` exists on
Windows and actually locks. `auth` and `certs` stop silently disabling their
cross-process serialisation.

**Files.** `internal/fsutil/atomic.go` (edit), `syncdir_unix.go` (create),
`syncdir_windows.go` (create), `lock_unix.go` (edit — extract),
`lock_windows.go` (create), `atomic_test.go` (edit),
`lock_windows_test.go` (create), `internal/auth/filelock_other.go` (edit),
`internal/auth/filelock_windows.go` (create),
`internal/certs/filelock_other.go` (edit),
`internal/certs/filelock_windows.go` (create).

**Steps.**

1. **Move `syncDir` out of `atomic.go`.** Delete `atomic.go:125–131`. Create
   `syncdir_unix.go` (`//go:build unix`) with the identical body, and
   `syncdir_windows.go`:

   ```go
   //go:build windows

   package fsutil

   // syncDir is a no-op on Windows.
   //
   // File.Sync is FlushFileBuffers (internal/poll/fd_fsync_windows.go), which
   // requires GENERIC_WRITE on the handle; os.Open gives read access, so
   // syncing a directory handle returns "Access is denied" — golang/go#75541.
   // There is no Windows API that flushes a directory entry independently of
   // its files.
   //
   // MADR 0116 D5 decided this is a no-op rather than an error: returning the
   // error would break every SyncDir caller (the device token store included,
   // internal/auth/store.go:602), and swallowing it inside callers would
   // re-introduce exactly the silent success MADR 0074 D25 forbids. The
   // consequence is real and documented in docs/ops-windows-install.md: on
   // NTFS the rename is ordered, but the directory entry is not separately
   // flushed, so a power loss in the window after WriteFileAtomic returns can
   // lose the rename. SyncFile is unaffected — FlushFileBuffers on a file
   // handle works normally.
   func syncDir(dir string) error { return nil }
   ```

2. **Skip `os.Chmod` after rename on Windows.** `atomic.go:113`'s
   `_ = os.Chmod(path, opts.Perm)` is already error-ignoring, so it is
   harmless — but it is misleading. Leave the call; add a one-line comment
   noting it is inert on Windows, where the P2 lock file and the P1 DACL on
   the parent directory carry the access control instead.

3. **Extract the lock retry loop.** In `lock_unix.go`, keep `WithLock` and
   `flockWithTimeout` unchanged. Create `lock_windows.go`:

   ```go
   //go:build windows

   package fsutil

   import (
       "fmt"
       "os"
       "time"

       "golang.org/x/sys/windows"
   )

   // WithLock holds an exclusive byte-range lock on path+".lock" for the
   // duration of fn.
   //
   // LockFileEx is MANDATORY on Windows, unlike flock's advisory semantics on
   // Unix, so a holder genuinely blocks a peer's write rather than relying on
   // cooperation. The lock is taken over byte range [0,1) of a zero-length
   // file, which is legal and is the conventional whole-file lock idiom.
   func WithLock(path string, timeout time.Duration, fn func() error) error {
       if path == "" {
           return fmt.Errorf("fsutil: empty lock path")
       }
       if timeout <= 0 {
           timeout = 5 * time.Second
       }
       lockPath := path + ".lock"
       f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
       if err != nil {
           return fmt.Errorf("fsutil: open lock %s: %w", lockPath, err)
       }
       defer f.Close()
       h := windows.Handle(f.Fd())
       if err := lockWithTimeout(h, timeout); err != nil {
           return fmt.Errorf("fsutil: lock %s: %w", lockPath, err)
       }
       defer func() {
           var ol windows.Overlapped
           _ = windows.UnlockFileEx(h, 0, 1, 0, &ol)
       }()
       return fn()
   }

   func lockWithTimeout(h windows.Handle, timeout time.Duration) error {
       deadline := time.Now().Add(timeout)
       for {
           var ol windows.Overlapped
           err := windows.LockFileEx(h,
               windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
               0, 1, 0, &ol)
           if err == nil {
               return nil
           }
           if err != windows.ERROR_LOCK_VIOLATION && err != windows.ERROR_IO_PENDING {
               return err
           }
           if time.Now().After(deadline) {
               return fmt.Errorf("lock busy for more than %s: %w", timeout, err)
           }
           time.Sleep(20 * time.Millisecond)
       }
   }
   ```

   **Deferred `UnlockFileEx` must run before `f.Close()`** — Go runs defers
   LIFO, so the unlock defer is registered *after* the close defer and
   therefore runs first. This ordering is load-bearing; add it as a comment.

4. **`auth` and `certs` delegate.** `internal/fsutil` imports nothing from
   this module (verified), so `auth` and `certs` may depend on it without a
   cycle — `auth` already does. Create
   `internal/auth/filelock_windows.go`:

   ```go
   //go:build windows

   package auth

   import "github.com/maccavelli/magic-cli-remote/internal/fsutil"

   // withPathLock holds the cross-process lock for path. Windows gets a real
   // lock via LockFileEx (MADR 0116 D6) — the CLI and daemon share
   // devices.json there exactly as they do on Unix, so the pre-0116 no-op was
   // a silent correctness hole, not a platform limitation.
   func withPathLock(path string, fn func() error) error {
       return fsutil.WithLock(path, lockTimeout, fn)
   }
   ```

   `lockTimeout` currently lives in `filelock_unix.go`; move it to a new
   untagged `filelock.go` so both platforms share the one value and its
   rationale comment. Do the same for `certs`: move `certLockTimeout` to an
   untagged file and add `filelock_windows.go` wrapping `fsutil.WithLock` —
   note `lockCertDir` returns `(func(), error)` rather than taking a closure,
   so it needs a small adapter that acquires and returns the releaser; keep
   its `.certs.lock` filename.

5. **Narrow the `!unix` fallbacks.** Change both `filelock_other.go` build
   tags from `//go:build !unix` to `//go:build !unix && !windows`, and rewrite
   their doc comments to say the no-op applies to platforms with no file
   locking at all (js/wasm, plan9) — not "a Linux/macOS deployment concern",
   which is now false.

6. **Tests.** `atomic_test.go`: the existing
   `TestWriteFileAtomicSyncDirError` injects a failing `ops.syncDir` and is
   platform-independent — it stays. Add `TestSyncDirIsNoOpOnWindows`
   (`//go:build windows`) asserting `syncDir(t.TempDir())` returns nil, and
   `TestWriteFileAtomicSyncDirSucceedsOnWindows` writing a real file with
   `SyncDir: true`. Create `lock_windows_test.go` proving two goroutines
   serialise and that a busy lock times out.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/fsutil/ ./internal/auth/ ./internal/certs/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./internal/fsutil/ ./internal/auth/ ./internal/certs/ ./internal/providerauth/
# The three no-op fallbacks must no longer claim windows:
! grep -rn '^//go:build !unix$' internal/auth/ internal/certs/
```

Plus the full stability rule — **including** all five cross-compile lines,
which from here on must stay green except for `internal/admin` (P3).

---

### P3 — `admin`: an authenticated control plane on Windows, or none (F6)

**Outcome.** The last `undefined:` errors are gone; the whole tree builds for
both Windows architectures with no shims. The admin socket is owner-verified
on Windows or `Serve` returns an error.

**Files.** `internal/admin/admin.go` (edit), `owner_unix.go` (create),
`owner_windows.go` (create), `admin_test.go` (edit),
`owner_windows_test.go` (create).

> **File-scope addition 2026-08-27 (P3 execution).** Step 4 specifies that the
> Windows `secureSocket` "applies P1's `privateDACL`", which requires that DACL
> to be reachable from `internal/admin` — but `internal/appdirs` was not in the
> file list above. Added to this phase's scope:
> **`internal/appdirs/security_windows.go`**, gaining two exported wrappers,
> `CurrentUserSID` and `SecurePrivateFile`. Purely additive, so contract C6
> holds. The alternative — duplicating the descriptor logic inside `admin` —
> was rejected: two copies of a security primitive can drift, and only one of
> them would be covered by the appdirs tests.

**Steps.**

1. **Extract the POSIX stat calls behind two functions.** Create
   `internal/admin/owner_unix.go` (`//go:build unix`):

   ```go
   //go:build unix

   package admin

   import (
       "fmt"
       "io/fs"
       "os"
       "syscall"
   )

   // ownedByCurrentUser reports whether fi belongs to the calling user.
   func ownedByCurrentUser(_ string, fi fs.FileInfo) (bool, error) {
       st, ok := fi.Sys().(*syscall.Stat_t)
       if !ok {
           return false, fmt.Errorf("admin: cannot inspect ownership")
       }
       return int(st.Uid) == os.Getuid(), nil
   }

   // socketIdentity returns a value that changes if the path stops naming the
   // same socket. On Unix that is the inode.
   func socketIdentity(fi fs.FileInfo) (uint64, bool) {
       st, ok := fi.Sys().(*syscall.Stat_t)
       if !ok {
           return 0, false
       }
       return st.Ino, true
   }
   ```

2. **Rewrite the three call sites in `admin.go`** to use them —
   `admin.go:98–101` becomes:

   ```go
   owned, oerr := ownedByCurrentUser(socketPath, fi)
   if oerr != nil {
       return fmt.Errorf("admin: %s: %w", socketPath, oerr)
   }
   if !owned {
       return fmt.Errorf("admin: %s not owned by current user", socketPath)
   }
   ```

   Note the behaviour change: today `st, ok := …; if ok && …` **skips** the
   ownership check when the type assertion fails. The new form makes an
   un-inspectable socket a hard error. That is deliberate and is the C3
   fail-closed rule; call it out in the commit body.

   `admin.go:120–125` and `137–142` use `socketIdentity`.

3. **Create `owner_windows.go`** implementing both:

   * `ownedByCurrentUser` reads the file's owner SID via
     `windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)`
     → `sd.Owner()` and compares with `SID.Equals` to the process token user.
   * `socketIdentity` returns
     `(FileIndexHigh<<32 | FileIndexLow, true)` from
     `windows.GetFileInformationByHandle` on a handle opened with
     `FILE_FLAG_BACKUP_SEMANTICS`; on any failure it returns `(0, false)`,
     which the existing `sockInode != 0` guard at `admin.go:139` already
     treats as "do not remove" — the conservative branch.

4. **Replace the chmod with a DACL, fail closed.** `admin.go:114`'s
   `os.Chmod(socketPath, 0o600)` becomes a call to a new per-platform
   `secureSocket(socketPath string) error`: on Unix it is today's chmod; on
   Windows it applies P1's `privateDACL` to the socket file via
   `SetNamedSecurityInfo`. **The existing error handling is kept verbatim** —
   the listener is closed and the error returned — so a Windows host that
   cannot secure the socket does not serve one. Update the package doc
   (`admin.go:1–3`) to say auth is "filesystem permissions (Unix mode 0600 /
   Windows owner-only DACL)".

5. **Guard the socket path length on both platforms.** No change needed —
   `appdirs.CheckSocketPathLength` now exists on Windows from P1 step 7.

6. **Tests.** `admin_test.go` gains `TestServeRefusesUnownedSocket` driven
   through the extracted seam. Create `owner_windows_test.go`
   (`//go:build windows`) covering `ownedByCurrentUser` on a
   just-created file (true) and `socketIdentity` stability across two calls.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/admin/
# THE gate for this phase — no shims, both architectures, whole tree:
! ls internal/**/*probe*.go internal/**/*shim*.go 2>/dev/null
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...   # exit 0
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet   ./...   # exit 0
! grep -n 'syscall.Stat_t' internal/admin/admin.go
```

At the end of P3 the MADR's Confirmation items 1 and 2 are satisfied.

---

### P4 — `procutil`: Job Objects and correct liveness (F7)

**Outcome.** Provider process trees are killed as a unit on Windows;
`OwnerAlive` stops returning false for every live process.

**Files.** `internal/procutil/procutil_other.go` (edit — narrow tag),
`procutil_windows.go` (create), `owner_other.go` (edit — narrow tag),
`owner_windows.go` (create), `starttoken_other.go` (edit — narrow tag),
`starttoken_windows.go` (create), `procutil_windows_test.go` (create).

**Steps.**

1. Narrow all three `_other.go` build tags to exclude Windows:
   `//go:build !unix && !windows`, `//go:build !linux && !darwin && !windows`
   (twice). Update their doc comments — the current text on
   `procutil_other.go` (*"no portable 'ask nicely' signal"*) is true for the
   remaining platforms but was never true for Windows.

2. **`procutil_windows.go`.** `SetProcessGroup(cmd)` sets
   `cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags:
   windows.CREATE_NEW_PROCESS_GROUP}` and records nothing else — the Job
   Object is created at `Start` time, which `os/exec` does not expose a hook
   for. Therefore introduce a **new** exported function used by callers that
   already call `SetProcessGroup` then `Start`:

   ```go
   // SuperviseStarted attaches an already-started process to a kill-on-close
   // job object so its whole descendant tree dies with it. It is a no-op on
   // Unix, where SetProcessGroup + a negative-pid signal already covers the
   // tree. Callers must invoke it immediately after cmd.Start().
   func SuperviseStarted(p *os.Process) (release func(), err error)
   ```

   On Windows: `CreateJobObject`, `SetInformationJobObject` with
   `JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation:
   {LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}`,
   `AssignProcessToJobObject`; `release` closes the job handle (killing the
   tree). On Unix it returns `(func() {}, nil)`.

   **This adds an exported symbol rather than changing one, per C6.** Wiring
   it into the provider start paths is *not* in this phase — it is listed in
   "Deferred" below, because the call sites live in provider packages this
   plan only touches for P6's `LookPath` substitution. Landing the primitive
   with tests here, and wiring it under a follow-up, keeps P4 reviewable.

3. `KillProcessGroup(p)` on Windows: open the process with
   `PROCESS_TERMINATE`, call `windows.TerminateProcess(h, 1)`.
   `TerminateProcessGroup(p, exited, timeout)`: send
   `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uint32(p.Pid))` — valid only
   because step 2 sets `CREATE_NEW_PROCESS_GROUP` — then poll `exited` /
   liveness exactly as `procutil_unix.go:60–79` does, escalating to
   `KillProcessGroup` on timeout and returning `false`. Preserve the Unix
   semantics of the return value: true iff the polite signal sufficed.

4. **`owner_windows.go`.** `OwnerToken()` returns
   `fmt.Sprintf("%d:%d", os.Getpid(), creationTime)` mirroring
   `owner_linux.go:55`'s `pid:starttime` shape. `OwnerAlive(token)` parses the
   pid, calls
   `windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)`,
   and returns false on error; otherwise liveness is
   `windows.WaitForSingleObject(h, 0) == uint32(windows.WAIT_TIMEOUT)` — a
   still-running process never signals.

   **Do not use `GetExitCodeProcess` + `STILL_ACTIVE` here**, for two reasons
   found while planning: `STILL_ACTIVE` is **not exported** by
   `x/sys/windows` or by Go's `syscall` package at this version, so it would
   have to be hand-defined as 259; and a process that legitimately exits with
   code 259 is then indistinguishable from a running one.
   `WaitForSingleObject` has neither problem, which is why `SYNCHRONIZE` is in
   the access mask above.

   Then compare `GetProcessTimes`' creation time to the token's second field
   so a recycled pid reads as dead. `SetDeathSignal` is a no-op
   with a comment pointing at `SuperviseStarted` as the Windows answer.
   `ProcessEnv` / `FindByEnv` return the same "unavailable" values as
   `owner_other.go` — reading another process's environment on Windows needs
   `NtQueryInformationProcess` and cross-bitness PEB walking, which is out of
   scope and **must be stated in the doc comment**, not left implicit.

5. **`starttoken_windows.go`.** `ProcessStartToken(pid)` opens the process
   and returns `GetProcessTimes`' creation time as a decimal string, `true`;
   `("", false)` on any failure.

6. **Tests.** `procutil_windows_test.go` (`//go:build windows`): spawn
   `cmd.exe /c ping -n 30 127.0.0.1`, attach via `SuperviseStarted`, close the
   job, assert the child is gone within 2s; assert `OwnerAlive(OwnerToken())`
   is **true** (the direct F7 inversion regression); assert
   `ProcessStartToken(os.Getpid())` returns `ok`.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/procutil/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./internal/procutil/
# No file may claim to be the non-Unix fallback while Windows has a real one:
! grep -rn '^//go:build !unix$'            internal/procutil/
! grep -rn '^//go:build !linux && !darwin$' internal/procutil/
```

Plus the stability rule.

---

### P5 — Shutdown signals (F8)

**Outcome.** The two daemons stop cleanly on every signal their platform
actually delivers, with no dead `syscall.SIGTERM` on Windows.

**Files.** `internal/cli/signals_unix.go`, `internal/cli/signals_windows.go`,
`internal/relay/signals_unix.go`, `internal/relay/signals_windows.go` (all
create), `internal/cli/serve.go:86` (edit), `internal/relay/cli.go:248`
(edit), plus one test per package.

**Steps.**

1. In each package add `shutdownSignals() []os.Signal` —
   `{os.Interrupt, syscall.SIGTERM}` on Unix, `{os.Interrupt}` on Windows with
   this comment:

   ```go
   // Windows never delivers SIGTERM. os.Interrupt covers CTRL_C_EVENT,
   // CTRL_BREAK_EVENT and CTRL_CLOSE_EVENT for a console process, and it is
   // the ONLY graceful trigger: the scheduled task that runs this daemon is
   // stopped with `schtasks /end`, a TerminateProcess (MADR 0116 D9/D12).
   // Provider trees still die with the Job Object, and a left-behind admin
   // socket is handled by the stale-socket path.
   ```

2. Replace both call sites with
   `signal.NotifyContext(ctx, shutdownSignals()...)`. Drop the now-unused
   `syscall` import from `internal/relay/cli.go` if nothing else uses it
   (grep confirms line 248 is its only use).

3. Tests: assert `shutdownSignals()` contains `os.Interrupt` on both, and
   `syscall.SIGTERM` only under `//go:build unix`.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/cli/ ./internal/relay/
! grep -n 'syscall.SIGTERM' internal/cli/serve.go internal/relay/cli.go
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./internal/cli/ ./internal/relay/
```

Plus the stability rule.

---

### P6 — Provider launch: PATHEXT shims handled or refused (F10)

**Outcome.** A Windows npm shim either launches correctly or produces an
actionable typed error. No provider argument reaches `cmd.exe` unescaped.

**Files.** `internal/provider/launch/` (new package: `launch.go`,
`launch_unix.go`, `launch_windows.go`, `launch_test.go`,
`launch_windows_test.go`), plus the four `exec.LookPath` call sites.

**Steps.**

1. **New package `internal/provider/launch`.** Exported surface:

   ```go
   // Kind classifies how a resolved executable must be invoked.
   type Kind int

   const (
       KindNative Kind = iota // a PE/ELF/Mach-O image: exec directly
       KindBatch              // Windows .bat/.cmd: needs cmd.exe /c
   )

   // Resolved is the outcome of resolving a configured engine binary name.
   type Resolved struct {
       Path string // absolute path to the resolved file
       Kind Kind
   }

   // ErrUnsafeBatchArgs is returned when a .bat/.cmd must be invoked through
   // cmd.exe but an argument contains a character cmd.exe would reinterpret.
   var ErrUnsafeBatchArgs = errors.New("launch: argument unsafe for cmd.exe")

   // ErrCommandLineTooLong is returned when the assembled command line would
   // exceed the CreateProcessW limit.
   var ErrCommandLineTooLong = errors.New("launch: command line exceeds 32767 characters")

   // Resolve finds bin on PATH and classifies it.
   func Resolve(bin string) (Resolved, error)

   // Command builds an *exec.Cmd for a Resolved plus args, routing a batch
   // file through cmd.exe /c and refusing arguments cmd.exe would reinterpret.
   func Command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error)
   ```

2. **`launch_unix.go`:** `Resolve` is `exec.LookPath` and always
   `KindNative`; `Command` is `exec.CommandContext`. Behaviour is byte-identical
   to today on Unix — this phase must be a no-op there.

3. **`launch_windows.go`:** `Resolve` calls `exec.LookPath` (which already
   honours `PATHEXT` — `os/exec/lp_windows.go:113–130`) and classifies by
   `strings.EqualFold(filepath.Ext(p), ".bat"|".cmd")`. `Command`:

   * `KindNative` → `exec.CommandContext(ctx, r.Path, args...)`;
   * `KindBatch` → validate **every** arg against
     `^[A-Za-z0-9 ._:\\/@=+,'-]*$`; any arg failing it returns
     `fmt.Errorf("%w: %q — set providers.<name>.bin to the real executable", ErrUnsafeBatchArgs, arg)`.
     The rejected set is deliberately broad: `& | < > ^ % " ( ) !` are all
     cmd.exe metacharacters, and `%` and `!` survive quoting under delayed
     expansion, so quoting is **not** treated as a substitute for rejection.
     On success build
     `exec.CommandContext(ctx, comspec(), append([]string{"/c", r.Path}, args...)...)`
     where `comspec()` is `%COMSPEC%` or `cmd.exe`;
   * both kinds: sum the escaped command-line length and return
     `ErrCommandLineTooLong` above 32767, per the `CreateProcessW`
     documentation.

   Document in the package comment why `cmd.exe` is unavoidable, quoting
   Microsoft: *"To run a batch file, you must start the command interpreter;
   set lpApplicationName to cmd.exe and set lpCommandLine to … /c plus the
   name of the batch file."*

4. **Substitute at the four availability checks only.** Replace
   `exec.LookPath(p.cfg.Bin)` with `launch.Resolve(p.cfg.Bin)` in
   `codex/provider.go:213`, `acpagent/acpagent.go:399`,
   `acphttp/provider.go:185`, `httpagent/provider.go:154`. **Do not** change
   how these packages construct their `exec.Cmd` in this phase — that is the
   deferred wiring noted below. `codex/provider.go:1003`'s second `LookPath`
   is left alone (it resolves for a different purpose; changing it is a
   deviation).

   `acpagent/terminal.go:315`'s `exec.LookPath("bash")` and
   `grok/device_auth.go:90`'s `sandbox-exec` are **untouched** — both are
   deliberately Unix-only per D15.

5. **Tests.** `launch_test.go` runs everywhere and asserts the Unix path is a
   pass-through. `launch_windows_test.go` (`//go:build windows`) covers:
   a `.cmd` written to `t.TempDir()` resolving as `KindBatch`; a safe arg list
   producing a `cmd.exe /c` argv; each of `& | < > ^ % " ( ) !` rejected with
   `ErrUnsafeBatchArgs`; and a 40 000-character arg returning
   `ErrCommandLineTooLong`.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/provider/...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./internal/provider/...
# The four availability checks must no longer call LookPath directly:
! grep -n 'exec.LookPath(p.cfg.Bin)' internal/provider/codex/provider.go \
    internal/provider/acpagent/acpagent.go internal/provider/acphttp/provider.go \
    internal/provider/httpagent/provider.go
```

Plus the stability rule. **Unix behaviour must be unchanged** — the full
`CGO_ENABLED=0 go test -race ./...` on the host is the oracle for that.

---

### P7 — Self-update on Windows (F9)

**Outcome.** `mcremote update` on Windows compares versions correctly and does
not strand a half-swapped binary.

**Files.** `internal/update/swap.go` (edit), `internal/update/github.go`
(edit), `internal/update/swap_test.go` (edit),
`internal/update/github_test.go` (edit).

**Steps.**

1. **`github.go` — strip the extension before returning `VER`.** In
   `AssetFor`, after `ver := strings.TrimPrefix(a.Name, prefix)`:

   ```go
   // Windows assets carry the extension LAST (mcremote-windows-amd64-1.2.3.4.exe),
   // so the version suffix is everything before it. Stripping here is what keeps
   // NewerPublished and SumsAsset comparing versions rather than filenames
   // (MADR 0116 F9/F17, convention C5).
   ver = strings.TrimSuffix(ver, ".exe")
   ```

   Add `TestAssetForStripsWindowsExtension` asserting
   `mcremote-windows-amd64-0.14.10.1.exe` → `"0.14.10.1"`. This is MADR
   Confirmation item 9.

2. **`swap.go:71` — stop discarding the `.prev` removal.** Replace
   `_ = os.Remove(prev)` with a checked removal that tolerates
   `os.IsNotExist` and otherwise **returns**:

   ```go
   // A leftover .prev that cannot be removed is fatal, not ignorable. On
   // Windows a file still open by another process refuses deletion, and the
   // rename at dest→prev below would then fail with the swap half-done; on
   // Unix this simply never triggers (MADR 0116 F9).
   if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
       return fmt.Errorf("remove stale backup %s: %w", prev, err)
   }
   ```

   Place it **before** the service stop so the failure is free of side
   effects. Note the existing `_ = os.Remove(prev)` at line 159 is the
   success-path cleanup and stays best-effort.

3. **`swap.go:130` — skip the inert chmod on Windows.** Guard
   `_ = os.Chmod(dest, 0o755)` with `if runtime.GOOS != "windows"`, commented
   as inert there (the execute bit is not a Windows concept; the `.exe`
   extension is).

4. **Leave the rename ordering alone.** `dest → prev` then `staged → dest`
   (lines 123–127) is already the correct Windows update idiom: a running
   `.exe` cannot be deleted or written but **can** be renamed. Add a comment
   recording that, so a future reader does not "fix" it into a delete-then-copy
   that would break on Windows.

5. **Tests.** `swap_test.go` gains `TestSwapFailsOnUndeletablePrev` using the
   existing test seams, asserting the swap aborts before the service stop.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/update/
CGO_ENABLED=0 go test -run TestAssetForStripsWindowsExtension -count=1 ./internal/update/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./internal/update/
```

Plus the stability rule.

---

### P8 — Build flags, release matrix, asset naming, installer (D2, D13, D17, D19, D20a, D21, F2, F3, F13, F17, F22)

**Outcome.** All five targets are built, tag-checked and published with correct
asset names and aliases; **every published target is executed natively in CI**
on a free GitHub-hosted runner; **every published binary is proven cgo-free**;
`install.ps1` exists. `windows/arm64` is absent throughout, by D19.

**Files.** `Makefile`, `scripts/verify-build-metadata.sh`,
`.github/workflows/ci.yml`, `scripts/install.sh`, `scripts/install.ps1`
(create), `scripts/test-linux-arm64.sh` (create), `scripts/acceptance-windows.ps1`
(create), `scripts/testdata/cgo-gate-check.sh` (create),
`scripts/testdata/alias-loop-check.sh` (create),
`internal/cli/service/setup.go` (edit — Windows default binary discovery only).

**Steps.**

1. **Makefile — Windows gets no tags.** Insert a `windows` arm before the
   `else` at `Makefile:89`:

   ```make
   else ifeq ($(GOOS),windows)
     # No netgo/osusergo (MADR 0116 D2). osusergo is inert on Windows —
     # os/user has always been cgo-free there. netgo is NOT inert: it forces
     # the pure-Go resolver, overriding net/conf.go's goosPrefersCgo(), which
     # names windows explicitly. The pure-Go path reads DNS servers only from
     # up-and-gatewayed adapters (net/dnsconfig_windows.go) and honours no
     # search list or NRPT policy, so a VPN or virtual adapter's resolver is
     # silently dropped.
     GO_TAGS :=
     GO_BUILDFLAGS := -trimpath
   ```

2. **Makefile — refuse a cgo release build, loudly (D21 layer 3).**
   `Makefile:38`'s `CGO_ENABLED ?= 0` is defeated by the environment: `?=`
   assigns only when undefined, and an exported variable counts as defined
   (F22, reproduced). Add a guard prerequisite and wire it into the three
   release-producing targets (`build`, `build-remote`, `build-relay`):

   ```make
   # MADR 0116 D21. `CGO_ENABLED ?= 0` is a default the environment overrides:
   #   $ CGO_ENABLED=1 make build   →  make sees 1, and a cgo binary ships.
   # Fail loudly rather than `override`-ing it silently — an operator who
   # asked for cgo must be told they cannot have it here, not quietly ignored.
   check-cgo-off:
   	@if [ "$(CGO_ENABLED)" != "0" ]; then \
   		echo "refusing to build a release binary with CGO_ENABLED=$(CGO_ENABLED)." >&2; \
   		echo "Shipped binaries are pure-Go (MADR 0116 D21). For a cgo build," >&2; \
   		echo "use 'make debug', which is never published." >&2; \
   		exit 1; \
   	fi
   ```

   Add `check-cgo-off` to `.PHONY`. Attach it the way `install` attaches
   `check-host-target` (`Makefile:245–252`) — as a **prerequisite**, with the
   real work in the recipe — because `make -j` may run prerequisites
   concurrently and a guard that races the build it guards is not a guard.

   Delete the now-false comment at `Makefile:37`
   (*"Override for a cgo build: make build CGO_ENABLED=1."*) and replace it
   with a pointer to `make debug`. **Leave `make debug` itself untouched** —
   those binaries are explicitly never shipped (`Makefile:165–174`).

3. **`verify-build-metadata.sh` — cover all five.** Add
   `build_one linux arm64 "netgo,osusergo"` and `build_one windows amd64 ""`.
   Assert `netgo` **and** `osusergo` present for both Linux targets, and assert
   **absent** for the Windows target by extending the Darwin loop's binary list
   (`verify-build-metadata.sh:50–60`) rather than writing a second loop.
   **Do not add a `windows arm64` case** (D19) — an unbuilt target must not
   acquire a policy assertion that implies it ships.

   Then add the **cgo assertion (D21 layer 2)** over *every* built target, not
   just the new ones:

   ```bash
   # MADR 0116 D21 / F22. The tag assertions above prove which net and user
   # implementation is linked; this proves cgo is off at all. `go version -m`
   # reads the binary's own build settings, so it is authoritative regardless
   # of how the binary was produced.
   for bin in "$tmpdir"/mcremote-*; do
     if ! go version -m "$bin" | grep -q '^	build	CGO_ENABLED=0$'; then
       echo "error: $bin is not CGO_ENABLED=0" >&2
       go version -m "$bin" | grep -i cgo >&2
       exit 1
     fi
   done
   ```

4. **`ci.yml` — add the cgo-free pass to the existing `go` job (D20a).**
   Insert **immediately after** the existing "Test (race, all packages)" step
   (`ci.yml:107–108`). Do not edit, rename or reorder that step:

   ```yaml
   # MADR 0116 D20a. The race pass above implies CGO_ENABLED=1, so it links a
   # libc-backed os/user and a net whose resolver selection has a branch the
   # shipped binary does not have. This second pass runs the exact pure-Go
   # stack the linux/amd64 release binary is built with (CGO_ENABLED=0 +
   # netgo,osusergo). Neither pass replaces the other: the first catches data
   # races, the second catches "works under libc, breaks under netgo".
   #
   # Cost: one extra full-suite run on the busiest job, on a free runner.
   - name: Test (cgo-free, all packages)
     run: CGO_ENABLED=0 go test ./...
   ```

   `go-native`'s `ubuntu-24.04-arm` leg already covers Linux cgo-free on
   **arm64**; this step is specifically about **amd64**, where scheduling and
   code paths can differ.

5. **`ci.yml` — the matrix.** Replace lines 160–165 with:

   ```bash
   PLATFORMS=""
   PLATFORMS="$PLATFORMS linux/amd64"
   PLATFORMS="$PLATFORMS linux/arm64"
   PLATFORMS="$PLATFORMS darwin/arm64"
   PLATFORMS="$PLATFORMS darwin/amd64"
   PLATFORMS="$PLATFORMS windows/amd64"
   # windows/arm64 is deliberately absent (MADR 0116 D19). Re-entry is this
   # one line plus a verify-build-metadata case and a windows-11-arm CI leg.
   ```

6. **`ci.yml` — fix the alias loop (F17).** Lines 622–630's glob
   `mcremote-*-"${VER}"` cannot match a `.exe` asset, so Windows would ship
   with no unversioned alias and `install.ps1` would 404. Replace with an
   extension-aware form:

   ```bash
   # Convention C5: <product>-<goos>-<goarch>[-<VER>][<ext>], extension LAST.
   # The glob below must therefore tolerate a trailing extension — the pre-0116
   # form (mcremote-*-"${VER}") silently skipped every Windows asset.
   for f in mcremote-*-"${VER}"* mcrelay-*-"${VER}"*; do
     [ -f "$f" ] || continue
     case "$f" in SHA256SUMS*|*.apk) continue ;; esac
     base="${f%%-"${VER}"*}"          # mcremote-windows-amd64
     ext="${f#*-"${VER}"}"            # .exe  (empty for POSIX)
     alias_name="${base}${ext}"
     cp -f "$f" "$alias_name"
     ASSETS+=("$alias_name")
   done
   ```

7. **`ci.yml` — two native test jobs (D17).** The repository is **public**,
   so every runner below is free. Each job runs on **pull requests and tags**,
   not `ref_type == 'tag'` — a target verified only at tag time is verified too
   late, which is exactly the shape that let F17 sit undetected.

   Add one reusable job body via `strategy.matrix` with
   **`fail-fast: false`** (one platform's failure must not mask another's):

   ```yaml
   go-native:
     name: Go (${{ matrix.label }})
     needs: []
     strategy:
       fail-fast: false
       matrix:
         include:
           # linux/arm64 and windows/amd64 support the race detector and both
           # images ship a C compiler (ubuntu arm64: GCC 12/13/14;
           # windows-2025: GCC 15.2.0), so -race needs no install step.
           - { runner: ubuntu-24.04-arm, label: linux/arm64,   shell: bash }
           - { runner: windows-latest,   label: windows/amd64, shell: bash }
           # No windows-11-arm leg: windows/arm64 is out of scope (MADR 0116
           # D19) — not a first-class port (zosarch.go:112-113), no local
           # acceptance host, and the most divergent runner image (no MSYS2).
     runs-on: ${{ matrix.runner }}
     timeout-minutes: 30
     # C7 / MADR D20: cgo-free everywhere, builds and tests. This is also what
     # keeps CI on the same pure-Go net and os/user paths the shipped binaries
     # use — with cgo on, `net` prefers the system resolver and os/user goes
     # through libc, so a green run would prove something users never execute.
     env:
       CGO_ENABLED: "0"
     defaults:
       run:
         shell: ${{ matrix.shell }}
     steps:
       - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
       # go-version-file, not a literal: both images preinstall an older Go
       # (windows-2025 defaults to 1.24.13) and go.mod is the single source
       # of truth for this module's toolchain.
       - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
         with:
           go-version-file: go.mod
           cache: true
       - run: go build ./...
       - run: go vet ./...
       # No -race: off darwin it forces CGO_ENABLED=1
       # (cmd/go/internal/work/init.go:194-204), which C7 refuses. Race
       # coverage is the darwin gate in the plan's stability rule.
       - run: go test ./...
   ```

   Add `go-native` to the `release` job's `needs:` list, so a tag cannot
   publish an artifact for a platform whose tests did not run.

   **One thing to confirm on the first CI run**, because it is the only claim
   here not verified from source or an official image manifest: `cache: true`
   behaving on the `ubuntu-24.04-arm` partner image. If it misbehaves, drop
   `cache` for that leg — a one-line change, not a design problem. (Dropping
   `windows/arm64` also removed the other open question, whether
   `actions/setup-go` can resolve a `windows/arm64` Go 1.26.5 toolchain.)

8. **`ci.yml` — native `linux/arm64` smoke, no emulation.** The tag job's
   post-build smoke only executes `*-linux-amd64` (F2). **Do not add QEMU** —
   MADR open question 1 is resolved by F18/D17. Instead, add a small
   `smoke-arm64` job gated on `github.ref_type == 'tag'`, `needs: [go]`,
   `runs-on: ubuntu-24.04-arm`, which downloads the
   `mcremote-mcrelay-<version>` artifact and runs:

   ```bash
   chmod +x mcremote-linux-arm64 mcrelay-linux-arm64
   for b in mcremote mcrelay; do
     got="$(./${b}-linux-arm64 version | awk '{print $2}')"
     test "$got" = "${{ needs.go.outputs.version }}" || exit 1
   done
   ```

   This is the same assertion the existing amd64 smoke makes, on real
   hardware. Add `smoke-arm64` to the `release` job's `needs:`.

   The `windows/amd64` binaries are smoked on `windows-latest` in the same
   shape; keep the two loops identical so they cannot drift.

9. **`ci.yml` — assert cgo-freeness on the artifacts, before upload (D21
   layer 1, the guarantee).** This is the check that cannot be bypassed: it
   inspects the files being published, whatever produced them.

   `debug/buildinfo` parses ELF, PE and Mach-O
   (`debug/buildinfo/buildinfo.go:126–150`), so the Linux release runner can
   read the Windows `.exe` and both Darwin binaries **without executing
   them** — verified on this tree by inspecting a cross-built `linux/arm64`
   ELF from the darwin host.

   Add to the `go` job immediately after `build_matrix` populates `dist/`,
   and **before** `sha256sum`:

   ```bash
   # MADR 0116 D21. Every published binary must be pure-Go. Asserted on the
   # artifact, not inferred from the build recipe: CGO_ENABLED ?= 0 in the
   # Makefile is defeated by a stray environment variable (F22), and a future
   # workflow change could bypass make entirely.
   for f in dist/mcremote-* dist/mcrelay-*; do
     case "$f" in *SHA256SUMS*) continue ;; esac
     if ! go version -m "$f" | grep -q '^	build	CGO_ENABLED=0$'; then
       echo "error: $f is not CGO_ENABLED=0 — refusing to publish" >&2
       go version -m "$f" | grep -i cgo >&2
       exit 1
     fi
     echo "ok: $f is cgo-free"
   done
   ```

   Mirror the same loop in the `release` job over `go-bin/` after the
   `sha256sum -c` round-trip check, so an artifact that changed between jobs
   is still caught. Two loops, one rule — keep them byte-identical.

   **Prove the check works before trusting it.** Add
   `scripts/testdata/cgo-gate-check.sh`, which builds one deliberately
   cgo-linked binary (`CGO_ENABLED=1 go build`) into a temp dir, runs the
   assertion against it, and **fails if the assertion passes**. A gate nobody
   has seen reject anything is not known to be a gate.

10. **`service/setup.go` — Windows default binary discovery (MADR D13).**
    `setup.go:52` documents the default as *"`~/.local/bin/<product>` if
    present, else this process's executable"*. Add the Windows analogue:
    `%LOCALAPPDATA%\Programs\<product>\<product>.exe` if present, else
    `os.Executable()`. Without this, `setup-service` on Windows would bake
    whatever path the user happened to invoke into the scheduled task — a
    downloads folder, a temp extract — and the task would break the moment
    that file moved.

    This is a `setup.go` edit, not a new file, and it is the one place P8
    touches `internal/cli/service`; the rest of that package is P9.

11. **`install.sh` — strip the extension in `verify_and_resolve`.** Line 189's
   `RESOLVED_VER=${_name#"${_p}-${OS}-${ARCH}-"}` yields `0.14.10.1.exe` for a
   Windows manifest line. Add `RESOLVED_VER=${RESOLVED_VER%.exe}` with a
   comment pointing at C5. The script still refuses to *run* on Windows
   (lines 73–81) — this fix is for correctness of the shared manifest format
   and for a WSL user pinning a Windows asset.

12. **`scripts/install.ps1` — new.** Mirror `install.sh`'s contract exactly:
   detect `amd64`/`arm64` from `$env:PROCESSOR_ARCHITECTURE`; download
   `SHA256SUMS` and the two unversioned aliases from
   `$BASE_URL/latest/download` (or `/download/v$PinVersion`); **verify by hash
   VALUE against the versioned manifest line, never by filename** — the same
   reason `install.sh:170–174` documents; install to
   **`$env:LOCALAPPDATA\Programs\<product>\`** (MADR D13, owner decision
   2026-08-27) — per-user, no elevation, the analogue of `~/.local/bin`; warn
   if that directory is not on `PATH`; print the SmartScreen note from D14. `Set-StrictMode -Version
   Latest` and `$ErrorActionPreference = 'Stop'` at the top. Add it to the
   release job's shipped assets next to `install.sh`.

13. **Two local-verification helpers (D18).** These are developer tooling, not
   CI, and must stay runnable with no GitHub involvement.

   `scripts/test-linux-arm64.sh` — runs the suite on **native** aarch64 Linux
   from the owner's Apple Silicon Mac. Virtualization.framework hosts a real
   arm64 VM, so this is full speed and **QEMU must not appear anywhere in it**:

   ```bash
   #!/usr/bin/env bash
   # Run the Go suite on native linux/arm64 from an Apple Silicon host.
   #
   # No emulation: on Apple Silicon the Linux VM is itself aarch64, so the
   # container runs at native speed. If this ever starts needing QEMU, the
   # host is not arm64 and the result is not the thing we wanted to test.
   set -euo pipefail
   [ "$(uname -m)" = "arm64" ] || { echo "host is not arm64; results would be emulated" >&2; exit 1; }
   docker info >/dev/null 2>&1 || {
     echo "no docker daemon. Start one:  colima start --arch aarch64 --vm-type vz" >&2
     exit 1
   }
   # -e CGO_ENABLED=0 is REQUIRED, not tidy: the golang images default to
   # CGO_ENABLED=1, so inheriting the image default would silently break C7
   # and test a libc-backed net/os-user stack the shipped binary never uses.
   # No -race for the same reason -- off darwin it forces cgo back on.
   exec docker run --rm --platform linux/arm64 \
     -v "$PWD":/src -w /src \
     -e CGO_ENABLED=0 \
     golang:1.26.5 \
     go test ./...
   ```

   This mirrors the `ubuntu-24.04-arm` CI leg exactly: same architecture, same
   cgo posture, same command. Race coverage for this code is the darwin gate,
   not this script (MADR D20).

   `scripts/acceptance-windows.ps1` — the manual acceptance run for the
   owner's Windows laptop, scripted so it is repeatable and so its output can
   be pasted into a phase handoff. It sets `$env:CGO_ENABLED = '0'` first
   (C7), then performs `go build ./...`, `go vet ./...`, `go test ./...`
   (**no `-race`** — it would force cgo), then the functional checks: `version`,
   `paths`, `pair create` (**the direct F5 regression**), `pair list`,
   `doctor`, and a `mcrelay serve` on loopback that is stopped with Ctrl+C to
   exercise the P5 drain path. It prints a `PASS`/`FAIL` line per check.

   Its header must state F20 plainly:

   ```powershell
   # Manual acceptance for windows/amd64. Run on the owner's Windows laptop.
   #
   # This laptop is NOT a CI runner and must never be registered as a
   # self-hosted GitHub Actions runner: this repository is public, so anyone
   # who can fork it and open a pull request could execute code on it, and
   # self-hosted runners are non-ephemeral by default (MADR 0116 F20).
   # CI for windows/amd64 is the hosted windows-latest job.
   ```

> **Deviation 2026-08-27 (P8 execution).** Two grep gates in this phase were
> **wrong as written**: they matched the explanatory comments that record *why*
> a thing is absent, so they failed against a correct tree. Both now exclude
> comment lines (`| grep -vE ':[[:space:]]*#'`). No behaviour changed — only the
> gates that check it. Recorded because a gate that cannot pass on a correct
> tree gets disabled by the next person who hits it.

**Verification.**

```bash
make verify-build-metadata                      # all five targets + cgo assertion
# D21 layer 3: the release targets must refuse a cgo build outright.
CGO_ENABLED=1 make build 2>&1 | grep -q 'refusing to build a release binary' \
  || { echo "cgo guard did not fire"; exit 1; }
# D21 layer 1: prove the artifact gate actually rejects a cgo binary.
bash scripts/testdata/cgo-gate-check.sh
# Every locally built binary is pure-Go:
for b in bin/mcremote* bin/mcrelay*; do
  go version -m "$b" | grep -q '^	build	CGO_ENABLED=0$' || { echo "$b has cgo"; exit 1; }
done
make build GOOS=windows GOARCH=amd64 VERSION=0.0.0-test && ls -l bin/mcremote.exe
make build GOOS=linux   GOARCH=arm64 VERSION=0.0.0-test
go version -m bin/mcremote.exe | grep -q 'netgo' && { echo "windows has netgo"; exit 1; } || true
# Alias-loop regression, run standalone against a fixture directory:
bash scripts/testdata/alias-loop-check.sh        # create in this phase
pwsh -NoProfile -Command 'Set-StrictMode -Version Latest; . ./scripts/install.ps1 -WhatIf'
# Native linux/arm64 suite, from the Mac (needs a running colima/docker daemon):
./scripts/test-linux-arm64.sh
# C7: nothing this plan added may enable cgo.
# The `grep -v` is load-bearing: these files CARRY comments explaining why cgo
# is off, and a naive grep matches its own rationale (found in P8 execution).
! grep -rn 'CGO_ENABLED[=: ]*.1.' scripts/test-linux-arm64.sh scripts/acceptance-windows.ps1 .github/workflows/ \
    | grep -vE ':[[:space:]]*#'
# C7/D20a: exactly ONE -race invocation in the workflows — not zero (that
# would delete Linux race coverage), not two (that would be a new cgo dep).
# Count-based, not line-based: adding the cgo-free step moves every line below it.
test "$(grep -c 'go test .*-race' .github/workflows/ci.yml)" = "1"
# D20a: the cgo-free pass exists alongside it.
grep -q 'CGO_ENABLED=0 go test ./\.\.\.' .github/workflows/ci.yml
# Workflow must not have acquired an emulation dependency:
! grep -rn 'setup-qemu\|qemu-aarch64\|binfmt' .github/workflows/
# Every action stays SHA-pinned (no bare @v tags):
! grep -nE 'uses: [^@]+@v[0-9]+\s*$' .github/workflows/ci.yml
# windows/arm64 must not have leaked back in (D19) — code only; the comments
# that RECORD its absence must survive (found in P8 execution).
! grep -rn 'windows-11-arm\|windows/arm64' .github/workflows/ scripts/verify-build-metadata.sh \
    | grep -vE ':[[:space:]]*#'
```

Plus the stability rule. Do **not** push.

**A note on validating the workflow itself.** `act` runs Linux containers
only and cannot execute the `windows-latest` leg, so it proves little here. The honest position is that the three native jobs are
first exercised on the owner's next push or tag; the YAML is validated
statically before then (`actionlint` if available, plus the two greps above),
and the matrix is written `fail-fast: false` precisely so that first run
reports every platform at once instead of one at a time.

---

### P9 — Windows background execution: a per-user Task Scheduler task (D12)

**Outcome.** `setup-service` installs, starts, stops and queries a Task
Scheduler at-logon task on Windows, **with no elevation**, replacing the four
`service control unsupported on windows` returns for `windows` only.

**Unblocked 2026-08-27.** This phase was gated on MADR open questions 2 and 3;
both are answered. The model is **per-user Task Scheduler**, not a
`LocalSystem` service — so D3's path table is unaffected and there is no
one-way door here any more.

**Files.** `internal/cli/service/setup.go` (edit — dispatch),
`setup_schtasks.go` (create), `control.go` (edit — dispatch),
`control_schtasks.go` (create), `schtasks.go` (create), `taskname.go` (create),
plus tests. **`internal/svcrun/` is not created** — see the note below.

> **Deviation 2026-08-27 (P9 execution).** ~~The file list named
> `setup_windows.go`, `control_windows.go` and `schtasks_windows.go`.~~
> **Those names are wrong for this code.** Go applies an *implicit* build
> constraint to any file ending `_windows.go`, regardless of its `//go:build`
> line — so those names would have made the Task Scheduler path compile only on
> Windows, which directly defeats step 7's requirement that the branch be
> "exercised on the owner's Mac" through `OverrideInstallOS`.
>
> Nothing in this path touches a Windows API: it shells out to `schtasks.exe`
> via the `runSchtasks` seam and manipulates XML. The files are therefore
> **untagged and cross-platform**, named `schtasks.go`, `setup_schtasks.go`
> and `control_schtasks.go`, with `taskname.go` holding the one helper
> `ProbeStatus` needs on every platform. All eleven P9 tests run on macOS as a
> result. Recorded because the naming looks like an oversight otherwise.

**Steps.**

1. **No SCM integration.** The earlier draft added an `internal/svcrun`
   wrapping `svc.Run`/`svc.IsWindowsService`. D12 removes the SCM from the
   design, so that package is **not built**: shipping an untested code path
   for a deployment mode the tooling never installs is worse than not having
   it. `cmd/mcremote/main.go` and `cmd/mcrelay/main.go` are **untouched** by
   this phase.

   Record the consequence in `docs/ops-windows-install.md` (P10) rather than in
   code: `sc.exe create` / NSSM against these binaries will fail at the SCM
   start-up timeout, because they never call `StartServiceCtrlDispatcher`.

2. **`schtasks_windows.go` — one seam over the task engine.** Implement
   against `schtasks.exe` rather than the COM Task Scheduler 2.0 API: it is a
   documented, stable CLI, it needs no COM plumbing in a codebase that has
   none, and it keeps the implementation reviewable. Wrap it in a
   `runSchtasks` variable mirroring the existing `runLaunchctl` /
   `runSystemctl` seams (`setup.go:192`, and `OverrideRunLaunchctl` at
   `setup.go:205`), so tests drive the Windows branch on any host.

   Task name: `mcremote` / `mcrelay` (bare product, matching the systemd unit
   and the launchd label mapping).

3. **`setup_windows.go` — register the task.** Generate the task XML (not
   `/create /tr`, which cannot express working directory or restart policy
   cleanly) and register with `schtasks /create /tn <product> /xml <file> /f`.
   The XML must set:

   * `<LogonTrigger>` with the current user — at-logon start;
   * `<Principal><LogonType>InteractiveToken</LogonType>` and
     `<RunLevel>LeastPrivilege</RunLevel>` — **no elevation**, the whole point
     of D12;
   * `<Settings><MultipleInstancesPolicy>IgnoreNew`, `<StopIfGoingOnBatteries>false`,
     `<DisallowStartIfOnBatteries>false`, `<ExecutionTimeLimit>PT0S` (no limit),
     and `<RestartOnFailure>` with a bounded count — the closest analogue to
     the systemd unit's `Restart=` that the task engine offers;
   * `<Exec><Command>` = the resolved binary, `<Arguments>` = `serve --config
     <path>` exactly as the systemd and launchd renderers already bake it
     (`setup.go:233`).

   **Idempotency (C2 at the service layer).** `/f` overwrites, so re-running is
   safe; but to honour "a second call performs no writes", first
   `schtasks /query /tn <product> /xml ONE` and compare the existing XML to the
   rendered one, registering only when they differ. This mirrors the existing
   `res.Unchanged` behaviour the Linux and macOS paths already report
   (`relay/cli.go:451`).

4. **`control_windows.go`.** `IsInstalled` → `schtasks /query /tn <product>`
   exit status. `IsActive` → `schtasks /query /tn <product> /fo LIST /v` and
   match `Status: Running`. `Start` → `schtasks /run /tn <product>`. `Stop` →
   `schtasks /end /tn <product>`.

   **Document at the `Stop` implementation** that `/end` is a
   `TerminateProcess`, not a graceful signal (MADR D9), and that the drain
   skipped there is covered by D8's Job Object and the stale-socket path. A
   future reader must not mistake this for an oversight.

5. **Dispatch.** Add `case "windows":` to the `installOS` switch in `Setup`
   (`setup.go:268`) and to the four switches in `control.go` (lines 47, 64,
   107, 147). Every other platform keeps its existing error. The `default:`
   branch keeps its `--print-only` preview.

6. **`update` reconnects.** With `IsInstalled`/`IsActive` now answering on
   Windows, `SwapAndRestart`'s `RestartService`/`HealStart` gating (`run.go:140–154`)
   starts working there — no change needed in `internal/update`, which is the
   point of having kept the `ServiceControl` interface intact (C6).

7. **Tests.** Drive `Setup`/`Remove`/`IsActive` through `OverrideInstallOS`
   plus a stubbed `runSchtasks`, so the Windows branch is exercised on the
   owner's Mac. Assert: the rendered XML contains `LeastPrivilege` and
   `InteractiveToken` (**an elevation regression must fail the suite**); a
   second `Setup` with identical XML reports `Unchanged` and issues no
   `/create`; `Stop` maps to `/end`.

**Verification.**

```bash
CGO_ENABLED=0 go test -race -count=1 ./internal/cli/service/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
# No SCM path may have been introduced (D9/D12):
! grep -rn 'svc.Run\|IsWindowsService\|StartServiceCtrlDispatcher' internal/ cmd/
! ls internal/svcrun 2>/dev/null
```

On the owner's Windows laptop, **unelevated**:

```powershell
mcremote setup-service --force
schtasks /query /tn mcremote /fo LIST /v      # Status, and Run As User = the logged-in user
mcremote setup-service --force                # second run: reports Unchanged, no /create
mcremote setup-service --remove
```

The first command must succeed from a **non-administrator** shell. If it
prompts for elevation, D12 has been implemented wrongly.

### P10 — Docs, coverage, final regression

**Outcome.** The support tier is discoverable before install, the Windows
durability caveat is written down, and coverage does not regress.

**Files.** `README.md`, `docs/ops-windows-install.md` (create),
`docs/config.md`, `docs/config-mcrelay.md`, `docs/ops-mcrelay.md`,
`internal/relay/cli.go` (the F15 copy), plus any test files needed to hold the
coverage floor.

**Steps.**

1. **`README.md`** — a "Supported platforms" table stating Tier 1
   (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64) and Tier 2
   (windows/amd64), with the D15 exclusion list linked. State plainly that
   `windows/arm64` is **not supported** — not "coming soon" — per D19, so a
   Windows-on-Arm user learns it before downloading rather than after.
2. **`docs/ops-windows-install.md`** — install via `install.ps1`; the Known
   Folders path table from D3; the D5 durability caveat in plain language; the
   D14 SmartScreen note; the D15 unsupported-surface list; the F15 port notes
   (no privileged-port restriction, but check
   `netsh int ipv4 show excludedportrange` and expect a Defender Firewall
   prompt on first bind).
3. **`internal/relay/cli.go:462–472`** — the `setcap` guidance is wrong on
   Windows (F15). Extend the existing `res.Scope` switch with a Windows arm
   printing the excluded-port-range and firewall text. This uses the scope
   value already computed, so no new `runtime.GOOS` is introduced.
4. **`docs/config.md` / `docs/config-mcrelay.md`** — document that default
   config and data paths differ per OS and point at the D3 table rather than
   hard-coding `~/.config/...`. The `ConfigPathHint` / `DataDirHint`
   fallbacks at `relay/fileconfig.go:787,796` return POSIX strings only when
   `appdirs` fails; leave them, but add a comment that they are a
   last-resort display string, not a path anyone opens.
5. **Coverage.**

   > **Deviation 2026-08-27 (P10 execution).** ~~"Any target below 80.0% gets
   > failure-path tests in this phase."~~ **Narrowed by owner decision.**
   > Measured against the pre-0116 baseline (`997896d`), four packages sit
   > below the floor and **all four were already below it before this work**:
   >
   > | package | baseline | after 0116 | delta |
   > | --- | --- | --- | --- |
   > | `internal/appdirs` | 66.0% | 68.5% | **+2.5** |
   > | `internal/fsutil` | 81.9% | 81.3% | −0.6 |
   > | `internal/admin` | 61.6% | 61.6% | 0.0 |
   > | `internal/procutil` | 41.5% | 41.5% | 0.0 |
   > | `internal/update` | 77.7% | 78.0% | **+0.3** |
   > | `internal/provider/launch` | (new) | 100.0% | n/a |
   >
   > Reaching the floor needs **+130 covered statements**, of which **+81 are
   > in `procutil`** — `registry.go`, `reap.go` and `owner_darwin.go`, all Unix
   > code this Windows port never touched. That is the debt
   > [0113](0113-MADR-preexisting-unit-coverage-debt.md) exists to track, and
   > doing it here would land it under the wrong number.
   >
   > **Resolution:** P10 covers the seams **0116 itself added** and requires
   > that **no package regresses**. The `fsutil` −0.6pp dip is restored. The
   > remaining shortfall stays with 0113. Recorded rather than silently
   > skipped, because a floor that is quietly not applied stops being a floor.

   Capture profiles before and after the whole plan and run the
   0112 A13 gate over the packages this plan touched:

   ```bash
   scripts/coverage-delta.sh floor --after "$AFTER" --minimum 80.0 \
     --go ./internal/appdirs/ ./internal/fsutil/ ./internal/admin/ \
          ./internal/procutil/ ./internal/update/ ./internal/provider/launch/
   ```

   Any target below 80.0% gets failure-path tests in this phase — the same
   shape as 0115 P8.

**Verification.** The whole-plan verification block below.

---

## Verification (whole plan)

Run in order. Every line must pass.

```bash
# 1. Format, vet, tidy — host
gofmt -l cmd internal                                    # prints nothing
go vet ./...
go mod tidy && git diff --exit-code go.mod go.sum

# 2. All five targets build, no shims
! ls internal/**/*probe*.go internal/**/*shim*.go 2>/dev/null
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  CGO_ENABLED=0 GOOS="${t%/*}" GOARCH="${t#*/}" go build ./... || exit 1
done

# 3. Windows type-checks including tests
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...

# 4. Full host suite under the race detector, cgo OFF (darwin exemption)
CGO_ENABLED=0 go test -race -count=1 ./...

# 5. Build-tag policy on all five
make verify-build-metadata

# 6. Repo gates
make preflight
./scripts/next-build-version_test.sh
make verify-units

# 7. C7 / D20: no cgo in anything this plan added
! grep -rn 'CGO_ENABLED[=: ]*.1.' scripts/test-linux-arm64.sh scripts/acceptance-windows.ps1 .github/workflows/
test "$(grep -c 'go test .*-race' .github/workflows/ci.yml)" = "1"
grep -q 'CGO_ENABLED=0 go test ./\.\.\.' .github/workflows/ci.yml

# 8. D21: shipped binaries are pure-Go, proven on the artifact
make build VERSION=0.0.0-test
for b in bin/mcremote* bin/mcrelay*; do
  go version -m "$b" | grep -q '^	build	CGO_ENABLED=0$' || { echo "$b has cgo"; exit 1; }
done
CGO_ENABLED=1 make build 2>&1 | grep -q 'refusing to build a release binary'
bash scripts/testdata/cgo-gate-check.sh          # the gate rejects a cgo binary

# 9. Anti-regression greps (each must print nothing)
grep -rn 'os.Getuid()' internal/appdirs/roots.go internal/appdirs/paths.go
grep -rn 'syscall.Stat_t' internal/admin/admin.go
grep -rn '^//go:build !unix$' internal/auth/ internal/certs/ internal/procutil/
grep -n  'syscall.SIGTERM' internal/cli/serve.go internal/relay/cli.go
grep -n  'exec.LookPath(p.cfg.Bin)' internal/provider/codex/provider.go \
         internal/provider/acpagent/acpagent.go \
         internal/provider/acphttp/provider.go \
         internal/provider/httpagent/provider.go
```

**On native `linux/arm64` — from the owner's Mac, no emulation:**

```bash
colima start --arch aarch64 --vm-type vz     # once per boot; free, Lima-based
./scripts/test-linux-arm64.sh                # CGO_ENABLED=0 go test ./... in golang:1.26.5
```

**On the owner's Windows laptop (`windows/amd64`) — the acceptance run:**

```powershell
$env:CGO_ENABLED = '0'                       # C7 — no cgo, and no -race (it would re-enable it)
go build ./... ; go vet ./... ; go test ./...
.\bin\mcremote.exe version
.\bin\mcremote.exe paths                 # asserts the D3 Known Folders layout
.\bin\mcremote.exe pair create --name test   # the direct F5 regression
.\bin\mcremote.exe pair list
.\bin\mcremote.exe doctor
.\bin\mcrelay.exe serve --listen-host 127.0.0.1 --listen-port 8443   # Ctrl+C drains
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | Phase | Confirmed by |
| --- | --- | --- | --- |
| 1 | `windows/amd64` + `linux/arm64` build with no shims | P3 | Verification step 2 |
| 2 | `GOOS=windows go vet ./...` clean | P3 | Verification step 3 |
| 3 | `go-native` matrix green on `ubuntu-24.04-arm` and `windows-latest` under `CGO_ENABLED=0`, or every skip names its platform reason | P8 | CI |
| 4 | `linux/arm64` and `windows/amd64` binaries report the expected version when **executed on their native runner** | P8 | `smoke-arm64` + Windows smoke jobs |
| 5 | Tag policy asserted on five targets, and `windows/arm64` absent from it | P8 | `make verify-build-metadata` |
| 6 | Known Folders on Windows / XDG on Unix from one `Resolve`; ensure is idempotent and converging on both | P1 | `./internal/appdirs/` tests |
| 7 | `mcremote pair create` succeeds on Windows | P2 | Windows acceptance run |
| 8 | `admin.Serve` binds owner-verified or errors | P3 | `TestServeRefusesUnownedSocket` |
| 9 | Grandchild dies with the job; `OwnerAlive` true for a live process | P4 | `procutil_windows_test.go` |
| 10 | `AssetFor` returns `0.14.10.1` for the `.exe` asset | P7 | `TestAssetForStripsWindowsExtension` |
| 11 | Tag publishes five targets with matching sums **and working aliases** | P8 | Release job + F17 fixture check |
| 12 | `setup-service` registers a Task Scheduler task **from a non-elevated shell**; a second run reports `Unchanged`; the XML carries `LeastPrivilege` + `InteractiveToken` | P9 | `./internal/cli/service/` tests + Windows acceptance |
| 13 | **Every published binary reports `build CGO_ENABLED=0`**, asserted on the artifact before upload; a deliberately cgo-linked binary in `dist/` fails the release | P8 | `cgo-gate-check.sh` + release job |

Three plan-local criteria with no MADR Confirmation counterpart, because they
constrain *how* the work is verified rather than *what* it achieves:

| # | Criterion | Phase | Confirmed by |
| --- | --- | --- | --- |
| L1 | Every command this plan adds sets `CGO_ENABLED=0`; exactly one `-race` invocation exists in the workflows, and the existing race step is unedited | P8 | count-based grep gate + `ci.yml` review |
| L6 | The `go` job runs both passes (D20a) and both are green | P8 | CI |
| L5 | `make build CGO_ENABLED=1` fails with a message naming the variable — never silently ignored | P8 | P8 verification |
| L7 | No SCM code exists: no `svc.Run`, no `IsWindowsService`, no `internal/svcrun` | P9 | grep gate |
| L8 | `appdirs` warns rather than refuses on a long Windows layout | P1 | grep gate + `roots_windows_test.go` |
| L4 | `windows/arm64` appears nowhere in `.github/workflows/` or `verify-build-metadata.sh` | P8 | grep gate |
| L2 | No emulation dependency anywhere in `.github/workflows/` | P8 | grep gate |
| L3 | No self-hosted runner registered; `acceptance-windows.ps1` carries the F20 rationale in its header | P8 | script review |

## Rollout and Rollback

**Rollout.** P1–P3 and P8 are the shippable unit; the first Windows release is
whatever tag follows P8. Because the new CI jobs run on pull requests, the
first push after P8 exercises `linux/arm64` and `windows/amd64` natively —
the plan does not wait for a tag to learn whether the port works. P4–P7 and
P9 land incrementally and each is independently revertable. Windows ships as **Tier 2** (D16) — the README
table lands in P10 but the CI job in P8 is what makes the claim true, so P8
must not be the last thing merged before a tag.

**Rollback.** Every phase is one commit touching a disjoint file set, so
`git revert` of any single phase is clean, with three exceptions to know
before starting:

* Reverting **P1** re-breaks the Windows build for P2–P3 — revert those first
  or revert the range.
* Reverting **P8**'s cgo assertions returns the project to F22 — cgo-freeness
  as an overridable Makefile default that nothing checks. Of everything in P8,
  this is the piece least safe to revert while binaries are published.
* Reverting **P8**'s alias-loop fix re-introduces F17, which affects Linux and
  macOS releases not at all, so it is safe to revert alone.
* Reverting **P2**'s `syncdir_windows.go` makes every Windows durable write
  fail again. It is the one file that must never be reverted while a Windows
  binary is published.

**The path layout is the one-way door — and it is now shut safely.** Once a
Windows user has state under `%LocalAppData%\mcremote`, changing D3's table
needs a migration, not a revert. The 2026-08-27 decision to use a **per-user**
Task Scheduler task rather than a `LocalSystem` service is what removes that
risk: `FOLDERID_LocalAppData` resolves to the real user in both the
interactive and the scheduled-task case, so P9 cannot invalidate the layout P1
ships. `%ProgramData%` is recorded in MADR D3 for a machine-wide service this
record does not build.

**No `git push` is performed by this plan.** Publishing is an explicit owner
action.

## Post-merge: first CI run (2026-08-27)

The first real run of the new jobs — [33084467102][run] on `db79629` — failed.
What it proved, and what it cost:

**Both Linux failures were pre-existing flakes, confirmed by evidence rather
than argued.** `gh run rerun --failed` on the **identical commit** turned both
green: `Go (linux/arm64)` and `Go (test; build on tag)` passed second time with
no code change. They are recorded here so a future reader does not re-diagnose
them:

- `TestReconcileGooseKeyringHostControls` (`internal/daemon`, `-race`) —
  `bad file descriptor` on an `os.ReadFile`. `internal/daemon` is not in this
  plan's diff at all. An EBADF under the race detector suggests a real,
  pre-existing race worth its own investigation; it is **not** 0116's.
- `TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight`
  (`internal/provider/codex`) — a timing-sensitive single-flight assertion, on
  a 4-vCPU `ubuntu-24.04-arm` runner it had never executed on before. This
  plan's only change to that package is a one-line `exec.LookPath` →
  `launch.Resolve` in `Ready()`, which on Unix *is* `exec.LookPath`.

**Windows failed for real: 128 tests across 19 packages**, while `Build` and
`Vet` passed. Two of those are product defects, now recorded as MADR **F23a**
and **F23b** and fixed under **D22** and **D23** (see P11 below). The
remaining ~100 are test-suite portability.

The estimate that got this wrong was **F14**, which measured that the tree
*compiles and vets* under `GOOS=windows` and concluded a Windows CI lane was
"affordable; not a rewrite". Compiling is evidence about the port; passing is
evidence about the product. F14 should have said which one it had.

[run]: https://github.com/maccavelli/magic-cli-remote/actions/runs/33084467102

### P11 — Windows test-suite portability (added 2026-08-27)

**Outcome.** `go test ./...` is green on `windows-latest`, or every skip names
its platform reason (MADR Confirmation item 3).

**Status (2026-08-27).** Complete pending a CI run.

**A third product bug surfaced while doing it.** `isExecutableFile`
(`setup.go:1090`) tested `Mode()&0o111`, which Windows never sets — so
`setup-service` refused its own `mcremote.exe`. Recorded as **F23c**, fixed
under **D24** by moving the check to `launch.IsExecutableFile` beside the
PATHEXT logic. That is three POSIX predicates inlined at call sites that a
cross-compile could not see (D22, D23, D24).

**A design split inside D22.** The first cut required a candidate file to carry
the strict owner+SYSTEM protected DACL. That is wrong for *validation*: a
credential written by an agent CLI into its own profile inherits an ACL that
normally includes Administrators, so the strict check would have rejected every
real candidate — the same failure F23a describes, differently caused.
Enforcement (what we create) stays strict; validation (what others write) now
asks `noForeignTrustee`: no principal outside {owner, SYSTEM, Administrators}
has access. Administrators is not a boundary on Windows — an admin can take
ownership regardless — but another standard user is, and that is what is
enforced.

**The guards, per D25.**

| Gate | Uses | What it covers |
| --- | --- | --- |
| `SkipIfNoPOSIXModes` | 15 | assertions on `0600`/`0700` mode bits, and `chmod`-based failure injection |
| `SkipIfNoPOSIXShell` | 12 | `#!/bin/sh` stubs standing in for an agent CLI |
| `SkipIfNoPOSIXPaths` | 8 | fixtures using `/work/project`-style literals |
| `SkipIfNoXDG` | 2 | hermetic config placed via `XDG_CONFIG_HOME` |
| `SkipIfNoUnlinkOpenFile` | 1 | cleanup assertions that need POSIX unlink-while-open |

Nine test files became `//go:build unix` instead, because they assert a Unix
artifact by definition: systemd unit text, launchd plist XML, and XDG
resolution.

**Not yet verified.** The Windows suite has not been run since these changes —
that needs a push. `go vet` passes for `GOOS=windows`, and the full Unix suite
is green under `-race` with `CGO_ENABLED=0`, but neither proves the skips land
where intended. Expect a second pass.

**The four groups, from the run's own output.**

1. **Fixtures that write extensionless shell stubs and exec them** —
   `fake-cli`, `codex-stub`. Windows resolves executables through `PATHEXT`, so
   a file with no extension is not runnable however its bits are set.
   Affects `procutil`, `provider/grok`, `provider/codex`, `provider/credstore`.
   Fix: give the stub a `.bat`/`.cmd` extension on Windows via a shared test
   helper, or skip with a named reason where the test is about POSIX process
   groups specifically.
2. **POSIX mode assertions in tests** — `want 0600`, `mode = -rw-rw-rw-`.
   Affects `certs`, `credstore`, `providerauth`, `fsutil`, `session`,
   `provider/goose`. Fix: assert through `appdirs.FileIsOwnerOnly` (D22) rather
   than `Perm()`, which is the same property stated portably.
3. **Non-absolute Unix paths in test data** — `/var/lib/mcremote`,
   `/tmp/...`. `filepath.IsAbs` is false for these on Windows. Affects
   `appdirs` (`TestInstanceKeyStable`, and this plan's own
   `TestInstanceKeyCaseFolding`), `config`. Fix: build fixtures with
   `t.TempDir()` or a platform-appropriate root.
4. **Tests that assert XDG semantics directly** — `TestSystemRootsRelativeXDG`,
   `TestSystemRootsAbsoluteXDG`, the systemd/launchd renderers. Fix:
   `//go:build unix`, since they are testing a Unix layout by definition.

**Do not** make the Windows job non-blocking to get master green. MADR D16
claims Tier 2 means "unit-tested in CI"; a job that reports failure without
blocking makes that claim false and stops being read.

## Deferred (named, so they are not mistaken for oversights)

* Wiring `procutil.SuperviseStarted` into the provider start paths (P4 lands
  the primitive and its tests only) — needs a follow-up number, because the
  call sites are in provider packages this plan touches only for P6.
* Wiring `launch.Command` into provider `exec.Cmd` construction (P6 lands
  `Resolve` at the four availability checks; the command-building substitution
  is the same follow-up).
* **Windows Service Control Manager support** — refused, not deferred
  (MADR D9/D12/D15). These binaries never call `StartServiceCtrlDispatcher`,
  so `sc.exe create` and NSSM fail at the SCM start-up timeout. Adding it means
  building `internal/svcrun` *and* a `LocalSystem` install path, which reopens
  D3's machine-scope table — a new decision, not a plan amendment.
* **Pre-logon start for `mcrelay` on Windows** — a direct consequence of the
  per-user model (D12). Windows is not an unattended-server platform for
  `mcrelay` under this record.
* **`windows/arm64` — the whole target** (MADR D19, owner decision
  2026-08-27). Not a code deferral: every file this plan adds is
  `//go:build windows` and therefore already arch-agnostic, and the tree was
  measured cross-compiling clean for `windows/arm64` once the `GOOS` gaps
  close. Re-entry, when wanted, is three lines and no new source:

  1. `PLATFORMS="$PLATFORMS windows/arm64"` in `ci.yml`;
  2. `build_one windows arm64 ""` in `scripts/verify-build-metadata.sh`;
  3. one `go-native` matrix leg
     `{ runner: windows-11-arm, label: windows/arm64, shell: pwsh }`
     — no `-race` (C7 forbids it off darwin regardless of port support),
     `pwsh` because that image has no MSYS2 (F21).

  Then delete the two grep gates that currently assert its absence. Expect to
  confirm one unknown at that point: whether `actions/setup-go` resolves a
  `windows/arm64` Go toolchain for this module's version.
* **Automatic race detection on `linux/arm64` and `windows/amd64`** — refused,
  not deferred. `-race` forces `CGO_ENABLED=1` off darwin
  (`cmd/go/internal/work/init.go:194–204`), which C7 and MADR D20 rule out.
  Race coverage is the darwin gate. Reopening this needs a decision to allow
  cgo in test builds, i.e. a change to D20, not a plan amendment.
* ~~The pre-existing `go test -race ./...` on `ubuntu-latest`~~ — **resolved
  2026-08-27 (MADR D20a, options A + B)**: kept, and joined by a
  `CGO_ENABLED=0 go test ./...` pass added in P8. It remains the only
  sanctioned cgo-enabled command in the repository, and it touches no
  artifact.
* Authenticode certificate procurement (D14 wires `MC_WINDOWS_SIGN_*`; the
  cert is an owner purchase).
* MSI / winget packaging.
* `internal/session/store.go`'s `safeDir` semantics: `filepath.Clean("/"+id)`
  behaves differently on Windows, where `\` is a separator and `:` is illegal
  in a filename. The current form is **safe** on both (it cannot escape the
  root) but is not *identical*; a shared sanitizer is a separate concern.
* `internal/receipt/store.go:51,55` builds filenames as `deviceID+".jsonl"`
  with no sanitisation. Device IDs are UUIDs today, so this is latent, not
  live — worth a follow-up, not a scope expansion here.
