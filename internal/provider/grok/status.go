package grok

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// AuthStatus implements [provider.Auth] for a coordinated Grok provider: the
// plain provider's credential picture, plus the coordinator's non-secret
// recovery projection (MADR 0074 D42).
//
// Before this, the wrapper's status was the embedded provider's, so grok's
// backup state never reached the phone's status payload although codex's did.
func (c *CoordinatedProvider) AuthStatus(ctx context.Context) (provider.AuthState, error) {
	st, err := c.Provider.AuthStatus(ctx)
	if err != nil {
		return st, err
	}
	st.BackupState, st.RecoveryAvailable = c.backupProjection(ctx)
	return st, nil
}

// backupProjection reports the recovery state by codex's rule (codex's
// backupProjection): no coordinator, or a coordinator that cannot answer,
// reports nothing, so such a provider reads as an uncoordinated one. codex's
// other branch, an unprotectable store reported as unsupported, has no grok
// counterpart: grok's credential is always its auth.json, which the
// coordinator protects.
func (c *CoordinatedProvider) backupProjection(ctx context.Context) (string, bool) {
	if c.coord == nil {
		return "", false
	}
	st, err := c.coord.Status(ctx)
	if err != nil {
		return "", false
	}
	return string(st.BackupState), st.RecoveryAvailable
}
