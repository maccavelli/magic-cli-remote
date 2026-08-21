# Operator guide — provider credential backup and recovery

<!-- markdownlint-disable MD013 -->

Codex and Grok logins driven from the phone run inside a **credential
transaction** ([MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md),
decisions D20–D29). This document is the operator's view: what the states mean,
and what to do when one needs a decision.

## Why this exists

`codex login --device-auth` revokes the stored refresh token server-side and
then deletes `auth.json`, before the user has entered anything. mcremote used to
start that command against the live credential and keep the only copy in daemon
memory. Several ordinary paths — a dropped socket, an admission rejection, a
daemon restart — discarded that copy while the credential was already gone, and
the operator had to sign in again.

Logins now run against a **private, empty** `CODEX_HOME` or `GROK_HOME`. The
child finds nothing to delete and nothing to revoke, and the live credential is
not read, copied, or replaced until a validated candidate is published
atomically. Failure is no longer loss.

## What is stored

Under `<data_dir>/provider-auth/<provider>/`, mode `0700`:

| Item | Meaning |
| --- | --- |
| `manifest.json` | Labels, fingerprints, and transaction state. Contains **no** token, device code, or credential path. |
| `generations/*.auth` | Immutable `0600` copies: the current credential and the one before it. Exactly two are retained. |
| `pending/<id>/home/` | An isolated home for one in-flight login. Always starts empty. |

These files are a recovery aid, never an authentication source. They never leave
the host.

## Backup states

Read them with:

```bash
mcremote auth-recovery status          # every provider
mcremote auth-recovery status codex    # one provider
```

| State | Meaning | Action |
| --- | --- | --- |
| `unmanaged` | No credential is under management (a cold host). | Sign in normally. |
| `current` | A validated generation matches the live credential. | Nothing. This is healthy. |
| `pending` | A login is in flight. | Wait, or cancel from the phone. |
| `logged_out` | An explicit logout removed the credential. | Sign in when you want access back. |
| `recovery_required` | Durable evidence is ambiguous. Every file is preserved. | Choose an outcome (below). |
| `reauth_required` | The only surviving generations were revoked by a logout. No restore can work. | Sign in again. |
| `unsupported` | The provider stores credentials somewhere mcremote cannot protect. | See "Unsupported backends". |

## Resolving `recovery_required`

The daemon never guesses. It reaches this state when the credential on disk is
valid but is neither the one it published nor a provably newer refresh — for
example, someone ran `codex login` directly while a transaction was open.

```bash
mcremote auth-recovery choose <provider> <live|current|previous|logged-out>
```

| Choice | Effect |
| --- | --- |
| `live` | Validate and adopt whatever is on disk now as the new current generation. |
| `current` | Republish the retained current generation over the live file. |
| `previous` | Promote the prior generation, retaining the displaced one as previous. |
| `logged-out` | Record the tombstone, then remove the live credential and all retained generations. |

Exit codes: `0` success, `2` usage or unknown provider/choice, `3` validation,
locking, or recovery failure. A failed choice changes nothing and leaves the
state exactly as it was.

Output contains provider, state, and timestamps only — never a path,
fingerprint, generation id, or credential.

## Unsupported backends

Codex can be configured to keep credentials outside `auth.json`:

```toml
# ~/.codex/config.toml
cli_auth_credentials_store = "keyring"   # or "auto", "ephemeral"
```

mcremote cannot observe, snapshot, lock, or restore those, so it refuses the
transactional path rather than claiming a protection that does not exist.
Mutation attempts return a typed unsupported-backend result; status stays
readable. The default, `file`, is fully supported.

## Watchers on Linux

The daemon watches each credential directory to notice an autonomous token
refresh. On Linux that is inotify, whose `max_user_instances` and
`max_user_watches` limits a container can exhaust; some filesystems report no
events at all. A watcher that cannot start is logged and skipped.

This costs latency, not correctness: startup and pre-mutation reconciliation are
the mandatory path, so a refresh is always checkpointed by the next daemon start
or the next login at the latest.

## What is never done automatically

* An externally deleted credential is never resurrected.
* A generation revoked by a logout is never offered for recovery.
* An older credential never overwrites a newer one.
* A rollback never happens after a publication has been verified.
