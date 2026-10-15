---
status: accepted
date: 2026-10-15
decision-makers: Project Owner
consulted: none
informed: maintainers of the live grok suite
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Check the grok model catalog against what grok advertises, not against a count

## Context and Problem Statement

`TestLiveGrokColdCatalog` (`internal/provider/grok/live_model_test.go`, `live_grok` tag) fails
against `grok 1.0.50 (c58f321264ba) [stable]`:

```text
cold catalog: source=live n=4 defaults=[grok-4.6] took=371ms
  grok-4.7 | Grok 4.7
  grok-4.7-build-fast | Grok 4.7 Fast
  grok-4.6 | Grok 4.6
  grok-4.5 | Grok 4.5
cold catalog has 4 options; the live list is being merged with the static floor, which offers models the agent refuses
```

It was first recorded on 2026-10-13 in
[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
(the A3 amendment). On 2026-10-15 it failed again in 0184 B1's `make live-grok` on `acp-go-sdk2
v1.14.0`, and identically on a scratch export of the commit before the bump (`v0.17.1`). The owner
counted B1 done and asked for this test to be fixed separately.

### What the test checks, and why it now fails

The test is MADR 0043 D7's acceptance test: the create-session picker shows the agent's own models
before any session exists. One of its checks is that the live catalog is not padded with the static
floor:

```go
if len(cat.Options) > 3 {
	t.Errorf("cold catalog has %d options; the live list is being merged with the static floor, …")
}
```

It counts, as a stand-in for "nothing outside the agent's list". When it was written (`d315e613`),
grok listed at most three models, and the floor named models grok no longer accepted, so a merge
showed as a longer list.

Both premises have changed. All of the facts below were read on 2026-10-15.

* **grok lists four models.** Its `session/new` answer, its `model` config option and its
  `_x.ai/models/update` each give `grok-4.7`, `grok-4.7-build-fast`, `grok-4.6` and `grok-4.5`
  (wire capture from 0184 B1's probes). The catalog's four options are exactly those.
* **The floor is inside grok's list.** grok's static floor is `grok-4.6` and `grok-4.5`
  (`internal/provider/grok/grok.go:28-31`). A merge would add nothing, so the count cannot detect
  one.
* **The code is right.** `ListModels` (`internal/provider/acpagent/acpagent.go:344`) returns the
  live catalog as-is, and falls back to the floor only when there is no live catalog. Its comment
  says so: "A live catalog is authoritative and must not be padded with the static floor".
* **No unit test pins that rule.** The live count was the only check, and it is now stale.

So the failure is a stale check, not a defect. Raising the bound would pass today, still detect
nothing, and fail again when grok adds a model.

### The question

How should Magic check that the live catalog is the agent's own list and not padded with the floor,
in a way that does not go stale when the agent changes its models?

## Decision Drivers

* The check must catch the defect it names, padding with the floor, whatever models grok lists.
* The live test must compare against what grok actually advertises, not a remembered number.
* No model tokens: the catalog check must stay quota-free, as it is today.
* No change to Magic's behaviour, which is already right.

## Considered Options

* Compare the catalog with grok's own list, and pin the no-padding rule with a unit test
* Raise the bound
* Remove the count check
* Put a model grok does not list into the static floor

## Decision Outcome

Chosen option: "Compare the catalog with grok's own list, and pin the no-padding rule with a unit
test". It is the only option that detects padding whatever grok lists, and keeps the live test about
the real agent.

* **D1. A unit test pins the rule.**
  * In `internal/provider/acpagent`, a provider with a live catalog in its cache, and a static floor
    holding one ID the live catalog shares and one sentinel it does not.
  * `ListModels` must return exactly the live IDs: no sentinel.
  * It needs no agent and no tokens. It catches padding in any form, because the sentinel is never a
    model the agent lists.
* **D2. The live test compares sets, not counts.**
  * It starts a grok session, without a prompt, and reads the `model` config option the session
    advertises.
  * The `ListModels` IDs must equal that option's values, as sets.
  * The session's list (`session/new`) and the cold catalog (`initialize`'s `_meta`) are two
    independent reads of grok's own answer. If they disagree, the failure names both lists.
  * If grok does not advertise a `model` option (an older grok), that comparison is skipped, with
    the reason logged. The other checks stay.
* **D3. The count check is removed.** D1 and D2 replace it.
* **D4. The static floor is unchanged.** It is the fallback when no agent can be read. Whether it
  should list `grok-4.7` is a separate question, outside this record.

### Consequences

* Good, because padding is caught by D1 whatever grok lists, and D2 checks the real agent's list.
* Good, because the test no longer fails when grok adds or drops a model.
* Good, because both checks stay quota-free.
* Neutral, because the live test now starts one session, which takes about a second.
* Bad, because D2 depends on grok advertising a `model` config option. Without one, only the
  unit test guards padding.

### Confirmation

* D1's unit test fails on a scratch copy where `ListModels` merges the floor into the live catalog.
* D2's live test passes against grok `1.0.50`. On a scratch copy with the merge planted and a
  sentinel in grok's floor, it fails, naming the sentinel.
* `make live-grok` runs with `TestLiveGrokColdCatalog` passing.

## Pros and Cons of the Options

### Compare the catalog with grok's own list, and pin the no-padding rule with a unit test

* Good, because it checks the property, not a number.
* Good, because the unit test runs in every `go test`, not only in live runs.
* Bad, because the live test gains a session start.

### Raise the bound

* Good, because it is one character.
* Bad, because a merge adds nothing while the floor is inside grok's list, so it detects nothing.
* Bad, because it fails again the next time grok adds a model.

### Remove the count check

* Good, because the stale check goes.
* Bad, because nothing then guards padding at all.

### Put a model grok does not list into the static floor

* Good, because the count check would work again.
* Bad, because the floor is what users see when no agent can be read. Offering a model grok refuses
  would bring back the problem MADR 0043 D7 fixed.

## More Information

* The test's history: added in `d315e613` (MADR 0043 D7); failing since at least 2026-10-13.
* The PLAN is
  [0194-PLAN-check-the-grok-catalog-against-what-grok-advertises.md](0194-PLAN-check-the-grok-catalog-against-what-grok-advertises.md).
* **Accepted 2026-10-15.** The owner accepted the chosen option and approved the PLAN ("committed
  proceed", after the records were committed as `ff34eb23`).
* **D2's premise, probed 2026-10-15** (PLAN G2 step 1, no prompt, no tokens). A session started
  through Magic against `grok 1.0.50 (c58f321264ba) [stable]` sends a `session_config` event whose
  options are:

  ```text
  id="model" kind="select" current="grok-4.6" values=[grok-4.7 grok-4.7-build-fast grok-4.6 grok-4.5]
  id="reasoning_effort" kind="select" current="high" values=[xhigh high medium low]
  ```

  The `model` option's values are the cold catalog's four IDs. `TestLiveGrokColdCatalog` now pins
  that, and `TestLiveGrokAdvertisesStandardConfigOptions` already pins the option's presence at the
  wire.
* **What D2 cannot see.** A merge adds nothing while every floor ID is one grok lists, so the live
  comparison catches padding only when the floor holds a model grok does not list. D1 catches it
  in every case, which is why both exist.
