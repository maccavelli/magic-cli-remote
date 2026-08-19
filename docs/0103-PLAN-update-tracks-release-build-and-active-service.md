# Implement update tracking of published `BASE.N` and per-product service recycle

Associated MADR: [0103-MADR-update-tracks-release-build-and-active-service.md](./0103-MADR-update-tracks-release-build-and-active-service.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

## Goal

Make `mcremote update` and `mcrelay update` honour the version the
operator is actually running and the service file that actually exists
for **that** product.

* A binary stamped `0.13.9.1` is a published release. Compare it to the
  GitHub **asset** suffix (`mcremote-<os>-<arch>-0.13.9.1` → `0.13.9.1`),
  not to the three-part tag `v0.13.9`. `--force` is only for a real local
  compile (`dev`, `debug`, `.g<hash>`, `ci<runid>`, any extra non-integer
  suffix).
* Recycle the user service if and only if **this product** has a unit
  file (`IsInstalled`). Running or crashed both restart. No unit file
  means binary-only: no Stop, no Start, no new unit. `mcremote update`
  never inspects mcrelay's unit, and vice versa.

Review decisions already fixed by the MADR (2026-08-19): crashed daemon
with a unit file is HealStart (0100 F3 kept, not withdrawn); no unit
file is binary-only; same function for both binaries; a host may run
one, the other, or both. GitHub tags stay `vX.Y.Z`.

## Scope

**In scope**

* `internal/update/version.go` and `version_test.go` — parse and compare
  published `BASE.N`.
* `internal/update/run.go` and `run_test.go` — version gate uses asset
  `VER`; `RestartService` equals `IsInstalled(product)`.
* CLI `--force` help on both products (`internal/cli/update.go`,
  `internal/relay/update.go`).
* Operator docs: `docs/config.md` update section, `docs/ops-linux-install.md`
  updating section, `README.md` upgrading paragraph. Cite MADR 0103.

**Out of scope** (MADR More Information)

* Changing GitHub tag shape or `build/<BASE>.<N>` allocation.
* Enabling mcrelay from the curl one-liner by default.
* Phone "Restart to update".
* Re-running `curl|sh` as an upgrade tool.
* Rewriting historical 0065 / 0100 rationale. Those records stay; this
  plan amends behaviour and points operators at 0103.

## Implementation Steps

Commit after each phase (per AGENTS.md). No `-m`; `git commit --no-edit`.

`ParseBase` / `NewerBase` stay in the package as wrappers so
`BaseString` and any stray caller keep compiling; `Run` must not use
`NewerBase` after Phase 2.

### Phase 1 — Version identity

**1.1 `internal/update/version.go` — `Version` and `ParseVersion`**

Replace the "any fourth dotted field is dev" rule. Keep `ParseBase` and
`BaseString` as wrappers over the new type so `github.go:70` does not
change.

Insert **above** `ParseBase` (currently line 11):

```go
// Version is a stamped mcremote/mcrelay version (MADR 0103).
// N is 0 for a legacy three-part string ("0.13.9").
// Local is true for a locally compiled/installed binary, not a GitHub asset.
type Version struct {
	Major, Minor, Patch, N int
	Local                  bool
}

// String renders major.minor.patch or major.minor.patch.N (N>0). It does
// not reconstruct a local suffix.
func (v Version) String() string {
	if v.N <= 0 {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	}
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Patch, v.N)
}

// ParseVersion splits a stamped version.
//
//	"0.13.9"              → N=0, Local=false
//	"v0.13.9.1"           → N=1, Local=false   // published release
//	"0.13.9.1.gdeadbee"   → N=1, Local=true    // make install, offline
//	"0.13.9.1.ci123"      → N=1, Local=true
//	"dev" / "debug" / ""  → Local=true
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" || s == "dev" || s == "debug" {
		return Version{Local: true}, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return Version{}, fmt.Errorf("version %q: need at least major.minor.patch", s)
	}
	atoi := func(field, name string) (int, error) {
		n, err := strconv.Atoi(field)
		if err != nil {
			return 0, fmt.Errorf("version %s: %w", name, err)
		}
		return n, nil
	}
	maj, err := atoi(parts[0], "major")
	if err != nil {
		return Version{}, err
	}
	min, err := atoi(parts[1], "minor")
	if err != nil {
		return Version{}, err
	}
	pat, patRest, err := leadingInt(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("version patch: %w", err)
	}
	out := Version{Major: maj, Minor: min, Patch: pat, Local: patRest != ""}
	if len(parts) == 3 {
		return out, nil
	}
	n, nRest, nerr := leadingInt(parts[3])
	if nerr != nil || nRest != "" {
		out.Local = true
		return out, nil
	}
	out.N = n
	if len(parts) > 4 {
		out.Local = true
	}
	return out, nil
}

func leadingInt(s string) (n int, rest string, err error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, fmt.Errorf("%q has no leading digits", s)
	}
	n, err = strconv.Atoi(s[:i])
	return n, s[i:], err
}
```

Rewrite `ParseBase` to wrap it (keep the name; the `dev` result is now
`Local`, so `0.13.9.1` returns `dev=false`):

```go
func ParseBase(v string) (maj, min, pat int, dev bool, err error) {
	pv, err := ParseVersion(v)
	if err != nil {
		return 0, 0, 0, false, err
	}
	return pv.Major, pv.Minor, pv.Patch, pv.Local, nil
}
```

Delete the body that set `dev=true` on `len(parts) > 3` and
`strings.Count(v, ".") > 2`. Keep `NewerBase` implemented via
`ParseVersion` three-part fields only (still used by `TestNewerBase`;
`Run` stops calling it in Phase 2). Change its comment from
"MADR 0065 D2" to "three-part compare; `Run` uses NewerPublished (MADR
0103)".

