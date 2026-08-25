package codex

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestEmitCapabilities pins MADR 0035 D4: at session create, codex emits
// exactly one session_capabilities with image=true and load_session=true.
// ACP-specific fields (mcp_http, embedded_context, etc.) stay false.
func TestEmitCapabilities(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 4),
		done:    make(chan struct{}),
		agentID: "thread-xyz",
		log:     silentLogger(),
	}
	defer close(s.done)

	s.emitCapabilities()

	select {
	case ev := <-s.events:
		if ev.Type != event.TypeSessionCapabilities {
			t.Fatalf("type=%s, want session_capabilities", ev.Type)
		}
		if ev.SessionID != s.localID {
			t.Errorf("sessionID=%q, want %q", ev.SessionID, s.localID)
		}
		if ev.AgentSessionID != "thread-xyz" {
			t.Errorf("agentSessionID=%q, want thread-xyz", ev.AgentSessionID)
		}
		if ev.Capabilities == nil {
			t.Fatal("capabilities nil")
		}
		if !ev.Capabilities.Image {
			t.Error("Image should be true (MADR 0028 §16.3: all 6 models advertise image)")
		}
		if !ev.Capabilities.LoadSession {
			t.Error("LoadSession should be true (MADR 0028 §16.3: thread/resume verified)")
		}
		if ev.Capabilities.Audio {
			t.Error("Audio should be false: codex advertises no audio modality")
		}
		// ACP-specific fields must NOT be claimed for codex.
		if ev.Capabilities.EmbeddedContext || ev.Capabilities.MCPHTTP ||
			ev.Capabilities.MCPSSE || ev.Capabilities.MCPACP {
			t.Error("ACP-specific capability fields must stay false for codex")
		}
		if !ev.Capabilities.ListSessions || ev.Capabilities.CloseSession {
			t.Error("Codex must advertise native listing but not conflate close with delete")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}

	// No second event.
	select {
	case ev := <-s.events:
		t.Fatalf("unexpected second event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}
