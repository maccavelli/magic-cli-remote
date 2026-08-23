package opencode

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func discoveryDialect() *httpDialect { return &httpDialect{log: slog.Default()} }

// errAPI returns a transport failure for every request.
func errAPI(msg string) httpagent.API {
	return func(context.Context, string, string, any, any) error { return fmt.Errorf("%s", msg) }
}

// ---------------------------------------------------------------------------
// Root-session discovery (MADR 0112 A1)
// ---------------------------------------------------------------------------

func TestListAgentSessionsLiveRequestsRootsOnly(t *testing.T) {
	var gotMethod, gotPath string
	api := func(_ context.Context, method, path string, _, out any) error {
		gotMethod, gotPath = method, path
		return jsonAPI(`[]`)(context.Background(), method, path, nil, out)
	}
	if _, err := discoveryDialect().ListAgentSessionsLive(context.Background(), api); err != nil {
		t.Fatalf("ListAgentSessionsLive: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET; discovery must never mutate", gotMethod)
	}
	if gotPath != "/session?roots=true&limit=100" {
		t.Errorf("path = %q, want the bounded roots-only query", gotPath)
	}
}

func TestListAgentSessionsLiveMapsFields(t *testing.T) {
	const body = `[{
		"id":"ses_alpha",
		"parentID":null,
		"directory":"/work/repo",
		"title":"Refactor the parser",
		"agent":"build",
		"model":{"providerID":"opencode","id":"big-pickle","variant":"high"},
		"cost":0.25,
		"tokens":{"input":1200,"output":340,"reasoning":10,"cache":{"read":800,"write":5}},
		"time":{"created":1787440000000,"updated":1787440099000}
	}]`
	got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListAgentSessionsLive: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	s := got[0]
	if s.ID != "ses_alpha" || s.CWD != "/work/repo" || s.Title != "Refactor the parser" {
		t.Errorf("identity/title/cwd wrong: %+v", s)
	}
	if s.Agent != "build" {
		t.Errorf("agent = %q, want build", s.Agent)
	}
	if s.ModelID != "opencode/big-pickle" {
		t.Errorf("model = %q, want the full provider/model id", s.ModelID)
	}
	if s.ThinkingLevel != "high" {
		t.Errorf("thinking level = %q, want high", s.ThinkingLevel)
	}
	if want := time.UnixMilli(1787440099000).UTC(); !s.UpdatedAt.Equal(want) {
		t.Errorf("updated = %v, want %v", s.UpdatedAt, want)
	}
	if s.Aggregate == nil {
		t.Fatal("aggregate usage missing")
	}
	if s.Aggregate.Input != 1200 || s.Aggregate.Output != 340 || s.Aggregate.Reasoning != 10 ||
		s.Aggregate.CacheRead != 800 || s.Aggregate.CacheWrite != 5 {
		t.Errorf("token buckets wrong: %+v", *s.Aggregate)
	}
	if s.Aggregate.CostUSD == nil || *s.Aggregate.CostUSD != 0.25 {
		t.Errorf("cost = %v, want 0.25", s.Aggregate.CostUSD)
	}
}

// A session that never ran a model turn returns explicit nulls for model,
// agent, parentID and share — the observed 1.18.21 shape for a fresh session.
func TestListAgentSessionsLiveHandlesNullModelAndAgent(t *testing.T) {
	const body = `[{
		"id":"ses_fresh","parentID":null,"directory":"/work","title":"probe",
		"agent":null,"model":null,"share":null,
		"cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},
		"time":{"created":1787440000000,"updated":1787440000000}
	}]`
	got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListAgentSessionsLive: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	s := got[0]
	if s.ModelID != "" || s.Agent != "" || s.ThinkingLevel != "" {
		t.Errorf("null fields must stay empty, got %+v", s)
	}
	// Zero usage is a real answer — a free session — and must be distinguishable
	// from an agent that reported nothing at all.
	if s.Aggregate == nil {
		t.Fatal("a session reporting zero usage must carry a present, zero aggregate")
	}
	if s.Aggregate.CostUSD == nil || *s.Aggregate.CostUSD != 0 {
		t.Errorf("a reported zero cost must survive as zero, got %v", s.Aggregate.CostUSD)
	}
}

