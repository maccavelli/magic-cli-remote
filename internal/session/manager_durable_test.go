package session

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
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
	hist := transcript(mgr2.History(meta.ID))
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
	live := transcript(mgr2.History(meta2.ID))
	if len(live) < 3 {
		t.Fatalf("resumed live history len=%d want >=3", len(live))
	}
	if live[0].Seq != hist[0].Seq {
		t.Fatalf("seeded seq %d want %d", live[0].Seq, hist[0].Seq)
	}
}

// CloseAll is a soft close (purge=false). Every live session must remain on
// the next manager's list as a non-live resume row — not vanish the way a
// goose session did after last night's daemon restart.
func TestCloseAllKeepsSessionsListable(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: 0})
	mgr1 := NewManager(reg, store, nil, nil)
	ctx := context.Background()

	first, err := mgr1.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: "sess-grok",
		Name:           "grok-chat",
	}, "phone")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr1.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: "sess-goose",
		Name:           "goose-chat",
	}, "phone")
	if err != nil {
		t.Fatal(err)
	}
	mgr1.CloseAll(ctx)

	mgr2 := NewManager(reg, store, nil, nil)
	snap, err := mgr2.ListSnapshot("phone")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || snap.Skipped != 0 {
		t.Fatalf("snapshot complete=%v skipped=%d", snap.Complete, snap.Skipped)
	}
	got := map[string]Meta{}
	for _, m := range snap.Sessions {
		got[m.ID] = m
		if m.Live {
			t.Fatalf("%s still live after CloseAll", m.ID)
		}
	}
	if _, ok := got[first.ID]; !ok {
		t.Fatalf("missing first session %s in %v", first.ID, got)
	}
	if _, ok := got[second.ID]; !ok {
		t.Fatalf("missing second session %s in %v", second.ID, got)
	}
	if len(snap.Sessions) < 2 {
		t.Fatalf("list=%d want >=2", len(snap.Sessions))
	}
	// Newest durable write first so a just-closed session is not buried.
	//
	// CloseAll writes both rows back-to-back, so their timestamps can be
	// equal — and ListSnapshot's documented tie-break is then `ID >`, under
	// which "sess-grok" precedes "sess-goose" legitimately. Asserting
	// "newest first" unconditionally made this a coin flip: measured on the
	// untouched baseline b0e7261, 5 failures in 30 runs (MADR 0095 F12).
	// Assert the rule the sort actually implements, not the tie.
	ta, tb := got[first.ID].UpdatedAt, got[second.ID].UpdatedAt
	switch {
	case tb.After(ta):
		if snap.Sessions[0].ID != second.ID {
			t.Fatalf("list order[0]=%s want %s (newest first)", snap.Sessions[0].ID, second.ID)
		}
	case ta.After(tb):
		if snap.Sessions[0].ID != first.ID {
			t.Fatalf("list order[0]=%s want %s (newest first)", snap.Sessions[0].ID, first.ID)
		}
	default:
		// Equal timestamps: the tie-break is descending id.
		if snap.Sessions[0].ID != "sess-grok" {
			t.Fatalf("tied timestamps must break on descending id, got order[0]=%s",
				snap.Sessions[0].ID)
		}
	}
}

// A daemon-origin event (notice / echoed command / help) must be scheduled for
// durable persistence, not just appended to the live ring — otherwise a crash
// drops it from the on-disk transcript.
func TestDaemonOriginEventSchedulesHistoryPersist(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: 0}) // no provider events; notice is the only one

	mgr := NewManager(reg, store, nil, nil)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{
		LocalSessionID: "sess-notice",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}

	mgr.emitNotice(meta.ID, "hello from the daemon")

	// The event must have marked the session's transcript dirty.
	mgr.historyMu.Lock()
	_, dirty := mgr.dirtyHistory[meta.ID]
	mgr.historyMu.Unlock()
	if !dirty {
		t.Fatal("daemon-origin event did not schedule a durable history persist")
	}

	// End-to-end: flushing the dirty set lands the notice on disk.
	mgr.FlushHistory()
	found := false
	for _, ev := range store.LoadHistory(meta.ID) {
		if ev.Type == event.TypeNotice && ev.Text == "hello from the daemon" {
			found = true
		}
	}
	if !found {
		t.Fatalf("durable history missing daemon notice: %+v", store.LoadHistory(meta.ID))
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
	// Oversized on purpose: the file is bounded by bytes, and by the same class
	// rule the live ring uses. A count-based re-trim here would undo the
	// retention the ring just enforced, which is how two of the operator's
	// sessions ended up with no user_message at all (MADR 0138 F1).
	const chunk = 64 << 10
	events := make([]event.Event, 0, 1200)
	for i := range 1200 {
		typ := event.TypeToolUpdate
		text := strings.Repeat("o", chunk)
		if i%400 == 0 {
			typ = event.TypeUserMessage
			text = "prompt " + strconv.Itoa(i)
		}
		events = append(events, event.Event{
			Type:      typ,
			SessionID: "big",
			Seq:       uint64(i + 1),
			ToolID:    "exec-1",
			Text:      text,
		})
	}
	if err := store.SaveHistory("big", events); err != nil {
		t.Fatal(err)
	}
	got := store.LoadHistory("big")

	total := 0
	users := 0
	for i := range got {
		total += event.Bytes(&got[i])
		if got[i].Type == event.TypeUserMessage {
			users++
		}
	}
	if total > historyFileBudgetBytes {
		t.Fatalf("durable transcript is %d bytes, over the %d budget", total, historyFileBudgetBytes)
	}
	if users != 3 {
		t.Fatalf("kept %d user messages, want all 3: telemetry is evicted before the conversation", users)
	}
	if got[len(got)-1].Seq != 1200 {
		t.Fatalf("newest event seq = %d, want 1200", got[len(got)-1].Seq)
	}
}
