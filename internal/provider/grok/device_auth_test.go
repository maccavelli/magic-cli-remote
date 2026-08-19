package grok

import (
	"os"
	"path/filepath"
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
