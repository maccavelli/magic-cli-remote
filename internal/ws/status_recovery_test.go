package ws_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Credentials each adapter's Validate accepts, so Seed records a CURRENT.
const (
	recoveryCodexCred = `{"tokens":{"access_token":"at","refresh_token":"rt"},"last_refresh":"2026-08-20T00:00:00Z"}`
	recoveryGrokCred  = `{"https://auth.x.ai::c1":{"key":"k","auth_mode":"oauth","refresh_token":"rt","expires_at":"2026-08-20T00:00:00Z"}}`
)

// todaysKiloAuthBlock is the auth block providers.list sent for sampleState()
// before MADR 0074 D42, captured at 6bf1e9a4. An uncoordinated provider's
// block must stay byte-identical to it.
const todaysKiloAuthBlock = `{"status":"configured","active_upstream":"kilo","upstreams":[{"id":"github-copilot","label":"GitHub Copilot","status":"missing","methods":[{"id":"github-copilot:0","type":"oauth_device","label":"Login with GitHub Copilot","inputs":[{"key":"deploymentType","type":"select","message":"Select GitHub deployment type","options":[{"value":"github.com","label":"GitHub.com"},{"value":"enterprise","label":"Enterprise"}]},{"key":"enterpriseUrl","type":"text","when":{"key":"deploymentType","op":"eq","value":"enterprise"}}],"available":false,"reason":"device_unsupported"}]}],"upstream_switch_unsupported":true}`

// seededCoordinator writes cred as the provider's LIVE file and seeds a
// coordinator from it, so its status is current with a restorable CURRENT.
func seededCoordinator(t *testing.T, live, cred string, ad providerauth.Adapter) *providerauth.Coordinator {
	t.Helper()
	if err := os.WriteFile(live, []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := providerauth.NewCoordinator(t.TempDir(), ad, providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return coord
}

// authBlocks lists providers over a v2 connection and returns each entry's
// raw auth block, keyed by provider id.
func authBlocks(t *testing.T, provs ...provider.Provider) map[string]json.RawMessage {
	t.Helper()
	w, _ := startAuthServer(t, []int{1, 2}, provs...)
	defer w.close()
	_, raw := listProviders(t, w, protocol.V2)
	var res struct {
		Providers []struct {
			ID   string          `json:"id"`
			Auth json.RawMessage `json:"auth"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	for _, p := range res.Providers {
		out[p.ID] = p.Auth
	}
	return out
}

// members decodes the two recovery members, reporting whether each is present.
func members(t *testing.T, block json.RawMessage) (map[string]json.RawMessage, bool, bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(block, &m); err != nil {
		t.Fatalf("auth block %s: %v", block, err)
	}
	_, bs := m["backup_state"]
	_, ra := m["recovery_available"]
	return m, bs, ra
}

// TestStatusCarriesRecoveryState: the status payload of a coordinated codex and
// a coordinated grok provider carries backup_state and recovery_available; an
// uncoordinated provider's carries neither, and its block is byte-identical to
// the one sent before the members existed (MADR 0074 D42, 16.5 item 11).
func TestStatusCarriesRecoveryState(t *testing.T) {
	codexHome, grokHome := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("XAI_API_KEY", "")
	// A codex binary that does not exist: the reality probe answers unknown,
	// which overrides nothing, and no real codex is run.
	codexBin := filepath.Join(t.TempDir(), "codex-absent")

	t.Run("coordinated codex and grok carry both members", func(t *testing.T) {
		codexCoord := seededCoordinator(t, filepath.Join(codexHome, "auth.json"), recoveryCodexCred,
			codex.NewCredentialAdapter("codex", codexBin))
		grokCoord := seededCoordinator(t, filepath.Join(grokHome, "auth.json"), recoveryGrokCred,
			grok.NewCredentialAdapter("grok"))
		blocks := authBlocks(t,
			codex.NewCoordinated(codex.Config{Bin: codexBin}, nil, codexCoord, nil),
			grok.NewCoordinated(grok.New(grok.Config{}), "grok", nil, grokCoord, nil),
		)
		for _, id := range []string{"codex", "grok"} {
			block := blocks[id]
			if block == nil {
				t.Fatalf("%s has no auth block", id)
			}
			m, bs, ra := members(t, block)
			if !bs || !ra {
				t.Fatalf("%s's auth block lacks the recovery members (backup_state %v, recovery_available %v): %s",
					id, bs, ra, block)
			}
			if string(m["backup_state"]) != `"current"` || string(m["recovery_available"]) != "true" {
				t.Fatalf("%s's recovery members = %s, %s; want \"current\", true (its coordinator's)",
					id, m["backup_state"], m["recovery_available"])
			}
		}
	})

	t.Run("an uncoordinated provider carries neither and is unchanged", func(t *testing.T) {
		blocks := authBlocks(t,
			&authProbeProvider{id: "kilo", state: sampleState()},
			codex.New(codex.Config{Bin: codexBin}),
			grok.New(grok.Config{}),
		)
		for _, id := range []string{"kilo", "codex", "grok"} {
			block := blocks[id]
			if block == nil {
				t.Fatalf("%s has no auth block", id)
			}
			if _, bs, ra := members(t, block); bs || ra {
				t.Fatalf("uncoordinated %s's auth block carries a recovery member: %s", id, block)
			}
		}
		if got := string(blocks["kilo"]); got != todaysKiloAuthBlock {
			t.Fatalf("an uncoordinated provider's auth block changed:\n got %s\nwant %s", got, todaysKiloAuthBlock)
		}
	})
}
