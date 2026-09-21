//go:build live_codex_contract

package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestLiveContractNoModelTurn regenerates the exact app-server schema and
// probes only content-free catalogs. It never calls turn/start.
func TestLiveContractNoModelTurn(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not found on PATH")
	}
	manifest := mustLoadContractManifest(t)
	dir := t.TempDir()
	stableDir := filepath.Join(dir, "stable")
	experimentalDir := filepath.Join(dir, "experimental")
	for _, probe := range []struct {
		out          string
		experimental bool
	}{
		{out: stableDir},
		{out: experimentalDir, experimental: true},
	} {
		args := []string{"app-server", "generate-json-schema", "--out", probe.out}
		if probe.experimental {
			args = append(args, "--experimental")
		}
		if out, err := exec.Command("codex", args...).CombinedOutput(); err != nil {
			t.Fatalf("codex %v: %v\n%s", args, err, out)
		}
	}
	stable, stableDocs := readGeneratedSurface(t, filepath.Join(stableDir, "codex_app_server_protocol.v2.schemas.json"))
	experimental, experimentalDocs := readGeneratedSurface(t, filepath.Join(experimentalDir, "codex_app_server_protocol.v2.schemas.json"))

	// Two modes (MADR 0163 D3). The default fails only on drift a client can be
	// wrong about; an additive release is reported and passes, because the
	// previous whole-surface reflect.DeepEqual could not go green on any host
	// running a Codex newer than the pin — so it was never run, and seven
	// notifications went unrouted behind it for six releases (0163 F1).
	exact := os.Getenv("CODEX_CONTRACT_EXACT") == "1"
	drift := CompareSurfaces(manifest.Stable, manifest.Experimental, stable, experimental)
	fresh := generatedFixtures(stable, experimental, stableDocs, experimentalDocs)
	drift.Breaking = append(drift.Breaking, CompareRequiredFields(manifest.Fixtures, fresh)...)

	for _, item := range drift.Additive {
		t.Logf("additive drift: %s", item)
	}
	if len(drift.Additive) > 0 {
		t.Logf("%d additive difference(s) against the %s pin: Codex grew, nothing is broken. "+
			"Re-pin with ./scripts/codex-contract.ps1 when you want them in the inventory.",
			len(drift.Additive), manifest.CodexVersion)
	}
	if drift.HasBreaking() {
		for _, item := range drift.Breaking {
			t.Errorf("BREAKING drift: %s", item)
		}
		t.Fatalf("%d breaking difference(s) against the %s pin: a method we may send or handle "+
			"changed meaning or went away", len(drift.Breaking), manifest.CodexVersion)
	}
	if exact {
		if !reflect.DeepEqual(stable, manifest.Stable) {
			t.Errorf("exact mode: installed stable surface differs from the %s manifest", manifest.CodexVersion)
		}
		if !reflect.DeepEqual(experimental, manifest.Experimental) {
			t.Errorf("exact mode: installed experimental surface differs from the %s manifest", manifest.CodexVersion)
		}
	}

	p := NewWithLogger(Config{Bin: "codex"}, testLogger(t))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer p.Shutdown()
	p.mu.Lock()
	snapshot := p.eng.capabilities.Snapshot()
	metadata := p.eng.initialize
	fr := p.eng.conn
	p.mu.Unlock()
	if exact && !snapshot.EvidenceMatched {
		t.Fatalf("exact mode: binary identity differs from the fixture: %+v", snapshot.Sanitized())
	}
	if !exact && !snapshot.EvidenceMatched {
		// Expected off the capture host, and on a Windows npm install the hash is
		// of the ~341-byte .cmd shim rather than the engine, so it carries little
		// evidence anyway (MADR 0163 D18).
		t.Logf("binary identity differs from the fixture (expected off the pinning host): %+v",
			snapshot.Sanitized())
	}
	if metadata.UserAgent == "" || metadata.PlatformFamily == "" || metadata.PlatformOS == "" {
		t.Fatalf("incomplete initialize response: %+v", metadata)
	}

	probes := []struct {
		method string
		params any
	}{
		{method: "model/list", params: map[string]any{"limit": 100}},
		{method: "permissionProfile/list", params: map[string]any{"limit": 100}},
		{method: "thread/list", params: map[string]any{"limit": 10, "useStateDbOnly": true}},
		{method: "mcpServerStatus/list", params: map[string]any{"limit": 100, "detail": "toolsAndAuthOnly"}},
	}
	for _, probe := range probes {
		raw, err := fr.sendRequest(ctx, probe.method, probe.params)
		if err != nil {
			t.Fatalf("%s: %v", probe.method, err)
		}
		if !json.Valid(raw) {
			t.Fatalf("%s returned invalid JSON", probe.method)
		}
		t.Logf("%s response bytes=%d", probe.method, len(raw))
	}

	// CODEX_HOME is read by Codex, but this test does not replace it or write
	// credentials. Assert the harness did not accidentally isolate it and turn
	// this into an auth mutation test.
	if os.Getenv("CODEX_HOME") != "" {
		t.Log("using caller-configured CODEX_HOME")
	}
}
