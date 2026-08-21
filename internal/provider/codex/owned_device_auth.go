package codex

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// StartOwnedDeviceAuth implements [provider.OwnedDeviceAuth] for Codex
// (MADR 0074 D20/D22/D27).
//
// This replaces the D8 mechanism, which read the live auth.json into a byte
// slice and hoped a callback would put it back. Two things made that
// unrecoverable: the callback could be dropped by a caller that returned early,
// and — decisively — `codex login --device-auth` revokes the stored refresh
// token server-side before deleting it (F14), so even a perfect byte restore
// handed back a dead credential.
//
// The isolated flow removes both failure modes at once. The child runs against
// a private CODEX_HOME that starts empty, so its pre-login logout finds nothing
// to delete and nothing to revoke; the live credential is not read, not
// copied, and not touched until a validated candidate is published atomically.
// That is also why there is no destructive confirmation here: starting this
// flow cannot sign the host out, so there is nothing for the user to confirm.
func (p *Provider) StartOwnedDeviceAuth(
	ctx context.Context,
	upstreamID, methodID string,
	_ map[string]string,
) (provider.DeviceAuthHandle, error) {
	if p.coord == nil {
		return nil, fmt.Errorf("codex: provider was not built with a credential coordinator")
	}
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return nil, fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	if methodID != "" && methodID != openaiUpstreamID+":device" {
		return nil, fmt.Errorf("codex has no device method %q", methodID)
	}

	// Refuse before spawning anything if the credential lives somewhere this
	// transaction cannot observe, rather than claiming a protection that would
	// silently not apply (D22).
	if err := NewCredentialAdapter(p.coord.ProviderID(), p.cfg.Bin).CheckBackend(); err != nil {
		return nil, err
	}

	flow, err := providerauth.StartOwnedFlow(ctx, providerauth.OwnedFlowConfig{
		Coordinator: p.coord,
		Bin:         p.cfg.Bin,
		Args:        []string{"login", "--device-auth"},
		ScanTimeout: deviceCodeScanTimeout,
		EnvFor: func(home string) []string {
			return NewCredentialAdapter(p.coord.ProviderID()).PendingEnv(home)
		},
		Busy:   p.busy,
		Source: providerauth.SourceDeviceAuth,
		Log:    p.log,
	})
	if err != nil {
		return nil, err
	}
	return provider.NewOwnedFlowHandle(flow), nil
}
