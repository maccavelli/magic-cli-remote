package opencode

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestDispositionCoversTheExactUnion pins every member of the 1.18.21 Part
// union to a disposition. The union is a closed set on this release, so a
// member missing here is a decoding gap, not a default-branch detail.
func TestDispositionCoversTheExactUnion(t *testing.T) {
	want := map[string]partDisposition{
		partText:       dispositionChat,
		partReasoning:  dispositionChat,
		partTool:       dispositionChat,
		partFile:       dispositionChat,
		partSubtask:    dispositionElsewhere,
		partAgent:      dispositionElsewhere,
		partStepStart:  dispositionInternal,
		partStepFinish: dispositionInternal,
		partSnapshot:   dispositionInternal,
		partPatch:      dispositionInternal,
		partRetry:      dispositionInternal,
		partCompaction: dispositionInternal,
	}
	if len(want) != 12 {
		t.Fatalf("the 1.18.21 union has 12 members; this table has %d", len(want))
	}
	for typ, expect := range want {
		if got := dispositionOf(typ); got != expect {
			t.Fatalf("dispositionOf(%q) = %v, want %v", typ, got, expect)
		}
	}
	if got := dispositionOf("some-future-part"); got != dispositionUnknown {
		t.Fatalf("an unlisted member must be unknown, got %v", got)
	}
}

// TestFixtureUnionMatchesDisposition proves the committed P0 evidence and this
// decoder describe the same union — the fixture is the contract.
func TestFixtureUnionMatchesDisposition(t *testing.T) {
	var fixture struct {
		PartUnion         []string          `json:"part_union"`
		MapperDisposition map[string]string `json:"mapper_disposition"`
	}
	readSurfaceFixture(t, "message-parts.json", &fixture)
	if len(fixture.PartUnion) != 12 {
		t.Fatalf("fixture union has %d members, want 12", len(fixture.PartUnion))
	}
	// Map the schema type names onto their wire discriminators.
	wire := map[string]string{
		"TextPart": partText, "ReasoningPart": partReasoning, "ToolPart": partTool,
		"FilePart": partFile, "SubtaskPart": partSubtask, "AgentPart": partAgent,
		"StepStartPart": partStepStart, "StepFinishPart": partStepFinish,
		"SnapshotPart": partSnapshot, "PatchPart": partPatch,
		"RetryPart": partRetry, "CompactionPart": partCompaction,
	}
	for _, member := range fixture.PartUnion {
		w, ok := wire[member]
		if !ok {
			t.Fatalf("fixture names union member %q that this decoder does not map", member)
		}
		if dispositionOf(w) == dispositionUnknown {
			t.Fatalf("union member %q (%q) has no disposition", member, w)
		}
	}
}

// TestMapPartRendersOnlyChatMembers proves the non-chat two-thirds of the union
// produce no transcript row.
func TestMapPartRendersOnlyChatMembers(t *testing.T) {
	for _, typ := range []string{
		partSubtask, partAgent, partStepStart, partStepFinish,
		partSnapshot, partPatch, partRetry, partCompaction,
	} {
		if _, ok := mapPart("assistant", nativePart{Type: typ, Text: "internal"}, false, nil); ok {
			t.Fatalf("%q produced a chat row", typ)
		}
	}
}

// TestMapPartDropsUnknownWithoutRenderingText is the forward-compatibility
// rule: a member this release never defined must not reach the transcript as
// raw text, whatever its fields happen to contain.
func TestMapPartDropsUnknownWithoutRenderingText(t *testing.T) {
	ev, ok := mapPart("assistant", nativePart{
		Type: "quantum-part", Text: "internal engine detail",
	}, false, nil)
	if ok {
		t.Fatalf("unknown member rendered as %+v", ev)
	}
}

