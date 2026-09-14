---
status: completed
date: 2026-08-18
madr: "0098-MADR-ephemeral-cloud-install-verification.md"
owner: Project Owner
target: v0.13.5 (or the first release after the sweep completes)
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Plan: Close the outstanding `install.sh` acceptance rows on ephemeral cloud hosts

Associated MADR:
[0098-MADR-ephemeral-cloud-install-verification.md](0098-MADR-ephemeral-cloud-install-verification.md)

## Objective and Scope

Execute the 12 outstanding ⬜ rows in
[0097-PLAN](0097-PLAN-linux-curl-installer.md) §Verification against real
hosts, write the measured result of each back into that table, and destroy
every resource created in the process.

**Done means:** no ⬜ remains in the 0097-PLAN matrix — each is either ✅ with
measured output, or annotated with the recorded reason it is unreachable; the
teardown assertions in §Verification all return empty; and any defect found has
a follow-up record.

**In scope:** provisioning, running, observing, and destroying disposable hosts;
recording results; raising findings.

**Out of scope, deliberately:**

* **Fixing anything found.** This plan measures. A defect becomes a new record
  and its own plan — patching mid-sweep destroys the reproduction.
* **CI automation.** This is an attended, one-shot sweep.
* macOS, Windows-native `mcremote` (there is no Windows build), device pairing,
  and first-run configuration.
* Load, performance, or protocol testing. The daemon only has to start.

## Prerequisites and Dependencies

### Verified account state (measured 2026-08-17 — re-check, do not assume)

| Fact | Value |
|---|---|
| AWS identity / region | `621967135048` (root) / `us-east-1` |
| Default VPC | **none** — use `vpc-4c8fe820` (`smithclass`) |
| Public subnet | **`subnet-0fc17839ef9f6d906` (`us-east-1c`)** — see the NACL note below |
| Auto-assign public IP | **false** — every launch needs `--associate-public-ip-address` |
| Quotas | on-demand standard 1920 vCPU, spot standard 256 vCPU |
| Usable AWS key pairs | none — `EC2`/`ec2api` are RSA with no local private half |
| DO droplet limit | 3, one in use (`wonder-lallygag-net`) → **2 free** |
| DO SSH key | `vms-hosted-key` (`58480117`) already matches `~/.ssh/id_ecdsa.pub` |
| Workstation identity | **is** `wonder-lallygag-net` — DO droplet, nyc2, egress `107.170.40.212` |
| Windows out-of-band access | instance profile `AmazonSSMRoleForInstancesQuickSetup` exists, with `AmazonSSMManagedInstanceCore` attached |

**The workstation is itself one of the three droplets, and one of 0097's
already-tested hosts** — the `wonder` row in the 0097-PLAN matrix is this
machine, which therefore carries a live `mcremote` install. No phase of this
plan runs the installer locally, and none may: the local install is production
state, not a fixture. The only local actions are `aws` and `ssh` calls — and
under **C2** the workstation's own droplet is the sole DigitalOcean resource
involved in this sweep at all.

`107.170.40.212/32` is the address the Phase 0 security group opens to.

**Do not use `subnet-7e5b3912` (`us-east-1a`).** It has a correct route to the
internet gateway and looks usable from every check short of an actual
connection, but its network ACL (`acl-4d8fe821`) contains **only deny rules** —
no allow entry in either direction. An instance launched there boots normally,
cloud-init completes, the key is installed, status checks pass, and the host is
still unreachable: a security group audit shows port 22 open and the failure
looks like the instance's fault. Discovered by burning one host.

Use `subnet-0fc17839ef9f6d906` (`us-east-1c`, NACL `acl-07657d162f5d2adff`),
which carries real allow rules and is proven reachable — the long-running
`awsutility` instance sits in it and accepts SSH from this workstation. All four
required instance types (`t3.small`, `t4g.small`, `c7g.medium`, `m8i.xlarge`)
are offered in `us-east-1c`.

The NACL is **shared VPC state and must not be modified.** Selecting a working
subnet is additive; editing `acl-4d8fe821` would change the blast radius of this
sweep from "resources I created" to "someone else's network".

### Verified release state

`v0.13.4`, published 2026-08-17, **not** a prerelease, so
`releases/latest/download/` resolves to it. It carries `install.sh`,
`SHA256SUMS`, both alias binaries, and both versioned binaries for
`linux/amd64` and `linux/arm64`. Expected `RESOLVED_VER` is **`0.13.4.1`**.

Version-pin fixtures, confirmed by asset listing:

| Release | Alias assets | Use |
|---|---|---|
| `v0.13.3` | present | **positive** pin — expect `0.13.3.1` installed |
| `v0.12.0` | **absent** | **negative** pin — expect exit 2 and the "releases before MADR 0097" message |

### Image ids (`us-east-1`, measured 2026-08-17)

Re-resolve before launch with the commands in §Technical Design; ids rotate.

| Purpose | AMI | Default user |
|---|---|---|
| Alpine 3.23.5 x86_64 (cloud-init) | `ami-046d66961813ce250` | `alpine` |
| Alpine 3.23.5 aarch64 (cloud-init) | `ami-0541347c3f8f635dd` | `alpine` |
| Ubuntu 26.04 amd64 | `ami-02ebdb11bae1b2486` | `ubuntu` |
| Ubuntu 26.04 arm64 | `ami-0bcb8b16862f4d7ab` | `ubuntu` |
| Rocky 9.8 x86_64 | `ami-07f1ef003bc5de2b1` | `rocky` |
| Debian 13 amd64 | `ami-0b764bc5915734858` | `admin` |
| Windows Server 2025 Full Base | `ami-04fca11ec6cc2ddab` | `Administrator` |

### Tooling and access

* `aws-cli/2.36.24` — confirmed to accept `CpuOptions.NestedVirtualization`.
* `doctl`, authenticated — used only for the teardown safety-net assertion.
* `gh`, authenticated (intermittently 503s; retry).
* Outbound SSH (22) from this workstation.

### Execution constraints (supersede MADR 0098's fleet layout)

Two constraints were added after the plan was drafted. Both narrow it; neither
drops a 0097 row.

**C1 — One host at a time.** MADR 0098 permits phases 1–6 to run concurrently.
Execution does not. Each host is launched, tested, has its evidence collected,
and is terminated before the next is launched. A host's whole lifetime is a few
minutes, and at no point does an unattended fleet exist. This costs wall-clock
and saves nothing but risk — which is the correct trade when the operator's
session can end at any time.

