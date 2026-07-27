package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestConnSendRequestReadsResponse(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	c := newConn(stdoutW, stdinR, testLogger(t))

	var notifMu sync.Mutex
	var notifs []string
	onNotif := func(method string, _ json.RawMessage) {
		notifMu.Lock()
		notifs = append(notifs, method)
		notifMu.Unlock()
	}
	onReq := func(method string, _ json.RawMessage, _ json.RawMessage) {
		t.Errorf("unexpected server request: %s", method)
	}

	go c.readPump(onNotif, onReq)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Write request into conn's stdin (which is stdoutW).
	// Actually, conn reads from stdout (which is stdinR in our pipe naming),
	// and writes to stdin (which is stdoutW). Let me re-check:
	//   newConn(stdin io.Writer, stdout io.Reader, ...)
	//   c.stdin = stdin → writes to stdin of the process
	//   c.stdout = stdout → reads from stdout of the process
	// For our test pipes:
	//   stdinW → we write to it → c reads from it as "stdout"
	//   stdoutR → we read from it → c writes to it as "stdin"
	// Actually wait, let me be precise:
	//   newConn(stdoutW, stdinR, ...)
	//   c.stdin = stdoutW → when c.writeLine writes, it goes to stdoutW
	//   c.stdout = stdinR → when c reads, it reads from stdinR
	// So:
	//   We read from stdoutR to see what c wrote (c's "stdin" output = JSON-RPC to engine)
	//   We write to stdinW to send responses to c (c's "stdout" input = JSON-RPC from engine)

	resultCh := make(chan struct {
		result json.RawMessage
		err    error
	})
	go func() {
		r, err := c.sendRequest(ctx, "test/method", map[string]string{"key": "val"})
		resultCh <- struct {
			result json.RawMessage
			err    error
		}{r, err}
	}()

	// Read the JSON-RPC request that conn sent to its stdout (our stdoutR).
	var req struct {
		ID     int64           `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	dec := json.NewDecoder(stdoutR)
	if err := dec.Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if req.Method != "test/method" {
		t.Fatalf("method = %q, want test/method", req.Method)
	}
	if req.ID == 0 {
		t.Fatal("request id must not be zero")
	}

	// Send a response through conn's stdin (our stdinW).
	resp := map[string]any{
		"id":     req.ID,
		"result": map[string]string{"status": "ok"},
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	respData = append(respData, '\n')
	if _, err := stdinW.Write(respData); err != nil {
		t.Fatalf("write response: %v", err)
	}

	r := <-resultCh
	if r.err != nil {
		t.Fatalf("sendRequest: %v", r.err)
	}
	var result map[string]string
	if err := json.Unmarshal(r.result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("result.status = %q, want ok", result["status"])
	}

	notifMu.Lock()
	if len(notifs) > 0 {
		t.Errorf("unexpected notifications: %v", notifs)
	}
	notifMu.Unlock()

	stdinW.Close()
	stdoutR.Close()
	stdoutW.Close()
	stdinR.Close()
}

func TestConnSendRequestContextCancelled(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	c := newConn(stdoutW, stdinR, testLogger(t))

	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Drain the request from c's stdout so writeLine does not block.
	go func() {
		dec := json.NewDecoder(stdoutR)
		var req struct {
			ID int64 `json:"id"`
		}
		_ = dec.Decode(&req)
	}()

	_, err := c.sendRequest(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	stdinW.Close()
	stdoutR.Close()
	stdoutW.Close()
	stdinR.Close()
}

func TestConnDeliverResponseUnknownID(t *testing.T) {
	c := newConn(&bytes.Buffer{}, &bytes.Buffer{}, testLogger(t))
	c.deliverResponse(42, json.RawMessage(`"ok"`), nil)
}

func TestConnFailAllUnblocksPending(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	c := newConn(stdoutW, stdinR, testLogger(t))

	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})

	ctx := context.Background()
	resultCh := make(chan error)
	go func() {
		_, err := c.sendRequest(ctx, "test/method", nil)
		resultCh <- err
	}()

	// Drain the request from c's stdout so sendRequest lands in select.
	go func() {
		dec := json.NewDecoder(stdoutR)
		var req struct {
			ID int64 `json:"id"`
		}
		_ = dec.Decode(&req)
	}()

	time.Sleep(10 * time.Millisecond)

	c.failAll()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("expected error after failAll")
		}
	case <-time.After(time.Second):
		t.Fatal("sendRequest did not unblock after failAll")
	}

	stdinW.Close()
	stdoutR.Close()
	stdoutW.Close()
	stdinR.Close()
}

func TestConnSendNotification(t *testing.T) {
	c := newConn(&bytes.Buffer{}, &bytes.Buffer{}, testLogger(t))
	err := c.sendNotification(context.Background(), "test/notif", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("sendNotification: %v", err)
	}
}

func TestConnSendResponse(t *testing.T) {
	c := newConn(&bytes.Buffer{}, &bytes.Buffer{}, testLogger(t))
	err := c.sendResponse(context.Background(), json.RawMessage("1"), map[string]string{"status": "ok"}, nil)
	if err != nil {
		t.Fatalf("sendResponse: %v", err)
	}
}
