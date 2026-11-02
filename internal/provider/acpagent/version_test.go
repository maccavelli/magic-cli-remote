package acpagent

import (
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
)

// TestEngineVersionPrefersAgentInfoAndFallsBackToVendorMeta pins the shape of
// the two-source read (MADR 0137, ninth amendment).
//
// The plan originally named `agentInfo.version` as the single source for every
// ACP agent. An agent can send none and report its version in `_meta`
// instead, which the protocol permits because the SDK types agentInfo as
// optional. A reader that knows only the standard field is silent on such an
// agent; its Spec's InitializeMeta reads the `_meta`.
func TestEngineVersionPrefersAgentInfoAndFallsBackToVendorMeta(t *testing.T) {
	cases := []struct {
		name string
		resp *acp.InitializeResponse
		meta string
		want string
	}{
		{
			name: "agent: standard agentInfo",
			resp: &acp.InitializeResponse{AgentInfo: &acp.Implementation{
				Name: "agent", Version: "1.48.0"}},
			want: "1.48.0",
		},
		{
			name: "vendor _meta version, no agentInfo at all",
			resp: &acp.InitializeResponse{},
			meta: "1.0.13",
			want: "1.0.13",
		},
		{
			// The standard field wins when both are present: an agent that
			// starts sending agentInfo must not be read from the legacy slot.
			name: "agentInfo wins over _meta",
			resp: &acp.InitializeResponse{AgentInfo: &acp.Implementation{Version: "2.0.0"}},
			meta: "1.0.13",
			want: "2.0.0",
		},
		{
			name: "an empty agentInfo.version falls through rather than blanking",
			resp: &acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: "x"}},
			meta: "1.0.13",
			want: "1.0.13",
		},
		{
			name: "nothing reported is empty, not an error",
			resp: &acp.InitializeResponse{},
			want: "",
		},
		{
			name: "a nil response cannot panic",
			resp: nil,
			meta: "1.0.13",
			want: "1.0.13",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := engineVersionOf(c.resp, c.meta); got != c.want {
				t.Fatalf("engineVersionOf = %q, want %q", got, c.want)
			}
		})
	}
}
