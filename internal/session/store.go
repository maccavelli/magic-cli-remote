package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Record is durable session metadata on disk.
type Record struct {
	ID       string      `json:"id"`
	Provider provider.ID `json:"provider"`
	Name     string      `json:"name"`
	CWD      string      `json:"cwd,omitempty"`
	// Model is the agent model this session was last (re)started with.
	// Empty means the provider's default. Persisted so resume after daemon
	// restart does not silently switch models (Phase 3.3).
	Model          string `json:"model,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// OwnerDeviceID is the paired device that owns this session (R4=B).
	// Empty means legacy/unowned — visible until claimed.
	OwnerDeviceID string    `json:"owner_device_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Status        string    `json:"status"`
}

// Store persists session metadata under data_dir/sessions/<id>/meta.json.
type Store struct {
	root string
	log  *slog.Logger
	mu   sync.Mutex
}

// OpenStore creates the sessions root directory.
func OpenStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "sessions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root, log: slog.Default()}, nil
}

// atomicWrite durably replaces path with b: write a temp file in the same
// directory, fsync it, rename over path, then fsync the parent directory.
// The file fsync guards against a truncated/empty file after a crash; the
// parent-dir fsync makes the rename itself survive a crash — without it, a
// crash right after the rename can lose the rename on some filesystems.
// Directory fsync is best-effort (not every filesystem supports it): a failure
// is logged at debug and never fails the write.
func (s *Store) atomicWrite(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.syncDir(filepath.Dir(path))
	return nil
}

// syncDir fsyncs a directory so a rename within it is durable across a crash.
// Best-effort: some filesystems do not support directory fsync, so errors are
// logged at debug and swallowed.
func (s *Store) syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		if s.log != nil {
			s.log.Debug("open session dir for fsync failed",
				slog.String("dir", dir), slog.String("err", err.Error()))
		}
		return
	}
	if err := d.Sync(); err != nil && s.log != nil {
		s.log.Debug("session dir fsync failed",
			slog.String("dir", dir), slog.String("err", err.Error()))
	}
	d.Close()
}

func (s *Store) safeDir(id string) string {
	// Clean the ID and make it absolute to evaluate out any ".." components,
	// then Join strips the leading slash, pinning it safely under root.
	return filepath.Join(s.root, filepath.Clean("/"+id))
}

func (s *Store) path(id string) string {
	return filepath.Join(s.safeDir(id), "meta.json")
}

// Save writes a session record.
func (s *Store) Save(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.safeDir(rec.ID)
	if dir == s.root {
		return fmt.Errorf("invalid session id")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// tmp + fsync + rename + parent-dir fsync: without the file fsync a crash
	// right after the rename can land an empty meta.json on some filesystems,
	// and without the parent-dir fsync the rename itself can be lost — the exact
	// corruption the rename dance exists to prevent.
	return s.atomicWrite(s.path(rec.ID), b)
}

// Get loads a record by id.
func (s *Store) Get(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// List returns all saved records.
func (s *Store) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.root, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var rec Record
		if json.Unmarshal(b, &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// Delete removes a session directory.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.safeDir(id)
	if dir == s.root {
		return fmt.Errorf("invalid session id")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	return nil
}

// historyFile is the on-disk shape of a durable transcript (Phase D).
// Same retention as the live ring: last historyBufferCap events, oldest drop.
type historyFile struct {
	// Events is oldest-first, each stamped with Seq from the live pump.
	Events []event.Event `json:"events"`
}

func (s *Store) historyPath(id string) string {
	return filepath.Join(s.safeDir(id), "history.json")
}

// SaveHistory atomically writes the durable transcript for a session.
// events may be nil or longer than historyBufferCap; only the tail is kept.
// Files are 0600 under the session dir (same uid as the daemon; no off-host sync).
func (s *Store) SaveHistory(id string, events []event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.safeDir(id)
	if dir == s.root {
		return fmt.Errorf("invalid session id")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Cap to the live ring budget so disk cannot grow without bound.
	if len(events) > historyBufferCap {
		events = events[len(events)-historyBufferCap:]
	}
	// Always write a concrete slice so the file is never null-JSON for events.
	if events == nil {
		events = []event.Event{}
	}
	b, err := json.Marshal(historyFile{Events: events})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return s.atomicWrite(s.historyPath(id), b)
}

// LoadHistory returns the durable transcript for a session, or an empty
// non-nil slice when missing/corrupt. Unknown ids are not an error (same
// contract as live History).
func (s *Store) LoadHistory(id string) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.historyPath(id))
	if err != nil {
		return []event.Event{}
	}
	var hf historyFile
	if json.Unmarshal(b, &hf) != nil {
		return []event.Event{}
	}
	if hf.Events == nil {
		return []event.Event{}
	}
	if len(hf.Events) > historyBufferCap {
		return hf.Events[len(hf.Events)-historyBufferCap:]
	}
	return hf.Events
}
