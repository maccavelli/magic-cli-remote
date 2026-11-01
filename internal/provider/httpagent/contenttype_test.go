package httpagent

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// MADR 0214 D12: an engine that answers a 1.x path with something other than
// what that path serves fails loudly. OpenCode v2 answers every 1.x path with
// its web shell's HTML and a 200, and before D12 the health probe, the event
// stream and the API all accepted it (0220-REPORT, executive assessment).
//
// The kilo and OpenCode 1.x dialects both serve health at /global/health and
// events at /global/event, so one stub with those paths covers both.

const (
	helperBadHealthEnv = "MC_HELPER_BAD_HEALTH"
	helperExitCodeEnv  = "MC_HELPER_EXIT_CODE"
	webShellHTML       = "<!doctype html><html><head><title>OpenCode</title></head></html>"
)

// globalPathsDialect has the kilo and OpenCode 1.x health and event paths.
type globalPathsDialect struct{ fakeDialect }

func (d *globalPathsDialect) HealthPath() string { return "/global/health" }
func (d *globalPathsDialect) EventsPath() string { return "/global/event" }

// badHealthDialect spawns TestHelperProcessServesBadHealth as its engine.
type badHealthDialect struct{ globalPathsDialect }

func (d *badHealthDialect) ServeArgs(port int) []string {
	return []string{"-test.run=^TestHelperProcessServesBadHealth$", strconv.Itoa(port)}
}

// TestHelperProcessServesBadHealth is not a test. It is an engine whose health
// answer is a 200 that is not health: the web shell's HTML, or JSON without a
// version. It runs only when armed and spawned by startServer, which sets
// procutil.EnvEngineID (the idiom of TestHelperProcessExitsNonZero).
func TestHelperProcessServesBadHealth(t *testing.T) {
	mode := os.Getenv(helperBadHealthEnv)
	if mode == "" || os.Getenv(procutil.EnvEngineID) == "" {
		return
	}
	port := flag.Arg(0)
	if port == "" {
		os.Exit(3)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(webShellHTML))
		case "noversion":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"healthy":true}`))
		}
	})
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
}

func TestStartServerRefusesAnHTMLHealthAnswer(t *testing.T) {
	t.Setenv(helperBadHealthEnv, "html")
	p := NewWithLogger(&badHealthDialect{}, Config{Bin: testBin(t)}, nil)
	_, err := p.startServer(context.Background())
	if err == nil {
		p.Shutdown()
		t.Fatal("startServer accepted an HTML page as health")
	}
	for _, want := range []string{`Content-Type "text/html; charset=utf-8"`, "want application/json", "<!doctype html>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng != nil {
		t.Error("the refused engine was published")
	}
}

func TestStartServerRefusesAHealthAnswerWithoutAVersion(t *testing.T) {
	t.Setenv(helperBadHealthEnv, "noversion")
	p := NewWithLogger(&badHealthDialect{}, Config{Bin: testBin(t)}, nil)
	_, err := p.startServer(context.Background())
	if err == nil {
		p.Shutdown()
		t.Fatal("startServer accepted a health answer that names no version")
	}
	if !strings.Contains(err.Error(), "reported no version") {
		t.Errorf("error = %q, want it to say the engine reported no version", err)
	}
}

func TestEventStreamRefusesAnHTMLAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(webShellHTML))
	}))
	defer srv.Close()
	p := NewWithLogger(&globalPathsDialect{}, Config{}, nil)
	err := p.streamOnce(srv.URL, 1)
	if err == nil {
		t.Fatal("the event stream accepted an HTML page")
	}
	for _, want := range []string{"GET /global/event", `Content-Type "text/html; charset=utf-8"`, "want text/event-stream", "<!doctype html>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestAPIRefusesAnAnswerThatIsNotJSON(t *testing.T) {
	cases := []struct {
		contentType string
		wantErr     bool
	}{
		{"text/html; charset=utf-8", true},
		{"text/plain", true},
		{"", true},
		{"application/json", false},
		{"application/json; charset=utf-8", false},
		{"application/problem+json", false},
	}
	for _, c := range cases {
		t.Run(c.contentType, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.contentType != "" {
					w.Header()["Content-Type"] = []string{c.contentType}
				} else {
					w.Header()["Content-Type"] = nil // no sniffing: the header is absent
				}
				_, _ = w.Write([]byte(`{"id":"ses_1"}`))
			}))
			defer srv.Close()
			p := NewWithLogger(&globalPathsDialect{}, Config{}, nil)
			var out struct {
				ID string `json:"id"`
			}
			err := p.apiAt(srv.URL)(context.Background(), http.MethodGet, "/session", nil, &out)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "GET /session answered Content-Type") {
				t.Errorf("error = %q, want it to name the request and the content type", err)
			}
		})
	}
}

// A request whose answer is not decoded is not held to a content type: there is
// nothing to misread.
func TestAPIDoesNotCheckAnAnswerItDoesNotDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("true"))
	}))
	defer srv.Close()
	p := NewWithLogger(&globalPathsDialect{}, Config{}, nil)
	if err := p.apiAt(srv.URL)(context.Background(), http.MethodPost, "/session/x/abort", nil, nil); err != nil {
		t.Fatalf("an undecoded answer was refused: %v", err)
	}
}

// TestHelperProcessExitsWith is not a test. It exits with the status in
// MC_HELPER_EXIT_CODE, for TestOrderlyExit.
func TestHelperProcessExitsWith(t *testing.T) {
	code := os.Getenv(helperExitCodeEnv)
	if code == "" {
		return
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		os.Exit(3)
	}
	os.Exit(n)
}

func TestOrderlyExit(t *testing.T) {
	for _, c := range []struct {
		code    int
		orderly bool
	}{
		{0, true},
		{130, true},
		{1, false},
		{2, false},
	} {
		cmd := exec.Command(testBin(t), "-test.run=^TestHelperProcessExitsWith$")
		cmd.Env = append(os.Environ(), helperExitCodeEnv+"="+strconv.Itoa(c.code))
		err := cmd.Run()
		if got := exitCode(err); got != c.code {
			t.Fatalf("helper exited %d, want %d (err %v)", got, c.code, err)
		}
		if got := orderlyExit(err); got != c.orderly {
			t.Errorf("orderlyExit(exit %d) = %v, want %v", c.code, got, c.orderly)
		}
	}
	if orderlyExit(context.Canceled) {
		t.Error("an error that is not an exit status counted as orderly")
	}
}
