---
status: proposed
date: 2026-10-30
decision-makers: Project Owner
consulted: "go-tui-lib v2.6.2 (README.md; go doc of safetext, termcap and termsvc, from the module cache); gobble-cli's go.mod; this repository at cd2245ea: internal/auth/store.go, internal/ws/server.go, internal/cli/pair.go, internal/picker, internal/command, check_libs.json; apps/mobile/lib/features/connect/connect_screen.dart; scratch probes leftovers/tuilib_probe.py and leftovers/tuilib_clean_probe.py"
informed: anyone who prints text Magic did not write to the operator's terminal
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Clean device names and copy pair URIs with go-tui-lib

## Context and Problem Statement

On 2026-10-30 the owner asked: "go-tui-lib also updated to a new release. assess this. we might be
ready to adopt functionality now". The assessment found that Magic has no terminal UI to adopt
most of the library into. The owner chose its two small candidates, "safetext + termsvc
(Recommended)": clean the device names that phones send before they reach the terminal, and offer
a copy of the pair URI to the clipboard.

### What go-tui-lib is [observed 2026-10-30, `git ls-remote`, its README at `v2.6.2`, and `go doc` from the module cache]

**F1. `v2.6.2` is current.** It was tagged 2026-10-29 at `696f4af`, under the module path
`github.com/maccavelli/go-tui-lib/v2`. The repository has no published security advisory (the
GitHub API answers an empty list). gobble-cli already requires it (`go.mod:19`).

**F2. Most of the library is for interactive programs.** It is a TUI layer that stacks on a
program's own CLI. `workspace`, `layout`, `palette`, `launch` and `keymap` are for a full-screen TUI.
`ask`, `pick`, `lineedit`, `stream` and `transcript` (v2.3.0 to v2.6.2) are line-mode components
for a chat CLI such as gobble's.

**F3. Three packages are for any CLI, and link nothing from Charm but `x/ansi`** (its README, from
v1.13.0 and v2.0.0):

* `safetext` cleans untrusted text for a terminal. `Clean(s, Rules)` reads escape sequences as a
  terminal reads them, removes invisible and bidirectional controls, and turns invalid UTF-8 into
  U+FFFD. The `Line()` preset is `Rules{Flatten: true}`: one line, line breaks and tabs as one
  space, bidirectional controls removed.
* `termcap.EnvCaps(env, goos)` gives what the environment says about the terminal, with no probe.
  It sets `Remote` from `SSH_TTY` or `SSH_CONNECTION`.
* `termsvc.CopySequence(caps, text)` returns the bytes that copy `text` to the clipboard: OSC 52,
  and inside tmux the same again wrapped for passthrough. It refuses more than `MaxCopyBytes`
  (100 000). The terminal does not acknowledge OSC 52, so a copy is written without confirmation.

### What Magic does [read at `cd2245ea`]

**F4. Magic has no interactive terminal UI.** Its CLI prints lines: `pair`, `doctor`,
`setup-service`, `update`, `receipts`, `engines`. Nothing prompts or redraws.
`internal/picker` and `internal/command` look like `pick` and `command`, but each is a catalogue
the daemon sends. The phone draws it (`picker.go:1-4`, `command.go:1-13`).

**F5. A phone names itself, and the name reaches the operator's terminal.**

* A phone that holds a valid pair code sends a name in its pair request. It replaces the code's
  name, and is cut to `maxNameLen` bytes (`internal/ws/server.go:1317-1323`).
* The store passes every name through `sanitizeDeviceName` (`internal/auth/store.go:26-67`).
  It drops each rune that `unicode.IsControl` reports, and invalid UTF-8. It also collapses white
  space, cuts the name to 64 bytes on a rune boundary, and deflects a name shaped like a UUID.
* The CLI prints names at `pair.go:80` (`pair create`), `:143` (`pair code`, and bare `pair`,
  which runs the same function, `:172-177`), `:246` (`pair list`), `:267` (`pair revoke`) and
  `:305` (`pair prune`).
* Names stored before the sanitiser existed are printed as stored (`store.go:487-488`).

**F6. The sanitiser misses what a terminal still acts on.** A scratch copy of `cd2245ea` with
`go-tui-lib/v2 v2.6.2` compared the two on hostile names (`leftovers/tuilib_clean_probe.py`):

| Name | `sanitizeDeviceName` | `safetext.Clean(…, Line())` |
| :--- | :--- | :--- |
| `"phone‮enohp"` (right-to-left override) | `"phone‮enohp"` | `"phoneenohp"` |
| `"ab⁦cd⁩ef"` (isolates) | unchanged | `"abcdef"` |
| `"pixel​‍name"` (zero-width space, zero-width joiner) | unchanged | `"pixel‍name"` |
| `"red\x1b[31mtext\x1b[0m"` | `"red[31mtext[0m"` | `"redtext"` |
| `"c1\u009b31mtext"` (C1 introducer) | `"c131mtext"` | `"c1text"` |
| `"plain name"` | unchanged | unchanged |