Add:

```go
// NewerPublished reports whether remote is a strictly newer published
// version than local (major, minor, patch, then N). Local-ness is
// ignored here; callers refuse local builds separately (MADR 0103).
func NewerPublished(remote, local string) (bool, error) {
	r, err := ParseVersion(remote)
	if err != nil {
		return false, fmt.Errorf("remote: %w", err)
	}
	l, err := ParseVersion(local)
	if err != nil {
		return false, fmt.Errorf("local: %w", err)
	}
	if r.Major != l.Major {
		return r.Major > l.Major, nil
	}
	if r.Minor != l.Minor {
		return r.Minor > l.Minor, nil
	}
	if r.Patch != l.Patch {
		return r.Patch > l.Patch, nil
	}
	return r.N > l.N, nil
}
```

`BaseString` stays; it already uses `ParseBase`.

**1.2 `internal/update/version_test.go`**

Extend `TestParseBase` with cases that would have caught F1. Keep the
existing rows (the `.g` case stays `dev=true`). Add:

| in | maj,min,pat | dev | err |
|---|---|---|---|
| `0.13.9.1` | 0,13,9 | **false** | no |
| `v0.13.9.1` | 0,13,9 | false | no |
| `0.13.9.1.gdeadbee` | 0,13,9 | true | no |
| `0.13.9.1.ci99` | 0,13,9 | true | no |
| `debug` | 0,0,0 | true | no |

Add `TestParseVersionN` asserting `ParseVersion("0.13.9").N == 0`,
`ParseVersion("0.13.9.1").N == 1`, `ParseVersion("0.13.9.2").N == 2`.

Add `TestNewerPublished`:

```go
func TestNewerPublished(t *testing.T) {
	cases := []struct {
		remote, local string
		newer         bool
	}{
		{"0.13.10.1", "0.13.9.1", true},
		{"0.13.9.2", "0.13.9.1", true},  // chase N
		{"0.13.9.1", "0.13.9.1", false},
		{"0.13.9.1", "0.13.9", true},    // N=1 > N=0
		{"0.13.9.1", "0.13.9.1.gdead", false}, // same N; local-ness ignored here
		{"0.13.9.1", "0.13.10.1", false},
	}
	for _, tc := range cases {
		got, err := NewerPublished(tc.remote, tc.local)
		if err != nil || got != tc.newer {
			t.Errorf("NewerPublished(%q,%q)=%v err=%v, want %v",
				tc.remote, tc.local, got, err, tc.newer)
		}
	}
}
```

