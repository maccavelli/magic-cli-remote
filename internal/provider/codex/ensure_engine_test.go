package codex

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A caller that arrives while another goroutine owns the start gate must park
// until the engine is published rather than re-checking in a tight loop.
func TestEnsureEngineWaiterWakesWhenEngineArrives(t *testing.T) {
	p := New(Config{Bin: "codex"})
	p.mu.Lock()
	p.starting = true
	p.mu.Unlock()

	want := newConn(nil, nil, p.log)
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.mu.Lock()
		p.eng = &engine{conn: want}
		p.starting = false
		p.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	if got != want {
		t.Fatalf("got conn %p, want %p", got, want)
	}
}

// A start gate that never clears must not outlive the caller's deadline.
func TestEnsureEngineWaiterHonoursDeadline(t *testing.T) {
	p := New(Config{Bin: "codex"})
	p.mu.Lock()
	p.starting = true
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := p.ensureEngine(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
