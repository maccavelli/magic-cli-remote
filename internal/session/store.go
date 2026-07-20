package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Record is durable session metadata on disk.
type Record struct {
	ID             string      `json:"id"`
	Provider       provider.ID `json:"provider"`
	Name           string      `json:"name"`
	CWD            string      `json:"cwd,omitempty"`
	AgentSessionID string      `json:"agent_session_id,omitempty"`
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
	mu   sync.Mutex
}

// OpenStore creates the sessions root directory.
func OpenStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "sessions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.root, id, "meta.json")
}

// Save writes a session record.
func (s *Store) Save(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, rec.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path(rec.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(rec.ID))
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
	dir := filepath.Join(s.root, id)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete session %s: %w", id, err)
	}
	return nil
}
