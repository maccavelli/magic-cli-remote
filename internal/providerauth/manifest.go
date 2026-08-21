package providerauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// manifestVersion is the only schema this binary may mutate. A newer version is
// refused rather than reinterpreted, so an older daemon cannot damage state it
// only partly understands (MADR 0074 P17 step 4).
const manifestVersion = 1

// Fingerprint is a lowercase SHA-256 hex digest of credential bytes, or the
// distinct value FingerprintAbsent. Absence is its own value and is never the
// hash of empty bytes, because "no credential" and "an empty credential" lead
// to different recovery decisions (MADR 0074 P17 step 9).
type Fingerprint string

// FingerprintAbsent means no credential file was present.
const FingerprintAbsent Fingerprint = "absent"

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FingerprintOf returns the SHA-256 fingerprint of data.
func FingerprintOf(data []byte) Fingerprint {
	sum := sha256.Sum256(data)
	return Fingerprint(hex.EncodeToString(sum[:]))
}

// Valid reports whether f is a well-formed digest or the absent sentinel.
func (f Fingerprint) Valid() bool {
	return f == FingerprintAbsent || hexDigest.MatchString(string(f))
}

// Short is the logging projection: enough to correlate two observations, never
// enough to confirm a guessed credential (MADR 0074 D29).
func (f Fingerprint) Short() string {
	if f == FingerprintAbsent {
		return string(f)
	}
	if len(f) < 8 {
		return "invalid"
	}
	return string(f[:8])
}

// State is the durable transaction state. Transitions are synced before their
// associated side effect so a crash is classified, never guessed (D26).
type State string

const (
	// StateIdle means no transaction is in flight.
	StateIdle State = "idle"
	// StatePending means a candidate is isolated and LIVE is untouched.
	StatePending State = "pending"
	// StateCommitting means publication started; a crash here is classified
	// by comparing LIVE with the candidate and the expected starting value.
	StateCommitting State = "committing"
	// StateRecoveryRequired means durable evidence is ambiguous and only an
	// explicit operator decision may resolve it.
	StateRecoveryRequired State = "recovery_required"
	// StateLoggedOut is the intentional transition to no live credential.
	StateLoggedOut State = "logged_out"
)

func (s State) valid() bool {
	switch s {
	case StateIdle, StatePending, StateCommitting, StateRecoveryRequired, StateLoggedOut:
		return true
	}
	return false
}

// Label names a retained generation. Exactly one CURRENT and at most one
// PREVIOUS are committed; at most one PENDING exists at a time (D23).
type Label string

const (
	// LabelCurrent is the most recently validated committed generation.
	LabelCurrent Label = "current"
	// LabelPrevious is the immediately prior CURRENT, when one exists.
	LabelPrevious Label = "previous"
	// LabelPending is an isolated candidate not yet published.
	LabelPending Label = "pending"
)

// Source records what produced a generation.
type Source string

const (
	// SourceSeed is first-use capture of a pre-existing live credential.
	SourceSeed Source = "seed"
	// SourceDeviceAuth is an mcremote-driven device login.
	SourceDeviceAuth Source = "device_auth"
	// SourceAPIKey is an mcremote-driven API-key login.
	SourceAPIKey Source = "api_key"
	// SourceRefresh is an autonomous provider refresh observed and checkpointed.
	SourceRefresh Source = "refresh"
)

func (s Source) valid() bool {
	switch s {
	case SourceSeed, SourceDeviceAuth, SourceAPIKey, SourceRefresh:
		return true
	}
	return false
}

// Generation is one retained immutable credential copy. It stores labels and
// fingerprints only — never a token field, device code, raw path, or child
// output (MADR 0074 D23/P17 step 4).
type Generation struct {
	ID          string      `json:"id"`
	Label       Label       `json:"label"`
	Fingerprint Fingerprint `json:"fingerprint"`
	Mode        string      `json:"mode"`
	Source      Source      `json:"source"`
	CreatedAt   time.Time   `json:"created_at"`
	ValidatedAt time.Time   `json:"validated_at,omitzero"`
	// Revoked marks a generation whose server-side grant a coordinator action
	// invalidated. It can never be restored or advertised as recoverable,
	// because restoring it is guaranteed to fail (MADR 0074 D24/F14).
	Revoked bool `json:"revoked,omitempty"`
}

