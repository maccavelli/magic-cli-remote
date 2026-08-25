package codex

import (
	"maps"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// CapabilityID is a closed product capability name. Values intentionally
// name the exact wire prerequisite while later protocol-facing projections
// may group several IDs into one mobile operation.
type CapabilityID string

// Capability IDs consumed by the current P1 provider paths.
const (
	CapabilityThreadList          CapabilityID = "rpc:thread/list"
	CapabilityThreadRead          CapabilityID = "rpc:thread/read"
	CapabilityThreadLoadedList    CapabilityID = "rpc:thread/loaded/list"
	CapabilityThreadFork          CapabilityID = "rpc:thread/fork"
	CapabilityThreadRename        CapabilityID = "rpc:thread/name/set"
	CapabilityThreadMetadata      CapabilityID = "rpc:thread/metadata/update"
	CapabilityThreadArchive       CapabilityID = "rpc:thread/archive"
	CapabilityThreadUnarchive     CapabilityID = "rpc:thread/unarchive"
	CapabilityThreadUnsubscribe   CapabilityID = "rpc:thread/unsubscribe"
	CapabilityThreadDelete        CapabilityID = "rpc:thread/delete"
	CapabilityThreadSectionMove   CapabilityID = "rpc:thread/section/move"
	CapabilityThreadSectionList   CapabilityID = "rpc:threadSection/list"
	CapabilityThreadSectionCreate CapabilityID = "rpc:threadSection/create"
	CapabilityThreadSectionUpdate CapabilityID = "rpc:threadSection/update"
	CapabilityThreadSectionDelete CapabilityID = "rpc:threadSection/delete"
	CapabilityThreadTurnsList     CapabilityID = "rpc:thread/turns/list"
	CapabilityThreadItemsList     CapabilityID = "rpc:thread/items/list"
	CapabilityThreadSearch        CapabilityID = "rpc:thread/search"
	CapabilityProjectList         CapabilityID = "rpc:project/list"
	CapabilityProjectRead         CapabilityID = "rpc:project/read"
	CapabilityProjectCreate       CapabilityID = "rpc:project/create"
	CapabilityProjectImport       CapabilityID = "rpc:project/import"
	CapabilityProjectUpdate       CapabilityID = "rpc:project/update"
	CapabilityProjectMove         CapabilityID = "rpc:project/move"
	CapabilityProjectDelete       CapabilityID = "rpc:project/delete"
	CapabilityThreadSettings      CapabilityID = "rpc:thread/settings/update"
	CapabilityCollaborationModes  CapabilityID = "rpc:collaborationMode/list"
	CapabilityThreadSource        CapabilityID = "field:threadSource"
	CapabilityThreadForkDeferGoal CapabilityID = "field:thread/fork.deferGoalContinuation"
	CapabilityAccountRead         CapabilityID = "rpc:account/read"
	CapabilityAccountRateLimits   CapabilityID = "rpc:account/rateLimits/read"
	CapabilityAccountUsage        CapabilityID = "rpc:account/usage/read"
	CapabilityWorkspaceMessages   CapabilityID = "rpc:account/workspaceMessages/read"
	CapabilityExperimentalFeature CapabilityID = "rpc:experimentalFeature/list"
	CapabilityMCPServerStatus     CapabilityID = "rpc:mcpServerStatus/list"
	CapabilityServerDiagnostics   CapabilityID = "rpc:server/diagnostics"
	CapabilityPermissionProfiles  CapabilityID = "rpc:permissionProfile/list"
	CapabilityGuardianApprove     CapabilityID = "rpc:thread/approveGuardianDeniedAction"
	CapabilityConfigRead          CapabilityID = "rpc:config/read"
	CapabilityConfigRequirements  CapabilityID = "rpc:configRequirements/read"
	CapabilityConfigBatchWrite    CapabilityID = "rpc:config/batchWrite"
)

type threadCreationOperation string

const (
	threadCreate threadCreationOperation = "create"
	threadFork   threadCreationOperation = "fork"
	threadResume threadCreationOperation = "resume"
)

func stampThreadSource(operation threadCreationOperation, params map[string]any) {
	if operation == threadCreate || operation == threadFork {
		params["threadSource"] = "mcremote"
	}
}

// SecurityClass is the most sensitive authority exercised by a capability.
type SecurityClass string

// Supported capability security classes.
const (
	SecurityRead        SecurityClass = "read"
	SecurityWrite       SecurityClass = "write"
	SecurityDestructive SecurityClass = "destructive"
	SecuritySecret      SecurityClass = "secret"
	SecurityExecution   SecurityClass = "execution"
	SecurityRealtime    SecurityClass = "realtime"
)

func (s SecurityClass) valid() bool {
	switch s {
	case SecurityRead, SecurityWrite, SecurityDestructive, SecuritySecret, SecurityExecution, SecurityRealtime:
		return true
	default:
		return false
	}
}

// CapabilityDenial is a sanitized, machine-readable reason.
type CapabilityDenial string

// Sanitized capability denial reasons.
const (
	DenialExperimentalRejected CapabilityDenial = "experimental_rejected"
	DenialMethodNotFound       CapabilityDenial = "method_not_found"
	DenialInvalidParams        CapabilityDenial = "invalid_params"
	DenialManagedPolicy        CapabilityDenial = "managed_policy"
)

// BinaryIdentity identifies evidence. Path is retained only inside the
// provider and is deliberately absent from SanitizedCapabilitySnapshot.
type BinaryIdentity struct {
	Path    string
	Version string
	SHA256  string
}

// CapabilitySnapshot is immutable after construction. capabilityState uses
// copy-on-write when a generation-specific probe disables one member.
type CapabilitySnapshot struct {
	BinaryIdentity       BinaryIdentity
	Generation           int
	ExperimentalAccepted bool
	EvidenceMatched      bool
	Supported            map[CapabilityID]bool
	Denied               map[CapabilityID]CapabilityDenial
	ProbedAt             time.Time
}

// Supports reports whether an ID is enabled in this immutable snapshot.
func (s CapabilitySnapshot) Supports(id CapabilityID) bool { return s.Supported[id] }

// SanitizedCapabilitySnapshot is safe for diagnostics and client projection.
type SanitizedCapabilitySnapshot struct {
	BinaryName           string                            `json:"binary_name,omitempty"`
	Version              string                            `json:"version"`
	SHA256               string                            `json:"sha256"`
	Generation           int                               `json:"generation"`
	ExperimentalAccepted bool                              `json:"experimental_accepted"`
	EvidenceMatched      bool                              `json:"evidence_matched"`
	Supported            []CapabilityID                    `json:"supported"`
	Denied               map[CapabilityID]CapabilityDenial `json:"denied,omitempty"`
	ProbedAt             time.Time                         `json:"probed_at"`
}

// Sanitized returns the bounded client-safe projection without a binary path.
func (s CapabilitySnapshot) Sanitized() SanitizedCapabilitySnapshot {
	supported := make([]CapabilityID, 0, len(s.Supported))
	for id, ok := range s.Supported {
		if ok {
			supported = append(supported, id)
		}
	}
	sort.Slice(supported, func(i, j int) bool { return supported[i] < supported[j] })
	return SanitizedCapabilitySnapshot{
		BinaryName:           filepath.Base(s.BinaryIdentity.Path),
		Version:              s.BinaryIdentity.Version,
		SHA256:               s.BinaryIdentity.SHA256,
		Generation:           s.Generation,
		ExperimentalAccepted: s.ExperimentalAccepted,
		EvidenceMatched:      s.EvidenceMatched,
		Supported:            supported,
		Denied:               maps.Clone(s.Denied),
		ProbedAt:             s.ProbedAt,
	}
}

func buildCapabilitySnapshot(m *ContractManifest, identity BinaryIdentity, generation int, experimental bool, probedAt time.Time) (CapabilitySnapshot, error) {
	if err := m.Validate(); err != nil {
		return CapabilitySnapshot{}, err
	}
	snapshot := CapabilitySnapshot{
		BinaryIdentity:       identity,
		Generation:           generation,
		ExperimentalAccepted: experimental,
		EvidenceMatched:      identity.Version == m.CodexVersion && identity.SHA256 == m.BinarySHA256,
		Supported:            make(map[CapabilityID]bool, len(m.Capabilities)),
		Denied:               make(map[CapabilityID]CapabilityDenial),
		ProbedAt:             probedAt,
	}
	for _, capability := range m.Capabilities {
		if capability.Stability == StabilityExperimental && !experimental {
			snapshot.Denied[capability.ID] = DenialExperimentalRejected
			continue
		}
		snapshot.Supported[capability.ID] = true
	}
	return snapshot, nil
}

type capabilityState struct {
	mu       sync.RWMutex
	snapshot CapabilitySnapshot
}

func newCapabilityState(snapshot CapabilitySnapshot) *capabilityState {
	return &capabilityState{snapshot: cloneCapabilitySnapshot(snapshot)}
}

func (s *capabilityState) Snapshot() CapabilitySnapshot {
	if s == nil {
		return CapabilitySnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCapabilitySnapshot(s.snapshot)
}

func (s *capabilityState) Supports(id CapabilityID) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot.Supports(id)
}

func (s *capabilityState) Disable(id CapabilityID, reason CapabilityDenial) {
	if s == nil {
		return
	}
	s.mu.Lock()
	next := cloneCapabilitySnapshot(s.snapshot)
	delete(next.Supported, id)
	next.Denied[id] = reason
	s.snapshot = next
	s.mu.Unlock()
}

func cloneCapabilitySnapshot(in CapabilitySnapshot) CapabilitySnapshot {
	out := in
	out.Supported = maps.Clone(in.Supported)
	out.Denied = maps.Clone(in.Denied)
	return out
}

// SurfaceCapabilityIDs returns the installed manifest's stable and
// experimental product capability IDs in bytewise order. It is the source for
// the v2 phone advertisement; errors fail closed to empty lists.
func SurfaceCapabilityIDs() (stable, experimental []string) {
	m, err := loadEmbeddedContractManifest()
	if err != nil {
		return nil, nil
	}
	for _, capability := range m.Capabilities {
		if capability.Stability == StabilityExperimental {
			experimental = append(experimental, string(capability.ID))
		} else {
			stable = append(stable, string(capability.ID))
		}
	}
	sort.Strings(stable)
	sort.Strings(experimental)
	return stable, experimental
}
