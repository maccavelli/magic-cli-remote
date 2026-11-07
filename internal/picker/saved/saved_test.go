package saved

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// lockedBuffer is a log sink the writer goroutine and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// countingStore is a store on a temp directory whose writes are counted, and
// whose log is kept.
func countingStore(t *testing.T, dir string) (*Store, *int, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	s := Open(dir, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	writes := new(int)
	real := s.write
	var mu sync.Mutex
	s.write = func(path string, data []byte) error {
		mu.Lock()
		*writes++
		mu.Unlock()
		return real(path, data)
	}
	return s, writes, logs
}

func live(ids ...string) picker.Catalog {
	opts := make([]picker.Option, 0, len(ids))
	for _, id := range ids {
		opts = append(opts, picker.Option{ID: id, Group: "vendor"})
	}
	def := ""
	if len(ids) > 0 {
		def = ids[0]
	}
	return picker.SingleCatalog(picker.SourceLive, opts, def, true)
}

func optionIDs(c picker.Catalog) []string {
	out := make([]string, 0, len(c.Options))
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := countingStore(t, dir)
	s.Save("vendor", "1.2.3", live("vendor-b", "vendor-a"))
	s.Flush()

	got, ok := Open(dir, nil).Load("vendor")
	if !ok {
		t.Fatal("no saved catalog after Save and Flush")
	}
	if got.Source != picker.SourceSaved {
		t.Errorf("source = %q, want %q", got.Source, picker.SourceSaved)
	}
	if ids := optionIDs(got); !slices.Equal(ids, []string{"vendor-b", "vendor-a"}) {
		t.Errorf("options = %v, want [vendor-b vendor-a]", ids)
	}
	if !slices.Equal(got.DefaultIDs, []string{"vendor-b"}) || !got.AllowCustom {
		t.Errorf("defaults = %v, allow custom = %v; want [vendor-b], true", got.DefaultIDs, got.AllowCustom)
	}
}

func TestLoadIsInMemoryBeforeTheWrite(t *testing.T) {
	s, _, _ := countingStore(t, t.TempDir())
	release := make(chan struct{})
	real := s.write
	s.write = func(path string, data []byte) error {
		<-release
		return real(path, data)
	}
	s.Save("vendor", "", live("vendor-a"))
	got, ok := s.Load("vendor")
	close(release)
	s.Flush()
	if !ok || !slices.Equal(optionIDs(got), []string{"vendor-a"}) {
		t.Fatalf("Load before the write = %v, %v; want [vendor-a], true", optionIDs(got), ok)
	}
}

func TestOnlyANonEmptyLiveCatalogIsSaved(t *testing.T) {
	dir := t.TempDir()
	s, writes, _ := countingStore(t, dir)
	for _, c := range []picker.Catalog{
		picker.SingleCatalog(picker.SourceStatic, []picker.Option{{ID: "a"}}, "", true),
		picker.SingleCatalog(picker.SourceMerged, []picker.Option{{ID: "a"}}, "", true),
		picker.SingleCatalog(picker.SourceSaved, []picker.Option{{ID: "a"}}, "", true),
		picker.SingleCatalog(picker.SourceLive, nil, "", true),
	} {
		s.Save("vendor", "", c)
	}
	s.Flush()
	if *writes != 0 {
		t.Errorf("writes = %d, want 0", *writes)
	}
	if _, ok := s.Load("vendor"); ok {
		t.Error("a catalog that is not live and non-empty loads")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("directory holds %d entries, want none", len(entries))
	}
}

func TestAnEqualCatalogIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	s, writes, _ := countingStore(t, dir)
	s.Save("vendor", "", live("vendor-a"))
	s.Flush()
	s.Save("vendor", "", live("vendor-a"))
	s.Flush()
	if *writes != 1 {
		t.Fatalf("writes after two equal saves = %d, want 1", *writes)
	}
	s.Save("vendor", "", live("vendor-a", "vendor-b"))
	s.Flush()
	if *writes != 2 {
		t.Fatalf("writes after a changed save = %d, want 2", *writes)
	}

	// A restarted daemon that sees the same catalog again leaves the file.
	again, againWrites, _ := countingStore(t, dir)
	again.Save("vendor", "", live("vendor-a", "vendor-b"))
	again.Flush()
	if *againWrites != 0 {
		t.Errorf("a new store rewrote an equal file: writes = %d, want 0", *againWrites)
	}
}

