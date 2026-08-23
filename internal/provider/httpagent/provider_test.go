package httpagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestWithConfiguredDefaultOverridesLiveDefault(t *testing.T) {
	cat := picker.SingleCatalog(picker.SourceLive, []picker.Option{{ID: "engine/default"}}, "engine/default", true)
	got := withConfiguredDefault(cat, "opencode-go/deepseek-v4-flash")
	if len(got.DefaultIDs) != 1 || got.DefaultIDs[0] != "opencode-go/deepseek-v4-flash" {
		t.Fatalf("defaults=%v", got.DefaultIDs)
	}
	if unchanged := withConfiguredDefault(cat, ""); unchanged.DefaultIDs[0] != "engine/default" {
		t.Fatalf("empty config changed defaults=%v", unchanged.DefaultIDs)
	}
}

// An engine that dies instantly (bad binary, immediate crash) must fail startup
// at once, not spin the full serverStartTimeout probing a corpse on
// connection-refused. `false` exits non-zero immediately, so the health poll
// never connects; the fix watches cmd.Wait and bails as soon as it exits.
func TestStartServerBailsWhenEngineExitsImmediately(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("no 'false' binary on PATH")
	}
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	start := time.Now()
	_, err := p.startServer(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when engine exits immediately")
	}
	// Must return promptly — well under serverStartTimeout, which the buggy code
	// would spin in full before failing.
	if elapsed > 5*time.Second {
		t.Fatalf("startServer took %s; expected prompt failure well under serverStartTimeout (%s)",
			elapsed, serverStartTimeout)
	}
	if !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("error=%q, want it to mention 'exited during startup'", err)
	}
}

// MADR 0074 D16: the vendor catalog is a multi-megabyte read on the real
// engines (4.7 MB from opencode 1.18.16), and the phone pages through it and
// searches it, so it is cached. A credential write must drop that cache,
// because the catalog carries the per-vendor status the user just changed.
func TestAuthCatalogCacheHitsThenInvalidates(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("a fresh provider reported a cached catalog")
	}
	p.storeCatalog(provider.AuthCatalog{
		Upstreams: []provider.UpstreamAuth{{ID: "togetherai"}},
		Source:    provider.AuthCatalogSourceEngine,
	})
	got, ok := p.cachedCatalog()
	if !ok || len(got.Upstreams) != 1 || got.Upstreams[0].ID != "togetherai" {
		t.Fatalf("cache miss or wrong contents: ok=%v got=%+v", ok, got)
	}

	p.InvalidateAuthCatalog()
	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("catalog survived invalidation; a stale status would be shown after a write")
	}
}

// An expired entry is a miss, so a vendor list that changed under a long-lived
// daemon is picked up without a restart.
func TestAuthCatalogCacheExpires(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	p.storeCatalog(provider.AuthCatalog{Upstreams: []provider.UpstreamAuth{{ID: "x"}}})
	p.authCatalogMu.Lock()
	p.authCatalogExpiry = time.Now().Add(-time.Second)
	p.authCatalogMu.Unlock()
	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("an expired catalog was served from cache")
	}
}

// ---------------------------------------------------------------------------
// MADR 0112 A1 — provider-native discovery forwarding
// ---------------------------------------------------------------------------

// discoveryDialect implements the two optional discovery interfaces on top of
// the shared fake, so the forwarding contract can be tested without an engine.
type discoveryDialect struct {
	fakeDialect
	sessions    []provider.AgentSessionMeta
	projects    []provider.ProjectMeta
	sessionsErr error
	projectsErr error
	sessionCall int
	projectCall int
}

func (d *discoveryDialect) ListAgentSessionsLive(context.Context, API) ([]provider.AgentSessionMeta, error) {
	d.sessionCall++
	return d.sessions, d.sessionsErr
}

func (d *discoveryDialect) ListProjectsLive(context.Context, API) ([]provider.ProjectMeta, error) {
	d.projectCall++
	return d.projects, d.projectsErr
}

// A dialect without the optional interfaces must report the operation as
// unsupported rather than returning an empty list. "None found" and "cannot
// look" are different answers, and only one of them should invite the user to
// start a fresh session.
func TestDiscoveryUnsupportedWithoutDialectSupport(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	if _, err := p.ListAgentSessions(context.Background()); err == nil {
		t.Error("ListAgentSessions must fail on a dialect without discovery")
	} else if !strings.Contains(err.Error(), "native session discovery") {
		t.Errorf("error %q should name the missing capability", err)
	}

	if _, err := p.ListProjects(context.Background()); err == nil {
		t.Error("ListProjects must fail on a dialect without discovery")
	} else if !strings.Contains(err.Error(), "project discovery") {
		t.Errorf("error %q should name the missing capability", err)
	}
}

