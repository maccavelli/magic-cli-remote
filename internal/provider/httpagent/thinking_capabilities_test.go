package httpagent

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// capableDialect implements the optional thinking/capability hooks so the
// transport's forwarding can be observed. MADR 0112 A2/A14.
type capableDialect struct {
	fakeDialectSession
	level    string
	setErr   error
	image    bool
	audio    bool
	refined  int
	setCalls []string
}

func (c *capableDialect) SetThinkingLevel(_ context.Context, level string) error {
	c.setCalls = append(c.setCalls, level)
	if c.setErr != nil {
		return c.setErr
	}
	c.level = level
	return nil
}
func (c *capableDialect) ThinkingLevel() string { return c.level }
func (c *capableDialect) PromptCapabilities() (bool, bool) {
	return c.image, c.audio
}
func (c *capableDialect) AfterBootRefined() { c.refined++ }

// newHookSession builds an unregistered session with the given dialect.
func newHookSession(t *testing.T, ds DialectSession) *session {
	t.Helper()
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s := &session{
		p:          p,
		localID:    "local-1",
		agentID:    "agent-1",
		events:     make(chan event.Event, 64),
		done:       make(chan struct{}),
		pending:    map[string]struct{}{},
		permOrigin: map[string]string{},
		treeNodes:  map[string]NodeStatus{},
		log:        p.log,
	}
	if ds == nil {
		ds = &fakeDialectSession{h: s}
	}
	s.ds = ds
	return s
}

// TestThinkingForwardsToDialect proves the transport delegates rather than
// deciding, so provider-specific validation stays in the dialect.
func TestThinkingForwardsToDialect(t *testing.T) {
	ds := &capableDialect{level: "default"}
	s := newHookSession(t, ds)

	if got := s.ThinkingLevel(); got != "default" {
		t.Fatalf("ThinkingLevel = %q, want %q", got, "default")
	}
	if err := s.SetThinkingLevel(context.Background(), "high"); err != nil {
		t.Fatal(err)
	}
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("level not forwarded: %q", got)
	}
	if len(ds.setCalls) != 1 || ds.setCalls[0] != "high" {
		t.Fatalf("dialect saw %v", ds.setCalls)
	}
}

// TestThinkingSurfacesDialectRejection proves a dialect refusal is propagated
// verbatim rather than swallowed into a silent no-op.
func TestThinkingSurfacesDialectRejection(t *testing.T) {
	want := errors.New("not advertised")
	s := newHookSession(t, &capableDialect{setErr: want})
	if err := s.SetThinkingLevel(context.Background(), "nope"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestThinkingWithoutDialectSupport proves a provider whose dialect has no
// effort control reports the empty level and refuses a change, instead of
// pretending the setting took.
func TestThinkingWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	if got := s.ThinkingLevel(); got != "" {
		t.Fatalf("ThinkingLevel = %q, want empty", got)
	}
	if err := s.SetThinkingLevel(context.Background(), "high"); err == nil {
		t.Fatal("a dialect without thinking accepted a level")
	}
}

// TestThinkingSessionInterfaceIsSatisfied is the contract the session manager
// type-asserts to advertise /thinking.
func TestThinkingSessionInterfaceIsSatisfied(t *testing.T) {
	var _ provider.ThinkingSession = newHookSession(t, nil)
}

// TestStartThinkingLevelIsCarried proves StartOptions reaches the dialect,
// which previously ignored it entirely (PLAN P3 step 6).
func TestStartThinkingLevelIsCarried(t *testing.T) {
	s := newHookSession(t, nil)
	if got := s.StartThinkingLevel(); got != "" {
		t.Fatalf("default = %q, want empty", got)
	}
	s.startThinkingLevel = "high"
	if got := s.StartThinkingLevel(); got != "high" {
		t.Fatalf("StartThinkingLevel = %q", got)
	}
}

// TestEmitCapabilitiesReflectsDialect proves the advertised inputs come from
// the dialect's active model, not a fixed transport constant.
func TestEmitCapabilitiesReflectsDialect(t *testing.T) {
	ds := &capableDialect{image: true, audio: false}
	s := newHookSession(t, ds)
	s.emitCapabilities()

	evs := drainEvents(s)
	var caps *event.Capabilities
	for _, ev := range evs {
		if ev.Type == event.TypeSessionCapabilities {
			caps = ev.Capabilities
		}
	}
	if caps == nil {
		t.Fatalf("no session_capabilities emitted; got %+v", evs)
	}
	if !caps.Image || caps.Audio {
		t.Fatalf("capabilities = %+v, want image only", caps)
	}

	ds.audio = true
	s.emitCapabilities()
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionCapabilities {
			caps = ev.Capabilities
		}
	}
	if !caps.Image || !caps.Audio {
		t.Fatalf("capabilities did not follow the model: %+v", caps)
	}
}

