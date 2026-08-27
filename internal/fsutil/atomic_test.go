package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestWriteFileAtomic(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	if err := WriteFileAtomic(path, []byte(`{"a":1}`), AtomicOptions{Perm: 0o600, SyncFile: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("content = %q", b)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode %04o", fi.Mode().Perm())
	}
	// No leftover temps.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}

	if err := WriteFileAtomic(path, []byte(`{"a":2}`), AtomicOptions{Perm: 0o600, SyncFile: true, SyncDir: true}); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"a":2}` {
		t.Fatalf("overwrite content = %q", b)
	}
}

func TestWriteFileAtomicRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("y"), AtomicOptions{Perm: 0o600}); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestWithLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	var ran bool
	if err := WithLock(path, 0, func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn not run")
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatal(err)
	}
}

// TestWriteFileAtomicSyncDirError proves a requested directory sync reports its
// failure instead of discarding it (MADR 0074 D25). Before this, WriteFileAtomic
// dropped the error, so a caller that asked for durability could be told the
// write succeeded when the rename was never durably recorded.
func TestWriteFileAtomicSyncDirError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")
	wantErr := errors.New("boom")

	ops := realOps()
	ops.syncDir = func(string) error { return wantErr }

	err := writeFileAtomic(path, []byte("x"), AtomicOptions{Perm: 0o600, SyncDir: true}, ops)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
	// The rename already happened, so the caller must be able to see the file
	// and decide; a sync failure is not a reason to silently unlink it.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("stat after sync failure: %v", statErr)
	}
}

// TestWriteFileAtomicSyncDirNotRequested proves the seam is inert when the
// caller did not ask for a directory sync.
func TestWriteFileAtomicSyncDirNotRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	ops := realOps()
	ops.syncDir = func(string) error { return errors.New("must not be called") }

	if err := writeFileAtomic(path, []byte("x"), AtomicOptions{Perm: 0o600}, ops); err != nil {
		t.Fatal(err)
	}
}

// TestWriteFileAtomicInjectedFailures proves every operation seam surfaces its
// failure and that a failure before rename leaves any prior file untouched and
// no temporary behind (MADR 0074 D25/D29).
func TestWriteFileAtomicInjectedFailures(t *testing.T) {
	testexec.SkipIfNoUnlinkOpenFile(t)
	const prior = "PRIOR"
	wantErr := errors.New("injected")

	cases := []struct {
		name         string
		mutate       func(*fileOps)
		renameOccurs bool
	}{
		{"createTemp", func(o *fileOps) {
			o.createTemp = func(string, string) (*os.File, error) { return nil, wantErr }
		}, false},
		{"chmodTemp", func(o *fileOps) {
			o.chmodFile = func(*os.File, os.FileMode) error { return wantErr }
		}, false},
		{"writeTemp", func(o *fileOps) {
			o.writeFile = func(*os.File, []byte) (int, error) { return 0, wantErr }
		}, false},
		{"syncTemp", func(o *fileOps) {
			o.syncFile = func(*os.File) error { return wantErr }
		}, false},
		{"closeTemp", func(o *fileOps) {
			o.closeFile = func(*os.File) error { return wantErr }
		}, false},
		{"rename", func(o *fileOps) {
			o.rename = func(string, string) error { return wantErr }
		}, false},
		{"syncDir", func(o *fileOps) {
			o.syncDir = func(string) error { return wantErr }
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "creds.json")
			if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
				t.Fatal(err)
			}

			ops := realOps()
			tc.mutate(&ops)

			err := writeFileAtomic(path, []byte("NEXT"), AtomicOptions{
				Perm: 0o600, SyncFile: true, SyncDir: true,
			}, ops)
			if !errors.Is(err, wantErr) {
				t.Fatalf("err = %v, want wrapping %v", err, wantErr)
			}

			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.renameOccurs {
				if string(got) != "NEXT" {
					t.Fatalf("content = %q, want NEXT after a post-rename failure", got)
				}
			} else if string(got) != prior {
				t.Fatalf("content = %q, want the prior bytes untouched", got)
			}

			entries, dirErr := os.ReadDir(dir)
			if dirErr != nil {
				t.Fatal(dirErr)
			}
			if len(entries) != 1 {
				t.Fatalf("entries = %d, want 1 (no temporary left behind)", len(entries))
			}
		})
	}
}
