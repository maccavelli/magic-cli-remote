package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// OpenCode omits idle sessions from GET /session/status entirely — a real
// engine returns `{}` once everything settles (verified against 1.18.4). So a
// child listed by GET /session/{id}/children but missing from the status map is
// authoritatively idle, not unknown.
func TestConfirmTreeIdleMissingStatusIsIdle(t *testing.T) {
	h := newTreeHost()
	h.api = stubAPI(
		route{"/children", `[{"id":"child1"}]`},
		route{"/session/status", `{}`},
	)
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	busy, disc, err := s.ConfirmTreeIdle(context.Background(), "parent1", []string{"parent1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(disc) != 1 || disc[0] != "child1" {
		t.Fatalf("discovered=%v want [child1]", disc)
	}
	if len(busy) != 0 {
		t.Fatalf("stillBusy=%v; a child absent from /session/status is idle", busy)
	}
}

// With no liveness oracle we cannot verify a freshly discovered child, so it is
// reported busy: keeping the turn active preserves the 0014 resync recovery,
// whereas ending it on an unverified tree cannot be undone (MADR 0020 KD2).
func TestConfirmTreeIdleStatusFailureKeepsDiscoveredBusy(t *testing.T) {
	h := newTreeHost()
	h.api = func(_ context.Context, _, path string, _, out any) error {
		if strings.Contains(path, "/children") {
			return json.Unmarshal([]byte(`[{"id":"child1"}]`), out)
		}
		return fmt.Errorf("status endpoint down")
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	busy, disc, err := s.ConfirmTreeIdle(context.Background(), "parent1", []string{"parent1"})
	if err != nil {
		t.Fatalf("status failure must soft-fail, got err=%v", err)
	}
	if len(disc) != 1 || len(busy) != 1 || busy[0] != "child1" {
		t.Fatalf("discovered=%v stillBusy=%v want both [child1]", disc, busy)
	}
}

// Same soft-fail, but with nothing to verify: the parent message-log recovery
// (0014) must still get its chance, so nothing is reported busy.
func TestConfirmTreeIdleStatusFailureNoChildrenIsIdle(t *testing.T) {
	h := newTreeHost()
	h.api = func(_ context.Context, _, path string, _, out any) error {
		if strings.Contains(path, "/children") {
			return json.Unmarshal([]byte(`[]`), out)
		}
		return fmt.Errorf("status endpoint down")
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	busy, disc, err := s.ConfirmTreeIdle(context.Background(), "parent1", []string{"parent1"})
	if err != nil || len(busy) != 0 || len(disc) != 0 {
		t.Fatalf("busy=%v discovered=%v err=%v; want all empty", busy, disc, err)
	}
}

// Resync must give every tree member an explicit status. BindChildAlias seeds
// an unknown child busy, and the engine omits idle sessions from the status
// map — leaving the seed in place would wedge the NEXT tree-idle EndTurn.
func TestResyncMarksUnlistedChildIdle(t *testing.T) {
	completed := nowMS(0)
	msgJSON := `[
		{"info":{"id":"msg_a","role":"assistant","time":{"created":` + jsonInt(completed-500) + `,"completed":` + jsonInt(completed) + `}},
		 "parts":[{"id":"prt_t","type":"text","text":"done"}]}
	]`
	h := newTreeHost()
	h.turnOn = true
	h.api = stubAPI(
		route{"/children", `[{"id":"child1"}]`},
		route{"/session/status", `{}`},
		route{"/permission", `[]`},
		route{"/question", `[]`},
		route{"/message", msgJSON},
	)
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.Resync(context.Background(), time.UnixMilli(completed).Add(-time.Minute))

	h.mu.Lock()
	defer h.mu.Unlock()
	if got := h.nodes["child1"]; got != httpagent.NodeIdle {
		t.Fatalf("child1=%q want idle; a child missing from /session/status is idle", got)
	}
	if h.endTurns != 1 {
		t.Fatalf("endTurns=%d want 1 — finished turn should be recovered", h.endTurns)
	}
}

// Resync cannot verify children when /session/status is down, so it must keep
// the turn active rather than ending it on a tree it could not read.
func TestResyncKeepsTurnWhenStatusUnavailableWithChildren(t *testing.T) {
	h := newTreeHost()
	h.turnOn = true
	h.api = func(_ context.Context, _, path string, _, out any) error {
		switch {
		case strings.Contains(path, "/children"):
			return json.Unmarshal([]byte(`[{"id":"child1"}]`), out)
		case strings.Contains(path, "/session/status"):
			return fmt.Errorf("status endpoint down")
		case strings.Contains(path, "/permission"), strings.Contains(path, "/question"):
			return json.Unmarshal([]byte(`[]`), out)
		case strings.Contains(path, "/message"):
			t.Error("must not reach parent message recovery with an unverified tree")
			return nil
		}
		return nil
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endTurns != 0 || !h.turnOn {
		t.Fatalf("endTurns=%d turnOn=%v; turn must stay active", h.endTurns, h.turnOn)
	}
}

// route is one stubAPI path-fragment → JSON body mapping.
type route struct{ frag, body string }

// stubAPI routes by path substring in the order given (first match wins), so
// more specific fragments go first. Unmatched paths decode nothing.
func stubAPI(routes ...route) httpagent.API {
	return func(_ context.Context, _, path string, _, out any) error {
		for _, r := range routes {
			if !strings.Contains(path, r.frag) {
				continue
			}
			if out == nil {
				return nil
			}
			return json.Unmarshal([]byte(r.body), out)
		}
		return nil
	}
}