`unicode.IsControl` is only Cc. So the bidi overrides and isolates (Cf) pass, and can make a name
in `pair list` read as another, or reorder the columns after it. An escape sequence loses its ESC
but leaves its parameters as visible residue. `safetext` keeps the zero-width joiner, which emoji
sequences need.

**F7. The pair URI is typed or scanned today.**

* `pair create` prints a URI that carries the device's long-lived token
  (`pair.go:86`, `pairPayload`'s `Token`, `:374-381`). `pair code`, and bare `pair`, print one that
  carries a one-time code, valid 5 minutes by default and at most `auth.MaxPairCodeTTL`
  (`:110-154`).
* `printPairQR` prints a QR code when stdout is a terminal or `--qr` is given (`:340-370`).
* The phone pastes a URI with "Paste URI / code / token" (`connect_screen.dart:1503-1505`). An
  Android emulator pairs that way, and a desktop's clipboard can reach a phone through the
  platform's own clipboard sharing.
* Over SSH, the operator selects the URI by hand. A wrapped line or a tmux pane can break that
  selection.

**F8. What adding the library costs** (`leftovers/tuilib_probe.py`, a scratch copy of `cd2245ea`
with one probe package using `safetext.Clean`, `termcap.EnvCaps` and `termsvc.CopySequence`):

* `go get github.com/maccavelli/go-tui-lib/v2@v2.6.2` and `go mod tidy` add one direct
  requirement and five indirect ones: `charmbracelet/x/ansi v0.11.8`, `clipperhouse/displaywidth
  v0.11.0`, `clipperhouse/uax29/v2 v2.7.0`, `lucasb-eyer/go-colorful v1.4.1` and
  `mattn/go-runewidth v0.0.27`. No other line of `go.mod` changes.
* The probe links those modules' packages, and `go-tui-lib/v2`'s `safetext`, `termcap`,
  `termsvc`, `glyph`, `internal/enum` and `internal/sanitize`. Nothing from Bubble Tea, Lip
  Gloss or Bubbles.
* `go build ./...` and `go vet` exit 0. `govulncheck` over the probe: "No vulnerabilities found."

**F9. `make check-libs` checks the owner's libraries that `check_libs.json` names:**
`acp-go-sdk2` and `go-selfupdate-lib`. A new library of the owner's would not be checked unless it
is named there.

**F10. Nothing standing would hold D1's limit** (issue #30, filed 2026-10-30 while this pair was
proposed; each point re-read on 2026-10-30, at `896834a5`).

* `check_libs.json`'s `packages` tell `check_libs.py` where to build, vet and test a new tag. They
  refuse no import anywhere else.
* `internal/modguard_test.go` guards the `acp-go-sdk2` pin only: an exact tag, no `replace`, and no
  import of the Coder module. Its import check is a text search of `.go` files
  (`importProblems`, `:111-137`), not a dependency walk.
* There is no `.golangci.yml`, so no depguard.
* This pair's only boundary check was a one-time `go list -deps ./cmd/...`. A later import of
  `palette`, `workspace`, `launch` or a `…tea` package would link Bubble Tea, Lip Gloss or Bubbles
  into the daemon, and every gate would stay green.
* gobble-cli holds the same limit with `internal/archtest` rule 5: `go list -e -deps -test -json
  ./...` (`check.go:60`), go-tui-lib's v1 module path refused, and each go-tui-lib package's closure
  checked for Charm (`check.go:291-342`). go-tui-lib checks itself with
  `internal/conformance`'s `TestCoresAreCharmFree`.
* The probe of F8 linked `github.com/charmbracelet/x/ansi` and `x/ansi/parser`. "Nothing from Charm
  but `x/ansi`" is short by that one package.

## Decision Drivers

* Text Magic did not write must not steer the operator's terminal. A device name is chosen by
  whoever holds a pair code.
* One implementation of terminal-safe text across the owner's programs, rather than a second one
  in Magic. gobble-cli already uses go-tui-lib.
* No full-screen TUI stack in a daemon's binary.
* A pair URI that carries a long-lived token is a credential. It should not leave the terminal by
  default.
* A limit that no gate holds is a limit in prose only (F10).

## Considered Options

* A — `safetext` at the store and at every print of a name, and `pair code --copy` through `termsvc`
* B — The standard library only: widen `sanitizeDeviceName` to drop Cf and parse escape sequences
  by hand, and no copy
* C — `safetext` only, with no copy
* D — Nothing

## Decision Outcome

