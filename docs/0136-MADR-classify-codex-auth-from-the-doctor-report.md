---
status: proposed
date: 2026-09-03
decision-makers: maccavelli
consulted: —
informed: —
---

# Codex credential reality is read from `codex doctor --json`, not inferred from an exit code

## Context and Problem Statement

`ObserveCredentialStore` decides whether mcremote can protect Codex's
credential. Since MADR 0134 that decision also drives a durable manifest state,
so a wrong answer is not cosmetic: `external` suppresses the operator warning
and the phone's `credential_failed`, which is correct for a credential we
cannot protect and dangerous for one that is simply broken.

The probe it rests on does not work. `cliIsAuthenticated`
(`internal/provider/codex/store_reality.go`) returns `cmd.Run() == nil` for
`codex login status`, and that exit status is **always zero**. Verified against
isolated `CODEX_HOME` directories on codex-cli 0.152.1:

```text
home with no auth.json at all  ->  "Not logged in"              exit 0
home containing `{}`           ->  "Logged in using ChatGPT"    exit 0
```

Two consequences follow mechanically. `RealityLoggedOut` is unreachable
whenever the binary runs. And `RealityExternal` — "the CLI is authenticated but
not from the file" — is returned for **any** host whose `auth.json` is
unusable, including one that is signed out or corrupt.

That is what happened on the reporting host. MADR 0134's Phase 6 recorded the
daemon falling silent as success; it had in fact classified a genuinely broken
credential as unprotectable. `codex doctor` on the same host says so plainly —
`stored ChatGPT tokens: false`, `stored auth issue: ChatGPT auth is missing
refresh metadata`, and a websocket handshake returning `401 Unauthorized` —
while `auth storage mode` is `File`, meaning `auth.json` is exactly where Codex
intends to keep it.

Worse, the text the exit code stands in for is itself misleading: `{}` makes
the CLI print "Logged in using ChatGPT". The string agrees with the broken
exit code, so parsing it would not help.

### The CLI already answers the question properly

`codex doctor --json` emits a report with a top-level `schemaVersion` (`1` on
0.152.1) and a `checks["auth.credentials"]` entry that distinguishes every case
the exit code collapses. Measured directly:

| Codex home | `status` | `summary` | `auth storage mode` |
| --- | --- | --- | --- |
| no `auth.json` | `fail` | no Codex credentials were found | `File` |
| `{}` stub | `fail` | stored credentials are incomplete | `File` |
| reporting host | `warning` | auth is provided by environment, but stored credentials are incomplete | `File` |
| `-c cli_auth_credentials_store="keyring"` | `fail` | no Codex credentials were found | `Keyring` |

It also reports `stored ChatGPT tokens`, `stored API key`, `stored agent
identity`, a `stored auth issue` list, and `auth env vars present`. The probe
costs about 1.3 s.

The `auth storage mode` row matters most. It is the **resolved** backend,
reported by the CLI itself. `DetectCredentialStore`
(`internal/provider/codex/authstore.go`) currently infers the backend by reading
`config.toml` with a deliberately limited non-TOML reader that only sees bare
top-level keys before the first table header. That reader cannot see a key set
under a profile or via `-c`, and it treats `auto` as unsupported outright —
even though `auto` resolves to the file backend on a host with no usable
keyring, which mcremote *can* protect.

### The environment case, which nothing in the model covers

The reporting host also shows `auth env vars present: OPENAI_API_KEY`, and
Codex reports auth as coming from the environment. This is not a storage
backend and must not be treated as one: it is **per-process**. The user's shell
has that variable; the daemon's LaunchAgent does not — it sets only `HOME`,
`LOGNAME`, `PATH`, `USER` and the XDG variables. So the same host is
"authenticated" for a terminal and not for the daemon, and any classification
that folds environment auth into a durable, host-wide state would be wrong for
one of the two.

### What is not established

