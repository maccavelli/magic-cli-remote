package session

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// pump → close manager → new manager → history non-empty (0009 Phase D).
func TestDurableHistorySurvivesManagerRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	sp := &scriptedProvider{count: 0} // events fed after Create
	reg.Register(sp)

	var mu sync.Mutex
	seen := 0
	done := make(chan struct{})
	mgr1 := NewManager(reg, store, nil, func(event.Event) {
		mu.Lock()
		seen++
		if seen == 3 {
			close(done)
		}
		mu.Unlock()
	})

	ctx := context.Background()
	meta, err := mgr1.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: "sess-durable-1",
		Name:           "chat",
	}, "device-a")
	if err != nil {
		t.Fatal(err)
	}

	// Feed user + assistant turns into the live ring.
	if sp.last == nil {
		t.Fatal("expected scripted session")
	}
	for i, text := range []string{"hello", "hi there", "done"} {
		typ := event.TypeAssistantChunk
		if i == 0 {
			typ = event.TypeUserMessage
		}
		if i == 2 {
			typ = event.TypeTurnComplete
		}
		sp.last.events <- event.Event{
			Type:      typ,
			SessionID: meta.ID,
			Timestamp: time.Now().UTC(),
			Text:      text,
		}
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pump")
	}

	// Ensure transcript is on disk before tearing the manager down.
	mgr1.FlushHistory()
	if err := mgr1.Close(ctx, meta.ID, "device-a"); err != nil {
		t.Fatal(err)
	}
	// CloseAll path also used on daemon shutdown.
	mgr1.CloseAll(ctx)

	// New manager, same store: session is non-live but history must load.
	mgr2 := NewManager(reg, store, nil, nil)
	hist := mgr2.History(meta.ID)
	if len(hist) != 3 {
		t.Fatalf("history after restart len=%d want 3; events=%+v", len(hist), hist)
	}
	if hist[0].Text != "hello" || hist[0].Type != event.TypeUserMessage {
		t.Fatalf("first event = %+v", hist[0])
	}
	if hist[0].Seq == 0 || hist[2].Seq <= hist[0].Seq {
		t.Fatalf("seq not monotonic: %d %d %d", hist[0].Seq, hist[1].Seq, hist[2].Seq)
	}

	// Paging from disk works too.
	page, trunc, next, err := mgr2.HistoryPageFor(meta.ID, "device-a", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || !trunc || next == 0 {
		t.Fatalf("page len=%d trunc=%v next=%d", len(page), trunc, next)
	}

	// Resume create seeds the ring so live history continues seq.
	meta2, err := mgr2.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: meta.ID,
		Name:           "chat",
	}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	live := mgr2.History(meta2.ID)
	if len(live) < 3 {
		t.Fatalf("resumed live history len=%d want >=3", len(live))
	}
	if live[0].Seq != hist[0].Seq {
		t.Fatalf("seeded seq %d want %d", live[0].Seq, hist[0].Seq)
	}
}

func TestDurableHistoryPurgedOnDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: 2})

	var mu sync.Mutex
	seen := 0
	done := make(chan struct{})
	mgr := NewManager(reg, store, nil, func(event.Event) {
		mu.Lock()
		seen++
		if seen == 2 {
			close(done)
		}
		mu.Unlock()
	})
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: "to-delete",
	}, "d1")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	mgr.FlushHistory()
	if len(mgr.History(meta.ID)) == 0 {
		t.Fatal("want live history before delete")
	}
	if err := mgr.Delete(ctx, meta.ID, "d1"); err != nil {
		t.Fatal(err)
	}
	if got := mgr.History(meta.ID); len(got) != 0 {
		t.Fatalf("history after delete = %d, want 0", len(got))
	}
	// Disk gone with session dir.
	if _, err := store.Get(meta.ID); err == nil {
		t.Fatal("meta should be gone")
	}
	histPath := filepath.Join(dir, "sessions", meta.ID, "history.json")
	if store.LoadHistory(meta.ID); len(store.LoadHistory(meta.ID)) != 0 {
		t.Fatalf("disk history survived delete at %s", histPath)
	}
}

func TestDurableHistoryRespectsCap(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic oversized slice: SaveHistory keeps only the tail.
	events := make([]event.Event, historyBufferCap+50)
	for i := range events {
		events[i] = event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: "big",
			Seq:       uint64(i + 1),
			Text:      strconv.Itoa(i),
		}
	}
	if err := store.SaveHistory("big", events); err != nil {
		t.Fatal(err)
	}
	got := store.LoadHistory("big")
	if len(got) != historyBufferCap {
		t.Fatalf("len=%d want %d", len(got), historyBufferCap)
	}
	if got[0].Text != strconv.Itoa(50) {
		t.Fatalf("oldest kept text=%q want %q", got[0].Text, strconv.Itoa(50))
	}
	if got[len(got)-1].Text != strconv.Itoa(historyBufferCap+49) {
		t.Fatalf("newest text=%q", got[len(got)-1].Text)
	}
}
