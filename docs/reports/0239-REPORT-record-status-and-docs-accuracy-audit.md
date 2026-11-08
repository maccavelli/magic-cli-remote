# Record status and docs accuracy audit, 2026-11

<!-- markdownlint-disable MD013 -->

Started 2026-11-08, at `master` `cb68b744`. This report records what the audit of
[0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-MADR-audit-record-statuses-and-docs-accuracy-2026-11.md)
found and corrected. It decides nothing: each correction cites its evidence in the record it changed, and here.
Its sections are filled phase by phase, as
[0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md](../decisions/0239-PLAN-audit-record-statuses-and-docs-accuracy-2026-11.md)
runs. This version covers P1, the open records, and P2, the `completed` spelling.

## Rules

The rules of [0200-REPORT-record-status-audit.md](0200-REPORT-record-status-audit.md), with the owner's answers of
2026-11-08:

- **MADR `proposed` → `accepted`** when its PLAN ran and the change is on `master`, or the owner's approval is on
  record. Where the acceptance was never written down, it is marked accepted with a note that it is inferred (Q2:
  "Keep the 0200 rule").
- **PLAN → `complete`** only when the code, or the PLAN's own record, shows every step landed. **PLAN →
  `in-progress`** when some did, with what remains named.
- **Additive only.** A record with front matter has its `status` and `date` changed and gains a closing "Status note
  — 2026-11-08". Nothing earlier is rewritten. Every note links here.
- A record that is open on purpose and reads accurately is left as it is.

## P1 — the open records

### Method

- A script listed every MADR and PLAN whose front-matter status is `proposed` or `in-progress`: 53 files, the 51 the
  MADR counted plus 0239's own pair. For each, it gathered the last headings, the last "Not done" text, and the
  commits since 2026-10-18 that touched it or named it.
- Eighteen records were checked by one read-only helper pass, against the code, git history, citing records and CI.
  Its load-bearing claims were spot-checked against the files before any edit: 0211's recorded approval, `ci.yml`'s
  path filters, the repository's rulesets, 0208's "stays `proposed`" sentence, `internal/config/load.go:310-318`, and
  0217's remaining dotfiles step.
- The rest were checked directly: `grep` for the symbols, files, keys and targets each names, and `git log` since
  0200's audit.
- No test was run, and no provider quota was spent.

### Corrected

| Record | Change | Evidence |
| --- | --- | --- |
| 0211-MADR | `proposed` → `accepted` | its PLAN records the owner's approval; `88d37d1c` |
| 0090-MADR | `proposed` → `accepted` (inferred) | D1–D5 with the template parity test; D6 through 0201 D23 (`84011354`) |
| 0208-MADR | `proposed` → `accepted` | the owner's answer, 2026-11-08 ("Accept it now"); "Proceed" already on record; PLAN P1–P8 run |
| 0203-PLAN | `in-progress` → `complete` | V3: `de29a288` ran only Docs (`42490295468`); `74ad525e` ran CI, Docs and the ledger. V4: the `v0.25.1` tag ran full CI (`42493767398`). V5: scheduled ledger `42393302902` |
| 0089-MADR | `proposed` kept; **D1 superseded in part** by 0126 F3 | the owner's answer, 2026-11-08; `84876c6b` removed `WAKE_LOCK`; the manifest strips it today |
| 0130-MADR | `proposed` → `accepted` (inferred) | its decision, option A, was to investigate first; its PLAN is `complete` and its 2026-09-02 amendment records the investigation and the parking. Found by the Confirmation 2 check; 0200 had listed it as parked |

### Status kept; stale text corrected by a note

