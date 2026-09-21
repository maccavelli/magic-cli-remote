package codex

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestContractManifestExactVersion(t *testing.T) {
	m := mustLoadContractManifest(t)
	if m.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", m.SchemaVersion)
	}
	if m.CodexVersion != "0.155.1" {
		t.Fatalf("codex version = %q, want 0.155.1", m.CodexVersion)
	}
	// The npm shim's digest, not the engine's: PATH resolves codex to a ~341-byte
	// .cmd on this platform, so version equality is the real evidence and this is
	// a tripwire against an accidental re-capture (MADR 0163 D18).
	if m.BinarySHA256 != "c54db6755e710c39703f7c37512f9e35ed41042d8080558d2b84b8d2694323c3" {
		t.Fatalf("binary digest = %q", m.BinarySHA256)
	}

	// 82 notifications on BOTH surfaces, which is what Codex's exporter emits: it
	// never prunes experimental notifications, so the bundles are identical there
	// even though the runtime suppresses 22 of them. The capture mirrors the
	// exporter rather than correcting it, because a manifest that disagrees with
	// its source breaks the drift gate's exact mode; recording the real split needs
	// schema_version 2 (0163 D4/F4, deferred).
	assertSurfaceCounts(t, "stable", m.Stable, 102, 82, 10)
	assertSurfaceCounts(t, "experimental", m.Experimental, 164, 82, 11)
	if err := m.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestContractManifestInventoriesAreBytewiseSorted(t *testing.T) {
	m := mustLoadContractManifest(t)
	for name, methods := range map[string][]WireContract{
		"stable requests":            m.Stable.ClientRequests,
		"stable notifications":       m.Stable.ServerNotifications,
		"stable callbacks":           m.Stable.ServerRequests,
		"experimental requests":      m.Experimental.ClientRequests,
		"experimental notifications": m.Experimental.ServerNotifications,
		"experimental callbacks":     m.Experimental.ServerRequests,
	} {
		names := make([]string, 0, len(methods))
		for _, method := range methods {
			names = append(names, method.Method)
		}
		if !slices.IsSorted(names) {
			t.Errorf("%s are not bytewise sorted", name)
		}
	}
}

func TestContractManifestPlanFixturesAreClassified(t *testing.T) {
	m := mustLoadContractManifest(t)
	seen := make(map[string]ContractFixture, len(m.Fixtures))
	for _, fixture := range m.Fixtures {
		key := string(fixture.Kind) + "\x00" + fixture.Method
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate fixture %s %q", fixture.Kind, fixture.Method)
		}
		seen[key] = fixture
		if fixture.Stability != StabilityStable && fixture.Stability != StabilityExperimental {
			t.Fatalf("fixture %q has stability %q", fixture.Method, fixture.Stability)
		}
		if fixture.ParamsSchema == "" || fixture.ResponseSchema == "" {
			t.Fatalf("fixture %q lacks params/response classification: %+v", fixture.Method, fixture)
		}
		if fixture.Kind != WireNotification && fixture.ResponseSchema == "typed" {
			t.Fatalf("fixture %q has a generic response classification", fixture.Method)
		}
	}

	for _, capability := range m.Capabilities {
		for _, requirement := range capability.Requires {
			if requirement.Kind == WireNotification {
				continue
			}
			key := string(requirement.Kind) + "\x00" + requirement.Method
			if _, ok := seen[key]; !ok {
				t.Errorf("capability %q requirement %s %q has no sanitized fixture", capability.ID, requirement.Kind, requirement.Method)
			}
		}
	}
}

