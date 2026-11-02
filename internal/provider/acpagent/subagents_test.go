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
// never checked params.SessionId — so an agent's sub-agent frames, which carry
// the child's id, were emitted straight into the parent's chat. Measured: 63
// child assistant chunks against the parent's 56, plus 46 child
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

// wireRPC401 is a session/prompt JSON-RPC error whose cause is in `data`, in the
// shape an agent's 401 takes (MADR 0073 follow-up), rendered the way
// acp-go-sdk's RequestError.Error() presents it to emitClassifiedTurnError. Each
// agent's package pins its own captured payload.
const wireRPC401 = `{"code":-32603,"message":"Internal error","data":"Unauthorized (401) from https://api.vendor.invalid/v1/responses: Invalid or expired credentials (reason=no auth context)\n\n  Model:     model-b\n  Auth:      ApiKey"}`

// A 401 must surface as a classified auth error with the remedy — not the
// literal "Internal error" the JSON-RPC wrapper carries (the original symptom:
// the real cause lives in `data`, which extraction used to drop).
func TestEmitClassifiedTurnErrorAuth401(t *testing.T) {
	s := modeSession(t)
	s.emitClassifiedTurnError(wireRPC401)

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
