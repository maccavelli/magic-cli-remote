package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// envUpstreams maps an environment variable OpenCode honours to the upstream
// it authenticates. A key supplied this way is real credential state the phone
// should see, even though it never appears in auth.json.
var envUpstreams = map[string]string{
	"OPENROUTER_API_KEY": "openrouter",
	"HF_TOKEN":           "huggingface",
	"ANTHROPIC_API_KEY":  "anthropic",
	"OPENAI_API_KEY":     "openai",
	"GROQ_API_KEY":       "groq",
	"XAI_API_KEY":        "xai",
}

// AuthStatus implements [httpagent.AuthDialect] for OpenCode (MADR 0074 D3,
// D16).
//
// Two sources, in order:
//
//  1. The engine, when one is running: `GET /provider` gives the connected set
//     — including vendors keyed through the environment, which never appear in
//     auth.json — and `GET /provider/auth` gives the typed methods (ChatGPT
//     headless device sign-in, GitLab's instance URL, and so on).
//  2. auth.json plus a known env-var map, when no engine is up. Chips stay
//     accurate on a cold host; only the typed methods are missing.
//
// The MADR's original text said OpenCode exposed no auth API and that a key
// write therefore had to be a file write. That was true of the *CLI* — its
// masked prompt ignores piped stdin — but not of the engine: opencode 1.18.16
// answers the same `/provider`, `/provider/auth` and `PUT /auth/{id}` surface
// kilo does, which is what D16 now builds on.
func (d *httpDialect) AuthStatus(ctx context.Context, api httpagent.API) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
	}
	if st, ok := d.engineAuthStatus(ctx, api); ok {
		return st, nil
	}
	path, err := credstore.OpenCodeAuthPath()
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	entries, err := credstore.ReadJSONAuth(path)
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}

	seen := make(map[string]struct{}, len(entries))
	state := provider.AuthState{Upstreams: make([]provider.UpstreamAuth, 0, len(entries)+2)}
	for _, e := range entries {
		seen[e.ID] = struct{}{}
		state.Upstreams = append(state.Upstreams, provider.UpstreamAuth{
			ID:      e.ID,
			Label:   e.ID,
			Status:  provider.AuthConfigured,
			Methods: []provider.AuthMethod{apiKeyMethod(e.ID)},
		})
	}
	for env, id := range envUpstreams {
		if _, dup := seen[id]; dup {
			continue
		}
		if strings.TrimSpace(os.Getenv(env)) == "" {
			continue
		}
		seen[id] = struct{}{}
		state.Upstreams = append(state.Upstreams, provider.UpstreamAuth{
			ID:    id,
			Label: id + " (env " + env + ")",
			// Configured, but mcremote must not pretend it can rewrite the
			// daemon's own environment: the method is still a key write, which
			// lands in auth.json and takes precedence for future sessions.
			Status:  provider.AuthConfigured,
			Methods: []provider.AuthMethod{apiKeyMethod(id)},
		})
	}
	sort.Slice(state.Upstreams, func(i, j int) bool {
		return state.Upstreams[i].ID < state.Upstreams[j].ID
	})

	state.Status = provider.AuthMissing
	if len(state.Upstreams) > 0 {
		state.Status = provider.AuthConfigured
	}
	state.ActiveUpstream = d.activeUpstream()
	return state, nil
}

// engineAuthStatus builds status from a live engine. ok=false means no engine
// answered and the caller should fall back to the on-disk store.
//
// Status stays deliberately small: the connected vendors plus the ones whose
// auth needs more than a key. The other ~170 vendors are reachable through the
// on-demand catalog (D16), not through a block that rides on every
// providers.list.
func (d *httpDialect) engineAuthStatus(ctx context.Context, api httpagent.API) (provider.AuthState, bool) {
	if api == nil {
		return provider.AuthState{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, authProbeTimeout)
	defer cancel()

	// `/config/providers` rather than `/provider`: both answer "which vendors
	// are connected", but the second carries the whole models.dev snapshot
	// (4.7 MB on 1.18.16) and this runs on every providers.list. The full
	// catalog is fetched once, on demand, by AuthCatalogList.
	conn, err := d.connectedProviders(ctx, api)
	if err != nil {
		return provider.AuthState{}, false
	}
	labels := make(map[string]string, len(conn.Providers))
	connected := make(map[string]struct{}, len(conn.Providers))
	for _, v := range conn.Providers {
		labels[v.ID] = v.Name
		connected[v.ID] = struct{}{}
	}
	methods, err := httpagent.FetchAuthMethods(ctx, api)
	if err != nil {
		methods = nil
	}

	state := provider.AuthState{Upstreams: make([]provider.UpstreamAuth, 0, len(connected)+len(methods))}
	add := func(id string) {
		label := labels[id]
		if label == "" {
			label = id
		}
		up := provider.UpstreamAuth{ID: id, Label: label, Status: provider.AuthMissing}
		if m, ok := methods[id]; ok {
			up.Methods = m
		} else {
			up.Methods = []provider.AuthMethod{httpagent.DefaultAPIKeyMethod(id)}
		}
		if _, ok := connected[id]; ok {
			up.Status = provider.AuthConfigured
		}
		state.Upstreams = append(state.Upstreams, up)
	}
	seen := make(map[string]struct{}, len(connected)+len(methods))
	for id := range connected {
		seen[id] = struct{}{}
		add(id)
	}
	for id := range methods {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		add(id)
	}
	sort.Slice(state.Upstreams, func(i, j int) bool {
		return state.Upstreams[i].ID < state.Upstreams[j].ID
	})

	state.Status = provider.AuthMissing
	if len(connected) > 0 {
		state.Status = provider.AuthConfigured
	}
	state.ActiveUpstream = d.activeUpstream()
	return state, true
}

// authProbeTimeout bounds the engine reads on the providers.list path. The
// vendor catalog is a multi-megabyte body, so this is looser than a plain REST
// call but still short enough that a wedged engine cannot stall the phone's
// provider screen.
const authProbeTimeout = 20 * time.Second

// AuthCatalogList implements [httpagent.AuthCatalogDialect] (MADR 0074 D16):
// every vendor OpenCode can be pointed at — 184 on 1.18.16 — not just the ones
// already configured.
func (d *httpDialect) AuthCatalogList(ctx context.Context, api httpagent.API) (provider.AuthCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, authProbeTimeout)
	defer cancel()
	return httpagent.EngineCatalog(ctx, api)
}

