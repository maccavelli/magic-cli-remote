package session

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// storeWithLog returns a Store whose warnings land in buf.
//
// The seeded file goes where the store actually reads —
// <dataDir>/sessions/<id>/history.json. Writing one level higher produces an
// empty load that looks identical to a parse failure, which is how MADR 0141's
// own first measurement went wrong.
func storeWithLog(t *testing.T, id string, body []byte) (*Store, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	store.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if body != nil {
		sdir := filepath.Join(dir, "sessions", id)
		if err := os.MkdirAll(sdir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sdir, "history.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(store.historyPath(id)); err != nil {
			t.Fatalf("seeded file is not where the store looks: %v", err)
		}
	}
	return store, &buf
}

// TestColdSessionDoesNotWarn is acceptance 1's first half.
//
// A session with no transcript yet is the normal case. Warning on it would
// train a reader to skip the line, which would cost more than it saves.
func TestColdSessionDoesNotWarn(t *testing.T) {
	store, buf := storeWithLog(t, "cold-1", nil)

	if got := store.LoadHistory("cold-1"); len(got) != 0 {
		t.Fatalf("a cold session returned %d events", len(got))
	}
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("a session with no history file warned:\n%s", buf.String())
	}
}

// TestUnreadableHistoryWarns covers a file that exists and cannot be read.
func TestUnreadableHistoryWarns(t *testing.T) {
	store, buf := storeWithLog(t, "unreadable-1", []byte(`{"events":[]}`))
	if err := os.Chmod(store.historyPath("unreadable-1"), 0o000); err != nil {
		t.Skipf("cannot make a file unreadable here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.historyPath("unreadable-1"), 0o600) })

	if os.Geteuid() == 0 {
		t.Skip("running as root; mode bits do not deny a read")
	}
	_ = store.LoadHistory("unreadable-1")
	out := buf.String()
	if !strings.Contains(out, "unreadable") || !strings.Contains(out, "unreadable-1") {
		t.Errorf("an unreadable history did not warn with its session id:\n%s", out)
	}
}

// TestUnparseableHistoryWarnsAndNamesTheContainer is the case that cost MADR
// 0141 an hour: a bare JSON array is valid JSON, is accepted by every tool, and
// loads as nothing.
func TestUnparseableHistoryWarnsAndNamesTheContainer(t *testing.T) {
	bare, _ := json.Marshal([]event.Event{
		{Type: event.TypeNotice, Seq: 1, Text: "one"},
		{Type: event.TypeNotice, Seq: 2, Text: "two"},
	})
	store, buf := storeWithLog(t, "bare-1", bare)

	got := store.LoadHistory("bare-1")
	if len(got) != 0 {
		t.Fatalf("a bare array loaded %d events; this test's premise is gone", len(got))
	}
	out := buf.String()
	if !strings.Contains(out, "did not parse") {
		t.Errorf("a bare array did not warn:\n%s", out)
	}
	if !strings.Contains(out, "bare-1") {
		t.Errorf("the warning does not name the session:\n%s", out)
	}
	// The hint is the whole value of the line: "did not parse" alone sends the
	// reader looking for corruption, when the file is well-formed JSON.
	if !strings.Contains(out, "events") {
		t.Errorf("the warning does not say what the container must be:\n%s", out)
	}
}

// TestHistoryWithNoEventsKeyWarns covers a well-formed object that carries no
// events, which decodes cleanly and yields nothing.
func TestHistoryWithNoEventsKeyWarns(t *testing.T) {
	store, buf := storeWithLog(t, "nokey-1", []byte(`{"session":"nokey-1"}`))

	if got := store.LoadHistory("nokey-1"); len(got) != 0 {
		t.Fatalf("returned %d events", len(got))
	}
	if !strings.Contains(buf.String(), "no events key") {
		t.Errorf("an events-less object did not warn:\n%s", buf.String())
	}
}

// TestAGoodHistoryLoadsQuietly: the happy path must not add log volume to
// every chat open.
func TestAGoodHistoryLoadsQuietly(t *testing.T) {
	body, _ := json.Marshal(historyFile{Events: []event.Event{
		{Type: event.TypeNotice, Seq: 1, Text: "one"},
		{Type: event.TypeNotice, Seq: 2, Text: "two"},
	}})
	store, buf := storeWithLog(t, "good-1", body)

	got := store.LoadHistory("good-1")
	if len(got) != 2 {
		t.Fatalf("loaded %d of 2 events", len(got))
	}
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("a good history warned:\n%s", buf.String())
	}
}
