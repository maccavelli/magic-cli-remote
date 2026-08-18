# Findings — sweep 0098, Session A (Alpine amd64, 2026-08-18)

## F1 — `--uninstall` and upgrade leave an orphaned daemon on s6 hosts (HIGH)

`scripts/install.sh:svc_is_active()` probes an s6 service with:

    s6-svc -l "$S6_DIR/$1"

`-l` is **not a valid `s6-svc` option**. On s6 as shipped in Alpine 3.23.5 it
exits **100** with a usage error, so the probe can never return true.

Consequence: `svc_note_active()` never adds the product to `SVC_ACTIVE_LIST`,
so `svc_stop_if_running()` iterates an empty list and the running daemon is
never stopped — while the script still prints `stopping any running service…`
and exits **0**.

Measured on a real s6 host:

    $ pgrep -f "mcremote serve"      -> 2515
    $ sh install.sh --uninstall      -> exit 0, "stopping any running service…"
    $ ls ~/.local/bin/               -> empty (binaries removed)
    $ ps aux | grep mcremote         -> 2515 still running
    $ ls -l /proc/2515/exe
      /proc/2515/exe -> /home/alpine/.local/bin/mcremote (deleted)

The `(deleted)` inode is exactly the failure the code comments say the
mechanism exists to prevent:

> "a rename over a running executable leaves the process on its old inode, so a
> service we neither stop nor restart keeps executing the OLD code while the
> on-disk binary reports the new version. Silent staleness is worse than a crash."

Two distinct impacts:
  * **uninstall** — user is told it is gone; a daemon keeps serving from a
    deleted binary until reboot.
  * **upgrade** — binary replaced, daemon never cycled, so it keeps running old
    code while `mcremote version` reports the new one.

Correct tool is `s6-svstat`, confirmed working on the same host:

    $ s6-svstat ~/.local/share/s6/service/mcremote
    up (pid 2515 pgid 2515) 40 seconds

Scope: **s6 backend only.** The runit branch uses `sv status` and the systemd
branch uses `systemctl --user is-active`, both valid. 0097-PLAN listed s6 as
"untested — stubs only", which is precisely where this survived.

## F2 — `openrc-user` is unreachable on stock Alpine, but the backend is NOT dead code

0097 open question 2 asked whether to delete the `openrc-user` backend. Answer:
**do not delete it** — the probe fails for an environmental reason, not a
missing feature.

    $ rc-service --version                    -> OpenRC 0.63
    $ rc-service --help | grep user           -> "-U, --user   Run in user mode"
    $ rc-service --user --help                -> "XDG_RUNTIME_DIR unset."  exit 1
    $ XDG_RUNTIME_DIR=/tmp/x rc-service --user --help  -> exit 0

OpenRC 0.63 fully supports user mode. The probe fails only because stock Alpine
installs no elogind, so `XDG_RUNTIME_DIR` is never set for an SSH session. The
installer therefore falls through to `openrc-system` — which is arguably the
correct outcome (user services genuinely cannot work without the runtime dir),
but the diagnosis is indistinguishable from "OpenRC too old".

Note this is the *same* root cause as the `systemd-broken` classification. Both
hinge on `XDG_RUNTIME_DIR`.

## F3 — `subnet-7e5b3912` (us-east-1a) has a deny-all NACL (environment, not product)

Route table points at the IGW and the security group is correct, but NACL
`acl-4d8fe821` contains only deny entries in both directions. An instance there
boots, completes cloud-init, installs the key, passes both status checks, and is
still unreachable. Use `subnet-0fc17839ef9f6d906` (us-east-1c). Cost: one host.

## Confirmed working (no defect)

* **Row 10 — busybox `wget` fallback.** No `curl` on the host; `wget -qO- | sh`
  followed GitHub's redirect to `objects.githubusercontent.com` over TLS and the
  SHA-256 verified. exit 0, version `0.13.4.1`.
* **Row 8a — `openrc-system`.** `SERVICE_RESULT=none`, exit **0**, background
  `nohup` command printed, exactly as 0097-PLAN §4.F specifies.
