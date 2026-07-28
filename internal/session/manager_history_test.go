package session

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// scriptedSession emits a fixed batch of events then idles, so the ring buffer
// and History replay can be driven deterministically.
type scriptedSession struct {
	id     string
	events chan event.Event
}

func (s *scriptedSession) ID() string                                       { return s.id }
func (s *scriptedSession) ProviderID() provider.ID                          { return provider.IDFake }
func (s *scriptedSession) AgentSessionID() string                           { return s.id }
func (s *scriptedSession) Events() <-chan event.Event                       { return s.events }
func (s *scriptedSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *scriptedSession) Cancel(context.Context) error                     { return nil }
func (s *scriptedSession) Close(context.Context) error                      { return nil }

type scriptedProvider struct {
	count int
	// last is the most recently started session (for tests that feed events).
	last *scriptedSession
}

type metadataSession struct {
	id          string
	events      chan event.Event
	renameTo    string
	renameErr   error
	diagnostics provider.Diagnostics
}

func (s *metadataSession) ID() string                                       { return s.id }
func (s *metadataSession) ProviderID() provider.ID                          { return "metadata" }
func (s *metadataSession) AgentSessionID() string                           { return s.id }
func (s *metadataSession) Events() <-chan event.Event                       { return s.events }
func (s *metadataSession) Prompt(context.Context, []provider.Content) error { return nil }
func (s *metadataSession) Cancel(context.Context) error                     { return nil }
func (s *metadataSession) Close(context.Context) error                      { return nil }
func (s *metadataSession) Rename(_ context.Context, title string) error {
	if s.renameErr != nil {
		return s.renameErr
	}
	s.renameTo = title
	return nil
}
func (s *metadataSession) Diagnostics(context.Context) (provider.Diagnostics, error) {
	return s.diagnostics, nil
}

type metadataProvider struct{ session *metadataSession }

func (p *metadataProvider) ID() provider.ID { return "metadata" }
func (p *metadataProvider) Ready() bool     { return true }
func (p *metadataProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.session = &metadataSession{
		id:     opts.LocalSessionID,
		events: make(chan event.Event),
		diagnostics: provider.Diagnostics{
			Branch: "feature/parity",
			VCS:    &provider.VCSStatusSummary{Modified: 2},
		},
	}
	return p.session, nil
}

func TestRenameIsOwnerAuthorizedAndProviderAtomic(t *testing.T) {
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "before"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Rename(context.Background(), meta.ID, "after", "other"); err == nil {
		t.Fatal("non-owner rename succeeded")
	}
	if _, err := mgr.Rename(context.Background(), meta.ID, "after", "owner"); err != nil {
		t.Fatal(err)
	}
	if p.session.renameTo != "after" {
		t.Fatalf("provider title=%q", p.session.renameTo)
	}
	got, err := mgr.Get(meta.ID)
	if err != nil || got.Name != "after" {
		t.Fatalf("meta=%+v err=%v", got, err)
	}
	p.session.renameErr = context.DeadlineExceeded
	if _, err := mgr.Rename(context.Background(), meta.ID, "should-not-stick", "owner"); err == nil {
		t.Fatal("provider failure succeeded")
	}
	got, _ = mgr.Get(meta.ID)
	if got.Name != "after" {
		t.Fatalf("provider failure changed daemon name: %q", got.Name)
	}
}

func TestDiagnosticsIsOwnerAuthorizedAndDoesNotWriteHistory(t *testing.T) {
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	before := len(mgr.History(meta.ID))
	if _, err := mgr.Diagnostics(context.Background(), meta.ID, "other"); err == nil {
		t.Fatal("non-owner diagnostics succeeded")
	}
	got, err := mgr.Diagnostics(context.Background(), meta.ID, "owner")
	if err != nil || got.Branch != "feature/parity" || got.VCS == nil || got.VCS.Modified != 2 {
		t.Fatalf("diagnostics=%+v err=%v", got, err)
	}
	if history := mgr.History(meta.ID); len(history) != before {
		t.Fatalf("diagnostics changed history: before=%d after=%d", before, len(history))
	}
}

func (p *scriptedProvider) ID() provider.ID { return provider.IDFake }
func (p *scriptedProvider) Ready() bool     { return true }
func (p *scriptedProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &scriptedSession{id: opts.LocalSessionID, events: make(chan event.Event, p.count+2)}
	p.last = s
	for i := 0; i < p.count; i++ {
		s.events <- event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: s.id,
			Timestamp: time.Now().UTC(),
			Text:      strconv.Itoa(i),
		}
	}
	return s, nil
}

