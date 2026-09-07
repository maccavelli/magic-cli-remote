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
	// AgentSessionAliases retain prior provider ids that reconcile to this
	// managed record. Old records omit the field and remain valid.
	AgentSessionAliases []string `json:"agent_session_aliases,omitempty"`
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

// ResolveAgentSession finds the one managed record that already represents a
// provider-native thread. Exact current ids win over historical aliases, so a
// reconnect cannot create a second managed row for one loaded thread.
func (s *Store) ResolveAgentSession(providerID provider.ID, agentSessionID string) (Record, bool) {
	if agentSessionID == "" {
		return Record{}, false
	}
	records, _, err := s.List()
	if err != nil {
		return Record{}, false
	}
	for _, rec := range records {
		if rec.Provider == providerID && rec.AgentSessionID == agentSessionID {
			return rec, true
		}
	}
	for _, rec := range records {
		if rec.Provider != providerID {
			continue
		}
		for _, alias := range rec.AgentSessionAliases {
			if alias == agentSessionID {
				return rec, true
			}
		}
	}
	return Record{}, false
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
// Same retention policy as the live ring: bounded by bytes, evicted lowest
// content class first (MADR 0138 Phase 2).
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
//
// The caller's slice is already classed and budgeted by the live ring, so this
// writes it whole. The independent guard below exists for the paths that do not
// come from a live ring — a caller passing arbitrary history — and applies the
// same class rule rather than a count: re-trimming by count here would undo the
// retention the ring just enforced, which is what made two of the operator's
// sessions lose every user_message (MADR 0138 F1).
//
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
	// Bound the file so disk cannot grow without bound, by the same policy the
	// live ring uses.
	events = boundHistoryByClass(events, historyFileBudgetBytes)
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
		// A session with no transcript yet is the normal cold case; warning on
		// it would train the reader to ignore this line. Anything else lost a
		// transcript that exists (MADR 0141 F3).
		if !os.IsNotExist(err) {
			s.log.Warn("session history unreadable; transcript dropped",
				slog.String("session_id", id),
				slog.String("path", s.historyPath(id)),
				slog.String("err", err.Error()),
			)
		}
		return []event.Event{}
	}
	var hf historyFile
	if err := json.Unmarshal(b, &hf); err != nil {
		s.log.Warn("session history did not parse; transcript dropped",
			slog.String("session_id", id),
			slog.Int("bytes", len(b)),
			slog.String("err", err.Error()),
			slog.String("hint", "the file must be {\"events\":[...]}; a bare JSON array "+
				"decodes as no events"),
		)
		return []event.Event{}
	}
	if hf.Events == nil {
		s.log.Warn("session history has no events key; transcript dropped",
			slog.String("session_id", id),
			slog.Int("bytes", len(b)),
		)
		return []event.Event{}
	}
	return boundHistoryByClass(hf.Events, historyFileBudgetBytes)
}

// historyFileBudgetBytes bounds one session's durable transcript. Equal to the
// live per-session budget: a file larger than the ring it seeds would be
// silently truncated on load anyway, and a smaller one would throw away
// retention the ring had already decided to keep.
const historyFileBudgetBytes = historyBudgetBytes

// boundHistoryByClass returns events unchanged when it fits budget, and
// otherwise drops lowest-class-first, oldest-first within a class, until it
// does. The newest event is always kept.
//
// This is the same rule entry.enforceHistoryBudgetLocked applies in memory,
// expressed over a plain slice so the file and the ring cannot disagree about
// what a transcript is.
func boundHistoryByClass(events []event.Event, budget int) []event.Event {
	if events == nil {
		// Always write a concrete slice so the file is never null-JSON.
		return []event.Event{}
	}
	total := 0
	for i := range events {
		total += event.Bytes(&events[i])
	}
	if total <= budget || len(events) < 2 {
		return events
	}

	need := total - budget
	drop := make([]bool, len(events))
	freed := 0
	newest := len(events) - 1
	for cls := event.ClassTelemetry; freed < need; cls++ {
		for i := 0; i < newest && freed < need; i++ {
			if drop[i] || event.ClassOf(events[i].Type) != cls {
				continue
			}
			drop[i] = true
			freed += event.Bytes(&events[i])
		}
		if cls == event.ClassAnchor {
			break
		}
	}

	out := make([]event.Event, 0, len(events))
	for i := range events {
		if !drop[i] {
			out = append(out, events[i])
		}
	}
	return out
}
