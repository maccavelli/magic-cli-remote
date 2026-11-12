package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// fakeEngine answers the control plane's integration and credential routes as
// opencode 2.2.3 does (0214 PLAN P6b Deviation 1): get and connect answer 404
// until a list has activated plugins; a connect adds a credential and makes it
// active; DELETE answers 204 whether or not it removed anything.
type fakeEngine struct {
	t *testing.T

	mu           sync.Mutex
	activated    bool
	integrations []*fakeIntegration
	next         int
	requests     []string
	keys         []string
	bodies       []map[string]any
	cancelled    []string
	statuses     []string // what successive OAuth status reads answer
	attempt      oauthAttempt

	// connectBehind stores a new credential behind the older ones, as an
	// engine not using it would.
	connectBehind bool
	// deleteKeeps answers 204 to a DELETE and removes nothing.
	deleteKeeps bool
}

type fakeIntegration struct {
	info  integration
	creds []connection // the active one first
	env   []string
}

func (f *fakeEngine) add(id, name string, methods ...integrationMethod) *fakeIntegration {
	in := &fakeIntegration{info: integration{ID: id, Name: name, Methods: methods}}
	f.integrations = append(f.integrations, in)
	return in
}

func (f *fakeEngine) find(id string) *fakeIntegration {
	for _, in := range f.integrations {
		if in.info.ID == id {
			return in
		}
	}
	return nil
}

func (in *fakeIntegration) view() integration {
	v := in.info
	v.Connections = append([]connection(nil), in.creds...)
	for _, n := range in.env {
		v.Connections = append(v.Connections, connection{Type: "env", Name: n})
	}
	return v
}

func (f *fakeEngine) cred(in *fakeIntegration, method string) {
	f.next++
	c := connection{Type: "credential", ID: fmt.Sprintf("cred_%d", f.next), Label: in.info.Name, Method: method}
	if f.connectBehind {
		in.creds = append(in.creds, c)
		return
	}
	in.creds = append([]connection{c}, in.creds...)
}

func reply(status int, v any) (control.Response, error) {
	if v == nil {
		return control.Response{Status: status}, nil
	}
	b, err := json.Marshal(v)
	return control.Response{Status: status, Body: b}, err
}

func notFound(id string) (control.Response, error) {
	return reply(http.StatusNotFound, map[string]any{"_tag": "IntegrationNotFoundError", "message": "Integration not found: " + id})
}

func (f *fakeEngine) Do(_ context.Context, req control.Request) (control.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req.Method+" "+req.Path)
	body, _ := req.Body.(map[string]any)
	if body != nil {
		if k, ok := body["key"].(string); ok {
			f.keys = append(f.keys, k)
		}
		f.bodies = append(f.bodies, body)
	}
	parts := strings.Split(strings.TrimPrefix(req.Path, "/api/"), "/")
	switch {
	case req.Method == http.MethodGet && req.Path == "/api/integration":
		f.activated = true
		var list []integration
		for _, in := range f.integrations {
			list = append(list, in.view())
		}
		return reply(http.StatusOK, map[string]any{"location": map[string]any{}, "data": list})
	case parts[0] == "credential" && req.Method == http.MethodGet:
		f.t.Errorf("GET %s: the control plane never reads credentials", req.Path)
		return reply(http.StatusOK, nil)
	case parts[0] == "credential" && req.Method == http.MethodDelete && len(parts) == 2:
		if !f.deleteKeeps {
			for _, in := range f.integrations {
				in.creds = slices.DeleteFunc(in.creds, func(c connection) bool { return c.ID == parts[1] })
			}
		}
		return reply(http.StatusNoContent, nil)
	case parts[0] != "integration" || len(parts) < 2:
		f.t.Errorf("unexpected request %s %s", req.Method, req.Path)
		return reply(http.StatusNotFound, nil)
	}
	in := f.find(parts[1])
	if !f.activated || in == nil {
		return notFound(parts[1])
	}
	rest := strings.Join(parts[2:], "/")
	switch {
	case req.Method == http.MethodGet && rest == "":
		return reply(http.StatusOK, map[string]any{"location": map[string]any{}, "data": in.view()})
	case req.Method == http.MethodPost && rest == "connect/key":
		f.cred(in, "key")
		return reply(http.StatusNoContent, nil)
	case req.Method == http.MethodPost && rest == "connect/oauth":
		return reply(http.StatusOK, map[string]any{"location": map[string]any{}, "data": f.attempt})
	case strings.HasPrefix(rest, "connect/oauth/") && req.Method == http.MethodDelete:
		f.cancelled = append(f.cancelled, strings.TrimPrefix(rest, "connect/oauth/"))
		return reply(http.StatusNoContent, nil)
	case strings.HasPrefix(rest, "connect/oauth/") && req.Method == http.MethodGet:
		st := "pending"
		if len(f.statuses) > 0 {
			st, f.statuses = f.statuses[0], f.statuses[1:]
		}
		if st == "complete" {
			f.cred(in, "oauth")
		}
		return reply(http.StatusOK, map[string]any{"data": map[string]any{"status": st, "message": "the vendor said no"}})
	}
	f.t.Errorf("unexpected request %s %s", req.Method, req.Path)
	return reply(http.StatusNotFound, nil)
}

