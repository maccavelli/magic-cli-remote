package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testdata147(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "0.147.0", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollaborationCatalogValid(t *testing.T) {
	cat, err := decodeCollaborationCatalog(testdata147(t, "collaborationMode-list-success.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cat.has("plan") || !cat.has("default") {
		t.Fatalf("catalog missing required modes: %+v", cat.modes)
	}
	plan, _ := cat.lookup("plan")
	if plan.Name != "Plan" || plan.ReasoningEffort == nil || *plan.ReasoningEffort != "medium" {
		t.Fatalf("plan mask = %+v", plan)
	}
}

func TestCollaborationCatalogAdditiveUnknownFields(t *testing.T) {
	cat, err := decodeCollaborationCatalog(testdata147(t, "collaborationMode-catalog-additive.json"))
	if err != nil {
		t.Fatalf("additive fields must be ignored: %v", err)
	}
	if !cat.has("plan") || !cat.has("default") {
		t.Fatal("additive catalog lost required modes")
	}
}

func TestCollaborationCatalogRejectsInvalid(t *testing.T) {
	cases := []string{
		"collaborationMode-catalog-empty-id.json",
		"collaborationMode-catalog-duplicate-id.json",
		"collaborationMode-catalog-absent-plan.json",
		"collaborationMode-catalog-invalid-effort.json",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			cat, err := decodeCollaborationCatalog(testdata147(t, name))
			if err == nil {
				t.Fatalf("decoded invalid catalog: %+v", cat)
			}
			if len(cat.modes) != 0 {
				t.Fatalf("partial catalog retained: %+v", cat.modes)
			}
		})
	}
}

func TestCollaborationListRequestCarriesEmptyParams(t *testing.T) {
	want := bytes.TrimSpace(testdata147(t, "collaborationMode-list-request.json"))
	got, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("params = %s, want %s", got, want)
	}
}

func TestRpcErrorBodyDecodesData(t *testing.T) {
	var rpc rpcErrorBody
	if err := json.Unmarshal(testdata147(t, "initialize-rejection-experimental.json"), &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Code != -32600 || rpc.Message == "" || len(rpc.Data) == 0 {
		t.Fatalf("rpc = %+v", rpc)
	}
	if !bytes.Contains(rpc.Data, []byte("experimentalApi")) {
		t.Fatalf("data = %s", rpc.Data)
	}
}

func TestExperimentalInitRejectionClassifier(t *testing.T) {
	var experimental rpcErrorBody
	if err := json.Unmarshal(testdata147(t, "initialize-rejection-experimental.json"), &experimental); err != nil {
		t.Fatal(err)
	}
	// The measured rejection carries data.capability, so it must be recognised
	// EXACTLY — not by the prose fallback. That distinction is the point of
	// MADR 0163 D9: a match on free text silently steers the provider between two
	// whole surfaces, so it must be reported as a guess when it happens.
	got := classifyExperimentalInitRejection(&experimental)
	if !got.matched {
		t.Fatal("measured experimental rejection must match")
	}
	if !got.exact || got.via != "data.capability" {
		t.Errorf("rejection = %+v, want an exact match via data.capability", got)
	}

	// Prose only: still recognised, because a server that stopped sending the
	// field would otherwise strand every experimental capability — but flagged
	// inexact so the log says the provider was steered by a string match.
	prose := &rpcErrorBody{Code: -32600, Message: "thread/search requires experimentalApi capability"}
	if got := classifyExperimentalInitRejection(prose); !got.matched || got.exact {
		t.Errorf("prose rejection = %+v, want matched and NOT exact", got)
	}

	for name, err := range map[string]error{
		"unrelated rpc": &rpcErrorBody{Code: -32600, Message: "invalid clientInfo"},
		"eof":           io.EOF,
		"cancel":        context.Canceled,
		"timeout":       context.DeadlineExceeded,
	} {
		if got := classifyExperimentalInitRejection(err); got.matched {
			t.Errorf("%s must not trigger the experimental retry, got %+v", name, got)
		}
	}
}

func TestInitializeSendsExperimentalApiTrue(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "init.log")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_INIT_LOG", logPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	defer p.Shutdown()

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("init log: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"experimentalApi":true`)) {
		t.Fatalf("initialize did not send experimentalApi:true: %s", raw)
	}
	for _, want := range [][]byte{
		[]byte(`"requestAttestation":false`),
		[]byte(`"optOutNotificationMethods":[]`),
		[]byte(`"extensions":{}`),
	} {
		if !bytes.Contains(raw, want) {
			t.Fatalf("initialize did not send %s: %s", want, raw)
		}
	}
	if p.eng == nil || !p.eng.experimental {
		t.Fatal("engine should keep the experimental initialize")
	}
}

