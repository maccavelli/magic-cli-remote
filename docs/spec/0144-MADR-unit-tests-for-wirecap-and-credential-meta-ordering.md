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

## Amendment — 2026-09-06 (the redaction rule is not platform-neutral)

The wirecap table did what this record hoped it would: it failed, on Windows,
for a reason the record did not anticipate. `Go (windows/amd64)` in CI run
`34011907466` reports

```text
--- FAIL: TestRedactAbsoluteAndRelativeHome (0.00s)
    wirecap_test.go:86: username leaked after redact: "cwd=/home/user/proj and also Users/alice/proj"
```

The absolute form was rewritten and the stripped-separator form was not. Both
attempts failed, so the MADR 0143 retry correctly declined to mask it.

**What this contradicts.** Context item 1 states the redaction must cover "both
absolute and stripped-separator forms", and Decision Drivers name a leaked home
path in a captured fixture as "a standing secret-adjacent failure". Those are
written as properties of the package. They are in fact properties of the package
*on POSIX hosts only*, which this record did not know when it was accepted.

**Root cause.** `redact` decides how to build the stripped form from the host's
own path separator:

```go
if rel := strings.TrimPrefix(c.home, string(os.PathSeparator)); rel != "" && rel != c.home {
```

`os.PathSeparator` is `\` on Windows, so trimming leaves the string unchanged,
the `rel != c.home` guard is false, and the stripped-form replacement never
runs. This is not confined to the test's POSIX-shaped home. For the realistic
Windows home `C:\Users\alice` the trim is equally a no-op, because the string
begins with a drive letter rather than a separator — so **the stripped-form
redaction never fires on Windows for any home a Windows host can produce.**
Confirmed by evaluating the published `redact` body with the separator injected:

| host | home | result | leaks |
| --- | --- | --- | --- |
| POSIX | `/Users/alice` | `cwd=/home/user/proj and also home/user/proj` | no |
| Windows | `/Users/alice` | `cwd=/home/user/proj and also Users/alice/proj` | **yes** |
| Windows | `C:\Users\alice` | `cwd=/home/user\proj and also Users\alice\proj` | **yes** |

The category error is consulting a *host* property to perform *string surgery on
captured bytes*. What separator a captured payload contains is a fact about the
engine that produced it, not about the machine running the capture.

**Severity, stated plainly.** Low, and deliberately not inflated. wirecap is
inert unless `MCREMOTE_WIRE_CAPTURE_DIR` is set and is documented as "intended
for a developer capturing fixtures — never for production", so no shipped daemon
can reach this path. It is nevertheless real: redaction exists precisely so a
captured fixture can be shared or committed, and on Windows it under-delivers on
that promise. Context item 1 already records one fixture that shipped with a
username in it.

**Scope change, and why it stays inside this record rather than opening a new
one.** Decision Drivers already say "Tests only. No behaviour change *unless a
test forces a tiny fix*." A test has forced one, of exactly the anticipated
size. The decision itself is unchanged — close H1 and H2 with unit tables — so a
superseding MADR would restate it unaltered. The associated PLAN's blanket "Out
of scope: Product behaviour changes" is the stricter claim and is amended there.

### Considered options for the fix

* **Make the stripped form host-independent.** Remove the drive prefix and any
  leading separator of either flavour, without consulting `os.PathSeparator` or
  `filepath.VolumeName`.
  * Good, because it fixes every leaking case above, including the realistic
    Windows one the failing test does not itself cover.
  * Good, because `redact` then yields identical output for identical input on
    every host, which is what makes the behaviour testable without a
    `runtime.GOOS` branch.
  * Neutral, because it slightly widens what is treated as a home prefix.
* **Use `filepath.VolumeName` to strip the drive.**
  * Good, because it is the standard library's answer.
  * Bad, because it returns `""` for `C:\...` on POSIX, so behaviour would still
    differ by host and a Windows-shaped case could not be asserted portably.
* **Correct only the test, leaving `redact` as it is.**
  * Good, because it is the smallest diff and turns CI green.
  * Bad, because the leak on Windows is real and would survive, now with a test
    documenting that it is tolerated. Rejected.

Chosen: **make the stripped form host-independent**.

### Confirmation (amended)

The original three criteria stand. Added:

* `redact` yields no home-derived username for a POSIX home, a drive-lettered
  Windows home, and a UNC home, asserted in one table that needs no
  `runtime.GOOS` branch.
* `Go (windows/amd64)` is green on the PR — the leg that found this, and the
  only one that could have.