| Record | Correction |
| --- | --- |
| 0090-PLAN | tasks 3.1 and 3.2 landed through 0201 D23; task 4.2 no longer applies (0208 P8). Remaining: `config.md`'s `MCREMOTE_LIMITS_*` rows (3.3) and `README.md`'s kilo version, 7.4.20 against the 7.8.18 pin (4.1). Both fall in this audit's guide phases |
| 0143-PLAN | `Go (linux/arm64)` already retries through the shared matrix step (`ci.yml:265`); `Go (test; build on tag)` is now `Go (test)`. Remaining: Phase 3 for `Go (test)` and both Flutter jobs, Phase 4, Phase 5 |
| 0163-PLAN | only the manual phone pass remains; the codex pin has since moved under 0226 and 0236 |
| 0169-PLAN | remaining: D7's git on the Linux test host and WSL, and D3's Flutter lockstep (magic-git's pair). A6 is overtaken by 0217 D2 |
| 0217-PLAN | remaining: the owner's dotfiles commit on the Linux test host (owner: "Not yet", 2026-11-08). A5 reads as amended by D4's siblings-only rule (`spf13/pflag` held) |

### Waiting on the owner

- **0227-MADR / PLAN** (`proposed` / `in-progress`): Phases 1–3 are in `.claude/settings.json`, `opencode.json` and
  `.gobble/settings.yaml` (`254279ac`). Phase 4 waits on the owner's manual check that a read-only turn no longer
  prompts in Claude Code, OpenCode and gobble, and that `git add` still does. The owner asked for the procedure on
  2026-11-08; the result decides both statuses.

### Checked and left as they are

Each reads accurately against the tree:

- **Not implemented, proposals:** 0113, 0117, 0121, 0131, 0135, 0146, 0157 and 0181 (MADR and PLAN where both exist);
  0213 (MADR and PLAN), 0221, 0228-PLAN, 0229 and 0233. Spot checks: no `LoadPurpose` (0117), `closeMatching` still
  purges in place of closing (0146), no rolling-log sink (0157), no application message limit (0181), no
  golangci-lint target (0131), no `OverrideRecordDir` (0233), `SkillInfo` unchanged (0229), no `ListPlugins` (0228).
- **Open by the record's own rule:** 0095-MADR (acceptance held for a device check).
- **`in-progress` and accurate:** 0089-PLAN (Phases 0 and 3–5), 0109-PLAN expand (P8–P17 not started), 0123, 0125,
  0132, 0133, 0134, 0136, 0145, 0159 (P12, `--env-file`), 0208-PLAN (P9, P10), 0216 (P3–P7), 0219 (V3, waiting on
  `/vN` support in go-shared).
- **`proposed` and accurate:** 0109-PLAN retire (waiting on the owner), 0179-PLAN (P0–P6, amended 2026-11-08), 0205-PLAN
  (not executed), and 0239's own pair.

### Findings for the owner

- **0205's tag ruleset was never created.** 0205-MADR chose N2, "do not sign; add a tag ruleset that limits `v*` tags
  to the owner". The repository has one ruleset, `Protect` (`20181285`), which targets branches. The PLAN's
  execution record is empty. Its status, `proposed`, is accurate; the decision's one action is not done.
- **Eight proposals have been idle since August or September 2026:** 0113, 0117, 0121, 0131, 0135, 0146, 0157 and
  0181. None was started. Each could stay as backlog, or be closed as `rejected` or deferred by the owner.
- **0213 is partly overtaken.** Its markdownlint-cli2 pin (0.23.2) is behind 0217 D6's 0.24.0, and 0203's
  `docs.yml` already carries a home-path step. Its portable disclosure check and parity test are not built.
- **0227's configuration landed in an unrelated commit,** `254279ac` ("feat(opencode): add control plane and fixture
  host-tag guard"), not under 0227's own.

## P2 — the `completed` spelling

On the owner's answer to Q1 ("Normalize to complete"), the front-matter line `status: completed` became `status:
complete` in 31 PLANs, with no note: 0087, 0088, 0091, 0098, 0115, 0118, 0119, 0120, 0124, 0142, 0147–0156, 0160,
0161, 0162, 0164, 0165, 0166, 0168, 0171, 0172, 0173 and 0182. No status changed meaning.

**Left as `completed`:** `0122-PLAN-deterministic-goose-file-log-tail-attach.md`.
[0160-MADR-remove-goose-cli-support.md](../decisions/0160-MADR-remove-goose-cli-support.md) D9 freezes the status
lines of goose-only PLANs, and the owner chose to keep it that way (PLAN P2 Deviation 1).
