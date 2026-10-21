//go:build unix

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unitHas reports whether body carries directive as a whole line.
func unitHas(body, directive string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) == directive {
			return true
		}
	}
	return false
}

// TestUnitIsTypeNotify: mcremote's unit waits for the daemon's READY=1, with
// a start timeout longer than the three-minute Let's Encrypt bound the daemon
// waits before it listens (MADR 0054 amendment S8, S2). mcrelay sends no
// READY=1, so its unit stays Type=simple: as notify it would never start.
func TestUnitIsTypeNotify(t *testing.T) {
	remote, err := renderWith(Options{Product: "mcremote", UnitName: "mcremote", Binary: "/opt/bin/mcremote"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Type=notify", "TimeoutStartSec=240"} {
		if !unitHas(remote, want) {
			t.Errorf("mcremote unit lacks %q", want)
		}
	}
	if unitHas(remote, "Type=simple") {
		t.Error("mcremote unit still says Type=simple")
	}

	relay, err := renderWith(Options{Product: "mcrelay", UnitName: "mcrelay", Binary: "/opt/bin/mcrelay"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !unitHas(relay, "Type=simple") || unitHas(relay, "Type=notify") {
		t.Errorf("mcrelay unit must stay Type=simple:\n%s", relay)
	}
}

// TestRefreshMovesASimpleUnitToNotify: a unit an earlier release wrote, as
// Type=simple with no start timeout, is refreshed to the current template by
// the update that installs this one.
func TestRefreshMovesASimpleUnitToNotify(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{
		Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir,
	}
	path := writeUnit(t, unitDir, opts, nil)
	canonical, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(canonical), "Type=notify\n", "Type=simple\n", 1)
	old = strings.Replace(old, "TimeoutStartSec=240\n", "", 1)
	if old == string(canonical) || strings.Contains(old, "TimeoutStartSec") {
		t.Fatal("could not write the earlier release's unit")
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictRefreshed {
		t.Fatalf("verdict = %q (%s), want refreshed", got.Verdict, got.Reason)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(now) != string(canonical) {
		t.Fatalf("the refreshed unit is not the current template:\n%s", diffLines(string(canonical), string(now)))
	}
}
