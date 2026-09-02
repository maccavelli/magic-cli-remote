package updateclient

import (
	"fmt"
	"strings"
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

// --- Frozen legacy selector -------------------------------------------------
//
// The code below is a verbatim copy of the asset selection that shipped in
// internal/update/github.go up to and including v0.15.3, preserved here as a
// fixture. It is NOT production code and must never be "improved": its whole
// purpose is to reproduce what an ALREADY-INSTALLED binary out in the world
// does when it reads the v0.16.0 bridge release. If this copy drifts, the
// fixture stops proving anything about real installed clients.

type legacyAsset struct{ Name string }

// legacyAssetFor is internal/update/github.go:81-111 at v0.15.3.
func legacyAssetFor(assets []legacyAsset, product, goos, goarch string) (legacyAsset, string, error) {
	prefix := product + "-" + goos + "-" + goarch + "-"
	var matches []legacyAsset
	for _, a := range assets {
		if strings.HasPrefix(a.Name, prefix) && !strings.Contains(a.Name, "SHA256") {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		return legacyAsset{}, "", fmt.Errorf("no asset matching %s*", prefix)
	}
	if len(matches) > 1 {
		return legacyAsset{}, "", fmt.Errorf("multiple assets matching %s*", prefix)
	}
	a := matches[0]
	ver := strings.TrimSuffix(strings.TrimPrefix(a.Name, prefix), ".exe")
	if ver == "" {
		return legacyAsset{}, "", fmt.Errorf("asset %q has empty version suffix", a.Name)
	}
	return a, ver, nil
}

// legacySumsAsset is internal/update/github.go:114-128 at v0.15.3, including
// its permissive prefix fallback.
func legacySumsAsset(assets []legacyAsset, ver string) (legacyAsset, error) {
	name := "SHA256SUMS-" + ver
	for _, a := range assets {
		if a.Name == name {
			return a, nil
		}
	}
	for _, a := range assets {
		if a.Name == "SHA256SUMS" || strings.HasPrefix(a.Name, "SHA256SUMS") {
			return a, nil
		}
	}
	return legacyAsset{}, fmt.Errorf("no checksums asset for VER %s", ver)
}

// bridgeAssets is the complete v0.16.0 staged file set, exactly as the release
// job assembles it and as mcplib's verify-selfupdate-release.sh validates it.
func bridgeAssets() ([]legacyAsset, map[string]string) {
	products := []string{"mcremote", "mcrelay"}
	plats := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}, {"windows", "amd64"}}
	var out []legacyAsset
	compatSums := map[string]string{}
	for _, p := range products {
		for _, pl := range plats {
			base := p + "-" + pl[0] + "-" + pl[1]
			ext := ""
			if pl[0] == "windows" {
				ext = ".exe"
			}
			out = append(out, legacyAsset{Name: base + ext}) // canonical
			compat := base + "-0.16.0" + ext                 // compatibility
			out = append(out, legacyAsset{Name: compat})
			compatSums[compat] = "sha-of-" + compat
		}
	}
	out = append(out, legacyAsset{Name: "SHA256SUMS"})
	out = append(out, legacyAsset{Name: "SHA256SUMS-0.16.0"})
	out = append(out, legacyAsset{Name: "install.sh"})
	out = append(out, legacyAsset{Name: "install.ps1"})
	out = append(out, legacyAsset{Name: "magic-cli-remote-v0.16.0-arm64.apk"})
	return out, compatSums
}

// TestLegacySelectorCrossesTheBridge is the proof that an installed v0.15.x
// binary can still update through v0.16.0. It runs the frozen selector against
// the complete staged bridge asset list and requires, for every product and
// platform, that it picks the compatibility binary, resolves the compatibility
// manifest, and finds a matching checksum entry.
func TestLegacySelectorCrossesTheBridge(t *testing.T) {
	assets, compatSums := bridgeAssets()
	products := []string{"mcremote", "mcrelay"}
	plats := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}, {"windows", "amd64"}}

	for _, p := range products {
		for _, pl := range plats {
			name := p + "/" + pl[0] + "/" + pl[1]
			t.Run(name, func(t *testing.T) {
				asset, ver, err := legacyAssetFor(assets, p, pl[0], pl[1])
				if err != nil {
					t.Fatalf("legacy selector failed: %v", err)
				}
				wantExt := ""
				if pl[0] == "windows" {
					wantExt = ".exe"
				}
				wantAsset := p + "-" + pl[0] + "-" + pl[1] + "-0.16.0" + wantExt
				if asset.Name != wantAsset {
					t.Fatalf("selected %q, want the compatibility binary %q", asset.Name, wantAsset)
				}
				if ver != "0.16.0" {
					t.Fatalf("derived version %q, want 0.16.0 — SumsAsset would request the wrong manifest", ver)
				}
				sums, err := legacySumsAsset(assets, ver)
				if err != nil {
					t.Fatalf("legacy manifest lookup failed: %v", err)
				}
				if sums.Name != "SHA256SUMS-0.16.0" {
					t.Fatalf("resolved manifest %q, want SHA256SUMS-0.16.0", sums.Name)
				}
				if _, ok := compatSums[asset.Name]; !ok {
					t.Fatalf("no checksum entry for %q in the compatibility manifest", asset.Name)
				}
			})
		}
	}
}

// TestLegacySelectorIsNotConfusedByCanonicalNames guards the one way the
// bridge could fail closed: the canonical name has no trailing hyphen, so the
// legacy prefix must not match it. If it ever did, AssetFor would see two
// matches and refuse to update at all.
func TestLegacySelectorIsNotConfusedByCanonicalNames(t *testing.T) {
	assets, _ := bridgeAssets()
	for _, a := range assets {
		if a.Name == "mcremote-linux-amd64" {
			if strings.HasPrefix(a.Name, "mcremote-linux-amd64-") {
				t.Fatal("canonical name matches the legacy prefix; AssetFor would fail with multiple matches")
			}
		}
	}
	if _, _, err := legacyAssetFor(assets, "mcremote", "linux", "amd64"); err != nil {
		t.Fatalf("legacy selector must find exactly one match: %v", err)
	}
}

// TestLegacySumsFallbackIsNotReached documents why the compatibility manifest
// must be named exactly SHA256SUMS-0.16.0. The frozen fallback takes the first
// asset merely PREFIXED "SHA256SUMS", which would hand a legacy client the
// canonical manifest listing canonical names — none of which it can verify.
func TestLegacySumsFallbackIsNotReached(t *testing.T) {
	assets, _ := bridgeAssets()
	got, err := legacySumsAsset(assets, "0.16.0")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got.Name != "SHA256SUMS-0.16.0" {
		t.Fatalf("fallback was reached and returned %q", got.Name)
	}
	// Remove the exact manifest and the fallback does reach the wrong file,
	// which is exactly what the release contract must prevent.
	var without []legacyAsset
	for _, a := range assets {
		if a.Name != "SHA256SUMS-0.16.0" {
			without = append(without, a)
		}
	}
	fallback, err := legacySumsAsset(without, "0.16.0")
	if err != nil || fallback.Name != "SHA256SUMS" {
		t.Fatalf("expected the fallback to reach the canonical manifest, got %q / %v", fallback.Name, err)
	}
}
