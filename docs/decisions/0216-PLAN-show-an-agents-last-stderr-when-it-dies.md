---
status: in-progress
date: 2026-10-29
associated-madr: "0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement the stderr tail for every supported agent, and the phone's view of it (0216 item 1)

Associated MADR: [0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md](0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md):

* D1 to D5 and D20, accepted 2026-10-29;
* its "Amendment — 2026-10-29 (A)": D22 to D30, decided by the owner the same day (questions 8 to
  11), and D31, accepted the same day ("accept d31").

The MADR has three plans (D18). This is the first, and it depends on neither of the others:
`0216-PLAN-record-both-ways-of-the-acp-wire.md` (D6 to D12) and
`0216-PLAN-replay-recorded-acp-connections.md` (D13 to D17) are written later.

**The owner's answers fix the design.** On 2026-10-29 the owner chose the recommended option of each
question:

* one writer for every transport (D23, question 9);
* 4096 bytes and 64 lines (D24, question 8);
* the credential-name pattern plus named extras (D27, question 10);
* the shared separator, with the exit error in every death message (D26 and D28, question 11).

## Goal

Every supported agent reports its last stderr, bounded and with known secrets removed, when it fails
to start and when it dies while running. The agents are grok and gobble (`acpagent`), codex, and kilo
and opencode (`httpagent`).

* **The phone** shows a long error collapsed, with a two-line preview, and expands it on a tap.
* **The OS notification** shows the error's first line only.
* **Memory** stays bounded: 4096 bytes of tail per engine process in the daemon, and 8 KiB per error
  item on the phone.

## Scope

* **In scope:**
  * a new package `internal/provider/stderrtail`;
  * `internal/provider/acpagent` (`acpagent.go`, its tests, `Spec`);
  * `internal/provider/grok/grok.go`, but only if a credential name falls outside D27's pattern, and
    none is known;
  * `internal/provider/codex` (`provider.go`, `session.go`, their tests);
  * `internal/provider/httpagent` (`provider.go`, `session.go`, their tests);
  * `internal/provider/cwd.go`, the `LogStderrTail` comment only;
  * `internal/ws/server.go` (`writeSessionErr`) and its tests;
  * `apps/mobile/lib/data/chat/transcript_reducer.dart`, `apps/mobile/lib/features/chat/chat_bubble.dart`,
    `apps/mobile/lib/data/notifications/notification_coordinator.dart` and their tests;
  * the records: this plan, the MADR, 0207's D50 annotation (D19), and `docs/README.md`'s index.
