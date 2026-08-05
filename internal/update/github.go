package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultAPI is the public GitHub API for this repository's releases.
const DefaultAPI = "https://api.github.com/repos/maccavelli/magic-cli-remote/releases/latest"

// Release is a GitHub release summary used by the updater.
type Release struct {
	Tag    string
	Base   string // three-part base without leading v
	Assets []Asset
}

// Asset is one downloadable file on a release.
type Asset struct {
	Name string
	URL  string
	Size int64
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// Latest fetches the latest non-prerelease release. baseURL is injectable for
// tests (full URL to the JSON document).
func Latest(ctx context.Context, baseURL string) (Release, error) {
	if baseURL == "" {
		baseURL = DefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mcremote-update")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, fmt.Errorf("github releases: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	tag := strings.TrimSpace(raw.TagName)
	base := BaseString(tag)
	if base == "" {
		return Release{}, fmt.Errorf("release tag %q has no major.minor.patch base", tag)
	}
	assets := make([]Asset, 0, len(raw.Assets))
	for _, a := range raw.Assets {
		assets = append(assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	return Release{Tag: tag, Base: base, Assets: assets}, nil
}

// AssetFor picks the single product/os/arch binary for this release base
// (MADR 0065 F6). Returns the asset and the full VER suffix used in sums
// filenames (e.g. "0.6.7.4" from mcremote-darwin-arm64-0.6.7.4).
func (r Release) AssetFor(product, goos, goarch string) (Asset, string, error) {
	prefix := fmt.Sprintf("%s-%s-%s-", product, goos, goarch)
	var matches []Asset
	for _, a := range r.Assets {
		if strings.HasPrefix(a.Name, prefix) && !strings.Contains(a.Name, "SHA256") {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		return Asset{}, "", fmt.Errorf("no asset matching %s* in release %s", prefix, r.Tag)
	}
	if len(matches) > 1 {
		return Asset{}, "", fmt.Errorf("multiple assets matching %s* in release %s", prefix, r.Tag)
	}
	a := matches[0]
	// Name: product-os-arch-VER  → VER is after the third hyphen-group.
	// product may contain no hyphens; os/arch known. Strip prefix.
	ver := strings.TrimPrefix(a.Name, prefix)
	if ver == "" {
		return Asset{}, "", fmt.Errorf("asset %q has empty version suffix", a.Name)
	}
	return a, ver, nil
}

// SumsAsset returns the SHA256SUMS-<VER> asset for the given VER.
func (r Release) SumsAsset(ver string) (Asset, error) {
	name := "SHA256SUMS-" + ver
	for _, a := range r.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	// Also accept SHA256SUMS without suffix (some releases).
	for _, a := range r.Assets {
		if a.Name == "SHA256SUMS" || strings.HasPrefix(a.Name, "SHA256SUMS") {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("no checksums asset for VER %s in release %s", ver, r.Tag)
}
