package kilo

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// authProbeTimeout bounds the whole status probe. providers.list waits on it,
// so a wedged engine must degrade to a partial answer rather than stall the
// phone's provider screen (same budget as Diagnostics).
const authProbeTimeout = 15 * time.Second

// maxAuthUpstreams caps how many upstreams reach the phone. Kilo advertises 13
// today, but the catalog is engine-driven and a fork could grow it without
// warning; the frame budget is not unlimited.
const maxAuthUpstreams = 64

// AuthCatalogList implements [httpagent.AuthCatalogDialect] (MADR 0074 D16).
//
// Status reports what is configured; this reports what *can* be configured —
// 185 vendors on kilo 7.4.21 against the 13 in `GET /provider/auth`. Without
// it the phone can only ever re-key a vendor kilo already knows about, which
// left togetherai, deepseek, groq and ~170 others reachable only over SSH.
func (d *httpDialect) AuthCatalogList(ctx context.Context, api httpagent.API) (provider.AuthCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, authProbeTimeout)
	defer cancel()
	return httpagent.EngineCatalog(ctx, api)
}

// AuthStatus implements [httpagent.AuthDialect] (MADR 0074 D3).
//
// Three engine reads compose the picture, each optional:
//   - GET /provider/auth   — the method catalog (types, labels, prompt inputs)
//   - GET /config/providers — which upstreams actually have a credential
//   - GET /kilo/auth-status — the Kilo Gateway session
//
// When no engine is running every one of those fails; the on-disk auth.json
// still answers "which upstreams are configured", which is the question the
// provider list most needs. That fallback is why a cold host still shows
// useful chips instead of a row of unknowns.
func (d *httpDialect) AuthStatus(ctx context.Context, api httpagent.API) (provider.AuthState, error) {
	ctx, cancel := context.WithTimeout(ctx, authProbeTimeout)
	defer cancel()

	catalog := d.fetchAuthCatalog(ctx, api)
	configured, live := d.fetchConfiguredUpstreams(ctx, api)
	if !live {
		// Engine down: fall back to the store the engine itself would read.
		configured = d.configuredFromDisk()
	}

	state := provider.AuthState{Upstreams: make([]provider.UpstreamAuth, 0, len(catalog))}
	seen := make(map[string]struct{}, len(catalog))
	for _, up := range catalog {
		seen[up.ID] = struct{}{}
		if _, ok := configured[up.ID]; ok {
			up.Status = provider.AuthConfigured
		} else {
			up.Status = provider.AuthMissing
		}
		state.Upstreams = append(state.Upstreams, up)
	}
	// A credential can exist for an upstream the catalog does not list (an
	// env-provided key, or a provider retired from the catalog). Report it:
	// hiding a live credential is worse than showing one we cannot re-auth.
	for id := range configured {
		if _, ok := seen[id]; ok {
			continue
		}
		state.Upstreams = append(state.Upstreams, provider.UpstreamAuth{
			ID:     id,
			Label:  id,
			Status: provider.AuthConfigured,
		})
	}
	sort.Slice(state.Upstreams, func(i, j int) bool {
		return state.Upstreams[i].ID < state.Upstreams[j].ID
	})
	if len(state.Upstreams) > maxAuthUpstreams {
		state.Upstreams = state.Upstreams[:maxAuthUpstreams]
	}

	state.ActiveUpstream = d.activeUpstream()
	state.Status = provider.AuthMissing
	if len(configured) > 0 {
		state.Status = provider.AuthConfigured
	}
	if len(catalog) == 0 && !live {
		// Nothing answered and nothing on disk: say so rather than claiming
		// the host is unconfigured.
		if len(configured) == 0 {
			state.Status = provider.AuthError
		}
	}
	return state, nil
}

