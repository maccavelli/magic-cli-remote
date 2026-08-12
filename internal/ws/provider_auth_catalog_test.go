package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// catalogProvider stands in for an engine-backed agent: it reports two
// configured upstreams and a catalog of many more (MADR 0074 D16).
type catalogProvider struct {
	id      provider.ID
	catalog provider.AuthCatalog
	err     error
	calls   int
}

func (p *catalogProvider) ID() provider.ID { return p.id }
func (p *catalogProvider) Ready() bool     { return true }
func (p *catalogProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	return nil, errors.New("not used")
}

func (p *catalogProvider) AuthStatus(context.Context) (provider.AuthState, error) {
	return provider.AuthState{
		Status:         provider.AuthConfigured,
		ActiveUpstream: "opencode-go",
		Upstreams: []provider.UpstreamAuth{{
			ID: "opencode-go", Label: "OpenCode Go", Status: provider.AuthConfigured,
		}},
	}, nil
}

func (p *catalogProvider) AuthCatalogList(context.Context) (provider.AuthCatalog, error) {
	p.calls++
	if p.err != nil {
		return provider.AuthCatalog{}, p.err
	}
	return p.catalog, nil
}

func bigCatalog(n int) provider.AuthCatalog {
	ups := make([]provider.UpstreamAuth, 0, n+2)
	ups = append(ups,
		provider.UpstreamAuth{ID: "togetherai", Label: "Together AI", Status: provider.AuthMissing,
			Methods: []provider.AuthMethod{{ID: "togetherai:api", Type: provider.AuthMethodAPIKey, Label: "API key"}}},
		provider.UpstreamAuth{ID: "opencode-go", Label: "OpenCode Go", Status: provider.AuthConfigured,
			Methods: []provider.AuthMethod{{ID: "opencode-go:api", Type: provider.AuthMethodAPIKey, Label: "API key"}}},
	)
	for i := 0; i < n; i++ {
		ups = append(ups, provider.UpstreamAuth{
			ID: fmt.Sprintf("vendor-%03d", i), Label: fmt.Sprintf("Vendor %03d", i),
			Status:  provider.AuthMissing,
			Methods: []provider.AuthMethod{{ID: "x:api", Type: provider.AuthMethodAPIKey, Label: "API key"}},
		})
	}
	return provider.AuthCatalog{Upstreams: ups, Source: provider.AuthCatalogSourceEngine}
}

func requestCatalog(t *testing.T, w *authWS, providerID, query string) protocol.AuthCatalogPayload {
	t.Helper()
	return requestCatalogPage(t, w, providerID, query, 0, 0)
}