**C2 — AWS only. No DigitalOcean droplets.** DO was chosen in MADR 0098 for
provider diversity, but it is the *only* part of the fleet with no self-destruct:
DO bills powered-off droplets, so the dead-man switch cannot be a guest-side
`poweroff`, and the workstation-side guard is a shell job that dies with the
session. Under C1 that is the single way this sweep can quietly keep costing
money after the terminal closes. Both DO rows are relocated rather than dropped:

| Was | Now |
|---|---|
| DO AlmaLinux 9, root login | Rocky 9 on AWS, with a **real root SSH session** enabled for the root-model sub-test (Phase 3.2) |
| DO Debian 13 → sysvinit | Debian 13 on AWS, `ami-0b764bc5915734858` (Phase 6) |

What is genuinely lost is cross-provider diversity — a second kernel build and
cloud-init lineage. That was a nice-to-have in the MADR's own words ("provider
diversity, not cost"), and it is not a 0097 acceptance row. Recorded here so the
reduction is deliberate rather than silent.

With C2 applied every resource in the sweep self-terminates within three hours
of being abandoned.

### Blocking dependencies

None. Every image except literal Oracle Linux 9 is a free public image, and
OL9 is substituted per MADR 0098 §Substitutions.

## Technical Design

### Naming, tagging, and the teardown contract

Every resource carries `mcremote-test=0098`. This tag is the *only* thing
teardown keys on, so **a resource created without it is a leak by
construction**. Names are `mc0098-<role>`.

| Role | Name |
|---|---|
| Alpine x86 | `mc0098-alpine-amd64` |
| Alpine arm64 | `mc0098-alpine-arm64` |
| Ubuntu arm64 (Graviton) | `mc0098-graviton` |
| Rocky 9 | `mc0098-rocky9` |
| Ubuntu 26.04 amd64 (relay + pin + tamper) | `mc0098-ubuntu` |
| Windows 2025 (WSL) | `mc0098-wsl` |
| Debian 13 → sysvinit | `mc0098-sysvinit` |
| Security group | `mc0098-ssh` |
| Key pair | `mc0098` |
| S3 tamper bucket | `mcremote-tamper-<random>` |

### Session environment

Sourced once per session; every later command depends on it.

```sh
export AWS_DEFAULT_REGION=us-east-1
export MC_TAG=mcremote-test
export MC_TAGV=0098
export MC_SUBNET=subnet-0fc17839ef9f6d906
export MC_VPC=vpc-4c8fe820
export MC_KEY="$HOME/.ssh/mc0098"
export MC_R=https://github.com/maccavelli/magic-cli-remote/releases/latest/download
export MC_WORKDIR=/data/cache/tmp/claude-1000/-data-gitrepos-magic-cli-remote/mc0098
mkdir -p "$MC_WORKDIR"
```

### Re-resolving image ids

```sh
alpine_ami() {  # $1 = x86_64 | aarch64
  aws ec2 describe-images --owners 538276064493 \
    --filters "Name=name,Values=alpine-3.*-$1-uefi-cloudinit-r0" \
    --query 'reverse(sort_by(Images,&CreationDate))[0].ImageId' --output text
}
ubuntu_ami() { # $1 = amd64 | arm64
  aws ec2 describe-images --owners 099720109477 \
    --filters "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-*-26.04-$1-server-*" \
    --query 'reverse(sort_by(Images,&CreationDate))[0].ImageId' --output text
}
rocky_ami()   { aws ec2 describe-images --owners 792107900819 \
    --filters "Name=name,Values=Rocky-9-EC2-Base-*.x86_64" \
    --query 'reverse(sort_by(Images,&CreationDate))[0].ImageId' --output text; }
win_ami()     { aws ssm get-parameter \
    --name /aws/service/ami-windows-latest/Windows_Server-2025-English-Full-Base \
    --query 'Parameter.Value' --output text; }
```

### Launch helper

One function, used for every Linux host, so no launch can silently omit the
tag, the public IP, or the dead-man switch.

```sh
mc_launch() {  # $1=name  $2=ami  $3=instance-type  [$4=extra args…]
  _name=$1; _ami=$2; _type=$3; shift 3
  aws ec2 run-instances \
    --image-id "$_ami" --instance-type "$_type" \
    --key-name mc0098 --subnet-id "$MC_SUBNET" \
    --security-group-ids "$MC_SG" --associate-public-ip-address \
    --instance-initiated-shutdown-behavior terminate \
    --user-data "file://$MC_WORKDIR/deadman.yml" \
    --tag-specifications \
      "ResourceType=instance,Tags=[{Key=$MC_TAG,Value=$MC_TAGV},{Key=Name,Value=$_name}]" \
      "ResourceType=volume,Tags=[{Key=$MC_TAG,Value=$MC_TAGV},{Key=Name,Value=$_name}]" \
    "$@" \
    --query 'Instances[0].InstanceId' --output text
}

mc_ip() { aws ec2 describe-instances --instance-ids "$1" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' --output text; }

mc_ssh() { _u=$1; _ip=$2; shift 2
  ssh -i "$MC_KEY" -o StrictHostKeyChecking=accept-new \
      -o UserKnownHostsFile="$MC_WORKDIR/known_hosts" "$_u@$_ip" "$@"; }
```

The dead-man switch is a **single shared user-data file**, written once in
Phase 0 and referenced by every launch:

```sh
cat > "$MC_WORKDIR/deadman.yml" <<'EOF'
#cloud-config
runcmd:
  - [ sh, -c, "nohup sh -c 'sleep 10800; poweroff' >/dev/null 2>&1 &" ]
EOF
```

Combined with `--instance-initiated-shutdown-behavior terminate`, an abandoned
AWS host powers off after three hours and the instance is then terminated.

**Why not `shutdown -h +180`.** Two of the eight hosts are Alpine, where
`shutdown` is busybox: it has no `+minutes` form, and busybox `poweroff -d`
takes *seconds* and blocks rather than backgrounding. The obvious command
therefore fails **silently** on exactly the hosts where nobody is watching —
cloud-init logs an error nobody reads, and the instance runs until someone
notices the bill. A backgrounded `sleep` + `poweroff` is the only form that
behaves identically under busybox, coreutils, and systemd.

