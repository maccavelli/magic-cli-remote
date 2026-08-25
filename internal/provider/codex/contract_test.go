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
	if m.CodexVersion != "0.149.1" {
		t.Fatalf("codex version = %q, want 0.149.1", m.CodexVersion)
	}
	if m.BinarySHA256 != "73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba" {
		t.Fatalf("binary digest = %q", m.BinarySHA256)
	}

	assertSurfaceCounts(t, "stable", m.Stable, 95, 75, 10)
	assertSurfaceCounts(t, "experimental", m.Experimental, 150, 75, 11)
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
	if watch.Commit != "6143217c6730e147f4a1a5a3405d10f580fe9244" {
		t.Fatalf("source commit = %q", watch.Commit)
	}
	assertSurfaceCounts(t, "source stable", watch.Stable, 95, 76, 10)
	assertSurfaceCounts(t, "source experimental", watch.Experimental, 152, 76, 11)
	want := []string{
		"client_request:mcpServer/event/stream/start",
		"client_request:mcpServer/event/stream/stop",
		"server_notification:mcpServer/event/stream/notification",
	}
	if !slices.Equal(watch.InstalledDelta, want) {
		t.Fatalf("source-only delta = %q, want %q", watch.InstalledDelta, want)
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