**Phase 1 verification**

```bash
make pre-add-check FILES="internal/update/version.go internal/update/version_test.go"
go test ./internal/update/ -count=1 -run 'TestParse|TestNewer'
```

Commit.

### Phase 2 — `Run` compares published asset `VER`

`AssetFor` already returns the suffix (`github.go:81-105`). Today `Run`
compares **before** `AssetFor`, against `rel.Base`. Move discovery of
the asset (and therefore `ver`) to immediately after `Latest`, **before**
the up-to-date / local-build gates.

**2.1 `internal/update/run.go` — gate rewrite**

Replace the block from the "latest release" print through the `--check`
return (currently lines 67-94) with:

```go
	asset, publishedVER, err := rel.AssetFor(opts.Product, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "latest release: %s (base %s, published %s)\n", rel.Tag, rel.Base, publishedVER)
	fmt.Fprintf(out, "local version:  %s\n", opts.LocalVersion)

	local, err := ParseVersion(opts.LocalVersion)
	if err != nil {
		return fmt.Errorf("local: %w", err)
	}
	if local.Local && !opts.Force {
		return fmt.Errorf("local build %q looks like a dev suffix; pass --force to update anyway", opts.LocalVersion)
	}
	newer, err := NewerPublished(publishedVER, opts.LocalVersion)
	if err != nil {
		return err
	}
	// MADR 0103: equal published VER is "up to date". --force re-seeds
	// from the asset. --check never honours --force.
	reinstall := false
	if !newer {
		if opts.Check || !opts.Force {
			fmt.Fprintln(out, "already up to date")
			return nil
		}
		reinstall = true
	}
	if opts.Check {
		fmt.Fprintf(out, "update available: %s → %s\n", opts.LocalVersion, publishedVER)
		return ErrUpdateAvailable
	}
```

Then the existing `--yes` prompt, using `publishedVER` in the update
prompt (`update %s → %s`, local → publishedVER) and `rel.Tag` still
fine for the reinstall prompt.

**Delete the later `AssetFor` call** (currently lines 109-112). `asset`
is already in scope. Keep `rel.SumsAsset(ver)` but pass `publishedVER`:

```go
	sums, err := rel.SumsAsset(publishedVER)
```

Update `ErrUpdateAvailable`'s comment (line 16) from "newer base" to
"newer published VER".

Gate order is mandatory (MADR shape):

1. local-build without `--force` → error (even if `--check`, even if
   not newer). A make-install host must not be told "update available".
2. not newer → "already up to date" (`--check` included; `--force` on
   a non-check run re-seeds).
3. `--check` + newer → `ErrUpdateAvailable`.

**2.2 `internal/update/run_test.go` — every `Run` fixture needs an asset**

`--check` now calls `AssetFor`. Fixtures that ship `"assets": []` will
fail with `no asset matching`. Give every `Run` test a single binary
asset named

```text
opts.Product + "-" + runtime.GOOS + "-" + runtime.GOARCH + "-" + publishedVER
```

Add a helper next to `releaseServer` (currently line 256):

```go
func releaseJSON(product, tag, publishedVER string) []byte {
	name := product + "-" + runtime.GOOS + "-" + runtime.GOARCH + "-" + publishedVER
	body, _ := json.Marshal(map[string]any{
		"tag_name": tag,
		"assets": []map[string]any{
			{"name": name, "browser_download_url": "http://invalid.example/bin", "size": 1},
			{"name": "SHA256SUMS-" + publishedVER, "browser_download_url": "http://invalid.example/sums", "size": 1},
		},
	})
	return body
}
```

Use it in the JSON-only tests (the ones that currently inline
`tag_name` / empty `assets`). `releaseServer` already emits
`…-9.9.9.1`; leave it, it is the happy-path swap fixture.

