package appdirs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNotOwnerOnlyDetailIsPlatformCorrect is the per-platform half of MADR
// 0155 D4. Windows' naming and the remedy's effect are pinned in
// owneronly_message_windows_test.go; this checks the shape every caller
// composes against, on the platform running it.
func TestNotOwnerOnlyDetailIsPlatformCorrect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NotOwnerOnlyDetail(path)
	if !strings.HasPrefix(got, "readable by ") || !strings.Contains(got, "; run: ") {
		t.Fatalf("detail %q is not \"readable by <who>; run: <command>\"", got)
	}
	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(got), "chmod") {
			t.Errorf("Windows detail says chmod, which does not exist there: %q", got)
		}
		if !strings.Contains(got, "icacls ") {
			t.Errorf("Windows detail %q gives no icacls remedy", got)
		}
		return
	}
	if want := "readable by group/other; run: chmod 0600 " + path; got != want {
		t.Errorf("Unix detail = %q, want %q", got, want)
	}
}
