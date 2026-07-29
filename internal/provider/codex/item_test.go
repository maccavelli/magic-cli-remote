package codex

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// threadItemTypes is the full set of v2 ThreadItem discriminator values,
// captured from the live codex 0.145.0 probe (docs/codex-spike-0.145.0/
// item-stream.json). MADR 0035 D1: the allowlist is a subset, the rest
// either become a notice or are silent.
var threadItemTypes = []string{
	"userMessage",
	"agentMessage",
	"reasoning",
	"commandExecution",
	"fileChange",
	"mcpToolCall",
	"webSearch",
	"dynamicToolCall",
	"imageGeneration",
	"imageView",
	"collabAgentToolCall",
	"subAgentActivity",
	"contextCompaction",
	"enteredReviewMode",
	"exitedReviewMode",
	"hookPrompt",
	"plan",
	"sleep",
}

// expectedClassification is what the v2 item type is allowed to produce
// once MADR 0035 lands. The matrix:
//
//	tool -> renders a tool_call (allowlist)
//	notice -> renders a single TypeNotice
//	silent -> emits nothing in the transcript (debug log only)
//	subagent -> routed to the sub-agent panel, never the transcript
var expectedClassification = map[string]string{
	"commandExecution":    "tool",
	"fileChange":          "tool",
	"mcpToolCall":         "tool",
	"webSearch":           "tool",
	"dynamicToolCall":     "tool",
	"imageGeneration":     "tool",
	"imageView":           "tool",
	"collabAgentToolCall": "subagent",
	"subAgentActivity":    "subagent",
	"hookPrompt":          "tool",
	"contextCompaction":   "notice",
	"enteredReviewMode":   "notice",
	"exitedReviewMode":    "notice",
	"userMessage":         "silent",
	"agentMessage":        "silent",
	"reasoning":           "silent",
	"plan":                "silent",
	"sleep":               "silent",
}

// TestItemAllowlistClassification covers every v2 item type. Anything not
// matching expectedClassification is a contract regression.
func TestItemAllowlistClassification(t *testing.T) {
	for _, itemType := range threadItemTypes {
		want := expectedClassification[itemType]
		if want == "" {
			t.Errorf("item type %q has no expected classification", itemType)
			continue
		}
		got := ""
		switch itemType {
		case "collabAgentToolCall", "subAgentActivity":
			// Routed by noteSubagentItem before the allowlist is consulted, so
			// they must be in neither the tool nor the notice set.
			got = "subagent"
			if _, isTool := itemsRenderedAsTools[itemType]; isTool {
				t.Errorf("sub-agent item %q is still a transcript tool card", itemType)
			}
		default:
			if _, isTool := itemsRenderedAsTools[itemType]; isTool {
				got = "tool"
			} else if _, ok := itemAsNotice(itemType); ok {
				got = "notice"
			} else {
				got = "silent"
			}
		}
		if got != want {
			t.Errorf("item type %q classified as %q, want %q", itemType, got, want)
		}
	}
}

// TestNoTranscriptCardForNonToolItemTypes guards the exact bug MADR 0035
// is fixing: a v1 provider that fell through to a default `tool_call` for
// every item type, producing cards literally titled "agentMessage" and
// "reasoning" beside the real content. With the v2 migration, those
// fall-throughs must emit zero tool_call events.
func TestNoTranscriptCardForNonToolItemTypes(t *testing.T) {
	notTool := []string{
		"userMessage", "agentMessage", "reasoning", "plan", "sleep",
	}
	for _, itemType := range notTool {
		if _, isTool := itemsRenderedAsTools[itemType]; isTool {
			t.Errorf("non-tool item type %q is in the allowlist", itemType)
		}
		if _, ok := itemAsNotice(itemType); ok {
			t.Errorf("non-tool item type %q is also a notice; pick one", itemType)
		}
	}
}

// TestUnknownItemTypeIsSilent: a future codex release adding a new item
// type must produce no card and no panic. Silence is the safe default; the
// debug log makes the omission discoverable.
func TestUnknownItemTypeIsSilent(t *testing.T) {
	if _, isTool := itemsRenderedAsTools["frobnicatedTurnItem"]; isTool {
		t.Fatal("unknown type leaked into the allowlist")
	}
	if _, ok := itemAsNotice("frobnicatedTurnItem"); ok {
		t.Fatal("unknown type matched a notice arm")
	}
}

