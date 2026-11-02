package grok

// acpagent behaviour pinned on grok's own recorded data. acpagent's copies of
// these tests use neutral data (0208 PLAN, P6c Deviation 1).

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
)

// TestSessionInfoUpdateTitleIsAGeneratedSessionTitle is PLAN 0187 T1: grok's
// session_info_update titles, replayed from the committed 1.0.13 capture,
// become session_title events marked generated, in order (MADR 0187 D1).
func TestSessionInfoUpdateTitleIsAGeneratedSessionTitle(t *testing.T) {
	f, err := os.Open("testdata/wire/1.0.13/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, a := startSession(t, Config{}, acptest.Options{})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	replayed := 0
	for sc.Scan() {
		var frame struct {
			Method string `json:"method"`
			Params struct {
				Update json.RawMessage `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &frame) != nil || frame.Method != "session/update" {
			continue
		}
		var u struct {
			SessionUpdate string `json:"sessionUpdate"`
		}
		if json.Unmarshal(frame.Params.Update, &u) != nil || u.SessionUpdate != "session_info_update" {
			continue
		}
		// The capture's agent session id is not this session's.
		a.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":` +
			string(frame.Params.Update) + `}}`)
		replayed++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if replayed != 2 {
		t.Fatalf("replayed %d session_info_update frames, want the capture's 2", replayed)
	}
	// A barrier: once this mode update has reached Magic, the titles have.
	a.Send(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1","update":{"sessionUpdate":"current_mode_update","currentModeId":"plan"}}}`)

	var titles []event.Event
	for _, ev := range eventsUntil(t, s, event.TypeMode) {
		if ev.Type == event.TypeSessionTitle {
			titles = append(titles, ev)
		}
	}
	want := []string{
		"Initial User Greeting and Conversation Start",
		"User Greeting to Initiate Conversation Session",
	}
	if len(titles) != len(want) {
		t.Fatalf("session_title events = %d, want %d", len(titles), len(want))
	}
	for i, ev := range titles {
		if ev.Title != want[i] || !ev.TitleGenerated {
			t.Errorf("title %d = %q generated=%v, want %q generated", i, ev.Title, ev.TitleGenerated, want[i])
		}
	}
}

// wireRPC401Data is the exact `data` of the session/prompt JSON-RPC error
// captured live from grok 1.0.0 (2026-08-09 probe, MADR 0073 follow-up).
const wireRPC401Data = "Unauthorized (401) from https://cli-chat-proxy.grok.com/v1/responses: Invalid or expired credentials (auth_kind=none, x_xai_token_auth=xai-grok-cli, upstream=Unauthenticated, reason=no auth context)\n\n  Model:     grok-4.5\n  Auth:      ApiKey\n  Version:   1.0.0\n  Available: grok-4.5"

// TestEmitClassifiedTurnErrorGrok401: a grok 401 must surface as a classified
// auth error with the remedy — not the literal "Internal error" the JSON-RPC
// wrapper carries (the original symptom: the real cause lives in `data`,
// which extraction used to drop).
func TestEmitClassifiedTurnErrorGrok401(t *testing.T) {
	s, a := startSession(t, Config{}, acptest.Options{})
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	a.RespondErrorData(a.Await("session/prompt"), -32603, "Internal error", wireRPC401Data)

	evs := eventsUntil(t, s, event.TypeError)
	errEv := evs[len(evs)-1]
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
