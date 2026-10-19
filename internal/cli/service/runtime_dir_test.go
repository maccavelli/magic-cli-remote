//go:build unix

package service

import (
	"strings"
	"testing"
)

// TestUnitPinsRuntimeDirWhenInstallerHasNone is MADR 0201 D27 (0073 §6.9).
// The units set PrivateTmp=true, and named XDG_RUNTIME_DIR only when the
// installer's environment had it. Without it, and without /run/user/$UID,
// the service fell back to its private /tmp, and its admin socket split from
// the CLI's. With no installer value, both units now name systemd's %t.
func TestUnitPinsRuntimeDirWhenInstallerHasNone(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		for _, tc := range []struct {
			installer, want string
		}{
			{"", "Environment=XDG_RUNTIME_DIR=%t\n"},
			{"/run/user/1000", "Environment=XDG_RUNTIME_DIR=/run/user/1000\n"},
		} {
			penv := &renderEnv{Home: "/home/u", User: "u", Path: "/usr/bin", XDGRuntimeDir: tc.installer}
			body, err := renderWith(Options{
				Product: product, UnitName: product, Description: "d",
				Binary: "/home/u/bin/" + product, WorkingDirectory: "/home/u",
			}, penv)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s unit, installer XDG_RUNTIME_DIR=%q: no %q line in:\n%s",
					product, tc.installer, strings.TrimSpace(tc.want), body)
			}
			if n := strings.Count(body, "XDG_RUNTIME_DIR="); n != 1 {
				t.Errorf("%s unit, installer XDG_RUNTIME_DIR=%q: %d XDG_RUNTIME_DIR lines, want 1",
					product, tc.installer, n)
			}
		}
	}
}
