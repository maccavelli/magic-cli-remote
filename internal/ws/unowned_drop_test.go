package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// dropRecords returns the log records for events dropped as unowned.
func dropRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if line == "" || json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["msg"] == "event dropped: its session has no known owner" {
			out = append(out, rec)
		}
	}
	return out
}

// TestEventsForUnownedSessionsAreLoggedOncePerSession is MADR 0201 D3 (0073
// F-T1, first half). BroadcastEvent drops an event whose session has no known
// owner; that drop was silent. It is now logged once per session id — at info
// when the session is unknown, at debug when it was purged on purpose, where
// dropping its late events is correct.
func TestEventsForUnownedSessionsAreLoggedOncePerSession(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	mgr := session.NewManager(reg, store, nil, nil)
	buf := &bytes.Buffer{}
	s := New(Options{
		Sessions: mgr,
		Registry: reg,
		Version:  "test",
		Log:      slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	s.BroadcastEvent(event.Event{Type: event.TypeAssistantChunk, SessionID: "ghost-1"})
	s.BroadcastEvent(event.Event{Type: event.TypeAssistantChunk, SessionID: "ghost-1"})
	s.BroadcastEvent(event.Event{Type: event.TypeTurnComplete, SessionID: "ghost-2"})

	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{}, "dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Delete(ctx, meta.ID, "dev-1"); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	s.BroadcastEvent(event.Event{Type: event.TypeAssistantChunk, SessionID: "ghost-1"})
	s.BroadcastEvent(event.Event{Type: event.TypeAssistantChunk, SessionID: "ghost-3"})
	s.BroadcastEvent(event.Event{Type: event.TypeAssistantChunk, SessionID: meta.ID})

	recs := dropRecords(t, buf)
	if len(recs) != 2 {
		t.Fatalf("after reset: %d drop records, want 2 (ghost-3, and the purged session):\n%s", len(recs), buf.String())
	}
	if recs[0]["session_id"] != "ghost-3" || recs[0]["level"] != "INFO" {
		t.Fatalf("unknown session record = %v; want INFO for ghost-3", recs[0])
	}
	if recs[1]["session_id"] != meta.ID || recs[1]["level"] != "DEBUG" {
		t.Fatalf("purged session record = %v; want DEBUG for %s", recs[1], meta.ID)
	}
}