* **Not in scope:**
  * the two-way capture and replay (the other two plans);
  * the `make live-grok` timeout (D21's own pair);
  * gobble-cli itself (D30: nothing to change there);
  * the debug-level logging of a whole stderr line, which stays as it is;
  * `errorKind` cards (`_LimitNotice`, permission, auth), which keep their rendering (A-7).
* **No provider quota.** Every test uses a test binary or a scratch helper as the agent. The phone
  check uses a scratch fake agent as grok's binary (P6, step 6). No live suite runs.
* **Host runs** need the owner's word: P7 on the Linux test host and the Windows test host, and P6's
  check on the Android emulator. *Given 2026-10-29: "approved for p6 and p7".* That is the word
  for those runs when their phases come. It is not approval to start executing: that waits for
  the owner's "proceed" on this plan.

## Implementation Steps

Each phase works the same way:

1. The new tests are written first. Each is seen failing on a scratch copy of the tree, never in it,
   by a plant script `leftovers/st_pN_plants.py`. That script copies the tree, applies one break per
   test, runs the test, requires it to fail, and removes the copy.
2. Then the change.
3. Then the phase's gates. Go: `make pre-add-check FILES="…"` on each changed `.go` file; `go vet
   ./...` for darwin/arm64, linux/amd64 and windows/amd64 with `CGO_ENABLED=0`; `make staticcheck`,
   whose 56 exceptions are unchanged; `make test`; `make race`. Dart: `make preflight`.
4. Then the execution record. Then the phase's files are staged for the owner, who commits with
   `git commit --no-edit`.
5. Before staging, `leftovers/guard_preview.py` runs over every changed record.

### P1 — `internal/provider/stderrtail` (D23, D24, D27's helper, D28's type)

Files: `internal/provider/stderrtail/stderrtail.go` and `stderrtail_test.go`, both new.

1. **The API.** Each declaration gets a doc comment naming its decision.

   ```go
   const (
       DefaultBytes = 4096                 // D1, D24: the bound on what is held
       DefaultLines = 64                   // D24: a backstop; bytes govern
       Separator    = "\nlast stderr:\n"   // D2, D28
       minSecret    = 8                    // D4
   )

   // Writer keeps the end of what is written to it and forwards every byte to next.
   type Writer struct { /* next io.Writer; bytes, lines int; mu sync.Mutex; buf []byte */ }
   func New(next io.Writer) *Writer          // DefaultBytes, DefaultLines
   func (w *Writer) Write(p []byte) (int, error)
   func (w *Writer) Tail() string            // the kept end, final newline trimmed

   func Secrets(env, names, values []string) []string // D27
   func Scrub(tail string, secrets []string) string   // D4: raw and JSON-escaped, longest first

   type Error struct { Err error; Tail string }        // D28
   func (e *Error) Error() string                     // Err.Error() + Separator + Tail
   func (e *Error) Unwrap() error
   func Wrap(err error, tail string) error            // err unchanged when tail is ""
   func Message(headline, tail string) string         // headline unchanged when tail is ""
   ```

2. **`Writer`'s rules.** They are those of the SDK's `tailWriter` (`agentproc/stderr.go:25-82`),
   reimplemented because codex and httpagent cannot use `agentproc` (D23):
   * Before appending, it keeps only the last `bytes+1` bytes of `p`.
   * Then it drops whole lines from the front while there are more than `lines`.
   * Then, if the buffer is over `bytes`, it cuts to the last `bytes`, and moves on to the next
     line's beginning unless the last line is all that is left.
   * It clones the buffer when its capacity is more than twice `max(len, 4096)`.
   * It forwards `p` to `next` after releasing its lock, and returns `next`'s result (`len(p), nil`
     when `next` is nil).
3. **`Secrets(env, names, values)`.** It returns the values to remove, deduplicated, each of at least
   `minSecret` bytes, longest first:
   * the value of every `NAME=value` in `env` whose name ends in `_API_KEY`, `_TOKEN`, `_SECRET` or
     `_PASSWORD`, compared in upper case;
   * the value of each variable in `env` named in `names`;
   * each of `values`.

   On Windows the names compare in upper case already, so no GOOS branch is needed.
4. **Failing first** (`TestWriter*`, `TestSecrets*`, `TestScrub*`, `TestError*`, table-driven):
   * 70 short lines keep the last 64;
   * one 64 KiB line keeps its last 4096 bytes;
   * 1 MiB of 40-byte lines keeps at most 4096 bytes, starting at a line's beginning;
   * a line split across three `Write`s;
   * `next` sees every byte, in order;
   * eight goroutines writing at once under `-race`, then `len(Tail()) <= 4096`;
   * `Secrets` skips a 7-byte value, matches `GH_TOKEN` and `xai_api_key`, ignores `PATH`, and
     dedupes;
   * `Scrub` replaces the raw form and the `json.Marshal` form of a value with a backslash and a
     quote;
   * `errors.Is(Wrap(e, "t"), e)`; the separator is present once; `Wrap(e, "")` is `e`;
   * `Message("h", "")` is `"h"`.

   Plants: the line cap off, the byte cap off, the line-start rule off, `Scrub`'s escaped form
   off, `Secrets`'s floor off, and `Wrap` dropping `%w`.
5. **Gates**, as above, plus `go test -race -count=50 ./internal/provider/stderrtail/`.

### P2 — `acpagent`: grok, and gobble when it registers (D1 as amended, D2, D3, D4, D5, D27, D30)

Files: `internal/provider/acpagent/acpagent.go`, `agentproc_test.go`, a new `stderrtail_test.go`
in the package, and `session.go` (only if the tail is held on `session`).

1. **`Spec.CredentialEnv []string`** (`acpagent.go:51`). Its doc says: names whose values are
   removed from the tail, beyond D27's pattern. It is empty for grok, because `XAI_API_KEY`
   matches.
2. **The writer** (`acpagent.go:494-501`).
   * `tail := stderrtail.New(&slogWriter{…as now…})`, then `cmd.Stderr = tail`. The SDK's
     `StderrTail` fields stay unset (D23).
   * `secrets := stderrtail.Secrets(envOf(cmd), p.spec.CredentialEnv, p.mcpHeaderValues())`.
   * `envOf` returns `cmd.Env`, or `os.Environ()` when `cmd.Env` is nil, which is what `os/exec`
     passes. Neither `internal/provider/launch` nor `internal/procutil` sets `cmd.Env` (`grep -rn
     '\.Env\b'` over both, outside their tests: no match), so for `acpagent` it is nil, and the agent
     gets the daemon's environment.
   * `mcpHeaderValues` collects every `p.cfg.McpServers[i].Headers` value.
   * Both are held on the session beside `s.wire`, for the watcher.
3. **Start-up failures** (`acpagent.go:543-577`). Each `return` after `killTree()`, which are
   `initializeError`, `selectACPAuthMethod`'s error and both `acp authenticate` errors, becomes
   `stderrtail.Wrap(err, stderrtail.Scrub(tail.Tail(), secrets))`. Each also calls
   `provider.LogStderrTail` with the scrubbed tail, as codex and httpagent do (A-2).
   `killTree`'s `proc.Kill()` returns only after Wait, so the copy into `tail` has finished.
4. **The death message** (`acpagent.go:608-615`). After `proc.Wait()`, `msg =
   stderrtail.Message(msg, scrubbed)`. When the tail is non-empty, `provider.LogStderrTail` runs on
   it before `signalDisconnected(msg)`. The first line is unchanged.
5. **The helper agent** (`agentproc_test.go:31-62`). It gains two modes, read from
   `MCREMOTE_TEST_ACP_STDERR` (text to write) and `MCREMOTE_TEST_ACP_EXIT` (the status):
   * `"die"`: write, then exit, before reading stdin;
   * `"serve-die"`: answer `initialize`, then write and exit 50 ms later.
6. **Failing first** (D5, D29):
   * `TestDeathMessageCarriesTheTail`: `serve-die`, 70 lines and status 3. The message's first
     line is `"testagent process exited: exit status 3"`, followed by `Separator` and the last 64
     lines.
   * `TestDeathOfStatusZeroCarriesTheTail`: status 0.
   * `TestDeathWithNoStderrIsUnchanged`: the message is exactly today's.
   * `TestTailRemovesKnownSecrets`:
     * a `t.Setenv("MCREMOTE_TEST_API_KEY", …)` value, an MCP header value and a
       `Spec.CredentialEnv` value, each written to stderr, come out as `<redacted>`;
     * a 7-byte value is left as it is.
   * `TestDeathTailIsBounded`: a 64 KiB stderr line; the message is at most its first line, plus
     `len(Separator)`, plus 4096 bytes.
   * `TestGoPanicMessageSurvivesTheTail`: stderr shaped like A-5's depth-8 panic. The message
     contains its `panic:` line.
   * `TestStartFailureCarriesTheTail`: `die` with status 2. `spawnAgent`'s error contains the tail,
     and `errors.As` finds a `*stderrtail.Error`.
   * `TestSpawnedAgentDeathIsOneDisconnect` passes unchanged.

   Plants: the tail left off the message, `Scrub` skipped, `CredentialEnv` ignored, the Wait race
   (the tail read before `proc.Wait()`), and the start path not wrapped.
7. **Gates**, plus `go test -race -count=20 -run 'Tail|Death|StartFailure' ./internal/provider/acpagent/`.

### P3 — codex (D25, D26, D27, D28)

Files: `internal/provider/codex/provider.go`, `session.go`, `managed_daemon_p3_test.go` or a new
`stderrtail_test.go` in the package.

1. **The ring goes** (`provider.go:1322-1410`). `lineRing` keeps `log`, `prefix`, `onLine`, `buf`
   and the 4096-byte cut of a line with no newline. It loses `max`, `ring` and `tail()`. Its doc says
   it logs and scans, and that `stderrtail` keeps the tail.
2. **The writer** (`provider.go:601-608`). `cmd.Stderr = stderrtail.New(&lineRing{…})`.
   `engineAttempt.stderr` (`:463`) becomes `*stderrtail.Writer`, and gains
   `secrets = stderrtail.Secrets(cmd.Env, nil, nil)`, built after `cmd.Env` is set at `:569-573`.
3. **Start-up failure** (`provider.go:873-883`). This becomes `stderrtail.Wrap(fmt.Errorf("initialize:
   %w", err), scrubbed)`. `LogStderrTail` stays, with the scrubbed tail.
4. **Death** (`handleUnexpectedEngineExit`, `:1003-1065`).
   * The headline is `fmt.Sprintf("engine lost: codex exited: %v", exitErr)`.
   * `msg := stderrtail.Message(headline, scrubbed)`.
   * `LogStderrTail` runs beside the existing `"engine exited"` warning.
   * `s.engineLost(msg)` replaces `s.engineLost()`. `engineLost` (`session.go:2127-2147`) takes the
     message for its `TypeError`. It has one caller, `provider.go:1042`.
   * `att.stderr` is nil for an attempt that never spawned (the managed-daemon test's), and
     `Message` then gets `""`.
5. **Failing first:**
   * `TestEngineLossCarriesTheTail`: `handleUnexpectedEngineExit` on an `engineAttempt` whose
     `stderr` holds lines, with two live sessions. Each session's `TypeError` starts
     `"engine lost: codex exited: "` and carries the tail.
   * `TestEngineLossScrubsTheEnvironment`: an `_API_KEY` value in the attempt's env is removed.
   * `TestCodexInitFailureCarriesTheTail`: through the app-server helper (`provider_test.go:18-24`),
     with a new mode that writes to stderr and exits before answering. The error is a
     `*stderrtail.Error`.
   * The managed-daemon test at `managed_daemon_p3_test.go:158` passes unchanged.

   Plants: the old `"engine lost"` text kept, the tail left out, `Scrub` skipped.
6. **Gates.**

### P4 — httpagent: kilo and opencode (D25, D26, D27, D28)

Files: `internal/provider/httpagent/provider.go`, `session.go`, `provider_test.go`,
`session_test.go`, `expiry_test.go`.

1. **The ring goes** (`provider.go:1206-1261`), as in P3. `TestLineRingKeepsTail`
   (`session_test.go:450-457`) is removed, and its case is in P1's table.
2. **The writer** (`provider.go:543-545`). `cmd.Stderr = stderrtail.New(&lineRing{…})`, and
   `secrets := stderrtail.Secrets(cmd.Env, nil, nil)` after `:534-537`.
3. **Start-up failures** (`provider.go:603-650`). The health timeout (`:609-615`) and the early exit
   (`:641-647`) each become `stderrtail.Wrap(fmt.Errorf("%s server did not become healthy in %s",
   …), scrubbed)` and `stderrtail.Wrap(fmt.Errorf("%s server exited during startup", …), scrubbed)`.
   `LogStderrTail` stays. `TestStartServerBailsWhenEngineExitsImmediately` passes unchanged.
4. **Death** (the monitor, `provider.go:694-721`).
   * `msg := stderrtail.Message(fmt.Sprintf("%s server exited: %v", p.cfg.Bin, err), scrubbed)`.
   * `LogStderrTail` runs beside `"engine exited"`.
   * `s.serverDied(msg)` (`session.go:1360-1364`) takes the message. `expiry_test.go:308` passes
     one.
5. **Failing first:**
   * `TestServerDeathCarriesTheTail`: a new helper, `TestHelperProcessServesHealthThenDies`, beside
     `TestHelperProcessServesHealth` (`supervise_wiring_test.go:77-…`) and guarded the same way (an
     arming variable plus `procutil.EnvEngineID`, MADR 0147 D10).
     * It serves the health path until the test creates a trigger file, then writes lines to
       stderr and exits 3. It starts no grandchild, so the existing supervision test is untouched.
     * Each session's `TypeError` starts `"<bin> server exited: exit status 3"` and carries the
       tail.
   * `TestEarlyExitCarriesAScrubbedTail`: `TestHelperProcessExitsNonZero`'s helper
     (`provider_test.go:61-78`) writes an `OPENROUTER_API_KEY` value set by `t.Setenv` to stderr. The
     start error carries the tail with `<redacted>`, and `errors.As` finds a `*stderrtail.Error`.

   Plants: as P3.
6. **Gates.** Both dialects' packages are in `make test`: `./internal/provider/kilo/...` and
   `./internal/provider/opencode/...`, unchanged.

### P5 — `writeSessionErr` keeps the tail (D3 as amended, D28)

Files: `internal/ws/server.go` and `internal/ws/error_codes_test.go`, which with
`permission_code_test.go` and `observability_test.go` already calls `writeSessionErr`.

1. **The change** (`server.go:2165-2170`). `full := err.Error()`. When `errors.As(err, &te)` finds
   a `*stderrtail.Error` and `full` ends with `stderrtail.Separator + te.Tail`:
   * the text before that suffix is clipped to 300 bytes, rune-safe, as now;
   * then the suffix is appended whole.

   Otherwise, today's clip applies. That keeps a wrapper that adds a prefix (`"create: %w"`)
   working. A wrapper that adds a suffix falls back to today's behaviour, and the test pins that.
2. **Failing first:**
   * a 500-byte headline wrapping a 2 KiB tail: the message is 300 bytes, `…`, `Separator` and the
     whole tail;
   * a `fmt.Errorf("create: %w", te)`: the same;
   * a plain 500-byte error: clipped as today;
   * a suffix-wrapped `stderrtail.Error`: clipped as today;
   * `permissionGuidance` still follows a permission error.

   Plants: the tail clipped, `errors.As` replaced with a string search.
3. **Gates.**

### P6 — the phone (D20, D31)

Files: `apps/mobile/lib/data/chat/transcript_reducer.dart`, `apps/mobile/lib/features/chat/chat_bubble.dart`,
`apps/mobile/lib/data/notifications/notification_coordinator.dart`,
`apps/mobile/test/transcript_reducer_test.dart`, `apps/mobile/test/chat_render_test.dart` (its
`ChatItem.system` cases are at `:632-756`), and `apps/mobile/test/notifications_test.dart`. Its fake
service records each `showError` detail in `errors` (`:109-113`), and its error cases assert on that
list (`:735`, `:911`, `:917`).

1. **The reducer's bound** (`transcript_reducer.dart:253-264`).
   * A new `@visibleForTesting const int kErrorTextMax = 8192;` replaces the literal `300` at
     `:259`. Its comment cites D20 and `0042-MADR-android-app-remediation.md` D8, which set 300.
   * `_clip`'s whitespace rules (`:1125-1136`) are unchanged: line breaks are kept, and runs of
     spaces collapse.
2. **The collapsible item** (`chat_bubble.dart:540-565`).
   * When `isError` and the text has more than 3 lines, the plain branch returns a new private
     `_CollapsibleError` instead of the centred `Text`.
   * **Collapsed:** the error icon; the first 3 lines in `bodySmall`, in `scheme.error` colour, with
     `maxLines: 3` and an ellipsis; and a `TextButton` "Show details", with a semantic label.
   * **Expanded:** the whole text in a `SelectableText` with the `monoDetail` style already used at
     `:908`, in a container of `maxWidth: 420` like `_LimitNotice`'s, and a "Hide details" button.
   * Its state is local, and it starts collapsed.
   * An error of 3 lines or fewer renders exactly as today.
3. **The notification** (`notification_coordinator.dart:233-243`). `detail:` becomes the error's
   first line, clipped to 300 characters by a small helper, `firstLineOf`, which is unit-tested.
4. **Failing first** (Flutter, under `make preflight`). They assert on structure and on found text,
   not on widths (the test font is fixed-width):
   * the surrogate test (`transcript_reducer_test.dart:1202-1233`) is rebuilt on `kErrorTextMax`:
     `'x' * (kErrorTextMax - 1)`;
   * a 5000-character error survives whole, and a 9000-character one is clipped to `kErrorTextMax`
     plus `…`;
   * a 2-line error has no "Show details";
   * a 12-line error shows "Show details" and not line 12;
   * a tap shows line 12 in a `SelectableText`, and "Hide details";
   * a second tap hides it;
   * a `quota` item still renders `_LimitNotice`;
   * `firstLineOf` returns line one, and clips at 300;
   * in `notifications_test.dart`, a 12-line `error` event records only its first line in
     `errors`, while the three existing single-line cases (`:735`, `:911`, `:917`) pass unchanged.

   Plants: the old 300 bound, the control never shown, the expansion not toggling, and the
   notification given the whole text.
5. **Gates:** `dart format` on each changed `.dart` file, then `make preflight` (`flutter analyze`,
   `flutter test`, `dart format --set-exit-if-changed`).
6. **The emulator check**, with the owner's word. No quota, and no live service:
   * A scratch fake ACP agent outside the tree answers `initialize` and `session/new`, writes a
     panic-shaped stderr, and exits 2.
   * A scratch daemon runs in the foreground, with a scratch config and data dir whose
     `providers.grok.bin` is that fake.
   * The AVD `mcremote_test` is paired by pasting the URI. A session is started, and the agent
     dies.
   * Host-side screenshots record the collapsed item, the expanded item and the notification.
   * The scratch daemon and directories are removed, and their absence is checked.

### P7 — the test hosts, and the records (D19, and Confirmation)

1. **Host runs**, each in a scratch copy removed afterwards (`leftovers/st_hosts.py`):
   * the Linux test host: `go test -race -count=1 ./...`;
   * the Windows test host: `make ci-windows`, after the `make -n` check. The Windows path matters
     here: `launch.Command`'s batch-shim wrapping puts `cmd.exe` between Magic and an npm-installed
     agent, and its stderr must still reach the tail. The helper agents are `.exe` test binaries, so
     the shim path itself is exercised by the existing launch tests, not these.
2. **Records:**
   * this plan's execution record, with every gate's output quoted;
   * the MADR's D1 to D5, D20 and D22 to D31 marked carried out;
   * 0207's D50 annotated (D19): "carried by `0216-MADR-…`, one MADR with three PLANs, as the
     owner asked on 2026-10-29", its text kept;
   * `docs/README.md` regenerated with `scripts/check_records.py --write-index`;
   * `make check-records`, `make markdownlint-docs` and the guard's redact preview.
3. **Before asking to push:** the pre-push disclosure guard over the outgoing commits
   (`leftovers/guard_range.py`).

## Verification

* `go test ./internal/provider/stderrtail/ -race -count=50`: ok.
* Every test P1 to P6 names passes. Each was seen failing first on a scratch copy, and the record
  says how.
* `TestSpawnedAgentDeathIsOneDisconnect`, `TestStartServerBailsWhenEngineExitsImmediately`, and the
  managed-daemon test at `managed_daemon_p3_test.go:158` pass unchanged.
* `grep -n "ring \[\]string\|func (w \*lineRing) tail" internal/provider/codex/provider.go internal/provider/httpagent/provider.go`:
  no match (D25).
* `make test`, `make race`, `make staticcheck` (56), `make preflight`, `make check-records`,
  `make markdownlint-docs`.
* The Linux test host and `make ci-windows` on the Windows test host.
* The emulator screenshots (P6, step 6).

## Rollout and Rollback

* **Rollout.** With the next tag. The phone and the daemon each work with the other's old version:
  * a new phone shows an old daemon's short errors as today;
  * an old phone shows a new daemon's errors clipped at 300 characters, which is today's behaviour.
* **Rollback.** Revert the phase's commit. Each phase stands alone, except that P2 to P5 import P1's
  package, so P1 is reverted last. No stored data changes form: an error item holds a longer
  string, and an old phone clips it.

## Execution record

~~Nothing has run.~~

* 2026-10-29: questions 8 to 11 answered, and D31 accepted.
* 2026-10-29: the owner's word for the P7 host runs and the P6 emulator check: "approved for p6 and
  p7".
* ~~The plan waits for the owner's "proceed" before P1.~~ *Approved 2026-10-29:* "proceed".

### P1, 2026-10-29: `internal/provider/stderrtail`

**What was built** (`stderrtail.go`, `stderrtail_test.go`, both new): the API of step 1, as
written.

* `Writer` follows step 2's rules, with one addition: when the last line alone is longer than the
  cap, the tail starts at a rune's beginning, so it is always valid UTF-8. The SDK's `tailWriter`
  can start mid-rune. The addition has its own test and plant.
* `Tail()` trims a final `\r\n` as well as `\n`, since a Windows engine may end its lines so.
* `Secrets` skips an entry with an empty name, such as Windows' `=C:=C:\…`.
* `Scrub`'s JSON form is written with HTML escaping off, as engines write it.

**The tests**, all passing (`go test -race -count=1 -v`):

| Test | What it holds |
| :--- | :--- |
| `TestWriterCaps` | 70 short lines keep the last 64; 1 MiB of 40-byte lines, written in 4096-byte chunks, keeps the last 64; 100 lines of 100 bytes are cut by bytes to the last 40, from a line's beginning; a short tail written a byte at a time is kept whole. Each tail is at most 4096 bytes |
| `TestWriterKeepsTheEndOfALongLine` | a 64 KiB line of two-byte runes keeps its last 4095 or 4096 bytes, ending the line, as valid UTF-8 |
| `TestWriterJoinsALineAcrossWrites` | `"pan"`, `"ic: boo"`, `"m\nnext"` give `"panic: boom\nnext"` |
| `TestWriterForwardsEveryByte` | the writer behind it gets all 25,000 bytes, in order, in 333-byte writes |
| `TestWriterReturnsNextsResult` | the wrapped writer's error is returned; with none, `len(p), nil` |
| `TestWriterIsSafeForConcurrentUse` | 8 goroutines × 200 writes and reads under `-race`; the tail ends within the cap |
| `TestSecrets` | credential-shaped names in any case, a named extra, a given value; the 7-byte value, `PATH`, a nameless and an `=`-less entry skipped; a duplicate once; longest first |
| `TestScrub` | a value with a quote and a backslash is replaced raw and JSON-escaped; a 7-byte value is left |
| `TestErrorKeepsItsChain` | `errors.Is` and `errors.As` see through `fmt.Errorf("start grok: %w", Wrap(…))`; the text has the separator once; `Wrap` of no tail or a nil error returns the error |
| `TestMessage` | no tail leaves the headline; a tail follows `Separator` |

**Seen failing** (`leftovers/st_p1_plants.py`). Each plant was applied to a fresh scratch module,
holding only `go.mod`, `go.sum` and the package, by an exact replace asserted to match once. Each
copy was removed and its absence checked. All ten failed the test named, with:

| Plant | Failure |
| :--- | :--- |
| line cap off | `stderrtail_test.go:59: tail starts "line 000000 …", want "line 000006 "` |
| byte cap off | `:59: tail starts "e 000059 xxx…", want "line 000060 "` |
| line-start rule off | `:59: tail starts " 000059 xxx…", want "line 000060 "` |
| rune rule off | `:82: tail does not end the line, or is not valid UTF-8: ..."\xa9ééEND"` |
| no forwarding | `:111: next got 0 bytes, want all 25000 in order` |
| `Scrub`'s JSON form off | `:182: a form of the secret is left: "raw <redacted>\njson {\"key\":\"k3y\\\"with\\\\slash\"}…"` |
| `Secrets`' floor off | `:167: Secrets = [… "1234567" "tiny"]` |
| `Scrub`'s floor off | `:185: want 2 replacements, raw and JSON: "…short <redacted>"` |
| `Wrap` drops `%w` | `:198: errors.Is lost the wrapped error` |
| `Message` ignores the tail | `:218: Message = "h"` |

**Gates** (`leftovers/st_gates.py p1 …`; logs `leftovers/st_gates/p1/`):

| Gate | Result |
| :--- | :--- |
| `gofmt -l` on both files | no output |
| `make pre-add-check FILES=…` | "go-precheck: 2 file(s) clean (gofmt, golint, govulncheck)." |
| `make staticcheck` | exit 0; "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name" |
| `go build ./...`; `go vet ./...` for darwin/arm64, linux/amd64, windows/amd64, `CGO_ENABLED=0` | exit 0, each |
| `go test -race -count=50 ./internal/provider/stderrtail/` | ok, 5.680 s |
| `make test`, `make race` | exit 0; 45 packages ok, the 44 before and `stderrtail` |
| `make check-records`, `make markdownlint-docs` | exit 0; "0 issues" |

`scripts/check_records.py --write-index` added this plan's and the MADR's rows to `docs/README.md`,
and nothing else.

**Files for P1's commit:** `internal/provider/stderrtail/stderrtail.go`,
`internal/provider/stderrtail/stderrtail_test.go`, the MADR, this plan, `docs/README.md`. No
caller uses the package yet; P2 is the first.
