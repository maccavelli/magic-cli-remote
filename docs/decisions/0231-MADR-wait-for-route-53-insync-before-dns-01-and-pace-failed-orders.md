---
status: accepted
date: 2026-11-05
decision-makers: Project Owner
consulted: "GitHub issue #35 and its live evidence; certmagic v0.25.6, libdns/route53 v1.6.2, aws-sdk-go-v2/service/route53 v1.70.6, smithy-go v1.28.7 and mholt/acmez/v3 v3.1.7 as pinned in go.mod/go.sum, read from the module cache; the AWS Route 53 API Reference for GetChange and ChangeResourceRecordSets and the Let's Encrypt rate-limits page, fetched 2026-11-05; docs/guides/iam-route53-acme.md; 0207-MADR D22 and its 2026-10-26 addition; this repository at a115e40e"
informed: anyone who runs mcrelay or mcremote with tls.letsencrypt, or changes internal/certs or the service unit templates
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Wait for Route 53 to report INSYNC before DNS-01 validation, and stop re-ordering after a failed validation

## Context and Problem Statement

[GitHub issue #35](https://github.com/maccavelli/magic-cli-remote/issues/35) reports that no
production Let's Encrypt certificate can be issued over DNS-01 for a name in a public Route 53
zone. `mcrelay` never opens its listener. Restarting it spends Let's Encrypt's failed-validation
allowance for the name.

The evidence, from a live `mcrelay` 0.20.1 on an EC2 host, with `tls.letsencrypt.challenge:
dns-01`, `staging: false` and instance-role credentials:

* About seven seconds after the service started, the challenge TXT record was on two of the
  zone's four authoritative Route 53 nameservers and absent from the other two.
* About eight seconds after start, Let's Encrypt failed the order: `HTTP 403
  urn:ietf:params:acme:error:unauthorized - No TXT record found at _acme-challenge.<name>`. An
  earlier attempt the same day failed with `During secondary validation: No TXT record found`.
* The process exited before `listening`, and `Restart=always` started it again.

The issue reproduced it with the parent delegation and the apex NS set listing the same four
nameservers. An earlier NS mismatch had been corrected first, so it is not the cause.

### F1. Nothing waits for Route 53 to finish

`internal/certs/acme.go:108-118` builds the DNS-01 solver like this:

```go
solver := &certmagic.DNS01Solver{
    DNSProvider: &route53.Provider{
        Region:       opts.Route53.Region,
        Profile:      opts.Route53.Profile,
        HostedZoneID: opts.Route53.HostedZoneID,
        MaxRetries:   opts.Route53.MaxRetries,
    },
    TTL:                acmeChallengeTTL,       // 5 min
    PropagationTimeout: acmePropagationTimeout, // 2 min
    Logger:             logger,
}
```

`DNS01Solver` embeds `certmagic.DNSManager` (`solvers.go:256-258`), so these fields are
`DNSManager`'s. `PropagationDelay` is left at zero.

The issue cites certmagic v0.25.4, but `go.mod` pins v0.25.6, which is the newest release
(`go list -m -u` lists no update). The code below is v0.25.6's, read from the module cache.

* **`DNSManager.wait`** (`solvers.go:419-482`) sleeps for `PropagationDelay` only when it is
  greater than zero (`:424`). It then polls every 2 seconds (`:442`) until
  `checkDNSPropagation` reports ready, or until `PropagationTimeout` passes. With `Resolvers`
  unset, as here, it checks the zone's authoritative nameservers (`:445`).
* **`checkAuthoritativeNss`** (`dnsutil.go:266-306`) is documented as querying "each of the
  given nameservers". In fact it returns `true` at the first nameserver whose TXT value matches
  (`:288-289`), and never queries the rest. A nameserver answering NXDOMAIN or SERVFAIL returns
  `false` for that round (`:274-278`), so the outcome depends on the order the nameservers are
  queried in.
* So once any one Route 53 server has the record, the wait can end and the challenge be
  submitted. Let's Encrypt validates from several network perspectives, against more than one
  authoritative server, and fails the authorization if any of them answers that the record is
  missing.

`PropagationTimeout` does not help, because it only caps the poll, and the poll ends at the
first match.

### F2. The fix the repository documents was never wired up

`docs/guides/iam-route53-acme.md:186` explains the `PollChangePropagation` statement this way:
"the solver polls `GetChange` until `INSYNC` before telling Let's Encrypt to validate". Its policy
grants `route53:GetChange` for exactly that (`:162-166`). The code does not do it:

* **libdns/route53 v1.6.2 makes the wait opt-in.** `Provider.WaitForRoute53Sync` (`provider.go:48-51`)
  defaults to false. When it is true, each record change waits on the SDK's
  `ResourceRecordSetsChangedWaiter` until `GetChange` reports `INSYNC`, for at most
  `Route53MaxWait` (`client.go:405-433`). The default is 1 minute (`client.go:146-147`).
* **No Go file in this repository sets `WaitForRoute53Sync`.** It was opt-in under its earlier
  name `WaitForPropagation` too (libdns/route53 `BREAKING.md`, "WaitForPropagation →
  WaitForRoute53Sync"), so it was never on.
* **AWS defines `INSYNC` as the condition this bug needs:** "INSYNC indicates that the changes
  have propagated to all Route 53 DNS servers managing the hosted zone" (Route 53 API Reference,
  GetChange). AWS's `ChangeResourceRecordSets` reference says that changes "generally propagate
  to all Route 53 name servers managing the hosted zone within 60 seconds".

### F3. How the SDK waiter spaces its checks

`ResourceRecordSetsChangedWaiter` has a `MinDelay` of 30 s and a `MaxDelay` of 120 s
(`aws-sdk-go-v2/service/route53 api_op_GetChange.go:167-168`).

* Its loop (`:216-269`) checks once immediately. It sleeps `smithywaiter.ComputeDelay`'s delay
  between checks, and stops when the time left is less than `MinDelay` (`:251`).
* `ComputeDelay` (`smithy-go waiter/waiter.go`) starts at `MinDelay` and doubles with jitter.
  It shortens the last sleep so that `MinDelay` is left (`remainingTime-delay <= minDelay`).
* So with `Route53MaxWait` at its 1-minute default, `GetChange` is called at about 0 s and 30 s,
  and the wait fails then. A change that takes Route 53 its documented 60 seconds would fail at
  the default.
* At 2 minutes, `GetChange` is called at about 0 s and 30 s, then between 60 s and 90 s, then
  about 90 s. The wait returns no later than about 90 s, `INSYNC` or not.

When the wait ends without `INSYNC`, `Present` returns the waiter's error ("exceeded max wait
time for ResourceRecordSetsChanged waiter").

### F4. Every failed start places one production order

* **acmez publishes, waits, and only then asks the CA.** In mholt/acmez/v3 v3.1.7, `client.go`
  calls the solver's `Present` (`:424`), then its `Wait` (`:453`), and only then
  `InitiateChallenge` (`:486`). A failure or a cancelled context during `Present` or `Wait`
  ends the attempt before Let's Encrypt is asked to validate, so it costs no failed validation.
* **`ManageSync` makes one attempt.** It calls `ObtainCertSync`, which is `obtainCert(…,
  interactive=true)` (`config.go:615-616`), and the interactive path calls the obtain function
  once with no retry (`config.go:822-827`). `doWithRetry` and its backoff serve only the
  asynchronous path.
* **Only an empty storage orders.** `manageOne` loads a certificate from storage first and obtains
  only when there is none (`config.go:496-500`). An existing one is renewed if it needs it.
* **The relay exits on failure.** `mcrelay` returns `EnsureACME`'s error from `applyLetsEncrypt`
  (`internal/relay/tls.go:96-100`) and exits.
  * Its systemd unit has `Restart=always`, `RestartSec=5`, `StartLimitIntervalSec=300` and
    `StartLimitBurst=30` (`mcrelay.user.service.tmpl:14-15,31-32`). A failure that takes about
    eight seconds is a cycle of about 13 s, about 23 starts per 300 s, so the start limit is never
    reached.
  * Its launchd plist has `KeepAlive` and `ThrottleInterval` 2 (`plist_render.go:170-171`).
* **The daemon falls back.** `mcremote` logs the failure, falls back to a self-signed identity and
  keeps running (`internal/daemon/certs.go:111-123`). It orders again only on its next start.
* **Let's Encrypt's limit:** "Up to 5 authorization failures per identifier can be incurred by one
  account every hour", refilling "at a rate of 1 per identifier every 12 minutes". A separate
  limit allows up to 1,152 consecutive authorization failures per identifier and account, after
  which issuance for the name is paused. (Let's Encrypt, "Rate Limits", fetched 2026-11-05.)
* **Result:** a relay that keeps failing validation spends the hourly allowance in about its
  first minute. After that, its orders are refused for the rest of each hour.

### F5. The time bounds

| Bound | Value | Where | What it limits |
| :--- | :--- | :--- | :--- |
| `ACMEStartupTimeout` | 3 min | `internal/certs/acme.go:30` | the context of the first `ManageSync`, for both daemons |
| `TimeoutStartSec` | 240 s | both unit templates; `docs/guides/config.md:839` | systemd's wait for `READY=1`. 0207-MADR D22, as added 2026-10-26, set it above `ACMEStartupTimeout`. `TestUnitIsTypeNotify` (`internal/cli/service/notify_unit_test.go:47`) fails if it is not longer |
| `acmePropagationTimeout` | 2 min | `acme.go:37` | certmagic's authoritative-nameserver poll |
| `acmeChallengeTTL` | 5 min | `acme.go:33` | the TXT record's TTL |
| `Route53MaxWait` | 1 min (the library's default; not set) | libdns/route53 `client.go:146-147` | the `INSYNC` wait, and only when `WaitForRoute53Sync` is true |

Neither caller sets `ACMEOptions.Timeout`, so both use `ACMEStartupTimeout`. The daemon does not
listen until `EnsureTLS` returns, so a longer bound is a longer start before even the self-signed
fallback.

### F6. The tests today

* `TestEnsureACMEValidation` checks option validation.
* `TestEnsureACMEManageFailsCleanly` (`acme_test.go:77-93`) runs one real failing `ManageSync`
  against the staging directory, with a 1.5 s timeout.
* `internal/relay/tls_test.go:91-99` notes that `ApplyTLS` cannot be tested end to end, because
  the bound is not injectable.

Nothing asserts the solver's fields, because `EnsureACME` does not expose the solver.

## Decision Drivers

* A production DNS-01 order must not be submitted to Let's Encrypt while any authoritative
  Route 53 server for the zone lacks the challenge record (issue #35's acceptance).
* Prefer the authority's own completion signal over a fixed guess. Waiting less than Route 53
  needs fails, and waiting more than it needs slows every issuance and renewal.
* A wait that runs out must end before the challenge is submitted, so it costs no failed
  validation (F4).
* A configuration that keeps failing validation must not use up the name's Let's Encrypt
  allowance, under any of the service managers mcrelay installs under.
* Keep the 3-minute start bound and the 240 s unit timeout, unless the wait cannot fit inside
  them. Moving them means changing both templates, `TestUnitIsTypeNotify`, the guide, and every
  installed unit (F5).
* No new configuration key. The timing is Route 53's and Let's Encrypt's, not an operator's
  choice.
* Locked by unit tests that need no network.

## Considered Options

For the propagation wait (F1 to F3):

* **A1 — Wait for `INSYNC`** (`WaitForRoute53Sync`), with a `Route53MaxWait` that covers Route
  53's 60 seconds, and keep certmagic's authoritative check as confirmation.
* **A2 — A fixed 60 s `PropagationDelay`**, and `ACMEStartupTimeout` raised to 5 minutes. This is
  the issue's prescription.
* **A3 — Both** `INSYNC` and a fixed delay.
* **A4 — Point certmagic at recursive resolvers** (`DNSManager.Resolvers`) instead of the
  authoritative servers.
* **A5 — Patch or replace certmagic**, so that its authoritative check requires every
  nameserver.

For a validation that keeps failing (F4):

* **B1 — A cooldown recorded in ACME storage.** After a validation failure, `internal/certs`
  refuses to place another order for the same names and directory until the allowance has
  refilled by one.
* **B2 — Pace restarts in the service units**, for example systemd's `RestartSteps` and
  `RestartMaxDelaySec`.
* **B3 — Keep the relay running** on a self-signed certificate, and issue in the background
  through certmagic's asynchronous retry.
* **B4 — Change nothing**, and document that the unit must be stopped by hand.

## Decision Outcome

Chosen options: **A1**, because `INSYNC` is AWS's own statement that every Route 53 server has
the record, the guide already promises it, and its failure lands before the challenge is
submitted. And **B1**, because it bounds the quota whatever restarts the process, and needs no
change to any service manager.

### The decisions (accepted 2026-11-05)

*Accepted by the owner on 2026-11-05, answering the PLAN's four questions at approval: "D1 insync alone D2 leave HTTP-01 alone it works fine. D3 we will complete the plan D4 you will, you have ssh access to it from here, it is <relay-host>".* The
owner's numbers are those questions', not this record's decisions:

* **Question 1:** `INSYNC` alone. D1 stands, and A3 is not chosen.
* **Question 2:** the cooldown covers DNS-01 only. `EnsureACMEHTTP` is not changed. D4 is narrowed
  to `EnsureACME`.
* **Question 3:** the plan is completed as a whole. P1 is not released on its own.
* **Question 4:** the agent runs the live acceptance on the relay host over SSH.

* **D1 — The Route 53 provider waits for `INSYNC` (F2, F3).** `EnsureACME`'s `route53.Provider`
  sets:
  * `WaitForRoute53Sync: true`;
  * `Route53MaxWait: 2 * time.Minute`, a new unexported constant `acmeRoute53SyncWait`. That
    gives a `GetChange` check between 60 s and 90 s, after Route 53's documented 60 seconds, and
    a final one at about 90 s (F3). The library's 1-minute default stops checking at about 30 s;
  * `SkipRoute53SyncOnDelete: true`. Removing the record after validation does not need to wait.

  `PropagationDelay` stays zero (A2 and A3 are not chosen). certmagic's authoritative poll stays
  as it is, and after `INSYNC` its first match is safe, because every Route 53 server has the
  record.
* **D2 — The bounds fit inside the 3 minutes, and the unit timeout stays (F5).**
  * `acmePropagationTimeout` goes from 2 minutes to **30 seconds**. After `INSYNC`, the
    authoritative poll confirms and does not wait. 30 s is 15 polls.
  * `ACMEStartupTimeout` stays **3 minutes**, and `TimeoutStartSec` stays **240**. Their
    relationship, and `TestUnitIsTypeNotify`, are unchanged.
  * The nominal sum fits: `acmeRoute53SyncWait` (2 min) + `acmePropagationTimeout` (30 s) + a
    30 s allowance for the CA's validation, finalisation and download, which is 3 minutes. The
    `INSYNC` wait really returns by about 90 s (F3), so the worst case leaves about 30 s spare.
  * A run that reaches the outer bound inside `Present` or `Wait` is cancelled before the
    challenge is submitted (F4), so it costs no failed validation.
* **D3 — The solver is built by one function that tests can call (F6).** An unexported
  `newDNS01Solver(opts Route53Options, logger *zap.Logger, providerLog *slog.Logger)` returns the
  `*certmagic.DNS01Solver`, and `EnsureACME` calls it. Tests assert every timing field, and the
  budget of D2.
  * When `ACMEOptions.Verbose` is set, the provider's `Logger` writes its debug events to stderr.
    That includes "waiting for Route53 sync" and "Route53 sync complete" with the change id.
  * Otherwise it is nil, which the library discards. The live acceptance reads those lines.
* **D4 — After a failed validation, no new order for 12 minutes (F4; B1).**
  * **What records a failure.** A first-issuance `ManageSync` on the DNS-01 path (`EnsureACME`;
    the owner: "leave HTTP-01 alone it works fine") whose error wraps an `acme.Problem` of these types records one:
    `unauthorized`, `dns`, `caa`, `connection`, `incorrectResponse`, `tls`, `compound` or
    `rateLimited`. Those types are the validation failures Let's Encrypt counts, `compound`
    being how several of them arrive together, plus its refusal for the limit itself. The record is a small JSON file in the ACME storage directory, keyed by the
    directory URL and the sorted names, holding the time and the problem type.
  * **What it does not record.** A context deadline, a network error before the order, and any
    other problem type, including `serverInternal`, record nothing. They keep today's
    behaviour.
  * **What a fresh record does.** If a matching record is younger than **12 minutes**, Let's
    Encrypt's refill interval, and storage has no certificate for one of the names,
    `EnsureACME` returns an error at once without ordering. The error names the problem, the
    time, and when an order will next be placed.
  * **What it never blocks.** A name whose certificate is already in storage still loads and
    renews. The check uses certmagic's `CacheManagedCertificate`, the same load `manageOne`
    does first (`config.go:496`).
  * **What clears it.** A successful issuance deletes the record. A record dated in the future,
    because the clock moved, is deleted and ignored.
  * **The effect.** At most one failed validation per 12 minutes, which is the rate Let's
    Encrypt refills at, so the 5-per-hour limit is never reached by a loop.
  * **How each daemon behaves.** `mcrelay` still exits. Under systemd its refusals are fast, so
    30 starts fall inside 300 s and the unit stops as `failed (start-limit-hit)`, which needs a
    `systemctl --user reset-failed` before the next start. `mcremote` falls back to self-signed
    at once, instead of after the order.
* **D5 — Docs say what the code does.**
  * `docs/guides/iam-route53-acme.md`: the `PollChangePropagation` row stays, and is now true. A
    paragraph gives the timing of D1 and D2. The troubleshooting table gains rows for the
    cooldown refusal, the waiter's "exceeded max wait time", and a relay unit left
    `start-limit-hit`.
  * `docs/guides/config.md:839` is unchanged, because `TimeoutStartSec` stays 240.

### Consequences

* Good, because a challenge is submitted only after AWS reports the record on every Route 53
  server. That closes the race in issue #35 for first issuance and for renewal, which use the
  same solver.
* Good, because a normal issuance waits only as long as Route 53 needs. The first `GetChange`
  check is immediate and the second at 30 s, where A2 would add a fixed 60 s every time.
* Good, because each new wait ends before the challenge is submitted, so running out of time costs
  no failed validation.
* Good, because the unit timeout, its test and the installed units are untouched.
* Good, because the cooldown bounds Let's Encrypt usage under systemd, launchd and the Windows
  service manager alike.
* Neutral, because `route53:GetChange` is now really called. The guide's policy has always
  granted it, but a role written without it now fails `Present` with `AccessDenied`, before any
  validation. That costs no allowance, and the guide's troubleshooting row already names it.
* Neutral, because Let's Encrypt's consecutive-failure limit is still reachable. At one failure
  per 12 minutes, its 1,152 would take about 9.6 days of continuous failure.
* Bad, because the relay can now stop as `start-limit-hit` after a validation failure, where
  before it looped. The loop was spending quota, and the failure needs a human to fix it. But
  even a transient validation failure now needs a manual `reset-failed` under systemd.
* Bad, because `internal/certs` adds a small record of its own beside certmagic's storage, and a
  direct dependency on `github.com/mholt/acmez/v3/acme` for the problem type. That package is
  already in the build through certmagic, so `go.mod` only moves it from indirect to direct.
* Neutral, because an HTTP-01 relay that keeps failing validation still spends the allowance as
  F4 describes. The owner left HTTP-01 unchanged.
* Bad, because `INSYNC` is Route 53's notion of done. A different DNS provider would need its own
  signal. This repository supports only Route 53 for DNS-01.

### Confirmation

* **Unit tests, none of which use the network, each seen failing on a scratch copy:**
  * `newDNS01Solver`'s fields: `WaitForRoute53Sync`, `Route53MaxWait == 2m`,
    `SkipRoute53SyncOnDelete`, `PropagationDelay == 0`, `PropagationTimeout == 30s` and
    `TTL == 5m`;
  * the D2 budget: `acmeRoute53SyncWait + acmePropagationTimeout + acmeCABudget <=
    ACMEStartupTimeout`;
  * the cooldown's record, match, expiry, clearing and future-date rules;
  * `EnsureACME` refusing without a network call while a fresh record stands and storage is empty.
* `TestUnitIsTypeNotify` passes unchanged.
* **A live acceptance on the relay host:**
  * a staging issuance into an empty storage directory. Issue #35 found that a staging
    certificate already in storage hides the order;
  * at debug level, the log shows "waiting for Route53 sync" and then "Route53 sync complete"
    before the order's validation;
  * then one production issuance, with `listening` and a certificate from the production
    directory;
  * run by the owner or with the owner's approval, and recorded with the host and name as
    placeholders.

## Pros and Cons of the Options

### A1 — Wait for `INSYNC` (chosen)

* Good, because it waits for the condition itself, "propagated to all Route 53 DNS servers
  managing the hosted zone", rather than a guess at its duration.
* Good, because the library and the SDK already implement it, so it is three fields.
* Good, because the repository's own guide already describes this behaviour, and its policy
  already grants `GetChange`.
* Neutral, because the waiter checks at 30 s steps (F3), so a change that is ready in 5 s still
  takes about 30 s to be seen. That is the same order of cost as A2's, without A2's fixed floor.
* Bad, because it depends on `INSYNC` being accurate. If live evidence ever shows a Route 53 server
  without the record after `INSYNC`, A3's delay is the fallback.

### A2 — Fixed 60 s `PropagationDelay` and a 5-minute bound

* Good, because it is one field, and the issue's prescription.
* Bad, because 60 s is Route 53's "generally", not a guarantee. A slower change still races.
* Bad, because it adds 60 s to every issuance and renewal, however fast Route 53 is.
* Bad, because its budget (60 s + the 2-minute poll + the CA) does not fit in 3 minutes. The
  bound would go to 5 minutes, which needs `TimeoutStartSec` above 300 in both templates,
  `TestUnitIsTypeNotify`'s and the refresh test's `TimeoutStartSec=240` literals changed
  (`notify_unit_test.go:37,97`), `config.md:839` changed, and every installed unit refreshed.
  It also makes `mcremote` wait up to 5 minutes before even its self-signed fallback listens.

### A3 — Both `INSYNC` and a fixed delay

* Good, because it covers an `INSYNC` that turned out to be inaccurate.
* Bad, because there is no evidence that it is. It pays A2's fixed cost and A2's bound changes for
  a case not seen.

### A4 — Recursive resolvers

* Bad, because a recursive resolver answering does not mean every authoritative server has the
  record, which is what Let's Encrypt checks. It would also cache the NXDOMAIN it saw before the
  record was published. The issue notes the zone's negative-cache TTL is 86400.

### A5 — Patch or replace certmagic

* Good, because it would make certmagic's check what its comment says it is.
* Bad, because it forks or replaces a library that is otherwise correct here, for a gap that A1
  makes harmless for Route 53. An upstream fix would still be compatible with A1.

### B1 — A cooldown in ACME storage (chosen)

* Good, because it works the same whatever restarts the process: systemd, launchd or Windows.
* Good, because it needs no network to test, and leaves a stored certificate's load and renewal
  alone.
* Bad, because it is new state, beside certmagic's, that a reader must know exists. The error it
  returns names the file and the time.
* Bad, because under systemd the fast refusals now reach the unit's start limit (D4), so the relay
  stays down until reset.

### B2 — Pace restarts in the unit

* Good, because it is configuration, not code.
* Bad, because `RestartSteps` and `RestartMaxDelaySec` need systemd 254 or later, launchd's
  `ThrottleInterval` has no backoff, and the Windows service manager has its own policy. Each
  needs its own answer, and none knows whether the last exit spent quota.

### B3 — Keep the relay running and issue in the background

* Good, because the relay stays up.
* Bad, because it serves a certificate phones do not trust, which changes the relay's contract.
  certmagic's asynchronous backoff also starts at 1, 2, 2, 5 and 10 minutes
  (`async.go:260-285`), which still allows 5 failures in about 20 minutes.

### B4 — Change nothing

* Good, because it costs nothing.
* Bad, because any validation failure, including a misconfiguration after A1, spends the name's
  allowance until someone notices.

## More Information

### What is out of scope

As issue #35 says:

* the hosted zone's records;
* the zone's SOA minimum (negative-cache TTL), which is an operator change in the Route 53 console;
* replacing certmagic.

Also:

* the HTTP-01 path (`acme_http.go`), by the owner's decision: it has no DNS step, and it works.
  D4's cooldown does not apply to it, so an HTTP-01 relay that keeps failing validation still
  loops as F4 describes;
* a configuration key for any of these times;
* renewal's own retry. certmagic's maintenance renews asynchronously with backoff. D1 changes its
  solver, and D4 does not gate it, because a certificate in storage is never blocked.

### The relay host, probed 2026-11-05 (read only, over SSH)

* Ubuntu on AWS's kernel, `systemd 259`, so the unit facts of F4 hold there.
* `mcrelay 0.20.1 (a078372b)`, built 2026-09-30, at `~/.local/bin/mcrelay`.
* `systemctl --user` reports the unit `inactive`, `NRestarts=0`, and an `ActiveEnterTimestamp`
  of the start issue #35 recorded. It was stopped by hand, so nothing is ordering now.
* The config: `tls.mode: letsencrypt`, `challenge: dns-01`, `staging: false`, a hosted zone id and
  region set, `cache_dir` empty (the default, `<data_dir>/acme`, exists), `listen.port: 8443`,
  and `log.level: info`. Only key names and these non-identifying values were read.
* `aws route53 get-change` with a made-up id answers `NoSuchChange`, not `AccessDenied`. The
  instance role may call `GetChange`, which D1 needs.
* The host's Go is 1.26.5, older than `go.mod`'s `go 1.27.3`, so the acceptance binary is
  cross-built on the development Mac.

### Evidence index

* Issue #35, opened 2026-11-05: the live sequence, the nameserver split and the CA's errors.
* certmagic v0.25.6 (module cache):
  * `solvers.go:256-258,419-482`;
  * `certificates.go:257` (`CacheManagedCertificate`);
  * `dnsutil.go:265-306`;
  * `config.go:486-560,615-627,822-827`;
  * `acmeissuer.go:418-440`;
  * `async.go:180-290`.
* libdns/route53 v1.6.2: `provider.go:44-56`, `client.go:140-150,400-437`, `BREAKING.md`.
* aws-sdk-go-v2/service/route53 v1.70.6: `api_op_GetChange.go:160-270`. smithy-go v1.28.7:
  `waiter/waiter.go` `ComputeDelay`.
* mholt/acmez/v3 v3.1.7: `client.go:424,453,486`, `acme/problem.go:140-160`.
* AWS Route 53 API Reference, GetChange and ChangeResourceRecordSets. The Let's Encrypt "Rate
  Limits" page. All fetched 2026-11-05.
* This repository at `a115e40e`:
  * `internal/certs/acme.go`;
  * `internal/relay/tls.go:40-120`;
  * `internal/daemon/certs.go:80-125`;
  * both unit templates;
  * `internal/cli/service/plist_render.go:170-171`;
  * `internal/cli/service/notify_unit_test.go:27-53,85-100`;
  * `docs/guides/iam-route53-acme.md`;
  * `docs/guides/config.md:839`.

### Related records

* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md)
  D22 and its addition of 2026-10-26: `TimeoutStartSec=240` above `ACMEStartupTimeout`, which D2
  keeps.
* [0004-MADR-certificate-management.md](0004-MADR-certificate-management.md): the certificate
  modes this changes nothing about.
* The plan: [0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md](0231-PLAN-wait-for-route-53-insync-before-dns-01-and-pace-failed-orders.md).
