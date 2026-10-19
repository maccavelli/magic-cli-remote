package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

// maxSeq is the highest seq rec has delivered.
func (r *seqRecorder) maxSeq() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n uint64
	for _, s := range r.seqs {
		n = max(n, s)
	}
	return n
}

// snapshot copies the delivered events.
func (r *seqRecorder) snapshot() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.evs...)
}

// waitType waits until rec has delivered at least n events of type typ.
func waitType(t *testing.T, rec *seqRecorder, typ event.Type, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := 0
		for _, ev := range rec.snapshot() {
			if ev.Type == typ {
				got++
			}
		}
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saw %d %s deliveries, want %d", got, typ, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// newStoredFakeManager is a manager over a fresh store with the fake provider,
// recording every delivery.
func newStoredFakeManager(t *testing.T) (*Manager, *Store, *seqRecorder, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := &seqRecorder{}
	reg := provider.NewRegistry()
	reg.Register(fake.New())
	return NewManager(reg, store, nil, rec.record), store, rec, dir
}

func turn(t *testing.T, mgr *Manager, rec *seqRecorder, id, text string, nth int) {
	t.Helper()
	if err := mgr.Prompt(context.Background(), id, text, nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	waitType(t, rec, event.TypeTurnComplete, nth)
}

// setSnapshotHook installs afterHistorySnapshotHook for one test.
func setSnapshotHook(t *testing.T, hook func(string)) {
	t.Helper()
	afterHistorySnapshotHook.Store(&hook)
	t.Cleanup(func() { afterHistorySnapshotHook.Store(nil) })
}

// pauseOneFlush makes the first FlushHistory to reach its write wait until
// resume is closed. paused closes when it gets there.
func pauseOneFlush(t *testing.T) (paused, resume chan struct{}) {
	t.Helper()
	paused, resume = make(chan struct{}), make(chan struct{})
	var once sync.Once
	setSnapshotHook(t, func(string) {
		once.Do(func() {
			close(paused)
			<-resume
		})
	})
	return paused, resume
}

func waitClosed(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

// TestSeqStaysAheadAfterAFailedCloseSave is MADR 0201 D14 (0073 F-S4). A
// failed close-time history save was only logged, and the next Create seeded
// seq from the shorter history on disk, so clients dropped the new events as
// duplicates of seqs already sent.
func TestSeqStaysAheadAfterAFailedCloseSave(t *testing.T) {
	mgr, _, rec, dir := newStoredFakeManager(t)
	// history.json as a non-empty directory: every history write fails, and
	// every read finds nothing.
	if err := os.MkdirAll(filepath.Join(dir, "sessions", "d14", "history.json", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "d14"}, "dev-a"); err != nil {
		t.Fatal(err)
	}
	turn(t, mgr, rec, "d14", "hello", 1)
	sent := rec.maxSeq()
	if err := mgr.Close(ctx, "d14", "dev-a"); err != nil {
		t.Fatal(err)
	}
	before := rec.len()
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "d14"}, "dev-a"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(ctx, "d14", "dev-a") })
	waitDeliveries(t, rec, before+1)
	if first := rec.snapshot()[before]; first.Seq <= sent {
		t.Fatalf("first event after re-create has seq %d (%s); seqs up to %d were already sent",
			first.Seq, first.Type, sent)
	}
}