// The per-session ring buffer caps at historyBufferCap and drops the oldest
// events; History returns the survivors in emission order, unknown -> empty.
func TestHistoryRingBufferCapsAndOrders(t *testing.T) {
	const emitted = historyBufferCap + 100

	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: emitted})

	var mu sync.Mutex
	seen := 0
	done := make(chan struct{})
	mgr := NewManager(reg, nil, nil, func(ev event.Event) {
		// Count provider events only: the daemon's own command advertisement
		// would otherwise satisfy the count before the ring was full.
		if ev.Type == event.TypeRemoteCommands {
			return
		}
		mu.Lock()
		seen++
		if seen == emitted {
			close(done)
		}
		mu.Unlock()
	})

	ctx := context.Background()
	meta, err := mgr.Create(ctx, provider.IDFake, provider.StartOptions{Name: "t"}, "")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events to pump")
	}

	hist := mgr.History(meta.ID)
	// Trimming happens in batches: the ring never exceeds the cap and never
	// shrinks below the trim floor. This is a property of the whole ring, so it
	// counts every event in it.
	if len(hist) > historyBufferCap || len(hist) < historyTrimTo {
		t.Fatalf("history len = %d, want within [%d, %d]", len(hist), historyTrimTo, historyBufferCap)
	}
	// Content and ordering are about the conversation. The daemon's own command
	// advertisement shares the ring and is emitted from Create while the pump is
	// already consuming, so where it lands — and whether the batch trim has
	// reached it yet — varies from run to run; counting it here made the
	// arithmetic below off by one at random.
	chat := transcript(hist)
	// The newest event always survives; the oldest retained is whatever the
	// batch trim left.
	if got := chat[len(chat)-1].Text; got != strconv.Itoa(emitted-1) {
		t.Fatalf("newest retained = %q, want %q", got, strconv.Itoa(emitted-1))
	}
	if got, _ := strconv.Atoi(chat[0].Text); got != emitted-len(chat) {
		t.Fatalf("oldest retained = %d, want %d", got, emitted-len(chat))
	}
	// Strictly increasing (emission) order, and monotonically increasing seq.
	for i := 1; i < len(chat); i++ {
		prev, _ := strconv.Atoi(chat[i-1].Text)
		cur, _ := strconv.Atoi(chat[i].Text)
		if cur != prev+1 {
			t.Fatalf("out of order at %d: %d then %d", i, prev, cur)
		}
	}
	// Seq is stamped per ring append, so it must be gapless across the whole
	// ring — advertisement included.
	for i := 1; i < len(hist); i++ {
		if hist[i].Seq != hist[i-1].Seq+1 {
			t.Fatalf("seq gap at %d: %d then %d", i, hist[i-1].Seq, hist[i].Seq)
		}
	}
	if hist[0].Seq == 0 {
		t.Fatal("history events must carry a non-zero seq")
	}

	// Unknown session yields an empty, non-nil slice (not an error).
	if got := mgr.History("no-such-session"); got == nil || len(got) != 0 {
		t.Fatalf("unknown session history = %v, want empty non-nil", got)
	}

	// Paging: first page, then continue from NextSinceSeq (Phase 3.5).
	page1, trunc, next, err := mgr.HistoryPageFor(meta.ID, "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 50 {
		t.Fatalf("page1 len=%d want 50", len(page1))
	}
	if !trunc || next == 0 {
		t.Fatalf("want truncated with next_since_seq, trunc=%v next=%d", trunc, next)
	}
	page2, trunc2, _, err := mgr.HistoryPageFor(meta.ID, "", next, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) == 0 {
		t.Fatal("page2 empty")
	}
	if page2[0].Seq <= next {
		t.Fatalf("page2 first seq %d not > since %d", page2[0].Seq, next)
	}
	_ = trunc2
}

func TestPendingAsksAreOwnerScopedAndRetired(t *testing.T) {
	p := &scriptedProvider{count: 0}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := NewManager(reg, nil, nil, nil)
	meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{}, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	p.last.events <- event.Event{Type: event.TypePermission, SessionID: meta.ID, PermissionID: "perm-1"}
	p.last.events <- event.Event{Type: event.TypeQuestion, SessionID: meta.ID, QuestionID: "question-1"}

	deadline := time.Now().Add(time.Second)
	for len(mgr.PendingAsks("device-a")) != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := mgr.PendingAsks("device-a"); len(got) != 2 {
		t.Fatalf("pending asks=%+v", got)
	}
	if got := mgr.PendingAsks("device-b"); len(got) != 0 {
		t.Fatalf("foreign device received asks: %+v", got)
	}
	p.last.events <- event.Event{Type: event.TypePermissionResolved, SessionID: meta.ID, PermissionID: "perm-1"}
	p.last.events <- event.Event{Type: event.TypeTurnComplete, SessionID: meta.ID}
	deadline = time.Now().Add(time.Second)
	for len(mgr.PendingAsks("device-a")) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := mgr.PendingAsks("device-a"); len(got) != 0 {
		t.Fatalf("retired asks still present: %+v", got)
	}
}

