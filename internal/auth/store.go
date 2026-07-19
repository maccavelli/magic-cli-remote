// Package auth manages device tokens for application-level authentication.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a device cannot be located.
var ErrNotFound = errors.New("device not found")

// ErrInvalidToken is returned when a presented token does not match any device.
var ErrInvalidToken = errors.New("invalid device token")

// Device is the public metadata for a paired device (never includes the token).
type Device struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type deviceRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"token_hash"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type fileData struct {
	Devices []deviceRecord `json:"devices"`
}

// Store is a file-backed device token store.
type Store struct {
	path string
	mu   sync.Mutex
}

// OpenStore opens or creates a device store at path (typically data_dir/devices.json).
func OpenStore(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{path: path}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.save(fileData{Devices: []deviceRecord{}}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Create issues a new device token. The plaintext token is returned once.
func (s *Store) Create(name string) (device Device, plaintextToken string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return Device{}, "", err
	}

	token, err := GenerateToken()
	if err != nil {
		return Device{}, "", err
	}
	if name == "" {
		name = "device"
	}
	now := time.Now().UTC()
	rec := deviceRecord{
		ID:        uuid.NewString(),
		Name:      name,
		TokenHash: HashToken(token),
		CreatedAt: now,
	}
	data.Devices = append(data.Devices, rec)
	if err := s.save(data); err != nil {
		return Device{}, "", err
	}
	return Device{
		ID:        rec.ID,
		Name:      rec.Name,
		CreatedAt: rec.CreatedAt,
	}, token, nil
}

// List returns device metadata (no tokens or hashes).
func (s *Store) List() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(data.Devices))
	for _, rec := range data.Devices {
		out = append(out, Device{
			ID:         rec.ID,
			Name:       rec.Name,
			CreatedAt:  rec.CreatedAt,
			LastUsedAt: rec.LastUsedAt,
		})
	}
	return out, nil
}

// Revoke removes a device by id or name. Returns ErrNotFound if missing.
func (s *Store) Revoke(idOrName string) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return Device{}, err
	}
	idx := -1
	for i, rec := range data.Devices {
		if rec.ID == idOrName || rec.Name == idOrName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Device{}, ErrNotFound
	}
	rec := data.Devices[idx]
	data.Devices = append(data.Devices[:idx], data.Devices[idx+1:]...)
	if err := s.save(data); err != nil {
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
// On success, updates last_used_at.
func (s *Store) Validate(plaintextToken string) (Device, error) {
	if plaintextToken == "" {
		return Device{}, ErrInvalidToken
	}
	hash := HashToken(plaintextToken)

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return Device{}, err
	}
	for i, rec := range data.Devices {
		if rec.TokenHash != hash {
			continue
		}
		now := time.Now().UTC()
		data.Devices[i].LastUsedAt = &now
		if err := s.save(data); err != nil {
			return Device{}, err
		}
		return Device{
			ID:         rec.ID,
			Name:       rec.Name,
			CreatedAt:  rec.CreatedAt,
			LastUsedAt: &now,
		}, nil
	}
	return Device{}, ErrInvalidToken
}

func (s *Store) load() (fileData, error) {
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

func (s *Store) save(data fileData) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode devices: %w", err)
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write devices: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace devices: %w", err)
	}
	// Ensure mode even if umask interfered on create.
	_ = os.Chmod(s.path, 0o600)
	return nil
}
