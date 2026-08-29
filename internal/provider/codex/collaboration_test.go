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
	if !isExperimentalInitRejection(&experimental) {
		t.Fatal("measured experimental rejection must match")
	}

	unrelated := &rpcErrorBody{Code: -32600, Message: "invalid clientInfo"}
	if isExperimentalInitRejection(unrelated) {
		t.Fatal("unrelated JSON-RPC must not retry")
	}
	if isExperimentalInitRejection(io.EOF) {
		t.Fatal("EOF must not retry")
	}
	if isExperimentalInitRejection(context.Canceled) {
		t.Fatal("cancel must not retry")
	}
	if isExperimentalInitRejection(context.DeadlineExceeded) {
		t.Fatal("timeout must not retry")
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

func TestUnrelatedInitializeErrorDoesNotRetry(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "launches")
	t.Setenv("GO_WANT_CODEX_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HELPER_REJECT_INIT", "1")
	t.Setenv("CODEX_HELPER_LAUNCH_LOG", countPath)

	p := NewWithLogger(Config{Bin: os.Args[0]}, testLogger(t))
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
