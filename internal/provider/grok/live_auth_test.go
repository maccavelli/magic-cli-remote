//go:build live_grok

package grok_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

func isolateGrokHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", filepath.Join(home, ".grok"))
	t.Setenv("XAI_API_KEY", "")
	_ = os.Unsetenv("XAI_API_KEY")
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func initAuthMethods(t *testing.T, p *acpProc) ([]string, string) {
	t.Helper()
	msg := p.result(1)
	if msg == nil {
		t.Fatal("no initialize result")
	}
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		t.Fatalf("initialize result: %v", msg)
	}
	var ids []string
	if raw, ok := res["authMethods"].([]any); ok {
		for _, item := range raw {
			m, _ := item.(map[string]any)
			id, _ := m["id"].(string)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	def := ""
	if meta, ok := res["_meta"].(map[string]any); ok {
		def, _ = meta["defaultAuthMethodId"].(string)
	}
	return ids, def
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func rpcError(msg map[string]any) (code float64, message, data string) {
	errObj, _ := msg["error"].(map[string]any)
	if errObj == nil {
		return 0, "", ""
	}
	code, _ = errObj["code"].(float64)
	message, _ = errObj["message"].(string)
	switch d := errObj["data"].(type) {
	case string:
		data = d
	default:
		data = ""
	}
	return code, message, data
}

func TestLiveInitializeAuthMethodsHost(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok", "auth.json")); err != nil {
		t.Skip("no ~/.grok/auth.json")
	}
	p := startACPInit(t)
	ids, def := initAuthMethods(t, p)
	if !containsID(ids, "cached_token") || !containsID(ids, "grok.com") {
		t.Fatalf("host methods = %v, want cached_token and grok.com", ids)
	}
	if def != "cached_token" {
		t.Fatalf("defaultAuthMethodId = %q, want cached_token", def)
	}
}

func TestLiveColdInitializeOnlyGrokCom(t *testing.T) {
	isolateGrokHome(t)
	p := startACPInit(t)
	ids, def := initAuthMethods(t, p)
	if len(ids) != 1 || ids[0] != "grok.com" {
		t.Fatalf("cold methods = %v, want [grok.com]", ids)
	}
	if def != "" {
		t.Fatalf("cold defaultAuthMethodId = %q, want empty", def)
	}
}

func TestLiveQuotedModelKeyAdvertisesAPIKey(t *testing.T) {
	home := isolateGrokHome(t)
	// Fixture writes both static ids so we do not have to guess
	// currentModelId before initialize. Production SetCredential still
	// writes one table (MADR 0085 D4 / P2).
	path := filepath.Join(home, ".grok", "config.toml")
	if err := credstore.SetGrokModelAPIKey(path, "grok-4.5", "xai-probe-not-real"); err != nil {
		t.Fatal(err)
	}
	if err := credstore.SetGrokModelAPIKey(path, "grok-4.6", "xai-probe-not-real"); err != nil {
		t.Fatal(err)
	}
	p := startACPInit(t)
	ids, _ := initAuthMethods(t, p)
	if !containsID(ids, "xai.api_key") {
		t.Fatalf("methods = %v, want xai.api_key", ids)
	}
	p.send(t, 2, "authenticate", map[string]any{"methodId": "xai.api_key"})
	if err := p.waitID(t, 2, 15*time.Second); err != nil {
		t.Fatalf("authenticate xai.api_key: %v", err)
	}
	p.send(t, 3, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	if err := p.waitID(t, 3, 25*time.Second); err != nil {
		t.Fatalf("session/new: %v", err)
	}
}

func TestLiveUnquotedModelKeyDoesNotCount(t *testing.T) {
	home := isolateGrokHome(t)
	path := filepath.Join(home, ".grok", "config.toml")
	if err := os.WriteFile(path, []byte("[model.grok-4.5]\napi_key = \"xai-probe-not-real\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := startACPInit(t)
	p.send(t, 2, "authenticate", map[string]any{"methodId": "xai.api_key"})
	msg, err := p.waitRaw(t, 2, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	code, message, data := rpcError(msg)
	if code != -32000 {
		t.Fatalf("code = %v, want -32000 (%s %s)", code, message, data)
	}
	if !strings.Contains(data, "XAI_API_KEY") && !strings.Contains(message, "Authentication required") {
		t.Fatalf("error = %s data=%s, want XAI_API_KEY hint", message, data)
	}
}

func TestLiveLegacyAuthTableDoesNotCount(t *testing.T) {
	home := isolateGrokHome(t)
	path := filepath.Join(home, ".grok", "config.toml")
	if err := os.WriteFile(path, []byte("[auth]\napi_key = \"xai-probe-not-real\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := startACPInit(t)
	p.send(t, 2, "authenticate", map[string]any{"methodId": "xai.api_key"})
	msg, err := p.waitRaw(t, 2, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	code, message, data := rpcError(msg)
	if code != -32000 {
		t.Fatalf("code = %v, want -32000 (%s %s)", code, message, data)
	}
}

func TestLiveColdSessionNewWithoutAuth(t *testing.T) {
	isolateGrokHome(t)
	p := startACPInit(t)
	p.send(t, 2, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	msg, err := p.waitRaw(t, 2, 8*time.Second)
	if err != nil {
		t.Logf("session/new did not return in 8s (G5 hang): %v", err)
		return
	}
	code, message, data := rpcError(msg)
	if code != -32000 {
		t.Fatalf("session/new = code %v msg=%s data=%s, want -32000 or timeout", code, message, data)
	}
}

func TestLiveUnknownMethod(t *testing.T) {
	p := startACPInit(t)
	p.send(t, 2, "authenticate", map[string]any{"methodId": "does-not-exist"})
	msg, err := p.waitRaw(t, 2, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	code, message, data := rpcError(msg)
	if code != -32602 {
		t.Fatalf("code = %v, want -32602 (%s %s)", code, message, data)
	}
	if !strings.Contains(data, "unsupported auth method") && !strings.Contains(message, "Invalid params") {
		t.Fatalf("error = %s data=%s", message, data)
	}
}

func TestLiveLogoutAbsent(t *testing.T) {
	p := startACPInit(t)
	p.send(t, 2, "logout", map[string]any{})
	msg, err := p.waitRaw(t, 2, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := rpcError(msg)
	if code != -32601 {
		t.Fatalf("logout code = %v, want -32601", code)
	}
}
