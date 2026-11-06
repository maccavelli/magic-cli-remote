---
status: proposed
date: 2026-11-06
decision-makers: Project Owner
consulted: "this repository at 06322d51; acp-go-sdk2 v1.31.2, v1.33.0 and v1.34.0 from the module cache (helpers.go, types_gen.go, errors.go, schema/schema.json, docs/reports/0049-REPORT-release-notes-v1-32-0.md, docs/decisions/0050-MADR-auth-method-ids-and-the-prompt-result-for-issue-14.md); the grok wire captures under internal/provider/grok/testdata/wire; 0085-MADR-grok-acp-auth-method-wiring.md; three scratch probes, including an owner-approved initialize-only probe of grok 1.0.70 on 2026-11-06, listed under More Information"
informed: anyone who changes ACP authentication in internal/provider/acpagent, a provider Spec's auth fields, or the acp-go-sdk2 pin
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Authenticate only an id the agent advertised, using the SDK's list

## Context and Problem Statement

[0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)
moved `acp-go-sdk2` from v1.31.2 to v1.33.0 in `a115e40e` (F3f), and to v1.34.0 in `4b988bb0`
(F3g). v1.32.0, between them, added a client-side gate: `Authenticate` refuses a `methodId` that
the agent's `initialize` answer did not advertise, before anything is sent. v1.34.0 then exported
the gate's own list, `AuthenticateMethodIDs`, which the SDK's 0050 MADR (F10) suggests Magic use in
place of its own copy.

The owner asked for a record on adopting that helper. Investigating it found more than a
refactor. Magic's grok wiring, by
[0085-MADR-grok-acp-auth-method-wiring.md](0085-MADR-grok-acp-auth-method-wiring.md)'s D2 step 4,
sends `xai.api_key` when a key is present "even when that id was not advertised". The SDK now
refuses exactly that.

0217's F3f, on S8, said "Magic's auth selection picks from the advertised list". That is wrong
for this branch. F3g is right as written: it compares v1.33.1 and v1.34.0 with v1.33.0, which
already refused (F4), so they changed only the refusal's text. *Amended 2026-11-06. The
2026-11-05 draft counted F3g wrong as well.*

The question: which list of ids Magic authenticates from, and what Magic does when it holds a
key that the agent did not advertise.

### F1. Magic builds its own list, and selects from it

* `applyInitializeResponse` keeps the ids of `Agent` entries with a non-empty id, in order
  (`internal/provider/acpagent/acpagent.go:1480-1486`), as `s.advertisedAuth`.
* When a Spec names `SafeAuthMethodIDs`, `selectACPAuthMethod` picks from that list
  (`acpagent.go:716-735`; `authselect.go:22-38`):
  1. the configured pin, when it is advertised and safe;
  2. else the first id of the Spec's preference that is advertised and safe;
  3. else, **when a key exists, the Spec's key method, "advertised or not"**
     (`authselect.go:34-35`);
  4. else nothing, and the session is marked `authRequired`. Start then fails with
     `ErrACPAuthRequired`, whose text names the advertised ids (`authselect.go:40-45`,
     `acpagent.go:1007-1010` and `:1186-1189`). That is 0085's D7.
* Without `SafeAuthMethodIDs`, a configured `auth_method_id` is sent unchecked whenever anything
  is advertised (`acpagent.go:736-742`).
* Only grok sets the auth fields: safe ids `cached_token` and `xai.api_key`, that preference, key
  method `xai.api_key`, and `grokHasAPIKey` as the key check (`internal/provider/grok/grok.go:58-66`).
  `grokHasAPIKey` is true for a non-empty `XAI_API_KEY`, or a quoted `[model."<id>"] api_key` in
  grok's `config.toml` (`internal/provider/grok/auth.go:79-88`).

### F2. Why step 3 sends an unadvertised id

0085 measured grok 1.0.3 on 2026-08-13, with isolated homes (its table, lines 112-118):

