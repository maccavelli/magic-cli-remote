// Package auth manages device tokens for application-level authentication.
package auth

import (
	"crypto/ecdsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

const deviceNameMaxLen = 64

// sanitizeDeviceName makes a client- or CLI-supplied device name safe to store,
// render in `pair list`, and disambiguate from device ids:
//   - strips control characters (C0/C1, including the ANSI escape introducer)
//     that would otherwise corrupt terminal output;
//   - collapses whitespace runs and trims;
//   - truncates on a rune boundary (a byte cut splits a multi-byte rune);
//   - deflects a UUID-shaped name, which would otherwise collide with a device
//     id in Revoke(idOrName) and let one device shadow another's revoke.
//
// An empty (or fully stripped) name falls back to "device".
func sanitizeDeviceName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r == '\t' || r == ' ':
			b.WriteByte(' ')
		case unicode.IsControl(r) || r == utf8.RuneError:
			// drop control chars and invalid UTF-8 bytes
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Join(strings.Fields(b.String()), " ")
	if name == "" {
		return "device"
	}
	if len(name) > deviceNameMaxLen {
		cut := deviceNameMaxLen
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = strings.TrimSpace(name[:cut])
	}
	if name == "" {
		return "device"
	}
	if _, err := uuid.Parse(name); err == nil {
		name = "device-" + name
	}
	return name
}

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
	// ClientKeySPKI is the DER-encoded SubjectPublicKeyInfo the fingerprint
	// above was hashed from (MADR 0077 D9). Unlike the one-way fingerprint,
	// this is the actual public key, needed to verify a signed receipt.
	// Empty for records created before this field existed or that have never
	// reconnected since — see BackfillClientKeySPKI.
	ClientKeySPKI []byte `json:"client_key_spki,omitempty"`
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
	// ClientKeySPKI mirrors Device.ClientKeySPKI; see its doc comment.
	ClientKeySPKI []byte `json:"client_key_spki,omitempty"`
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

// reloadForcedLocked always re-reads the file from disk. Used under withPathLock
// so multi-process mutations start from the latest durable state, not a stale
// mtime/size cache. Caller holds s.mu. Does not clear lastUsedDirty — callers
// that need a clean merge should snapshot timestamps first (see mergeLastUsedLocked).
func (s *Store) reloadForcedLocked() error {
	data, err := s.readFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.devices = nil
			return nil
		}
		return err
	}
	s.devices = data.Devices
	s.noteFileLocked()
	return nil
}

// snapshotLastUsedLocked returns pending LastUsedAt values while dirty.
// Caller holds s.mu.
func (s *Store) snapshotLastUsedLocked() map[string]time.Time {
	if !s.lastUsedDirty {
		return nil
	}
	pending := make(map[string]time.Time, len(s.devices))
	for _, rec := range s.devices {
		if rec.LastUsedAt != nil {
			pending[rec.ID] = *rec.LastUsedAt
		}
	}
	return pending
}

// mergeLastUsedLocked re-applies pending LastUsedAt onto the current device set
// when they are newer than (or missing from) disk. Caller holds s.mu.
func (s *Store) mergeLastUsedLocked(pending map[string]time.Time) {
	if len(pending) == 0 {
		s.lastUsedDirty = false
		return
	}
	dirty := false
	for i := range s.devices {
		t, ok := pending[s.devices[i].ID]
		if !ok {
			continue
		}
		cur := s.devices[i].LastUsedAt
		if cur == nil || t.After(*cur) {
			tt := t
			s.devices[i].LastUsedAt = &tt
			dirty = true
		}
	}
	s.lastUsedDirty = dirty
}

// withLocked runs fn while holding the cross-process path lock and s.mu.
// The file is force-reloaded (with LastUsedAt merge) before fn so concurrent
// CLI/daemon writers cannot clobber each other (Phase 1.5).
func (s *Store) withLocked(fn func() error) error {
	return withPathLock(s.path, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		pending := s.snapshotLastUsedLocked()
		if err := s.reloadForcedLocked(); err != nil {
			return err
		}
		s.mergeLastUsedLocked(pending)
		return fn()
	})
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

// Flush writes any debounced LastUsedAt updates to disk under the path flock,
// merging timestamps onto the latest durable device set so a concurrent CLI
// Create/Revoke is not clobbered.
func (s *Store) Flush() error {
	s.mu.Lock()
	dirty := s.lastUsedDirty
	s.mu.Unlock()
	if !dirty {
		return nil
	}
	return s.withLocked(func() error {
		if !s.lastUsedDirty {
			return nil
		}
		return s.persistLocked(fileData{Devices: s.devices})
	})
}

