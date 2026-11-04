package acpagent

import "testing"

// TestShutdownRunsTheSpecHookOnce: Shutdown calls Spec.OnShutdown once, after
// the provider is marked closed, so a hook that releases a shared resource
// cannot race a new spare (0214 PLAN P7 Deviation 1).
func TestShutdownRunsTheSpecHookOnce(t *testing.T) {
	calls := 0
	var p *Provider
	p = New(Spec{OnShutdown: func() {
		calls++
		p.warmMu.Lock()
		closed := p.closed
		p.warmMu.Unlock()
		if !closed {
			t.Error("OnShutdown ran before the provider was marked closed")
		}
	}}, Config{})
	p.Shutdown()
	if calls != 1 {
		t.Fatalf("OnShutdown ran %d times, want 1", calls)
	}
}

// A Spec without the hook shuts down as before.
func TestShutdownWithoutTheSpecHook(t *testing.T) {
	New(Spec{}, Config{}).Shutdown()
}
