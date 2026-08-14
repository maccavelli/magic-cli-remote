package goose

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// AuthStatus implements [provider.Auth] for Goose (MADR 0074 D3).
//
// Goose keeps secrets in the OS keyring and per-provider token files, neither
// of which mcremote reads. What config.yaml does give — the configured
// provider set and which one is active — is exactly what the MADR 0073 hang
// needed: the phone could not see that four other upstreams were already
// authenticated and ready to take over from a quota-blocked one.
//
// Every configured upstream is reported as configured. That is an assumption,
// not an observation: goose writes a provider into config.yaml as part of
// authenticating it, but a revoked token still looks configured here. Auth
// failures surface separately through agenterr on the turn itself.
func authStatus(ctx context.Context) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
	}
	path, err := credstore.GooseConfigPath()
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	configured := configuredUpstreams(cfg)
	keyringManaged := !credstore.GooseKeyringDisabled(path)
	state := provider.AuthState{
		ActiveUpstream: cfg.ActiveProvider,
		Upstreams:      make([]provider.UpstreamAuth, 0, len(configured)),
	}
	for _, id := range configured {
		def, known := catalogByID[id]
		if !known {
			def = upstreamDef{ID: id, Label: id, SecretKey: ""}
		}
		methods := []provider.AuthMethod{{
			ID:    id + ":api",
			Type:  provider.AuthMethodAPIKey,
			Label: "API key",
		}}
		if known {
			methods = catalogMethods(def)
		}
		if keyringManaged {
			methods = markKeyringManaged(methods)
		}
		state.Upstreams = append(state.Upstreams, provider.UpstreamAuth{
			ID:      id,
			Label:   upstreamLabel(id),
			Status:  provider.AuthConfigured,
			Methods: methods,
		})
	}
	state.Status = provider.AuthMissing
	if len(state.Upstreams) > 0 {
		state.Status = provider.AuthConfigured
	}
	return state, nil
}

// configuredUpstreams is every provider goose has state for: the ones named in
// config.yaml, plus any vendor whose secret sits in goose's file store.
//
// The second source matters because config.yaml only lists providers goose's
// own configure flow has touched. A key mcremote wrote (D18) shows up as a
// secret first, and a credential the phone just set must not read back as
// "needs setup".
func configuredUpstreams(cfg credstore.GooseConfig) []string {
	seen := make(map[string]struct{}, len(cfg.Providers))
	out := make([]string, 0, len(cfg.Providers))
	for _, id := range cfg.Providers {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range upstreamsWithStoredSecret() {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// upstreamsWithStoredSecret maps goose's file-store secret names back to the
// vendors they authenticate. Only names are read, never values (D2).
func upstreamsWithStoredSecret() []string {
	path, err := credstore.GooseSecretsPath()
	if err != nil {
		return nil
	}
	names, err := credstore.ReadGooseSecretNames(path)
	if err != nil {
		return nil
	}
	byKey := make(map[string]string, len(gooseUpstreams))
	for _, u := range gooseUpstreams {
		if u.SecretKey != "" {
			byKey[u.SecretKey] = u.ID
		}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if id, ok := byKey[n]; ok {
			out = append(out, id)
		}
	}
	return out
}

// authCatalogList implements [provider.AuthCataloger] for goose (MADR 0074
// D16): all 73 vendors in goose's registry, not just the handful config.yaml
// happens to mention.
func authCatalogList(ctx context.Context) (provider.AuthCatalog, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthCatalog{}, err
	}
	path, err := credstore.GooseConfigPath()
	if err != nil {
		return provider.AuthCatalog{}, err
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		return provider.AuthCatalog{}, err
	}
	configured := make(map[string]struct{})
	for _, id := range configuredUpstreams(cfg) {
		configured[id] = struct{}{}
	}
	keyringManaged := !credstore.GooseKeyringDisabled(path)
	return authCatalog(configured, keyringManaged), nil
}

// setCredential stores a vendor key in goose's own secret store (MADR 0074
// D1/D18).
//
// Two refusals here are deliberate, and both prevent a write that would look
// like success and do nothing:
//
//   - a vendor with no key at all (ChatGPT Codex, Gemini OAuth, …) — those come
//     from another CLI's session, and there is nothing to store;
//   - a host whose goose reads the OS keyring rather than the file store.
func setCredential(ctx context.Context, upstreamID, methodID, secret string, _ map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	// goose advertises exactly one storable method per vendor (MADR 0083 D2):
	// a foreign or non-key method id must refuse, not silently write.
	if m := strings.TrimSpace(methodID); m != "" && m != upstreamID+":api" {
		return fmt.Errorf("goose method %q: %w", m, provider.ErrAuthMethodUnsupported)
	}
	key, ok := secretKeyFor(upstreamID)
	if !ok {
		return fmt.Errorf("goose upstream %q takes no API key; configure it on the host", upstreamID)
	}
	secretsPath, cfgPath, err := goosePaths()
	if err != nil {
		return err
	}
	if !credstore.GooseKeyringDisabled(cfgPath) {
		return credstore.ErrGooseKeyringManaged
	}
	if err := credstore.SetGooseSecret(secretsPath, key, secret); err != nil {
		return err
	}
	names, err := credstore.ReadGooseSecretNames(secretsPath)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == key {
			return nil
		}
	}
	_ = credstore.DeleteGooseSecret(secretsPath, key)
	return fmt.Errorf("goose: %w", provider.ErrCredentialNotAccepted)
}

// clearCredential removes a vendor key from goose's file store.
func clearCredential(ctx context.Context, upstreamID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, ok := secretKeyFor(upstreamID)
	if !ok {
		return fmt.Errorf("goose upstream %q has no stored API key", upstreamID)
	}
	secretsPath, cfgPath, err := goosePaths()
	if err != nil {
		return err
	}
	if !credstore.GooseKeyringDisabled(cfgPath) {
		return credstore.ErrGooseKeyringManaged
	}
	return credstore.DeleteGooseSecret(secretsPath, key)
}

// goosePaths resolves the two files goose's credential state lives in.
func goosePaths() (secrets, config string, err error) {
	secrets, err = credstore.GooseSecretsPath()
	if err != nil {
		return "", "", err
	}
	config, err = credstore.GooseConfigPath()
	if err != nil {
		return "", "", err
	}
	return secrets, config, nil
}

// setActiveUpstream repoints goose at another already-configured provider
// (MADR 0074 D14). This is the operational fix for MADR 0073: when the active
// upstream hits its weekly quota, the phone can move to one of the others
// without any credential work and without SSH.
//
// `goose configure` takes no flags at all, so rewriting the scalar is the only
// non-interactive path (probed 2026-08-10).
func setActiveUpstream(ctx context.Context, upstreamID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := credstore.GooseConfigPath()
	if err != nil {
		return err
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		return err
	}
	// Refuse a switch to an upstream goose has no configuration for: it would
	// look like it worked and then fail every turn. A vendor whose key
	// mcremote just wrote counts as configured even before goose's own
	// configure flow has ever named it (D18).
	known := false
	for _, id := range configuredUpstreams(cfg) {
		if id == upstreamID {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("goose has no configured provider %q", upstreamID)
	}
	return credstore.SetGooseActiveProvider(path, upstreamID)
}
