package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// captureHost records emitted events for dialect unit tests.
type captureHost struct {
	mu     sync.Mutex
	events []event.Event
	model  string
	api    httpagent.API
}

func (h *captureHost) ID() string               { return "local" }
func (h *captureHost) AgentSessionID() string   { return "ses_test" }
func (h *captureHost) CWD() string              { return "/tmp" }
func (h *captureHost) Model() string            { return h.model }
func (h *captureHost) Config() httpagent.Config { return httpagent.Config{} }
func (h *captureHost) Log() *slog.Logger        { return slog.Default() }
func (h *captureHost) API() httpagent.API {
	if h.api != nil {
		return h.api
	}
	return func(context.Context, string, string, any, any) error { return nil }
}
func (h *captureHost) Emit(ev event.Event) {
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
}
func (h *captureHost) EmitReplay(ev event.Event) { h.Emit(ev) }
func (h *captureHost) EndTurn() bool             { return true }
func (h *captureHost) TrackPermission(string)    {}
func (h *captureHost) TakePending(string) bool   { return true }

func (h *captureHost) texts(t event.Type) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b string
	for _, e := range h.events {
		if e.Type == t {
			b += e.Text
		}
	}
	return b
}

func TestPartDeltaStreamsAssistantText(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)

	// Role registration (assistant).
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_1","role":"assistant"}}`))
	// Announce reasoning part type, then stream deltas.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_r","messageID":"msg_1","type":"reasoning","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_r","field":"text","delta":"think"
	}`))
	// Text part deltas (the primary streaming path on OpenCode 1.18).
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":""}
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"Hel"
	}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_1","partID":"prt_t","field":"text","delta":"lo"
	}`))
	// Final snapshot must not double-emit already-streamed text.
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_1","type":"text","text":"Hello"}
	}`))

	if got := h.texts(event.TypeAssistantChunk); got != "Hello" {
		t.Fatalf("assistant text=%q want Hello", got)
	}
	if got := h.texts(event.TypeThoughtChunk); got != "think" {
		t.Fatalf("thought text=%q want think", got)
	}
}

func TestUserDeltasAreDropped(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_u","role":"user"}}`))
	s.HandleEvent("message.part.delta", json.RawMessage(`{
		"messageID":"msg_u","partID":"prt_u","field":"text","delta":"hi"
	}`))
	if got := h.texts(event.TypeAssistantChunk); got != "" {
		t.Fatalf("user delta leaked: %q", got)
	}
}

func TestSeededFallbackModel(t *testing.T) {
	// NewHTTP must seed a usable free fast model without waiting on AfterBoot.
	_ = NewHTTP(Config{})
	h := &captureHost{model: ""}
	d := &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
	}
	s := d.NewSession(h).(*httpSession)
	mp, mid := s.resolveModel()
	if mp != "opencode" || mid != zenDefaultModel {
		t.Fatalf("resolveModel=%s/%s want opencode/%s", mp, mid, zenDefaultModel)
	}
	if zenDefaultModel != "deepseek-v4-flash-free" {
		t.Fatalf("zenDefaultModel=%q; expected deepseek-v4-flash-free for latency", zenDefaultModel)
	}
	_ = provider.Content{}
}

func TestPickFastCatalogDefault(t *testing.T) {
	// Prefer flash-free over engine default big-pickle when both exist.
	available := map[string]struct{}{
		"big-pickle":             {},
		"deepseek-v4-flash-free": {},
		"north-mini-code-free":   {},
	}
	chosen := ""
	for _, id := range zenFallbackModels {
		if _, ok := available[id]; ok {
			chosen = id
			break
		}
	}
	if chosen != "deepseek-v4-flash-free" {
		t.Fatalf("chosen=%q", chosen)
	}
}
