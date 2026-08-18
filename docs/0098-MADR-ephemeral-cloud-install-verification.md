---
status: accepted
date: 2026-08-18
decision-makers: Project Owner (scope, spend, and acceptance)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Close the outstanding `install.sh` acceptance rows on ephemeral AWS and DigitalOcean hosts

## Context and Problem Statement

[MADR 0097](0097-MADR-linux-curl-installer.md) shipped in **v0.13.4**, and
[0097-PLAN](0097-PLAN-linux-curl-installer.md) §Verification carries a live
acceptance matrix: **11 rows executed, 12 outstanding**. The outstanding rows
are not incidental — they are every environment class the workstation cannot
produce: RHEL-family hosts with SELinux enforcing, real OpenRC and s6
supervisors, WSL1/WSL2, arm64 on non-Apple silicon, a genuinely tampered
download, and the `--with-relay-service` path that has never been exercised
from scratch.

The plan states the reason this matters in its own words:

> Every one of the three real environments tested so far surfaced a defect the
> 57-assertion suite missed, and all three were **pre-existing-state** bugs —
> an already-installed unit, an already-running second daemon, an
> already-running daemon at uninstall. The harness always starts from nothing;
> real hosts do not.

So the question is not "should we test more" but **what infrastructure produces
those environments cheaply, reproducibly, and with a teardown we can prove**.
Both `aws` and `doctl` are installed and authenticated on this workstation with
create/delete permissions, which makes disposable hosts the obvious raw
material — but the shape of that fleet, its cost ceiling, and its safety
mechanism are the actual decision.

## Decision Drivers

* **D1 — Reach.** Must produce: `systemd-user` on SELinux-enforcing RHEL 9
  family; real `runit`/`s6`/`openrc` supervisors; WSL1 and WSL2; arm64 on cloud
  hardware; a musl host with no `curl`.
* **D2 — Pre-existing state.** Hosts must be long-lived enough within a session
  to install, upgrade, re-run, and uninstall against state the *previous* step
  created. This is the defect class the offline suite structurally cannot find.
* **D3 — Fully CLI-drivable** from this workstation. No GUI, no RDP client, no
  console clicks in the normal path.
* **D4 — Bounded and provable spend.** A cost ceiling known before launch, and
  a teardown assertion that returns *empty*, not "looks fine".
* **D5 — No mutation of published artifacts.** Nothing may rewrite, re-tag, or
  re-upload a GitHub release asset. The tamper test must therefore synthesise
  its own hostile mirror.
* **D6 — No silent gaps.** Every outstanding row either gets executed on real
  hardware or gets an explicit recorded reason it cannot be, written back into
  the 0097-PLAN matrix. A row that is quietly dropped is worse than one marked
  ⬜.

## Considered Options

1. **Ephemeral dual-provider cloud fleet** — AWS EC2 for the rows only it can
   serve (Alpine/musl, Graviton arm64, Windows with nested virtualization for
   WSL) plus the mainstream glibc/systemd/SELinux rows, DigitalOcean droplets
   for a second, root-login provider perspective, and a short-lived S3 bucket
   as a hostile mirror for the checksum row.
2. **Local virtualisation only** — extend the existing Lima usage with
   QEMU/libvirt VMs on the workstation for every remaining distro.
3. **Containers only** — extend the existing `docker run` matrix with
   `systemd`-enabled containers, s6/runit base images, and privileged
   containers.
4. **Defer** — accept the untested surface, ship, and let user bug reports
   drive the remaining rows.

## Decision Outcome

Chosen option: **"Ephemeral dual-provider cloud fleet"**, because it is the
only option that satisfies **D1** at all — WSL and Graviton hardware simply do
not exist inside a container or on this x86 workstation — while satisfying
**D2** in a way containers actively defeat, since a container's whole value
proposition is starting from nothing. The measured account state makes it cheap
and immediate: DigitalOcean already holds the workstation's SSH key, AWS has a
routable VPC with an internet gateway and a 1920-vCPU on-demand quota, and
every image needed except literal Oracle Linux is a free public image.

Provider split is by capability, not preference:

