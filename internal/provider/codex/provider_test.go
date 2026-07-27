package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestMain doubles as the app-server process for
// TestProviderInitializesBeforeTimingOut, which re-execs this binary. Serving
// the handshake from here rather than from a test body means the reply is
// immediate no matter how many (or how slow) the package's other tests are —
// as a test it would have queued behind all of them.
func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_CODEX_APP_SERVER_HELPER") == "1" {
		runAppServerHelper()
		return
	}
	os.Exit(m.Run())
}

// runAppServerHelper is a minimal app-server: it replies immediately to
// initialize, which verifies that startEngine has a reader active before it
// waits on the handshake response.
func runAppServerHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		if request.Method != "initialize" {
			continue
		}
		response := map[string]any{
			"id":     json.RawMessage(request.ID),
			"result": map[string]string{"codexHome": "/tmp/codex"},
		}
		if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func TestProviderInitializesBeforeTimingOut(t *testing.T) {
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	p := New(Config{Bin: os.Args[0]})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fr, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	if fr == nil {
		t.Fatal("ensure engine returned nil connection")
	}
	p.Shutdown()
}
