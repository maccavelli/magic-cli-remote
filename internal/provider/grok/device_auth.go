package grok

import (
	"context"
	"fmt"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// deviceCodeScanTimeout bounds the wait for grok to print its code.
const deviceCodeScanTimeout = 30 * time.Second

// startDeviceAuth runs `grok login --device-auth` (MADR 0074 Strategy A).
//
// Unlike codex, this flow is non-destructive: grok leaves ~/.grok/auth.json
// alone until the exchange succeeds, so no snapshot or confirmation is needed.
// The flag survived the 0.2.118 -> 1.0.0 major bump (re-probed 2026-08-10).
func startDeviceAuth(
	ctx context.Context,
	bin, upstreamID string,
	_ bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return provider.DeviceFlow{}, nil, fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		ctx, bin, []string{"login", "--device-auth"}, deviceCodeScanTimeout)
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}
	return provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		Interval:        5,
	}, flow.Wait, nil
}
