package acpagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// drain returns everything the session emitted so far.
func drain(s *session) []event.Event {
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

// Sub-agent content must not reach the transcript (MADR 0051 D6).
//
// acpagent runs one process and one connection per session, and SessionUpdate
// never checked params.SessionId — so grok's sub-agent frames, which carry the
// child's id, were emitted straight into the parent's chat. Measured on grok
// 0.2.114: 63 child assistant chunks against the parent's 56, plus 46 child
// thought chunks and the sub-agent's own prompt echoed as a user message.
//
// Driven twice with the same frame — once as the live session, once as a child
// — so it asserts suppression rather than "emits nothing".
func TestChildSessionUpdatesAreDropped(t *testing.T) {
	s := modeSession(t) // agentID "agent-1"

	chunk := func(sid string) acp.SessionNotification {
		text := "hello"
		return acp.SessionNotification{
			SessionId: acp.SessionId(sid),
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: text}},
				},
			},
		}
	}

	if err := s.SessionUpdate(context.Background(), chunk("agent-1")); err != nil {
		t.Fatalf("SessionUpdate(parent): %v", err)
	}
	if got := drain(s); len(got) != 1 || got[0].Type != event.TypeAssistantChunk {
		t.Fatalf("parent-origin chunk emitted %v, want one assistant chunk", got)
	}

	if err := s.SessionUpdate(context.Background(), chunk("child-9")); err != nil {
		t.Fatalf("SessionUpdate(child): %v", err)
	}
	if got := drain(s); len(got) != 0 {
		t.Fatalf("child-origin chunk emitted %v; sub-agent text must not reach chat", got)
	}
}

// A frame arriving before session/new has returned (agentID still empty) is
// not a child frame and must not be dropped.
func TestUpdateBeforeSessionIDIsKept(t *testing.T) {
	s := modeSession(t)
	s.agentID = ""

	err := s.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: acp.SessionId("whatever"),
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "early"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if got := drain(s); len(got) != 1 {
		t.Fatalf("emitted %v, want the early chunk kept", got)
	}
}

// The three payloads below are captured verbatim from grok 0.2.114 driving a
// sub-agent that read two files (MADR 0051 §10.2).
func TestXAISubagentLifecycle(t *testing.T) {
	s := modeSession(t)

	spawned := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_spawned",
		"subagent_id":"019fae56-077f","parent_session_id":"agent-1",
		"child_session_id":"019fae56-077f","subagent_type":"explore",
		"description":"Read a.txt and b.txt words","role":"explore",
		"model":"grok-4.5","capability_mode":"read-only"}}`
	progress := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_progress","subagent_id":"019fae56-077f",
		"duration_ms":2498,"turn_count":1,"tokens_used":1457}}`
	finished := `{"sessionId":"agent-1","update":{
		"sessionUpdate":"subagent_finished","subagent_id":"019fae56-077f",
		"status":"completed","tool_calls":2,"turns":1,"duration_ms":4929,
		"output":"## Summary\n\nhello and world"}}`

	ctx := context.Background()
	HandleXAISessionNotification(ctx, s, json.RawMessage(spawned))
	evs := drain(s)
	if len(evs) != 1 || evs[0].Type != event.TypeSubagents {
		t.Fatalf("spawned emitted %v, want one subagents event", evs)
	}
	set := evs[0].Subagents
	if len(set) != 1 {
		t.Fatalf("set=%v, want one entry", set)
	}
	if set[0].ID != "019fae56-077f" || set[0].Name != "explore" ||
		set[0].Task != "Read a.txt and b.txt words" ||
		set[0].Status != event.SubagentStatusRunning {
		t.Fatalf("entry=%+v", set[0])
	}

	HandleXAISessionNotification(ctx, s, json.RawMessage(progress))
	if evs := drain(s); len(evs) != 1 {
		t.Fatalf("progress emitted %v, want one refresh", evs)
	}

	HandleXAISessionNotification(ctx, s, json.RawMessage(finished))
	evs = drain(s)
	if len(evs) != 1 {
		t.Fatalf("finished emitted %v", evs)
	}
	if got := evs[0].Subagents[0].Status; got != event.SubagentStatusCompleted {
		t.Fatalf("status=%q, want completed", got)
	}
	// The sub-agent's own report is exactly what stays out of the transcript.
	if evs[0].Subagents[0].Task == "" ||
		evs[0].Text != "" || evs[0].Subagents[0].Name == "" {
		t.Fatalf("unexpected payload: %+v", evs[0])
	}

	// A late progress frame must not reopen a finished agent.
	HandleXAISessionNotification(ctx, s, json.RawMessage(progress))
	if evs := drain(s); len(evs) != 0 {
		t.Fatalf("late progress reopened a finished sub-agent: %v", evs)
	}

	// Turn end clears the panel.
	s.clearSubagents()
	evs = drain(s)
	if len(evs) != 1 || len(evs[0].Subagents) != 0 {
		t.Fatalf("clear emitted %v, want one empty subagents event", evs)
	}
}

func TestXAISubagentFailedStatus(t *testing.T) {
	s := modeSession(t)
	HandleXAISessionNotification(context.Background(), s, json.RawMessage(
		`{"sessionId":"agent-1","update":{"sessionUpdate":"subagent_finished",
		  "subagent_id":"x","subagent_type":"explore","status":"interrupted"}}`))
	evs := drain(s)
	if len(evs) != 1 || evs[0].Subagents[0].Status != event.SubagentStatusFailed {
		t.Fatalf("emitted %v, want failed", evs)
	}
}

