// Package credstore reads and writes the agent CLIs' own credential stores.
//
// MADR 0074 D1/D2: mcremote never invents a credential vault. Every secret it
// accepts from the phone lands in the store the agent itself reads, in that
// agent's own format. This package is the single place that knows those
// formats, so a vendor format change breaks one package with live-pinned tests
// (D15) rather than leaking across the daemon.
//
// Two probes decided the shape of this package (MADR 0074 §5):
//   - `opencode auth login` reaches its key prompt non-interactively but its
//     masked TUI widget ignores piped stdin and writes nothing.
//   - `goose configure` has no non-interactive flags at all.
//
// So for those two agents a direct file write is not a shortcut around the CLI;
// it is the only mechanism that exists.
//
// Read helpers in this package return provider ids, labels and presence only —
// never key material.
package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one credential slot in an agent's store, without its secret.
type Entry struct {
	// ID is the agent's own provider id ("opencode-go", "anthropic").
	ID string
	// Type is the agent's own credential kind ("api", "oauth", "wellknown").
	Type string
}

// Home returns the user's home directory. Split out so tests can redirect the
// whole package with t.Setenv("HOME", …).
func Home() (string, error) {
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// xdg returns $<env> when set, else <home>/<fallback>. The agent stores follow
// the XDG spec for their own product, which is not mcremote's layout, so this
// is deliberately separate from internal/appdirs.
func xdg(env, fallback string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}

// OpenCodeAuthPath is ~/.local/share/opencode/auth.json (XDG_DATA_HOME aware).
func OpenCodeAuthPath() (string, error) {
	base, err := xdg("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opencode", "auth.json"), nil
}

// KiloAuthPath is ~/.local/share/kilo/auth.json. Only a fallback: the live
// engine's HTTP API is the supported write path for kilo (MADR 0074 D1).
func KiloAuthPath() (string, error) {
	base, err := xdg("XDG_DATA_HOME", filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "kilo", "auth.json"), nil
}

// GooseConfigPath is ~/.config/goose/config.yaml (XDG_CONFIG_HOME aware).
func GooseConfigPath() (string, error) {
	base, err := xdg("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "goose", "config.yaml"), nil
}

// GooseSecretsPath is ~/.config/goose/secrets.yaml — goose's file secret
// store, used whenever its keyring is disabled or unreachable.
func GooseSecretsPath() (string, error) {
	base, err := xdg("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "goose", "secrets.yaml"), nil
}

// GooseKeyringDisabled reports whether goose reads secrets from
// GooseSecretsPath rather than the OS keyring (MADR 0074 D18).
//
// Goose decides this two ways, and both are honoured here: the
// GOOSE_DISABLE_KEYRING environment variable, and the same key in config.yaml.
// It also flips to file storage at runtime when a keyring operation fails with
// an availability error — the headless case — but that decision lives inside a
// goose process and is not observable from here, so it is not inferred.
func GooseKeyringDisabled(configPath string) bool {
	if v := strings.TrimSpace(os.Getenv("GOOSE_DISABLE_KEYRING")); v != "" {
		return !isFalsey(v)
	}
	b, err := os.ReadFile(configPath) //nolint:gosec // fixed store location
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != strings.TrimLeft(line, " \t") {
			continue // nested: not the top-level flag
		}
		k, v, ok := splitYAMLScalar(trimmed)
		if !ok || k != "GOOSE_DISABLE_KEYRING" {
			continue
		}
		return v != "" && !isFalsey(v)
	}
	return false
}

// isFalsey treats the YAML/env spellings of "off" as off. Anything else that is
// set at all means on, matching goose's own env check (presence is enough).
func isFalsey(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// GrokAuthPath is ~/.grok/auth.json — the OAuth session, not a key store.
func GrokAuthPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok", "auth.json"), nil
}

// GrokConfigPath is ~/.grok/config.toml, where a quoted
// [model."<id>"] api_key lives (MADR 0085 D4).
func GrokConfigPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok", "config.toml"), nil
}

// CodexAuthPath is ~/.codex/auth.json.
func CodexAuthPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// ReadJSONAuth parses an OpenCode/Kilo-style auth.json — a flat object of
// provider id → {type, key, …} — and returns the ids and types only. The key
// is deliberately dropped at the parse boundary so no caller can leak what it
// never received (D2).
//
// A missing file is not an error: it means no credentials, which is a normal
// state on a cold host.
func ReadJSONAuth(path string) ([]Entry, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path is one of this package's fixed store locations
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var raw map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	out := make([]Entry, 0, len(raw))
	for id, v := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, Entry{ID: id, Type: strings.TrimSpace(v.Type)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ReadJSONAuthMeta returns the non-secret type and expires fields for one
// entry (MADR 0086 D5). Missing file or id is ok=false.
func ReadJSONAuthMeta(path, id string) (typ, expires string, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", false
	}
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if err != nil {
		return "", "", false
	}
	var raw map[string]struct {
		Type    string `json:"type"`
		Expires any    `json:"expires"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return "", "", false
	}
	e, found := raw[id]
	if !found {
		return "", "", false
	}
	if e.Expires != nil {
		expires = fmt.Sprint(e.Expires)
	}
	return strings.TrimSpace(e.Type), expires, true
}

// GooseConfig is the subset of goose's config.yaml that matters for auth.
type GooseConfig struct {
	// ActiveProvider is goose's `active_provider` — what a turn actually uses,
	// and what the MADR 0073 hang needed a phone-side switch for.
	ActiveProvider string
	// Providers are the configured provider ids, from the `providers` mapping
	// when present plus any provider-shaped keys goose writes at the top level.
	Providers []string
}

// ReadGooseConfig extracts the active provider and the configured provider set.
//
// This is a deliberately small hand-rolled scan rather than a YAML dependency:
// it reads two shapes (a scalar `active_provider` and the keys of a `providers`
// mapping) and must never fail the whole listing because goose added a field.
// Anything it does not understand is ignored.
func ReadGooseConfig(path string) (GooseConfig, error) {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return GooseConfig{}, nil
	}
	if err != nil {
		return GooseConfig{}, fmt.Errorf("read goose config: %w", err)
	}
	var cfg GooseConfig
	seen := map[string]struct{}{}
	inProviders := false
	// providerIndent is the column at which provider ids sit inside the
	// providers block, learned from the first entry. Anything deeper is that
	// provider's own settings — without this, `key:` and `kind:` nested under
	// a provider get mistaken for provider ids.
	providerIndent := -1
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		if indent == 0 {
			// A new top-level key ends any providers block.
			inProviders = strings.HasPrefix(trimmed, "providers:")
			providerIndent = -1
			if k, v, ok := splitYAMLScalar(trimmed); ok && k == "active_provider" {
				cfg.ActiveProvider = v
			}
			continue
		}
		if !inProviders {
			continue
		}
		if providerIndent < 0 {
			providerIndent = indent
		}
		if indent != providerIndent {
			// Deeper: a setting belonging to the provider above. Shallower is
			// malformed; either way it is not a provider id.
			continue
		}
		k, _, ok := splitYAMLScalar(trimmed)
		if !ok || k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		cfg.Providers = append(cfg.Providers, k)
	}
	if cfg.ActiveProvider != "" {
		if _, ok := seen[cfg.ActiveProvider]; !ok {
			// The active provider is always part of the configured set even
			// when goose keeps its settings elsewhere (keyring, token files).
			cfg.Providers = append(cfg.Providers, cfg.ActiveProvider)
		}
	}
	sort.Strings(cfg.Providers)
	return cfg, nil
}

// splitYAMLScalar splits `key: value`, unquoting a simple scalar value. It
// returns ok=false for lines that are not `key:`-shaped.
func splitYAMLScalar(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	value = strings.TrimSpace(line[idx+1:])
	value = strings.Trim(value, `"'`)
	// Strip a trailing comment on a scalar value.
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, true
}

// FileExists reports whether path exists and is a regular file. Used for
// presence-only signals such as ~/.grok/auth.json.
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
