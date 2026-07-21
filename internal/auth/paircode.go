package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Pair code alphabet: Crockford-ish, no 0/O/1/I ambiguity.
const pairCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// DefaultPairCodeTTL is the lifetime of a short pair code.
const DefaultPairCodeTTL = 5 * time.Minute

// MaxPairCodeTTL caps operator-supplied lifetimes: 40-bit one-shot codes are
// pairing aids, not durable credentials.
const MaxPairCodeTTL = time.Hour

// Pair code length (before display hyphen).
const pairCodeLen = 8

var (
	// ErrInvalidPairCode is returned when the code is unknown or malformed.
	ErrInvalidPairCode = errors.New("invalid pair code")
	// ErrExpiredPairCode is returned when the code is past its TTL.
	ErrExpiredPairCode = errors.New("pair code expired")
)

// PairCodeInfo is returned after creating a short code (plaintext once).
type PairCodeInfo struct {
	// Display is human-friendly XXXX-XXXX form.
	Display string
	// Code is the normalized 8-char form (no hyphen).
	Code      string
	Name      string
	ExpiresAt time.Time
}

type pairCodeRecord struct {
	CodeHash  string    `json:"code_hash"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type pairCodeFile struct {
	Codes []pairCodeRecord `json:"codes"`
}

// PairCodeStore holds short-lived pairing codes (hashed) for claim-before-auth.
type PairCodeStore struct {
	path string
	mu   sync.Mutex
}

// OpenPairCodeStore opens or creates the pair-code file under data_dir.
func OpenPairCodeStore(path string) (*PairCodeStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &PairCodeStore{path: path}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := s.save(pairCodeFile{Codes: []pairCodeRecord{}}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Create issues a new 8-char pair code. Plaintext is returned once; only the hash is stored.
func (s *PairCodeStore) Create(name string, ttl time.Duration) (PairCodeInfo, error) {
	if ttl <= 0 {
		ttl = DefaultPairCodeTTL
	}
	if ttl > MaxPairCodeTTL {
		ttl = MaxPairCodeTTL
	}
	if name == "" {
		name = "device"
	}
	code, err := generatePairCode()
	if err != nil {
		return PairCodeInfo{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return PairCodeInfo{}, err
	}
	data.Codes = purgeExpired(data.Codes, time.Now().UTC())

	now := time.Now().UTC()
	rec := pairCodeRecord{
		CodeHash:  HashPairCode(code),
		Name:      name,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	data.Codes = append(data.Codes, rec)
	if err := s.save(data); err != nil {
		return PairCodeInfo{}, err
	}
	return PairCodeInfo{
		Display:   FormatPairCode(code),
		Code:      code,
		Name:      name,
		ExpiresAt: rec.ExpiresAt,
	}, nil
}

// Claim validates a short code and removes it (one-shot). Returns the device name to use.
func (s *PairCodeStore) Claim(raw string) (name string, err error) {
	code := NormalizePairCode(raw)
	if len(code) != pairCodeLen {
		return "", ErrInvalidPairCode
	}
	// Validate alphabet.
	for _, r := range code {
		if !strings.ContainsRune(pairCodeAlphabet, r) {
			return "", ErrInvalidPairCode
		}
	}
	hash := HashPairCode(code)

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()

	// Look up before purging: purging first would reclassify a just-expired
	// code as "invalid", and the client's "expired" error path would be
	// unreachable.
	idx := -1
	for i, rec := range data.Codes {
		if rec.CodeHash == hash {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Unknown code: still persist the purge of anything stale.
		data.Codes = purgeExpired(data.Codes, now)
		_ = s.save(data)
		return "", ErrInvalidPairCode
	}
	rec := data.Codes[idx]
	data.Codes = append(data.Codes[:idx], data.Codes[idx+1:]...)
	data.Codes = purgeExpired(data.Codes, now)
	if now.After(rec.ExpiresAt) {
		_ = s.save(data)
		return "", ErrExpiredPairCode
	}

	// Success: the code is one-shot; the removal above burns it.
	if err := s.save(data); err != nil {
		return "", err
	}
	return rec.Name, nil
}

// NormalizePairCode strips hyphens/spaces/underscores and uppercases. The
// ambiguous glyphs 0/1/I/O are deliberately absent from the generation
// alphabet, so nothing maps onto them — a typed one is simply invalid and the
// caller's alphabet check rejects it.
func NormalizePairCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == '-' || r == ' ' || r == '_' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FormatPairCode renders XXXX-XXXX.
func FormatPairCode(code string) string {
	code = NormalizePairCode(code)
	if len(code) != pairCodeLen {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// HashPairCode returns SHA-256 hex of the normalized code.
func HashPairCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizePairCode(code)))
	return hex.EncodeToString(sum[:])
}

func generatePairCode() (string, error) {
	var out [pairCodeLen]byte
	// Rejection sampling for uniform distribution over alphabet.
	const n = byte(len(pairCodeAlphabet))
	buf := make([]byte, pairCodeLen*2)
	i := 0
	for i < pairCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate pair code: %w", err)
		}
		for _, b := range buf {
			if i >= pairCodeLen {
				break
			}
			// 256 % 32 == 0 so no bias for 32-char alphabet.
			if int(b) >= 256-(256%int(n)) {
				continue
			}
			out[i] = pairCodeAlphabet[int(b)%int(n)]
			i++
		}
	}
	return string(out[:]), nil
}

func purgeExpired(codes []pairCodeRecord, now time.Time) []pairCodeRecord {
	out := codes[:0]
	for _, c := range codes {
		if now.Before(c.ExpiresAt) || now.Equal(c.ExpiresAt) {
			out = append(out, c)
		}
	}
	// Keep capacity if empty.
	if out == nil {
		return []pairCodeRecord{}
	}
	return out
}

func (s *PairCodeStore) load() (pairCodeFile, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return pairCodeFile{}, fmt.Errorf("read pair codes: %w", err)
	}
	var data pairCodeFile
	if len(b) == 0 {
		return pairCodeFile{Codes: []pairCodeRecord{}}, nil
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return pairCodeFile{}, fmt.Errorf("parse pair codes: %w", err)
	}
	if data.Codes == nil {
		data.Codes = []pairCodeRecord{}
	}
	return data, nil
}

func (s *PairCodeStore) save(data pairCodeFile) error {
	if data.Codes == nil {
		data.Codes = []pairCodeRecord{}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode pair codes: %w", err)
	}
	b = append(b, '\n')
	if err := writeFileAtomic(s.path, b); err != nil {
		return fmt.Errorf("write pair codes: %w", err)
	}
	return nil
}
