package acpagent

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// TestEngineVersionPrefersAgentInfoAndFallsBackToVendorMeta pins the shape of
// the two-source read (MADR 0137, ninth amendment).
//
// The plan originally named `agentInfo.version` as the single source for both
// grok and goose. grok sends none — zero occurrences of "agentInfo" across all
// 247 frames of its full-turn fixture — and reports `_meta.agentVersion`
// instead, which the protocol permits because the SDK types agentInfo as
// optional. A reader that knows only the standard field is silent on grok,
// which is the provider whose performance prompted this record.
func TestEngineVersionPrefersAgentInfoAndFallsBackToVendorMeta(t *testing.T) {
	cases := []struct {
		name string
		resp *acp.InitializeResponse
		meta string
		want string
	}{
		{
			name: "goose: standard agentInfo",
			resp: &acp.InitializeResponse{AgentInfo: &acp.Implementation{
				Name: "goose", Version: "1.48.0"}},
			want: "1.48.0",
		},
		{
			name: "grok: vendor _meta.agentVersion, no agentInfo at all",
			resp: &acp.InitializeResponse{},
			meta: "1.0.13",
			want: "1.0.13",
		},
		{
			// The standard field wins when both are present: a future grok that
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
