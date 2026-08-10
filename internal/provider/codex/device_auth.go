package codex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// deviceCodeScanTimeout bounds the wait for codex to print its code. The real
// CLI prints within a second; anything past this is a hang, and hanging while
// the host is signed out (see below) is the worst possible state.
const deviceCodeScanTimeout = 30 * time.Second

// StartDeviceAuth implements [provider.DeviceAuth] for Codex (MADR 0074 D8).
//
// This flow is destructive before it is useful. Observed on codex-cli 0.146.0:
// `codex login --device-auth` DELETES ~/.codex/auth.json the moment it starts,
// before the user has entered anything. Abandon the flow and the host is
// simply signed out. That is why:
//
//   - confirmDestructive must be true, or the call is refused outright;
//   - the existing credential is copied to a 0600 sidecar first;
//   - any failure, cancellation, or timeout restores that sidecar.
//
// Without this, a mis-tap on a phone would silently sign the host out of
// ChatGPT with no way back short of an interactive login at the machine.
func (p *Provider) StartDeviceAuth(
	ctx context.Context,
	upstreamID, _ string,
	_ map[string]string,
	confirmDestructive bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return provider.DeviceFlow{}, nil, fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	authPath, err := credstore.CodexAuthPath()
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}
	hadCredential := credstore.FileExists(authPath)
	if hadCredential && !confirmDestructive {
		return provider.DeviceFlow{}, nil, fmt.Errorf(
			"starting ChatGPT device sign-in signs this host out immediately, before you finish: %w",
			provider.ErrAuthConfirmRequired)
	}

	var backup []byte
	if hadCredential {
		backup, err = os.ReadFile(authPath) //nolint:gosec // fixed store location
		if err != nil {
			return provider.DeviceFlow{}, nil, fmt.Errorf("snapshot codex credential: %w", err)
		}
	}
	restore := func(reason string) {
		if backup == nil {
			return
		}
		if credstore.FileExists(authPath) {
			// The flow succeeded and wrote a new credential; do not stomp it.
			return
		}
		if err := os.WriteFile(authPath, backup, 0o600); err != nil {
			p.log.Error("could not restore the codex credential after a failed device sign-in",
				slog.String("reason", reason), slog.String("err", err.Error()))
			return
		}
		p.log.Info("restored the codex credential after a failed device sign-in",
			slog.String("reason", reason))
	}

	cls, flow, err := providerauth.StartCLIDeviceFlow(
		ctx, p.cfg.Bin, []string{"login", "--device-auth"}, deviceCodeScanTimeout)
	if err != nil {
		restore("start failed")
		return provider.DeviceFlow{}, nil, err
	}

	wait := func(waitCtx context.Context) error {
		werr := flow.Wait(waitCtx)
		if werr != nil {
			restore("sign-in did not complete")
			return werr
		}
		// A clean exit that left no credential is still a failure — and one
		// that leaves the host signed out unless we put the old one back.
		if !credstore.FileExists(authPath) {
			restore("sign-in produced no credential")
			return errors.New("codex device sign-in finished without storing a credential")
		}
		return nil
	}
	return provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		// codex states 15 minutes in its own output.
		ExpiresIn: int((15 * time.Minute).Seconds()),
		Interval:  5,
	}, wait, nil
}
