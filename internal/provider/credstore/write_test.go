package credstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

func readJSONDoc(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("store is not valid JSON after write: %v\n%s", err, b)
	}
	return doc
}

// The single most damaging bug this code could have: writing one credential
// and losing the others. MADR 0074 D1 merges rather than overwrites.
func TestMergeJSONAuthPreservesOtherProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	seed := `{"opencode":{"type":"api","key":"sk-keep-me"},"kilo":{"type":"oauth","refresh":"rt-keep"}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.MergeJSONAuth(path, "opencode-go", "api", "sk-new"); err != nil {
		t.Fatal(err)
	}
	doc := readJSONDoc(t, path)
	if len(doc) != 3 {
		t.Fatalf("expected 3 providers after merge, got %d: %v", len(doc), doc)
	}
	if doc["opencode"]["key"] != "sk-keep-me" {
		t.Errorf("clobbered an unrelated credential: %v", doc["opencode"])
	}
	if doc["kilo"]["refresh"] != "rt-keep" {
		t.Errorf("clobbered an oauth entry: %v", doc["kilo"])
	}
	if doc["opencode-go"]["key"] != "sk-new" || doc["opencode-go"]["type"] != "api" {
		t.Errorf("new entry wrong: %v", doc["opencode-go"])
	}
}

// Replacing an entry keeps fields this code does not own, so a key refresh
// does not silently drop a refresh token sitting beside it.
func TestMergeJSONAuthKeepsUnmanagedFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	seed := `{"kilo":{"type":"oauth","refresh":"rt-1","expires":123}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.MergeJSONAuth(path, "kilo", "api", "sk-new"); err != nil {
		t.Fatal(err)
	}
	e := readJSONDoc(t, path)["kilo"]
	if e["refresh"] != "rt-1" {
		t.Errorf("dropped refresh token: %v", e)
	}
	if e["expires"] == nil {
		t.Errorf("dropped expiry: %v", e)
	}
	if e["key"] != "sk-new" || e["type"] != "api" {
		t.Errorf("managed fields not updated: %v", e)
	}
}

// A credential file must never be group- or world-readable, and the directory
// it lands in must not be either.
func TestMergeJSONAuthFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "auth.json")
	if err := credstore.MergeJSONAuth(path, "kilo", "api", "sk-1"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("credential file mode = %o, want 600", got)
	}
	dst, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dst.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("credential dir mode = %o, want no group/other bits", got)
	}
}

// An unparseable store is far more likely a format change than corruption.
// Overwriting it would destroy every credential in it, so refuse instead.
func TestMergeJSONAuthRefusesUnparseableStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	original := "{ this is not json"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.MergeJSONAuth(path, "kilo", "api", "sk-1"); err == nil {
		t.Fatal("overwrote an unparseable store")
	}
	b, _ := os.ReadFile(path)
	if string(b) != original {
		t.Fatalf("store was modified despite the refusal: %s", b)
	}
}

func TestMergeJSONAuthValidatesSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := credstore.MergeJSONAuth(path, "kilo", "api", "   "); err == nil {
		t.Error("accepted a blank credential")
	}
	big := strings.Repeat("x", credstore.MaxSecretBytes+1)
	if err := credstore.MergeJSONAuth(path, "kilo", "api", big); err == nil {
		t.Error("accepted an oversized credential")
	}
	if credstore.FileExists(path) {
		t.Error("a rejected write still created the store")
	}
}

func TestDeleteJSONAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	seed := `{"a":{"type":"api","key":"k1"},"b":{"type":"api","key":"k2"}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.DeleteJSONAuth(path, "a"); err != nil {
		t.Fatal(err)
	}
	doc := readJSONDoc(t, path)
	if _, present := doc["a"]; present {
		t.Error("entry not removed")
	}
	if doc["b"]["key"] != "k2" {
		t.Error("delete clobbered the other entry")
	}
	// Idempotent: deleting what is not there, or from a store that does not
	// exist, is success — the caller asked for it to be gone.
	if err := credstore.DeleteJSONAuth(path, "a"); err != nil {
		t.Errorf("second delete errored: %v", err)
	}
	if err := credstore.DeleteJSONAuth(filepath.Join(dir, "absent.json"), "a"); err != nil {
		t.Errorf("delete from a missing store errored: %v", err)
	}
}

// MADR 0074 D10 is last-writer-wins, not "one of the writes corrupts the
// file". Concurrent writers must always leave valid JSON with both entries.
func TestMergeJSONAuthConcurrentWritersLeaveValidStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := credstore.MergeJSONAuth(path, "seed", "api", "sk-seed"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = credstore.MergeJSONAuth(path, "racer", "api", "sk-value")
		}(i)
	}
	wg.Wait()
	doc := readJSONDoc(t, path) // fails the test if a partial write landed
	if doc["racer"]["key"] != "sk-value" {
		t.Errorf("racer entry = %v", doc["racer"])
	}
}

// The goose switch must touch exactly one line: users hand-edit this file, and
// a YAML round-trip would reformat it and drop their comments.
func TestSetGooseActiveProviderIsSurgical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `# my notes
active_provider: opencode_go
GOOSE_MODEL: some-model
providers:
  opencode_go:
    kind: api
  gemini_oauth:
    kind: oauth
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGooseActiveProvider(path, "gemini_oauth"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(body, "active_provider: opencode_go", "active_provider: gemini_oauth", 1)
	if string(got) != want {
		t.Fatalf("switch was not surgical:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSetGooseActiveProviderAddsKeyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  a: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGooseActiveProvider(path, "a"); err != nil {
		t.Fatal(err)
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProvider != "a" {
		t.Fatalf("active = %q", cfg.ActiveProvider)
	}
	// Prepended, never inserted into another block's indented body.
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "active_provider: a\n") {
		t.Fatalf("key not prepended: %s", b)
	}
}

func TestSetAndClearGrokAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[model]\nname = \"grok-4.5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGrokModelAPIKey(path, "xai-secret-1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `api_key = "xai-secret-1"`) {
		t.Fatalf("key not written: %s", b)
	}
	if !strings.Contains(string(b), "[model]") {
		t.Fatalf("clobbered the rest of the config: %s", b)
	}
	// Replace rather than append a second key.
	if err := credstore.SetGrokModelAPIKey(path, "xai-secret-2"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if n := strings.Count(string(b), "api_key"); n != 1 {
		t.Fatalf("expected exactly one api_key line, got %d: %s", n, b)
	}
	if strings.Contains(string(b), "xai-secret-1") {
		t.Fatalf("stale key survived: %s", b)
	}
	if err := credstore.ClearGrokModelAPIKey(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "api_key") {
		t.Fatalf("key not cleared: %s", b)
	}
	if !strings.Contains(string(b), "[model]") {
		t.Fatalf("clear removed unrelated config: %s", b)
	}
}

// A key with a quote or backslash must not produce a config the agent cannot
// parse — vendor key formats are opaque and not ours to assume about.
func TestSetGrokAPIKeyEscapesValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := credstore.SetGrokModelAPIKey(path, `we"ird\key`); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `api_key = "we\"ird\\key"`) {
		t.Fatalf("value not escaped: %s", b)
	}
}

func TestValidateSecret(t *testing.T) {
	if err := credstore.ValidateSecret(""); err == nil {
		t.Error("empty accepted")
	}
	if err := credstore.ValidateSecret("  \t "); err == nil {
		t.Error("whitespace-only accepted")
	}
	if err := credstore.ValidateSecret(strings.Repeat("x", credstore.MaxSecretBytes+1)); err == nil {
		t.Error("oversized accepted")
	}
	if err := credstore.ValidateSecret("sk-fine"); err != nil {
		t.Errorf("rejected a normal key: %v", err)
	}
}
