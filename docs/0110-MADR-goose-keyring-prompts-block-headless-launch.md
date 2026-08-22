---
status: accepted
date: 2026-08-21
decision-makers: Project Owner (scope and acceptance)
consulted: block/goose documentation; macOS keychain ACL research; local host probes
informed: Implementers of the daemon, Goose provider, and host setup tooling
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Stop Goose keychain prompts from blocking headless launch

## Context and Problem Statement

Launching Goose from the phone fails unless a human is physically at the Mac.
Goose reads its secrets from the macOS Keychain, and macOS prompts for the
login password on every read. Choosing **Always Allow** does not suppress the
next prompt.

That defeats the product's central promise. mcremote exists so an operator can
drive coding agents from a phone; a provider that requires someone standing at
the host keyboard is not remotely operable at all. Every other provider —
Codex, Grok, OpenCode, Kilo — reads a file and starts unattended. Goose is the
only outlier.

This record asks:

> Why does "Always Allow" not persist for Goose, and what is the smallest
> durable change that lets a phone-initiated Goose session start with nobody at
> the host?

### Facts established on this host

All observations are from 2026-08-21 on the affected machine.

| ID | Finding | Evidence | Confidence |
| --- | --- | --- | --- |
| **F1** | Goose stores its secrets in the macOS Keychain as a single item. | `security find-generic-password -s goose` returns one generic-password item with `"svce"="goose"` and `"acct"="secrets"`. These match the identifiers block/goose documents for keyring storage. | Confirmed by probe. |
| **F2** | There is no file-based secret store on this host. | `~/.config/goose/secrets.yaml` does not exist, and `~/.config/goose/config.yaml` contains no `GOOSE_DISABLE_KEYRING` key. | Confirmed by probe. |
| **F3** | The `goose` binary is ad-hoc, linker-signed, with no team identity. | `codesign -dv` on `/Users/saxsmith/.local/bin/goose` reports `Signature=adhoc`, `flags=0x20002(adhoc,linker-signed)`, `TeamIdentifier=not set`, and a per-build `Identifier=goose-9212d893d42becc0`. | Confirmed by probe. |
| **F4** | The binary is rebuilt frequently and its identity changes each time. | The installed binary's mtime is 2026-08-21 11:05, and the identifier embeds a build-specific hash. | Confirmed by probe. |
| **F5** | The keychain item is **not** being rewritten on each launch. | The item's `cdat` is 2026-07-25 and `mdat` is 2026-08-16 — both older than the current binary and unchanged across today's repeated prompts. | Confirmed by probe. |
| **F6** | The daemon runs as a user LaunchAgent inside the GUI session. | `com.magiccliremote.mcremote.plist` is in `~/Library/LaunchAgents`, and `launchctl list` shows the label with PID 81345 running as `saxsmith`. This is why a prompt can appear at all: a LaunchDaemon could not reach the login keychain. | Confirmed by probe. |
| **F7** | Neither the LaunchAgent nor the daemon process sets any Goose keyring variable. | The plist's `EnvironmentVariables` contains only `HOME`, `USER`, `LOGNAME`, `PATH`, and the four `XDG_*` paths. `ps eww` on the daemon shows no `GOOSE_*` variable. | Confirmed by probe. |
| **F8** | The daemon spawns Goose with the inherited environment plus two ownership stamps only. | `internal/provider/acphttp/provider.go:310` builds the command and `:317` sets `cmd.Env = append(os.Environ(), …)` adding only `EnvEngineID` and `EnvEngineOwner`. Nothing influences Goose's secret backend. | Confirmed by code. |
| **F9** | mcremote already understands the file-based path and already tells the operator to use it. | `credstore.GooseKeyringDisabled` (`internal/provider/credstore/credstore.go:109-132`) reads both `GOOSE_DISABLE_KEYRING` and the same key in `config.yaml`. `credstore.ErrGooseKeyringManaged` (`write.go:220`) is returned from `goose/auth.go:180` and `:212` with the text "set GOOSE_DISABLE_KEYRING on the host or run `goose configure` there". | Confirmed by code. |
| **F10** | Every other provider mcremote supports already keeps credentials in a plaintext, owner-only file. | Codex and Grok use `auth.json`; OpenCode and Kilo use their own `auth.json`. The repository's credential-transaction work (MADR 0074 §15) is built entirely around file-backed stores. | Confirmed by code. |
| **F11** | `GOOSE_DISABLE_KEYRING` is unset by default on macOS, and the keyring is enabled. | Goose's environment-variable reference gives the default as "Keyring enabled". This host has no such key in `config.yaml`, none in the daemon environment (F7), and demonstrably uses the Keychain (F1). | Confirmed by documentation and probe. |
| **F13** | **Confirmed: `config.yaml` IS honoured.** The documentation table is simply incomplete. | `Config` resolution (`base.rs:206-209`) is `env::var("GOOSE_DISABLE_KEYRING").is_ok() \|\| no_secrets_config.get_param(…)`, and the secondary constructor (`base.rs:422-423`) uses the dedicated `keyring_disabled_in_config` raw-file reader (`base.rs:365-371`). The key is read from `config.yaml` on both paths. | **Confirmed against source at the installed version.** |
| **F14** | **Withdrawn.** mcremote reading the key from `config.yaml` is correct, because Goose reads it there too (F13). | `credstore.GooseKeyringDisabled` (`credstore.go:113-130`) is right to parse it. The only residual divergence is F12's value handling. | **Resolved: not a defect.** |
| **F12** | **Confirmed: Goose parses the value, mcremote's falsey handling is close but not identical.** | `keyring_disabled_value` (`crates/goose/src/config/base.rs:299-301`) accepts a YAML boolean `true`, or the strings `"true"` and `"1"` — everything else, including `0`, `false`, `no`, `off`, and any other string, leaves the keyring **enabled**. That is *not* presence-only: the documentation's "any non-empty string" is wrong. mcremote's `isFalsey` (`credstore.go`) agrees on `0`/`false` but diverges on arbitrary values: it treats `GOOSE_DISABLE_KEYRING: maybe` as disabled, while Goose treats it as enabled. | **Confirmed against source at the installed version.** |

