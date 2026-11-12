---
status: proposed
date: 2026-11-10
decision-makers: Project Owner
consulted: "this repository at 912b9aa1 (master), where the pin commit of 0217-PLAN-update-every-pin-to-its-current-release.md landed (acp-go-sdk2 v1.35.0, go-llmprovider-sdk v1.29.0), read from code; first drafted at 995d442b, and every file this record cites for auth is identical at both; 0179-PLAN-gobble-native-acp-provider.md P4's unstaged files, none of which this record reads for auth; two independent reviews of these drafts, 2026-11-10; docs/reports/0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md and the Magic audit behind it; the owner's answers of 2026-11-10 to that report's Q1-Q22 and to the drafts' open questions; go-llmprovider-sdk's proposed 0073 to 0076 pairs (untracked in that repository) and its v1.28.0 and v1.29.0 module sources; gobble-cli's proposed 0051 and 0052 pairs (untracked there), its tags v0.5.0, acpext/v0.2.0, acpext/v0.3.0 and acpext/v0.4.0, read with git show; acp-go-sdk2 v1.35.0 read from the module cache"
informed: "go-llmprovider-sdk, which publishes the auth contract module; gobble-cli, which relays its sign-in steps through ACP elicitation to this repository"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Take provider auth types from the SDK's auth contract, relay every sign-in as contract steps, and cut the daemon and the app over together

## Context and Problem Statement

[0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md](../reports/0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md)
found that signing in without an API key is built three times, in three vocabularies, around one data shape, the
device code. Its Magic findings (F4, F5, F9 to F11, F14 to F16, F28, F34 to F51, F57, F60) and its ledger M1 to M23 are
this record's input. The report decides nothing. The owner answered its questions on 2026-11-10, and the answers bind
here:

* **Q8, the contract module:** "stdlib-only, nested in the SDK repo with its own tags. SDK, gobble and Magic all use its
  types. Magic embeds them in its own envelope, superseding Magic 0185-MADR in part."
* **Q7, ACP elicitation now:** gobble signs in "inside ACP `authenticate` through request-scoped
  `elicitation/create`", which needs "an amendment to Magic 0191-MADR D1/D6". This is against the report's
  recommendation, which was a CLI answer channel.
* **Q9, Dart models:** "generated from the contract's JSON Schema, with a generator and a drift gate".
* **Q15, one error vocabulary:** "the contract's `ErrorKind` everywhere".
* **Q19, the live debt:** `0074-PLAN-remote-provider-auth-from-phone.md` P22 steps 5 and 6, and `0179-PLAN-gobble-native-acp-provider.md` P6, are "re-scoped into the new Magic pair's
  acceptance, run once against the new design". This is against the report's recommendation.
* **Q20:** "refuse a mismatch at connect, with a clear 'update the app' message".
* **Old phones do not matter.** "The daemon and the app change in lockstep, with one clean cut-over and no
  compatibility shims."
* **Standing:** Magic "describes methods, relays sign-in steps, returns answers and reports status. Magic-side flow
  logic, heuristics and hard-coded id tables are code creep to remove."
* **Later answers on the gobble drafts** (2026-11-10, all as recommended): QB (a), acpext's `ErrorKind` and
  `RetryCause` take the contract's spellings at `acpext/v0.5.0`, released in lockstep with this cut-over; QC (i),
  acpext publishes the provider list only, and per-method facts come from the contract; QD (a), no ACP `logout`; QE (a),
  gobble's ACP `authenticate` runs the SDK's `signin.Start`.
* **The owner's answers to this record's own open questions** (2026-11-10; the options as put are under More
  Information):
  * **O1, a method whose kind is known only at start:** "A contract value: the SDK contract gains an `oauth` kind and
    an `at_start` completion (or equivalent) before `authcontract/v0.1.0` is tagged. The phone shows 'a sign-in whose
    type is learned on start'." As recommended.
  * **O2, a fresh gobble sign-in when a credential exists:** "A gobble flag: amend gobble 0051 so `authenticate`
    accepts `_meta.gobble.fresh` and runs the sign-in even when a credential exists. The old credential is replaced
    only if the new one saves." As recommended.
  * **O3, the command for a host-only method:** "A contract field: a typed `command` field on the contract's
    `Method`, before `authcontract/v0.1.0`. The phone shows it with a copy button." **Against the recommendation,**
    which was `Method.detail`.
  * **O4, whether Magic advertises `elicitation.url` for gobble's sign-in:** "Form only. gobble sends `open_url` and
    `show_code` as form text with `_meta.gobble.step`; the phone renders each step from it; 0191's URL trust rules are
    unchanged." As recommended (D8).
* **The owner's answer of 2026-11-10 on timing (DF):** the standalone defect fixes, go-llmprovider-sdk's 0076,
  [0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md) P23 and
  [0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P7, land "now, ahead of the
  redesign", each on its own once its record is accepted.

Q13, the pasted-callback replay, is
[0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md).
Q14 and Q18 are defects fixed before the cut-over, inside their own records: Q14 in [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) A9, Q18 in [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md)'s
amendment of 2026-11-10.

### What the daemon has at `912b9aa1`

Every line below was re-read at `HEAD`, `912b9aa1`. The commits since `995d442b`, where this record was first drafted,
change `go.mod`, `go.sum`, `ci.yml` and the 0217 pair only, so every cited line is the same at both. None of these files
differs in the working tree.
*2026-11-11: `HEAD` is `bcde74ab` (0074 §16 accepted, P23 step 0), and P23 step 2 is staged for the next commit; it
changes `internal/provider/httpagent/authcatalog.go`, `deviceauth.go` and `internal/provider/opencode/acp_auth.go`, so
lines cited in those three files move (D7's `host_oauth` literal at `authcatalog.go:259-260` is at `:285`). This
section is re-anchored once P23's commit series lands, before P1 starts; nothing else cited has changed.*

* **The wire is device-code-shaped.** Five auth messages: `provider.start_auth`, `oauth.device_flow`,
  `oauth.device_flow_result`, `oauth.device_flow_update` and `oauth.cancel` (`internal/protocol/messages.go:112-116`).
  `DeviceFlowPayload.UserCode` is required (`:925`), and `Classify` refuses a flow without a code
  (`internal/providerauth/classify.go:73-85`). The only phone-to-daemon message into a running flow is `oauth.cancel`
  (`messages.go:958-960`). Methods have three types, `api_key`, `oauth_device` and `oauth_browser` (`:705-712`), and
  four unavailability reasons, `keyring_managed`, `browser_only`, `device_unsupported` and `host_oauth` (`:782-787`).
* **The daemon annotates centrally.** `upstreamAuthPayload` marks every `oauth_browser` method `browser_only` and every
  device method `device_unsupported` when the transport is not wired (`internal/ws/server.go:2380-2397`).
* **Two lifecycles.** A provider that implements `OwnedDeviceAuth` (codex, grok) reserves admission first and is owned
  by one goroutine (`internal/ws/provider_auth_owned.go:26-99`, `internal/providerauth/reservation.go:166-184`). Every
  other one (kilo, opencode, gobble) starts its flow under `context.WithoutCancel` and is admitted afterwards
  (`server.go:2649-2669`); its result goes to the socket that started it (`:2712`).
* **Results for an absent phone are retained and never delivered.** `Registry.retain` keeps at most two per device
  (`registry.go:286-297`); `PendingResults` (`:301-307`) has no non-test caller.
* **The owned reservation's expiry is reported and not enforced.** `expiresAt` is reserve time plus `DefaultTTL`, 15
  minutes (`registry.go:21`, `:255`), sent as `expires_in` (`provider_auth_owned.go:88`). The owner goroutine waits on
  `context.Background()` (`reservation.go:180`). `ActivationGrace`'s comment says "The original flow deadline still
  caps it" (`bounds.go:36-38`), and there is none.
* **gobble's phone sign-in runs its CLI.** `gobble auth login <id> --method device --json`
  (`internal/provider/gobble/auth.go:321-385`); a device method wherever acpext marks `Login`
  (`:160-164`); grok's browser row by id (`:165-171`).
* **Magic cancels what gobble's elicitation sign-in sends.** `CreateElicitation` answers URL mode `cancel`
  (`internal/provider/acpagent/elicitation.go:34-36`) and a form with no session `cancel`, declined as
  `elicitation.request_scope` (`:45-48`). `CompleteElicitation` is method-not-found (`:112-114`).
* **The phone guesses.** `isUsable => available && !isBrowserOAuth` (`apps/mobile/lib/data/protocol/models.dart:1522`);
  `destructive = providerId == 'codex'` (`apps/mobile/lib/features/settings/provider_detail_screen.dart:581`); the start
  push is matched by provider id with a 60 s timeout (`:609-612`); a result is read for `ok` and `error` only
  (`:644-649`); the removal copy is gated on codex (`:726-729`). `pubspec.yaml` lists no code generator.
* **The SDK may be imported only by grok, and grok only two of its packages.** `TestSDKIsImportedOnlyByGrok`
  (`internal/provider/grok/sdk_boundary_test.go:32`) fails any import whose path starts with
  `github.com/maccavelli/go-llmprovider-sdk` (`sdkModule`, `:17`; the prefix test, `:72`) outside
  `internal/provider/grok` (`:76-77`). Inside grok it allows `llmprovider` and `llmprovider/catalog` alone (`sdkAllowed`,
  `:23-26`; the refusal, `:78-79`), and in grok's production files only four names from them (`:100`). The contract
  module's path, `github.com/maccavelli/go-llmprovider-sdk/authcontract` (go-llmprovider-sdk
  `docs/decisions/0073-MADR-publish-an-auth-contract-module.md` D1), has that prefix, so the test refuses it in every
  package, grok's mapper included. Neither the report nor the SDK draft names this gate.
* **Version negotiation is protocol numbers only.** `NegotiateVersion` picks the highest of V1 and V2
  (`messages.go:17-31`, `:47-59`), settled in `handleAuth` before the token is checked (`server.go:1172-1179`). Nothing
  compares the daemon's and the app's builds.

### Can a sign-in run for about ten minutes through Magic's ACP path? Not today

The SDK's own flows run up to 10 minutes for a browser sign-in (`llmprovider/auth/oauth_loopback.go:25`,
`oauthBrowserTimeout`), 15 for OpenAI's device code (`oauth_device.go:24`) and 10 for Kilo's by default
(`kilo_device.go:29`), each at go-llmprovider-sdk v1.29.0. gobble's elicitation sign-in holds Magic's `authenticate`
request open for that long (gobble-cli `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md`
D4, its consequence "`authenticate` can now run for minutes"). Every timeout on the path Magic has, with what it does
to a ten-minute sign-in:

| # | Where | Bound | Effect |
| :--- | :--- | :--- | :--- |
| T1 | `acpagent.go:41-43`, `:758`, `:789`, `:801` | `startTimeout`, 30 s (or `Spec.StartTimeout`, `:90-93`) | The only `authenticate` Magic sends is inside the session handshake, under `initCtx`. It is cancelled at 30 s. |
| T2 | `server.go:1003`, `:1031`, `:1144-1145` | the async op deadline, 30 s for `provider.start_auth` (default branch), from `c.lifeCtx` | Any work bound to the request's context dies at 30 s, or at once on a disconnect. |
| T3 | `elicitation.go:45-48` | none: refused | A request-scoped form is answered `cancel` at once. |
| T4 | `extensions.go:152-157`, `internal/provider/gobble/section.go:26` | `PermissionTimeout`, 120 s for gobble | A form routed through the question flow is cancelled after 2 minutes. |
| T5 | `server.go:651-652`, `internal/ws/resume.go:32-35`, `internal/config/config.go:569`, `internal/providerauth/registry.go:312-326` | the resume window, 120 s by default, for owned flows only | An owned sign-in (codex, grok) whose phone disconnects is cancelled 2 minutes later: `DetachDevice` arms the window on `r.reservations` alone. A legacy flow (kilo, opencode, and gobble today) is not detached: it runs on, and its result is written to the socket that started it (`server.go:2712`), which is gone, so it is lost. A user who leaves the app for the browser may lose the socket (unverified, probe P-B). |
| T6 | `registry.go:21`, `:114-120` | `DefaultTTL`, 15 min | A legacy flow's context ends at the provider's expiry, capped at 15 minutes. |
| T7 | acp-go-sdk2 v1.35.0 `connection.go:23`, `:2668-2694`, `:2494-2518` | none per request | The SDK imposes no request timeout: an outbound request waits for its context, an inbound handler's context ends on `$/cancel_request` or the connection's end. The 5 s constant is the shutdown drain. |

So the path needs a new home for `authenticate` (T1, T3, T4), a context that outlives the request (T2), and a
disconnect rule for sign-ins (T5). D5 and D8 set them.

### What the upstream drafts give