// Discovery needs a reachable engine. A missing binary must surface as an
// error, never as an empty catalog.
func TestDiscoveryRequiresAReachableEngine(t *testing.T) {
	d := &discoveryDialect{fakeDialect: fakeDialect{id: "test"}}
	p := NewWithLogger(d, Config{Bin: "definitely-not-a-real-binary-xyz"}, nil)

	if _, err := p.ListAgentSessions(context.Background()); err == nil {
		t.Error("expected an error when the engine binary is absent")
	}
	if _, err := p.ListProjects(context.Background()); err == nil {
		t.Error("expected an error when the engine binary is absent")
	}
	if d.sessionCall != 0 || d.projectCall != 0 {
		t.Errorf("the dialect must not be consulted without an engine (calls: %d, %d)",
			d.sessionCall, d.projectCall)
	}
}

// The provider satisfies both shared optional interfaces, so the WebSocket
// layer's type assertions find it.
func TestProviderImplementsDiscoveryInterfaces(t *testing.T) {
	p := NewWithLogger(&discoveryDialect{fakeDialect: fakeDialect{id: "test"}}, Config{Bin: "false"}, nil)
	if _, ok := any(p).(provider.AgentSessionLister); !ok {
		t.Error("Provider must implement provider.AgentSessionLister")
	}
	if _, ok := any(p).(provider.ProjectCatalog); !ok {
		t.Error("Provider must implement provider.ProjectCatalog")
	}
}

// withFakeEngine points the provider at an already-running test server, so the
// forwarding path can be exercised without spawning a real agent binary.
func withFakeEngine(t *testing.T, p *Provider, handler http.HandlerFunc) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	p.mu.Lock()
	p.eng = &engine{url: ts.URL, dead: make(chan struct{})}
	p.mu.Unlock()
}

func TestDiscoveryForwardsToDialectWhenEngineIsUp(t *testing.T) {
	cost := 0.5
	d := &discoveryDialect{
		fakeDialect: fakeDialect{id: "test"},
		sessions: []provider.AgentSessionMeta{{
			ID: "ses_1", CWD: "/w", Title: "t",
			ModelID: "opencode/big-pickle", Agent: "build", ThinkingLevel: "high",
			Aggregate: &provider.AgentSessionUsage{Input: 10, CostUSD: &cost},
		}},
		projects: []provider.ProjectMeta{{ID: "p1", Name: "repo", Worktree: "/work/repo"}},
	}
	p := NewWithLogger(d, Config{Bin: "false"}, nil)
	withFakeEngine(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	sessions, err := p.ListAgentSessions(context.Background())
	if err != nil {
		t.Fatalf("ListAgentSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_1" {
		t.Fatalf("sessions = %+v", sessions)
	}
	// The additive metadata must survive forwarding untouched: the transport
	// is a pass-through, not a second place that can drop fields.
	got := sessions[0]
	if got.ModelID != "opencode/big-pickle" || got.Agent != "build" || got.ThinkingLevel != "high" {
		t.Errorf("additive fields lost in transport: %+v", got)
	}
	if got.Aggregate == nil || got.Aggregate.CostUSD == nil || *got.Aggregate.CostUSD != cost {
		t.Errorf("aggregate lost in transport: %+v", got.Aggregate)
	}

	projects, err := p.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Worktree != "/work/repo" {
		t.Fatalf("projects = %+v", projects)
	}
	if d.sessionCall != 1 || d.projectCall != 1 {
		t.Errorf("dialect call counts = %d, %d, want 1, 1", d.sessionCall, d.projectCall)
	}
}

// A dialect-level failure must surface, not degrade to an empty list.
func TestDiscoveryPropagatesDialectErrors(t *testing.T) {
	d := &discoveryDialect{
		fakeDialect: fakeDialect{id: "test"},
		sessionsErr: errors.New("session listing blew up"),
		projectsErr: errors.New("project listing blew up"),
	}
	p := NewWithLogger(d, Config{Bin: "false"}, nil)
	withFakeEngine(t, p, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := p.ListAgentSessions(context.Background()); err == nil {
		t.Error("a dialect error must surface")
	}
	if _, err := p.ListProjects(context.Background()); err == nil {
		t.Error("a dialect error must surface")
	}
}
