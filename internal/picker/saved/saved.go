// Package saved keeps each provider's last live model catalog on disk, one
// file per provider in a per-instance directory, so that a provider whose
// agent cannot answer serves the list that agent last gave on this host
// before it falls back to its static list (MADR 0235 D1).
//
// Only a non-empty live catalog is saved. Save never touches the disk on the
// caller's goroutine: it records the catalog in memory, where Load finds it at
// once, and a background writer puts the newest one for each provider on disk.
// Save is called with a provider's or a session's lock held, and a file write
// there would stall the agent spawn path (0235 PLAN C8).
package saved

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// MaxOptions is the most options a saved catalog keeps: the transport's own
// cap on a models.list reply, so a saved list never holds rows no reply could
// carry (MADR 0235 D1; MADR 0043 D4).
const MaxOptions = 500

// fileVersion is the version of the file format. A file of any other version
// loads as no saved catalog, and the next live answer replaces it.
const fileVersion = 1

// providerID is the shape a provider id must have to name a file. The ids are
// the registry's own constants; the check keeps any other string from naming a
// path outside the directory.
var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// file is the on-disk form. EngineVersion and SavedAt are written for the
// debug log only (MADR 0235 D1).
type file struct {
	Version       int             `json:"version"`
	Provider      string          `json:"provider"`
	SavedAt       time.Time       `json:"saved_at"`
	EngineVersion string          `json:"engine_version,omitempty"`
	Catalog       json.RawMessage `json:"catalog"`
}

// pendingWrite is one provider's catalog waiting for the writer.
type pendingWrite struct {
	catalog []byte // the catalog's JSON, as the equality test compares it
	data    []byte // the whole file
}

// Store holds the saved catalogs of one daemon instance. It is safe for
// concurrent use. A nil *Store is a valid store that holds nothing: Load
// reports nothing, and Save and Flush do nothing. That is how a provider built
// without one, and mcremote doctor, behave.
type Store struct {
	dir string
	log *slog.Logger
	// write puts data at path. It is fsutil.WriteFileAtomic, owner-only;
	// tests replace it to count or fail writes.
	write func(path string, data []byte) error
	now   func() time.Time

	mu sync.Mutex
	// current is each provider's newest catalog JSON, saved or loaded.
	current map[string][]byte
	// read records the providers whose file Load has already read.
	read map[string]bool
	// checked records the providers whose file the writer has compared with.
	checked map[string]bool
	pending map[string]pendingWrite
	writing bool
	idle    *sync.Cond
}