func (f *fakeEngine) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// newFakeAuth is acpAuth on a fake engine, counting restarts.
func newFakeAuth(t *testing.T) (*acpAuth, *fakeEngine, *[]string) {
	f := &fakeEngine{t: t}
	var restarts []string
	return &acpAuth{plane: f, log: slog.New(slog.DiscardHandler), restart: func(n string) { restarts = append(restarts, n) }}, f, &restarts
}

var (
	keyMethod   = integrationMethod{Type: "key"}
	envMethod   = integrationMethod{Type: "env"}
	oauthDevice = integrationMethod{Type: "oauth", ID: "device", Label: "Login with the vendor"}
)

// TestACPAuthListsFirst: every operation reads the list before anything else,
// so the engine has activated its integrations; on 2.2.3, get and connect
// answer 404 before that.
func TestACPAuthListsFirst(t *testing.T) {
	ops := map[string]func(a *acpAuth) error{
		"status":  func(a *acpAuth) error { _, err := a.status(context.Background()); return err },
		"catalog": func(a *acpAuth) error { _, err := a.catalog(context.Background()); return err },
		"set": func(a *acpAuth) error {
			return a.setCredential(context.Background(), "vendor", "vendor:key", "sk-new", nil)
		},
		"clear": func(a *acpAuth) error { return a.clearCredential(context.Background(), "vendor", "") },
		"device": func(a *acpAuth) error {
			_, _, err := a.startDeviceAuth(context.Background(), "", "vendor", "vendor:oauth:device", nil)
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			a, f, _ := newFakeAuth(t)
			v := f.add("vendor", "Vendor", keyMethod, oauthDevice)
			f.cred(v, "key")
			f.attempt = oauthAttempt{AttemptID: "con_1", URL: "https://vendor.example/device", Instructions: "Enter code: ABCD-1234", Mode: "auto"}
			if err := op(a); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got := f.sent(); len(got) == 0 || got[0] != "GET /api/integration" {
				t.Errorf("%s sent %v first, want the list", name, got)
			}
		})
	}
}

