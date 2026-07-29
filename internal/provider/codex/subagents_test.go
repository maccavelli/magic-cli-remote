package codex

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func subagentSession(t *testing.T) *session {
	t.Helper()
	s := &session{
		localID: "local-1",
		agentID: "thread-1",
		events:  make(chan event.Event, 32),
		done:    make(chan struct{}),
		log:     slog.Default(),
	}
	t.Cleanup(func() { close(s.done) })
	return s
}

func drainSubagents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// TestCollabWaitProducesNoPhantomSubagent is the guard for the trap measured
// live twice on codex 0.145.0: the model calls the `wait` collab tool with
// `receiverThreadIds: []` and `agentsStates: {}` even when nothing was ever
// spawned. Deriving a panel entry from the item's existence would invent a
// sub-agent on every wait (MADR 0051 §10.3).
//
// Payload copied verbatim from a `codex exec --json` capture.
func TestCollabWaitProducesNoPhantomSubagent(t *testing.T) {
	s := subagentSession(t)
	item := json.RawMessage(`{"id":"item_1","type":"collabAgentToolCall",
		"tool":"wait","senderThreadId":"thread-1","receiverThreadIds":[],
		"prompt":null,"agentsStates":{},"status":"completed"}`)

	if !s.noteSubagentItem("collabAgentToolCall", item) {
		t.Fatal("collabAgentToolCall must be routed to the sub-agent set")
	}
	if evs := drainSubagents(s); len(evs) != 0 {
		t.Fatalf("emitted %v; a wait with no agents must not invent one", evs)
	}
}

func TestCollabAgentsStatesPopulateTheSet(t *testing.T) {
	s := subagentSession(t)
	s.noteSubagentItem("collabAgentToolCall", json.RawMessage(`{
		"id":"item_2","type":"collabAgentToolCall","tool":"spawnAgent",
		"senderThreadId":"thread-1","receiverThreadIds":["thread-2"],
		"prompt":"Read a.txt and b.txt","status":"inProgress",
		"agentsStates":{"thread-2":{"status":"running"}}}`))

	evs := drainSubagents(s)
	if len(evs) != 1 || len(evs[0].Subagents) != 1 {
		t.Fatalf("emitted %v, want one entry", evs)
	}
	got := evs[0].Subagents[0]
	if got.ID != "thread-2" || got.Status != event.SubagentStatusRunning ||
		got.Task != "Read a.txt and b.txt" {
		t.Fatalf("entry=%+v", got)
	}

	// A terminal agentsStates closes it.
	s.noteSubagentItem("collabAgentToolCall", json.RawMessage(`{
		"id":"item_2","type":"collabAgentToolCall","tool":"wait",
		"senderThreadId":"thread-1","receiverThreadIds":["thread-2"],
		"status":"completed",
		"agentsStates":{"thread-2":{"status":"completed"}}}`))
	evs = drainSubagents(s)
	if len(evs) != 1 || evs[0].Subagents[0].Status != event.SubagentStatusCompleted {
		t.Fatalf("emitted %v, want completed", evs)
	}
}

// The full CollabAgentStatus enum, from the schema codex generates itself.
func TestCollabAgentStatusMapping(t *testing.T) {
	for in, want := range map[string]string{
		"pendingInit": event.SubagentStatusRunning,
		"running":     event.SubagentStatusRunning,
		"completed":   event.SubagentStatusCompleted,
		"shutdown":    event.SubagentStatusCompleted,
		"interrupted": event.SubagentStatusFailed,
		"errored":     event.SubagentStatusFailed,
		"notFound":    event.SubagentStatusFailed,
		// An unknown state reads as still-working: claiming a sub-agent
		// finished when it has not is the worse error.
		"somethingNew": event.SubagentStatusRunning,
	} {
		if got := collabAgentStatus(in); got != want {
			t.Errorf("collabAgentStatus(%q)=%q, want %q", in, got, want)
		}
	}
}

// subAgentActivity carries agentThreadId/agentPath/kind — NOT agentName, goal
// or instructions, which the daemon used to parse. Every card therefore
// rendered as the literal "sub-agent" with an empty detail.
func TestSubAgentActivityUsesTheFieldsThatExist(t *testing.T) {
	s := subagentSession(t)
	s.noteSubagentItem("subAgentActivity", json.RawMessage(`{
		"id":"item_3","type":"subAgentActivity","kind":"started",
		"agentThreadId":"thread-9","agentPath":"/home/u/.codex/agents/explore.md"}`))

	evs := drainSubagents(s)
	if len(evs) != 1 || len(evs[0].Subagents) != 1 {
		t.Fatalf("emitted %v, want one entry", evs)
	}
	got := evs[0].Subagents[0]
	if got.ID != "thread-9" {
		t.Errorf("id=%q, want the agent thread id", got.ID)
	}
	if got.Name != "explore" {
		t.Errorf("name=%q, want the agentPath basename", got.Name)
	}
	if got.Status != event.SubagentStatusRunning {
		t.Errorf("status=%q, want running", got.Status)
	}
}

func TestAgentDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/.codex/agents/explore.md": "explore",
		"agents/general.md":                "general",
		"reviewer":                         "reviewer",
		"":                                 "",
		"/":                                "",
	} {
		if got := agentDisplayName(in); got != want {
			t.Errorf("agentDisplayName(%q)=%q, want %q", in, got, want)
		}
	}
}

// SubAgentActivityKind is started|interacted|interrupted — no terminal value
// exists, so turn end is the only thing that can close these entries.
func TestSubagentSetClearsAtTurnEnd(t *testing.T) {
	s := subagentSession(t)
	s.noteSubagentItem("subAgentActivity", json.RawMessage(`{
		"id":"i","type":"subAgentActivity","kind":"started",
		"agentThreadId":"t9","agentPath":"explore.md"}`))
	drainSubagents(s)

	s.clearSubagents()
	evs := drainSubagents(s)
	if len(evs) != 1 || len(evs[0].Subagents) != 0 {
		t.Fatalf("emitted %v, want one empty set", evs)
	}
}

// A session that never ran a sub-agent must not emit a clear at every turn end.
func TestClearWithoutSubagentsIsANoOp(t *testing.T) {
	s := subagentSession(t)
	s.clearSubagents()
	if evs := drainSubagents(s); len(evs) != 0 {
		t.Fatalf("emitted %v, want nothing", evs)
	}
}

// A late activity frame must not resurrect a finished sub-agent.
func TestFinishedSubagentDoesNotReopen(t *testing.T) {
	s := subagentSession(t)
	s.noteSubagentItem("collabAgentToolCall", json.RawMessage(`{
		"id":"i","type":"collabAgentToolCall","tool":"closeAgent",
		"senderThreadId":"thread-1","receiverThreadIds":["t9"],
		"status":"completed","agentsStates":{"t9":{"status":"completed"}}}`))
	drainSubagents(s)

	s.noteSubagentItem("subAgentActivity", json.RawMessage(`{
		"id":"i2","type":"subAgentActivity","kind":"interacted",
		"agentThreadId":"t9","agentPath":"explore.md"}`))
	if evs := drainSubagents(s); len(evs) != 0 {
		t.Fatalf("emitted %v; a finished sub-agent must not reopen", evs)
	}
}
