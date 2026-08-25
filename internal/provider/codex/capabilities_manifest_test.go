package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCapabilityManifestRejectsDuplicateUnknownAndFallbackCycle(t *testing.T) {
	base := mustLoadContractManifest(t)

	t.Run("duplicate capability", func(t *testing.T) {
		m := cloneContractManifest(t, base)
		m.Capabilities = append(m.Capabilities, m.Capabilities[0])
		if err := m.Validate(); err == nil {
			t.Fatal("duplicate capability accepted")
		}
	})

	t.Run("unknown classification", func(t *testing.T) {
		m := cloneContractManifest(t, base)
		m.Stable.ClientRequests[0].Classification = ContractClassification("surprise")
		if err := m.Validate(); err == nil {
			t.Fatal("unknown classification accepted")
		}
	})

	t.Run("fallback cycle", func(t *testing.T) {
		m := cloneContractManifest(t, base)
		m.Capabilities[0].Fallback = m.Capabilities[1].ID
		m.Capabilities[1].Fallback = m.Capabilities[0].ID
		if err := m.Validate(); err == nil {
			t.Fatal("fallback cycle accepted")
		}
	})

	t.Run("unknown requirement", func(t *testing.T) {
		m := cloneContractManifest(t, base)
		m.Capabilities[0].Requires[0].Method = "not/installed"
		if err := m.Validate(); err == nil {
			t.Fatal("unknown wire requirement accepted")
		}
	})
}

func TestCapabilityManifestRejectsMissingStableOrExperimentalMethod(t *testing.T) {
	base := mustLoadContractManifest(t)
	for name, mutate := range map[string]func(*ContractManifest){
		"stable": func(m *ContractManifest) {
			m.Stable.ClientRequests = removeWireMethod(m.Stable.ClientRequests, "thread/list")
		},
		"experimental": func(m *ContractManifest) {
			m.Experimental.ClientRequests = removeWireMethod(m.Experimental.ClientRequests, "thread/search")
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := cloneContractManifest(t, base)
			mutate(m)
			if err := m.Validate(); err == nil {
				t.Fatalf("missing %s method accepted", name)
			}
		})
	}
}

func TestCapabilitySnapshotExperimentalIsolation(t *testing.T) {
	m := mustLoadContractManifest(t)
	identity := BinaryIdentity{
		Path:    "/private/codex",
		Version: m.CodexVersion,
		SHA256:  m.BinarySHA256,
	}
	snapshot, err := buildCapabilitySnapshot(m, identity, 7, true, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.EvidenceMatched || !snapshot.Supports(CapabilityThreadList) || !snapshot.Supports(CapabilityThreadSearch) {
		t.Fatalf("unexpected initial snapshot: %+v", snapshot.Sanitized())
	}

	state := newCapabilityState(snapshot)
	state.Disable(CapabilityThreadSearch, DenialMethodNotFound)
	got := state.Snapshot()
	if got.Supports(CapabilityThreadSearch) {
		t.Fatal("disabled capability remained enabled")
	}
	if !got.Supports(CapabilityThreadList) || !got.Supports(CapabilityCollaborationModes) {
		t.Fatal("disabling thread search disabled an unrelated capability")
	}
	if snapshot.Supports(CapabilityThreadSearch) == false {
		t.Fatal("immutable source snapshot was mutated")
	}
}

func TestCapabilitySnapshotStableWithoutExperimental(t *testing.T) {
	m := mustLoadContractManifest(t)
	snapshot, err := buildCapabilitySnapshot(m, BinaryIdentity{Version: m.CodexVersion, SHA256: m.BinarySHA256}, 1, false, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Supports(CapabilityThreadList) {
		t.Fatal("stable capability disabled when experimental initialization was rejected")
	}
	if snapshot.Supports(CapabilityThreadSearch) || snapshot.Supports(CapabilityCollaborationModes) {
		t.Fatal("experimental capability enabled after experimental rejection")
	}
}

func TestCapabilitySnapshotGenerationAndIdentity(t *testing.T) {
	m := mustLoadContractManifest(t)
	first, err := buildCapabilitySnapshot(m, BinaryIdentity{Path: "/a", Version: m.CodexVersion, SHA256: m.BinarySHA256}, 1, true, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCapabilitySnapshot(m, BinaryIdentity{Path: "/b", Version: "0.150.0", SHA256: strings.Repeat("a", 64)}, 2, true, time.Unix(200, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation == second.Generation || second.EvidenceMatched {
		t.Fatalf("replacement identity was not reflected: first=%+v second=%+v", first.Sanitized(), second.Sanitized())
	}
	encoded, err := json.Marshal(second.Sanitized())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"binary_name":"/b"`) || strings.Contains(string(encoded), `"path":`) {
		t.Fatalf("sanitized diagnostics leaked binary path: %s", encoded)
	}
}

func TestInitializeCapabilityProfile(t *testing.T) {
	params := initializeParamsWithProfile(true, MCPClientExtensionProfile{
		OpenAIForm:        true,
		StandardFormInput: true,
		MCPAppUI:          true,
	})
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"experimentalApi":true`,
		`"requestAttestation":false`,
		`"optOutNotificationMethods":[]`,
		`"openai/form":{}`,
		`"openai/standard-form-input":{}`,
		`"io.modelcontextprotocol/ui":{"mimeTypes":["text/html;profile=mcp-app"]}`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("initialize params missing %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "mcpServerOpenaiFormElicitation") {
		t.Fatalf("typed extension profile leaked legacy form flag: %s", raw)
	}

	legacy := initializeParamsWithLegacyForm(true)
	legacyRaw, _ := json.Marshal(legacy)
	if !strings.Contains(string(legacyRaw), `"mcpServerOpenaiFormElicitation":true`) || strings.Contains(string(legacyRaw), `"extensions"`) {
		t.Fatalf("legacy fallback = %s", legacyRaw)
	}
}

func TestThreadSourceStampedOnlyForCreateAndFork(t *testing.T) {
	for _, operation := range []threadCreationOperation{threadCreate, threadFork} {
		params := map[string]any{}
		stampThreadSource(operation, params)
		if params["threadSource"] != "mcremote" {
			t.Errorf("%s thread source = %#v", operation, params["threadSource"])
		}
	}
	params := map[string]any{}
	stampThreadSource(threadResume, params)
	if _, ok := params["threadSource"]; ok {
		t.Fatalf("resume received threadSource: %#v", params)
	}
}

func cloneContractManifest(t *testing.T, in *ContractManifest) *ContractManifest {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ContractManifest
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func removeWireMethod(entries []WireContract, method string) []WireContract {
	out := entries[:0]
	for _, entry := range entries {
		if entry.Method != method {
			out = append(out, entry)
		}
	}
	return out
}
