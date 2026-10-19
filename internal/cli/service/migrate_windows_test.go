//go:build windows

package service

import (
	"path/filepath"
	"testing"
)

// TestDefaultInstalledBinaryIsTheNewFolder: setup-service's default binary on
// Windows is the library installer's folder (MADR 0054 D9, PLAN S5g).
func TestDefaultInstalledBinaryIsTheNewFolder(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\L`)
	if got, want := defaultInstalledBinary(`C:\Users\user`, "mcrelay"), filepath.Join(`C:\L`, "Programs", "magic-cli-remote", "mcrelay.exe"); got != want {
		t.Fatalf("defaultInstalledBinary = %s, want %s", got, want)
	}
}