// Other variants share this channel — tool_call_delta_chunk and turn_completed
// arrive here too, including for the child. None of them is status.
func TestXAINonSubagentVariantsIgnored(t *testing.T) {
	s := modeSession(t)
	ctx := context.Background()
	for _, frame := range []string{
		`{"sessionId":"agent-1","update":{"sessionUpdate":"tool_call_delta_chunk","tool_call_id":"c","name":"read_file"}}`,
		`{"sessionId":"agent-1","update":{"sessionUpdate":"turn_completed","stop_reason":"end_turn"}}`,
		`{"sessionId":"agent-1","update":{"sessionUpdate":"model_changed","model_id":"grok-4.5"}}`,
		// A sub-agent frame addressed to the child, not to us.
		`{"sessionId":"child-9","update":{"sessionUpdate":"subagent_spawned","subagent_id":"z","subagent_type":"explore"}}`,
	} {
		HandleXAISessionNotification(ctx, s, json.RawMessage(frame))
	}
	if evs := drain(s); len(evs) != 0 {
		t.Fatalf("emitted %v for frames that carry no sub-agent status", evs)
	}
}

// A session that never ran a sub-agent must not emit a clear at turn end: that
// would put a control event on the wire to undo something that never happened.
func TestClearIsANoOpWithoutSubagents(t *testing.T) {
	s := modeSession(t)
	s.clearSubagents()
	if evs := drain(s); len(evs) != 0 {
		t.Fatalf("emitted %v, want nothing", evs)
	}
}

// grokWireRPC401 is the exact `data` payload captured live from grok 1.0.0's
// session/prompt JSON-RPC error (2026-08-09 probe, MADR 0073 follow-up),
// rendered the way acp-go-sdk's RequestError.Error() presents it to
// emitClassifiedTurnError.
const grokWireRPC401 = `{"code":-32603,"message":"Internal error","data":"Unauthorized (401) from https://cli-chat-proxy.grok.com/v1/responses: Invalid or expired credentials (auth_kind=none, x_xai_token_auth=xai-grok-cli, upstream=Unauthenticated, reason=no auth context)\n\n  Model:     grok-4.5\n  Auth:      ApiKey\n  Version:   1.0.0\n  Available: grok-4.5"}`

// A grok 401 must surface as a classified auth error with the remedy — not
// the literal "Internal error" the JSON-RPC wrapper carries (the original
// symptom: the real cause lives in `data`, which extraction used to drop).
func TestEmitClassifiedTurnErrorGrok401(t *testing.T) {
	s := modeSession(t)
	s.emitClassifiedTurnError(grokWireRPC401)

	evs := drain(s)
	var errEv *event.Event
	for i := range evs {
		if evs[i].Type == event.TypeError {
			errEv = &evs[i]
		}
	}
	if errEv == nil {
		t.Fatalf("no error event emitted; got %v", evs)
	}
	if errEv.ErrorKind != "auth" {
		t.Fatalf("error_kind=%q msg=%q, want auth", errEv.ErrorKind, errEv.Error)
	}
	if errEv.Error == "Internal error" {
		t.Fatal("the opaque JSON-RPC wrapper reached the chat — data was not extracted")
	}
	if !strings.Contains(errEv.Error, "Sign in") {
		t.Fatalf("message should carry the remedy, got %q", errEv.Error)
	}
}

// retry_state on _x.ai/session_notification: a non-terminal retry with limit
// text mid-turn goes through the same abort machinery as stderr scraping —
// the structured analog of goose's "Backing off for 3600s" lines.
func TestRetryStateNotificationAbortsTurn(t *testing.T) {
	s := modeSession(t)
	s.pending = map[string]*permWaiter{}
	s.mu.Lock()
	s.prompting = true
	s.mu.Unlock()

	frame := `{"sessionId":"agent-1","update":{"sessionUpdate":"retry_state","type":"retrying","error_type":"rate_limit","message":"request failed (attempt 1/3), retrying in 20s: 429 Too Many Requests"}}`
	HandleXAISessionNotification(context.Background(), s, json.RawMessage(frame))

	evs := drain(s)
	var errEv *event.Event
	for i := range evs {
		if evs[i].Type == event.TypeError {
			errEv = &evs[i]
		}
	}
	if errEv == nil {
		t.Fatalf("no error event; got %v", evs)
	}
	if errEv.ErrorKind != "rate_limit" {
		t.Fatalf("error_kind=%q, want rate_limit", errEv.ErrorKind)
	}
	if errEv.RetryAt.IsZero() {
		t.Fatal("retry_at should be parsed from 'retrying in 20s'")
	}
}

// A terminal retry_state ("failed") is deliberately NOT handled here: the
// session/prompt JSON-RPC error carrying the same cause arrives immediately
// after and owns the turn's error emission — handling both would double-post
// the failure into the chat.
func TestRetryStateFailedIsLeftToRPCError(t *testing.T) {
	s := modeSession(t)
	s.pending = map[string]*permWaiter{}
	s.mu.Lock()
	s.prompting = true
	s.mu.Unlock()

	frame := `{"sessionId":"agent-1","update":{"sessionUpdate":"retry_state","type":"failed","error_type":"auth","message":"Unauthorized (401) from upstream"}}`
	HandleXAISessionNotification(context.Background(), s, json.RawMessage(frame))

	if evs := drain(s); len(evs) != 0 {
		t.Fatalf("terminal retry_state must emit nothing (RPC error owns it); got %v", evs)
	}
}