| Need | Provider | Why the other cannot |
|---|---|---|
| RHEL 9 family, SELinux enforcing | DigitalOcean | Equal on AWS; DO is ~4× cheaper per hour and needs no AMI hunting |
| Debian → sysvinit conversion | DigitalOcean | Equal on AWS; same reason |
| Alpine / musl / busybox `wget` | **AWS** | DO publishes no Alpine image |
| arm64 on real cloud silicon | **AWS** (Graviton) | DO offers no ARM droplets |
| WSL1 / WSL2 | **AWS** (Windows + nested virtualization) | DO offers no Windows image |
| Hostile HTTPS mirror | **AWS** (S3) | Needs a valid TLS cert; see §Tamper mirror |

DigitalOcean's remaining role is **provider diversity, not cost** — at
$0.0089/hr versus $0.0208/hr for a comparable `t3.small` the saving is a cent
an hour and decides nothing. What DO actually contributes is a genuinely
different environment: a different kernel build, a different cloud-init
lineage, and a **root-by-default login model** where AWS gives a non-root sudo
user. Since `install.sh` is explicitly a no-root, `$HOME`-scoped installer,
exercising it under both login models is real coverage. Two droplet slots are
free (limit 3, one in use), which is exactly the number used.

### Consequences

* Good, because every one of the 12 outstanding rows gets a named host, a named
  image, and a measured hourly cost — no row remains "untested" for want of a
  place to run it.
* Good, because hosts persist across steps within a session, so the
  install → upgrade → re-run → `--uninstall` sequence runs against genuine
  accumulated state, which is exactly the defect class 0097 identifies as the
  one the harness misses.
* Good, because the fleet is disposable and tagged, so the cost of a mistake is
  bounded by teardown rather than by cleanup archaeology.
* Good, because the tamper mirror (§More Information) tests the security
  property that matters most — *a bad digest leaves an existing install
  untouched* — over a real HTTPS transport, without touching a published
  release (**D5**).
* Good, because the WSL rows turned out to be **cheap, not expensive**. The
  first draft of this record budgeted a 48-vCPU bare-metal Windows instance at
  a measured $2.60/hr spot, on the long-standing rule that EC2 exposes no
  nested virtualisation outside `*.metal`. **That rule changed in February
  2026**: AWS now supports nested virtualisation on virtual instances via the
  `NestedVirtualization=enabled` CPU option, at no additional charge, and the
  EC2 WSL documentation was updated to match — WSL2 is supported on "instances
  that support nested virtualization and have the `NestedVirtualization` CPU
  Option enabled". An `m8i.xlarge` at **$0.215/hr spot / $0.396/hr on-demand**
  replaces the metal instance, a ~92% reduction on what was to be 91% of the
  bill.