func TestContractSourceWatchDelta(t *testing.T) {
	watch := mustLoadSourceWatchManifest(t)
	if watch.Commit != "be2951ea3" {
		t.Fatalf("source commit = %q", watch.Commit)
	}
	// The source surface is now decompressed from the committed .zst blobs at the
	// same tag as the installed binary, so the two agree exactly and the delta
	// below is empty by OBSERVATION rather than by construction (0163 P4).
	assertSurfaceCounts(t, "source stable", watch.Stable, 102, 82, 10)
	assertSurfaceCounts(t, "source experimental", watch.Experimental, 164, 82, 11)
	// Empty, and that is the correct answer at this pin: the source tree and the
	// installed binary are both rust-v0.155.1, so nothing is source-only.
	//
	// It previously held three mcpServer/event/stream/* entries because the pin's
	// source-watch commit (rust-v0.150.0) was AHEAD of its manifest (0.149.1) —
	// the two baseline files did not describe the same upstream state (0163 F3).
	// A non-empty delta here means the source is ahead of the installed binary and
	// names what is coming; an empty one must be an observation, which is why both
	// sides are now filtered identically and decompressed from the same committed
	// blobs (0163 P4).
	if len(watch.InstalledDelta) != 0 {
		t.Fatalf("source-only delta = %q, want none at a matching source/binary pin", watch.InstalledDelta)
	}
}

func TestContractSchemaParserRejectsMalformedVariants(t *testing.T) {
	base := func(variant string) []byte {
		return []byte(`{"definitions":{"ClientRequest":{"oneOf":[` + variant + `]}}}`)
	}
	cases := map[string][]byte{
		"missing method enum": base(`{"properties":{"params":{"$ref":"#/definitions/P"}}}`),
		"duplicate method":    []byte(`{"definitions":{"ClientRequest":{"oneOf":[{"properties":{"method":{"enum":["x"]},"params":{"$ref":"#/definitions/P"}}},{"properties":{"method":{"enum":["x"]},"params":{"$ref":"#/definitions/P"}}}]},"P":{"type":"object"}}}`),
		"unresolved ref":      base(`{"properties":{"method":{"enum":["x"]},"params":{"$ref":"#/definitions/Missing"}}}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSchemaMethods(raw, "ClientRequest"); err == nil {
				t.Fatal("malformed schema accepted")
			}
		})
	}
}

func TestContractRPCErrorBoundsAndClassifies(t *testing.T) {
	raw := []byte(`{"code":-32601,"message":"` + strings.Repeat("m", maxRPCErrorMessageBytes+50) + `","data":{"capability":"thread/search","detail":"` + strings.Repeat("d", maxRPCErrorDataBytes+50) + `"}}`)
	var rpc rpcErrorBody
	if err := json.Unmarshal(raw, &rpc); err != nil {
		t.Fatal(err)
	}
	if len(rpc.Message) > maxRPCErrorMessageBytes || len(rpc.Data) > maxRPCErrorDataBytes {
		t.Fatalf("unbounded rpc error: message=%d data=%d", len(rpc.Message), len(rpc.Data))
	}
	if !rpc.IsMethodNotFound() {
		t.Fatal("-32601 was not classified as method-not-found")
	}
	if got := rpc.CapabilityName(); got != "thread/search" {
		t.Fatalf("capability = %q", got)
	}
}

func mustLoadContractManifest(t *testing.T) *ContractManifest {
	t.Helper()
	m, err := loadEmbeddedContractManifest()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mustLoadSourceWatchManifest(t *testing.T) *SourceWatchManifest {
	t.Helper()
	m, err := loadEmbeddedSourceWatchManifest()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertSurfaceCounts(t *testing.T, name string, surface ContractSurface, requests, notifications, callbacks int) {
	t.Helper()
	if got := len(surface.ClientRequests); got != requests {
		t.Errorf("%s requests = %d, want %d", name, got, requests)
	}
	if got := len(surface.ServerNotifications); got != notifications {
		t.Errorf("%s notifications = %d, want %d", name, got, notifications)
	}
	if got := len(surface.ServerRequests); got != callbacks {
		t.Errorf("%s callbacks = %d, want %d", name, got, callbacks)
	}
}
