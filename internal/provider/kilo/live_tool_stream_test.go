//go:build live_kilo

package kilo_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// TestLiveToolStreamDynamics proves the raw-tool-frame hook (MADR 0076 L2,
// restored parity with opencode's MADR 0034 Phase 0 instrumentation) actually
// fires against a real kilo serve turn, and pins the two invariants the
// downstream tool lane depends on: state.output only grows across frames for
// one call, and a terminal status (completed/error/failed) is always the last
// frame for that call — never followed by a stale non-terminal repeat.
func TestLiveToolStreamDynamics(t *testing.T) {
	var mu sync.Mutex
	var rawFrames []kilo.RawToolPartFrame

	hook := func(frame kilo.RawToolPartFrame) {
		mu.Lock()
		rawFrames = append(rawFrames, frame)
		mu.Unlock()
	}

	p := kilo.NewHTTPWithToolFrameHook(kilo.Config{AlwaysApprove: true, Model: liveModel()}, nil, hook)
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-tool-stream-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Execute this bash command exactly: for i in $(seq 1 8); do echo \"line $i\"; sleep 0.2; done"}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	deadline := time.After(120 * time.Second)
	var sawComplete bool
	for !sawComplete {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events channel closed before turn_complete")
			}
			if ev.Type == event.TypeTurnComplete {
				sawComplete = true
			} else if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn completion")
		}
	}

	mu.Lock()
	frames := make([]kilo.RawToolPartFrame, len(rawFrames))
	copy(frames, rawFrames)
	mu.Unlock()

	if len(frames) == 0 {
		t.Skip("the model ran no tools this turn; nothing to assert")
	}

	byCall := map[string][]kilo.RawToolPartFrame{}
	var order []string
	for _, f := range frames {
		id := f.CallID
		if id == "" {
			id = f.PartID
		}
		if id == "" {
			continue
		}
		if _, seen := byCall[id]; !seen {
			order = append(order, id)
		}
		byCall[id] = append(byCall[id], f)
	}

	for _, id := range order {
		callFrames := byCall[id]
		lastOutLen := -1
		for i, f := range callFrames {
			outLen := len(f.Output)
			if lastOutLen >= 0 && outLen < lastOutLen {
				t.Errorf("tool %s: state.output shrank across frames (frame %d: %d -> %d bytes)",
					id, i, lastOutLen, outLen)
			}
			lastOutLen = outLen
			terminal := f.Status == "completed" || f.Status == "error" || f.Status == "failed"
			if terminal && i != len(callFrames)-1 {
				t.Errorf("tool %s: terminal status %q at frame %d of %d, not the last frame",
					id, f.Status, i, len(callFrames))
			}
		}
	}
	t.Logf("captured %d raw tool frames across %d call(s)", len(frames), len(order))
}

// TestLiveToolLaneKeepsTerminalState is the acceptance test for the chunkbuf
// tool lane (MADR 0042 D4, ported from opencode's live_tool_stream_test.go).
// The lane holds non-terminal in-place tool updates so a `bash` streaming its
// output does not cost one WebSocket frame per SSE delta — but a coalescer
// that swallowed a terminal status would pin a tool card on "running"
// forever, which is strictly worse than the burst it fixes.
//
// So the hard assertion is the safety property: every tool that reached a
// terminal state upstream must reach one downstream too, carrying its output.
func TestLiveToolLaneKeepsTerminalState(t *testing.T) {
	var mu sync.Mutex
	rawCount := map[string]int{}
	rawTerminal := map[string]bool{}

	hook := func(f kilo.RawToolPartFrame) {
		id := f.CallID
		if id == "" {
			id = f.PartID
		}
		if id == "" {
			return
		}
		mu.Lock()
		rawCount[id]++
		if f.Status == "completed" || f.Status == "error" || f.Status == "failed" {
			rawTerminal[id] = true
		}
		mu.Unlock()
	}

	p := kilo.NewHTTPWithToolFrameHook(kilo.Config{AlwaysApprove: true, Model: liveModel()}, nil, hook)
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-tool-lane-acceptance", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Execute this bash command exactly: for i in $(seq 1 12); do echo \"tick $i\"; sleep 0.2; done"}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	emitted := map[string][]event.Event{}
	var order []string
	deadline := time.After(180 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events channel closed before turn_complete")
			}
			switch ev.Type {
			case event.TypeToolCall, event.TypeToolUpdate:
				if ev.ToolID == "" {
					continue
				}
				if _, seen := emitted[ev.ToolID]; !seen {
					order = append(order, ev.ToolID)
				}
				emitted[ev.ToolID] = append(emitted[ev.ToolID], ev)
			case event.TypeTurnComplete:
				done = true
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn completion")
		}
	}

	if len(order) == 0 {
		t.Skip("the model ran no tools this turn; nothing to assert")
	}

	mu.Lock()
	defer mu.Unlock()

	var checked int
	for _, id := range order {
		evs := emitted[id]
		last := evs[len(evs)-1]
		t.Logf("tool %s: %d raw frames -> %d emitted events, final status %q (%d bytes of detail)",
			id, rawCount[id], len(evs), last.Status, len(last.Text))

		if len(evs) > rawCount[id] {
			t.Errorf("tool %s emitted %d events from %d raw frames — the lane must never manufacture events",
				id, len(evs), rawCount[id])
		}
		if !rawTerminal[id] {
			continue
		}
		checked++
		if !event.IsTerminalToolStatus(last.Status) {
			t.Errorf("tool %s reached a terminal state upstream but its last emitted event was %q — "+
				"a held update was lost and the card is pinned mid-flight",
				id, last.Status)
		}
		if strings.TrimSpace(last.Text) == "" {
			t.Errorf("tool %s finished with no detail: the terminal payload was dropped", id)
		}
	}

	if checked == 0 {
		t.Skip("no tool reached a terminal state upstream this turn")
	}
	t.Logf("verified terminal state survived coalescing for %d tool(s)", checked)
}