// TestStaleFlushCannotOverwriteTheCloseTranscript is MADR 0201 D15 (0073
// F-S5). A flush snapshots under a read lock and writes later, so a stale
// snapshot could overwrite the transcript Close had just written.
func TestStaleFlushCannotOverwriteTheCloseTranscript(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, store, nil, nil)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, "metadata", provider.StartOptions{LocalSessionID: "d15"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		send(p, event.Event{Type: event.TypeAssistantChunk, Text: fmt.Sprintf("early %d", i)})
	}
	flush(p)

	paused, resume := pauseOneFlush(t)
	flushed := make(chan struct{})
	go func() { mgr.FlushHistory(); close(flushed) }()
	waitClosed(t, paused, "the flush snapshot")

	for i := range 3 {
		send(p, event.Event{Type: event.TypeAssistantChunk, Text: fmt.Sprintf("late %d", i)})
	}
	flush(p)
	if err := mgr.Close(ctx, meta.ID, "dev-a"); err != nil {
		t.Fatal(err)
	}
	want := store.LoadHistory(meta.ID)
	close(resume)
	waitClosed(t, flushed, "the stale flush finishing")

	got := store.LoadHistory(meta.ID)
	if len(got) != len(want) || len(got) == 0 || got[len(got)-1].Seq != want[len(want)-1].Seq {
		t.Fatalf("a stale flush overwrote the close transcript: disk has %d events, the close wrote %d",
			len(got), len(want))
	}
}

// TestFlushAfterDeleteLeavesNoDirectory is MADR 0201 D15: a flush that
// resumes after Delete must not recreate the session directory.
func TestFlushAfterDeleteLeavesNoDirectory(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, store, nil, nil)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, "metadata", provider.StartOptions{LocalSessionID: "d15b"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	send(p, event.Event{Type: event.TypeAssistantChunk, Text: "hello"})
	flush(p)

	paused, resume := pauseOneFlush(t)
	flushed := make(chan struct{})
	go func() { mgr.FlushHistory(); close(flushed) }()
	waitClosed(t, paused, "the flush snapshot")
	if err := mgr.Delete(ctx, meta.ID, "dev-a"); err != nil {
		t.Fatal(err)
	}
	close(resume)
	waitClosed(t, flushed, "the stale flush finishing")

	if _, err := os.Stat(filepath.Join(dir, "sessions", meta.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session directory after Delete and a late flush: stat err = %v, want not-exist", err)
	}
}

// blockingProvider's Start waits for release, or for its context.
type blockingProvider struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (p *blockingProvider) ID() provider.ID { return "blocking" }
func (p *blockingProvider) Ready() bool     { return true }
func (p *blockingProvider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &metadataSession{id: opts.LocalSessionID, events: make(chan event.Event)}, nil
}

// TestCreateLockWaitEndsWithTheContext is MADR 0201 D16 (0073 F-S6). The
// per-session create lock was a plain mutex held across Start, so one hung
// start wedged every later create for that id, whatever its deadline.
func TestCreateLockWaitEndsWithTheContext(t *testing.T) {
	p := &blockingProvider{release: make(chan struct{}), started: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(p.release) }) }
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)

	firstDone := make(chan error, 1)
	go func() {
		_, err := mgr.Create(context.Background(), "blocking", provider.StartOptions{LocalSessionID: "d16"}, "dev-a")
		firstDone <- err
	}()
	waitClosed(t, p.started, "the first Start")
	t.Cleanup(func() {
		release()
		<-firstDone
		_ = mgr.Close(context.Background(), "d16", "dev-a")
	})
	// The old code would wait for the lock until this fires.
	timer := time.AfterFunc(time.Second, release)
	defer timer.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := mgr.Create(ctx, "blocking", provider.StartOptions{LocalSessionID: "d16"}, "dev-a")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) || elapsed > 300*time.Millisecond {
		t.Fatalf("second Create = %v after %v; want context.DeadlineExceeded within 300ms", err, elapsed)
	}
}