* Bad, because nested virtualisation is **instance-generation gated** and the
  two authoritative sources disagree on the list. The EC2 User Guide names
  C8i/M8i/R8i/C8id/R8id/M8id/*-flex/X8i **and** C7i/M7i/R7i/C7i-flex/M7i-flex/
  I7i; the installed `aws-cli/2.36.24` help text says "supported only on 8th
  generation Intel-based instance types (c8i, m8i, r8i, and their flex
  variants)". The plan therefore pins **`m8i.xlarge`**, which appears in both
  lists, rather than trusting either source alone.
* Bad, because the Windows host is still the most expensive line by an order of
  magnitude and is the one resource whose abandonment costs real money. This is
  mitigated by a dead-man switch (§Safety), not eliminated.
* Bad, because **literal Oracle Linux 9 is not a free public AMI.** The only
  OL9 images in `us-east-1` are third-party AWS Marketplace listings
  (ProComputers, owner `679593333241`), which need a one-time subscription
  acceptance in the console — a **D3** violation. Rocky 9 and AlmaLinux 9 are
  substituted; see §Substitutions for what that does and does not prove.
* Bad, because this is an operator-run procedure, not CI. It will rot unless
  the 0097-PLAN matrix is updated in the same session it is executed.
* Neutral, because the DigitalOcean account has a **3-droplet limit** with one
  droplet already in use. Two free slots is exactly what the DO half of the
  fleet needs, so the limit binds without costing anything — but it leaves no
  headroom, and any expansion of the DO share must move to AWS instead.
* Neutral, because AWS has **no default VPC**. The existing `smithclass` VPC
  (`vpc-4c8fe820`) is usable but its subnets do not auto-assign public IPs, so
  every launch must pass `--associate-public-ip-address` explicitly. Measured,
  not assumed; see §Provisioning.

### Amendment — execution narrowed the fleet to one provider

Recorded here rather than by editing the decision above, so the reasoning stays
legible in sequence.

The chosen option is a *dual-provider* fleet, justified by provider diversity:
a second kernel build, a second cloud-init lineage, and a root-by-default login
model. Two constraints adopted at execution time removed DigitalOcean from it.

The first is operational: hosts are launched **one at a time**, each terminated
before the next is created, because the operator's session can end at any point
and an unattended fleet is the only way this sweep leaks money.

The second follows from the first. Every AWS host self-destructs via the
guest-side timer plus `instance-initiated-shutdown-behavior terminate`.
DigitalOcean **bills powered-off droplets**, so no guest-side switch can work
there, and the workstation-side guard is a shell job that dies with the session.
DO was therefore the only remaining way for the sweep to keep costing money
after the terminal closed.

Both DO rows were relocated, not dropped — the root-login model moves to a real
root SSH session on the Rocky host, and `sysvinit` moves to Debian 13 on EC2.
What is actually lost is cross-provider diversity, which this record itself
described as "provider diversity, not cost" — a nice-to-have, and not a 0097
acceptance row.

* Good, because every resource in the sweep now self-terminates within three
  hours of being abandoned, with no exceptions to remember.
* Bad, because the kernel/cloud-init diversity argument that justified the
  dual-provider choice is forfeited, so a defect that only manifests on DO's
  images would not be found.
* Neutral, because cost barely moves: the DO half was five cents.

### Confirmation

Three checkable mechanisms, all of which must pass before this record moves to
`accepted`:

1. **The matrix is the fitness function.** Each executed row is written back
   into the 0097-PLAN Verification table with the host, the image id, and the
   *actual* output — the same discipline the existing 11 ✅ rows follow. A row
   that produced an unexpected `INIT` is recorded as what it produced, not what
   was expected.
2. **Teardown is asserted, not assumed.** Every resource carries
   `mcremote-test=0098`. Session end runs, and must return empty:

   ```sh
   aws ec2 describe-instances \
     --filters Name=tag:mcremote-test,Values=0098 \
               Name=instance-state-name,Values=pending,running,stopping,stopped \
     --query 'Reservations[].Instances[].InstanceId' --output text          # empty
   aws ec2 describe-volumes --filters Name=tag:mcremote-test,Values=0098 \
     --query 'Volumes[].VolumeId' --output text                             # empty
   aws s3api list-buckets --query 'Buckets[?starts_with(Name,`mcremote-tamper-`)].Name' --output text   # empty
   doctl compute droplet list --tag-name mcremote-test --format ID --no-header             # empty
   ```

   Plus a next-day Cost Explorer check filtered to the tag, which is the only
   assertion that catches a resource created outside the tagging convention.
3. **Dead-man switch fires.** Every AWS host is launched with
   `--instance-initiated-shutdown-behavior terminate` and a user-data
   `shutdown` timer, so an abandoned session self-terminates within three
   hours. Verified once, deliberately, by letting a cheap instance expire.
   DigitalOcean has no equivalent — it bills powered-off droplets, so DO hosts
   depend on an explicit destroy and a workstation-side guard. That asymmetry
   is recorded rather than papered over.

## Pros and Cons of the Options

### 1. Ephemeral dual-provider cloud fleet (chosen)

* Good, because it is the only option that reaches WSL and Graviton at all
  (**D1**).
* Good, because hosts are mutable and persistent within a session (**D2**).
* Good, because both CLIs are already authenticated with create/delete scope,
  so there is no access work to do first (**D3**).
* Good, because per-row cost is known in advance and teardown is a tag query
  (**D4**).
* Bad, because the Windows host still dominates the bill and is the one
  resource whose abandonment costs real money.
* Bad, because DigitalOcean bills powered-off droplets, so half the fleet has
  no self-destruct and depends on an explicit teardown step.
* Bad, because it introduces a manual procedure with no CI enforcement.

### 2. Local virtualisation only

* Good, because it is free and needs no credentials.
* Good, because Lima has already proven itself on the arm64 Ubuntu row.
* Bad, because it **fails D1 outright**: the workstation is x86, so arm64 is
  emulated at best (which proves the binary decodes, not that it runs on
  Graviton), and Windows/WSL2 needs nested virtualisation the workstation
  cannot supply for a Windows guest.
* Bad, because RHEL-family cloud images still need conversion or manual
  installation, so the "free" option costs the most operator time.
* Neutral, because it remains the right tool for the rows it *can* serve — it
  is not being removed, only supplemented.

### 3. Containers only

* Good, because it is nearly free and already wired into
  `scripts/install_test.sh`.
* Bad, because it **fails D2 by construction**. The value of a container is a
  clean start; the defect class 0097 wants is dirty state.
* Bad, because it fails D1: `INIT=container` is a terminal classification in
  `detect_init`, so the systemd, SELinux, and boot-persistence paths are
  unreachable by design — the plan already records this ("containers report
  `INIT=container`, so they exercise the install and verify paths, not the
  systemd path").
* Neutral, because it stays valuable as the fast pre-flight before spending a
  cent.

### 4. Defer

* Good, because it costs nothing today.
* Bad, because it fails **D6** and inverts the risk: the installer is the
  **first** thing a new user runs, so a defect here is maximally visible and
  maximally likely to lose that user permanently.
* Bad, because the three defects already found were all found *by* real-host
  testing, which is direct evidence the remaining surface is not clean.

## More Information

### Row-to-host mapping

Every ⬜ row from 0097-PLAN §Verification, with the resource that closes it.
Image ids are measured in `us-east-1` on 2026-08-17 and should be re-resolved
at launch, not copied blindly.

| # | Outstanding row | Provider / resource | Image | ~$/hr |
|---|---|---|---|---|
| 1 | WSL2, systemd **enabled** | AWS `m8i.xlarge` + `--cpu-options NestedVirtualization=enabled`, `us-east-1a` | Windows Server 2025 `ami-04fca11ec6cc2ddab` | 0.396 |
| 2 | WSL2, systemd **disabled** | same host — edit `/etc/wsl.conf`, `wsl --shutdown` | — | 0 |
| 3 | WSL1 | same host — `wsl --set-version <distro> 1` | — | 0 |
| 4 | Rocky 9 real host, SELinux enforcing | AWS `t3.small`, non-root user | Rocky `ami-07f1ef003bc5de2b1` (9.8 x86_64) | 0.021 |
| 4b | Root-login model (see amendment) | same Rocky host, real root SSH session | — | 0 |
| 5 | `--with-relay-service` from scratch | AWS `t3.small` (fresh, never had a unit) | Ubuntu 26.04 `ami-02ebdb11bae1b2486` | 0.021 |
| 6 | `s6` on a real `s6-svscan` | AWS `t3.small` | Alpine `ami-046d66961813ce250` (3.23.5 x86_64 cloudinit) | 0.021 |
| 7 | `openrc-user` on real OpenRC | same Alpine host, **before** `apk add s6` | — | 0 |
| 8a | `openrc-system` messaging | same Alpine host, before `apk add s6`/`runit` | — | 0 |
| 8b | `sysvinit` messaging — **see §Unreachable rows** | AWS `t3.small` + `apt install sysvinit-core` + reboot | Debian 13 `ami-0b764bc5915734858` | 0.021 |
| 9 | `MCREMOTE_VERSION` pin against a real release | any host above | — | 0 |
| 10 | `wget` fallback, no `curl` | Alpine host (busybox `wget`, no `curl` installed) | — | 0 |
| 11 | Checksum failure on a real host | S3 tamper mirror + a host with an existing install | — | ~0 |
| 12 | arm64 on cloud hardware | AWS `c7g.medium` (Graviton3), `us-east-1a` | Ubuntu 26.04 arm64 `ami-0bcb8b16862f4d7ab` | 0.036 |
| 12b | arm64 **and** musl **and** `wget`, one host | AWS `t4g.small` | Alpine aarch64 `ami-0541347c3f8f635dd` | 0.017 |

Eight of the fourteen lines cost nothing extra — they ride on a host another
row already paid for. That is deliberate: the sequencing below is chosen so
each host accumulates state before the row that needs dirty state runs.

### Per-host sequences (state accumulates on purpose)

**Alpine host** — the highest-yield box in the fleet, four rows on one $0.02/hr
instance, and the order is load-bearing because `detect_init` resolves
`runit` → `s6` → `openrc`:

1. Untouched Alpine. Run the installer. `curl` is absent, so this *is* the
   `wget` fallback row (10) — and specifically it proves busybox `wget` follows
   GitHub's redirect to `objects.githubusercontent.com` over TLS, which is the
   one genuinely unknown link in that chain.
   Expect `INIT=openrc-user` **or** `openrc-system` — this single result decides
   0097 open question 2.
2. `apk add s6` → re-run → expect `INIT=s6`, run script created, `s6-svc -l`
   reports up (rows 6, and an idempotent re-run over an existing install).
3. `--uninstall`, confirm the s6 service dir and both binaries are gone.

**Rocky 9 / AlmaLinux 9 hosts** — the SELinux question, which 0097 explicitly
flags as assumption rather than measurement:

1. Confirm `getenforce` reports `Enforcing` before anything else. If it does
   not, the row proves nothing and must be recorded as such.
2. Install, then `systemctl --user is-active mcremote` and
   `loginctl show-user "$USER" --property=Linger`.
3. `ausearch -m avc -ts recent` — **the actual test.** A pass here retires the
   0097 open question; a denial produces the `restorecon` guidance that
   `ops-linux-install.md` currently offers speculatively.
4. Reboot the host and re-check `is-active` — this is the only place boot
   persistence is proven on a machine we are willing to reboot.

**Windows host** — still the most expensive line, so all three WSL rows run in a
single session and the instance is terminated immediately after:

Drive it entirely over SSH (**D3**): EC2Launch v2 runs PowerShell user-data at
first boot, so the user-data installs `OpenSSH.Server`, writes the workstation
public key to `administrators_authorized_keys`, enables
`Microsoft-Windows-Subsystem-Linux` **and** `VirtualMachinePlatform`, and
reboots. No RDP client is needed on this workstation.

Then, in order: WSL2 + systemd enabled (row 1) → remove `[boot] systemd=true`,
`wsl --shutdown`, re-run (row 2) → `wsl --set-version <distro> 1`, re-run
(row 3) → terminate.

**A concrete prediction worth recording before the test runs.** 0097-PLAN
expects `INIT=none` for the WSL2-without-systemd and WSL1 rows. Reading
`detect_init` (`scripts/install.sh:108`), that expectation looks wrong: the
branch is entered on `have systemctl`, and Ubuntu-on-WSL ships the `systemd`
package regardless of whether `[boot] systemd=true` is set — so `systemctl` is
on `PATH`, the user bus is unreachable, and the result should be
**`systemd-broken`**, not `none`. The user then gets *both* advisories: the
`su`/`pam_systemd`/`XDG_RUNTIME_DIR` explanation from line 413 (which is
irrelevant and misleading on WSL) and the correct `/etc/wsl.conf` remedy from
line 421. If the test confirms this, the fix is small — suppress the `su` cause
when `ENVIRONMENT` is `wsl1`/`wsl2` — but it is exactly the kind of thing only
a real host surfaces, and the 0097-PLAN expectation column needs correcting
either way.

### Unreachable rows and gaps found by reading the code

Grounding the fleet against `scripts/install.sh` turned up three things that
change what the outstanding rows can even mean. All three are recorded here so
the plan tests the reachable thing rather than chasing an impossible one.

1. **`sysvinit` is not a classification `detect_init` can produce.**
   0097-PLAN §4.G lists a combined `openrc-system / sysvinit` row, but the
   function has no sysvinit branch: it emits `systemd-user`, `systemd-broken`,
   `runit`, `s6`, `openrc-user`, `openrc-system`, or `none`. Worse, the
   `have systemctl` test fires first and returns unconditionally — so a Debian
   host converted with `apt install sysvinit-core` still has `/usr/bin/systemctl`
   from the `systemd` package and is classified **`systemd-broken`**, then told
   about `su` and `pam_systemd`, which is the wrong diagnosis for a machine
   whose PID 1 is simply not systemd. The row is therefore re-scoped: Alpine
   proves the `openrc-system` branch, and the Debian/sysvinit host is run to
   *document the misclassification*, not to reach a sysvinit branch that does
   not exist.
2. **`--with-relay-service` is honoured only on the systemd path.**
   `setup_service` passes the flag to `mcrelay setup-service` in the systemd
   branch only; `svc_runit`, `svc_s6`, and `svc_openrc` call `write_run_script`
   for `mcremote` alone, so the flag is silently ignored on every non-systemd
   backend. It is also skipped on the systemd *upgrade* path, which returns
   early when a unit already exists — which is precisely why the row demands a
   host that has never had a unit.
3. **`--force-service` exists but appears in no table.** The script accepts it
   (rewrite an existing unit via `setup-service --force`); neither 0097-PLAN
   §3.2 nor `ops-linux-install.md` lists it. A documentation fix, surfaced by
   reading rather than by testing.

### Tamper mirror (row 11)

The one row with no natural host, because it needs a server that serves a
*wrong* digest. Reading `fetch()` (`scripts/install.sh:41`) settles the design:

* `curl` is invoked as `curl -fsSL --proto '=https' --tlsv1.2`. The
  `--proto '=https'` restriction means **a plain `http://` mirror is rejected
  outright under curl** and would silently only exercise the `wget` branch. The
  mirror must be real HTTPS with a valid certificate.
* `MC_TEST_BASE_URL` accepts an absolute path (the `/*` branch), which is what
  the offline harness uses — but a local path skips the transport entirely and
  therefore does not close this row.

S3 supplies valid TLS for free: a bucket whose name contains **no dots** is
reachable at `https://<bucket>.s3.amazonaws.com` under the wildcard
certificate. So:

```sh
B=mcremote-tamper-$(od -An -N4 -tx1 /dev/urandom | tr -d ' ')
aws s3api create-bucket --bucket "$B"
# mirror the real release, then flip one byte in mcremote-linux-amd64
# upload to  s3://$B/latest/download/{SHA256SUMS,mcremote-linux-amd64,mcrelay-linux-amd64}
MC_TEST_BASE_URL="https://$B.s3.amazonaws.com" sh install.sh
```

Run it **on a host that already carries a working install** — the assertion is
not merely "exit 2", it is *"exit 2 and `mcremote version` still reports the
previously-installed version"*. That is the security property, and it is only
meaningful against pre-existing state.

The same bucket doubles as a clean second data point for the busybox-`wget`
row, isolating "does wget work" from "does wget survive GitHub's redirect
chain".

Public read access is required for the window; the bucket is deleted at
session end and its name is randomised so it cannot be pre-squatted.

### Provisioning prerequisites (measured, not assumed)

**AWS.** Verified today: identity `621967135048` (root), region `us-east-1`.

* **No default VPC.** Use `vpc-4c8fe820` (`smithclass`), subnet
  `subnet-7e5b3912` (`us-east-1a`) — confirmed to have a `0.0.0.0/0` route to
  `igw-0fccd3b2d5c93b7e2`. `MapPublicIpOnLaunch` is **false**, so every
  `run-instances` call must pass `--associate-public-ip-address`.
* Security group `sg-0563d5a31d40f1a30` (`any-any`) already permits all ingress
  from `0.0.0.0/0`. Prefer a purpose-built SG limited to this workstation's
  egress IP on 22/3389 and delete it at teardown; the existing one is a
  fallback, not a recommendation.
* **Key pair.** The two existing key pairs (`EC2`, `ec2api`) are RSA and their
  private halves are not on this workstation — only `~/.ssh/id_ecdsa` is.
  Generate a dedicated throwaway ed25519 key, import it as
  `mcremote-test-0098`, and delete it at teardown. A separate **RSA** key pair
  is needed only if Windows password retrieval is used as a fallback to the SSH
  path.
* **Quotas are not a constraint:** on-demand standard 1920 vCPU, spot standard
  256 vCPU, against a fleet whose largest single instance is 4 vCPU.
* All required instance types — `m8i.xlarge`, `t3.small`, `t4g.small`,
  `c7g.medium` — are confirmed offered in `us-east-1a`.
* `aws-cli/2.36.24` accepts `CpuOptions.NestedVirtualization`, verified against
  `run-instances --generate-cli-skeleton`.

**DigitalOcean.** Account `macsmith71@gmail.com`, **droplet limit 3**, one
droplet already running (`wonder-lallygag-net`), so **two slots are free**.
SSH key `vms-hosted-key` (`58480117`) is already registered and its MD5
fingerprint matches this workstation's `~/.ssh/id_ecdsa.pub` — droplets are
SSH-able the moment they boot, with no key work at all.

Available and relevant: `rockylinux-9-x64`, `almalinux-9-x64`,
`debian-13-x64`, `ubuntu-26-04-x64`, `centos-stream-9-x64`, `fedora-44-x64`.
**Not** available: Alpine, any ARM size, any Windows image, Oracle Linux —
which is precisely the split that sends those rows to AWS.

### Substitutions and what they do not prove

* **Oracle Linux 9 → Rocky 9 + AlmaLinux 9.** All three are RHEL 9 rebuilds
  with the same `selinux-policy` package and the same default `Enforcing`
  posture, so the SELinux question 0097 actually raises — *does a
  `systemd --user` unit run an unlabelled binary out of `$HOME`* — is answered
  identically. What it does **not** cover is anything Oracle-specific: UEK
  kernels, `ol9_*` repository layout, or Oracle's own hardening defaults. If
  the literal distro is required, it is a Marketplace subscription click on
  `ami-08bce63b2fc64f706` (ProComputers OL 9 x86_64 LATEST) and then a normal
  launch — recorded here so the choice is deliberate rather than forgotten.
* **Alpine 3.23.5, not 3.22** as the plan's matrix names. Newer, same libc,
  same OpenRC lineage; the version is recorded per-row rather than pinned,
  since the point is musl and busybox, not a specific Alpine release.
* **runit** is already ✅ from the Alpine container run and is not re-run here.

### Safety, cost ceiling, and teardown

Expected total for one full sweep, assuming the Windows host lives **2 hours**
and every Linux host lives **3 hours**. All figures measured on 2026-08-17 in
`us-east-1`:

| Component | Rate | Hours | ~Cost |
|---|---|---|---|
| `m8i.xlarge` Windows on-demand, nested virt | $0.396/hr | 2 | **$0.79** |
| `t3.small` ×4 — Alpine, Rocky, Ubuntu, Debian | $0.0208/hr | 3 each | $0.25 |
| arm64: `c7g.medium` + `t4g.small` | $0.0363 / $0.0168 | 3 each | $0.16 |
| S3 tamper mirror + EBS + transfer | — | — | <$0.10 |
| **Ceiling** | | | **≈ $1.30** |

That is a *ceiling*, assuming every host lives its full three hours. Under the
plan's one-host-at-a-time constraint each Linux host lives minutes, so the
realistic Linux total is nearer $0.20 and the Windows host dominates even more
completely than the table suggests.

Choosing spot for the Windows host would take it to $0.215/hr, but at these
absolute numbers the saving is $0.36 and the cost is a spot-interruption risk
in the middle of a WSL install. **Use on-demand**; the simplicity is worth more
than the coin.

Controls, in the order they matter:

1. **Dead-man switch.** Launch the Windows host with
   `--instance-initiated-shutdown-behavior terminate` and a user-data scheduled
   shutdown (`shutdown -s -t 10800`). Note the EC2Launch v2 constraint: a
   script that wants a *reboot* must `exit 3010` rather than call
   `Restart-Computer`, or the run status becomes inconsistent — so the WSL
   reboot and the self-destruct timer are different mechanisms and must not be
   conflated.
2. **On-demand, not spot,** for Windows — see above. Every Linux host is a
   burstable or small fixed instance where spot buys nothing.
3. **Uniform tagging.** `mcremote-test=0098` on instances, volumes, security
   groups, key pairs, and droplets, so teardown is one query per provider and
   the §Confirmation assertions are meaningful.
4. **Teardown asserted, not assumed** — the empty-result queries in
   §Confirmation, then a tag-filtered Cost Explorer check the following day.
5. **Blast radius.** Everything lands in the existing `smithclass` VPC
   alongside a running `awsutility` instance. Nothing in this procedure
   modifies shared VPC state — no route table, IGW, or existing security group
   is edited. Only per-session resources are created and deleted.

### What this record deliberately does not decide

* **Whether to keep the `openrc-user` backend.** 0097 open question 2 says to
  delete it if it proves unreliable. Row 7 produces the evidence; the decision
  belongs in its own record once there is a measurement to decide on.
* **CI automation of any of this.** This is an attended, operator-run sweep.
  Whether the cheap subset (Alpine, Graviton, Rocky) becomes a scheduled job is
  a separate question, and one worth asking only after the manual sweep shows
  which rows are worth repeating.
* **First-run configuration and pairing** — still out of scope, exactly as
  0097 open question 3 leaves it.

### Related records

* [0097-MADR-linux-curl-installer.md](0097-MADR-linux-curl-installer.md) — the
  installer decision this record verifies.
* [0097-PLAN-linux-curl-installer.md](0097-PLAN-linux-curl-installer.md) — the
  acceptance matrix that is updated as rows are executed.
* [ops-linux-install.md](ops-linux-install.md) — the runbook whose SELinux and
  WSL guidance is currently speculative and becomes measured as a result.
* [0048-MADR-codex-sandbox-namespace.md](0048-MADR-codex-sandbox-namespace.md)
  — AppArmor userns advisory printed by the installer.
* [0065-MADR-update-automation.md](0065-MADR-update-automation.md) —
  `mcremote update` owns upgrades, so the installer runs once per host.
* Implementation plan:
  [0098-PLAN-ephemeral-cloud-install-verification.md](0098-PLAN-ephemeral-cloud-install-verification.md)

### Outcome

Executed 2026-08-18. All 12 outstanding rows run on real hosts (one blocked and
recorded as such); **seven findings, two HIGH**, every one in a row 0097 had
marked untested. Teardown assertions all returned empty. Measured spend well
under the $1.30 ceiling. Findings:
[0098-findings-install-verification-sweep.md](0098-findings-install-verification-sweep.md).

One prerequisite this record got wrong, corrected in the findings: **EC2 rejects
ed25519 key pairs for Windows AMIs outright**, not merely for password
retrieval — an RSA key pair is mandatory to launch at all.

### External sources

* [Use nested virtualization to run hypervisors in Amazon EC2 instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html)
  — supported instance types, `--cpu-options NestedVirtualization=enabled`,
  Windows caveats, no additional cost.
* [Install Windows Subsystem for Linux on your EC2 Windows instance](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/install-wsl-on-ec2-windows-instance.html)
  — WSL1 vs WSL2 instance requirements, exact `wsl --install` commands.
* [Amazon EC2 supports nested virtualization on virtual Amazon EC2 instances](https://aws.amazon.com/about-aws/whats-new/2026/02/amazon-ec2-nested-virtualization-on-virtual)
  — the February 2026 launch that invalidated the bare-metal assumption.
* [Configure EC2Launch v2 settings for Windows instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2launch-v2-settings.html)
  — `<powershell>`/`<persist>` user-data format, `exit 3010` reboot contract.
* [OpenRC User Services — Alpine Linux wiki](https://wiki.alpinelinux.org/wiki/OpenRC_User_Services)
  — user services from Alpine 3.22 / OpenRC 0.60.1-r1, `$XDG_CONFIG_HOME/rc/`
  layout, and the warning not to invoke them under `doas`/`sudo`.
* [Install Linux Subsystem on Windows Server — Microsoft Learn](https://learn.microsoft.com/en-us/windows/wsl/install-on-server)
  — Windows Server 2025 WSL support without the Microsoft Store.
* [Alpine Linux cloud images](https://alpinelinux.org/cloud/) — login user
  `alpine`, keys from IMDS, tiny-cloud vs cloud-init variants.