// TestACPAuthStatusIsSmall: status lists the configured integrations and those
// needing more than a bare key, with methods the phone can drive; the rest of
// the catalog, and MCP sign-ins, stay out.
func TestACPAuthStatusIsSmall(t *testing.T) {
	a, f, _ := newFakeAuth(t)
	f.add("plain", "Plain", keyMethod, envMethod)
	keyed := f.add("keyed", "Keyed", keyMethod, envMethod)
	f.cred(keyed, "key")
	f.add("envd", "Env", keyMethod, envMethod).env = []string{"ENVD_API_KEY"}
	f.add("oauthy", "OAuthy", keyMethod, oauthDevice, integrationMethod{Type: "oauth", ID: "browser", Label: "Login (browser)"})
	f.add("formed", "Formed", integrationMethod{Type: "key", Form: []formField{
		{Key: "kind", Type: "string", Title: "Kind", Required: true, Options: []struct {
			Value       string `json:"value"`
			Label       string `json:"label"`
			Description string `json:"description"`
		}{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}},
		{Key: "url", Type: "string", Title: "URL", Default: "https://default.example", When: []struct {
			Key   string `json:"key"`
			Op    string `json:"op"`
			Value any    `json:"value"`
		}{{Key: "kind", Op: "eq", Value: "b"}}},
		{Key: "server", Type: "string", Hidden: true, Default: "https://hidden.example"},
	}})
	f.add("numbered", "Numbered", integrationMethod{Type: "key", Form: []formField{{Key: "port", Type: "number"}}})
	f.add("cloud", "Cloud", integrationMethod{Type: "external", ID: "profile", Label: "Cloud profile"})
	mcp := f.add("mcp-server", "MCP", oauthDevice)
	mcp.info.Metadata = map[string]any{"source": "mcp"}
	f.cred(mcp, "oauth")

	st, err := a.status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != provider.AuthConfigured {
		t.Errorf("status %q, want configured", st.Status)
	}
	got := map[string]provider.UpstreamAuth{}
	var ids []string
	for _, up := range st.Upstreams {
		got[up.ID] = up
		ids = append(ids, up.ID)
	}
	if want := []string{"cloud", "envd", "formed", "keyed", "numbered", "oauthy"}; !slices.Equal(ids, want) {
		t.Fatalf("status lists %v, want %v", ids, want)
	}
	if k := got["keyed"]; k.Status != provider.AuthConfigured || len(k.Methods) != 1 || k.Methods[0].ID != "keyed:key" ||
		k.Methods[0].Type != provider.AuthMethodAPIKey || !k.Methods[0].ConfiguredKnown || !k.Methods[0].Configured {
		t.Errorf("keyed = %+v, want configured with its key method configured", k)
	}
	if e := got["envd"]; e.Status != provider.AuthConfigured || e.Methods[0].Configured {
		t.Errorf("envd = %+v, want configured through env, its key method not", e)
	}
	o := got["oauthy"]
	if o.Status != provider.AuthMissing || len(o.Methods) != 3 || o.Methods[1].ID != "oauthy:oauth:device" ||
		o.Methods[1].Type != provider.AuthMethodOAuthDevice || o.Methods[2].Type != provider.AuthMethodOAuthDevice ||
		o.Methods[1].ConfiguredKnown {
		t.Errorf("oauthy = %+v, want a key and two device methods (no label decides, MADR 0074 D30), which OAuth owns the credential unknown", o)
	}
	fm := got["formed"].Methods[0]
	if len(fm.Inputs) != 2 || fm.Inputs[0].Type != provider.AuthInputSelect || len(fm.Inputs[0].Options) != 2 ||
		fm.Inputs[1].Placeholder != "https://default.example" || fm.Inputs[1].When == nil || fm.Inputs[1].When.Value != "b" {
		t.Errorf("formed's inputs = %+v, want a select, then a text shown when kind is b, and no hidden field", fm.Inputs)
	}
	if m := got["numbered"].Methods[0]; !m.Unavailable || m.Reason != hostOnlyReason {
		t.Errorf("a form with a number field = %+v, want unavailable, host only", m)
	}
	if m := got["cloud"].Methods[0]; m.ID != "cloud:external:profile" || !m.Unavailable || m.Reason != hostOnlyReason {
		t.Errorf("an external method = %+v, want unavailable, host only", m)
	}
}

