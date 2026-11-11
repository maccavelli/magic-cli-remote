---
status: proposed
date: 2026-11-10
decision-makers: Project Owner
consulted: "this repository at 912b9aa1, read from code (first drafted at 995d442b; every file cited here is identical at both); two independent reviews of these drafts, 2026-11-10; docs/reports/0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md §5.6.4, F13 to F15, M19 and M23, U5, U6 and U13; the owner's answer of 2026-11-10 to its Q13; 0246-MADR (proposed, this repository); go-llmprovider-sdk v1.29.0's llmprovider/auth/oauth_loopback.go, read from the module cache; codex-cli 0.167.0's login help, from the audit's probe log"
informed: "go-llmprovider-sdk and gobble-cli, whose own paste channels come first"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Replay a pasted sign-in callback to the agent's own loopback listener, only where the agent offers no paste of its own

## Context and Problem Statement

A browser sign-in that redirects to `http://localhost:<port>/<path>` completes only where the browser runs. When the
browser is the phone's and the agent runs on the host, the redirect lands on the phone's own loopback and fails; the
URL the phone's browser shows then holds the authorization code and the `state`. `0074-MADR-remote-provider-auth-from-phone.md` deferred this as W3, a
reverse tunnel, and its D7 refuses every loopback flow at start today: kilo's (`httpagent/deviceauth.go:79-83`) and
opencode's (`opencode/acp_auth.go:662-670`) return "signs in through a browser on the host, which the phone cannot
reach yet". codex's browser login is not advertised at all (`codex/auth.go:136-154` lists the key and the device code).

