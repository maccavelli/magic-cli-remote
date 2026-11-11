package acpagent_test

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// TestSessionOpenRequestsCarryNoSystemPrompt holds that the daemon never sends
// a client system prompt (F3k of
// 0217-MADR-update-every-pin-to-its-current-release.md). From acp-go-sdk2
// v1.36.0 the SDK refuses a systemPrompt the agent did not advertise, and an
// agent built on it answers one -32602, so a later change that adds one must
// read the agent's sessionCapabilities.systemPrompt first. This test makes
// that change visible: it reads the members of the params on the wire,
// whatever the agent advertises, so it names no identifier of any SDK release.
func TestSessionOpenRequestsCarryNoSystemPrompt(t *testing.T) {
	const advertised = `"sessionCapabilities":{"systemPrompt":{}}`
	cases := []struct {
		name   string
		method string
		caps   string
		start  provider.StartOptions
	}{
		{"new, capability not advertised", "session/new", `{}`, provider.StartOptions{}},
		{"new, capability advertised", "session/new", `{` + advertised + `}`, provider.StartOptions{}},
		{"load, capability not advertised", "session/load", `{"loadSession":true}`,
			provider.StartOptions{AgentSessionID: "agent-9"}},
		{"load, capability advertised", "session/load", `{"loadSession":true,` + advertised + `}`,
			provider.StartOptions{AgentSessionID: "agent-9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := acpagent.New(acpagent.Spec{ID: provider.ID("fixture")}, acpagent.Config{})
			_, a := acptest.Start(t, p, acptest.Options{
				AgentCapabilities: []byte(tc.caps),
				Start:             tc.start,
			})
			params := a.ParamsFor(tc.method)
			if v, ok := params["systemPrompt"]; ok {
				t.Errorf("%s params carry systemPrompt %v; the daemon sends none", tc.method, v)
			}
		})
	}
}
