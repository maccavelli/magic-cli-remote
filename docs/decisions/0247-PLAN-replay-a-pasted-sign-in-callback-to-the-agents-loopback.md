---
status: proposed
date: 2026-11-10
associated-madr: "0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement replaying a pasted sign-in callback to the agent's loopback

Associated MADR: [0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md)

## Goal

When this plan is complete, a browser sign-in whose agent listens on a loopback redirect completes from the phone: by
the agent's own paste channel where U5 or U6 finds one, otherwise by the daemon's one-shot replay under the MADR's D2
checks. codex's browser login is offered (D4). grok's browser stays `host_only` (D1). The MADR's Confirmation 1 to 7 is
met.

This record is `proposed`. Nothing below starts before the owner accepts the MADR and approves this PLAN, whole or by
phase. P1's probes each ask first.

## Scope

**Base.** [0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
P3 committed: the sessions, the steps, the mappers and the quarantined scraper exist. `HEAD` is `912b9aa1` (this pair
was first drafted at `995d442b`; every file it cites is identical at both). P2 to P5 here may land in that plan's
release or after it; the phone already renders `open_url` with `accepts` and `paste` (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D9), so no app
change is needed.

**In scope:** `internal/providerauth/replay/` (new) and its tests; `internal/providerauth/session.go` (D3's port
admission); `internal/provider/kilo/signin.go`, `internal/provider/httpagent/deviceauth.go`;
`internal/provider/opencode/signin.go`, `acp_auth.go`; `internal/provider/codex/signin.go`, a new
`codex/browser_signin.go`, `codex/adapter.go` (the pending home's environment); `internal/providerauth/scrape/` (codex's
authorize URL); `docs/guides/protocol-v1.md` (the methods it adds); this pair; P0's notes.

**Out of scope:** the reverse tunnel (MADR D5); grok's browser (D1); any agent's own code; the phone.

## Conventions

`0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`'s C1 to C8 apply unchanged: start state, line numbers, seen failing on a scratch clone, gates on the commit,
the agent stages and the owner commits with `git commit --no-edit`, deviations stop the phase, no identifiers, and no
sign-in or quota outside P1 and P6.

## Implementation Steps

### P0 — the records (on acceptance)

1. The MADR `accepted`, dated; this PLAN `in-progress`.
2. Dated notes, quoting what they amend: `0074-MADR-remote-provider-auth-from-phone.md` D7 and W3;
   `0086-MADR-phone-provider-auth-completion.md` D6's SuperGrok row.
3. `python3 scripts/check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview.
   **Stage** the records and `docs/README.md` only.

### P1 — the probes (MADR "Not verified")

Each probe asks the owner first, names its scratch home, and records its commands, exit statuses and what it found,
with any URL's code and `state` replaced by `<CODE>` and `<STATE>`. Scripts live in the session scratchpad.

1. **U5, offline first.** Read kilo's OpenAPI document from a local `kilo serve` with a scratch `XDG_DATA_HOME` (`GET
   /doc` or the spike's `openapi-paths.txt` for the installed version) for `POST /provider/{providerID}/oauth/callback`'s
   body. If it names a `code` or a URL: one throwaway OpenAI browser sign-in through that engine, the redirect URL
   pasted to the route. *Owner-run; asks first; scratch home; quota: none; one grant on the owner's ChatGPT account,
   revoked through kilo's logout where it offers one, else left to expire; the record says which. Deleting the
   scratch home removes the local copy only, never the grant at OpenAI.*
2. **U6, offline first.** Read opencode's control-plane API (its OpenAPI document from a scratch `opencode serve`) for
   how an attempt of `mode: "code"` takes its code. If a route exists: one throwaway sign-in of an integration whose
   attempt is `mode: "code"`. *Owner-run; asks first; scratch home; quota: none.*
3. **U13.** `codex login` with a scratch `CODEX_HOME` and `PATH` stubs for `open` and `xdg-open` that record their
   argument; the owner opens the printed URL on another device and pastes the landed URL into
   `<scratchpad>/replay_probe.py`, which applies the MADR's D2 checks and makes the one `GET`. Record: whether codex
   printed the URL, whether a stub was called, the port bound, and whether `codex login status` in the scratch home
   then reports ChatGPT. *Owner-run; asks first; scratch home; quota: none; the scratch grant is revoked with `codex
   logout` in the scratch home.*
4. **The table.** The MADR's "What each agent offers" table and D1's assignment updated from the findings, as an
   amendment if a decision changes (C6).

Acceptance: each probe's outcome recorded; the MADR matches it.

### P2 — the replay helper (MADR D2, D3)

1. `internal/providerauth/replay/replay.go`: `type Target struct{...}` from `ParseAuthorize(authorizeURL string)
   (Target, error)` (check 1); `(Target).Check(paste string) (Callback, error)` (checks 2 and 3, errors named
   `replay: host`, `replay: port`, `replay: path`, `replay: state`, `replay: no code`, `replay: denied`
   *(2026-11-11, the owner's ruling on the MADR's check 3: and `replay: provider error`, for an `error` other than `access_denied`, which the session ends
   `failed` with kind `other`; `replay: denied` is `access_denied` alone, kind `denied`. Both are terminal, not bad
   pastes: the session does not ask again and the re-ask counter does not move)*); `(*Replayer).Send(ctx,
   Callback) (status int, err error)` (checks 4 to 6), spent after the first call.
2. `Callback`'s `String`, `GoString` and `LogValue` print `<redacted>`.
3. `session.go`: a replayable session holds its port; a second on the same port is refused `too_many_sign_ins`. The
   port is known only from the authorize URL, after the agent started its attempt, so the refusal comes after start
   (MADR D3): the session cancels the agent's attempt by the agent's own route (opencode's `DELETE`, the `codex login`
   child killed and its pending home aborted, kilo's attempt left to expire) and ends `failed` with
   `too_many_sign_ins`.
4. Tests, each seen failing:

   | Test | Asserts | Planted break |
   | :--- | :--- | :--- |
   | `TestParseAuthorizeNeedsALoopbackRedirect` | `https`, a non-loopback host, no port, and no `redirect_uri` are each refused | the scheme check removed |
   | `TestCheckRefusesEachMismatch` | another host (`127.0.0.2`, `example.test`), port, path, `state`, and a missing `code` are each refused by name; `localhost` and `127.0.0.1` are equal | the `state` compare removed |
   | `TestCheckErrorParamIsDenied` | `?error=access_denied&state=<STATE>` is `denied`, not replayed *(2026-11-11, the owner's ruling on the MADR's check 3: and the session ends `failed` with kind `denied` at once, with no new `paste` step and the re-ask counter unchanged)* | `error` ignored *(2026-11-11, the owner's ruling on the MADR's check 3: and, separately, `access_denied` mapped to `other`; and the denial counted as a bad paste, so a `paste` step follows)* |
   | *(2026-11-11, the owner's ruling on the MADR's check 3)* `TestCheckOtherErrorParamIsOther` | `?error=server_error&state=<STATE>`, and `?error=invalid_scope&state=<STATE>`, are `replay: provider error`, not replayed, and end the session `failed` with kind `other`, with no new `paste` step; `?error=access_denied` with another `state` is `replay: state`, a bad paste that asks again | (a) every `error` mapped to `denied`, the struck rule of the MADR's check 3; (b) another `error` value counted as a bad paste, so a `paste` step follows; (c) the `state` compare skipped when an `error` is present |
   | `TestSendUsesTheRedirectNotThePaste` | the request goes to the fake listener at the `redirect_uri`'s port, with the paste's query | the paste's host used |
   | `TestSendIsOneShot` | a second `Send` is refused with nothing sent | the spent flag removed |
   | `TestSendFollowsNoRedirect` | a `302` from the listener is returned, its `Location` never fetched | `CheckRedirect` left default |
   | `TestSendUsesNoProxy` | through a test seam, the replayer's `Transport.Proxy` is nil; and, with `Transport.Proxy` forced to the fake proxy by the plant, the fake proxy sees the request, so the assertion can fail | `Transport.Proxy = http.ProxyURL(fakeProxy)`, which proxies every request (a plant of `http.ProxyFromEnvironment` cannot fail: it never proxies `localhost` or a loopback address) |
   | `TestSendReadsAtMost64KiB` | a 1 MiB body is not read past 64 KiB | the limit removed |
   | `TestReplayNeverLogsThePaste` | a planted sentinel in the paste is in no log line and no error; each replayable session's `paste` step carries `secret: true` | an error wrapping the URL; the `paste` step built without `secret` |
   | `TestOneReplayablePerPort` | a second replayable session on port 1455 is refused; another port is admitted | the port map removed |
   | `TestPortHeldRefusedAfterStart` | a session whose fake agent returns an authorize URL on a held port ends `failed` `too_many_sign_ins`, the fake agent sees its attempt cancelled (a `DELETE` for the opencode fake, a killed child for the codex stub), and no `paste` step is emitted | the cancel of the agent's attempt skipped, so the fake's attempt stays open |

5. Gates (`0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` C4). **Stage.**

### P3 — kilo's browser upstreams (MADR D1)

1. By U5: `deviceauth.go` posts the paste to `oauth/callback`, or hands it to the replay. Either way the session's
   steps are `open_url` with `accepts`, `paste`, `waiting` (the configured-set poll, as today), `done` or `failed`.
2. kilo's `oauth` methods stay listed `oauth`, `at_start` (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7); a session whose authorize URL passes check 1
   now emits `open_url` with `accepts` and a `paste` step instead of `failed` `unsupported`; the rest stay as `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P3
   left them.
3. Tests: `TestKiloBrowserSignInByPaste` against a fake engine whose authorize returns a loopback URL, with the fake
   listener or the fake callback route, and the poll seeing the credential; seen failing with the paste not
   delivered. `TestKiloBrowserBadPasteAsksAgain`: a wrong `state` asks again, the third ends `failed`
   `invalid_request`; seen failing with the counter removed.
4. Gates. **Stage.**

### P4 — opencode's code and browser attempts (MADR D1)

1. By U6: `mode: "code"` becomes a `paste` step with `accepts: code`, submitted to the route U6 found; a browser
   attempt with a loopback URL takes the replay.
2. Tests: `TestOpencodeCodeAttemptByPaste`, `TestOpencodeBrowserAttemptByReplay`, each against the fake control plane,
   each seen failing with the submission removed.
3. Gates. **Stage.** If U6 found no route, step 1's first half is not built and the record says so.

### P5 — codex's browser login (MADR D4)

**Gate:** U13 recorded with a replay that completes `codex login` (P1 step 3). The owner accepted this
orchestration as named Magic-side logic by O5 (a) of
`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`, on 2026-11-10. Until U13 is
recorded, codex's browser method stays `host_only` and this phase waits.

1. `codex/browser_signin.go`: an owned flow running `codex login` in the pending `CODEX_HOME`, with `open` and
   `xdg-open` stubs in the transaction directory as grok's (`grok/owned_device_auth.go`), and on macOS the same
   sandbox profile if U13 shows codex opens the browser another way; the authorize URL read by
   `scrape.AuthorizeURL`; the replay's `Target` from it.
2. Steps: `open_url` (`accepts: url_only`), `paste`, `waiting`, then the coordinator's stage, validate and commit, and
   `done`, or `failed`.
3. `codex/signin.go`: the `browser` method (`browser_paste`, `oauth_session`, `ClientIdentity` `none`).
4. Tests: `TestCodexBrowserSignInByReplay` with a stub `codex` that prints an authorize URL, listens on a test port,
   and writes a credential when called with the right `state`; `TestCodexBrowserLeavesLiveUntouched` (cancel, bad
   paste and timeout leave LIVE byte-identical, `0074-MADR-remote-provider-auth-from-phone.md` D22); `TestCodexBrowserOpensNoHostBrowser` (the stub
   records no call); each seen failing on a planted break (the replay skipped; LIVE written; the stubs not on `PATH`).
5. Gates, the Linux and Windows test hosts included (the stubs are POSIX; on Windows the method is listed
   `host_only` unless U13 shows a way to suppress the browser there). **Stage.**

### P6 — acceptance (MADR Confirmation)

1. Confirmation 1 to 5 and 7, each with its evidence.
2. **Owner-run, each asked first, each in a scratch home:**
   * **R1:** a codex browser sign-in from the phone, the landed URL pasted. *Owner-run; asks first; scratch home;
     quota: none; the scratch grant revoked afterwards.*
   * **R2:** kilo's OpenAI browser upstream from the phone, by the route or the replay. *Owner-run; asks first;
     scratch `XDG_DATA_HOME`; quota: none.*
   * **R3:** an opencode browser or code attempt, where P4 built one. *Owner-run; asks first; scratch home; quota:
     none.*
3. This PLAN `complete`; `check_records.py --write-index`.

## Verification

```bash
make pre-add-check FILES="<the phase's Go files>"
go test -race -count=1 ./internal/providerauth/... ./internal/provider/...
make test
go test -race -count=1 ./...
make lint staticcheck vet-tags preflight
make ci-windows                     # on the Windows test host, on a scratch copy
go test -race -count=1 ./...        # on the Linux test host, on a scratch copy
python3 scripts/check_records.py --check-all
python3 ~/.global-git-hooks/github-disclosure.py redact
```

Before any push the owner asks for, the guard over the outgoing commits (`AGENTS.md`, "Host identifiers never appear
in records"):

```bash
echo "refs/heads/master $(git rev-parse HEAD) refs/heads/master $(git rev-parse origin/master)" | python3 ~/.global-git-hooks/github-disclosure.py pre-push origin "$(git remote get-url origin)"
```

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 each check refused by name | P2 | step 4 |
| 2 one replay, no redirect, no proxy, bounded body | P2 | step 4 |
| 3 no paste in logs | P2 | step 4 |
| 4 one session per port | P2 | step 4 |
| 5 the probes recorded | P1 | step 4 |
| 6 the owner-run sign-ins | P6 | step 2 |
| 7 a held port refused after start | P2 | steps 3 and 4 (`TestPortHeldRefusedAfterStart`) |

## Rollout and Rollback

* **Order:** after `0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` P3. P2 to P5 add methods to the new vocabulary; no wire message changes, and the app needs
  none. Each phase is one commit; none is released before that plan's cut-over release.
* **Rollback:** revert P5, P4, P3 and P2, newest first. The methods they added return to `host_only`, which is the
  state before this plan. No stored credential changes: the agents keep their own stores, and codex's coordinator
  aborts any pending candidate (`0074-MADR-remote-provider-auth-from-phone.md` D26).

## Execution record

Not started. The MADR is `proposed`.