func requestCatalogPage(t *testing.T, w *authWS, providerID, query string, offset, limit int) protocol.AuthCatalogPayload {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProviderAuthCatalog, "cat",
		protocol.AuthCatalogRequestPayload{
			ProviderID: providerID, Query: query, Offset: offset, Limit: limit,
		})
	env.V = protocol.V2
	writeEnv(ctx, t, w.conn, env)
	for {
		got := readEnv(ctx, t, w.conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeProviderAuthCatalogRes {
			t.Fatalf("want provider.auth_catalog_result, got %s %s", got.Type, got.Payload)
		}
		var res protocol.AuthCatalogPayload
		if err := json.Unmarshal(got.Payload, &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
}

// The catalog carries every vendor, including ones with no credential — that
// is the whole difference from the status block.
func TestAuthCatalogReturnsUnconfiguredVendors(t *testing.T) {
	p := &catalogProvider{id: "opencode", catalog: bigCatalog(20)}
	w, _ := startAuthServer(t, []int{protocol.V2}, p)
	defer w.close()

	res := requestCatalog(t, w, "opencode", "")
	if res.ProviderID != "opencode" {
		t.Errorf("provider_id = %q", res.ProviderID)
	}
	if res.Source != protocol.AuthCatalogSourceEngine {
		t.Errorf("source = %q, want engine", res.Source)
	}
	var found bool
	for _, up := range res.Upstreams {
		if up.ID == "togetherai" && up.Status == protocol.AuthStatusMissing {
			found = true
		}
	}
	if !found {
		t.Fatal("catalog did not carry the unconfigured vendor")
	}
	if res.Total != 22 {
		t.Errorf("total = %d, want 22", res.Total)
	}
}

// Searching narrows server-side: a phone typing "together" must not have to
// pull 185 rows to show three.
func TestAuthCatalogQueryFiltersByIDAndLabel(t *testing.T) {
	p := &catalogProvider{id: "opencode", catalog: bigCatalog(20)}
	w, _ := startAuthServer(t, []int{protocol.V2}, p)
	defer w.close()

	res := requestCatalog(t, w, "opencode", "together")
	if len(res.Upstreams) != 1 || res.Upstreams[0].ID != "togetherai" {
		t.Fatalf("upstreams = %+v, want just togetherai", res.Upstreams)
	}
	// Label match, different case.
	res = requestCatalog(t, w, "opencode", "VENDOR 007")
	if len(res.Upstreams) != 1 || res.Upstreams[0].ID != "vendor-007" {
		t.Fatalf("upstreams = %+v, want vendor-007 by label", res.Upstreams)
	}
}

// A page that is not the last must say so, and the next page must continue
// where it left off. A silently short list reads as "this agent does not
// support that vendor".
func TestAuthCatalogPagesLargeCatalogs(t *testing.T) {
	p := &catalogProvider{id: "opencode", catalog: bigCatalog(250)}
	w, _ := startAuthServer(t, []int{protocol.V2}, p)
	defer w.close()

	first := requestCatalogPage(t, w, "opencode", "", 0, 0)
	if !first.Truncated {
		t.Fatal("first page of a 252-entry catalog was not flagged truncated")
	}
	if len(first.Upstreams) != 100 {
		t.Fatalf("page size = %d, want the 100 default", len(first.Upstreams))
	}
	if first.Total != 252 {
		t.Errorf("total = %d, want 252", first.Total)
	}

	second := requestCatalogPage(t, w, "opencode", "", 100, 100)
	if second.Offset != 100 {
		t.Errorf("offset = %d, want 100", second.Offset)
	}
	if len(second.Upstreams) == 0 || second.Upstreams[0].ID == first.Upstreams[0].ID {
		t.Fatal("second page repeated the first")
	}

	last := requestCatalogPage(t, w, "opencode", "", 200, 200)
	if last.Truncated {
		t.Error("final page still flagged truncated")
	}
}

// An oversized page request is clamped rather than honoured: the cap exists to
// bound the frame, so a client must not be able to ask past it.
func TestAuthCatalogClampsPageSize(t *testing.T) {
	p := &catalogProvider{id: "opencode", catalog: bigCatalog(500)}
	w, _ := startAuthServer(t, []int{protocol.V2}, p)
	defer w.close()

	res := requestCatalogPage(t, w, "opencode", "", 0, 5000)
	if len(res.Upstreams) != 200 {
		t.Fatalf("page size = %d, want the 200 cap", len(res.Upstreams))
	}
}

// D6 again: a client that never negotiated the capability gets an error, not a
// catalog.
func TestAuthCatalogRefusedWithoutCapability(t *testing.T) {
	p := &catalogProvider{id: "opencode", catalog: bigCatalog(3)}
	w, _ := startAuthServer(t, nil, p)
	defer w.close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProviderAuthCatalog, "cat",
		protocol.AuthCatalogRequestPayload{ProviderID: "opencode"})
	writeEnv(ctx, t, w.conn, env)
	got := readEnv(ctx, t, w.conn)
	if got.Type != protocol.TypeError {
		t.Fatalf("want error, got %s %s", got.Type, got.Payload)
	}
	if !strings.Contains(string(got.Payload), "unsupported") {
		t.Fatalf("payload = %s, want unsupported", got.Payload)
	}
}

// An agent with no catalog (codex, grok) answers cleanly rather than failing
// the connection.
func TestAuthCatalogUnsupportedProviderIsAnError(t *testing.T) {
	p := &authProbeProvider{id: "codex", state: sampleState()}
	w, _ := startAuthServer(t, []int{protocol.V2}, p)
	defer w.close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	env, _ := protocol.NewEnvelope(protocol.TypeProviderAuthCatalog, "cat",
		protocol.AuthCatalogRequestPayload{ProviderID: "codex"})
	env.V = protocol.V2
	writeEnv(ctx, t, w.conn, env)
	for {
		got := readEnv(ctx, t, w.conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s %s", got.Type, got.Payload)
		}
		return
	}
}
