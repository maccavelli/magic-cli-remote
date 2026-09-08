---
status: accepted
date: 2026-09-08
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The skill is `writing-madr-and-plans`, and the second machine is why the repo kept saying otherwise

## Context and Problem Statement

Four instruction files in this repository tell every agent to load a skill
named `madr-and-plan-writing`. No such skill exists on this host. Calling it
fails, and an agent that carries on without the skill writes something shaped
like a MADR while missing MADR 4.0.0's headings, the `Good, because …` argument
form, and the slug rule.

The name has been reversed four times (`0416ac3`, `e06a0b6`, `fd1e75f`,
`4e1d0a7`). Each reversal claimed to settle it. `AGENTS.md` currently claims the
name was "verified three ways on 2026-09-06" — all three of those checks
contradict the filesystem today.

Both prior records also state that **why** this keeps happening "is still not
established, and this note does not guess". This record establishes it. The
mechanism was never in this repository, and it was never a typo.

Owner decision, 2026-09-07: the name is `writing-madr-and-plans`.

### What was measured, not assumed

All on `4f37b9f`, Windows 11.

**The skill on this host, and the only one.**

```console
$ ls -d ~/.claude/skills/*
/c/Users/macsm/.claude/skills/writing-madr-and-plans
$ grep '^name:' ~/.claude/skills/writing-madr-and-plans/SKILL.md
name: writing-madr-and-plans
```

Loading it by that name in this session returned the skill body. Loading
`madr-and-plan-writing` returned `Unknown skill`.

**`AGENTS.md`'s three 2026-09-06 verifications are each false today.**

| Claim in `AGENTS.md` | Filesystem, 2026-09-07 |
| --- | --- |
| "the only entry under any skills root is `~/.claude/skills/madr-and-plan-writing`" | the only entry is `writing-madr-and-plans` |
| "its `SKILL.md` `name:` field reads `madr-and-plan-writing`" | it reads `writing-madr-and-plans` |
| "loading it by that name in a live session resolved" | that name returns `Unknown skill`; the other resolves |

**They were false when written, not overtaken by a later rename.** A rename
inside a directory updates that directory's mtime. The skills root has not been
touched since it was created:

```console
$ stat -c '%n mtime=%y btime=%w' ~/.claude/skills
/c/Users/macsm/.claude/skills mtime=2026-08-28 17:10:18 btime=2026-08-28 17:10:18
```

mtime equals btime, both 2026-08-28 — nine days before the claimed
verification and ten days before today. No entry has been created, renamed or
removed there since. So `madr-and-plan-writing` was never present under
`~/.claude/skills`, and the 2026-09-06 note did not observe what it reports.

**The symlink explanation is also wrong.** Both files state that
`~/.claude/skills/madr-and-plan-writing` is a symlink into `~/.agents/skills/`.

```console
$ ls -ld ~/.claude/skills/writing-madr-and-plans
drwxr-xr-x ... /c/Users/macsm/.claude/skills/writing-madr-and-plans
$ ls -d ~/.agents
ls: cannot access '/c/Users/macsm/.agents': No such file or directory
```

A real directory, not a link, and the target root does not exist.

**Where the other spelling really lives — a captured fixture answers it.** The
grok wire fixture, captured 2026-09-03 on the POSIX host, contains the engine's
own skill listing:

```text
{"name":"madr-and-plan-writing", ... "path":"/home/user/.grok/skills/madr-and-plan-writing/SKILL.md"}
```

So the spelling is not invented and never was. On the other machine, under a
*different agent's* skills root (`~/.grok/skills`, not `~/.claude/skills`),
a directory with that name exists. `docs/0106-MADR-grok-1.0.5-surface-parity.md:278`
records the same observation from 2026-08-30: "plus user skill
`madr-and-plan-writing`".

**Both spellings are therefore true statements about different machines**, which
is exactly the shape that produces a four-times-reversed fact: each author
checked a filesystem, each was right about the one they checked, and each wrote
an unqualified sentence.

