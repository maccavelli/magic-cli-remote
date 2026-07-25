//go:build live_grok

package session_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Run with: go test -tags live_grok ./internal/session/ -run TestLiveCanonical -count=1
//
// End to end through the daemon against a real grok: the canonical commands
// resolve to grok's actual behaviour (MADR 0023). Unit tests cannot cover this —
// the point of the exercise is that grok's advertised catalog lies, so only the
// real agent can confirm the translation works and the refusal is warranted.
func TestLiveCanonicalCommandsThroughTheDaemon(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	reg := provider.NewRegistry()
	reg.Register(p)

	var sink eventSink
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	meta, err := mgr.Create(ctx, provider.IDGrok,
		provider.StartOptions{Name: "canon-e2e", CWD: t.TempDir()}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer mgr.Close(context.Background(), meta.ID, "dev-a")

	// The advertised list is what a client renders: grok's real answer, not the
	// canonical wish list.
	sink.waitForAdvertised(t, "context", true)
	sink.waitForAdvertised(t, "compact", false)
	if c, _ := sink.advertisedCommand(t, "compact"); !strings.Contains(c.Reason, "terminal UI") {
		t.Errorf("/compact reason = %q, want grok's own words", c.Reason)
	}

	// /context is translated to the command grok actually executes, so the answer
	// comes back as agent output.
	before := countAssistantChunks(sink.snapshot())
	if err := mgr.Prompt(ctx, meta.ID, "/context", nil, "dev-a"); err != nil {
		t.Fatalf("/context: %v", err)
	}
	waitFor(t, "assistant output for /context", func() bool {
		return countAssistantChunks(sink.snapshot()) > before
	})
	// And the transcript shows what the user typed, not the translation.
	if !hasUserMessage(sink.snapshot(), "/context") {
		t.Error("/context was not echoed as the user's own message")
	}

	// /compact refuses with the recorded reason instead of forwarding into
	// silence.
	if err := mgr.Prompt(ctx, meta.ID, "/compact", nil, "dev-a"); err != nil {
		t.Fatalf("/compact: %v", err)
	}
	sink.waitForNoticeContaining(t, "isn't available here")

	// /help is rendered from the same resolution the router uses.
	if err := mgr.Prompt(ctx, meta.ID, "/help", nil, "dev-a"); err != nil {
		t.Fatalf("/help: %v", err)
	}
	sink.waitForNoticeContaining(t, "Not here:")
	for _, n := range sink.notices() {
		t.Logf("notice: %s", n)
	}
}

func countAssistantChunks(evs []event.Event) int {
	n := 0
	for _, ev := range evs {
		if ev.Type == event.TypeAssistantChunk {
			n++
		}
	}
	return n
}

func hasUserMessage(evs []event.Event, text string) bool {
	for _, ev := range evs {
		if ev.Type == event.TypeUserMessage && ev.Text == text {
			return true
		}
	}
	return false
}