**Environment precedence is presence-only, config is value-checked.** Note the
asymmetry at `base.rs:206-209`: the environment branch is `is_ok()` — the
variable being *set at all* disables the keyring, so `GOOSE_DISABLE_KEYRING=0`
in the environment disables it. The `config.yaml` branch runs the value through
`keyring_disabled_value`, where `0` does not. The same text means opposite
things depending on where it is written.

### Why "Always Allow" does not stick

macOS binds a keychain item's ACL to the **Designated Requirement** of the
process granted access — an expression derived from that process's code
signature. "Always Allow" records a trusted-application entry against that
requirement.

An ad-hoc, linker-signed binary has no stable identity to express. Its
requirement collapses to the exact code-directory hash, and its identifier
(`goose-9212d893d42becc0`, F3) is regenerated per build. Two consequences
follow, and the evidence distinguishes them:

* **Rebuilds invalidate any stored grant.** The binary changed on 2026-08-21
  (F4), after the keychain item was last touched on 2026-08-16 (F5). Any ACL
  entry recorded before that no longer matches.
* **A durable grant cannot be formed at all.** A rebuild alone would explain
  one prompt after each update, then silence. The operator reports a prompt on
  *every* launch. That matches the documented behaviour for binaries with no
  `TeamIdentifier`: macOS cannot construct a stable ACL and falls back to
  prompting each time.

F5 also rules out the other common cause. Some tools wipe an item's ACL by
deleting and recreating it on every write; Goose is not doing that here, since
the item's modification date has not moved.

The conclusion is that this is not a misconfiguration the operator can fix
through the prompt dialog. As long as Goose reads the Keychain from an ad-hoc
signed binary, the prompt is structural.

### Why this is worse than it looks

The prompt is presented in the GUI login session (F6). A phone-initiated
session therefore does not fail with an error the operator can act on remotely
— it blocks on a dialog they cannot see, until it times out or someone walks to
the machine. The failure is silent from the phone's perspective.

## Decision Drivers

* A phone-initiated Goose session must start with nobody at the host. That is
  the product's reason to exist, and this is currently the one provider that
  cannot do it.
* The fix must survive Goose upgrades. Goose is rebuilt often (F4), and any
  remedy tied to a specific binary identity will break on the next release.
* The fix should not fight the vendor's release pipeline. mcremote does not
  control how block/goose signs its binaries.
* Secret handling must not regress relative to the other providers, and must
  not put a secret anywhere it can be read out of `ps`, a log, or a transcript.
* Prefer the path the vendor documents for headless operation over a
  platform-specific workaround, because the vendor's path is tested and
  supported.
* The daemon is headless by definition. A remedy that assumes an interactive
  session has not solved the problem, only relocated it.

## Considered Options

* Option 1: Change nothing and document the limitation
* Option 2: Re-sign the Goose binary with a stable self-signed identity
* Option 3: Unlock the login keychain from the daemon
* Option 4: Move Goose to its documented file-based secret store, and have the
  daemon set that explicitly when it spawns Goose (chosen)
