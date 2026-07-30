# MADR 0048: Codex sandbox user-namespace failure — auto cannot write

- **Status**: Proposed
- **Date**: 2026-07-29
- **Deciders**: Project Owner (risk posture, product surface); Implementer
  (daemon/providers/mobile/ops)
- **Related**:
  - [MADR 0028](./0028-MADR-codex-provider.md) — codex app-server transport;
    spike already recorded bwrap / user-namespace failure on this class of host
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — auto as `never` +
    `workspace-write`; full-access gated
  - [MADR 0047](./0047-MADR-codex-default-mode.md) — default mode + create-time
    seed + never-alone repair (policy **wire** fixed; **execution** still broken)
  - [protocol-v1.md](./protocol-v1.md) — `session_mode`, `session_capabilities`,
    notices / tool cards
- **Companion plan**:
  [0048-PLAN-codex-sandbox-namespace.md](./0048-PLAN-codex-sandbox-namespace.md)
- **Evidence** (this host, 2026-07-29, codex-cli **0.145.0**):
  - Live rollouts under `~/.codex/sessions/…` with
    `approval_policy: never` + `sandbox_policy.type: workspace-write`
  - Direct `codex sandbox` / `bwrap` / sysctl probes (below)
  - Code inspection of `internal/provider/codex/{mode,session,provider,config}.go`

---

## 1. Problem

Two user-visible failures on the same host class (Ubuntu/Debian with AppArmor
restricting unprivileged user namespaces):

1. **Codex in auto mode cannot write.** Permissions are auto-approved (or never
   requested), the mode chip says `auto`, and the daemon correctly puts
   `never` + `workspace-write` on the wire — but every `apply_patch`,
   workspace shell write, and sandboxed file helper fails with bubblewrap
   unable to create a user namespace. The agent loops on failed edits.
2. **The codex provider cannot establish (or work around) a Linux user
   namespace for the sandbox.** There is no health probe, no session notice, no
   readiness gate, and no config path that turns a broken sandbox into a
   working write session without the operator already knowing to enable
   `allow_full_access` and switch to `full-access`.

MADR 0044/0047 fixed **policy selection and wire shape**. They did **not** fix
**sandbox execution**. On hosts where bwrap namespaces fail, "auto means
unattended *edits in the workspace*" is a lie: the session is unattended
*refusals to write*.

These are product bugs in mcremote, not merely "fix your kernel":

- The daemon **starts** codex and advertises auto as a working mode.
- It never checks whether the sandbox that backs auto can actually run.
- Engine stderr already logs the failure; the phone never sees it until tools
  die mid-turn with opaque helper errors.
- The only working write mode (`full-access` / `danger-full-access`) is **hidden
  by default** (`allow_full_access: false`).

---

## 2. Evidence

### 2.1 Host: AppArmor blocks bwrap userns (not the older sysctl alone)

Probed 2026-07-29 on the development host:

| check | result |
|---|---|
| `kernel.unprivileged_userns_clone` | **1** (enabled) |
| `kernel.apparmor_restrict_unprivileged_userns` | **1** (restricts) |
| `unshare --user --map-root-user true` | **fails** (`uid_map: Operation not permitted`) |
| `bwrap --unshare-user --ro-bind / / true` | **passes** — see §2.1.1; this row previously read "fails" and was wrong |
| `codex sandbox` under `sandbox_mode=read-only` | same bwrap error |
| `codex sandbox` under `sandbox_mode=workspace-write` | same bwrap error |
| `codex sandbox` under `sandbox_mode=danger-full-access` | **writes succeed** |
| `codex exec --dangerously-bypass-approvals-and-sandbox` | **writes succeed** |

So:

- "User namespaces" in the product sense = the **Linux user namespace +
  bubblewrap** path Codex uses for every sandboxed exec/patch.
- Enabling `unprivileged_userns_clone` is **not** sufficient on modern Ubuntu
  when AppArmor's userns restriction is on.
- **Only** `danger-full-access` (no sandbox) produces working writes on this
  host class.

#### 2.1.1 Exact mechanism, and why two obvious probes lie

Re-measured 2026-07-29 while fixing the host. The kernel names the denial
precisely:

```text
apparmor="DENIED" operation="capable" class="cap" profile="unprivileged_userns"
comm="bwrap" capability=21 capname="sys_admin"
```

