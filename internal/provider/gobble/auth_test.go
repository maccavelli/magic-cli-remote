package gobble

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Item 8: the catalog is acpext's providers, in its order, each with a key
// method, a device method where acpext marks a login, and grok's browser
// sign-in marked unavailable.
func TestCatalogIsAcpextsProviders(t *testing.T) {
	cat, err := newAuth("gobble", nil).catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.Source != provider.AuthCatalogSourceStatic {
		t.Errorf("source = %q, want static", cat.Source)
	}
	var got, want []string
	for _, u := range cat.Upstreams {
		got = append(got, u.ID)
	}
	for _, p := range acpext.AuthProviders() {
		want = append(want, p.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("upstreams = %v, want acpext's %v", got, want)
	}
	for i, p := range acpext.AuthProviders() {
		var types []string
		for _, m := range cat.Upstreams[i].Methods {
			types = append(types, m.Type)
			if m.Type == provider.AuthMethodOAuthBrowser && (!m.Unavailable || m.Reason != provider.AuthReasonHostOAuth) {
				t.Errorf("%s's browser sign-in is %+v, want unavailable, host_oauth", p.ID, m)
			}
		}
		if slices.Contains(types, provider.AuthMethodOAuthDevice) != p.Login {
			t.Errorf("%s methods %v; a device method only where acpext marks a login", p.ID, types)
		}
	}
}

// Item 8: a key goes to gobble on stdin, never in argv.
func TestSetCredentialPutsTheKeyOnStdin(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	a := newAuth(st.bin, nil)
	const key = "sk-test-not-a-real-key"
	if err := a.setCredential(context.Background(), "claude", "claude"+methodAPI, key,
		map[string]string{workspaceInput: "ws-1"}); err != nil {
		t.Fatalf("setCredential: %v", err)
	}
	runs := st.runs(t)
	if len(runs) != 1 || runs[0] != "auth login claude --api-key - --json --workspace ws-1" {
		t.Fatalf("argv = %q, want the login with the workspace", runs)
	}
	if strings.Contains(strings.Join(runs, "\n"), key) {
		t.Fatal("the key is in argv")
	}
	if got := st.stdin(t); got != key+"\n" {
		t.Fatalf("stdin = %q, want the key", got)
	}
}

// Item 8: a refused login is gobble's error event's message.
func TestSetCredentialReportsGobblesError(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	if err := os.WriteFile(filepath.Join(st.dir, "login.error"),
		[]byte(`{"event":"error","provider":"openai","kind":"usage","message":"openai: the key is empty"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := newAuth(st.bin, nil).setCredential(context.Background(), "openai", "openai"+methodAPI, "k", nil)
	if err == nil || !strings.Contains(err.Error(), "openai: the key is empty") {
		t.Fatalf("setCredential err = %v, want gobble's message", err)
	}
}

// Item 8: a device sign-in for a provider without a login is unsupported.
func TestDeviceAuthNeedsALogin(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	_, _, err := newAuth(st.bin, nil).startDeviceAuth(context.Background(), "", "claude", "claude"+methodDevice, nil, false)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err = %v, want ErrAuthMethodUnsupported", err)
	}
	if runs := st.runs(t); len(runs) != 0 {
		t.Fatalf("gobble was run for a provider with no login: %q", runs)
	}
}

// D3: a device sign-in's device_code event is the phone's flow, and wait
// follows it to saved.
func TestDeviceAuthGivesTheCode(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	flow, wait, err := newAuth(st.bin, nil).startDeviceAuth(context.Background(), "", "grok", "grok"+methodDevice, nil, false)
	if err != nil {
		t.Fatalf("startDeviceAuth: %v", err)
	}
	if flow.VerificationURI != "https://example.invalid/device" || flow.UserCode != "ABCD-1234" {
		t.Fatalf("flow = %+v, want the stub's url and code", flow)
	}
	if err := wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if runs := st.runs(t); len(runs) != 1 || runs[0] != "auth login grok --method device --json" {
		t.Fatalf("argv = %q", runs)
	}
}

// A4: the status maps gobble's rows: usable is configured; only an
// auth.json api_key row's key method is Configured; an env key is ready but
// not the daemon's to remove; an error report is the state's error.
func TestStatusMapsGobblesRows(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON, "openai"))
	a := newAuth(st.bin, nil)
	state, err := a.status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != provider.AuthConfigured {
		t.Errorf("state = %q, want configured", state.Status)
	}
	for _, u := range state.Upstreams {
		want := provider.AuthMissing
		if u.ID == "openai" {
			want = provider.AuthConfigured
		}
		if u.Status != want {
			t.Errorf("%s = %q, want %q", u.ID, u.Status, want)
		}
		if got := u.Methods[0].Configured; got != (u.ID == "openai") || !u.Methods[0].ConfiguredKnown {
			t.Errorf("%s's key method Configured = %v (known %v)", u.ID, got, u.Methods[0].ConfiguredKnown)
		}
	}

	st.setStatus(t, stubStatus("env", "openai"), 0)
	a.drop()
	state, err = a.status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, u := range state.Upstreams {
		if u.ID == "openai" && (u.Status != provider.AuthConfigured || u.Methods[0].Configured) {
			t.Errorf("an env key: %s = %q, Configured %v; want configured and not removable", u.ID, u.Status, u.Methods[0].Configured)
		}
	}

	st.setStatus(t, `{"error":{"file":"auth.json","message":"auth.json: line 3: invalid character"}}`, 2)
	a.drop()
	state, err = a.status(context.Background())
	if state.Status != provider.AuthError || err == nil || !strings.Contains(err.Error(), "auth.json: line 3") {
		t.Fatalf("an error report: state %q, err %v; want error with gobble's message", state.Status, err)
	}
}

// D3: one status call serves the cache's time, and a write drops it.
func TestStatusIsCachedUntilAWrite(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	a := newAuth(st.bin, nil)
	for range 3 {
		if _, err := a.status(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(st.runs(t)); n != 1 {
		t.Fatalf("%d status runs, want 1 within the cache's time", n)
	}
	if err := a.clearCredential(context.Background(), "openai", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(st.runs(t)); n != 3 {
		t.Fatalf("%d runs, want status, logout, status: a write drops the cache", n)
	}
}

// D4: auth_method_id is empty or one of gobble's methods.
func TestAuthMethodIDRule(t *testing.T) {
	for id, ok := range map[string]bool{"": true, "openai": true, "gobble": false, "openai-login": false} {
		s := defaultSection()
		s.AuthMethodID = id
		err := s.Validate()
		if (err == nil) != ok {
			t.Errorf("auth_method_id %q: err = %v, want ok %v", id, err, ok)
		}
		if err != nil && !strings.Contains(err.Error(), "gemini, openai") {
			t.Errorf("the refusal %q does not name the allowed ids", err)
		}
	}
}

// The section loads with its defaults.
func TestDefaultSectionValidates(t *testing.T) {
	var _ config.ProviderSection = defaultSection()
	if err := defaultSection().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if d := Descriptor(); d.ID != provider.IDGobble || d.Floor != nil || d.Credentials != nil {
		t.Fatalf("descriptor = %+v, want gobble with no floor and no credential adapter", d)
	}
}
