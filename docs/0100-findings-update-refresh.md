<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# 0100 Phase 0 — host confirmation of the update-refresh findings

Executed 2026-08-18 on **wonder** (Ubuntu 26.04 LTS, x86_64, systemd 259,
`mcremote` 0.13.7.1 / `6b7b470`), per
[0100-PLAN](0100-PLAN-update-unit-refresh-and-daemon-reload.md) Phase 0.

Read-only except where stated. No live service was stopped, started, or
rewritten: F2 used a throwaway `mc0100probe.service`, F3 used an isolated copy
of `mcrelay` in `/tmp`, and both were removed afterwards. Host verified
unchanged at the end — same unit directory, `mcremote.service` still `active`,
`NeedDaemonReload=no`, no leftover files.

## Results

| Item | Claim under test | Result |
|---|---|---|
| **F2** | a restart without `daemon-reload` runs the manager's cached definition | ✅ confirmed |
| **F3** | `update` fails, and rolls back a good swap, where no unit is installed | ✅ confirmed |
| **P0.3** | `<unit>.service.prev` in the unit directory is inert | ✅ confirmed |
| **F4** *(new)* | a re-render recomputes environment-derived values from the caller's process | ⚠️ confirmed — **changes the design** |

## F2 — a restart without `daemon-reload` uses the stale definition

Scratch unit installed, started, then edited on disk with no reload — exactly
what `update` does today:

    1. loaded Description after install : 0100 probe v1
    2. NeedDaemonReload after edit      : yes
    3. restart WITHOUT daemon-reload:
         Warning: The unit file, source configuration file or drop-ins of
         mc0100probe.service changed on disk. Run 'systemctl --user
         daemon-reload' to reload units.
       loaded Description now           : 0100 probe v1
       on-disk Description              : 0100 probe v2
    4. restart WITH daemon-reload:
       loaded Description now           : 0100 probe v2

The restart *succeeds* and runs the old definition. That is the failure mode:
not an error, a silent no-op.

`systemctl --user show -p NeedDaemonReload --value <unit>` answers `yes`/`no`
correctly on systemd 259, so it is usable for reporting. The design still
reloads unconditionally rather than gating on it.

## F3 — `update` rolls back a successful swap where no unit exists

`mcrelay.service` is `LoadState=not-found` on this host, so an isolated copy of
the binary reproduces the case without touching anything live:

    $ cp ~/.local/bin/mcrelay /tmp/mc0100/f3/mcrelay
    $ /tmp/mc0100/f3/mcrelay update --force --yes
    latest release: v0.13.7 (base 0.13.7)
    local version:  0.13.7.1
    downloading mcrelay-linux-amd64-0.13.7.1 …
    stopping mcrelay service
    stop: … exit status 5 (Failed to stop mcrelay.service: Unit mcrelay.service
          not loaded.) (continuing)
    starting mcrelay service
    restored previous binary from .prev
    start service: … exit status 5 (Failed to start mcrelay.service: Unit
          mcrelay.service not found.)
    error: start service: …
    EXIT=1

Post-state: the binary is byte-identical to before —
`sha256 687d9e61…` both sides, version unchanged. The download and the swap both
succeeded; the update then undid them and reported failure. Every
`runit`/`s6`/`openrc` host and every plain binary install is in this state.

## P0.3 — the backup file is inert

    5. daemon-reload with mc0100probe.service.prev present:
       exit=0
       list-unit-files matching prev    : 0
       journal complaints               : 0

`<unit>.service.prev` beside the unit is safe. The design's backup location
stands; no move to `$XDG_STATE_HOME` is needed.

## F4 (new) — a re-render is not a pure function of the unit

This was not in the plan. It came out of comparing the installed unit against
what the **same binary version** renders now:

    $ diff ~/.config/systemd/user/mcremote.service <(mcremote setup-service --print-only)
    45c45
    < Environment=PATH=/opt/homebrew/bin:…/.local/flutter/bin:…/.local/go/bin:…
    ---
    > Environment=PATH=/opt/homebrew/bin:…/.local/flutter/bin:…/.cache/kilo/bin:…
    58c58,59
    < # Hardening (user-unit safe)
    ---
    > # Hardening (user-unit safe). On by default; set any of these to false
    > # in a drop-in to disable. Do not omit them from this template.

