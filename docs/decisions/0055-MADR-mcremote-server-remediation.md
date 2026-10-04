---
status: accepted
date: 2026-10-03
decision-makers: Project Owner
---
<!-- markdownlint-disable MD013 MD041 -->

# Remediate the mcremote Go server from the 2026-07 audit

> **2026-10-03 (0180 P1).** Thin MADR written so PLAN 0055 has a same-number decision. The locked choices and implementation live in [0055-PLAN-mcremote-server-remediation.md](0055-PLAN-mcremote-server-remediation.md) (phases 0–5 landed 2026-07-21). This record does not re-open those phases.

## Context and Problem Statement

A deep-dive audit of the mcremote Go server (bugs, gaps, wiring, hardening, concurrency) produced an ordered remediation plan at number 0055 with no MADR. The pairing rule requires a same-number MADR. The work is already in the tree; this record states the decisions the PLAN locked.

Companions: [0004-MADR-certificate-management.md](0004-MADR-certificate-management.md) / [0004-PLAN-hardening-implementation.md](0004-PLAN-hardening-implementation.md) (front door, TLS, identity — complete), [0009-MADR-post-hardening-action-plan.md](0009-MADR-post-hardening-action-plan.md) (what remains), [0012-MADR-mcremote-daemon-assessment-action-plan.md](0012-MADR-mcremote-daemon-assessment-action-plan.md) (post-audit phases 0–4).

## Decision Drivers

* Live WebSocket clients must drop when `pair revoke` / `prune` mutates the store.
* A disconnected provider session must not stay promptable.
* `session.create` with a live id must not leak the old session.
* Multi-device isolation belongs in this remediation, not a later pair.
* Tests enable `providers.fake`; the default build does not.

## Considered Options

* Record the PLAN's locked rows (R1–R6) as this MADR and leave number 0055.
* Rename the PLAN under 0009 or 0012 and skip a 0055 MADR.
* Leave 0055 as a lone PLAN.

## Decision Outcome

Chosen option: "Record the PLAN's locked rows (R1–R6) as this MADR and leave number 0055", because the owner kept 0055 and asked for a thin MADR (0180, 2026-10-03).

* **R1** — `pair revoke` / `prune` kicks live sockets through a Unix admin socket under `data_dir` (`admin.sock`, 0600).
* **R2** — Provider process exit auto-closes: drop the map entry, persist `disconnected`, drop history; no commandable tombstone.
* **R3** — `session.create` with an existing live `session_id` fully closes the prior session, then starts and registers the new one; the result is live. Never overwrite the map without that close.
* **R4** — Sessions are owned by `device_id`; list, broadcast, and mutating ops filter to that owner. Empty legacy owners remain operable until stamped.
* **R5** — Never drop control events; disconnect a client that still cannot accept them.
* **R6** — `providers.fake.enabled` defaults to `false`.

Executed by [0055-PLAN-mcremote-server-remediation.md](0055-PLAN-mcremote-server-remediation.md).

### Consequences

* Good, because 0055 is a real pair and `make check-records` no longer warns about a lone PLAN at this number.
* Neutral, because the code already matches R1–R6; this file is the missing decision record.
* Bad, because a thin MADR written years after the PLAN cannot reconstruct every discarded option from the audit.

## Pros and Cons of the Options

* **Thin 0055 MADR (chosen).**
  * Good, because the number stays stable for existing citations.
  * Bad, because the decision is documented after the fact.
* **Rename under 0009 or 0012.**
  * Good, because no new MADR.
  * Bad, because the owner rejected that fork.
* **Leave lone PLAN.**
  * Good, because zero new prose.
  * Bad, because the checker warning never clears.

## More Information

* PLAN status: phases 0–5 implemented 2026-07-21; phase 6 product follow-ons live in 0009.
