package grok

import (
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestAutoAllowPrefersAllowKind(t *testing.T) {
	resp := autoAllow(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "deny", Name: "Deny", Kind: "reject_once"},
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
	})
	if resp.Outcome.Selected == nil {
		t.Fatal("expected selected")
	}
	if string(resp.Outcome.Selected.OptionId) != "allow" {
		t.Fatalf("got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestContentText(t *testing.T) {
	if contentText(acp.TextBlock("hi")) != "hi" {
		t.Fatal("expected hi")
	}
	if contentText(acp.ContentBlock{}) != "" {
		t.Fatal("expected empty")
	}
}

// UserMessageChunk must not emit user_message: Prompt already does, and ACP
// echoes the same prompt (often in chunks), which duplicated UI bubbles.
func TestSessionUpdateIgnoresUserMessageChunk(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
				Content: acp.TextBlock("hello twice?"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.events:
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(20 * time.Millisecond):
		// ok
	}
}