Chosen option (proposed): "A", because it closes F6's gap where names are stored and where they
are printed, with the implementation the owner's other programs use, for F8's small cost, and
F7's copy is opt-in on the URI that carries no long-lived token.

### The decisions (proposed)

* **D1 — Require `go-tui-lib/v2 v2.6.2`, for `safetext`, `termcap` and `termsvc` only.** Magic
  imports no other package of it. `check_libs.json` names it, with the packages that import it, so
  `make check-libs` checks each new tag (F9). D6 holds the limit.
* **D2 — Clean a name where it is stored.** `sanitizeDeviceName` takes its character pass from
  `safetext.Clean(name, safetext.Line())`, in place of its `unicode.IsControl` loop. Its other
  duties stay as they are: collapsing white space, the 64-byte cut on a rune boundary, the UUID
  deflection and the "device" fallback. A new name is then clean wherever it goes, the phone and
  `pair revoke <name>` included.
* **D3 — Clean a name where it is printed.** Each of F5's five prints passes the name through one
  helper in `internal/cli` that calls `safetext.Clean(…, safetext.Line())`. That covers names
  stored before D2, and any later path that prints a name. A clean name passes through unchanged
  (`Clean` returns its input and allocates nothing).
* **D4 — `pair code --copy`, and the same flag on bare `pair`, which runs `pair code`.**
  * With `--copy`, and stdout a terminal, `pair code` writes the URI's
    `termsvc.CopySequence(termcap.EnvCaps(…), uri)` to stdout after the URI line. Then it prints
    one line: the copy was sent to the terminal, which does not confirm it.
  * With stdout not a terminal, `--copy` is an error and nothing is minted. A sequence in a pipe or
    a file would be noise there, and would put the code in a log.
  * `pair create` gets no `--copy`, because its URI carries the device's long-lived token (Q1).
  * The copy is never written without the flag.
* **D5 — What is not adopted.**
  * `termsvc.Link`, because an OSC 8 link of the pair URI opens on the desktop, not on the phone.
  * `glyph`, for `doctor`'s lines, which is style and no fix.
  * F2's interactive packages, until a record proposes a terminal client for Magic.
