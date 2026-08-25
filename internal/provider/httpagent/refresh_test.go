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

// shareDialect implements the optional share hooks with a settable policy.
type shareDialect struct {
	fakeDialectSession
	allow bool
	state provider.ShareState
	err   error
	calls []string
}

func (d *shareDialect) CurrentShare(context.Context) (provider.ShareState, error) {
	d.calls = append(d.calls, "state")
	return d.state, d.err
}
func (d *shareDialect) Share(context.Context) (provider.ShareState, error) {
	d.calls = append(d.calls, "share")
	return d.state, d.err
}
func (d *shareDialect) Unshare(context.Context) error {
	d.calls = append(d.calls, "unshare")
	return d.err
}
func (d *shareDialect) ShareMutationAllowed() bool { return d.allow }

// TestShareCapabilitiesSeparateReadFromMutate is the A8 rule: an existing
// public link stays visible even where mutation is forbidden, so the two
// capabilities cannot be one flag.
func TestShareCapabilitiesSeparateReadFromMutate(t *testing.T) {
	readOnly := newHookSession(t, &shareDialect{allow: false})
	if !readOnly.supportsShareState() {
		t.Fatal("share state should be readable")
	}
	if readOnly.supportsShareMutation() {
		t.Fatal("mutation advertised while policy is off")
	}

	enabled := newHookSession(t, &shareDialect{allow: true})
	if !enabled.supportsShareState() || !enabled.supportsShareMutation() {
		t.Fatal("an enabled session should advertise both")
	}

	none := newHookSession(t, nil)
	if none.supportsShareState() || none.supportsShareMutation() {
		t.Fatal("a provider without the interface advertised sharing")
	}
}

// TestShareCapabilitiesAreEmitted proves the flags reach the wire.
func TestShareCapabilitiesAreEmitted(t *testing.T) {
	s := newHookSession(t, &shareDialect{allow: false})
	s.emitCapabilities()
	var caps *event.Capabilities
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionCapabilities {
			caps = ev.Capabilities
		}
	}
	if caps == nil {
		t.Fatal("no capabilities emitted")
	}
	if !caps.ShareState {
		t.Fatal("share_state was not advertised")
	}
	if caps.Share {
		t.Fatal("share mutation was advertised while policy is off")
	}
}

// TestShareForwardsToDialect proves the transport delegates.
func TestShareForwardsToDialect(t *testing.T) {
	ds := &shareDialect{allow: true, state: provider.ShareState{Shared: true, URL: "https://x/y"}}
	s := newHookSession(t, ds)
	ctx := context.Background()

	if got, err := s.CurrentShare(ctx); err != nil || got.URL != "https://x/y" {
		t.Fatalf("state = %+v, %v", got, err)
	}
	if got, err := s.Share(ctx); err != nil || !got.Shared {
		t.Fatalf("share = %+v, %v", got, err)
	}
	if err := s.Unshare(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"state", "share", "unshare"}
	for i, w := range want {
		if ds.calls[i] != w {
			t.Fatalf("call %d = %q, want %q", i, ds.calls[i], w)
		}
	}
}

// TestShareWithoutDialectSupport proves other providers refuse cleanly.
func TestShareWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	ctx := context.Background()
	if _, err := s.CurrentShare(ctx); err == nil {
		t.Fatal("state was allowed without dialect support")
	}
	if _, err := s.Share(ctx); err == nil {
		t.Fatal("share was allowed without dialect support")
	}
	if err := s.Unshare(ctx); err == nil {
		t.Fatal("unshare was allowed without dialect support")
	}
}

// TestShareSessionInterfaceIsSatisfied is the contract the manager asserts.
func TestShareSessionInterfaceIsSatisfied(t *testing.T) {
	var _ provider.ShareSession = newHookSession(t, nil)
}

// shellDialect implements the optional shell hooks.
type shellDialect struct {
	fakeDialectSession
	mu      sync.Mutex
	allow   bool
	err     error
	calls   int
	release chan struct{}
}

