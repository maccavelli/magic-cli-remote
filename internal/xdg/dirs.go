// Package xdg resolves XDG base directories for mcremote on Linux and macOS.
package xdg

import (
	"os"
	"path/filepath"
)

const appName = "mcremote"

// ConfigHome returns $XDG_CONFIG_HOME/mcremote or ~/.config/mcremote.
func ConfigHome() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appName), nil
}

// DataHome returns $XDG_DATA_HOME/mcremote or ~/.local/share/mcremote.
func DataHome() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}

// DefaultConfigFile returns the default path to config.yaml.
func DefaultConfigFile() (string, error) {
	dir, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// EnsureDir creates dir with mode 0700, and tightens a pre-existing directory
// to 0700 — MkdirAll alone is a no-op on an existing 0755 dir, which would
// leave the data dir (device names, file listing) group/world readable.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(dir); err == nil && st.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}