**This host's other agent roots hold no skills at all.** `~/.grok` and
`~/.config/cagent` are empty; `~/.codex` has no skills directory and its
`config.toml` mentions none; `~/.gemini` has no skills root. There is no
global instruction file on this host — no `~/.claude/CLAUDE.md`, no
`~/.codex/AGENTS.md`, no `~/AGENTS.md`.

### Findings

**F1 — the repository is wrong on this host, in four instruction files.**
`AGENTS.md` (9 occurrences), `.claude/rules/madr-and-plan-skill.md` (5),
`.grok/rules/madr-plan-before-mutating-work.md` (4), `.opencode/rules.md` (4).
Every one names a skill that does not resolve here.

**F2 — the failure is silent.** A call to a non-existent skill does not error
loudly in every harness; in this one it returns `Unknown skill` and an agent may
simply proceed. That is what both prior notes warned about, and it is the whole
reason the name is worth this much record.

**F3 — the 2026-09-06 verification in `AGENTS.md` did not happen as described.**
Proven by the skills-root mtime above. This matters more than the name: a
fabricated verification is worse than a wrong guess, because it is specifically
designed to stop the next reader from checking.

**F4 — the symlink mechanism both files assert does not exist.** No symlink, and
no `~/.agents`.

**F5 — the real mechanism is two machines with two agent roots.**
`~/.grok/skills/madr-and-plan-writing` on the POSIX host (evidenced by the
2026-09-03 fixture and by 0106), `~/.claude/skills/writing-madr-and-plans`
here. Both spellings are locally correct; neither is globally correct.

**F6 — the "birth time" correction in `AGENTS.md` is wrong too.** It argues
about 2026-08-06 versus 2026-08-14; the actual btime is 2026-08-28 17:10:18.
Both dates in that dispute are wrong, which is what happens when a record
argues about a fact instead of re-reading it.

**F7 — nothing on this host needs changing outside the repository.** The one
skill present is correctly named and correctly resolvable. The assessment the
owner asked for returns clean.

## Decision Drivers

* The owner has decided the name; that ends the question of *which*.
* A fact that lives on two filesystems cannot be stated unqualified in a file
  that both machines read.
* A false verification note is more damaging than a wrong name, because it
  suppresses the check.
* The fix must not depend on the Mac, which is not reachable from here.

## Considered Options

* **A — Adopt `writing-madr-and-plans` everywhere, and make every claim
  machine-qualified** (chosen)
* **B — Rename the skill on this host to `madr-and-plan-writing`**
* **C — Name both spellings and tell agents to try each**

## Decision Outcome

Chosen option: **A**, per the owner's decision, with the machine-qualification
added because it is the only thing that stops a fifth reversal.

### The decisions

**D1 — the name is `writing-madr-and-plans`** in all four instruction files.

**D2 — delete the reversal ledger, the symlink paragraph, and the 2026-09-06
verification note.** They are wrong (F3, F4, F6) and long. Replace them with
F5's mechanism and the check command, which is shorter and true.

**D3 — state the fact as machine-qualified.** The instruction files say the
name is `writing-madr-and-plans` under `~/.claude/skills`, and that a different
agent root on another host carries the other spelling — so an agent that finds
`madr-and-plan-writing` has not found a bug, it is on the other machine.

**D4 — keep the filesystem check, and keep it outranking the prose.** That
instruction is the one thing all four previous versions got right.

**D5 — do not touch the fixtures or `0106`.** They are captured evidence of what
was true on another host on a given day; editing them would destroy the only
thing that explained F5. `docs/kilo-spike-7.4.20/*` likewise.

**D6 — do not rename anything under `~`.** F7 says there is nothing to fix
there, and D1 matches the name that is already correct.

### Consequences

* Good: the skill resolves for every agent reading these files on this host.
* Good: F5 replaces "why this keeps going wrong is not established" with a
  mechanism, which is what stops the fifth reversal.