func (d *shellDialect) Shell(ctx context.Context, _ string) error {
	d.mu.Lock()
	d.calls++
	rel := d.release
	err := d.err
	d.mu.Unlock()
	if rel != nil {
		select {
		case <-rel:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func (d *shellDialect) ShellAllowed() bool { return d.allow }
func (d *shellDialect) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// TestShellCapabilityRequiresBothInterfaceAndPolicy proves there is no
// read-only half: a command either may be run or may not.
func TestShellCapabilityRequiresBothInterfaceAndPolicy(t *testing.T) {
	if newHookSession(t, &shellDialect{allow: false}).supportsShell() {
		t.Fatal("shell advertised while policy is off")
	}
	if !newHookSession(t, &shellDialect{allow: true}).supportsShell() {
		t.Fatal("shell not advertised while enabled")
	}
	if newHookSession(t, nil).supportsShell() {
		t.Fatal("shell advertised without the interface")
	}
}

// TestShellClaimsTheTurnSlot proves a shell command and a prompt cannot
// overlap: two agents' output interleaved in one transcript is unreadable.
func TestShellClaimsTheTurnSlot(t *testing.T) {
	ds := &shellDialect{allow: true, release: make(chan struct{})}
	s := newHookSession(t, ds)

	started := make(chan struct{})
	go func() {
		close(started)
		_ = s.Shell(context.Background(), "sleep 1")
	}()
	<-started
	// Wait for the claim to land.
	for i := 0; i < 200; i++ {
		s.mu.Lock()
		claimed := s.turnActive
		s.mu.Unlock()
		if claimed {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// A prompt now queues rather than running concurrently.
	if err := s.Prompt(context.Background(), text("hi")); err != nil {
		t.Fatalf("prompt during a shell command: %v", err)
	}
	s.mu.Lock()
	queued := len(s.promptQueue)
	s.mu.Unlock()
	if queued != 1 {
		t.Fatalf("prompt queue = %d, want the prompt to queue behind the command", queued)
	}
	close(ds.release)
}

// TestShellRejectsEveryBusyState proves a command cannot start on top of work
// already in flight.
func TestShellRejectsEveryBusyState(t *testing.T) {
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
			ds := &shellDialect{allow: true}
			s := newHookSession(t, ds)
			s.mu.Lock()
			tc.set(s)
			s.mu.Unlock()

			if err := s.Shell(context.Background(), "ls"); !errors.Is(err, provider.ErrTurnBusy) {
				t.Fatalf("err = %v, want ErrTurnBusy", err)
			}
			if ds.count() != 0 {
				t.Fatalf("a command was submitted on a busy session (%d)", ds.count())
			}
		})
	}
}

// TestConcurrentShellRequestsRunOne proves only one command can claim the slot.
func TestConcurrentShellRequestsRunOne(t *testing.T) {
	ds := &shellDialect{allow: true, release: make(chan struct{})}
	s := newHookSession(t, ds)

	var wg sync.WaitGroup
	busy := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			busy <- s.Shell(context.Background(), "ls")
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(ds.release)
	wg.Wait()
	close(busy)

	var ok, rejected int
	for err := range busy {
		if err == nil {
			ok++
		} else if errors.Is(err, provider.ErrTurnBusy) {
			rejected++
		}
	}
	if ok == 0 {
		t.Fatal("no command ran at all")
	}
	if ok+rejected != 8 {
		t.Fatalf("ok=%d rejected=%d, want 8 accounted for", ok, rejected)
	}
	// Whatever ran, the dialect saw exactly as many submissions as succeeded.
	if ds.count() != ok {
		t.Fatalf("dialect saw %d submissions for %d successes", ds.count(), ok)
	}
}

// TestShellEndsTheTurnExactlyOnce proves an upstream idle that already ended
// the turn does not produce a second idle the client renders as a spurious
// turn boundary.
func TestShellEndsTheTurnExactlyOnce(t *testing.T) {
	ds := &shellDialect{allow: true}
	s := newHookSession(t, ds)
	if err := s.Shell(context.Background(), "ls"); err != nil {
		t.Fatal(err)
	}
	var idles int
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionStatus && ev.Status == "idle" {
			idles++
		}
	}
	if idles != 1 {
		t.Fatalf("emitted %d idle events, want exactly 1", idles)
	}
}

// TestShellFailureReturnsToIdleWithoutRetry proves a failed submission leaves
// the session usable and does not resubmit.
func TestShellFailureReturnsToIdleWithoutRetry(t *testing.T) {
	want := errors.New("upstream refused")
	ds := &shellDialect{allow: true, err: want}
	s := newHookSession(t, ds)

	if err := s.Shell(context.Background(), "ls"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if ds.count() != 1 {
		t.Fatalf("submitted %d times; a failure must not retry", ds.count())
	}
	s.mu.Lock()
	active := s.turnActive
	s.mu.Unlock()
	if active {
		t.Fatal("a failed command left the session stuck busy")
	}
	var idles int
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionStatus && ev.Status == "idle" {
			idles++
		}
	}
	if idles != 1 {
		t.Fatalf("failure emitted %d idle events, want 1", idles)
	}
}

// TestShellWithoutDialectSupport proves other providers refuse cleanly.
func TestShellWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	if err := s.Shell(context.Background(), "ls"); err == nil {
		t.Fatal("shell was allowed without dialect support")
	}
}

// TestShellSessionInterfaceIsSatisfied is the contract the manager asserts.
func TestShellSessionInterfaceIsSatisfied(t *testing.T) {
	var _ provider.ShellSession = newHookSession(t, nil)
}
