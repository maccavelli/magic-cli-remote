package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Verifier checks a staged file against the sums body. Default is sha256
// of the asset name line in SHA256SUMS.
type Verifier func(stagedPath, assetName, sumsBody string) error

// SHA256SumsVerifier implements the release SHA256SUMS format:
// "<hex>  <filename>" or "<hex> *<filename>".
func SHA256SumsVerifier(stagedPath, assetName, sumsBody string) error {
	want := ""
	for _, line := range strings.Split(sumsBody, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == assetName {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums file has no entry for %s", assetName)
	}
	f, err := os.Open(stagedPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", assetName, got, want)
	}
	return nil
}

// DownloadVerified downloads asset, verifies against sums, and returns the
// path of the staged executable under destDir. On any failure the staged
// file is removed.
func DownloadVerified(ctx context.Context, asset, sums Asset, destDir string, verify Verifier) (stagedPath string, err error) {
	if verify == nil {
		verify = SHA256SumsVerifier
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	sumsBody, err := httpGet(ctx, sums.URL)
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	stagedPath = filepath.Join(destDir, asset.Name+".staging")
	_ = os.Remove(stagedPath)
	if err := httpGetToFile(ctx, asset.URL, stagedPath); err != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("download asset: %w", err)
	}
	if err := verify(stagedPath, asset.Name, string(sumsBody)); err != nil {
		_ = os.Remove(stagedPath)
		return "", err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		_ = os.Remove(stagedPath)
		return "", err
	}
	return stagedPath, nil
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mcremote-update")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func httpGetToFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mcremote-update")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}
