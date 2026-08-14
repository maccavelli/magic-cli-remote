---
status: proposed
date: 2026-08-14
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Treat a phone credential setup as complete only when the agent can actually use it

## Context and Problem Statement

Configuring auth and API keys from the phone is a product-critical path.
[0074](./0074-MADR-remote-provider-auth-from-phone.md) made it the control-plane
purpose of `provider_auth`. [0082](./0082-MADR-settings-provider-menu-ux-overhaul.md)
and [0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md) shipped the
settings hub, the per-agent sheets, and an "activation pass" that claimed the
write and device paths were complete.

On a physical phone against this host they are not. Pasting an API key under
Settings → Providers reports that the credential was created. The agent stays
unconfigured, the vendor is not unlocked in the model picker, and the file the
operator inspects on the host often does not change. OAuth is worse: the
verification link cannot be tapped open, and kilo's "Headless / Remote / VPS"
xAI method starts, then never delivers a token.

This record asks: **what must be true before the phone may say a provider is
configured, and which write, status, and OAuth gaps have to close before that
sentence is honest?**

It does not replace 0074's protocol (C+D first, then A; Strategy B deferred) or
[0085](./0085-MADR-grok-acp-auth-method-wiring.md)'s grok ACP handshake. It
corrects the claim that those decisions are *complete* for the user.

### What the user actually hit (this host, 2026-08-12–14)

Daemon `mcremote` 0.10.x, phone device `s22+`, engines `kilo 7.4.21` on
`:52010` and `opencode 1.18.17` on `:52009`. Evidence is the daemon log, the
on-disk stores, and live `GET`s against the running engines (ids only; no
secret material is recorded here).

| When | What the phone did | What the host did |
| --- | --- | --- |
| 2026-08-12 20:57–21:00 | Two `provider.start_auth` attempts | `unsupported for this provider` |
| 2026-08-12 21:00 | kilo × xAI method 1 ("Headless / Remote / VPS") | `kilo device flow started upstream=xai method=1`; `ok=false` 113 s later. `~/.local/share/kilo/auth.json` unchanged (still `kilo` oauth + `opencode-go` api, mtime 2026-08-06) |
| 2026-08-13 12:34 | opencode × xAI method 0 ("SuperGrok Subscription") | `device flow started … ok=false` 92 s later |
| 2026-08-13 18:32 | kilo × kilo Gateway device | `ok=true` in **5 s** — kilo was *already* in `GET /config/providers`. No new token |
| 2026-08-13 23:43 | opencode × kilo API key, twice | `provider credential set` both times. `~/.local/share/opencode/auth.json` gained `kilo: {type: api}` (mtime 23:43). Live `GET /provider.connected` is still `["opencode","opencode-go","openai"]` — **kilo is not connected** |
| anytime since | grok prewarm | `agent advertises auth methods but none configured` (`count=2`). `~/.grok/config.toml` has **no** `api_key`. Grok is SuperGrok via `~/.grok/auth.json` only |

