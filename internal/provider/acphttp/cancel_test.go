package acphttp

import (
	"context"
	"testing"
)

func TestCancelSendsACPCancelNotification(t *testing.T) {
	s := newTestSession(t)
	s.agentID = "goose-session"
	fr := &fakeFramer{}
	s.withFramer(fr)

	if err := s.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := fr.calls("session/cancel"); got != 0 {
		t.Fatalf("session/cancel requests = %d, want 0", got)
	}
	params, ok := fr.notification("session/cancel")
	if !ok {
		t.Fatal("session/cancel notification not sent")
	}
	got, ok := params.(map[string]any)
	if !ok {
		t.Fatalf("notification params = %T, want map", params)
	}
	if got["sessionId"] != "goose-session" {
		t.Fatalf("sessionId = %#v, want goose-session", got["sessionId"])
	}
}
