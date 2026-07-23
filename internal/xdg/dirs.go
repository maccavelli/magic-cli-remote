// Package xdg resolves XDG base directories for mcremote/mcrelay on Linux and macOS.
package xdg

import (
	"os"
	"path/filepath"
)

// App names under XDG config/data homes.
const (
	AppMcremote = "mcremote"
	AppMcrelay  = "mcrelay"
)

const appName = AppMcremote // default for ConfigHome/DataHome (mcremote)

// ConfigHome returns $XDG_CONFIG_HOME/mcremote or ~/.config/mcremote.
func ConfigHome() (string, error) { return ConfigHomeFor(AppMcremote) }

// DataHome returns $XDG_DATA_HOME/mcremote or ~/.local/share/mcremote.
func DataHome() (string, error) { return DataHomeFor(AppMcremote) }

// DefaultConfigFile returns the default path to mcremote config.yaml.
func DefaultConfigFile() (string, error) { return DefaultConfigFileFor(AppMcremote) }

// ConfigHomeFor returns $XDG_CONFIG_HOME/<app> or ~/.config/<app>.
func ConfigHomeFor(app string) (string, error) {
	if app == "" {
		app = appName
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, app), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", app), nil
}

// DataHomeFor returns $XDG_DATA_HOME/<app> or ~/.local/share/<app>.
func DataHomeFor(app string) (string, error) {
	if app == "" {
		app = appName
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, app), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", app), nil
}

// DefaultConfigFileFor returns the default path to config.yaml for app.
func DefaultConfigFileFor(app string) (string, error) {
	dir, err := ConfigHomeFor(app)
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
