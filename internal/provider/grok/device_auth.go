package grok

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// deviceCodeScanTimeout bounds the wait for grok to print its code.
const deviceCodeScanTimeout = 30 * time.Second

// grokDeviceExpirySeconds is grok-build MIN_DEVICE_CODE_EXPIRY_FALLBACK_SECS
// (10 minutes). DeviceFlowSheet treats ExpiresIn==0 as already expired
// (MADR 0107 D9).
const grokDeviceExpirySeconds = 600

const hostOpenStub = "#!/bin/sh\nexit 0\n"

// startDeviceAuth runs `grok login --device-auth` (MADR 0074 Strategy A,
// MADR 0107 D1).
//
// Unlike codex, this flow is non-destructive: grok leaves ~/.grok/auth.json
// alone until the exchange succeeds, so no snapshot or confirmation is needed.
// grok 1.0.5 (5115b46bc909) stdout is pinned in providerauth/cli_test.go.
//
// grok's CLI always calls webbrowser::open (device_code.rs), which on macOS
// shells out to `open` and flashes the host browser. Phone-driven auth puts
// a stub `open`/`xdg-open` on the child PATH only (MADR 0107 D6).
func startDeviceAuth(
	ctx context.Context,
	bin, upstreamID string,
	_ bool,
) (provider.DeviceFlow, func(context.Context) error, error) {
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return provider.DeviceFlow{}, nil, fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	dir, extra, err := hostOpenStubDir()
	if err != nil {
		return provider.DeviceFlow{}, nil, err
	}
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		ctx, bin, []string{"login", "--device-auth"}, deviceCodeScanTimeout, extra)
	if err != nil {
		_ = os.RemoveAll(dir)
		return provider.DeviceFlow{}, nil, err
	}
	wait := func(wctx context.Context) error {
		defer func() { _ = os.RemoveAll(dir) }()
		return flow.Wait(wctx)
	}
	return grokDeviceFlowResult(cls), wait, nil
}

func grokDeviceFlowResult(cls providerauth.Classification) provider.DeviceFlow {
	return provider.DeviceFlow{
		VerificationURI: cls.VerificationURI,
		UserCode:        cls.UserCode,
		ExpiresIn:       grokDeviceExpirySeconds,
		Interval:        5,
	}
}

// hostOpenStubDir writes Unix executables named open and xdg-open that exit 0
// without launching a browser. extraEnv is a PATH overlay for the grok child
// only (MADR 0107 D6). Caller must RemoveAll the dir after the child exits.
func hostOpenStubDir() (dir string, extraEnv []string, err error) {
	dir, err = os.MkdirTemp("", "mcremote-grok-open-")
	if err != nil {
		return "", nil, fmt.Errorf("host open stub: %w", err)
	}
	for _, name := range []string{"open", "xdg-open"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(hostOpenStub), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return "", nil, fmt.Errorf("host open stub %s: %w", name, err)
		}
	}
	path := dir + string(os.PathListSeparator) + os.Getenv("PATH")
	return dir, []string{"PATH=" + path}, nil
}