// Txn is the durable record of an in-flight transaction.
type Txn struct {
	ID string `json:"id"`
	// ExpectedLive is the LIVE fingerprint observed at Begin. Commit refuses
	// to publish over any other value (D25).
	ExpectedLive Fingerprint `json:"expected_live"`
	Source       Source      `json:"source"`
	CreatedAt    time.Time   `json:"created_at"`
	// ActivationDeadline bounds how long a validated candidate may wait for a
	// busy provider to go idle.
	ActivationDeadline time.Time `json:"activation_deadline,omitzero"`

	// home is the isolated pending directory. It is derived from the store
	// layout, never persisted, so no effective path enters the manifest.
	home string `json:"-"`
}

// Home is the isolated directory the provider child runs against. It starts
// empty of credential material and no live credential is ever copied into it
// (MADR 0074 D22/F14).
func (t *Txn) Home() string { return t.home }

// Manifest is the durable per-provider journal.
type Manifest struct {
	Version     int          `json:"version"`
	Provider    string       `json:"provider"`
	State       State        `json:"state"`
	Generations []Generation `json:"generations"`
	Transaction *Txn         `json:"transaction,omitempty"`
	// LoggedOutExpected is the LIVE fingerprint the logout tombstone expects
	// to remove, so an interrupted logout can be finished or refused (D26).
	LoggedOutExpected Fingerprint `json:"logged_out_expected,omitempty"`
	LoggedOutAt       time.Time   `json:"logged_out_at,omitzero"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

func newManifest(provider string) *Manifest {
	return &Manifest{
		Version:  manifestVersion,
		Provider: provider,
		State:    StateIdle,
	}
}

func (m *Manifest) byLabel(l Label) *Generation {
	for i := range m.Generations {
		if m.Generations[i].Label == l {
			return &m.Generations[i]
		}
	}
	return nil
}

func (m *Manifest) dropLabel(l Label) {
	m.Generations = slices.DeleteFunc(m.Generations, func(g Generation) bool {
		return g.Label == l
	})
}

// validate enforces the invariants a reader must be able to rely on before
// mutating: known version, known state, well-formed fingerprints, and no
// duplicate labels (P17 step 5).
func (m *Manifest) validate() error {
	if m.Version != manifestVersion {
		return fmt.Errorf("provider auth: unsupported manifest version %d", m.Version)
	}
	if !m.State.valid() {
		return fmt.Errorf("provider auth: unknown manifest state %q", m.State)
	}
	seen := map[Label]bool{}
	ids := map[string]bool{}
	for _, g := range m.Generations {
		if g.ID == "" {
			return fmt.Errorf("provider auth: generation with no id")
		}
		if ids[g.ID] {
			return fmt.Errorf("provider auth: duplicate generation id")
		}
		ids[g.ID] = true
		switch g.Label {
		case LabelCurrent, LabelPrevious, LabelPending:
		default:
			return fmt.Errorf("provider auth: unknown generation label %q", g.Label)
		}
		if seen[g.Label] {
			return fmt.Errorf("provider auth: duplicate %s label", g.Label)
		}
		seen[g.Label] = true
		if !g.Fingerprint.Valid() || g.Fingerprint == FingerprintAbsent {
			return fmt.Errorf("provider auth: malformed generation fingerprint")
		}
		if !g.Source.valid() {
			return fmt.Errorf("provider auth: unknown generation source %q", g.Source)
		}
	}
	if m.Transaction != nil && !m.Transaction.ExpectedLive.Valid() {
		return fmt.Errorf("provider auth: malformed transaction fingerprint")
	}
	return nil
}

// loadManifest reads and validates a manifest. Unknown fields are refused so a
// future schema cannot be silently rewritten by this binary.
func loadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path derived from the daemon data dir
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("provider auth: read manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// saveManifest durably replaces the manifest. The caller must already hold the
// provider lock, and must sync this transition before performing the side
// effect it describes (D26).
func saveManifest(path string, m *Manifest) error {
	m.UpdatedAt = time.Now().UTC()
	if err := m.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("provider auth: encode manifest: %w", err)
	}
	return fsutil.WriteFileAtomic(path, b, fsutil.AtomicOptions{
		Perm: 0o600, SyncFile: true, SyncDir: true,
	})
}
