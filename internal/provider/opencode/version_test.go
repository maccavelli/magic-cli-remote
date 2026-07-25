package opencode

import (
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat int
		ok            bool
	}{
		{"1.18.4", 1, 18, 4, true},
		{"v1.18.4", 1, 18, 4, true},
		{"1.18.4-beta", 1, 18, 4, true},
		{"1.18", 1, 18, 0, true},
		{"", 0, 0, 0, false},
		{"not-a-version", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, pat, ok := parseSemver(c.in)
		if ok != c.ok || maj != c.maj || min != c.min || pat != c.pat {
			t.Errorf("parseSemver(%q)=%d.%d.%d ok=%v want %d.%d.%d ok=%v",
				c.in, maj, min, pat, ok, c.maj, c.min, c.pat, c.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.18.4", "1.18.0") <= 0 {
		t.Fatal("1.18.4 should be > 1.18.0")
	}
	if CompareVersions("1.17.9", MinVersion) >= 0 {
		t.Fatal("1.17.9 should be < MinVersion")
	}
	if CompareVersions("1.18.0", MinVersion) != 0 {
		t.Fatal("1.18.0 should equal MinVersion")
	}
	if !VersionMeetsMin("1.18.4") {
		t.Fatal("1.18.4 should meet min")
	}
	if VersionMeetsMin("1.17.0") {
		t.Fatal("1.17.0 should not meet min")
	}
}

func TestOnHealthyAndVersionGate(t *testing.T) {
	d := &httpDialect{log: slogDefault()}
	if err := d.OnHealthy([]byte(`{"healthy":true,"version":"1.17.0"}`)); err != nil {
		t.Fatal(err)
	}
	if d.EngineVersion() != "1.17.0" {
		t.Fatalf("version=%q", d.EngineVersion())
	}
	// Tree on → refuse old engine.
	err := d.CheckMinVersion(httpagent.Config{}) // default tree on
	if err == nil {
		t.Fatal("expected version pin error for 1.17 with tree on")
	}
	// Tree off → allow.
	off := false
	if err := d.CheckMinVersion(httpagent.Config{SessionTree: &off}); err != nil {
		t.Fatalf("kill switch should allow old engine: %v", err)
	}
	// Empty version → allow (unknown).
	d2 := &httpDialect{log: slogDefault()}
	if err := d2.CheckMinVersion(httpagent.Config{}); err != nil {
		t.Fatalf("unknown version must not block: %v", err)
	}
}

func slogDefault() *slog.Logger {
	return slog.Default()
}
