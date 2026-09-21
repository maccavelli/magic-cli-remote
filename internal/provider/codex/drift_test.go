package codex

import (
	"strings"
	"testing"
)

// These are acceptance criterion A9 of PLAN 0163: the drift gate must fail on the
// four differences a client can be wrong about, and pass on the one it cannot.
//
// They are unit tests against synthetic surfaces rather than live probes, so the
// four cases can be asserted independently and without a Codex binary. The live
// gate (live_contract_test.go) wires the same functions to a real schema export.

func surface(requests, notifications, serverRequests []string) ContractSurface {
	build := func(methods []string) []WireContract {
		out := make([]WireContract, 0, len(methods))
		for _, method := range methods {
			out = append(out, WireContract{
				Method: method, Params: "P", Response: "typed",
				Classification: ClassificationImplemented,
			})
		}
		return out
	}
	return ContractSurface{
		ClientRequests:      build(requests),
		ServerNotifications: build(notifications),
		ServerRequests:      build(serverRequests),
	}
}

func TestCompareSurfacesTreatsAdditionsAsNonBreaking(t *testing.T) {
	pinnedStable := surface([]string{"thread/start"}, []string{"turn/started"}, []string{"execCommandApproval"})
	freshStable := surface([]string{"thread/start", "thread/attachment/add"},
		[]string{"turn/started", "thread/attachment/updated"}, []string{"execCommandApproval"})

	drift := CompareSurfaces(pinnedStable, pinnedStable, freshStable, freshStable)
	if drift.HasBreaking() {
		t.Fatalf("additions were classified as breaking: %v", drift.Breaking)
	}
	// Both surfaces are compared, so each addition is reported once per surface.
	if len(drift.Additive) != 4 {
		t.Errorf("additive = %v, want two methods across two surfaces", drift.Additive)
	}
	if !strings.Contains(strings.Join(drift.Additive, " "), "thread/attachment/add") {
		t.Errorf("additive diff does not name the new method: %v", drift.Additive)
	}
}

func TestCompareSurfacesFlagsARemovedMethod(t *testing.T) {
	pinned := surface([]string{"thread/start", "thread/rollback"}, nil, nil)
	fresh := surface([]string{"thread/start"}, nil, nil)

	drift := CompareSurfaces(pinned, pinned, fresh, fresh)
	if !drift.HasBreaking() {
		t.Fatal("a removed method was not classified as breaking")
	}
	joined := strings.Join(drift.Breaking, " ")
	if !strings.Contains(joined, "removed") || !strings.Contains(joined, "thread/rollback") {
		t.Errorf("breaking diff does not name the removal: %v", drift.Breaking)
	}
	// The classification travels with it, because "removed a method we
	// implement" and "removed one we never used" need different responses.
	if !strings.Contains(joined, string(ClassificationImplemented)) {
		t.Errorf("breaking diff omits the classification: %v", drift.Breaking)
	}
}

func TestCompareSurfacesFlagsAStabilityDemotion(t *testing.T) {
	// The method still exists, so neither the removal nor the addition check sees
	// a problem: it leaves stable and is still present in experimental. Reported
	// as an addition it would look like news nobody has to act on, when in fact
	// the call now needs an opt-in and fails with -32600 at the call site.
	pinnedStable := surface([]string{"thread/items/list"}, nil, nil)
	freshStable := surface(nil, nil, nil)
	freshExperimental := surface([]string{"thread/items/list"}, nil, nil)

	drift := CompareSurfaces(pinnedStable, pinnedStable, freshStable, freshExperimental)
	if !drift.HasBreaking() {
		t.Fatal("a stable -> experimental demotion was not classified as breaking")
	}
	if !strings.Contains(strings.Join(drift.Breaking, " "), "demoted to experimental-only") {
		t.Errorf("breaking diff does not name the demotion: %v", drift.Breaking)
	}
}

func TestCompareRequiredFieldsFlagsNewlyRequiredAndIgnoresRelaxations(t *testing.T) {
	pinned := []ContractFixture{{
		Kind: WireClientRequest, Method: "turn/start",
		ParamsSchema: "TurnStartParams", ResponseSchema: "TurnStartResponse",
		RequiredParams: []string{"input", "threadId"},
		RequiredResult: []string{"turnId"},
	}}

	// A field becomes required: we start sending a request the engine rejects.
	stricter := []ContractFixture{{
		Kind: WireClientRequest, Method: "turn/start",
		ParamsSchema: "TurnStartParams", ResponseSchema: "TurnStartResponse",
		RequiredParams: []string{"input", "threadId", "serviceTierForTurn"},
		RequiredResult: []string{"turnId"},
	}}
	breaking := CompareRequiredFields(pinned, stricter)
	if len(breaking) != 1 || !strings.Contains(breaking[0], "serviceTierForTurn") {
		t.Fatalf("newly required param not flagged: %v", breaking)
	}
	if !strings.Contains(breaking[0], "newly required param") {
		t.Errorf("message does not say what changed: %v", breaking)
	}

	// A result field becomes required: we may read what can now be absent.
	resultStricter := []ContractFixture{{
		Kind: WireClientRequest, Method: "turn/start",
		ParamsSchema: "TurnStartParams", ResponseSchema: "TurnStartResponse",
		RequiredParams: []string{"input", "threadId"},
		RequiredResult: []string{"turnId", "startedAtMs"},
	}}
	if breaking := CompareRequiredFields(pinned, resultStricter); len(breaking) != 1 ||
		!strings.Contains(breaking[0], "newly required result field") {
		t.Errorf("newly required result field not flagged: %v", breaking)
	}

	// A relaxation is not breaking: we keep sending the field. codex 0.155.1 did
	// exactly this to FunctionCallOutputResponseItem.call_id.
	relaxed := []ContractFixture{{
		Kind: WireClientRequest, Method: "turn/start",
		ParamsSchema: "TurnStartParams", ResponseSchema: "TurnStartResponse",
		RequiredParams: []string{"input"},
		RequiredResult: []string{},
	}}
	if breaking := CompareRequiredFields(pinned, relaxed); len(breaking) != 0 {
		t.Errorf("a relaxation was classified as breaking: %v", breaking)
	}

	// A method that disappeared is CompareSurfaces's business, not this one, so it
	// must not be double-reported here.
	if breaking := CompareRequiredFields(pinned, nil); len(breaking) != 0 {
		t.Errorf("a removed method was reported by the required-field check too: %v", breaking)
	}
}