// TestEmitCapabilitiesSilentWithoutDialectSupport proves providers that cannot
// answer stay silent rather than publishing an all-false claim.
func TestEmitCapabilitiesSilentWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	s.emitCapabilities()
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionCapabilities {
			t.Fatalf("a dialect without capabilities still emitted %+v", ev.Capabilities)
		}
	}
}

// TestPromptCapabilitiesDefaultsClosed proves the conservative answer: a
// dialect that cannot report inputs advertises none.
func TestPromptCapabilitiesDefaultsClosed(t *testing.T) {
	s := newHookSession(t, nil)
	if image, audio := s.promptCapabilities(); image || audio {
		t.Fatalf("promptCapabilities = %v/%v, want false/false", image, audio)
	}
}

// TestRefineModelSurfaceReRunsAndReEmits proves the post-catalog hook both
// re-resolves the dialect and republishes the result.
func TestRefineModelSurfaceReRunsAndReEmits(t *testing.T) {
	ds := &capableDialect{image: true}
	s := newHookSession(t, ds)
	s.refineModelSurface()
	if ds.refined != 1 {
		t.Fatalf("AfterBootRefined called %d times, want 1", ds.refined)
	}
	found := false
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeSessionCapabilities {
			found = true
		}
	}
	if !found {
		t.Fatal("refine did not re-emit capabilities")
	}
}

// TestRefineModelSurfaceWithoutHooks proves a dialect implementing neither
// hook is safe to refine — the transport must not assume either exists.
func TestRefineModelSurfaceWithoutHooks(t *testing.T) {
	s := newHookSession(t, nil)
	s.refineModelSurface() // must not panic
}

// TestRefineSessionsSnapshotsRegistered proves the provider fans the refresh
// out to registered sessions of the current generation only.
func TestRefineSessionsSnapshotsRegistered(t *testing.T) {
	ds := &capableDialect{image: true}
	s := newHookSession(t, ds)
	p := s.p
	p.mu.Lock()
	p.generation = 7
	p.sessions = map[string]*session{"agent-1": s}
	p.mu.Unlock()

	p.refineSessions(7)
	if ds.refined != 1 {
		t.Fatalf("refined %d times, want 1", ds.refined)
	}

	// A stale generation must be ignored: that engine is gone.
	p.refineSessions(6)
	if ds.refined != 1 {
		t.Fatalf("a stale generation still refreshed sessions (%d)", ds.refined)
	}

	// A closed provider must not touch its sessions either.
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.refineSessions(7)
	if ds.refined != 1 {
		t.Fatalf("a closed provider still refreshed sessions (%d)", ds.refined)
	}
}

// workspaceDialect implements the optional workspace hooks.
type workspaceDialect struct {
	fakeDialectSession
	entries []provider.WorkspaceEntry
	content provider.WorkspaceContent
	search  provider.WorkspaceSearch
	err     error
	calls   []string
}

func (d *workspaceDialect) ListWorkspace(_ context.Context, p string) ([]provider.WorkspaceEntry, error) {
	d.calls = append(d.calls, "list:"+p)
	return d.entries, d.err
}

func (d *workspaceDialect) ReadWorkspace(_ context.Context, p string) (provider.WorkspaceContent, error) {
	d.calls = append(d.calls, "read:"+p)
	return d.content, d.err
}

func (d *workspaceDialect) SearchWorkspace(_ context.Context, kind, q string) (provider.WorkspaceSearch, error) {
	d.calls = append(d.calls, "search:"+kind+":"+q)
	return d.search, d.err
}

