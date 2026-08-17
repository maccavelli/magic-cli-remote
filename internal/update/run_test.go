package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRun_CheckAvailable(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets":   []any{},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.1.0",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &out,
	})
	if !errors.Is(err, ErrUpdateAvailable) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out.String(), "update available") {
		t.Fatalf("out=%s", out.String())
	}
}

func TestRun_CheckUpToDate(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v0.1.0",
		"assets":   []any{},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.1.0",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRun_DevRequiresForce(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets":   []any{},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.1.0.1.gdead",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err=%v", err)
	}
}

func TestRun_SameBaseWithoutForceIsUpToDate(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets":   []any{},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "9.9.9",
		Yes:          true,
		APIURL:       ts.URL,
		Out:          &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("out=%s", out.String())
	}
}

// --force at an equal base re-seeds from the published asset instead of
// short-circuiting on "already up to date".
func TestRun_ForceReinstallsSameBase(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets": []map[string]any{
			{
				"name":                 "mcremote-" + runtime.GOOS + "-" + runtime.GOARCH + "-9.9.9.1",
				"browser_download_url": "http://invalid.example/bin",
				"size":                 1,
			},
			{
				"name":                 "SHA256SUMS-9.9.9.1",
				"browser_download_url": "http://invalid.example/sums",
				"size":                 1,
			},
		},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	var out bytes.Buffer
	// Local build of the tagged commit: same base, dev suffix — the exact
	// shape a `make install` host is in.
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "9.9.9.2.gdeadbee",
		Yes:          true,
		Force:        true,
		APIURL:       ts.URL,
		Out:          &out,
		Err:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected download error (no sums asset)")
	}
	if strings.Contains(out.String(), "already up to date") {
		t.Fatalf("force must not short-circuit: out=%s", out.String())
	}
	if !strings.Contains(out.String(), "downloading") {
		t.Fatalf("expected download attempt: out=%s", out.String())
	}
}

// --force must not fabricate an available update for --check.
func TestRun_CheckIgnoresForce(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets":   []any{},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "9.9.9.2.gdeadbee",
		Check:        true,
		Force:        true,
		APIURL:       ts.URL,
		Out:          &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Fatalf("out=%s", out.String())
	}
}

func TestRun_HappyPathSwap(t *testing.T) {
	// Install into a temp dir by replacing ExecutableDir isn't easy — run
	// download+swap manually with staged files is covered by swap_test.
	// Here we only prove --yes with abort path when asset missing.
	body, _ := json.Marshal(map[string]any{
		"tag_name": "v9.9.9",
		"assets": []map[string]any{
			{
				"name":                 "mcremote-" + runtime.GOOS + "-" + runtime.GOARCH + "-9.9.9.1",
				"browser_download_url": "http://invalid.example/bin",
				"size":                 1,
			},
		},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer ts.Close()
	// Without force on non-dev local, will try download and fail on network —
	// use Yes + force + local clean version.
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.1.0",
		Yes:          true,
		APIURL:       ts.URL,
		Out:          &bytes.Buffer{},
		Err:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected download error (no sums asset)")
	}
}

func TestRun_DownloadAndSwapCompose(t *testing.T) {
	binBody := []byte("fake-binary")
	sum := sha256.Sum256(binBody)
	hexSum := hex.EncodeToString(sum[:])
	name := "mcremote-" + runtime.GOOS + "-" + runtime.GOARCH + "-9.9.9.1"
	sumsBody := hexSum + "  " + name + "\n"

	var binURL, sumsURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bin":
			_, _ = w.Write(binBody)
		case "/sums":
			_, _ = w.Write([]byte(sumsBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	binURL = ts.URL + "/bin"
	sumsURL = ts.URL + "/sums"

	dir := t.TempDir()
	staged, err := DownloadVerified(context.Background(),
		Asset{Name: name, URL: binURL},
		Asset{Name: "SHA256SUMS-9.9.9.1", URL: sumsURL},
		dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "mcremote")
	if err := SwapAndRestart(staged, dest, SwapOpts{Product: "mcremote"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != string(binBody) {
		t.Fatalf("dest content mismatch")
	}
}
