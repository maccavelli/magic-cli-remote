package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionStringDefaults(t *testing.T) {
	got := VersionString()
	if !strings.Contains(got, "dev") || !strings.Contains(got, "none") || !strings.Contains(got, "unknown") {
		t.Fatalf("VersionString() = %q, want dev/none/unknown", got)
	}
}

func TestSetVersionInfo(t *testing.T) {
	SetVersionInfo("1.0.0", "abc123", "2026-07-25")
	got := VersionString()
	if !strings.Contains(got, "1.0.0") || !strings.Contains(got, "abc123") || !strings.Contains(got, "2026-07-25") {
		t.Fatalf("VersionString() = %q, want 1.0.0/abc123/2026-07-25", got)
	}
}

func TestSetVersionInfoSkipsEmpty(t *testing.T) {
	SetVersionInfo("2.0.0", "", "")
	got := VersionString()
	if !strings.Contains(got, "2.0.0") {
		t.Fatalf("VersionString() = %q, want 2.0.0", got)
	}
}

func TestVersionCommandOutput(t *testing.T) {
	SetVersionInfo("3.0.0", "def456", "2026-07-26")
	cmd := newVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "3.0.0") || !strings.Contains(out, "def456") {
		t.Fatalf("version output = %q, want 3.0.0/def456", out)
	}
}