`file://` is used rather than an inline `$(printf …)` so the CLI's base64
handling and the shell's quoting are both taken out of the equation — the same
bytes reach every host.

Under **C2** every host in the sweep is an EC2 instance, so this switch is the
whole safety story — there is no resource left that needs a workstation-side
guard, and nothing that survives a closed terminal.

### The one-at-a-time loop (C1)

Every host follows the same lifecycle, and the next host is not launched until
the previous one is gone:

```sh
mc_cycle() {   # $1=role  $2=ami  $3=type  $4=ssh-user
  I=$(mc_launch "$1" "$2" "$3")
  aws ec2 wait instance-running --instance-ids "$I"
  IP=$(mc_ip "$I")
  echo "$1 $I $IP"
  # …phase-specific work over mc_ssh "$4" "$IP" …
  mc_collect "$1" "$4" "$IP"                 # MUST succeed before the next line
  [ -n "$(ls -A "$MC_EVID/$1" 2>/dev/null)" ] || { echo "NO EVIDENCE — do not terminate"; return 1; }
  aws ec2 terminate-instances --instance-ids "$I"
  aws ec2 wait instance-terminated --instance-ids "$I"
}
```

The evidence check is a hard gate, not a courtesy: terminating a host whose logs
were never copied means the row must be paid for and run again.

### The standard host procedure

Every Linux host runs the same five steps, so results are comparable and
deviations are obviously deviations:

```sh
# S1 classification only — must write nothing
wget -qO install.sh "$MC_R/install.sh" 2>/dev/null || curl -fsSLo install.sh "$MC_R/install.sh"
sh install.sh --dry-run --verbose ; echo "exit=$?"
# Assert on the BINARY, not the directory: ~/.local/bin already exists on
# stock Ubuntu and several other images, so "no such directory" would be a
# spurious failure rather than evidence.
ls "$HOME/.local/bin/mcremote" 2>&1       # expect: No such file or directory

# S2 the advertised one-liner, exactly as the README publishes it.
# Capture the installer's own exit code, not tee's — the pipeline's status is
# the last command's, and `pipefail` is not POSIX.
curl -fsSL "$MC_R/install.sh" | sh > install-1.log 2>&1 ; echo "exit=$?"
cat install-1.log
#   on hosts without curl:  wget -qO- "$MC_R/install.sh" | sh

# S3 what landed
"$HOME/.local/bin/mcremote" version        # must equal RESOLVED_VER from the log
command -v mcremote || echo "not on PATH (expected on some hosts)"

# S4 idempotent re-run over the state S2 created
sh install.sh --verbose 2>&1 | tee install-2.log ; echo "exit=$?"

# S5 removal
sh install.sh --uninstall 2>&1 | tee uninstall.log ; echo "exit=$?"
ls "$HOME/.local/bin" 2>&1
```

Record for every host: `arch=`/`env=`/`init=`/`pid1=` from `--verbose`, the
`service:` line from the summary, both exit codes, and `mcremote version`.

### Pulling the evidence back before the host dies

**This is the step most likely to be skipped and the most expensive to skip.**
Every log in the procedure above is written on a machine that is about to be
terminated, and the entire deliverable of this plan is measured output written
back into the 0097-PLAN matrix. A terminated host with un-copied logs means the
row has to be paid for and run twice.

So the last action on every host, *before* it is terminated, is:

```sh
mc_collect() {   # $1 = role name (mc0098-alpine-amd64, …)  $2 = ssh user  $3 = ip
  _d="$MC_EVID/$1"; mkdir -p "$_d"
  # the logs the standard procedure wrote
  scp -i "$MC_KEY" -o StrictHostKeyChecking=no \
      "$2@$3:~/install-*.log" "$2@$3:~/uninstall.log" "$_d/" 2>/dev/null || true
  # host facts that explain the logs
  mc_ssh "$2" "$3" 'uname -a; echo ---; cat /etc/os-release; echo ---;
    cat /proc/1/comm; echo ---; command -v systemctl runsvdir s6-svscan rc-service curl wget;
    echo ---; ls -la ~/.local/bin 2>&1; echo ---;
    ~/.local/bin/mcremote version 2>&1' > "$_d/host-facts.txt" 2>&1
}
```

Terminate only once `ls "$MC_EVID/<role>"` is non-empty. The evidence directory
is what Phase 8 reads from; nothing in Phase 8 requires a live host.

### Expected `SERVICE_RESULT` strings

Assertions must match the script's actual output vocabulary
(`scripts/install.sh:445`), not the prose in 0097-PLAN:

| `SERVICE_RESULT` | Summary line |
|---|---|
| `supervised+boot` | `service:  running, and enabled at boot (systemd user unit + linger)` |
| `supervised` | `service:  supervised, restarts on crash` |
| `supervised-session` | `service:  supervised for this login session only` |
| `skipped` | `service:  skipped (--no-service)` |
| `failed` | `service:  setup FAILED — the binaries are installed and usable` |
| `none` | `service:  not configured (no supported service manager detected: <INIT>)` |

## Execution Phases

Phases 1–6 are independent and may run concurrently; **Phase 7 (Windows) runs
last** because it is the only host that costs real money per hour. Phase 0 is a
hard prerequisite for all of them, and Phase 8 must run even if an earlier
phase fails.

---

### Phase 0 — Session setup

**Deliverable:** key pair, security group, and a verified release, all tagged.