| Process state | Advertised `authMethods` | `defaultAuthMethodId` |
| :--- | :--- | :--- |
| Cold, no credentials | `[grok.com]` | omitted |
| Cold + `XAI_API_KEY` in env | `[grok.com]`, yet `authenticate xai.api_key` succeeds | omitted |
| Cold + quoted model `api_key` in `config.toml` | `[xai.api_key, grok.com]` | `xai.api_key` |
| `auth.json`, no `XAI_API_KEY` | `[cached_token, grok.com]` | `cached_token` |
| `auth.json` + `XAI_API_KEY` | `[xai.api_key, cached_token, grok.com]` | `cached_token` |

So step 3 exists for one row: a cold host whose only credential is `XAI_API_KEY`. A key in the
config file is advertised, and so is an env key beside `auth.json`, so steps 1 and 2 cover them.

### F3. The protocol forbids it, and the SDK now enforces that

* ACP's schema, on `AuthenticateRequest.methodId`: "The ID of the authentication method to use.
  Must be one of the methods advertised in the initialize response." (`schema/schema.json:4948` in
  `acp-go-sdk2` v1.34.0.)
* The SDK's S8, released in v1.32.0: `Authenticate` with an id the answer did not advertise
  returns an error wrapping `acp.ErrUnadvertisedAuthMethod`, and a received one is answered
  `-32602` (`docs/reports/0049-REPORT-release-notes-v1-32-0.md:64-69`). The sentinel is
  `errors.go:188`. A terminal method's id meets `ErrTerminalAuthMethod` first (`errors.go:180`).
* In v1.34.0, by its 0050 MADR, one generated function is both the gate's rule and the exported
  list: `acceptedAuthenticateRequestMethodIds` (`types_gen.go:25449`), which the gate calls
  (`types_gen.go:25478`). `AuthenticateMethodIDs` (`helpers.go:787`) returns it. The refusal's text
  now ends with the accepted ids.

### F4. Proven: Magic's real start path refuses step 3 since `a115e40e`

Probe 1 (`libs/authprobe.py`) adds one test to package `acptest` on scratch copies of three
commits. It runs Magic's real `acpagent` provider, through the real SDK client, against the
fixture agent. The Spec is grok's auth wiring (F1) with a key present. The agent advertises only
`grok.com`, as F2's env-key row recorded.

| Commit | `acp-go-sdk2` | `StartOn` error | What the agent received, in order |
| :--- | :--- | :--- | :--- |
| `e2d6493b` | v1.31.2 | none | `initialize`, `authenticate` with `"methodId":"xai.api_key"`, `session/new` |
| `a115e40e` | v1.33.0 | `acp authenticate (xai.api_key): authenticate: methodId "xai.api_key": acp: methodId names no auth method the agent advertised` | `initialize` |
| `06322d51` (`HEAD`) | v1.34.0 | the same, ending `; authenticate accepts "grok.com"` | `initialize` |

So since `a115e40e` (2026-11-05), a cold grok host whose only credential is `XAI_API_KEY` cannot
start a session. The fixture agent stands in for grok, so this proves Magic's side and the SDK's.
Grok's side is F5, which shows that grok 1.0.70 still advertises as F2 recorded.

### F5. Probed: grok 1.0.70 still does not advertise an env-only key

*Amended 2026-11-06.* On 2026-11-05 this finding was open. Grok 1.0.70's cold env-key answer had
not been recorded:

* Magic pins grok `KnownGoodVersion = "1.0.70"` (`internal/provider/grok/version.go:23`), but F2
  measured 1.0.3.
* The committed 1.0.70 capture (`testdata/wire/1.0.70/frames.jsonl`) came from a host with a key
  in its config, F2's third row.
* `TestLiveColdInitializeOnlyGrokCom` and `TestLiveQuotedModelKeyAdvertisesAPIKey`
  (`internal/provider/grok/live_auth_test.go`) never set `XAI_API_KEY`.

The owner approved D5's probe on 2026-11-06, and it has now been run.

Probe 3 (`libs/grokprobe.py`) adds one live-tagged test to package `grok_test` on a scratch copy
of `06322d51`. The test uses the committed helpers `isolateGrokHome`, `startACPInit` and
`initAuthMethods`. Each state starts the installed grok, `1.0.70 (f50904eaee6c) [stable]`, in a
fresh isolated home, and sends only `initialize`. The key, wherever one appears, is the
placeholder `xai-probe-not-real`. Each state also reports whether the running grok process's
environment holds the placeholder, read with `ps eww` on its pid.

