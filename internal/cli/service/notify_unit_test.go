//go:build unix

package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/certs"
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

// TestUnitIsTypeNotify: each daemon's unit waits for its READY=1, with a start
// timeout longer than the three-minute Let's Encrypt bound each waits before it
// listens (MADR 0054 amendment S8, S2; MADR 0207 D22, U8 Deviation 1). mcrelay
// was Type=simple until it sent READY=1.
func TestUnitIsTypeNotify(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) {
			unit, err := renderWith(Options{Product: product, UnitName: product, Binary: "/opt/bin/" + product}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Type=notify", "TimeoutStartSec=240"} {
				if !unitHas(unit, want) {
					t.Errorf("%s unit lacks %q", product, want)
				}
			}
			if unitHas(unit, "Type=simple") {
				t.Errorf("%s unit still says Type=simple", product)
			}
			// The literal above is today's value; this is the reason for it. A
			// longer ACME bound without a longer timeout fails here.
			if got := startTimeout(t, unit); got <= certs.ACMEStartupTimeout {
				t.Errorf("%s unit: TimeoutStartSec %s is not longer than certs.ACMEStartupTimeout %s",
					product, got, certs.ACMEStartupTimeout)
			}
		})
	}
}

// startTimeout is the unit's TimeoutStartSec, in whole seconds as the
// templates write it.
func startTimeout(t *testing.T, unit string) time.Duration {
	t.Helper()
	for line := range strings.SplitSeq(unit, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "TimeoutStartSec="); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatalf("TimeoutStartSec=%q is not whole seconds: %v", v, err)
			}
			return time.Duration(n) * time.Second
		}
	}
	t.Fatal("the unit has no TimeoutStartSec")
	return 0
}

// TestRefreshMovesASimpleUnitToNotify: a unit an earlier release wrote, as
// Type=simple with no start timeout, is refreshed to the current template by
// the update that installs this one: mcremote's since S8, mcrelay's since
// MADR 0207 D22.
func TestRefreshMovesASimpleUnitToNotify(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		t.Run(product, func(t *testing.T) { refreshMovesASimpleUnitToNotify(t, product) })
	}
}

func refreshMovesASimpleUnitToNotify(t *testing.T, product string) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{
		Product: product, UnitName: product, Description: "d",
		Binary: filepath.Join(dir, product), WorkingDirectory: dir,
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

	got, err := RefreshUnit(Options{Product: product, UnitDir: unitDir}, RefreshOptions{})
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