The owner chose on 2026-11-10 (the 0245 report's Q13): "**Scoped replay now**: the daemon replays a pasted callback URL
to the agent's own loopback listener, only where the agent has no paste channel. The checks are loopback host, port,
path and state equal to the agent's own authorize URL, one shot, never logged or stored. The reverse tunnel (`0074-MADR-remote-provider-auth-from-phone.md` W3)
stays deferred. Prefer agent-native paste where it exists: gobble through elicitation, and kilo or opencode if U5 or U6
confirm their routes."

[0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
gives the steps this needs: `open_url` with `accepts`, then `paste`, answered by `provider.auth_answer` with a redacted
`Answer.paste`. This record decides what the daemon does with that paste when the agent cannot take it.

### What each agent offers, at `912b9aa1`

| Agent and method | Redirect | Agent-native paste | Today |
| :--- | :--- | :--- | :--- |
| gobble, a method with `completion` `browser_paste` (openai's plan) | gobble's own SDK flow | **yes:** ACP elicitation, the `paste` step's `callback` form (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D8; gobble-cli `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4) | host-only |
| gobble, `browser_loopback` (grok's browser) | gobble's own SDK flow, on gobble's host | **no:** gobble drives it only when named, and it completes only where the browser reaches gobble's host (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4 step 3); the SDK refuses a paste for Grok, as its paste is unverified (go-llmprovider-sdk `docs/decisions/0074-MADR-one-sign-in-handle-and-method-facts.md` D3's step rule, under which a browser sign-in gets `accepts` and a `paste` step only when the method's `Paste` fact is set, so `browser_loopback` gets none, and D4, which refuses one as `ErrUnsupported`; re-pointed 2026-11-11) | listed `host_only` (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7) |
| kilo, OpenAI ChatGPT browser | `http://localhost:1455/auth/callback` (`0074-MADR-remote-provider-auth-from-phone.md` §7.2, a live authorize response of 2026-08-10) | **unverified:** kilo's OpenAPI paths list `/provider/{providerID}/oauth/callback` (`internal/provider/kilo/testdata/kilo-spike-7.4.23/openapi-paths.txt:188`, a list of paths without verbs), and neither its verb nor its body is read; Magic never calls it (`deviceauth.go:38-40`). The same file lists `/api/integration/{integrationID}/connect/oauth` (`:17`) | refused at start |
| kilo, xAI SuperGrok | a `redirect_uri` on loopback (the probe of `0086-MADR-phone-provider-auth-completion.md` D6) | as above | refused at start |
| opencode, a browser integration | by the attempt's URL | **unverified:** an attempt of `mode: "code"` wants a code pasted back, and Magic refuses it (`acp_auth.go:658-661`); its submit route is not known | refused at start |
| codex, `codex login` | `localhost:1455`, as the SDK's OpenAI client mirrors (`llmprovider/auth/oauth_loopback.go:38`, go-llmprovider-sdk v1.29.0) | **no:** codex-cli 0.167.0's `login` takes `--with-api-key`, `--with-access-token` and `--device-auth`, and no paste (the audit's probe log) | not advertised |
| grok, `grok login --oauth` | accounts.x.ai delivers the code to the loopback by a cross-origin fetch (`oauth_loopback.go:471-474`), so the phone's address bar may never show it (report F13) | **unverified:** ACP `x.ai/auth/submit_code`, declined by `0107-MADR-grok-phone-api-key-and-device-code-auth.md` D5 | host-only |

### Facts that bound a replay

* **The listener is the agent's.** The agent binds the port and checks the `state` itself; Magic adds nothing a
  vendor must accept. The code is worth a token only with the agent's PKCE verifier, which never leaves the agent.
* **Ports are shared.** OpenAI-style sign-ins bind 1455, else 1457 (`oauth_loopback.go:38`), and fail with "registered
  callback ports unavailable" when both are taken (`:369`). codex's login and kilo's OpenAI browser flow use 1455
  (`0074-MADR-remote-provider-auth-from-phone.md` §7.2). Two such sign-ins at once on one host contend (report F14).
* **The paste is a secret.** It holds an authorization code. `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D3 redacts it in every log and never retains
  it. The mapper's `paste` step carries `secret: true`, the daemon sets the answer's `secret` from that step, and the
  contract's `Answer` then masks the paste in `String`, `GoString` and `LogValue` (go-llmprovider-sdk
  `docs/decisions/0073-MADR-publish-an-auth-contract-module.md` D2, D6 rule 5).

## Decision Drivers

* Magic relays and decides nothing about a flow; a helper that needs no vendor knowledge is the only Magic-side logic
  the owner accepted (Q13).
* The agent's own channel wins wherever one exists.
* A paste must never become a way to make the daemon fetch an arbitrary URL.
* Secrets: one use, never logged, never stored.
* Probe before building: U5, U6 and U13 are unverified.

## Considered Options

* **A. A scoped replay in the daemon,** after agent-native paste, under fixed checks.
* **B. The reverse tunnel now (`0074-MADR-remote-provider-auth-from-phone.md` W3):** the phone's browser is sent to a daemon-hosted relay URL that forwards the
  callback over the WebSocket to the host's loopback.
* **C. No replay:** a browser method stays `host_only` until each agent offers a paste channel.

## Decision Outcome

Chosen option: "A. A scoped replay in the daemon", because it is the owner's choice (Q13), completes codex's and the
engines' browser sign-ins from the phone with no vendor knowledge, and keeps the agent's own `state` and PKCE checks as
the guard. This record is `proposed`, awaiting the owner's review.

### D1. Which channel a browser sign-in uses

In this order, decided per method by the mapper (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7), never by a label:

1. **The agent's own paste.** gobble's elicitation (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D8); kilo's `oauth/callback` if U5 shows it takes the
   pasted URL or code; opencode's code route if U6 names it.
2. **The replay (D2),** where the agent listens on a loopback redirect and offers no paste.
3. **`host_only`,** where neither holds: grok's browser until U1 or U7, and any flow whose callback is not a plain
   `GET` the user can see.

A method whose mechanism is known before start (codex's browser login) is listed with `completion` `browser_paste`
for 1 and 2, and `browser_loopback` with reason `host_only` for 3. kilo's and opencode's OAuth methods stay listed
`oauth` with `completion` `at_start` (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7; the owner's O1), and their session's steps carry the channel: an
`open_url` with `accepts` and a `paste` step for 1 and 2, a `failed` step of kind `unsupported` for 3.

### D2. The replay's checks

`internal/providerauth/replay` takes the agent's authorize URL, captured when the session starts, and the pasted
value, and does one thing: a `GET` to the agent's listener. It refuses, naming which check failed and never echoing
the paste:

1. **The authorize URL's `redirect_uri`** is `http`, its host `localhost`, `127.0.0.1` or `::1`, with an explicit
   port. Otherwise the method is not replayable, and the mapper lists it `host_only`.
2. **The paste** parses as an absolute URL whose scheme, host, port and path equal the `redirect_uri`'s (host compared
   as one of the three loopback names, since a browser may show either `localhost` or `127.0.0.1`).
3. **Its `state`** equals the authorize URL's `state`, compared in constant time; it has a `code`, or an `error`, ~~which
   ends the session `failed` with kind `denied` and is not replayed~~. *(Corrected 2026-11-11 by the owner's ruling
   of that day, relayed by the coordinator of the four-repository auth-contract work, to go-llmprovider-sdk `docs/decisions/0074-MADR-one-sign-in-handle-and-method-facts.md` D4's rule,
   which the owner confirmed: a pasted URL that carries this sign-in's `state` and `error=access_denied` ends the
   session `failed` with kind `denied`; one that carries this sign-in's `state` and any other `error` value ends it
   `failed` with kind `other`. Neither is replayed, neither is a bad paste, and neither is asked for again (check 6
   does not count it). A pasted `error` with another `state` fails this check's `state` compare and is a bad paste
   under check 6. The struck words ended every `error` as `denied`. This repository's interactive session agreed to
   the alignment the same day; this note is written by one writer, the coordinator's records writer, so that the
   text has one author.)*
4. **The request is built from the `redirect_uri`,** never from the paste's host: scheme, port and path from the
   authorize URL, the query from the paste. It dials `127.0.0.1`, then `::1` for `localhost`, never a name lookup and
   never a proxy (`Transport.Proxy` nil).
5. **No redirect is followed** (`CheckRedirect` returns `http.ErrUseLastResponse`); the response body is read to at
   most 64 KiB and discarded; the request times out after 10 s.
6. **One shot.** The first request spends the replay, whatever it returns. A paste that fails checks 2 or 3 spends
   nothing and is asked for again, with a reason and without the paste, ~~at most three times~~ at most twice, so a
   sign-in takes at most three pastes in all and the third bad one ends it `failed` with `invalid_request` (as
   go-llmprovider-sdk `docs/decisions/0074-MADR-one-sign-in-handle-and-method-facts.md` D4 asks of the SDK's own paste; corrected 2026-11-11 to that record's count).
7. **Only while the session is live.** A paste for an ended session is `no_such_sign_in`.
8. **Never logged or stored.** The paste exists in the `Answer`, which redacts itself, and in the one request. Errors
   carry the check's name. Success is the agent's: codex's process exits 0 and its candidate commits; kilo's
   configured set changes. The replay's HTTP status is logged, the URL never.

### D3. One loopback port, one sign-in

A replayable session holds its `redirect_uri`'s port: Magic admits one replayable session per loopback port per host
at a time, and refuses a second with `too_many_sign_ins`, naming the port. This is on top of the admission of
`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D5 (2 per device, 8 in all, one per
provider and upstream).

It cannot be part of that admission. The port is learned from the authorize URL, which exists only after the agent has
started its attempt, and that record's admission runs before any side effect. So the port check is a **refusal after start**:
when the authorize URL's port is held by another replayable session, the new session cancels the agent's attempt by
the agent's own route (opencode's attempt `DELETE`, `opencode/acp_auth.go:731-743`; the `codex login` child killed,
its pending home discarded by the coordinator; kilo's attempt left to expire, as that record's D5 records kilo has no cancel
route) and ends `failed` with `too_many_sign_ins`, naming the port. Nothing is replayed and no paste is asked for.

### D4. What it enables

* **codex's browser login.** Named Magic-side logic, accepted by the owner's answer to O5 of
  `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` ((a), 2026-11-10). It is built
  only after U13 shows that a replay completes it (the PLAN's P5 waits for that). `codex login` runs in an isolated, empty `CODEX_HOME` inside the coordinator, as the
  device login does (`0074-MADR-remote-provider-auth-from-phone.md` D22), with the host's browser suppressed as grok's is (`ownedGrokEnv`'s stubs,
  `grok/owned_device_auth.go`); the authorize URL is read from its output by the quarantined scraper (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7).
  Steps: `open_url` with `accepts: url_only`, `paste`, `waiting`, `done` or `failed`. Its method: `browser`,
  `browser_paste`, `oauth_session`, `ClientIdentity` `none` (codex's own client, run by codex), `slot` `openai`, with
  codex's other three methods (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7). Whether `codex login`
  prints the URL, how it opens a browser, and that a replay completes it are U13's.
* **kilo's and opencode's loopback flows,** by D1's order.
* **Not codex's access-token paste,** which `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7 carries as a value method through `provider.set_credential`.

### D5. The tunnel stays deferred

`0074-MADR-remote-provider-auth-from-phone.md`'s W3 is not started. It is reopened by a record of its own when any of these holds:

* an agent's callback reaches its loopback by a request the user cannot see or paste (grok's accounts.x.ai, if U1
  finds no pasteable fallback and grok's ACP `x.ai/auth/*` is gone, U7) and the owner wants that method on the phone;
* a provider's callback is a form `POST` or carries a body;
* a provider rejects a redirect the user completes on another device.

### Consequences

* Good, because codex's browser login, and kilo's and opencode's browser upstreams, can complete from the phone.
* Good, because the replay knows no vendor: it checks URLs against the agent's own authorize URL.
* Good, because the agent's `state` and PKCE checks still decide; the replay only delivers.
* Neutral, because an agent that adds its own paste moves to D1's first branch with no change here.
* Bad, because the user must copy the address bar's URL after a failed redirect, which is clumsier than a tunnel.
* Bad, because one sign-in per loopback port can be in flight on a host.

### Confirmation

1. A unit test refuses each failing check of D2 by name, on planted inputs (a non-loopback host, another port,
   another path, another `state`, no `code`, an `https` redirect, a paste naming `127.0.0.2`), and seen failing with
   each check removed. *(2026-11-11, the owner's ruling on check 3: and check 3's two endings, `error=access_denied` as `denied` and another
   `error` value as `other`, each with this sign-in's `state`, each not replayed and not asked again, each seen
   failing on its own planted break.)*
2. A test with a fake listener: one replay arrives with the paste's query; a second is refused; a redirect from the
   listener is not followed; the request carries no proxy; the response body is not read past 64 KiB.
3. No log line, error or retained record holds a planted paste sentinel.
4. D3's admission refuses a second replayable session on one port.
5. U5, U6 and U13 are recorded, and D1's per-agent table matches what they found, gobble's two rows included
   (`0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7's gobble row).
6. The owner-run runs of the PLAN complete a codex browser sign-in, and each kilo or opencode method D1 assigns,
   from the phone.
7. A session whose authorize URL names a port another replayable session holds is refused after start: the agent's
   attempt is cancelled by its own route and the session ends `too_many_sign_ins`; seen failing with the check
   removed.

## Pros and Cons of the Options

### A. A scoped replay in the daemon

* Good, because it is small, generic, and needs no new network surface.
* Good, because the agent's checks still guard the code.
* Bad, because the user copies a URL by hand.
* Bad, because it fails for a callback the user cannot see.

### B. The reverse tunnel now

* Good, because the user copies nothing: the redirect reaches the host through the daemon.
* Bad, because the `redirect_uri` is the agent's loopback, so the tunnel needs a listener inside the app on the
  phone's own loopback port (`0074-MADR-remote-provider-auth-from-phone.md` D13's deferred in-app listener) and a relay over the WebSocket; whether a phone
  operating system keeps that listener running while its browser is in front is unverified.
* Bad, because it is the order-of-magnitude complexity `0074-MADR-remote-provider-auth-from-phone.md` §4 deferred, and the owner kept it deferred (Q13).

### C. No replay

* Good, because nothing is added.
* Bad, because codex's browser login, and kilo's and opencode's browser upstreams, stay host-only, which is today's
  F11.

## More Information

### Records amended, applied by the PLAN's P0 on acceptance

* [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md): D7 ("A returned URL whose
  query contains `redirect_uri` pointing at `localhost`/`127.0.0.1` is a **browser loopback** flow (Strategy B, out of
  scope for wave 1)") amended: such a flow is replayable under this record's D2; W3 stays deferred under D5.
* [0086-MADR-phone-provider-auth-completion.md](0086-MADR-phone-provider-auth-completion.md) D6's SuperGrok row ("Mark
  `available: false`, `reason: browser_only`"): replayable under D1 once its redirect passes D2's check 1.

### Where the report differs

* **The report's §5.6.4 says Magic replays "to the agent's own listener on `127.0.0.1`".** kilo's redirect names
  `localhost` (`0074-MADR-remote-provider-auth-from-phone.md` §7.2); D2 accepts the three loopback names and dials the two addresses.
* **The report's M23 puts codex's access-token paste here.** `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md` D7 carries it as a value method.

### Not verified, with the probe that settles it

Each is owner-run, asks first, and runs in a scratch home; none is run by this record.

* **U5, kilo's `POST /provider/{providerID}/oauth/callback`:** read kilo's OpenAPI document and source for its body
  offline; then one throwaway OpenAI browser sign-in through `kilo serve` with a scratch `XDG_DATA_HOME`, the redirect
  URL pasted to the route. *Owner-run; asks first; scratch home; quota: none; the sign-in uses the owner's ChatGPT
  account; its grant is revoked where kilo offers a logout, else left to expire, and which of the two is recorded:
  deleting the scratch home deletes the local copy, not the grant at OpenAI.*
* **U6, opencode's code submit for `mode: "code"`:** read opencode's control-plane API offline; then one throwaway
  sign-in of an integration whose attempt is `mode: "code"`. *Owner-run; asks first; scratch home; quota: none.*
* **U13, a replayed callback completes `codex login`:** `codex login` in a scratch `CODEX_HOME`; open the printed URL
  on another device; paste the landed URL into a scratch replay script that applies D2's checks. The scratch home
  keeps codex's pre-login revoke of a stored ChatGPT grant (`0074-MADR-remote-provider-auth-from-phone.md` F14) away from the host's real one.
  *Owner-run; asks first; scratch home; quota: none; one new grant at OpenAI, revoked afterwards with `codex logout`
  in the scratch home.*
* **Whether `codex login` opens a browser through `open` or `xdg-open`,** so that stubs suppress it: U13 records it.

### Related

* PLAN: [0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md).
* [0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md).
* The 0245 report, §5.6.4 and §12.