```sh
# 0.1 dedicated throwaway key
ssh-keygen -t ed25519 -N '' -f "$MC_KEY" -C mcremote-test-0098
aws ec2 import-key-pair --key-name mc0098 \
  --public-key-material "fileb://$MC_KEY.pub" \
  --tag-specifications "ResourceType=key-pair,Tags=[{Key=$MC_TAG,Value=$MC_TAGV}]"

# 0.2 security group limited to this workstation
MY_IP=$(curl -fsS https://checkip.amazonaws.com)
export MC_SG=$(aws ec2 create-security-group --group-name mc0098-ssh \
  --description "MADR 0098 ephemeral install verification" --vpc-id "$MC_VPC" \
  --tag-specifications "ResourceType=security-group,Tags=[{Key=$MC_TAG,Value=$MC_TAGV}]" \
  --query GroupId --output text)
aws ec2 authorize-security-group-ingress --group-id "$MC_SG" \
  --ip-permissions "IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=$MY_IP/32}]"

# 0.3 the shared dead-man switch, written once, used by every launch
cat > "$MC_WORKDIR/deadman.yml" <<'EOF'
#cloud-config
runcmd:
  - [ sh, -c, "nohup sh -c 'sleep 10800; poweroff' >/dev/null 2>&1 &" ]
EOF

# 0.4 evidence directory — one per host, created before any host exists
export MC_EVID="$MC_WORKDIR/evidence"
mkdir -p "$MC_EVID"

# 0.5 prove the release the whole sweep depends on
for a in install.sh SHA256SUMS mcremote-linux-amd64 mcremote-linux-arm64 \
         mcrelay-linux-amd64 mcrelay-linux-arm64; do
  printf '%-24s %s\n' "$a" "$(curl -sIL -o /dev/null -w '%{http_code}' "$MC_R/$a")"
done                                  # all must print 200
```

**Exit criterion:** all six assets return `200`; `$MC_SG`, the key pair,
`deadman.yml`, and `$MC_EVID` all exist.

---

### Phase 1 — Alpine amd64: four rows on one host

**Closes:** rows 7 (`openrc-user`), 8a (`openrc-system`), 6 (`s6`), 10 (`wget`
fallback). **Order is load-bearing** — `detect_init` resolves
`runit` → `s6` → `openrc`, so every OpenRC observation must be made *before*
s6 is installed.

```sh
I=$(mc_launch mc0098-alpine-amd64 "$(alpine_ami x86_64)" t3.small)
aws ec2 wait instance-running --instance-ids "$I"; IP=$(mc_ip "$I")
```

**1.1 — Baseline probe (before touching anything).**

```sh
mc_ssh alpine "$IP" 'command -v curl; command -v wget; wget --version 2>&1|head -1;
  rc-service --version; rc-service --user --help >/dev/null 2>&1; echo "user-mode-exit=$?";
  cat /proc/1/comm'
```

If `curl` is present, remove it — the row is meaningless otherwise:
`doas apk del curl` (fall back to `sudo` if `doas` is absent), then re-assert
`command -v curl` is empty.

**1.2 — Row 10 + row 7/8a.** Run the standard procedure S1–S3 using the
`wget -qO- … | sh` form. Two things are being measured at once:

* busybox `wget` must follow GitHub's redirect to
  `objects.githubusercontent.com` over TLS and produce a byte-identical file —
  a checksum pass proves both.
* `INIT` is either `openrc-user` (Alpine ≥ 3.22 ships OpenRC 0.60.1-r1 with
  `rc-service --user`) or `openrc-system`. **Record which, verbatim.** This
  single observation resolves 0097 open question 2.

If `openrc-user`: assert `~/.config/rc/init.d/mcremote` exists and is `0755`,
and run `rc-service --user mcremote status` **without `doas`/`sudo`** — the
Alpine wiki explicitly warns that privileged invocation errors out. A failure
here is a legitimate result, not an operator error: `svc_openrc` treats start
failure as non-fatal by design.

If `openrc-system`: assert `SERVICE_RESULT=none`, exit 0, and that the summary
prints the `nohup … mcremote serve` background line.

**1.3 — Row 6 (`s6`).**

```sh
mc_ssh alpine "$IP" 'doas apk add s6; command -v s6-svscan s6-svc'
```

Re-run S2. Assert: `INIT=s6`;
`~/.local/share/s6/service/mcremote/run` exists, is `0755`, and contains
`exec "$HOME/.local/bin/mcremote" serve`; `SERVICE_RESULT` is
`supervised-session` (no pre-existing `s6-svscan`) with the summary line
`at boot:  NOT configured — arrange for your supervisor to start at boot`.
Then `s6-svc -l ~/.local/share/s6/service/mcremote` and confirm the daemon
process exists.

**1.4 — S4 idempotent re-run, then S5 uninstall.** After uninstall assert the
s6 service directory *and* both binaries are gone, and that no `mcremote`
process survives (`pgrep -u "$(id -u)" mcremote` empty) — the deleted-inode
failure mode 0097 found on `awsutility`.

**Exit criterion:** rows 6, 7/8a, 10 recorded with verbatim `INIT` and
`SERVICE_RESULT`; open question 2 answered yes or no.

---

### Phase 2 — arm64 on real cloud silicon

**Closes:** rows 12 and 12b.

```sh
G=$(mc_launch mc0098-graviton     "$(ubuntu_ami arm64)" c7g.medium)
A=$(mc_launch mc0098-alpine-arm64 "$(alpine_ami aarch64)" t4g.small)
```

**2.1 — Graviton Ubuntu (`ubuntu@`).** Standard procedure S1–S5. Assert
`arch=arm64`, `INIT=systemd-user`, `SERVICE_RESULT=supervised+boot`,
`systemctl --user is-active mcremote` → `active`,
`loginctl show-user ubuntu --property=Linger` → `Linger=yes`, and
`file ~/.local/bin/mcremote` reports `ELF 64-bit LSB … ARM aarch64`.
This is the row 0097 could only prove under Apple Virtualization.

**2.2 — Alpine aarch64.** Standard procedure. This is arm64 **and** musl
**and** busybox `wget` simultaneously — the strongest single evidence that the
static `CGO_ENABLED=0 -tags netgo,osusergo` build claim in
`ops-linux-install.md` holds. Assert the same binary reports `0.13.4.1`.

**Exit criterion:** both hosts install, the daemon starts, and the arm64
binary's ELF header is confirmed on non-Apple hardware.

---

### Phase 3 — RHEL 9 family with SELinux enforcing

**Closes:** rows 4 and 4b. Two hosts, two login models, deliberately.

**3.1 — AWS Rocky 9, non-root user (`rocky@`).**

```sh
R9=$(mc_launch mc0098-rocky9 "$(rocky_ami)" t3.small)
```

Before anything else — if this reports `Disabled` or `Permissive`, the row
proves nothing and must be recorded as inconclusive rather than passed:

```sh
mc_ssh rocky "$IP" 'getenforce; sestatus | head -5; echo "XDG=$XDG_RUNTIME_DIR"'
```

Standard procedure S1–S5, then **the actual test**:

