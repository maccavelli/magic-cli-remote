package acpagent

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// countingBuf lets the test observe how many times EnsureWarm got past its
// early returns and actually tried to spawn.
type countingBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *countingBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *countingBuf) count(substr string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), substr)
}

func (b *countingBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// rearmProvider builds a Provider whose prewarm is enabled and whose binary
// exists but exits immediately.
//
// That combination is what makes the call countable: EnsureWarm's early
// returns are `!cfg.Prewarm` and `!Ready()`, so both must pass, and the spawn
// that follows fails fast and logs exactly one "prewarm failed" per call. No
// production seam is added for the test to look through.
func rearmProvider(t *testing.T) (*Provider, *countingBuf) {
	t.Helper()
	testexec.SkipIfNoPOSIXModes(t)
	stub := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "agent-stub"), "exit 0")
	buf := &countingBuf{}
	return &Provider{
		log: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		cfg: Config{Bin: stub, Prewarm: true, DefaultCWD: t.TempDir()},
	}, buf
}

func waitForCount(t *testing.T, buf *countingBuf, substr string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := buf.count(substr); got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wanted %d %q, got %d", want, substr, buf.count(substr))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSpareIsRearmedAtTurnEndNotTurnStart is the F5 regression (MADR 0137).
//
// Re-arming used to be `defer p.EnsureWarm()` in Start, which put a full
// ~3.8 s replacement spawn — process launch, ACP initialize, model-catalog
// harvest — in flight at the same moment as the user's first prompt, competing
// with the turn they were waiting on. The spare is for the NEXT session; there
// is no reason for it to be built while someone is waiting.
//
// The assertion is ordering, not merely occurrence: nothing arms while the
// turn is in flight, and exactly one arm happens once it ends.
func TestSpareIsRearmedAtTurnEndNotTurnStart(t *testing.T) {
	p, buf := rearmProvider(t)
	s := newQueueTestSession()
	s.provider = p

	hold := make(chan struct{})
	s.testSubmit = func(ctx context.Context, _ []acp.ContentBlock) (acp.PromptResponse, error) {
		select {
		case <-hold:
		case <-ctx.Done():
			return acp.PromptResponse{}, ctx.Err()
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}

	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}

	// The turn is in flight. Give a stray arm time to appear.
	time.Sleep(250 * time.Millisecond)
	if got := buf.count("prewarm failed"); got != 0 {
		t.Fatalf("the spare was armed %d time(s) while the turn was still "+
			"running; that spawn competes with the turn the user is waiting on", got)
	}

	close(hold)
	waitForCount(t, buf, "prewarm failed", 1)

	// And exactly one: a turn must not arm repeatedly.
	time.Sleep(250 * time.Millisecond)
	if got := buf.count("prewarm failed"); got != 1 {
		t.Fatalf("the spare was armed %d times for one turn, want exactly 1", got)
	}
}

// TestAFailedTurnStillRearmsTheSpare covers the exit paths the happy path does
// not.
//
// The re-arm sits in the turn goroutine's deferred cleanup rather than beside
// the turn_complete emit, because a session whose first turn errored would
// otherwise be left with no spare at all — which is the state prewarm exists
// to avoid, reached exactly when the engine is already having trouble.
func TestAFailedTurnStillRearmsTheSpare(t *testing.T) {
	p, buf := rearmProvider(t)
	s := newQueueTestSession()
	s.provider = p

	s.testSubmit = func(context.Context, []acp.ContentBlock) (acp.PromptResponse, error) {
		return acp.PromptResponse{}, context.DeadlineExceeded
	}
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, buf, "prewarm failed", 1)

	// Drain so the session's event channel cannot block the turn goroutine.
	for len(s.events) > 0 {
		<-s.events
	}
	_ = event.TypeError
}

// TestATurnOnASessionWithNoProviderDoesNotPanic is the regression for the
// defect that reached master (CI run 33811336395).
//
// The turn-end re-arm added in MADR 0137 F5 dereferenced s.provider, which is
// nil on every session built as a test fixture. The panic was swallowed by the
// turn goroutine's own recover, so four tests in this package kept passing
// while their turn goroutines died mid-cleanup — the local suite was green and
// only a -race CI run surfaced it.
//
// The assertion is not "no panic": a recovered panic is invisible to that.
// It is that the cleanup AFTER the re-arm point completes, which a dead
// goroutine cannot do.
func TestATurnOnASessionWithNoProviderDoesNotPanic(t *testing.T) {
	buf := &countingBuf{}
	s := newQueueTestSession()
	s.log = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if s.provider != nil {
		t.Fatal("this test is only meaningful with a nil provider")
	}

	s.testSubmit = func(context.Context, []acp.ContentBlock) (acp.PromptResponse, error) {
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
	}
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hi"}}); err != nil {
		t.Fatal(err)
	}

	// The turn goroutine must reach the end of its deferred cleanup: prompting
	// is cleared there, after the re-arm.
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		done := !s.prompting
		s.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the turn goroutine never finished its cleanup")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if n := buf.count("agent turn handler panic"); n != 0 {
		t.Fatalf("the turn goroutine panicked %d time(s):\n%s", n, buf.String())
	}
}