| State | Placeholder in grok's environment | Advertised ids | `defaultAuthMethodId` |
| :--- | :--- | :--- | :--- |
| cold, no credential | no | `grok.com` | omitted |
| cold, `XAI_API_KEY` in env | **yes** | **`grok.com`** | omitted |
| cold, quoted `[model."<id>"] api_key` in `config.toml` | no | `xai.api_key`, `grok.com` | `xai.api_key` |
| cold, `XAI_API_KEY` in env, and quoted `[model."<id>"] env_key = "XAI_API_KEY"` | yes | `xai.api_key`, `grok.com` | `xai.api_key` |
| cold, that `env_key` table, `XAI_API_KEY` unset | no | `grok.com` | omitted |

* The probe ran in three batches, adding states as it went:
  * the first three states ran six times;
  * the `env_key` table with the variable set ran three times;
  * the `env_key` table without it ran once.

  The environment check was added after the first batch, so it covers every state's runs after
  that one. Every run of a state gave the same answer.
* The `xai.api_key` entry's own description reads `XAI_API_KEY or api_key/env_key in config.toml`.
  On a cold host, though, the environment variable alone does not make grok advertise it.
* So F4's failure reaches a real grok 1.0.70 user on a cold host whose only credential is
  `XAI_API_KEY`. F2's row still holds at 1.0.70.
* An `env_key` table naming `XAI_API_KEY` makes the env key advertised, and only while the
  variable is set. A key in the environment can therefore be kept there. That gives D3 a remedy
  besides moving the key into the file.

What the probe does not establish:

* **Whether `authenticate` with a real key succeeds.** No `authenticate` was sent, so nothing
  reached xAI. This shows what grok advertises.
* **Whether one model table is enough.** The config states wrote tables for both `grok-4.5` and
  `grok-4.6`, as the committed `TestLiveQuotedModelKeyAdvertisesAPIKey` does, because the current
  model id is not known before `initialize`. Whether a table for one model suffices was not
  probed.
* **The `env_key` table shape.** It is read from grok's own description and error text (0085,
  lines 182 and 381), not from grok's documentation.
* **Other hosts and states.** The probe ran on macOS only. The `auth.json` rows of F2 were not
  re-probed, since they need a grok sign-in.

### F6. Nothing that is released has it

`v0.24.0` (`5ca4a3b1`, 2026-10-30) pins `acp-go-sdk2` v1.26.0, and `a115e40e` is not an ancestor of
it (`git merge-base --is-ancestor`: exit 1). Every commit since `a115e40e` on `master` has it.

### F7. Magic's list and the SDK's agree on what grok sends, and differ elsewhere

Probe 2 (`idsprobe/`, a scratch module on v1.34.0) copies F1's loop verbatim and compares it with
`acp.AuthenticateMethodIDs` on the same decoded answers:

| Answer | Magic's list | The SDK's list |
| :--- | :--- | :--- |
| grok 1.0.70's capture | `xai.api_key`, `grok.com` | the same |
| F2's cold env-key row | `grok.com` | the same |
| a terminal id that an agent entry also names | `a`, `t` | `a` |
| an entry of an unknown type with a string id | none | `u` |
| an unknown type without an id | none | none |
| one agent id twice | `a`, `a` | `a` |
| an agent entry whose id is empty | none | `""` |

* Grok's entries carry no `type`. The SDK decodes an untyped entry with `id` and `name` as an
  `Agent` (`types_gen.go:851-866`), so F1's loop sees them.
* Where the lists differ, Magic's selects an id the gate refuses (the terminal one), or leaves out
  one it accepts (the unknown type). No agent Magic drives sends any of those shapes today.

### F8. Nothing tests step 3 against the SDK

* `TestSelectACPAuthMethod`'s case "cold+file key" asserts that the selection returns the key
  method when only the browser method is advertised (`authselect_test.go:34`). It calls the
  selection function alone, so the SDK's refusal never runs. By F2 a file key is advertised, so the
  case's name does not match the state it stands for, a cold env-only key.
* `acptest`'s own test of authentication advertises the method it selects
  (`acptest/acptest_test.go`, `TestStartOnAuthenticatesWithTheSpecsMethod`).
