package updateclient

import (
	"testing"

	"github.com/maccavelli/mcplib/selfupdate"
)

func TestNormalizeInstalled(t *testing.T) {
	cases := []struct {
		name      string
		version   string
		buildKind string
		wantVer   string
		wantKind  selfupdate.BuildKind
	}{
		{"plain three part", "0.15.3", "release", "v0.15.3", selfupdate.ReleaseBuild},
		{"v prefixed", "v0.15.3", "release", "v0.15.3", selfupdate.ReleaseBuild},
		{"legacy base dot n", "0.15.3.7", "release", "v0.15.3", selfupdate.ReleaseBuild},
		{"legacy base dot n v prefixed", "v0.15.3.7", "release", "v0.15.3", selfupdate.ReleaseBuild},
		{"legacy local hash is local", "0.15.3.7.gdeadbee", "local", "v0.15.3", selfupdate.LocalBuild},
		{"release string but local stamp", "0.15.3", "local", "v0.15.3", selfupdate.LocalBuild},
		{"empty stamp is local", "0.15.3", "", "v0.15.3", selfupdate.LocalBuild},
		{"dev", "dev", "release", "dev", selfupdate.LocalBuild},
		{"debug", "debug", "release", "debug", selfupdate.LocalBuild},
		{"empty", "", "release", "", selfupdate.LocalBuild},
		{"two part", "0.15", "release", "0.15", selfupdate.LocalBuild},
		{"non numeric patch", "0.15.x", "release", "0.15.x", selfupdate.LocalBuild},
		{"leading zero rejected", "0.015.3", "release", "0.015.3", selfupdate.LocalBuild},
		{"whitespace trimmed", "  0.15.3  ", "release", "v0.15.3", selfupdate.ReleaseBuild},
		{"garbage", "not-a-version", "release", "not-a-version", selfupdate.LocalBuild},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVer, gotKind := NormalizeInstalled(tc.version, tc.buildKind)
			if gotVer != tc.wantVer {
				t.Errorf("version = %q, want %q", gotVer, tc.wantVer)
			}
			if gotKind != tc.wantKind {
				t.Errorf("kind = %v, want %v", gotKind, tc.wantKind)
			}
		})
	}
}

// TestNormalizeInstalledNeverYieldsUnknown guards the contract the shared
// request validator depends on: every path returns a decided build kind.
func TestNormalizeInstalledNeverYieldsUnknown(t *testing.T) {
	for _, v := range []string{"", "dev", "0.1.2", "0.1.2.3", "0.1.2.3.gabc", "x", "1.2", "1.2.3.4.5"} {
		for _, k := range []string{"release", "local", "", "Release"} {
			if _, kind := NormalizeInstalled(v, k); kind == selfupdate.BuildUnknown {
				t.Fatalf("NormalizeInstalled(%q, %q) returned BuildUnknown", v, k)
			}
		}
	}
}
