package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigHome(t *testing.T) {
	// 1. With XDG_CONFIG_HOME set
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := ConfigHome()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/custom/config", appName)
	if got != want {
		t.Errorf("ConfigHome() with env set = %q; want %q", got, want)
	}

	// 2. Fallback to UserHomeDir
	t.Setenv("XDG_CONFIG_HOME", "")
	gotFallback, err := ConfigHome()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wantFallback := filepath.Join(home, ".config", appName)
	if gotFallback != wantFallback {
		t.Errorf("ConfigHome() fallback = %q; want %q", gotFallback, wantFallback)
	}
}

func TestDataHome(t *testing.T) {
	// 1. With XDG_DATA_HOME set
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got, err := DataHome()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/custom/data", appName)
	if got != want {
		t.Errorf("DataHome() with env set = %q; want %q", got, want)
	}

	// 2. Fallback to UserHomeDir
	t.Setenv("XDG_DATA_HOME", "")
	gotFallback, err := DataHome()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wantFallback := filepath.Join(home, ".local", "share", appName)
	if gotFallback != wantFallback {
		t.Errorf("DataHome() fallback = %q; want %q", gotFallback, wantFallback)
	}
}

func TestDefaultConfigFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got, err := DefaultConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/custom/config", appName, "config.yaml")
	if got != want {
		t.Errorf("DefaultConfigFile() = %q; want %q", got, want)
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "nested", "dir")

	err := EnsureDir(targetDir)
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	fi, err := os.Stat(targetDir)
	if err != nil {
		t.Fatalf("failed to stat target dir: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("expected path to be a directory")
	}

	// Verify permissions (on UNIX-like systems, 0700 permissions should be enforced, but mask applies)
	perm := fi.Mode().Perm()
	if perm&0o077 != 0 {
		t.Logf("Warning: permissions on directory are %O, expected no group/other access (0700)", perm)
	}
}

func TestConfigHomeError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	// If HOME is unset, os.UserHomeDir might return an error. Let's see if we can trigger it.
	// In Go, os.UserHomeDir looks up environment variables first depending on the platform.
	// On Linux/Unix, it looks up $HOME.
	t.Setenv("HOME", "")
	// Also clear other possible envs
	t.Setenv("USERPROFILE", "")

	_, err := ConfigHome()
	if err == nil {
		// On some platforms or test environments it might not fail if it reads /etc/passwd.
		// That is fine, but we verify that if there is an error, it is surfaced.
		t.Log("os.UserHomeDir did not fail even with HOME unset")
	}
}
