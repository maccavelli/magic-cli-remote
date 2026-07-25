package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// waitEvent blocks until an event of type t is seen or the deadline passes.
func waitEvent(t *testing.T, mu *sync.Mutex, events *[]event.Event, typ event.Type) event.Event {
	t.Helper()
	return waitEventMatching(t, mu, events, typ, func(event.Event) bool { return true })
}

// waitEventMatching is waitEvent narrowed to events satisfying match — needed
// where several events of one type arrive (e.g. the mode list at create, then
// the echo of a switch).
func waitEventMatching(t *testing.T, mu *sync.Mutex, events *[]event.Event, typ event.Type, match func(event.Event) bool) event.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, ev := range *events {
			if ev.Type == typ && match(ev) {
				mu.Unlock()
				return ev
			}
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event %s not observed", typ)
	return event.Event{}
}

func newFakeManager(t *testing.T) (*session.Manager, *sync.Mutex, *[]event.Event) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	var mu sync.Mutex
	var events []event.Event
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	return mgr, &mu, &events
}

func TestManagerSetMode(t *testing.T) {
	mgr, mu, events := newFakeManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}

	// Wrong device is refused (ownership).
	if err := mgr.SetMode(ctx, meta.ID, "code", "dev-b"); !errors.Is(err, session.ErrForbidden) {
		t.Fatalf("SetMode from other device = %v, want ErrForbidden", err)
	}

	if err := mgr.SetMode(ctx, meta.ID, "code", "dev-a"); err != nil {
		t.Fatal(err)
	}
	// The session advertises its mode list at create, so scan for the *echo* of
	// this switch rather than the first mode event.
	ev := waitEventMatching(t, mu, events, event.TypeMode, func(ev event.Event) bool {
		return ev.CurrentModeID == "code"
	})
	if ev.CurrentModeID != "code" {
		t.Fatalf("mode event current = %q, want code", ev.CurrentModeID)
	}
}

func TestManagerSetConfigOption(t *testing.T) {
	mgr, mu, events := newFakeManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.SetConfigOption(ctx, meta.ID, "web", "boolean", "true", "dev-a"); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, mu, events, event.TypeSessionConfig)
	if len(ev.ConfigOptions) != 1 || ev.ConfigOptions[0].ID != "web" || !ev.ConfigOptions[0].BoolValue {
		t.Fatalf("config event = %+v", ev.ConfigOptions)
	}
}

// A prompt carrying an image attachment must flow through Prompt without error
// (the fake ignores non-text, but the manager must accept and forward it).
func TestManagerPromptWithAttachment(t *testing.T) {
	mgr, mu, events := newFakeManager(t)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	atts := []provider.Content{{Type: "image", MimeType: "image/png", Data: "aGVsbG8="}}
	if err := mgr.Prompt(ctx, meta.ID, "describe this", atts, "dev-a"); err != nil {
		t.Fatal(err)
	}
	// The user_message echo carries attachment descriptors (kind + mime, no
	// bytes) so clients can show the turn included an image.
	um := waitEvent(t, mu, events, event.TypeUserMessage)
	if len(um.Attachments) != 1 || um.Attachments[0].Kind != "image" || um.Attachments[0].MimeType != "image/png" {
		t.Fatalf("user_message attachments = %+v, want one image/png descriptor", um.Attachments)
	}
	// The turn still runs (fake echoes text); assert it completed.
	waitEvent(t, mu, events, event.TypeTurnComplete)
}