```sh
mc_ssh rocky "$IP" 'command -v ausearch >/dev/null 2>&1 \
    && sudo ausearch -m avc -ts recent 2>&1 | tail -40 \
    || { echo "(no auditd — falling back)"; sudo dmesg | grep -i -E "avc|denied" | tail -40; }
  echo "---"; ls -Z ~/.local/bin/mcremote; systemctl --user is-active mcremote;
  loginctl show-user rocky --property=Linger'
```

The Rocky EC2 Base image is minimal and may ship without `auditd`, hence the
`dmesg` fallback — an *absent* `ausearch` must not be misread as an absence of
denials. *No AVC denials* retires 0097 open question 1 and makes the SELinux paragraph
in `ops-linux-install.md` measured rather than speculative. *Any denial*
becomes a finding, and the `restorecon -Rv ~/.local/bin` remedy is tested on
the spot and written up.

**3.2 — The root-login model, on the same host.**

Per **C2** this no longer uses a DigitalOcean droplet. The property under test
is not the distro but the *login model*: `install.sh` run by root in a real
login session, where `INSTALL_DIR` becomes `/root/.local/bin` and
`systemctl --user` depends on `pam_systemd` having created `/run/user/0`.

`sudo -i` is **not** a substitute — it does not create a logind session, so
`/run/user/0` may be absent and the test would measure the wrong thing. Enable a
genuine root SSH session on the Rocky host instead:

```sh
mc_ssh rocky "$IP" 'sudo install -d -m700 /root/.ssh &&
  sudo cp ~/.ssh/authorized_keys /root/.ssh/authorized_keys &&
  sudo sed -i "s/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/" /etc/ssh/sshd_config &&
  sudo systemctl reload sshd'
```

Cloud images commonly ship root's `authorized_keys` with a forced command that
prints "Please login as…", which the `cp` above overwrites. Then run the
standard procedure as `root@`, and assert `XDG_RUNTIME_DIR=/run/user/0`,
`INIT=systemd-user`, `Linger=yes` for root, and `getenforce` → `Enforcing`.

If root's user bus is *not* available the result is `systemd-broken`, and the
advisory it prints must be evaluated for whether it makes sense for a root
session — that is a real-world outcome, not a test failure.

Because this runs on the host Phase 3.1 already dirtied, it doubles as a second
pre-existing-state case: a second user installing over a machine that already
carries another user's install.

**Exit criterion:** rows 4/4b recorded with `getenforce`, `ausearch` output,
and `Linger` for both login models.

---

### Phase 4 — Relay service and version pinning

**Closes:** rows 5 and 9. Requires a host that has **never** had a unit —
`setup_service` returns early on the upgrade path, so a reused host silently
skips the relay creation this row exists to test.

```sh
U=$(mc_launch mc0098-ubuntu "$(ubuntu_ami amd64)" t3.small)
```

**4.1 — Row 5, `--with-relay-service` from scratch.**

```sh
mc_ssh ubuntu "$IP" 'ls ~/.config/systemd/user/ 2>&1'   # must be empty/absent
mc_ssh ubuntu "$IP" 'curl -fsSLo install.sh "'"$MC_R"'/install.sh" &&
  sh install.sh --with-relay-service --verbose; echo "exit=$?"'
```

Assert **both** units exist and are active, both have `Linger=yes`, and
`systemctl --user cat mcrelay` shows the expected `ExecStart`:

```sh
mc_ssh ubuntu "$IP" 'systemctl --user is-active mcremote mcrelay;
  ls ~/.config/systemd/user/; loginctl show-user ubuntu --property=Linger'
```

Then confirm MADR 0098 §Unreachable-rows finding 2 on the same host: re-run
with `--with-relay-service` now that a unit exists and verify the flag is
ignored (early return, `existing unit(s) kept` in the log).

**4.2 — Row 9, version pin.** Three cases, in order:

```sh
# positive: a release that carries alias assets
sh install.sh --version 0.13.3 --verbose; echo "exit=$?"
~/.local/bin/mcremote version                 # expect 0.13.3.1

# negative: a pre-0097 release with no alias assets
sh install.sh --version 0.12.0 --verbose; echo "exit=$?"     # expect 2
~/.local/bin/mcremote version                 # MUST still be 0.13.3.1 — unchanged

# back to current
curl -fsSL "$MC_R/install.sh" | sh
~/.local/bin/mcremote version                 # expect 0.13.4.1
```

The `v0.12.0` case is the valuable one: it must fail with exit 2 and the
"releases before MADR 0097 do not [carry the alias assets]" guidance, and it
must leave the working 0.13.3.1 install untouched.

**Exit criterion:** relay unit created and active on a virgin host; all three
pin cases produce the expected version and exit code.

---

### Phase 5 — Checksum failure over real HTTPS

**Closes:** row 11. Runs on the `mc0098-ubuntu` host from Phase 4, **after** it
has a working install — the assertion is not "exit 2" but "exit 2 and the
existing install still works".

```sh
# 5.1 build the hostile mirror (bucket name must contain NO dots, so the
#     wildcard cert on *.s3.amazonaws.com validates under curl's --proto '=https')
B=mcremote-tamper-$(od -An -N4 -tx1 /dev/urandom | tr -d ' ')
aws s3api create-bucket --bucket "$B"
aws s3api put-bucket-tagging --bucket "$B" \
  --tagging "TagSet=[{Key=$MC_TAG,Value=$MC_TAGV}]"
aws s3api put-public-access-block --bucket "$B" \
  --public-access-block-configuration \
  BlockPublicAcls=false,IgnorePublicAcls=false,BlockPublicPolicy=false,RestrictPublicBuckets=false
aws s3api put-bucket-policy --bucket "$B" --policy "$(cat <<EOF
{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*",
 "Action":"s3:GetObject","Resource":"arn:aws:s3:::$B/*"}]}
EOF
)"

# 5.2 mirror the genuine release, then corrupt exactly one binary
cd "$MC_WORKDIR" && mkdir -p latest/download && cd latest/download
for f in SHA256SUMS mcremote-linux-amd64 mcrelay-linux-amd64; do
  curl -fsSLO "$MC_R/$f"; done
printf 'x' | dd of=mcremote-linux-amd64 bs=1 seek=1024 conv=notrunc   # flip one byte
cd "$MC_WORKDIR" && aws s3 cp --recursive latest "s3://$B/latest"
```

