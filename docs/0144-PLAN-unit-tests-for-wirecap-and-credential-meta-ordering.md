---
status: in-progress
date: 2026-09-06
associated-madr: "0144-MADR-unit-tests-for-wirecap-and-credential-meta-ordering.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement: unit tests for wirecap and CredentialMeta ordering

Associated MADR: [0144-MADR-unit-tests-for-wirecap-and-credential-meta-ordering.md](0144-MADR-unit-tests-for-wirecap-and-credential-meta-ordering.md).

## Goal

Pin wirecap's disable/redact/frame/tee behaviour and CredentialMeta's Fresher /
NotOlder ordering with deterministic unit tables. Open one PR against `master`.

## Scope

**In scope.** `internal/wirecap/*_test.go`; `internal/providerauth` CredentialMeta
table tests; this MADR/PLAN pair.

**Out of scope.** ~~Product behaviour changes;~~ debugserve tests; flake/CI retry
ledger (0143); converting timing-sensitive tests.

> **Amended 2026-09-06.** The product-behaviour exclusion is struck for one
> narrowly bounded change: `internal/wirecap.redact`. Phase 1's own test found
> that its stripped-separator redaction never runs on Windows, so the exclusion
> as written would require shipping a known home-path leak. The MADR's driver
> already allowed this — "no behaviour change *unless a test forces a tiny
> fix*" — and the amendment there records the analysis. Nothing else in
> `internal/wirecap` or `internal/providerauth` is in scope; the exclusion
> stands for every other product file.

## Execution Phases

### Phase 1 — wirecap unit table

Add tests covering at least:

* `TestForNilWhenEnvUnset`
* `TestForNilWhenProviderBlank`
* `TestForWritesFramesWhenEnabled`
* `TestNilCaptureMethodsNoop`
* `TestFrameEscapesEmbeddedNewlines`
* `TestRedactAbsoluteAndRelativeHome`
* `TestTeeReaderEmitsOnNewline`
* `TestFrameNoopOnWhitespace`

*Exit criterion.* `go test ./internal/wirecap -count=1` green.

### Phase 2 — CredentialMeta table

Confirm no dedicated Fresher/NotOlder table exists (recovery uses `NotOlder`
indirectly). Add `credential_meta_test.go` with `TestFresher` and `TestNotOlder`
tables for mode mismatch, expiry order/equality, sequence order/equality,
zero-sequence refusal, and expiry-preferred-over-sequence.

*Exit criterion.* `go test ./internal/providerauth -count=1` green with the new
subtests.

### Phase 3 — PR

Branch `test/0144-wirecap-and-credential-meta`, include MADR+PLAN+tests, open PR.

*Exit criterion.* PR open; CI test jobs green on the new packages.
**Not met** — see the deviation below. PR #23 is open, but `Go (windows/amd64)`
is red.

### Phase 4 — make wirecap redaction host-independent

Added 2026-09-06 by the deviation below. Ordered so the instrument is proven
before the fix exists, per the repository's rule that a check is not trusted
until it has been seen to fail.

1. **Rewrite `TestRedactAbsoluteAndRelativeHome` as a portable table.** Cases:
   POSIX home `/Users/alice`; Windows home `C:\Users\alice` with a
   drive-stripped payload form; UNC home `\\srv\home\alice`; empty home is
   inert; `/` home is inert. Each case asserts both that no home-derived
   username survives and that the expected replacement text is present. No
   `runtime.GOOS` branch and no `t.Skip`: after step 2 the function's output
   depends only on its inputs, so a platform branch here would be hiding
   something.
2. **Watch it fail before fixing.** Run the new table against the *unmodified*
   `redact` on this POSIX host. The drive-lettered and UNC cases must fail;
   record the failure text in the execution record. A table that has only ever
   been seen green would not distinguish a real fix from no fix.
