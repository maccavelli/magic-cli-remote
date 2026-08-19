//go:build live_grok

package grok_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run Command -count=1
//
// Pins the finding the whole command framework rests on (MADR 0023),
// re-checked against grok 1.0.5: grok's advertised catalog is its TUI's, and
// only part of it answers over ACP. If a future grok starts executing /compact
// over the protocol, or stops answering /session-info, this test is what
// notices — the table would otherwise keep telling users something untrue.
func TestLiveGrokCommandTableMatchesReality(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "cmd-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	advertised := waitForCommands(t, s, 30*time.Second)
	t.Logf("grok advertises %d commands", len(advertised))

	tbl := p.CommandTable()
	// /context is mapped to grok's own /session-info; it must still be there.
	if m := tbl["context"]; m.Kind != command.KindNative || m.Native == "" {
		t.Fatalf("context mapping = %+v, want a native command", m)
	}
	for _, name := range []string{tbl["context"].Native, tbl["goal"].Native} {
		if !slicesContainsFold(advertised, name) {
			t.Errorf("grok no longer advertises %q, which the table forwards to", name)
		}
	}

	// The mapped command answers with a message…
	if got := promptText(t, s, "/"+tbl["context"].Native, 90*time.Second); got == "" {
		t.Errorf("/%s returned nothing; the /context mapping is dead",
			tbl["context"].Native)
	} else {
		t.Logf("/%s answered %d bytes", tbl["context"].Native, len(got))
	}

	// …while /compact, which grok advertises, answers with nothing at all. That
	// silence is why the table declares it unavailable.
	if m := tbl["compact"]; m.Kind != command.KindNone {
		t.Fatalf("compact mapping = %+v, want KindNone", m)
	}
	if !slicesContainsFold(advertised, "compact") {
		t.Log("grok no longer advertises /compact at all — the note may be stale")
	}
	if got := promptText(t, s, "/compact", 60*time.Second); got != "" {
		t.Errorf("/compact answered %q — grok may now support it over ACP; "+
			"re-probe and update the table", got)
	}
}

// waitForCommands returns the command names from the first available_commands
// event.
func waitForCommands(t *testing.T, s provider.Session, within time.Duration) []string {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("event stream closed before commands were advertised")
			}
			if ev.Type == event.TypeAvailableCommands && len(ev.Commands) > 0 {
				names := make([]string, 0, len(ev.Commands))
				for _, c := range ev.Commands {
					names = append(names, strings.TrimPrefix(c.Name, "/"))
				}
				return names
			}
		case <-deadline:
			t.Fatal("no available_commands event")
		}
	}
}

// promptText sends one prompt and returns the assistant text it produced, or ""
// when the turn ended without any.
func promptText(t *testing.T, s provider.Session, text string, within time.Duration) string {
	t.Helper()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: text}}); err != nil {
		t.Fatalf("prompt %q: %v", text, err)
	}
	var b strings.Builder
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return strings.TrimSpace(b.String())
			}
			switch ev.Type {
			case event.TypeAssistantChunk:
				b.WriteString(ev.Text)
			case event.TypeTurnComplete:
				return strings.TrimSpace(b.String())
			}
		case <-deadline:
			return strings.TrimSpace(b.String())
		}
	}
}

func slicesContainsFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}
