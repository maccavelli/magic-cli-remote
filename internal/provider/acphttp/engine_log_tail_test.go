package acphttp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestNewestLogUnder(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026-08-05")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(day, "old.log")
	newer := filepath.Join(day, "new.log")
	if err := os.WriteFile(old, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure distinct mtimes.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-log ignored.
	if err := os.WriteFile(filepath.Join(day, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := newestLogUnder(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != newer {
		t.Fatalf("newest = %q, want %q", got, newer)
	}
}

// TestTailGooseFileLogsSurfacesQuota pins the MADR 0073 gap: goose writes
// 429/quota to ~/.local/state/goose/logs/cli, not stderr. The file tailer
// must feed those lines into onEngineLogLine so a hanging session/prompt
// ends with a classified TypeError.
func TestTailGooseFileLogsSurfacesQuota(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026-08-05")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(day, "session.log")
	// Pre-create with a stale line that must NOT fire (seek-to-EOF on attach).
	stale := `{"level":"WARN","fields":{"message":"stale Weekly usage limit reached"}}` + "\n"
	if err := os.WriteFile(logPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newTestSession(t)
	s.p.spec.ID = provider.IDGoose
	s.p.sessions = map[string]*session{s.localID: s}
	gate := make(chan struct{})
	s.withFramer(&fakeFramer{
		blockOn: map[string]chan struct{}{"session/prompt": gate},
	})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	waitTurnBusy(t, s)

	attached := make(chan struct{})
	s.p.gooseTailAttached = func(path string, offset int64) {
		if path != logPath {
			return
		}
		select {
		case <-attached:
		default:
			close(attached)
		}
	}
	t.Cleanup(func() { s.p.gooseTailAttached = nil })

	stop := make(chan struct{})
	defer close(stop)
	go s.p.tailGooseFileLogs(root, stop)

	select {
	case <-attached:
	case <-time.After(2 * time.Second):
		t.Fatal("tailer never attached and seeked to EOF")
	}

	// Append a real goose-shaped 429 line (post-attach).
	fresh := `{"timestamp":"2026-08-05T23:51:12Z","level":"WARN","fields":{"message":"Provider request failed with status: 429 Too Many Requests. Payload: {\"error\":{\"type\":\"GoUsageLimitError\",\"message\":\"Weekly usage limit reached. Resets in 4 days.\"}}"},"target":"goose_providers::http_status"}` + "\n"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(fresh); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	ev := recvType(t, s, event.TypeError)
	if ev.ErrorKind != "quota" {
		t.Fatalf("ErrorKind = %q, want quota (msg %q)", ev.ErrorKind, ev.Error)
	}
	if !strings.Contains(strings.ToLower(ev.Error), "usage limit") &&
		!strings.Contains(strings.ToLower(ev.Error), "quota") {
		t.Fatalf("error text not natural-language limit: %q", ev.Error)
	}
	close(gate)
}

func waitTurnBusy(t *testing.T, s *session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		busy := s.turnBusy && s.turnCancel != nil
		s.mu.Unlock()
		if busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never became busy")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLooksLikeLongBackoffRetryDelaySome(t *testing.T) {
	// Shared agenterr helper — pin the forms providers scrape from logs.
	if !agenterr.LooksLikeLongBackoff(`retrying (1/3): RateLimitExceeded { details: "…", retry_delay: Some(3600s) }`) {
		t.Fatal("expected retry_delay: Some(3600s) to count as long backoff")
	}
	if agenterr.LooksLikeLongBackoff(`retry_delay: Some(5s)`) {
		t.Fatal("short retry_delay should not count as long backoff")
	}
	if !agenterr.LooksLikeLongBackoff("Backing off for 3600s before retry") {
		t.Fatal("prose backoff form still required")
	}
}
