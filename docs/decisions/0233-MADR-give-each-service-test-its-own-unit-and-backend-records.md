---
status: proposed
date: 2026-11-05
decision-makers: Project Owner
consulted: "this repository at 06322d51: internal/cli/service (unitrecord.go, backendrecord.go, setup.go, control.go, refresh.go and their tests), internal/updateclient/managed_script_linux_test.go, internal/appdirs/roots_unix.go; six scratch probes, listed under More Information"
informed: anyone who writes or changes a test in internal/cli/service, or the package's TestMain
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Give each service test its own unit and backend records, and fail a test that reaches the shared ones

## Context and Problem Statement

While finding the session tests that leak writes
([0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md](0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md),
Deviation 10), a run of the whole suite with `-count=2` failed one test in `internal/cli/service`:

```text
--- FAIL: TestIsInstalledDarwinPlistPresent
    installed_test.go:92: plist present must be true
```

Alone, with `-count=1` or `-count=2`, it passed. So it depends on another test. The investigation
found that it is one of eleven tests that fail when the package's tests run in another order, and
that 36 tests depend on state that the package's `TestMain` shares across the whole test binary.
None of this touches the product: it is how the tests are isolated.

### F1. Two records, one shared directory

* `setup-service` records the unit name it installed under, and the backend it used, in the
  product's state directory (`unitrecord.go:20-39`, `backendrecord.go:21-30`; MADR 0201 D30). Every
  later control and refresh path reads them:
  * `namesFor(product, "")` takes the name from `ResolveUnitName` (`unitrecord.go:114-126`), and
    `IsInstalled` builds its launchd label and systemd unit from it (`control.go:44-61`);
  * `RefreshUnit` takes the name from the record when none is given (`refresh.go:112-119`), and on
    Linux the backend from `ResolveBackend` (`refresh.go:130`), which reads the backend record when
    one exists (`backendrecord.go:39-65`).
* `Setup` writes both records after installing (`setup.go:500-531`).
* The directory comes from a package variable, `unitRecordDir` (`unitrecord.go:25-31`), which both
  records use (`backendrecord.go:26`).
* `TestMain` (`unitrecord_test.go:15-46`, added in `91960170`, 2026-10-19) points `unitRecordDir`
  at **one** `os.MkdirTemp` directory for the whole binary, the external `service_test` files
  included. Its purpose, in its comment: "no test may write it into the developer's real state
  directory". It is removed only after `m.Run`.
* Two ways to isolate one test exist, and some tests use them: `withUnitRecord`
  (`unitrecord_test.go:59-72`), used by 11 other test files, and a direct override in
  `uninstall_unix_test.go:28-30`.

### F2. Proven: the plist test reads a name another test recorded

Probe 1 (`libs/plist_probe.py`) makes the failing assertion log what `IsInstalled` read, on a
scratch copy, and runs the package with `-count=2`. At the failure:

```text
installOS="darwin" HOME="<TMPDIR>/TestIsInstalledDarwinPlistPresent…/001"
ResolveUnitName="mcrelay-test" recordPath="<TMPDIR>/service-unit-record-…/mcrelay/service-unit" record="mcrelay-test\n"
label="com.magiccliremote.mcrelay-test" statOfLabelPlist=stat …/Library/LaunchAgents/com.magiccliremote.mcrelay-test.plist: no such file or directory
```

The test writes `com.magiccliremote.mcrelay.plist`. `TestSetupWritesDefaultMcrelayConfig`
(`setup_test.go:139`) installs with `UnitName: "mcrelay-test"` (`:156`). `Setup` records that name
in the shared directory, and nothing removes it.

### F3. Proven: the order decides, not the count

In the default order, the plist test (`installed_test.go`, internal) runs before the setup test
(`setup_test.go`, external), so in one run the read comes before the write: F2's first run
passed it. `-count=2` repeats the run in the same process, after the write.

Probe 2 (`libs/plist_seeds.py`): the two tests alone, `-count=1`, `-shuffle` seeds 1 to 12, in the
committed tree.

| Seeds | Order | The plist test |
| :--- | :--- | :--- |
| 6 and 12 | setup first | fails, `installed_test.go:92` |
| the other ten | plist first | passes |

Probe 3 (`libs/plist_order.py`) repeats it on a scratch copy where the plist test takes its own
record directory (`withUnitRecord(t, "mcrelay", "")`): it passes for all 12 seeds, both
setup-first seeds included. The whole package on that copy with `-count=2`: exit 0, `ok …
139.357s`.

### F4. Proven: it is not one test. Eleven fail under shuffle

Probe 4 (`libs/service_shuffle.py`, `libs/shuffle_msgs.py`): the whole package, committed,
`-count=1`, `-shuffle` seeds 6, 12, 101 and 202. Every seed failed:

