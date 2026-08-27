package providerauth

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// store owns the on-disk layout under <DataDir>/provider-auth (MADR 0074 D23,
// P17 step 3). Every path is derived from the data directory and the fixed
// provider id; no caller supplies a path fragment, so traversal is structurally
// impossible rather than filtered.
//
//	<DataDir>/provider-auth/                          0700
//	  <provider>/                                     0700
//	    manifest.json                                 0600, atomic replacement
//	    transaction.lock                              advisory coordinator lock
//	    generations/<generation-uuid>.auth            0600, immutable after sync
//	    pending/<transaction-uuid>/home/              0700, provider-native layout
type store struct {
	root     string
	provider string
}

func newStore(dataDir, provider string) (*store, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("provider auth: empty data directory")
	}
	if provider == "" || provider != filepath.Base(provider) || provider == "." || provider == ".." {
		return nil, fmt.Errorf("provider auth: invalid provider id")
	}
	s := &store{root: filepath.Join(dataDir, "provider-auth"), provider: provider}
	for _, d := range []string{s.root, s.providerDir(), s.generationsDir(), s.pendingDir()} {
		// EnsurePrivateDir rather than MkdirAll+Chmod: it creates the
		// directory AND converges its access to owner-only, per platform —
		// mode 0700 on Unix, an owner+SYSTEM DACL on Windows, where a mode
		// carries no access control at all. The credentials retained here are
		// exactly what that protects (MADR 0116 D22/D26).
		if err := appdirs.EnsurePrivateDir(d); err != nil {
			return nil, fmt.Errorf("provider auth: create store: %w", err)
		}
	}
	return s, nil
}

func (s *store) providerDir() string    { return filepath.Join(s.root, s.provider) }
func (s *store) manifestPath() string   { return filepath.Join(s.providerDir(), "manifest.json") }
func (s *store) lockPath() string       { return filepath.Join(s.providerDir(), "transaction") }
func (s *store) generationsDir() string { return filepath.Join(s.providerDir(), "generations") }
func (s *store) pendingDir() string     { return filepath.Join(s.providerDir(), "pending") }

func (s *store) generationPath(id string) string {
	return filepath.Join(s.generationsDir(), id+".auth")
}

func (s *store) txnDir(id string) string  { return filepath.Join(s.pendingDir(), id) }
func (s *store) txnHome(id string) string { return filepath.Join(s.txnDir(id), "home") }

// createPendingHome makes the isolated directory a provider child runs against.
// It is created empty and nothing is ever copied into it: Codex revokes any
// credential it finds at login start, so a seeded home would invalidate the
// live grant while leaving LIVE byte-identical (MADR 0074 D22/F14).
func (s *store) createPendingHome(txnID string) (string, error) {
	home := s.txnHome(txnID)
	// The agent CLI writes its credential in here, and ValidateCandidate then
	// requires that file to be owner-only. On Windows the file inherits this
	// directory's DACL, so making the directory private is what makes the
	// candidate valid — MkdirAll's 0o700 would be silently ignored there
	// (MADR 0116 D26).
	if err := appdirs.EnsurePrivateDir(home); err != nil {
		return "", fmt.Errorf("provider auth: create pending home: %w", err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return "", fmt.Errorf("provider auth: inspect pending home: %w", err)
	}
	if len(entries) != 0 {
		return "", fmt.Errorf("provider auth: pending home is not empty")
	}
	return home, nil
}

func (s *store) removeTxn(txnID string) error {
	if txnID == "" {
		return nil
	}
	return os.RemoveAll(s.txnDir(txnID))
}

// removeStalePending deletes pending directories that no live transaction
// claims. Only the labelled transaction's data survives (P17 step 9).
func (s *store) removeStalePending(keep string) error {
	entries, err := os.ReadDir(s.pendingDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.pendingDir(), e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// readCandidate reads and structurally checks a staged candidate before any
// provider parsing. Symlinks, irregular files, oversized payloads, and
// group/world-accessible modes are refused (MADR 0074 D25).
func (s *store) readCandidate(path string, maxBytes int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: no credential was written", ErrInvalidCandidate)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidCandidate, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: candidate is a symlink", ErrInvalidCandidate)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: candidate is not a regular file", ErrInvalidCandidate)
	}
	// Owner-only is asked as a property, not as a mode test: files report 0666
	// on Windows whatever their ACL says, so the inline Perm()&0o077 check this
	// replaces rejected every candidate there (MADR 0116 F23a/D22).
	ownerOnly, err := appdirs.FileIsOwnerOnly(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCandidate, err)
	}
	if !ownerOnly {
		return nil, fmt.Errorf("%w: candidate is not owner-only", ErrInvalidCandidate)
	}
	if maxBytes > 0 && fi.Size() > maxBytes {
		return nil, fmt.Errorf("%w: candidate exceeds %d bytes", ErrInvalidCandidate, maxBytes)
	}
	b, err := os.ReadFile(path) //nolint:gosec // path derived from the store layout
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCandidate, err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: candidate is empty", ErrInvalidCandidate)
	}
	return b, nil
}

// writeGeneration copies exact candidate bytes into a new immutable payload and
// syncs it. Publication always reads from here, never from the child-writable
// pending home (MADR 0074 D25/P17 step 7).
func (s *store) writeGeneration(data []byte) (string, error) {
	id := uuid.NewString()
	path := s.generationPath(id)
	if err := fsutil.WriteFileAtomic(path, data, fsutil.AtomicOptions{
		Perm: 0o600, SyncFile: true, SyncDir: true,
	}); err != nil {
		return "", fmt.Errorf("provider auth: write generation: %w", err)
	}
	return id, nil
}

func (s *store) readGeneration(id string) ([]byte, error) {
	return os.ReadFile(s.generationPath(id)) //nolint:gosec // derived path
}

func (s *store) removeGeneration(id string) error {
	err := os.Remove(s.generationPath(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// pruneGenerations deletes every payload the manifest no longer labels. It runs
// only after the new manifest is durable, so a crash never loses a payload the
// journal still references (D23/P17 step 8).
func (s *store) pruneGenerations(m *Manifest) error {
	keep := make(map[string]bool, len(m.Generations))
	for _, g := range m.Generations {
		keep[g.ID] = true
	}
	entries, err := os.ReadDir(s.generationsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".auth" {
			continue
		}
		id := name[:len(name)-len(".auth")]
		if keep[id] {
			continue
		}
		if err := os.Remove(filepath.Join(s.generationsDir(), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// liveFingerprint reports the LIVE file's fingerprint, distinguishing absence
// from any digest. Structural checks run before the hash so an unreadable or
// irregular LIVE is never mistaken for a specific value (P17 step 9).
func liveFingerprint(path string) (Fingerprint, []byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FingerprintAbsent, nil, nil
		}
		return "", nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return "", nil, fmt.Errorf("provider auth: live credential is not a regular file")
	}
	b, err := os.ReadFile(path) //nolint:gosec // provider-native path from the adapter
	if err != nil {
		return "", nil, err
	}
	return FingerprintOf(b), b, nil
}

// removeIfExists unlinks path, tolerating an already-absent file.
func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