**5.3 — Run it against a host with a working install.**

```sh
mc_ssh ubuntu "$IP" '~/.local/bin/mcremote version'            # note it: V_BEFORE
mc_ssh ubuntu "$IP" 'MC_TEST_BASE_URL=https://'"$B"'.s3.amazonaws.com sh install.sh --verbose; echo "exit=$?"'
mc_ssh ubuntu "$IP" '~/.local/bin/mcremote version; systemctl --user is-active mcremote'
```

Assert: exit **2**; stderr contains `checksum mismatch for mcremote` with both
digests and `Nothing was installed.`; `mcremote version` is **unchanged**; the
service is still `active`; and no `.mcinstall.*` directory is left behind in
`~/.local/bin`.

**5.4 — Control case.** Repeat against the *uncorrupted* mirror
(`aws s3 cp` the clean binary back) and confirm it installs cleanly — this
proves the failure in 5.3 came from the tampering and not from the mirror
being unreachable. It also exercises `curl`'s `--proto '=https'` path against
a non-GitHub host.

**Exit criterion:** tamper rejected with the install intact; clean mirror
accepted.

---

### Phase 6 — The sysvinit misclassification

**Closes:** row 8b, re-scoped per MADR 0098 §Unreachable rows — `detect_init`
has no `sysvinit` branch, so this phase **documents what actually happens**
rather than asserting a classification that cannot occur.

```sh
D=$(mc_launch mc0098-sysvinit ami-0b764bc5915734858 t3.small)   # Debian 13, user: admin
aws ec2 wait instance-running --instance-ids "$D"; IP=$(mc_ip "$D")
```

```sh
mc_ssh admin "$IP" 'sudo apt-get update -qq && sudo apt-get install -y sysvinit-core && sudo reboot'
# reboot drops the connection; wait, then reconnect:
aws ec2 wait instance-running --instance-ids "$D"
mc_ssh admin "$IP" 'cat /proc/1/comm; command -v systemctl; echo "XDG=$XDG_RUNTIME_DIR"'
mc_ssh admin "$IP" 'curl -fsSLo install.sh "'"$MC_R"'/install.sh" && sh install.sh --dry-run --verbose'
```

Note the Debian AMI's default user is **`admin`**, not `debian` or `ubuntu`.
Switching PID 1 out from under a running system is exactly the kind of change
that can leave a host unreachable — if it does not come back, terminate and
record the row as blocked rather than debugging a disposable host.

**Predicted result:** `pid1=init`, `systemctl` still present (the `systemd`
package remains; only `systemd-sysv` is replaced), therefore
`INIT=systemd-broken` and the advisory explains `su`/`pam_systemd`/
`XDG_RUNTIME_DIR` — a diagnosis that is simply wrong for a host whose PID 1
is not systemd. Run the full install and capture the complete advisory text
verbatim; that text is the deliverable.

**Exit criterion:** the classification and the printed advisory are recorded
verbatim, and a finding is raised if the prediction holds.

---

### Phase 7 — WSL1 and WSL2 (run last — the only metered host)

**Closes:** rows 1, 2, 3. One Windows host, three rows, then immediate
termination.

**7.1 — Launch with nested virtualization.** `m8i.xlarge` is chosen because it
appears in *both* the EC2 User Guide list and the narrower `aws-cli` help text
(MADR 0098 §Consequences).

```sh
cat > "$MC_WORKDIR/win-userdata.txt" <<'EOF'
<powershell>
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' `
  -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
$k = 'PUBKEY_PLACEHOLDER'
$f = "$env:ProgramData\ssh\administrators_authorized_keys"
Set-Content -Path $f -Value $k -Encoding ascii
icacls $f /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F'
shutdown -s -t 10800
</powershell>
<persist>true</persist>
EOF
sed -i "s|PUBKEY_PLACEHOLDER|$(cat "$MC_KEY.pub")|" "$MC_WORKDIR/win-userdata.txt"

W=$(aws ec2 run-instances --image-id "$(win_ami)" --instance-type m8i.xlarge \
  --cpu-options "NestedVirtualization=enabled" \
  --key-name mc0098 --subnet-id "$MC_SUBNET" --security-group-ids "$MC_SG" \
  --associate-public-ip-address \
  --instance-initiated-shutdown-behavior terminate \
  --user-data "file://$MC_WORKDIR/win-userdata.txt" \
  --iam-instance-profile Name=AmazonSSMRoleForInstancesQuickSetup \
  --tag-specifications \
    "ResourceType=instance,Tags=[{Key=$MC_TAG,Value=$MC_TAGV},{Key=Name,Value=mc0098-wsl}]" \
    "ResourceType=volume,Tags=[{Key=$MC_TAG,Value=$MC_TAGV},{Key=Name,Value=mc0098-wsl}]" \
  --query 'Instances[0].InstanceId' --output text)
```

`shutdown -s -t 10800` is the dead-man switch; combined with
`instance-initiated-shutdown-behavior terminate` the host destroys itself
after three hours. Windows first boot takes several minutes — poll SSH rather
than assuming.

**Fallback if the OpenSSH user-data path fails.** This is the only step in the
plan with no rehearsed precedent, and this workstation has no RDP client, so a
failure would otherwise dead-end the phase. The instance is therefore launched
with the pre-existing `AmazonSSMRoleForInstancesQuickSetup` profile
(`AmazonSSMManagedInstanceCore` confirmed attached), which makes every
subsequent PowerShell step drivable without SSH:

```sh
aws ssm send-command --instance-ids "$W" \
  --document-name AWS-RunPowerShellScript \
  --parameters 'commands=["wsl -l -v"]' \
  --query 'Command.CommandId' --output text
aws ssm get-command-invocation --instance-id "$W" --command-id "$CID" \
  --query 'StandardOutputContent' --output text
```

Attaching the profile costs nothing and removes the phase's only single point
of failure. Confirm SSM registration with
`aws ssm describe-instance-information --filters Key=InstanceIds,Values=$W`
before concluding the host is unreachable.

Confirm nested virtualisation is actually on before spending time on WSL:

```sh
aws ec2 describe-instances --instance-ids "$W" \
  --query 'Reservations[0].Instances[0].CpuOptions' --output json