Two differences, from two different causes:

* The hardening comment is **genuine template drift** — the unit was written
  2026-08-15 by an older binary. This is the class of change a refresh exists to
  apply, and a byte comparison finds it correctly.
* The `PATH` line is **not** drift. `servicePathEnv` (`setup.go:804-833`) builds
  mcremote's PATH by prepending tool prefixes to `os.Getenv("PATH")`, so the
  rendered unit is a function of *whoever runs the render*:

      caller PATH = ambient    -> Environment=PATH=/opt/homebrew/bin:…/.local/share/mise/shims:…
      caller PATH=/usr/bin:/bin -> Environment=PATH=/usr/local/bin:/opt/homebrew/bin:…

  `mcrelay` is immune — its PATH is a fixed closed set (MADR 0091 D1):

      Environment=PATH=/home/mac/.local/bin:/usr/local/bin:/usr/bin:/bin

  identical under both callers.

`XDG_RUNTIME_DIR` behaves the same way, and worse — it disappears entirely:

    $ diff a.service <(env -u XDG_RUNTIME_DIR mcremote setup-service --print-only)
    50d49
    < Environment=XDG_RUNTIME_DIR=/run/user/1000

### Why this changes the design

The plan as written re-rendered from recovered `Options` alone. On this host
that would have:

1. reported `refreshed` on every single update, because the PATH line always
   differs from whatever shell last ran `setup-service`; and
2. **rewritten the daemon's `PATH` to the environment of the updating process** —
   an ssh session, a cron job, or a systemd timer — and dropped
   `XDG_RUNTIME_DIR` when the caller had none.

