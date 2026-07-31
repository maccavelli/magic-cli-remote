package procutil_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

func TestRegisterListRemoveEngine(t *testing.T) {
	dir := t.TempDir()
	procutil.SetDefaultEngineRegistryDir(dir)
	t.Cleanup(func() { procutil.SetDefaultEngineRegistryDir("") })

	// Use this test process as a stand-in for a live engine.
	pid := os.Getpid()
	st, ok := procutil.ProcessStartToken(pid)
	if !ok {
		t.Skip("ProcessStartToken unavailable on this platform")
	}
	lease, err := procutil.RegisterEngine(dir, procutil.EngineRecord{
		ID:         "eng-test-1",
		Provider:   "fake",
		PID:        pid,
		StartToken: st,
		Owner:      procutil.OwnerToken(),
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := procutil.ListEngineRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != "eng-test-1" {
		t.Fatalf("recs=%+v", recs)
	}
	if !procutil.RecordLive(recs[0]) {
		t.Fatal("expected live record")
	}
	// Owner is this process, so not an orphan.
	if orphans := procutil.FindRegistryOrphans(dir); len(orphans) != 0 {
		t.Fatalf("orphans=%v", orphans)
	}
	if err := procutil.RemoveEngine(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "eng-test-1.json")); !os.IsNotExist(err) {
		t.Fatalf("record still present: %v", err)
	}
}

func TestCorruptRecordQuarantined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := procutil.ListEngineRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("recs=%v", recs)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("expected quarantine: %v", err)
	}
}