```

**7.2 — Install WSL2.** Over SSH as `Administrator`. Per the EC2 WSL
documentation, on a nested-virt-enabled instance `wsl --install` installs WSL 2
by default:

```powershell
wsl --install --no-launch
Restart-Computer -Force        # from the SSH session, NOT from user data —
                               # user-data scripts must use `exit 3010` instead
# after reconnect:
wsl --install -d Ubuntu
wsl --version
wsl -l -v                      # Ubuntu must show VERSION 2
```

**7.3 — Row 1: WSL2 with systemd enabled.**

```powershell
wsl -d Ubuntu -u root -- sh -c 'printf "[boot]\nsystemd=true\n" > /etc/wsl.conf'
wsl --shutdown
wsl -d Ubuntu -- sh -c 'cat /proc/sys/kernel/osrelease; ps -p 1 -o comm=; echo "XDG=$XDG_RUNTIME_DIR"'
wsl -d Ubuntu -- sh -c 'curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh'
```

Expect `env=wsl2`, `INIT=systemd-user`, `SERVICE_RESULT=supervised+boot`.
Then verify `systemctl --user is-active mcremote` and `Linger=yes` **inside**
WSL — linger under WSL's systemd is the part worth actually measuring.

**7.4 — Row 2: WSL2 without systemd.** Remove `/etc/wsl.conf`, `wsl --shutdown`,
re-enter, re-run `install.sh --dry-run --verbose` then the full install.

**Predicted result** (MADR 0098): **`INIT=systemd-broken`, not `none`** —
`systemctl` is present in the Ubuntu image regardless of the `[boot]` setting,
so `detect_init` returns before reaching `none`. The output should then contain
*both* the `su`/`pam_systemd` paragraph (`scripts/install.sh:413`) and the
correct `/etc/wsl.conf` remedy (`:421`). Capture the whole block verbatim: if
the prediction holds, the 0097-PLAN expectation column is wrong and the `su`
advisory should be suppressed when `ENVIRONMENT` is `wsl1`/`wsl2`.

**7.5 — Row 3: WSL1.**

```powershell
wsl --set-version Ubuntu 1
wsl -l -v                      # must show VERSION 1
wsl -d Ubuntu -- sh -c 'cat /proc/sys/kernel/osrelease'   # expect ...-Microsoft
wsl -d Ubuntu -- sh -c 'curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh'
```

Expect `env=wsl1` (matched by the `*Microsoft*` case at
`scripts/install.sh:90`) and the WSL1 advisory "upgrade the distro to WSL2 for
background operation". Record the `INIT` actually produced.

**7.6 — Terminate immediately.** Do not leave this host running while writing
up results.

```sh
aws ec2 terminate-instances --instance-ids "$W"
```

**Exit criterion:** all three WSL rows recorded with verbatim `env`/`init`/
advisory text; instance terminated.

---

### Phase 8 — Record and tear down

**8.1 — Update the 0097-PLAN matrix.** Replace each ⬜ with ✅ plus the host,
the image id, and the measured result — matching the style of the existing ✅
rows. Where a row was re-scoped or proved inconclusive (SELinux not enforcing,
`sysvinit` unreachable), say so in the Result column rather than marking it
done.

**8.2 — Raise findings.** Each defect gets one entry with the host, the exact
command, and the verbatim output. Expected candidates, from the code reading
in MADR 0098 §Unreachable rows:

* WSL/`sysvinit` hosts misdiagnosed as a `su` problem.
* `--with-relay-service` silently ignored on non-systemd backends.
* `--force-service` missing from 0097-PLAN §3.2 and `ops-linux-install.md`.
* 0097-PLAN §3.3 lists `uname mkdir mv chmod grep sed` as preflight
  requirements; `main()` actually checks `awk` and `grep`.

**8.3 — Confirm the evidence survived the hosts.**

```sh
find "$MC_EVID" -type f | sort
for d in "$MC_EVID"/*/; do
  printf '%-26s %s files\n' "$(basename "$d")" "$(find "$d" -type f | wc -l)"
done
```

Any role directory with zero files means that row was not actually measured,
whatever the notes say. Resolve it before teardown — after teardown it cannot
be resolved at all.

**8.4 — Tear down everything.**

```sh
aws ec2 terminate-instances --instance-ids $(aws ec2 describe-instances \
  --filters "Name=tag:$MC_TAG,Values=$MC_TAGV" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' --output text)
aws ec2 wait instance-terminated --instance-ids …
aws ec2 delete-security-group --group-id "$MC_SG"
aws ec2 delete-key-pair --key-name mc0098
aws s3 rb "s3://$B" --force
rm -f "$MC_KEY" "$MC_KEY.pub"

# C2 means no droplet should ever have been created. Assert it anyway —
# this is the check that catches a step run from an older copy of this plan.
doctl compute droplet list --tag-name mcremote-test --format ID --no-header   # expect empty
```

The security group can only be deleted after every instance using it is
terminated — hence the `wait`.

**Exit criterion:** every assertion in §Verification returns empty.

## Verification

### Per-row acceptance

A row counts as executed only with **captured output**, not a recollection.
For each: the `--verbose` line `arch=… env=… init=… (pid1=…)`, the `service:`
summary line, exit codes for install / re-run / uninstall, and
`mcremote version`.

| Row | Pass condition |
|---|---|
| 1 WSL2 + systemd | `env=wsl2`, `INIT=systemd-user`, `supervised+boot`, `Linger=yes` inside WSL |
| 2 WSL2 − systemd | classification and full advisory text captured verbatim; prediction (`systemd-broken`) confirmed or refuted |
| 3 WSL1 | `env=wsl1`; WSL1 advisory printed; exit 0 |
| 4 Rocky 9 | `getenforce`=`Enforcing`, daemon active, `Linger=yes`, `ausearch` output recorded |
| 4b root login | same host, real root SSH session, `XDG_RUNTIME_DIR=/run/user/0` |
| 5 `--with-relay-service` | both units exist and are `active` on a virgin host |
| 6 s6 | `INIT=s6`, run script `0755`, `supervised-session`, daemon running |
| 7 openrc-user | `INIT` recorded verbatim; open question 2 answered |
| 8a openrc-system | `SERVICE_RESULT=none`, exit **0**, background command printed |
| 8b sysvinit | classification + advisory captured; finding raised if misdiagnosed |
| 9 version pin | `0.13.3` installs `0.13.3.1`; `0.12.0` exits 2 and changes nothing |
| 10 wget fallback | install succeeds on a host with no `curl`; checksum passes |
| 11 checksum failure | exit 2, `Nothing was installed.`, prior version still active, no `.mcinstall.*` left |
| 12 arm64 | ELF `ARM aarch64`, daemon active on Graviton |

### Cross-cutting, on every Linux host

```sh
mcremote version                 # equals RESOLVED_VER from the installer log
pgrep -u "$(id -u)" mcremote     # empty after --uninstall
ls -a ~/.local/bin               # no .mcinstall.* residue after any run
```

Plus, once on the workstation (unchanged from 0097-PLAN):

```sh
sh -n scripts/install.sh
shellcheck -s sh scripts/install.sh
tail -1 scripts/install.sh       # exactly: main "$@"
```

### Teardown assertions (all must return empty)

```sh
aws ec2 describe-instances \
  --filters "Name=tag:mcremote-test,Values=0098" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' --output text
aws ec2 describe-volumes --filters "Name=tag:mcremote-test,Values=0098" \
  --query 'Volumes[].VolumeId' --output text
aws ec2 describe-security-groups --filters "Name=group-name,Values=mc0098-ssh" \
  --query 'SecurityGroups[].GroupId' --output text
aws ec2 describe-key-pairs --key-names mc0098 2>&1 | grep -q NotFound && echo gone
aws s3api list-buckets --query 'Buckets[?starts_with(Name,`mcremote-tamper-`)].Name' --output text
doctl compute droplet list --tag-name mcremote-test --format ID --no-header
```

**Next day:** Cost Explorer filtered on `mcremote-test=0098`. This is the only
assertion that catches a resource created outside the tagging convention, so it
is not optional.

## Rollback

No production system is touched, so rollback is resource deletion plus document
revert.

| Risk | Trigger | Action |
|---|---|---|
| Host unreachable / cloud-init failed | no SSH within 5 min | terminate and relaunch; do not debug a disposable host |
| Spend higher than expected | Cost Explorer > $5 for the day | run Phase 8.3 immediately; investigate afterwards |
| Windows host wedged mid-WSL | `wsl --install` fails twice | terminate; record the row as blocked with the error text |
| Public S3 bucket left exposed | teardown assertion non-empty | `aws s3 rb --force`; the randomised name limits exposure to the window |
| Bad edits to 0097-PLAN | review | `git checkout -- docs/0097-PLAN-linux-curl-installer.md` |

**Irreversible boundary:** none. No published artifact is rewritten, no tag
moved, no shared VPC state (route tables, IGW, existing security groups)
modified, and no code changed.

## Task Checklist

**Phase 0 — setup**

- [ ] Session environment exported
- [ ] `mc0098` ed25519 key generated and imported, tagged
- [ ] `mc0098-ssh` security group created, limited to workstation `/32`, tagged
- [ ] `deadman.yml` written; `$MC_EVID` created
- [ ] All six `latest/download` assets return `200`

**Phase 1 — Alpine amd64**

- [ ] Host launched via `mc_cycle`, tagged, reachable — and no other host running
- [ ] Baseline probe: `curl` absent, `wget` present, `rc-service --user` exit recorded
- [ ] Row 10 — install via `wget -qO- … | sh`, checksum passes
- [ ] Row 7/8a — `INIT` recorded verbatim; open question 2 answered
- [ ] Row 6 — `apk add s6`, re-run, `INIT=s6`, run script `0755`, daemon up
- [ ] S4 re-run idempotent; S5 uninstall leaves nothing, no surviving process

**Phase 2 — arm64**

- [ ] Graviton Ubuntu: `arch=arm64`, `supervised+boot`, `Linger=yes`
- [ ] Alpine aarch64: musl + arm64 + `wget` in one, version `0.13.4.1`
- [ ] `file` confirms `ELF … ARM aarch64` on both

**Phase 3 — SELinux**

- [ ] Rocky 9: `getenforce` = `Enforcing` **before** install
- [ ] Rocky 9: daemon active, `Linger=yes`, `ausearch` captured
- [ ] Root SSH enabled on the Rocky host; run as `root@` with a real logind session
- [ ] Root model: `XDG_RUNTIME_DIR=/run/user/0`, `Linger=yes`, result recorded
- [ ] 0097 open question 1 resolved

**Phase 4 — relay + pin**

- [ ] Virgin host confirmed (no `~/.config/systemd/user/mcremote.service`)
- [ ] Row 5 — both units created and active, both lingering
- [ ] Re-run confirms `--with-relay-service` ignored on the upgrade path
- [ ] Row 9 — `0.13.3` positive, `0.12.0` negative (exit 2, install unchanged), back to latest

**Phase 5 — tamper mirror**

- [ ] Bucket created, tagged, public-read, dot-free name
- [ ] Genuine release mirrored; one byte flipped
- [ ] Exit 2, `Nothing was installed.`, prior version still active, no residue
- [ ] Control case with clean mirror installs successfully

**Phase 6 — sysvinit**

- [ ] Debian 13 **EC2 instance** converted with `sysvinit-core`, rebooted and reachable
- [ ] `/proc/1/comm`, `command -v systemctl`, classification captured
- [ ] Advisory text captured verbatim; finding raised if misdiagnosed

**Phase 7 — WSL (last)**

- [ ] Instance launched with `NestedVirtualization=enabled`; CPU options verified
- [ ] SSH reachable via user-data-installed OpenSSH
- [ ] WSL2 installed, `wsl -l -v` shows VERSION 2
- [ ] Row 1 — systemd enabled: `systemd-user`, `supervised+boot`, linger inside WSL
- [ ] Row 2 — systemd disabled: classification + both advisories captured
- [ ] Row 3 — WSL1: `env=wsl1`, WSL1 advisory printed
- [ ] Instance terminated immediately

**Phase 8 — record and tear down**

- [ ] Evidence collected from every host **before** termination; no empty role directory
- [ ] 0097-PLAN matrix updated; no ⬜ left unexplained
- [ ] Findings written up with verbatim output
- [ ] All AWS resources terminated/deleted; droplet list confirmed empty (none expected)
- [ ] Every teardown assertion returns empty
- [ ] Local throwaway key removed
- [ ] Next-day Cost Explorer check on `mcremote-test=0098`
- [ ] MADR 0098 moved to `accepted`