The full set of `status` values `auth.credentials` can take is unknown; `ok`,
`warning` and `fail` were observed, and the enumeration is not documented. The
official auth page (<https://learn.chatgpt.com/docs/auth.md>) documents
`cli_auth_credentials_store` and its `file` / `keyring` / `auto` values but
describes no precedence between the environment variable and stored
credentials, and does not mention the `schemaVersion` field or any stability
guarantee for the report. Treating the schema as stable is therefore an
assumption, and this decision handles its violation rather than relying on it.

## Decision Drivers

* A probe that cannot fail is not a probe. Whatever replaces it must give
  different answers for signed-out, broken, and unprotectable.
* Under-refusing is the dangerous direction here: a broken credential silently
  classified as unprotectable is the failure MADR 0134 shipped.
* Never guess from an unrecognised answer. An unknown schema, a missing field,
  or a parse failure must fall back to the conservative classification, not to
  a convenient one.
* The classification is host-wide and durable; per-process facts such as an
  environment variable must not enter it.
* The probe already sits on a degraded path and is cached. Cost is acceptable
  there and nowhere else.
* Depending on another tool's JSON is a coupling that must be visible and
  version-gated, not implicit.

## Considered Options

* Parse the `auth.credentials` check from `codex doctor --json`
* Parse the human-readable output of `codex login status`
* Drop the CLI probe and classify from `auth.json` plus `config.toml` alone
* Keep the exit-code probe and revert MADR 0134's trigger, so its answer stops
  driving a state

## Decision Outcome

Chosen option: "Parse the `auth.credentials` check from `codex doctor --json`",
because it is the only source that reports the resolved storage backend and the
presence of usable stored credentials as separate, machine-readable facts —
which is precisely the distinction every other option collapses.

The classification becomes:

1. **Unreadable, unparseable, or `schemaVersion` not recognised** →
   `RealityUnknown`. Callers treat it exactly as they treat today's unknown: no
   `external`, and MADR 0133's escalation stands.
2. **`auth storage mode` is not `File`** → `RealityUnsupported`. A keyring
   credential cannot be protected by this coordinator whether or not one is
   currently stored.
3. **`auth storage mode` is `File`** — the file is the store, so the file
   decides:
   * a usable stored credential → `RealityFileProtected`;
   * no stored credential at all → `RealityLoggedOut`, reachable for the first
     time;
   * a stored credential that is present but incomplete → **not external**. It
     is the corruption case, and MADR 0133's escalation to `recovery_required`
     is correct for it.
4. **`auth env vars present` is recorded and never classified.** It may be
   logged to explain a discrepancy to an operator; it must not move the state.

`RealityExternal` is **retired**. It was defined as "the CLI is authenticated
but not from the file", and every host that produced it did so through the
broken exit code. Under this classification the genuine cases are covered by
`RealityUnsupported`, and nothing is left that the name describes.

MADR 0134's `StateExternal` survives unchanged in meaning and is now entered
from `RealityUnsupported` — the backend is not the file this coordinator
protects, so there is nothing to protect and nothing for an operator to decide.
That is what the state's own documentation already says; only the evidence
changes.

### Consequences

* Good, because the reporting host is classified correctly for the first time:
  a broken credential escalates again instead of falling silent.
* Good, because `RealityLoggedOut` becomes reachable, so "signed out" and
  "corrupt" stop being the same answer.
* Good, because the backend comes from the CLI's own resolution, so a key set
  under a profile or via `-c` is seen, and `auto` resolving to the file backend
  is protected rather than refused.
* Good, because MADR 0134's state keeps its meaning while resting on evidence
  that supports it.
* Bad, because it couples mcremote to another tool's JSON report. Mitigated by
  gating on `schemaVersion` and degrading to `RealityUnknown`, which is a safe
  answer — but a Codex release that renames the check or the fields will silence
  the improvement until someone notices.
* Bad, because the probe is roughly 1.3 s and performs network reachability
  checks that the previous one did not. It stays on the degraded path, behind
  the existing cache, and inside `ProbeTimeout`; a host with a working
  credential never reaches it.
* Neutral, because `DetectCredentialStore` is not deleted. It keeps answering
  when the CLI cannot be run at all, where its limitations are better than
  nothing.

### Confirmation

* Table-driven tests over recorded `doctor --json` fixtures — the four rows
  measured above, captured verbatim — assert one classification each.
* A test asserts an unrecognised `schemaVersion` yields `RealityUnknown`, and
  another asserts the same for malformed JSON and for a missing
  `auth.credentials` key.
* A test asserts `auth env vars present` alone never produces `external` or
  `unsupported`, using the reporting host's fixture, which has it set.
* A test asserts a `File` backend with an incomplete credential escalates to
  `recovery_required` — the regression MADR 0134 introduced, run against the
  current tree first to watch it classify as `external`.
* On the reporting host, before any Codex re-login: the daemon logs the
  operator-decision warning for codex again, and the manifest leaves
  `external`. That is a *deliberate* return of the warning, because the
  credential really is broken.
* After a repaired `codex login`: `doctor` reports `auth.credentials` ok, the
  provider returns to `idle`, and a `refresh` generation is recorded.

## Pros and Cons of the Options

### Parse the `auth.credentials` check from `codex doctor --json`

* Good, because it separates backend from credential health, which is the
  distinction the whole classification needs.
* Good, because it is explicitly machine-readable and carries a
  `schemaVersion` to gate on.
* Good, because it makes two currently-unreachable classifications reachable
  and one currently-wrong classification right.
* Neutral, because it replaces a hand-rolled config reader with the CLI's own
  resolution, trading our bug surface for their contract.
* Bad, because of the coupling and the added latency and network I/O.

### Parse the human-readable output of `codex login status`

* Good, because it is cheap, needs no network, and needs no new dependency.
* Bad, because the text is wrong: `{}` prints "Logged in using ChatGPT" while
  doctor calls the same credential incomplete. A probe that parses it inherits
  the lie the exit code told.
* Bad, because it reports no backend at all, so `unsupported` would still have
  to come from the existing config reader and its blind spots.
* Bad, because human-readable output carries no stability contract whatsoever.

### Drop the CLI probe and classify from `auth.json` plus `config.toml` alone

* Good, because it is the fastest option, spawns nothing, and removes a
  dependency rather than adding one.
* Good, because mcremote already parses `auth.json` in `Validate`, so
  "usable credential present" needs no new code.
* Bad, because the backend can only be guessed from `config.toml`, which is
  exactly the reader that cannot see profile keys, `-c` overrides, or what
  `auto` resolved to.
* Bad, because it cannot distinguish "keyring holds a good credential" from
  "keyring holds nothing", so the operator message would be wrong half the time.

### Keep the exit-code probe and revert MADR 0134's trigger

* Good, because it is small, immediate, and removes the shipped defect: broken
  credentials would escalate again.
* Good, because it takes no new dependency and needs no fixtures.
* Bad, because it abandons the genuine case — a keyring-backed host still gets
  an operator prompt whose every answer is destructive.
* Bad, because it leaves a probe in the tree that cannot fail, which will be
  trusted again by the next person who reads its name.

## More Information

* The broken probe: `internal/provider/codex/store_reality.go`
  (`cliIsAuthenticated`, `ObserveCredentialStore`, `ObserveCredentialStoreCached`).
* The config-reading backend detector:
  `internal/provider/codex/authstore.go` (`DetectCredentialStore`).
* The consumer this feeds since 0134:
  `internal/provider/codex/adapter.go` (`CredentialIsExternal`),
  `internal/providerauth/recovery.go` (`credentialIsExternal`, `recoverIdle`).
* Evidence and the invalidated Phase 6:
  [0134-MADR-an-external-credential-store-is-not-an-ambiguity.md](0134-MADR-an-external-credential-store-is-not-an-ambiguity.md)
  ("Amendment, 2026-09-03") and its plan's 2026-09-03 deviation.
* The escalation this restores:
  [0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md](0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md).
* Upstream: <https://learn.chatgpt.com/docs/auth.md>;
  <https://github.com/openai/codex/issues/5212> for the environment-variable
  behaviour.
* Implementation:
  [0136-PLAN-classify-codex-auth-from-the-doctor-report.md](0136-PLAN-classify-codex-auth-from-the-doctor-report.md).
