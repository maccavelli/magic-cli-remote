package httpagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Instance recycling and diagnostics debounce (MADR 0112 A6/A10, PLAN P7
// steps 6 and 10).
//
// The property that matters: a refresh must never destroy work a user believes
// is in flight, and it must refuse rather than wait — the skill file is already
// written, so a retry costs nothing while a block reads as a hang.

// refreshDialect records dispose/reload calls.
type refreshDialect struct {
	fakeDialectSession
	mu         sync.Mutex
	disposals  int
	reloads    int
	disposeErr error
	reloadErr  error
}

func (d *refreshDialect) DisposeInstance(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disposals++
	return d.disposeErr
}

func (d *refreshDialect) ReloadSkillCatalogs(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reloads++
	return d.reloadErr
}

func (d *refreshDialect) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.disposals, d.reloads
}

// registerFor puts s in the provider's routing table under the given directory.
func registerFor(t *testing.T, s *session, dir string) {
	t.Helper()
	s.cwd = dir
	s.p.mu.Lock()
	s.p.sessions[s.agentID] = s
	s.p.mu.Unlock()
}

func TestRefreshDisposesThenReloadsWhenIdle(t *testing.T) {
	ds := &refreshDialect{}
	s := newHookSession(t, ds)
	registerFor(t, s, "/work/project")

	if err := s.RefreshSkills(context.Background()); err != nil {
		t.Fatal(err)
	}
	disposals, reloads := ds.counts()
	if disposals != 1 || reloads != 1 {
		t.Fatalf("dispose=%d reload=%d, want 1 and 1", disposals, reloads)
	}
}

// TestRefreshRefusesEveryBusyState is the core safety rule. Each of these means
// work the user believes is in flight; recycling would destroy it.
func TestRefreshRefusesEveryBusyState(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*session)
	}{
		{"turn active", func(s *session) { s.turnActive = true }},
		{"prompt in flight", func(s *session) { s.promptInFlight = true }},
		{"prompt queued", func(s *session) {
			s.promptQueue = []queuedPrompt{{parts: []provider.Content{{Text: "x"}}}}
		}},
		{"permission pending", func(s *session) { s.pending = map[string]struct{}{"p": {}} }},
		{"question pending", func(s *session) { s.questionPending = map[string]struct{}{"q": {}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := &refreshDialect{}
			s := newHookSession(t, ds)
			registerFor(t, s, "/work/project")
			s.mu.Lock()
			tc.set(s)
			s.mu.Unlock()

			err := s.RefreshSkills(context.Background())
			if !errors.Is(err, provider.ErrInstanceBusy) {
				t.Fatalf("err = %v, want ErrInstanceBusy", err)
			}
			if disposals, _ := ds.counts(); disposals != 0 {
				t.Fatalf("a busy instance was disposed anyway (%d)", disposals)
			}
		})
	}
}

// TestRefreshIgnoresBusySessionsInOtherDirectories proves a busy unrelated
// project does not block this one — otherwise refresh would be unusable on a
// machine doing any other work.
func TestRefreshIgnoresBusySessionsInOtherDirectories(t *testing.T) {
	ds := &refreshDialect{}
	target := newHookSession(t, ds)
	registerFor(t, target, "/work/target")

	other := newHookSession(t, &refreshDialect{})
	other.p = target.p
	other.agentID = "agent-other"
	registerFor(t, other, "/work/elsewhere")
	other.mu.Lock()
	other.turnActive = true
	other.mu.Unlock()

	if err := target.RefreshSkills(context.Background()); err != nil {
		t.Fatalf("a busy unrelated project blocked the refresh: %v", err)
	}
}

// TestRefreshNormalizesTheDirectoryKey proves "busy" is decided on the same
// identity OpenCode uses to key an instance.
func TestRefreshNormalizesTheDirectoryKey(t *testing.T) {
	ds := &refreshDialect{}
	target := newHookSession(t, ds)
	registerFor(t, target, "/work/project")

	busy := newHookSession(t, &refreshDialect{})
	busy.p = target.p
	busy.agentID = "agent-busy"
	// The same directory spelled differently must still count as busy.
	registerFor(t, busy, "/work/project/")
	busy.mu.Lock()
	busy.turnActive = true
	busy.mu.Unlock()

	if err := target.RefreshSkills(context.Background()); !errors.Is(err, provider.ErrInstanceBusy) {
		t.Fatalf("a trailing separator defeated the busy check: %v", err)
	}
}