func TestListAgentSessionsLiveFiltersAndBounds(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantIDs []string
	}{
		{
			name: "child sessions are never resumable roots",
			body: `[
				{"id":"ses_root","parentID":null,"time":{"updated":2}},
				{"id":"ses_child","parentID":"ses_root","time":{"updated":3}}
			]`,
			wantIDs: []string{"ses_root"},
		},
		{
			// A blank parentID string is not a parent.
			name:    "blank parentID counts as a root",
			body:    `[{"id":"ses_root","parentID":"  ","time":{"updated":1}}]`,
			wantIDs: []string{"ses_root"},
		},
		{
			name:    "an absent parentID counts as a root",
			body:    `[{"id":"ses_root","time":{"updated":1}}]`,
			wantIDs: []string{"ses_root"},
		},
		{
			name:    "rows without an id are dropped",
			body:    `[{"id":"","time":{"updated":1}},{"id":"  ","time":{"updated":2}},{"id":"ses_ok","time":{"updated":3}}]`,
			wantIDs: []string{"ses_ok"},
		},
		{
			name: "newest first",
			body: `[
				{"id":"ses_old","time":{"updated":100}},
				{"id":"ses_new","time":{"updated":300}},
				{"id":"ses_mid","time":{"updated":200}}
			]`,
			wantIDs: []string{"ses_new", "ses_mid", "ses_old"},
		},
		{
			// Batch operations really do stamp identical update times; without
			// a tie-break the picker reshuffles under the user's finger.
			name: "identical timestamps break the tie on id",
			body: `[
				{"id":"ses_c","time":{"updated":100}},
				{"id":"ses_a","time":{"updated":100}},
				{"id":"ses_b","time":{"updated":100}}
			]`,
			wantIDs: []string{"ses_a", "ses_b", "ses_c"},
		},
		{
			name:    "created stands in for a missing update time",
			body:    `[{"id":"ses_x","time":{"created":500}}]`,
			wantIDs: []string{"ses_x"},
		},
		{
			name:    "an empty list is not an error",
			body:    `[]`,
			wantIDs: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(c.body))
			if err != nil {
				t.Fatalf("ListAgentSessionsLive: %v", err)
			}
			var ids []string
			for _, s := range got {
				ids = append(ids, s.ID)
			}
			if len(ids) != len(c.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, c.wantIDs)
			}
			for i := range ids {
				if ids[i] != c.wantIDs[i] {
					t.Fatalf("ids = %v, want %v", ids, c.wantIDs)
				}
			}
		})
	}
}

func TestListAgentSessionsLiveCapsAtOneHundred(t *testing.T) {
	body := "["
	for i := 0; i < 150; i++ {
		if i > 0 {
			body += ","
		}
		body += fmt.Sprintf(`{"id":"ses_%03d","time":{"updated":%d}}`, i, 1000+i)
	}
	body += "]"

	got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListAgentSessionsLive: %v", err)
	}
	if len(got) != maxListedSessions {
		t.Fatalf("got %d sessions, want the %d cap", len(got), maxListedSessions)
	}
	// The cap is applied after sorting, so it keeps the newest, not an
	// arbitrary prefix of the engine's ordering.
	if got[0].ID != "ses_149" {
		t.Errorf("first entry = %q, want the newest ses_149", got[0].ID)
	}
}

func TestListAgentSessionsLiveClipsOversizedFields(t *testing.T) {
	long := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		return string(b)
	}
	body := fmt.Sprintf(`[{"id":%q,"title":%q,"directory":%q,"agent":%q,"time":{"updated":1}}]`,
		long(maxSessionIDLen+50), long(maxSessionTitleLen+50),
		long(maxSessionCWDLen+50), long(maxAgentLen+50))

	got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListAgentSessionsLive: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	s := got[0]
	for name, pair := range map[string][2]int{
		"id":    {len(s.ID), maxSessionIDLen},
		"title": {len(s.Title), maxSessionTitleLen},
		"cwd":   {len(s.CWD), maxSessionCWDLen},
		"agent": {len(s.Agent), maxAgentLen},
	} {
		// clip appends an ellipsis, so the bound is the limit plus that marker.
		if pair[0] > pair[1]+len("…") {
			t.Errorf("%s is %d bytes, want at most %d", name, pair[0], pair[1])
		}
	}
}

// "default" is OpenCode's sentinel for "no variant override". Surfacing it as a
// rung would show every default session as though the user had chosen one
// (MADR 0112 A14).
func TestListAgentSessionsLiveDropsDefaultVariantSentinel(t *testing.T) {
	for _, variant := range []string{"default", "  default  ", "", "   "} {
		body := fmt.Sprintf(
			`[{"id":"ses_a","model":{"providerID":"opencode","id":"big-pickle","variant":%q},"time":{"updated":1}}]`,
			variant)
		got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
		if err != nil {
			t.Fatalf("variant %q: %v", variant, err)
		}
		if got[0].ThinkingLevel != "" {
			t.Errorf("variant %q surfaced as thinking level %q, want none", variant, got[0].ThinkingLevel)
		}
	}
}

