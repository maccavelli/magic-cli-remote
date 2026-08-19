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

func releaseJSON(product, tag, publishedVER string) []byte {
	name := product + "-" + runtime.GOOS + "-" + runtime.GOARCH + "-" + publishedVER
	body, _ := json.Marshal(map[string]any{
		"tag_name": tag,
		"assets": []map[string]any{
			{"name": name, "browser_download_url": "http://invalid.example/bin", "size": 1},
			{"name": "SHA256SUMS-" + publishedVER, "browser_download_url": "http://invalid.example/sums", "size": 1},
		},
	})
	return body
}

func serveReleaseJSON(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestRun_CheckAvailable(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.10", "0.13.10.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &out,
	})
	if !errors.Is(err, ErrUpdateAvailable) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out.String(), "0.13.9.1 → 0.13.10.1") {
		t.Fatalf("out=%s", out.String())
	}
}

func TestRun_CheckUpToDate(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.9", "0.13.9.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1",
		Check:        true,
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

func TestRun_NewerNIsAvailable(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.9", "0.13.9.2"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &out,
	})
	if !errors.Is(err, ErrUpdateAvailable) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out.String(), "0.13.9.1 → 0.13.9.2") {
		t.Fatalf("out=%s", out.String())
	}
}

func TestRun_PublishedFourPartIsNotDev(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.10", "0.13.10.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1",
		Yes:          true,
		APIURL:       ts.URL,
		Out:          &out,
		Err:          &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected download error (no sums asset)")
	}
	if strings.Contains(err.Error(), "dev suffix") || strings.Contains(out.String(), "dev suffix") {
		t.Fatalf("published BASE.N must not be treated as local: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "downloading") {
		t.Fatalf("expected download attempt: out=%s", out.String())
	}
}

func TestRun_DevRequiresForce(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.10", "0.13.10.1"))
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1.gdead",
		Check:        true,
		APIURL:       ts.URL,
		Out:          &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err=%v", err)
	}
	if errors.Is(err, ErrUpdateAvailable) {
		t.Fatal("local compile must not report update available")
	}
}

func TestRun_SamePublishedVERIsUpToDate(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.9", "0.13.9.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1",
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
	if strings.Contains(out.String(), "downloading") {
		t.Fatalf("must not download when published VER matches: out=%s", out.String())
	}
}

// --force at an equal published VER re-seeds from the asset instead of
// short-circuiting on "already up to date".
func TestRun_ForceReinstallsSameBase(t *testing.T) {
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.9", "0.13.9.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1.gdeadbee",
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
	ts := serveReleaseJSON(t, releaseJSON("mcremote", "v0.13.9", "0.13.9.1"))
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.13.9.1.gdeadbee",
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

// releaseServer serves a releases JSON plus the binary and SHA256SUMS assets,
// so a full Run can reach the swap without touching the network.
func releaseServer(t *testing.T, product, binBody string) *httptest.Server {
	t.Helper()
	name := product + "-" + runtime.GOOS + "-" + runtime.GOARCH + "-9.9.9.1"
	sum := sha256.Sum256([]byte(binBody))
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(binBody)) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(sums)) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{
			"tag_name": "v9.9.9",
			"assets": []map[string]any{
				{"name": name, "browser_download_url": ts.URL + "/bin", "size": len(binBody)},
				{"name": "SHA256SUMS-9.9.9.1", "browser_download_url": ts.URL + "/sums", "size": len(sums)},
			},
		})
		_, _ = w.Write(body)
	})
	return ts
}

// pinExecutableDir points the install directory at a temp dir for one test.
func pinExecutableDir(t *testing.T, dir string) {
	t.Helper()
	prev := executableDir
	executableDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { executableDir = prev })
}

// MADR 0100 F3: HealStart was unconditional, so a host with no service
// installed — a plain binary install, runit/s6/openrc, macOS without the
// LaunchAgent — failed the start and rolled a good swap back.
func TestRunSkipsHealStartWhenNotInstalled(t *testing.T) {
	ts := releaseServer(t, "mcremote", "fresh-binary")
	dir := t.TempDir()
	pinExecutableDir(t, dir)

	starts := 0
	var out bytes.Buffer
	err := Run(context.Background(), RunOpts{
		Product:      "mcremote",
		LocalVersion: "0.1.0",
		Yes:          true,
		APIURL:       ts.URL,
		Out:          &out,
		Err:          &bytes.Buffer{},
		Service: FuncService{
			IsActiveFn:    func(string) (bool, error) { return false, nil },
			IsInstalledFn: func(string) (bool, error) { return false, nil },
			// systemd's own wording, lowercased for golint: "Failed to stop
			// mcremote.service: Unit mcremote.service not loaded." and
			// "Failed to start mcremote.service: Unit mcremote.service not found."
			StopFn: func(string) error { return errors.New("unit mcremote.service not loaded") },
			StartFn: func(string) error {
				starts++
				return errors.New("unit mcremote.service not found")
			},
		},
	})
	if err != nil {
		t.Fatalf("update must succeed where no service is installed: %v\n%s", err, out.String())
	}
	if starts != 0 {
		t.Fatalf("Start called %d times for a host with no service", starts)
	}
	if !strings.Contains(out.String(), "updated mcremote") {
		t.Fatalf("out = %s", out.String())
	}
}

