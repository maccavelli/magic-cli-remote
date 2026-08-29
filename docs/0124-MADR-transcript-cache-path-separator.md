---
status: proposed
date: 2026-08-29
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Parse entry filenames with a separator-agnostic basename, not `split('/')`

## Context and Problem Statement

Six Flutter tests fail on a Windows host and pass in CI. They have been
attributed to "Windows path weirdness" twice in this repository — in
[0123](0123-PLAN-unify-session-controls-below-the-composer.md)'s execution
record and again in its deferred list — without anyone reading the code.

The cause is one line of production Dart, and the tests are right.

### What was measured, not assumed

**The failing set is exactly six, and all six run through one function.**

```text
transcript_cache_test.dart  retainOnly keeps the index for sessions that are all live
transcript_cache_test.dart  retainOnly re-adopts blobs stranded by a lost index
transcript_cache_test.dart  clear() racing saves removes every entry
transcript_cache_test.dart  evicting the oldest session under load keeps index and blobs in step
transcript_cache_test.dart  eviction still bites after a retainOnly
history_replay_test.dart    syncFromMeta evicts dead sessions from the cache too
```

**`_storedIds` splits a filesystem path on `/` only**
(`transcript_cache.dart:340`):

```dart
final name = e.path.split('/').last;
```

**On Windows `Directory.list()` yields backslashes, so that returns the wrong
string.** Measured directly, saving one session and listing the directory:

```text
listed path            = C:\Users\...\probe_cache…/transcripts\s1.json
split('/').last        = transcripts\s1.json        <- wanted: s1.json
index after save       = [s1]
index after retainOnly = []
```

The save is correct. `retainOnly` is what destroys it.

**The mechanism, end to end.** `transcripts\s1.json` still satisfies
`name.endsWith('.json')`, so the entry is admitted with
`transcripts\s1` as its *session id*. That id is not in `liveIds`, so the
file is treated as dead: `_deleteFile` is called for a percent-encoded
non-existent name, `surviving` ends up empty, and the index is rewritten
empty — dropping sessions that are alive. The `PathAccessException:
Access is denied` seen in the logs is the same defect one step further on,
when a mangled id is turned back into a filename.

**It is one site, not a class.** A sweep for the same shape across the app
found no other `split('/')` on a filesystem path.
`attachmentBasename` (`chat_screen.dart:200-205`) already strips both
separators, deliberately.

**The app ships Android only.** `apps/mobile/` has `android/`, `ios/` and
`linux/` scaffolding, and the only artifact built anywhere is
`flutter build apk` (`Makefile:433`, `ci.yml:553`). `Directory.list()` returns
`/`-separated paths on every one of those, so **this defect reaches no user
today**. Its victims are a Windows developer host and any future desktop
target.

**CI cannot see it.** The Flutter lane is `ubuntu-latest` (`ci.yml:385`), so
the lane that would catch it is the one platform where the code is correct.
That job's three steps are `dart format`, `flutter analyze` and `flutter test`
(`ci.yml:406-413`); the first two are platform-independent, the third is not.

**`windows-latest` is already in use and already free.** Two jobs run on it —
`go-native` and `smoke-native` (`ci.yml:275`, `:341`) — and both set
`shell: bash` explicitly, because the runner's default shell is PowerShell.
Standard GitHub runners are free and unlimited on public repositories
([0120](0120-MADR-retire-the-darwin-amd64-target.md) F8), so the cost of a
Windows Flutter leg is wall-clock in a parallel job, not money.

