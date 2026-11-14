---
status: proposed
date: 2026-11-14
decision-makers: Project Owner
consulted: "this repository at 0fbed31b (master, clean), read-only; a read-only documentation survey of this repository at 0fbed31b by a mainstream-tier agent, 2026-11-14, kept in the coordinator's session and not committed; the coordinator's re-check of three of its rows; this draft's own verification of the survey's leads at 0fbed31b; 0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md, its PLAN and 0239-REPORT-record-status-and-docs-accuracy-audit.md; 0246 and 0247's MADRs and PLANs (proposed); the CHANGELOG.md files of go-selfupdate-lib v1.26.2 and go-llmprovider-sdk v1.29.0, read from the module cache"
informed: "the cross-repository coordinator, which relayed the owner's rulings and writes the briefs that carry the docs step"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Bring this repository's docs current, keep a changelog, and keep both current through a docs step in every phase

## Context and Problem Statement

On 2026-11-14 the owner set a mandate for every related repository: "Docs across all related repos should be accurate,
comprehensive and kept up to date. When they fall behind they are hard to fix". The owner then ruled on four questions:
what "comprehensive" requires, whether to keep a changelog, how currency is enforced, and how docs work orders against
a release tag. The cross-repository coordinator relayed the rulings. This record applies them to this repository.
It also states the evidence that this repository's docs have fallen behind.

### How recently the docs were last brought current

[0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)
audited all 41 unnumbered docs against the tree, and its PLAN closed on 2026-11-08. That audit checked that what a doc
says is true. It did not check that every surface is described somewhere, it left `AGENTS.md` and the per-agent rule
files to the owner (its D8), and it did not cover doc comments or a changelog.

Within four days `v0.26.0` was tagged (`d78d42ea`, 2026-11-12). It ships the native gobble provider: `f4248820`
(2026-11-09) added it and `83b68cbe` (2026-11-11) registered it. That work updated `docs/guides/config.md` and
`docs/guides/protocol-v1.md`. It did not update `README.md`, `docs/architecture.md` or `apps/mobile/README.md`, and
each of those still gives the provider set as a closed list without gobble. So docs that were current on 2026-11-08 were
wrong again in a release four days later. This is the failure the mandate describes.

### The evidence, at 0fbed31b

**The survey.** A mainstream-tier agent surveyed this repository read-only at `0fbed31b` on 2026-11-14. It counted 2
wrong statements, 8 missing items and 2 real stale items. It read in full `README.md`, `AGENTS.md`,
`docs/architecture.md`, `docs/guides/config.md`, `scripts/check_records.py`, and parts of `docs/README.md` and the
`Makefile`. It sampled `internal/cli`, `internal/config`, `internal/relay`, the workflows and the deploy units by `grep`.

**What the survey did not read.** Most of `docs/guides/` (config-mcrelay, protocol-v1, protocol-v2, receipts,
headscale, iam-route53-acme, every `ops-*` guide, chat-performance, `standards/go/`), `apps/mobile/README.md` past
line 40, all of `apps/mobile/docs/`, the per-agent rule files, package doc comments, and the protocol message list
compared with `internal/protocol`. It did not measure exported names without doc comments. The survey is evidence of
where to look. It is not a verified list.

**Re-checked by the coordinator; all three held:**

* `README.md` never mentions gobble. Yet `internal/config/prewarm_write.go:28` lists it in `KnownProviderIDs`, and
  `internal/provider/gobble/` exists.
* The records index in `docs/README.md` has no rows for the four `0246-*` and `0247-*` files on disk.
* `make check-records` passes regardless. It checks links and pairing, not whether the index is complete.

**Verified by this draft, read-only, at 0fbed31b:**

