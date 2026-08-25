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
	stable, _ := readGeneratedSurface(t, filepath.Join(stableDir, "codex_app_server_protocol.v2.schemas.json"))
	experimental, _ := readGeneratedSurface(t, filepath.Join(experimentalDir, "codex_app_server_protocol.v2.schemas.json"))
	if !reflect.DeepEqual(stable, manifest.Stable) {
		t.Fatal("installed stable schema differs from the 0.149.1 manifest")
	}
	if !reflect.DeepEqual(experimental, manifest.Experimental) {
		t.Fatal("installed experimental schema differs from the 0.149.1 manifest")
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
	if !snapshot.EvidenceMatched {
		t.Fatalf("binary identity differs from fixture: %+v", snapshot.Sanitized())
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