There is **no** `provider credential set` log line for the kilo *agent*. A user
who opened Settings → Providers → kilo, pasted a key, and then inspected
`~/.local/share/kilo/auth.json` is looking at the file that never received
that write. The one write that did land (opencode's `kilo` api entry) is in a
different product store and is ignored by the engine.

## Decision Drivers

* Phone-driven auth is P0. A toast that the key was created is a lie unless
  the agent can run a turn with it.
* Headless-first (0074): no browser on the host, no SSH, no mcremote-owned
  vault, no secret in argv or logs.
* Honest UI (0083): every method the sheet offers must complete or be disabled
  *before* the user types a secret or starts a sign-in.
* The live CLI/engine is the contract (AGENTS.md; 0074 D15). Status, write
  verification, and OAuth classification must be pinned to what kilo 7.4.21,
  opencode 1.18.17, grok 1.0.3, codex, and goose actually honour today.
* Do not reopen 0074 W3 (loopback tunnel) as a rider. Browser-callback
  vendors stay a successor. Device and API-key paths must work without it.
* Small, testable increments; each fix independently shippable. Prior MADRs
  already claimed this surface "implemented" — confirmation must be
  end-to-end, not "the RPC returned 2xx".

## Considered Options

* **O1 — Patch the reported kilo key write** (engine PUT, maybe a file
  fallback) and leave status, OAuth, and the other agents as they are
* **O2 — Auth-completion contract:** a set-credential or device flow is
  successful only when the agent's *runtime connected set* (and native store)
  agrees; every phone method is either finishable end-to-end or marked
  unavailable; agent-level "configured" means "can run", not "every catalog
  row has a key"
* **O3 — O2 plus Strategy B now** (reverse callback tunnel so host-loopback
  OAuth can finish in the phone browser)
* **O4 — Abandon engine PUTs** and write every agent's file from the daemon

## Decision Outcome

Chosen option: **"O2 — Auth-completion contract"**, because O1 would fix one
symptom (and this host's log shows the kilo-*agent* never got a key write at
all) while leaving the user-visible loop broken; O3 is still the right
successor for GitLab / Snowflake / ChatGPT-browser but it is an order of
magnitude more surface and is not what failed this week; O4 re-fights 0074 D1
and loses the engine's own validation.

This MADR is **proposed** for review. It does not change code. The
companion plan is
[0086-PLAN-phone-provider-auth-completion.md](./0086-PLAN-phone-provider-auth-completion.md).

### Sub-decisions (proposed)

**D1 — Success is verify-after-write, not HTTP 2xx.**

`handleSetCredential` may return `ok` only after both of:

1. The agent's native write succeeded (kilo/opencode `PUT /auth/{id}`, grok
   quoted `[model."<id>"]`, codex `login --with-api-key`, goose file store).
2. A *read* the agent itself uses now reports the upstream. For
   OpenCode-family engines that read is the **D13 verify ladder**, not a
   mandatory `GET /provider` (4.7–4.9 MB). Disk `auth.json` presence
   alone is not enough — that is the 23:43 lie. For grok/codex/goose,
   use the same presence function `AuthStatus` already uses (file/env,
   no catalog).

