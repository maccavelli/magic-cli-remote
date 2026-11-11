package httpagent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

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
// The engine's catalog gives a method no id of its own: the authorize endpoint
// addresses it by its index in the vendor's array — `POST
// /provider/{id}/oauth/authorize {method: 1}`. Magic mints the id
// `<upstream>:<label slug>` instead of `<upstream>:<index>` (MADR 0074 D31),
// so a catalog reordered between listing and start still starts the method
// the phone chose: ResolveEngineMethod re-reads the catalog at start and
// resolves the slug to its index then. The phone re-reads the catalog when it
// opens the sheet rather than caching ids across daemon versions.
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
		for _, m := range methods {
			am := provider.AuthMethod{
				ID:    id + ":" + methodSlug(m.Type, m.Label),
				Type:  ClassifyCatalogMethod(m.Type),
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

// methodSlug mints the id half of a catalog method (MADR 0074 D31): the label
// lower-cased, every run of characters other than letters and digits made one
// `-`, with no leading or trailing `-`. A label with nothing in it falls back
// to the method's type, so an entry is never addressed as "<upstream>:".
//
// The slug is derived from the label and not from the array index because
// the index is what the engine may reorder between the phone's listing and
// its start; the label is what the user chose.
func methodSlug(typ, label string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		return strings.ToLower(strings.TrimSpace(typ))
	}
	return b.String()
}

// noSyntheticAPIKey is vendor ids that appear in GET /provider.all but must
// not receive DefaultAPIKeyMethod (MADR 0086 D2). OpenCode lists `kilo` in
// `all` and not in /provider/auth; a synthesised kilo:api is the 23:43 write.
var noSyntheticAPIKey = map[string]struct{}{
	"kilo": {},
}

// ClassifyCatalogMethod maps a catalog entry's type to a method type.
//
// Every `oauth` entry is listed oauth_device and startable (MADR 0074 D30).
// No label decides the flow: the live probe showed these engines return
// method:"auto" for browser and device flows alike, and the only honest
// discriminator is the authorize response's URL, which does not exist until a
// flow starts. D7's URL rule decides then, and StartEngineDeviceFlow refuses a
// loopback URL with "signs in through a browser on the host". The label
// heuristic this replaced ("browser", "supergrok subscription") marked those
// entries oauth_browser so the phone never offered them; it also marked
// entries by their wording alone, which is not a fact about the flow.
func ClassifyCatalogMethod(typ string) string {
	if !strings.EqualFold(strings.TrimSpace(typ), "oauth") {
		return provider.AuthMethodAPIKey
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
			// protocol.AuthReasonHostOAuth — not imported here: protocol
			// imports session, and session tests import this package.
			Reason: "host_oauth",
		}}
	}
	return []provider.AuthMethod{DefaultAPIKeyMethod(id)}
}

// VerifyAPIKeyMethod guards a credential write against the wrong method
// (MADR 0083 D2): a catalog-minted method id ("<upstream>:<label slug>")
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
	methods, err := FetchAuthMethods(ctx, api)
	if err != nil {
		return fmt.Errorf("resolve method %s: %w", methodID, err)
	}
	idx, err := resolveMethod(methods[upstreamID], upstreamID, methodID)
	if err != nil {
		return err
	}
	if t := methods[upstreamID][idx].Type; t != provider.AuthMethodAPIKey {
		return fmt.Errorf("method %q is %s, not an API key: %w",
			methodID, t, provider.ErrAuthMethodUnsupported)
	}
	return nil
}

// resolveMethod finds the catalog index of a minted method id among one
// upstream's methods, as the catalog stands now (MADR 0074 D31). A slug that
// matches no method, or more than one, is refused as unsupported: starting
// "the first match" would start a method the user did not choose. An index id
// ("<upstream>:1", the pre-D31 shape) matches no slug and is refused the same
// way.
func resolveMethod(list []provider.AuthMethod, upstreamID, methodID string) (int, error) {
	if !strings.HasPrefix(methodID, upstreamID+":") {
		return 0, fmt.Errorf("method %q does not belong to upstream %q: %w",
			methodID, upstreamID, provider.ErrAuthMethodUnsupported)
	}
	found := -1
	for i, m := range list {
		if m.ID != methodID {
			continue
		}
		if found >= 0 {
			return 0, fmt.Errorf("method %q matches more than one of the engine's methods: %w",
				methodID, provider.ErrAuthMethodUnsupported)
		}
		found = i
	}
	if found < 0 {
		return 0, fmt.Errorf("method %q is not in the engine's catalog: %w",
			methodID, provider.ErrAuthMethodUnsupported)
	}
	return found, nil
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
