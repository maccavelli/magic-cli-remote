package httpagent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// This file holds the parts of MADR 0074 D16 that OpenCode and Kilo share.
//
// Kilo is an OpenCode fork, so both engines answer the same two reads:
//
//	GET /provider       {all: [{id,name,env,models}], connected: [id]}
//	GET /provider/auth  {id: [{type,label,prompts[]}]}
//
// The first is the vendor catalog — 184 entries on opencode 1.18.16, 185 on
// kilo 7.4.21, including togetherai, deepseek, groq and every other plain
// API-key vendor. The second describes only the ~10-13 vendors whose auth is
// more than a key (OAuth, or a key plus account/instance fields).
//
// Before D16 the daemon showed only the second list plus whatever was already
// configured, so a phone could not add a credential for any of the ~170
// key-only vendors — the gap D16 closes.

// MaxCatalogUpstreams caps a catalog answer. The vendor list is engine-driven
// and grows with models.dev; the cap keeps one frame bounded, and Truncated
// tells the phone the list it is showing is not the whole one.
const MaxCatalogUpstreams = 512

// VendorEntry is one vendor from GET /provider's `all`.
type VendorEntry struct {
	ID   string
	Name string
	// Env are the environment variables the agent also accepts this vendor's
	// key through. Surfaced as a hint, never read for its value.
	Env []string
	// Models is how many models the vendor advertises. A vendor with none is
	// still authenticable, so this only orders and annotates.
	Models int
}

// providerCatalogResponse is GET /provider. Only the fields below are read:
// widening it would risk pulling credential material into memory, which the
// kilo dialect learned the hard way with GET /config/providers.
type providerCatalogResponse struct {
	All []struct {
		ID     string         `json:"id"`
		Name   string         `json:"name"`
		Env    []string       `json:"env"`
		Models map[string]any `json:"models"`
	} `json:"all"`
	Connected []string `json:"connected"`
}

// FetchVendorCatalog reads GET /provider.
//
// The returned connected set is authoritative in a way the on-disk store is
// not: it includes vendors authenticated through the environment, which never
// appear in auth.json (openrouter and huggingface on this host).
func FetchVendorCatalog(ctx context.Context, api API) (vendors []VendorEntry, connected map[string]struct{}, err error) {
	var resp providerCatalogResponse
	if err := api(ctx, "GET", "/provider", nil, &resp); err != nil {
		return nil, nil, fmt.Errorf("provider catalog: %w", err)
	}
	vendors = make([]VendorEntry, 0, len(resp.All))
	for _, v := range resp.All {
		id := strings.TrimSpace(v.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(v.Name)
		if name == "" {
			name = id
		}
		vendors = append(vendors, VendorEntry{ID: id, Name: name, Env: v.Env, Models: len(v.Models)})
	}
	sort.Slice(vendors, func(i, j int) bool { return vendors[i].ID < vendors[j].ID })
	connected = make(map[string]struct{}, len(resp.Connected))
	for _, id := range resp.Connected {
		if id = strings.TrimSpace(id); id != "" {
			connected[id] = struct{}{}
		}
	}
	return vendors, connected, nil
}

// authMethodsResponse is GET /provider/auth.
type authMethodsResponse map[string][]struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Prompts []struct {
		Type    string `json:"type"`
		Key     string `json:"key"`
		Message string `json:"message"`
		Options []struct {
			Value string `json:"value"`
			Label string `json:"label"`
			Hint  string `json:"hint"`
		} `json:"options"`
		Placeholder string `json:"placeholder"`
		Required    bool   `json:"required"`
		When        *struct {
			Key   string `json:"key"`
			Op    string `json:"op"`
			Value string `json:"value"`
		} `json:"when"`
	} `json:"prompts"`
}

// FetchAuthMethods reads GET /provider/auth into typed methods keyed by vendor.
//
// Method ids encode the index into the vendor's method array because that is
// how the authorize endpoint addresses a method — `POST
// /provider/{id}/oauth/authorize {method: 1}`. Reordering the array upstream
// therefore changes what a stored method id means, which is why the phone
// re-reads the catalog rather than caching ids across daemon versions.
func FetchAuthMethods(ctx context.Context, api API) (map[string][]provider.AuthMethod, error) {
	var raw authMethodsResponse
	if err := api(ctx, "GET", "/provider/auth", nil, &raw); err != nil {
		return nil, fmt.Errorf("auth method catalog: %w", err)
	}
	out := make(map[string][]provider.AuthMethod, len(raw))
	for id, methods := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		typed := make([]provider.AuthMethod, 0, len(methods))
		for i, m := range methods {
			am := provider.AuthMethod{
				ID:    fmt.Sprintf("%s:%d", id, i),
				Type:  ClassifyCatalogMethod(m.Type, m.Label),
				Label: strings.TrimSpace(m.Label),
			}
			for _, p := range m.Prompts {
				key := strings.TrimSpace(p.Key)
				if key == "" {
					continue
				}
				in := provider.AuthInput{
					Key:         key,
					Type:        provider.AuthInputText,
					Message:     strings.TrimSpace(p.Message),
					Placeholder: strings.TrimSpace(p.Placeholder),
					Required:    p.Required,
				}
				if strings.EqualFold(strings.TrimSpace(p.Type), "select") {
					in.Type = provider.AuthInputSelect
				}
				for _, o := range p.Options {
					in.Options = append(in.Options, provider.AuthInputOption{
						Value: o.Value, Label: o.Label, Hint: o.Hint,
					})
				}
				if p.When != nil && p.When.Key != "" {
					in.When = &provider.AuthInputCondition{Key: p.When.Key, Op: p.When.Op, Value: p.When.Value}
				}
				am.Inputs = append(am.Inputs, in)
			}
			if am.Label == "" {
				am.Label = am.Type
			}
			typed = append(typed, am)
		}
		if len(typed) > 0 {
			out[id] = typed
		}
	}
	return out, nil
}