**The workspace wire contract normalises separators, so `parentOf` is not the
same bug.** This was checked rather than assumed. Every `WorkspaceEntry` in the
tree is produced in exactly one place, `opencode/workspace.go:226` (which backs
both opencode and kilo through httpagent's dialect), plus the test fake. Both
the path a client sends and the path the engine returns pass through
`normalizeWorkspacePath` (`opencode/workspace.go:63-94`):

```go
// Accept either separator from a client, then work in slash form.
s := strings.ReplaceAll(in, "\\", "/")
...
if strings.HasPrefix(s, "/") {
    return "", fmt.Errorf("%w: absolute paths are not accepted", errWorkspaceInvalidPath)
}
cleaned := path.Clean(s)   // POSIX path, not filepath
```

Backslashes are rewritten, absolute paths are refused, and `path.Clean` is the
POSIX cleaner regardless of host OS. `validateReturnedPath` (`:148`) puts the
engine's own output through the same function, so a Windows daemon host cannot
emit a backslash path either. The wire format is **relative and
slash-separated, enforced on both ingress and egress**, which is what
`workspace_sheet_test.dart:164-168` already encodes (`parentOf('a/b') == 'a'`).

`parentOf` is therefore correct as written.

### Findings

**F1 — The six failures are one defect in production code, not test flakiness
and not an environment quirk.** They were twice recorded as "Windows-only test
failures", which framed a real bug as an environmental nuisance.

**F2 — `retainOnly` silently empties the LRU index on Windows.** Its own test
names the invariant: *"a live session must not lose its place in the LRU
index."* On a Windows host it loses every one.

**F3 — No user is affected today.** Android, iOS and Linux all use `/`. The
severity is "wrong code that cannot currently bite", not "data loss in the
field", and the record should not claim otherwise.

**F4 — The blast radius grows the moment a Windows desktop target appears.**
`retainOnly` runs on every sessions-screen refresh and every reconnect, so on
such a build the cache would be wiped continuously.

**F5 — The guard that would have caught it does not exist.** Nothing asserts
that entry enumeration is separator-agnostic, and CI runs only on Linux, so
the next equivalent mistake is equally invisible.

**F6 — `workspace_sheet.dart`'s `parentOf` is not a second instance of this
bug.** The suspicion was reasonable and it is wrong. The daemon normalises
workspace paths to relative slash form on both ingress and egress
(`opencode/workspace.go:63-94`, `:148`), and every entry in the app comes from
that one producer. A Windows daemon host emits `/` like any other. Changing
`parentOf` would add defensive code against a state the protocol forbids, which
is worse than leaving it: it would imply the wire contract is untrusted at one
call site and trusted everywhere else.

**F7 — Two of the Flutter lane's three steps are platform-independent.**
`dart format` and `flutter analyze` produce the same answer on any host, so a
second lane that repeated them would buy nothing and double the surface that
can go red for unrelated reasons. Only `flutter test` exercises `dart:io`.

## Decision Drivers

* The tests are correct; the code is wrong. Fix the code.
* A developer host that cannot run a green suite erodes the suite's authority —
  six permanent reds teach people to ignore reds.
* Path handling should not be re-derived by hand at each call site.
* Do not overstate severity: no user is affected, and the record should say so
  plainly rather than borrowing urgency it has not earned.

## Considered Options

* **A — Take a separator-agnostic basename at the one site.**
* **B — Adopt the `path` package's `basename` across the app.**
* **C — Normalise separators when the directory is constructed.**
* **D — Skip the six tests on Windows.**

## Decision Outcome

**Chosen: A, with the helper written once and tested.**

B is the textbook answer and `path` is already in the lock file transitively,
but promoting a transitive dependency to a direct one to fix a single call site
is a bigger change than the defect warrants, and it invites a sweep of every
string-path expression in the app — worth doing, not worth doing *here*.

C treats the symptom: the interpolated `'${base.path}/$_dirName'` is not the
problem, and a build where `dir.path` is normalised would still be one
`Directory.list()` away from the same bug, because the separator comes from the
platform, not from us.

D is rejected outright. It is the option that has effectively been in force for
two records by describing the failures as environmental, and it would convert a
real defect into a permanent blind spot on the exact platform this project is
currently trying to support.

### The decisions

**D1 — Parse entry names with a separator-agnostic basename.** Split on
`[\\/]`, at `transcript_cache.dart:340`.

**D2 — The tests do not change.** All six assert correct behaviour today. If a
fix requires editing them, the fix is wrong.

**D3 — Guard the invariant directly.** Add a test that a
backslash-separated listing yields a bare session id, so the next person who
reaches for `split('/')` fails on Linux too rather than only on a machine CI
never runs.

**D4 — State the severity honestly (F3).** No user-facing data loss. The fix is
justified by correctness and by a usable developer host, not by an incident.

**D5 — Do not widen the fix itself.** No `path` package adoption and no sweep
of other path expressions. Both are named as deferred.

**D6 — Add a Windows leg to the Flutter CI lane, running tests only (F5, F7).**
This defect survived two records because nothing but a developer's own machine
could see it, and the fix's guard (D3) is worth little if the only host that
would fail without it never runs.

The leg runs **`flutter test` alone**. `dart format` and `flutter analyze` stay
on Linux: F7 says they answer identically on any host, so repeating them would
buy nothing and add a second way to go red for reasons unrelated to Windows.
It sets `shell: bash` explicitly, as `go-native` and `smoke-native` already do
on `windows-latest` (`ci.yml:275`, `:341`), because the runner defaults to
PowerShell. Cost is wall-clock in a job that runs in parallel; standard runners
are free on public repositories (0120 F8).

**D7 — `workspace_sheet.dart`'s `parentOf` is left exactly as it is (F6).**
It was raised as a suspected second instance and it is not one. The daemon
guarantees relative, slash-separated workspace paths on both ingress and
egress, so splitting on `/` is reading the contract correctly, not assuming a
platform. Recorded as a decision rather than dropped, so the same suspicion is
not re-raised and "fixed" later by someone who has not read
`normalizeWorkspacePath`.

### Consequences

* Good: `flutter test` is green on a Windows host, so a developer on the
  platform this project is currently focused on can trust the suite.
* Good: a latent landmine under any future desktop target is removed before it
  can be stepped on (F4).
* Good: the "Windows-only test failures" line disappears from two records
  rather than being carried forward a third time.
* Neutral: no user-visible change on any shipped platform.
* Good: CI gains the ability to see a Windows-only Flutter regression at all
  (D6), which is the gap that let this one survive two records.
* Bad: a fourth lane that can block a merge, on the platform whose toolchain
  differs most. The `.gitattributes` `eol=lf` rule should keep `dart format`
  honest there, but that is a prediction this plan has to test rather than
  assume.

### Confirmation

```bash
cd apps/mobile
flutter analyze
flutter test          # 0 failing on a Windows host
git diff --stat apps/mobile/test/transcript_cache_test.dart   # additions only (D2)
git diff apps/mobile/lib/features/chat/workspace_sheet.dart   # empty (D7)
```

```text
CI, Flutter (test) on windows-latest  -> green
same leg with the fix reverted        -> red, and red on the new guard
```

## Pros and Cons of the Options

### A — Separator-agnostic basename at the one site (chosen)

* Good: one line, at the one place the sweep found.
* Good: leaves the tests untouched, which is the proof the fix is right.
* Bad: the codebase still hand-rolls path parsing; the next site is unguarded
  except by D3's test.

### B — Adopt `path` and use `basename` everywhere

* Good: the correct long-term answer, and `path` is already resolved
  transitively.
* Bad: promotes a transitive dependency and invites an app-wide sweep, for a
  defect that is one line.

### C — Normalise separators at directory construction

* Good: looks like it fixes a class of problems.
* Bad: it does not. `Directory.list()` returns platform separators regardless
  of how the parent path was spelled.

### D — Skip the six tests on Windows

* Good: green immediately.
* Bad: it is the status quo in all but name, and it hides a real defect on the
  one platform currently under active work.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| The offending split | `apps/mobile/lib/data/chat/transcript_cache.dart:340` |
| Wrong basename, measured | probe run: `split('/').last = transcripts\s1.json` |
| Index emptied by `retainOnly` | probe run: `index after save = [s1]`, `after retainOnly = []` |
| The invariant the test names | `transcript_cache_test.dart:320-324` |
| Only one such site in the app | `grep -rn "split('/')" apps/mobile/lib` → one hit |
| `attachmentBasename` already handles both | `chat_screen.dart:200-205` |
| Android is the only shipped artifact | `Makefile:433`, `ci.yml:553` |
| Flutter CI lane is Linux | `.github/workflows/ci.yml` |

### Related records

* [0123](0123-PLAN-unify-session-controls-below-the-composer.md) — recorded
  these six as pre-existing and Windows-only, twice, and deferred them. This
  record is that deferral coming due.
* [0116](0116-MADR-windows-and-linux-arm64-build-targets.md) — established
  `windows/amd64` as a supported daemon target; the same class of separator
  assumption is what its P11 found in Go golden files.

### Open questions for the plan

1. ~~Should the Flutter CI lane gain a Windows leg?~~ **Answered: yes** — D6.
   Scoped to `flutter test` only, per F7.
2. ~~Is `parentOf` the same bug for a Windows daemon host?~~ **Answered: no** —
   F6, D7. The daemon normalises to relative slash form on both ingress and
   egress (`opencode/workspace.go:63-94`, `:148`), so a Windows host emits `/`
   like any other. The suspicion was checked and closed rather than carried.
3. Does `dart format --set-exit-if-changed` behave identically on
   `windows-latest`? `.gitattributes` pins `eol=lf` for the whole tree, so a
   fresh checkout there should hold LF — but this host's *stale* checkout is
   CRLF and does make `dart format` disagree, which is why the leg runs tests
   only (D6) and why this is worth watching rather than assuming.
4. Flutter setup is the slow part of that job. Is `cache: true` (already set on
   the Linux leg, `ci.yml:401`) effective on `windows-latest`, or does the leg
   pay a full SDK download every run?
