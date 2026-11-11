package ws

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// ownedOnly is a provider with an owned device flow and no legacy hook, as
// codex's and grok's coordinated providers can be.
type ownedOnly struct{}

func (ownedOnly) StartOwnedDeviceAuth(context.Context, string, string, map[string]string) (provider.DeviceAuthHandle, error) {
	return nil, nil
}

// ownedAndLegacyOff has an owned flow, and a Spec whose legacy hook says no:
// acpagent's SupportsDeviceAuth reads that hook, which is how grok's owned
// flow came to be refused.
type ownedAndLegacyOff struct{ ownedOnly }

func (ownedAndLegacyOff) SupportsDeviceAuth() bool { return false }

// legacyOff has no owned flow, and says it cannot run a device flow.
type legacyOff struct{}

func (legacyOff) SupportsDeviceAuth() bool { return false }
func (legacyOff) StartDeviceAuth(context.Context, string, string, map[string]string, bool) (provider.DeviceFlow, func(context.Context) error, error) {
	return provider.DeviceFlow{}, nil, nil
}

// TestDeviceAuthWiredForOwnedFlows (MADR 0074 D34): a provider that implements
// OwnedDeviceAuth is wired, whatever its legacy hook answers.
func TestDeviceAuthWiredForOwnedFlows(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    any
		want bool
	}{
		{"owned flow, no legacy hook", ownedOnly{}, true},
		{"owned flow, legacy hook says no", ownedAndLegacyOff{}, true},
		{"legacy hook says no, no owned flow", legacyOff{}, false},
		{"neither", struct{}{}, false},
	} {
		if got := deviceAuthWired(tc.p); got != tc.want {
			t.Errorf("%s: deviceAuthWired = %v, want %v", tc.name, got, tc.want)
		}
	}
}