* So `make race` stayed green across `a115e40e` and `4b988bb0`, while F4's path broke.

## Decision Drivers

* **Magic sends what the protocol allows.** F3's rule is the schema's, and the SDK enforces it.
* **One rule, not two copies of it.** A list built apart from the gate already differs from it
  (F7).
* **A start that cannot succeed fails clearly, with the reason and the remedy.** The SDK's refusal
  names the accepted ids, but not why Magic chose an id outside them.
* **Each claim about an engine's behaviour is pinned by a test of that behaviour** (AGENTS.md: "When
  a decision rests on how an external CLI behaves, record the probe evidence in the MADR and pin it
  with a live-tagged test").
* **Nothing released regresses unnoticed.** F6: the next release would carry F4.

## Considered Options

* **A** — Select from the SDK's list, use the key method only when it is advertised, and fail as
  D7 does otherwise, naming the key. Pin grok 1.0.70's cold env-key answer with a live test.
* **B** — Keep Magic's list, and only drop the "advertised or not" branch.
* **C** — Keep step 3 by sending `authenticate` around the SDK's gate (a raw or extension call).
* **D** — Pin `acp-go-sdk2` back to v1.31.2.
* **E** — Do nothing.
* **F** — A, and also have Magic add an `env_key = "XAI_API_KEY"` table to grok's config when it
  finds only an env key, so that grok advertises it (added 2026-11-06, from F5).

## Decision Outcome

Chosen option: **"A"**, proposed. It brings Magic's selection inside the protocol's rule, with the
SDK's own list as the single statement of that rule, and turns F4's opaque start failure into
D7's failure with its cause named. F5's live probe settled that the cold env-key case still exists
on grok 1.0.70, and it found a remedy that keeps the key in the environment.

The owner decided D5 (run the probe) and D7 (hold releases) on 2026-11-06. The rest is proposed.

### The decisions

* **D1 — The advertised list is `acp.AuthenticateMethodIDs(initResp.AuthMethods)`.**
  `applyInitializeResponse` stores it in `s.advertisedAuth`, as strings, in place of F1's loop.
  Selection, D7's error text, and anything else that reads `advertisedAuth` then use the gate's
  rule. By F7 nothing changes for grok's answers.
* **D2 — The key method is chosen only when it is advertised.** `selectACPAuthMethod` loses
  "advertised or not". With a key present and the key method not advertised, nothing is
  authenticated, and the session is `authRequired` (0085 D7). This supersedes 0085's D2 step 4.
  * The `hasKey` argument stays, for D3's text only.
* **D3 — D7's error says why a present key was not used.** When `authRequired` is set while a key
  is present, the error still wraps `ErrACPAuthRequired`. Its text keeps the advertised ids, as
  today, and adds two things:
  * a sentence from `acpagent`, which names no vendor, saying that a key is present and the
    agent did not advertise the Spec's `KeyAuthMethodID`;
  * a remedy from a new Spec field, `KeyNotAdvertisedHint`, appended only when the field is set.
  * The remedy lives in the Spec because `acpagent` names no vendor
    ([0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md)
    D2: "Auth method selection becomes a Spec policy"). That rule is enforced:
    `TestNoProviderIsNamedInAcpagent` (`acpagent/neutral_test.go`) fails on `grok`, `xai` or `x.ai`
    anywhere in `acpagent`'s files, its tests and subpackages included.
  * grok's hint names the three remedies F5 and F2 show:
    1. an `env_key = "XAI_API_KEY"` line in a `[model."<id>"]` table of grok's `config.toml`;
    2. setting the key from the phone, which writes it into that file (0085 D4);
    3. a grok sign-in.
  * No secret appears in the text. `rejectUnauthenticated` needs to know whether a key was present,
    so the session keeps D2's `hasKey`.
