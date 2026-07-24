package session_test

import (
	"context"
	"sync"
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

	var mu sync.Mutex
	var events []event.Event
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"}, "dev-test")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID == "" {
		t.Fatal("empty id")
	}

	if err := mgr.Prompt(ctx, meta.ID, "hello", nil, "dev-test"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		var hasComplete, hasChunk bool
		for _, ev := range events {
			if ev.Type == event.TypeTurnComplete {
				hasComplete = true
			}
			if ev.Type == event.TypeAssistantChunk {
				hasChunk = true
			}
		}
		mu.Unlock()
		if hasComplete && hasChunk {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	var hasChunk bool
	for _, ev := range events {
		if ev.Type == event.TypeAssistantChunk {
			hasChunk = true
		}
	}
	if !hasChunk {
		t.Fatalf("expected assistant chunk, events=%+v", events)
	}

	if err := mgr.Close(ctx, meta.ID, "dev-test"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := session.Record{
		ID:             "abc",
		Provider:       provider.IDGrok,
		Name:           "n",
		AgentSessionID: "agent-1",
		CreatedAt:      time.Now().UTC(),
		Status:         "idle",
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "agent-1" {
		t.Fatalf("got=%+v", got)
	}
}
