package codex

import (
	// embed pins the exact contract evidence into the provider binary.
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stability records whether a wire surface is available without the
// experimentalApi initialization opt-in.
type Stability string

// Supported stability classes.
const (
	StabilityStable       Stability = "stable"
	StabilityExperimental Stability = "experimental"
)

// WireKind identifies which side initiated a JSON-RPC message.
type WireKind string

// Supported wire directions.
const (
	WireClientRequest WireKind = "client_request"
	WireNotification  WireKind = "server_notification"
	WireServerRequest WireKind = "server_request"
)

// ContractClassification is a closed, fail-closed disposition for a captured
// app-server method. Later phases move entries from typed_deferred to their
// final implemented/ignored/rejected state without changing the inventory.
type ContractClassification string

// Supported contract dispositions.
const (
	ClassificationImplemented   ContractClassification = "implemented"
	ClassificationInternal      ContractClassification = "internal"
	ClassificationTypedDeferred ContractClassification = "typed_deferred"
	ClassificationIgnored       ContractClassification = "intentionally_ignored"
	ClassificationRejected      ContractClassification = "intentionally_rejected"
)

func (c ContractClassification) valid() bool {
	switch c {
	case ClassificationImplemented, ClassificationInternal,
		ClassificationTypedDeferred, ClassificationIgnored, ClassificationRejected:
		return true
	default:
		return false
	}
}

// WireContract is the compact, generated projection of one schema union arm.
// Params and Response are schema type classifications, not example payloads.
type WireContract struct {
	Method         string                 `json:"method"`
	Params         string                 `json:"params"`
	Response       string                 `json:"response"`
	Classification ContractClassification `json:"classification"`
}

// ContractSurface is one complete stable or experimental schema inventory.
type ContractSurface struct {
	ClientRequests      []WireContract `json:"client_requests"`
	ServerNotifications []WireContract `json:"server_notifications"`
	ServerRequests      []WireContract `json:"server_requests"`
}

// WireRequirement binds a product capability to exact captured wire names.
type WireRequirement struct {
	Kind   WireKind `json:"kind"`
	Method string   `json:"method"`
}

// CapabilityContract is one independently degradable capability latch.
type CapabilityContract struct {
	ID        CapabilityID      `json:"id"`
	Stability Stability         `json:"stability"`
	Requires  []WireRequirement `json:"requires"`
	Fallback  CapabilityID      `json:"fallback,omitempty"`
	Security  SecurityClass     `json:"security"`
}

// ContractFixture is a sanitized shape fixture for a method used by Plan
// 0109. It records required field names and schema identities without storing
// paths, credentials, prompt content, or live account data.
type ContractFixture struct {
	Kind           WireKind  `json:"kind"`
	Stability      Stability `json:"stability"`
	Method         string    `json:"method"`
	ParamsSchema   string    `json:"params_schema"`
	ResponseSchema string    `json:"response_schema"`
	RequiredParams []string  `json:"required_params,omitempty"`
	RequiredResult []string  `json:"required_result,omitempty"`
}

// ContractManifest is the exact installed-binary evidence used at runtime.
type ContractManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	CodexVersion  string               `json:"codex_version"`
	BinarySHA256  string               `json:"binary_sha256"`
	Stable        ContractSurface      `json:"stable"`
	Experimental  ContractSurface      `json:"experimental"`
	Capabilities  []CapabilityContract `json:"capabilities"`
	Fixtures      []ContractFixture    `json:"fixtures,omitempty"`
}

// SourceWatchManifest is deliberately separate from executable evidence.
type SourceWatchManifest struct {
	SchemaVersion  int             `json:"schema_version"`
	Commit         string          `json:"commit"`
	Stable         ContractSurface `json:"stable"`
	Experimental   ContractSurface `json:"experimental"`
	InstalledDelta []string        `json:"installed_delta"`
}

//go:embed testdata/0.149.1/manifest.json
var embeddedContractManifest []byte

//go:embed testdata/0.149.1/source-watch-manifest.json
var embeddedSourceWatchManifest []byte

//go:embed testdata/0.149.1/fixtures.json
var embeddedContractFixtures []byte