// TestMapPartTextRoles proves role decides attribution, and that a user cannot
// author reasoning.
func TestMapPartTextRoles(t *testing.T) {
	user, ok := mapPart("user", nativePart{ID: "p1", MessageID: "m1", Type: partText, Text: "hi"}, true, nil)
	if !ok || user.Type != event.TypeUserMessage {
		t.Fatalf("user text = %+v", user)
	}
	if user.NativeMessageID != "m1" || user.NativePartID != "p1" || !user.Replace {
		t.Fatalf("identity lost: %+v", user)
	}
	asst, ok := mapPart("assistant", nativePart{Type: partText, Text: "hello"}, false, nil)
	if !ok || asst.Type != event.TypeAssistantChunk || asst.Replace {
		t.Fatalf("assistant text = %+v", asst)
	}
	if _, ok := mapPart("user", nativePart{Type: partReasoning, Text: "hmm"}, false, nil); ok {
		t.Fatal("a user part was rendered as reasoning")
	}
	think, ok := mapPart("assistant", nativePart{Type: partReasoning, Text: "hmm"}, false, nil)
	if !ok || think.Type != event.TypeThoughtChunk {
		t.Fatalf("reasoning = %+v", think)
	}
}

// TestMapPartToolPrecedence pins the visible-detail precedence shared by live
// streaming and replay: error, then output, then title, then input echo.
func TestMapPartToolPrecedence(t *testing.T) {
	base := nativePart{ID: "p", MessageID: "m", Type: partTool, Tool: "bash", CallID: "c1"}

	withTitle := base
	withTitle.State = nativeToolState{Status: "completed", Title: "ls -la"}
	ev, _ := mapPart("assistant", withTitle, true, nil)
	if ev.Text != "ls -la" {
		t.Fatalf("title detail = %q", ev.Text)
	}

	withOutput := withTitle
	withOutput.State.Output = "file-a\nfile-b"
	ev, _ = mapPart("assistant", withOutput, true, nil)
	if ev.Text != "file-a\nfile-b" {
		t.Fatalf("output must beat title, got %q", ev.Text)
	}

	withErr := withOutput
	withErr.State.Status = "error"
	withErr.State.Error = "boom"
	ev, _ = mapPart("assistant", withErr, true, nil)
	if ev.Text != "boom" {
		t.Fatalf("error must beat output, got %q", ev.Text)
	}
	if ev.Status == "completed" {
		t.Fatal("a failed tool part replayed as completed")
	}

	inputOnly := base
	inputOnly.State = nativeToolState{Status: "running", Input: json.RawMessage(`{"cmd":"ls"}`)}
	ev, _ = mapPart("assistant", inputOnly, true, nil)
	if !strings.Contains(ev.Text, "cmd") {
		t.Fatalf("input echo = %q", ev.Text)
	}
}

// TestToolVisibleOutputIsCapped proves the 8,000-character cap applies and
// keeps line structure.
func TestToolVisibleOutputIsCapped(t *testing.T) {
	long := strings.Repeat("line\n", 5000)
	got := toolVisibleOutput(nativeToolState{Status: "completed", Output: long})
	if len(got) <= maxToolOutputChars {
		if len(long) > maxToolOutputChars {
			t.Fatalf("expected truncation marker, got %d bytes", len(got))
		}
	}
	if !strings.Contains(got, "\n") {
		t.Fatal("line structure was collapsed")
	}
	if !strings.HasSuffix(got, "…[truncated]") {
		t.Fatalf("missing truncation marker: %q", got[max(0, len(got)-40):])
	}
}

// TestCompactedMarker proves only a state carrying the marker reports it.
func TestCompactedMarker(t *testing.T) {
	var st nativeToolState
	if st.isCompacted() {
		t.Fatal("a bare state reported compaction")
	}
	if err := json.Unmarshal([]byte(`{"status":"completed","time":{"compacted":1.5}}`), &st); err != nil {
		t.Fatal(err)
	}
	if !st.isCompacted() {
		t.Fatal("the compaction marker was not decoded")
	}
}

// TestMapPartIsPure proves live and replay get identical results for identical
// input — the property that makes one mapper worth having.
func TestMapPartIsPure(t *testing.T) {
	p := nativePart{ID: "p", MessageID: "m", Type: partTool, Tool: "bash", CallID: "c",
		State: nativeToolState{Status: "completed", Output: "out"}}
	a, okA := mapPart("assistant", p, true, nil)
	b, okB := mapPart("assistant", p, true, nil)
	if okA != okB || !reflect.DeepEqual(a, b) {
		t.Fatalf("mapPart is not pure: %+v vs %+v", a, b)
	}
}
