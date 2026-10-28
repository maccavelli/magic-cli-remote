package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// The JSON Magic's hand-built protocol values marshalled to before they were
// built with the SDK's constructors, captured from that code (MADR 0207
// amendment H, D26; the SDK-gaps PLAN's C1). The constructors must keep every
// byte.
const (
	goldenMcpServers = `[{"headers":[{"name":"X-Key","value":"k"}],"name":"web","type":"http","url":"https://mcp.example/http"},` +
		`{"headers":[{"name":"X-Key","value":"k"}],"name":"events","type":"sse","url":"https://mcp.example/sse"}]`
	goldenCancelled       = `{"outcome":{"outcome":"cancelled"}}`
	goldenSelected        = `{"outcome":{"optionId":"allow","outcome":"selected"}}`
	goldenSetBoolean      = `{"configId":"bool-opt","sessionId":"agent-1","type":"boolean","value":true}`
	goldenSetValueID      = `{"configId":"sel-opt","sessionId":"agent-1","value":"v2"}`
	goldenSetSelectOption = `{"configId":"model-opt","sessionId":"agent-1","value":"m2"}`
)

func marshalString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestMcpServersKeepTheirBytes: buildMcpServers' entries, one per transport.
func TestMcpServersKeepTheirBytes(t *testing.T) {
	headers := map[string]string{"X-Key": "k"}
	p := &Provider{cfg: Config{McpServers: []McpServer{
		{Name: "web", Transport: McpHTTP, URL: "https://mcp.example/http", Headers: headers},
		{Name: "events", Transport: McpSSE, URL: "https://mcp.example/sse", Headers: headers},
	}}}
	caps := acp.AgentCapabilities{McpCapabilities: acp.McpCapabilities{Http: true, Sse: true}}
	if got := marshalString(t, p.buildMcpServers(caps, slog.New(slog.DiscardHandler))); got != goldenMcpServers {
		t.Errorf("MCP servers:\n got %s\nwant %s", got, goldenMcpServers)
	}
}

// TestPermissionOutcomesKeepTheirBytes: both outcomes, from auto-approval and
// from a person's answer.
func TestPermissionOutcomesKeepTheirBytes(t *testing.T) {
	allow := []acp.PermissionOption{{OptionId: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}}
	if got := marshalString(t, autoAllow(acp.RequestPermissionRequest{Options: allow})); got != goldenSelected {
		t.Errorf("auto-approval, selected:\n got %s\nwant %s", got, goldenSelected)
	}
	if got := marshalString(t, autoAllow(acp.RequestPermissionRequest{})); got != goldenCancelled {
		t.Errorf("auto-approval, cancelled:\n got %s\nwant %s", got, goldenCancelled)
	}
	for _, tc := range []struct {
		name      string
		cancelled bool
		want      string
	}{
		{"a person allows", false, goldenSelected},
		{"a person cancels", true, goldenCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{localID: "l", agentID: "a", events: make(chan event.Event, 64), done: make(chan struct{}),
				log: slog.New(slog.DiscardHandler), pending: make(map[string]*permWaiter), attached: true}
			defer close(s.done)
			permID := make(chan string, 1)
			go func() {
				for ev := range s.events {
					if ev.Type == event.TypePermission {
						permID <- ev.PermissionID
						return
					}
				}
			}()
			out := make(chan acp.RequestPermissionResponse, 1)
			go func() {
				resp, _ := s.RequestPermission(context.Background(), acp.RequestPermissionRequest{Options: allow})
				out <- resp
			}()
			if err := s.RespondPermission(context.Background(), <-permID, "allow", tc.cancelled, "dev-1"); err != nil {
				t.Fatal(err)
			}
			if got := marshalString(t, <-out); got != tc.want {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestConfigRequestsKeepTheirBytes: the three session/set_config_option
// requests, as the agent reads them off the wire.
func TestConfigRequestsKeepTheirBytes(t *testing.T) {
	model := acp.SessionConfigOptionCategoryModel
	s, a := newWireSession(t, "{}")
	s.configOptions = []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id: "model-opt", Name: "Model", CurrentValue: "m1", Category: &model,
		Options: acp.SessionConfigSelectOptions{Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{{Value: "m1", Name: "M1"}, {Value: "m2", Name: "M2"}}},
	}}}
	answer := map[string]any{"configOptions": []any{map[string]any{
		"type": "select", "id": "model-opt", "name": "Model", "currentValue": "m2", "category": "model",
		"options": []any{map[string]any{"value": "m1", "name": "M1"}, map[string]any{"value": "m2", "name": "M2"}}}}}
	for _, tc := range []struct {
		name string
		send func() error
		want string
	}{
		{"boolean", func() error { return s.SetConfigOption(context.Background(), "bool-opt", "boolean", "true") }, goldenSetBoolean},
		{"value id", func() error { return s.SetConfigOption(context.Background(), "sel-opt", "select", "v2") }, goldenSetValueID},
		{"select option", func() error {
			_, _, err := s.setSelectOption(context.Background(), model, "m2")
			return err
		}, goldenSetSelectOption},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errc := make(chan error, 1)
			go func() { errc <- tc.send() }()
			m := a.request("session/set_config_option", 5*time.Second)
			if got := string(m.Params); got != tc.want {
				t.Errorf("params:\n got %s\nwant %s", got, tc.want)
			}
			a.respond(m, answer)
			if err := <-errc; err != nil {
				t.Fatal(err)
			}
		})
	}
}
