package acptest

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

const keyHint = "put the key where the agent advertises it"

// keySpec is a Spec whose key method is vendor.key, with a key present: the
// shape of an agent that takes an API key (MADR 0234 D4).
func keySpec() acpagent.Spec {
	return acpagent.Spec{
		ID:                    provider.ID("fixture"),
		SafeAuthMethodIDs:     []string{"vendor.token", "vendor.key"},
		AuthPreference:        []string{"vendor.token", "vendor.key"},
		KeyAuthMethodID:       "vendor.key",
		HasHeadlessCredential: func() bool { return true },
		KeyNotAdvertisedHint:  keyHint,
	}
}

// startOn is Start without the fatal: it returns StartOn's error, so a test
// can assert on a start that must fail.
func startOn(t *testing.T, p *acpagent.Provider, opts Options) (provider.Session, *Agent, error) {
	t.Helper()
	opts.SessionID = "agent-1"
	opts.Start.CWD = t.TempDir()
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	a := &Agent{t: t, opts: opts, toConn: agentWrites, replies: map[string]string{},
		held: make(chan Message, 64), answers: make(chan Message, 64), nextID: 1000}
	go a.read(agentReads)
	ctx, cancel := context.WithTimeout(context.Background(), Budget)
	defer cancel()
	s, err := p.StartOn(ctx, opts.Start, clientReads, clientWrites)
	t.Cleanup(func() {
		if s != nil {
			_ = s.Close(context.Background())
		}
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	return s, a, err
}

func methods(a *Agent) []string {
	var got []string
	for _, m := range a.Requests() {
		got = append(got, m.Method)
	}
	return got
}

// TestAnUnadvertisedKeyIsNotSent: with a key present and its method not
// advertised, the start fails with ErrACPAuthRequired before authenticate,
// naming the key method and the Spec's remedy (MADR 0234 D2, D3). Before D2
// the selection sent the key method anyway, and the SDK refused it with
// ErrUnadvertisedAuthMethod.
func TestAnUnadvertisedKeyIsNotSent(t *testing.T) {
	p := acpagent.New(keySpec(), acpagent.Config{})
	_, a, err := startOn(t, p, Options{AuthMethods: []string{"vendor.browser"}})
	if !errors.Is(err, acpagent.ErrACPAuthRequired) {
		t.Fatalf("StartOn err = %v, want ErrACPAuthRequired", err)
	}
	for _, want := range []string{`"vendor.key"`, keyHint, "vendor.browser"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if got := methods(a); !slices.Equal(got, []string{"initialize"}) {
		t.Fatalf("the agent received %v, want initialize only", got)
	}
}

// TestAnAdvertisedKeyIsAuthenticated: the same Spec, with the key method
// advertised, authenticates with it and reaches session/new.
func TestAnAdvertisedKeyIsAuthenticated(t *testing.T) {
	p := acpagent.New(keySpec(), acpagent.Config{})
	_, a := Start(t, p, Options{AuthMethods: []string{"vendor.key", "vendor.browser"}})
	if got := methods(a); !slices.Equal(got, []string{"initialize", "authenticate", "session/new"}) {
		t.Fatalf("the agent received %v, want initialize, authenticate, session/new", got)
	}
	if got := a.ParamsFor("authenticate")["methodId"]; got != "vendor.key" {
		t.Fatalf("authenticate methodId = %v, want vendor.key", got)
	}
}

// upstreamSpec is a Spec whose agent advertises one method per upstream,
// whatever is stored, and whose AuthPreferenceFunc answers the upstreams
// that hold a credential (MADR 0179 D4). seen records each call's context.
func upstreamSpec(answer []string, seen *[]context.Context) acpagent.Spec {
	return acpagent.Spec{
		ID:                provider.ID("fixture"),
		SafeAuthMethodIDs: []string{"vendor.a", "vendor.b"},
		AuthPreference:    []string{"vendor.a", "vendor.b"},
		AuthPreferenceFunc: func(ctx context.Context) []string {
			*seen = append(*seen, ctx)
			return answer
		},
	}
}

// TestAuthPreferenceFuncChoosesTheUpstream: the func's answer replaces the
// fixed preference, so the handshake authenticates the upstream that has a
// credential, not the first one listed (MADR 0179 D4, G8).
func TestAuthPreferenceFuncChoosesTheUpstream(t *testing.T) {
	var seen []context.Context
	p := acpagent.New(upstreamSpec([]string{"vendor.b"}, &seen), acpagent.Config{})
	_, a := Start(t, p, Options{AuthMethods: []string{"vendor.a", "vendor.b"}})
	if got := methods(a); !slices.Equal(got, []string{"initialize", "authenticate", "session/new"}) {
		t.Fatalf("the agent received %v, want initialize, authenticate, session/new", got)
	}
	if got := a.ParamsFor("authenticate")["methodId"]; got != "vendor.b" {
		t.Fatalf("authenticate methodId = %v, want vendor.b, the func's answer", got)
	}
	if len(seen) != 1 {
		t.Fatalf("AuthPreferenceFunc called %d times, want once", len(seen))
	}
	if seen[0].Err() == nil {
		t.Fatal("the func's context is still live after Start returned; it must end with the handshake")
	}
}

// TestAnEmptyAuthPreferenceFuncRequiresAuth: no upstream with a credential
// selects nothing, and Start fails ErrACPAuthRequired with no authenticate
// and no session/new, though AuthPreference lists advertised methods.
func TestAnEmptyAuthPreferenceFuncRequiresAuth(t *testing.T) {
	var seen []context.Context
	p := acpagent.New(upstreamSpec(nil, &seen), acpagent.Config{})
	_, a, err := startOn(t, p, Options{AuthMethods: []string{"vendor.a", "vendor.b"}})
	if !errors.Is(err, acpagent.ErrACPAuthRequired) {
		t.Fatalf("StartOn err = %v, want ErrACPAuthRequired", err)
	}
	if got := methods(a); !slices.Equal(got, []string{"initialize"}) {
		t.Fatalf("the agent received %v, want initialize only", got)
	}
}

// TestAPinnedMethodWinsOverAuthPreferenceFunc: an operator's auth_method_id
// is still the first rule; the func only replaces the preference.
func TestAPinnedMethodWinsOverAuthPreferenceFunc(t *testing.T) {
	var seen []context.Context
	p := acpagent.New(upstreamSpec([]string{"vendor.b"}, &seen), acpagent.Config{AuthMethodID: "vendor.a"})
	_, a := Start(t, p, Options{AuthMethods: []string{"vendor.a", "vendor.b"}})
	if got := a.ParamsFor("authenticate")["methodId"]; got != "vendor.a" {
		t.Fatalf("authenticate methodId = %v, want the pinned vendor.a", got)
	}
}
