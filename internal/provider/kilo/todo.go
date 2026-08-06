package kilo

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// openCodeTodo is the OpenCode engine Todo shape (SDK 1.18+).
type openCodeTodo struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// handleTodoUpdated maps todo.updated → event.TypePlan (MADR 0020 Sprint 2 / PR6).
// Replace-semantics: empty todos clears the phone plan strip.
func (o *httpSession) handleTodoUpdated(props json.RawMessage) {
	var p struct {
		SessionID string         `json:"sessionID"`
		Todos     []openCodeTodo `json:"todos"`
	}
	if json.Unmarshal(props, &p) != nil {
		return
	}
	// Demux routes child frames here too, for tree bookkeeping — but a
	// sub-agent's todo list is not this session's plan, and accepting it
	// replaced the parent's plan strip wholesale under the event's replace
	// semantics. The panel shows the conversation the user is having
	// (MADR 0051 D7).
	if o.fromChild() {
		return
	}
	_ = p.SessionID
	entries := mapOpenCodeTodos(p.Todos)
	o.h.Emit(event.Event{Type: event.TypePlan, Entries: entries})
}

// mapOpenCodeTodos converts OpenCode todos to plan entries (Q4 cancelled mapping).
// Preserves engine order; drops empty content; never marks cancelled as completed.
func mapOpenCodeTodos(todos []openCodeTodo) []event.PlanEntry {
	// Non-nil empty slice so mobile clear (PlanRemoved) is distinguishable.
	entries := make([]event.PlanEntry, 0, len(todos))
	for _, t := range todos {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(t.Status))
		switch st {
		case "cancelled":
			content = "(cancelled) " + content
			st = event.PlanStatusPending
		case "pending", "in_progress", "completed":
			// pass through
		default:
			st = event.PlanStatusPending
		}
		pr := strings.ToLower(strings.TrimSpace(t.Priority))
		switch pr {
		case event.PlanPriorityHigh, event.PlanPriorityMedium, event.PlanPriorityLow:
			// ok
		default:
			pr = event.PlanPriorityMedium
		}
		entries = append(entries, event.PlanEntry{
			Content:  content,
			Status:   st,
			Priority: pr,
		})
	}
	return entries
}

// resyncTodos fetches the parent session todo list and emits a plan snapshot.
// Runs even while the tree is busy so mid-turn SSE gaps still recover A3 lists.
func (o *httpSession) resyncTodos(ctx context.Context, parent string) {
	if parent == "" {
		return
	}
	var todos []openCodeTodo
	if err := o.h.API()(ctx, "GET", "/session/"+parent+"/todo"+o.dir(), nil, &todos); err != nil {
		o.h.Log().Debug("sse resync: todo fetch failed", slog.String("err", err.Error()))
		return
	}
	entries := mapOpenCodeTodos(todos)
	o.h.Emit(event.Event{Type: event.TypePlan, Entries: entries})
}