// TestRefreshLeavesStateIntactWhenDisposalFails proves a failed disposal does
// not pretend to have reloaded.
func TestRefreshLeavesStateIntactWhenDisposalFails(t *testing.T) {
	want := errors.New("engine refused")
	ds := &refreshDialect{disposeErr: want}
	s := newHookSession(t, ds)
	registerFor(t, s, "/work/project")

	if err := s.RefreshSkills(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if _, reloads := ds.counts(); reloads != 0 {
		t.Fatal("reload ran after a failed disposal")
	}
}

// TestRefreshSurfacesReloadFailure proves a disposal that succeeded but whose
// reload failed is reported, not swallowed.
func TestRefreshSurfacesReloadFailure(t *testing.T) {
	want := errors.New("reload refused")
	ds := &refreshDialect{reloadErr: want}
	s := newHookSession(t, ds)
	registerFor(t, s, "/work/project")
	if err := s.RefreshSkills(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestRefreshWithoutDialectSupport proves other providers refuse cleanly.
func TestRefreshWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	if err := s.RefreshSkills(context.Background()); err == nil {
		t.Fatal("refresh was allowed without dialect support")
	}
	if s.supportsSkillRefresh() {
		t.Fatal("supportsSkillRefresh is true without the interface")
	}
}

// TestRefreshSerializesAgainstConcurrentRefreshes proves the write lock means
// two refreshes cannot interleave their dispose and reload halves.
func TestRefreshSerializesAgainstConcurrentRefreshes(t *testing.T) {
	ds := &refreshDialect{}
	s := newHookSession(t, ds)
	registerFor(t, s, "/work/project")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.RefreshSkills(context.Background())
		}()
	}
	wg.Wait()
	disposals, reloads := ds.counts()
	if disposals != reloads {
		t.Fatalf("dispose=%d reload=%d: the halves interleaved", disposals, reloads)
	}
}

// TestNormalizeInstanceKey pins the directory identity used for busy checks.
func TestNormalizeInstanceKey(t *testing.T) {
	for in, want := range map[string]string{
		"/work/project":    "/work/project",
		"/work/project/":   "/work/project",
		"/work/project//":  "/work/project",
		"/work/./project":  "/work/project",
		"  /work/project ": "/work/project",
		"":                 "",
		".":                "",
	} {
		if got := normalizeInstanceKey(in); got != want {
			t.Fatalf("normalizeInstanceKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDiagnosticsMarkerIsDebounced proves a burst of engine events produces one
// marker per window, driven by a fake clock rather than by sleeping.
func TestDiagnosticsMarkerIsDebounced(t *testing.T) {
	s := newHookSession(t, nil)
	p := s.p
	registerFor(t, s, "/work/project")
	p.mu.Lock()
	p.generation = 3
	p.mu.Unlock()

	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	now := base
	p.nowFn = func() time.Time { return now }

	// A burst inside one window yields exactly one marker.
	for i := 0; i < 10; i++ {
		now = base.Add(time.Duration(i) * 10 * time.Millisecond)
		p.noteDiagnosticsChanged(3)
	}
	if got := countMarkers(drainEvents(s)); got != 1 {
		t.Fatalf("burst produced %d markers, want 1", got)
	}

	// Just inside the window still yields nothing.
	now = base.Add(diagnosticsDebounce - time.Millisecond)
	p.noteDiagnosticsChanged(3)
	if got := countMarkers(drainEvents(s)); got != 0 {
		t.Fatalf("a marker escaped inside the window (%d)", got)
	}

	// At the boundary a new marker is allowed.
	now = base.Add(diagnosticsDebounce)
	p.noteDiagnosticsChanged(3)
	if got := countMarkers(drainEvents(s)); got != 1 {
		t.Fatalf("no marker at the debounce boundary (%d)", got)
	}
}

// TestDiagnosticsMarkerCarriesNoPayload is the sanitization rule: the engine's
// global events name servers and carry errors, so the marker carries nothing.
func TestDiagnosticsMarkerCarriesNoPayload(t *testing.T) {
	s := newHookSession(t, nil)
	p := s.p
	registerFor(t, s, "/work/project")
	p.mu.Lock()
	p.generation = 1
	p.mu.Unlock()
	p.noteDiagnosticsChanged(1)

	for _, ev := range drainEvents(s) {
		if ev.Type != event.TypeDiagnosticsChanged {
			continue
		}
		if ev.Text != "" || ev.ToolName != "" || ev.Error != "" || ev.Codex != nil {
			t.Fatalf("the marker carried payload: %+v", ev)
		}
	}
}

// TestDiagnosticsMarkerIgnoresStaleGenerations proves an engine that has been
// replaced cannot emit markers for the new one.
func TestDiagnosticsMarkerIgnoresStaleGenerations(t *testing.T) {
	s := newHookSession(t, nil)
	p := s.p
	registerFor(t, s, "/work/project")
	p.mu.Lock()
	p.generation = 5
	p.mu.Unlock()

	p.noteDiagnosticsChanged(4)
	if got := countMarkers(drainEvents(s)); got != 0 {
		t.Fatalf("a stale generation emitted %d markers", got)
	}

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.noteDiagnosticsChanged(5)
	if got := countMarkers(drainEvents(s)); got != 0 {
		t.Fatalf("a closed provider emitted %d markers", got)
	}
}

func countMarkers(evs []event.Event) int {
	n := 0
	for _, ev := range evs {
		if ev.Type == event.TypeDiagnosticsChanged {
			n++
		}
	}
	return n
}
