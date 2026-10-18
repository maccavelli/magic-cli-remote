---
status: accepted
date: 2026-10-18
decision-makers: Project Owner
consulted: none
informed: contributors who run or change the live grok suite
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Prove grok's auto mode by any write in the workspace, not by one filename

## Context and Problem Statement

`TestLiveGrokAutoDiscriminationPair` (`internal/provider/grok/live_automode_test.go`) proves that
[0049-MADR-grok-auto-mode.md](0049-MADR-grok-auto-mode.md) works. It does not just check that auto
mode is wired up. It asks grok, in `permission_mode: default`, to "Create a file named probe.txt
containing OK. Do it now, then stop.", and runs the request twice:

* **unarmed**, it must get a permission prompt;
* **armed** with auto, it must get no prompt *and* `probe.txt` must exist. The second condition is
  there because a session that is silently blocked also produces no prompt.

On 2026-10-18 the owner approved a full `make live-grok` to close
[0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
B2 and
[0194-PLAN-check-the-grok-catalog-against-what-grok-advertises.md](0194-PLAN-check-the-grok-catalog-against-what-grok-advertises.md).
On grok `1.0.50`, 102 of 103 top-level tests passed. The one failure was this test's armed half:

```text
live_automode_test.go:84: arm=true  prompts=0 wrote=false turnDone=true
live_automode_test.go:90: armed auto must let the agent complete the write
```

### What grok did

grok keeps each session's updates under `~/.grok/sessions/<cwd>/<id>/updates.jsonl`. Both halves
were read there:

* **Unarmed.** The agent listed the workspace, said "Creating `probe.txt` with `OK` now", and
  called `write`. The call waited on a permission prompt, as it should, until the test's 90 s
  ended. Result: one prompt and no file.
* **Armed.** The agent listed the workspace, then ran the skill-check command from the host's
  global agent rules, and loaded the MADR-and-plan skill. It said "No pair exists yet, so I will
  write `docs/0001-MADR-*` / `docs/0001-PLAN-*` and stop before creating `probe.txt`". It then
  wrote `docs/decisions/0001-MADR-create-probe-txt.md` and `…/0001-PLAN-create-probe-txt.md`, with
  no prompt for either, and ended the turn asking for "proceed".

So auto mode did what 0049 promises: two writes, no prompts. The test failed because the model
chose, under the host's own agent rules, not to write the one file the test looks for. Both
halves got the same prompt and the same rules, and the model went a different way in each. On
another host, or another day, it can go either way.

grok `1.0.50` has no switch to leave out the host's rules. `--rules` only adds rules, and
`grok --help` lists no option or environment variable that turns them off.

### The question

What should the armed half require, so that it proves auto mode and not the model's reading of the
host's rules?

## Decision Drivers

* The test must still fail in the case it was written for: auto armed, and the session silently
  blocked, with no prompt and no write.
* It must not depend on which file the model decides to write.
* It must not copy grok's credentials anywhere new.
* Each live run spends quota, so a test whose result depends on chance wastes it.

## Considered Options

* The armed half requires at least one file written in the workspace, with no prompt
* Run the test against a temporary grok home that has no host rules
* Keep the test, and re-run it until the model writes `probe.txt`
* Keep the test, and leave B2 and 0194 open

## Decision Outcome

Chosen option: "The armed half requires at least one file written in the workspace, with no
prompt", because it proves exactly 0049's claim, that an armed session's writes go through without
a prompt, and still fails a session that is silently blocked. It does this without depending on
which file the model chooses.

* **A1.** The armed half passes only when there were no prompts, and the workspace holds at least
  one regular file. Hidden entries, names starting with `.`, are ignored, so a file that grok
  writes for itself cannot pass the test.
* **A2.** The unarmed half is unchanged: it must prompt.
* **A3.** The test logs the files it found, so a failure or a pass shows what the agent wrote.
* **A4.** The walk is a helper in an untagged test file, with a unit test that runs without grok.
  That unit test is the check that is seen to fail. The live test is then run alone, once, with
  the owner's approval.
* **A5.** B2 and 0194 close when that run passes, with this run's 102 passes as the rest of their
  evidence. Both PLANs record that.

### Consequences

* Good, because the armed half now tests auto mode, not which way the model reads the rules.
* Good, because the blocked-session case still fails: no prompt and an empty workspace.
* Bad, because a model that writes some other file while ignoring the request still passes. That
  is still a write without a prompt, which is what 0049 promises, but the test no longer checks
  that the model did what it was asked.
* Neutral, because the prompt text stays the same, so earlier runs remain comparable.

### Confirmation

* The new unit test passes, and fails when the helper is planted to ignore its rules.
* `TestLiveGrokAutoDiscriminationPair` passes when run alone on grok `1.0.50`.

## Pros and Cons of the Options

### At least one file, with no prompt

* Good, because it holds whatever the model decides to write.
* Good, because it keeps the case the test exists for.
* Bad, because it no longer checks that the model did what it was asked.

### A temporary grok home with no host rules

* Good, because the model would see only the test's prompt.
* Bad, because grok `1.0.50` has no switch for it. It would mean copying `~/.grok/auth.json` into a
  temporary directory on every run, which puts credentials in more places.
* Bad, because grok can still change how it finds rules, so the isolation could break quietly.

### Re-run until the model writes `probe.txt`

* Good, because nothing changes.
* Bad, because each run is a coin toss that spends quota, and a pass would prove nothing new.

### Leave B2 and 0194 open

* Good, because nothing changes.
* Bad, because both records stay incomplete for a reason that is not in Magic.

## More Information

* The full run's log and the two sessions' updates were read in the session scratch directory and
  under `~/.grok/sessions`. Neither is committed: both carry host paths and the host's own rule
  and skill text.
* [0050-MADR-grok-cli-surface-drift.md](0050-MADR-grok-cli-surface-drift.md) is why the test uses
  `permission_mode: default`.
