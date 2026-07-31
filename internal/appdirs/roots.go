package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Roots holds absolute base directories used to build Paths.
// Tests inject Roots; production uses SystemRoots.
type Roots struct {
	Home        string
	ConfigHome  string // XDG_CONFIG_HOME value (not product leaf)
	DataHome    string
	StateHome   string
	CacheHome   string
	RuntimeHome string // XDG_RUNTIME_DIR or validated fallback parent for product
	Temp        string
	Logs        string // LaunchAgent stdio base on Darwin (home/Library/Logs)
}

// Diagnostic is a non-fatal path resolution note.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SystemRoots discovers XDG roots from the environment and home directory.
// Relative $XDG_* values are ignored with a diagnostic (XDG absolute-path rule).
func SystemRoots(product Product) (Roots, []Diagnostic, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Roots{}, nil, fmt.Errorf("home dir: %w", err)
	}
	if !filepath.IsAbs(home) {
		return Roots{}, nil, fmt.Errorf("home dir is not absolute: %q", home)
	}
	home = filepath.Clean(home)

	var diags []Diagnostic
	xdgOr := func(env, fallback string) string {
		v := os.Getenv(env)
		if v == "" {
			return fallback
		}
		if !filepath.IsAbs(v) {
			diags = append(diags, Diagnostic{
				Code:    "xdg_relative_ignored",
				Message: fmt.Sprintf("%s=%q is relative; using %s", env, v, fallback),
			})
			return fallback
		}
		return filepath.Clean(v)
	}

	r := Roots{
		Home:       home,
		ConfigHome: xdgOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		DataHome:   xdgOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
		StateHome:  xdgOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state")),
		CacheHome:  xdgOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")),
		Temp:       filepath.Clean(os.TempDir()),
		Logs:       filepath.Join(home, "Library", "Logs"),
	}

	runtimeHome, runtimeDiags := resolveRuntimeHome(home, product.Name)
	diags = append(diags, runtimeDiags...)
	r.RuntimeHome = runtimeHome

	return r, diags, nil
}

// resolveRuntimeHome picks XDG_RUNTIME_DIR, else /run/user/$UID on Linux,
// else a secure per-uid temp leaf. Validation of the leaf happens in Ensure.
func resolveRuntimeHome(home, product string) (string, []Diagnostic) {
	var diags []Diagnostic
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		if !filepath.IsAbs(v) {
			diags = append(diags, Diagnostic{
				Code:    "xdg_runtime_relative_ignored",
				Message: fmt.Sprintf("XDG_RUNTIME_DIR=%q is relative; using fallback", v),
			})
		} else {
			return filepath.Clean(v), diags
		}
	}
	if runtime.GOOS == "linux" {
		uid := os.Getuid()
		candidate := filepath.Join("/run/user", fmt.Sprintf("%d", uid))
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, diags
		}
	}
	// Secure per-uid temp leaf (XDG allows replacement with warning).
	uid := os.Getuid()
	leaf := filepath.Join(os.TempDir(), fmt.Sprintf("%s-runtime-%d", product, uid))
	diags = append(diags, Diagnostic{
		Code:    "xdg_runtime_fallback",
		Message: fmt.Sprintf("XDG_RUNTIME_DIR unset or unusable; using %s", leaf),
	})
	return leaf, diags
}
