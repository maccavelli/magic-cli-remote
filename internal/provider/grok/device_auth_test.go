package grok

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

func TestGrokDeviceAuthSuppressesHostOpen(t *testing.T) {
	dir, extra, err := hostOpenStubDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if len(extra) != 1 || !strings.HasPrefix(extra[0], "PATH=") {
		t.Fatalf("extraEnv = %v, want one PATH= overlay", extra)
	}
	pathVal := strings.TrimPrefix(extra[0], "PATH=")
	first, _, _ := strings.Cut(pathVal, string(os.PathListSeparator))
	if first != dir {
		t.Fatalf("PATH first element = %q, want stub dir %q", first, dir)
	}
	for _, name := range []string{"open", "xdg-open"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
	}
}

func TestGrokDeviceFlowExpiry(t *testing.T) {
	got := grokDeviceFlowResult(providerauth.Classification{})
	if got.ExpiresIn != grokDeviceExpirySeconds {
		t.Fatalf("ExpiresIn = %d, want %d", got.ExpiresIn, grokDeviceExpirySeconds)
	}
	if got.Interval != 5 {
		t.Fatalf("Interval = %d, want 5", got.Interval)
	}
}

func TestGrokDeviceAuthDarwinSandboxWrap(t *testing.T) {
	const fakeBin = "/opt/fake/grok"
	if !strings.Contains(grokDeviceAuthSandbox, "(deny default)") {
		t.Fatal("sandbox profile missing (deny default)")
	}
	if strings.Contains(grokDeviceAuthSandbox, "(allow mach-lookup") {
		t.Fatal("sandbox profile must not allow blanket mach-lookup")
	}
	spawnBin, args, err := wrapGrokDeviceAuth(fakeBin)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "darwin" {
		if spawnBin != fakeBin {
			t.Fatalf("spawnBin = %q, want %q", spawnBin, fakeBin)
		}
		if len(args) != 2 || args[0] != "login" || args[1] != "--device-auth" {
			t.Fatalf("args = %v, want [login --device-auth]", args)
		}
		return
	}
	if !strings.Contains(spawnBin, "sandbox-exec") {
		t.Fatalf("spawnBin = %q, want sandbox-exec", spawnBin)
	}
	want := []string{"-p", grokDeviceAuthSandbox, fakeBin, "login", "--device-auth"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
	cmd := exec.Command(spawnBin, "-p", grokDeviceAuthSandbox, "/usr/bin/true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandbox-exec profile rejected /usr/bin/true: %v\n%s", err, out)
	}
}