// browserOAuthMarkers identify catalog entries whose OAuth completes through a
// loopback browser redirect rather than a device code.
//
// This is a catalog-time *hint*, not the D7 decision. D7 is authoritative and
// needs the authorize response's URL, which does not exist until a flow starts
// — the live probe showed these engines return method:"auto" for browser and
// device flows alike, so the catalog cannot be trusted to distinguish them.
// The hint exists only so the phone does not offer a "sign in" button that
// would fail the instant it is pressed; StartDeviceAuth re-checks by URL.
var browserOAuthMarkers = []string{"browser", "external browser", "supergrok subscription"}

// noSyntheticAPIKey is vendor ids that appear in GET /provider.all but must
// not receive DefaultAPIKeyMethod (MADR 0086 D2). OpenCode lists `kilo` in
// `all` and not in /provider/auth; a synthesised kilo:api is the 23:43 write.
var noSyntheticAPIKey = map[string]struct{}{
	"kilo": {},
}

// ClassifyCatalogMethod maps a catalog entry to a method type.
func ClassifyCatalogMethod(typ, label string) string {
	if !strings.EqualFold(strings.TrimSpace(typ), "oauth") {
		return provider.AuthMethodAPIKey
	}
	l := strings.ToLower(label)
	for _, marker := range browserOAuthMarkers {
		if strings.Contains(l, marker) {
			return provider.AuthMethodOAuthBrowser
		}
	}
	return provider.AuthMethodOAuthDevice
}

