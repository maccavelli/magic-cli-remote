//go:build live_kilo

package kilo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

func isolateKiloHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

func TestLiveKiloTogetherAIKeyRoundTrip(t *testing.T) {
	if os.Getenv("MCREMOTE_LIVE_AUTH_WRITE") != "1" {
		t.Skip("set MCREMOTE_LIVE_AUTH_WRITE=1")
	}
	isolateKiloHome(t)
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-write-together", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	w := any(p).(provider.AuthWriter)
	const scratch = "togetherai"
	const key = "mcremote-live-probe-not-a-real-key"
	err = w.SetCredential(ctx, scratch, scratch+":api", key, nil)
	if err != nil && !errors.Is(err, provider.ErrCredentialNotAccepted) {
		t.Fatalf("SetCredential: %v", err)
	}
	if err == nil {
		st, err := p.AuthStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, up := range st.Upstreams {
			if up.ID == scratch && up.Status == provider.AuthConfigured {
				found = true
			}
		}
		if !found {
			t.Fatal("togetherai not configured after a successful write")
		}
		path, _ := credstore.KiloAuthPath()
		entries, _ := credstore.ReadJSONAuth(path)
		seen := false
		for _, e := range entries {
			if e.ID == scratch {
				seen = true
			}
		}
		if !seen {
			t.Fatal("isolated auth.json missing togetherai")
		}
		if err := w.ClearCredential(ctx, scratch); err != nil {
			t.Fatalf("ClearCredential: %v", err)
		}
	}
}

func TestLiveKiloCatalogMarksXaiMethods(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-xai-catalog", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	st, err := p.AuthStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var xai *provider.UpstreamAuth
	for i := range st.Upstreams {
		if st.Upstreams[i].ID == "xai" {
			xai = &st.Upstreams[i]
			break
		}
	}
	if xai == nil {
		t.Fatal("xai missing from status")
	}
	var sawBrowser, sawDevice, sawAPI bool
	for _, m := range xai.Methods {
		switch {
		case m.Type == provider.AuthMethodOAuthBrowser:
			sawBrowser = true
		case m.Type == provider.AuthMethodOAuthDevice && !m.Unavailable:
			sawDevice = true
		case m.Type == provider.AuthMethodAPIKey:
			sawAPI = true
		}
	}
	if !sawBrowser {
		t.Error("expected SuperGrok subscription as oauth_browser (P0)")
	}
	if !sawDevice {
		t.Error("expected Headless/VPS as a usable device method (P0)")
	}
	if !sawAPI {
		t.Error("expected xAI API key method")
	}
}

func TestLiveKiloGatewayDeviceDoesNotFalseComplete(t *testing.T) {
	isolateKiloHome(t)
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-device-false", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	starter, ok := any(p).(provider.DeviceAuth)
	if !ok {
		t.Fatal("no device auth")
	}
	old := httpagent.DevicePollInterval
	httpagent.DevicePollInterval = 30 * time.Millisecond
	t.Cleanup(func() { httpagent.DevicePollInterval = old })

	_, wait, err := starter.StartDeviceAuth(ctx, "kilo", "kilo:0", nil, false)
	if err != nil {
		// Isolated cold host may refuse if authorize needs a session.
		t.Skipf("could not start gateway device: %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer waitCancel()
	err = wait(waitCtx)
	if err == nil {
		t.Fatal("gateway device completed without a token change")
	}
}
