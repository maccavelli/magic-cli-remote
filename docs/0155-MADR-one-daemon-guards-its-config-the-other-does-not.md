---
status: accepted
date: 2026-09-12
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The same shared secret is permission-guarded in mcrelay's config and unguarded in mcremote's

## Context and Problem Statement

`mcrelay` refuses to read a config file that anyone but its owner can read. It
calls `appdirs.FileIsOwnerOnly` at both config-read sites and again on TLS key
files, and a failure is fatal.

`mcremote` performs no permission check on its config at all — and its config
can hold `relay.secret`, which is the *same shared secret* mcrelay stores as
`hosts[].secret`. One end of a credential pair is guarded; the other is not.

This came out of executing PLAN 0154, where a test fixture kept being refused
by mcrelay and the reason turned out to be worth its own record.

### What was measured, not assumed

All on `b383dd6`, Windows 11 Home 10.0.26200, and by reading the source at that
commit.

**Every owner-only gate in the repository.** `grep -rn 'FileIsOwnerOnly'`,
excluding tests and the two implementations:

```text
internal/providerauth/store.go:136   credential candidate, before adopting it
internal/relay/fileconfig.go:288     explicit --config file
internal/relay/fileconfig.go:305     discovered config file
internal/relay/fileconfig.go:861     TLS cert/key in files mode
```

Four production gates. All four are mcrelay or credential storage. `grep` over
`internal/config/` — mcremote's entire configuration package — returns no
permission check of any kind: no `FileIsOwnerOnly`, no `Mode().Perm()`, no
`Stat` before `ReadInConfig`.

**mcremote's config can carry a secret, and the code says so.**
`internal/config/config.go:96-101`:

```go
// HostID is the public registration id (pair URI hid=). URL-safe token.
HostID string `mapstructure:"host_id"`
// Secret is the registration secret shared with mcrelay --allow / hosts.
// Prefer env MCREMOTE_RELAY_SECRET over committing to YAML.
Secret string `mapstructure:"secret"`
```

It is the only secret-bearing field in mcremote's config — a full scan for
`Secret|Token|Password|APIKey` mapstructure tags returns this one and
`require_device_token`, which is a bool. So the exposure is narrow and
specific, not diffuse.

The "prefer env" comment is the point: the field is documented as sensitive by
the same file that defines it, and `MCREMOTE_RELAY_SECRET` and
`--relay-secret` both exist as alternatives (`internal/cli/serve.go:126`).
Nothing prevents or detects putting it in YAML.

**mcremote guards its data directory but not its config directory.**

| Path | How it is created | Private? |
| --- | --- | --- |
| data dir | `appdirs.EnsurePrivateDir` (`internal/daemon/daemon.go:86`) | yes, on both platforms |
| config dir | `os.MkdirAll(dir, 0o700)` (`internal/cli/service/setup.go:1080`) | POSIX only |
| config file | `tmp.Chmod(0o600)` before rename (`setup.go:~1114`) | POSIX only |

`EnsurePrivateDir` exists, is used ten lines into daemon startup for the data
directory, and is not used for the configuration directory. Its doc comment
states why the difference matters: *"The 0700 passed to MkdirAll is ignored by
the platform"* on Windows.

**What "owner-only" means on each platform.** Two implementations of one
question (MADR 0116 D22):

* POSIX (`owneronly_unix.go`): `fi.Mode().Perm()&0o077 == 0`.
* Windows (`security_windows.go:290`): the file's owner SID must be the current
  user, and `noForeignTrustee` must hold — the DACL may name only the owner,
  `SY` (SYSTEM) and `BA` (Administrators). No DACL at all counts as "everyone",
  not "nobody".

**A measured correction to an earlier claim.** While executing 0154 this author
reported that mcrelay "refuses a `--config` file in most Windows locations,
making `--config` unusable there". That was wrong, and the measurement that
disproves it is worth recording because it is the same error this codebase
keeps finding in its own tests — generalising from one host.

