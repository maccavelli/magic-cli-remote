# 0099 re-verification results (v0.13.5 / v0.13.6)

Executed 2026-08-18 against **published release artifacts**, one host at a time,
per [0099-PLAN](0099-PLAN-installer-service-state-verification.md) Phase 7.
All resources torn down; every teardown assertion returned empty.

The decisive output is quoted inline below. Raw per-host logs are deliberately
**not** committed — they are working artifacts, not documentation, and a commit
message in this repo's history references a `docs/0099-evidence-reverification/`
directory that was removed for that reason.

## Results

| Finding | Fixed in | Host | Result |
|---|---|---|---|
| **F1** s6 orphaned daemon | v0.13.5 | Alpine 3.23.5 + s6, `t3.small` | ✅ |
| **F4a** relay `218/CAPABILITIES` | v0.13.5 | virgin Ubuntu 26.04, `t3.small` | ✅ |
| **F4b** relay config refused | v0.13.5 | same host | ✅ |
| **F5** false "running" | v0.13.5 **+ v0.13.6** | same host, fresh user | ✅ — see below |
| **F6** WSL `su` advisory | v0.13.5 | WS2025 `m8i.xlarge`, WSL 2.7.11 | ✅ |

### F1 — daemon is stopped before removal

Identified by `/proc/<pid>/exe`, not by `pgrep -f` (which matches the shell
running the test — a false positive that cost two attempts):

    fresh install  -> s6-svstat: up (pid 2975 pgid 2975)
                      /proc/2975/exe -> /home/alpine/.local/bin/mcremote
    --uninstall    -> exit 0, no process with that exe, no (deleted) inode

v0.13.4 left PID 2515 running on a deleted binary here.

### F4a + F4b — `--with-relay-service` works for the first time

Virgin host, no prior unit:

    systemctl --user is-active mcremote mcrelay   -> active / active
    mcrelay: ActiveState=active SubState=running MainPID=1375
             NRestarts=0 Result=success
    journal 218/CAPABILITIES matches: 0
    journal "plaintext listen" matches: 0
    LISTEN 127.0.0.1:8443 mcrelay / 127.0.0.1:7531 mcremote
    curl 127.0.0.1:8443/healthz -> {"ok":true}

### F5 — the gate had a hole, and this sweep found it

**v0.13.5 was not sufficient.** With a second user contending for
`127.0.0.1:7531`, the shipped gate still reported success:

    service:  running, and enabled at boot     exit 0
    …while NRestarts climbed 7 -> 9 -> 10, Result=exit-code,
      journal: bind: address already in use

Root cause: with `Type=simple`, systemd reports `ActiveState=active` the moment
the process is **forked**, before it can fail. `svc_settle` returned on the
first `active` sighting and caught that window.

Fixed in v0.13.6 (`cef8a29`): `active` must be sustained across three
consecutive samples, plus two independent loop signals — `NRestarts` above the
baseline captured at entry, and `SubState=auto-restart` with `NRestarts >= 2`.
Re-tested on a clean fresh-user scenario with the port held:

    >>> EXIT = 3
    warning: mcremote is not running after 15s
    service:  FAILED to start — the binaries are installed and usable
      journalctl --user -u mcremote -n 20 --no-pager
    actual unit: activating / auto-restart / NRestarts=1 / Result=exit-code

Regression case 23 reproduces it offline and fails against v0.13.5.

### F6 — WSL hosts get only the advice that applies

WSL2 with `[boot] systemd=false`, then WSL1, both on WSL 2.7.11:

    PID1=init(Ubuntu)   systemctl present   XDG=/mnt/wslg/runtime-dir

`XDG_RUNTIME_DIR` is **set**, confirming the removed advisory's stated cause
("leaves XDG_RUNTIME_DIR unset") was factually false on these hosts.

    WSL2 -> only the /etc/wsl.conf remedy
    WSL1 -> only "upgrade the distro to WSL2"
    grep -c pam_systemd across both -> 0

Classification is `systemd-broken` in both, not `none` as 0097-PLAN expected —
that expectation was corrected during the 0098 sweep.

## Method notes for the next run

* **`pgrep -f` and `pkill -f` are unusable here.** Both match the shell running
  the test script, because the pattern appears in its command line. `pkill -f
  s6-svscan` killed the SSH session. Identify daemons by `/proc/<pid>/exe`.
* **SSM cannot drive WSL.** `AWS-RunPowerShellScript` executes as
  `NT AUTHORITY\SYSTEM`, and WSL refuses to run as local system
  (`WSL_E_LOCAL_SYSTEM_NOT_SUPPORTED`). SSM is still the right tool for
  enabling features and repairing sshd, but the WSL steps need a real user
  session over SSH.
* **Windows user-data ran only partway** this time: `Add-WindowsCapability`
  succeeded but `Start-Service sshd` and both DISM calls did not, leaving port
  22 closed. SSM recovered it — which is why the instance profile is attached.
* `wsl --install` on Server 2025 still requires the MSI
  (`WSL/releases/.../wsl.2.7.11.0.x64.msi`); the inbox stub cannot self-install.
