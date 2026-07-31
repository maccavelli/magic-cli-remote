package procutil

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// EngineRecordSchema is the on-disk record version.
const EngineRecordSchema = 1

// EngineRecord is a durable ownership record for one spawned engine process.
type EngineRecord struct {
	Schema      int       `json:"schema"`
	ID          string    `json:"id"`
	Provider    string    `json:"provider,omitempty"`
	InstanceKey string    `json:"instance_key,omitempty"`
	PID         int       `json:"pid"`
	PGID        int       `json:"pgid,omitempty"`
	Owner       string    `json:"owner"`
	StartToken  string    `json:"start_token"`
	DaemonID    string    `json:"daemon_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// EngineLease is returned by RegisterEngine for later RemoveEngine.
type EngineLease struct {
	Dir string
	ID  string
}

var defaultEngineRegistryDir string

// SetDefaultEngineRegistryDir sets the process-wide registry directory used
// when RegisterEngine is called without an explicit dir (provider packages).
func SetDefaultEngineRegistryDir(dir string) {
	defaultEngineRegistryDir = dir
}

// DefaultEngineRegistryDir returns the directory set by SetDefaultEngineRegistryDir.
func DefaultEngineRegistryDir() string { return defaultEngineRegistryDir }

// RegisterEngine writes a 0600 JSON record for a started engine.
// On failure the caller must kill the process group.
func RegisterEngine(dir string, rec EngineRecord) (EngineLease, error) {
	if dir == "" {
		dir = defaultEngineRegistryDir
	}
	if dir == "" {
		// No registry configured (unit tests, incomplete setup). Env markers
		// still apply on Linux; daemon startup always sets the default dir.
		return EngineLease{}, nil
	}
	if rec.ID == "" || rec.PID <= 0 {
		return EngineLease{}, fmt.Errorf("procutil: engine record requires id and pid")
	}
	if rec.Schema == 0 {
		rec.Schema = EngineRecordSchema
	}
	if rec.Owner == "" {
		rec.Owner = OwnerToken()
	}
	if rec.StartToken == "" {
		if st, ok := ProcessStartToken(rec.PID); ok {
			rec.StartToken = st
		}
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		return EngineLease{}, err
	}
	path := recordPath(dir, rec.ID)
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return EngineLease{}, err
	}
	if err := fsutil.WriteFileAtomic(path, b, fsutil.AtomicOptions{Perm: 0o600, SyncFile: true}); err != nil {
		return EngineLease{}, err
	}
	return EngineLease{Dir: dir, ID: rec.ID}, nil
}

// RemoveEngine deletes the record for lease if present.
func RemoveEngine(lease EngineLease) error {
	if lease.Dir == "" || lease.ID == "" {
		return nil
	}
	path := recordPath(lease.Dir, lease.ID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func recordPath(dir, id string) string {
	// Sanitize id for filesystem.
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
	return filepath.Join(dir, safe+".json")
}

// ListEngineRecords reads all records under dir.
func ListEngineRecords(dir string) ([]EngineRecord, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EngineRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rec EngineRecord
		if err := json.Unmarshal(b, &rec); err != nil {
			// Quarantine corrupt records by renaming.
			_ = os.Rename(filepath.Join(dir, e.Name()), filepath.Join(dir, e.Name()+".corrupt"))
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// ListEngineRecordsUnderState scans StateDir/instances/*/engines.
func ListEngineRecordsUnderState(stateDir string) ([]EngineRecord, error) {
	if stateDir == "" {
		return nil, nil
	}
	root := filepath.Join(stateDir, "instances")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EngineRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recs, err := ListEngineRecords(filepath.Join(root, e.Name(), "engines"))
		if err != nil {
			continue
		}
		out = append(out, recs...)
	}
	return out, nil
}

// RecordLive reports whether rec still refers to a live process incarnation.
func RecordLive(rec EngineRecord) bool {
	return recordMatchesLiveProcess(rec)
}

// recordMatchesLiveProcess verifies PID + start token.
func recordMatchesLiveProcess(rec EngineRecord) bool {
	if rec.PID <= 0 {
		return false
	}
	st, ok := ProcessStartToken(rec.PID)
	if !ok {
		return false
	}
	if rec.StartToken != "" && st != rec.StartToken {
		return false
	}
	return true
}

// ListEngineRegistryDirs returns StateDir/instances/*/engines directories.
func ListEngineRegistryDirs(stateDir string) ([]string, error) {
	if stateDir == "" {
		return nil, nil
	}
	root := filepath.Join(stateDir, "instances")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, filepath.Join(root, e.Name(), "engines"))
	}
	return out, nil
}

// FindRegistryOrphans returns registry records whose owner is dead but whose
// process still matches the recorded start token (true orphans).
func FindRegistryOrphans(dir string) []Orphan {
	recs, err := ListEngineRecords(dir)
	if err != nil {
		return nil
	}
	var out []Orphan
	for _, rec := range recs {
		if rec.Owner != "" && OwnerAlive(rec.Owner) {
			continue
		}
		if !recordMatchesLiveProcess(rec) {
			// Stale record: process gone or pid reused — drop record.
			_ = RemoveEngine(EngineLease{Dir: dir, ID: rec.ID})
			continue
		}
		out = append(out, Orphan{PID: rec.PID, EngineID: rec.ID, Owner: rec.Owner})
	}
	return out
}

// ReapRegistryOrphans stops orphan engines recorded under dir.
func ReapRegistryOrphans(log *slog.Logger, dir string) int {
	if log == nil {
		log = slog.Default()
	}
	if dir == "" {
		return 0
	}
	orphans := FindRegistryOrphans(dir)
	reaped := 0
	for _, o := range orphans {
		proc, err := os.FindProcess(o.PID)
		if err != nil {
			continue
		}
		graceful := TerminateProcessGroup(proc, nil, orphanStopTimeout)
		_ = RemoveEngine(EngineLease{Dir: dir, ID: o.EngineID})
		reaped++
		log.Info("reaped orphaned agent engine (registry)",
			slog.Int("pid", o.PID),
			slog.String("engine_id", o.EngineID),
			slog.String("dead_owner", o.Owner),
			slog.Bool("graceful", graceful),
		)
	}
	return reaped
}