// TestACPAuthCatalogListsEveryProvider: the catalog is every integration but
// an MCP server's, sorted, from the engine.
func TestACPAuthCatalogListsEveryProvider(t *testing.T) {
	a, f, _ := newFakeAuth(t)
	f.add("zeta", "Zeta", keyMethod)
	f.add("alpha", "Alpha", keyMethod)
	f.add("mcp", "MCP", oauthDevice).info.Metadata = map[string]any{"source": "mcp"}
	cat, err := a.catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, up := range cat.Upstreams {
		ids = append(ids, up.ID)
	}
	if !slices.Equal(ids, []string{"alpha", "zeta"}) || cat.Source != provider.AuthCatalogSourceEngine {
		t.Errorf("catalog %v from %q, want alpha and zeta from the engine", ids, cat.Source)
	}
}

// TestACPSetCredentialReplacesTheOlderKey pins "Replace, clear all" for an
// add: the key goes to connect/key once, the new credential is confirmed
// active, the older key is removed and its removal confirmed, an OAuth
// credential stays, and the sessions restart once.
func TestACPSetCredentialReplacesTheOlderKey(t *testing.T) {
	a, f, restarts := newFakeAuth(t)
	v := f.add("vendor", "Vendor", integrationMethod{Type: "key", Form: []formField{{Key: "region", Type: "string"}}}, oauthDevice)
	f.cred(v, "key")   // cred_1
	f.cred(v, "oauth") // cred_2
	err := a.setCredential(context.Background(), "vendor", "vendor:key", "sk-new", map[string]string{"region": "eu", "blank": " "})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/integration", "GET /api/integration/vendor", "POST /api/integration/vendor/connect/key",
		"GET /api/integration/vendor", "DELETE /api/credential/cred_1", "GET /api/integration/vendor"}
	if got := f.sent(); !slices.Equal(got, want) {
		t.Errorf("requests\n got %v\nwant %v", got, want)
	}
	if !slices.Equal(f.keys, []string{"sk-new"}) {
		t.Errorf("keys sent %v, want the new key once", f.keys)
	}
	if ans, _ := f.bodies[0]["answer"].(map[string]any); len(ans) != 1 || ans["region"] != "eu" {
		t.Errorf("answer %v, want region alone: an empty value is left out", f.bodies[0]["answer"])
	}
	var methods []string
	for _, c := range v.creds {
		methods = append(methods, c.ID+":"+c.Method)
	}
	if !slices.Equal(methods, []string{"cred_3:key", "cred_2:oauth"}) {
		t.Errorf("credentials after %v, want the new key and the OAuth sign-in", methods)
	}
	if len(*restarts) != 1 || (*restarts)[0] != credentialChangedNotice {
		t.Errorf("restarts %v, want one", *restarts)
	}
}

// TestACPSetCredentialChecksTheEngine: a key the engine did not make active is
// not accepted, though the sessions still restart; a replaced key that
// survives its DELETE is an error; a method that is not a key is refused
// before anything is sent.
func TestACPSetCredentialChecksTheEngine(t *testing.T) {
	t.Run("not active", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		f.cred(f.add("vendor", "Vendor", keyMethod), "key")
		f.connectBehind = true
		err := a.setCredential(context.Background(), "vendor", "", "sk-new", nil)
		if !errors.Is(err, provider.ErrCredentialNotAccepted) {
			t.Errorf("err = %v, want not accepted", err)
		}
		if len(*restarts) != 1 {
			t.Errorf("restarts %v, want one: the engine stored the key", *restarts)
		}
	})
	t.Run("old key kept", func(t *testing.T) {
		a, f, _ := newFakeAuth(t)
		f.cred(f.add("vendor", "Vendor", keyMethod), "key")
		f.deleteKeeps = true
		err := a.setCredential(context.Background(), "vendor", "vendor:api", "sk-new", nil)
		if err == nil || !strings.Contains(err.Error(), "still holds 1 replaced") {
			t.Errorf("err = %v, want the surviving key named", err)
		}
	})
	t.Run("not a key", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		f.add("vendor", "Vendor", keyMethod, oauthDevice)
		err := a.setCredential(context.Background(), "vendor", "vendor:oauth:device", "sk-new", nil)
		if !errors.Is(err, provider.ErrAuthMethodUnsupported) || len(f.sent()) != 0 || len(*restarts) != 0 {
			t.Errorf("err = %v, sent %v, restarts %v; want refused with nothing sent", err, f.sent(), *restarts)
		}
	})
}

