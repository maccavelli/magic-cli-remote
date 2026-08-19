package grok

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// grokDeviceAuthSandbox is a seatbelt profile for the grok login child on
// darwin. webbrowser 1.0.6 calls LSOpenFromURLSpec, not PATH `open`; deny
// default with no mach-lookup makes that fail so the phone is the browser
// (MADR 0107 D6 amendment). Do not add blanket (allow mach-lookup).
const grokDeviceAuthSandbox = `(version 1)
(deny default)
(allow process*)
(allow signal)
(allow file-read*)
(allow file-write*)
(allow file-ioctl)
(allow sysctl-read)
(allow system-socket)
(allow network*)
`

// startDeviceAuth runs `grok login --device-auth` (MADR 0074 Strategy A,
// MADR 0107 D1).
//
// Unlike codex, this flow is non-destructive: grok leaves ~/.grok/auth.json
// alone until the exchange succeeds, so no snapshot or confirmation is needed.
// grok 1.0.5 (5115b46bc909) stdout is pinned in providerauth/cli_test.go.
//
// grok always calls webbrowser::open. On Linux a stub open/xdg-open on the
// child PATH is enough. On macOS the crate uses Launch Services, so the child
// is wrapped in sandbox-exec (MADR 0107 D6). The daemon itself is not sandboxed.
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
	spawnBin, args, err := wrapGrokDeviceAuth(bin)
	if err != nil {
		_ = os.RemoveAll(dir)
		return provider.DeviceFlow{}, nil, err
	}
	cls, flow, err := providerauth.StartCLIDeviceFlow(
		ctx, spawnBin, args, deviceCodeScanTimeout, extra)
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

// wrapGrokDeviceAuth returns the argv used to spawn grok login --device-auth.
// Darwin wraps with sandbox-exec so LSOpenFromURLSpec cannot open a host
// browser; missing sandbox-exec is an error rather than an unsandboxed spawn.
func wrapGrokDeviceAuth(bin string) (spawnBin string, args []string, err error) {
	args = []string{"login", "--device-auth"}
	if runtime.GOOS != "darwin" {
		return bin, args, nil
	}
	sb, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return "", nil, fmt.Errorf("sandbox-exec is required to suppress the host browser on macOS: %w", err)
	}
	return sb, append([]string{"-p", grokDeviceAuthSandbox, bin}, args...), nil
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