// A partial model reference cannot form a usable id, so it must be omitted
// rather than emitted as "opencode/" or "/big-pickle".
func TestListAgentSessionsLiveRequiresBothModelHalves(t *testing.T) {
	for _, body := range []string{
		`[{"id":"a","model":{"providerID":"opencode","id":""},"time":{"updated":1}}]`,
		`[{"id":"a","model":{"providerID":"","id":"big-pickle"},"time":{"updated":1}}]`,
		`[{"id":"a","model":{"providerID":"  ","id":"  "},"time":{"updated":1}}]`,
	} {
		got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(body))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].ModelID != "" {
			t.Errorf("partial model reference produced %q, want empty", got[0].ModelID)
		}
	}
}

func TestSessionUsageRejectsUnusableNumbers(t *testing.T) {
	cases := []struct {
		name string
		body string
		want provider.AgentSessionUsage
		// nilCost is true when the cost must be dropped entirely.
		nilCost bool
	}{
		{
			name:    "negative token counts clamp to zero",
			body:    `[{"id":"a","tokens":{"input":-5,"output":-1,"reasoning":-2,"cache":{"read":-3,"write":-4}},"time":{"updated":1}}]`,
			want:    provider.AgentSessionUsage{},
			nilCost: true,
		},
		{
			name:    "a negative cost is dropped, not shown as a credit",
			body:    `[{"id":"a","cost":-1.5,"time":{"updated":1}}]`,
			want:    provider.AgentSessionUsage{},
			nilCost: true,
		},
		{
			name: "fractional token counts truncate",
			body: `[{"id":"a","tokens":{"input":10.9,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},"time":{"updated":1}}]`,
			want: provider.AgentSessionUsage{Input: 10},
			// no cost field at all
			nilCost: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := discoveryDialect().ListAgentSessionsLive(context.Background(), jsonAPI(c.body))
			if err != nil {
				t.Fatal(err)
			}
			u := got[0].Aggregate
			if u == nil {
				t.Fatal("aggregate missing")
			}
			if u.Input != c.want.Input || u.Output != c.want.Output || u.Reasoning != c.want.Reasoning ||
				u.CacheRead != c.want.CacheRead || u.CacheWrite != c.want.CacheWrite {
				t.Errorf("buckets = %+v, want %+v", *u, c.want)
			}
			if c.nilCost && u.CostUSD != nil {
				t.Errorf("cost = %v, want nil", *u.CostUSD)
			}
		})
	}

	t.Run("no accounting at all yields no aggregate", func(t *testing.T) {
		got, err := discoveryDialect().ListAgentSessionsLive(context.Background(),
			jsonAPI(`[{"id":"a","time":{"updated":1}}]`))
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Aggregate != nil {
			t.Errorf("aggregate = %+v, want nil when the engine reported none", *got[0].Aggregate)
		}
	})
}

func TestListAgentSessionsLivePropagatesTransportError(t *testing.T) {
	_, err := discoveryDialect().ListAgentSessionsLive(context.Background(), errAPI("engine down"))
	if err == nil {
		t.Fatal("a failed listing must not read as an empty list: the user would start a duplicate session")
	}
}

func TestListAgentSessionsLiveHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := func(ctx context.Context, _, _ string, _, _ any) error { return ctx.Err() }
	if _, err := discoveryDialect().ListAgentSessionsLive(ctx, api); err == nil {
		t.Fatal("expected the cancelled context to surface")
	}
}

// ---------------------------------------------------------------------------
// Project discovery (MADR 0112 A1)
// ---------------------------------------------------------------------------

func TestListProjectsLiveMapsAndSorts(t *testing.T) {
	const body = `[
		{"id":"p2","name":"zeta","worktree":"/work/zeta"},
		{"id":"p1","name":"alpha","worktree":"/work/alpha"},
		{"id":"p3","worktree":"/work/derived-name"}
	]`
	got, err := discoveryDialect().ListProjectsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListProjectsLive: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d projects, want 3", len(got))
	}
	wantNames := []string{"alpha", "derived-name", "zeta"}
	for i, w := range wantNames {
		if got[i].Name != w {
			t.Fatalf("names = %v, want %v", []string{got[0].Name, got[1].Name, got[2].Name}, wantNames)
		}
	}
	// A project with no name falls back to the worktree's base name, never the
	// full path — rendering the path is exactly what discovery avoids.
	for _, p := range got {
		if p.Name == p.Worktree {
			t.Errorf("project %q used its full path as a display name", p.ID)
		}
	}
}