// TestWorkspaceForwardsToDialect proves the transport delegates rather than
// deciding, so path validation stays in one place.
func TestWorkspaceForwardsToDialect(t *testing.T) {
	ds := &workspaceDialect{
		entries: []provider.WorkspaceEntry{{Name: "a", Path: "a"}},
		content: provider.WorkspaceContent{Path: "a.txt", Text: "x", Bytes: 1},
		search:  provider.WorkspaceSearch{Kind: "text", Cap: 10},
	}
	s := newHookSession(t, ds)
	ctx := context.Background()

	entries, err := s.ListWorkspace(ctx, "lib")
	if err != nil || len(entries) != 1 {
		t.Fatalf("list = %+v, %v", entries, err)
	}
	content, err := s.ReadWorkspace(ctx, "a.txt")
	if err != nil || content.Text != "x" {
		t.Fatalf("read = %+v, %v", content, err)
	}
	res, err := s.SearchWorkspace(ctx, "text", "q")
	if err != nil || res.Cap != 10 {
		t.Fatalf("search = %+v, %v", res, err)
	}
	want := []string{"list:lib", "read:a.txt", "search:text:q"}
	for i, w := range want {
		if ds.calls[i] != w {
			t.Fatalf("call %d = %q, want %q", i, ds.calls[i], w)
		}
	}
}

// TestWorkspaceSurfacesDialectErrors proves a validation refusal is propagated
// verbatim rather than flattened into a generic failure.
func TestWorkspaceSurfacesDialectErrors(t *testing.T) {
	want := errors.New("path_escape: nope")
	s := newHookSession(t, &workspaceDialect{err: want})
	ctx := context.Background()
	if _, err := s.ListWorkspace(ctx, ".."); !errors.Is(err, want) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := s.ReadWorkspace(ctx, ".."); !errors.Is(err, want) {
		t.Fatalf("read err = %v", err)
	}
	if _, err := s.SearchWorkspace(ctx, "text", "q"); !errors.Is(err, want) {
		t.Fatalf("search err = %v", err)
	}
}

// TestWorkspaceWithoutDialectSupport proves a provider without the surface
// refuses rather than returning an empty listing, which would read as "this
// project has no files".
func TestWorkspaceWithoutDialectSupport(t *testing.T) {
	s := newHookSession(t, nil)
	ctx := context.Background()
	if _, err := s.ListWorkspace(ctx, ""); err == nil {
		t.Fatal("list was allowed without dialect support")
	}
	if _, err := s.ReadWorkspace(ctx, "a"); err == nil {
		t.Fatal("read was allowed without dialect support")
	}
	if _, err := s.SearchWorkspace(ctx, "text", "q"); err == nil {
		t.Fatal("search was allowed without dialect support")
	}
	if s.supportsWorkspace() {
		t.Fatal("supportsWorkspace is true without the interface")
	}
}

// TestWorkspaceCapabilityFollowsTheDialect proves the advertised flag matches
// what the session can actually do.
func TestWorkspaceCapabilityFollowsTheDialect(t *testing.T) {
	with := newHookSession(t, &workspaceDialect{})
	if !with.supportsWorkspace() {
		t.Fatal("a workspace dialect was not detected")
	}
	with.emitCapabilities()
	var seen *event.Capabilities
	for _, ev := range drainEvents(with) {
		if ev.Type == event.TypeSessionCapabilities {
			seen = ev.Capabilities
		}
	}
	if seen != nil && !seen.WorkspaceRead {
		t.Fatalf("capabilities did not advertise workspace_read: %+v", seen)
	}

	// A dialect that reports capabilities but no workspace keeps it false.
	plain := newHookSession(t, &capableDialect{image: true})
	plain.emitCapabilities()
	for _, ev := range drainEvents(plain) {
		if ev.Type == event.TypeSessionCapabilities && ev.Capabilities.WorkspaceRead {
			t.Fatal("workspace_read is true without the interface")
		}
	}
}

// TestWorkspaceSessionInterfaceIsSatisfied is the contract the manager asserts.
func TestWorkspaceSessionInterfaceIsSatisfied(t *testing.T) {
	var _ provider.WorkspaceSession = newHookSession(t, nil)
}