// SetCredential implements [httpagent.AuthWriterDialect] (MADR 0074 D1).
//
// Kilo prefers PUT /auth/{id}. A file merge exists as AuthFileWriterDialect
// when the engine cannot start (MADR 0086 D9). The write is verified before
// the phone is told it succeeded (D1).
func (d *httpDialect) SetCredential(ctx context.Context, api httpagent.API, upstreamID, methodID, secret string, inputs map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("kilo set credential: upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()
	if err := httpagent.VerifyAPIKeyMethod(ctx, api, upstreamID, methodID); err != nil {
		return err
	}
	body := httpagent.APIKeyAuthBody(secret, inputs)
	if err := api(ctx, "PUT", "/auth/"+url.PathEscape(upstreamID), body, nil); err != nil {
		// The error may quote a response body; it must never quote the key,
		// and the engine does not echo it back. Wrap with the upstream only.
		return fmt.Errorf("kilo set credential for %s: %w", upstreamID, err)
	}
	return nil
}

// ClearCredential implements [httpagent.AuthWriterDialect].
func (d *httpDialect) ClearCredential(ctx context.Context, api httpagent.API, upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("kilo clear credential: upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()
	if err := api(ctx, "DELETE", "/auth/"+url.PathEscape(upstreamID), nil, nil); err != nil {
		return fmt.Errorf("kilo clear credential for %s: %w", upstreamID, err)
	}
	return nil
}

// authWriteTimeout bounds a credential write. Shorter than the status probe:
// this is a single local HTTP call with no catalog to build.
const authWriteTimeout = 20 * time.Second

// SetCredentialFile implements [httpagent.AuthFileWriterDialect] (MADR 0086 D9).
// Used only when the engine cannot start; the caller restarts or boots it
// and then verifies connectedness (D1).
func (d *httpDialect) SetCredentialFile(upstreamID, methodID, secret string, inputs map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("kilo set credential: upstream id required")
	}
	if m := strings.TrimSpace(methodID); m != "" && !strings.HasPrefix(m, upstreamID+":") {
		return fmt.Errorf("method %q does not belong to upstream %q: %w",
			m, upstreamID, provider.ErrAuthMethodUnsupported)
	}
	path, err := credstore.KiloAuthPath()
	if err != nil {
		return err
	}
	return credstore.MergeJSONAuthMetadata(path, upstreamID, "api", secret, inputs)
}

// ClearCredentialFile implements [httpagent.AuthFileWriterDialect].
func (d *httpDialect) ClearCredentialFile(upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("kilo clear credential: upstream id required")
	}
	path, err := credstore.KiloAuthPath()
	if err != nil {
		return err
	}
	return credstore.DeleteJSONAuth(path, upstreamID)
}

// fetchAuthCatalog reads GET /provider/auth into typed methods. Failure yields
// an empty catalog, never an error: the configured-set half of the picture is
// still worth showing.
//
// Parsing lives in httpagent because kilo is an OpenCode fork and both engines
// answer this endpoint identically; keeping one parser means a shape change
// breaks one place (MADR 0074 D16).
func (d *httpDialect) fetchAuthCatalog(ctx context.Context, api httpagent.API) []provider.UpstreamAuth {
	methods, err := httpagent.FetchAuthMethods(ctx, api)
	if err != nil {
		d.log.Debug("kilo auth catalog unavailable", slog.String("err", err.Error()))
		return nil
	}
	out := make([]provider.UpstreamAuth, 0, len(methods))
	for id, m := range methods {
		out = append(out, provider.UpstreamAuth{ID: id, Label: id, Methods: m})
	}
	return out
}

// fetchConfiguredUpstreams reports which upstreams have a credential, and
// whether the engine answered at all.
//
// SECURITY: GET /config/providers includes the plaintext key for every
// connected provider. Only the id is read out of it here; the response is
// never stored, logged, or forwarded. Never widen this struct to include
// `key` (there is a regression test for exactly that in catalog_test.go).
func (d *httpDialect) fetchConfiguredUpstreams(ctx context.Context, api httpagent.API) (map[string]struct{}, bool) {
	var resp struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := api(ctx, "GET", "/config/providers", nil, &resp); err != nil {
		d.log.Debug("kilo connected providers unavailable", slog.String("err", err.Error()))
		return nil, false
	}
	out := make(map[string]struct{}, len(resp.Providers))
	for _, p := range resp.Providers {
		if id := strings.TrimSpace(p.ID); id != "" {
			out[id] = struct{}{}
		}
	}
	return out, true
}

// configuredFromDisk reads kilo's own auth.json when the engine is not up.
func (d *httpDialect) configuredFromDisk() map[string]struct{} {
	path, err := credstore.KiloAuthPath()
	if err != nil {
		return nil
	}
	entries, err := credstore.ReadJSONAuth(path)
	if err != nil {
		d.log.Debug("kilo auth.json unreadable", slog.String("err", err.Error()))
		return nil
	}
	out := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		out[e.ID] = struct{}{}
	}
	return out
}

// activeUpstream reports the provider half of the engine's resolved default
// model — the upstream a new session actually bills against.
func (d *httpDialect) activeUpstream() string {
	p, _ := d.fallbackModel()
	return p
}