* **D4 — A test through the real SDK pins D2.**
  * **The `acptest` test.** A Spec shaped like grok's, with a key present, runs against an agent
    that advertises only the browser method. `StartOn` fails with `ErrACPAuthRequired`, and the
    agent receives `initialize` and nothing else.
  * The test uses neutral ids, `vendor.key` and `vendor.browser`, as `authselect_test.go`
    already does, because `TestNoProviderIsNamedInAcpagent` covers `acptest` too.
  * A second case advertises `vendor.key`. There `StartOn` authenticates with it and reaches
    `session/new`.
  * **The selection case.** `TestSelectACPAuthMethod`'s "cold+file key" case becomes
    "cold+unadvertised key", and expects no method.
  * Each is seen failing first, on a scratch copy:
    * the refusal case and the renamed selection case on the code before D2;
    * the advertised case on a copy whose selection returns no method, since it passes both
      before and after D2.
* **D5 — Live tests pin grok 1.0.70's answers that D2 and D3 rest on.** The owner approved the probe
  on 2026-11-06. It has run, and its result is F5.
  * Three live-tagged tests go beside `TestLiveColdInitializeOnlyGrokCom`. Each uses an isolated
    home, the placeholder key, and `initialize` only:
    1. **env key only:** grok advertises `grok.com` alone, with no default. This is the case D2
       refuses.
    2. **env key and an `env_key` table:** grok advertises `xai.api_key`, with default
       `xai.api_key`. This is D3's first remedy.
    3. **the `env_key` table with the variable unset:** grok advertises `grok.com` alone. This
       shows the table counts only with the variable set.
  * If a later grok advertises an env-only key, test 1 fails. D2 is still correct then, since it
    picks the key method once it is advertised, but D3's hint and this record are revised.
  * They run in `make live-grok` with no Makefile change, since `live-grok-rest` runs every live
    grok test outside the discrimination set (`Makefile:386-387`).
  * They spend no provider quota, since nothing is authenticated.
* **D6 — The records are corrected.**
  * 0217's F3f bullet on S8 ("Magic's auth selection picks from the advertised list") gets a
    dated correction pointing at F4.
  * F3g is left as it stands, since it is correct (Context).
  * 0085's D2 step 4 is marked superseded by D2.