Four locations, config written identically to each:

| Location | Result | Trustees on the file |
| --- | --- | --- |
| `%TEMP%` subdirectory | refused | `CodexSandboxUsers`, SYSTEM, Administrators, owner |
| `Documents` | refused | `CodexSandboxUsers`, SYSTEM, Administrators, owner |
| repository checkout | refused | (same shape) |
| user home subdirectory | **accepted** | SYSTEM, Administrators, owner |

Every refusal was caused by `MAC420\CodexSandboxUsers`, a group that exists on
this machine and is inherited into those paths. The home directory, lacking it,
is accepted. **The check is correct and was doing its job**; on a Windows
install without such a group the standard locations would pass.

**The refusal names a remedy that does not exist on Windows.**

```console
error: config …\c.yaml is readable by group/other; chmod 0600
```

There is no `chmod` on Windows, the file's mode bits are not what was tested,
and the message does not name the trustee that actually caused the refusal.
Diagnosing today's instance required reading the ACL by hand.

**`EnsurePrivateDir` converges a directory *and its existing files*.** Measured,
because it decides the whole migration question:

```console
$ # config in a %TEMP% dir carrying the foreign group
$ mcrelay paths --config …\legacy\c.yaml     → exit 1   (refused)
$ EnsurePrivateDir("…\legacy")               → converged
$ mcrelay paths --config …\legacy\c.yaml     → exit 0   (accepted)
$ Get-Acl …\legacy\c.yaml                    → OWNER RIGHTS, NT AUTHORITY\SYSTEM
```

Severing inheritance and installing a private DACL on the directory
re-propagated to the file that was already inside it. The resulting DACL is
`OWNER RIGHTS` + `SYSTEM`, inheritance protected.

**On this host, the default mcremote config location would fail a check
today.** `%AppData%\Roaming` carries `CodexSandboxUsers`, so a config created
there by `setup-service` inherits it. This is not hypothetical migration risk;
it is the state of the machine the record is being written on.

### Findings

**F1 — mcremote applies no permission check to a config that may hold a shared
secret.** Four owner-only gates exist in the repository and none is on
mcremote's config-read path.

**F2 — the unguarded secret is the guarded one.** `relay.secret` in mcremote's
config and `hosts[].secret` in mcrelay's are two copies of the same
registration credential. Reading either one lets an attacker register as the
host against the relay. Guarding one copy and not the other buys nothing
against an attacker who can read files as another user on the mcremote host.

**F3 — the codebase's own pattern is "secrets require owner-only".** Credential
candidates in `providerauth`, mcrelay's config, mcrelay's TLS key. mcremote's
config is the single exception, and mcremote's *data* directory follows the
pattern correctly ten lines into daemon startup — which reads as an omission
rather than a decision.

**F4 — mcremote's config directory is private on POSIX and not on Windows.**
`os.MkdirAll(dir, 0o700)` plus `Chmod(0o600)` on the file are both inert for
Windows access control, by the explicit statement of `EnsurePrivateDir`'s own
doc comment. So even the "prefer env" advice does not save a Windows operator
who follows it: the config still sits in a directory the platform gave a broad
inherited DACL.

**F5 — the earlier "unusable on Windows" claim was false.** Measured above.
Every refusal traced to one machine-specific group; a home-directory config was
accepted. This finding exists so the correction is on the record next to the
claim.

**F6 — the refusal message is not actionable on Windows.** It prescribes
`chmod 0600` and withholds the one fact that would help: which trustee failed
`noForeignTrustee`.

**F7 — `EnsurePrivateDir` converges pre-existing files, on Windows.** Measured
above. This removes most of the migration cost of adding a check on that
platform: converge the directory first, and configs already inside it become
compliant without the operator doing anything.

