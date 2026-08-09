package receipt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	s, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dataDir
}

// appendPermissionDecision builds, signs, and appends one permission-decision
// entry for deviceID, chaining onto whatever LastHash currently reports —
// the same sequence P7's orchestration will follow.
func appendPermissionDecision(t *testing.T, s *Store, priv *ecdsa.PrivateKey, deviceID string, seq int) {
	t.Helper()
	last, ok, err := s.LastHash(deviceID)
	if err != nil {
		t.Fatal(err)
	}
	var prev *string
	if ok {
		prev = &last
	}
	stmt, err := BuildPermissionDecisionStatement(
		"sess-1", fmt.Sprintf("perm-%d", seq), deviceID, "once",
		"bash", fmt.Sprintf("echo %d", seq),
		time.Unix(int64(seq), 0).UTC(), "device:"+deviceID, prev,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := SignES256Compact(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(deviceID, compact); err != nil {
		t.Fatal(err)
	}
}

func TestStoreAppendAndVerifyIntact(t *testing.T) {
	s, _ := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const n = 25
	for i := 0; i < n; i++ {
		appendPermissionDecision(t, s, priv, "dev-1", i)
	}
	broken, err := s.Verify("dev-1", &priv.PublicKey, &daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("broken = %d, want -1 (intact)", broken)
	}
}

func TestStoreVerifyNoEntriesIsIntact(t *testing.T) {
	s, _ := newTestStore(t)
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	daemonPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	broken, err := s.Verify("no-such-device", &priv.PublicKey, &daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("broken = %d, want -1 for a device with no entries", broken)
	}
}

func TestStoreVerifyDetectsTamperedLine(t *testing.T) {
	s, dataDir := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const n = 10
	for i := 0; i < n; i++ {
		appendPermissionDecision(t, s, priv, "dev-1", i)
	}

	path := filepath.Join(dataDir, "receipts", "dev-1.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("lines = %d, want %d", len(lines), n)
	}
	// Mutate line 5 (1-indexed) — flipPart (jws_test.go) guarantees a real
	// change, unlike overwriting a fixed trailing character.
	const tamperedLine = 5
	lines[tamperedLine-1] = flipPart(t, lines[tamperedLine-1], 2)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A fresh store instance so nothing is served from the in-memory cache.
	fresh, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	broken, err := fresh.Verify("dev-1", &priv.PublicKey, &daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != tamperedLine {
		t.Fatalf("broken = %d, want %d", broken, tamperedLine)
	}
}

func TestStoreLastHashSurvivesRestart(t *testing.T) {
	s, dataDir := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		appendPermissionDecision(t, s, priv, "dev-1", i)
	}
	want, ok, err := s.LastHash("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want ok=true after appends")
	}

	fresh, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := fresh.LastHash("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("want ok=true from a fresh store instance")
	}
	if got != want {
		t.Fatalf("LastHash after restart = %s, want %s", got, want)
	}
}

func TestStorePermissions(t *testing.T) {
	s, dataDir := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	appendPermissionDecision(t, s, priv, "dev-1", 0)

	dirInfo, err := os.Stat(filepath.Join(dataDir, "receipts"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("receipts dir perm = %o, want 0700", perm)
	}
	fileInfo, err := os.Stat(filepath.Join(dataDir, "receipts", "dev-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("receipt file perm = %o, want 0600", perm)
	}
}

// TestStoreScalesLinearly is this phase's "large synthetic chain"
// smoke check (§Acceptance: "sanity bound... not a hard perf gate... nothing
// here is accidentally quadratic"). It deliberately does NOT assert against
// a literal 10,000-entry absolute wall-clock budget: Append's cost is
// dominated by one fsync per entry — a deliberate durability requirement
// (D6/D8: a receipt must survive a crash right after it's acknowledged), and
// measured fsync latency on this sandbox's disk (~2.8ms/entry) would make a
// real 10k-entry run take ~30s, added to every routine `go test ./...` — a
// real regression to the edit-test loop, for a number this repo has no
// existing `-short`-gating convention to exempt. Instead: append N entries,
// then 4N more, and assert the second batch isn't disproportionately slower
// per-entry than the first — a relative-growth check that catches an
// accidentally-quadratic regression (e.g. LastHash falling back to a
// full-file scan) without hardcoding an assumption about disk speed.
func TestStoreScalesLinearly(t *testing.T) {
	s, _ := newTestStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const firstBatch = 200
	const secondBatch = 800 // 4x firstBatch, same device, same file, growing

	start := time.Now()
	for i := 0; i < firstBatch; i++ {
		appendPermissionDecision(t, s, priv, "dev-1", i)
	}
	firstElapsed := time.Since(start)

	start = time.Now()
	for i := firstBatch; i < firstBatch+secondBatch; i++ {
		appendPermissionDecision(t, s, priv, "dev-1", i)
	}
	secondElapsed := time.Since(start)

	broken, err := s.Verify("dev-1", &priv.PublicKey, &daemonPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if broken != -1 {
		t.Fatalf("broken = %d, want -1 (intact) after %d entries", broken, firstBatch+secondBatch)
	}

	firstPerEntry := firstElapsed / firstBatch
	secondPerEntry := secondElapsed / secondBatch
	t.Logf("per-entry append cost: first %d entries = %s, next %d entries (file already %d lines) = %s",
		firstBatch, firstPerEntry, secondBatch, firstBatch, secondPerEntry)

	// A quadratic LastHash (e.g. re-scanning the whole file instead of
	// seeking from the end) would make secondPerEntry grow with file size;
	// linear behavior keeps it roughly flat. 5x tolerates real-world jitter
	// (GC pauses, disk scheduling) without masking an actual O(n) -> O(n^2)
	// regression, which would blow well past this on a file 5x larger.
	if secondPerEntry > 5*firstPerEntry {
		t.Fatalf("per-entry append cost grew %vx (from %s to %s) as the file grew — looks quadratic, want roughly linear",
			float64(secondPerEntry)/float64(firstPerEntry), firstPerEntry, secondPerEntry)
	}
}