* **D7 — No release from `master` until D2 and D3 land.** The owner decided this on 2026-11-06:
  "Hold the release". F6: the next release carries `a115e40e`, and F5 shows the failure reaches a
  grok 1.0.70 user. The hold ends with the commit that lands D1 to D4 (the PLAN's P1).

### Consequences

* Good, because Magic never sends an `authenticate` the schema forbids, and its list cannot drift
  from the gate's.
* Good, because F4's failure becomes D7's, which names the cause and the remedy.
* Good, because the case that broke unnoticed (F8) is then tested where it breaks, through the
  SDK, and the engine behaviour it rests on is pinned live (D5).
* Neutral, because for every answer grok is known to send, the list is the same (F7).
* Bad, because a cold host whose only credential is `XAI_API_KEY` cannot authenticate it.
  * F5 shows that grok 1.0.70 does not advertise that key.
  * That host started sessions at `v0.24.0`. It now needs one line of grok config
    (`env_key = "XAI_API_KEY"`), the key set from the phone, or a sign-in. D3's text says which.
  * Option C is the only way to keep it with no change on the host, and C breaks the protocol.
* Neutral, because D5's live tests start grok, but send only `initialize`, so they spend no
  provider quota.

### Confirmation

* D4's tests, each seen failing first on a scratch copy:
  * on the code before D2, the SDK-path refusal case fails with the SDK's
    `ErrUnadvertisedAuthMethod`, not `ErrACPAuthRequired`;
  * on the code before D2, the renamed selection case returns the key method;
  * on a copy whose selection returns no method, the advertised case fails.
* After D1 and D2, F4's probe on the changed tree gives `ErrACPAuthRequired`, and the agent
  receives `initialize` only.
* F7's probe, re-pointed at the new `advertisedAuth`, gives the SDK's list in every row.
* D3's text, from the refusal case, names the key method and grok's hint, and carries no key.
* `TestNoProviderIsNamedInAcpagent` passes.
* D5's three live tests pass on this host with grok 1.0.70. Each is seen failing on a scratch copy
  whose state is swapped with another test's.
* `make race`, the Linux test host's `go test -race ./...`, and `make ci-windows`.

## Pros and Cons of the Options

### A — The SDK's list, the key only when advertised, D7 otherwise (chosen)

* Good, because selection and gate are one rule (D1), and the rule is the protocol's (F3).
* Good, because the result for each state is defined and tested (D4), and the engine's side is
  pinned (D5).
* Bad, because the env-only cold host loses what it had at `v0.24.0`, since 1.0.70 behaves as 1.0.3
  did (F5). It is told how to get it back, in one config line (D3).

### B — Magic's list, without the unadvertised branch

* Good, because it is the smallest change that stops F4.
* Bad, because F7's differences stay: a terminal id that an agent entry also names is selected and
  then refused, and an unknown type's id is never selected though the gate allows it.
* Bad, because the SDK's 0050 MADR names exactly this copy as the drift its helper removes.

### C — Send `authenticate` around the gate

* Good, because the env-only cold host keeps working, if grok still accepts it.
* Bad, because it sends what the schema forbids (F3), on purpose, through a side door the SDK does
  not offer for it.
* Bad, because it relies on grok accepting a request the protocol says it may refuse, and any
  grok release could start refusing it.

### D — Pin back to v1.31.2

* Good, because F4 goes away at once.
* Bad, because it gives up v1.32.0 to v1.34.0, including v1.33.1's notification-queue fix, which
  reaches Magic's `OverflowBlock` connections (0217 F3g).
* Bad, because 0217's rule moves every pin to current. It only defers the decision.

### E — Do nothing

* Good, because nothing changes.
* Bad, because F4 ships with the next release, as an SDK refusal that does not say why Magic chose
  that id.

### F — A, and Magic writes the `env_key` line itself

* Good, because the env-only cold host keeps working with no action from the user, inside the
  protocol: grok then advertises the key (F5).
* Bad, because Magic would write grok's config at session start, unasked. Today it writes that
  file only on the phone's key write (0085 D4), which the user starts.
* Bad, because F5 leaves open whether one model table is enough. Magic would have to write a table
  for every model, or guess the current one before `initialize`.
* Bad, because it is a new write path into a file Magic does not own. That is a decision of its
  own, which can follow this record if the owner wants it.

## More Information

### The owner's answers, 2026-11-06

The 2026-11-05 draft asked two questions:

1. **D5's live probe:** start grok 1.0.70 with an isolated home and a placeholder `XAI_API_KEY`,
   sending `initialize` only. Run it? The owner answered "Yes run it". The result is F5.
2. **D7's hold:** hold releases from `master` until D2 and D3 land, or accept F4 for a release
   before then? The owner answered "Hold the release". That is D7.

### Evidence index

Every probe ran on scratch copies or in a scratch module, never in the tree. Scripts are in the
session scratchpad, under `libs/`.

* **Probe 1** (`libs/authprobe.py`, logs `libs/authprobe-<rev>.log`): F4's table. The first run
  printed `acptest`'s `Methods()`, which leaves out the handshake, so it showed nothing for any
  commit. It was changed to print every request in order, and re-run. The table is the re-run.
* **Probe 2** (`idsprobe/main.go`, `go run .` on `acp-go-sdk2` v1.34.0): F7's table.
* **Probe 3** (`libs/grokprobe.py`, logs `libs/grokprobe-<n>.log`): F5's table.
  * `go test -tags live_grok -run '^TestZProbe0234$'` on a scratch copy of `06322d51`, against the
    installed grok 1.0.70. Every run exited 0, and the copy was removed afterwards.
  * The script fails if the placeholder appears in any logged probe line. None did.
* **Release check:** `git show v0.24.0:go.mod` (v1.26.0); `git merge-base --is-ancestor a115e40e
  v0.24.0`, exit 1; `git log -G 'acp-go-sdk2 v' -- go.mod` for the pin's history.
* **Captures:** `internal/provider/grok/testdata/wire/1.0.70/frames.jsonl` (two methods, default
  `xai.api_key`); `1.0.13/frames.jsonl` (three methods, default `cached_token`).

### Related records

* [0234-PLAN-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md](0234-PLAN-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md):
  the implementation.

* [0085-MADR-grok-acp-auth-method-wiring.md](0085-MADR-grok-acp-auth-method-wiring.md): D2 step 4,
  superseded by D2 here; D4, the phone's key write; D7, the auth-required failure.
* [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md):
  F3f, corrected by D6.
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  adopting the SDK and offloading protocol handling to it, of which D1 is one more step.
