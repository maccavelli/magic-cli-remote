package ws

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// serverWithLog returns a Server whose log lands in buf, at debug so the
// per-page line is visible.
func serverWithLog() (*Server, *bytes.Buffer) {
	var buf bytes.Buffer
	s := &Server{log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	return s, &buf
}

// TestHistoryPageLogsItsDirectionAndCursors is acceptance 2's first half.
//
// Before this, the daemon log was identical whether a request returned nothing,
// one page, or 6,400 events across 32 round trips (MADR 0141 F4).
func TestHistoryPageLogsItsDirectionAndCursors(t *testing.T) {
	s, buf := serverWithLog()

	s.logHistoryPage(
		protocol.SessionHistoryPayload{SessionID: "s1", BeforeSeq: 500},
		"dev-a",
		protocol.SessionHistoryResultPayload{
			Events:        []event.Event{{Seq: 498}, {Seq: 499}},
			Truncated:     true,
			PrevBeforeSeq: 498,
		},
		1, 19800,
	)

	out := buf.String()
	for _, want := range []string{
		"session history page", "session_id=s1", "device_id=dev-a",
		"direction=backward", "cursor_in=500", "cursor_out=498",
		"events=2", "truncated=true", "latest_seq=19800",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "WARN") {
		t.Errorf("a normal page warned:\n%s", out)
	}
}

// TestForwardPageIsLabelledForward keeps the two directions distinguishable —
// the whole point of the field is that they page the same ring differently.
func TestForwardPageIsLabelledForward(t *testing.T) {
	s, buf := serverWithLog()
	s.logHistoryPage(
		protocol.SessionHistoryPayload{SessionID: "s1", SinceSeq: 200},
		"dev-a",
		protocol.SessionHistoryResultPayload{
			Events: []event.Event{{Seq: 201}}, NextSinceSeq: 201,
		},
		1, 19800,
	)
	out := buf.String()
	if !strings.Contains(out, "direction=forward") ||
		!strings.Contains(out, "cursor_in=200") || !strings.Contains(out, "cursor_out=201") {
		t.Errorf("forward page mislabelled:\n%s", out)
	}
}

// TestSilentEmptyPageWarns is acceptance 2's second half, and the state that
// cost MADR 0141 an hour.
//
// Zero events is not an error at any layer, so writeError never fires and the
// phone simply shows an empty chat. Nothing else reports it.
func TestSilentEmptyPageWarns(t *testing.T) {
	s, buf := serverWithLog()

	s.logHistoryPage(
		protocol.SessionHistoryPayload{SessionID: "s1", Newest: true},
		"dev-a",
		protocol.SessionHistoryResultPayload{Events: nil},
		1, 19800, // the session demonstrably has 19,800 events
	)

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("serving nothing for a session with 19,800 events did not warn:\n%s", out)
	}
	if !strings.Contains(out, "s1") {
		t.Errorf("the warning does not name the session:\n%s", out)
	}
	if !strings.Contains(out, "empty chat") {
		t.Errorf("the warning does not say what the user sees:\n%s", out)
	}
}

// TestEmptyPageAtTheOldestEdgeDoesNotWarn.
//
// A cursor-bounded page legitimately returns zero when the walk reaches the
// oldest event. Warning there would fire on every completed backward walk and
// make the real signal worthless.
func TestEmptyPageAtTheOldestEdgeDoesNotWarn(t *testing.T) {
	s, buf := serverWithLog()
	s.logHistoryPage(
		protocol.SessionHistoryPayload{SessionID: "s1", BeforeSeq: 1},
		"dev-a",
		protocol.SessionHistoryResultPayload{Events: nil},
		1, 19800,
	)
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("the end of a backward walk warned:\n%s", buf.String())
	}
}

// TestEmptyPageForAnEmptySessionDoesNotWarn: a session with no events at all
// is not a fault.
func TestEmptyPageForAnEmptySessionDoesNotWarn(t *testing.T) {
	s, buf := serverWithLog()
	s.logHistoryPage(
		protocol.SessionHistoryPayload{SessionID: "s1", Newest: true},
		"dev-a",
		protocol.SessionHistoryResultPayload{Events: nil},
		0, 0,
	)
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("an empty session warned:\n%s", buf.String())
	}
}