// TestACPClearCredentialRemovesEvery pins "Replace, clear all" for a removal.
func TestACPClearCredentialRemovesEvery(t *testing.T) {
	t.Run("every stored credential, and the env named", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		v := f.add("vendor", "Vendor", keyMethod, oauthDevice, envMethod)
		f.cred(v, "key")
		f.cred(v, "oauth")
		v.env = []string{"VENDOR_API_KEY"}
		err := a.clearCredential(context.Background(), "vendor", "")
		if err == nil || !strings.Contains(err.Error(), "VENDOR_API_KEY") {
			t.Errorf("err = %v, want the environment variable named", err)
		}
		if len(v.creds) != 0 {
			t.Errorf("credentials left %v", v.creds)
		}
		if got := f.sent(); !slices.Contains(got, "DELETE /api/credential/cred_1") || !slices.Contains(got, "DELETE /api/credential/cred_2") {
			t.Errorf("requests %v, want both removed", got)
		}
		if len(*restarts) != 1 {
			t.Errorf("restarts %v, want one", *restarts)
		}
	})
	t.Run("a DELETE that removed nothing", func(t *testing.T) {
		a, f, _ := newFakeAuth(t)
		f.cred(f.add("vendor", "Vendor", keyMethod), "key")
		f.deleteKeeps = true
		if err := a.clearCredential(context.Background(), "vendor", ""); err == nil || !strings.Contains(err.Error(), "still holds 1") {
			t.Errorf("err = %v, want the surviving credential reported", err)
		}
	})
	t.Run("nothing stored", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		f.add("vendor", "Vendor", keyMethod)
		if err := a.clearCredential(context.Background(), "vendor", ""); err != nil || len(*restarts) != 0 {
			t.Errorf("err = %v, restarts %v; want nothing to do", err, *restarts)
		}
	})
}

