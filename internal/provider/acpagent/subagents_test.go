package acpagent

import (
	"context"
	"strings"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
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
