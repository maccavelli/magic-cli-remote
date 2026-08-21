package grok

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// CoordinatedProvider is a Grok provider whose device login runs inside a
// credential transaction (MADR 0074 D21/D22).
//
// Grok's Provider is an alias for the shared ACP agent type, so the coordinator
// is carried by this wrapper rather than by extra fields on that shared struct.
// Production daemon construction still builds the plain Provider; P20 activates
// this one.
type CoordinatedProvider struct {
	*Provider

	bin   string
	coord *providerauth.Coordinator
	busy  func() int
	log   *slog.Logger

	// wrap builds the spawn argv, including the macOS sandbox-exec wrapper
	// that stops LSOpenFromURLSpec opening a host browser. It is a seam so
	// tests can exercise the transaction without a sandbox profile.
	wrap func(bin string) (string, []string, error)
}

// NewCoordinated wraps a Grok provider with a credential coordinator.
func NewCoordinated(p *Provider, bin string, log *slog.Logger, coord *providerauth.Coordinator, busy func() int) *CoordinatedProvider {
	if log == nil {
		log = slog.Default()
	}
	return &CoordinatedProvider{
		Provider: p,
		bin:      bin,
		coord:    coord,
		busy:     busy,
		log:      log.With(slog.String("component", "provider.grok.auth")),
		wrap:     wrapGrokDeviceAuth,
	}
}

// StartOwnedDeviceAuth implements [provider.OwnedDeviceAuth] for Grok.
//
// Grok does not delete or revoke its credential at login start, so this is not
// repairing the same wound as Codex. It closes the shared half of the defect:
// the flow's process, temporary browser stubs, and publication all become owned
// by one transaction, so no ordinary lifecycle path can orphan a running child
// or publish over a credential another writer just rotated (MADR 0074 F9/D27).
func (c *CoordinatedProvider) StartOwnedDeviceAuth(
	ctx context.Context,
	upstreamID, methodID string,
	_ map[string]string,
) (provider.DeviceAuthHandle, error) {
	if c.coord == nil {
		return nil, fmt.Errorf("grok: provider was not built with a credential coordinator")
	}
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return nil, fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	if methodID != "" && methodID != xaiUpstreamID+":device" {
		return nil, fmt.Errorf("grok has no device method %q", methodID)
	}

	spawnBin, args, err := c.wrap(c.bin)
	if err != nil {
		return nil, err
	}

	flow, err := providerauth.StartOwnedFlow(ctx, providerauth.OwnedFlowConfig{
		Coordinator: c.coord,
		Bin:         spawnBin,
		Args:        args,
		ScanTimeout: deviceCodeScanTimeout,
		EnvFor:      ownedGrokEnv,
		Busy:        c.busy,
		Source:      providerauth.SourceDeviceAuth,
		Log:         c.log,
	})
	if err != nil {
		return nil, err
	}
	return provider.NewOwnedFlowHandle(flow), nil
}

// ownedGrokEnv points the child at the isolated home and at browser stubs that
// live inside the same transaction directory.
//
// Rooting the stubs in the transaction is what makes them self-cleaning: the
// coordinator removes the whole transaction directory on commit and on abort,
// so no ordinary or failing path can leave an executable stub behind in the
// system temp directory the way the pre-transaction flow could
// (MADR 0107 D6 preserved, MADR 0074 D27 ownership).
func ownedGrokEnv(home string) []string {
	env := []string{credstore.GrokHomeEnv(home)}
	stubs := filepath.Join(filepath.Dir(home), "openstub")
	if err := os.MkdirAll(stubs, 0o700); err != nil {
		// Without stubs grok may open a host browser, which is undesirable but
		// not a credential hazard; the flow still runs and the phone is still
		// the intended browser.
		return env
	}
	for _, name := range []string{"open", "xdg-open"} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(hostOpenStub), 0o700); err != nil {
			return env
		}
	}
	return append(env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
}