func loadEmbeddedContractManifest() (*ContractManifest, error) {
	var m ContractManifest
	if err := json.Unmarshal(embeddedContractManifest, &m); err != nil {
		return nil, fmt.Errorf("decode embedded codex contract: %w", err)
	}
	if err := json.Unmarshal(embeddedContractFixtures, &m.Fixtures); err != nil {
		return nil, fmt.Errorf("decode embedded codex fixtures: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("validate embedded codex contract: %w", err)
	}
	return &m, nil
}

func loadEmbeddedSourceWatchManifest() (*SourceWatchManifest, error) {
	var m SourceWatchManifest
	if err := json.Unmarshal(embeddedSourceWatchManifest, &m); err != nil {
		return nil, fmt.Errorf("decode codex source-watch contract: %w", err)
	}
	return &m, nil
}

// Validate checks the invariants that make capability negotiation fail closed.
func (m *ContractManifest) Validate() error {
	if m == nil {
		return errors.New("nil manifest")
	}
	if m.SchemaVersion != 1 || strings.TrimSpace(m.CodexVersion) == "" || len(m.BinarySHA256) != 64 {
		return errors.New("invalid manifest identity")
	}
	for name, surface := range map[string]ContractSurface{
		"stable": m.Stable, "experimental": m.Experimental,
	} {
		if err := validateSurface(name, surface); err != nil {
			return err
		}
	}

	capabilities := make(map[CapabilityID]CapabilityContract, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if capability.ID == "" || !capability.Security.valid() ||
			(capability.Stability != StabilityStable && capability.Stability != StabilityExperimental) {
			return fmt.Errorf("invalid capability %q", capability.ID)
		}
		if _, duplicate := capabilities[capability.ID]; duplicate {
			return fmt.Errorf("duplicate capability %q", capability.ID)
		}
		if len(capability.Requires) == 0 {
			return fmt.Errorf("capability %q has no wire requirements", capability.ID)
		}
		for _, requirement := range capability.Requires {
			if !m.hasRequirement(capability.Stability, requirement) {
				return fmt.Errorf("capability %q requires unknown %s %q", capability.ID, requirement.Kind, requirement.Method)
			}
		}
		capabilities[capability.ID] = capability
	}
	for id := range capabilities {
		seen := map[CapabilityID]bool{id: true}
		for fallback := capabilities[id].Fallback; fallback != ""; fallback = capabilities[fallback].Fallback {
			if _, ok := capabilities[fallback]; !ok {
				return fmt.Errorf("capability %q has unknown fallback %q", id, fallback)
			}
			if seen[fallback] {
				return fmt.Errorf("capability fallback cycle at %q", fallback)
			}
			seen[fallback] = true
		}
	}
	return validateFixtures(m)
}

func validateSurface(name string, surface ContractSurface) error {
	for kind, entries := range map[WireKind][]WireContract{
		WireClientRequest: surface.ClientRequests, WireNotification: surface.ServerNotifications,
		WireServerRequest: surface.ServerRequests,
	} {
		last := ""
		for i, entry := range entries {
			if entry.Method == "" || entry.Params == "" || entry.Response == "" || !entry.Classification.valid() {
				return fmt.Errorf("%s %s entry %d is unclassified", name, kind, i)
			}
			if i > 0 && entry.Method <= last {
				return fmt.Errorf("%s %s inventory is unsorted or duplicate at %q", name, kind, entry.Method)
			}
			last = entry.Method
		}
	}
	return nil
}

func validateFixtures(m *ContractManifest) error {
	seen := make(map[string]struct{}, len(m.Fixtures))
	for _, fixture := range m.Fixtures {
		key := string(fixture.Kind) + "\x00" + fixture.Method
		if fixture.Method == "" || fixture.ParamsSchema == "" || fixture.ResponseSchema == "" {
			return fmt.Errorf("incomplete fixture %q", fixture.Method)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate fixture %s %q", fixture.Kind, fixture.Method)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (m *ContractManifest) hasRequirement(stability Stability, requirement WireRequirement) bool {
	surface := m.Stable
	if stability == StabilityExperimental {
		surface = m.Experimental
	}
	var entries []WireContract
	switch requirement.Kind {
	case WireClientRequest:
		entries = surface.ClientRequests
	case WireNotification:
		entries = surface.ServerNotifications
	case WireServerRequest:
		entries = surface.ServerRequests
	default:
		return false
	}
	i := sort.Search(len(entries), func(i int) bool { return entries[i].Method >= requirement.Method })
	return i < len(entries) && entries[i].Method == requirement.Method
}

type schemaDocument struct {
	Definitions map[string]json.RawMessage `json:"definitions"`
}

type schemaUnion struct {
	OneOf []schemaVariant `json:"oneOf"`
}

type schemaVariant struct {
	Properties struct {
		Method struct {
			Enum []string `json:"enum"`
		} `json:"method"`
		Params struct {
			Ref string `json:"$ref"`
		} `json:"params"`
	} `json:"properties"`
}

// parseSchemaMethods extracts and normalizes one generated request or
// notification union. It is used by the live drift test and deliberately
// rejects partial or ambiguous schema arms.
func parseSchemaMethods(raw []byte, definition string) ([]WireContract, error) {
	var document schemaDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	unionRaw, ok := document.Definitions[definition]
	var union schemaUnion
	if ok {
		if err := json.Unmarshal(unionRaw, &union); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(raw, &union); err != nil {
			return nil, err
		}
		if len(union.OneOf) == 0 {
			return nil, fmt.Errorf("missing definition %q", definition)
		}
	}
	out := make([]WireContract, 0, len(union.OneOf))
	seen := make(map[string]struct{}, len(union.OneOf))
	for i, variant := range union.OneOf {
		if len(variant.Properties.Method.Enum) != 1 || variant.Properties.Method.Enum[0] == "" {
			return nil, fmt.Errorf("%s arm %d has no single method enum", definition, i)
		}
		method := variant.Properties.Method.Enum[0]
		if _, duplicate := seen[method]; duplicate {
			return nil, fmt.Errorf("duplicate %s method %q", definition, method)
		}
		seen[method] = struct{}{}
		params := "none"
		if ref := variant.Properties.Params.Ref; ref != "" {
			const prefix = "#/definitions/"
			if !strings.HasPrefix(ref, prefix) {
				return nil, fmt.Errorf("method %q has unsupported params ref %q", method, ref)
			}
			params = strings.TrimPrefix(ref, prefix)
			if _, resolved := document.Definitions[params]; !resolved {
				return nil, fmt.Errorf("method %q has unresolved params ref %q", method, ref)
			}
		}
		out = append(out, WireContract{Method: method, Params: params})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Method < out[j].Method })
	return out, nil
}