3. **Apply the fix.** Replace the `os.PathSeparator` trim with a helper that
   strips a leading `X:` drive prefix and then any leading `/` or `\`,
   consulting neither `os.PathSeparator` nor `filepath.VolumeName`. Keep the
   existing early return for `home == ""` and `home == "/"`, and keep the
   `rel != c.home` guard so a home that strips to itself still performs no
   second replacement.
4. **Re-run.** The table passes; `go test ./internal/wirecap -count=1` green.

*Affected files.* `internal/wirecap/wirecap.go` (the `redact` body and one new
unexported helper), `internal/wirecap/wirecap_test.go` (one test rewritten).
No other file changes.

*Exit criterion.* The table is green on POSIX **and** `Go (windows/amd64)` is
green on PR #23 — the leg that found the defect is the only one that can
confirm the fix. A green POSIX run alone does not close this phase.

## Verification

```bash
go test ./internal/wirecap ./internal/providerauth -count=1

# Phase 4: the redaction table specifically, verbose so each case is named
go test ./internal/wirecap -run TestRedactAbsoluteAndRelativeHome -count=1 -v

# Phase 4 step 2 (before the fix): the drive-lettered and UNC cases must FAIL.
# Run against a scratch copy of the tree, never by dirtying this one.
```

The Windows half cannot be verified locally on a POSIX host. It is verified by
`Go (windows/amd64)` on PR #23, which is where the defect surfaced.

## Task Checklist

* [x] Owner greenlight (Chief of Staff / Mac)
* [x] Phase 1 wirecap tests
* [x] Phase 2 CredentialMeta table
* [x] Phase 3 PR opened — https://github.com/maccavelli/magic-cli-remote/pull/23
* [ ] Phase 3 exit criterion — CI green on the new packages (blocked: windows leg red)
* [ ] Phase 4 step 1 — portable redaction table written
* [ ] Phase 4 step 2 — table seen to fail against unmodified `redact`
* [ ] Phase 4 step 3 — host-independent strip applied to `redact`
* [ ] Phase 4 step 4 — `Go (windows/amd64)` green on PR #23

## Execution Record

### Deviation — 2026-09-06, Phase 1's test found a product defect

*Evidence.* CI run `34011907466`, job `Go (windows/amd64)`:

```text
--- FAIL: TestRedactAbsoluteAndRelativeHome (0.00s)
    wirecap_test.go:86: username leaked after redact: "cwd=/home/user/proj and also Users/alice/proj"
```

Deterministic, not a flake: it failed both attempts of the MADR 0143 retry, and
the other four test jobs were green. Genuinely pre-existing rather than caused
by this branch — `internal/wirecap/wirecap.go` is untouched here (the branch
diff is +495/-0 across two test files and two docs), and the defect is in the
`redact` body as published on `master`.

*Diagnosis.* `redact` builds its stripped-separator form with
`strings.TrimPrefix(c.home, string(os.PathSeparator))`. On Windows that
separator is `\`, so the trim is a no-op, the `rel != c.home` guard is false,
and the replacement never runs. It is not limited to the POSIX-shaped home the
test happens to use: a realistic `C:\Users\alice` also begins with a drive
letter rather than a separator, so the stripped form never fires on Windows for
any home a Windows host can produce. The MADR amendment carries the evaluated
table.

*Resolution chosen — fix `redact`, add Phase 4.* The alternative, correcting
only the test, was rejected: it turns CI green while leaving a real leak in
place and adds a test asserting that the leak is acceptable. That is the
workaround shape this repository does not ship.

*Scope grown.* `internal/wirecap/wirecap.go` joins the phase's file list. It is
the first product file in a PR that declared itself tests-only; the Scope
section is annotated accordingly and the MADR amendment explains why this stays
inside 0144 rather than opening 0145.

*Consequence of doing nothing.* PR #23 cannot merge — the branch is
`MERGEABLE/UNSTABLE` on a red required leg. Reverting the test to make it pass
would ship the leak with a test documenting it as intended.
