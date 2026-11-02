package acpagent

import (
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// TestApplyInitializeResponseRecordsWhatTheSpecRead is the wiring guard P2's
// fail-first run found missing: before it, discarding an agent's _meta at the
// call site left the whole package green (PLAN 0167, deviation of 2026-09-22).
// What a Spec's InitializeMeta reads from `_meta` reaches the session and the
// provider, with the capabilities and the advertised auth methods; the grok
// package pins what grok's hook reads from grok's real response.
func TestApplyInitializeResponseRecordsWhatTheSpecRead(t *testing.T) {
	var resp acp.InitializeResponse
	if err := json.Unmarshal([]byte(`{"protocolVersion":1,"agentCapabilities":{"loadSession":true},`+
		`"authMethods":[{"id":"vendor.key","name":"key"},{"id":"vendor.token","name":"token"}],`+
		`"_meta":{"vendor":"block"}}`), &resp); err != nil {
		t.Fatalf("decode typed InitializeResponse: %v", err)
	}
	var sawMeta map[string]any
	catalog := picker.Catalog{Options: []picker.Option{{ID: "model-a"}, {ID: "model-b"}}}
	p := &Provider{log: slog.Default(), spec: Spec{InitializeMeta: func(meta map[string]any) InitializeInfo {
		sawMeta = meta
		return InitializeInfo{Version: "9.9.9", CurrentModel: "model-a", Models: &catalog}
	}}}
	s := &session{log: slog.Default()}

	p.applyInitializeResponse(s, resp)

	if sawMeta["vendor"] != "block" {
		t.Errorf("InitializeMeta saw %v, want initialize's _meta", sawMeta)
	}
	s.mu.Lock()
	engineModel := s.engineModelID
	s.mu.Unlock()
	if want := "model-a"; engineModel != want {
		t.Errorf("engineModelID = %q, want %q (the hook's CurrentModel)", engineModel, want)
	}

	p.catalogMu.RLock()
	has, cat := p.catalogHas, p.catalogCache
	p.catalogMu.RUnlock()
	if !has {
		t.Fatal("no model catalog recorded from the hook's Models")
	}
	ids := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		ids = append(ids, o.ID)
	}
	if !slices.Equal(ids, []string{"model-a", "model-b"}) {
		t.Errorf("catalog options = %v, want model-a and model-b", ids)
	}

	if want := "9.9.9"; p.EngineVersion() != want {
		t.Errorf("EngineVersion() = %q, want %q (the hook's Version)", p.EngineVersion(), want)
	}
	if !s.agentCaps.LoadSession {
		t.Error("agentCaps.LoadSession = false, want true")
	}
	if want := []string{"vendor.key", "vendor.token"}; !slices.Equal(s.advertisedAuth, want) {
		t.Errorf("advertisedAuth = %v, want %v", s.advertisedAuth, want)
	}
}
