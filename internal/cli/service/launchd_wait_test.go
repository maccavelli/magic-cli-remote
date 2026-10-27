package service

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The teardown wait MADR 0125 asked for is the library's: setup, the update and
// --remove stop a launchd job with launchd.Job.Stop (MADR 0207 D43). What is
// left here is launchdLoaded, which setup --no-start reads.

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
