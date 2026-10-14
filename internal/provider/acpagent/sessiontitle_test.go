package acpagent

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestSessionInfoUpdateTitleIsAGeneratedSessionTitle is PLAN 0187 T1: grok's
// session_info_update titles, replayed from the committed 1.0.13 capture,
// become session_title events marked generated, in order (MADR 0187 D1).
func TestSessionInfoUpdateTitleIsAGeneratedSessionTitle(t *testing.T) {
	f, err := os.Open("../grok/testdata/wire/1.0.13/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, _ := newUpdateSession()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	replayed := 0
	for sc.Scan() {
		var frame struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &frame) != nil || frame.Method != "session/update" {
			continue
		}
		var n acp.SessionNotification
		if err := json.Unmarshal(frame.Params, &n); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if n.Update.SessionInfoUpdate == nil {
			continue
		}
		n.SessionId = "" // the capture's agent session id is not this session's
		if err := s.SessionUpdate(t.Context(), n); err != nil {
			t.Fatalf("SessionUpdate: %v", err)
		}
		replayed++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if replayed != 2 {
		t.Fatalf("replayed %d session_info_update frames, want the capture's 2", replayed)
	}

	var titles []event.Event
	for len(s.events) > 0 {
		if ev := <-s.events; ev.Type == event.TypeSessionTitle {
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

// TestSessionInfoUpdateWithoutATitleEmitsNothing: an update that carries no
// title, or only whitespace, names no session.
func TestSessionInfoUpdateWithoutATitleEmitsNothing(t *testing.T) {
	for _, update := range []string{
		`{"sessionUpdate":"session_info_update","updatedAt":"2026-10-13T00:00:00Z"}`,
		`{"sessionUpdate":"session_info_update","title":"   "}`,
	} {
		s, _ := newUpdateSession()
		deliver(t, s, update)
		for len(s.events) > 0 {
			if ev := <-s.events; ev.Type == event.TypeSessionTitle {
				t.Errorf("%s: emitted session_title %q", update, ev.Title)
			}
		}
	}
}
