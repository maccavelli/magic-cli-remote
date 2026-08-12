//go:build live_grok

package grok_test

import (
	"testing"
	"time"
)

// T-C2: grok 1.0.3 (1a29d5bc12d4) emits _x.ai/models/update (slash) as a
// notification. The in-process handler is a separate claim (MADR 0081 P1.2).
func TestLiveGrokModelsUpdateMethodName(t *testing.T) {
	p := startACP(t, nil)
	// session/new already ran; give a moment for late notifications.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.stdoutContains(`"method":"_x.ai/models/update"`) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("stdout never contained method _x.ai/models/update")
}
