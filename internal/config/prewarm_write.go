package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	yaml "go.yaml.in/yaml/v3"
)

// ErrUnknownProvider is returned when SetProviderPrewarmFile is given an
// id that is not one of the five agent providers.
var ErrUnknownProvider = errors.New("unknown provider")

// ErrNoConfigFile means there is no config file to edit: the daemon booted on
// pure defaults (no file was found or named), or the file on disk is empty.
// Callers match on this rather than on message text, and must not create a
// config file as a side effect of a prewarm toggle.
var ErrNoConfigFile = errors.New("no config file to write")

var prewarmWriteMu sync.Mutex

// KnownProviderIDs is the closed set of agents that expose prewarm (MADR 0089 D5/D7).
var KnownProviderIDs = []string{"grok", "goose", "opencode", "codex", "kilo"}

// KnownProvider reports whether id is a prewarm-controllable agent.
func KnownProvider(id string) bool {
	for _, k := range KnownProviderIDs {
		if k == id {
			return true
		}
	}
	return false
}

// SetProviderPrewarm updates the in-memory flag for one agent.
func (c *Config) SetProviderPrewarm(id string, prewarm bool) error {
	if c == nil {
		return errors.New("nil config")
	}
	switch id {
	case "grok":
		c.Providers.Grok.Prewarm = prewarm
	case "goose":
		c.Providers.Goose.Prewarm = prewarm
	case "opencode":
		c.Providers.Opencode.Prewarm = prewarm
	case "codex":
		c.Providers.Codex.Prewarm = prewarm
	case "kilo":
		c.Providers.Kilo.Prewarm = prewarm
	default:
		return fmt.Errorf("%w: %s", ErrUnknownProvider, id)
	}
	return nil
}

// ProviderPrewarm returns the in-memory flag and whether id is known.
func (c Config) ProviderPrewarm(id string) (prewarm bool, ok bool) {
	switch id {
	case "grok":
		return c.Providers.Grok.Prewarm, true
	case "goose":
		return c.Providers.Goose.Prewarm, true
	case "opencode":
		return c.Providers.Opencode.Prewarm, true
	case "codex":
		return c.Providers.Codex.Prewarm, true
	case "kilo":
		return c.Providers.Kilo.Prewarm, true
	default:
		return false, false
	}
}

// Live is the running config plus its file path. SetPrewarm writes one YAML
// key and updates memory under one lock (MADR 0089 D7 / T2.7).
type Live struct {
	mu   sync.Mutex
	Path string
	Cfg  *Config
}

// SetPrewarm persists providers.<id>.prewarm and updates Cfg.
func (l *Live) SetPrewarm(id string, prewarm bool) error {
	if l == nil || l.Cfg == nil {
		return errors.New("nil live config")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := SetProviderPrewarmFile(l.Path, id, prewarm); err != nil {
		return err
	}
	return l.Cfg.SetProviderPrewarm(id, prewarm)
}

// GetPrewarm returns the live in-memory value.
func (l *Live) GetPrewarm(id string) (prewarm bool, ok bool) {
	if l == nil || l.Cfg == nil {
		return false, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Cfg.ProviderPrewarm(id)
}

// SetProviderPrewarmFile updates providers.<id>.prewarm in path.
// Creates the key if missing. Preserves comments and unknown keys.
// Atomic: write path+".tmp" (0600) then rename.
func SetProviderPrewarmFile(path, providerID string, prewarm bool) error {
	if !KnownProvider(providerID) {
		return fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}
	if path == "" {
		return fmt.Errorf("%w: no config file path", ErrNoConfigFile)
	}
	prewarmWriteMu.Lock()
	defer prewarmWriteMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("%w: %s is empty", ErrNoConfigFile, path)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	root := mappingRoot(&doc)
	if root == nil {
		return errors.New("config root is not a mapping")
	}
	providers := ensureMapping(root, "providers")
	if providers == nil {
		return errors.New("providers is not a mapping")
	}
	block := ensureMapping(providers, providerID)
	if block == nil {
		return fmt.Errorf("providers.%s is not a mapping", providerID)
	}
	setBool(block, "prewarm", prewarm)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	// Encode the document node so comments on the root survive.
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return writeFileAtomic0600(path, buf.Bytes())
}

func mappingRoot(doc *yaml.Node) *yaml.Node {
	n := doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

func ensureMapping(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			n := parent.Content[i+1]
			if n.Kind != yaml.MappingNode {
				return nil
			}
			return n
		}
	}
	keyN := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valN := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyN, valN)
	return valN
}

func setBool(mapping *yaml.Node, key string, v bool) {
	val := "false"
	if v {
		val = "true"
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			n := mapping.Content[i+1]
			n.Kind = yaml.ScalarNode
			n.Tag = "!!bool"
			n.Value = val
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val},
	)
}

func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mcremote-prewarm-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
