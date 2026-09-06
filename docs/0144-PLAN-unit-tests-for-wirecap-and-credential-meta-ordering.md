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

**Out of scope.** Product behaviour changes; debugserve tests; flake/CI retry
ledger (0143); converting timing-sensitive tests.

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

## Verification

```bash
go test ./internal/wirecap ./internal/providerauth -count=1
```

## Task Checklist

* [x] Owner greenlight (Chief of Staff / Mac)
* [ ] Phase 1 wirecap tests
* [ ] Phase 2 CredentialMeta table
* [ ] Phase 3 PR opened
