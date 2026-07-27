package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestCodexAppServerHelper is a minimal app-server process used by
// TestProviderInitializesBeforeTimingOut. It deliberately replies immediately
// to initialize, which verifies that startEngine has a reader active before it
// waits on the handshake response.
func TestCodexAppServerHelper(t *testing.T) {
	if os.Getenv("GO_WANT_CODEX_APP_SERVER_HELPER") != "1" {
		return
	}

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