* go-llmprovider-sdk `docs/decisions/0073-MADR-publish-an-auth-contract-module.md` (proposed): the module
  `github.com/maccavelli/go-llmprovider-sdk/authcontract`, standard library only, tagged `authcontract/vX.Y.Z` from
  `v0.1.0` (D1); `Method`, `Consent`, `Link`, `Field`, `Step`, `Answer`, `Failure` and the open enums `Kind`,
  `Completion`, `Paste`, `Yields`, `ClientIdentity`, `StepKind`, `LinkRel`, `FieldType`, `ErrorKind` (D2, D3); a
  generated schema, `authcontract/authcontract.schema.json`, with no `oneOf` and `"x-open-enum": true` on each enum
  (D4); golden vectors in `authcontract/testdata/vectors/` and `forward/` (D5); additive-only rules, a reader refusing a
  higher `schema_version` (D6). By the owner's O1 and O3 it also has, before `v0.1.0`, `KindOAuth` (`"oauth"`) and
  `CompletionAtStart` (`"at_start"`) for a method whose mechanism its first steps reveal, with `YieldsUnknown` and
  `ClientUnknown` so such a method states no fact Magic lacks, and `Method.command` for a host-only method's command
  (D2). Its schema marks each pointer member `"x-nullable": true`, so a generator knows from the schema alone that
  `Answer.confirm` `false` is written (D4). A step marks a secret answer with `Step.secret`; `Answer.secret` is the answered
  step's, set by the reader that holds the step; `Answer`'s `String`, `GoString` and `LogValue` mask `Paste` and every
  field value unless `secret` is a non-nil false (D2, D6 rule 5). It names no ACP mode: the mapping is gobble's.