If (1) succeeds and (2) fails, return a typed error
(`credential_not_accepted`: "the agent stored the value but is not using it
— this vendor needs a different sign-in method") and do **not** push a
configured status. That is exactly the 2026-08-13 23:43 opencode/`kilo` case.

**D2 — Do not synthesise an API-key method the engine will not accept.**

`BuildCatalog` currently invents `{id: "<vendor>:api", type: api_key}` for
every vendor missing from `GET /provider/auth`
(`httpagent/authcatalog.go` `BuildCatalog`). OpenCode's catalog does **not**
list `kilo`; the phone therefore offered a kilo API key, `VerifyAPIKeyMethod`
skipped the engine check for the `:api` suffix, `PUT` wrote
`{type:"api",key}`, and the engine left `kilo` out of `connected`.

Rules:

* A synthesised `:api` method is allowed only after a successful
  verify-after-write against a scratch or live probe, *or* the vendor is in
  a small allow-list of known key-only ids (togetherai, deepseek, groq, …).
* `kilo` as an OpenCode vendor is **not** key-only. Offer no API-key row
  until the engine lists one.
* On the kilo *agent*, the `kilo` upstream's only catalog method is
  `oauth` "Kilo Gateway (Device Authorization)". Never invent a key field
  for it.

**D3 — Agent-level "configured" means the agent can run, not that every
catalog row has a key.**

`worstAuthStatus` (`provider_status.dart`) ranks `missing > configured`.
Kilo's `AuthStatus` always emits the 13 `GET /provider/auth` vendors, most
of them `missing`. The Providers card and the detail header therefore stay
"Needs setup" even when kilo Gateway OAuth and an OpenCode Go key are live.

Use `ProviderAuthInfo.status` (already `configured` when
`len(configured) > 0`) for the agent chip. Keep per-row chips honest.
`worstAuthStatus` remains for `error` / `quota` only.

**D4 — The status list must include a vendor the user just configured.**

Today kilo status is `GET /provider/auth` (13) ∪ `GET /config/providers`.
A long-tail key (togetherai, …) is not in the 13, and only appears if
`/config/providers` lists it. After D1, a successful write is in
`connected`; D4 requires status to union that connected set into the
upstreams it returns, so the new row is visible without re-opening the
185-vendor catalog.

On this host `/config/providers` ids *happened* to equal `/provider.connected`
(`kilo`: `opencode-go`, `kilo`; opencode: `opencode`, `opencode-go`,
`openai`). That coincidence is not a contract, but `/config/providers` is
the **cheap** connected-id oracle (123–353 KB vs 4.7–4.9 MB) and is D13
Layer 1. Device-flow completion and status read the in-process connected
cache (D13 Layer 0), refreshed by that cheap oracle, not by `/provider`.

**D5 — Device-flow completion cannot treat "already configured" as success.**

`awaitEngineCredential` (`httpagent/deviceauth.go`) polls the configured set
and returns on first membership. The 2026-08-13 kilo Gateway flow returned
`ok=true` in 5 s because `kilo` was already in `/config/providers`. Snapshot
the set (or the oauth `expires` / type) *before* `POST …/oauth/authorize`
and wait for a *change*. If the set is unchanged at timeout, fail.

**D6 — Catalog hints follow the isolated authorize probe, not the
label's adjectives.**

Live `GET /provider/auth` on kilo 7.4.21:

* `xai:0` `oauth` "xAI Grok OAuth (SuperGrok Subscription)"
* `xai:1` `oauth` "xAI Grok OAuth (Headless / Remote / VPS)"
* `xai:2` `api` "Manually enter API Key"

P0 (isolated `kilo serve`, 2026-08-14, engine killed without completing
either flow):

* Method 0 authorize URL is `https://auth.x.ai/oauth2/authorize?response_type=code&…&redirect_uri=…` (PKCE). `Classify` = **browser**. Mark `available: false`, `reason: browser_only`. The label has no "browser" word, so today's hint would have offered *Start sign-in* and then died at D7.
* Method 1 authorize URL is `https://accounts.x.ai/oauth2/device?user_code=…` with instructions `Enter code: XXXX-XXXXX`. `Classify` = **device**. **Keep offered.** "Headless / Remote / VPS" is a real RFC 8628 grant. The 2026-08-12 hang (`ok=false` after 113 s) was D5/D7 (copy-only URL, poll), not a host-only flow. Do **not** mark it `host_oauth`.
* Method 2 stays the API-key path.

`host_oauth` is still the reason for rows that are neither device nor
loopback (synthesised kilo-via-opencode sign-in). Re-probe authorize
URL shape on every kilo minor; 0074 D7 still classifies by URL at
start time.

**D7 — Device verification URLs open in the system browser.**

`device_flow_sheet.dart` still documents that `url_launcher` is not a
dependency and renders the URL as copyable text only. 0074 D13 required
one-tap open. Add `url_launcher`, make the URL a button, keep copy as a
secondary action. This is necessary for every genuine device flow (kilo
Gateway, ChatGPT headless, Copilot, grok `--device-auth`,
codex `--device-auth`).

It does **not** implement Strategy B. A loopback `redirect_uri` still
cannot complete from the phone; those rows stay `browser_only`.

**D8 — Push the device-flow result the phone is waiting on, even when the
wait fails.**

`awaitDeviceFlow` writes `oauth.device_flow_result` and a status push.
The phone waits 60 s for the *start* frame and then sits on the sheet
until result or cancel. Failed xAI flows ended `ok=false` with no
`error` line in the daemon info log beyond the boolean. Surface
`error_kind` + a clipped reason on the sheet (D5 taxonomy from 0083),
and treat a cancelled sheet as `oauth.cancel` (already wired) so the
host stops polling.

**D9 — kilo file-writer fallback stays, but it is not a substitute for D1.**

kilo implements `AuthWriterDialect` only. A cold or unstartable engine
cannot accept a phone key at all (opencode can: `AuthFileWriterDialect`).
Add the same 0600 `auth.json` merge kilo's engine itself writes, then
restart per 0074 D9, **and** still apply D1 against the post-restart
connected set. A file the engine will not read is the 23:43 bug in
another costume.

**D10 — Model picker and session defaults consume the same connected set.**

`ListModelProvidersLive` already prefers `/config/providers`. After D1
it must read D13 Layer 0 (the same cache status uses), not issue a
second `/provider`. `InvalidateAuthCatalog` drops the *vendor list*
cache; a write also bumps the connected-set generation so the picker
does not wait out the 5-minute catalog TTL. The empty-state copy "No
configured providers were reported. Set one up in Settings or on the
host" is the other surface of the same unlock failure.

**D13 — Verify and status never default to rereading `GET /provider`.**

D1 requires a confirm. The first draft of this record (and the first
draft of the plan) named `GET /provider` → `connected` as that confirm.
Live-measured on this host 2026-08-14, that is the wrong default:

| Endpoint | kilo 7.4.21 | opencode 1.18.17 | What it answers |
| --- | --- | --- | --- |
| `GET /provider` | 4 944 680 B, `{"all":[` first | 4 812 416 B, same shape | models.dev snapshot + `connected` **at the end** |
| `GET /config/providers` | 352 660 B | 123 555 B | connected vendors + models + **plaintext `key`** |
| `GET /provider/auth` | 3 381 B | 2 543 B | typed methods only (~10–13 vendors) |
| `GET /api/provider` | 96 B | 96 B | v2 *integration* list, not credentials |
| `GET /api/provider/{id}` | 210–323 B | 210–323 B | package/url; **200 even when unconnected** |
| `GET /provider?fields=connected` | still 4.9 MB | still 4.8 MB | ignored; no sparse fieldsets |
| `HEAD /provider` | 404 | 200 HTML, 0 body | no Content-Length of the JSON |
| ETag / Last-Modified / Cache-Control | **absent** | **absent** | RFC 7232 304 is not available |

`connected` sits *after* `all`. A streaming decoder can avoid retaining
the model tree in the heap; it cannot avoid reading 4.7 MB off the
loopback socket. Query parameters do not project the field. The v2
single-provider GET is the right *shape* (targeted, ~300 B) and the
wrong *oracle* (it is not membership).

Industry patterns that apply here, and those that do not:

* **RFC 7232 conditional GET (ETag / If-None-Match → 304).** The
  standard way to skip a large unchanged body
  ([MDN If-None-Match](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/If-None-Match),
  [OneUptime ETag guide](https://oneuptime.com/blog/post/2026-01-30-api-etag-headers/view)).
  The engines emit no ETag. Adopt it as Layer 1.5 **if** a live probe
  ever sees one. Until then, mint a *local* generation: SHA-256 of the
  sorted connected ids, compared after each cheap fetch, so an
  unchanged set does not rebuild status or push the phone.
* **JSON:API sparse fieldsets / GraphQL projections.** Ask only for
  `connected`. The engines ignore `?fields=` / `?include=` / `?connected=`.
  Do not pretend they work. Re-probe on kilo/opencode minor bumps.
* **Stale-while-revalidate + TTL cache.** Serve the last connected set
  from memory for a short TTL; refresh in the background. The existing
  `catalogTTL` (5 min) already does this for the vendor *list* and must
  be split from vendor *status* (status goes stale on every write).
* **Single-flight / request coalescing.** One in-flight refresh per
  engine (`golang.org/x/sync/singleflight` or an equivalent mutex).
  Five phones hitting Settings after one write must not start five
  4.7 MB downloads.
* **Write-ahead mutation ring (the honest ring-buffer).** Ring buffers
  fit sequential streams (SSE, logs), not random membership in a 4.7 MB
  snapshot. What we keep is a **fixed-capacity ring of credential
  mutations** `{seq, op, upstream, at}` (~32 slots). D5 snapshots
  `seq` + a per-upstream fingerprint, not the catalog. Status and
  device-poll compare the ring, not the blob.
* **Bloom filters.** Correct for "is this one of a million keys"
  ([Redis Bloom](https://redis.io/docs/latest/develop/data-types/probabilistic/bloom-filter/),
  Akamai one-hit-wonder caches). Our connected set is tens of ids.
  An exact `map[string]struct{}` is smaller, exact, and deletable.
  Do not use a Bloom filter here.
* **Do not ring-buffer the 4.7 MB body.** That is a worse cache: it
  still moves 4.7 MB, then spends RAM to replay it.

Therefore the confirm is a **ladder**. Higher layers run first; a
lower layer runs only when the one above cannot decide.

| Layer | What | When | Budget |
| --- | --- | --- | --- |
| **0** | In-process connected-set cache: ids, local generation, source, fetched-at. Mutation ring. | Every status, picker, and device-poll read | 0 B on the wire if fresh |
| **1** | `GET /config/providers` → ids only (struct **must not** declare `key`, 0043 D4) ∪ disk ∪ env | Cache miss, TTL expiry, or post-write confirm | 123–353 KB |
| **1.5** | Conditional `GET /provider` with `If-None-Match` | Only if a live probe has seen an ETag (none today) | 304 or 4.7 MB |
| **2** | Targeted single-vendor reads. `GET /api/provider/{id}` is **not** membership. There is no JSON `GET /auth/{id}`. After PUT, Layer 1 is the targeted confirm. | — | — |
| **3** | `GET /provider`, stream-decode `connected` only, discard `all`, single-flight | Layer 1 vs disk **disagree** (the 23:43 class: file has the id, cheap connected set does not), or an operator/debug path | 4.7–4.9 MB, at most once per dispute |

TTL / invalidation (Layer 0):

* Default TTL **20 s** for the connected set (status is frequent;
  credentials do not flap faster than that).
* A successful write, clear, or D5 completion **invalidates immediately**
  (do not wait out the TTL), applies an optimistic membership change,
  and runs Layer 1 inside the existing 20 s write timeout.
* Negative result ("togetherai not connected") is cached **10 s** so a
  failed verify does not hammer Layer 1.
* The 5-minute `catalogTTL` stays for the models.dev vendor *list*
  only. Writes bump the connected generation; they do not have to
  refetch 185 vendors.

Optimistic insert is not success. D1 still waits for Layer 1 (or
Layer 3 on dispute) before `ok`. The toast is never issued off
Layer 0 alone.

**D11 — 0085 remains the grok ACP record.** This one only adds
verify-after-write (D1) and the agent-chip rule (D3) to grok. A phone
xAI key still lands under quoted `[model."<default>"]`. `AuthStatus`
already reads that table. There is still no `provider credential set`
for grok on this host — until a phone write is proven, do not mark grok
key entry done.

**D12 — Goose and Codex stay on their 0074/0083 paths**, with D1
verification (file presence / `codex login status` file) and D3 chips.
Goose keyring hosts remain `keyring_managed` (0083 D6), not a silent
save.

### Consequences

* Good, because "Credential saved" becomes a statement about the agent's
  store and connected set, which is what unlocks models and turns.
* Good, because the kilo card can show configured when Gateway OAuth is
  live, instead of "needs setup" for the twelve vendors nobody asked to
  configure.
* Good, because the Headless/VPS xAI row stops starting a poll that
  cannot finish.
* Good, because a tap opens the device-code page; copy-paste is no longer
  the only path.
* Neutral, because Strategy B / host-loopback OAuth stays deferred. Those
  vendors remain visible and disabled.
* Neutral, because phone method ids (`kilo:0`, `xai:api`, …) do not
  change.
* Bad, because D1 adds a confirm after every write. D13 keeps that
  confirm on Layer 1 (123–353 KB), not 4.7 MB, except on a
  file-vs-engine dispute.
* Bad, because refusing a synthesised kilo-via-opencode API key is a
  behaviour change: the 23:43 write would now error instead of toasting.
  That is the point.
* Bad, because D6 needs a live authorize probe that must be cancelled
  and must not leave a half-open vendor flow on the operator's engine.

### Confirmation

1. Isolated `XDG_DATA_HOME`, kilo engine: `SetCredential(togetherai, togetherai:api, scratch)`
   returns OK iff Layer 1 (`GET /config/providers` ids) contains
   `togetherai` **and** `auth.json` has the id. The write path must
   **not** call `GET /provider` on this happy path (test spies the
   API). Deleting the scratch key restores both.
2. Isolated OpenCode home: `SetCredential(kilo, kilo:api, scratch)`
   returns `credential_not_accepted` or `method_unsupported`; `auth.json`
   is unchanged; `connected` does not gain `kilo`.
3. On this host's shape (kilo already in connected): starting kilo
   Gateway device auth must **not** return `ok=true` within one poll
   tick unless `expires` / token material changed.
4. Phone Providers card for kilo with two live upstreams and eleven
   missing catalog rows shows a configured chip and "2 credentials",
   not "Needs setup".
5. After (1), the kilo detail list contains a togetherai row without
   re-opening the catalog; the model picker lists togetherai under
   Connected.
6. xAI method 0 is visible, disabled, reason `browser_only`. xAI
   method 1 (Headless / Remote / VPS) is usable device OAuth. xAI
   method 2 accepts a key and passes (1) on an isolated home.
7. Device-flow sheet: tapping the URL launches the system browser;
   copy still works; a failed `ok=false` shows the error line and
   closes the wait spinner.
8. `go test -tags live_kilo,live_opencode` (opt-in write tests behind
   `MCREMOTE_LIVE_AUTH_WRITE=1`) fail if connected-set verification or
   the kilo-via-opencode refusal drift.
9. No new `provider credential set` info line is emitted unless D1 (2)
   passed. A failed verification logs at warn with provider, upstream,
   and no secret.
10. A unit test that stubs Layer 1 as matching the write never
    observes `GET /provider`. A unit test that stubs Layer 1 omit +
    disk present *does* observe one single-flight `GET /provider` and
    then `credential_not_accepted` if `connected` still omits the id.

## Pros and Cons of the Options

### O1 — Patch the kilo key write only

* Good, because it is a small diff if the only bug were "PUT never
  called".
* Bad, because this host's log shows the kilo *agent* never received a
  set-credential at all; the write that did fire landed in OpenCode's
  store and was ignored.
* Bad, because the agent chip, the model picker, and OAuth would still
  lie.

### O2 — Auth-completion contract

* Good, because it is the minimum that makes the three user requirements
  true: keys persist into a store the agent reads, the provider unlocks,
  and OAuth either finishes or is refused up front.
* Good, because it reuses 0074's messages and 0083's availability
  annotation; the wire stays additive.
* Neutral, because Strategy B is still a later MADR.
* Bad, because verify-after-write and catalog honesty need live pins;
  unit fixtures that stub `PUT` as success would hide the 23:43 class
  of bug.

### O3 — O2 plus Strategy B now

* Good, because SuperGrok-via-kilo browser, GitLab, Snowflake, and
  DigitalOcean would become reachable.
* Bad, because it gates the P0 key and device-code fixes behind reverse
  HTTP over the WS control plane — the reason 0074 deferred W3.
* Bad, because the failures on this host were a fake API-key row, a
  false "saved", a false Gateway completion, and an unopenable device
  URL, none of which need a tunnel.

### O4 — File-write everything, skip engine PUT

* Good, because the operator can `cat` the file and see the key.
* Bad, because 0074's OpenCode/kilo probe is that the *engine* PUT is
  what makes `connected` flip without a restart — when the body is a
  shape the engine accepts.
* Bad, because writing a type the engine ignores is how 23:43 happened.

## More Information

### Findings (code + this host)

| # | Finding | Evidence |
| --- | --- | --- |
| F1 | `set_credential` success is "engine returned 2xx", not "agent will use it" | `kilo/auth.go:126-131`, `opencode/auth.go:213-218`, `ws/server.go:2085-2097`; 23:43 log vs live `connected` |
| F2 | Synthesised `:api` methods skip `VerifyAPIKeyMethod`'s catalog check | `authcatalog.go:251-254` |
| F3 | kilo-the-vendor is absent from OpenCode `GET /provider/auth` (10 vendors). Catalog therefore invents `kilo:api` | live 2026-08-14; `BuildCatalog` |
| F4 | kilo-the-upstream on the kilo agent has **only** Gateway device OAuth | live `GET /provider/auth`; fixture `kilo/testdata/provider-auth-7.4.20.json:167-172` |
| F5 | Agent chip uses `worstAuthStatus` (`missing > configured`) over the whole methods catalog | `provider_status.dart:9-25`; kilo `AuthStatus` emits all 13 |
| F6 | Long-tail writes do not appear on the detail list | status = methods catalog ∪ `/config/providers`, not ∪ Layer 0 cache |
| F15 | `GET /provider` is 4.7–4.9 MB, `all` first, no ETag, no sparse fields | live 2026-08-14; D13 |
| F16 | `GET /api/provider/{id}` is ~300 B and **not** a connected oracle | live 200 for togetherai/kilo while unconnected |
| F7 | Device poll succeeds if the upstream was already configured | `deviceauth.go:131-135`; 18:32 kilo Gateway 5 s `ok=true` |
| F8 | "Headless / Remote / VPS" is classified `oauth_device` | `ClassifyCatalogMethod`; live xai method 1; 21:00 `ok=false` |
| F9 | Device URL is copy-only; `url_launcher` is not in `pubspec.yaml` | `device_flow_sheet.dart:7-10` |
| F10 | kilo has no `AuthFileWriterDialect`; a down engine cannot take a phone key | `httpagent.go:166-197`; kilo implements API writer only |
| F11 | No live kilo *write* test exists (opencode has `MCREMOTE_LIVE_AUTH_WRITE`) | `kilo/live_auth_test.go` is catalog/status only |
| F12 | Grok still logs 0085's prewarm warning; no phone grok key write in the log; `config.toml` has no `api_key` | `mcremote.err.log`; `~/.grok/config.toml` |
| F13 | Failed device flows log `ok=false` with no reason at info | `ws/server.go:2233-2248` |
| F14 | 0083 A8 / 0074 §14 still claim "plain API-key writes on kilo work" | contradicted by F1–F4 and this host |

### Surfaces that share the same state

A credential change has to be true on all of these, not just the settings
sheet:

* Settings → Providers card (D3)
* Settings → provider detail rows (D4)
* Add-credential catalog "Configured" band
* Chat model picker Connected group / "not configured" badge (D10)
* Active-upstream switcher (only configured ids, 0074 D14)
* Next session / prewarm (0085 for grok; engine connected for kilo/opencode)

### Verify-cost research (2026-08-14)

Sources that shaped D13, besides the live engine probe:

* [RFC 7232](https://datatracker.ietf.org/doc/html/rfc7232) / [MDN If-None-Match](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/If-None-Match) — 304 on unchanged representations. Engines do not send ETags today.
* [JSON:API sparse fieldsets](https://jsonapi.org/format/#fetching-sparse-fieldsets) — project a collection down to the fields a client needs. Engines ignore `?fields=`.
* [OpenCode server API](https://opencode.ai/docs/server/) — `GET /provider` is documented as `{all, default, connected}`; `config.providers()` is the small connected view the SDK already prefers.
* [OpenCode v2 Get provider](https://opencode.ai/v2/docs/api/provider/v2-provider-get) — `GET /api/provider/{id}` is a settings record, not auth membership (live-confirmed).
* [Go `json.Decoder` Token](https://pkg.go.dev/encoding/json#Decoder.Token) — stream-extract one field without retaining the rest. Helps heap on Layer 3; not bandwidth, because `connected` is last.
* Bloom filters ([Redis](https://redis.io/docs/latest/develop/data-types/probabilistic/bloom-filter/), Akamai one-hit caches) — rejected for this set size; exact map is the right structure.

### What stays out of this record

* 0074 W3 / Strategy B loopback tunnel.
* 0083 D6 goose OS-keyring writes.
* 0085 ACP `authenticate` selection (already proposed).
* Anthropic Pro/Max plugin OAuth (0074 D12).

### Related

* [0086-PLAN-phone-provider-auth-completion.md](./0086-PLAN-phone-provider-auth-completion.md)
  — implementation
* [0074](./0074-MADR-remote-provider-auth-from-phone.md) — protocol and
  per-agent stores; W1/W2/W5 "done" claims this record corrects
* [0082](./0082-MADR-settings-provider-menu-ux-overhaul.md) — hub / detail
  chrome; not reopened
* [0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md) — activation
  pass; D2/D3/D4 here tighten what 0083 thought it finished
* [0085](./0085-MADR-grok-acp-auth-method-wiring.md) — grok ACP handshake
  and quoted-table write
* [0075](./0075-MADR-kilo-cli-provider.md) — kilo engine as lead target
