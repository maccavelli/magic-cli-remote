//go:build live_grok

package grok_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// T-H1: session/new _meta yoloMode/autoMode discrimination (MADR 0081 Phase H).
// Production NewSession must not send these fields until a follow-up MADR.
//
// Measured 2026-08-12 grok 1.0.3: baseline requested permission and did not
// write; yoloMode and autoMode requested no permission, did not finish the
// prompt in 90s, and did not write. Not taken — does not replace AlwaysApprove
// or MADR 0049.
func TestLiveGrokSessionMetaDiscrimination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "probe.txt")
	prompt := "Create a file at " + target + " containing exactly the single character x. Do not do anything else."

	type row struct {
		name string
		meta map[string]any
	}
	rows := []row{
		{name: "baseline", meta: nil},
		{name: "yolo", meta: map[string]any{"yoloMode": true}},
		{name: "auto", meta: map[string]any{"autoMode": true}},
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_ = os.Remove(target)
			p := startACP(t, r.meta)
			sid := p.sessionID()
			if sid == "" {
				t.Fatal("session/new returned no sessionId")
			}
			p.send(t, 10, "session/prompt", map[string]any{
				"sessionId": sid,
				"prompt":    []any{map[string]any{"type": "text", "text": prompt}},
			})
			deadline := time.Now().Add(90 * time.Second)
			var sawPerm, sawPromptDone bool
			for time.Now().Before(deadline) {
				if p.stdoutContains(`"method":"session/request_permission"`) {
					sawPerm = true
				}
				p.mu.Lock()
				_, done := p.byID[10]
				p.mu.Unlock()
				if done {
					sawPromptDone = true
					break
				}
				if r.name == "baseline" && sawPerm {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			_, err := os.Stat(target)
			wrote := err == nil
			t.Logf("%s: permission_request=%v prompt_done=%v wrote=%v", r.name, sawPerm, sawPromptDone, wrote)
		})
	}
}