// Create issues a new device token. The plaintext token is returned once.
func (s *Store) Create(name string) (device Device, plaintextToken string, err error) {
	return s.CreateWithClientKey(name, "", nil)
}

// CreateWithClientKey issues a new device token and records the device's client
// key fingerprint (ADR 0005) and public key (MADR 0077 D9). An empty
// clientKeyFP records a keyless device; clientKeySPKI may be nil independent
// of clientKeyFP (a caller that only knows the fingerprint, not the raw key).
// The plaintext token is returned once.
func (s *Store) CreateWithClientKey(name, clientKeyFP string, clientKeySPKI []byte) (device Device, plaintextToken string, err error) {
	token, err := GenerateToken()
	if err != nil {
		return Device{}, "", err
	}
	name = sanitizeDeviceName(name)
	var out Device
	err = s.withLocked(func() error {
		now := time.Now().UTC()
		rec := deviceRecord{
			ID:            uuid.NewString(),
			Name:          name,
			TokenHash:     HashToken(token),
			CreatedAt:     now,
			ClientKeyFP:   clientKeyFP,
			ClientKeySPKI: clientKeySPKI,
		}
		s.devices = append(s.devices, rec)
		if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
			s.devices = s.devices[:len(s.devices)-1]
			return err
		}
		out = Device{
			ID:            rec.ID,
			Name:          rec.Name,
			CreatedAt:     rec.CreatedAt,
			ClientKeyFP:   rec.ClientKeyFP,
			ClientKeySPKI: rec.ClientKeySPKI,
		}
		return nil
	})
	if err != nil {
		return Device{}, "", err
	}
	return out, token, nil
}

// List returns device metadata (no tokens or hashes).
func (s *Store) List() ([]Device, error) {
	var out []Device
	err := s.withLocked(func() error {
		out = make([]Device, 0, len(s.devices))
		for _, rec := range s.devices {
			out = append(out, Device{
				ID:            rec.ID,
				Name:          rec.Name,
				CreatedAt:     rec.CreatedAt,
				LastUsedAt:    rec.LastUsedAt,
				ClientKeyFP:   rec.ClientKeyFP,
				ClientKeySPKI: rec.ClientKeySPKI,
			})
		}
		return nil
	})
	return out, err
}

