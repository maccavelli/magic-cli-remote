package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestServeContextEndsOnTheShutdownRequest: the admin socket's shutdown
// operation cancels the context serve runs the daemon under, with its own
// cause, which the daemon logs as the shutdown reason (MADR 0054 A2).
func TestServeContextEndsOnTheShutdownRequest(t *testing.T) {
	ctx, requestShutdown, release := serveContext(context.Background())
	defer release()
	if ctx.Err() != nil {
		t.Fatal("the context ended before any signal or request")
	}
	requestShutdown()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the shutdown request did not end serve's context")
	}
	if got := context.Cause(ctx); !errors.Is(got, errShutdownRequested) {
		t.Errorf("cause = %v, want %v", got, errShutdownRequested)
	}
}

// TestServeContextFollowsItsParent: the parent's end still ends it, as
// before the request existed.
func TestServeContextFollowsItsParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, _, release := serveContext(parent)
	defer release()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("serve's context outlived its parent")
	}
}
