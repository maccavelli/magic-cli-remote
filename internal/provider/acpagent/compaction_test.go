package acpagent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// The frames below are built from the SDK's own types. They are the wire
// fixture for MADR 0190 D5 until a live probe replaces them, and they are not a
// capture: no agent Magic runs sends compaction updates yet.

func textBlock(s string) acp.ContentBlock {
	return acp.ContentBlock{Text: &acp.ContentBlockText{Type: "text", Text: s}}
}

func compactionFrame(t *testing.T, u acp.SessionCompactionUpdate) string {
	t.Helper()
	b, err := json.Marshal(acp.SessionUpdate{CompactionUpdate: &u})
	if err != nil {
		t.Fatalf("marshal compaction_update: %v", err)
	}
	return string(b)
}

func chunkFrame(t *testing.T, id string, c acp.ContentBlock, meta map[string]any) string {
	t.Helper()
	b, err := json.Marshal(acp.SessionUpdate{CompactionSummaryChunk: &acp.SessionUpdateCompactionSummaryChunk{
		CompactionId: acp.CompactionId(id), Content: c, Meta: meta,
	}})
	if err != nil {
		t.Fatalf("marshal compaction_summary_chunk: %v", err)
	}
	return string(b)
}

// compactionState is what a compaction event says, for comparison.
type compactionState struct {
	ID, Status, Summary, Error string
	Truncated                  bool
}

func drainCompactions(s *session) []compactionState {
	var out []compactionState
	for len(s.events) > 0 {
		ev := <-s.events
		if ev.Type == event.TypeCompaction {
			out = append(out, compactionState{ev.CompactionID, ev.CompactionStatus,
				ev.CompactionSummary, ev.CompactionError, ev.CompactionTruncated})
		}
	}
	return out
}

// lastCompaction returns the final compaction event, failing when there is none.
func lastCompaction(t *testing.T, s *session) compactionState {
	t.Helper()
	got := drainCompactions(s)
	if len(got) == 0 {
		t.Fatal("no compaction events")
	}
	return got[len(got)-1]
}

// TestCompactionUpdatesFoldByID is PLAN 0190 K1 (MADR 0190 D1): the two ACP
// compaction variants fold into one state per ID, emitted on every update and
// on a first-seen chunk, never on a later chunk.
func TestCompactionUpdatesFoldByID(t *testing.T) {
	t.Run("updates alone", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c1", Status: acp.CompactionStatusInProgress}))
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c1", Status: acp.CompactionStatusCompleted,
			Summary: acp.PatchValue([]acp.ContentBlock{textBlock("Kept the failing test.")})}))
		want := []compactionState{
			{ID: "c1", Status: "in_progress"},
			{ID: "c1", Status: "completed", Summary: "Kept the failing test."},
		}
		if got := drainCompactions(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %+v\nwant     %+v", got, want)
		}
	})

	t.Run("chunks then a completing update without a summary", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, chunkFrame(t, "c2", textBlock("Kept "), nil)) // first-seen: in progress
		deliver(t, s, chunkFrame(t, "c2", textBlock("the parser fix."), nil))
		if got := drainCompactions(s); !reflect.DeepEqual(got, []compactionState{{ID: "c2", Status: "in_progress", Summary: "Kept "}}) {
			t.Fatalf("after two chunks: %+v, want one in_progress event and nothing for the second chunk", got)
		}
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c2", Status: acp.CompactionStatusCompleted}))
		want := []compactionState{{ID: "c2", Status: "completed", Summary: "Kept the parser fix."}}
		if got := drainCompactions(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("completed = %+v, want %+v", got, want)
		}
	})

	t.Run("a completing update's summary replaces the chunks", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, chunkFrame(t, "c3", textBlock("draft"), nil))
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c3", Status: acp.CompactionStatusCompleted,
			Summary: acp.PatchValue([]acp.ContentBlock{textBlock("final")})}))
		got := drainCompactions(s)
		if len(got) != 2 || got[1].Summary != "final" {
			t.Fatalf("events = %+v, want the summary replaced by \"final\"", got)
		}
	})

	t.Run("an omitted field is unchanged and an empty summary clears", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c4", Status: acp.CompactionStatusFailed,
			Error: acp.PatchValue("context store unavailable"), Summary: acp.PatchValue([]acp.ContentBlock{textBlock("partial")})}))
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c4", Status: acp.CompactionStatusCancelled}))
		// Raw JSON: the SDK's encoder drops an empty Summary (omitempty), so a
		// Go agent cannot send this frame, but an agent in another language can.
		deliver(t, s, `{"sessionUpdate":"compaction_update","compactionId":"c4","status":"cancelled","summary":[]}`)
		want := []compactionState{
			{ID: "c4", Status: "failed", Summary: "partial", Error: "context store unavailable"},
			{ID: "c4", Status: "cancelled", Summary: "partial", Error: "context store unavailable"},
			{ID: "c4", Status: "cancelled", Error: "context store unavailable"},
		}
		if got := drainCompactions(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %+v\nwant     %+v", got, want)
		}
	})

	t.Run("an unknown status is passed through", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c5", Status: "paused"}))
		if got := drainCompactions(s); len(got) != 1 || got[0].Status != "paused" {
			t.Fatalf("events = %+v, want status paused as sent", got)
		}
	})
}