**F8 — POSIX migration is not symmetrical.** `ensureDefaultConfig` writes the
file with `Chmod(0o600)`, so any config `setup-service` created already passes.
A hand-written `0644` config does not, and no directory convergence fixes a
file's own mode on POSIX the way re-propagation does on Windows. **[unverified]**
how many real installs have a hand-written config; the mechanism is certain,
the population is not.

## Decision Drivers

* The two ends of one credential should be protected alike, or the weaker end
  sets the actual security level.
* mcremote ships through a self-update feed. A change that makes a running
  daemon refuse to start is far more costly here than a change that warns.
* The repository already owns the right primitive on both platforms; nothing
  needs inventing.
* A security check whose remedy is unavailable on the platform it fires on is
  a support burden, not a control.
* MADR 0116 D22's rule stands: ask "is this private" as a property, never as a
  mode test.

## Considered Options

* **A — Converge the directory, then check the file; fail only when a secret is
  actually inline** (chosen)
* **B — Mirror mcrelay exactly: hard-fail any non-private config**
* **C — Warn only, never fail**
* **D — Remove the secret from the config surface instead**
* **E — Do nothing; document "prefer env" harder**

## Decision Outcome

Chosen option: **A**. It closes F2 for the case that matters, uses F7 to make
the common path self-healing, and keeps a self-update release from stopping
daemons that are running fine today.

### The decisions

**D1 — mcremote's config directory is created and converged with
`appdirs.EnsurePrivateDir`.** Replacing `os.MkdirAll(dir, 0o700)` in
`ensureDefaultConfig`, and called again on the daemon's config-read path so an
existing installation converges without re-running setup. F7 makes this fix
pre-existing Windows configs in place; on POSIX it fixes the directory, and the
file is already `0600` when `setup-service` wrote it (F8).

**D2 — mcremote checks its config file with `appdirs.FileIsOwnerOnly`.** The
same primitive, the same question, on both config-read sites — explicit
`--config` and discovered.

**D3 — a failed check is fatal only when the file actually contains an inline
secret; otherwise it warns.** Concretely: if `relay.secret` is non-empty in the
file, refuse to start; if not, log a warning naming the file and continue. This
is the whole reason option B is rejected — see Consequences.

**D4 — the message names the platform's real remedy and the real cause.** On
Windows: which trustee failed, and that the fix is to move the file under the
private config directory (or re-run `setup-service`), not `chmod`. On POSIX:
`chmod 0600`, unchanged. The wording lives with `FileIsOwnerOnly`'s callers, so
mcrelay's three sites get the same treatment.

**D5 — mcrelay's behaviour does not change.** It keeps hard-failing on any
non-private config, secret or not. Its config's *only* purpose is to carry host
credentials, so there is no benign case to preserve; and it is not shipped
through a self-update feed to interactive users the way mcremote is.

**D6 — record F5's correction where the claim was made.** PLAN 0154's execution
record asserts that `--config` is unusable on Windows. It is amended to point
here.

**D7 — the fatal condition asks the config type, not a field name.** `Config`
gains a predicate (`HasInlineSecret()` or equivalent) that reports whether any
secret-bearing field is set inline in the file, and the check calls that.
Owner decision, 2026-09-08, resolving open question 1: `relay.secret` is the
only such field today, and the second one will be added by someone who has not
read this record. A literal field check would fail open for them silently,
which is the worst available failure mode for a security check.

**D8 — on POSIX, a non-private config file is repaired to `0600` and the repair
is logged.** Owner decision, 2026-09-08, resolving open question 2, which
investigation had already answered on the facts: `EnsurePrivateDir` is
`MkdirAll` plus `validatePrivateDir(dir)` there, which `Lstat`s the directory
and never enumerates children — so D1's convergence cannot self-heal a file the
way it does on Windows.

Repairing matches what the product already does when it *creates* the config
(`ensureDefaultConfig` chmods `0600`), and it makes the outcome the same on
both platforms rather than real on one and advisory on the other. The cost is
accepted and recorded: an operator who deliberately made the file
group-readable for a shared account has that choice silently reversed, which is
why the repair is logged rather than silent.

