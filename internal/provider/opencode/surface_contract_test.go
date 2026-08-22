package opencode_test

// Default-tag contract tests for the committed OpenCode 1.18.21 surface corpus
// (MADR 0112 D3/D12/A11, PLAN P0).
//
// These tests deliberately touch no production statement in package opencode:
// the corpus is evidence, and P0 records the provider's coverage baseline
// before any production edit. Everything here reads testdata and repository
// sources only. The live counterparts that talk to a real engine live in
// live_http_test.go behind the live_opencode build tag.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const surfaceDir = "testdata/surface-1.18.21"

// Canonical 1.18.21 primary-API sizes. Identical at 1.18.7; see MADR 0112 D3.
const (
	wantPaths      = 162
	wantOperations = 188
	wantSchemas    = 472
	wantEventTypes = 89
)

func readLines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(surfaceDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(surfaceDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

// The four canonical sets are the contract. Counts alone are not enough: a set
// that gained one entry and lost another has the same size, so sortedness and
// uniqueness are asserted too.
func TestSurfaceContractCanonicalSets(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{
		{"openapi-paths.txt", wantPaths},
		{"openapi-operations.txt", wantOperations},
		{"openapi-schemas.txt", wantSchemas},
		{"event-types.txt", wantEventTypes},
	} {
		t.Run(tc.file, func(t *testing.T) {
			got := readLines(t, tc.file)
			if len(got) != tc.want {
				t.Errorf("%s has %d entries, want %d", tc.file, len(got), tc.want)
			}
			if !sort.StringsAreSorted(got) {
				t.Errorf("%s is not sorted; the comparison depends on a stable order", tc.file)
			}
			seen := make(map[string]bool, len(got))
			for _, e := range got {
				if seen[e] {
					t.Errorf("%s repeats %q; a set may not contain duplicates", tc.file, e)
				}
				seen[e] = true
			}
		})
	}
}

// Every path referenced by an operation must exist in the path set, and every
// path must carry at least one operation. This is what makes the two files a
// consistent view of one document rather than two independent lists.
func TestSurfaceContractOperationsAgreeWithPaths(t *testing.T) {
	paths := make(map[string]bool)
	for _, p := range readLines(t, "openapi-paths.txt") {
		paths[p] = true
	}
	covered := make(map[string]bool)
	for _, op := range readLines(t, "openapi-operations.txt") {
		method, path, ok := strings.Cut(op, " ")
		if !ok {
			t.Fatalf("operation %q is not %q", op, "METHOD /path")
		}
		switch method {
		case "GET", "PUT", "POST", "DELETE", "PATCH", "OPTIONS", "HEAD", "TRACE":
		default:
			t.Errorf("operation %q has unexpected method %q", op, method)
		}
		if !paths[path] {
			t.Errorf("operation %q references a path absent from the path set", op)
		}
		covered[path] = true
	}
	for p := range paths {
		if !covered[p] {
			t.Errorf("path %q carries no operation", p)
		}
	}
}

// The Event discriminators the dialect decodes must be present. These are
// asserted explicitly rather than inferred from the count (MADR 0112
// Confirmation).
func TestSurfaceContractRequiredEventTypes(t *testing.T) {
	have := make(map[string]bool)
	for _, e := range readLines(t, "event-types.txt") {
		have[e] = true
	}
	required := []string{
		"message.updated", "message.part.updated", "message.part.delta",
		"message.removed", "message.part.removed",
		"session.created", "session.deleted", "session.updated",
		"session.idle", "session.error", "session.status", "session.compacted",
		"session.diff", "permission.asked", "permission.replied",
		"permission.v2.asked", "permission.v2.replied",
		"question.asked", "question.replied", "question.rejected",
		"question.v2.asked", "question.v2.replied", "question.v2.rejected",
		"todo.updated", "command.executed", "file.edited",
		"mcp.tools.changed", "lsp.updated", "server.connected",
		"server.instance.disposed",
	}
	for _, r := range required {
		if !have[r] {
			t.Errorf("event discriminator %q is missing from the 1.18.21 set", r)
		}
	}
}

// The 1.18.21 Event union has no formatter, skill or config update event, so
// PLAN P7 refreshes those explicitly instead of reacting to a canonical event.
// If a future release adds one, this test fails and that decision is revisited.
func TestSurfaceContractNoFormatterOrSkillEvent(t *testing.T) {
	for _, e := range readLines(t, "event-types.txt") {
		low := strings.ToLower(e)
		if strings.HasPrefix(low, "formatter.") || strings.HasPrefix(low, "skill.") {
			t.Errorf("1.18.21 unexpectedly exposes %q; PLAN P7 assumes no such event", e)
		}
	}
}

func TestSurfaceContractManifest(t *testing.T) {
	var m struct {
		OpenCodeVersion string `json:"opencode_version"`
		Binary          struct {
			Command string `json:"command"`
			SHA256  string `json:"sha256"`
			GOOS    string `json:"goos"`
			GOARCH  string `json:"goarch"`
		} `json:"binary"`
		SourceBoundary struct {
			BehaviorCommit       string `json:"behavior_commit"`
			PackageVersionCommit string `json:"package_version_commit"`
			LocalTagPresent      bool   `json:"local_v1_18_21_tag_present"`
		} `json:"source_boundary"`
		ComparisonBaseline struct {
			Previous   string `json:"previous_assessed_release"`
			Paths      int    `json:"paths"`
			Operations int    `json:"operations"`
			Schemas    int    `json:"schemas"`
			EventTypes int    `json:"event_types"`
		} `json:"comparison_baseline"`
		ProbeIsolation struct {
			Unset           []string `json:"unset"`
			RedirectedRoots []string `json:"redirected_roots"`
		} `json:"probe_isolation"`
	}
	readJSON(t, "manifest.json", &m)

	if m.OpenCodeVersion != "1.18.21" {
		t.Errorf("manifest version = %q, want 1.18.21", m.OpenCodeVersion)
	}
	if m.Binary.Command != "opencode" {
		t.Errorf("binary command = %q; the user-specific absolute path must not be recorded", m.Binary.Command)
	}
	if len(m.Binary.SHA256) != 64 {
		t.Errorf("binary sha256 = %q, want 64 hex characters", m.Binary.SHA256)
	}
	if m.Binary.GOOS != "darwin" || m.Binary.GOARCH != "arm64" {
		t.Errorf("binary platform = %s/%s, want darwin/arm64", m.Binary.GOOS, m.Binary.GOARCH)
	}
	if m.SourceBoundary.BehaviorCommit != "57fa34f23599f65dd1027f9caac31e6c576ce644" {
		t.Errorf("behavior commit = %q", m.SourceBoundary.BehaviorCommit)
	}
	if m.SourceBoundary.PackageVersionCommit != "ad0bb6d9a3e779def694adc093a811e86a529df0" {
		t.Errorf("package-version commit = %q", m.SourceBoundary.PackageVersionCommit)
	}
	if m.SourceBoundary.LocalTagPresent {
		t.Error("the clone must not be claimed to carry a v1.18.21 tag")
	}
	if m.ComparisonBaseline.Previous != "1.18.7" {
		t.Errorf("comparison baseline = %q, want 1.18.7", m.ComparisonBaseline.Previous)
	}
	if m.ComparisonBaseline.Paths != wantPaths || m.ComparisonBaseline.Operations != wantOperations ||
		m.ComparisonBaseline.Schemas != wantSchemas || m.ComparisonBaseline.EventTypes != wantEventTypes {
		t.Errorf("manifest counts %d/%d/%d/%d disagree with the committed sets",
			m.ComparisonBaseline.Paths, m.ComparisonBaseline.Operations,
			m.ComparisonBaseline.Schemas, m.ComparisonBaseline.EventTypes)
	}
	// Both isolation requirements are load-bearing: without them the corpus
	// measures the host rather than the release (PLAN P0 step 3 and step 4).
	if !contains(m.ProbeIsolation.Unset, "BASH_ENV") {
		t.Error("manifest must record that BASH_ENV was unset for the shell fixture")
	}
	if !contains(m.ProbeIsolation.RedirectedRoots, "HOME") {
		t.Error("manifest must record that HOME was redirected; --pure plus XDG is not sufficient")
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// The shell fixture is what PLAN P10 maps. Its tool id is "bash": 1.18.21 has
// no distinct shell tool, which is why no new tool-kind entry is needed.
func TestSurfaceContractShellFixture(t *testing.T) {
	var s struct {
		BlockingResponse struct {
			Info struct {
				Role   string `json:"role"`
				Cost   float64
				Tokens struct {
					Input     int `json:"input"`
					Output    int `json:"output"`
					Reasoning int `json:"reasoning"`
					Cache     struct {
						Read  int `json:"read"`
						Write int `json:"write"`
					} `json:"cache"`
				} `json:"tokens"`
			} `json:"info"`
			Parts []struct {
				Type   string `json:"type"`
				Tool   string `json:"tool"`
				Status string `json:"status"`
				Output string `json:"output"`
			} `json:"parts"`
		} `json:"blocking_response"`
		SSESequence []struct {
			Type      string `json:"type"`
			Part      string `json:"part"`
			Tool      string `json:"tool"`
			Status    string `json:"status"`
			Role      string `json:"role"`
			Synthetic bool   `json:"synthetic"`
		} `json:"sse_sequence"`
	}
	readJSON(t, "shell-events.json", &s)

	if s.BlockingResponse.Info.Role != "assistant" {
		t.Errorf("shell response role = %q, want assistant", s.BlockingResponse.Info.Role)
	}
	if s.BlockingResponse.Info.Cost != 0 {
		t.Errorf("shell cost = %v, want 0: no model turn occurs", s.BlockingResponse.Info.Cost)
	}
	tk := s.BlockingResponse.Info.Tokens
	if tk.Input != 0 || tk.Output != 0 || tk.Reasoning != 0 || tk.Cache.Read != 0 || tk.Cache.Write != 0 {
		t.Errorf("shell token buckets = %+v, want all zero", tk)
	}
	if len(s.BlockingResponse.Parts) != 1 {
		t.Fatalf("shell response has %d parts, want exactly 1", len(s.BlockingResponse.Parts))
	}
	p := s.BlockingResponse.Parts[0]
	if p.Type != "tool" || p.Tool != "bash" || p.Status != "completed" {
		t.Errorf("shell part = %+v, want a completed bash tool part", p)
	}
	if p.Output != "mcremote-shell-probe" {
		t.Errorf("shell output = %q, want the exact probe output; a captured host rc line means "+
			"the fixture was taken without unsetting BASH_ENV", p.Output)
	}

	// The canonical stream order P10 relies on.
	var order []string
	for _, e := range s.SSESequence {
		order = append(order, e.Type)
	}
	joined := strings.Join(order, ",")
	for _, want := range []string{"session.status", "message.updated", "message.part.updated", "session.idle"} {
		if !strings.Contains(joined, want) {
			t.Errorf("shell SSE sequence %q is missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "message.part.delta") {
		t.Error("the shell path emits full snapshots, not deltas; a delta here changes the P4 mapping")
	}
	var sawSyntheticUserText, sawCompletedBash bool
	for _, e := range s.SSESequence {
		if e.Part == "text" && e.Synthetic {
			sawSyntheticUserText = true
		}
		if e.Part == "tool" && e.Tool == "bash" && e.Status == "completed" {
			sawCompletedBash = true
		}
	}
	if !sawSyntheticUserText {
		t.Error("the synthetic user text part must be present and marked synthetic")
	}
	if !sawCompletedBash {
		t.Error("the completed bash tool part must be present on the SSE path")
	}
}

// PLAN P5: attachments exist only on ToolStateCompleted in 1.18.21, so a failed
// tool state can never produce an artifact.
func TestSurfaceContractToolStateAttachments(t *testing.T) {
	var m struct {
		ToolStates map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"tool_states"`
		PartUnion []string `json:"part_union"`
	}
	readJSON(t, "message-parts.json", &m)

	completed, ok := m.ToolStates["ToolStateCompleted"]
	if !ok {
		t.Fatal("ToolStateCompleted missing from the fixture")
	}
	if _, has := completed.Properties["attachments"]; !has {
		t.Error("ToolStateCompleted must carry attachments")
	}
	errState, ok := m.ToolStates["ToolStateError"]
	if !ok {
		t.Fatal("ToolStateError missing from the fixture")
	}
	if _, has := errState.Properties["attachments"]; has {
		t.Error("ToolStateError must not carry attachments on 1.18.21; " +
			"PLAN P5 depends on a failed tool state producing no artifact")
	}

	wantUnion := []string{
		"TextPart", "SubtaskPart", "ReasoningPart", "FilePart", "ToolPart",
		"StepStartPart", "StepFinishPart", "SnapshotPart", "PatchPart",
		"AgentPart", "RetryPart", "CompactionPart",
	}
	if len(m.PartUnion) != len(wantUnion) {
		t.Errorf("part union has %d members, want %d: %v", len(m.PartUnion), len(wantUnion), m.PartUnion)
	}
	have := make(map[string]bool, len(m.PartUnion))
	for _, p := range m.PartUnion {
		have[p] = true
	}
	for _, w := range wantUnion {
		if !have[w] {
			t.Errorf("part union is missing %q", w)
		}
	}
}

// PLAN P2: the engine always publishes a sentinel project at the filesystem
// root. Discovery must reject it on both the id and the worktree.
func TestSurfaceContractProjectSentinel(t *testing.T) {
	var s struct {
		ProjectList struct {
			Response []struct {
				ID       string `json:"id"`
				Worktree string `json:"worktree"`
			} `json:"response"`
		} `json:"project_list"`
	}
	readJSON(t, "session-project-lists.json", &s)

	var sawGlobalRoot bool
	for _, p := range s.ProjectList.Response {
		if p.ID == "global" && p.Worktree == "/" {
			sawGlobalRoot = true
		}
	}
	if !sawGlobalRoot {
		t.Error(`the fixture must contain the sentinel {"id":"global","worktree":"/"}; ` +
			"PLAN P2 rejects it on both signals")
	}
}

// PLAN P7: the MCP status vocabulary is closed, and the normalization must be
// total so an unrecognized future member degrades instead of leaking.
func TestSurfaceContractMCPStatusVocabulary(t *testing.T) {
	var d struct {
		MCP struct {
			Upstream   []string          `json:"upstream_status_vocabulary"`
			Normalized map[string]string `json:"normalized"`
		} `json:"mcp"`
	}
	readJSON(t, "diagnostics-endpoints.json", &d)

	want := map[string]string{
		"connected":                 "connected",
		"disabled":                  "disabled",
		"failed":                    "failed",
		"needs_auth":                "needs_auth",
		"needs_client_registration": "needs_registration",
	}
	if len(d.MCP.Upstream) != len(want) {
		t.Errorf("upstream vocabulary %v has %d members, want %d", d.MCP.Upstream, len(d.MCP.Upstream), len(want))
	}
	for _, u := range d.MCP.Upstream {
		got, ok := d.MCP.Normalized[u]
		if !ok {
			t.Errorf("upstream status %q has no normalization", u)
			continue
		}
		if got != want[u] {
			t.Errorf("status %q normalizes to %q, want %q", u, got, want[u])
		}
	}
	if _, ok := d.MCP.Normalized["<absent|empty|unknown>"]; !ok {
		t.Error("the normalization must be total: an unknown status maps to unknown")
	}
}

// PLAN P6: the two documented-but-empty handlers are recorded as exclusions,
// not as fixtures for planned functionality.
func TestSurfaceContractEmptyHandlersExcluded(t *testing.T) {
	var w struct {
		ExcludedFileStatus struct {
			Response []any `json:"response"`
		} `json:"excluded_file_status"`
		ExcludedFindSymbol struct {
			Response []any `json:"response"`
		} `json:"excluded_find_symbol"`
	}
	readJSON(t, "workspace-endpoints.json", &w)
	if len(w.ExcludedFileStatus.Response) != 0 {
		t.Error("GET /file/status returned data; the 1.18.21 exclusion must be reassessed")
	}
	if len(w.ExcludedFindSymbol.Response) != 0 {
		t.Error("GET /find/symbol returned data; the 1.18.21 exclusion must be reassessed")
	}
}

// The corpus must carry no credential or host-path material (PLAN P0 step 6).
//
// Two categories of legitimate content would trip a naive substring scan and
// are excluded deliberately, not accidentally:
//
//   - The canonical set files are lists of OpenAPI route and schema *names*.
//     They contain /experimental/* routes by construction (D3 requires the
//     complete sets) and names such as ProviderAuthAuthorization. Whether any
//     Go file actually calls an experimental endpoint is asserted separately by
//     TestNoExperimentalRoute.
//   - Prose in the manifest and route summary explains why secret-bearing
//     routes are excluded, so it names the concepts.
//
// What must never appear is credential *material*: a header carrying a token, a
// JSON field holding a key, or a host path identifying the operator.
func TestSurfaceContractCorpusIsSanitized(t *testing.T) {
	// Set files are name inventories; scanned only for host paths below.
	nameInventory := map[string]bool{
		"openapi-paths.txt":      true,
		"openapi-operations.txt": true,
		"openapi-schemas.txt":    true,
		"event-types.txt":        true,
	}
	// Credential material, as it would actually appear in a captured payload.
	secretMaterial := []string{
		`"authorization":`, `"api_key":`, `"apikey":`, `"access_token":`,
		`"refresh_token":`, `"client_secret":`, `"password":`, `"cookie":`,
		"bearer ey", "sk-", "ghp_", "-----begin",
	}
	// Host identifiers, which must not appear in any file including set files.
	hostMaterial := []string{"/users/", "/home/", "/private/tmp/claude-"}

	entries, err := os.ReadDir(surfaceDir)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("corpus is empty")
	}
	var scanned int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(surfaceDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		low := strings.ToLower(string(b))
		for _, f := range hostMaterial {
			if strings.Contains(low, f) {
				t.Errorf("%s contains a host path fragment %q", e.Name(), f)
			}
		}
		if nameInventory[e.Name()] {
			continue
		}
		for _, f := range secretMaterial {
			if strings.Contains(low, f) {
				t.Errorf("%s contains credential material %q", e.Name(), f)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no corpus files; the guard would pass vacuously")
	}
}

// A11: no runtime or test helper in this package may call an experimental
// endpoint. P11 repeats this as an acceptance grep.
func TestNoExperimentalRoute(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var scanned int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		src := string(b)
		// This file names the string in its own assertion, so skip itself.
		if e.Name() == "surface_contract_test.go" {
			continue
		}
		if strings.Contains(src, "/experimental/") {
			t.Errorf("%s references an experimental OpenCode endpoint", e.Name())
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no Go files; the guard would pass vacuously")
	}
}

// The committed baseline is the reference P0A and P11 compare against, so its
// shape is part of the contract.
func TestSurfaceContractCoverageBaseline(t *testing.T) {
	var b struct {
		Floors struct {
			General  float64 `json:"general"`
			OpenCode float64 `json:"opencode"`
			NewDart  float64 `json:"new_dart"`
			P0A      float64 `json:"p0a_target"`
		} `json:"floors"`
		Stability struct {
			Varied []string `json:"varied_between_runs"`
		} `json:"stability"`
		Targets []struct {
			Kind      string  `json:"kind"`
			Target    string  `json:"target"`
			Covered   int     `json:"covered"`
			Total     int     `json:"total"`
			Uncovered int     `json:"uncovered"`
			Percent   float64 `json:"percent"`
		} `json:"targets"`
	}
	readJSON(t, "unit-coverage-baseline.json", &b)

	if b.Floors.General != 80.0 || b.Floors.OpenCode != 85.0 ||
		b.Floors.NewDart != 90.0 || b.Floors.P0A != 82.0 {
		t.Errorf("floors = %+v, want 80/85/90/82", b.Floors)
	}
	if len(b.Targets) == 0 {
		t.Fatal("baseline records no targets")
	}
	seen := make(map[string]bool)
	for _, tg := range b.Targets {
		key := tg.Kind + " " + tg.Target
		if seen[key] {
			t.Errorf("baseline repeats target %q", key)
		}
		seen[key] = true
		if tg.Total <= 0 {
			t.Errorf("%s has total %d", key, tg.Total)
		}
		if tg.Covered < 0 || tg.Covered > tg.Total {
			t.Errorf("%s has covered %d out of %d", key, tg.Covered, tg.Total)
		}
		if tg.Uncovered != tg.Total-tg.Covered {
			t.Errorf("%s uncovered %d != %d-%d", key, tg.Uncovered, tg.Total, tg.Covered)
		}
	}
	for _, want := range []string{
		"go ./internal/provider/opencode", "go ./internal/provider/httpagent",
		"go ./internal/ws", "go ./internal/session", "dart <application>",
		"dart lib/features/chat/chat_screen.dart",
	} {
		if !seen[want] {
			t.Errorf("baseline is missing target %q", want)
		}
	}
	// The three packages whose counts moved between P0 runs must be recorded,
	// so a later phase compares them same-run instead of against prose.
	for _, want := range []string{"./internal/daemon", "./internal/session", "./internal/ws"} {
		if !contains(b.Stability.Varied, want) {
			t.Errorf("baseline must record %q as varying between runs", want)
		}
	}
}