// cwdSession reports a resolved working directory (provider.CWDSession).
type cwdSession struct {
	scriptedSession
	cwd string
}

func (s *cwdSession) CWD() string { return s.cwd }

type cwdProvider struct{ cwd string }

func (p *cwdProvider) ID() provider.ID { return provider.IDFake }
func (p *cwdProvider) Ready() bool     { return true }
func (p *cwdProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	return &cwdSession{
		scriptedSession: scriptedSession{id: opts.LocalSessionID, events: make(chan event.Event, 1)},
		cwd:             p.cwd,
	}, nil
}

// Meta.CWD reflects the session's *resolved* directory (config default or
// home fallback), not the raw request value, so the phone can display where
// the agent actually runs even when the field was left empty.
func TestCreateUsesResolvedCWDForMeta(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&cwdProvider{cwd: "/resolved/home"})
	mgr := NewManager(reg, nil, nil, nil)

	meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if meta.CWD != "/resolved/home" {
		t.Fatalf("meta.CWD = %q, want resolved /resolved/home", meta.CWD)
	}
}

// Model is persisted on the session record so resume after restart keeps it
// (Phase 3.3).
func TestModelPersistedOnRecord(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(&scriptedProvider{count: 0})
	mgr := NewManager(reg, store, nil, nil)

	meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{
		LocalSessionID: "model-sess",
		Model:          "grok-4",
		Name:           "n",
	}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "grok-4" {
		t.Fatalf("meta.Model=%q", meta.Model)
	}
	rec, err := store.Get("model-sess")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Model != "grok-4" {
		t.Fatalf("record.Model=%q", rec.Model)
	}
	// Disk-only list path after close.
	_ = mgr.Close(context.Background(), "model-sess", "dev")
	list := mgr.ListFor("dev")
	var found bool
	for _, m := range list {
		if m.ID == "model-sess" {
			found = true
			if m.Model != "grok-4" {
				t.Fatalf("listed Model=%q", m.Model)
			}
		}
	}
	if !found {
		t.Fatal("session not in list after close")
	}
}

// Replay events (agent re-emitting prior conversation on session/load) enter
// the history ring but are never broadcast live — a resuming client already
// displays that content.
func TestPumpKeepsReplayEventsOutOfBroadcast(t *testing.T) {
	reg := provider.NewRegistry()
	p := &scriptedProvider{count: 0}
	reg.Register(p)

	var mu sync.Mutex
	var broadcast []event.Event
	done := make(chan struct{})
	mgr := NewManager(reg, nil, nil, func(ev event.Event) {
		mu.Lock()
		broadcast = append(broadcast, ev)
		if ev.Type == event.TypeTurnComplete {
			close(done)
		}
		mu.Unlock()
	})

	meta, err := mgr.Create(context.Background(), provider.IDFake, provider.StartOptions{}, "")
	if err != nil {
		t.Fatal(err)
	}
	sess := p.last
	sess.events <- event.Event{
		Type: event.TypeAssistantChunk, SessionID: meta.ID,
		Timestamp: time.Now().UTC(), Text: "replayed", Replay: true,
	}
	sess.events <- event.Event{
		Type: event.TypeTurnComplete, SessionID: meta.ID,
		Timestamp: time.Now().UTC(), StopReason: "end_turn",
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for live event")
	}

	mu.Lock()
	for _, ev := range broadcast {
		if ev.Replay {
			t.Fatalf("replay event was broadcast live: %+v", ev)
		}
	}
	mu.Unlock()

	hist := transcript(mgr.History(meta.ID))
	if len(hist) != 2 || !hist[0].Replay || hist[0].Text != "replayed" {
		t.Fatalf("history = %+v, want replay chunk then turn_complete", hist)
	}
}

// transcript drops the daemon's canonical-command advertisement from a history
// snapshot. It is recorded in the ring so a cold client can render autocomplete
// after a replay (MADR 0023), but it is control state rather than conversation,
// and these assertions are about the conversation.
func transcript(hist []event.Event) []event.Event {
	out := make([]event.Event, 0, len(hist))
	for _, ev := range hist {
		if ev.Type == event.TypeRemoteCommands {
			continue
		}
		out = append(out, ev)
	}
	return out
}
