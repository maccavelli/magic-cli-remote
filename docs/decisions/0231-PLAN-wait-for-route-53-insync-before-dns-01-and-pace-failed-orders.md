---
status: in-progress
date: 2026-11-05
associated-madr: "0231-MADR-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement waiting for Route 53 INSYNC before DNS-01 validation, and pacing failed orders

Associated MADR: [0231-MADR-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md](0231-MADR-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md)

## Goal

Close [issue #35](https://github.com/maccavelli/magic-cli-remote/issues/35):

1. A DNS-01 challenge is submitted to Let's Encrypt only after Route 53 reports the challenge
   record `INSYNC`, which AWS defines as on every Route 53 server for the zone (MADR D1).
2. Every DNS-01 wait fits inside the existing 3-minute start bound, and the 240 s unit timeout
   stays (D2).
3. The solver's timing is locked by tests that need no network (D3).
4. After a failed validation, neither daemon places another order for the same names within 12
   minutes, so a restart loop cannot use up the name's Let's Encrypt allowance (D4).
5. The guides say what the code does (D5).
6. A live staging issuance, and then a production one, succeed on the relay host that reproduced
   the bug.

## Scope

**In:**

* `internal/certs/acme.go` and `acme_test.go` (P1, P2);
* `internal/certs/acme_cooldown.go` and `acme_cooldown_test.go`, both new (P2);
* `go.mod`, where `github.com/mholt/acmez/v3` moves from indirect to direct, and `go.sum` if
  `go mod tidy` changes it (P2);
* `docs/guides/iam-route53-acme.md` (P1, P2);
* this pair, and `docs/README.md`'s generated records index (P0).

**Out, by the MADR:**

* the unit templates;
* `TimeoutStartSec`;
* `ACMEStartupTimeout`;
* `docs/guides/config.md`;
* any configuration key;
* `internal/certs/acme_http.go` and its tests, by the owner's decision ("leave HTTP-01 alone it
  works fine");
* the hosted zone and its SOA;
* certmagic itself;
* a certmagic or libdns upgrade. Both are at their newest release (F1). An upgrade during this
  plan is a deviation.

## Constraints

* No unit test reaches the network. The existing `TestEnsureACMEManageFailsCleanly` and
  `TestEnsureACMEHTTPManageFailsCleanly` already do, against the staging directory with a short
  timeout. They are left as they are.
* Each new test is seen failing on a scratch copy of the tree, never in the working tree. The
  record names the break and the failure text.
* Gates before staging any Go file:
  * `make pre-add-check FILES=…`;
  * `go vet` on darwin, linux and windows;
  * `make vet-tags`;
  * `make staticcheck`;
  * `make vulncheck`;
  * `make race`.

  Before a commit with Go changes: `go test -race ./...` on the Linux test host and `make
  ci-windows` on the Windows test host.
* Nothing committed names the relay host, its zone, its domain, the account, or a nameserver of
  the zone. Use `<relay-host>`, `<name>` and `<zone>`. Run the redact preview and the
  identifier scan before staging.
* The agent stages. The owner commits with `git commit --no-edit`. Push, tags and releases need
  the owner's word in the same turn.
* Live runs against Let's Encrypt (P3) happen only with the owner's approval. Production gets
  one order, after staging has passed.

## Implementation Steps

### P0 — The records

1. Present this pair. The owner decides D1 to D5, and the questions under "Owner decisions
   needed at approval".
2. Once the owner's uncommitted `docs/README.md` edits are committed, run `python3
   scripts/check_records.py --write-index`. Confirm that its diff adds only this pair's two
   rows. Until then the index is not touched.
3. `python3 scripts/check_records.py --check-all` exits 0. Then the redact preview and the
   identifier scan, over this pair.
4. Stage the pair, and `docs/README.md` if step 2 ran. The owner commits. The plan moves to
   `in-progress` on approval.

### P1 — The `INSYNC` wait and the bounds (D1, D2, D3)

Files: `internal/certs/acme.go`, `internal/certs/acme_test.go`, `docs/guides/iam-route53-acme.md`.

1. **Re-read before editing**, and stop on any difference, which is a deviation:
   * libdns/route53 v1.6.2: `WaitForRoute53Sync`, `Route53MaxWait`, `SkipRoute53SyncOnDelete`
     and `Logger` (`provider.go:44-76`). That `Route53MaxWait` defaults to 1 minute only when
     zero (`client.go:146-147`).
   * certmagic v0.25.6: `PropagationDelay`, `PropagationTimeout` and `TTL` on `DNSManager`.
   * The waiter's 30 s `MinDelay` (`api_op_GetChange.go:167`).
2. **Constants** in `acme.go`'s `const` block, each with a comment saying why:
   * `acmePropagationTimeout` becomes `30 * time.Second`. Its comment says that after `INSYNC`
     the authoritative poll confirms and does not wait, and that `PropagationTimeout` caps the
     poll but is not a wait (MADR F1).
   * New: `acmeRoute53SyncWait = 2 * time.Minute`. The comment says it is the most time spent
     waiting for Route 53 to report the change `INSYNC` ("propagated to all Route 53 DNS
     servers"). With the waiter's 30 s steps, 2 minutes gives a check between 60 s and 90 s,
     after Route 53's documented 60 seconds. The library's 1-minute default stops checking at
     about 30 s.
   * New: `acmeCABudget = 30 * time.Second`. It is an allowance used only by the budget test:
     the CA's validation, finalisation and download.
   * `ACMEStartupTimeout` stays `3 * time.Minute`. Its comment adds that it must cover
     `acmeRoute53SyncWait + acmePropagationTimeout + acmeCABudget` (MADR D2), and that a run
     cut off before the challenge is submitted costs no failed validation (MADR F4).
3. **`newDNS01Solver(r Route53Options, logger *zap.Logger, providerLog *slog.Logger)
   *certmagic.DNS01Solver`**, unexported, in `acme.go`. It returns today's solver with:
   * `route53.Provider{…, WaitForRoute53Sync: true, Route53MaxWait: acmeRoute53SyncWait,
     SkipRoute53SyncOnDelete: true, Logger: providerLog}`;
   * `TTL: acmeChallengeTTL` and `PropagationTimeout: acmePropagationTimeout`;
   * `PropagationDelay` left zero, with a comment naming MADR D1 and the rejected 60 s delay.

   The comment above the function states issue #35's race in two sentences: certmagic's first
   match, then `INSYNC`. `EnsureACME` calls it in place of the literal.
4. **The provider's log (D3).** `EnsureACME` passes `providerLog` as nil when `opts.Verbose` is
   false. When it is true, it passes `slog.New(slog.NewTextHandler(os.Stderr,
   &slog.HandlerOptions{Level: slog.LevelDebug}))`, so the library's debug events reach the
   journal beside certmagic's. Those events are "Route53 change submitted", "waiting for Route53
   sync" and "Route53 sync complete". No new dependency.
5. **Tests in `acme_test.go`:**
   * `TestDNS01SolverWaitsForRoute53Sync`: call `newDNS01Solver(Route53Options{HostedZoneID:
     "Z0", Region: "us-east-1"}, zap.NewNop(), nil)`. Assert:
     * the provider is a `*route53.Provider` with `WaitForRoute53Sync`,
       `SkipRoute53SyncOnDelete`, `Route53MaxWait == 2*time.Minute`, and the zone, region and
       retries passed through;
     * `PropagationDelay == 0`, `PropagationTimeout == 30*time.Second` and `TTL ==
       5*time.Minute`.

     Each failure message names MADR D1 or D2.
   * `TestDNS01BudgetFitsTheStartBound`: `acmeRoute53SyncWait + acmePropagationTimeout +
     acmeCABudget <= ACMEStartupTimeout`, and `acmeRoute53SyncWait >= 90*time.Second`. The
     second check keeps a `GetChange` call after Route 53's 60 seconds, given the waiter's 30 s
     steps (F3).
   * `TestEnsureACMEVerboseLogsTheProvider` is not added. The live acceptance reads those lines
     (P3). A unit test of a handler choice would assert the code, not the outcome.
6. **Seen failing (scratch copy), one break each:**
   * `WaitForRoute53Sync: false`: fails the solver test;
   * the line `Route53MaxWait: acmeRoute53SyncWait` removed, leaving the library's default: the
     solver test fails, and records the value it found;
   * `acmeRoute53SyncWait = time.Minute`: fails the budget test's second check;
   * `acmePropagationTimeout = 2 * time.Minute`: fails the solver test and the budget test;
   * `PropagationDelay: 60 * time.Second` added: fails the solver test.
7. **`docs/guides/iam-route53-acme.md`:**
   * Under "Why each statement exists", a short paragraph headed "When validation starts". It
     gives the order: the TXT is written; the daemon waits up to 2 minutes for Route 53 to report
     `INSYNC`; certmagic confirms on the zone's nameservers for up to 30 s; only then is Let's
     Encrypt asked. It also says that the first issuance as a whole is bounded at 3 minutes.
   * The troubleshooting row "Validation times out" is rewritten. "exceeded max wait time for
     ResourceRecordSetsChanged waiter" means Route 53 did not report `INSYNC` within 2 minutes.
     It cost no failed validation, so restart, and check the Route 53 Health Dashboard.
   * A new row for issue #35's symptom: "No TXT record found" / "During secondary validation" on
     a build from before this plan means upgrade; the race is the reason.
   * `python3 scripts/check_records.py --check-all` exits 0.
8. Gates, host runs, identifier scan and redact preview. Stage the three files and this PLAN with
   its P1 record. The owner commits.

### P2 — The cooldown (D4)

Files:

* `internal/certs/acme_cooldown.go` (new) and `internal/certs/acme_cooldown_test.go` (new);
* `internal/certs/acme.go` and `internal/certs/acme_test.go`;
* `go.mod`, and `go.sum` if it changes;
* `docs/guides/iam-route53-acme.md`.

1. **Re-read and record:**
   * acmez v3.1.7's `acme.Problem` (`acme/problem.go:26`) and the type constants (`:140-160`);
   * that certmagic matches the problem with `errors.As(err, &problem)` on a value
     (`config.go:759-762`, `acmeissuer.go:427-428`), so the problem survives certmagic's
     wrapping;
   * `(*certmagic.Config).CacheManagedCertificate` (`certificates.go:257`), and that it returns
     an error wrapping `fs.ErrNotExist` when storage has no certificate (`config.go:496-500`).
2. **`acme_cooldown.go`:**
   * `const acmeCooldown = 12 * time.Minute`. The comment says it is Let's Encrypt's refill
     interval, one authorization failure per identifier every 12 minutes, so a loop stays at or
     under the 5-per-hour limit (F4).
   * The record is `<StorageDir>/mcremote-issuance-failure.json`, mode 0600, holding
     `{"directory", "domains" (sorted), "failed_at" (RFC 3339, UTC), "problem"}`. The name does
     not collide with certmagic's `FileStorage` layout, whose top level holds only directories
     (`certificates`, `acme`, `locks`, `ocsp`).
   * `cooldownProblem(err error) (string, bool)`: `errors.As` to `acme.Problem`. True for the
     MADR's types: `unauthorized`, `dns`, `caa`, `connection`, `incorrectResponse`, `tls`,
     `compound` and `rateLimited`, by `acme.ProblemType…` constant.
   * `recordFailure(dir, directory string, domains []string, problem string, now time.Time)
     error`: an atomic write, a temp file then a rename, as `certs.go` writes its own files.
   * `activeCooldown(dir, directory string, domains []string, now time.Time) (until time.Time,
     problem string, ok bool)`.
     * A missing or unreadable record, or one for other domains or another directory, is not
       active.
     * A `failed_at` more than a minute in the future is deleted and treated as not active.
   * `clearFailure(dir string)`: removes the file. A missing file is fine.
   * `var ErrIssuanceCooldown = errors.New("certs: acme: waiting out a failed validation")`,
     exported so callers can tell the refusal apart. The returned error wraps it, and names the
     problem, `failed_at`, the time an order is next allowed, and the record's path.
3. **Wire it into `EnsureACME`** only, after `magic` is built and before `ManageSync`.
   `EnsureACMEHTTP` is not touched (the owner's decision):
   * If `activeCooldown` holds, check every domain with `magic.CacheManagedCertificate(mctx, d)`.
     If any returns `fs.ErrNotExist`, stop the cache and return the `ErrIssuanceCooldown` error,
     without ordering. If every domain is in storage, carry on to `ManageSync`, which manages the
     cached certificates and does not order (`config.go:488-493`).
   * After `ManageSync` fails, call `recordFailure` if `cooldownProblem(err)` is true. A write
     error is joined to the returned error and does not replace it.
   * After `ManageSync` succeeds, call `clearFailure`.
   * The `now` time is a package variable, `var nowFunc = time.Now`, so tests can set it.
4. **`go.mod`:** `github.com/mholt/acmez/v3 v3.1.7` moves to the direct `require` block. `go mod
   tidy` makes that move and nothing else. Assert that `go.sum` is unchanged, or changes only
   for that module. No version moves.
5. **Tests**, `acme_cooldown_test.go`, all offline:
   * `TestCooldownProblemTypes`: a table of `acme.Problem` values, wrapped as certmagic and
     acmez wrap them (`fmt.Errorf("[%s] Obtain: %w", …)` around `fmt.Errorf("solving challenges:
     %w (order=%s)", …)`). The eight listed types are true. `serverInternal`, `malformed`, a
     `context.DeadlineExceeded` and a plain error are false.
   * `TestCooldownRecordAndExpiry`: record at T, active at T+11m59s, not active at T+12m, and
     `until` is T+12m.
   * `TestCooldownKeyedByDirectoryAndNames`: active for the same sorted names in another order,
     and not for another directory or another name set.
   * `TestCooldownFutureRecordIgnored`: a `failed_at` two minutes ahead is not active, and the
     file is gone afterwards.
   * `TestCooldownRecordIsPrivate`: on non-Windows, the file's mode is 0600.
   * `TestEnsureACMERefusesDuringCooldown`, in `acme_test.go`:
     * a fresh record for `unlikely-acme-test.invalid` in an empty storage directory, with a
       `DirectoryURL` that points at `http://127.0.0.1:1/directory`;
     * `EnsureACME` returns an error that `errors.Is` `ErrIssuanceCooldown` in under a second;
     * the unreachable directory proves no request was made, since a request would fail with a
       connection error instead.
   * `TestEnsureACMECooldownStillLoadsAStoredCertificate`: a self-signed certificate written
     into certmagic's storage layout for the test directory's issuer key, via
     `certmagic.StorageKeys.SiteCert` and `SitePrivateKey` with the issuer's `IssuerKey()`, and
     a fresh record. `EnsureACME` does not return `ErrIssuanceCooldown`.
     * If the issuer key cannot be derived without the network, this test is replaced by one on
       the gate function itself, with a stubbed loader, and the record says so.
6. **Seen failing (scratch copy):**
   * `acmeCooldown = 0`: fails the expiry test;
   * `rateLimited` dropped from the list: fails the types test on that row;
   * the gate in `EnsureACME` removed: the refusal test fails with a connection error, not the
     sentinel;
   * the stored-certificate check skipped, so the gate refuses unconditionally: fails the
     stored-certificate test;
   * the future-date rule removed: fails the future-record test.
7. **`docs/guides/iam-route53-acme.md`'s troubleshooting**, two rows:
   * "waiting out a failed validation" names the record and the 12 minutes. It says to fix the
     cause, then either wait for the time the error gives or delete the record. Deleting it
     spends one more of Let's Encrypt's five per hour.
   * "the relay unit is `failed (start-limit-hit)`" gives `systemctl --user reset-failed
     mcrelay` and then `systemctl --user start mcrelay`, after the cause is fixed.
8. Gates, host runs, scans. Stage with the P2 record. The owner commits.

### P3 — Live acceptance on the relay host (Confirmation)

Run by the agent over SSH to `<relay-host>` (the owner, 2026-11-05: "you will, you have ssh access
to it from here"). Every remote call is one plain command, with `-o BatchMode=yes -o
ConnectTimeout=5`. Nothing here is committed except the record, written with placeholders. The
host's facts are the MADR's "The relay host, probed 2026-11-05".

1. **The build.**
   * Cross-built on this Mac from the commit after P2: `GOOS=linux GOARCH=amd64
     CGO_ENABLED=0`, with the Makefile's `build` flags (`-trimpath`, `-ldflags "-s -w -X
     <buildinfo>.version=… -X <buildinfo>.kind=local"`), written to the session scratchpad,
     not `bin/`. The host's Go 1.26.5 cannot build `go 1.27.3`.
   * Copied with `scp` to `~/.local/bin/mcrelay.0231`.
   * The installed `~/.local/bin/mcrelay` is copied to `mcrelay.bak-0231` beside it before it
     is replaced.
   * `mcrelay version` on the host names the commit.
   * No tag or release. Those need the owner's word.
2. **Before:**
   * the unit is `inactive`; if not, `systemctl --user stop mcrelay`;
   * the instance role allows `route53:GetChange`: `aws route53 get-change` with a made-up id
     answers `NoSuchChange`, as it did on 2026-11-05;
   * back up the config and the ACME cache directory, `tls.letsencrypt.cache_dir`, beside
     themselves.
3. **Staging into an empty cache.** A copy of the config with `staging: true`, a new empty
   `cache_dir` and `log.level: debug`. Run `mcrelay serve --config <copy>` in the foreground
   under `timeout 240` until `listening`, then stop it. The log must show, in this order:
   * "Route53 change submitted";
   * "waiting for Route53 sync";
   * "Route53 sync complete";
   * certmagic's validation success;
   * `listening`.

   Record the times between them.
4. **Production, once.** The real config and its default cache directory, which holds no
   production certificate (issue #35), through the unit: `systemctl --user start mcrelay`.
   `journalctl --user -u mcrelay` is read for the three Route 53 lines at `info`; they are debug
   events, so their absence there is expected, and step 3 is their evidence. `listening` arrives, and
   `openssl s_client -connect <name>:<listen.port> -servername <name>` shows a Let's Encrypt production
   issuer. If it fails: stop the unit at once, record the error, and treat it as a deviation.
   No second production order without the owner's word.
5. **The cooldown, against staging.** Optional, and only with the owner's word in that turn. Force an
   unauthorized result in a staging run, for example by pointing `route53.hosted_zone_id` at a
   zone that does not serve `<name>`. The next start must refuse with `ErrIssuanceCooldown`'s
   message and make no order.
6. **The daemon.** If a daemon host uses `tls.mode: letsencrypt`, one restart there, with the
   log showing the same three Route 53 lines. Otherwise record that none does.
7. Remove the temporary config copy and the staging cache. Keep the backups until the owner
   removes them.

### P4 — Close-out

1. This PLAN goes to `complete`, and the MADR to `accepted`, with the date. The Verification's
   items are checked against the records of P1 to P3.
2. Offer the owner a comment for issue #35 that links this pair and the commits. Post it, and
   close the issue, only on the owner's word.

## Verification

### Acceptance criteria (mapped to the MADR's Confirmation)

| # | Criterion | Phase |
| :--- | :--- | :--- |
| V1 | `newDNS01Solver` sets `WaitForRoute53Sync`, a 2-minute `Route53MaxWait`, `SkipRoute53SyncOnDelete`, a 30 s `PropagationTimeout`, a 5-minute TTL, and no `PropagationDelay`; its test fails on each listed break | P1 |
| V2 | The D2 budget holds by test, and `TestUnitIsTypeNotify` passes unchanged | P1 |
| V3 | After a failure of a listed type, a second `EnsureACME` within 12 minutes makes no request, and returns `ErrIssuanceCooldown`; `acme_http.go` is unchanged | P2 |
| V4 | A certificate in storage is not blocked by a cooldown | P2 |
| V5 | Gates clean on this Mac, the Linux test host and the Windows test host, for each commit | P1, P2 |
| V6 | Live staging into an empty cache logs the `INSYNC` wait before validation, and reaches `listening` | P3 |
| V7 | One live production issuance reaches `listening` with a production certificate | P3 |
| V8 | The guide's timing and troubleshooting rows match the code | P1, P2 |

### Commands

* `go test -race -count=1 ./internal/certs/... ./internal/relay/... ./internal/daemon/... ./internal/cli/service/...`
* `make race`
* `make pre-add-check FILES="…"`
* `make lint staticcheck vet-tags vulncheck`
* `python3 scripts/check_records.py --check-all`
* the host runs: the Linux test host's `go test -race ./...`, and `make ci-windows` on the
  Windows test host.

## Rollout and Rollback

* **Rollout.** P1 fixes issue #35's race, and P2 bounds the cost of any later failure. By the
  owner's decision they ship together, after P3. Both reach a running host only as a
  new binary. A host keeps today's behaviour until it is upgraded.
* **Compatibility.**
  * No configuration, unit or state change is needed to upgrade.
  * The cooldown record is created only after a failure, and old binaries ignore it.
  * A role without `route53:GetChange` now fails before validation, with `AccessDenied`. The
    guide has always required it.
* **Rollback.** Install the previous binary. Delete `mcremote-issuance-failure.json` from the
  ACME cache directory if it exists. Nothing else was written. A rolled-back relay has issue
  #35's race again.

## Owner decisions (answered 2026-11-05)

The owner: "D1 insync alone D2 leave HTTP-01 alone it works fine. D3 we will complete the plan D4 you will, you have ssh access to it from here, it is <relay-host>".

1. **D1's mechanism:** `INSYNC` alone.
2. **D4:** the cooldown, on DNS-01 only. HTTP-01 is left as it is.
3. **Shipping:** the whole plan is completed. P1 is not released on its own.
4. **P3:** the agent runs it over SSH. Whether P3 step 5 runs is asked in that turn.

## Not done by this plan

* The zone's SOA minimum, an operator change in the console (issue #35).
* certmagic's `checkAuthoritativeNss` itself. A5 is not chosen. An upstream issue about its
  first-match return could be offered to the owner separately.
* Pacing restarts in the units (B2), and keeping the relay up on a self-signed certificate (B3).
* Any change to renewal's asynchronous retry. A stored certificate is never gated (D4).

## Execution record

### P0 — The records (2026-11-05; approved by the owner: "proceed")

* **Step 1.** The pair was presented. The owner's answers to the four questions are under "Owner
  decisions (answered 2026-11-05)", and the MADR records them and is `accepted`.
* **Step 2, deferred.** `docs/README.md` still holds the owner's own uncommitted edits, so
  `--write-index` was not run. It would rewrite the file under them. The index gains this pair's
  rows in the first phase after those edits are committed. That phase's record says so.
* **Step 3.**
  * `python3 scripts/check_records.py --check-all`: exit 0.
  * The redact preview: "would edit 0 file(s)".
  * The identifier scan over the pair: "hits: 0".
  * A search of the pair for the relay host's alias, its domain and the account's home path
    found nothing.
* **Step 4.** Staged: this pair. The owner commits.

**Files for this commit:** this PLAN and the 0231 MADR.
