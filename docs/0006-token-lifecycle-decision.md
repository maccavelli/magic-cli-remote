# 0006 — Token lifecycle

* Status: **Accepted**
* Date: 2026-07-20
* Relates to: [0005-client-identity-decision.md](0005-client-identity-decision.md),
  hardening plan §5.1

## Context

Hardening plan §5.1 asked whether the device token needs expiry, rotation, or
per-token scope. Grounding the question in the code:

* Tokens are 256-bit `crypto/rand`, hex, `mcr_`-prefixed
  (`internal/auth/token.go:13-19`) — ample entropy.
* Stored as SHA-256 at rest (`token.go:22-25`).
* `deviceRecord` carries `CreatedAt`/`LastUsedAt` but **no `ExpiresAt` and no
  scope** (`internal/auth/store.go`).
* `LastUsedAt` is written on every `Validate` and shown by `pair list`, but
  nothing acts on it — there is no reap.

Phase 3 (ADR 0005) then bound each device to a client key, enforced by default
(D7). On the RCE surface a stolen token *alone* is now inert: `authenticate`
runs `Validate` then `verifyClientKey`, and a device with no matching key is
rejected (`internal/ws/server.go`).

## Decision

**Close §5.1. The token's cryptography is already sound and Phase 3 subsumes the
threats that expiry/rotation/scope would address. Do not build lifecycle
machinery. Add one operator ergonomic (`prune`) and one completeness fix.**

The model is SSH `authorized_keys` (ADR 0005): lifecycle is "remove the entry,"
not "expire the credential."

### Built

1. **`Store.Prune(staleBefore, keylessOnly)`** (`internal/auth/store.go`) and
   **`mcremote pair prune`** (`--stale <dur>` / `--keyless`). Acts on the
   already-recorded `LastUsedAt`, and drops the legacy keyless record a re-pair
   leaves behind under D7 enforcement. This is the idiomatic reap for a
   file-backed `devices.json`.

2. **`/v1/hello` now enforces the client key too** (`authorizeHTTP`,
   `internal/ws/server.go`). This was a genuine residual surfaced during
   analysis: the endpoint previously validated the token only, so a stolen
   token alone still leaked the version, listen address, and **Headscale
   control URL**. It now requires the presented client cert to match the
   enrolled key, making "a stolen token alone is useless" true uniformly —
   scoped as Phase 3 enforcement-completeness, closed here because it is one
   line and reuses the same SPKI check.

### Explicitly rejected, with reason

* **Token expiry (mandatory or default).** Manufactures forced re-pairs with no
  threat reduction — the key binding already neutralises a leaked token — and
  breaks the authorized-keys posture. An *opt-in* `--ttl` was considered and
  skipped: no current need on a single-phone fleet; add it only if a temporary
  loaner device ever becomes real.
* **Rotation flow.** Defends against silent bearer-token leak, a threat Phase 3
  already answers. No code to justify.
* **Per-token scopes/capabilities.** RBAC for multi-tenant services. This is one
  operator with one capability.
* **bcrypt/argon2 at rest.** Those stretch *low-entropy passwords*. A 256-bit
  uniform-random token is not brute-forceable; a fast hash is the correct tool.
  Switching adds latency for nothing.
* **JWT/PASETO.** Replaces an opaque hashed random — whose store is already the
  source of truth and whose revocation is a file delete — with a heavier
  self-describing format. Strictly more machinery, no gain.

## Consequences

The token remains the non-secret half of a two-factor credential. Operator
hygiene for stale/legacy records now exists. The one recon endpoint that still
honoured a bare token no longer does. No new failure modes; `pair prune`
requires an explicit `--stale` or `--keyless` so it cannot wipe records by
accident.
