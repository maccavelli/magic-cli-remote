package codex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestReconnectRetryNotInitializedReadOnlyOnly(t *testing.T) {
	var calls int
	send := func(context.Context, string, any) (json.RawMessage, error) {
		calls++
		if calls < 3 {
			return nil, &rpcErrorBody{Code: -32001, Message: "Server overloaded; retry later."}
		}
		return json.RawMessage(`{"data":[]}`), nil
	}
	sleep := func(context.Context, time.Duration) error { return nil }
	if _, err := sendWithOverloadRetry(context.Background(), "thread/list", map[string]any{}, true, send, sleep); err != nil {
		t.Fatalf("read-only retry: %v", err)
	}
	if calls != 3 {
		t.Fatalf("read-only calls = %d, want 3", calls)
	}

	calls = 0
	_, err := sendWithOverloadRetry(context.Background(), "thread/start", map[string]any{}, false, send, sleep)
	var rpcErr *rpcErrorBody
	if !errors.As(err, &rpcErr) || calls != 1 {
		t.Fatalf("mutation err/calls = %v/%d, want original error/1", err, calls)
	}
}
