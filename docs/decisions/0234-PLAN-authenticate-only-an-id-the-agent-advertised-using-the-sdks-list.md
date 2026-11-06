---
status: proposed
date: 2026-11-06
associated-madr: "0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement authenticating only an id the agent advertised, using the SDK's list

Associated MADR: [0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md](0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md)

## Goal

Magic authenticates only with an id the agent advertised, taken from the SDK's own list. When a
key is present and the agent did not advertise its method, the start fails with
`ErrACPAuthRequired`, and the error text names the cause and the remedy. Live tests pin the grok
1.0.70 behaviour this rests on, and the records that said otherwise are corrected. The release
hold (MADR D7) ends when P1 lands.

## Scope

**In:**

* P1: the MADR's D1 to D4;
* P2: D5;
* P3: D6, and the closing of this pair.

**Out:**

* Option F, Magic writing grok's `env_key` line itself, which would need a record of its own;
* any change to `grokHasAPIKey`. F5 shows that a key named by `env_key` is advertised only while
  `XAI_API_KEY` is set, and the function already counts that variable;
* the `auth.json` rows of 0085's table, which F5 did not re-probe;
* 0233's pair, which has a separate approval.

## Constraints

* **C1 — One phase, one commit.** The agent stages, and the owner commits with
  `git commit --no-edit`. `make pre-add-check FILES="…"` passes on every staged Go file first.
* **C2 — No push and no tag without an explicit ask in the same turn.** Before asking for a push,
  run the disclosure guard's `pre-push` over the outgoing commits (AGENTS.md).
* **C3 — The owner's word before each host run.** That covers the Linux test host and the Windows
  test host, in P1. It is asked for at approval.
* **C4 — Each new test is seen failing first, on a scratch copy, never by dirtying the tree.**
  The handoff says what each failure looked like.
* **C5 — `acpagent` names no vendor.** That includes its tests and `acptest`.
  `TestNoProviderIsNamedInAcpagent` must pass, so these tests use `vendor.key` and
  `vendor.browser`.
* **C6 — Live grok tests send `initialize` only, with the placeholder key `xai-probe-not-real`.**
  They spend no provider quota, and no real key is read or used.
* **C7 — Records carry no identifier.** No hostname, account name or real path. Run the
  disclosure guard's `redact` preview and the identifier scan before staging.

## Implementation Steps

### P1 — Select from the SDK's list, and fail clearly on an unadvertised key (D1 to D4)

Files:

* `internal/provider/acpagent/acpagent.go`
* `internal/provider/acpagent/authselect.go`
* `internal/provider/acpagent/session.go`
* `internal/provider/acpagent/authselect_test.go`
* `internal/provider/acpagent/acptest/acptest_test.go`, or a new `_test.go` file beside it
* `internal/provider/grok/grok.go`
* a grok unit test for the hint

Steps:

1. **Tests first, on scratch copies of `HEAD`.** Write D4's tests, and run each on a copy where
   it must fail (C4):
   * **The refusal case.** A Spec with `SafeAuthMethodIDs {vendor.token, vendor.key}`,
     `KeyAuthMethodID vendor.key` and `HasHeadlessCredential` true runs against an agent
     advertising `vendor.browser` only. It calls the provider's `StartOn` over the fixture agent,
     as Probe 1 did.
     * It asserts `errors.Is(err, acpagent.ErrACPAuthRequired)`.
     * It asserts that `Requests()` is exactly `initialize`.
     * It asserts that the error text contains `vendor.key` and the Spec's hint.
     * Expected on `HEAD`: it fails, with an error wrapping `ErrUnadvertisedAuthMethod`.
   * **The advertised case.** The same Spec runs against an agent advertising `vendor.key` and
     `vendor.browser`. The agent receives `authenticate` with `methodId` `vendor.key`, then
     `session/new`.
     * Expected to fail on a copy whose `selectACPAuthMethod` always returns `""`.
   * **The selection case.** The `TestSelectACPAuthMethod` case "cold+file key" is renamed
     "cold+unadvertised key", and expects `""`.
     * Expected on `HEAD`: `got "vendor.key", want ""`.
2. **D1.** In `applyInitializeResponse` (`acpagent.go:1480-1486`), replace the loop with
   `acp.AuthenticateMethodIDs(initResp.AuthMethods)`, converted to `[]string`. The conversion keeps
   the SDK's order.
3. **D2.** In `selectACPAuthMethod` (`authselect.go:22-38`), the key branch also requires
   `slices.Contains(advertised, keyMethod)`.
   * The doc comment loses "advertised or not", and cites this MADR as superseding 0085 D2 step 4.
   * The comments on `KeyAuthMethodID` and `HasHeadlessCredential` (`acpagent.go:118-123`) are
     updated to match.
4. **D3.**
   * Add `KeyNotAdvertisedHint string` to `acpagent.Spec`, beside `KeyAuthMethodID`.
   * The session records `keyNotAdvertised` when a key was present and the key method was not
     advertised (`acpagent.go:716-735`, `session.go:89-92`).
   * `rejectUnauthenticated` (`authselect.go:40-45`) then returns:
     `acp: authentication required (advertised %v; a key is present, but the agent did not advertise %q[: <hint>]): %w`,
     still wrapping `ErrACPAuthRequired`.
   * Without a key, the text is unchanged.
5. **grok's hint.** In `internal/provider/grok/grok.go`, set `KeyNotAdvertisedHint` to a sentence
   naming D3's three remedies:
   1. `env_key = "XAI_API_KEY"` in a `[model."<id>"]` table of grok's `config.toml`;
   2. setting the key from the phone;
   3. `grok login`. `grok --help` on 1.0.70 lists `login` as "Sign in to Grok".

   A grok unit test asserts that the hint is set and names `env_key`.