// The heal-start itself must survive the gate: installed but down still starts.
func TestRunHealStartsWhenInstalledButDown(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			ts := releaseServer(t, product, "fresh-binary")
			dir := t.TempDir()
			pinExecutableDir(t, dir)

			starts := 0
			active := false
			err := Run(context.Background(), RunOpts{
				Product:      product,
				LocalVersion: "0.1.0",
				Yes:          true,
				APIURL:       ts.URL,
				Out:          &bytes.Buffer{},
				Err:          &bytes.Buffer{},
				Service: FuncService{
					IsActiveFn:    func(string) (bool, error) { return active, nil },
					IsInstalledFn: func(string) (bool, error) { return true, nil },
					StopFn:        func(string) error { return nil },
					StartFn: func(string) error {
						starts++
						active = true
						return nil
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if starts != 1 {
				t.Fatalf("Start called %d times, want the heal start", starts)
			}
		})
	}
}

type recordingService struct {
	inner FuncService
	calls []string
}

func (r *recordingService) record(op, product string) {
	r.calls = append(r.calls, op+":"+product)
}

func (r *recordingService) IsActive(product string) (bool, error) {
	r.record("IsActive", product)
	return r.inner.IsActive(product)
}

func (r *recordingService) IsInstalled(product string) (bool, error) {
	r.record("IsInstalled", product)
	return r.inner.IsInstalled(product)
}

func (r *recordingService) Stop(product string) error {
	r.record("Stop", product)
	return r.inner.Stop(product)
}

func (r *recordingService) Start(product string) error {
	r.record("Start", product)
	return r.inner.Start(product)
}

func TestRunServiceCallsUseThisProductOnly(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			ts := releaseServer(t, product, "fresh-binary")
			dir := t.TempDir()
			pinExecutableDir(t, dir)

			active := true
			rec := &recordingService{
				inner: FuncService{
					IsActiveFn:    func(string) (bool, error) { return active, nil },
					IsInstalledFn: func(string) (bool, error) { return true, nil },
					StopFn:        func(string) error { active = false; return nil },
					StartFn:       func(string) error { active = true; return nil },
				},
			}
			err := Run(context.Background(), RunOpts{
				Product:      product,
				LocalVersion: "0.1.0",
				Yes:          true,
				APIURL:       ts.URL,
				Out:          &bytes.Buffer{},
				Err:          &bytes.Buffer{},
				Service:      rec,
			})
			if err != nil {
				t.Fatal(err)
			}
			other := "mcrelay"
			if product == "mcrelay" {
				other = "mcremote"
			}
			if len(rec.calls) == 0 {
				t.Fatal("expected service probes")
			}
			for _, c := range rec.calls {
				if !strings.HasSuffix(c, ":"+product) {
					t.Errorf("call %q is not for %s", c, product)
				}
				if strings.HasSuffix(c, ":"+other) {
					t.Errorf("call %q mentions the other product", c)
				}
			}
		})
	}
}

// The refresher runs on the real update path, against the freshly swapped
// binary — not the one performing the update.
func TestRunPassesRefresherToSwap(t *testing.T) {
	ts := releaseServer(t, "mcrelay", "fresh-relay")
	dir := t.TempDir()
	pinExecutableDir(t, dir)

	var order []string
	ref := &fakeRefresher{log: &order}
	active := true
	err := Run(context.Background(), RunOpts{
		Product:      "mcrelay",
		LocalVersion: "0.1.0",
		Yes:          true,
		APIURL:       ts.URL,
		Out:          &bytes.Buffer{},
		Err:          &bytes.Buffer{},
		Refresher:    ref,
		Service: FuncService{
			IsActiveFn:    func(string) (bool, error) { return active, nil },
			IsInstalledFn: func(string) (bool, error) { return true, nil },
			StopFn:        func(string) error { active = false; return nil },
			StartFn:       func(string) error { order = append(order, "start"); active = true; return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.refreshed != 1 {
		t.Fatalf("RefreshUnit called %d times, want 1", ref.refreshed)
	}
	if strings.Join(order, ",") != "refresh,start" {
		t.Fatalf("order = %v, want refresh,start", order)
	}
}
