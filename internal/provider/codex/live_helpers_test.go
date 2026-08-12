//go:build live_codex || live_codex_turn || live_codex_review

package codex

import (
	"context"
	"strings"
	"testing"
	"time"
)

func liveEngine(t *testing.T) (*conn, func()) {
	t.Helper()
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fr, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("engine start: %v", err)
	}
	return fr, p.Shutdown
}

func isParamError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "-32600") || strings.Contains(s, "Invalid request")
}