The chain is: an **unprofiled** process creates a user namespace → AppArmor
transitions it into the `unprivileged_userns` profile → that profile denies
`CAP_SYS_ADMIN` → bwrap cannot set up its mounts. The daemon itself runs under
that profile (`aa-status | grep unprivileged_userns` lists
`/home/mac/.local/bin/mcremote` and its grok child), and codex reaches bwrap
through **node**, which is unprofiled and so transitions.

Two probes look right and are useless — both were tried during this work:

| probe | why it lies |
|---|---|
| `bwrap --unshare-user --ro-bind / / true` | **Passes in the broken state.** bwrap ships its own AppArmor profiles (`bwrap`, `unpriv_bwrap` are both loaded), so it never transitions into the restrictive one. This is why §2.1 originally recorded it as failing — it does not, and a health check built on it reports success on a host where every agent write fails. |
| `aa-exec -p unprivileged_userns -- bwrap …` | **Fails in both states**, by design: it forces the profile, which denies `CAP_SYS_ADMIN` even after the remedy. The fix stops processes *entering* the profile; it does not make the profile permissive. |

The discriminating cheap probe is **`unshare -Ur true`** — an unprofiled binary
creating a userns *and claiming capabilities in it*, the same shape as
node→bwrap. Verified to flip in both directions with the sysctl. The expensive
but most faithful probe is the real thing: `codex sandbox` with
`sandbox_mode="workspace-write"` writing a file, which is what the plan's
Phase 1 health probe uses — keep it that way.

#### 2.1.2 Remedy: the profile-override approach cannot work

The obvious fix — grant the capability back inside the profile via
`/etc/apparmor.d/local/unprivileged_userns` — is **inert**.
`/etc/apparmor.d/unprivileged_userns` contains:

```text
audit deny capability,
...
include if exists <local/unprivileged_userns>
```

In AppArmor an explicit `deny` always beats a later `allow`; precedence is not
order-based. So `allow capability,` in the local include changes nothing, and
the denial above persists after `apparmor_parser -r`. This was tried, applied
cleanly, and still failed.

What works is removing the *transition*, host-wide:

```text
kernel.apparmor_restrict_unprivileged_userns = 0
```

`scripts/bwrap-apparmor-fix.sh` applies it, persists it to
`/etc/sysctl.d/60-mcremote-userns.conf`, and verifies with the `unshare -Ur`
probe. **This is a real security loosening** — it restores pre-24.04 behaviour
in which any unprivileged user can create user namespaces — so it is an
operator decision, not something the daemon should do for itself. Verified on
this host: after applying it and `systemctl --user restart mcremote`, the
daemon runs `unconfined` and `codex sandbox -c 'sandbox_mode="workspace-write"'`
writes successfully in daemon context.

A narrower alternative, not pursued here: ship an AppArmor profile for the
codex/node binary granting `userns,` so only that binary escapes the
restriction. Better security posture, more moving parts, and it must track the
agent's install path.

Spike MADR 0028 §16.1 already recorded the same environment class
(`unshare --user` denied; app-server stderr about bubblewrap/user namespaces).
It deferred live approval tests. The deferred environment is still broken; the
provider shipped modes that **depend** on that environment.

### 2.2 Live auto session: policy correct, patch path dead

From a real codex rollout on this repo (2026-07-29), turn context after auto:

```text
approval_policy: never
sandbox_policy: { type: workspace-write, network_access: false, … }
workspace_roots: [/home/mac/gitrepos/magic-cli-remote]
permission_profile: write access to the workspace root
```

`apply_patch` then fails before any file is touched:

```text
apply_patch verification failed: Failed to read file to update …:
fs sandbox helper failed with status exit status: 1:
bwrap: No permissions to create a new namespace, likely because the kernel
does not allow non-privileged user namespaces.
```

Shell tools under the same sandbox emit the same bwrap line. The model reports
that it "could not create files" / "sandbox is malfunctioning" — accurate about
symptoms, useless about remediation.

**Conclusion:** auto's policy pair is on the wire (0044 D5 / 0047 D5). The
regression the user feels is **not** "sandbox_mode omitted on turn/start" for
this path; it is **sandbox runtime cannot start**.

### 2.3 What mcremote does today (gaps)

