package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func seedFastSession(t *testing.T, experimental bool) *session {
	t.Helper()
	s := seededCollabSession(t)
	s.opts.Model = "gpt-5.6-sol"
	recs, err := decodeModelRecords(testdata147(t, "model-list-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.p = &Provider{log: testLogger(t), models: recs, cfg: Config{Model: "gpt-5.6-sol"}}
	s.p.eng = &engine{generation: 1, experimental: experimental}
	s.engineGeneration = 1
	s.agentID = "thread-1"
	return s
}

func TestHasFastAndPersonalityFromActiveModel(t *testing.T) {
	s := seedFastSession(t, false)
	if !s.HasFast() {
		t.Fatal("sol should have Fast")
	}
	if s.PersonalitySupported() {
		t.Fatal("sol does not support personality")
	}
	s.opts.Model = "gpt-5.5"
	if s.HasFast() {
		t.Fatal("gpt-5.5 has no Fast")
	}
	if !s.PersonalitySupported() {
		t.Fatal("gpt-5.5 supports personality")
	}
}

func TestSetServiceTierNextTurnAndOpaqueID(t *testing.T) {
	s := seedFastSession(t, false)
	err := s.SetServiceTier(context.Background(), true)
	if !errors.Is(err, provider.ErrAppliesNextTurn) {
		t.Fatalf("err = %v", err)
	}
	if s.ServiceTier() != "priority" {
		t.Fatalf("stored id = %q, must be opaque Fast id", s.ServiceTier())
	}
	if err := s.SetServiceTier(context.Background(), true); err != nil {
		t.Fatalf("repeat on must be idempotent: %v", err)
	}
	if err := s.SetServiceTier(context.Background(), false); !errors.Is(err, provider.ErrAppliesNextTurn) {
		t.Fatalf("off err = %v", err)
	}
	if s.ServiceTier() != "" {
		t.Fatalf("off left %q", s.ServiceTier())
	}
}

func TestSetServiceTierImmediateExperimental(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	s := seedFastSession(t, true)
	s.p.eng.conn = c
	got := make(chan map[string]any, 1)
	go func() {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		got <- map[string]any{"method": req.Method, "params": req.Params}
		b, _ := json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{}})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	if err := s.SetServiceTier(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-got:
		if req["method"] != "thread/settings/update" {
			t.Fatalf("method = %v", req["method"])
		}
		params := req["params"].(map[string]any)
		if params["serviceTier"] != "priority" {
			t.Fatalf("serviceTier = %#v", params["serviceTier"])
		}
		if _, ok := params["collaborationMode"]; ok {
			t.Fatal("must not rewrite collaboration")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSetPersonalityEnumAndUnsupported(t *testing.T) {
	s := seedFastSession(t, false)
	if err := s.SetPersonality(context.Background(), "friendly"); !errors.Is(err, provider.ErrPersonalityUnsupported) {
		t.Fatalf("sol err = %v", err)
	}
	s.opts.Model = "gpt-5.5"
	if err := s.SetPersonality(context.Background(), "default"); !errors.Is(err, provider.ErrPersonalityInvalid) {
		t.Fatalf("default alias err = %v", err)
	}
	if err := s.SetPersonality(context.Background(), "none"); !errors.Is(err, provider.ErrAppliesNextTurn) {
		t.Fatalf("none err = %v", err)
	}
	if s.Personality() != "none" {
		t.Fatalf("personality = %q", s.Personality())
	}
}

func TestTurnStartCarriesConfirmedSettings(t *testing.T) {
	params := map[string]any{"model": "gpt-5.6-sol"}
	applyCollaborationTurnParams(params, true, collaborationModeMask{Mode: "default"}, "default", "gpt-5.6-sol", "high")
	applyServiceTurnParams(params, "priority", "friendly")
	if params["serviceTier"] != "priority" {
		t.Fatalf("tier = %#v", params["serviceTier"])
	}
	if params["personality"] != "friendly" {
		t.Fatalf("personality = %#v", params["personality"])
	}
	if params["effort"] != "high" {
		t.Fatal("must not drop thinking")
	}
	if _, ok := params["collaborationMode"]; !ok {
		t.Fatal("must keep collaboration")
	}
}

func TestTurnStartServiceTierNullWhenOff(t *testing.T) {
	params := map[string]any{}
	applyServiceTurnParams(params, "", "")
	if params["serviceTier"] != nil {
		t.Fatalf("off must send JSON null, got %#v", params["serviceTier"])
	}
	if _, ok := params["personality"]; ok {
		t.Fatal("unset personality must be omitted, not JSON null")
	}
	applyServiceTurnParams(params, "default", "none")
	if params["serviceTier"] != nil {
		t.Fatalf("default normalizes to null, got %#v", params["serviceTier"])
	}
	if params["personality"] != "none" {
		t.Fatalf("none must serialize the enum, got %#v", params["personality"])
	}
}

func TestModelSwitchClearsUnsupportedOverrides(t *testing.T) {
	s := seedFastSession(t, false)
	s.serviceTier = "priority"
	s.personality = "friendly"
	s.opts.Model = "gpt-5.5"
	s.revalidateModelSettingsLocked()
	if s.serviceTier != "" {
		t.Fatalf("Fast must clear on model without Fast: %q", s.serviceTier)
	}
	if s.personality != "friendly" {
		t.Fatalf("personality should remain on supporting model: %q", s.personality)
	}
	s.opts.Model = "gpt-5.6-sol"
	s.revalidateModelSettingsLocked()
	if s.personality != "" {
		t.Fatalf("personality must clear on unsupported model: %q", s.personality)
	}
}

func TestSetServiceTierUnsupportedModel(t *testing.T) {
	s := seedFastSession(t, false)
	s.opts.Model = "gpt-5.5"
	if err := s.SetServiceTier(context.Background(), true); !errors.Is(err, provider.ErrServiceTierUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsUpdatedReconcilesTierAndPersonality(t *testing.T) {
	s := seedFastSession(t, true)
	applySettingsServiceFields(s, []byte(`{"threadSettings":{"serviceTier":"priority","personality":"pragmatic"}}`))
	if s.ServiceTier() != "priority" || s.Personality() != "pragmatic" {
		t.Fatalf("tier=%q personality=%q", s.ServiceTier(), s.Personality())
	}
	applySettingsServiceFields(s, []byte(`{"threadSettings":{"serviceTier":null}}`))
	if s.ServiceTier() != "" {
		t.Fatalf("null tier = %q", s.ServiceTier())
	}
}
