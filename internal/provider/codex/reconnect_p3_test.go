package codex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestReconnectReplacementGenerationReconcilesOnce(t *testing.T) {
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	requestLog := t.TempDir() + "/requests.log"
	t.Setenv("CODEX_HELPER_REQUEST_LOG", requestLog)
	p := NewWithLogger(Config{Bin: os.Args[0], ReconnectAttempts: 3}, testLogger(t))
	p.sleepFn = func(context.Context, time.Duration) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("start first generation: %v", err)
	}
	p.mu.Lock()
	first := p.eng
	p.mu.Unlock()
	s := newSession(p, p.cfg, provider.StartOptions{}, testLogger(t))
	s.agentID = "thread-reconnect"
	s.engineGeneration = first.generation
	s.pendingPerms["permission-stale"] = pendingCallback{}
	s.pendingQuestions["question-stale"] = pendingQuestion{}
	p.mu.Lock()
	p.sessions[s.agentID] = s
	p.mu.Unlock()

	if err := first.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill first engine: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.mu.Lock()
		eng := p.eng
		ready := eng != nil && eng.ready
		generation := 0
		if eng != nil {
			generation = eng.generation
		}
		p.mu.Unlock()
		if ready && generation == first.generation+1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement engine did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	closed := s.closed
	generation := s.engineGeneration
	permissions := len(s.pendingPerms)
	questions := len(s.pendingQuestions)
	s.mu.Unlock()
	if closed || generation != first.generation+1 {
		t.Fatalf("session closed/generation = %v/%d", closed, generation)
	}
	if permissions != 0 || questions != 0 {
		t.Fatalf("stale callbacks remain: permissions=%d questions=%d", permissions, questions)
	}
	raw, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatalf("read request log: %v", err)
	}
	if strings.Count(string(raw), "loaded/list\n") != 1 || strings.Count(string(raw), "thread/resume\n") != 1 {
		t.Fatalf("reconciliation log = %q", raw)
	}

	p.Shutdown()
	_ = s.Close(context.Background())
}
