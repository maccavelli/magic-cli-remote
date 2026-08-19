//go:build live_grok

package grok_test

import (
	"strings"
	"testing"
	"time"
)

// T-T1: session/set_model _meta.reasoningEffort is applied; a top-level
// reasoningEffort field is the 0052 silent-accept trap (MADR 0106 Phase A).
// Grok 1.0.5 (5115b46bc909).
func TestLiveGrokSetModelMetaReasoningEffort(t *testing.T) {
	p := startACP(t, nil)
	sid := p.sessionID()
	if sid == "" {
		t.Fatal("no sessionId")
	}
	if got := appliedEffort(rpcResult(t, p, 2)); got != "high" {
		t.Fatalf("baseline effort %q, want high", got)
	}

	p.send(t, 10, "session/set_model", map[string]any{
		"sessionId": sid,
		"modelId":   "grok-4.6",
		"_meta":     map[string]any{"reasoningEffort": "xhigh"},
	})
	if err := p.waitID(t, 10, 15*time.Second); err != nil {
		t.Fatalf("set_model _meta xhigh: %v", err)
	}
	resumeID := 11
	before := p.lineCount()
	p.send(t, resumeID, "session/resume", map[string]any{"sessionId": sid, "cwd": p.cwd})
	if err := p.waitID(t, resumeID, 20*time.Second); err != nil {
		t.Fatalf("session/resume after xhigh: %v", err)
	}
	if replay := p.chunksAfter(before); replay {
		t.Fatal("session/resume streamed agent_message_chunk before its response; unsafe read-back")
	}
	if got := appliedEffort(rpcResult(t, p, resumeID)); got != "xhigh" {
		t.Fatalf("set_model _meta xhigh: applied %q, want xhigh", got)
	}

	p.send(t, 12, "session/set_model", map[string]any{
		"sessionId": sid,
		"modelId":   "grok-4.6",
		"_meta":     map[string]any{"reasoningEffort": "low"},
	})
	if err := p.waitID(t, 12, 15*time.Second); err != nil {
		t.Fatalf("set_model _meta low: %v", err)
	}
	p.send(t, 13, "session/resume", map[string]any{"sessionId": sid, "cwd": p.cwd})
	if err := p.waitID(t, 13, 20*time.Second); err != nil {
		t.Fatalf("session/resume after low: %v", err)
	}
	if got := appliedEffort(rpcResult(t, p, 13)); got != "low" {
		t.Fatalf("set_model _meta low: applied %q, want low", got)
	}

	p.send(t, 14, "session/set_model", map[string]any{
		"sessionId":       sid,
		"modelId":         "grok-4.6",
		"reasoningEffort": "xhigh",
	})
	msg, err := p.waitRaw(t, 14, 15*time.Second)
	if err != nil {
		t.Fatalf("set_model top-level xhigh: %v", err)
	}
	t.Logf("top-level reasoningEffort envelope: %v", msg["result"])
	p.send(t, 15, "session/resume", map[string]any{"sessionId": sid, "cwd": p.cwd})
	if err := p.waitID(t, 15, 20*time.Second); err != nil {
		t.Fatalf("session/resume after top-level xhigh: %v", err)
	}
	if got := appliedEffort(rpcResult(t, p, 15)); got == "xhigh" {
		t.Fatal("top-level reasoningEffort applied xhigh; want the 0052 silent-accept trap (effort unchanged)")
	}
}

func (p *acpProc) lineCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.lines)
}

func (p *acpProc) chunksAfter(from int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if from < 0 {
		from = 0
	}
	for _, line := range p.lines[from:] {
		if strings.Contains(line, `"sessionUpdate":"agent_message_chunk"`) ||
			strings.Contains(line, `"sessionUpdate": "agent_message_chunk"`) {
			return true
		}
	}
	return false
}
