package grok

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// testSession is a grok session as grok's tests use it: the Host its hooks
// receive, and the operations the session offers (MADR 0208, P6b deviation 1).
type testSession interface {
	acpagent.Host
	provider.Session
	provider.PermissionSession
	provider.QuestionDeclineSession
	provider.UndoSession
	provider.ForkSession
	provider.RuntimeSession
	provider.MCPStatusSession
}

// startSession starts a session with grok's Spec on acptest's fixture agent.
// The agent advertises grok's cached token, as an installed grok does, so the
// start authenticates headlessly.
func startSession(t *testing.T, cfg Config, opts acptest.Options) (testSession, *acptest.Agent) {
	t.Helper()
	if opts.AuthMethods == nil {
		opts.AuthMethods = []string{"cached_token"}
	}
	s, a := acptest.Start(t, New(cfg), opts)
	ts, ok := s.(testSession)
	if !ok {
		t.Fatalf("%T is not a testSession", s)
	}
	return ts, a
}

// newHostSession is a grok session whose agent id is "agent-q", the id the
// notification tests' frames name.
func newHostSession(t *testing.T) testSession {
	t.Helper()
	return newSession(t, "agent-q")
}

// newSession is a grok session whose agent id is id.
func newSession(t *testing.T, id string) testSession {
	t.Helper()
	s, _ := startSession(t, Config{}, acptest.Options{SessionID: id})
	return s
}

// startTurn starts a turn whose prompt the agent holds unanswered, and drains
// what the start emitted.
func startTurn(t *testing.T, s testSession, a *acptest.Agent) acptest.Message {
	t.Helper()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	m := a.Await("session/prompt")
	acptest.Drain(s)
	return m
}

// replayHost is a session's Host as a session/load replay of agent session id
// sees it: Loading reports loading, and every other call, the Spec's state
// included, is the session's own.
type replayHost struct {
	testSession
	id      string
	loading bool
}

func (r *replayHost) AgentSessionID() string { return r.id }
func (r *replayHost) Loading() bool          { return r.loading }

// identityHost is a session's Host with its agent session id and working
// directory replaced, for an operation's checks on a missing one.
type identityHost struct {
	testSession
	id, cwd string
}

func (i *identityHost) AgentSessionID() string { return i.id }
func (i *identityHost) CWD() string            { return i.cwd }

// declineRecorder is a session's Host that records each Decline.
type declineRecorder struct {
	testSession
	mu       sync.Mutex
	declined []string
}

func (d *declineRecorder) Decline(variant string) {
	d.mu.Lock()
	d.declined = append(d.declined, variant)
	d.mu.Unlock()
	d.testSession.Decline(variant)
}

func (d *declineRecorder) got() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.declined)
}

// compactionState is the part of a compaction event the tests compare.
type compactionState struct {
	ID, Status, Summary, Error string
	Truncated                  bool
}

// drainCompactions returns the compaction events s has queued.
func drainCompactions(s testSession) []compactionState {
	var out []compactionState
	for _, ev := range acptest.Drain(s) {
		if ev.Type == event.TypeCompaction {
			out = append(out, compactionState{ev.CompactionID, ev.CompactionStatus,
				ev.CompactionSummary, ev.CompactionError, ev.CompactionTruncated})
		}
	}
	return out
}

// mcpStatus is the session's MCP server list.
func mcpStatus(t *testing.T, s testSession) []provider.MCPServerStatus {
	t.Helper()
	st, err := s.MCPStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// scriptedSession is a grok session whose agent answers each method in
// replies with its raw JSON result, and any other with method-not-found.
func scriptedSession(t *testing.T, replies map[string]string) (testSession, *acptest.Agent) {
	t.Helper()
	return startSession(t, Config{}, acptest.Options{Replies: replies})
}

// newRequestSession is a grok session for grok's extension requests, whose
// permission and question timeout is timeout (0 for none). Its agent id is
// "a1", the id planParams and askParams name.
func newRequestSession(t *testing.T, timeout time.Duration) testSession {
	t.Helper()
	s, _ := startSession(t, Config{PermissionTimeout: timeout}, acptest.Options{SessionID: "a1"})
	return s
}

// callExtension runs grok's handler for the extension request method on its
// own goroutine, as the SDK does (it blocks on the client's answer), and hands
// back its result.
func callExtension(ctx context.Context, s testSession, method string, params json.RawMessage) <-chan any {
	h, ok := spec.ExtensionRequests[method]
	out := make(chan any, 1)
	go func() {
		if !ok {
			out <- acpagent.ExtensionRequestHandler(nil)
			return
		}
		res, err := h(ctx, s, params)
		if err != nil {
			out <- err
			return
		}
		out <- res
	}()
	return out
}

// eventsUntil returns s's events up to and including the first of type want,
// or fails.
func eventsUntil(t *testing.T, s testSession, want event.Type) []event.Event {
	t.Helper()
	var out []event.Event
	deadline := time.After(acptest.Budget)
	for {
		select {
		case ev := <-s.Events():
			out = append(out, ev)
			if ev.Type == want {
				return out
			}
		case <-deadline:
			t.Fatalf("no %s event within %s; got %v", want, acptest.Budget, out)
			return nil
		}
	}
}

// waitEvent returns the next event of type want, skipping others.
func waitEvent(t *testing.T, s testSession, want event.Type) event.Event {
	t.Helper()
	return acptest.Next(t, s, want)
}