// TestClearResetsTheDurableTranscript is MADR 0201 D17 (0073 F-S7). /clear
// relaunched through Create, which reloaded the durable history, so a cold
// reload replayed the conversation the user had cleared.
func TestClearResetsTheDurableTranscript(t *testing.T) {
	mgr, _, rec, _ := newStoredFakeManager(t)
	ctx := context.Background()
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "d17"}, "dev-a"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		turn(t, mgr, rec, "d17", fmt.Sprintf("before clear %d", i), i)
	}
	cleared := rec.maxSeq()
	if err := mgr.cmdReset(ctx, "d17", "dev-a"); err != nil {
		t.Fatal(err)
	}
	turn(t, mgr, rec, "d17", "after clear", 4)
	if err := mgr.Close(ctx, "d17", "dev-a"); err != nil {
		t.Fatal(err)
	}

	page, _, _ := mgr.HistoryPage("d17", 0, 10000)
	sawAfter := false
	for _, ev := range page {
		if ev.Seq <= cleared {
			t.Fatalf("cold history kept seq %d (%s %q) from before /clear (last pre-clear seq %d)",
				ev.Seq, ev.Type, ev.Text, cleared)
		}
		sawAfter = sawAfter || (ev.Type == event.TypeUserMessage && ev.Text == "after clear")
	}
	if !sawAfter {
		t.Fatalf("cold history after /clear lacks the new turn: %d events", len(page))
	}
}

// TestModelSwitchKeepsTheTranscript pins the other relaunch (MADR 0201 D17):
// a model switch that restarts the agent keeps the durable transcript.
func TestModelSwitchKeepsTheTranscript(t *testing.T) {
	mgr, _, rec, _ := newStoredFakeManager(t)
	ctx := context.Background()
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "d17m"}, "dev-a"); err != nil {
		t.Fatal(err)
	}
	turn(t, mgr, rec, "d17m", "before switch", 1)
	if err := mgr.cmdModel(ctx, "d17m", "dev-a", "other-model", false); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Close(ctx, "d17m", "dev-a"); err != nil {
		t.Fatal(err)
	}
	page, _, _ := mgr.HistoryPage("d17m", 0, 10000)
	for _, ev := range page {
		if ev.Type == event.TypeUserMessage && ev.Text == "before switch" {
			return
		}
	}
	t.Fatalf("a model-switch relaunch dropped the transcript: %d events, none the earlier turn", len(page))
}

// TestPumpNeverWaitsOnAHistoryFlush is MADR 0201 D18 (0073 F-S8(a)). Past
// the latency bound, scheduleHistoryPersist flushed inline, on the pump, so
// a slow disk delayed every broadcast.
func TestPumpNeverWaitsOnAHistoryFlush(t *testing.T) {
	prev := historyMaxLatency
	historyMaxLatency = 10 * time.Millisecond
	t.Cleanup(func() { historyMaxLatency = prev })
	setSnapshotHook(t, func(string) { time.Sleep(2 * time.Second) })

	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	got := map[string]time.Time{}
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, store, nil, func(ev event.Event) {
		if ev.Type == event.TypeAssistantChunk {
			mu.Lock()
			got[ev.Text] = time.Now()
			mu.Unlock()
		}
	})
	ctx := context.Background()
	meta, err := mgr.Create(ctx, "metadata", provider.StartOptions{LocalSessionID: "d18"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(ctx, meta.ID, "dev-a") })

	const n = 50
	sentAt := make([]time.Time, n)
	for i := range n {
		sentAt[i] = time.Now()
		send(p, event.Event{Type: event.TypeAssistantChunk, Text: fmt.Sprintf("chunk %d", i)})
		time.Sleep(time.Millisecond)
	}
	flush(p)
	mu.Lock()
	defer mu.Unlock()
	for i := range n {
		at, ok := got[fmt.Sprintf("chunk %d", i)]
		if !ok {
			t.Fatalf("chunk %d was never delivered", i)
		}
		if d := at.Sub(sentAt[i]); d > 50*time.Millisecond {
			t.Fatalf("chunk %d reached clients %v after the provider sent it; want under 50ms", i, d)
		}
	}
}

