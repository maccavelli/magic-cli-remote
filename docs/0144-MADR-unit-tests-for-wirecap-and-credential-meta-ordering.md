---
status: accepted
date: 2026-09-06
decision-makers: maccavelli
consulted: Testbot testing/regression review; Chief of Staff execution greenlight
informed: Anyone changing wire capture or provider-auth adoption ordering
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Add unit tests for wirecap redaction and CredentialMeta ordering

## Context and Problem Statement

A testing/regression review of this repository found two high-ROI gaps that are
small enough to close in one PR and load-bearing enough that a silent regression
would hurt:

1. **`internal/wirecap` has no `*_test.go` files.** The package is inert in
   production unless `MCREMOTE_WIRE_CAPTURE_DIR` is set (MADR 0137), but when it
   *is* used to capture fixtures for version pins it must redact the operator
   home directory in both absolute and stripped-separator forms. That redaction
   rule was discovered the hard way (a "redacted" fixture still carried a
   username). Nothing in CI currently pins it.
2. **`providerauth.CredentialMeta.Fresher` / `NotOlder` lack a dedicated table.**
   Recovery and reconcile paths *use* `NotOlder` (and sequence/expiry ordering
   is the D24/0133 adoption gate), but there is no direct table exercising mode
   mismatch, equal expiry, zero-sequence refusal, and expiry-vs-sequence
   precedence. Those cases are easy to break while refactoring `order` and hard
   to notice from an integration failure message.

Mac greenlit execution of these two items (via Chief of Staff) as tests-only
work.

## Decision Drivers

* **Privacy of captured fixtures.** A leaked home path in a committed fixture is
  a standing secret-adjacent failure.
* **Auth adoption must stay conservative.** Promoting an older credential, or
  escalating a benign equal rewrite into `recovery_required`, are both product
  defects (MADR 0074 D24 / 0133).
* **Tests only.** No behaviour change unless a test forces a tiny fix.
* **Proportionate.** A focused unit table beats a broader coverage programme for
  these two surfaces.

## Considered Options

1. **Add the wirecap unit table and a CredentialMeta Fresher/NotOlder table now.**
2. **Defer until a flake ledger (0143 Phase 2+) names more offenders.**
3. **Cover ordering only via more recovery integration cases.**

## Decision Outcome

Chosen option: **"Add the wirecap unit table and a CredentialMeta Fresher/NotOlder
table now"**, because both gaps are already diagnosed, owner-approved, and
cheaper to pin directly than to rediscover through integration failures or a
flake ledger.

### Consequences

* Good, because redaction and nil/env disable paths for wirecap become
  regression-gated.
* Good, because Fresher/NotOlder edge cases become readable without reconstructing
  them from recovery fixtures.
* Good, because the change set is tests (+ MADR/PLAN docs) with no intentional
  product diff.
* Bad, because integration tests already exercise some adoption paths, so a
  careless reader might think the table is redundant — it is not: it pins the
  comparison function itself.
* Neutral, because 0143 flake work continues independently.

### Confirmation

* `go test ./internal/wirecap ./internal/providerauth -count=1` passes, including
  the new cases named in the plan.
* `internal/wirecap` contains a `*_test.go` file.
* `CredentialMeta` ordering has a dedicated table test file (not only recovery
  fixtures).

## Pros and Cons of the Options

### Add the wirecap and CredentialMeta tables now

* Good, because it closes the review's H1 and H2 with owner approval already in
  hand.
* Good, because effort is S for each.
* Bad, because it does not reduce the CI flake rate (out of scope here).

### Defer to the 0143 ledger

* Good, because it keeps one programme in flight.
* Bad, because neither gap is a flake; waiting teaches nothing new about them.

### Cover ordering only via recovery integration

* Good, because it stays closer to production call sites.
* Bad, because recovery fixtures obscure which comparison branch failed.
* Bad, because wirecap still has zero coverage under that option.

## More Information

* Wire capture decision context: MADR 0137 (prompt-to-first-token / wire pin
  evidence).
* Adoption ordering: MADR 0074 D24, MADR 0133.
* Implementation:
  [0144-PLAN-unit-tests-for-wirecap-and-credential-meta-ordering.md](0144-PLAN-unit-tests-for-wirecap-and-credential-meta-ordering.md).
