---
status: accepted
date: 2026-10-20
decision-makers: Project Owner
consulted: "go-selfupdate-lib v1.17.1: docs/guides/building-releases.md §13, docs/reports/0004-REPORT-release-signing-research.md, selfupdate/verify/signednote and selfupdate/verify/ghattest; this repository's GitHub settings, read 2026-10-20"
informed: anyone who cuts a release or installs one
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Do not sign release manifests while every release stays immutable on GitHub

## Context and Problem Statement

[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md),
amendment S9, decision S9-4, left release signing for "a decision of its own". `go-selfupdate-lib`
`v1.16.0` and later can sign each release's `SHA256SUMS` with a key, and the updater can refuse a
release whose note does not verify. The question is whether this product should, now, and what it
would buy.

### Facts [observed 2026-10-20, read-only]

**What the library offers.**

* **A signed note** (`selfupdate/verify/signednote`). The publish workflow's `sign` job signs
  `SHA256SUMS` as `SHA256SUMS.note`, with an Ed25519 key held as the secret `SELFUPDATE_NOTE_KEY` of
  an environment `selfupdate-signing`. The updater embeds the verifier keys as Go constants, and from
  a `RequiredFrom` tag on refuses a release without a valid note (`building-releases.md` §13).
* **What adopting it takes.** The owner makes two keys on their own machine: a CI key and an offline
  backup key, stored encrypted in two places. The owner then creates the environment, the secret and
  a variable for the verifier key. `ci.yml` passes `sign: true`, and a release ships the embedded
  keys. The hop from an unsigned to a signed release is trust-on-upgrade.
* **Attestation checks** (`selfupdate/verify/ghattest`) run `gh attestation verify` on the user's
  host. They need `gh` installed and logged in there.

**What already protects an update.** The library's own threat table (0004-REPORT, "What
`selfupdate` already guarantees"), with this repository's state:

* HTTPS covers tampering in transit.
* Immutable releases stop files being swapped after publication. They are on here
  (`repos/…/immutable-releases`: `enabled: true`), and the updater refuses a release that is not
  immutable. PLAN 0054's S8d and S9d checked `v0.21.0` and `v0.22.0` for it by hand.
* `SHA256SUMS` and GitHub's per-asset digests are checked before install.
* A downgrade needs an exact `--version`.

**What signing would add.** The same report, "What signing would add today":

* Against a stolen account or token with write access, nothing: whoever can push a tag can change
  the workflow at that tag and read the secret. Repository write access is key access.
* Against GitHub serving altered bytes, marginally: the key is in GitHub's own secret store.
* Against releases served from a host that does not guarantee immutability, a lot. This product
  serves none.
* A human gate on each release only with required reviewers on the environment. On a single-owner
  repository the reviewer is the same account an attacker would hold.

**This repository.**

* One collaborator, the owner (`repos/…/collaborators`).
* One ruleset, on branches; none on tags (`repos/…/rulesets`). No deployment environments.
* Two workflows hold `contents: write`: the publish job (`ci.yml:728-733`) and the flake ledger
  (`ci-flake-ledger.yml:31-33`). Both use `GITHUB_TOKEN`, and a tag pushed with `GITHUB_TOKEN`
  starts no workflow, so neither token can publish a release by pushing a tag.

## Decision Drivers

* A control should cover a threat this product actually has.
* A key is a long-lived liability: lost, it stops every later update; leaked, it signs anything.
* The cost falls on the owner: keys, two offline copies, a yearly restore check.
* Reversible: signing can be adopted later without breaking any binary in the field.

## Considered Options

* **N1** — Do not sign now; record when to revisit
* **N2** — Do not sign; add a tag ruleset that limits `v*` tags to the owner
* **S1** — Sign with a CI-held key, auto-approved
* **S2** — Sign with a CI-held key behind a required reviewer
* **G1** — Verify the existing attestations with `ghattest` on users' hosts

## Decision Outcome

Chosen option: "N2", decided by the owner on 2026-10-20 ("N2 + tag ruleset"), because every threat
signing covers on GitHub is already covered by immutable releases and the digests, and the threat
that remains, the owner's account, is one no CI-held key covers. The tag ruleset costs minutes. No one
but the owner can cut a release today, so it guards the future: a collaborator added later, or a
tag pushed by mistake. N1, the same without the ruleset, was the proposal.

Revisit when any of these becomes true:

* releases are served from somewhere without immutable releases: a mirror, object storage, another
  forge;
* a second person gets write access;
* the owner wants a deliberate per-release approval, which is S2.

### Consequences

* Good, because there is no key to lose, leak, rotate or check each year.
* Good, because only the owner can create, move or delete a `v*` tag, so a future collaborator, or a
  mistake, cannot start a release.
* Good, because nothing in the field changes. Adopting S1 or S2 later is the library's documented
  rollout, with `RequiredFrom` at the first signed tag.
* Neutral, because each release's provenance stays checkable by anyone with `gh`, as S8d and S9d
  checked it.
* Bad, because a user who wants a signature independent of GitHub has none.
* Bad, because the ruleset does not stop the owner's own account, which bypasses it.

### Confirmation

* 0054's S9-4 points to this record.
* The ruleset reads back from the API as the PLAN specifies, and the next release tag, pushed by the
  owner, is accepted and publishes.
* The repository's immutable-releases setting reads `enabled: true` when this is decided, and at
  each later release whose record checks it.

## Pros and Cons of the Options

### N1 — Do not sign now

* Good, because it costs nothing and closes S9-4 with a reason.
* Bad, because it leaves the revisit triggers to be noticed by a person.

### N2 — Do not sign; restrict `v*` tags to the owner

A tag ruleset that lets only the owner create, update or delete `v*` tags.

* Good, because it is minutes of settings, and stops anyone else with write access cutting a
  release.
* Neutral, because today nobody else has write access, and the workflows' tokens cannot start a
  release. It guards against a future collaborator or a mistake.
* Bad, because the owner's own account, the remaining threat, bypasses it.

### S1 — Sign, auto-approved

* Good, because a release carries a signature, and a later move off GitHub is already covered.
* Bad, because on GitHub it adds almost nothing: write access is key access.
* Bad, because it brings the full key burden, and a lost key with no backup in the binaries stops
  every later update.

### S2 — Sign, with a required reviewer

* Good, because each release needs a deliberate approval.
* Bad, because on a single-owner repository the approver is the account an attacker would hold.
* Bad, because it has S1's key burden, and a release waits for the click.

### G1 — Check attestations on users' hosts

* Good, because it checks provenance that already exists, with no key.
* Bad, because every host that updates needs `gh` installed and logged in, which a headless daemon
  host usually has not.

## More Information

* The library's design and its threat table: `go-selfupdate-lib` `docs/reports/0004-REPORT-release-signing-research.md`, and `docs/guides/building-releases.md` §13 (repository go-selfupdate-lib, read at `v1.17.1`).
* The PLAN: [0205-PLAN-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md](0205-PLAN-do-not-sign-release-manifests-while-releases-stay-immutable-on-github.md).
  It changes no code.
