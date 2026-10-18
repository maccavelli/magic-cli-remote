package grok_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// workspaceFiles returns the regular files under dir as sorted slash paths
// relative to it. An entry whose name starts with "." is skipped, a directory
// with everything in it, so a file grok writes for itself is never taken for
// the agent's write.
func workspaceFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// TestWorkspaceFilesSkipsHiddenEntries: the auto-mode pair's armed half passes
// on any file the agent wrote (0198-MADR-prove-grok-auto-mode-by-any-write-not-one-filename.md),
// so the walk must find a write wherever the agent put it, and must not count
// a file grok keeps for itself.
func TestWorkspaceFilesSkipsHiddenEntries(t *testing.T) {
	dir := t.TempDir()
	if got, err := workspaceFiles(dir); err != nil || len(got) != 0 {
		t.Fatalf("empty workspace: %q, %v; want no files", got, err)
	}

	for _, f := range []string{
		"probe.txt",
		"docs/decisions/0001-MADR-x.md",
		".grok/state.json",
		".hidden",
		"docs/.cache/y",
	} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("OK"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := workspaceFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/decisions/0001-MADR-x.md", "probe.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("workspaceFiles = %q, want %q", got, want)
	}
}
