// Package auth manages device tokens for application-level authentication.
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a device cannot be located.
var ErrNotFound = errors.New("device not found")

// ErrInvalidToken is returned when a presented token does not match any device.
var ErrInvalidToken = errors.New("invalid device token")

// lastUsedFlushInterval bounds how often Validate rewrites devices.json solely
// for LastUsedAt updates (H7). Create/Revoke/Prune always flush immediately.
const lastUsedFlushInterval = 5 * time.Minute

// Device is the public metadata for a paired device (never includes the token).
type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// ClientKeyFP is the SPKI fingerprint of the device's client key (ADR 0005),
	// recorded at pair time. Empty means a legacy/keyless device: it can still
	// authenticate by token while auth.require_client_key is off, but must
	// re-pair once enforcement is on.
	ClientKeyFP string `json:"client_key_fp,omitempty"`
}

type deviceRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"token_hash"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// ClientKeyFP is additive and optional — a record written before Phase 3
	// simply omits it, and unmarshals to "". No migration is required.
	ClientKeyFP string `json:"client_key_fp,omitempty"`
}

type fileData struct {
	Devices []deviceRecord `json:"devices"`
}

// Store is a file-backed device token store with an in-memory cache.
// Mutating ops (Create/Revoke/Prune) flush immediately; Validate updates
// LastUsedAt in memory and debounces disk writes.
//
// The file is shared with other processes (the CLI's pair revoke/prune/create
// opens its own Store), so every operation re-reads the file when its
// mtime/size changed since we last touched it. Without that, a daemon that
// cached devices.json at startup would keep honoring a token the CLI revoked —
// and its next debounced LastUsedAt flush would rewrite the file from the
// stale cache, resurrecting the revoked device and erasing CLI-created ones.
type Store struct {
	path string
	mu   sync.Mutex

	// devices is the in-memory set (loaded on Open, refreshed on file change).
	devices []deviceRecord
	// fileMod/fileSize identify the file version devices was loaded from.
	fileMod  time.Time
	fileSize int64
	// lastFlush is when we last wrote the file for a LastUsedAt-only dirty.
	lastFlush time.Time
	// lastUsedDirty is true when LastUsedAt changed since last flush.
	lastUsedDirty bool

	// saveCount counts durable writes (tests / diagnostics).
	saveCount int
}

// reloadIfChangedLocked re-reads the file when another process rewrote it.
// In-memory LastUsedAt updates not yet flushed are lost on reload — usage
// timestamps are best-effort; credential state wins. Caller holds s.mu.
func (s *Store) reloadIfChangedLocked() {
	st, err := os.Stat(s.path)
	if err != nil {
		return
	}
	if st.ModTime().Equal(s.fileMod) && st.Size() == s.fileSize {
		return
	}
	data, err := s.readFile()
	if err != nil {
		return
	}
	s.devices = data.Devices
	s.fileMod = st.ModTime()
	s.fileSize = st.Size()
	s.lastUsedDirty = false
}

// noteFileLocked records the on-disk version matching the in-memory state.
func (s *Store) noteFileLocked() {
	if st, err := os.Stat(s.path); err == nil {
		s.fileMod = st.ModTime()
		s.fileSize = st.Size()
	}
}

// OpenStore opens or creates a device store at path (typically data_dir/devices.json).
func OpenStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{path: path, lastFlush: time.Now().UTC()}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.persistLocked(fileData{Devices: []deviceRecord{}}); err != nil {
			return nil, err
		}
		s.devices = nil
		return s, nil
	}
	data, err := s.readFile()
	if err != nil {
		return nil, err
	}
	s.devices = data.Devices
	s.noteFileLocked()
	return s, nil
}

// SaveCount returns how many times the store has written devices.json (tests).
func (s *Store) SaveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveCount
}

// Flush writes any debounced LastUsedAt updates to disk.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastUsedDirty {
		return nil
	}
	s.reloadIfChangedLocked()
	if !s.lastUsedDirty {
		// The reload discarded the pending timestamps along with the stale view.
		return nil
	}
	return s.persistLocked(fileData{Devices: s.devices})
}

// Create issues a new device token. The plaintext token is returned once.
func (s *Store) Create(name string) (device Device, plaintextToken string, err error) {
	return s.CreateWithClientKey(name, "")
}

// CreateWithClientKey issues a new device token and records the device's client
// key fingerprint (ADR 0005). An empty clientKeyFP records a keyless device.
// The plaintext token is returned once.
func (s *Store) CreateWithClientKey(name, clientKeyFP string) (device Device, plaintextToken string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()

	token, err := GenerateToken()
	if err != nil {
		return Device{}, "", err
	}
	if name == "" {
		name = "device"
	}
	now := time.Now().UTC()
	rec := deviceRecord{
		ID:          uuid.NewString(),
		Name:        name,
		TokenHash:   HashToken(token),
		CreatedAt:   now,
		ClientKeyFP: clientKeyFP,
	}
	s.devices = append(s.devices, rec)
	if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
		// Roll back in-memory append on failure.
		s.devices = s.devices[:len(s.devices)-1]
		return Device{}, "", err
	}
	return Device{
		ID:          rec.ID,
		Name:        rec.Name,
		CreatedAt:   rec.CreatedAt,
		ClientKeyFP: rec.ClientKeyFP,
	}, token, nil
}

