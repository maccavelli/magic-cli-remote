---
status: proposed
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0152 — Make every instruction file name the skill that exists

Implements [0152-MADR-the-skill-name-is-writing-madr-and-plans.md](0152-MADR-the-skill-name-is-writing-madr-and-plans.md)
decisions D1–D6, closing findings F1–F7.

## Goal

1. `grep -rn 'madr-and-plan-writing' AGENTS.md .claude/ .grok/ .opencode/`
   returns nothing.
2. Each of the four files names `writing-madr-and-plans`, says which skills
   root that is, and says that another agent root on another host carries the
   other spelling.
3. The false verification ledger, the symlink paragraph and the birth-time
   dispute are gone from all four.
4. The filesystem check survives in each file, still outranking the prose.
5. No captured evidence is edited.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `AGENTS.md` | P1 | the normative section (D1, D2, D3, D4) |
| `.claude/rules/madr-and-plan-skill.md` | P2 | per-agent pointer |
| `.grok/rules/madr-plan-before-mutating-work.md` | P2 | per-agent pointer |
| `.opencode/rules.md` | P2 | per-agent pointer |

### Out of scope

* **Everything under `~`** (D6). The assessment found one skill, correctly
  named; there is nothing to change, and editing a user's global environment to
  match a document is the failure mode this record exists to end.
* **The wire fixtures, `docs/0106-*`, `docs/kilo-spike-7.4.20/*`** (D5). They
  are captured evidence of another host, and they are the only reason F5 could
  be established. `grep` matches there are observations, not instructions.
* **The Mac's `~/.grok/skills/`.** Not reachable from this host; MADR open
  question 1.
* **Any source, test or CI file.** This plan changes prose only.

## Stability rule

There is no build to run: no phase touches Go. Each phase ends with

```bash
grep -rn 'madr-and-plan-writing' AGENTS.md .claude/ .grok/ .opencode/
git diff --stat -- internal/ docs/0106-* docs/kilo-spike-7.4.20/
```

the first printing nothing after P2, the second printing nothing after either.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — no captured evidence is edited.** The fixtures, `0106` and the kilo
spike keep every byte. They are the record of what was true elsewhere.

**C2 — every file keeps a filesystem check that outranks its own prose.** All
four current versions carry one; none may lose it. It is the only instruction
that survived four reversals intact.

**C3 — no file asserts an unqualified global fact about the skill.** Each says
which root it means. An unqualified sentence is what produced four reversals
(MADR F5).

**C4 — the name is only ever copied from the filesystem, never typed from
memory.** Including while executing this plan.

**The contract most at risk is C1**, because `sed -i` across the repository is
the obvious way to do a rename, and the fixtures contain 23 matches that would
be silently rewritten by it. That would destroy F5's evidence and leave a
"redacted" fixture that no longer reproduces the engine's bytes — a second
defect on top of the one MADR 0151 already records against the same file. Every
phase names its files explicitly for this reason.

## Dependency and delivery order

P1 before P2: `AGENTS.md` holds the normative text and the three rule files
point at it, so the pointers should be written against the section they now
point to.

## Implementation Steps

### P1 — correct the normative section (D1, D2, D3, D4; closes F1, F3, F4, F6)

`AGENTS.md`, the "MADR and PLAN before mutating work" section. Nine occurrences
of the name become `writing-madr-and-plans`. Then delete, rather than correct:

* the four-row reversal ledger;
* the paragraph asserting the symlink into `~/.agents/skills/` (F4);
* the birth-time dispute (F6, wrong on both of its dates);
* the "verified three ways on 2026-09-06" note (F3).

Replace them with two short paragraphs: the name and its root, and MADR 0152
F5's mechanism — the other spelling is a real directory under a different
agent's root on the other machine, so finding it is not evidence of a bug here.
Keep the check block (C2).

**Verification.**

```bash
grep -c 'madr-and-plan-writing' AGENTS.md          # 0
grep -c 'writing-madr-and-plans' AGENTS.md         # >= 2
grep -A2 'ls -d ~/.claude/skills' AGENTS.md        # the check survives
```

### P2 — correct the three per-agent pointers (D1, D2, D3, D4; closes F1)

