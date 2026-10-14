package session

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func sendEvent(p *metadataProvider, ev event.Event) {
	s := p.session
	ev.SessionID = s.id
	s.events <- ev
	s.events <- event.Event{Type: event.TypeThoughtChunk, SessionID: s.id, Text: "sync"}
}

func agentNotice(severity, title, description string) event.Event {
	return event.Event{Type: event.TypeAgentNotice, NoticeSeverity: severity, NoticeTitle: title, NoticeDescription: description}
}

func agentNoticesInHistory(mgr *Manager, id string) []string {
	var out []string
	for _, ev := range mgr.History(id) {
		if ev.Type == event.TypeAgentNotice {
			out = append(out, ev.NoticeSeverity+"|"+ev.NoticeTitle+"|"+ev.NoticeDescription)
		}
	}
	return out
}

// TestAgentNoticeIsKeptAndDeduplicated is PLAN 0188 N1 step 4: an agent
// notice is kept in history for a client that reconnects, and one identical
// to the notice before it is dropped (MADR 0188 D2).
func TestAgentNoticeIsKeptAndDeduplicated(t *testing.T) {
	mgr, p := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	sendEvent(p, agentNotice("warning", "gopls unavailable", "Navigation is limited."))
	sendEvent(p, agentNotice("warning", "gopls unavailable", "Navigation is limited.")) // repeat: dropped
	sendEvent(p, agentNotice("error", "gopls unavailable", "Navigation is limited."))   // new severity: kept
	sendEvent(p, agentNotice("error", "gopls unavailable", "Restart the server."))      // new detail: kept
	// A daemon notice with the same words is a different message.
	sendEvent(p, event.Event{Type: event.TypeNotice, Text: "gopls unavailable"})

	got := agentNoticesInHistory(mgr, meta.ID)
	want := []string{
		"warning|gopls unavailable|Navigation is limited.",
		"error|gopls unavailable|Navigation is limited.",
		"error|gopls unavailable|Restart the server.",
	}
	if len(got) != len(want) {
		t.Fatalf("agent notices in history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("agent notices in history = %v, want %v", got, want)
		}
	}
	daemon := 0
	for _, ev := range mgr.History(meta.ID) {
		if ev.Type == event.TypeNotice {
			daemon++
		}
	}
	if daemon != 1 {
		t.Fatalf("daemon notices in history = %d, want 1", daemon)
	}
}
