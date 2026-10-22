package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/output"
)

// TestToolOutputsEvictOldestFirst is PLAN 0208 P2 step 5: the store keeps whole
// outputs, replaces a tool's earlier one, and evicts the oldest tool once the
// session's bytes are over budget.
func TestToolOutputsEvictOldestFirst(t *testing.T) {
	var s toolOutputs
	big := strings.Repeat("x", maxToolOutputBytes/3)
	s.put("a", toolOutput{text: big})
	s.put("b", toolOutput{text: big, diffs: []event.ToolDiff{{Path: "/f", NewText: "n"}}})
	if got, ok := s.get("a"); !ok || got.text != big {
		t.Fatalf("a = %d bytes, %v; want it kept whole", len(got.text), ok)
	}
	if got, ok := s.get("b"); !ok || len(got.diffs) != 1 {
		t.Fatalf("b = %+v, %v; want its diff kept", got.diffs, ok)
	}

	s.put("a", toolOutput{text: "replaced"})
	if got, _ := s.get("a"); got.text != "replaced" {
		t.Fatalf("a = %q, want the replacement", got.text)
	}

	s.put("c", toolOutput{text: big})
	s.put("d", toolOutput{text: big})
	if _, ok := s.get("b"); ok {
		t.Fatal("b survived; the oldest tool must go first once over budget")
	}
	for _, id := range []string{"a", "c", "d"} {
		if _, ok := s.get(id); !ok {
			t.Fatalf("%s was evicted; only the oldest should be", id)
		}
	}
	if s.bytes > maxToolOutputBytes {
		t.Fatalf("store holds %d bytes, over %d", s.bytes, maxToolOutputBytes)
	}

	s.drop("c")
	if _, ok := s.get("c"); ok {
		t.Fatal("c survived drop")
	}
}

// TestManagerCutsToolEventsAndKeepsTheWhole is PLAN 0208 P2 steps 1–3: a tool
// event over the phone's bound reaches history cut and marked truncated, its
// diffs cut too, and ToolOutput returns the whole of both to the owner only.
// A short tool event is left alone and has nothing to fetch.
func TestManagerCutsToolEventsAndKeepsTheWhole(t *testing.T) {
	p := &scriptedProvider{count: 0}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("0123456789abcdef\n", 1000) // 17,000 bytes, many lines
	bigDiff := event.ToolDiff{Path: "/f.go", OldText: strings.Repeat("o", 9000), NewText: strings.Repeat("n", 9000)}
	p.last.events <- event.Event{Type: event.TypeToolUpdate, SessionID: meta.ID, ToolID: "t-long",
		Status: "completed", Text: long, Diffs: []event.ToolDiff{bigDiff}}
	p.last.events <- event.Event{Type: event.TypeToolUpdate, SessionID: meta.ID, ToolID: "t-short",
		Status: "completed", Text: "ok"}

	var gotLong, gotShort *event.Event
	deadline := time.Now().Add(2 * time.Second)
	for (gotLong == nil || gotShort == nil) && time.Now().Before(deadline) {
		for _, ev := range mgr.History(meta.ID) {
			switch ev.ToolID {
			case "t-long":
				gotLong = &ev
			case "t-short":
				gotShort = &ev
			}
		}
		time.Sleep(time.Millisecond)
	}
	if gotLong == nil || gotShort == nil {
		t.Fatal("tool events never reached history")
	}
	if !gotLong.Truncated || len(gotLong.Text) > output.MaxToolShown+len(output.Truncated) {
		t.Fatalf("long event: truncated=%v, %d bytes; want it cut to %d", gotLong.Truncated, len(gotLong.Text), output.MaxToolShown)
	}
	if !strings.HasSuffix(gotLong.Text, output.Truncated) || strings.Count(gotLong.Text, "\n") < 100 {
		t.Fatalf("long event lost its lines or its marker: %q…", gotLong.Text[:40])
	}
	if d := gotLong.Diffs; len(d) != 1 || len(d[0].OldText) > output.MaxToolShown+len(output.Truncated) ||
		len(d[0].NewText) > output.MaxToolShown+len(output.Truncated) {
		t.Fatalf("long event's diff was not cut: %d diffs", len(d))
	}
	if gotShort.Truncated || gotShort.Text != "ok" {
		t.Fatalf("short event = %+v, want it untouched", *gotShort)
	}

	text, diffs, err := mgr.ToolOutput(meta.ID, "t-long", "device-a")
	if err != nil || text != strings.TrimRight(long, "\n") || len(diffs) != 1 || diffs[0] != bigDiff {
		t.Fatalf("ToolOutput = %d bytes, %d diffs, %v; want the whole of both", len(text), len(diffs), err)
	}
	if _, _, err := mgr.ToolOutput(meta.ID, "t-long", "device-b"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("another device read the output: err=%v", err)
	}
	if _, _, err := mgr.ToolOutput(meta.ID, "t-short", "device-a"); !errors.Is(err, ErrToolOutputNotFound) {
		t.Fatalf("an uncut tool had output to fetch: err=%v", err)
	}
}

// TestToolOutputsKeepsAnOversizedNewest: one output larger than the budget is
// still kept, alone, rather than evicting itself.
func TestToolOutputsKeepsAnOversizedNewest(t *testing.T) {
	var s toolOutputs
	s.put("small", toolOutput{text: "s"})
	s.put("huge", toolOutput{text: strings.Repeat("y", maxToolOutputBytes+1)})
	if _, ok := s.get("huge"); !ok {
		t.Fatal("the newest output evicted itself")
	}
	if _, ok := s.get("small"); ok {
		t.Fatal("small survived an over-budget put")
	}
}
