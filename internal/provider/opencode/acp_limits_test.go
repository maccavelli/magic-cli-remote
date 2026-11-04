package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests of the bounds and guards acp_limits.go holds, moved with them from
// the 1.x dialect's tests when 0214 PLAN P7 retired it.

func TestNormalizeWorkspacePath(t *testing.T) {
	nul := "a" + string(rune(0)) + "b"
	for _, tc := range []struct {
		name, in, want string
		err            error
	}{
		{name: "empty is the root", in: "", want: ""},
		{name: "dot is the root", in: ".", want: ""},
		{name: "dot slash is the root", in: "./", want: ""},
		{name: "plain relative", in: "a/b.txt", want: "a/b.txt"},
		{name: "backslashes are normalised", in: `a\b.txt`, want: "a/b.txt"},
		{name: "redundant segments collapse", in: "a/./b/../b.txt", want: "a/b.txt"},
		{name: "inner traversal that stays inside is fine", in: "a/b/../c", want: "a/c"},
		{name: "absolute is refused", in: "/etc/passwd", err: errWorkspaceInvalidPath},
		{name: "windows-style absolute is refused", in: `\etc\passwd`, err: errWorkspaceInvalidPath},
		{name: "traversal is refused", in: "../secrets", err: errWorkspacePathEscape},
		{name: "deep traversal is refused", in: "a/../../secrets", err: errWorkspacePathEscape},
		{name: "bare parent is refused", in: "..", err: errWorkspacePathEscape},
		{name: "NUL is refused", in: nul, err: errWorkspaceInvalidPath},
		{name: "a file URI is refused", in: "file:///etc/passwd", err: errWorkspaceInvalidPath},
		{name: "an https URI is refused", in: "https://example.com/a", err: errWorkspaceInvalidPath},
		{name: "over-long is refused", in: strings.Repeat("a", maxWorkspacePathBytes+1), err: errWorkspaceInvalidPath},
		{name: "invalid utf-8 is refused", in: "a" + string([]byte{0xff}) + "b", err: errWorkspaceInvalidPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeWorkspacePath(tc.in)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSymlinkComponentsAreRejectedNotResolved is the confinement rule: a
// resolved link is trusted, and what it points at can change afterwards.
func TestSymlinkComponentsAreRejectedNotResolved(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "ok.txt"), filepath.Join(root, "inside-link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := checkNoSymlinkComponents(root, "real/ok.txt"); err != nil {
		t.Fatalf("a plain path was rejected: %v", err)
	}
	for _, bad := range []string{"escape", "escape/secret.txt", "inside-link.txt"} {
		if err := checkNoSymlinkComponents(root, bad); !errors.Is(err, errWorkspacePathSymlink) {
			t.Fatalf("checkNoSymlinkComponents(%q) = %v, want a symlink refusal", bad, err)
		}
	}
	// A link that points inside the root is refused too: the rule is about the
	// link, not about where it happens to point today.
	if err := checkNoSymlinkComponents(root, "does/not/exist"); err != nil {
		t.Fatalf("a missing path must not be a symlink error: %v", err)
	}
}

// TestSymlinkCheckSurfacesInspectionFailures proves an unreadable component is
// a refusal rather than a silent pass.
func TestSymlinkCheckSurfacesInspectionFailures(t *testing.T) {
	root := t.TempDir()
	// A file where a directory is expected makes Lstat of a child fail with a
	// non-NotExist error.
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkNoSymlinkComponents(root, "afile/child/leaf")
	if err != nil && !errors.Is(err, errWorkspaceInvalidPath) {
		t.Fatalf("err = %v, want an invalid-path refusal or nil", err)
	}
}

func TestValidShellCommand(t *testing.T) {
	nul := "echo" + string(rune(0)) + "hi"
	for _, tc := range []struct {
		name, in string
		ok       bool
	}{
		{"simple", "ls -la", true},
		{"pipeline", "go test ./... | tee out.txt", true},
		{"at the byte cap", strings.Repeat("a", maxShellCommandBytes), true},
		{"empty", "", false},
		{"blank", "   ", false},
		{"one byte over the cap", strings.Repeat("a", maxShellCommandBytes+1), false},
		{"NUL", nul, false},
		{"invalid utf-8", "echo " + string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validShellCommand(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("validShellCommand err=%v, want ok=%v", err, tc.ok)
			}
		})
	}
}