* Good: the four files get shorter — the ledger and symlink paragraphs go.
* Neutral: `AGENTS.md`'s section loses its history-of-errors table. The history
  is preserved here, which is where a record of what was believed belongs.
* Bad: the Mac's `~/.grok/skills/madr-and-plan-writing` is untouched and
  unreachable from here, so an agent run there still sees the other spelling.
  D3 makes that legible rather than fixing it; the owner can rename it there.
* Bad: this record asserts things about a machine it cannot see, from a fixture
  captured four days ago. If that directory has since been renamed, F5's
  evidence is stale — but the mechanism it explains does not depend on the
  directory still existing.

### Confirmation

```bash
# 1. No instruction file carries the other spelling:
grep -rn 'madr-and-plan-writing' AGENTS.md .claude/ .grok/ .opencode/   # expect none

# 2. The name each file gives is the one on disk:
ls -d ~/.claude/skills/*madr* && grep '^name:' ~/.claude/skills/*madr*/SKILL.md

# 3. The captured evidence for F5 is untouched:
git diff --stat -- internal/provider/ docs/0106-* docs/kilo-spike-7.4.20/   # expect empty
```

## Pros and Cons of the Options

### A — Adopt `writing-madr-and-plans`, machine-qualified (chosen)

* Good, because it matches the owner's decision and the only skill that exists
  on the machine these files are being read on.
* Good, because qualifying the claim addresses the actual cause rather than the
  latest symptom — four unqualified corrections have not held.
* Bad, because it leaves two machines disagreeing. The repo is then correct for
  one of them and explicit about the other, which is the best a repository can
  do about a fact that lives outside it.

### B — Rename the skill on this host to `madr-and-plan-writing`

* Good, because it would make the repository's current text true with no doc
  edit, and would match the Mac.
* Bad, because the owner decided the other way.
* Bad, because it edits a user's global environment to make a document right,
  which is the wrong direction of authority — and the strongest argument
  against every version of this note that "corrected" the filesystem from
  memory.

### C — Name both spellings, tell agents to try each

* Good, because it works on both machines with no rename anywhere.
* Bad, because "try one, then the other" is a rule that reads as uncertainty,
  and an agent that silently falls through to the second has no signal that its
  environment is misconfigured.
* Bad, because it entrenches the divergence rather than recording it as one.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Only skill present is `writing-madr-and-plans` | `ls -d ~/.claude/skills/*` |
| Its `name:` field agrees | `grep '^name:' .../SKILL.md` |
| `madr-and-plan-writing` does not resolve | skill call this session → `Unknown skill` |
| Skills root untouched since 2026-08-28 | `stat -c '%y %w' ~/.claude/skills` → mtime = btime = 2026-08-28 17:10:18 |
| Not a symlink | `ls -ld`, `readlink -f` returns itself |
| `~/.agents` absent | `ls -d ~/.agents` → No such file or directory |
| The other spelling exists under `~/.grok/skills` on the POSIX host | `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`, captured 2026-09-03 |
| Same observation on 2026-08-30 | `docs/0106-MADR-grok-1.0.5-surface-parity.md:278` |
| Occurrence counts in the four files | `grep -rc` |
| No other skills root or global instruction file on this host | `ls ~/.codex ~/.gemini ~/.grok ~/.config/cagent`; `~/.codex/config.toml` |

### Related records

* **MADR 0105** — established that mutating work requires a pair; these four
  files are its per-agent pointers, which is why a wrong name in them is not
  cosmetic.
* **MADR 0106** — recorded the grok skill listing on 2026-08-30, unknowingly
  capturing half of F5.
* **MADR 0151** — the fixture that proves F5 is the same one 0151 scrubs. Its
  P2 must preserve the `skills` frames; only the `_meta` account values change.

### Open questions for the plan

1. **Should the Mac's `~/.grok/skills/madr-and-plan-writing` be renamed?** It
   would remove the divergence at the source. It is the owner's machine and not
   reachable from here, so this record cannot do it and does not assume it.
   **[unverified]** whether that directory still exists.
