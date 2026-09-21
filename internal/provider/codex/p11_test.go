package codex

import (
	"context"
	"encoding/json"
	"testing"
)

// TestThreadShellBoundsTheCommandInsideTheEngine is acceptance criterion A21 of
// PLAN 0163. Omitting timeoutMs is not neutral: Codex then applies a one-hour
// default (app-server-protocol v2/thread.rs:1138-1140), so a command we have
// stopped watching outlives the session that asked for it.
//
// Zero is asserted against explicitly because it is the plausible wrong answer —
// upstream documents that zero "requests an immediate timeout, not unlimited
// execution", so a bound that computed 0 would look like a bound and behave like
// an instant kill.
func TestThreadShellBoundsTheCommandInsideTheEngine(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/shellCommand": {json.RawMessage(`{}`)},
	}}
	api := newExecutionAPI(stub.send, func(CapabilityID) bool { return true }, nil, 1, nil)
	if _, err := api.RunThreadShell(context.Background(), "thread-1", "printf hi"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(stub.requests))
	}
	got, ok := stub.requests[0].params["timeoutMs"].(int64)
	if !ok {
		t.Fatalf("timeoutMs = %#v (%T), want int64; a float or string is a different JSON type upstream",
			stub.requests[0].params["timeoutMs"], stub.requests[0].params["timeoutMs"])
	}
	if got != threadShellTimeout.Milliseconds() {
		t.Errorf("timeoutMs = %d, want %d", got, threadShellTimeout.Milliseconds())
	}
	if got <= 0 {
		t.Errorf("timeoutMs = %d; zero or negative is rejected upstream, and zero means immediate timeout", got)
	}
}

// TestMCPStatusListSurfacesRuntimeStatusAndToolsError is acceptance criterion A24
// of PLAN 0163. authStatus alone cannot distinguish an authenticated server that
// is not running from one that is, and before this the refresh replaced the map
// with entries that had no error at all.
func TestMCPStatusListSurfacesRuntimeStatusAndToolsError(t *testing.T) {
	p := New(Config{})
	p.applyMCPStatusList(json.RawMessage(`{"data":[
		{"name":"broken","authStatus":"oauth","runtimeStatus":"failed","toolsError":"spawn ENOENT"},
		{"name":"healthy","authStatus":"oauth","runtimeStatus":"connected","toolsError":null},
		{"name":"older","authStatus":"unknown"}
	],"nextCursor":null}`))
	servers := p.RuntimeSnapshot().MCPServers
	if len(servers) != 3 {
		t.Fatalf("servers = %d, want 3", len(servers))
	}
	byName := make(map[string]RuntimeMCPServer, len(servers))
	for _, server := range servers {
		byName[server.Name] = server
	}
	if got := byName["broken"]; got.RuntimeStatus != "failed" || got.Error != "spawn ENOENT" {
		t.Errorf("broken = %+v, want runtimeStatus failed and the tools error", got)
	}
	// A healthy server must not acquire an error, or every snapshot reads as broken.
	if got := byName["healthy"]; got.RuntimeStatus != "connected" || got.Error != "" {
		t.Errorf("healthy = %+v, want runtimeStatus connected and no error", got)
	}
	// An engine predating these fields must still decode, not fail the whole list.
	if got := byName["older"]; got.Status != "unknown" || got.RuntimeStatus != "" || got.Error != "" {
		t.Errorf("older = %+v, want authStatus only", got)
	}
}