// TestACPDeviceAuth: a device attempt shows its code, its wait follows the
// attempt's status and adopts the credential; a browser or code-mode attempt
// is refused and cancelled; a cancelled wait cancels the attempt.
func TestACPDeviceAuth(t *testing.T) {
	defer func(d time.Duration) { acpOAuthPoll = d }(acpOAuthPoll)
	acpOAuthPoll = time.Millisecond
	device := oauthAttempt{AttemptID: "con_1", URL: "https://github.com/login/device", Instructions: "Enter code: ABCD-1234", Mode: "auto"}
	device.Time.Expires = time.Now().Add(10 * time.Minute).UnixMilli()

	t.Run("device, complete", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		v := f.add("vendor", "Vendor", keyMethod, integrationMethod{Type: "oauth", ID: "device", Form: []formField{{Key: "deploymentType", Type: "string"}}})
		f.cred(v, "oauth") // cred_1, replaced
		f.cred(v, "key")   // cred_2, kept
		f.attempt = device
		f.statuses = []string{"pending", "complete"}
		flow, wait, err := a.startDeviceAuth(context.Background(), "", "vendor", "vendor:oauth:device", map[string]string{"deploymentType": "github.com"})
		if err != nil {
			t.Fatal(err)
		}
		if flow.UserCode != "ABCD-1234" || flow.VerificationURI == "" || flow.ExpiresIn < 500 {
			t.Errorf("flow = %+v", flow)
		}
		if b := f.bodies[0]; b["methodID"] != "device" || b["answer"].(map[string]any)["deploymentType"] != "github.com" {
			t.Errorf("connect/oauth body %v", b)
		}
		if err := wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		var left []string
		for _, c := range v.creds {
			left = append(left, c.ID+":"+c.Method)
		}
		if !slices.Equal(left, []string{"cred_3:oauth", "cred_2:key"}) || len(*restarts) != 1 {
			t.Errorf("credentials %v, restarts %v; want the new sign-in and the key, and one restart", left, *restarts)
		}
	})
	t.Run("failed", func(t *testing.T) {
		a, f, restarts := newFakeAuth(t)
		f.add("vendor", "Vendor", oauthDevice)
		f.attempt = device
		f.statuses = []string{"failed"}
		_, wait, err := a.startDeviceAuth(context.Background(), "", "vendor", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := wait(context.Background()); err == nil || !strings.Contains(err.Error(), "the vendor said no") || len(*restarts) != 0 {
			t.Errorf("err = %v, restarts %v; want the engine's message and no restart", err, *restarts)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		a, f, _ := newFakeAuth(t)
		f.add("vendor", "Vendor", oauthDevice)
		f.attempt = device
		_, wait, err := a.startDeviceAuth(context.Background(), "", "vendor", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := wait(ctx); !errors.Is(err, context.DeadlineExceeded) || !slices.Equal(f.cancelled, []string{"con_1"}) {
			t.Errorf("err = %v, cancelled %v; want the attempt cancelled", err, f.cancelled)
		}
	})
	for name, att := range map[string]oauthAttempt{
		"browser": {AttemptID: "con_2", URL: "https://vendor.example/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fcallback", Mode: "auto"},
		"code":    {AttemptID: "con_2", URL: "https://github.com/login/device", Instructions: "Enter code: ABCD-1234", Mode: "code"},
	} {
		t.Run(name+" refused", func(t *testing.T) {
			a, f, _ := newFakeAuth(t)
			f.add("vendor", "Vendor", oauthDevice)
			f.attempt = att
			_, _, err := a.startDeviceAuth(context.Background(), "", "vendor", "", nil)
			if !errors.Is(err, provider.ErrAuthUnsupported) || !slices.Equal(f.cancelled, []string{"con_2"}) {
				t.Errorf("err = %v, cancelled %v; want refused and cancelled", err, f.cancelled)
			}
		})
	}
	t.Run("which method", func(t *testing.T) {
		a, f, _ := newFakeAuth(t)
		f.add("vendor", "Vendor", oauthDevice, integrationMethod{Type: "oauth", ID: "other"})
		_, _, err := a.startDeviceAuth(context.Background(), "", "vendor", "", nil)
		if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
			t.Errorf("no method named among two: %v, want refused", err)
		}
	})
}

// TestACPProviderWiresAuth: the provider NewACP builds answers auth through
// the control plane, and a credential change restarts its open sessions.
func TestACPProviderWiresAuth(t *testing.T) {
	f := &fakeEngine{t: t}
	f.add("vendor", "Vendor", keyMethod, oauthDevice)
	p := newACP(acpagent.Config{}, nil, f)
	if !p.SupportsDeviceAuth() {
		t.Error("device auth not wired")
	}
	if _, err := p.AuthCatalogList(context.Background()); err != nil {
		t.Errorf("catalog: %v", err)
	}
	s, _ := acptest.Start(t, p, acptest.Options{Replies: map[string]string{
		"initialize": `{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.2.3"},"agentCapabilities":{"loadSession":true}}`,
	}})
	acptest.Drain(s)
	if err := p.SetCredential(context.Background(), "vendor", "vendor:key", "sk-new", nil); err != nil {
		t.Fatal(err)
	}
	if ev := acptest.Next(t, s, event.TypeNotice); ev.Text != credentialChangedNotice {
		t.Errorf("notice %q, want the credential change", ev.Text)
	}
	if st, err := p.AuthStatus(context.Background()); err != nil || st.Status != provider.AuthConfigured {
		t.Errorf("status %+v, %v; want configured", st, err)
	}
}