| Seed | Failing tests, with their first message |
| :--- | :--- |
| 6 | `TestRefreshPinsRenderedEnvironment` (`refresh_test.go:285`: verdict "none", want unchanged); `TestRefreshKeepsUnparseableExecStart`, five subtests (`:393`: verdict "none", want kept); `TestRefreshWarnsOnForeignExecStartBinary` (`:487`: expected a foreign-binary warning) |
| 12 | `TestIsInstalledLinuxLoadState`, two subtests (`installed_test.go:41`: probe = `[--user show -p LoadState --value mcremote-test.service]`; `:71`); `TestIsInstalledDarwinPlistPresent` (`:92`); `TestRefreshRewritesStaleUnit` (`refresh_test.go:195`: verdict "none", want refreshed) |
| 101 | `TestRefreshKeepsForeignUnit` (`:358`: verdict "none", want kept); `TestRefreshKeepsHandEditedUnit` (`:323`); `TestRefreshDarwinRewritesPlist` (`:459`: verdict "none", want refreshed) |
| 202 | `TestRefreshKeepsHandEditedUnit`; `TestRefreshDarwinRewritesPlist`; `TestRefreshMovesASimpleUnitToNotify/mcrelay` (`notify_unit_test.go:110`); `TestRefreshWarnsOnForeignExecStartBinary` |

That is eleven distinct top-level tests. Each refresh failure is the verdict "none": no
definition found.

### F5. Proven: both records leak

Probe 5 (`libs/record_scope.py mech`): `TestRefreshKeepsForeignUnit`'s failing check logs both
records, under seed 101:

```text
ResolveUnitName="mcremote-env" unit record="mcremote-env\n"
ResolveBackend="launchd-agent" backend record="launchd-agent\n"
```

The test writes `mcremote.service` and passes no `UnitName` (`refresh_test.go:353`). With the
record naming `mcremote-env`, which `setup_test.go:447` installs, the refresh looks for
`mcremote-env.service` and finds nothing. The backend record is another test's too.

### F6. Proven: 36 tests depend on the shared directory

Probe 6 (`libs/record_scope.py scope`) replaces `TestMain`'s directory with one that returns an
error, on a scratch copy, so any test that reaches a record without its own directory meets the
error, in any order. The whole package, `-count=1`, in its normal order: 36 top-level tests fail,
in 10 files:

| File | Package | Tests |
| :--- | :--- | ---: |
| `refresh_test.go` | internal | 11 |
| `setup_test.go` | external | 6 |
| `launchd_sequence_test.go` | external | 5 |
| `control_test.go` | internal | 4 |
| `setup_launchd_test.go` | external | 3 |
| `installed_test.go` | internal | 2 |
| `installerenv_unix_test.go` | internal | 2 |
| `backend_test.go` | internal | 1 |
| `notify_unit_test.go` | internal | 1 |
| `nostart_launchd_test.go` | external | 1 |

Every test in F4 is among them. The full list is in the probe's log. This finds a test that fails
when the record cannot be reached. A test that reaches a record and ignores the error passes, and
is not listed. It is harmless today for the same reason: nothing it checks depends on the record.

### F7. Why nothing has caught it

* Neither the `Makefile`, `.github/workflows/ci.yml` nor `scripts/` runs a test with `-shuffle`
  (`git grep shuffle`: no match). `make test` and `make race` pass no `-count` either
  (`Makefile:342`, `:415`), so each test runs once. So the order is always the default one, in
  which every reader happens to run before the writer that would break it.
* A new test, a renamed file or a moved function changes that order.

### F8. Outside this package

* `internal/updateclient/managed_script_linux_test.go` calls `service.Setup`, which writes the
  records through the real `unitRecordDir`. It sets `HOME` to a temporary directory (`:61`) and
  clears `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` (`:62`). The
  state directory follows `XDG_STATE_HOME` or `HOME` (`internal/appdirs/roots_unix.go:44`), so that
  test is isolated.
* No other package's tests call a function that reads or writes the records (`git grep` for
  `service.(Setup|Remove|RefreshUnit|IsInstalled|ProbeStatus|ResolveUnitName|ResolveBackend)(`
  outside the package: only that file).

## Decision Drivers

* **A test's result does not depend on which tests ran before it.**
* **No test writes the developer's real records**, which is why `TestMain` exists (F1).
* **A test that forgets isolation fails at once, with a message that says so**, in any order. A
  rule in a comment did not stop 36.
* **Follow the package's own idioms:** `withUnitRecord` inside, and exported
  `Override…(…) (restore func())` functions for the external tests (ten exist, among them
  `OverrideInstallOS`, `setup.go:340`).

## Considered Options

* **A** — `TestMain`'s directory becomes an error. Every one of the 36 tests takes its own record
  directory: `withUnitRecord` inside the package, and a new exported `OverrideRecordDir` for the
  external files.
* **B** — Isolate the eleven tests F4 found failing, and leave the shared directory.
* **C** — Keep the shared directory, and run the package with `-shuffle=on` in the gates, to catch
  order dependence when it appears.
* **D** — Clear the shared directory after each writing test.
* **E** — Do nothing.

## Decision Outcome

Chosen option: **"A"**, proposed. It removes the shared state and turns any future reliance on it
into an immediate failure, in any order. It keeps `TestMain`'s guarantee that no test reaches the
real state directory.

### The decisions (proposed)

