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
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
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
	Model string `json:"model,omitempty"`
	// ThinkingLevel is the session's reasoning/thinking effort override.
	// Empty means the provider default (MADR 0052).
	ThinkingLevel       string `json:"thinking_level,omitempty"`
	ModeID              string `json:"mode_id,omitempty"`
	CollaborationModeID string `json:"collaboration_mode_id,omitempty"`
	PermissionProfileID string `json:"permission_profile_id,omitempty"`
	ApprovalsReviewer   string `json:"approvals_reviewer,omitempty"`
	ServiceTier         string `json:"service_tier,omitempty"`
	Personality         string `json:"personality,omitempty"`
	AgentSessionID      string `json:"agent_session_id,omitempty"`
	// OwnerDeviceID is the paired device that owns this session (R4=B).
	// Empty means legacy/unowned — visible until claimed.
	OwnerDeviceID string `json:"owner_device_id,omitempty"`
	// PendingHandoffTo scopes a released session to one target device during
	// a handoff (MADR 0078 D2). Persisted so a release survives a daemon
	// restart mid-transfer; always empty once owned.
	PendingHandoffTo string `json:"pending_handoff_to,omitempty"`
	// HandoffNonce links the release and claim receipts of one transfer
	// (MADR 0078 D4). Persisted alongside PendingHandoffTo.
	HandoffNonce string    `json:"handoff_nonce,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Status       string    `json:"status"`
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

// atomicWrite durably replaces path with b via unique same-directory staging.
func (s *Store) atomicWrite(path string, b []byte) error {
	return fsutil.WriteFileAtomic(path, b, fsutil.AtomicOptions{
		Perm:     0o600,
		SyncFile: true,
		SyncDir:  true,
	})
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

// List returns all readable records. skipped counts session dirs whose
// meta.json could not be read or decoded (MADR 0056 H-6). A root ReadDir
// failure is returned as err (not masked as an empty complete list).
func (s *Store) List() (out []Record, skipped int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	out = make([]Record, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(s.root, e.Name(), "meta.json"))
		if readErr != nil {
			skipped++
			s.log.Warn("session list skipped unreadable meta",
				slog.String("session_id", e.Name()),
				slog.String("err", readErr.Error()),
			)
			continue
		}
		var rec Record
		if err := json.Unmarshal(b, &rec); err != nil {
			skipped++
			s.log.Warn("session list skipped corrupt meta",
				slog.String("session_id", e.Name()),
				slog.String("err", err.Error()),
			)
			continue
		}
		out = append(out, rec)
	}
	return out, skipped, nil
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

// epochFile records the daemon boot epoch and whether the last shutdown
// flushed cleanly (MADR 0068 P3). A dirty marker at load time means up to
// persistDebounce worth of events may be unflushed and seq counters can
// have regressed — the manager mints a fresh epoch so clients drop cached
// seqs instead of silently filtering real events.
type epochFile struct {
	Epoch string `json:"epoch"`
	Clean bool   `json:"clean"`
}

func (s *Store) epochPath() string { return filepath.Join(s.root, "epoch.json") }

// LoadEpoch returns the persisted boot epoch and clean-shutdown flag;
// ok=false when missing or corrupt (both mean "mint a new epoch").
func (s *Store) LoadEpoch() (epoch string, clean bool, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.epochPath())
	if err != nil {
		return "", false, false
	}
	var f epochFile
	if err := json.Unmarshal(b, &f); err != nil || f.Epoch == "" {
		return "", false, false
	}
	return f.Epoch, f.Clean, true
}

// SaveEpoch atomically persists the boot epoch and clean flag.
func (s *Store) SaveEpoch(epoch string, clean bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(epochFile{Epoch: epoch, Clean: clean})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return s.atomicWrite(s.epochPath(), b)
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