| layer | behaviour | gap |
|---|---|---|
| Mode table (`mode.go`) | `auto` → `never` + `workspace-write` | Correct product semantics **when sandbox works** |
| Seed / wire (`seedPolicy`, `applyPolicyParams`, `sandboxPolicyParam`) | Always send both fields after 0047 | Does not detect that sandbox cannot execute |
| `Provider.Ready()` | `exec.LookPath(bin)` only | Binary present ≠ sandbox usable |
| Engine start (`startEngine`) | Captures stderr in `lineRing` (debug log) | Namespace ERROR never becomes a session notice |
| Session create | Emits modes + capabilities (image, load) | No capability / readiness for "sandboxed writes work" |
| `allow_full_access` | Off by default; hides `full-access` | Only working write mode is invisible |
| Tool / item stream | Failed fileChange/command cards | Error text may include bwrap, but no **host-level** diagnosis once per session |
| Config | `sandbox_mode` / `approval_policy` / `allow_full_access` | No "sandbox broken" policy, no forced no-sandbox path short of operator knowledge |

### 2.4 Why 0047 is necessary but insufficient

0047 closed:

- missing `default` mode
- empty create → untrusted `readOnly` while chip lied
- `never` alone → auto without workspace-write on the wire

0047 **did not** close: host cannot create the bwrap user namespace that
`workspace-write` and `read-only` require. After 0047, a healthy chip/policy
pair still produces zero writes on this host.

| mode | approval | sandbox | writes on this host? |
|---|---|---|---|
| `default` | on-request | workspace-write | **No** (bwrap) |
| `read-only` | on-request | read-only | **No** (even reads via helper may fail) |
| `auto` | never | workspace-write | **No** (bwrap) — *the reported bug* |
| `full-access` | never | danger-full-access | **Yes** — gated off |

### 2.5 "Cannot set a namespace" — precise meaning

Not a missing YAML key named `namespace`. The failure is:

1. Codex's Linux sandbox **must** create a user namespace via bubblewrap for
   every sandboxed tool (including the apply_patch fs helper).
2. The kernel/AppArmor stack on the daemon host **refuses** that create.
3. mcremote **never probes, configures, or falls back** around that refusal.

So "the codex provider cannot set a namespace" = **provider has no control
plane for sandbox host fitness**, and therefore cannot deliver the modes it
advertises.

---

## 3. Decision drivers

1. Auto must not claim unattended workspace edits when the OS sandbox cannot
   run.
2. Operators and phones need a **loud, early, actionable** signal — not a mid-
   turn bwrap line buried in a tool card.
3. The only portable write path on broken-userns hosts is no-sandbox
   (`danger-full-access`); that remains **dangerous** and must stay opt-in.