func TestTheLatestQueuedCatalogWins(t *testing.T) {
	dir := t.TempDir()
	s, writes, _ := countingStore(t, dir)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	counted := s.write
	s.write = func(path string, data []byte) error {
		started <- struct{}{}
		<-release
		return counted(path, data)
	}
	s.Save("vendor", "", live("a"))
	<-started // a is being written
	s.Save("vendor", "", live("b"))
	s.Save("vendor", "", live("c"))
	close(release)
	s.Flush()

	if *writes > 2 {
		t.Errorf("writes = %d, want at most 2: a queued catalog that a newer one replaced was written", *writes)
	}
	got, ok := Open(dir, nil).Load("vendor")
	if !ok || !slices.Equal(optionIDs(got), []string{"c"}) {
		t.Fatalf("file holds %v, %v; want [c]", optionIDs(got), ok)
	}
}

func TestABadFileIsNoFile(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":   "{not json",
		"truncated":      `{"version":1,"provider":"vendor","catalog":{"kind":"single","options":[{"id":"a"`,
		"version 2":      `{"version":2,"provider":"vendor","catalog":{"kind":"single","source":"live","options":[{"id":"a"}]}}`,
		"other provider": `{"version":1,"provider":"other","catalog":{"kind":"single","source":"live","options":[{"id":"a"}]}}`,
		"no options":     `{"version":1,"provider":"vendor","catalog":{"kind":"single","source":"live","options":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "vendor.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s, _, logs := countingStore(t, dir)
			if c, ok := s.Load("vendor"); ok {
				t.Fatalf("a bad file loaded: %v", optionIDs(c))
			}
			if !strings.Contains(logs.String(), "level=DEBUG") {
				t.Errorf("nothing logged at debug for a bad file:\n%s", logs.String())
			}
			s.Save("vendor", "", live("fresh"))
			s.Flush()
			got, ok := Open(dir, nil).Load("vendor")
			if !ok || !slices.Equal(optionIDs(got), []string{"fresh"}) {
				t.Fatalf("after a live save the file holds %v, %v; want [fresh]", optionIDs(got), ok)
			}
		})
	}
}

func TestTheCatalogIsCutAtMaxOptions(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := countingStore(t, dir)
	ids := make([]string, MaxOptions+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%04d", i)
	}
	s.Save("vendor", "", live(ids...))
	s.Flush()
	got, ok := Open(dir, nil).Load("vendor")
	if !ok {
		t.Fatal("no saved catalog")
	}
	if len(got.Options) != MaxOptions || !got.Truncated {
		t.Fatalf("options = %d, truncated = %v; want %d, true", len(got.Options), got.Truncated, MaxOptions)
	}
	if got.Options[MaxOptions-1].ID != ids[MaxOptions-1] {
		t.Errorf("last kept option = %q, want %q", got.Options[MaxOptions-1].ID, ids[MaxOptions-1])
	}
}

func TestAProviderIDCannotNameAPath(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "catalogs")
	s, writes, logs := countingStore(t, dir)
	for _, id := range []string{"../x", "a/b", "", "Vendor", `a\b`} {
		s.Save(id, "", live("a"))
		if _, ok := s.Load(id); ok {
			t.Errorf("Load(%q) reports a catalog", id)
		}
	}
	s.Flush()
	if *writes != 0 {
		t.Errorf("writes = %d, want 0", *writes)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the parent holds %d entries, want none", len(entries))
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("a refused id was not logged at warn:\n%s", logs.String())
	}
}

func TestAFailedWriteIsLoggedAndKeptInMemory(t *testing.T) {
	dir := t.TempDir()
	s, _, logs := countingStore(t, dir)
	s.write = func(string, []byte) error { return errors.New("disk full") }
	s.Save("vendor", "", live("vendor-a"))
	s.Flush()
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "disk full") {
		t.Errorf("the failed write was not logged at warn:\n%s", logs.String())
	}
	if got, ok := s.Load("vendor"); !ok || !slices.Equal(optionIDs(got), []string{"vendor-a"}) {
		t.Errorf("Load after a failed write = %v, %v; want [vendor-a], true", optionIDs(got), ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "vendor.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file exists after a failed write: %v", err)
	}
}

func TestNilStoreIsInert(t *testing.T) {
	var s *Store
	s.Save("vendor", "", live("a"))
	if _, ok := s.Load("vendor"); ok {
		t.Error("a nil store loads a catalog")
	}
	s.Flush()
	if Open("", nil) != nil {
		t.Error("Open of an empty directory is not nil")
	}
}

// TestSaveDoesNotChangeTheCallersCatalog: a provider's cached catalog is
// shared, so normalizing or cutting it must happen on a copy.
func TestSaveDoesNotChangeTheCallersCatalog(t *testing.T) {
	s, _, _ := countingStore(t, t.TempDir())
	c := picker.Catalog{Kind: picker.KindSingle, Source: picker.SourceLive,
		Options: []picker.Option{{ID: "a"}}}
	s.Save("vendor", "", c)
	s.Flush()
	if c.Options[0].Label != "" {
		t.Errorf("caller's option label = %q, want it untouched", c.Options[0].Label)
	}
}