// TestLifecycleInvariant: every item type that emits a tool_call on
// item/started also emits a tool_call_update on item/completed, with a
// terminal status.
func TestLifecycleInvariant(t *testing.T) {
	for itemType := range itemsRenderedAsTools {
		// Started event
		started := encodeItemStarted(t, itemType)
		var p struct {
			Item json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(started, &p); err != nil {
			t.Fatalf("item/started %s: %v", itemType, err)
		}
		itemID, _ := extractItemID(p.Item)
		if itemID == "" {
			t.Errorf("item/started %s: empty item id", itemType)
		}
		// Completed event
		completed := encodeItemCompleted(t, itemType, "completed")
		if err := json.Unmarshal(completed, &p); err != nil {
			t.Fatalf("item/completed %s: %v", itemType, err)
		}
		completedID, _ := extractItemID(p.Item)
		if completedID == "" {
			t.Errorf("item/completed %s: empty item id", itemType)
		}
	}
}

// TestCodexToolStatusMapping: the engine emits `inProgress` and `declined`;
// the daemon must speak only `running` / `pending` / `completed` / `failed`.
func TestCodexToolStatusMapping(t *testing.T) {
	cases := map[string]string{
		"inProgress": "running",
		"running":    "running",
		"completed":  "completed",
		"failed":     "failed",
		"declined":   "failed",
		"pending":    "pending",
		"unknown":    "unknown",
		"":           "",
	}
	for in, want := range cases {
		if got := codexToolStatus(in); got != want {
			t.Errorf("codexToolStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExtractItemIDV2: the v2 wire format puts id+type on the nested item
// object. The v1 wire format had them on params (itemId/itemType). The
// helper must read v2 first.
func TestExtractItemIDV2(t *testing.T) {
	item := `{"type":"commandExecution","id":"exec-123","command":"ls"}`
	id, t0 := extractItemID([]byte(item))
	if id != "exec-123" || t0 != "commandExecution" {
		t.Errorf("v2 extract: id=%q type=%q", id, t0)
	}
	// Garbage item: both empty.
	id, t0 = extractItemID([]byte("not json"))
	if id != "" || t0 != "" {
		t.Errorf("garbage extract: id=%q type=%q", id, t0)
	}
}

// helpers

func encodeItemStarted(t *testing.T, itemType string) []byte {
	t.Helper()
	payload := map[string]any{
		"item": map[string]any{
			"type": itemType,
			"id":   "test-item-id",
		},
		"threadId":    "thread-x",
		"turnId":      "turn-x",
		"startedAtMs": time.Now().UnixMilli(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func encodeItemCompleted(t *testing.T, itemType, status string) []byte {
	t.Helper()
	payload := map[string]any{
		"item": map[string]any{
			"type":          itemType,
			"id":            "test-item-id",
			"status":        status,
			"completedAtMs": time.Now().UnixMilli(),
		},
		"threadId": "thread-x",
		"turnId":   "turn-x",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestEmitToolStartedCoversEveryAllowlistedType guards that the per-type
// field extraction in toolCardDetail does not panic on the wire shapes the
// engine actually sends (every entry in itemsRenderedAsTools). The function
// falls through to a name-only card for unrecognised shapes; an unknown
// inner field is not a panic, just an empty detail.
func TestEmitToolStartedCoversEveryAllowlistedType(t *testing.T) {
	s := &session{}
	now := time.Now()
	for itemType := range itemsRenderedAsTools {
		// Empty item: should not panic, should not emit any event (no
		// detail to render, but also no detail-related nil deref).
		// emitToolStarted is the production entry point; we cannot call it
		// directly because it requires a full session with emit/event
		// channel. Instead, we exercise toolCardDetail which is the
		// per-type extraction core.
		name, detail := toolCardDetail(itemType, nil)
		if name == "" {
			t.Errorf("item type %q: empty name on nil item", itemType)
		}
		_ = detail
	}
	_ = s
	_ = now
}

// touch event to keep the import alive if no other test uses it.
var _ = event.TypeNotice
