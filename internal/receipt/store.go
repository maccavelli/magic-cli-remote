package receipt

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// seekChunkSize bounds how much of a device's receipt file LastHash reads
// from disk on a cache miss: fixed-size backward chunks, not the whole
// file — a device's receipt log can grow large over its lifetime by design
// (MADR 0077 D6/§8 risk table).
const seekChunkSize = 4096

// Store is an append-only, per-device, backward-chained store of
// signed receipts (MADR 0077 D6): one JSON Lines file per device
// (<dir>/<deviceID>.jsonl), each line a JWS compact string, never rewritten.
type Store struct {
	dir string
	mu  sync.Mutex
	// lastHash caches each device's last stored line's SHA-256 (hex), so a
	// hot sequence of Appends for the same device never re-reads disk.
	lastHash map[string]string
}

// NewStore opens (creating if needed) the receipts store rooted at
// <dataDir>/receipts — the same dataDir-relative-subdirectory convention
// internal/session.OpenStore uses for "sessions".
func NewStore(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "receipts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create receipts dir: %w", err)
	}
	return &Store{dir: dir, lastHash: make(map[string]string)}, nil
}

func (s *Store) path(deviceID string) string {
	return filepath.Join(s.dir, deviceID+".jsonl")
}

// LastHash returns the SHA-256 (lowercase hex) of deviceID's last stored
// line, and false if the device has no entries yet.
func (s *Store) LastHash(deviceID string) (hash string, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastHashLocked(deviceID)
}

func (s *Store) lastHashLocked(deviceID string) (string, bool, error) {
	if h, ok := s.lastHash[deviceID]; ok {
		return h, true, nil
	}
	line, ok, err := readLastLine(s.path(deviceID))
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	sum := sha256.Sum256(line)
	h := hex.EncodeToString(sum[:])
	s.lastHash[deviceID] = h
	return h, true, nil
}

// readLastLine returns the last non-empty line of path (without its
// trailing newline), reading backward from the end in fixed-size chunks
// rather than the whole file. Returns ok=false if the file does not exist
// or is empty.
func readLastLine(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", path, err)
	}
	size := info.Size()
	if size == 0 {
		return nil, false, nil
	}

	var tail []byte
	pos := size
	for pos > 0 {
		readSize := int64(seekChunkSize)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize
		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, pos); err != nil && err != io.EOF {
			return nil, false, fmt.Errorf("read %s: %w", path, err)
		}
		tail = append(chunk, tail...)

		trimmed := tail
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if idx := bytes.LastIndexByte(trimmed, '\n'); idx >= 0 {
			return trimmed[idx+1:], true, nil
		}
		if pos == 0 {
			if len(trimmed) == 0 {
				return nil, false, nil
			}
			return trimmed, true, nil
		}
	}
	return nil, false, nil
}

// Append durably appends jwsCompact as a new line for deviceID and updates
// the in-memory last-hash cache. Never opens the file for anything but
// append — no read-modify-rewrite path exists in this type at all, by
// construction, so internal/session.Store.SaveHistory's whole-file rewrite
// pattern cannot accidentally get copied into this file later.
func (s *Store) Append(deviceID, jwsCompact string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Warm the cache from disk first (a no-op if already warm) so a process
	// that restarts mid-sequence still chains onto its true last entry.
	if _, _, err := s.lastHashLocked(deviceID); err != nil {
		return err
	}

	p := s.path(deviceID)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(jwsCompact + "\n")); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", p, err)
	}

	sum := sha256.Sum256([]byte(jwsCompact))
	s.lastHash[deviceID] = hex.EncodeToString(sum[:])
	return nil
}

// peekPredicateType reads compact's predicateType without verifying its
// signature — used only to choose which candidate key to verify against
// next; the actual trust decision still comes from the cryptographic
// verification that follows, not from this unverified label (MADR 0077
// §7.2: "predicateType alone tells a verifier which key to check against").
func peekPredicateType(compact string) (string, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return "", ErrMalformedJWS
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("%w: payload: %v", ErrMalformedJWS, err)
	}
	var peek struct {
		PredicateType string `json:"predicateType"`
	}
	if err := json.Unmarshal(payload, &peek); err != nil {
		return "", fmt.Errorf("parse statement: %w", err)
	}
	return peek.PredicateType, nil
}

// Verify walks deviceID's chain from the first line, confirming each line's
// JWS signature (against devicePub for permission-decision entries, against
// daemonPub for receipt-unavailable entries — MADR 0077 D8) and that its
// chain.prev_sha256 matches the SHA-256 of the line above it. Returns -1 if
// the chain is intact (including a device with no entries yet), or the
// 1-indexed line at which the first break was found — a bad signature, a
// chain-link mismatch, an unrecognized predicateType, or malformed content
// all count as "broken at this line."
func (s *Store) Verify(deviceID string, devicePub, daemonPub *ecdsa.PublicKey) (brokenAtLine int, err error) {
	p := s.path(deviceID)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return -1, nil
		}
		return 0, fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var prevHash *string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		predType, perr := peekPredicateType(line)
		if perr != nil {
			return lineNum, nil
		}
		var pub *ecdsa.PublicKey
		switch predType {
		case PredicateTypePermissionDecision:
			pub = devicePub
		case PredicateTypeReceiptUnavailable:
			pub = daemonPub
		default:
			return lineNum, nil
		}

		payload, verr := VerifyES256Compact(pub, line)
		if verr != nil {
			return lineNum, nil
		}
		var stmt Statement
		if err := json.Unmarshal(payload, &stmt); err != nil {
			return lineNum, nil
		}
		if !sameHash(stmt.Chain.PrevSHA256, prevHash) {
			return lineNum, nil
		}

		sum := sha256.Sum256([]byte(line))
		h := hex.EncodeToString(sum[:])
		prevHash = &h
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", p, err)
	}
	return -1, nil
}

func sameHash(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
