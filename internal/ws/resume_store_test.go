package ws

import (
	"errors"
	"testing"
	"time"
)

// 0070 F3 / P1.B — mint failure omits a token rather than sending "".

func TestResumeIssueOK(t *testing.T) {
	r := newResumeStore(30 * time.Second)
	token, window, ok := r.issue("dev-1", 0)
	if !ok || token == "" {
		t.Fatalf("issue = (%q, %v, %v), want non-empty ok token", token, window, ok)
	}
	if window != 30*time.Second {
		t.Fatalf("window = %v, want 30s", window)
	}
	if !r.validate("dev-1", token) {
		t.Fatal("fresh token must validate")
	}
}

func TestResumeIssueRandFailure(t *testing.T) {
	r := newResumeStore(60 * time.Second)
	r.readRand = func([]byte) (int, error) {
		return 0, errors.New("rng unavailable")
	}
	token, window, ok := r.issue("dev-1", 0)
	if ok || token != "" {
		t.Fatalf("issue on RNG failure = (%q, %v, %v), want empty !ok", token, window, ok)
	}
	if window != 60*time.Second {
		t.Fatalf("window = %v, want server default still reported", window)
	}
	if r.validate("dev-1", "anything") {
		t.Fatal("failed mint must not store an entry")
	}
	// Map must stay empty so a later successful mint is clean.
	r.mu.Lock()
	n := len(r.byDevice)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("byDevice size = %d, want 0 after failed mint", n)
	}
}

func TestResumeIssueNarrowsWindow(t *testing.T) {
	r := newResumeStore(120 * time.Second)
	_, window, ok := r.issue("dev-1", 30*time.Second)
	if !ok {
		t.Fatal("issue failed")
	}
	if window != 30*time.Second {
		t.Fatalf("window = %v, want 30s (client narrow request)", window)
	}
	_, window, ok = r.issue("dev-1", 10*time.Minute)
	if !ok {
		t.Fatal("issue failed")
	}
	if window != 120*time.Second {
		t.Fatalf("window = %v, want 120s clamp (never widen)", window)
	}
}

func TestResumePurgeExpired(t *testing.T) {
	r := newResumeStore(60 * time.Second)
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }

	tok, _, ok := r.issue("old", 0)
	if !ok || tok == "" {
		t.Fatal("issue old")
	}
	// Advance past expiry.
	now = now.Add(2 * time.Minute)
	r.now = func() time.Time { return now }

	if r.validate("old", tok) {
		t.Fatal("expired token must not validate")
	}
	// issue for another device triggers purge of expired entries.
	if _, _, ok := r.issue("new", 0); !ok {
		t.Fatal("issue new")
	}
	r.mu.Lock()
	_, still := r.byDevice["old"]
	n := len(r.byDevice)
	r.mu.Unlock()
	if still {
		t.Fatal("expired entry must be purged on issue")
	}
	if n != 1 {
		t.Fatalf("byDevice size = %d, want 1 (only new)", n)
	}
}
