package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0187 T2: where a session's name came from, and the rule that a
// generated title never replaces a name the user set (MADR 0187 D2–D4).

func newNameManager(t *testing.T, store *Store) (*Manager, *metadataProvider) {
	t.Helper()
	p := &metadataProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	return NewManager(reg, store, nil, nil), p
}

// sendTitle delivers a session_title to the live session and returns once the
// manager has processed it: the events channel is unbuffered, so the pump has
// finished with the title when it takes the next event.
func sendTitle(t *testing.T, p *metadataProvider, title string, generated bool) {
	t.Helper()
	s := p.session
	s.events <- event.Event{Type: event.TypeSessionTitle, SessionID: s.id, Title: title, TitleGenerated: generated}
	s.events <- event.Event{Type: event.TypeThoughtChunk, SessionID: s.id, Text: "sync"}
}

func nameAndSource(t *testing.T, mgr *Manager, id string) (string, string) {
	t.Helper()
	got, err := mgr.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Name, got.NameSource
}

func titlesInHistory(mgr *Manager, id string) []string {
	var out []string
	for _, ev := range mgr.History(id) {
		if ev.Type == event.TypeSessionTitle {
			out = append(out, ev.Title)
		}
	}
	return out
}

func TestGeneratedTitleNamesAnUnnamedSession(t *testing.T) {
	mgr, p := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if meta.NameSource != "" {
		t.Fatalf("unnamed session source = %q, want empty", meta.NameSource)
	}
	sendTitle(t, p, "First title", true)
	if name, src := nameAndSource(t, mgr, meta.ID); name != "First title" || src != NameSourceGenerated {
		t.Fatalf("after a generated title: %q/%q, want First title/generated", name, src)
	}
	sendTitle(t, p, "Refined title", true) // grok 1.0.13 refines its title
	if name, src := nameAndSource(t, mgr, meta.ID); name != "Refined title" || src != NameSourceGenerated {
		t.Fatalf("after a refined title: %q/%q, want Refined title/generated", name, src)
	}
	if got := titlesInHistory(mgr, meta.ID); len(got) != 2 {
		t.Fatalf("history titles = %v, want both", got)
	}
}

func TestGeneratedTitleNeverReplacesAUserName(t *testing.T) {
	mgr, p := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "mine"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if meta.NameSource != NameSourceUser {
		t.Fatalf("named at create: source %q, want user", meta.NameSource)
	}
	sendTitle(t, p, "Agent title", true)
	if name, src := nameAndSource(t, mgr, meta.ID); name != "mine" || src != NameSourceUser {
		t.Fatalf("a generated title replaced the user's name: %q/%q", name, src)
	}
	if got := titlesInHistory(mgr, meta.ID); len(got) != 0 {
		t.Fatalf("a refused generated title still reached history (and so the phone): %v", got)
	}

	// A Rename makes a generated name the user's.
	other, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	sendTitle(t, p, "Agent title", true)
	if _, err := mgr.Rename(context.Background(), other.ID, "picked", "owner"); err != nil {
		t.Fatal(err)
	}
	sendTitle(t, p, "Agent title 2", true)
	if name, src := nameAndSource(t, mgr, other.ID); name != "picked" || src != NameSourceUser {
		t.Fatalf("after Rename then a generated title: %q/%q, want picked/user", name, src)
	}
}

// TestUnflaggedTitleIsAppliedOverAUserName: a title without TitleGenerated
// follows a user's own rename (codex thread/name/updated) and is applied as
// before MADR 0187.
func TestUnflaggedTitleIsAppliedOverAUserName(t *testing.T) {
	mgr, p := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "mine"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	sendTitle(t, p, "renamed in codex", false)
	if name, src := nameAndSource(t, mgr, meta.ID); name != "renamed in codex" || src != NameSourceUser {
		t.Fatalf("unflagged title: %q/%q, want renamed in codex/user", name, src)
	}
}

// TestResumeKeepsTheStoredNameSource: the phone resumes by re-sending the
// session's id and current name (resumeSession), so a re-create with the
// stored name keeps the stored source — a generated name stays open to the
// agent's titles, and a record from before MADR 0187 keeps its name (D4).
func TestResumeKeepsTheStoredNameSource(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr, p := newNameManager(t, store)
	for _, rec := range []Record{
		{ID: "generated-1", Provider: "metadata", Name: "Agent title", NameSource: NameSourceGenerated},
		{ID: "legacy-1", Provider: "metadata", Name: "legacy name"},
	} {
		if err := store.Save(rec); err != nil {
			t.Fatal(err)
		}
	}

	g, err := mgr.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: "generated-1", Name: "Agent title"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if g.NameSource != NameSourceGenerated {
		t.Fatalf("resumed generated name: source %q, want generated", g.NameSource)
	}
	sendTitle(t, p, "Newer agent title", true)
	if name, _ := nameAndSource(t, mgr, g.ID); name != "Newer agent title" {
		t.Fatalf("resumed generated name was not refreshed: %q", name)
	}

	l, err := mgr.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: "legacy-1", Name: "legacy name"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if l.NameSource != NameSourceUser {
		t.Fatalf("resumed pre-0187 record: source %q, want user", l.NameSource)
	}
	sendTitle(t, p, "Agent title", true)
	if name, _ := nameAndSource(t, mgr, l.ID); name != "legacy name" {
		t.Fatalf("a pre-0187 name was overwritten: %q", name)
	}

	// Relaunch (/reset, model recovery) re-creates a live session the same way.
	r, err := mgr.Create(context.Background(), "metadata",
		provider.StartOptions{LocalSessionID: "generated-1", Name: "Newer agent title"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if r.NameSource != NameSourceGenerated {
		t.Fatalf("relaunched generated name: source %q, want generated", r.NameSource)
	}
}

// TestManagerWritesTheNameSource: the source the manager decides is what lands
// in meta.json, so the next resume reads it back.
func TestManagerWritesTheNameSource(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mgr, _ := newNameManager(t, store)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{Name: "mine"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get(meta.ID)
	if err != nil || rec.NameSource != NameSourceUser {
		t.Fatalf("stored record: %+v %v; want name_source user", rec, err)
	}
}

func TestNameSourceIsPersisted(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{ID: "s1", Provider: "fake", Name: "n", NameSource: NameSourceGenerated}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("s1")
	if err != nil || got.NameSource != NameSourceGenerated {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	// A meta.json written before MADR 0187 has no name_source.
	raw, err := json.Marshal(map[string]any{"id": "old", "provider": "fake", "name": "kept"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sessions", "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sessions", "old", "meta.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := store.Get("old")
	if err != nil {
		t.Fatal(err)
	}
	if old.NameSource != "" || effectiveNameSource(old.Name, old.NameSource) != NameSourceUser {
		t.Fatalf("old record: source %q, effective %q; want empty and user",
			old.NameSource, effectiveNameSource(old.Name, old.NameSource))
	}
}
