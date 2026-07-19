package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

func TestManagerFakePrompt(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(fake.New())

	var events []event.Event
	mgr := session.NewManager(reg, nil, func(ev event.Event) {
		events = append(events, ev)
	})

	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID == "" {
		t.Fatal("empty id")
	}

	if err := mgr.Prompt(ctx, meta.ID, "hello"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var hasComplete bool
		for _, ev := range events {
			if ev.Type == event.TypeTurnComplete {
				hasComplete = true
			}
		}
		if hasComplete {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var hasChunk bool
	for _, ev := range events {
		if ev.Type == event.TypeAssistantChunk {
			hasChunk = true
		}
	}
	if !hasChunk {
		t.Fatalf("expected assistant chunk, events=%+v", events)
	}

	if err := mgr.Close(ctx, meta.ID); err != nil {
		t.Fatal(err)
	}
}