* **D1 — `TestMain`'s record directory returns an error.** `unitRecordDir`, in `TestMain`, returns
  an error that names the product and says the test reached its unit or backend record without its
  own directory. No directory is shared. The real state directory is still never reached, so F1's
  guarantee holds.
* **D2 — An exported override for the external tests.** `OverrideRecordDir(dir string) (restore
  func())` beside `unitRecordDir`, in the shape of the package's other `Override…` functions: it
  points both records at `dir/<product>` and returns the restore. `withUnitRecord` stays for the
  internal tests, and so does `uninstall_unix_test.go`'s own override.
* **D3 — Each of F6's 36 tests takes its own directory:** `withUnitRecord(t, <product>, "")` in an
  internal test, or `defer service.OverrideRecordDir(t.TempDir())()` in an external one, before
  its first `Setup`, `Remove`, `RefreshUnit`, `IsInstalled` or status probe. A test that installs
  two products, or reads one another wrote, uses one directory for both.
* **D4 — The comment on `TestMain` says what a test must do,** and why the directory is an error.
* **D5 — No change outside the package** (F8).

### Consequences

* Good, because no test's result depends on order: F4's eleven pass under any seed.
* Good, because a new test that touches a record without its own directory fails every run, in
  any order, with D1's message. It does not fail later in someone else's test.
* Good, because the developer's real state directory stays out of reach.
* Neutral, because the product does not change. D2 adds one exported function used only by tests,
  as the package's ten others are.
* Bad, because it touches 36 tests in 10 files, though each by one line.
* Bad, because a test that reaches a record and ignores the error still passes under D1 (F6). It
  checks nothing the record affects, but it is not forced to isolate.

### Confirmation

* **Seen failing first,** on scratch copies, never in the tree: F3's two seeds and F4's four fail
  on the commit before D3, and F6's run lists 36 tests.
* **After D1 to D3:**
  * F6's run, with D1 in place for real, finds no failing test;
  * F4's seeds 6, 12, 101 and 202, and F3's 1 to 12, pass;
  * the whole package passes with `-count=2`;
  * a planted test on a scratch copy that calls `IsInstalled` without its own directory fails
    with D1's message, in both orders.
* `make race`, the Linux test host's `go test -race ./...`, and `make ci-windows`, since the package
  runs on all three.

## Pros and Cons of the Options

### A — An error for the shared directory, a directory per test (chosen)

* Good, because it removes the cause, and makes the rule mechanical.
* Good, because F6's run, made real, is its own check.
* Bad, because it is the widest change: 36 tests.

### B — Isolate only F4's eleven

* Good, because it is the smallest change that makes today's seeds pass.
* Bad, because F4 is four seeds. The other 25 tests of F6 still share the directory, and other
  orders can still fail them.
* Bad, because writers keep writing the shared records, so the next reader added is the next
  victim.

### C — Keep sharing, and shuffle in the gates

* Good, because it would find the next order dependence in any package, not only this one.
* Bad, because it detects at random. A failure appears on whatever run draws a bad seed, often
  someone else's, and blames whichever test read last.
* Bad, because it changes the gates, which is the owner's decision, and it adds a run of every
  package's tests in a random order to each gate. It could follow A, as a separate decision.

### D — Clear the shared directory after each writer

* Good, because it keeps one directory.
* Bad, because every writer has to remember to clear it, which is the same rule as A without A's
  failure when it is forgotten.
* Bad, because a reader that runs before a writer's cleanup, as subtests and `t.Parallel` can, still
  sees another test's record.

### E — Do nothing

* Good, because nothing changes.
* Bad, because the next new or renamed test can reorder the package and fail any of the 36, under
  a name that has nothing to do with the cause. `-count=2` already does.

## More Information

### Evidence index

Every probe ran on a scratch copy of `06322d51` or in the clean committed tree (F3, F4: the
scripts assert `git status --porcelain` is empty), and wrote nothing to it. Scripts and logs are in
the session scratchpad, under `libs/`.

* **Probe 1** `plist_probe.py`, log `plist_probe.log`: F2.
* **Probe 2** `plist_seeds.py`: F3's table.
* **Probe 3** `plist_order.py`, log `plist_order_whole.log`: F3's fixed copy. Its first version also
  tallied the order, which shows 2 setup-first seeds of 12 on both copies, failing only on `HEAD`.
* **Probe 4** `service_shuffle.py`, logs `service_shuffle_<seed>.log`; messages by
  `shuffle_msgs.py`: F4. The first summary printed no messages: `go test -v` writes a test's messages
  before its `--- FAIL` line, and the script looked after it. `shuffle_msgs.py` reads between the
  test's `=== RUN` and its `--- FAIL`.
* **Probe 5** `record_scope.py mech`, log `record_scope_mech.log`: F5.
* **Probe 6** `record_scope.py scope`, log `record_scope_sentinel.log`: F6.

### Related records

* [0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md](0201-MADR-fix-the-open-findings-of-the-0073-debug-pass.md):
  D30, which added the unit record, its directory variable and `TestMain`.
* [0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md](0201-PLAN-fix-the-open-findings-of-the-0073-debug-pass.md):
  Deviation 10, which found this.