6. **Run the tests on the tree.** The P1 tests pass. `TestNoProviderIsNamedInAcpagent` passes.
7. **Re-run Probe 1** (`libs/authprobe.py`) on a scratch copy of the changed tree. Expected:
   `ErrACPAuthRequired`, and the agent receives `initialize` only.
8. **Re-run Probe 2,** pointed at the changed `advertisedAuth`. Expected: the SDK's list in every
   row of F7.
9. **Gates:**
   * `make race` on this host;
   * `go test -race ./...` on the Linux test host (C3);
   * `make ci-windows` on the Windows test host (C3).
10. **Stage.** Run `make pre-add-check` on the staged Go files, then stage P1 and this PLAN's P1
    execution record.

After the owner commits P1, the release hold (D7) ends.

### P2 — Pin grok 1.0.70's answers live (D5)

Files: `internal/provider/grok/live_auth_test.go`.

1. **Add three tests beside `TestLiveColdInitializeOnlyGrokCom`.** Each uses `isolateGrokHome`,
   `startACPInit` and `initAuthMethods`:
   * `TestLiveColdEnvKeyIsNotAdvertised`: `XAI_API_KEY` is the placeholder. Want ids `[grok.com]`
     and an empty default.
   * `TestLiveEnvKeyTableAdvertisesAPIKey`: `XAI_API_KEY` is the placeholder, and `config.toml`
     holds `[model."grok-4.5"]` and `[model."grok-4.6"]`, each with `env_key = "XAI_API_KEY"`. Want
     the ids to contain `xai.api_key`, and default `xai.api_key`.
   * `TestLiveEnvKeyTableWithoutTheVariable`: the same file, with `XAI_API_KEY` unset. Want
     `[grok.com]`.
2. **Seen failing first, on scratch copies.** Each test runs with another test's state swapped
   in, and must fail:
   * the first, with the `env_key` table added;
   * the second, with the table removed;
   * the third, with the variable set.
3. **Run the three on this host:**
   `go test -tags live_grok -count=1 -v -run '^(TestLiveColdEnvKeyIsNotAdvertised|TestLiveEnvKeyTableAdvertisesAPIKey|TestLiveEnvKeyTableWithoutTheVariable)$' ./internal/provider/grok/`.
   They pass on grok 1.0.70.
4. **Stage.** Run `make pre-add-check` on the file, then stage P2 and its execution record.

### P3 — Correct the records, and close the pair (D6)

Files:

* `docs/decisions/0217-MADR-update-every-pin-to-its-current-release.md`
* `docs/decisions/0085-MADR-grok-acp-auth-method-wiring.md`
* this pair
* `docs/README.md`, regenerated

1. **0217's F3f.** Its bullet on S8 (`0217-MADR-…:261-265`) gets a dated correction, 2026-11-06,
   under it. The bullet says "Magic's auth selection picks from the advertised list".
   * The correction says that 0085 D2 step 4 sent an unadvertised `xai.api_key`, and that S8
     refuses it. It points at the 0234 MADR's F4.
   * The original text stays.
   * F3g is not changed (MADR D6).
2. **0085's D2 step 4.** It is annotated "superseded 2026-11-06 by
   0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md D2". 0085's
   table gets a note that F5 re-probed its first three rows on grok 1.0.70.
3. **This pair.** The MADR moves to `accepted`, with the date. This PLAN moves to `complete`, with
   each phase's execution record.
4. **Checks.** Regenerate the index with `python3 scripts/check_records.py --write-index`. Then
   `python3 scripts/check_records.py --check-all`, markdownlint on the changed records, the
   guard's `redact` preview, and the identifier scan. Stage.

## Verification

| Check | Phase | Expected |
| :--- | :--- | :--- |
| Refusal case on a scratch copy of `HEAD` | P1 | fails, with an error wrapping `ErrUnadvertisedAuthMethod` |
| Advertised case on a copy whose selection returns `""` | P1 | fails, with no `authenticate` sent |
| "cold+unadvertised key" on a scratch copy of `HEAD` | P1 | fails with `got "vendor.key", want ""` |
| The three, on the tree | P1 | pass |
| `TestNoProviderIsNamedInAcpagent` | P1 | passes |
| Probe 1 on the changed tree | P1 | `ErrACPAuthRequired`; the agent receives `initialize` only |
| Probe 2 on the changed list | P1 | the SDK's list in every row |
| `make race`; the Linux test host's `go test -race ./...`; `make ci-windows` | P1 | pass |
| Each live test, with another test's state swapped in | P2 | fails |
| The three live tests on grok 1.0.70 | P2 | pass |
| `check_records.py --check-all`, markdownlint, `redact` preview, identifier scan | every phase | clean |

## Rollout and Rollback

* **Rollout:** P1 ships in the first release after the hold. Users then see D3's message, and
  not the SDK's refusal.
  * An env-only cold grok host needs one config line, the phone's key write, or a sign-in. The
    message says so.
  * The release notes say the same, pointing at that message.
* **Rollback:** revert P1's commit. Before reverting, check whether the release hold has already
  ended.
  * The revert alone brings back F4's opaque failure, not v0.24.0's behaviour, since the SDK still
    refuses the id.
  * A real rollback needs Option D, the SDK pin moved back, under its own record.
* P2 and P3 are tests and records. Reverting them changes no behaviour.

## Execution record

Nothing has been executed under this PLAN. The probe behind the MADR's F5 ran before it, with the
owner's approval of 2026-11-06, and is recorded in the MADR.
