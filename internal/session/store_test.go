package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Save/Get round-trips after the atomic-write + parent-dir fsync change.
func TestStoreSaveGetRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := Record{ID: "sess-rt", Provider: "fake", Name: "chat", Model: "m1", CWD: "/tmp"}
	if err := store.Save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Get("sess-rt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != rec.ID || got.Name != rec.Name || got.Model != rec.Model || got.CWD != rec.CWD {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, rec)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not stamped")
	}
	// Overwrite (rename over an existing file) must still round-trip.
	rec.Name = "renamed"
	if err := store.Save(rec); err != nil {
		t.Fatalf("resave: %v", err)
	}
	if got, _ := store.Get("sess-rt"); got.Name != "renamed" {
		t.Fatalf("overwrite not durable: %q", got.Name)
	}
}

// SaveHistory/LoadHistory round-trips after the change.
func TestStoreSaveHistoryRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := []event.Event{
		{Type: event.TypeUserMessage, SessionID: "h1", Seq: 1, Text: "hi"},
		{Type: event.TypeNotice, SessionID: "h1", Seq: 2, Text: "note"},
	}
	if err := store.SaveHistory("h1", events); err != nil {
		t.Fatalf("save history: %v", err)
	}
	got := store.LoadHistory("h1")
	if len(got) != 2 || got[0].Text != "hi" || got[1].Text != "note" {
		t.Fatalf("history round-trip mismatch: %+v", got)
	}
	// Overwrite the existing history file.
	if err := store.SaveHistory("h1", events[:1]); err != nil {
		t.Fatalf("resave history: %v", err)
	}
	if got := store.LoadHistory("h1"); len(got) != 1 {
		t.Fatalf("history overwrite len=%d want 1", len(got))
	}
}

// atomicWrite writes, syncs, renames over the target, and fsyncs the parent dir;
// the payload must be readable back and overwriting an existing file must work.
func TestAtomicWrite(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, "atomic.bin")
	if err := store.atomicWrite(path, []byte("first")); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "first" {
		t.Fatalf("got %q want %q", b, "first")
	}
	if err := store.atomicWrite(path, []byte("second")); err != nil {
		t.Fatalf("atomicWrite overwrite: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "second" {
		t.Fatalf("overwrite got %q want %q", b, "second")
	}
	// The temp file must not linger after a successful rename.
	// Unique staging temps must not linger as path+".tmp".
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}