// List returns device metadata (no tokens or hashes).
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()

	out := make([]Device, 0, len(s.devices))
	for _, rec := range s.devices {
		out = append(out, Device{
			ID:          rec.ID,
			Name:        rec.Name,
			CreatedAt:   rec.CreatedAt,
			LastUsedAt:  rec.LastUsedAt,
			ClientKeyFP: rec.ClientKeyFP,
		})
	}
	return out, nil
}

// Prune removes device records that are stale or keyless, returning what was
// removed. A record is pruned when it has never been used or was last used
// before staleBefore (when non-zero), or — if keylessOnly is set instead — when
// it carries no enrolled client key (the legacy record a re-pair leaves behind
// under client-key enforcement). This is the operator's reap for the
// LastUsedAt the store already records; the durable token deliberately does not
// self-expire (see ADR 0006).
func (s *Store) Prune(staleBefore time.Time, keylessOnly bool) ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()

	var removed []Device
	s.devices = slices.DeleteFunc(s.devices, func(rec deviceRecord) bool {
		prune := false
		if keylessOnly {
			prune = rec.ClientKeyFP == ""
		} else if !staleBefore.IsZero() {
			last := rec.LastUsedAt
			prune = last == nil || last.Before(staleBefore)
		}
		if prune {
			removed = append(removed, Device{
				ID: rec.ID, Name: rec.Name,
				CreatedAt: rec.CreatedAt, LastUsedAt: rec.LastUsedAt,
				ClientKeyFP: rec.ClientKeyFP,
			})
		}
		return prune
	})

	if len(removed) == 0 {
		return nil, nil
	}
	if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
		return nil, err
	}
	return removed, nil
}

// Revoke removes a device by id or name. Returns ErrNotFound if missing.
func (s *Store) Revoke(idOrName string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()

	idx := slices.IndexFunc(s.devices, func(rec deviceRecord) bool {
		return rec.ID == idOrName || rec.Name == idOrName
	})
	if idx < 0 {
		return Device{}, ErrNotFound
	}
	rec := s.devices[idx]
	s.devices = slices.Delete(s.devices, idx, idx+1)
	if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
		return Device{}, err
	}
	return Device{
		ID:         rec.ID,
		Name:       rec.Name,
		CreatedAt:  rec.CreatedAt,
		LastUsedAt: rec.LastUsedAt,
	}, nil
}

// Validate checks a plaintext token and returns the matching device.
// On success, updates last_used_at in memory and debounces disk flush.
func (s *Store) Validate(plaintextToken string) (Device, error) {
	if plaintextToken == "" {
		return Device{}, ErrInvalidToken
	}
	hash := HashToken(plaintextToken)
	hashB := []byte(hash)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Pick up external revokes/creates (CLI) before honoring the token.
	s.reloadIfChangedLocked()

	for i, rec := range s.devices {
		recHash := []byte(rec.TokenHash)
		// Constant-time compare (equal length SHA-256 hex = 64 bytes).
		if len(recHash) != len(hashB) || subtle.ConstantTimeCompare(recHash, hashB) != 1 {
			continue
		}
		now := time.Now().UTC()
		s.devices[i].LastUsedAt = &now
		s.lastUsedDirty = true
		if time.Since(s.lastFlush) >= lastUsedFlushInterval {
			if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
				return Device{}, err
			}
		}
		return Device{
			ID:          rec.ID,
			Name:        rec.Name,
			CreatedAt:   rec.CreatedAt,
			LastUsedAt:  &now,
			ClientKeyFP: rec.ClientKeyFP,
		}, nil
	}
	return Device{}, ErrInvalidToken
}

func (s *Store) readFile() (fileData, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return fileData{}, fmt.Errorf("read devices: %w", err)
	}
	var data fileData
	if len(b) == 0 {
		return fileData{Devices: []deviceRecord{}}, nil
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return fileData{}, fmt.Errorf("parse devices: %w", err)
	}
	if data.Devices == nil {
		data.Devices = []deviceRecord{}
	}
	return data, nil
}

// persistLocked writes data and clears lastUsedDirty. Caller holds s.mu.
func (s *Store) persistLocked(data fileData) error {
	if data.Devices == nil {
		data.Devices = []deviceRecord{}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode devices: %w", err)
	}
	b = append(b, '\n')
	if err := writeFileAtomic(s.path, b); err != nil {
		return fmt.Errorf("write devices: %w", err)
	}
	s.devices = data.Devices
	s.noteFileLocked()
	s.lastUsedDirty = false
	s.lastFlush = time.Now().UTC()
	s.saveCount++
	return nil
}

// writeFileAtomic writes b to path via tmp + fsync + rename, mode 0600. The
// fsync matters: without it a crash right after the rename can still land an
// empty credentials file on some filesystems.
func writeFileAtomic(path string, b []byte) error {
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
	// Belt and braces for a pre-existing file's mode.
	_ = os.Chmod(path, 0o600)
	return nil
}
