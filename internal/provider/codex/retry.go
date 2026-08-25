package codex

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"
)

var overloadRetryBackoffs = []time.Duration{250 * time.Millisecond, time.Second, 4 * time.Second}

type rpcRequestSender func(context.Context, string, any) (json.RawMessage, error)
type retrySleeper func(context.Context, time.Duration) error

func sendWithOverloadRetry(
	ctx context.Context,
	method string,
	params any,
	safe bool,
	send rpcRequestSender,
	sleep retrySleeper,
) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		raw, err := send(ctx, method, params)
		var rpcErr *rpcErrorBody
		if err == nil || !safe || !errors.As(err, &rpcErr) || rpcErr.Code != -32001 || attempt >= len(overloadRetryBackoffs) {
			return raw, err
		}
		backoff := overloadRetryBackoffs[attempt]
		jitter := time.Duration(rand.Int64N(int64(backoff/5) + 1))
		if err := sleep(ctx, backoff+jitter); err != nil {
			return nil, err
		}
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