// BuildCatalog merges the vendor list with the typed-method catalog into the
// full set of configurable upstreams (MADR 0074 D16).
//
// A vendor with no entry in methods gets a plain API-key method: that is what
// `PUT /auth/{id} {type:"api", key}` does, and it is the path that makes
// togetherai, deepseek, groq and ~170 others configurable from the phone.
// Vendors present only in the method catalog (never in `all`) are kept too — a
// vendor can be authenticable without shipping a model list.
func BuildCatalog(vendors []VendorEntry, methods map[string][]provider.AuthMethod, connected map[string]struct{}) []provider.UpstreamAuth {
	out := make([]provider.UpstreamAuth, 0, len(vendors)+len(methods))
	seen := make(map[string]struct{}, len(vendors))
	for _, v := range vendors {
		seen[v.ID] = struct{}{}
		up := provider.UpstreamAuth{ID: v.ID, Label: v.Name, Status: provider.AuthMissing}
		if m, ok := methods[v.ID]; ok {
			up.Methods = m
		} else {
			up.Methods = defaultMethodsFor(v.ID)
		}
		if _, ok := connected[v.ID]; ok {
			up.Status = provider.AuthConfigured
		}
		out = append(out, up)
	}
	for id, m := range methods {
		if _, dup := seen[id]; dup {
			continue
		}
		up := provider.UpstreamAuth{ID: id, Label: id, Status: provider.AuthMissing, Methods: m}
		if _, ok := connected[id]; ok {
			up.Status = provider.AuthConfigured
		}
		out = append(out, up)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DefaultAPIKeyMethod is the method every key-only vendor offers.
func DefaultAPIKeyMethod(id string) provider.AuthMethod {
	return provider.AuthMethod{ID: id + ":api", Type: provider.AuthMethodAPIKey, Label: "API key"}
}

func defaultMethodsFor(id string) []provider.AuthMethod {
	if _, deny := noSyntheticAPIKey[id]; deny {
		return []provider.AuthMethod{{
			ID:          id + ":oauth",
			Type:        provider.AuthMethodOAuthDevice,
			Label:       "Sign in on the host",
			Unavailable: true,
			Reason:      protocol.AuthReasonHostOAuth,
		}}
	}
	return []provider.AuthMethod{DefaultAPIKeyMethod(id)}
}

// VerifyAPIKeyMethod guards a credential write against the wrong method
// (MADR 0083 D2): an engine-minted typed method id ("<upstream>:<index>")
// must resolve to an api-key method, because the write path stores an
// ApiAuth. The synthesized long-tail id ("<upstream>:api") and an empty id
// need no engine round-trip. Refusing beats writing a wrong-shaped
// credential that looks like success.
func VerifyAPIKeyMethod(ctx context.Context, api API, upstreamID, methodID string) error {
	methodID = strings.TrimSpace(methodID)
	if _, deny := noSyntheticAPIKey[upstreamID]; deny && (methodID == "" || methodID == upstreamID+":api") {
		return fmt.Errorf("method %q: %w", methodID, provider.ErrAuthMethodUnsupported)
	}
	if methodID == "" || methodID == upstreamID+":api" {
		return nil
	}
	prefix := upstreamID + ":"
	if !strings.HasPrefix(methodID, prefix) {
		return fmt.Errorf("method %q does not belong to upstream %q: %w",
			methodID, upstreamID, provider.ErrAuthMethodUnsupported)
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(methodID, prefix))
	if err != nil || idx < 0 {
		return fmt.Errorf("method %q has no valid index: %w",
			methodID, provider.ErrAuthMethodUnsupported)
	}
	methods, err := FetchAuthMethods(ctx, api)
	if err != nil {
		return fmt.Errorf("resolve method %s: %w", methodID, err)
	}
	list := methods[upstreamID]
	if idx >= len(list) {
		return fmt.Errorf("method %q is not in the engine's catalog: %w",
			methodID, provider.ErrAuthMethodUnsupported)
	}
	if t := list[idx].Type; t != provider.AuthMethodAPIKey {
		return fmt.Errorf("method %q is %s, not an API key: %w",
			methodID, t, provider.ErrAuthMethodUnsupported)
	}
	return nil
}

// APIKeyAuthBody is the engine's ApiAuth write shape (PUT /auth/{id}).
// Typed prompt answers ride in `metadata` — the field the engine's OpenAPI
// declares for them (MADR 0083 G1); before this the daemon dropped them on
// the floor and azure/gitlab/copilot set-ups silently mis-stored.
func APIKeyAuthBody(secret string, inputs map[string]string) map[string]any {
	body := map[string]any{"type": "api", "key": secret}
	if len(inputs) > 0 {
		body["metadata"] = inputs
	}
	return body
}

// AuthCatalogDialect is optionally implemented by a [Dialect] whose engine can
// enumerate every upstream it supports (MADR 0074 D16).
type AuthCatalogDialect interface {
	AuthCatalogList(ctx context.Context, api API) (provider.AuthCatalog, error)
}

// catalogTTL is how long a fetched vendor catalog is reused.
//
// It exists because the phone pages: `GET /provider` is 4.7 MB on opencode
// 1.18.16, and re-fetching it for every 100-row page — or for every keystroke
// in the search field — would be absurd. The list itself changes only when the
// agent ships a new models.dev snapshot, so minutes of staleness cost nothing;
// per-vendor *status* inside it is refreshed by invalidating on write.
const catalogTTL = 5 * time.Minute

// AuthCatalogList implements [provider.AuthCataloger] by delegating to the
// dialect. Unlike AuthStatus this always needs an engine: the catalog lives in
// the engine's models.dev snapshot, and no on-disk file holds it.
func (p *Provider) AuthCatalogList(ctx context.Context) (provider.AuthCatalog, error) {
	d, ok := p.dialect.(AuthCatalogDialect)
	if !ok {
		return provider.AuthCatalog{}, provider.ErrAuthUnsupported
	}
	if cat, ok := p.cachedCatalog(); ok {
		return cat, nil
	}
	if _, err := p.ensureServer(ctx); err != nil {
		return provider.AuthCatalog{}, err
	}
	cat, err := d.AuthCatalogList(ctx, p.api)
	if err != nil {
		return provider.AuthCatalog{}, err
	}
	p.storeCatalog(cat)
	return cat, nil
}

func (p *Provider) cachedCatalog() (provider.AuthCatalog, bool) {
	p.authCatalogMu.Lock()
	defer p.authCatalogMu.Unlock()
	if p.authCatalog == nil || time.Now().After(p.authCatalogExpiry) {
		return provider.AuthCatalog{}, false
	}
	return *p.authCatalog, true
}

func (p *Provider) storeCatalog(cat provider.AuthCatalog) {
	p.authCatalogMu.Lock()
	defer p.authCatalogMu.Unlock()
	p.authCatalog = &cat
	p.authCatalogExpiry = time.Now().Add(catalogTTL)
}

// InvalidateAuthCatalog drops the cached catalog so the next read reflects a
// credential that was just written or cleared.
func (p *Provider) InvalidateAuthCatalog() {
	p.authCatalogMu.Lock()
	defer p.authCatalogMu.Unlock()
	p.authCatalog = nil
}

// EngineCatalog is the shared implementation both dialects use: read both
// endpoints, merge, and report the source as live.
func EngineCatalog(ctx context.Context, api API) (provider.AuthCatalog, error) {
	vendors, connected, err := FetchVendorCatalog(ctx, api)
	if err != nil {
		return provider.AuthCatalog{}, err
	}
	// A missing method catalog degrades to key-only rather than failing: every
	// vendor still gets its API-key method, which is the common case anyway.
	methods, err := FetchAuthMethods(ctx, api)
	if err != nil {
		methods = nil
	}
	return provider.AuthCatalog{
		Upstreams: BuildCatalog(vendors, methods, connected),
		Source:    provider.AuthCatalogSourceEngine,
	}, nil
}