`.claude/rules/madr-and-plan-skill.md`, `.grok/rules/madr-plan-before-mutating-work.md`,
`.opencode/rules.md`. Same three edits in each: the name, the deletions, the
one-line mechanism. Each keeps its check block and its pointer to `AGENTS.md`
as the normative text.

`.claude/rules/madr-and-plan-skill.md` additionally opens by warning against
restating the workflow, having been burned by exactly that. Honour it: these
files get the name, the mechanism, and the check — not a copy of the section.

**Verification.**

```bash
grep -rn 'madr-and-plan-writing' .claude/ .grok/ .opencode/   # nothing
grep -rln 'ls -d ~/.claude/skills' .claude/ .grok/ .opencode/ # all three
```

## Verification (whole plan)

```bash
grep -rn 'madr-and-plan-writing' AGENTS.md .claude/ .grok/ .opencode/
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md
git diff --stat -- internal/ docs/0106-* docs/kilo-spike-7.4.20/
git status --short
```

Then, the check this record is ultimately about: call the skill by the name the
files now give, in a live session, and confirm it returns the skill body rather
than `Unknown skill`.

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | No instruction file carries `madr-and-plan-writing` | D1, F1 |
| A2 | All four name `writing-madr-and-plans` and its root | D1, D3 |
| A3 | All four retain a filesystem check | D4, C2 |
| A4 | The ledger, symlink paragraph, verification note and birth-time dispute are gone | D2, F3, F4, F6 |
| A5 | Each file states the two-machine mechanism | D3, F5 |
| A6 | Zero diff under `internal/`, `docs/0106-*`, `docs/kilo-spike-7.4.20/` | D5, C1 |
| A7 | Nothing under `~` is modified | D6, F7 |
| A8 | The skill loads by the documented name in a live session | Confirmation 2 |

**A6 is the criterion most likely to be lost**, because the natural way to do
this job is one repo-wide `sed`, and that command passes A1 and A2 while
silently failing A6 on 23 lines of captured evidence.

**A8 is the one most likely to be skipped**, because it cannot be checked by
grep — it needs an actual skill call. Four previous versions of this note were
confident and wrong; a call is the only evidence that outranks confidence.

## Rollout and Rollback

Prose only. No build, no test, no runtime behaviour, no user-visible surface.
Each phase reverts independently, and reverting both restores files that name a
skill which does not resolve — so a revert is only ever a step toward a
different correction, not a safe resting state.

## Deferred (named, so they are not mistaken for oversights)

* **Renaming `~/.grok/skills/madr-and-plan-writing` on the Mac.** It would
  remove the divergence at its source rather than documenting it. Not reachable
  from this host, and it is the owner's environment. MADR open question 1;
  **[unverified]** whether it still exists.
* **Whether `.grok/` and `.opencode/` need pointer files at all.** Neither
  agent has a skills root on this host. Deciding that costs more than renaming
  them, so they are renamed now and the question is left open (MADR open
  question 2).
* **A guard that the documented skill name resolves.** The natural shape is a
  check that greps the name out of `AGENTS.md` and confirms a matching
  directory under `~/.claude/skills`. It would have caught all four reversals.
  It is deferred because it asserts against a path outside the repository,
  which no other test here does, and that is a precedent worth deciding
  deliberately rather than in passing.

## Amendment — 2026-09-07: A1 as written contradicts D3

A1 read "No instruction file carries `madr-and-plan-writing`". Executing P1
made it immediately false, and correctly so: D3 requires each file to *name*
the other spelling in order to say what finding it means. Five such mentions
now exist across the four files, and every one of them is the sentence D3 asks
for.

**Corrected A1:** no instruction file gives `madr-and-plan-writing` as the
skill to load. The name may appear only in the sentence explaining the
two-machine divergence.

```bash
grep -rn 'load the \*\*`madr-and-plan-writing`\|`madr-and-plan-writing` skill first' \
  AGENTS.md .claude/ .grok/ .opencode/    # expect none
```

This is worth recording rather than quietly rewording, because a criterion
phrased as a bare `grep -c … == 0` is exactly the kind that gets satisfied by
deleting the explanation along with the error. The plan's own C3 asks for the
qualification that A1 would have removed.