// Prune removes device records that are stale or keyless, returning what was
// removed. A record is pruned when it has never been used or was last used
// before staleBefore (when non-zero), or — if keylessOnly is set instead — when
// it carries no enrolled client key (the legacy record a re-pair leaves behind
// under client-key enforcement). This is the operator's reap for the
// LastUsedAt the store already records; the durable token deliberately does not
// self-expire (see ADR 0006).
func (s *Store) Prune(staleBefore time.Time, keylessOnly bool) ([]Device, error) {
	var removed []Device
	err := s.withLocked(func() error {
		removed = nil
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
					ClientKeyFP: rec.ClientKeyFP, ClientKeySPKI: rec.ClientKeySPKI,
				})
			}
			return prune
		})
		if len(removed) == 0 {
			return nil
		}
		return s.persistLocked(fileData{Devices: s.devices})
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// RevokeByClientKeyFP removes every device enrolled with the given SPKI
// fingerprint except keepID (the newly paired device). Empty fp is a no-op.
// Used at pair.claim so re-pairing the same phone key does not accumulate
// twin device rows (MADR 0072 D4).
func (s *Store) RevokeByClientKeyFP(fp, keepID string) ([]Device, error) {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return nil, nil
	}
	var removed []Device
	err := s.withLocked(func() error {
		removed = nil
		s.devices = slices.DeleteFunc(s.devices, func(rec deviceRecord) bool {
			if rec.ClientKeyFP != fp || rec.ID == keepID {
				return false
			}
			removed = append(removed, Device{
				ID: rec.ID, Name: rec.Name,
				CreatedAt: rec.CreatedAt, LastUsedAt: rec.LastUsedAt,
				ClientKeyFP: rec.ClientKeyFP, ClientKeySPKI: rec.ClientKeySPKI,
			})
			return true
		})
		if len(removed) == 0 {
			return nil
		}
		return s.persistLocked(fileData{Devices: s.devices})
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// ErrNoPublicKey is returned by PublicKeyFor when the device has an enrolled
// key fingerprint but no persisted SPKI bytes yet — a record created before
// MADR 0077 D9 that has not reconnected since (see BackfillClientKeySPKI).
var ErrNoPublicKey = errors.New("device has no persisted public key yet — it must reconnect once before its receipts can be verified")

// PublicKeyFor returns the P-256 public key persisted for deviceID (MADR 0077
// D9), for verifying that device's signed receipts. Returns ErrNotFound if no
// such device exists, ErrNoPublicKey if the device predates key persistence
// and has not reconnected since.
func (s *Store) PublicKeyFor(deviceID string) (*ecdsa.PublicKey, error) {
	var spki []byte
	err := s.withLocked(func() error {
		idx := slices.IndexFunc(s.devices, func(rec deviceRecord) bool {
			return rec.ID == deviceID
		})
		if idx < 0 {
			return ErrNotFound
		}
		spki = s.devices[idx].ClientKeySPKI
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(spki) == 0 {
		return nil, ErrNoPublicKey
	}
	pub, err := x509.ParsePKIXPublicKey(spki)
	if err != nil {
		return nil, fmt.Errorf("parse persisted public key: %w", err)
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("persisted public key is %T, not ECDSA", pub)
	}
	return ecPub, nil
}

// BackfillClientKeySPKI records deviceID's public key if it does not already
// have one persisted (MADR 0077 D9's self-healing backfill for devices
// enrolled before this field existed). A no-op if spki is empty or the
// device already has a persisted key — never overwrites a populated value.
func (s *Store) BackfillClientKeySPKI(deviceID string, spki []byte) error {
	if len(spki) == 0 {
		return nil
	}
	return s.withLocked(func() error {
		idx := slices.IndexFunc(s.devices, func(rec deviceRecord) bool {
			return rec.ID == deviceID
		})
		if idx < 0 {
			return ErrNotFound
		}
		if len(s.devices[idx].ClientKeySPKI) > 0 {
			return nil
		}
		s.devices[idx].ClientKeySPKI = spki
		return s.persistLocked(fileData{Devices: s.devices})
	})
}

// Revoke removes a device by id or name. Returns ErrNotFound if missing.
func (s *Store) Revoke(idOrName string) (Device, error) {
	var out Device
	err := s.withLocked(func() error {
		// Prefer an exact id match so a device whose NAME collides with another
		// device's id can never shadow revoking that id. New names are sanitized
		// to never be UUID-shaped; the id-first pass also protects legacy records.
		idx := slices.IndexFunc(s.devices, func(rec deviceRecord) bool {
			return rec.ID == idOrName
		})
		if idx < 0 {
			idx = slices.IndexFunc(s.devices, func(rec deviceRecord) bool {
				return rec.Name == idOrName
			})
		}
		if idx < 0 {
			return ErrNotFound
		}
		rec := s.devices[idx]
		s.devices = slices.Delete(s.devices, idx, idx+1)
		if err := s.persistLocked(fileData{Devices: s.devices}); err != nil {
			return err
		}
		out = Device{
			ID:         rec.ID,
			Name:       rec.Name,
			CreatedAt:  rec.CreatedAt,
			LastUsedAt: rec.LastUsedAt,
		}
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	return out, nil
}

// Validate checks a plaintext token and returns the matching device.
// On success, updates last_used_at in memory and debounces disk flush.
// Cross-process flock is held for the whole check+optional write so a concurrent
// CLI revoke cannot be clobbered by a LastUsedAt flush (Phase 1.5).
func (s *Store) Validate(plaintextToken string) (Device, error) {
	if plaintextToken == "" {
		return Device{}, ErrInvalidToken
	}
	hash := HashToken(plaintextToken)
	hashB := []byte(hash)

	var out Device
	err := withPathLock(s.path, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		pending := s.snapshotLastUsedLocked()
		if err := s.reloadForcedLocked(); err != nil {
			return err
		}
		s.mergeLastUsedLocked(pending)
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
					return err
				}
			}
			out = Device{
				ID:            rec.ID,
				Name:          rec.Name,
				CreatedAt:     rec.CreatedAt,
				LastUsedAt:    &now,
				ClientKeyFP:   rec.ClientKeyFP,
				ClientKeySPKI: rec.ClientKeySPKI,
			}
			return nil
		}
		return ErrInvalidToken
	})
	if err != nil {
		return Device{}, err
	}
	return out, nil
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

// writeFileAtomic writes b to path via unique temp + fsync + rename, mode 0600.
func writeFileAtomic(path string, b []byte) error {
	return fsutil.WriteFileAtomic(path, b, fsutil.AtomicOptions{
		Perm:     0o600,
		SyncFile: true,
		SyncDir:  true,
	})
}