func TestExperimentalInitializeRetriesOnce(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_REJECT_EXPERIMENTAL", "1")
	t.Setenv("CODEX_HELPER_LAUNCH_LOG", countPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine after experimental reject: %v", err)
	}
	defer p.Shutdown()

	waitLaunchCount(t, countPath, 2)
	if p.eng == nil || p.eng.experimental {
		t.Fatal("retry must publish a non-experimental engine")
	}
	ok, reason, _, _ := p.collaborationCapability()
	if ok {
		t.Fatal("downgraded path must not expose collaboration")
	}
	if reason != reasonExperimentalUnavailable("0.147.0") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestResolveBinaryIdentityHelperDoesNotCountAsLaunch(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_LAUNCH_LOG", countPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, err := resolveBinaryIdentity(ctx, os.Args[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if id.Version != "test-helper" {
		t.Fatalf("version = %q, want test-helper", id.Version)
	}
	// Assert on what the probe DID, not on whether the file exists. The file is
	// addressed by a process-wide environment variable, so its mere existence only
	// proves some helper ran somewhere in this package — which was enough to fail
	// this test on CI twice on 2026-09-21 while the probe itself was correct.
	//
	// The defect this guards is specific: resolveBinaryIdentity must not exec the
	// binary to read its version, because with the helper env inherited that exec
	// is itself a launch (MADR 0119 P6). So a row naming --version is the failure,
	// and rows from anyone else are not this test's business.
	for _, row := range launchRows(t, countPath) {
		if strings.Contains(row, "--version") {
			t.Fatalf("identity probe exec'd the binary: %q", row)
		}
	}
}

func TestUnrelatedInitializeErrorDoesNotRetry(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_REJECT_INIT", "1")
	t.Setenv("CODEX_HELPER_LAUNCH_LOG", countPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err == nil {
		t.Fatal("unrelated initialize error must fail")
	}
	waitLaunchCount(t, countPath, 1)
}

func TestInitializeTransportEOFDoesNotRetry(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_EOF_ON_INIT", "1")
	t.Setenv("CODEX_HELPER_LAUNCH_LOG", countPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err == nil {
		t.Fatal("EOF initialize must fail")
	}
	waitLaunchCount(t, countPath, 1)
}

func TestCollaborationProbeOncePerEngineGeneration(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "list")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_LIST_LOG", listPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	defer p.Shutdown()

	p.probeCollaboration(ctx, p.eng)
	p.probeCollaboration(ctx, p.eng)
	waitLaunchCount(t, listPath, 1)
	ok, _, cat, gen := p.collaborationCapability()
	if !ok || !cat.has("plan") || gen == 0 {
		t.Fatalf("capability ok=%v catalog=%+v gen=%d", ok, cat, gen)
	}
}

func TestCollaborationProbeMethodNotFound(t *testing.T) {
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_COLLAB", "notfound")

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	defer p.Shutdown()
	ok, reason, cat, _ := p.collaborationCapability()
	if ok || len(cat.modes) != 0 {
		t.Fatalf("not-found must disable collaboration: ok=%v cat=%+v", ok, cat)
	}
	if reason != reasonExperimentalUnavailable("0.147.0") {
		t.Fatalf("reason = %q", reason)
	}
}

func TestCollaborationProbeMalformedCatalog(t *testing.T) {
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_COLLAB", "malformed")

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("ensure engine: %v", err)
	}
	defer p.Shutdown()
	ok, reason, cat, _ := p.collaborationCapability()
	if ok || len(cat.modes) != 0 {
		t.Fatalf("malformed catalog retained: ok=%v cat=%+v", ok, cat)
	}
	if reason != reasonCatalogInvalid {
		t.Fatalf("reason = %q", reason)
	}
}

func TestEngineGenerationResetsCollaborationProbe(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "list")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_LIST_LOG", listPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
	p.version = "0.147.0"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("first engine: %v", err)
	}
	firstGen := p.generation
	if err := p.eng.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		gone := p.eng == nil
		p.mu.Unlock()
		if gone || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := p.ensureEngine(ctx); err != nil {
		t.Fatalf("replacement engine: %v", err)
	}
	defer p.Shutdown()
	if p.generation <= firstGen {
		t.Fatalf("generation %d did not advance from %d", p.generation, firstGen)
	}
	waitLaunchCount(t, listPath, 2)
}

func TestUnknownCollaborationNotificationIgnored(t *testing.T) {
	p := NewWithLogger(Config{Bin: "codex"}, testLogger(t))
	// Must not panic.
	p.routeNotification("collaborationMode/unknown", json.RawMessage(`{"x":1}`))
}

func TestCollaborationSettingsFixturesAreJSON(t *testing.T) {
	for _, name := range []string{
		"thread-settings-update-request.json",
		"thread-settings-update-response.json",
		"thread-settings-updated-notification.json",
	} {
		if !json.Valid(testdata147(t, name)) {
			t.Fatalf("%s is not valid JSON", name)
		}
	}
}

// waitLaunchCount polls path until it exists and contains want non-empty
// lines. A missing file is not a count of zero — that conflation is what
// let a premature read masquerade as "did not relaunch" (MADR 0119 D4).
func waitLaunchCount(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	sawFile := false
	last := -1
	for time.Now().Before(deadline) {
		n, err := readLaunchLog(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				time.Sleep(time.Millisecond)
				continue
			}
			t.Fatal(err)
		}
		sawFile = true
		last = n
		if n == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !sawFile {
		t.Fatalf("launch log never appeared at %s (want %d launches)", path, want)
	}
	t.Fatalf("launch count = %d, want %d", last, want)
}

// launchRows returns the launch log's rows, or nothing when no helper ran at all.
// A missing file is the ordinary case here, not an error.
func launchRows(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read launch log %s: %v", path, err)
	}
	var rows []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			rows = append(rows, strings.TrimSpace(line))
		}
	}
	return rows
}

func readLaunchLog(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}