// Open returns the store for dir, or nil when dir is empty. It touches no
// file: the directory is made on the first write.
func Open(dir string, log *slog.Logger) *Store {
	if dir == "" {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Store{
		dir: dir,
		log: log.With(slog.String("component", "picker.saved")),
		write: func(path string, data []byte) error {
			return fsutil.WriteFileAtomic(path, data, fsutil.AtomicOptions{Perm: 0o600, SyncFile: true})
		},
		now:     time.Now,
		current: map[string][]byte{},
		read:    map[string]bool{},
		checked: map[string]bool{},
		pending: map[string]pendingWrite{},
	}
	s.idle = sync.NewCond(&s.mu)
	return s
}

// Save records c as provider's last live catalog and queues it for writing.
// It does nothing unless c is a live catalog with at least one option, or when
// c equals the catalog already saved. engineVersion may be empty.
func (s *Store) Save(provider, engineVersion string, c picker.Catalog) {
	if s == nil || c.Source != picker.SourceLive || len(c.Options) == 0 {
		return
	}
	if !providerID.MatchString(provider) {
		s.log.Warn("saved catalog: provider id cannot name a file; not saved",
			slog.String("provider", provider))
		return
	}
	cat, err := encodeCatalog(c)
	if err != nil {
		s.log.Warn("saved catalog: encode failed; not saved",
			slog.String("provider", provider), slog.String("err", err.Error()))
		return
	}
	data, err := json.Marshal(file{
		Version:       fileVersion,
		Provider:      provider,
		SavedAt:       s.now().UTC(),
		EngineVersion: engineVersion,
		Catalog:       cat,
	})
	if err != nil {
		s.log.Warn("saved catalog: encode failed; not saved",
			slog.String("provider", provider), slog.String("err", err.Error()))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if bytes.Equal(s.current[provider], cat) {
		return
	}
	s.current[provider] = cat
	s.pending[provider] = pendingWrite{catalog: cat, data: data}
	if !s.writing {
		s.writing = true
		go s.drain()
	}
}

// encodeCatalog is c as it is saved: normalized, live, and cut to MaxOptions
// with Truncated set. c's options are copied, never changed in place, since
// the caller's catalog may be shared.
func encodeCatalog(c picker.Catalog) ([]byte, error) {
	c.Options = slices.Clone(c.Options)
	if len(c.Options) > MaxOptions {
		c.Options = c.Options[:MaxOptions]
		c.Truncated = true
	}
	c = c.Normalize()
	c.Source = picker.SourceLive
	return json.Marshal(c)
}

// drain writes pending catalogs until none is left. One runs at a time.
func (s *Store) drain() {
	for {
		s.mu.Lock()
		if len(s.pending) == 0 {
			s.writing = false
			s.idle.Broadcast()
			s.mu.Unlock()
			return
		}
		providers := make([]string, 0, len(s.pending))
		for p := range s.pending {
			providers = append(providers, p)
		}
		sort.Strings(providers)
		provider := providers[0]
		w := s.pending[provider]
		delete(s.pending, provider)
		check := !s.checked[provider]
		s.checked[provider] = true
		s.mu.Unlock()

		s.writeOne(provider, w, check)
	}
}

// writeOne puts one provider's catalog on disk. On a provider's first write in
// this process it first reads the file there, and leaves an equal one alone,
// so a restarted daemon does not rewrite what it finds.
func (s *Store) writeOne(provider string, w pendingWrite, check bool) {
	path := s.path(provider)
	if check {
		if cat, ok := s.readFile(provider); ok && bytes.Equal(cat, w.catalog) {
			return
		}
	}
	if err := appdirs.EnsurePrivateDir(s.dir); err != nil {
		s.log.Warn("saved catalog: directory unavailable; not written",
			slog.String("provider", provider), slog.String("err", err.Error()))
		return
	}
	if err := s.write(path, w.data); err != nil {
		s.log.Warn("saved catalog: write failed",
			slog.String("provider", provider), slog.String("err", err.Error()))
	}
}

// Load returns provider's saved catalog, with Source set to
// picker.SourceSaved, and true; or false when none is saved, or its file is
// missing, unreadable, of another version or another provider's. The
// returned catalog is the caller's own copy.
func (s *Store) Load(provider string) (picker.Catalog, bool) {
	if s == nil || !providerID.MatchString(provider) {
		return picker.Catalog{}, false
	}
	s.mu.Lock()
	cat, ok := s.current[provider]
	if !ok && !s.read[provider] {
		s.read[provider] = true
		if cat, ok = s.readFile(provider); ok {
			s.current[provider] = cat
		}
	}
	s.mu.Unlock()
	if !ok {
		return picker.Catalog{}, false
	}
	var c picker.Catalog
	if err := json.Unmarshal(cat, &c); err != nil {
		return picker.Catalog{}, false
	}
	c.Source = picker.SourceSaved
	return c, true
}

// readFile reads and checks provider's file, returning its catalog JSON in
// the form Save compares. Anything wrong with the file is logged at debug and
// reported as no file: the next live answer replaces it.
func (s *Store) readFile(provider string) ([]byte, bool) {
	data, err := os.ReadFile(s.path(provider))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.log.Debug("saved catalog: unreadable; ignored",
				slog.String("provider", provider), slog.String("err", err.Error()))
		}
		return nil, false
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		s.log.Debug("saved catalog: undecodable; ignored",
			slog.String("provider", provider), slog.String("err", err.Error()))
		return nil, false
	}
	if f.Version != fileVersion || f.Provider != provider {
		s.log.Debug("saved catalog: another version or provider; ignored",
			slog.String("provider", provider), slog.Int("version", f.Version),
			slog.String("file_provider", f.Provider))
		return nil, false
	}
	var c picker.Catalog
	if err := json.Unmarshal(f.Catalog, &c); err != nil || len(c.Options) == 0 {
		s.log.Debug("saved catalog: no usable catalog; ignored", slog.String("provider", provider))
		return nil, false
	}
	cat, err := encodeCatalog(c)
	if err != nil {
		return nil, false
	}
	s.log.Debug("saved catalog: loaded",
		slog.String("provider", provider),
		slog.Time("saved_at", f.SavedAt),
		slog.String("engine_version", f.EngineVersion),
		slog.Int("options", len(c.Options)))
	return cat, true
}

// Flush waits until every queued write has finished: for tests, and for the
// daemon's shutdown, so a catalog saved just before it reaches the disk.
func (s *Store) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for s.writing || len(s.pending) > 0 {
		s.idle.Wait()
	}
	s.mu.Unlock()
}

func (s *Store) path(provider string) string {
	return filepath.Join(s.dir, provider+".json")
}
