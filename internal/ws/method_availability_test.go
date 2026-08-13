package ws_test

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// MADR 0083 D4: the wire says per method whether THIS daemon can drive it.
// Absent means available, so the assertions distinguish nil from &false.
func TestUpstreamAuthPayloadAnnotatesAvailability(t *testing.T) {
	up := provider.UpstreamAuth{
		ID: "vendor",
		Methods: []provider.AuthMethod{
			{ID: "vendor:0", Type: provider.AuthMethodAPIKey, Label: "key"},
			{ID: "vendor:1", Type: provider.AuthMethodOAuthBrowser, Label: "browser"},
			{ID: "vendor:2", Type: provider.AuthMethodOAuthDevice, Label: "device"},
			{ID: "vendor:3", Type: provider.AuthMethodAPIKey, Label: "keyring",
				Unavailable: true, Reason: "keyring_managed"},
		},
	}

	got := ws.UpstreamAuthPayload(up, false) // device NOT wired
	want := []struct {
		avail  *bool
		reason string
	}{
		{nil, ""},
		{boolPtr(false), protocol.AuthReasonBrowserOnly},
		{boolPtr(false), protocol.AuthReasonDeviceUnsupported},
		{boolPtr(false), protocol.AuthReasonKeyringManaged},
	}
	for i, w := range want {
		m := got.Methods[i]
		if (m.Available == nil) != (w.avail == nil) ||
			(m.Available != nil && *m.Available != *w.avail) || m.Reason != w.reason {
			t.Errorf("method %d: available=%v reason=%q, want %v %q",
				i, m.Available, m.Reason, w.avail, w.reason)
		}
	}

	// With the device flow wired, the device method loses its annotation.
	got = ws.UpstreamAuthPayload(up, true)
	if m := got.Methods[2]; m.Available != nil || m.Reason != "" {
		t.Errorf("wired device method still annotated: %v %q", m.Available, m.Reason)
	}
}

func boolPtr(b bool) *bool { return &b }