**D9 — the message says the secret may already be exposed.** One sentence, no
mechanism. Owner decision, 2026-09-08, resolving open question 3. Tightening
the permissions does not un-leak a credential that was readable; a product that
fixes the file and says nothing leaves the operator believing the problem is
over.

### Consequences

* Good: the weaker end of the shared secret gains the protection the stronger
  end already had (F2).
* Good: on Windows, D1 makes existing installations compliant with no operator
  action, because convergence re-propagates (F7). The check that follows then
  passes for everyone whose config lives where the product put it.
* Good: `EnsurePrivateDir` on the config directory is a strict improvement even
  if the check were dropped — today that directory is private on one platform
  only (F4).
* Good: D4 turns a dead-end error into an actionable one, and fixes it for
  mcrelay's three existing sites at the same time.
* Neutral: one more owner-only gate, using a primitive already used four times.
* Bad: D3's split behaviour is more complex than "always fail", and complexity
  in a security check is itself a risk. A reader must now know that a
  world-readable config *without* a secret is tolerated. The mitigation is that
  the warning is unconditional and names the file.
* Bad: a config that holds the secret and cannot be made private will stop the
  daemon. That is the intended trade, but it is a behaviour change reaching
  users through self-update, and F8 says the POSIX population that could hit it
  is unmeasured.
* Bad: this does not protect a secret already leaked. A config that has been
  world-readable should have its registration secret rotated, and nothing here
  tells the operator that.

### Confirmation

```bash
# 1. mcremote refuses a world-readable config that carries an inline secret:
#    (POSIX) chmod 0644 config.yaml with relay.secret set -> exit non-zero
# 2. ...and warns, but starts, when the same file carries no secret:
go test ./internal/config/ -run TestConfigPermission -count=1 -v

# 3. The config directory is private on both platforms after setup:
go test ./internal/cli/service/ -run TestEnsureDefaultConfigPrivate -count=1 -v

# 4. Windows convergence fixes a pre-existing file (F7), asserted not assumed:
go test ./internal/appdirs/ -run TestEnsurePrivateDirConverges -count=1 -v

# 5. mcrelay is unchanged:
go test ./internal/relay/ -count=1
```

## Pros and Cons of the Options

### A — Converge, then check, fail only on an inline secret (chosen)

* Good, because it protects the case that actually matters — a secret on disk
  readable by another principal — and leaves alone the case that does not.
* Good, because D1 does the repair before the check runs, so on Windows the
  common installation becomes compliant rather than broken (F7).
* Good, because it is buildable entirely from primitives the repository already
  ships and already trusts.
* Bad, because "fatal sometimes" is a harder rule to hold in the head than
  "fatal always", and a future editor may collapse it to one or the other
  without noticing which.

### B — Mirror mcrelay: hard-fail any non-private config

* Good, because it is the simplest rule, matches mcrelay exactly, and removes
  the D3 subtlety entirely.
* Good, because it treats the config as sensitive by default, which is the
  conservative reading.
* Bad, and disqualifying on the evidence: on **this very host** the default
  config location inherits a foreign group, so a self-update carrying this
  change would stop the daemon on a machine whose configuration the operator
  never touched. The failure would arrive without an interactive session and
  with a message prescribing `chmod`.
* Its argument survives rejection: if D1's convergence proves reliable across
  hosts, B becomes available later at much lower risk, and is the better
  end-state. The order matters more than the destination.

### C — Warn only, never fail

* Good, because it cannot break anyone, and it surfaces the problem.
* Bad, because a warning in a daemon log is not a control. The secret stays
  readable and the daemon keeps running, which is the status quo with extra
  text.

### D — Remove the secret from the config surface

* Good, because a secret that cannot be written to the file cannot leak from
  it, and `MCREMOTE_RELAY_SECRET` and `--relay-secret` already exist.