// SetCredential implements [httpagent.AuthWriterDialect] (MADR 0074 D1/D16).
//
// `PUT /auth/{id} {type:"api", key}` is the same call kilo takes, and the
// engine applies it in place: a probe on 1.18.16 wrote a key for togetherai
// and saw it in `GET /provider`'s connected set without a restart. The file
// path below remains the cold-host fallback.
func (d *httpDialect) SetCredential(ctx context.Context, api httpagent.API, upstreamID, _, secret string, _ map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return errors.New("opencode set credential: upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()
	body := map[string]string{"type": "api", "key": secret}
	if err := api(ctx, "PUT", "/auth/"+url.PathEscape(upstreamID), body, nil); err != nil {
		// The error may quote a response body; the engine does not echo the
		// key back, and this wrap must not add it either.
		return fmt.Errorf("opencode set credential for %s: %w", upstreamID, err)
	}
	return nil
}

// ClearCredential implements [httpagent.AuthWriterDialect].
func (d *httpDialect) ClearCredential(ctx context.Context, api httpagent.API, upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return errors.New("opencode clear credential: upstream id required")
	}
	ctx, cancel := context.WithTimeout(ctx, authWriteTimeout)
	defer cancel()
	if err := api(ctx, "DELETE", "/auth/"+url.PathEscape(upstreamID), nil, nil); err != nil {
		return fmt.Errorf("opencode clear credential for %s: %w", upstreamID, err)
	}
	return nil
}

// authWriteTimeout bounds a credential write: one local HTTP call.
const authWriteTimeout = 20 * time.Second

// SetCredentialFile implements [httpagent.AuthFileWriterDialect] (MADR 0074 D1).
//
// A direct write is not a shortcut past the CLI here — it is the only
// mechanism. `opencode auth login -p <id> -m <method>` reaches its key prompt
// non-interactively, but that prompt is a masked TUI widget: piped stdin is
// consumed as keystrokes and nothing is written (probed 2026-08-10).
//
// The caller restarts the shared engine afterwards (D9); the engine reads
// auth.json at boot.
func (d *httpDialect) SetCredentialFile(upstreamID, _, secret string, _ map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	if strings.TrimSpace(upstreamID) == "" {
		return errors.New("opencode set credential: upstream id required")
	}
	path, err := credstore.OpenCodeAuthPath()
	if err != nil {
		return err
	}
	return credstore.MergeJSONAuth(path, upstreamID, "api", secret)
}

// ClearCredentialFile implements [httpagent.AuthFileWriterDialect].
func (d *httpDialect) ClearCredentialFile(upstreamID string) error {
	if strings.TrimSpace(upstreamID) == "" {
		return errors.New("opencode clear credential: upstream id required")
	}
	path, err := credstore.OpenCodeAuthPath()
	if err != nil {
		return err
	}
	return credstore.DeleteJSONAuth(path, upstreamID)
}

// apiKeyMethod is the single method every OpenCode upstream offers.
func apiKeyMethod(id string) provider.AuthMethod {
	return provider.AuthMethod{
		ID:    id + ":api",
		Type:  provider.AuthMethodAPIKey,
		Label: "API key",
	}
}

// activeUpstream is the provider half of the engine's default model.
func (d *httpDialect) activeUpstream() string {
	p, _ := d.fallbackModel()
	return p
}
