//go:build windows

package appdirs

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// withKnownFolders substitutes the Known Folder lookup so the Windows layout
// is testable without touching the real profile.
func withKnownFolders(t *testing.T, roaming, local string) {
	t.Helper()
	prev := knownFolder
	knownFolder = func(id *windows.KNOWNFOLDERID) (string, error) {
		switch id {
		case windows.FOLDERID_RoamingAppData:
			return roaming, nil
		case windows.FOLDERID_LocalAppData:
			return local, nil
		}
		return "", nil
	}
	t.Cleanup(func() { knownFolder = prev })
}

// TestSystemRootsWindowsLayout pins the MADR 0116 D3 table.
func TestSystemRootsWindowsLayout(t *testing.T) {
	root := t.TempDir()
	roaming := filepath.Join(root, "Roaming")
	local := filepath.Join(root, "Local")
	withKnownFolders(t, roaming, local)

	r, _, err := SystemRoots(ProductMcremote)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(local, "mcremote")
	for _, tc := range []struct{ name, got, want string }{
		{"ConfigHome", r.ConfigHome, roaming},
		{"DataHome", r.DataHome, local},
		{"StateHome", r.StateHome, filepath.Join(base, "State")},
		{"CacheHome", r.CacheHome, filepath.Join(base, "Cache")},
		{"RuntimeHome", r.RuntimeHome, filepath.Join(base, "Runtime")},
		{"Logs", r.Logs, filepath.Join(base, "Logs")},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if !r.ProductScoped {
		t.Error("ProductScoped = false; Windows roots are product-scoped")
	}
}

// TestResolveWindowsDoesNotDoubleJoin is the end-to-end form of the P1
// deviation: the leaf must appear once, not twice.
func TestResolveWindowsDoesNotDoubleJoin(t *testing.T) {
	root := t.TempDir()
	withKnownFolders(t, filepath.Join(root, "Roaming"), filepath.Join(root, "Local"))

	r, _, err := SystemRoots(ProductMcremote)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(ProductMcremote, r, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, got string }{
		{"StateDir", p.StateDir},
		{"CacheDir", p.CacheDir},
		{"RuntimeBase", p.RuntimeBase},
	} {
		if n := strings.Count(tc.got, string(filepath.Separator)+"mcremote"); n != 1 {
			t.Errorf("%s = %q contains the product leaf %d times, want 1", tc.name, tc.got, n)
		}
	}
	if want := filepath.Join(r.DataHome, "mcremote"); p.DataDir != want {
		t.Errorf("DataDir = %q, want %q", p.DataDir, want)
	}
}

// TestSystemRootsNoUID guards MADR 0116 F4: os.Getuid() returns -1 on Windows,
// so no resolved path may carry it.
func TestSystemRootsNoUID(t *testing.T) {
	root := t.TempDir()
	withKnownFolders(t, filepath.Join(root, "Roaming"), filepath.Join(root, "Local"))
	r, _, err := SystemRoots(ProductMcremote)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.RuntimeHome, "-1") {
		t.Errorf("RuntimeHome = %q carries a uid", r.RuntimeHome)
	}
}

// TestPathLengthDiagnostic proves the policy is a warning, never a refusal
// (MADR 0116 D3).
func TestPathLengthDiagnostic(t *testing.T) {
	shallow := Roots{RuntimeHome: `C:\R`}
	if d := pathLengthDiagnostics(shallow, ProductMcremote); len(d) != 0 {
		t.Errorf("shallow layout produced %d diagnostics, want 0", len(d))
	}
	deep := Roots{RuntimeHome: `C:\` + strings.Repeat("d", maxWindowsPathBudget)}
	d := pathLengthDiagnostics(deep, ProductMcremote)
	if len(d) != 1 || d[0].Code != "windows_path_length" {
		t.Fatalf("deep layout diagnostics = %#v, want one windows_path_length", d)
	}
	// A deep layout must still resolve — the policy warns, it does not refuse.
	deep.Home, deep.ConfigHome, deep.DataHome = `C:\H`, `C:\C`, `C:\D`
	deep.StateHome, deep.CacheHome, deep.Temp = `C:\S`, `C:\K`, `C:\T`
	if _, err := Resolve(ProductMcremote, deep, ""); err != nil {
		t.Errorf("Resolve refused a deep layout: %v", err)
	}
}

// TestEnsurePrivateDirIdempotent pins the MADR 0116 D4 contract on Windows:
// a second call converges and performs no writes.
func TestEnsurePrivateDirIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRuntimeDir(dir); err != nil {
		t.Fatalf("ValidateRuntimeDir after Ensure: %v", err)
	}
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatalf("second EnsurePrivateDir: %v", err)
	}
	if err := ValidateRuntimeDir(dir); err != nil {
		t.Fatalf("ValidateRuntimeDir after second Ensure: %v", err)
	}
}

// TestEnsurePrivateDirRejectsRelative mirrors the Unix precondition.
func TestEnsurePrivateDirRejectsRelative(t *testing.T) {
	if err := EnsurePrivateDir("relative"); err == nil {
		t.Fatal("expected rejection of a relative path")
	}
	if err := EnsurePrivateDir(""); err == nil {
		t.Fatal("expected rejection of an empty path")
	}
}

// TestMaxUnixSocketPathLen pins the stated sockaddr_un budget.
func TestMaxUnixSocketPathLen(t *testing.T) {
	if got := MaxUnixSocketPathLen(); got != 107 {
		t.Errorf("MaxUnixSocketPathLen() = %d, want 107", got)
	}
	if err := CheckSocketPathLength(`C:\r\admin.sock`); err != nil {
		t.Errorf("short socket path rejected: %v", err)
	}
	if err := CheckSocketPathLength(`C:\` + strings.Repeat("a", 200)); err == nil {
		t.Error("expected overlength socket path rejection")
	}
}