* Bad, because the config is how an operator makes a setting persist across a
  service restart; forcing the secret into the environment pushes it into the
  scheduled task or unit file, which is not obviously more private and is
  harder to rotate.
* Worth keeping in view: if the relay feature ever gains a second secret, this
  becomes more attractive than adding a second guarded field.

### E — Do nothing; document "prefer env" harder

* Good, because the comment already says it and costs nothing.
* Bad, because F4 shows the advice is insufficient on Windows regardless: the
  operator who follows it still has a config directory the platform made
  broadly readable, and the product has a primitive that would fix it.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Four owner-only gates, none in mcremote's config path | `grep -rn 'FileIsOwnerOnly' internal/` minus tests |
| No permission check of any kind in `internal/config/` | `grep -rn 'FileIsOwnerOnly\|Perm()\|readable by' internal/config/` |
| `relay.secret` is the only secret-bearing config field | `grep -nE 'Secret\|Token\|Password\|APIKey' internal/config/config.go` filtered to mapstructure tags |
| The field documents itself as sensitive | `internal/config/config.go:99-101` |
| Env and flag alternatives exist | `internal/cli/serve.go:126` |
| Data dir uses `EnsurePrivateDir` | `internal/daemon/daemon.go:86` |
| Config dir uses `os.MkdirAll(0700)` | `internal/cli/service/setup.go:1080` |
| Config file written `0600` | `internal/cli/service/setup.go` `tmp.Chmod(0o600)` |
| `EnsurePrivateDir` is a no-op for ACLs when called as MkdirAll | its own doc comment, `ensure_windows.go` |
| Owner-only is a mode test on POSIX, an ACL test on Windows | `owneronly_unix.go:13`, `security_windows.go:290` |
| `noForeignTrustee` tolerates SY and BA only | `security_windows.go`, the tolerated-trustee map |
| Three of four Windows locations refused, all via one group | measured; trustee table above |
| The refusal prescribes `chmod 0600` | observed CLI output |
| Convergence fixes a pre-existing file on Windows | measured; before/after exit codes and `Get-Acl` |
| `%AppData%\Roaming` carries the foreign group on this host | `Get-Acl 'C:\Users\macsm\AppData\Roaming'` |

### Related records

* **MADR 0116** — the Windows platform layer. D4 defines "private" as an
  explicit DACL with inheritance severed; D22 establishes that privacy is asked
  as a property rather than as a mode test. Both are load-bearing here.
* **MADR 0074** — the credential store, whose `providerauth` gate is the
  pattern F3 describes.
* **MADR 0154** — the record whose execution turned this up, and whose
  execution note D6 amends.
* **The 2026-09-08 Windows drive** — W-1, W-2 and W-4 remain unrecorded.

### Open questions for the plan

All four are resolved; the plan implements D7–D9.

1. ~~**Should the fatal case key on `relay.secret` alone?**~~ **Resolved
   2026-09-08 by owner decision: a predicate on the config type.** Recorded in
   **D7**.
2. ~~**Does `EnsurePrivateDir` converge existing files on POSIX?**~~
   **Resolved 2026-09-08 by reading the source: no.** `ensure_unix.go` is
   `MkdirAll(0o700)` followed by `validatePrivateDir(dir)`, which `Lstat`s the
   directory and never enumerates children. F8's asymmetry is therefore
   confirmed rather than suspected, and **D8** is the answer to it.
3. ~~**Should mcremote advise rotating a secret found world-readable?**~~
   **Resolved 2026-09-08 by owner decision: yes, one sentence.** Recorded in
   **D9**.
4. ~~**Is `CodexSandboxUsers` this project's artefact?**~~ **Resolved
   2026-09-08: no.** `grep -rin 'CodexSandboxUsers|SandboxUsers|net localgroup|New-LocalGroup'`
   across every `.go`, `.ps1`, `.sh` and `.yml` in the repository returns
   nothing. Nothing here creates local groups. The group is the host's own —
   most plausibly the Codex CLI's sandboxing — and its only role in this record
   is as the thing that made F5's refusals reproducible and F1's migration risk
   concrete.

