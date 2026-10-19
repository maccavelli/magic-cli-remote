package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestTakeLogsAStoreThatCannotWrite is MADR 0201 D29 (0073 §6.10(b)). Take
// ignored two save errors, the purge on an unknown code and the removal of an
// expired one, so a store that could not write was invisible, and the
// expired entries came back on the next load. Each is now one warn line; the
// claim's result is exactly as before.
func TestTakeLogsAStoreThatCannotWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop writes on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}
	dir := t.TempDir()
	store, err := OpenPairCodeStore(filepath.Join(dir, "pair_codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	store.log = slog.New(slog.NewTextHandler(&buf, nil))
	expired, err := store.Create("phone", 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	warns := func() int { return strings.Count(buf.String(), "level=WARN") }

	if _, err := store.Take("23456789"); !errors.Is(err, ErrInvalidPairCode) {
		t.Fatalf("Take(unknown) = %v, want ErrInvalidPairCode", err)
	}
	if n := warns(); n != 1 {
		t.Fatalf("after Take(unknown) on a store that cannot write: %d warn lines, want 1\n%s", n, buf.String())
	}
	if _, err := store.Take(expired.Code); !errors.Is(err, ErrExpiredPairCode) {
		t.Fatalf("Take(expired) = %v, want ErrExpiredPairCode", err)
	}
	if n := warns(); n != 2 {
		t.Fatalf("after Take(expired) on a store that cannot write: %d warn lines, want 2\n%s", n, buf.String())
	}
}