2. **Do the other three agent roots need the pointer file at all?** `.grok/` and
   `.opencode/` rules exist for agents that have no skills configured on this
   host. Out of scope here; renaming them is cheaper than deciding that.

## Amendment — 2026-09-08: option B is chosen after all, and the divergence is gone

Owner decision, 2026-09-08: the name is `madr-and-plan-writing`. This reverses
D1 one day after it was made, and it is the fifth reversal of this fact.

It is also the first one that fixes anything, and the difference is worth
stating precisely: **the four previous reversals edited a document; this one
renamed a directory.**

**F8 — the two-machine divergence is closed by convergence, not by prose.**
F5 established that both spellings were locally correct — `madr-and-plan-writing`
under the Mac's `~/.grok/skills`, `writing-madr-and-plans` under this host's
`~/.claude/skills`. D3 responded by qualifying every claim by machine, which
made the documents true but left two names in the world for a sixth reader to
find and "correct". Renaming this host's directory to match the Mac removes the
second name. There is now one spelling on both machines, so the condition that
produced five reversals no longer exists.

**D8 — the name is `madr-and-plan-writing`, and this host was renamed to it.**
`~/.claude/skills/writing-madr-and-plans` → `~/.claude/skills/madr-and-plan-writing`,
with the `name:` field in its `SKILL.md` updated to match. The four instruction
files follow.

**D9 — D6 is reversed.** D6 said "do not rename anything under `~`", on the
reasoning that editing a user's global environment to make a document right is
the wrong direction of authority. That reasoning was correct for what was being
asked then — the document was wrong and the filesystem was right. It does not
apply here: the owner has changed which name is wanted, so the filesystem is
what is now out of date, and renaming it is the direction of authority working
properly rather than being inverted.

This is exactly **option B**, whose analysis this record already contains. It
was rejected only because "the owner decided the other way", and that premise
has changed. Its recorded argument stands unaltered: it makes the repository's
text true with no per-machine qualification, and it matches the Mac.

**What was verified, 2026-09-08.** Before the rename, calling
`madr-and-plan-writing` in this session returned `Unknown skill`. After it, the
harness re-read the skills directory and the name appeared in the available
skills list with the correct description. `ls -d ~/.claude/skills/*` returns one
entry, `madr-and-plan-writing`, and its `SKILL.md` `name:` field agrees.

**What is corrected in the amendment above rather than left standing.** The
Consequences section said "the Mac's `~/.grok/skills/madr-and-plan-writing` is
untouched and unreachable from here, so an agent run there still sees the other
spelling". That is now the *desired* state rather than a defect, and the Mac
needs no change at all.

**What is deliberately not undone.** P1 and P2 also deleted `AGENTS.md`'s
"verified three ways on 2026-09-06" note (F3), the `~/.agents` symlink claim
(F4), the reversal ledger, and the birth-time dispute (F6). Those are false
regardless of which name wins, so this amendment changes the name and leaves
the deletions in place. A `git revert` of P1/P2 would have been the fast way to
reverse the name and would have restored four false claims with it.

**The honest residue.** This record has now asserted, in one calendar day, that
the name is `writing-madr-and-plans` (D1, with evidence) and that it is
`madr-and-plan-writing` (D8, by decision). Both were accurate statements at the
time about a fact the owner controls. The lesson is not about the spelling: a
fact that lives outside the repository, on more than one machine, cannot be
settled by a repository document — only recorded by one. It is settled by making
the machines agree, which is what D8 does.

**The title and filename are left as they are.** Both say
`writing-madr-and-plans`, which D8 reverses. They are not corrected because the
slug is how this record is referenced — from `AGENTS.md`, from the three rule
files, and from the commits that executed it — and because a record's title is
what it concluded on the day it was written. The amendment is the current
answer; the title is the history. Renaming the file to hide a reversal would be
the same instinct that produced the reversals.
