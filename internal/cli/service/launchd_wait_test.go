package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// MADR 0125. launchd teardown is asynchronous; these pin that the code waits
// for the job to leave the domain rather than sleeping a constant and hoping.
//
// They drive the launchctl seams and never touch launchd, so they prove
// *sequencing*, not timing. Only a run on a real Mac can prove the race is
// gone (0125 F7) — that is P4, and it is not this file.

// scriptedLaunchctl answers `print` from a queue of "loaded" booleans and
// records every command issued.
type scriptedLaunchctl struct {
	loaded []bool // consumed one per print; last value repeats
	calls  []string
	prints int
	sleeps int
	now    time.Time
	boot   error
}

func (s *scriptedLaunchctl) install(t *testing.T) {
	t.Helper()
	restoreRun := OverrideRunLaunchctl(func(args ...string) error {
		s.calls = append(s.calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "bootout" {
			return s.boot
		}
		return nil
	})
	prevCapture := runLaunchctlCapture
	runLaunchctlCapture = func(args ...string) (string, error) {
		if len(args) > 0 && args[0] == "print" {
			i := s.prints
			s.prints++
			if i >= len(s.loaded) {
				i = len(s.loaded) - 1
			}
			if s.loaded[i] {
				return "state = running\npid = 42\n", nil
			}
			return "", fmt.Errorf("could not find service")
		}
		return "", nil
	}
	prevSleep, prevNow := launchdSleep, launchdNow
	s.now = time.Unix(0, 0)
	launchdSleep = func(d time.Duration) {
		s.sleeps++
		s.now = s.now.Add(d)
	}
	launchdNow = func() time.Time { return s.now }

	t.Cleanup(func() {
		restoreRun()
		runLaunchctlCapture = prevCapture
		launchdSleep, launchdNow = prevSleep, prevNow
	})
}

func TestStopAndWaitPollsUntilTheJobLeavesTheDomain(t *testing.T) {
	// Loaded, still loaded, still loaded, then gone. The helper must keep
	// looking rather than returning after a fixed nap.
	s := &scriptedLaunchctl{loaded: []bool{true, true, true, false}}
	s.install(t)

	if err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if len(s.calls) != 1 || !strings.HasPrefix(s.calls[0], "bootout ") {
		t.Fatalf("expected exactly one bootout, got %v", s.calls)
	}
	if s.sleeps == 0 {
		t.Fatal("returned without polling — a wait that never waits is a sleep by another name")
	}
}

func TestStopAndWaitDoesNotBootOutAJobThatIsNotLoaded(t *testing.T) {
	// The common fresh-install case. Booting out something absent produces a
	// failure that used to be discarded wholesale, which is what let a real
	// failure through (MADR 0125 D3).
	s := &scriptedLaunchctl{loaded: []bool{false}}
	s.install(t)

	if err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote"); err != nil {
		t.Fatalf("want nil for an absent job, got %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("expected no launchctl mutation, got %v", s.calls)
	}
}

func TestStopAndWaitReportsAJobThatOutlivesTheDeadline(t *testing.T) {
	// Never leaves the domain. The caller must learn *that*, rather than
	// discovering it as a confusing error from the next command.
	s := &scriptedLaunchctl{loaded: []bool{true}}
	s.install(t)

	err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote")
	if err == nil {
		t.Fatal("want an error when the job never leaves the domain")
	}
	if !errors.Is(err, ErrLaunchdStillLoaded) {
		t.Fatalf("want ErrLaunchdStillLoaded, got %v", err)
	}
	if !strings.Contains(err.Error(), "com.magiccliremote.mcremote") {
		t.Errorf("error must name the job: %v", err)
	}
	if !strings.Contains(err.Error(), launchdWaitTimeout.String()) {
		t.Errorf("error must say how long it waited: %v", err)
	}
}

func TestStopAndWaitCarriesTheBootoutErrorOnTimeout(t *testing.T) {
	// When bootout itself failed, that is usually the reason the job is still
	// there — so it must reach the caller rather than being swallowed.
	s := &scriptedLaunchctl{loaded: []bool{true}, boot: errors.New("exit status 9")}
	s.install(t)

	err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote")
	if err == nil || !strings.Contains(err.Error(), "exit status 9") {
		t.Fatalf("want the bootout error carried through, got %v", err)
	}
}

func TestStopAndWaitToleratesABootoutRace(t *testing.T) {
	// bootout reports failure but the job is gone anyway — another path won
	// the race. That is success, not an error: the postcondition holds.
	s := &scriptedLaunchctl{loaded: []bool{true, false}, boot: errors.New("exit status 3")}
	s.install(t)

	if err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote"); err != nil {
		t.Fatalf("job is gone; want nil, got %v", err)
	}
}

func TestStopDarwinWaitsForTeardown(t *testing.T) {
	// Stop is the public entry point every caller uses before a bootstrap.
	restoreOS := OverrideInstallOS("darwin")
	defer restoreOS()
	s := &scriptedLaunchctl{loaded: []bool{true, false}}
	s.install(t)

	if err := Stop("mcremote"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(s.calls) != 1 || !strings.Contains(s.calls[0], "com.magiccliremote.mcremote") {
		t.Fatalf("expected one bootout of the product label, got %v", s.calls)
	}
	if s.prints < 2 {
		t.Fatalf("Stop must observe the domain before and after bootout, prints=%d", s.prints)
	}
}