## Amendment — 2026-09-12: exposure is judged on the file as found; a repair does not cancel the refusal

Additive. D3, D8 and D9 are unchanged. This records that the implementation of
PLAN 0155 P2 did not meet them on POSIX, and states the owner's reading of how
they compose, so the next implementation cannot drift the same way.

### What was observed

The first CI run that executed 0155 on Linux, run `34717428526` on
`5edaec3` (2026-09-12), failed `TestGuardConfigFileIsFatalWithAnInlineSecret`
three times: once on `ubuntu-latest` (`go test -race`), and twice on
`ubuntu-24.04-arm` (attempt and retry, so not a flake). The log line just
before each failure is the repair:

```text
WARN tightened config file permissions path=/tmp/TestGuardConfigFileIsFatalWithAnInlineSecret…/cfg/config.yaml mode=0600
--- FAIL: TestGuardConfigFileIsFatalWithAnInlineSecret (0.00s)
    configperm_test.go:101: a config readable by another principal and carrying a credential must be fatal
```

So on POSIX a world-readable config carrying `relay.secret` is tightened to
`0600`, a WARN is logged, and `guardConfigFile` returns nil: **the daemon
starts, and says nothing about the credential.** The same file on Windows is
fatal, because repair is a no-op there.

### Why it happened

* **PLAN P2 put the steps in the wrong order.** It says "chmod `0600`… re-test;
  if **still** not private: fatal when a secret is present". Once the repair
  succeeds, "still not private" is false, so the fatal branch is unreachable on
  POSIX for any file the process owns. That is nearly every real config.
* **That ordering contradicts this record.** Confirmation 1 specifies
  `(POSIX) chmod 0644 config.yaml with relay.secret set -> exit non-zero`. D9
  says *"a product that fixes the file and says nothing leaves the operator
  believing the problem is over"*, which is what POSIX now does. D8 justified
  repairing because it *"makes the outcome the same on both platforms"*, and it
  made them differ.
* **The test that should have caught it rested on a false premise.**
  `makeUnrepairable` (unix) drops write permission on the containing directory,
  on the belief that this stops `chmod`. It does not: `chmod(2)` on a file
  requires only ownership of that file, and directory write permission governs
  creating, removing and renaming entries, not an existing file's mode. The
  helper returned "unrepairable" and the repair succeeded anyway.
* **It was only ever run on Windows.** Every 0155 verification, including the
  end-to-end table in the plan's second amendment, ran on the Windows host,
  where `makeUnrepairable` is `return true` and repair never happens. The POSIX
  branch first executed in CI, after merge.

### The reading, by owner decision (2026-09-12)

**Exposure is a property of the file as the daemon found it.** If the file was
readable by another principal and carries an inline secret, the daemon refuses
to start (D3), even if D8 has just made the file private. D8 still runs first
and is still logged. The refusal says the permissions have been tightened, and
that the credential must be treated as exposed and rotated before starting
again (D9).

Consequences:

* Good: POSIX and Windows now reach the same outcome for the same file, which is
  D8's stated purpose.
* Good: the operator is told about the exposure at the only moment it can be
  detected. After the repair the file looks private, so a later start could not
  tell anyone.
* Neutral: the next start finds a private file and runs, so the refusal is
  one-shot and needs no manual `chmod`.
* Bad: a POSIX installation whose config was loose and carried a secret stops
  once on upgrade. This is D3's accepted cost, which the implementation had
  quietly waived on one platform. Secret-free configs are unaffected: they are
  repaired and start, as before.

Options considered at decision time: **repair then fatal (chosen)**; repair,
warn with D9 advice, and continue (rejected: POSIX and Windows would diverge,
undoing D8); fix only the fixture (rejected: a repaired secret config would stay
silent, contradicting D9).
