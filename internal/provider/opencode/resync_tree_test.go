package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestResyncKeepsTurnWhenChildBusy(t *testing.T) {
	completed := nowMS(0)
	msgJSON := `[
		{"info":{"id":"msg_a","role":"assistant","time":{"created":` + jsonInt(completed-500) + `,"completed":` + jsonInt(completed) + `}},
		 "parts":[{"id":"prt_t","type":"text","text":"done"}]}
	]`
	h := newTreeHost()
	h.turnOn = true
	h.api = func(ctx context.Context, method, path string, body, out any) error {
		if strings.Contains(path, "/session/status") {
			return json.Unmarshal([]byte(`{"parent1":{"type":"idle"},"child1":{"type":"busy"}}`), out)
		}
		if strings.Contains(path, "/children") {
			return json.Unmarshal([]byte(`[{"id":"child1"}]`), out)
		}
		if strings.Contains(path, "/permission") {
			return json.Unmarshal([]byte(`[]`), out)
		}
		if strings.Contains(path, "/message") {
			return json.Unmarshal([]byte(msgJSON), out)
		}
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.Resync(context.Background(), time.UnixMilli(completed).Add(-time.Minute))

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endTurns != 0 || !h.turnOn {
		t.Fatalf("must not EndTurn while child busy: endTurns=%d turnOn=%v", h.endTurns, h.turnOn)
	}
	if h.nodes["child1"] != httpagent.NodeBusy {
		t.Fatalf("child status=%q", h.nodes["child1"])
	}
}

func TestResyncReemitsTreePermission(t *testing.T) {
	h := newTreeHost()
	h.turnOn = true
	h.api = func(_ context.Context, method, path string, _, out any) error {
		switch {
		case strings.Contains(path, "/children"):
			return json.Unmarshal([]byte(`[]`), out)
		case strings.Contains(path, "/session/status"):
			return json.Unmarshal([]byte(`{"parent1":{"type":"busy"}}`), out)
		case strings.Contains(path, "/permission"):
			return json.Unmarshal([]byte(`[
				{"id":"p1","sessionID":"parent1","permission":"edit","patterns":["a.go"],"always":[]},
				{"id":"p2","sessionID":"other","permission":"bash","patterns":["x"],"always":[]}
			]`), out)
		case strings.Contains(path, "/message"):
			return json.Unmarshal([]byte(`[]`), out)
		default:
			return nil
		}
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))

	var sawP1, sawP2 bool
	for _, ev := range h.events {
		if ev.Type == event.TypePermission {
			if ev.PermissionID == "p1" {
				sawP1 = true
			}
			if ev.PermissionID == "p2" {
				sawP2 = true
			}
		}
	}
	if !sawP1 {
		t.Fatal("expected permission p1 for this tree")
	}
	if sawP2 {
		t.Fatal("must not re-emit foreign session permission p2")
	}
}
