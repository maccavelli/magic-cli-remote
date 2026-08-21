package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// TestGrokConfiguredIsPerMethod proves grok reports its two credentials
// independently — the case that makes a per-upstream flag wrong
// (MADR 0074 P18 step 12).
func TestGrokConfiguredIsPerMethod(t *testing.T) {
	const cfgWithKey = "[auth]\napi_key = \"xai-test\"\n"

	cases := []struct {
		name       string
		cfg        string
		auth       string
		wantAPI    bool
		wantDevice bool
	}{
		{"neither", "", "", false, false},
		{"key only", cfgWithKey, "", true, false},
		{"session only", "", liveGrokCred, false, true},
		{"both at once", cfgWithKey, liveGrokCred, true, true},
		{"unparseable session is not configured", "", `nope`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GROK_HOME", home)
			if tc.cfg != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.cfg), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.auth != "" {
				if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(tc.auth), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			gotAPI, gotDevice := configuredMethods()
			if gotAPI != tc.wantAPI {
				t.Errorf("api configured = %v, want %v", gotAPI, tc.wantAPI)
			}
			if gotDevice != tc.wantDevice {
				t.Errorf("device configured = %v, want %v", gotDevice, tc.wantDevice)
			}
		})
	}
}

// TestGrokEnvKeyIsNotAConfiguredMethod proves XAI_API_KEY does not make the
// method look removable: the daemon cannot edit its own service environment.
func TestGrokEnvKeyIsNotAConfiguredMethod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	t.Setenv("XAI_API_KEY", "xai-from-environment")

	api, device := configuredMethods()
	if api || device {
		t.Fatalf("env-only key reported as a configured method: api=%v device=%v", api, device)
	}
}

// TestGrokClearMethodsAreIndependent proves clearing one credential leaves the
// other alone (MADR 0074 P18 step 10).
func TestGrokClearMethodsAreIndependent(t *testing.T) {
	ctx := context.Background()

	t.Run("clearing the key keeps the session", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GROK_HOME", home)
		cfg := filepath.Join(home, "config.toml")
		auth := filepath.Join(home, "auth.json")
		if err := os.WriteFile(cfg, []byte("[auth]\napi_key = \"xai-test\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(auth, []byte(liveGrokCred), 0o600); err != nil {
			t.Fatal(err)
		}
		coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
		if err != nil {
			t.Fatal(err)
		}
		c := NewCoordinated(New(Config{Bin: "grok"}), "grok", nil, coord, nil)

		if err := c.ClearCredentialMethod(ctx, "", "xai:api"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(auth); err != nil {
			t.Fatal("clearing the API key removed the OAuth session")
		}
		if api, _ := configuredMethods(); api {
			t.Error("the API key is still reported as configured")
		}
	})

	t.Run("clearing the session keeps the key", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("GROK_HOME", home)
		cfg := filepath.Join(home, "config.toml")
		auth := filepath.Join(home, "auth.json")
		if err := os.WriteFile(cfg, []byte("[auth]\napi_key = \"xai-test\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(auth, []byte(liveGrokCred), 0o600); err != nil {
			t.Fatal(err)
		}
		coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := coord.Seed(ctx); err != nil {
			t.Fatal(err)
		}
		c := NewCoordinated(nil, "grok", nil, coord, nil)

		if err := c.ClearCredentialMethod(ctx, "", "xai:device"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(auth); !os.IsNotExist(err) {
			t.Error("clearing the session left auth.json in place")
		}
		if _, err := os.Stat(cfg); err != nil {
			t.Fatal("clearing the OAuth session removed the API key")
		}
		if api, _ := configuredMethods(); !api {
			t.Error("the API key should still be configured")
		}
	})
}

func TestGrokClearMethodRejectsUnknown(t *testing.T) {
	coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := NewCoordinated(nil, "grok", nil, coord, nil)
	ctx := context.Background()
	if err := c.ClearCredentialMethod(ctx, "", "xai:browser"); err == nil {
		t.Error("the host-only browser method was reported as clearable")
	}
	if err := c.ClearCredentialMethod(ctx, "nope", "xai:api"); err == nil {
		t.Error("an unknown upstream was accepted")
	}
	var _ provider.AuthMethodClearer = c
}
