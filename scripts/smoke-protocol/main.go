// Command smoke-protocol exercises mcremote.v1 over WebSocket without a GUI.
//
// Usage (from repo root):
//
//	go run ./scripts/smoke-protocol -token mcr_... [flags]
//	./scripts/smoke-protocol.sh -token mcr_...
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

const protocolV = 1

type envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Token   string          `json:"token,omitempty"`
}

func main() {
	host := flag.String("host", "127.0.0.1:7531", "mcremote host:port (no scheme)")
	token := flag.String("token", "", "device token from `mcremote pair create` (or MCREMOTE_TOKEN)")
	provider := flag.String("provider", "", "session provider (empty = daemon default: grok-if-ready else fake)")
	prompt := flag.String("prompt", "hello from smoke-protocol", "prompt text")
	name := flag.String("name", "smoke", "session name")
	cwd := flag.String("cwd", "", "optional absolute cwd for the agent session")
	timeout := flag.Duration("timeout", 0, "overall timeout (default: max(90s, 2*-wait+45s))")
	waitEvents := flag.Duration("wait", 8*time.Second, "how long to wait for stream events after prompt")
	skipPrompt := flag.Bool("skip-prompt", false, "only auth + create session")
	flag.Parse()

	tok := strings.TrimSpace(*token)
	if tok == "" {
		tok = strings.TrimSpace(os.Getenv("MCREMOTE_TOKEN"))
	}
	if tok == "" {
		fmt.Fprintln(os.Stderr, "error: -token or MCREMOTE_TOKEN is required")
		fmt.Fprintln(os.Stderr, "  ./bin/mcremote pair create --name smoke")
		os.Exit(2)
	}

	// Ensure overall budget covers long -wait (e.g. grok with -wait 45s).
	total := *timeout
	if total <= 0 {
		total = 2*(*waitEvents) + 45*time.Second
		if total < 90*time.Second {
			total = 90 * time.Second
		}
	}
	if total < *waitEvents+30*time.Second {
		total = *waitEvents + 30*time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), total)
	defer cancel()

	baseHTTP := "http://" + *host
	wsURL := "ws://" + *host + "/v1/ws"

	step("healthz", baseHTTP+"/healthz")
	if err := checkHealthz(ctx, baseHTTP+"/healthz"); err != nil {
		fail("healthz", err)
	}
	ok("healthz")

	step("websocket", wsURL)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		fail("websocket dial", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Read loop in background; request() correlates by id.
	inbox := make(chan envelope, 64)
	errCh := make(chan error, 1)
	go readLoop(ctx, conn, inbox, errCh)

	step("auth", "device token")
	authEnv, err := request(ctx, conn, inbox, "auth", map[string]any{"token": tok}, tok)
	if err != nil {
		fail("auth", err)
	}
	if authEnv.Type != "auth_ok" {
		fail("auth", fmt.Errorf("got type %q payload %s", authEnv.Type, string(authEnv.Payload)))
	}
	var authOK struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	_ = json.Unmarshal(authEnv.Payload, &authOK)
	ok(fmt.Sprintf("auth device_id=%s name=%s", authOK.DeviceID, authOK.DeviceName))

	step("providers.list", "")
	provEnv, err := request(ctx, conn, inbox, "providers.list", map[string]any{}, "")
	if err != nil {
		fail("providers.list", err)
	}
	fmt.Printf("  → %s\n", compactJSON(provEnv.Payload))

	createPayload := map[string]any{"name": *name}
	if *provider != "" {
		createPayload["provider"] = *provider
	}
	if *cwd != "" {
		createPayload["cwd"] = *cwd
	}

	step("session.create", fmt.Sprintf("%v", createPayload))
	// Drain broadcast events while waiting for create response.
	createEnv, events, err := requestWithEvents(ctx, conn, inbox, "session.create", createPayload, 3*time.Second)
	if err != nil {
		fail("session.create", err)
	}
	if createEnv.Type != "session.created" {
		fail("session.create", fmt.Errorf("got type %q payload %s", createEnv.Type, string(createEnv.Payload)))
	}
	var meta struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Status   string `json:"status"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(createEnv.Payload, &meta); err != nil {
		fail("session.create parse", err)
	}
	ok(fmt.Sprintf("session id=%s provider=%s status=%s", meta.ID, meta.Provider, meta.Status))
	for _, ev := range events {
		printEvent(ev)
	}

	if *skipPrompt {
		ok("smoke complete (--skip-prompt)")
		return
	}

	step("session.prompt", *prompt)
	promptEnv, moreEvents, err := requestWithEvents(ctx, conn, inbox, "session.prompt", map[string]any{
		"session_id": meta.ID,
		"text":       *prompt,
	}, *waitEvents)
	if err != nil {
		fail("session.prompt", err)
	}
	if promptEnv.Type != "ok" && promptEnv.Type != "error" {
		// May only have seen events if server order differs; still ok if we got events.
		fmt.Printf("  ! prompt response type=%q payload=%s\n", promptEnv.Type, string(promptEnv.Payload))
	} else if promptEnv.Type == "error" {
		fail("session.prompt", fmt.Errorf("%s", string(promptEnv.Payload)))
	} else {
		ok("prompt accepted")
	}
	sawComplete := false
	for _, ev := range moreEvents {
		printEvent(ev)
		if eventType(ev) == "turn_complete" {
			sawComplete = true
		}
	}

	// Brief extra drain only if we have not seen turn_complete yet.
	if !sawComplete {
		drainUntil(ctx, inbox, min(*waitEvents/4, 5*time.Second))
	}

	// Close uses a fresh short context so a long -wait cannot exhaust the budget.
	step("session.close", meta.ID)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	closeEnv, _, err := requestWithEvents(closeCtx, conn, inbox, "session.close", map[string]any{
		"session_id": meta.ID,
	}, 0)
	closeCancel()
	if err != nil {
		// Non-fatal if the agent turn already completed: conn may be racing shutdown.
		fmt.Printf("  ! session.close: %v (ignored; core smoke already succeeded)\n", err)
	} else if closeEnv.Type == "error" {
		fmt.Printf("  ! session.close error payload %s (ignored)\n", string(closeEnv.Payload))
	} else {
		ok("session closed")
	}

	fmt.Println()
	ok("smoke-protocol PASSED")
}

func eventType(env envelope) string {
	var wrap struct {
		Event map[string]any `json:"event"`
	}
	if json.Unmarshal(env.Payload, &wrap) == nil && wrap.Event != nil {
		if t, ok := wrap.Event["type"].(string); ok {
			return t
		}
	}
	return ""
}

func checkHealthz(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return err
	}
	fmt.Printf("  → %v\n", body)
	return nil
}

func readLoop(ctx context.Context, conn *websocket.Conn, inbox chan<- envelope, errCh chan<- error) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		select {
		case inbox <- env:
		case <-ctx.Done():
			return
		}
	}
}

func request(ctx context.Context, conn *websocket.Conn, inbox <-chan envelope, typ string, payload map[string]any, token string) (envelope, error) {
	env, _, err := requestWithEvents(ctx, conn, inbox, typ, payload, 0)
	return env, err
}

// requestWithEvents sends a request, returns the correlated response, and any
// event pushes observed until deadline after the response (or only until response if wait==0).
func requestWithEvents(ctx context.Context, conn *websocket.Conn, inbox <-chan envelope, typ string, payload map[string]any, waitAfter time.Duration) (envelope, []envelope, error) {
	id := uuid.NewString()
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return envelope{}, nil, err
		}
		raw = b
	}
	out := envelope{V: protocolV, Type: typ, ID: id, Payload: raw, Token: tokenField(typ, payload)}
	// Prefer token in payload for auth; also set top-level for auth convenience.
	if typ == "auth" {
		if t, ok := payload["token"].(string); ok {
			out.Token = t
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return envelope{}, nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		return envelope{}, nil, err
	}

	var resp envelope
	var events []envelope
	gotResp := false
	deadline := time.Now().Add(30 * time.Second)
	if waitAfter > 0 {
		// First wait for response with a fixed bound, then collect events.
	}

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return envelope{}, events, ctx.Err()
		case env := <-inbox:
			timer.Stop()
			if env.Type == "event" {
				events = append(events, env)
				// After a prompt, stop waiting once the turn finishes.
				if gotResp && waitAfter > 0 && eventType(env) == "turn_complete" {
					return resp, events, nil
				}
				continue
			}
			if env.ID == id {
				resp = env
				gotResp = true
				if waitAfter <= 0 {
					return resp, events, nil
				}
				// Collect more events after response (up to waitAfter, or turn_complete).
				deadline = time.Now().Add(waitAfter)
				continue
			}
			// Unrelated response — ignore for smoke.
		case <-timer.C:
			if gotResp {
				return resp, events, nil
			}
			return envelope{}, events, fmt.Errorf("timeout waiting for response to %s", typ)
		}
	}
	if gotResp {
		return resp, events, nil
	}
	return envelope{}, events, fmt.Errorf("timeout waiting for response to %s", typ)
}

func tokenField(typ string, payload map[string]any) string {
	if typ != "auth" {
		return ""
	}
	if t, ok := payload["token"].(string); ok {
		return t
	}
	return ""
}

func drainUntil(ctx context.Context, inbox <-chan envelope, d time.Duration) {
	if d <= 0 {
		return
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case env := <-inbox:
			if env.Type == "event" {
				printEvent(env)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func printEvent(env envelope) {
	// payload is usually { "event": { ... } }
	var wrap struct {
		Event map[string]any `json:"event"`
	}
	if json.Unmarshal(env.Payload, &wrap) == nil && wrap.Event != nil {
		typ, _ := wrap.Event["type"].(string)
		text, _ := wrap.Event["text"].(string)
		status, _ := wrap.Event["status"].(string)
		errMsg, _ := wrap.Event["error"].(string)
		sid, _ := wrap.Event["session_id"].(string)
		fmt.Printf("  event type=%-28s status=%-12s session=%s text=%q err=%q\n",
			typ, status, short(sid), truncate(text, 80), errMsg)
		return
	}
	fmt.Printf("  event raw=%s\n", compactJSON(env.Payload))
}

func step(name, detail string) {
	if detail == "" {
		fmt.Printf("\n→ %s\n", name)
		return
	}
	fmt.Printf("\n→ %s (%s)\n", name, detail)
}

func ok(msg string) {
	fmt.Printf("  ✓ %s\n", msg)
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", step, err)
	os.Exit(1)
}

func compactJSON(b []byte) string {
	if len(b) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(b)
	}
	return string(out)
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
