package acpagent

import (
	"context"
	"encoding/json"
	"testing"
)

// xaiFrame is one `_x.ai/session_notification` as grok serializes it
// (xai-grok-shell extensions/notification.rs, `tag = "sessionUpdate"`,
// snake_case fields).
func xaiFrame(session, update string) json.RawMessage {
	return json.RawMessage(`{"sessionId":"` + session + `","update":` + update + `}`)
}

// TestXAIManualCompactShowsACompaction is the regression for MADR 0208's P4b
// deviation 1. grok's `/compact` compacts, then reports it with a lone
// auto_compact_completed (compaction.rs run_compact: no started before it).
// Magic dropped the variant, so the turn looked silent and `/compact` was
// about to be marked unavailable.
func TestXAIManualCompactShowsACompaction(t *testing.T) {
	s := newQueueTestSession()
	HandleXAISessionNotification(context.Background(), s, xaiFrame("agent-q",
		`{"sessionUpdate":"auto_compact_completed","tokens_before":42000,"tokens_after":9000}`))

	got := drainCompactions(s)
	want := []compactionState{{ID: "xai-1", Status: "completed"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("compactions = %+v, want %+v", got, want)
	}
}

// TestXAIAutoCompactFoldsIntoOneCompaction covers grok's automatic path:
// started opens the compaction, and the terminal variant closes the same one.
func TestXAIAutoCompactFoldsIntoOneCompaction(t *testing.T) {
	cases := []struct {
		name     string
		terminal string
		want     compactionState
	}{
		{"completed",
			`{"sessionUpdate":"auto_compact_completed","tokens_after":9000,"elapsed_ms":812,"summary_preview":" grok's own words "}`,
			compactionState{ID: "xai-1", Status: "completed", Summary: "grok's own words"}},
		{"failed",
			`{"sessionUpdate":"auto_compact_failed","error":" context too large "}`,
			compactionState{ID: "xai-1", Status: "failed", Error: "context too large"}},
		{"cancelled",
			`{"sessionUpdate":"auto_compact_cancelled","reason":"user_cancelled"}`,
			compactionState{ID: "xai-1", Status: "cancelled"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newQueueTestSession()
			HandleXAISessionNotification(context.Background(), s, xaiFrame("agent-q",
				`{"sessionUpdate":"auto_compact_started","tokens_used":200000,`+
					`"context_window":256000,"percentage":82,"reason":"Context window 82% full"}`))
			HandleXAISessionNotification(context.Background(), s, xaiFrame("agent-q", tc.terminal))

			got := drainCompactions(s)
			if len(got) != 2 {
				t.Fatalf("got %d compaction events, want 2: %+v", len(got), got)
			}
			if started := (compactionState{ID: "xai-1", Status: "in_progress"}); got[0] != started {
				t.Fatalf("first event = %+v, want %+v", got[0], started)
			}
			if got[1] != tc.want {
				t.Fatalf("terminal event = %+v, want %+v", got[1], tc.want)
			}
		})
	}
}

// TestXAICompactionsAreNumbered proves a closed compaction is not reopened: the
// next one grok reports is a new compaction, so the phone shows two.
func TestXAICompactionsAreNumbered(t *testing.T) {
	s := newQueueTestSession()
	for _, u := range []string{
		`{"sessionUpdate":"auto_compact_started","reason":"x"}`,
		`{"sessionUpdate":"auto_compact_cancelled","reason":"user_cancelled"}`,
		`{"sessionUpdate":"auto_compact_completed","tokens_after":1}`,
	} {
		HandleXAISessionNotification(context.Background(), s, xaiFrame("agent-q", u))
	}
	got := drainCompactions(s)
	if len(got) != 3 || got[1].ID != "xai-1" || got[2].ID != "xai-2" {
		t.Fatalf("compactions = %+v, want xai-1 twice then xai-2", got)
	}
}

// TestXAICompactionOfAnotherSessionIsNotShown keeps the channel's origin rule:
// a sub-agent's frames arrive here too, and its compaction is not this one.
func TestXAICompactionOfAnotherSessionIsNotShown(t *testing.T) {
	s := newQueueTestSession()
	HandleXAISessionNotification(context.Background(), s, xaiFrame("some-child",
		`{"sessionUpdate":"auto_compact_completed","tokens_after":1}`))
	if got := drainCompactions(s); len(got) != 0 {
		t.Fatalf("compactions = %+v for another session, want none", got)
	}
}
