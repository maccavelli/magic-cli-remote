package acpagent

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestACompactionArrivesOverTheWire is PLAN 0190 K3: once Magic advertises
// session.compaction, an agent's compaction_update sent over a real
// connection, through the SDK's capability gate as production configures it,
// reaches the session as a compaction event. Without the advertisement the SDK
// refuses it before the handler runs (MADR 0190 D4).
func TestACompactionArrivesOverTheWire(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	a.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "agent-1",
			"update": map[string]any{
				"sessionUpdate": "compaction_update", "compactionId": "c1", "status": "completed",
				"summary": []any{map[string]any{"type": "text", "text": "Kept the parser fix."}},
			},
		},
	})
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeCompaction {
				if ev.CompactionID != "c1" || ev.CompactionStatus != "completed" || ev.CompactionSummary != "Kept the parser fix." {
					t.Fatalf("compaction = %q/%q/%q", ev.CompactionID, ev.CompactionStatus, ev.CompactionSummary)
				}
				return
			}
		case <-deadline:
			t.Fatal("no compaction within 5s: the update did not get past the SDK's capability gate")
		}
	}
}