* Option 5: Supply the provider key through a process environment variable only

## Decision Outcome

Chosen option: **"Option 4: move Goose to its documented file-based secret
store, and have the daemon set that explicitly when it spawns Goose"**.

This is the vendor's own answer for headless environments, it survives every
Goose upgrade because it depends on no binary identity, and it makes Goose
consistent with every other provider mcremote already drives (F10). The
security position is not a regression: it moves one secret from the Keychain to
the same class of owner-only file that already holds the Codex, Grok, OpenCode,
and Kilo credentials.

The daemon setting the variable itself — rather than relying on host
configuration — is what makes the outcome deterministic. F7 shows the current
host has no such variable anywhere, and asking every operator to hand-edit a
LaunchAgent plist is a setup step that will be missed.

### Locked decisions

| ID | Decision |
| --- | --- |
| **D1** | **Revised once F13 was confirmed.** The daemon reconciles the `GOOSE_DISABLE_KEYRING` key in Goose's own `~/.config/goose/config.yaml` to match mcremote's setting, rather than setting an environment variable on the child. F13 proves Goose reads the key there on both construction paths, and mcremote already writes this exact file through `credstore.SetGooseActiveProvider` (`write.go:178`), so this adds no new class of side effect. `keyring_disabled: true` writes the key; `keyring_disabled: false` **removes** it, so Goose falls back to its own default rather than carrying a residual line. Removing and writing `false` are identical to Goose — `keyring_disabled_value` returns false for an explicit `false`, and an absent key hits `unwrap_or(false)` — so removal is chosen because it leaves the operator's file as it was. |
| **D10** | **Owner-confirmed 2026-08-21: the marker, not decline-to-manage.** The alternative considered was that mcremote stops managing the key entirely once it sees a hand-set line — more predictable, but it would leave a host permanently stuck on whatever the operator typed once, with no way to change it from mcremote. The marker keeps mcremote able to manage its own line while never touching one it did not write. mcremote only removes a line it owns. Written lines carry a trailing marker comment, `# managed by mcremote (providers.goose.keyring_disabled)`, and toggling off removes the key **only** when that marker is present. A `GOOSE_DISABLE_KEYRING` the operator wrote themselves is left untouched and logged, because silently deleting a setting someone chose by hand is worse than declining to manage it. The marker is invisible to both readers: YAML strips it, and mcremote's own `splitYAMLScalar` already strips a trailing ` #` comment. |
| **D8** | Expose the choice as an mcremote configuration key under the Goose provider — `keyring_disabled`, boolean, defaulting to `true` — so the behaviour is declared where every other provider setting lives rather than inferred from the host environment. The key drives D1's spawn environment. It is deliberately mcremote's own key with mcremote's own semantics, which also satisfies D4: it is a real boolean, so it cannot inherit Goose's presence-versus-truth ambiguity (F12). |
| **D9** | **Adopted, replacing the environment mechanism.** The decisive advantage is that one store serves both launch paths: a Goose the operator runs by hand and the one mcremote spawns read the same secrets, and `goose configure` at a terminal writes where mcremote's Goose looks. The environment route would have split them permanently. The child-environment mechanism is retained only as a documented fallback if writing the operator's config file is later judged too invasive. |
| **D2** | The daemon must not make that switch silently when it would break a working host. If `GOOSE_DISABLE_KEYRING` is being introduced by mcremote and `secrets.yaml` holds no usable secret while the Keychain item exists, Goose would start with no credentials at all. Startup detects that case, leaves the variable unset, and surfaces a typed, actionable state instead of a silent downgrade. |
| **D3** | Migrating existing Keychain secrets into `secrets.yaml` is **out of scope**. Populating the file store is the operator's own task, performed with Goose's own tooling (`GOOSE_DISABLE_KEYRING=1 goose configure`). This record decides only which store Goose reads. |
| **D4** | An operator may opt out, but **not** by setting `GOOSE_DISABLE_KEYRING` to a falsey value: F12 makes that unsafe, because Goose may read presence rather than truth and would disable its keyring anyway. The opt-out is an explicit mcremote-side setting whose name cannot be confused with Goose's own variable. Its exact form is an implementation-plan decision; what is locked here is that it must not depend on falsey-value semantics. |
| **D5** | `secrets.yaml` is treated as credential material: owner-only mode, never logged, never echoed into an error, never included in a transcript or receipt. This matches the handling the other providers' credential files already receive. |
| **D6** | Do not re-sign the Goose binary, and do not automate keychain unlocking. Both are rejected below on durability and security grounds respectively. |
| **D7** | The phone must be able to distinguish "Goose has no credential" from "Goose is blocked on a host dialog". The second is the failure this record exists to remove, and while any path to it remains it must be reported rather than presenting as a hang. |

