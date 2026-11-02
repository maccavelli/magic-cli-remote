package acpagent

import (
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
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

// TestStartTimeoutBoundsTheHandshake pins Spec.StartTimeout (0214 PLAN P4
// Deviation 2): it bounds initialize and session/new, each.
func TestStartTimeoutBoundsTheHandshake(t *testing.T) {
	for _, stalled := range []string{"initialize", "session/new"} {
		t.Run(stalled, func(t *testing.T) {
			began := time.Now()
			_, err := startOnScript(t, New(Spec{ID: "vendor", StartTimeout: 200 * time.Millisecond}, Config{}),
				provider.StartOptions{}, func(r scriptRequest, _ func(string)) {
					switch {
					case r.Method == stalled:
					case r.Method == "initialize":
						r.Answer(`{"protocolVersion":1,"agentCapabilities":{}}`)
					}
				})
			if err == nil {
				t.Fatal("Start succeeded against an agent that never answered " + stalled)
			}
			if took := time.Since(began); took > 2*time.Second {
				t.Errorf("Start gave up after %v; the Spec bounds %s at 200ms", took.Round(time.Millisecond), stalled)
			}
		})
	}
}

// TestMinVersionRefusesAnOlderEngine pins the Spec's floor (0214 MADR D3;
// amendment C): an engine below MinVersion, or one that names no version, is
// refused at Start with the version found and the upgrade link. A Spec without
// a floor starts on any engine.
func TestMinVersionRefusesAnOlderEngine(t *testing.T) {
	spec := Spec{ID: "vendor", MinVersion: "2.1.0", UpgradeURL: "https://vendor.invalid/install"}
	for _, c := range []struct {
		name, agentInfo string
		spec            Spec
		refused         []string
	}{
		{"below the floor", `,"agentInfo":{"name":"Vendor","version":"1.18.26"}`, spec,
			[]string{"1.18.26", "2.1.0", "https://vendor.invalid/install"}},
		{"no version", ``, spec, []string{"no version", "2.1.0", "https://vendor.invalid/install"}},
		{"a prerelease of the floor", `,"agentInfo":{"name":"Vendor","version":"2.1.0-rc.1"}`, spec,
			[]string{"2.1.0-rc.1"}},
		{"at the floor", `,"agentInfo":{"name":"Vendor","version":"2.1.0"}`, spec, nil},
		{"above it", `,"agentInfo":{"name":"Vendor","version":"2.2.3"}`, spec, nil},
		{"no floor", ``, Spec{ID: "vendor"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := startOnScript(t, New(c.spec, Config{}), provider.StartOptions{},
				func(r scriptRequest, _ func(string)) {
					switch r.Method {
					case "initialize":
						r.Answer(`{"protocolVersion":1,"agentCapabilities":{}` + c.agentInfo + `}`)
					case "session/new":
						r.Answer(`{"sessionId":"agent-1"}`)
					}
				})
			if c.refused == nil {
				if err != nil {
					t.Fatalf("Start refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Start accepted the engine")
			}
			for _, want := range c.refused {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}