* **D6 — A standing gate holds D1 (F10, issue #30).** `internal/modguard_test.go`, beside its
  `acp-go-sdk2` rules, walks the module with `go list -deps -test -json ./...`, run from the module
  root, and fails on:
  * an import of a `github.com/maccavelli/go-tui-lib/v2` package other than `safetext`, `termcap`
    and `termsvc`, from any package of this module;
  * an import of those three from outside `internal/auth/...` and `internal/cli/...`, the packages
    `check_libs.json` names;
  * an import of `github.com/maccavelli/go-tui-lib`, the v1 module path, anywhere, as gobble-cli's
    rule does;
  * any `charm.land/…` package, or any `github.com/charmbracelet/…` package but `x/ansi` and
    `x/ansi/parser`, anywhere in the closure, test packages included. This catches an allowed
    package that one day links a new Charm module, which an import allowlist cannot.

  The test runs wherever `go test` runs: CI on three systems, both test hosts, `make race`.
  `-test` brings each package's tests into the walk, as gobble-cli's does. The compiler already
  refuses another module's `internal/…`, so the allowlist names none.

  Considered for D6 and not chosen, in issue #30's terms: depguard through golangci-lint, which binds
  direct imports only and adds a tool to every machine; an import allowlist in go-shared's
  `check_libs.py`, which runs on new tags, not on every push, and is shared with gobble-cli; and
  review alone, which is F10's gap.

### Consequences

* Good, because a phone can no longer name itself so that `pair list` misleads the operator, and
  an escape sequence leaves no residue (F6).
* Good, because the names Magic shows are cleaned by the same code as gobble's terminal text.
* Good, because an operator over SSH or in tmux can paste a pair code's URI into a phone or an
  emulator without selecting it by hand.
* Neutral, because `go.mod` gains one direct and five indirect requirements, and `make check-libs`
  one more library (F8, F9).
* Bad, because OSC 52 is unconfirmed: a terminal that refuses it, or a tmux without
  `set-clipboard`, drops the copy silently. The line D4 prints says so.
* Bad, because a pair code's URI in a desktop clipboard can be read by anything with clipboard
  access, and may sync to other devices, for the code's lifetime. The code is one-time and
  expires, and the flag is opt-in.
* Bad, because a name stored before D2 keeps its hidden characters on disk and in what the daemon
  sends the phone. D3 cleans only what the CLI prints. `pair revoke <id>` still reaches such a
  device.
* Good, because a change that pulls a TUI stack into the daemon fails `go test` on every system,
  rather than passing every gate (D6).
* Neutral, because the test runs the `go` tool once per `go test` of `internal`, which needs the
  module cache the test run already has.

### Confirmation

* Every new test is seen failing first on a scratch copy: F6's names through `sanitizeDeviceName`,
  and through each of F5's prints, against `cd2245ea`'s code.
* A test holds that `pair code --copy` with stdout not a terminal fails, and mints no code.
* ~~`go list -deps ./cmd/...` shows no `charm.land` package and no `github.com/charmbracelet` package
  other than `x/ansi`.~~ *Amended 2026-10-30 (F10, issue #30):* `go list -deps ./cmd/...` shows no
  `charm.land` package and no `github.com/charmbracelet` package but `x/ansi` and `x/ansi/parser`.
* D6's test passes in the tree, and was seen failing first on a scratch copy for each of: a
  planted `ask` import outside `internal/auth` and `internal/cli`, a planted `palette` import inside
  them, a planted Bubble Tea import, and a planted v1-path import.
* `make check-libs` checks `go-tui-lib/v2`, and passes.
* The gates of the phase: `make pre-add-check`, vet on three GOOS, `make staticcheck`,
  `make vulncheck`, `make race`, `make preflight`, and the Linux and Windows test hosts.

## Pros and Cons of the Options

### A — `safetext` at the store and at every print, and `pair code --copy` (proposed)

* Good, because it closes F6 for new names everywhere and for old ones in the CLI.
* Good, because the escape-sequence and bidi rules are maintained once, in the library.
* Good, because the copy helps where hand selection fails, and only when asked for.
* Bad, because it adds a dependency, and an unconfirmed side channel when `--copy` is used.

### B — The standard library only

* Good, because it adds no dependency.
* Neutral, because dropping Cf is a small change to `sanitizeDeviceName`.
* Bad, because parsing escape sequences as a terminal does (CSI, OSC, DCS, C1 introducers, string
  limits) is the part that is easy to get wrong, and F6's residue shows the current loop does not.
* Bad, because it is a second implementation beside gobble's.

### C — `safetext` only

* Good, because it closes F6 with no new terminal behaviour.
* Bad, because the SSH and tmux pairing friction of F7 stays.

### D — Nothing

* Good, because nothing changes.
* Bad, because F6's gap stays open to anyone who holds a pair code.

## More Information

### Questions for the owner

* **Q1. `pair create --copy` too?** The long-token URI is the credential itself. Proposed: no.
  Only `pair code`, and bare `pair`, copy.
* **Q2. D2 and D3, or D3 only?** D2 changes what is stored for a new device, and so what the phone
  shows and what `pair revoke <name>` matches. D3 alone changes only what the CLI prints.
  Proposed: both.
* **Q3. A one-time clean of stored names?** Rewriting legacy names in `devices.json` would end the
  last Bad consequence, but it changes stored records. Proposed: no. `revoke <id>` and D3 suffice.

*Answered by the owner, 2026-10-30:*

* **Q1.** "pair code only (Recommended)". D4 stands: `pair code` and bare `pair` take `--copy`, and
  `pair create` does not. Declined: `pair create --copy` with a warning, and no copy at all.
* **Q2.** "Store and print (Recommended)". D2 and D3 both stand. Declined: printing only, and
  storing only.
* **Q3.** "Leave them (Recommended)". No stored record is rewritten. Declined: a clean on load,
  and a `doctor` report of such names.
* **Q4** (the PLAN's host runs). "Yes, both hosts (Recommended)".

The decisions stay proposed until the owner accepts this record.

### Evidence index

* go-tui-lib: `git ls-remote --tags`; `README.md` at `v2.6.2`; `go doc` of `safetext`
  (`Rules`, `Clean`, `Line`), `termcap` (`EnvCaps`, `Env`, `Caps.Remote`) and `termsvc`
  (`CopySequence`, `CopyPlan`, `MaxCopyBytes`), from gobble-cli's module cache; the security
  advisories API.
* `leftovers/tuilib_probe.py` and `tuilib_probe.log`: F8.
* `leftovers/tuilib_clean_probe.py`: F6, run in the same scratch copy.
* This repository at `cd2245ea`: `internal/auth/store.go:24-67`, `:482-499`;
  `internal/ws/server.go:1304-1325`; `internal/cli/pair.go:60-162`, `:220-310`, `:340-370`;
  `internal/picker/picker.go:1-12`; `internal/command/command.go:1-25`; `check_libs.json`;
  `apps/mobile/lib/features/connect/connect_screen.dart:872-906`, `:1503-1505`.
* F10: issue #30 on the repository; `internal/modguard_test.go:14-137`; `git ls-files` finding no
  `.golangci.*`; gobble-cli's `internal/archtest/check.go:42-44`, `:60`, `:291-342`; go-tui-lib's
  `internal/conformance/charmfree_test.go:169`.

### Related records

* [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md):
  the pins this pair adds one to. Its phase 3 lands first.
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  how the owner's other libraries are pinned and checked.
