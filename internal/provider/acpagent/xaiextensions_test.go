package acpagent

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMCPServersUpdatedRecordsMembership is MADR 0137 F11.
//
// grok reports its full MCP membership on `_x.ai/mcp/servers_updated`. Unrouted,
// mcremote's server list came only from its own config, so a server grok added
// at runtime was invisible in /status.
func TestMCPServersUpdatedRecordsMembership(t *testing.T) {
	s := newQueueTestSession()
	params := json.RawMessage(`{"mcpServers":[{"name":"magictools","source":"local",` +
		`"type":"http","url":"http://localhost:48080/mcp"}]}`)

	HandleXAIMCPServersUpdated(context.Background(), s, params)

	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if len(s.mcpStatus) != 1 || s.mcpStatus[0].Name != "magictools" {
		t.Fatalf("mcpStatus = %+v, want one entry named magictools", s.mcpStatus)
	}
	// State is not invented. A server that has been named but has reported no
	// status is connecting, not ready — claiming it is up because it was
	// mentioned is exactly the guess /status must not make.
	if s.mcpStatus[0].State != "connecting" {
		t.Fatalf("state = %q, want connecting: membership is not health",
			s.mcpStatus[0].State)
	}
}

// TestMCPServersUpdatedDoesNotOverwriteKnownState proves a membership frame
// arriving after a status frame does not reset a server to "connecting".
func TestMCPServersUpdatedDoesNotOverwriteKnownState(t *testing.T) {
	s := newQueueTestSession()
	HandleMCPStatus(context.Background(), s,
		json.RawMessage(`{"name":"magictools","status":"ready"}`))
	HandleXAIMCPServersUpdated(context.Background(), s,
		json.RawMessage(`{"mcpServers":[{"name":"magictools"}]}`))

	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if len(s.mcpStatus) != 1 {
		t.Fatalf("mcpStatus = %+v, want one entry (no duplicate)", s.mcpStatus)
	}
	if s.mcpStatus[0].State != "ready" {
		t.Fatalf("state = %q, want ready: a membership frame must not "+
			"overwrite observed health", s.mcpStatus[0].State)
	}
}

// TestMCPInitProgressIsLoggedNotEmitted pins the placement decision.
//
// MCP startup is work between the prompt and the first token, so the counter
// is worth having where an operator investigating latency will find it. It is
// deliberately NOT a transcript event: promoting a progress counter to
// something the phone renders is a protocol and UI decision needing its own
// record.
func TestMCPInitProgressIsLoggedNotEmitted(t *testing.T) {
	s := newQueueTestSession()
	HandleXAIMCPInitProgress(context.Background(), s,
		json.RawMessage(`{"total":3,"connected":1,"sessionId":"agent-q"}`))
	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("progress produced %d events; it must not reach the transcript", n)
	}
	if len(s.events) != 0 {
		t.Fatalf("progress emitted %d events; it must not reach the transcript", len(s.events))
	}
}

// TestMCPInitProgressHonoursTheOriginRule: a sub-agent's startup progress is
// not this conversation's, and the rest of this channel already enforces that.
func TestMCPInitProgressHonoursTheOriginRule(t *testing.T) {
	s := newQueueTestSession()
	// Should be ignored entirely rather than panicking or being attributed here.
	HandleXAIMCPInitProgress(context.Background(), s,
		json.RawMessage(`{"total":3,"connected":1,"sessionId":"some-child"}`))
	if len(s.events) != 0 {
		t.Fatalf("a child's progress produced %d events", len(s.events))
	}
}