func TestListProjectsLiveRejectsTheRootSentinel(t *testing.T) {
	// The global/"/" entry is registered the first time a non-Git directory is
	// opened, and then persists. Offering it would root a session at the whole
	// filesystem.
	const body = `[
		{"id":"global","worktree":"/"},
		{"id":"real","name":"repo","worktree":"/work/repo"}
	]`
	got, err := discoveryDialect().ListProjectsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListProjectsLive: %v", err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("got %+v, want only the real project", got)
	}
}

// The unsafe property is the worktree, not the id. An engine that registered
// the filesystem root under some other id must not slip through, and a real
// project that happens to use the sentinel id must not be dropped.
func TestListProjectsLiveRejectsAnyRootWorktree(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		// The id alone is not disqualifying: only the root worktree is unsafe.
		{"global id at a real worktree is kept", `[{"id":"global","worktree":"/work/repo"}]`, 1},
		// The worktree alone *is* disqualifying, whatever the id — a session
		// rooted at "/" has the whole machine as its working directory.
		{"root worktree under another id is rejected", `[{"id":"other","worktree":"/"}]`, 0},
		{"the sentinel pair is rejected", `[{"id":"global","worktree":"/"}]`, 0},
		{"a redundant root spelling is rejected", `[{"id":"other","worktree":"//"}]`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := discoveryDialect().ListProjectsLive(context.Background(), jsonAPI(c.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.want {
				t.Errorf("got %d projects, want %d: %+v", len(got), c.want, got)
			}
		})
	}
}

func TestListProjectsLiveDropsUnusableRows(t *testing.T) {
	const body = `[
		{"id":"","worktree":"/work/a"},
		{"id":"no-worktree","worktree":""},
		{"id":"relative","worktree":"work/relative"},
		{"id":"ok","name":"ok","worktree":"/work/ok"}
	]`
	got, err := discoveryDialect().ListProjectsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListProjectsLive: %v", err)
	}
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("got %+v, want only the usable row", got)
	}
}

func TestListProjectsLiveCapsAndClips(t *testing.T) {
	body := "["
	for i := 0; i < 150; i++ {
		if i > 0 {
			body += ","
		}
		body += fmt.Sprintf(`{"id":"p%03d","name":"n%03d","worktree":"/work/%03d"}`, i, i, i)
	}
	body += "]"
	got, err := discoveryDialect().ListProjectsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxListedProjects {
		t.Fatalf("got %d projects, want the %d cap", len(got), maxListedProjects)
	}

	long := make([]byte, maxProjectNameLen+50)
	for i := range long {
		long[i] = 'x'
	}
	got, err = discoveryDialect().ListProjectsLive(context.Background(),
		jsonAPI(fmt.Sprintf(`[{"id":"p","name":%q,"worktree":"/work/p"}]`, string(long))))
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].Name) > maxProjectNameLen+len("…") {
		t.Errorf("name is %d bytes, want at most %d", len(got[0].Name), maxProjectNameLen)
	}
}

func TestListProjectsLivePropagatesTransportError(t *testing.T) {
	if _, err := discoveryDialect().ListProjectsLive(context.Background(), errAPI("engine down")); err == nil {
		t.Fatal("a failed project listing must surface, not read as an empty catalog")
	}
}

// Discovery is read-only. A spy proves neither call can reach a mutation.
func TestDiscoveryIssuesOnlyGETRequests(t *testing.T) {
	var methods []string
	api := func(_ context.Context, method, path string, body, out any) error {
		methods = append(methods, method)
		if body != nil {
			t.Errorf("%s %s carried a request body; discovery must be read-only", method, path)
		}
		return jsonAPI(`[]`)(context.Background(), method, path, nil, out)
	}
	d := discoveryDialect()
	if _, err := d.ListAgentSessionsLive(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ListProjectsLive(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	for _, m := range methods {
		if m != "GET" {
			t.Errorf("discovery issued a %s request", m)
		}
	}
	if len(methods) != 2 {
		t.Errorf("expected exactly two requests, got %d", len(methods))
	}
}
