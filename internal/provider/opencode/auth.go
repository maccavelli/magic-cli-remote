package opencode

import (
	"context"
	"os"
	"sort"
	"strings"

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

// AuthStatus implements [httpagent.AuthDialect] for OpenCode (MADR 0074 D3).
//
// Unlike kilo, OpenCode exposes no auth catalog over HTTP, so status comes
// from its on-disk store plus the environment. That also means it needs no
// running engine — a cold host reports accurate chips.
//
// Every upstream here offers exactly one method: an API key. That is not a
// simplification but the probe result — `opencode auth login`'s masked key
// widget ignores piped stdin, so a key written to auth.json is the only
// non-interactive path (MADR 0074 D1), and OpenCode's browser OAuth flows
// cannot be driven headlessly until the loopback-tunnel workstream lands.
func (d *httpDialect) AuthStatus(ctx context.Context, _ httpagent.API) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
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