### What this does not change

Goose's own precedence order is unchanged: an exact-match environment variable
still wins over both stores. D1 only decides between the Keychain and the file
for secrets Goose manages itself.

Explicitly out of scope:

* **Migrating existing secrets** out of the Keychain (D3). mcremote does not
  read the operator's Keychain, and building a one-off extractor for a store it
  otherwise never touches would add a credential-handling path that exists for
  a single transition.
* Enrolling Goose's file store into the MADR 0074 credential coordinator.
* Any change to how Goose is signed or installed.

## Consequences

* Good, because a phone-initiated Goose session starts with nobody at the host,
  which is the whole point.
* Good, because the remedy is indifferent to Goose's signing and survives its
  frequent rebuilds (F3, F4).
* Good, because Goose stops being the one provider with a different credential
  story, which removes a permanent exception from the daemon's mental model.
* Good, because it is the vendor-documented path for headless use rather than a
  macOS-specific trick.
* Bad, because the secret moves from the Keychain to a plaintext owner-only
  file. That is a real reduction in at-rest protection on a stolen, unlocked
  disk, accepted here because it is the protection level every other provider
  on this host already has, and because the alternative is a provider that does
  not work.
* Good, because one store serves both launch paths. This was the deciding
  factor once F13 was confirmed: the environment route would have left a Goose
  run by hand on the Keychain while mcremote's read `secrets.yaml`, so
  `goose configure` at a terminal would write where mcremote's Goose never
  looks. Writing the key into Goose's own config removes that split entirely.
* Bad, because mcremote now writes a key into the operator's Goose config file.
  That is a real side effect on a file mcremote does not own, mitigated only by
  the fact that it already writes this file (`write.go:178`) and that the key
  is reconciled rather than blindly appended.
* Bad, because an operator whose secrets are currently only in the Keychain
  must populate the file store before Goose is usable again. That work is
  deliberately theirs (D3), and it needs one interactive unlock: reading the
  existing secret is exactly the operation macOS is gating.
* Neutral, because Linux and Windows hosts are unaffected in practice — Linux
  headless installs already fall back to file storage, and this record makes
  that behaviour explicit rather than incidental.

## Confirmation

This decision is confirmed when an implementation plan defines and its
implementation demonstrates all of the following.

* With the daemon running and nobody at the host, a phone-initiated Goose
  session starts, reaches a prompt, and returns a reply. No keychain dialog
  appears on the host during the run.
* The same flow succeeds after the `goose` binary is replaced with a rebuilt
  one, proving the remedy does not depend on binary identity.
* A host whose file store holds no usable secret is detected before the switch
  is applied, and reports a typed actionable state rather than starting Goose
  with no credentials.
* The opt-out works and does not rely on a falsey value of Goose's own
  variable.
* F13 is settled against Goose's source before release. If `config.yaml` is
  honoured, D9 is reconsidered and `credstore.GooseKeyringDisabled` is correct
  as written; if it is not, that function's `config.yaml` branch is removed or
  marked, because it currently reports a state Goose may not be in (F14).
* A host that sets `keyring_disabled: false` keeps the Keychain, and no
  `GOOSE_DISABLE_KEYRING` is added to the child environment.
* F12 is settled against Goose's source before release, and
  `credstore.GooseKeyringDisabled` is aligned with whatever that source
  actually does. A disagreement here makes mcremote's reported credential state
  wrong, which is its own defect independent of this decision.
* `secrets.yaml` is owner-only, and a recursive search of logs, transcripts,
  receipts, and error text after a full session contains no secret value.
* Goose's env-var precedence still wins where an operator has set a
  provider-specific variable.
* The existing Goose provider, catalog, and auth tests remain green, and the
  `live_goose` suite passes against the installed binary.

## Pros and Cons of the Options

### Option 1: Change nothing and document the limitation

* Good, because it costs no engineering time and keeps the Keychain's at-rest
  protection.
* Bad, because Goose remains unusable for the product's primary use case. A
  documented limitation is still a broken feature.
* Bad, because the failure presents as a hang rather than an error, so the
  operator cannot tell from the phone what went wrong.

### Option 2: Re-sign the Goose binary with a stable self-signed identity

* Good, because it addresses the mechanism directly: a stable Designated
  Requirement is exactly what a durable ACL needs.
* Good, because the Keychain keeps holding the secret.
* Bad, because it must be redone on every Goose upgrade (F4), and a missed
  re-sign returns silently to prompting.