* **Row 6 — s6 detection and supervision.** `INIT=s6`, run script `0755` at
  `~/.local/share/s6/service/mcremote/run`, `s6-supervise` running, daemon up,
  `supervised-session` reported with an honest "at boot: NOT configured".
* **Static binary on musl.** `ldd` reports "Not a valid dynamic program" —
  the `CGO_ENABLED=0 -tags netgo,osusergo` claim in ops-linux-install.md holds.
* **`--dry-run` writes nothing.**
* **Idempotent re-run** over an existing install: exit 0, binaries replaced.
* **Dead-man switch** (`sleep 10800; poweroff`) confirmed running via cloud-init
  on Alpine — the busybox-incompatible `shutdown -h +180` form would have failed
  silently here.

---

# Session B (arm64, SELinux, relay + pin) — 2026-08-18

## F4 — `--with-relay-service` produces a unit that can never start (HIGH)

Row 5 of the 0097 matrix ("untested — never created a relay service from
scratch"). On a **fresh, stock Ubuntu 26.04 amd64 host**, `--with-relay-service`
creates both units and exits **0**, reporting:

    service:  running, and enabled at boot (systemd user unit + linger)

`mcrelay` is in fact in a permanent crash loop (`Restart=always`, `RestartSec=5`).
There are **two independent failures**, stacked:

### F4a — 218/CAPABILITIES: the unit cannot even spawn

    mcrelay.service: Failed to drop capabilities: Operation not permitted
    mcrelay.service: Failed at step CAPABILITIES spawning .../mcrelay: Operation not permitted
    status=218/CAPABILITIES

Bisected with drop-ins on the live host:

| Override | Result |
|---|---|
| none (as generated) | `218/CAPABILITIES` |
| `PrivateDevices=false` alone | `218/CAPABILITIES` |
| `RestrictNamespaces=false` alone | `218/CAPABILITIES` |
| `MemoryDenyWriteExecute=false` alone | `218/CAPABILITIES` |
| `RestrictAddressFamilies=` alone | `218/CAPABILITIES` |
| **`PrivateDevices=false` + `RestrictNamespaces=false`** | **cleared** → `1/FAILURE` (F4b) |

Both are needed; neither suffices alone. Both are present **only** on
`mcrelay.service` — `mcremote.service` omits them, which is precisely why
mcremote works on the same host and mcrelay never can.

The template's own comment shows the failure mode was known:

> "ProtectClock/ProtectKernelModules/ProtectKernelLogs are deliberately absent:
> they imply capability-bounding-set changes a user manager cannot perform in
> container-restricted hosts (218/CAPABILITIES crash loop)."

`PrivateDevices=` implies `CapabilityBoundingSet=~CAP_MKNOD` and
`RestrictNamespaces=` likewise constrains capabilities, so both belong on that
exclusion list and are not on it. The MADR 0091 D4 comment records probing
`RestrictAddressFamilies` and `MemoryDenyWriteExecute` on a user unit — the two
directives that turn out **not** to be the problem. The two that are were
apparently never probed in user scope.

### F4b — the default generated config is refused by the binary

With capabilities resolved, the next start fails on the config
`mcrelay setup-service` just wrote:

    mcrelay starting version=0.13.4.1 listen=0.0.0.0:8443 tls=off
    error: plaintext listen on 0.0.0.0:8443 refused: set tls.mode=files|letsencrypt,
           bind a loopback address, or pass --allow-plaintext
    status=1/FAILURE

So even after F4a is fixed, `--with-relay-service` still yields a non-starting
service out of the box: it writes `listen=0.0.0.0:8443` with `tls=off`, a
combination the binary deliberately refuses.

**Net:** the `--with-relay-service` path has, as far as this sweep can tell,
never worked on a user unit. It reports success while delivering a service that
cannot run.

## F5 — the installer reports "running" without verifying the unit started (MEDIUM)

Root cause of why F4 is invisible, and independently reproduced twice.

`setup_service()` sets `SERVICE_RESULT="supervised+boot"` if
`mcremote setup-service` returns 0. `systemctl --user enable --now` returns 0
once the start job is *issued* — not once the unit is *running*. A unit that
immediately dies and enters `auto-restart` therefore still reports:

    service:  running, and enabled at boot (systemd user unit + linger)

Observed twice, from unrelated causes:

1. **mcrelay** (F4) — `ActiveState=activating SubState=auto-restart MainPID=0`.
2. **A second user on the same host** — installing as root while another user's
   daemon held `127.0.0.1:7531` gave
   `error: daemon: listen 127.0.0.1:7531: bind: address already in use`,
   `NRestarts=3`, exit 0, and the same "running" message. Confirmed as the sole
   cause: stopping the first daemon let the second recover unaided
   (`NRestarts=8` → `active`, `MainPID=5106`, listening).

Suggested remedy (out of scope here): after `setup-service`, poll
`systemctl --user is-active` for a few seconds and report `failed` or
`activating` honestly instead of assuming `supervised+boot`.

## Confirmed working

* **Row 12 — arm64 on Graviton3** (`c7g.medium`, Ubuntu 26.04). `arch=arm64`,
  `systemd-user`, `supervised+boot`, `Linger=yes`, daemon listening.
  `file`: `ELF 64-bit LSB executable, ARM aarch64, statically linked`.
  Upgrade path correct — service stopped, binary replaced, restarted, and
  `/proc/<pid>/exe` showed **no** `(deleted)`, in direct contrast to F1 on s6.
* **Row 12b — arm64 + musl + busybox wget** (`t4g.small`, Alpine 3.23.5
  aarch64). Install and uninstall clean; `ld-musl-aarch64.so.1: Not a valid
  dynamic program` confirms the static build on arm64 musl.
* **Row 4 — SELinux enforcing** (Rocky 9.8). `getenforce`=**Enforcing** before
  the run; `ausearch -m avc -ts recent` → **`<no matches>`**; dmesg clean;
  binary context `unconfined_u:object_r:gconf_home_t:s0`; unit active and
  **actually listening** on `127.0.0.1:7531`. **Retires 0097 open question 1** —
  a `systemd --user` unit running an unlabelled binary from `$HOME` works
  unmodified under enforcing SELinux. The `restorecon` guidance in
  ops-linux-install.md is not required (harmless as a fallback).
* **Row 4b — root login model.** Real root SSH session (not `sudo -i`):
  `XDG_RUNTIME_DIR=/run/user/0`, `INSTALL_DIR=/root/.local/bin`,
  `systemd-user`, `Linger=yes`, unit created. Path works; see F5 for the
  reporting defect it exposed.
* **Row 9 — version pinning.** `--version 0.13.3` → URL
  `releases/download/v0.13.3`, installs `0.13.3.1` (a downgrade, and it worked).
  `--version 0.12.0` (pre-alias release) → exit **2**, 404 on `SHA256SUMS`, the
  exact "releases before MADR 0097 do not carry the alias assets" guidance, and
  **the existing install left untouched at 0.13.3.1**. Back to latest →
  `0.13.4.1`.
* **`--with-relay-service` on the upgrade path** is ignored, as predicted from
  reading `setup_service()` — the early return fires when a unit already exists.

---

# Session C (tamper, sysvinit, WSL) — 2026-08-18

## F6 — WSL hosts are told to fix a problem they do not have (MEDIUM)

**0097-PLAN's expectation column is wrong**, and the advisory that fires instead
is actively misleading. Measured on Windows Server 2025, WSL 2.7.11, Ubuntu.

Both WSL rows classify as **`systemd-broken`**, not `none` — `detect_init`
returns on `have systemctl`, and Ubuntu-on-WSL ships `/usr/bin/systemctl`
regardless of `[boot] systemd=`.

    WSL2, systemd=false:  arch=amd64 env=wsl2 init=systemd-broken (pid1=init(Ubuntu))
    WSL1:                 arch=amd64 env=wsl1 init=systemd-broken (pid1=init(Ubuntu))

Both then print **two advisories back to back** — the wrong one first:

    systemctl is present but the user bus is unreachable.
    This usually means the session was entered with 'su', which skips
    pam_systemd and leaves XDG_RUNTIME_DIR unset. Reconnect with ssh,
    or use: machinectl shell root@

    WSL2 without systemd. Enable it by adding to /etc/wsl.conf: ...

The first paragraph is wrong in three separate ways on a WSL host:

1. The session was not entered with `su`.
2. `XDG_RUNTIME_DIR` is **not** unset — measured `/mnt/wslg/runtime-dir` on
   WSL2. The stated cause is factually false on the host it is shown to.
3. "Reconnect with ssh, or use `machinectl shell`" is not actionable advice in
   WSL.

Fix is small: suppress the `systemd-broken` `su` paragraph when `ENVIRONMENT` is
`wsl1`/`wsl2` (`scripts/install.sh:413` guarded against `:421`). Also correct
the 0097-PLAN expectation column from `none` to `systemd-broken`.

## F7 — Windows prerequisites the plan got wrong (method, not product)

1. **`ED25519 key pairs are not supported with Windows AMIs.`** `run-instances`
   rejects the launch outright — this is not, as 0098-PLAN assumed, only needed
   for password retrieval. A separate RSA key pair (`mc0098-rsa`) is mandatory
   for any Windows AMI even when the public key is injected via user data.
2. **The dead-man switch blocks the reboots WSL needs.** `shutdown -s -t 10800`
   makes every later `shutdown -r` fail with
   `A system shutdown has already been scheduled.(1190)`. Each reboot must be
   `shutdown -a` then `shutdown -r`; `<persist>true</persist>` re-arms the timer
   on the next boot.
3. **`Enable-WindowsOptionalFeature` over SSH silently does nothing.** It
   returned no output and left both features `Disabled`. `dism /online
   /enable-feature` worked and reported "The operation completed successfully."
   Verify with `dism /online /get-featureinfo`, never by absence of an error.
4. **`wsl --install` does not work on Windows Server 2025 out of the box.** The
   System32 `wsl.exe` is a stub that answers every invocation with "The Windows
   Subsystem for Linux is not installed. You can install by running
   'wsl.exe --install'." — including that one. Even with both optional features
   enabled and a reboot. The working path is to install the MSI directly:
   `https://github.com/microsoft/WSL/releases/download/2.7.11/wsl.2.7.11.0.x64.msi`
   via `msiexec /i ... /quiet`, after which `wsl --version` reports 2.7.11.0.
5. **Modern WSL enables systemd by default.** A freshly installed Ubuntu under
   WSL 2.7.11 boots with systemd as PID 1 and no `/etc/wsl.conf`. Testing the
   "WSL2 without systemd" case now requires writing `[boot] systemd=false`
   explicitly; it is no longer the default state.

## Confirmed working

* **Row 11 — checksum failure over real HTTPS.** Against an S3 mirror
  (`https://<bucket>.s3.amazonaws.com`, valid wildcard cert, so curl's
  `--proto '=https' --tlsv1.2` accepted it) serving a binary with one byte
  flipped at offset 1024, run on a host with a **working install already
  present**:

      exit=2
      error: checksum mismatch for mcremote
        expected 80adcd856df1d4e578815d6fd5bf2e747aedb34bbe6010f3a08bf080367ea254
        got      28daa49202f6077f13ba5a46acb75e70cad56329e4920f80e65ae586b022f02a
      Nothing was installed.

  Post-conditions all held: binary digest **unchanged**, `mcremote version`
  still `0.13.4.1`, service still `active`, no `.mcinstall.*` residue.
  Control case against the same mirror with the clean binary: exit 0, installed.
  So the failure was the tampering, not the mirror.

* **Rows 1 and 3 — WSL2-with-systemd and WSL1 detection.**
  Row 1: `env=wsl2`, `init=systemd-user`, `supervised+boot`, unit created,
  `systemctl --user is-active` → `active`, `Linger=yes`, daemon running as
  PID 321 **inside WSL**. Linger under WSL's systemd works.
  Row 3: `env=wsl1` correctly matched from osrelease `4.4.0-26100-Microsoft`,
  and the WSL1-specific "upgrade to WSL2" advisory printed. Exit 0.

* **Nested virtualization on a virtual EC2 instance** — the corrected premise of
  MADR 0098, validated end to end. `m8i.xlarge` with
  `CpuOptions.NestedVirtualization=enabled`, `HypervisorPresent=True`, WSL2
  running kernel `6.18.33.2-microsoft-standard-WSL2`. No bare metal, ~$0.40/hr.
