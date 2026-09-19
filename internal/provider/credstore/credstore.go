// Package credstore reads and writes the agent CLIs' own credential stores.
//
// MADR 0074 D1/D2: mcremote never invents a credential vault. Every secret it
// accepts from the phone lands in the store the agent itself reads, in that
// agent's own format. This package is the single place that knows those
// formats, so a vendor format change breaks one package with live-pinned tests
// (D15) rather than leaking across the daemon.
//
// A probe decided the shape of this package (MADR 0074 §5): `opencode auth
// login` reaches its key prompt non-interactively, but its masked TUI widget
// ignores piped stdin and writes nothing. So a direct file write is not a
// shortcut around the CLI; it is the only mechanism that exists.
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

// GrokHome is the effective Grok home: non-empty $GROK_HOME, else ~/.grok.
//
// grok 1.0.5 resolves GROK_HOME before $HOME, so resolving $HOME
// unconditionally makes mcremote inspect a different file than the CLI mutates
// on any host that sets it. Every Grok path below derives from this one result
// so status, mutation, locking, and the child environment cannot disagree
// (MADR 0074 F10/D22).
func GrokHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("GROK_HOME")); v != "" {
		return v, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".grok"), nil
}

// GrokAuthPath is <GrokHome>/auth.json — the OAuth session, not a key store.
func GrokAuthPath() (string, error) {
	home, err := GrokHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "auth.json"), nil
}

// GrokAuthLockPath is the path fsutil.WithLock derives grok's native lock from,
// so mcremote serializes against a concurrent refresh instead of racing it
// (MADR 0074 F12).
//
// It returns auth.json, NOT auth.json.lock. WithLock appends the ".lock"
// suffix itself, so returning a name that already carries it made every
// publication flock auth.json.lock.lock — a file nothing else takes — while
// grok's own writer held auth.json.lock (MADR 0133). Return the base path here
// and let WithLock derive the rest, the way every other caller does.
func GrokAuthLockPath() (string, error) {
	return GrokAuthPath()
}

// GrokHomeEnv is the environment overlay pointing a grok child at home.
func GrokHomeEnv(home string) string { return "GROK_HOME=" + home }

// GrokConfigPath is <GrokHome>/config.toml, where a quoted
// [model."<id>"] api_key lives (MADR 0085 D4).
func GrokConfigPath() (string, error) {
	home, err := GrokHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.toml"), nil
}

// CodexHome is the effective Codex home: non-empty $CODEX_HOME, else ~/.codex.
// See GrokHome for why this must not resolve $HOME unconditionally
// (MADR 0074 F7/D22).
func CodexHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// CodexAuthPath is <CodexHome>/auth.json.
func CodexAuthPath() (string, error) {
	home, err := CodexHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "auth.json"), nil
}

// CodexAuthLockPath is the path fsutil.WithLock derives the lock every mcremote
// Codex mutation takes from.
//
// It returns auth.json, NOT auth.json.lock, for the reason spelled out on
// GrokAuthLockPath: WithLock appends ".lock" itself (MADR 0133).
//
// Codex 0.148.0 does not itself honor this lock, so it narrows but cannot
// eliminate a race with a separately launched CLI; the coordinator verifies
// published bytes and preserves every observed generation on conflict
// (MADR 0074 D25).
func CodexAuthLockPath() (string, error) {
	return CodexAuthPath()
}

// CodexHomeEnv is the environment overlay pointing a codex child at home.
func CodexHomeEnv(home string) string { return "CODEX_HOME=" + home }

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

// FileExists reports whether path exists and is a regular file. Used for
// presence-only signals such as ~/.grok/auth.json.
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