// TestNoticesForClosedSessionsAreSequencedAndDurable is MADR 0201 D19 (0073
// F-S8(b)). A notice for a session not in memory went out with seq 0, which
// the phone shows and keeps nowhere.
func TestNoticesForClosedSessionsAreSequencedAndDurable(t *testing.T) {
	mgr, _, rec, _ := newStoredFakeManager(t)
	ctx := context.Background()
	if _, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{LocalSessionID: "d19"}, "dev-a"); err != nil {
		t.Fatal(err)
	}
	turn(t, mgr, rec, "d19", "hello", 1)
	if err := mgr.Close(ctx, "d19", "dev-a"); err != nil {
		t.Fatal(err)
	}
	last := rec.maxSeq()
	before := rec.len()
	mgr.emitNotice("d19", "after close")
	waitDeliveries(t, rec, before+1)
	if ev := rec.snapshot()[before]; ev.Seq != last+1 {
		t.Fatalf("notice for a closed session went out with seq %d; want %d", ev.Seq, last+1)
	}
	page, _, _ := mgr.HistoryPage("d19", 0, 10000)
	for _, ev := range page {
		if ev.Type == event.TypeNotice && ev.Text == "after close" && ev.Seq == last+1 {
			return
		}
	}
	t.Fatalf("cold history lacks the notice sent after close: %d events", len(page))
}

// TestNoticesForUnknownSessionsAreNotBroadcast is MADR 0201 D19: with no
// record, no client could own the session, so the notice is not sent.
func TestNoticesForUnknownSessionsAreNotBroadcast(t *testing.T) {
	mgr, _, rec, _ := newStoredFakeManager(t)
	mgr.emitNotice("no-such-session", "x")
	if n := rec.len(); n != 0 {
		t.Fatalf("a notice for an unknown session was broadcast: %d deliveries", n)
	}
}

// TestAsksOpenAtTurnEndAreResolvedCancelled is MADR 0201 D20 (0073
// F-S8(c)). The pump cleared asks still open at turn end without telling
// anyone, so a second device or a cold replay kept a stale card.
func TestAsksOpenAtTurnEndAreResolvedCancelled(t *testing.T) {
	rec := &seqRecorder{}
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, rec.record)
	ctx := context.Background()
	meta, err := mgr.Create(ctx, "metadata", provider.StartOptions{}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(ctx, meta.ID, "dev-a") })

	send(p, event.Event{Type: event.TypePermission, PermissionID: "p-open"})
	send(p, event.Event{Type: event.TypeQuestion, QuestionID: "q-open"})
	send(p, event.Event{Type: event.TypePermission, PermissionID: "p-done"})
	send(p, event.Event{Type: event.TypePermissionResolved, PermissionID: "p-done",
		Status: event.PermissionStatusResolved})
	send(p, event.Event{Type: event.TypeTurnComplete})
	flush(p)

	var tc *event.Event
	resolved := map[string][]event.Event{}
	for _, ev := range rec.snapshot() {
		switch ev.Type {
		case event.TypeTurnComplete:
			tc = &ev
		case event.TypePermissionResolved:
			resolved["p:"+ev.PermissionID] = append(resolved["p:"+ev.PermissionID], ev)
		case event.TypeQuestionResolved:
			resolved["q:"+ev.QuestionID] = append(resolved["q:"+ev.QuestionID], ev)
		}
	}
	if tc == nil {
		t.Fatal("turn_complete was never delivered")
	}
	for _, key := range []string{"p:p-open", "q:q-open"} {
		evs := resolved[key]
		if len(evs) != 1 {
			t.Fatalf("%s: %d resolved events at turn end, want 1", key, len(evs))
		}
		if evs[0].Status != event.PermissionStatusCancelled || evs[0].Seq <= tc.Seq {
			t.Fatalf("%s: resolved with status %q at seq %d; want %q after turn_complete's seq %d",
				key, evs[0].Status, evs[0].Seq, event.PermissionStatusCancelled, tc.Seq)
		}
	}
	if n := len(resolved["p:p-done"]); n != 1 {
		t.Fatalf("p-done: %d resolved events, want only the provider's own", n)
	}
}