// TestCompactionIsBounded: the agent picks the summary's size and the number
// of IDs, so both are capped (MADR 0190 D1).
func TestCompactionIsBounded(t *testing.T) {
	t.Run("summary", func(t *testing.T) {
		s, _ := newUpdateSession()
		piece := strings.Repeat("k", 4096)
		for range (maxCompactionSummary / len(piece)) + 2 {
			deliver(t, s, chunkFrame(t, "big", textBlock(piece), nil))
		}
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "big", Status: acp.CompactionStatusCompleted}))
		last := lastCompaction(t, s)
		if len(last.Summary) != maxCompactionSummary || !last.Truncated {
			t.Fatalf("summary %d bytes, truncated %t; want %d and true", len(last.Summary), last.Truncated, maxCompactionSummary)
		}
	})

	t.Run("summary cut on a rune boundary", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, chunkFrame(t, "utf", textBlock(strings.Repeat("k", maxCompactionSummary-1)), nil))
		deliver(t, s, chunkFrame(t, "utf", textBlock("é"), nil)) // two bytes, one of which fits
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "utf", Status: acp.CompactionStatusCompleted}))
		last := lastCompaction(t, s)
		if len(last.Summary) != maxCompactionSummary-1 || !last.Truncated || strings.ContainsRune(last.Summary, '�') {
			t.Fatalf("summary %d bytes, truncated %t; want %d, true, and no split rune",
				len(last.Summary), last.Truncated, maxCompactionSummary-1)
		}
	})

	t.Run("IDs", func(t *testing.T) {
		s, _ := newUpdateSession()
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "id-0", Status: acp.CompactionStatusInProgress,
			Summary: acp.PatchValue([]acp.ContentBlock{textBlock("old")})}))
		for i := 1; i <= maxCompactions; i++ {
			deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{
				CompactionId: acp.CompactionId("id-" + string(rune('a'+i))), Status: acp.CompactionStatusCompleted}))
		}
		drainCompactions(s)
		// id-0 was the oldest of seventeen, so it was forgotten and starts over.
		deliver(t, s, chunkFrame(t, "id-0", textBlock("new"), nil))
		deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "id-0", Status: acp.CompactionStatusCompleted}))
		got := drainCompactions(s)
		want := []compactionState{{ID: "id-0", Status: "in_progress", Summary: "new"}, {ID: "id-0", Status: "completed", Summary: "new"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %+v\nwant     %+v (the forgotten summary must not come back)", got, want)
		}
		if n := len(s.compactions.order); n != maxCompactions {
			t.Fatalf("tracking %d IDs, want the cap %d", n, maxCompactions)
		}
	})
}

// TestCompactionDeclinesWhatItCannotShow: non-text summary content and _meta
// are declined by name, once per session.
func TestCompactionDeclinesWhatItCannotShow(t *testing.T) {
	s, h := newUpdateSession()
	image := acp.ContentBlock{Image: &acp.ContentBlockImage{Type: "image", Data: "AA==", MimeType: "image/png"}}
	deliver(t, s, chunkFrame(t, "c6", textBlock("Kept "), map[string]any{"x": 1}))
	deliver(t, s, chunkFrame(t, "c6", image, nil))
	deliver(t, s, chunkFrame(t, "c6", image, nil))
	deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c6", Status: acp.CompactionStatusCompleted,
		Meta: acp.PatchValue(map[string]any{"y": 2})}))
	deliver(t, s, compactionFrame(t, acp.SessionCompactionUpdate{CompactionId: "c7", Status: acp.CompactionStatusCompleted,
		Summary: acp.PatchValue([]acp.ContentBlock{textBlock("a"), image, textBlock("b")})}))

	got := drainCompactions(s)
	if len(got) != 3 || got[1].Summary != "Kept " || got[2].Summary != "ab" {
		t.Fatalf("events = %+v; want three, the last two with summaries \"Kept \" and \"ab\"", got)
	}
	variants, silent := h.declined()
	want := []string{"compaction_summary_chunk._meta", "compaction.summary.image", "compaction_update._meta"}
	if silent != 0 || !reflect.DeepEqual(variants, want) {
		t.Fatalf("declined %v (silent %d), want %v once each", variants, silent, want)
	}
}
