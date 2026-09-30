---
status: proposed
date: 2026-09-29
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# MADR 0174: Keep dependencies free of known advisories, with one gated exception

## Context and Problem Statement

The owner's rule for toolchains (MADR 0169 D1: no published advisory affects the chosen
version) should hold for this repository's dependencies too: "ideally no deps should have
active advisories or known vulnerabilities." The Go module fails it today, and nothing
catches that.

### What was measured, not assumed (2026-09-29)

- **`govulncheck -show verbose ./...` on `master` (go1.27.1)** reports no *called*
  vulnerability. It lists three in a *required* module, `golang.org/x/crypto@v0.55.0`:
  - **GO-2026-6354** and **GO-2026-6355**: denial of service from deadlocked `ssh` channels.
    Fixed in v0.56.0.
  - **GO-2026-5932**: "the `golang.org/x/crypto/openpgp` package is unmaintained, unsafe by
    design, and has known security issues". OSV's affected range is `introduced: 0` with no
    fix. Every x/crypto release, v0.57.0 included, still ships `openpgp`.
- **Nothing here uses `openpgp`.** `go mod why golang.org/x/crypto/openpgp` says the main
  module does not need it. The only x/crypto packages in the build are `ocsp` and
  `cryptobyte`, reached through `github.com/caddyserver/certmagic`.
- **x/crypto cannot leave the module graph.** It is required by certmagic (v0.25.4, its
  newest release), by certmagic's ACME client `github.com/mholt/acmez/v3`, and by
  `golang.org/x/net`. Dropping Let's Encrypt support would not remove it.
- **A bump clears the two ssh advisories.** In a scratch clone:
  - `go get golang.org/x/crypto@v0.56.0` changes only x/crypto.
  - `@v0.57.0`, the newest, also moves x/sys 0.47→0.48, x/mod 0.40→0.41, x/sync 0.22→0.23,
    x/term 0.45→0.46 and x/text 0.41→0.42.
  - After either bump, govulncheck reports only GO-2026-5932.
- **The gate does not see any of this.** `scripts/go-precheck.sh` (`make pre-add-check`)
  passes when govulncheck prints "No vulnerabilities found". govulncheck prints that
  whenever nothing is *called*, even with findings in imported packages or required
  modules. CI does not run govulncheck at all.
- **The Flutter app is clean.** All 163 packages in `apps/mobile/pubspec.lock` return no
  advisory from OSV (ecosystem Pub).

## Decision Drivers

- The owner's rule: no dependency with a known advisory.
- A finding must be seen by a gate, locally and in CI, not found by accident.
- An exception is allowed only when no fix exists, the vulnerable code is not in the
  build, and the record says so. It must stop being allowed the moment either fact
  changes.
- No feature is removed to silence a finding that does not reach the binary.

## Considered Options

* **A — Bump x/crypto to the newest release (v0.57.0), and record GO-2026-5932 as a gated
  exception**, while making every govulncheck level a failing finding.
* **B — Bump x/crypto to v0.56.0 only** (the minimal fix for the ssh advisories), with the
  same exception and gate.
* **C — Remove x/crypto from the build** by dropping certmagic and Let's Encrypt support.
* **D — Bump only**, with no gate change.

## Decision Outcome

Chosen option: **A**, because it clears every advisory that has a fix, at the newest
releases (0169's rule applied to dependencies). It also makes the one advisory with no fix
explicit and bounded, and it closes the gate gap that let three findings through. C is
impossible: `golang.org/x/net` keeps x/crypto in the graph. It would also remove a product
feature for a package the binary does not contain. B leaves the other `golang.org/x`
modules behind. D repeats today's gap.

### The decisions

- **D1 — Dependencies follow the toolchain rule.** No required Go module, and no pub
  package, may carry a known advisory that a newer release fixes. The newest release is
  preferred.
- **D2 — Bump `golang.org/x/crypto` to v0.57.0**, with the `golang.org/x` modules
  `go mod tidy` moves alongside it. This closes GO-2026-6354 and GO-2026-6355.
- **D3 — GO-2026-5932 is the one recorded exception.** It is listed in an allowlist file
  with its reason and review date. The gate accepts an allowlisted ID **only** at the
  required-module level. If a listed advisory ever shows up as imported or called, the
  gate fails, because `openpgp` would then be in the build. An unlisted finding fails at
  any level.
- **D4 — The gate checks every govulncheck level.** `scripts/go-precheck.sh` reads
  `govulncheck -show verbose` and fails on any called, imported or required-module
  finding not covered by D3. `make pre-add-check` and CI's Go job run it.
- **D5 — The Flutter app's pub dependencies are checked against OSV** in
  `make preflight`, by the same rule.

### Consequences

- Good, because every fixable advisory in the dependency graph is fixed now, and a new one
  fails a gate locally and in CI.
- Good, because the exception is narrow. It names one advisory, holds only while
  `openpgp` stays outside the build, and carries a date to review it again.
- Neutral, because CI gains a govulncheck step. It needs the vulnerability database, and
  the precheck's existing "database unreachable is a warning" rule applies there too.
- Bad, because a new advisory in any dependency now blocks `make pre-add-check`, with or
  without a code change. That is the point, but it can arrive at an inconvenient moment.
- Bad, because the exception has to be revisited by hand. It lapses only when upstream
  deletes `openpgp` or publishes a fix.

### Confirmation

```sh
govulncheck -show verbose ./...     # only GO-2026-5932, only as a required module
make pre-add-check                  # passes with the allowlist, and fails on a scratch copy without it
# and fails on a scratch clone at x/crypto v0.55.0, naming GO-2026-6354 and GO-2026-6355
make preflight                      # includes the pub OSV check
```

## Pros and Cons of the Options

### A — Newest x/crypto, a gated exception, a strict gate (chosen)

- Good, because it is the owner's rule, applied the same way as MADR 0169.
- Good, because the gate stops the next finding.
- Bad, because five other `golang.org/x` modules move in the same change, which widens what
  the gates must cover.

### B — x/crypto v0.56.0 only

- Good, because it is the smallest diff that fixes the ssh advisories.
- Bad, because it deliberately stops short of the newest release.

### C — Drop certmagic

- Bad, because it is impossible: `golang.org/x/net` still requires x/crypto.
- Bad, because it removes the Let's Encrypt TLS mode to silence a finding in code the binary
  does not contain.

### D — Bump without a gate

- Good, because it is the least work.
- Bad, because nothing would catch the next advisory. This one surfaced only because P3 of
  PLAN 0169 read govulncheck's verbose output.

## More Information

- `govulncheck -show verbose ./...`, 2026-09-29, go1.27.1: three x/crypto findings, none
  called.
- OSV GO-2026-5932: `affected: golang.org/x/crypto, introduced 0`, no fix.
- `go mod graph`: x/crypto is required by certmagic, acmez/v3, golang.org/x/net and this
  module.
- `proxy.golang.org/github.com/caddyserver/certmagic/@v/v0.25.4.mod`: requires x/crypto
  v0.50.0. v0.25.4 is the newest release.
- OSV `querybatch`, ecosystem Pub, over `apps/mobile/pubspec.lock`: 163 packages, 0
  advisories.
- Related: [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md).
  Its P3 found these findings.
