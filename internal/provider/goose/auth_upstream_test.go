package goose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gooseHome points the credstore helpers at a temp HOME holding this host's
// real provider set (MADR 0074 Appendix A): opencode_go active, four others
// configured. That is exactly the shape the MADR 0073 hang left behind.
func gooseHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := filepath.Join(home, ".config", "goose")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `active_provider: opencode_go
providers:
  opencode_go:
    kind: api
  gemini_oauth:
    kind: oauth
  chatgpt_codex:
    kind: oauth
  xai_oauth:
    kind: oauth
  google:
    kind: api
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "config.yaml")
}

// The MADR 0073 fix, end to end at the provider layer: move off the
// quota-blocked upstream to one already authenticated, with no credential work.
func TestSetActiveUpstreamSwitchesAwayFromQuotaBlocked(t *testing.T) {
	path := gooseHome(t)

	if err := setActiveUpstream(context.Background(), "gemini_oauth"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveUpstream != "gemini_oauth" {
		t.Fatalf("active = %q, want gemini_oauth", st.ActiveUpstream)
	}
	// The other four must survive: a switch is not a reconfiguration.
	if len(st.Upstreams) != 5 {
		t.Fatalf("provider set changed: %d entries", len(st.Upstreams))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode_go", "chatgpt_codex", "xai_oauth", "google"} {
		if !strings.Contains(string(b), id) {
			t.Errorf("%s disappeared from config.yaml:\n%s", id, b)
		}
	}
}

// Switching to something goose has never configured would look like it worked
// and then fail every turn, so it is refused up front.
func TestSetActiveUpstreamRefusesUnknown(t *testing.T) {
	gooseHome(t)
	err := setActiveUpstream(context.Background(), "not-configured")
	if err == nil {
		t.Fatal("accepted an unconfigured upstream")
	}
	if !strings.Contains(err.Error(), "not-configured") {
		t.Errorf("unhelpful error: %v", err)
	}
	st, _ := authStatus(context.Background())
	if st.ActiveUpstream != "opencode_go" {
		t.Fatalf("a refused switch still changed the active provider: %q", st.ActiveUpstream)
	}
}

func TestSetActiveUpstreamHonoursContext(t *testing.T) {
	gooseHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := setActiveUpstream(ctx, "gemini_oauth"); err == nil {
		t.Fatal("ignored a cancelled context")
	}
}

// Status must never read secret values out of the config, only ids.
func TestAuthStatusReportsConfiguredSet(t *testing.T) {
	gooseHome(t)
	st, err := authStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "configured" {
		t.Fatalf("status = %q", st.Status)
	}
	want := map[string]bool{
		"opencode_go": true, "gemini_oauth": true, "chatgpt_codex": true,
		"xai_oauth": true, "google": true,
	}
	for _, up := range st.Upstreams {
		if !want[up.ID] {
			t.Errorf("unexpected upstream %q", up.ID)
		}
		if up.Status != "configured" {
			t.Errorf("%s = %q", up.ID, up.Status)
		}
	}
}
