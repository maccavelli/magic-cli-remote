package service

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// MADR 0125. launchd teardown is asynchronous; these pin that the code waits
// for the job to leave the domain rather than sleeping a constant and hoping.
//
// They drive the launchctl and launchdLoaded seams and never touch launchd, so
// they prove *sequencing*, not timing. Only a run on a real Mac can prove the
// race is gone (0125 F7) — that is P4, and it is not this file.

// scriptedLaunchctl answers launchdLoaded from a queue of "loaded" booleans,
// fails the probes errs names, and records every command issued.
type scriptedLaunchctl struct {
	loaded []bool        // consumed one per probe; last value repeats
	errs   map[int]error // probe index → that probe's error
	calls  []string
	probes int
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
	restoreLoaded := OverrideLaunchdLoaded(func(context.Context, string) (bool, error) {
		i := s.probes
		s.probes++
		if err := s.errs[i]; err != nil {
			return false, err
		}
		if i >= len(s.loaded) {
			i = len(s.loaded) - 1
		}
		return s.loaded[i], nil
	})
	prevSleep, prevNow := launchdSleep, launchdNow
	s.now = time.Unix(0, 0)
	launchdSleep = func(d time.Duration) {
		s.sleeps++
		s.now = s.now.Add(d)
	}
	launchdNow = func() time.Time { return s.now }

	t.Cleanup(func() {
		restoreRun()
		restoreLoaded()
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

// TestStopAndWaitRefusesToGuessOnAFailedProbe is MADR 0207 D19: a probe that
// fails before the bootout says neither "loaded" nor "not loaded". Reading it
// as "not loaded" skipped the teardown; it is the caller's error instead.
func TestStopAndWaitRefusesToGuessOnAFailedProbe(t *testing.T) {
	probeErr := errors.New("launchctl list: exit 5")
	s := &scriptedLaunchctl{loaded: []bool{false}, errs: map[int]error{0: probeErr}}
	s.install(t)

	err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote")
	if !errors.Is(err, probeErr) {
		t.Fatalf("want the probe's error, got %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("expected no launchctl mutation after a failed probe, got %v", s.calls)
	}
}

// TestStopAndWaitReportsAProbeThatFailsMidWait is MADR 0207 D19: a probe that
// fails after the bootout is not "gone", and the bootout's own error, when
// there was one, goes with it.
func TestStopAndWaitReportsAProbeThatFailsMidWait(t *testing.T) {
	probeErr := errors.New("launchctl list: exit 5")
	for _, tc := range []struct {
		name string
		boot error
	}{
		{"bootout succeeded", nil},
		{"bootout failed", errors.New("exit status 9")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &scriptedLaunchctl{loaded: []bool{true}, errs: map[int]error{1: probeErr}, boot: tc.boot}
			s.install(t)

			err := stopAndWaitDarwin("gui/501/com.magiccliremote.mcremote")
			if !errors.Is(err, probeErr) {
				t.Fatalf("want the probe's error, got %v", err)
			}
			if errors.Is(err, ErrLaunchdStillLoaded) {
				t.Fatalf("a failed probe is not a job that outlived the wait: %v", err)
			}
			if tc.boot != nil && !strings.Contains(err.Error(), tc.boot.Error()) {
				t.Errorf("want the bootout error carried through, got %v", err)
			}
			if len(s.calls) != 1 || !strings.HasPrefix(s.calls[0], "bootout ") {
				t.Fatalf("expected exactly one bootout, got %v", s.calls)
			}
		})
	}
}

// TestLaunchdLoadedTellsNotLoadedFromAFailedProbe is MADR 0207 D19, through
// the library's real Job.Loaded with launchctl's exit codes scripted: 113 is
// "not loaded", and every other failure is an error, never "not loaded".
func TestLaunchdLoadedTellsNotLoadedFromAFailedProbe(t *testing.T) {
	const label = "com.magiccliremote.mcremote"
	svc := launchdServiceTarget(label)
	if runtime.GOOS != "darwin" {
		// The library's launchd job exists only on macOS. Elsewhere the
		// probe is an error, never "not loaded".
		loaded, err := launchdLoaded(svc)
		if loaded || !errors.Is(err, libservice.ErrUnsupported) {
			t.Fatalf("launchdLoaded off macOS = %v, %v; want false and ErrUnsupported", loaded, err)
		}
		return
	}
	for _, tc := range []struct {
		name    string
		exit    int
		runErr  error
		want    bool
		wantErr bool
	}{
		{"loaded", 0, nil, true, false},
		{"not loaded: exit 113", 113, nil, false, false},
		{"I/O error: exit 5", 5, nil, false, true},
		{"not permitted: exit 1", 1, nil, false, true},
		{"launchctl did not run", 0, errors.New("fork/exec /bin/launchctl: no such file or directory"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ran []string
			prev := launchdRunner
			launchdRunner = libservice.RunnerFunc(func(_ context.Context, c libservice.Command) (libservice.Output, error) {
				ran = append(ran, strings.Join(c.Args, " "))
				switch {
				case len(c.Args) > 0 && c.Args[0] == "managername":
					return libservice.Output{Stdout: []byte("Aqua\n")}, nil
				case tc.runErr != nil:
					return libservice.Output{}, tc.runErr
				}
				return libservice.Output{ExitCode: tc.exit}, nil
			})
			t.Cleanup(func() { launchdRunner = prev })

			loaded, err := launchdLoaded(svc)
			if loaded != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("launchdLoaded = %v, %v; want %v, error %v", loaded, err, tc.want, tc.wantErr)
			}
			// The job is the label svc names, in this user's GUI domain:
			// `list` sees that domain, `print` reads its target.
			if !slices.Contains(ran, "list "+label) && !slices.Contains(ran, "print "+svc) {
				t.Errorf("launchctl ran %q; want it to read %s", ran, label)
			}
		})
	}
}

// TestLaunchdLoadedRejectsAnotherTarget: launchdLoaded reads only a GUI-domain
// target, as launchdServiceTarget writes it; anything else is an error, never
// "not loaded".
func TestLaunchdLoadedRejectsAnotherTarget(t *testing.T) {
	prev := launchdRunner
	launchdRunner = libservice.RunnerFunc(func(_ context.Context, c libservice.Command) (libservice.Output, error) {
		t.Errorf("launchctl %v ran for a target launchdLoaded must refuse", c.Args)
		return libservice.Output{}, nil
	})
	t.Cleanup(func() { launchdRunner = prev })
	for _, svc := range []string{
		"user/501/com.magiccliremote.mcremote",
		"system/com.magiccliremote.mcremote",
		"gui/abc/com.magiccliremote.mcremote",
		"gui/501",
		"",
	} {
		if loaded, err := launchdLoaded(svc); loaded || err == nil {
			t.Errorf("launchdLoaded(%q) = %v, %v; want an error", svc, loaded, err)
		}
	}
}