* Bad, because it makes mcremote responsible for modifying another vendor's
  shipped binary, which is fragile and surprising.
* Bad, because a self-signed identity must itself be trusted on the host, which
  is another setup step requiring physical presence.

### Option 3: Unlock the login keychain from the daemon

* Good, because it would preserve Keychain storage with no migration.
* Bad, because `security unlock-keychain` takes the password in argv, where any
  process on the host can read it from `ps`. The repository already refuses
  this exact tradeoff in `ErrGooseKeyringManaged` (F9).
* Bad, because storing the login password to replay it is a strictly worse
  secret-handling posture than the file it was meant to avoid.
* Bad, because it does not survive reboot without persisting that password.

### Option 4: File-based secret store, set by the daemon (Chosen)

* Good, because it is vendor-documented, headless-first, and identity-agnostic.
* Good, because mcremote already models this backend (F9), so the daemon can
  reason about which store is live rather than guessing.
* Good, because it aligns Goose with every other provider (F10).
* Bad, because it lowers at-rest protection for that one secret.
* Bad, because it needs a one-time migration with an interactive unlock.

### Option 5: Provider key through an environment variable only

* Good, because env vars take precedence over both stores, so it works
  immediately with no migration.
* Good, because no new file is created.
* Bad, because a LaunchAgent plist is itself plaintext, and the value is
  visible to `ps eww` and `launchctl print` — a wider exposure than a 0600 file.
* Bad, because it only covers the active provider's key and silently fails for
  any other secret Goose manages.
* Bad, because rotating the key means editing and reloading a plist rather than
  using Goose's own tooling.

## More Information

### Sources

* [Goose known issues — keyring troubleshooting](https://goose-docs.ai/docs/troubleshooting/known-issues/)
  — documents `GOOSE_DISABLE_KEYRING`, the `~/.config/goose/secrets.yaml`
  fallback path, and the remedy for repeated keychain prompts.
* [Goose authentication systems](https://deepwiki.com/block/goose/6.3-authentication-systems)
  — precedence order (environment variable, then keyring, then `secrets.yaml`),
  and the `"goose"` / `"secrets"` keyring identifiers this host's item matches.
* [Goose configuration files](https://contextqmd.com/libraries/goose/versions/1.31.0/pages/documentation/docs/guides/config-files)
  — configuration layout and secret locations.
* [Preserve macOS app permissions across rebuilds with self-signed certificates](https://evoleinik.com/posts/macos-dev-signing-preserve-permissions/)
  — why ad-hoc signing produces a new identity per build and how a stable
  signing identity preserves grants; the basis for rejecting Option 2 on
  durability grounds.
* [macOS keychain binary ACL bug](https://github.com/abock/dnc-macos-keychain-binary-acl-bug)
  — ACLs are bound to the writing process's Designated Requirement.
* [Ship a stable codesigning identity to prevent recurring keychain prompts on upgrade](https://github.com/openclaw/gogcli/issues/569)
  — the same failure in another tool: ad-hoc linker-signed binaries whose hash
  changes each release invalidate keychain ACLs on every upgrade.
* [Keychain password prompt repeats despite "Always Allow"](https://github.com/steipete/CodexBar/issues/340)
  — corroborates repeated prompting where no stable identity exists.

### Reproducible read-only probes used for this record

None of these read or print a secret value. `security find-generic-password`
was deliberately run **without** `-g`, so it returns attributes only and
triggers no prompt.

```text
security find-generic-password -s goose
codesign -dv --verbose=2 "$(command -v goose)"
ls -la ~/.config/goose/secrets.yaml
grep -i 'keyring\|GOOSE_DISABLE' ~/.config/goose/config.yaml
launchctl list | grep mcremote
ps eww <daemon-pid>
```

### Relationship to other records

MADR 0074 §15 established transactional, file-backed credential handling for
Codex and Grok, including backup generations and recovery. Goose is out of
scope there and stays out of scope here: this record only changes which store
Goose reads. Whether Goose's file store should later join the 0074 coordinator
is a separate decision, and is deliberately not taken now.

### Relationship to implementation planning

This record was accepted by the Project Owner on 2026-08-21, together with its
paired [0110-PLAN-goose-keyring-prompts-block-headless-launch.md](0110-PLAN-goose-keyring-prompts-block-headless-launch.md),
which enumerates the phases, files, verification commands, and acceptance
criteria for D1-D10. Execution was approved in the same review.

Work stays inside D1-D10. Anything execution reveals outside them stops for an
amendment rather than being implemented opportunistically.