4. Prefer detect + surface + operator escape hatch over silently mapping every
   auto session to full-access (that would re-open MADR 0044's risk split).
5. Fix must be testable without inventing a working userns in CI: probe unit
   tests with fakes; live tests on this host class; optional live success path
   when userns works.
6. Reuse session notices / modes / config; avoid a new protocol message if
   possible (optional capability field only if mobile needs a hard gate).
7. Document host remediation (AppArmor / sysctl / container flags) as a first-
   class ops path, not an afterthought.

---

## 4. Decision

### D1 — Treat sandbox host fitness as a first-class provider state

Add a **sandbox health** snapshot on the codex `Provider`, computed once at
engine start (and optionally refreshed on demand):

| field | meaning |
|---|---|
| `ok` | sandboxed modes can execute (bwrap userns works for workspace-write) |
| `reason` | short machine-stable code, e.g. `userns_denied`, `bwrap_missing`, `ok` |
| `detail` | human string suitable for a TypeNotice (include first bwrap/sysctl line) |
| `probed_at` | wall clock of last probe |

**Probe algorithm** (order cheap → definitive):

1. `LookPath("bwrap")` — if missing → `bwrap_missing` (not always fatal if
   codex embeds a helper; still record).
2. Prefer `codex sandbox` with an explicit `sandbox_mode=workspace-write` and a
   trivial write in a temp dir under the probe cwd (matches production path).
3. Fallback if `codex sandbox` is unavailable: `bwrap --unshare-user …` smoke.
4. Optional parallel: read
   `/proc/sys/kernel/apparmor_restrict_unprivileged_userns` and
   `unprivileged_userns_clone` into `detail` for ops copy (do not treat sysctl
   alone as success — this host has clone=1 and still fails).

Cache under provider mutex. Do **not** re-probe every turn.

### D2 — Loud notice on engine ready and on every session create when unhealthy

When `!ok`:

1. **Daemon log** at Warn (already partial via stderr debug — upgrade to Warn
   with `reason`).
2. **Session TypeNotice** on create/resume (once per session), e.g.:

   > Codex sandbox cannot create a Linux user namespace (bubblewrap). Modes
   > `default`, `read-only`, and `auto` will not be able to write files on this
   > host. Enable `providers.codex.allow_full_access` and switch to **full
   > access**, or fix host userns/AppArmor (see docs). Detail: …

3. Do **not** invent a green "writes work" capability when unhealthy.

Optional (if mobile needs a hard UI gate in the same PR series): add an
optional `Capabilities` field or a notice-only contract without protocol
schema churn first. Prefer notice + mode menu honesty in v1 of this MADR.

### D3 — Mode semantics stay; honesty about effectiveness

Do **not** redefine auto as `danger-full-access`. MADR 0044's split stands:

| mode | wire pair | role |
|---|---|---|
| `default` | on-request + workspace-write | interactive, sandboxed |
| `read-only` | on-request + read-only | locked down |
| `auto` | never + workspace-write | unattended **contained** |
| `full-access` | never + danger-full-access | unattended **unsandboxed** (gated) |

When sandbox health is `!ok`, the menu still lists the same modes (so the
product vocabulary does not fork by host), but:

- the create-time notice explains that sandboxed modes cannot write;
- switching to `auto` while `!ok` re-emits a Warn notice (arming auto that
  cannot write is a footgun);
- `full-access` remains the intentional escape hatch.

### D4 — Operator escape hatches (config)

Keep `allow_full_access` as the gate for advertising `full-access`.

Add **one** explicit config (name bikeshed in plan; recommended):

```yaml
providers:
  codex:
    # When the sandbox health probe fails, what should mcremote do?
    #   warn (default) — notice only; modes unchanged
    #   require_full_access — if allow_full_access, seed sessions as full-access
    #                         and hide sandboxed modes that cannot work; if not
    #                         allowed, fail session create with a clear error
    #   refuse — fail session create whenever health is !ok (strict hosts)
    sandbox_broken_policy: warn
```

Rationale:

- **Default `warn`**: no surprise privilege escalation on hosts that suddenly
  lose userns; matches current security posture.
- **`require_full_access`**: for known broken remote/daemon hosts where the
  operator already accepts unsandboxed agents (common for personal lab boxes
  with AppArmor userns lock). Still requires `allow_full_access: true`.
- **`refuse`**: for CI / multi-tenant where broken sandbox must not look like a
  working codex session.

Do **not** auto-enable `allow_full_access`. That stays a separate, deliberate
opt-in.

### D5 — Detect mid-turn bwrap failures and promote them

Even with D1–D4, a host can break after probe (AppArmor policy reload, etc.).

When tool/item completion text or command output matches the known markers
(`bwrap: No permissions to create a new namespace`, `fs sandbox helper failed`,
`needs access to create user namespaces`):

1. Mark provider sandbox health `!ok` with reason `userns_denied` (sticky until
   re-probe).
2. Emit **one** session TypeNotice (dedupe per session) with the same remedia-
   tion copy as D2.
3. Leave the tool card as failed (truthful); the notice is the diagnosis.

### D6 — Ops remediation is part of the decision

Document, in `docs/config.md` and a short ops subsection of this MADR / README
codex section, the host fixes for the failure class:

1. **Preferred for multi-user / least privilege:** restore working unprivileged
   userns for bwrap. On Ubuntu 24.04+ that means
   `kernel.apparmor_restrict_unprivileged_userns=0` —
   `scripts/bwrap-apparmor-fix.sh` applies, persists and verifies it. Note the
   two dead ends established in §2.1.2: a `local/unprivileged_userns` override
   granting `allow capability,` is **inert** (the profile's `audit deny
   capability,` wins), and any health check built on bare `bwrap` **passes on a
   broken host**. A per-binary AppArmor profile granting `userns,` to codex/node
   is the narrower, better-posture variant if someone wants to build it. Ensure
   container runtimes pass user namespace capability.
2. **Personal daemon on a trusted box:** set
   `allow_full_access: true` and use `full-access` (or
   `sandbox_broken_policy: require_full_access`).
3. **Do not** recommend permanently running all agents as full-access on shared
   hosts.

### D7 — Tests are load-bearing

| test | asserts |
|---|---|
| Unit: probe parser / reason codes | fixture stderr → `userns_denied` |
| Unit: session create when health `!ok` | exactly one TypeNotice; modes still advertised per table |
| Unit: SetMode(auto) when `!ok` | arm notice; policy still never+workspace-write |
| Unit: `sandbox_broken_policy=require_full_access` + gate on | seed full-access; sandboxed modes hidden or create seeds full-access only |
| Unit: `require_full_access` + gate off | create returns clear error |
| Unit: mid-turn bwrap string | sticky health flip + single notice |
| Live (`live_codex`): probe matches `codex sandbox` workspace-write on this host | documents host class; skip or assert `!ok` when bwrap fails |
| Live: full-access write still works when allowed | regression pin for the only working path |

No fake "sandbox works" assertion in unit tests that only check wire shape.

### D8 — Out of scope

- Changing OpenCode / goose / grok sandbox models.
- Shipping a custom bwrap alternative or Landlock-only path (Codex's).
- Persist last mode across sessions (still 0044 D8).
- Making auto = full-access by default.
- Fixing Ubuntu AppArmor policy inside this repository (ops only).
- Experimental granular approval API / `thread/settings/update`.

---

## 5. Options considered

| option | verdict |
|---|---|
| **A. Probe + notice + config escape hatch + mid-turn promotion** (chosen) | Honest, safe default, workable on broken hosts with explicit opt-in |
| B. Silently map auto → danger-full-access when probe fails | Violates 0044 containment; surprising privilege escalation |
| C. Remove auto until userns works | Punishes healthy hosts; does not help operators who need remote agents now |
| D. Docs-only "fix your kernel" | Leaves phone silent; users keep filing "auto cannot write" |
| E. Always require `allow_full_access` for any write mode | Over-restricts healthy hosts where workspace-write is the right default |
| F. Spawn codex with `--dangerously-bypass-approvals-and-sandbox` process-wide | Global to every session on the shared engine (0044 Finding 1); undoes per-session modes |
| G. Depend only on 0047 wire repair | Already shipped; does not restore writes when bwrap is dead |

---

## 6. Consequences

**Good**

- Auto / default / read-only stop looking healthy when the host cannot sandbox.
- Operators get a documented, gated path to working writes (`full-access`).
- Mid-turn bwrap failures become a single clear diagnosis.
- 0044/0047 policy semantics remain intact on healthy hosts.

**Accepted trade-offs**

- Broken-host unattended work requires `allow_full_access` — intentional.
- Probe adds ~tens–hundreds of ms once per engine start (temp dir + short
  sandbox command).
- Mode vocabulary does not fork by host (avoids "auto-on-linux-broken" ids).

**Risks**

| risk | mitigation |
|---|---|
| Probe false-negative (sandbox works but probe wrong) | Prefer `codex sandbox` over raw bwrap; live pin |
| Probe false-positive (probe ok, later fails) | D5 sticky flip + notice |
| Operators enable full-access broadly | Config comments + dangerous flag + confirm dialog (0044) |
| Notice spam | One per session; sticky health without re-notice storm |

---

## 7. Verification criteria (acceptance)

1. On a host where `codex sandbox` workspace-write fails with bwrap userns:
   - engine start logs Warn with `reason=userns_denied` (or equivalent);
   - new codex session emits a TypeNotice before the first turn;
   - `auto` still wires never+workspace-write unless
     `sandbox_broken_policy=require_full_access` applies.
2. With `allow_full_access: true` + `sandbox_broken_policy: require_full_access`:
   - session seeds `full-access`; a write turn succeeds on this host class.
3. With gate off + `require_full_access`: session create fails with a clear
   error naming userns / allow_full_access.
4. Healthy host (working userns): probe `ok`; no scary notice; auto writes as
   0044 designed (live when environment allows).
5. Mid-turn synthetic bwrap failure string promotes health and one notice.
6. Unit + live_codex tests green; pre-add Go gates clean; docs updated.

---

## 8. Summary

| bug | root cause | fix |
|---|---|---|
| Auto mode cannot write | `workspace-write` needs bwrap user namespaces; AppArmor/userns denies create; apply_patch/exec fail closed | **D1–D5** health probe, notices, mid-turn promotion; **D4** gated full-access escape |
| Provider "cannot set a namespace" | No control plane for sandbox host fitness; stderr only; full-access hidden | **D1** provider state; **D2** surface; **D4** config; **D6** ops |
| False confidence after 0047 | Wire policy fixed; execution never checked | **D7** tests that pin health, not only wire shape |

0044/0047 remain correct for **what** auto means. This MADR makes the daemon
honest about **whether the host can run that meaning**, and gives operators a
deliberate path when it cannot.
