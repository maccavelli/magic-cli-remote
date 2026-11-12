package gobble

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// Item 8: the catalog is acpext's providers, in its order, each with a key
// method, a device method for the deviceSignIn ids alone (MADR 0179 A9.2: at
// acpext v0.4.0 openai is marked Login and has none), and grok's browser
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
		if slices.Contains(types, provider.AuthMethodOAuthDevice) != slices.Contains(deviceSignIn, p.ID) {
			t.Errorf("%s methods %v; a device method only for deviceSignIn %v", p.ID, types, deviceSignIn)
		}
		if p.ID == "openai" && slices.Contains(types, provider.AuthMethodOAuthDevice) {
			t.Errorf("openai methods %v; gobble refuses --method device for it", types)
		}
	}
}

// A9.2: the two id tables partition the providers acpext marks Login. A new
// Login provider in a later acpext is in neither and fails here, and someone
// decides which table it belongs in.
func TestDeviceTablesPartitionAcpextsLogins(t *testing.T) {
	listed := map[string]bool{}
	for _, p := range acpext.AuthProviders() {
		listed[p.ID] = true
		if !p.Login {
			continue
		}
		n := 0
		for _, table := range [][]string{deviceSignIn, noPhoneSignIn} {
			if slices.Contains(table, p.ID) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s is marked Login by acpext and is in %d of deviceSignIn %v, noPhoneSignIn %v; want exactly one",
				p.ID, n, deviceSignIn, noPhoneSignIn)
		}
	}
	for name, table := range map[string][]string{"deviceSignIn": deviceSignIn, "noPhoneSignIn": noPhoneSignIn} {
		for _, id := range table {
			if !listed[id] {
				t.Errorf("%s names %q, which acpext does not list", name, id)
			}
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

// Item 8: a device sign-in for a provider without one is unsupported.
func TestDeviceAuthNeedsADeviceSignIn(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	_, _, err := newAuth(st.bin, nil).startDeviceAuth(context.Background(), "", "claude", "claude"+methodDevice, nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err = %v, want ErrAuthMethodUnsupported", err)
	}
	if runs := st.runs(t); len(runs) != 0 {
		t.Fatalf("gobble was run for a provider with no login: %q", runs)
	}
}

// A9.2: openai is marked Login by acpext v0.4.0, but gobble refuses --method
// device for it, so no child is run and the method is unsupported.
func TestDeviceAuthRefusesOpenAI(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	_, _, err := newAuth(st.bin, nil).startDeviceAuth(context.Background(), "", "openai", "openai"+methodDevice, nil)
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err = %v, want ErrAuthMethodUnsupported", err)
	}
	if runs := st.runs(t); len(runs) != 0 {
		t.Fatalf("gobble was run for openai's device sign-in: %q", runs)
	}
}

// D3: a device sign-in's device_code event is the phone's flow, and wait
// follows it to saved.
func TestDeviceAuthGivesTheCode(t *testing.T) {
	st := newStub(t, stubStatus(sourceAuthJSON))
	flow, wait, err := newAuth(st.bin, nil).startDeviceAuth(context.Background(), "", "grok", "grok"+methodDevice, nil)
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

// A9.3: the read of the device code is bounded. A gobble that prints nothing
// and does not exit fails the start at deviceCodeScanTimeout (shortened here to
// 100 ms), and its child is killed: startDeviceAuth waits for the child to
// end, so returning long before the stub's own 20 s sleep ran out shows the
// kill.
func TestDeviceCodeReadIsBounded(t *testing.T) {
	const sleepSeconds = 20
	dir := t.TempDir()
	bin := testexec.WriteShellStub(t, filepath.Join(dir, "gobble"), "exec sleep "+strconv.Itoa(sleepSeconds)+"\n")
	old := deviceCodeScanTimeout
	deviceCodeScanTimeout = 100 * time.Millisecond
	t.Cleanup(func() { deviceCodeScanTimeout = old })

	start := time.Now()
	_, wait, err := newAuth(bin, nil).startDeviceAuth(context.Background(), "", "grok", "grok"+methodDevice, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("a silent gobble gave a flow (wait %v)", wait != nil)
	}
	if !errors.Is(err, errNoDeviceCode) || !strings.Contains(err.Error(), "gobble printed no device code within 100ms") {
		t.Fatalf("err = %v, want \"gobble printed no device code within 100ms\"", err)
	}
	if elapsed > sleepSeconds*time.Second/2 {
		t.Fatalf("the start returned after %s: the silent child was waited out, not killed", elapsed)
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

// A9.4: a row gobble gives a reason is not ready, whatever usable says. gobble
// v0.5.0 sets "oauth_unused" on an oauth entry of a provider with no login and
// leaves usable true; after gobble-cli's 0052-MADR D8 a reason means "not
// usable". Such a row is missing, none of its methods is Configured, and its
// id is not in the handshake's preference. kilo's row has no reason and is the
// control.
func TestReasonMeansNotConfigured(t *testing.T) {
	const withReason = `{"providers":[` +
		`{"provider":"claude","source":"auth.json","authType":"oauth","usable":true,"reason":"oauth_unused"},` +
		`{"provider":"openai","source":"auth.json","authType":"api_key","usable":true,"reason":"empty_key"},` +
		`{"provider":"grok","source":"auth.json","authType":"oauth","usable":true,"reason":"oauth_unused"},` +
		`{"provider":"kilo","source":"auth.json","authType":"oauth","usable":true}]}`
	st := newStub(t, withReason)
	a := newAuth(st.bin, nil)
	state, err := a.status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != provider.AuthConfigured {
		t.Errorf("state = %q, want configured: kilo's row has no reason", state.Status)
	}
	for _, u := range state.Upstreams {
		want := provider.AuthMissing
		if u.ID == "kilo" {
			want = provider.AuthConfigured
		}
		if u.Status != want {
			t.Errorf("%s = %q, want %q", u.ID, u.Status, want)
		}
		for _, m := range u.Methods {
			if !m.ConfiguredKnown {
				continue
			}
			wantCfg := u.ID == "kilo" && m.Type == provider.AuthMethodOAuthDevice
			if m.Configured != wantCfg {
				t.Errorf("%s: method %s Configured = %v, want %v", u.ID, m.ID, m.Configured, wantCfg)
			}
		}
	}
	if got := a.readyUpstreams(context.Background()); !slices.Equal(got, []string{"kilo"}) {
		t.Errorf("readyUpstreams = %v, want [kilo]: a row with a reason is not ready", got)
	}

	// With kilo's reason-free row gone, nothing is ready.
	st.setStatus(t, strings.Replace(withReason, `"authType":"oauth","usable":true}`,
		`"authType":"oauth","usable":true,"reason":"oauth_unused"}`, 1), 0)
	a.drop()
	state, err = a.status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if state.Status != provider.AuthMissing {
		t.Errorf("state = %q, want missing: every row has a reason", state.Status)
	}
	if got := a.readyUpstreams(context.Background()); len(got) != 0 {
		t.Errorf("readyUpstreams = %v, want none", got)
	}
	if a.hasCredential() {
		t.Error("hasCredential = true, want false: every row has a reason")
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