| Lead | Finding | Evidence |
| :--- | :--- | :--- |
| Survey A1 (wrong) | `README.md:773` gives `_MAX_RETRIES` as a short `MCREMOTE_TLS_ROUTE53_*` alias. No such alias is bound | `internal/config/load.go:68-70` binds only the hosted zone, region and profile. `load.go:304-306` says only the full `MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES` works. `docs/guides/config.md:468` says so too. The README line dates from 2026-07-23, so it outlived both earlier audits. mcrelay *does* bind the short `MCRELAY_TLS_ROUTE53_MAX_RETRIES` (`internal/relay/fileconfig.go:250`), so `config-mcrelay.md` is right |
| Survey A2, B1 (wrong, missing) | The provider set appears as a closed list without gobble at `README.md:241`, `:264`, `:273`, `:833-841` (the provider matrix), `:1466` and `:1612`; at `docs/architecture.md:37` and `:81`; and at `apps/mobile/README.md:13`. `README.md`, `docs/architecture.md` and `apps/mobile/README.md` mention gobble zero times. The guides mention it 29 times (`config.md` 28, `protocol-v1.md` 1) | `grep -c -i gobble`; `KnownProviderIDs`; `internal/provider/gobble/gobble.go:18` (`KnownGoodVersion = "0.4.0"`) |
| Survey B2 (missing) | `deploy/systemd/mcrelay.service` is cited by no unnumbered doc. Its three sibling units and both launchd plists are | `grep -n 'deploy/'` over `README.md`, `docs/guides/`, `docs/architecture.md` |
| Survey B3 (missing) | Ten `.PHONY` targets have no mention in any unnumbered doc or `AGENTS.md`, even under a loose match: `check-cgo-off`, `check-host-target`, `clean`, `live-gobble`, `live-grok-discrimination`, `live-grok-rest`, `live-replacefirst`, `manifest-surface`, `markdownlint-docs`, `vet-tags`. The survey's 19 counted only `README.md` and `AGENTS.md`. Some of its 19 are in guides | the `Makefile:158-161` `.PHONY` list against the docs |
| Survey B4 (missing) | `auth-recovery` is in the README's usage block (`README.md:639`) but has no row in its command table (`:646-660`). `docs/guides/ops-credential-recovery.md` describes it | `internal/cli/root.go:77-86` |
| Survey B5 | Refuted as a defect under the owner's definition. `doctor`, `completion` and `version` are described in the README's command table, and the definition asks for a description in "a README, guide or reference", not in every one | the table at `README.md:646-660` |
| Survey B6 (missing) | Nine variable names read by non-test Go code appear in no unnumbered doc: `MCREMOTE_ENGINE_ID` and `MCREMOTE_ENGINE_OWNER` (`internal/procutil/reap.go`), `MCREMOTE_WIRE_CAPTURE_DIR` and `MCREMOTE_WIRECAP_HOST_KEY` (`internal/wirecap/wirecap.go`), `MCREMOTE_TOKEN` (`scripts/smoke-protocol/main.go`), `MCREMOTE_STUB_WARM` (`internal/testexec/testexec.go`), and three `MCREMOTE_UPDATECLIENT_FIXTURE_*` (a test fixture under `testdata/`). Several are internal markers or test fixtures, which makes their scope a question (OQ4) | string literals in non-test `.go` files against the docs |
| Survey B7 | Refuted in part. The installer options the README documents (`--no-hooks`, `--dir`, `--dry-run`, and the `_NO_HOOKS`, `_INSTALL_DIR` and `_VERSION` variables) are in go-selfupdate-lib v1.26.2's installer template (`internal/cmd/selfupdate-release/installer/install.sh`, read from the module cache). How the template derives the `MCREMOTE_` prefix was not read | module cache, read-only |
| Survey B8 (missing) | No changelog file is tracked. The 17 GitHub Releases (`v0.17.1` to `v0.26.0`) each carry only a "Full Changelog" compare link, so there are no earlier release notes to link to. There are 123 `vX.Y.Z` tags from `v0.2.0` (2026-07-19), and 196 `build/*` tags | `git ls-files`; `gh release list`; `gh release view` |
| Survey C1 (stale) | The index lacks the four `0246`/`0247` rows. This draft regenerated the index with the repository's own `--write-index`, which adds them and this pair's two rows | `scripts/check_records.py` |
| Survey C8 | 168 of 467 tracked records have no YAML front matter: 82 MADRs, 67 PLANs and 19 REPORTs. The 149 MADRs and PLANs are the set 0239 D3 left as they are | a scratch count over `git ls-files` |
| New, this draft | `.opencode/rules.md:36` and `.kilo/agent/madr-and-plan-skill.md:41` put records in `docs/NNNN-MADR-*`. `AGENTS.md:110` and the other three rule files put them in `docs/decisions/` | the five rule files |
| New, this draft | `AGENTS.md:51` lists four live targets. `live-gobble` and four other `live-*` targets are not named there | `Makefile:158` |
| New, this draft | `README.md`'s documentation table omits `ops-codex-contract.md`, `ops-hardware-validation.md`, `ops-android-emulator.md` and `ops-ios-signing.md`. Its "Architecture & key decisions" list names the codex, kilo and grok provider MADRs but not `0179-MADR-gobble-native-acp-provider.md`. `docs/README.md`'s "I want to…" table gives no route to seven guides | `grep -F` per guide |
| New, this draft | gobble behaviour from after 0239 (`af8c42fb`: device sign-in only for the grok and kilo upstreams; `bac7db11`: the 30 s device-code read; `4614263c`: a status row with a reason is not ready) changed only its records, not a guide. Whether a guide owes a reader that behaviour is a lead | `git show --stat` of each commit |
| New, this draft | Doc comments, measured by a one-off `go/parser` program over the 359 tracked non-test `.go` files. Generated files were skipped. The program was first seen failing on a planted file, where it reported each of five planted gaps. Every exported top-level func and type, and every exported const and var under its own or its group's comment, has one. That holds because the pre-add rule's per-file `golint` already enforces it. What lacks one: 5 methods on exported types (`Orphan.String`, `CWDPermissionError.Error` and `.Unwrap`, `FlowKind.String`, `ExitError.Error`, all of which `golint` exempts); package doc comments in 4 of 56 packages (`cmd/mcremote`, `cmd/mcrelay`, `internal/provider/codex`, `internal/provider/opencode`); 1,177 of 1,822 exported struct fields; 107 of 216 interface methods; 176 of 384 exported methods on unexported types; 356 of 711 consts and 65 of 81 vars that have only a group comment. Dart was not measured | the program and its output, kept in the drafting session and not committed |