* go-llmprovider-sdk `docs/decisions/0074-MADR-one-sign-in-handle-and-method-facts.md`: the method facts per provider
  (D1's table) and `signin.Start` (D3). Its D11: "The mappings from codex, grok, kilo and opencode into the contract's
  steps stay in magic-cli-remote."
* gobble-cli `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4: each agent
  auth method carries `_meta.gobble.methods`; `authenticate` takes `_meta.gobble.method`, and `_meta.gobble.fresh: true`
  to sign in whatever is stored, the old credential replaced only if the new one saves (the owner's O2); each step is
  a request-scoped elicitation carrying `_meta.gobble.step`, the `authcontract.Step`; a client that offers
  `elicitation.form` only gets every step as a form (its D4 table's right-hand column), and an `open_url` or `show_code`
  form is acknowledged with an empty `accept`, never answered, the paste following as its own `paste` form; `done` is `authenticate`'s `{}`
  and `failed` its `auth_required` with `ErrorData.Kind`. Its U8 found that acp-go-sdk2 refuses a form unless the
  client advertised `elicitation.form` and URL mode unless it advertised `.url`. Its D1 amends gobble's own import
  boundary for the contract: archtest's rule 4 (`internal/archtest/check.go:308` in gobble-cli) admits `authcontract`
  in `acpserver` and `internal/cli`, and rule 1 (`:249`) keeps refusing it in the core packages, with a new test case.
* gobble-cli `docs/decisions/0052-MADR-finish-the-chatgpt-plan-sign-in-on-the-sdk-engine.md` D8: `auth status --json`
  gains `sessionKind` and `consents`, and an `oauth` entry the resolver does not accept is `usable: false` with
  `reason: "oauth_unused"`.

*2026-11-11, the owner's rulings of that day on go-llmprovider-sdk's 0074, 0075 and 0076 drafts, relayed by the
coordinator of the four-repository auth-contract work and checked here against this record, its PLAN and
`0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`. The SDK's and gobble-cli's records were being
edited the same day, so each is cited by its decision, and what it finally says is that repository's. Nothing below
changes a decision of this record.*

* *`(*OAuthSession).SessionKind()` is exported (go-llmprovider-sdk 0074 D5). gobble's `auth status --json`
  `sessionKind` rests on it (gobble-cli 0052 D8, the bullet above). **Not relied on here.** No decision of this record
  and no step of its PLAN reads `sessionKind` or `consents`. At `14200b6e` the daemon decodes a status row with a
  plain `json.Unmarshal` into `provider`, `source`, `authType` and `usable`
  (`internal/provider/gobble/auth.go:55-60`, `:123`), which drops members it does not name, so the two new members
  change nothing the phone shows. gobble-cli's 0052 PLAN says, in its rollout, that this cut-over "reads
  `sessionKind` and `consents`". This record has no such reader, and whether the phone should show a stored consent's
  answer is not decided here. That is a gap between the two records, reported to the owner, and this note does not
  fill it.*
  *(2026-11-11, later the same day: **superseded by the owner's ruling R1 of that day,** relayed by the coordinator.
  The coordinator relayed four rulings, labelled R1 to R4, and this record uses those labels: R1 the consent on the
  phone (D13), R2 `_meta.gobble.remote` (D8), R3 0247's check 3, R4 gobble's system prompt (the 0217 pair). D13 below reads `sessionKind` and `consents`, relays them to the phone, and lets the phone change a consent, so
  "Not relied on here", "No decision of this record … reads `sessionKind` or `consents`" and "this note does not fill
  it" no longer hold. gobble-cli's 0052 PLAN's rollout line, that this cut-over "reads `sessionKind` and
  `consents`", is now true of this record. The note above is kept as it was written.)*
* *`NoLoopback` exists only in the experimental `llmprovider/x/signin` (0074 D4). **Not relied on.** This repository
  imports `llmprovider` and `llmprovider/catalog` alone, D1's boundary test refuses `x/signin` everywhere (the PLAN's
  P1 step 6), and 0247's replay is the daemon's own request, which uses no SDK option.*
  *(2026-11-11, later the same day: **superseded in part by the owner's ruling R2 of that day.** This repository
  still imports and names no `NoLoopback`, and D1's boundary is unchanged. What changes is that the daemon now relies
  on gobble using it: every phone-started gobble sign-in carries `_meta.gobble.remote: true` (D8), on which gobble
  signs in with no loopback listener, so only a paste completes it, and offers such a sign-in no method that has no
  paste (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D3). The coordinator
  relays that the owner ruled the same day that `NoLoopback` is v1 in go-llmprovider-sdk
  `0074-MADR-one-sign-in-handle-and-method-facts.md` D4, which was being edited that day and, as read here, still
  calls it experimental; which it is, is that repository's. Magic depends only on gobble's behaviour, not on the
  SDK's option.)*
* *`wizard.Options.OAuthClients` is dropped (0076). **Not relied on.** Nothing here imports `wizard`.*
* *The three corrected rules of 0074 D3 and D4 are confirmed: a browser sign-in gets `accepts` and a `paste` step only
  when the method's `Paste` fact is set; a sign-in takes three pastes in all; a pasted `access_denied` ends `failed`
  with `denied`. **Relied on by 0247, and already aligned there:** its table row for gobble's `browser_loopback` and
  its D2 check 6 carry notes of 2026-11-11 and read as the SDK's rules do, its check 3 already ended a pasted denial
  as `denied`, and its PLAN's `TestKiloBrowserBadPasteAsksAgain` and `TestCheckErrorParamIsDenied` agree with them. D8's table here passes
  gobble's steps through and states none of the three. One difference is left as written, for the owner's review of
  0247: its check 3 ends any pasted `error` as `denied`, where the SDK ends an `error` other than `access_denied` as
  `other`.*
  *(2026-11-11, later the same day: **resolved by the owner's ruling R3 of that day.** 0247's D2 check 3 now reads as
  the SDK's rule does: `access_denied` ends the session `failed` with `denied`, any other `error` value `failed` with
  `other`, neither replayed. See that record's note of this date.)*
* *The contract's `Failure*` constant names are kept (0073 D3). **Relied on by value only.** This record and its PLAN
  name error kinds by their wire words (`expired`, `unsupported`, `canceled`, `denied`, `invalid_request`) and name
  no constant, so no text changes. The mappers of the PLAN's P2 to P4 use the `Failure*` names.*
* *A stored consent is read through a new reader, `StoredConsent`, and a read that fails is refused as not granted
  (0075 D5). A background request on a plan session then fails with `ErrNotPermitted`, kind `not_permitted`. **What
  the phone shows for it is outside this record's mappers** [inferred from the records and code named; nothing was
  run]:*
  * *D7's mappers map sign-ins. A `not_permitted` in a sign-in's `failed` step gets the phone's copy for that
    `ErrorKind` (the PLAN's P5 step 5).*
  * *This refusal does not arise in a sign-in. It arises in a session, on gobble's own background requests: a
    session's title and automatic compaction (gobble-cli 0052 D2).*
  * *A refused title shows nothing. A refused compaction is a `failed` `compaction_update` whose text is the error's
    first line (`0179-MADR-gobble-native-acp-provider.md` A2), shown on the compaction's card.*
  * *If it ends a turn, the prompt's error is presented from its text by `internal/agenterr`. gobble's `errorLimit`
    reads `ErrorData.Kind` for `quota` and the rate-limit word alone
    (`internal/provider/gobble/errorlimit.go:46`), and no Go or Dart file in this repository names `not_permitted`
    in any spelling (`git grep`, no match).*

  *So the phone shows gobble's message, classified by its wording. Nothing tells the user that a consent could not
  be read, or where to answer it. That is a gap, reported to the owner on 2026-11-11 and not decided here.*

  *(2026-11-11, later the same day: **resolved by the owner's ruling R1 of that day.** A turn that gobble refuses
  with `ErrorData.kind` `not_permitted` and `ErrorData.consent` becomes a phone prompt offering to allow that consent
  (D13, "A refused turn becomes a prompt"). What stays as this note says: a refused title still shows nothing, and a
  refused compaction still shows its first line only, since neither reaches a prompt's error (D13 says why).)*

## Decision Drivers

* The owner's answers above, and the standing rule that Magic relays and decides nothing about a flow.
* A sign-in started on the phone must be able to finish on the phone, for every method the agent can complete from
  another device, and survive the user leaving the app for the browser.
* One lifecycle, one vocabulary of reasons, one vocabulary of error kinds.
* Secrets: a pasted value and a form's answers are write-only, redacted in every log, never retained (`0074-MADR-remote-provider-auth-from-phone.md` D2,
  D11; Q11).
* Lockstep: the daemon and the app ship together; a mismatched pair is refused, not limped along.
* Every gate holds: `make pre-add-check`, `make preflight`, `go test -race`, the Dart trio, `make ci-windows` on the
  Windows test host, `make check-records`, and the disclosure guard.

## Considered Options

* **A. One cut-over onto the contract.** Magic pins `authcontract`, carries its `Method`, `Step` and `Answer` inside
  Magic's own envelope, runs every interactive sign-in through one owned lifecycle, signs gobble in through ACP
  elicitation, generates the phone's models from the contract's schema, and removes the old messages and types with
  the app in one release.
* **B. A Magic mirror of the contract.** Magic keeps its own wire types in `internal/protocol`, maps the contract's
  facts and gobble's steps into them by hand, and keeps hand-written Dart (the report's §5.2 option (a)).
* **C. A staged migration.** The contract messages are added beside today's, both are served for a release, and the
  old ones are removed once every phone has updated.

## Decision Outcome

Chosen option: "A. One cut-over onto the contract", because it is the only option that removes Magic's own copies of
the vocabulary (Q8), lets gobble's ACP-native sign-in reach the phone (Q7), and needs no compatibility path the owner
has ruled out. This record is `proposed`: the owner's answers of 2026-11-10 are recorded, O1 to O5 among them, and
the record awaits the owner's review. O5 (More Information) accepted the three named pieces of Magic-side logic.
*(2026-11-11: the owner's rulings of that day add D13, the consent on the phone (R1), and `_meta.gobble.remote` to D8
(R2). The record stays `proposed`.)*

### D1. The pin, the boundary, and 0185

* **The requirement.** `go.mod` requires `github.com/maccavelli/go-llmprovider-sdk/authcontract` at the tag
  go-llmprovider-sdk `0073-PLAN-publish-an-auth-contract-module.md` makes, `authcontract/v0.1.0`, with no `replace` and no committed `go.work`. The root
  SDK module's requirement stays as
  [0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md)
  set it, for grok's static floor; this record adds no use of it.
* **The boundary: a gate the cut-over must amend.** `TestSDKIsImportedOnlyByGrok`
  (`internal/provider/grok/sdk_boundary_test.go:32`) gains one allowed path, `.../authcontract`, importable by
  `internal/protocol`, `internal/provider` (the sign-in handle's types), `internal/providerauth`, `internal/ws`,
  `internal/provider/acpagent` and the agent packages' mappers. Two of its refusals change for it: the `!inGrok` case
  (`:76-77`) admits it in those packages, and grok's own `sdkAllowed` (`:23-26`) gains it with every name grok's
  mapper uses, since the name check (`:100`) would otherwise refuse `authcontract.Method` in `grok/signin.go`. Every
  other SDK path stays grok's alone. The test is seen failing on a planted import of `.../llmprovider/x/signin` from
  `internal/ws`, and on `authcontract` imported from `internal/session`.
* **gobble's boundary moves in step.** gobble's own import-boundary test, its archtest rules 1 and 4, is amended by
  gobble-cli `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D1 for the same
  module. Magic's gate and gobble's gate are each their repository's; neither reads the other.
* **`0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md` is superseded in part, for auth types, as `0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md` D5 did for catalogs.** Its Confirmation reads: "A review
  rejects any change that adds a `go-llmprovider-sdk` module to `go.mod`, or that calls its catalog API, unless a later
  MADR supersedes this one. Bringing the migration back into scope takes a new MADR, not an amendment to the 0183
  report." This record is such a MADR for the one nested module `authcontract`. Its catalog rule stands: "catalogs
  stay engine-backed, and no catalog moves to the SDK's listing".

### D2. What Magic embeds, and what stays Magic's

* **Embedded, as the contract defines them:** `authcontract.Method` in the method list (D4), `authcontract.Step` in
  `provider.auth_event`, `authcontract.Answer` in `provider.auth_answer`, `authcontract.ErrorKind` in a failed step's
  `error.kind`.
* **Magic's own, never in the contract** (the report's §5.3.2, and go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D2's "What is not here"): the session id,
  admission, device scoping, resume, retained results, cancel, the host reasons of D4, and the codex and grok
  coordinator's states.
* **Magic writes no `_meta` in a contract object.** The contract says nothing in `_meta` is required (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D6 rule 4), and Magic needs
  none in a `Method`, `Step` or `Answer`: its own facts go in its envelope. The only `_meta` Magic writes is on ACP
  `authenticate` to gobble, `_meta.gobble.method` and `_meta.gobble.fresh` (D8), which is gobble's extension, not the
  contract's. *(2026-11-11, the owner's ruling R2: and `_meta.gobble.remote`, D8's "A phone sign-in is always
  remote".)*
* **Readers refuse what they cannot read.** A `Method`, `Step` or `Answer` whose `schema_version` is above the pinned
  module's `SchemaVersion` is refused (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D6 rule 3): a method is listed unavailable with `app_unsupported`, a
  step fails the session with `unsupported`.

### D3. The envelope

| Message | Direction | Payload | Dispatch |
| :--- | :--- | :--- | :--- |
| `provider.auth_start` | phone → daemon | `{provider_id, upstream_id, method_id}` | async, 30 s: admission, then the agent's start; it answers before the first step |
| `ok` | daemon → phone | `{session_id}` | |
| `provider.auth_event` | daemon → phone | `{session_id, provider_id, upstream_id, method_id, step, coordinator_state?, backup_state?, recovery_available?}` | to the owning device's current connection |
| `provider.auth_answer` | phone → daemon | `{session_id, answer}` | async, 15 s, as `question.respond` |
| `provider.auth_cancel` | phone → daemon | `{session_id}` | inline, idempotent, as `oauth.cancel` is (`server.go:859-865`) |
| `provider.auth_sessions` | phone → daemon | `{}` | async, 30 s |
| `provider.auth_sessions_result` | daemon → phone | `{sessions: [{session_id, provider_id, upstream_id, method_id, last_step}], finished: [{session_id, provider_id, upstream_id, method_id, step}]}` | |

* **`step` is an `authcontract.Step`; `answer` is an `authcontract.Answer`.** `method_id` is `<upstream>:<contract
  method id>` for agents with upstreams, and the contract id alone for single-upstream agents.
* **`provider.auth_sessions` drains retained results** (F10). `finished` holds each terminal step retained for this
  device, once, and the drain empties it. A retained `failed` step keeps its `error.kind` and a fixed message, never
  the agent's text, as `TerminalResult` keeps nothing but a classified state today (`reservation.go:50-65`). The phone
  sends it after every authenticated connect and on opening the sign-in sheet.
* **`coordinator_state`** carries the codex and grok coordinator's non-terminal `ready_to_activate` on a `waiting`
  step, and its terminal `conflict`, `recovery_required` and `unsupported_backend` on a `failed` step, with
  `backup_state` and `recovery_available` (`0074-MADR-remote-provider-auth-from-phone.md` D28's states, `reservation.go:14-23`).
* **Redaction.** `AuthAnswerPayload`'s `String`, `GoString` and `LogValue` delegate to the contract `Answer`'s, which
  mask `paste` and every field value unless `secret` is a non-nil false. `SetCredentialPayload` masks its secret in
  `LogValue` and `String` only (`messages.go:866`, `:879`); it has no `GoString`, so `%#v` prints its `Secret` (no
  `GoString` exists anywhere in `internal/protocol`). It gains one, held by a `%#v` sentinel test (the PLAN's P2), so
  both secret-bearing payloads mask under every verb. The daemon sets the answer's `secret` from the step it relayed for that `seq`, before it
  logs, forwards or refuses the answer, and never keeps the phone's copy (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D6 rule 5); an answer for a step
  the session does not hold stays nil and is masked. A test plants a sentinel paste and fails on any log line, error
  string or retained record that holds it.
* **Key writes are unchanged.** A method whose `completion` is `value` (`api_key`, `token_paste`) is written by
  `provider.set_credential`, which already carries a redacted secret; `provider.clear_credential` and
  `provider.set_active_upstream` are unchanged.
* **Error codes,** registered under
  [0241-MADR-register-every-sent-error-code.md](0241-MADR-register-every-sent-error-code.md)'s rule:
  `too_many_sign_ins` (`ErrTooManyFlows`), `provider_auth_busy`, `credential_conflict`, `unsupported_backend`,
  `recovery_required` (the coordinator sentinels `0074-MADR-remote-provider-auth-from-phone.md` §16 D36 names), `invalid_answer`, `no_such_sign_in`,
  `app_outdated` (D6).
* `internal/protocol/op_timeouts.json` gains `provider.auth_start` 30000, `provider.auth_answer` 15000 and
  `provider.auth_sessions` 30000, its `_comment`'s list of inline methods gains `provider.auth_cancel`, and, at the
  cut-over (D11), it loses `provider.start_auth`. The table is held on both sides: `asyncOpTimeout`
  (`internal/ws/server.go:1111`) must match it exactly (`internal/ws/op_timeout_test.go`), and the phone's
  `opTimeoutFor` (`apps/mobile/lib/data/ws/mcremote_client.dart:411`) must exceed each row by the margin
  (`apps/mobile/test/op_timeout_ladder_test.dart`), so a row added here is added in all three in one commit.

### D4. Methods on the wire, and one vocabulary of host reasons

* **Each method entry** in `providers.list_result`'s auth block and in `provider.auth_catalog_result` is `{method:
  <authcontract.Method>, available, reason?, configured?}`. `configured` keeps its present meaning, absent when the
  daemon cannot tell (`messages.go:770-778`'s comment).
* **The auth block keeps the recovery state** that
  [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md) §16 D42 projects onto
  it before the cut-over (the owner's Q-C): `backup_state` and `recovery_available`, beside `status` and the
  upstreams. They are Magic's coordinator's facts, not the contract's.
* **The host reasons are Magic's, one list,** each a constant in `internal/provider` so that `httpagent` and
  `opencode`, which do not import `internal/protocol` (`httpagent/authcatalog.go:259-260`,
  `opencode/acp_auth.go:69-73`), use the same names:

  | Reason | When | Replaces |
  | :--- | :--- | :--- |
  | `host_only` | the method completes only on the host: `completion` `host_command`, or `browser_loopback` with no paste route and no replay (`0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`), gobble's grok browser method included (D7) | `browser_only`, `host_oauth` |
  | `not_wired` | the agent has no way to start it from the daemon | `device_unsupported` |
  | `input_unsupported` | it asks for an input the phone cannot send (opencode's non-string fields, `acp_auth.go:383-415`) | `host_oauth` as opencode uses it |
  | `unsupported_backend` | the agent's credential store is one Magic cannot protect (codex's keyring) | the start-time `credential_failed` |
  | `app_unsupported` | a `schema_version` or `feature` this build cannot render (D2) | — |

  `keyring_managed` and the status `quota` have no producer and go (F4).
* **No central annotation by type.** `upstreamAuthPayload`'s switch (`server.go:2384-2397`) goes: each mapper says why
  a method is unavailable, from its facts. `provider.UpstreamAuth.Methods` (`internal/provider/auth.go:126`, today
  `[]provider.AuthMethod`) carries `{authcontract.Method, available, reason, configured}` instead, so the payload
  copies what the mapper set. The one fact the daemon adds is D5's `not_wired`, from the runtime probe
  `provider.SignInWired`, which is about the transport, not the method.

### D5. One owned lifecycle

* **`providerauth.Session`** generalises `Reservation`: admission before any side effect (2 per device, 8 in all,
  `registry.go:17-18`); one owner goroutine; one terminal step; panic recovery; `CancelAll` and `WaitAll` at shutdown.
  Its handle is `provider.SignIn`:

  ```go
  type SignIn interface {
      Method() authcontract.Method
      Steps() <-chan authcontract.Step // closed after done or failed
      Answer(ctx context.Context, a authcontract.Answer) error
      Wait(ctx context.Context) error
      Cancel()
  }
  ```

  `provider.SignInStarter` (`StartSignIn(ctx, upstreamID, methodID string) (SignIn, error)`) replaces `DeviceAuth` and
  `OwnedDeviceAuth`. Every agent's flow is one: codex's and grok's owned flows, kilo's and opencode's engine flows, and
  gobble's ACP sign-in.
* **Which agents are wired is a runtime fact.** grok, opencode and gobble share `acpagent.Provider`, and kilo
  shares `httpagent`'s, so a type assertion on `SignInStarter` is true for all of them whatever their Spec holds
  (`internal/provider/auth_conformance_test.go:20-25`, "a type assertion succeeds for them regardless"). Today
  `deviceAuthWired` (`server.go:2354-2360`) asks `DeviceAuthCapable`, which `acpagent` answers from its Spec
  (`acpagent/acpagent.go:416-419`), so that the phone is not offered a flow that fails ([0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md)
  D4). Its replacement is `provider.SignInWired(p any) bool`, a runtime probe as `UpstreamSwitchWired` is
  ([0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md](0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md)
  D5): `acpagent` answers it from `Spec.SignInMeta != nil || Spec.StartSignIn != nil` (D8's hook for gobble, and
  `Spec.StartDeviceAuth`'s successor for the CLI flows), `httpagent` from its dialect,
  and a provider without the probe from the type assertion. A method of an agent that is not wired is listed
  `not_wired` (D4).
* **Admission per provider and upstream.** Besides 2 per device and 8 in all, Magic admits one session per (provider,
  upstream) at a time and refuses a second with `provider_auth_busy`. gobble's own "one sign-in per provider at a time"
  holds per process (gobble-cli
  `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4), and D8 starts one
  `gobble acp` per session, so without this two phone sign-ins to one gobble provider would run at once and both
  replace its stored credential. The same rule serves codex's and grok's single native store.
* **The deadline is enforced.** A session's deadline is the `expires_at` of the first step that carries one, else
  `DefaultTTL`, plus 60 s of grace, so that the agent's own expiry, not Magic's, ends it and names it `expired`. A
  session's first step need not carry it: an `at_start` method may begin with a `form`, and a plan sign-in with a
  `confirm`, neither of which expires. Until a step states one the deadline is `DefaultTTL` from admission, and it is
  recomputed when one arrives. A timer cancels it at the deadline, except while it is `ready_to_activate`, which
  `ActivationGrace` bounds (`bounds.go:36-38`).
* **The sign-in outlives its start request.** `provider.auth_start` answers once the session is attached, and its
  async op context (T2) ends then; the session runs under a context derived from the server's lifetime, never the
  request's.
* **A disconnect does not end a sign-in.** A detached session lives until its own deadline, not the 120 s resume
  window (T5): leaving the app for the browser is the expected path. Its terminal step is retained (D3). This amends
  `0074-MADR-remote-provider-auth-from-phone.md` D27's "A transient phone disconnect retains ownership only for the negotiated resume window, then cancels"
  for sign-ins only.
* **Cancel reaches the agent.** opencode's attempt is deleted, as today (`acp_auth.go:731-743`). kilo's `authorize`
  returns no attempt id (`httpagent/deviceauth.go:63-67`), so a cancelled kilo session stops polling and the engine's
  own poll runs to the code's expiry, unless probe U5 finds a cancel route.

### D6. A version check at connect (Q20)

* `auth` gains `app_contract`, an integer, and `auth_ok`'s caps gain the daemon's. The first value is 1, a constant
  `protocol.AppContract`, raised with every incompatible daemon-and-app change, this cut-over's included.
* A client that negotiated V2 and sends another value, or none, is refused before the token is checked, with
  `app_outdated` and "This app does not match mcremote on the host; install the app built with this mcremote." It is
  not a failed-auth strike, as `bad_version` is not (`server.go:1172-1179`).
* **Pairing is checked the same way.** A new pairing negotiates V2 on `pair.claim` (`server.go:1291`) and receives
  `caps` in `pair_ok` on that connection (`:1374-1383`), without ever sending `auth`. So `PairClaimPayload` gains
  `app_contract`, refused with `app_outdated` before the one-shot code is consumed, as `:1288-1294` refuses a version,
  and `pair_ok`'s caps carry the daemon's `app_contract`.
* The phone refuses a daemon whose `caps.app_contract` differs from its own, read from `auth_ok` and from `pair_ok`,
  with "Update mcremote on the host to the release built with this app."
* V1 clients (a `Bearer` upgrade, `server.go:657-659`) get no provider auth today and are unaffected.
* `authcontract.SchemaVersion` is separate and checked per object (D2): a contract change the module allows is not an
  app-contract change unless Magic's envelope changes with it.

### D7. Thin mappers, and one quarantined scraper

Under go-llmprovider-sdk `0074-MADR-one-sign-in-handle-and-method-facts.md` D11 the mappings stay here. Each builds `authcontract.Method` values and a
`provider.SignIn`; none decides a flow's kind from a label (`0074-MADR-remote-provider-auth-from-phone.md` §16 D30, Q18).

| Agent | Methods from | Steps |
| :--- | :--- | :--- |
| codex | fixed, all four with `slot` `openai`, since codex keeps one native credential and a write of any one replaces the others: `api_key` (`value`), `token_paste` (`value`, `codex login --with-access-token` on stdin, M23; built only after probe P-C settles its result, below), `device_code` (`device`), `browser` (`browser_paste` through the replay of `0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`, else `host_only`) | device: `show_code` (scraped URL and code, `expires_at` 15 min, codex's own statement, `codex/device_auth.go:107-109`), `waiting` (with `coordinator_state: ready_to_activate` when deferred), `done` or `failed` |
| grok | fixed: `api_key` and the OAuth methods with no shared `slot`, since grok keeps its API key and its OAuth session in two files that clear independently (`codex/logout.go:30-34`'s comment); `device_code`; `browser` (`browser_loopback`, `host_only` until probes U1 or U7) | device: `show_code` (`expires_at` 600 s, [0107-MADR-grok-phone-api-key-and-device-code-auth.md](0107-MADR-grok-phone-api-key-and-device-code-auth.md) D9), `waiting`, `done` or `failed` |
| kilo | `GET /provider/auth`: each `api` entry `value`; each `oauth` entry `kind` `oauth`, `completion` `at_start` (O1, below); ids by label (`0074-MADR-remote-provider-auth-from-phone.md` §16 D31) | a `form` first when the method has prompts (the inputs `authorize` takes, `deviceauth.go:59-62`); then by the authorize URL: `show_code` and `waiting`, or `open_url` with `accepts` and a `paste` answered through kilo's callback route (probe U5) or the replay (`0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`), else `failed` `unsupported` |
| opencode | `GET /api/integration`: `key` → `value`; `oauth` → `kind` `oauth`, `completion` `at_start` (O1); `command` and `external` → `kind` `host_only`, `completion` `host_command`, the command in `Method.command` (O3) | `form` for the attempt's string fields; by the attempt: `show_code` and `waiting`; `mode: "code"` a `paste` with `accepts: code` once probe U6 names its submit route, else `failed` `unsupported` |
| gobble | `_meta.gobble.methods` from gobble's `initialize` (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4), as gobble states them; a method whose `completion` is `browser_loopback` (gobble's grok browser sign-in, which gobble drives only when named and which completes only where the browser reaches gobble's host, that record's D4 step 3) is listed `host_only` (D4). *(2026-11-11, R2: every phone sign-in carries `_meta.gobble.remote: true` (D8), and gobble then offers no method without a paste, so the phone is never offered Grok's browser sign-in through gobble as one it can start. If `_meta.gobble.methods` at `initialize`, which carries no `remote`, still lists it, it stays listed `host_only` as above, "Finish on the host"; which methods gobble lists there is gobble-cli 0051 D4's.)* | passed through from `_meta.gobble.step` (D8) |

* **The scraper is quarantined** in one package, `internal/providerauth/scrape`: the ANSI, URL and code patterns of
  `cli.go:18-27` and `classify.go:52-59`, `:108-125`. No auth file may compile a regular expression over agent output:
  none in `internal/providerauth` outside `scrape/`, and none in an auth file of `internal/provider` (`auth*.go`,
  `signin.go`, `device_auth.go`, `owned_device_auth.go`), held by a test that walks those files. Packages that read
  agent output for other ends (`stderrtail`, `agenterr`) are not auth and keep their patterns. The live pins of
  `0074-MADR-remote-provider-auth-from-phone.md` D15 stay.
* **Three pieces of Magic-side logic remain, each named** (the owner's standing rule calls Magic-side flow logic and
  hard-coded tables code creep; O5 asks whether these three are accepted): the codex and grok code lifetimes above
  (15 minutes and 600 s), which neither CLI states in a structured form; kilo's method ids minted from labels
  (`0074-MADR-remote-provider-auth-from-phone.md` §16 D31), since kilo's catalog has no id; and the orchestration of `codex login` in `0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md` D4. Each is the
  mapper's alone and is pinned by its live probe; none reaches the phone as anything but a contract value.
* **"Decided at start" is a contract value (O1, answered).** kilo's and opencode's catalogs name no mechanism, and
  Q18 forbids guessing, so their OAuth entries are `kind` `oauth`, `completion` `at_start`, which go-llmprovider-sdk
  `0073-MADR-publish-an-auth-contract-module.md` D2 adds before `authcontract/v0.1.0`. The session's first steps reveal the mechanism (a `form`, then
  `show_code` or `open_url`), and the mapper emits them from the authorize URL, by `0074-MADR-remote-provider-auth-from-phone.md` D7's existing rule.
* **A host command is a contract field (O3, answered).** opencode's `command` and `external` entries carry their
  command in `Method.command` (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D2), never in `detail`; no Magic code runs it.

### D8. gobble through ACP elicitation (amends `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` D1; affirms D6)

`0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` D1 reads: "**D1. Scope: session-scoped form mode only.** Magic advertises `elicitation: {form: {}}`, and not
`url`. A **request-scoped** form, which has no session to show it in, is answered `cancel` and declined by name as
`elicitation.request_scope`. An **`Other` mode** is answered `cancel` and declined as `elicitation.mode.<mode>`. **URL
mode** stays unadvertised, so the SDK refuses it." Its D6 reads: "**D6. Magic advertises `elicitation.form`** in
`defaultClientCapabilities`, and adds it to `implementedClientCapabilities`, only when D1–D5 ship."

* **What Magic advertises: unchanged, form only (the owner's O4, 2026-11-10).** `elicitation.form` only
  (`acpagent.go:1401`): no `elicitation.url`, no `auth.terminal` (gobble's Spec clears `Auth`,
  `internal/provider/gobble/gobble.go:40`). D6 stands, and the URL trust rules of `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` are unchanged. With form only,
  gobble sends every step as a form (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4, its right-hand column), and Magic reads the step from
  `_meta.gobble.step`, so the phone renders each step from the contract whatever the form's text says. URL mode would
  add an `accept` that means "opened" and an `elicitation/complete` to track, for nothing Magic needs; it keeps waiting
  for its own record with a trust policy, as D1 said.
* **D1's request-scope bullet is amended:** a request-scoped form is relayed when, and only when, all of these hold:
  it arrives on a sign-in connection (below); its `requestId` equals the `authenticate` request Magic sent on that
  connection, which Magic learns from a sending middleware through `acp.RequestIDFromContext` (acp-go-sdk2 v1.35.0
  `middleware.go:133-155`, `:192`; *2026-11-11: `middleware.go` and `connection.go` are byte-identical at `v1.36.0`,
  `0217-MADR-update-every-pin-to-its-current-release.md` F3k, so this cite and T7's hold when the pin moves*); and
  its `_meta.gobble.step` decodes strictly into an `authcontract.Step` the
  pinned module can read. Any other request-scoped form is answered `cancel` and declined as today. URL mode and
  `Other` modes are unchanged.
* **The sign-in connection.** A sign-in starts its own `gobble acp` process, through an agent-neutral `acpagent`
  entry point, with the Spec's client capabilities and no session. It sends `initialize`, then `authenticate` with
  `methodId` the upstream id (gobble's agent auth method, `0179-MADR-gobble-native-acp-provider.md` D3), `_meta.gobble.method` the contract method
  id and `_meta.gobble.fresh` `true` (O2) *(2026-11-11, the owner's ruling R2: and `_meta.gobble.remote` `true`, the
  last bullet of this section)*, under a context derived from the server's lifetime with D5's deadline: not
  the handshake's 30 s (T1), not the request's (T2). The process is killed when `authenticate` answers or the session
  ends.
* **The steps, from gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4's form-only column:**

  | gobble sends (form-only) | `_meta.gobble.step` kind | Magic sends the phone | The phone's answer | Magic answers the form |
  | :--- | :--- | :--- | :--- | :--- |
  | a form with no fields, the URL and the paste instruction in `message` | `open_url` with `accepts` | the step | none | `accept` with empty `content`, as soon as the step is sent to the phone; gobble then sends the `paste` step's form (next row) |
  | a form with no fields | `open_url` without `accepts`, or `show_code` | the step | none | `accept` with empty `content`, as soon as the step is sent to the phone; the sign-in runs on |
  | a form with one required string `callback` | `paste` (a re-ask included) | the step | `Answer{seq: the paste step's, paste}` | `accept`, `{callback: paste}` |
  | a form with one boolean `allow`, default false | `confirm` | the step | `Answer{seq, confirm}`: `true` allows background use, `false` declines it and the sign-in goes on | `accept`, `{allow: confirm}` |
  | a form whose properties are the fields | `form` | the step | `Answer{seq, fields}` | `accept`, the fields typed by the schema, as [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](0191-MADR-answer-agent-elicitation-forms-on-the-phone.md) D3 types them |
  | `authenticate` answers `{}` | — | a `done` step Magic makes | — | — |
  | `authenticate` answers `auth_required` | — | a `failed` step, `error.kind` from `ErrorData.Kind` | — | — |

  * **A shown step is acknowledged, not answered.** An `open_url` or `show_code` step asks nothing (go-llmprovider-sdk
    `0074-MADR-one-sign-in-handle-and-method-facts.md` D3), so Magic builds no `Answer` for it and the phone sends
    none: Magic answers its form with an empty `accept` once the `provider.auth_event` carrying it is written to the
    phone, and gobble runs on (gobble-cli
    `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4, "Shown steps are acknowledged, not
    answered"). A phone answer naming a shown step's `seq` is refused `invalid_answer`. The phone shows an `open_url`
    with `accepts` and the `paste` step that follows it on one sheet, and its paste is the `Answer` for the `paste`
    step's `seq`.
  * **Cancel.** The phone's cancel, which is also how it declines the sign-in at a plan's `confirm` (D9), is
    *(2026-11-11: a plan re-sign-in whose session already holds the consent's answer starts at `open_url` with no
    `confirm` step, go-llmprovider-sdk `docs/decisions/0075-MADR-chatgpt-plan-consent-host-id-and-client-reuse.md` D4; the phone relays steps as they come and expects a `confirm` on no sign-in)*
    `provider.auth_cancel`: Magic answers any pending form `cancel` and ends `authenticate`'s context, which sends
    `$/cancel_request`, and gobble answers `auth_required` with kind `canceled`.

  The content's key names (`callback`, `allow`) are gobble's, so they live in gobble's Spec hooks, not in `acpagent`
  (`TestNoProviderIsNamedInAcpagent`, `acpagent/neutral_test.go:22`, whose pattern names agents, not these keys; a
  test of its own holds the keys out of `acpagent`, the PLAN's P4).
* **No question flow, no `PermissionTimeout`.** A sign-in form is never a `question_request`, so T4 does not apply,
  and the 0191 form checks run on the contract's own field types.
* **The catalog.** gobble's methods are `_meta.gobble.methods` from `initialize`, kept per gobble version; with none
  kept, an `initialize`-only run fills it. Its cost is not measured: the one figure, 424 to 556 ms, is spawn to
  `session/new`'s answer ([0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P3 step
  2's record), which bounds an `initialize`-only run from above. acpext at `v0.5.0` supplies the provider list only
  (QC (i)).
* **The CLI path goes.** `gobble auth login --method device --json` (`gobble/auth.go:321-410`) is removed; gobble's
  `--json` refuses a method that asks a question (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D5), so it cannot carry a plan sign-in anyway. Key writes
  and `gobble auth logout` stay (QD (a)).
* **A phone sign-in is always fresh (O2, answered).** The phone starts a sign-in only when the user asks for one, so
  every `authenticate` on a sign-in connection carries `_meta.gobble.fresh: true`, and gobble signs in whatever is
  stored (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4 steps 1 to 3). gobble replaces the stored credential only once the new one saves, so a phone
  cancel, a decline or a failure leaves the host's credential as it was. Without the flag, a phone signing in again
  over a stored but refused session would get `done` with nothing done.
* **A phone sign-in is always remote** *(added 2026-11-11, the owner's ruling R2, relayed by the coordinator; the
  name `_meta.gobble.remote` is the one the coordinator fixed for the four repositories).* Every `authenticate` on a
  sign-in connection carries `_meta.gobble.remote: true` beside `_meta.gobble.method` and `_meta.gobble.fresh: true`.
  It says that the person completing the sign-in is not on the agent's host, which is always so for a sign-in the
  phone starts. gobble then signs in with no loopback listener, so only a paste completes it, and offers such a
  sign-in no method that has no paste, Grok's `browser_loopback` among them (gobble-cli
  `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D3; go-llmprovider-sdk
  `0074-MADR-one-sign-in-handle-and-method-facts.md` D4's `NoLoopback`). Three things follow here:
  * **The phone never starts Grok's browser sign-in through gobble.** It was already listed `host_only` (D7's gobble
    row); with `remote` gobble would refuse it too, so the phone has no route to it and shows it, if gobble lists it
    at all, only as "Finish on the host".
  * **No gobble sign-in Magic starts binds a port on the host.** A plan sign-in through gobble holds no loopback
    port, so it cannot contend with codex's or kilo's 1455 (`0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`,
    "Ports are shared"), and 0247's D3 port admission does not apply to it: it is not a replayable session.
  * `remote` is a request member of gobble's extension. The contract's golden vectors carry no `_meta` (D2) and the
    recaptured wire fixtures hold gobble's frames, not Magic's requests, so it appears in neither; the sign-in tests
    assert it on the `authenticate` params the `acptest` fake records (the PLAN's P4).

### D9. The phone

* **Generated models (Q9).** `scripts/gen_authcontract_dart.py`, Python on the standard library, reads
  `authcontract.schema.json` from the pinned module's directory (`go list -m -f '{{.Dir}}'`) and writes
  `apps/mobile/lib/data/protocol/authcontract.g.dart`: one immutable class per `$defs` object with `fromJson` and
  `toJson`, and each open enum as a Dart extension type over `String` with its known values and `isKnown`, so an
  unknown value is kept, not thrown. It copies the golden vectors into `internal/protocol/testdata/authcontract/` and
  `apps/mobile/test/fixtures/authcontract/`, and refuses a schema keyword outside the list go-llmprovider-sdk
  `0073-MADR-publish-an-auth-contract-module.md` D4 states exactly (its validator's, `0073-PLAN-publish-an-auth-contract-module.md`
  Phase 3 step 4): `$schema`, `$id`, `$defs`, `$ref`, `type`, `properties`, `required`, `enum`, `items`,
  `additionalProperties`, `format` and the annotations `x-open-enum` and `x-nullable`. The envelope's own models stay
  hand-written beside it.
* **When `toJson` writes a member, from the schema alone** (go-llmprovider-sdk
  `0073-MADR-publish-an-auth-contract-module.md` D4, "When a member is on the wire"). A required member always; a
  member marked `"x-nullable": true` (`Step.default`, `Step.error`, `Answer.secret`, `Answer.confirm`) whenever it is
  not null, `false` included, so `Answer{confirm: false}`, the "no" to a consent, reaches the daemon; any other
  optional member only when it is not its zero value, so `Step.secret` `false` is omitted as Go omits it. A
  `date-time` member stays a `String` on the wire, as received, never re-formatted through `DateTime` (whose
  `toIso8601String` adds `.000`); the sheet parses it for display only.
* **What the Dart file must also be:** `dart format`-stable and clean under `flutter analyze` with the app's
  `flutter_lints` (`apps/mobile/pubspec.yaml`). CI's Flutter job already runs `dart format --output=none
  --set-exit-if-changed .` and `flutter analyze` over `apps/mobile`, the committed generated file included, and the
  drift gate in CI's Go job compares the generator's raw output with that file, byte for byte. The two agree only if
  the generator writes what `dart format` would, so it does; the PLAN tests it with `dart format` on a scratch output.
* **The drift gate.** `make check-authcontract` regenerates into a scratch directory and fails on any difference from
  the committed files. It runs in `make preflight`, which also runs `dart format --output=none --set-exit-if-changed`
  on the scratch output where `dart` is installed, and in CI's Go job, which has no Dart toolchain and compares bytes
  only.
* **One sign-in sheet** renders the steps: `open_url` (open in the system browser, `0074-MADR-remote-provider-auth-from-phone.md` D13, and a paste box when
  `accepts` is set), `show_code` (code, link, countdown from `expires_at`), `paste` (an obscured field when the step's
  `secret` is set, cleared on send, never stored), `confirm` (text, links, default "no", and three outcomes, as the
  owner answered the SDK's SDK-1: allow background use, decline background use with the sign-in going on, or Cancel,
  which declines the sign-in through `provider.auth_cancel`),
  `form` (the fields, a `secret` field obscured), `waiting` (with `coordinator_state`), `done`, `failed` (by
  `error.kind`), and an unknown kind as its `text` with Cancel (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D6 rule 2). The answer the phone sends
  carries the step's `secret`, and the daemon sets it again from its own copy (D3).
* **Methods the phone lists by their facts.** An `oauth` method with `completion` `at_start` is listed as "a sign-in
  whose type is learned on start", and its sheet shows whatever its first step is (O1). A `host_only` method shows
  "Finish on the host" with its `command`, when it has one, and a copy button (O3), and its `detail`.
* **Whose client a sign-in uses is shown (the owner's Q10: "The method's `client_identity` is shown on the
  phone").** The method row and the sign-in sheet name it from `Method.client_identity`: `vendor_borrowed`, "Signs in
  as the vendor's own app"; `app_registered`, "Signs in as this app's own registration with the provider"; `none`, no
  line; `unknown`, an `at_start` method's (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D2, O1),
  "Learned when the sign-in starts". An `at_start` method's `refreshable` and `revocable`, false there, are read as
  "not known", never as "no".
* **Removed:** `providerId == 'codex'` twice (F36), `isUsable`'s browser floor (F11), `confirmDestructiveSignIn`,
  `DeviceFlowSheet`, the `oauth.*` streams, and `AuthMethodType`.

### D10. Tests that pin heuristics are rewritten (F57)

| Test | Today | After |
| :--- | :--- | :--- |
| `TestCatalogIsAcpextsProviders` (`gobble/auth_test.go:21-52`) | `Login` ⇒ device; grok's `host_oauth` row | the catalog is `_meta.gobble.methods`, as gobble sends them |
| `TestAuthStatusSplitsBrowserFromDeviceOAuth` (`kilo/auth_test.go:136`) *(became `TestAuthStatusListsEveryOAuthMethodAsDevice` under 0074 P23 step 2, 2026-11-11)* | labels decide browser or device | each `oauth` entry is decided at start |
| `TestSuperGrokSubscriptionClassifiesBrowser`, `TestHeadlessVPSClassifiesDevice` (`httpagent/authcatalog_honesty_test.go:45`, `:52`) *(both became `TestOAuthEntriesClassifyDeviceWhateverTheLabel` under 0074 P23 step 2, 2026-11-11)* | labels as the rule | removed with `ClassifyCatalogMethod`'s label rule (`0074-MADR-remote-provider-auth-from-phone.md` §16 D30); the URL table in `classify_test.go` stays |
| `TestProviderAuthInterfaceConformance` (`internal/provider/auth_conformance_test.go:26`) | `DeviceAuth` for codex and grok, no gobble case | `SignInStarter` for every agent, gobble's case included, and a column for the runtime answer `SignInWired` (D5), as `wantSwitchWired` is for the probe of `0244-MADR-tell-the-phone-when-an-agent-cannot-switch-its-active-upstream.md` |

### D11. One cut-over

Removed in the same release as the app that no longer sends or reads them: `provider.start_auth`, the four `oauth.*`
messages and their payloads, `AuthMethodPayload`, `AuthInputPayload`, `AuthInputOptionPayload`,
`AuthInputConditionPayload`, the three method types, the four reasons, `AuthStatusQuota`, `provider.DeviceFlow`,
`DeviceAuth`, `OwnedDeviceAuth`, `DeviceAuthCapable`, `deviceAuthWired`, the legacy registry map, gobble's two id tables
`deviceSignIn` and `noPhoneSignIn` with their partition test (`0179-MADR-gobble-native-acp-provider.md` A9.2), and the phone's matching models.
`DeviceAuthCapable` and `deviceAuthWired` go only because D5's runtime probe `SignInWired` replaces them: the cut-over
keeps a probe, so the phone is still never offered a flow its agent cannot start (`0083-MADR-provider-auth-activation-and-layout-gaps.md` D4).
`Caps.ProviderAuth` and `Caps.ProviderAuthTransactions` stay, as what the host offers.

### D12. Live acceptance, folded in (Q19)

[0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md) P22 steps 5 and 6 and
[0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P6 become this record's acceptance,
run once against the new design, each owner-run, each asked first. Nothing of them is dropped:

* P22 step 5: LIVE hashed before the code is shown, during polling, and after a cancel, a timeout, a socket loss and a
  forced daemon death, byte-identical for both providers; one completed isolated login per provider, validated; a
  second rotation; CURRENT and PREVIOUS retained, owner-only, nothing printed.
* P22 step 6: one busy activation (a validated PENDING kept under the original flow, activated at idle with no second
  exchange) and one explicit logout; grok's API-key removal keeps OAuth and its OAuth logout keeps the key; codex's
  logout cannot be undone and removes its LIVE and generation payloads.
* `0179-PLAN-gobble-native-acp-provider.md` P6: step 1 `make live-gobble`; step 2 the phone key write, no key in mcremote's log; step 3 `mcremote
  doctor` lists gobble, runs no version check for it, and reports the installed binary; step 4 the other providers'
  suites; step 5 the wider gates; step 6 the Confirmation of `0179-MADR-gobble-native-acp-provider.md`; step 7 the issue #33 comment, written into this
  record's PLAN for the owner to post.
* *(2026-11-11, R1.)* The owner-run acceptance gains one run, the PLAN's L7: reading and changing a consent from the
  phone (D13).

### D13. The consent on the phone (added 2026-11-11, the owner's ruling R1)

*Added on 2026-11-11 by the owner's ruling of that day, relayed by the coordinator of the four-repository auth-contract
work. The owner chose to decide this inside this record rather than in a record of its own, knowing that it enlarges
a plan that tags no release from P2 until P6 (Consequences). gobble-cli's and go-llmprovider-sdk's records were being
edited the same day, so each is cited by its decision, and what it finally says is that repository's. This
section's line numbers in this repository are read at `14200b6e`, and re-read at P4a's start (C2).*

* **What gobble gives, and where.**
  * `gobble auth status --json` gains, on each row, `sessionKind`, the stored session's kind (the value of the SDK's
    `(*OAuthSession).SessionKind()`: `chatgpt`, `chatgpt_plan`, `grok` or `kilo_device`), when `authType` is `oauth`;
    and `consents`, an object from consent id to `granted` or `declined`, for answered consents only, e.g.
    `{"chatgpt_plan.background_use": "declined"}` (gobble-cli
    `0052-MADR-finish-the-chatgpt-plan-sign-in-on-the-sdk-engine.md` D8).
  * `gobble auth consent <provider> [<consent> allow|deny]` lists a provider's consents or changes one. A provider
    with no stored session is a usage error, exit 2; a store that cannot be read or locked exits 1 with the store's
    error and prints no answer (its D3, with that D3's note of 2026-11-11).
    *(2026-11-11, later: aligned with gobble-cli's final 0052, D3's note of that day, which fixes what a
    non-interactive caller gets. `<consent>` is the SDK's consent id (`chatgpt_plan.background_use`), the id
    `consents`, `consentTerms` and `ErrorData.consent` carry; gobble's word `background` is kept as an alias for a
    person at a terminal. The command never prompts and reads nothing from stdin. Exit 0: the answer is stored.
    Exit 2: each usage error, which is an unknown provider, a consent the stored session's method does not have, no
    stored session, a second argument with no third, or a third other than `allow` or `deny`. Exit 1: the store
    cannot be read, parsed or locked, and nothing is written. One line on stdout on success, one on stderr on an
    error, and no `--json`.)*
  * A consent's text and links are method facts: `authcontract.Method.Consents`, each an `authcontract.Consent{id,
    required, text, links}` (go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D2), filled from the
    SDK's `ConsentText` and `ConsentURL` (go-llmprovider-sdk
    `0075-MADR-chatgpt-plan-consent-host-id-and-client-reuse.md` D1). gobble advertises them in
    `_meta.gobble.methods` at `initialize` (gobble-cli 0051 D4: "A client learns … its consents, before it asks"),
    which D8 already keeps per gobble version. Neither `auth status --json` nor `auth consent` carries a text or a
    link, so the catalog is the daemon's one source for them. Whose wording that text is, is a dependency on gobble
    (More Information, "Dependencies D13 found").
    *(2026-11-11, later: aligned with gobble-cli's final 0052. "Neither `auth status --json` nor `auth consent`
    carries a text or a link" no longer holds. 0052 D8's note of that day adds `consentTerms` to a status row: an
    array of `authcontract.Consent` (`id`, `required`, `text`, `links`) listing every consent of the method the
    stored session signs in to, answered or not, present only for such a session (among the built-ins, the plan
    session alone). Its `text` is gobble's wording and its `links` the SDK's usage page. They are the text and link
    gobble advertises in `_meta.gobble.methods[].consents[]` and shows in a sign-in's `confirm` step, because
    `provider.Offered` sets one wording for all three (0052 D2's note of that day; gobble-cli
    `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D2's and D4's notes of that day). The
    daemon's source for a consent's text and links is therefore `consentTerms`, read with the answers, and no
    longer the kept catalog ("Read" below).)*
  * A request gobble refuses because a consent is not granted carries `ErrorData.kind` `not_permitted` and
    `ErrorData.consent`, the consent's id, from `acpext/v0.5.0`, which P4 pins (the name the coordinator fixed for
    the four repositories; gobble-cli's records carry it). The one consent id today is
    `chatgpt_plan.background_use` (go-llmprovider-sdk 0075 D1, `openai.ConsentBackgroundUse`).
    *(2026-11-11, later: aligned with gobble-cli's final 0052 and 0051. 0051 D7's note of that day sets
    `consent` only when allowing that consent would let the request through. 0052 D2's note of that day gives the two
    errors that end a turn. An overflow refused for want of a grant (`errPlanOverflow`), or an SDK refusal that
    gobble's own read then finds ungranted, is `not_permitted` with `consent` `chatgpt_plan.background_use`. An
    answer that cannot be read, by gobble's read or the SDK's, is `not_permitted` with no `consent`, so the phone
    shows the error and offers no allow. `provider` is `openai` in both. gobble tags the member `omitzero`, not
    `omitempty`; for a string the wire is the same, the empty value omitted.)*
* **Read: the auth block, not the sign-in envelope.** `provider.UpstreamAuth` (`internal/provider/auth.go:126`'s
  type) gains `SessionKind string` and `Consents []provider.ConsentEntry`, each `{Consent authcontract.Consent;
  Answer string}`. The auth block's upstream entry (D4) gains `session_kind?` and `consents?: [{consent:
  <authcontract.Consent>, answer?}]`, `answer` being gobble's word as written, absent when not asked. gobble's mapper
  fills them: `statusRow` (`internal/provider/gobble/auth.go:55-60`) decodes `sessionKind` and `consents`, which the
  plain `json.Unmarshal` of today (`:123`) drops; the list is every `Consent` of the upstream's methods in the kept
  catalog, one per id, each with its answer from `consents`. An answered id that no kept method describes is listed
  with `consent` holding its `id` alone and no text.
  *(2026-11-11, later: aligned with gobble-cli's final 0052 D8. `statusRow` decodes three members. `sessionKind`
  is a string, present only when `authType` is `oauth`, one of `chatgpt`, `chatgpt_plan`, `grok` and
  `kilo_device`; a value the daemon does not know is shown, not refused. `consents` is an object from consent id to
  `granted` or `declined`, present only for answered consents; a consent with no member is "not asked".
  `consentTerms` is an array of `authcontract.Consent`. The list is every `Consent` of the row's `consentTerms`, in
  its order, each with its answer from `consents`, in place of "every `Consent` of the upstream's methods in the
  kept catalog"; the catalog is not read for consents. An answered id that `consentTerms` does not list keeps the
  rule above: its `id` alone and no text, so it offers decline only. A store gobble cannot read gives 0045-MADR
  D5's error object and exit 2, which `statusReport` already decodes (`internal/provider/gobble/auth.go:64-71` at
  `14200b6e`): no row, so no answer is shown as current and no consent row is offered for a change.)*
  *(2026-11-11, later: the owner's ruling on an unreadable consent store. "no row, so no answer is shown as current
  and no consent row is offered for a change" no longer holds as the whole outcome. The daemon turns gobble's status
  error object into an upstream state, "consent store unreadable on the host": `status` lists each upstream of
  `acpext.AuthProviders()` in that state, since gobble's store is one for all of them, with no answer and no consent,
  where at `14200b6e` it lists no upstream (`auth.go:204-206`); the whole state's status stays `error`.
  `UpstreamAuth` gains `ConsentsUnreadable bool`, and the auth block's upstream entry `consents_unreadable?: true`,
  absent otherwise; the member's name is this record's, not the ruling's. The error object is cached for
  `statusTTL`, 15 s (`auth.go:31`), as every status answer is, and the state with it.)*
  * **Why the auth block, and not D3's `provider.auth_*` messages.** Those carry one running sign-in, keyed by its
    `session_id`, and are drained when it ends. A consent's answer belongs to the stored session and holds with no
    sign-in running, as `status`, `configured`, `backup_state` and `recovery_available` do, and the phone already
    reads those from the auth block (`providers.list_result` and `provider.auth_catalog_result`, D4). The change is
    a message of the `provider.auth_*` family (below); the reading is not.
  * **The contract's types where they exist.** `authcontract.Consent` and its `Link` are the contract's, generated
    into `authcontract.g.dart` (D9) and embedded as they are. **The contract has no session-kind type:**
    go-llmprovider-sdk 0073 D2's type list has none, and `auth.SessionKind` lives in the SDK's `llmprovider/auth`,
    which D1's boundary refuses in every package. So `session_kind` is an opaque string in Magic's own envelope,
    shown as gobble sent it, with no label table on either side; so is `answer`, except that the phone reads
    `granted` as allowed and every other value, an absent one included, as not allowed.
    *(2026-11-11, later: aligned with gobble-cli's final 0052 D8, which now names the four values of
    `sessionKind` and says the SDK may add kinds. **Recommended, not decided here; the owner's to settle:** the phone
    keeps showing the value verbatim. Labelling the four would put a table of the SDK's kinds in the app, which the
    PLAN's P5 step 7 search refuses for `chatgpt_plan` and the owner's standing rule calls code creep; a kind the
    SDK adds later would show raw beside labelled ones; and the contract still has no session-kind type to generate
    labels from (More Information, "Dependencies D13 found", item 4). If the owner wants labels, they belong in the
    contract, as that item says.)*
    *(2026-11-11, later: the owner's ruling on session-kind labels supersedes "The contract has no session-kind
    type" and the note's "Recommended, not decided here" above. go-llmprovider-sdk's auth contract module gains a
    session-kind type with the four values gobble reports, `chatgpt`, `chatgpt_plan`, `grok` and `kilo_device` (the
    strings of the SDK's `auth.SessionKind`, go-llmprovider-sdk 0074 D5), and their human labels, before
    `authcontract/v0.1.0` is tagged: the contract's session-kind type and its labels (go-llmprovider-sdk 0073-MADR,
    amendment of 2026-11-11). The daemon carries the kind as that contract type, `UpstreamAuth.SessionKind` taking it
    in place of `string`; the wire member `session_kind` keeps its name and its string value. The phone shows the
    contract's label, generated with the type into `authcontract.g.dart` (D9). A value the pinned contract tag does
    not know, a kind added after it, is shown verbatim, not refused. No label table is written by hand on either
    side. D1's boundary admits this unchanged: the type is `authcontract`'s, which D1 allows in `internal/protocol`,
    `internal/provider` and the agent packages' mappers, and `llmprovider/auth` and `llmprovider/x/signin` stay
    refused in every package. `answer` stays as above.)*
* **Change: `provider.auth_consent`.** Phone to daemon, `{provider_id, upstream_id, consent_id, allow}`, async 30 s,
  as `provider.set_credential` is (`internal/protocol/op_timeouts.json`); it answers `ok` with `{auth: <the
  provider's auth block, re-read>}`. An optional `provider.ConsentSetter` (`SetConsent(ctx, upstreamID, consentID
  string, allow bool) error`) in `internal/provider/auth.go` is the agent-neutral seam; a provider without it is
  refused `no_such_consent`. gobble's implements it the way `clearCredential` runs `gobble auth logout`
  (`internal/provider/gobble/auth.go:305-319`), driving gobble's own CLI as the daemon already does for key writes,
  logout and status (`0179-MADR-gobble-native-acp-provider.md` D3):
  * **Before it runs,** it refuses `no_such_consent` when the upstream's entry lists no such consent id, and when
    `allow` is true for a consent whose `text` the daemon does not hold, so that nothing is allowed that the phone
    could not show first.
    *(2026-11-11, later: the owner's ruling on an unreadable consent store. A check comes first: when the
    upstream's entry is in the unreadable state ("Read" above, its note of this time), the change is refused
    `consent_store_unreadable` and `gobble auth consent` is not run. Without it, the entry's empty consent list would
    refuse the change as `no_such_consent`, which gives the wrong reason. The check reads the status as the others
    do, from the cache while it is fresh, so it holds for a fresh error object and for a cached one.)*
  * **The command:** `gobble auth consent <upstream id> <consent id> allow|deny`, the id as the status and
    `ErrorData.consent` name it, never a word of gobble's mapped from it (a table would be the hard-coded id table
    the owner's standing rule removes; More Information, "Dependencies D13 found", item 2); no stdin; stdout and
    stderr captured; under `commandTimeout`, 10 s (`auth.go:34`). It is non-interactive: with both arguments given,
    gobble changes the answer and asks nothing (gobble-cli 0052 D3).
  * **Exit 0:** the cached status is dropped (`drop`, `auth.go:103-107`) and read again; the answer must then read
    `granted` after `allow` and `declined` after `deny`. Anything else is `consent_failed`.
  * **Exit 1, the store cannot be read or locked:** `consent_store_unreadable`, "gobble could not read its credential
    store on the host. Nothing was changed." The phone shows no answer as current for that upstream until a later
    read succeeds. gobble's stderr goes to the daemon's log, its last line only, and never to the phone, since the
    store's error names a file on the host.
    *(2026-11-11, later: the owner's ruling on an unreadable consent store. Exit 1 keeps this mapping for the case
    the pre-run check cannot see: a status read while the store was readable, still cached, and the store
    unreadable by the time the change runs. After it the phone shows that upstream in the unreadable state (below),
    which is the "no answer as current" this bullet names, until a later read succeeds.)*
  * **Exit 2, a usage error:** `consent_refused`, "gobble refused the change: it has no stored sign-in for this
    provider, or does not know this consent." gobble-cli 0052 D3 names exit 2 for a provider with no stored session
    only; the daemon's own check above makes an unknown consent unlikely, and the copy names both.
    *(2026-11-11, later: aligned with gobble-cli's final 0052 D3. Exit 1 is a store that cannot be read,
    parsed or locked, nothing written; its mapping to `consent_store_unreadable` holds, and gobble's stderr is one
    line, which goes to the log as before. Exit 2 is now named for each usage error: an unknown provider, a consent
    the stored session's method does not have, no stored session, and bad arguments. "gobble-cli 0052 D3 names exit
    2 for a provider with no stored session only" no longer holds. Its mapping to `consent_refused` holds and the
    copy is unchanged: after the daemon's own check, the cases that can reach it are no stored session and a consent
    the session's method no longer has, which the copy names; an unknown provider or bad arguments would be a
    daemon fault, and gobble's line goes to the log. On exit 0 the daemon still takes the result from re-reading
    `auth status --json`, not from the stdout line, as 0052 D3 tells a client to do; there is no `--json`.)*
  * **Any other exit, a signal or the timeout:** `consent_failed`.
  * The four codes `no_such_consent`, `consent_store_unreadable`, `consent_refused` and `consent_failed` join D3's,
    registered under `0241-MADR-register-every-sent-error-code.md`'s rule.
* **The phone shows the session kind and each consent's answer, and the text and link before any allow.** On the
  provider detail screen, an upstream with `session_kind` shows it on a "Stored sign-in" line, verbatim. Each consent
  is a row with its `text`, its `links`, and its answer: "Allowed", "Declined" or "Not asked". Changing it to allow
  opens `consent_sheet.dart`, which shows the whole `text` and every link before its allow button, whose default is
  no, as the sign-in's `confirm` is (D9); declining needs no sheet. A row with no `text` shows the id and the answer
  and offers decline only. After an answer the screen renders the auth block the `ok` carries. For gobble's ChatGPT
  plan session this is the `chatgpt_plan.background_use` row, shown under `session_kind` `chatgpt_plan`; nothing in
  the phone or the daemon names that id or that kind.
  *(2026-11-11, later: the owner's two rulings of that day on D13. The "Stored sign-in" line shows the contract's
  label for `session_kind`, not the value verbatim, and the value verbatim only when the pinned contract does not
  know it ("The contract's types" above, its note of this time); the kinds are named in the generated
  `authcontract.g.dart` alone. An upstream in the unreadable state shows "gobble could not read its credential store
  on the host, so its consents cannot be shown or changed." in place of its consent rows, with no answer and no
  control; a change answered `consent_store_unreadable` leaves the upstream in the same state.)*
* **A refused turn becomes a prompt.** `internal/provider/gobble/errorconsent.go` reads a failed prompt's raw error
  for `ErrorData` with `kind` `not_permitted` and a non-empty `consent`, and returns the upstream (`ErrorData.Provider`)
  and the consent id; it shares one decode of the error's `data` with `errorLimit`, factored out of
  `errorlimit.go:31-45`. `errorLimit` itself is unchanged: its check at `:46` reads `quota` and `rate_limited` only,
  and `not_permitted` is not a limit. `acpagent.Spec` gains `ErrorConsent func(raw string) (upstream, consent string,
  ok bool)`, agent-neutral, set by gobble's Spec beside `ErrorLimit` (`gobble.go:85`) and bound by `bindSpec` as
  `ErrorLimit` is (`acpagent/host.go:178`); the session's `present` path (`acpagent/session.go:826-836`) copies it onto
  the error event, and `event.Event` gains `consent_upstream` and `consent` beside the limit fields
  (`internal/event/event.go:748-751`), counted by `retention.go:140`'s size. On the phone the error bubble
  (`apps/mobile/lib/features/chat/chat_bubble.dart`) adds "Allow…" when that upstream's auth block holds the consent
  with its `text`; it opens the same consent sheet, and allowing sends `provider.auth_consent`. It does not send the
  turn again: the user does. An unknown consent, or one with no text, gets gobble's message as today and no button.
  * **Where the mapping does not reach.** D7's sign-in mappers do not hold it: a sign-in's `failed` step of kind
    `not_permitted` keeps the PLAN's P5 step 5 copy. A title that gobble does not make shows nothing, as before. A
    compaction the SDK refuses ends as a `failed` `compaction_update` whose text is the error's first line
    (`0179-MADR-gobble-native-acp-provider.md` A2); ACP's `compaction_update` has no error data, so no `consent`
    reaches Magic from it, and it gets no prompt. gobble's own gate makes no background request without a grant
    (gobble-cli 0052 D2), so what ends a turn is its overflow error; whether that error carries `not_permitted` and
    `consent` is gobble's (More Information, "Dependencies D13 found", item 3), and without both the bubble is as
    today.
    *(2026-11-11, later: aligned with gobble-cli's final 0052 D2, which resolves item 3: the overflow error
    carries both, so the bubble offers "Allow…"; the error for an answer that cannot be read carries
    `not_permitted` and no `consent`, so the bubble shows gobble's text, which names `/compact`, and no button,
    since `errorConsent` requires a non-empty `consent`.)*
* **Still one cut-over, `app_contract = 1`.** Every part of D13 lands between P2 and P6, which no release separates
  (the PLAN's C5), so no daemon or app build carries an auth block without these members, or a phone without the
  handler for `provider.auth_consent`. `protocol.AppContract` stays 1, as D6 set it for this cut-over.

### Consequences

* Good, because Magic stops defining sign-in vocabulary: methods, steps, answers and error kinds are the contract's,
  and the phone's models are generated from its schema.
* Good, because a browser sign-in, a paste, a consent and a form can be completed from the phone, for gobble now and
  for the other agents as their routes are proven.
* Good, because one lifecycle replaces two, with an enforced deadline, delivery after a reconnect, and a disconnect
  rule that fits leaving the app for the browser.
* Good, because the phone carries no provider id and no browser floor.
* Neutral, because key writes, clears and the upstream switch keep their messages.
* Bad, because the release order spans three repositories, and Magic's cut-over waits for the contract tag, gobble's
  `v0.6.0` and `acpext/v0.5.0`.
  *(2026-11-12: gobble's release here is now `v0.7.0`. By the owner's decision of that day, recorded in gobble-cli
  `0055-MADR-release-v0-6-0-now-and-the-sign-in-redesign-as-v0-7-0.md` (accepted), gobble `v0.6.0` is released now from
  gobble-cli's `main` without the ACP sign-in redesign: it changes neither `gobble auth login --json`, ACP
  `authenticate` nor `_meta.gobble.*` (that record's F5). The redesign this cut-over waits for is `v0.7.0`.)*
* Bad, because D7's kilo and opencode rows depend on `oauth`, `at_start` and `command`, which the owner's O1 and O3
  put into the contract before its first tag: the cut-over cannot start until that tag holds them (PLAN gate G1).
* Bad, because a sign-in holds a gobble process for up to 16 minutes, one per session, within the 8-session limit.
* Bad, because the quarantined scraper still parses codex's and grok's output until they emit structure.
* *(2026-11-11, R1.)* Good, because the user sees what the ChatGPT plan session was told and answered, changes it from
  the phone after reading its text and link, and is offered the consent when a refused turn needs it, instead of
  reading gobble's message classified by its wording.
* *(2026-11-11, R1.)* **Bad, because the release freeze is longer.** D13 adds a phase (the PLAN's P4a) and phone work
  to P5 inside the window in which no release is tagged, from P2 until P6 (C5). Nothing fixed, and nothing else
  new, ships while that window is open. It adds no new wait on another repository, since P4 already waits for
  gobble's `v0.6.0` (G2) and P4a reads the same release, but it lengthens the stretch from G2 to P6. The owner chose
  this, over a record of its own, on 2026-11-11.
  *(2026-11-12: "gobble's `v0.6.0` (G2)" now reads `v0.7.0`, the note on the release order above.)*
* *(2026-11-11, R1.)* Bad, because D13 rests on four more things gobble's `v0.6.0` must hold (the PLAN's G2's notes
  of R1), and gobble's records leave three questions about them open (More Information, "Dependencies D13 found",
  items 1 to 3).
  *(2026-11-11, later: aligned with gobble-cli's final 0052; items 1 to 3 are now resolved in gobble's records,
  each as its note in that subsection says. G2 still reads each from the tagged release.)*
  *(2026-11-12: "four more things gobble's `v0.6.0` must hold" now reads `v0.7.0`, the G2 release.)*
* *(2026-11-11, R2.)* Good, because no gobble sign-in the phone starts binds a port on the host, and the phone is
  never offered a gobble method it cannot finish.

### Confirmation

1. `go list -m all` names `.../authcontract` at its tag, with no `replace`; `TestSDKIsImportedOnlyByGrok` allows it in
   D1's packages and fails a planted `x/signin` import from `internal/ws`.
2. Every golden vector decodes and re-encodes in Go (`internal/protocol`) to the same bytes after compacting, and in
   Dart to an equal JSON value, every member present or absent as in the vector (`answer-confirm-false.json`'s
   `"confirm":false` included); every `forward/` vector keeps its unknown value; each seen failing on a vector edited in
   a scratch copy. Dart compares values, not bytes, because the vectors are Go's encoding, which escapes `<`, `>` and
   `&` in strings, and Dart's `jsonEncode` does not.
3. `make check-authcontract` passes, and fails on a hand edit of `authcontract.g.dart` and on a copied vector changed.
4. A wire test drives each agent's sign-in through `provider.auth_start`, `provider.auth_event`,
   `provider.auth_answer` and `provider.auth_cancel` against fakes, and asserts the steps of D7's table.
5. A test on a gobble fake that elicits with a request scope: the steps of D8's table reach the phone, a shown step's
   form answered with an empty `accept` and no `Answer`; a form whose `requestId` is not Magic's `authenticate` is
   cancelled; a sign-in held past `Spec.StartTimeout` and past the question timeout completes (the PLAN shortens both
   bounds to milliseconds and holds the sign-in for seconds, which tests the same path a ten-minute sign-in takes); a
   phone cancel sends `$/cancel_request`; every `authenticate` carries `_meta.gobble.fresh: true` *(2026-11-11, R2:
   and `_meta.gobble.remote: true`)*.
6. A session's deadline cancels it, recomputed when the first step that carries `expires_at` arrives; a detached
   session survives past the resume window and its terminal step is drained by `provider.auth_sessions` once; a
   session outlives its `provider.auth_start` request's context; a second session for one (provider, upstream) is
   refused `provider_auth_busy`.
7. A V2 client with another `app_contract`, through `auth` or through `pair.claim`, is refused `app_outdated`, not
   counted as a failed auth, and a pairing code is not consumed.
8. No log line, error, retained record or wire capture holds a planted paste sentinel.
9. `make preflight`, `make race`, the Linux test host's `go test -race ./...`, `make ci-windows` on the Windows test
   host, and the disclosure guard pass.
10. D12's owner-run runs are recorded with their outcomes.
11. *(2026-11-11, R1.)* D13: a status fixture's `sessionKind` and `consents` reach the auth block with each consent's
    text and links from the catalog; `provider.auth_consent` runs `gobble auth consent <upstream> <consent id>
    allow|deny` and maps exit 0, 1, 2 and any other to D13's outcomes, the store's error never reaching the phone; an
    allow for a consent with no text is refused before anything runs; a prompt error with `not_permitted` and
    `consent` becomes an error event carrying both; the phone shows the text and links before any allow; each seen
    failing on a planted break; the PLAN's L7 recorded.
    *(2026-11-11, later: aligned with gobble-cli's final 0052: each consent's text and links come from the
    status row's `consentTerms`, not the catalog (D13, "Read"); and a prompt error with `not_permitted` and no
    `consent` becomes an error event carrying neither.)*
    *(2026-11-11, later: the owner's two rulings on D13: a status error object gives the unreadable state on every
    upstream of the auth block, and a change for such an upstream is `consent_store_unreadable` with no command run,
    from a fresh read and from the cache; the phone shows that state, the contract's label for a known
    `session_kind`, and the value verbatim for one the pinned contract does not know.)*

## Pros and Cons of the Options

### A. One cut-over onto the contract

* Good, because it removes the vocabulary Magic owns today, which is what Q8 chose.
* Good, because gobble's elicitation sign-in reaches the phone with no mapping of gobble's steps.
* Good, because the lockstep release lets the old messages go at once, with no shim.
* Bad, because it is the largest change: protocol, daemon, every auth adapter and the phone, in one release.
* Bad, because it waits on two other repositories' tags.

### B. A Magic mirror of the contract

* Good, because Magic's wire moves only when Magic decides, and nothing waits on another repository.
* Bad, because every contract fact is mapped by hand again, which is the duplication the owner wants gone (F1, F18).
* Bad, because the Dart models stay hand-written, against Q9.
* Bad, because the owner chose against it (Q8).

### C. A staged migration

* Good, because an older phone keeps working through the change.
* Neutral, because it would let each agent move separately.
* Bad, because two wire vocabularies and two lifecycles live side by side for a release, which is the state F37
  describes.
* Bad, because the owner ruled it out: "old phones do not matter", "no compatibility shims".

## More Information

### Records amended or superseded in part, applied by the PLAN's P0 on acceptance

* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md):
  its "No dependency" bullet and its Confirmation (quoted in D1) superseded in part, for the module `authcontract`
  (D1).
* [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](0191-MADR-answer-agent-elicitation-forms-on-the-phone.md):
  D1's request-scope bullet amended (D8); D6 affirmed.
* [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md): D5 ("Auth methods are
  typed descriptors carrying declared inputs. A method is `{id, type: api_key|oauth_device|oauth_browser, label,
  inputs[]}`") superseded by the contract's `Method` (D4); D6 ("All new messages are gated behind a protocol v2
  capability (`provider_auth`)") reduced to D6's version check, the capability kept as an offer; D7 kept as the
  start-time rule for kilo and opencode; D13 kept (system browser); D16 kept, its entries reshaped (D4); D27's
  disconnect bullet amended for sign-ins (D5); D28's states carried as `coordinator_state` (D3). W3 stays deferred
  ([0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md)).
  Its §16 amendment's D33, D34 and D42, which land before this record under the owner's DF answer, are carried: D33's
  code lifetime becomes D5's deadline, D34's wiring rule becomes `SignInWired` (D5), and D42's projection stays on the
  auth block (D4).
* [0083-MADR-provider-auth-activation-and-layout-gaps.md](0083-MADR-provider-auth-activation-and-layout-gaps.md) D4
  ("`available: false` + `reason` (`keyring_managed`, `device_unsupported`, `browser_only`) on `AuthMethodPayload`"):
  the reasons become D4's list.
* [0107-MADR-grok-phone-api-key-and-device-code-auth.md](0107-MADR-grok-phone-api-key-and-device-code-auth.md): D3
  (browser host-only) kept until U1 or U7; D9's 600 s becomes `show_code`'s `expires_at`.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md): D3's `StartDeviceAuth` bullet
  ("runs `gobble auth login <id> --method device --json`") superseded by D8; D3's catalog bullet ("`oauth_device` for
  the providers `AuthProviders()` marks with a login") superseded by `_meta.gobble.methods`; D12 item 8
  ("`AuthCatalogList` equals `acpext.AuthProviders()` … `StartDeviceAuth` for a provider without a login is
  `ErrAuthMethodUnsupported`") superseded by the catalog from `_meta.gobble.methods` and D8's sign-in; D12 item 10
  ("only `internal/provider/gobble` imports acpext, and it imports no go-llmprovider-sdk, by source test") amended:
  it imports `authcontract`, the one SDK path D1 allows; D13's acpext list gains `v0.5.0`; D14 gate 3 (one
  acp-go-sdk2 tag on both sides) restated for the G2 release *(2026-11-11: that record's A10, accepted that day,
  says the gate is knowingly unmet from `912b9aa1` until gobble `v0.6.0`, on acp-go-sdk2 `v1.36.0`; the PLAN's G2
  and P7 step 3 carry the two checks)*; D16's "The gobble package does not import
  go-llmprovider-sdk" amended for `authcontract`.
  *(2026-11-12, of the A10 note above: gate 3 is met when gobble `v0.6.0` is published. gobble-cli releases it now
  from its `main`, on acp-go-sdk2 `v1.36.0` like this repository (gobble-cli
  `0055-MADR-release-v0-6-0-now-and-the-sign-in-redesign-as-v0-7-0.md`; 0179-MADR A10's note of this date). The G2 release, the
  redesign, is now `v0.7.0`, and the gate is read again there and at P7 step 3.)*
* [0086-MADR-phone-provider-auth-completion.md](0086-MADR-phone-provider-auth-completion.md) D6: its `browser_only`
  reason, already superseded for SuperGrok by `0074-MADR-remote-provider-auth-from-phone.md` §16 D30, is removed with the reason itself (D4).
* [0095-MADR-post-0094-assessment-and-debug-pass.md](0095-MADR-post-0094-assessment-and-debug-pass.md) D7's table,
  through `op_timeouts.json` (D3).

### Sub-decisions and their alternatives

* **The version check (D6).** Chosen: an `app_contract` integer. Not chosen: a protocol V3 that drops V1 and V2, which
  would also refuse the `Bearer` tools that settle on V1 (`server.go:657-659`); comparing release versions, which would
  refuse a development build and every unrelated release.
* **The Dart generator (D9).** Chosen: a repository script on Python's standard library. Not chosen: quicktype, which
  emits closed Dart enums that throw on an unknown value (against go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D6 rule 2) and adds an npm tool;
  `json_serializable` with `build_runner`, which generates from hand-written Dart, not from the schema, and adds two
  development dependencies.
* **The elicitation advertisement (D8).** Chosen by the owner (O4, 2026-11-10): form only. Not chosen: adding
  `elicitation.url`, which needs the trust policy of `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` and `elicitation/complete` and gains nothing `_meta.gobble.step`
  does not give; keeping request-scoped forms cancelled and driving gobble's CLI, which cannot carry a question
  (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D5).
* **The disconnect rule (D5).** Chosen: a sign-in lives to its own deadline. Not chosen: `0074-MADR-remote-provider-auth-from-phone.md` D27's resume window,
  which ends a sign-in two minutes after the phone's socket drops; a longer resume window for every connection, which
  keeps every disconnected session's state alive too.
* *(2026-11-11.)* **Where the consent is decided (D13).** Chosen by the owner (R1): inside this record. Not chosen: a
  record of its own after the cut-over, which keeps the P2 to P6 window as it was and leaves the phone showing
  gobble's message for a refused turn until then.
* *(2026-11-11.)* **Where the consent is carried (D13).** Chosen: the answer on the auth block, the change as
  `provider.auth_consent`. Not chosen: a step in D3's sign-in envelope, which has a `session_id` and a drain that a
  stored session's consent does not; a sign-in started only to change an answer, which gobble's SDK sign-in skips
  when the session already holds one (go-llmprovider-sdk 0075 D4, D8's note on `confirm`), and which would run a
  whole browser sign-in to change one answer; an ACP route, which gobble has none of for a consent (gobble-cli 0052 D3
  names its CLI command alone).

### Dependencies D13 found (2026-11-11)

Each is a fact D13 needs from gobble-cli or the SDK that their records, as read on 2026-11-11, do not state. Items 1
to 3 are read again at the PLAN's G2 from the tagged release. Item 2 is a stop there if the release does not meet it;
items 1 and 3 are recorded and stop nothing, since D13 degrades to what it shows today without them; item 4 waits for
nothing.

*(2026-11-11, later: aligned with gobble-cli's final 0052 and 0051. Items 1 to 3 are resolved in gobble's
records, each as its note says. G2 still reads each from the tagged release, and its R1 bullets say which miss is a
stop. Item 4 is unchanged.)*

1. **Whose wording the consent's text is.** gobble-cli 0051 D2 builds `_meta.gobble.methods` from `provider.Offered`,
   which is `signin.Methods(d)` with gobble's policy applied; go-llmprovider-sdk 0075 D1 fills `Consents[].text` from
   the SDK's default `ConsentText`. gobble's own wording, which names its host command and which its `confirm` step
   shows at sign-in (gobble-cli 0052 D2), reaches the SDK only as `signin.Options.ConsentText`, for the step. So, as
   the records read, the phone's consent sheet would show the SDK's default text while the sign-in's `confirm` showed
   gobble's. A text and a link exist either way, so D13 is not blocked; whether gobble puts its wording into the
   advertised `Consent.text` is gobble's to decide, and D13 shows whichever text gobble advertises.
   *(2026-11-11, later: **resolved** by gobble-cli 0052 D2's note of that day, with 0051 D2's and D4's notes:
   `provider.Offered` sets each honoured consent's `text` to gobble's wording, the text its `confirm` step shows,
   and keeps the SDK's `links`; the advertised `consents[]` and 0052 D8's new `consentTerms` carry that one
   wording. D13 reads it from `consentTerms`.)*
2. **What `<consent>` `gobble auth consent` takes.** gobble-cli 0052 D3 says gobble's word for
   `chatgpt_plan.background_use` is `background` and "the id stays the SDK's", and its PLAN's examples write
   `gobble auth consent openai background deny`. Neither says that the command accepts the id. D13 passes the id, as
   the status and `ErrorData.consent` name it, so it needs gobble's command to accept the id. Mapping the id to
   gobble's word in Magic would be a hard-coded table, which the owner's standing rule calls code creep.
   *(2026-11-11, later: **resolved** by gobble-cli 0052 D3's note of that day: `<consent>` is the SDK's consent
   id, the one `consents`, `consentTerms` and `ErrorData.Consent` carry, and `background` is kept as its alias for
   a person at a terminal. D13 passes the id and maps no word.)*
3. **Whether the turn gobble ends carries the consent.** `ErrorData.consent`, as the coordinator fixed it, is set
   with `not_permitted` when gobble refuses a request because a consent is not granted. gobble-cli 0052 D2 ends an overflow turn with `errPlanOverflow` for a
   declined consent and with an error of its own for an answer it could not read, and says neither's `ErrorData`.
   D13's prompt appears only when both members are present; without them the turn's error is shown as today.
   *(2026-11-11, later: **resolved** by gobble-cli 0052 D2's note of that day, with 0051 D7's: an overflow
   refused for want of a grant, or an SDK refusal that gobble's read then finds ungranted, carries `not_permitted`
   and `consent`; an answer that cannot be read carries `not_permitted` and no `consent`, so the phone shows the
   error and offers no allow.)*
4. **A session kind in the contract.** Not gobble's: the contract has no session-kind type (go-llmprovider-sdk 0073
   D2), so D13 carries `session_kind` as an opaque string. If the contract later gains one, the generator picks it up
   and the envelope can name it; nothing waits for it.
   *(2026-11-11, later: **resolved** by the owner's ruling on session-kind labels, through the contract's
   session-kind type and its labels (go-llmprovider-sdk 0073-MADR, amendment of 2026-11-11), added before
   `authcontract/v0.1.0`. D13 carries the kind as that type and the phone shows its label (D13, "The contract's
   types"). "nothing waits for it" no longer holds: the PLAN's G1 reads the type and its labels from the tag, and a
   miss is a stop.)*

### Where the report, the audit and the drafts differ from the code or from this record

* **The report's §5.5, §5.6.2 and §8.4 route gobble through `--json --answers`, and §8.3 amends 0191 "only at the
  elicitation phase".** Q7 chose elicitation now; gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D5 has no answer channel.
* **The report's M23 makes codex's access token a `form` method.** The contract's `completion` `value` names exactly
  this, and `provider.set_credential` already carries a redacted secret; D3 uses it.
* **The report's F4 asks for "one constant" for `host_oauth`.** `protocol.AuthReasonHostOAuth` exists
  (`messages.go:786`) and has no user: `httpagent` and `opencode` do not import `protocol`, whose import of
  `session` would close a cycle through `session`'s tests (`authcatalog.go:259-260`, `acp_auth.go:69-73`). D4 puts the
  constants in `internal/provider`.
* **The report's F43 says `provider.start_auth` "can hang".** More exactly: the context `firstDeviceCode` checks is
  `context.WithoutCancel`'s (`server.go:2652`), so its `ctx.Err()` check (`gobble/auth.go:406`) never fires; the async
  slot comes back after 35 s (`server.go:1003-1012`) and the goroutine stays blocked.
* **Neither the report nor go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` names `TestSDKIsImportedOnlyByGrok`,** which refuses the
  contract module outside grok (D1).
* **go-llmprovider-sdk `0073-MADR-publish-an-auth-contract-module.md` D2, as first drafted, had no `Kind` or `Completion` for a method decided at start,
  no field for a host command, and mapped `open_url` and `show_code` onto URL mode,** which a form-only client such as
  Magic cannot receive. On 2026-11-10 it gained `oauth`, `at_start` and `command` (O1, O3) and `Step.secret`, and
  left the ACP mapping to gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4.
* **gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4 step 2, as first drafted, short-circuited a named method when a credential of its kind is
  stored.** On 2026-11-10 it gained `_meta.gobble.fresh` (O2).

### Questions answered by the owner (2026-11-10)

The owner answered O1, O2 and O4 as recommended, and O3 against the recommendation. The options are kept as they were
put.

* **O1. "Decided at start" in the contract.** (a) *Recommended, chosen:* ask go-llmprovider-sdk to add `KindOAuth
  "oauth"` and `CompletionAtStart "at_start"` before `authcontract/v0.1.0`; (b) Magic lists such methods as
  `host_only` until the agents declare their mechanism, which hides kilo's and opencode's device sign-ins; (c) Magic
  lists them as `device_code` and fails a browser one at start, which states a fact Magic does not have.
* **O2. A fresh gobble sign-in over a stored session.** (a) *Recommended, chosen:* ask gobble-cli for
  `_meta.gobble.fresh: true` on `authenticate`, signing in whatever is stored, the old credential replaced only if the
  new one saves; (b) Magic runs `gobble auth logout <id>` first, which revokes the old session at the issuer before the
  new one exists; (c) accept it: a phone re-sign-in needs the session cleared on the host.
* **O3. The host command.** (a) *Recommended:* `Method.detail` holds it now, and a contract field is asked for later;
  (b) *chosen:* ask for a `command` field before `v0.1.0`, which the phone shows with a copy button; (c) show "Finish
  on the host" with no command.

* **O4. Whether Magic advertises `elicitation.url` for gobble's sign-in (D8).** Found in the consistency pass of
  2026-11-10; answered by the owner (2026-11-10): (a), "Form only. gobble sends `open_url` and `show_code` as form text
  with `_meta.gobble.step`; the phone renders each step from it; 0191's URL trust rules are unchanged."
  (a) *Recommended, chosen:* form only, as D8 drafts it; gobble sends `open_url` and `show_code` as forms whose message
  holds the URL and code (gobble-cli `0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md` D4's
  right-hand column), and the phone renders the step from `_meta.gobble.step`; no trust policy and no
  `elicitation/complete` to track. (b) Advertise `elicitation.url` on the sign-in connection only; gobble then sends URL
  mode and `elicitation/complete` (its left-hand column), and Magic needs the URL trust policy of `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` and a handler for
  `elicitation/complete`, which is method-not-found today (`elicitation.go:112-114`). (c) Advertise `elicitation.url`
  everywhere, sessions included, which reopens `0191-MADR-answer-agent-elicitation-forms-on-the-phone.md` D1's URL bullet for every agent, not only gobble's sign-in.

### Question answered by the owner (2026-11-10)

The owner chose O5 (a), as recommended, on 2026-11-10: each of the three is accepted as named Magic-side logic,
confined to its mapper, pinned by its live probe, and removed when the agent states the fact itself. The options are
kept as they were put.

* **O5. Whether three named pieces of Magic-side logic are accepted (D7).** Found by the review of 2026-11-10. The
  owner's standing rule calls Magic-side flow logic and hard-coded tables code creep, and no recorded answer covers
  these three: the codex and grok code lifetimes in the mappers (15 minutes, 600 s); kilo's method ids minted from
  labels (`0074-MADR-remote-provider-auth-from-phone.md` §16 D31); and the orchestration of `codex login` for the replay (`0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md` D4). Only the code that
  holds them waits for the answer: the two lifetimes in the PLAN's P3, D31 in `0074-PLAN-remote-provider-auth-from-phone.md` P23 step 2, and `0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md` P5,
  which also waits for U13.
  (a) *Recommended:* accept each as named Magic-side logic, confined to its mapper, pinned by its live probe, and
  removed when the agent states the fact itself (a structured expiry, an id in kilo's catalog, a paste channel in
  codex); (b) remove them: a session whose agent states no expiry takes `DefaultTTL`, kilo's ids stay array indexes
  with F5's reorder bug, and codex's browser method stays `host_only`; (c) ask codex, grok and kilo upstream for the
  structured facts first, and hold these three features until they ship.

### Not verified, with the probe that settles it

* **P-B.** Whether the phone's socket drops while the user is in the browser, on Android and iOS. Owner-run; asks
  first; a scratch daemon home (`MCREMOTE_HOME` and `XDG_*` in a temporary directory) and a scratch agent home for the
  sign-in it starts; no sign-in completed (a code shown and left); quota: none. The PLAN's L6.
* **U5, U6, U13** are `0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`'s. **U1, U7** (grok's browser) are go-llmprovider-sdk's and the report's.
* **P-C. Whether codex's access-token login yields a refreshable session or an access-only one, and the shape of its
  `auth.json`.** It decides whether the coordinator can adopt the result: codex's adapter treats any `tokens` object as
  ChatGPT mode, `Revocable: true`, ordered by `last_refresh` (`codex/adapter.go:150-159`), and `NotOlder` orders by
  that signal (`providerauth/adapter.go:71-87`), so an access-only file may fail `Validate` or never be adopted. The
  PLAN's P3 step 0 runs it before `token_paste` is built, and its result sets the method's `yields` and `refreshable`;
  an access-only result amends D7 for whether the coordinator adopts it. Owner-run; asks first; scratch `CODEX_HOME`;
  a dummy value fails, so a real token is needed; quota: none.

### Related

* PLAN: [0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md).
* The replay: [0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md).
* The defects first: [0074-PLAN-remote-provider-auth-from-phone.md](0074-PLAN-remote-provider-auth-from-phone.md)
  P23, and [0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md) P7.
* go-llmprovider-sdk: `docs/decisions/0073-MADR-publish-an-auth-contract-module.md`,
  `docs/decisions/0074-MADR-one-sign-in-handle-and-method-facts.md`,
  `docs/decisions/0075-MADR-chatgpt-plan-consent-host-id-and-client-reuse.md`,
  `docs/decisions/0076-MADR-auth-error-kinds-and-doc-drift.md`.
* gobble-cli: `docs/decisions/0051-MADR-sign-in-through-acp-elicitation-on-the-sdk-auth-contract.md`,
  `docs/decisions/0052-MADR-finish-the-chatgpt-plan-sign-in-on-the-sdk-engine.md`.
