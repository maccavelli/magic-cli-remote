package appdirs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths is the fully resolved product directory layout.
type Paths struct {
	Product           Product
	Home              string
	ConfigDir         string
	ConfigFile        string
	DataDir           string
	StateDir          string
	CacheDir          string
	LogDir            string // LaunchAgent stdio directory on Darwin
	RuntimeBase       string // product leaf under runtime home
	RuntimeDir        string // instance-keyed
	AdminSocket       string
	EngineRegistryDir string
	TempBase          string
	InstanceKey       string
}

// Resolve builds Paths from roots and an optional absolute dataDir override.
// Pure: no filesystem side effects.
func Resolve(product Product, roots Roots, dataDirOverride string) (Paths, error) {
	if !product.Valid() {
		return Paths{}, fmt.Errorf("appdirs: empty product name")
	}
	if roots.Home == "" || !filepath.IsAbs(roots.Home) {
		return Paths{}, fmt.Errorf("appdirs: home must be absolute")
	}
	for _, pair := range []struct {
		name, val string
	}{
		{"ConfigHome", roots.ConfigHome},
		{"DataHome", roots.DataHome},
		{"StateHome", roots.StateHome},
		{"CacheHome", roots.CacheHome},
		{"RuntimeHome", roots.RuntimeHome},
	} {
		if pair.val == "" || !filepath.IsAbs(pair.val) {
			return Paths{}, fmt.Errorf("appdirs: %s must be absolute, got %q", pair.name, pair.val)
		}
	}

	p := Paths{
		Product:    product,
		Home:       filepath.Clean(roots.Home),
		ConfigDir:  filepath.Join(roots.ConfigHome, product.Name),
		ConfigFile: filepath.Join(roots.ConfigHome, product.Name, "config.yaml"),
		DataDir:    filepath.Join(roots.DataHome, product.Name),
		StateDir:   roots.joinProduct(roots.StateHome, product.Name),
		CacheDir:   roots.joinProduct(roots.CacheHome, product.Name),
		TempBase:   roots.Temp,
	}
	if roots.Logs != "" {
		label := product.LaunchLabel
		if label == "" {
			label = product.Name
		}
		// Prefer short product name under Library/Logs for existing fixtures;
		// LaunchLabel is still available for service code that wants reverse-DNS.
		p.LogDir = filepath.Join(roots.Logs, product.Name)
		_ = label
	}

	// RuntimeBase: when RuntimeHome is already the product-specific temp fallback
	// (…/mcremote-runtime-UID), use it as the base; otherwise join product name.
	rt := filepath.Clean(roots.RuntimeHome)
	if strings.Contains(filepath.Base(rt), product.Name+"-runtime-") {
		p.RuntimeBase = rt
	} else {
		p.RuntimeBase = roots.joinProduct(rt, product.Name)
	}

	dataDir := p.DataDir
	if dataDirOverride != "" {
		if !filepath.IsAbs(dataDirOverride) {
			return Paths{}, fmt.Errorf("appdirs: data dir override must be absolute, got %q", dataDirOverride)
		}
		dataDir = filepath.Clean(dataDirOverride)
		p.DataDir = dataDir
	}

	key, err := InstanceKey(dataDir)
	if err != nil {
		return Paths{}, err
	}
	p.InstanceKey = key
	p.RuntimeDir = filepath.Join(p.RuntimeBase, key)
	p.AdminSocket = filepath.Join(p.RuntimeDir, "admin.sock")
	p.EngineRegistryDir = filepath.Join(p.StateDir, "instances", key, "engines")
	return p, nil
}

// WithDataDir returns a copy of p with instance paths recomputed for absolute dataDir.
func WithDataDir(p Paths, dataDir string) (Paths, error) {
	if dataDir == "" {
		return Paths{}, fmt.Errorf("appdirs: empty data dir")
	}
	if !filepath.IsAbs(dataDir) {
		return Paths{}, fmt.Errorf("appdirs: data dir must be absolute, got %q", dataDir)
	}
	dataDir = filepath.Clean(dataDir)
	key, err := InstanceKey(dataDir)
	if err != nil {
		return Paths{}, err
	}
	p.DataDir = dataDir
	p.InstanceKey = key
	p.RuntimeDir = filepath.Join(p.RuntimeBase, key)
	p.AdminSocket = filepath.Join(p.RuntimeDir, "admin.sock")
	p.EngineRegistryDir = filepath.Join(p.StateDir, "instances", key, "engines")
	return p, nil
}

// InstanceKey returns the first 16 lowercase hex chars of SHA-256(clean absolute dataDir).
func InstanceKey(dataDir string) (string, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return "", fmt.Errorf("appdirs: data dir for instance key must be absolute")
	}
	sum := sha256.Sum256([]byte(instanceKeyInput(dataDir)))
	return hex.EncodeToString(sum[:])[:16], nil
}

// instanceKeyInput normalizes dataDir before hashing. On a case-insensitive
// filesystem two spellings of one directory must produce one instance key, or
// a daemon started from a differently-cased path silently gets its own runtime
// dir and engine registry (MADR 0116 D3).
//
// This is a runtime.GOOS branch rather than a build tag on purpose: the
// behaviour has to be testable from a Unix development host.
func instanceKeyInput(dataDir string) string {
	clean := filepath.Clean(dataDir)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

// DefaultConfigFile is a convenience for product config.yaml via SystemRoots.
func DefaultConfigFile(product Product) (string, []Diagnostic, error) {
	roots, diags, err := SystemRoots(product)
	if err != nil {
		return "", diags, err
	}
	p, err := Resolve(product, roots, "")
	if err != nil {
		return "", diags, err
	}
	return p.ConfigFile, diags, nil
}

// SystemPaths resolves product paths from the live environment.
func SystemPaths(product Product, dataDirOverride string) (Paths, []Diagnostic, error) {
	roots, diags, err := SystemRoots(product)
	if err != nil {
		return Paths{}, diags, err
	}
	p, err := Resolve(product, roots, dataDirOverride)
	return p, diags, err
}
