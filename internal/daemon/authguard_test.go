package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// require_client_key binds to the mTLS peer certificate; with TLS off there is
// no handshake and the fingerprint is always empty, so every auth/claim/hello
// would 401 unrecoverably. Run must reject the combination at startup rather
// than serve a daemon nothing can authenticate to.
func TestRunRejectsClientKeyWithoutTLS(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = 0 // ephemeral: only reached if the guard is broken
	cfg.Auth.RequireClientKey = true
	cfg.TLS.Mode = config.TLSModeOff
	cfg.TLS = cfg.TLS.Normalized()
	if cfg.TLS.Active() {
		t.Fatal("precondition: TLS should be off")
	}

	// The guard must fire before Run consumes ctx or binds a listener. Bound the
	// context anyway so a regressed guard makes this fail fast (Run would proceed
	// to Serve and block) instead of hanging the whole test binary.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := Run(ctx, Options{
		Config:  cfg,
		Version: "test",
		Log:     quietLogger(),
	})
	if err == nil {
		t.Fatal("Run should reject require_client_key with TLS off")
	}
	if !strings.Contains(err.Error(), "require_client_key") {
		t.Fatalf("error = %v, want it to name require_client_key (guard did not fire before startup)", err)
	}
}

// The same daemon with TLS on (default selfsigned) must pass the guard — proof
// the check is specific to the unsatisfiable off combination, not a blanket ban.
func TestRunAllowsClientKeyWithTLS(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Listen.Host = "127.0.0.1"
	cfg.Auth.RequireClientKey = true
	cfg.TLS = cfg.TLS.Normalized()
	if !cfg.TLS.Active() {
		t.Fatal("precondition: default TLS should be active")
	}

	// Cancel immediately so Run tears down right after the guard/startup rather
	// than blocking on Serve; the guard error (if any) still surfaces first.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, Options{Config: cfg, Version: "test", Log: quietLogger()})
	if err != nil && strings.Contains(err.Error(), "require_client_key") {
		t.Fatalf("guard wrongly rejected a TLS-on daemon: %v", err)
	}
}