A second check confirmed that the wire surface is better held than the rest. All 94 error codes in
`internal/protocol/errors.go` appear in `protocol-v1.md` or `protocol-v2.md`. `internal/protocol/doc_coverage_test.go`
already holds event types to the spec, and the Docs workflow runs it. Changes since 0239 that altered the wire updated
`protocol-v1.md` in the same commit (`fd6a7dd3`, `33bffce8`, `ea18ceff`, `4bb23579`, `6c8f2623`, `d78d42ea`). Provider
and behaviour changes did not reach the README or `architecture.md`.

### What 0239 left open that bears on docs

0239-REPORT's findings table lists three doc statements left unverified ("as written"): `ops-hardware-validation.md`
F6's "≤60 s" bound, `ops-codex-contract.md`'s "164 methods", and `chat-performance.md`'s "Host dedup" row. It also
lists a standard that no code follows (the mobile architecture standard's `AsyncNotifier` with `AsyncValue.guard()`).
They are leads for this record's PLAN, Phase A.

### The next release, and the records already planned

The last tag is `v0.26.0`. The next planned work here is
[0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
and [0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md),
both `proposed` and not built. 0246's PLAN fixes the order: its P1, then P2 to P6 with no release until P6 (its C5),
then the cut-over release. It permits but does not plan a release after its P1. 0247's PLAN starts after 0246's P3, and
its P2 to P5 "may land in that plan's release or after it". 0246's gate G2 waits on a gobble release (`v0.7.0`) that did
not exist when 0246's PLAN was last updated. So the next tag here comes either after 0246's P1 or at its cut-over, and
its date depends on another repository. The 0246 and 0247 PLANs name `protocol-v1.md`, `protocol-v2.md` and
`architecture.md` as docs they edit. Neither names `README.md` or a changelog.

## Decision Drivers

* The owner's mandate, verbatim: "Docs across all related repos should be accurate, comprehensive and kept up to date.
  When they fall behind they are hard to fix".
* Evidence that one audit is not enough. Docs that 0239 made current on 2026-11-08 were wrong in a release on
  2026-11-12. The fix has to happen in each change, not in an occasional sweep.
* A reader of a release should be able to learn what it changed without reading `git log` or the records. Today the
  GitHub Releases carry only a compare link.
* No new gate, CI job or check script (the owner's ruling).
* A tag should not wait on gaps it did not cause, but it must not ship docs it made false (the owner's ruling).
* History is kept. Records are not rewritten. A guide's stale fact is replaced (0239's rules).
* The fleet's sibling libraries already keep `CHANGELOG.md` in the Keep a Changelog 1.1.0 form (go-selfupdate-lib
  v1.26.2, go-llmprovider-sdk v1.29.0).

## Considered Options

The owner was offered three options on each of four questions. The options appear here exactly as they were offered.

* Comprehensive: "Doc comment plus feature docs"
* Comprehensive: "Every name in a reference page"
* Comprehensive: "Feature docs only"
* Changelog: "CHANGELOG.md in every repo"
* Changelog: "Keep each repo's current practice"
* Changelog: "Release-notes reports everywhere"
* Enforcement: "Fix the docs, no new checks"
* Enforcement: "Add checks to each gate"
* Enforcement: "Checks in the three release repos only"
* Release ordering: "Release-caused fixes before each tag"
* Release ordering: "All fixes before each tag"
* Release ordering: "Tags first, docs after"

## Decision Outcome

Chosen options, each the owner's ruling of 2026-11-14 as the coordinator relayed it: "Doc comment plus feature docs",
"CHANGELOG.md in every repo", "Fix the docs, no new checks", and "Release-caused fixes before each tag". Together they
make every change carry its own docs, which is the only route the evidence above shows will keep the docs current. They
also avoid the cost of new gates and keep a tag from waiting on old gaps.

The rulings are decided. This record is `proposed` until the owner accepts this text as their statement and answers
OQ1 to OQ8 below.

* **D1. The mandate.** Docs in this repository are accurate, comprehensive and kept up to date. The owner's words:
  "Docs across all related repos should be accurate, comprehensive and kept up to date. When they fall behind they are
  hard to fix".
* **D2. What "comprehensive" requires** (the owner chose "Doc comment plus feature docs"). Every exported name has a doc
  comment. Every package, command, flag, environment variable, settings key and make target is described in a README,
  guide or reference. Names are not all listed in prose. Here, "settings key" means a key of `mcremote` or `mcrelay`
  configuration. "Package" means a Go package, described by its package doc comment and, where users meet it, by a
  README or guide. Where the boundaries of "exported name" and "environment variable" sit is OQ3 and OQ4. Rejected:
  "Every name in a reference page", "Feature docs only".
* **D3. One `CHANGELOG.md`** at the repository root, updated in the same phase as the change it records (the owner
  chose "CHANGELOG.md in every repo"). It covers what a release of this repository ships: `mcremote`, `mcrelay`, the
  installers and units, and the phone app built with the tag. It uses the Keep a Changelog 1.1.0 form that the sibling
  libraries use, with an `Unreleased` section at the top. Entries are for changes a user, operator or app can see. A
  correction to a user doc is an entry only when acting on the old text would have gone wrong (A1 is one). How far back
  it starts is OQ1. Rejected: "Keep each repo's current practice", "Release-notes reports everywhere".
* **D4. No new checks** (the owner chose "Fix the docs, no new checks"). This repository's PLAN adds no gate, no CI job
  and no check script, and changes no `Makefile` target. A one-off command a phase runs to prove its own work is
  verification. It lives in the executing session's scratchpad and is never committed. The checks that exist stay as
  they are: `make check-records`, `make markdownlint-docs`, `internal/protocol`'s doc-coverage tests, the home-path
  guard, and the pre-add rule's `golint`. Rejected: "Add checks to each gate", "Checks in the three release repos
  only".
* **D5. A docs step in every phase that changes behaviour.** Every later brief for code in this repository carries a
  docs step, per the mandate. The step names each doc the change makes false or incomplete: a README, a guide,
  `architecture.md`, a config or protocol reference, the doc comments of the names it adds, and the `CHANGELOG.md`
  `Unreleased` entry. It updates them in the same commit, or it states why none is needed. The phase's execution record
  says which. This is how the owner's choice of "no new checks" keeps the docs current. It applies to 0246's and
  0247's phases once they start. Their scopes gain `CHANGELOG.md` and the docs step by an amendment that the owner
  approves. This record does not make that amendment.
* **D6. Release ordering** (the owner chose "Release-caused fixes before each tag"). Before a tag, every statement that
  release makes false is fixed, and what it ships is documented, including its `CHANGELOG.md` section. Older gaps go
  into this repository's docs plan and land after the tag. Here, "release-caused" means made false or left undocumented
  by a commit between the previous tag and this one. Every gap in the evidence above was true at `v0.26.0` or before,
  so all of them are older gaps for the next tag. This pair orders against 0246 and 0247 as follows:
  * the PLAN's Phase A (a read-only defect list) and Phase E (create `CHANGELOG.md`) run before the next phase that
    changes behaviour in this repository, which is 0246's P1 or anything else that lands first, so that phase has a
    changelog to write to;
  * 0246's and 0247's phases each carry D5's step, and the brief for the next tag carries the pre-tag step (the PLAN's
    S2);
  * Phases B, C, D and F, the older gaps, land after that tag (OQ6 asks whether docs-only phases may land earlier while
    the tag is held by 0246's gate G2). Rejected: "All fixes before each tag", "Tags first, docs after".
* **D7. Rules carried from 0239.** Records keep their history. A guide's stale fact is replaced, not annotated. The
  generated index is regenerated, never hand-edited. Where a doc describes behaviour the code does not have, the doc is
  rewritten to the code and the difference is listed for the owner. That last rule was the owner's answer to 0239's Q3.
  Applying it here is this draft's proposal, not a new ruling.
* **D8. Out of scope.** No behaviour change to code. A doc-versus-code difference whose fix might be the code is listed
  for the owner. No decision that a stale doc exposes is made here. `AGENTS.md` and the per-agent rule files change only
  as OQ5 allows. Record front matter is OQ2. No other repository is touched.

### Consequences

* Good, because each change carries its own docs, which is the one route the 2026-11-08 to 2026-11-12 drift shows will
  hold.
* Good, because a release has notes a reader can use, in the same form as the sibling libraries.
* Good, because no gate is added, so no build or tag can be blocked by a docs check.
* Good, because the tag after `v0.26.0` is not held for gaps it did not cause.
* Neutral, because the existing checks still do not see completeness, so a missed docs step is caught only by the next
  brief or audit that reads the change.
* Bad, because currency now rests on every brief including the docs step and every executor doing it. A coordinator
  who drops the step drops the guarantee, and no check notices.
* Bad, because older gaps, including the README's missing gobble section, stay in place until after the next tag, and
  that tag is waiting on another repository (OQ6).
* Bad, because the doc-comment measure is large if OQ3 includes struct fields and interface methods: 1,284 more
  comments.

### Confirmation

Without a new check, a reader knows the docs are current by these records and files:

1. Phase A's REPORT lists every defect with its evidence. Each row is later closed by a named commit, or carried with
   its reason.
2. `CHANGELOG.md` has an `Unreleased` section. At each tag it has a section for that tag. In the brief for that tag, the
   user-visible commits in `git log <previous tag>..<tag>` are checked by hand against the section, and the execution
   record says so.
3. Each phase of any later PLAN in this repository that changes behaviour records its docs step. That record names the
   docs changed, or says why none were.
4. Phase D's one-off measurement, re-run at the end of that phase, reports zero missing doc comments in the categories
   OQ3 places in scope. The numbers are in the PLAN's execution record.
5. Phase C's one-off surface lists (providers, commands, flags, environment variables per OQ4, settings keys, make
   targets, deploy units) each find a description for every item, and the result is in the PLAN's execution record.
6. The existing checks pass at each phase: `make check-records`, `make markdownlint-docs`, and, where a protocol guide
   changed, `go test -count=1 ./internal/protocol/`.

## Pros and Cons of the Options

### Comprehensive: "Doc comment plus feature docs" (chosen)

* Good, because it asks for both levels a reader uses: the code's own comments and a feature-level description.
* Good, because the pre-add rule's `golint` already holds most declarations, so the remaining gap is small or
  well bounded (OQ3).
* Bad, because "every exported name" has more than one reading, and the readings differ by more than a thousand
  comments.

### Comprehensive: "Every name in a reference page"

* Good, because a reader could find every name in one place.
* Bad, because a hand-written reference of every name drifts fastest, and nothing here generates one.

### Comprehensive: "Feature docs only"

* Good, because it is the least work.
* Bad, because a maintainer reading the code would have no comment to read.

### Changelog: "CHANGELOG.md in every repo" (chosen)

* Good, because it gives a release notes that the GitHub Releases, with only a compare link, do not.
* Good, because the sibling libraries already use the form.
* Bad, because each phase that changes behaviour has one more file to edit.

### Changelog: "Keep each repo's current practice"

* Good, because nothing changes.
* Bad, because here the current practice is no notes at all.

### Changelog: "Release-notes reports everywhere"

* Good, because a numbered report can carry evidence.
* Bad, because a report per release adds records that the index and the reader must page through, and it is written at
  tag time rather than with the change.

### Enforcement: "Fix the docs, no new checks" (chosen)

* Good, because it adds no gate that can block a build or a tag, and no script to maintain.
* Bad, because a missed docs step goes unnoticed until someone reads the change.

### Enforcement: "Add checks to each gate"

* Good, because a missing description or comment would fail before commit.
* Bad, because each repository gains scripts to maintain, and a completeness check needs a list of surfaces that is
  itself hard to keep complete.

### Enforcement: "Checks in the three release repos only"

* Good, because it limits the cost to the repositories that ship releases.
* Bad, because it leaves the other repositories to drift, and it still adds gates.

### Release ordering: "Release-caused fixes before each tag" (chosen)

* Good, because a release never ships docs it made false.
* Good, because a tag does not wait on gaps it did not cause.
* Bad, because older gaps stay in place for one more release.

### Release ordering: "All fixes before each tag"

* Good, because each release would ship complete docs.
* Bad, because here the next tag would wait on every phase of this PLAN, Phase D included.

### Release ordering: "Tags first, docs after"

* Good, because no tag ever waits on docs.
* Bad, because releases ship docs they made false, which is how `v0.26.0` shipped without gobble in the README.

## More Information

### Open questions for the owner

Each question has options and a recommendation. None is decided here.

* **OQ1. Where `CHANGELOG.md` starts.**
  * (a) From the next release forward. The header says releases up to `v0.26.0` have no notes, and points to the tags,
    the GitHub Releases page and the records index.
  * (b) From the last tag. One backfilled `v0.26.0` section, written from `git log v0.25.1..v0.26.0` (63 commits, 30
    of them not docs-only) and the records those commits cite, with the same pointer for older releases.
  * (c) A full backfill: a section for each of the 123 `vX.Y.Z` tags since `v0.2.0`, covering 1,806 commits.
  * *Recommendation: (b).* "A link to earlier release notes" would link only to compare pages, since no earlier notes
    exist. Of the backfilled sections, `v0.26.0` is the one a reader needs: it is the baseline 0246's breaking
    cut-over changes. A full backfill costs a judgment-heavy pass over 1,806 commits to record releases nobody will
    upgrade from.
* **OQ2. The 168 records with no YAML front matter** (82 MADRs, 67 PLANs, 19 REPORTs).
  * (a) Leave them as dated history.
  * (b) Add front matter to all 168.
  * (c) Add it to the 19 REPORTs only, which 0239 did not cover.
  * *Recommendation: (a).* 0200 and 0239 D3 already left the 149 MADRs and PLANs as they are ("touch 149 closed
    records for no reader's benefit"). The mandate is about accuracy, and the missing front matter makes no record
    inaccurate.
* **OQ3. What "every exported name" covers.**
  * (i) Go declarations godoc lists at top level: package doc comments, exported funcs and types, methods on exported
    types, and consts and vars under their own or a group comment. At `0fbed31b` that is 9 missing: 5 methods and 4
    package comments.
  * (ii) (i) plus exported struct fields and interface methods: 1,284 more.
  * (iii) (ii) plus exported methods on unexported types (176) and a comment of its own for each const and var (421).
  * (iv) Any of these, plus public Dart API in `apps/mobile/lib`, which was not measured.
  * *Recommendation: (i) now, with (ii) limited to the types that are a contract with users: the wire structs in
    `internal/protocol` and the config and provider-section structs whose fields are settings keys.* Those fields are
    what a user configures or receives, and the guides describe each one already, so a field comment there links to
    them. Dart stays out unless the owner says otherwise. The ruling's other half, "feature docs", covers the app's
    behaviour.
* **OQ4. What "every environment variable" and "every make target" cover.**
  * (a) Every name non-test code reads, internal markers included (`MCREMOTE_ENGINE_ID`, `MCREMOTE_ENGINE_OWNER`, which
    the daemon sets on its children), and every `.PHONY` target.
  * (b) Only names a user or operator sets, and only targets a contributor runs.
  * (c) (a) plus every variable test code reads (the `MCREMOTE_LIVE_*` family and the fixtures).
  * *Recommendation: (a), with internal markers in a short "set by the daemon, do not set" table in
    `docs/guides/config.md`, test-fixture variables described where their live or fixture target is described, and
    every target as one row of the README's Development table.* It is the reading closest to the ruling's "every", and
    each item costs one table row.
* **OQ5. `AGENTS.md` and the per-agent rule files.** 0239 D8 left their text to the owner's word.
  * (a) Allow factual fixes in Phase B: the `docs/NNNN-MADR-*` path in `.opencode/rules.md:36` and
    `.kilo/agent/madr-and-plan-skill.md:41`, and `AGENTS.md:51`'s list of live targets. Also allow, in Phase E, one
    paragraph in `AGENTS.md` stating D5's docs step and D3's changelog rule.
  * (b) Allow the factual fixes only.
  * (c) Neither; list them for the owner.
  * *Recommendation: (a).* `AGENTS.md` is the normative copy every agent loads (its own words). A rule that keeps docs
    current only through briefs is safer when the rule is in the file every brief's executor reads. The paragraph
    states a rule; it adds no check.
* **OQ6. May older-gap phases land before the next tag?** D6 puts B, C, D and F after it, and that tag waits on
  0246's G2, a gobble release whose date depends on another repository.
  * (a) As ruled: after the tag.
  * (b) Docs-only Phases B, C and F may land before the tag, one at a time between other phases, never concurrently
    with one. Phase D waits for 0246's cut-over, because it edits Go files 0246 rewrites.
  * *Recommendation: (b).* The ruling's purpose, that a tag should not wait on old gaps, does not come into play
    while the tag is held by something else. The mandate says gaps get harder to fix the longer they stand.
* **OQ7. `CHANGELOG.md` in lint and in the GitHub Release.**
  * (a) Neither: each phase that edits it lints it by hand with the repository's markdownlint configuration.
  * (b) Add it to `MARKDOWNLINT_DOCS_ROOTS` in the `Makefile`.
  * (c) (b) plus publishing the tag's section as the GitHub Release body.
  * *Recommendation: (a).* (b) extends a gate and (c) changes the release workflow, which "Fix the docs, no new checks"
    rules out for now.
* **OQ8. `AGENTS.md` is a regular file and there is no `CLAUDE.md`.** The fleet's workspace layout makes `CLAUDE.md`
  canonical and `AGENTS.md` a link to it.
  * (a) Out of this pair: a workspace-scaffolding record of its own.
  * (b) Fold it into Phase F.
  * *Recommendation: (a).* It is a layout decision, not a docs fix.

### What is not verified

* Everything the survey did not read, listed above, until Phase A reads it.
* The survey's B7 prefix mapping in go-selfupdate-lib's installer template. Whether `deploy/systemd/mcrelay.service`
  is still installed by any code path or is an orphan.
* The B3 count depends on the match rule. Ten targets have no mention under a loose match. Phase A settles the list.
* Whether any statement in `docs/guides/` other than those named above is wrong now. 0239 found them accurate or
  corrected as of 2026-11-08, and six days of commits have landed since.
* The Dart public API's doc-comment state.
* The doc-comment counts are from a scratch program that `ast.IsGenerated` filtered. They were not compared with any
  other tool.
* No build, test, lint of Go, or live run was made for this record.

### Related

* The PLAN: [0248-PLAN-bring-the-docs-current-and-keep-them-so.md](0248-PLAN-bring-the-docs-current-and-keep-them-so.md).
* [0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)
  and [0239-REPORT-record-status-and-docs-accuracy-audit.md](../reports/0239-REPORT-record-status-and-docs-accuracy-audit.md):
  the last pass, its rules and its open findings. This record does not supersede it.
* [0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md](0246-PLAN-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md)
  and [0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md](0247-PLAN-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md):
  the planned work this pair orders against.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md): the provider the README omits.