Rewrite / add tests. Exact expected behaviour:

| Test | LocalVersion | tag / publishedVER | Force | Check | Want |
|---|---|---|---|---|---|
| `TestRun_CheckAvailable` | `0.13.9.1` | `v0.13.10` / `0.13.10.1` | false | true | `ErrUpdateAvailable`; output contains `0.13.9.1 → 0.13.10.1` |
| `TestRun_CheckUpToDate` | `0.13.9.1` | `v0.13.9` / `0.13.9.1` | false | true | err=nil; "already up to date" |
| `TestRun_NewerNIsAvailable` | `0.13.9.1` | `v0.13.9` / `0.13.9.2` | false | true | `ErrUpdateAvailable`; output contains `0.13.9.1 → 0.13.9.2` |
| `TestRun_PublishedFourPartIsNotDev` | `0.13.9.1` | `v0.13.10` / `0.13.10.1` | false | false, Yes=true | does **not** contain "dev suffix"; reaches download (network/sums error is OK, same as today's `TestRun_HappyPathSwap`) |
| `TestRun_DevRequiresForce` | `0.13.9.1.gdead` | `v0.13.10` / `0.13.10.1` | false | true | error contains `--force`; does **not** return `ErrUpdateAvailable` |
| `TestRun_SamePublishedVERIsUpToDate` | `0.13.9.1` | `v0.13.9` / `0.13.9.1` | false | false, Yes=true | "already up to date"; no "downloading" |
| `TestRun_ForceReinstallsSameBase` | `0.13.9.1.gdeadbee` | `v0.13.9` / `0.13.9.1` | true | false, Yes=true | not "already up to date"; "downloading" (keep today's assertion shape) |
| `TestRun_CheckIgnoresForce` | `0.13.9.1.gdeadbee` | `v0.13.9` / `0.13.9.1` | true | true | "already up to date" (force does not fabricate an available update when published VER is not newer) |

Rename `TestRun_SameBaseWithoutForceIsUpToDate` to
`TestRun_SamePublishedVERIsUpToDate`. Do not keep a test that treats
local `9.9.9` (N=0) vs asset `9.9.9.1` as up to date — that is the F1
hole.

Existing swap tests (`TestRunSkipsHealStartWhenNotInstalled`,
`TestRunHealStartsWhenInstalledButDown`, `TestRunPassesRefresherToSwap`)
use `releaseServer` + `LocalVersion: "0.1.0"` against published
`9.9.9.1`. That is a real newer VER and has no local suffix, so they
keep passing without edits.

**Phase 2 verification**

```bash
make pre-add-check FILES="internal/update/run.go internal/update/run_test.go"
go test ./internal/update/ -count=1
```

Commit.

### Phase 3 — Service cycle flag equals `IsInstalled(this product)`

The Start/no-Start split is already 0100 F3 (`HealStart: installed`).
The lie is `RestartService: opts.Service != nil` (`run.go:140`), which
is always true for both CLIs. MADR: both flags are `installed`.

**3.1 `internal/update/run.go`**

In the `SwapAndRestart` literal, replace

```go
		RestartService: opts.Service != nil,
```

with

```go
		RestartService: installed,
```

Keep `HealStart: installed` and the 0100 F3 comment. Add one sentence
to that comment: "RestartService is the same bit (MADR 0103): no unit
file means binary-only. The installer must never start a daemon
without a unit, so this is not an orphan-heal path."

`active` is still passed as `WasActive` for the running-unit path.
When `installed == false`, `RestartService` is false, so `swap.go:78`
skips Stop/Start. Do **not** add an "orphan process" fixture or treat
`IsActive && !IsInstalled` as a supported host state.

Do **not** change `internal/cli/update.go` or `internal/relay/update.go`
wiring: they already pass `Product: "mcremote"` / `"mcrelay"` and a
`FuncService` whose methods take that product name. Isolation is
`opts.Product` flowing into `IsInstalled(opts.Product)`
(`run.go:136`). No cross-product list.

**3.2 `internal/update/run_test.go` — keep HealStart tests, add isolation**

Keep `TestRunSkipsHealStartWhenNotInstalled` and
`TestRunHealStartsWhenInstalledButDown` unchanged in assertion. Duplicate
the HealStart test's `Product` field once as `"mcrelay"` (same fake
Service, still only called with that name) — either a second test
`TestRunHealStartsWhenInstalledButDownMcrelay` or a table `[]string{"mcremote","mcrelay"}`. The table is preferred: one loop, `releaseServer(t, product, …)`, `RunOpts.Product: product`.

Add a recording wrapper (same file):

```go
type recordingService struct {
	update.FuncService // cannot embed from same package — just fields:
	inner   FuncService
	calls   []string // "IsActive:mcremote", "Start:mcrelay", …
}

func (r *recordingService) record(op, product string) {
	r.calls = append(r.calls, op+":"+product)
}
```

Implement `IsActive` / `IsInstalled` / `Stop` / `Start` to append
`op+":"+product` then delegate. Do not import `update` — tests are
`package update`.

`TestRunServiceCallsUseThisProductOnly`:

* Table of `product` in `{"mcremote","mcrelay"}`.
* Recording service with `IsInstalledFn` true, `IsActiveFn` true,
  Stop/Start succeed.
* After `Run`, every recorded call's product suffix equals `product`.
  Fail if any call mentions the other name.

**Phase 3 verification**

```bash
make pre-add-check FILES="internal/update/run.go internal/update/run_test.go"
go test ./internal/update/ ./internal/cli/... ./internal/relay/... -count=1
```

Commit.

### Phase 4 — CLI help and operator docs

**4.1 Flag help** — both files, the `--force` `BoolVar` usage string
(`internal/cli/update.go:66`, `internal/relay/update.go:51`):

```go
cmd.Flags().BoolVar(&force, "force", false, "reinstall the latest release even when not newer, and over a locally compiled build (dev / debug / .g<hash>)")
```

Do not mention "four-part" or `BASE.N` as a reason for `--force`.

**4.2 `docs/config.md`** — section `### mcremote update / mcrelay update`
(currently lines 598-612):

* Title: keep the heading; add `(MADR 0065, amended 0103)`.
* Body: `update` compares the running binary to the published asset
  version (`BASE.N`, e.g. `0.13.9.1`), not the GitHub tag (`v0.13.9`).
  A newer `N` on the same tag is an update. Recycles the user service
  only when **this** product has a unit/plist; a crashed unit is
  restarted; no unit means binary-only.
* `--force` row: `Allow updating a locally compiled build (e.g.
  0.13.9.1.gdeadbeef). A published BASE.N such as 0.13.9.1 does not
  need --force.`

**4.3 `docs/ops-linux-install.md`** — "Updating" (currently lines
145-163): after the four-step list, add one paragraph:

`mcremote update` only cycles `mcremote.service`. `mcrelay update` only
cycles `mcrelay.service`. If that product has no unit file, the command
still replaces the binary and does not start a service. If the unit
exists but the process is down, the update starts it (MADR 0103).

**4.4 `README.md`** — "Upgrading later" (currently lines 80-86): after
the `mcremote update` fenced block, one sentence:

Published binaries are stamped `BASE.N` (e.g. `0.13.9.1`). `update`
follows that string, not the `v0.13.9` tag; `--force` is only for a
local `make` build. Each command recycles only its own unit, if one
exists.

**4.5 `scripts/install_test.sh` — installer never starts without a unit**

Near the top of the suite (after the helpers, before case 4-7), add a
static check against `scripts/install.sh`:

```sh
printf '\n0103. installer does not start a daemon without a unit\n'
# nohup appears only inside log/summary strings, never as a command.
nohup_cmd=$(grep -n 'nohup' "$INSTALLER" | grep -v 'log ' || true)
check "nohup is never exec'd (advice-only in summary)" \
  "$( [ -z "$nohup_cmd" ] && echo none || echo "$nohup_cmd" )" none
```

`--no-service` already installs binaries and starts nothing (existing
cases at lines 151-154). Do not add a path that launches `serve`
without `setup-service`. If this grep fails, the installer grew a
forbidden start; fix the script, do not weaken the check.

Do not edit 0065 or 0100 bodies.

**Phase 4 verification**

```bash
make pre-add-check FILES="internal/cli/update.go internal/relay/update.go"
go test ./internal/cli/... ./internal/relay/... -count=1
sh scripts/install_test.sh
```

Commit.

### Phase 5 — Full-suite verification and acceptance

1. `make test`
2. `make race`
3. `make preflight` (no Dart changes; still required by AGENTS.md)
4. Manual, on a host that already has the previous published binary
   (`mcremote version` prints `0.13.9.1` or whatever `BASE.N` is
   current **before** this lands — after a real newer GitHub release,
   or against a test API):
   * Stock curl\|sh (mcremote unit, **no** mcrelay unit):
     `mcremote update --yes` without `--force` applies a newer
     published VER and leaves mcremote active.
     `mcrelay update --yes` without `--force` replaces the binary,
     prints the "restart yourself if needed" line, does not create
     `mcrelay.service`.
   * `mcremote update --check` when already on the published VER:
     exit 0, "already up to date".
   * Kill the mcremote unit (`systemctl --user stop mcremote` or
     launchctl bootout), `mcremote update --yes`: unit is active
     again (HealStart).
   * mcrelay-only host (`mcrelay setup-service` done, no mcremote
     unit): the inverse of the first bullet.
   * A `make install` binary whose version contains `.g`: without
     `--force`, error; with `--force --yes`, re-seeds.

Live-tagged CLI suites are unaffected (no provider surface).

## Verification

| MADR 0103 Confirmation item | Where proven |
|---|---|
| `0.13.9.1` is not local; N-order; `.g` is local | Phase 1 `TestParseBase` / `TestParseVersionN` / `TestNewerPublished` |
| `0.13.9.1` → `0.13.10.1` without `--force` | `TestRun_PublishedFourPartIsNotDev`, `TestRun_CheckAvailable` |
| equal `0.13.9.1` is up to date | `TestRun_CheckUpToDate`, `TestRun_SamePublishedVERIsUpToDate` |
| `0.13.9.1` → `0.13.9.2` (same tag, newer N) | `TestRun_NewerNIsAvailable` |
| local `.g` without `--force` errors; `--check` is not exit 10 | `TestRun_DevRequiresForce` |
| `--check` names both full VERs | `TestRun_CheckAvailable` |
| no unit file: no Stop, no Start | `TestRunSkipsHealStartWhenNotInstalled` |
| installer never starts without a unit | Phase 4.5 `install_test.sh` nohup grep; existing `--no-service` cases |
| unit file, process down: Start once | `TestRunHealStartsWhenInstalledButDown` (both product names) |
| unit file, process up: stop → refresh → start | existing `TestRunPassesRefresherToSwap` |
| no cross-product service calls | `TestRunServiceCallsUseThisProductOnly` |
| Manual matrix (curl\|sh, crash heal, mcrelay-only) | Phase 5 step 4 |

## Rollout and Rollback

**Rollout.** No migration and no config key. Operators on `0.13.9.1`
pick this up the **first** time they run `update --force` against a
build that contains 0103 (the currently shipping binary still has F1,
so it cannot self-apply 0103 without `--force` or a re-run of the
one-liner). After that hop, further updates do not need `--force`.
Document that one-time exception in the Phase 4 README sentence if
this lands as `0.13.9.2` under tag `v0.13.9`, or as `0.13.10.1` under
`v0.13.10` — same story either way.

**Rollback.** Revert the four phase commits. Behaviour returns to
"four-part ⇒ `--force`" and `RestartService: Service != nil`. No
on-disk state other than the swapped binary.

**Compatibility.** Old phones and old daemons are unrelated. An old
`update` binary talking to a new GitHub release still has F1 until it
is replaced by one of the paths above.
