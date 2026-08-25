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
	if path := os.Getenv("CODEX_HELPER_LAUNCH_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString("launch\n")
			_ = f.Close()
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		switch request.Method {
		case "initialize":
			if path := os.Getenv("CODEX_HELPER_INIT_LOG"); path != "" {
				_ = os.WriteFile(path, request.Params, 0o600)
			}
			if os.Getenv("CODEX_HELPER_EOF_ON_INIT") == "1" {
				os.Exit(0)
			}
			var params struct {
				Capabilities struct {
					ExperimentalAPI bool `json:"experimentalApi"`
				} `json:"capabilities"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if os.Getenv("CODEX_HELPER_REJECT_EXPERIMENTAL") == "1" && params.Capabilities.ExperimentalAPI {
				writeRPCError(request.ID, -32600, "experimental API is not enabled", map[string]string{
					"capability": "experimentalApi",
				})
				continue
			}
			if os.Getenv("CODEX_HELPER_REJECT_INIT") == "1" {
				writeRPCError(request.ID, -32600, "invalid clientInfo", nil)
				continue
			}
			response := map[string]any{
				"id": json.RawMessage(request.ID),
				"result": map[string]string{
					"codexHome": "/tmp/codex", "userAgent": "codex-test/0.149.1",
					"platformFamily": "unix", "platformOs": "linux",
				},
			}
			if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
				os.Exit(3)
			}
		case "collaborationMode/list":
			if path := os.Getenv("CODEX_HELPER_LIST_LOG"); path != "" {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = f.WriteString("list\n")
					_ = f.Close()
				}
			}
			if os.Getenv("CODEX_HELPER_REJECT_EXPERIMENTAL") == "1" && os.Getenv("CODEX_HELPER_COLLAB") == "" {
				writeRPCError(request.ID, -32601, "Method not found", nil)
				continue
			}
			switch os.Getenv("CODEX_HELPER_COLLAB") {
			case "notfound":
				writeRPCError(request.ID, -32601, "Method not found", nil)
			case "gate":
				writeRPCError(request.ID, -32600, "experimental method requires experimentalApi", map[string]string{
					"capability": "experimentalApi",
				})
			case "malformed":
				response := map[string]any{
					"id": json.RawMessage(request.ID),
					"result": map[string]any{
						"data": []map[string]any{{"name": "Default", "mode": "default"}},
					},
				}
				if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
					os.Exit(3)
				}
			default:
				response := map[string]any{
					"id": json.RawMessage(request.ID),
					"result": map[string]any{
						"data": []map[string]any{
							{"name": "Plan", "mode": "plan", "model": nil, "reasoning_effort": "medium"},
							{"name": "Default", "mode": "default", "model": nil, "reasoning_effort": nil},
						},
					},
				}
				if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
					os.Exit(3)
				}
			}
		}
	}
	os.Exit(0)
}

func writeRPCError(id json.RawMessage, code int, message string, data any) {
	errBody := map[string]any{"code": code, "message": message}
	if data != nil {
		errBody["data"] = data
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"id":    json.RawMessage(id),
		"error": errBody,
	})
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

func TestInitializeRetainsPlatformMetadata(t *testing.T) {
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	defer p.Shutdown()

	p.mu.Lock()
	metadata := p.eng.initialize
	p.mu.Unlock()
	if metadata.UserAgent != "codex-test/0.149.1" || metadata.PlatformFamily != "unix" || metadata.PlatformOS != "linux" {
		t.Fatalf("initialize metadata = %+v", metadata)
	}
}
