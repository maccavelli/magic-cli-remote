package session

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// logSink is a goroutine-safe JSON log buffer.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// records returns the logged records whose msg contains substr.
func (s *logSink) records(t *testing.T, substr string) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if msg, _ := rec["msg"].(string); strings.Contains(msg, substr) {
			out = append(out, rec)
		}
	}
	return out
}

func newReplayManager(t *testing.T) (*Manager, *metadataProvider, *logSink, Meta) {
	t.Helper()
	sink := &logSink{}
	log := slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, log, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(context.Background(), meta.ID, "dev-a") })
	return mgr, p, sink, meta
}

// send delivers ev to the pump. The channel is unbuffered, so the pump has
// finished with an event once it takes the next; flush sends two live
// markers so everything before them has been fully handled.
func send(p *metadataProvider, ev event.Event) {
	ev.SessionID = p.session.id
	p.session.events <- ev
}

func flush(p *metadataProvider) {
	send(p, event.Event{Type: event.TypeThoughtChunk, Text: "sync"})
	send(p, event.Event{Type: event.TypeThoughtChunk, Text: "sync"})
}

func replayChunk(text string) event.Event {
	return event.Event{Type: event.TypeAssistantChunk, Text: text, Replay: true}
}

// TestReplaySuppressionIsCountedAndLogged is MADR 0201 D12 (0073 F-S2).
// Replay-marked events are kept out of live delivery; a provider whose replay
// flag stuck on would silence a session with no trace. A burst is now counted
// and logged once, at debug, when the first live event ends it.
func TestReplaySuppressionIsCountedAndLogged(t *testing.T) {
	_, p, sink, _ := newReplayManager(t)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		send(p, replayChunk(s))
	}
	flush(p)

	recs := sink.records(t, "replay events withheld")
	if len(recs) != 1 {
		t.Fatalf("withheld-replay records = %d, want 1: %v", len(recs), recs)
	}
	if lvl := recs[0]["level"]; lvl != "DEBUG" {
		t.Fatalf("level = %v, want DEBUG", lvl)
	}
	if n := recs[0]["count"]; n != float64(5) {
		t.Fatalf("count = %v, want 5", n)
	}
}

// TestAReplayEventDuringATurnWarns is MADR 0201 D12: a replay-marked event
// while a turn is in flight is the signature of a stuck replay flag, so it
// warns, naming the type, once per turn.
func TestAReplayEventDuringATurnWarns(t *testing.T) {
	mgr, p, sink, meta := newReplayManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	send(p, replayChunk("stuck 1"))
	send(p, replayChunk("stuck 2"))
	flush(p)

	recs := sink.records(t, "replay-marked event during a turn")
	if len(recs) != 1 {
		t.Fatalf("during-turn warnings = %d, want 1: %v", len(recs), recs)
	}
	if lvl := recs[0]["level"]; lvl != "WARN" {
		t.Fatalf("level = %v, want WARN", lvl)
	}
	if typ := recs[0]["type"]; typ != string(event.TypeAssistantChunk) {
		t.Fatalf("type = %v, want %s", typ, event.TypeAssistantChunk)
	}
}