That is precisely the silent-configuration-loss this record's D2 forbids, caused
by the fix rather than prevented by it. wonder happens to pin PATH in a drop-in
(`mcremote.service.d/path.conf`, whose own comment warns *"If `mcremote
setup-service` ever changes the generated PATH, re-derive this line"*), so the
running daemon would have survived — but a host without that drop-in would not.

**Remedy, folded into MADR 0100 and its plan:** recovery pins the
environment-derived values — `HOME`, `USER`, `LOGNAME`, `PATH`, and every
`XDG_*` — from the installed definition, and the re-render reuses them verbatim
instead of recomputing them. A refresh then changes exactly what the template
changed and nothing else. Where the current code would compute PATH entries the
pinned line lacks, the refresh reports it as a warning and leaves the value
alone; `setup-service --force` remains the way to re-derive.

## Commands

For reproduction. The F2 probe was a throwaway script around a scratch unit —
not committed; its full output is quoted above.

    ssh wonder 'systemctl --user show -p LoadState --value mcrelay.service'
    ssh wonder 'diff ~/.config/systemd/user/mcremote.service \
                     <(mcremote setup-service --print-only)'
    ssh wonder 'env PATH=/usr/bin:/bin ~/.local/bin/mcremote setup-service --print-only'
    ssh wonder 'env -u XDG_RUNTIME_DIR ~/.local/bin/mcremote setup-service --print-only'

## Phase 7 — host verification of the implemented fix

Executed 2026-08-18 on **wonder**, against linux/amd64 binaries built from the
implementation branch (`0.13.7.99.g0100test`). All test artifacts (a scratch
`mc0100p7` binary dir, a scratch `mcrelay` unit, a scratch XDG config tree, and
the `mcrelay` config directory `setup-service` created) were removed
afterward. Final state matches the Phase 0 snapshot exactly: `mcremote.service`
unchanged and `active`, `NeedDaemonReload=no`, `mcrelay.service` `not-found`.

| # | Claim | Result |
|---|---|---|
| C1/C9 | A unit carrying the 0099 F4a directives is rewritten and starts | ✅ |
| C2 | Baked `--listen-port` / `--env` and the `0600` mode survive a refresh | ✅ |
| C3 | A hand-edited unit (`ExecStartPre=`) is left byte-identical | ✅ |
| C4 | `daemon-reload` runs before the restart | ✅ (unit active, `NRestarts=0`) |
| C6 | A refresh backs up the previous unit as `.prev` | ✅ |
| C7 / F3 | `update` on a host with no unit succeeds and does not roll back | ✅ |
| C10 | The environment block is pinned under a hostile caller PATH | ✅ |

### C1/C9 — the 0099 F4a crash loop, cleared

A real `mcrelay setup-service` install was deliberately regressed to the old
template shape, then cleared by `--refresh`:

    == 2. inject the 0099 F4a directives ==
       ActiveState=activating SubState=auto-restart NRestarts=0
    Failed to drop capabilities: Operation not permitted
    Failed at step CAPABILITIES spawning /home/mac/.local/bin/mcrelay: Operation not permitted

    == 3. setup-service --refresh ==
    service definition refreshed: …/mcrelay.service (previous kept at …mcrelay.service.prev)
    warning: the definition runs /home/mac/.local/bin/mcrelay, not this binary (…)
       directive lines in the new unit : 0
       directive lines in .prev        : 2

    == 4. restart on the refreshed unit ==
       ActiveState=active SubState=running NRestarts=0
       mcremote (untouched): active

(The initial grep for "PrivateDevices\|RestrictNamespaces" matched 2 lines in
the refreshed unit — both in the template's own explanatory comment, not a
directive. `grep -cE '^(PrivateDevices|RestrictNamespaces)='` correctly reports
0; recorded here so the raw count isn't misread as a near-miss.)

The foreign-binary warning fired correctly: the refresh was invoked from
`/tmp/mc0100p7/mcrelay`, but the installed unit's `ExecStart=` still pointed at
`~/.local/bin/mcrelay` (the real install), and the refresh named the mismatch
instead of silently rewriting to the wrong path.

### C2 — baked options and secrets survive

Using a scratch `XDG_CONFIG_HOME` so the real unit was never touched:

    == 4. baked options + --env survive ==
       mode after setup      : 600
    service definition refreshed: …
       --listen-port 9099 kept : 1
       Environment=K=V kept    : 1
       stale directive gone    : 0
       mode after refresh      : 600

### C3 — a hand-edited unit is kept, byte-for-byte

    == 5. hand-edited unit is kept ==
    service definition kept: … — carries ExecStartPre=, which setup-service
    never writes; refresh it with: mcremote setup-service --force
       file byte-identical: yes

### C7 / F3 — the original bug, on the fixed binary

Same reproduction as Phase 0 §F3, now against the 0100 build:

    latest release: v0.13.7 (base 0.13.7)
    local version:  0.13.7.99.g0100test
    downloading mcrelay-linux-amd64-0.13.7.1 …
    service definition refresh failed: … setup-service --refresh: exit status 1
      (error: unknown flag: --refresh) (continuing)
    binary installed at /tmp/mc0100p7/f3/mcrelay (restart the service yourself if needed)
    reinstalled mcrelay at v0.13.7
    EXIT=0

The refresh step failed as expected — the *downloaded* release binary
(v0.13.7.1) predates `--refresh`, exactly the downgrade path Phase 4's tests
model — and was correctly stepped over. No unit existed, so `HealStart` was
gated off, `Start` was never called, and the update that used to fail now
exits 0 with the binary swapped (sha256 changed `51c5003a…` → `687d9e61…`,
version now `0.13.7.1`).

### C10 — environment pinning under a hostile caller

    == 7. env pinning: real mcrelay unit, hostile caller environment ==
    (env -i HOME=... USER=... PATH=/usr/bin:/bin, no XDG_RUNTIME_DIR)
    service definition unchanged: …/mcrelay.service
       PATH line unchanged: yes
       XDG_RUNTIME_DIR lines: 1
       mcrelay still active: active

Confirms the Phase 0 F4 fix holds against the actual defect it was written for:
a refresh run from a stripped environment neither rewrote `PATH` nor dropped
`XDG_RUNTIME_DIR`.

## Result

All Phase 7 acceptance criteria observed. MADR 0100 status → `accepted`.
