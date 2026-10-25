package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// bakedOptions are a definition with every option the probe follows baked in.
func bakedOptions(product string) Options {
	return Options{
		Product:    product,
		UnitName:   product,
		Binary:     "/opt/bin/" + product,
		ConfigPath: "/srv/" + product + "/config.yaml",
		DataDir:    "/srv/" + product + "/data",
		ListenHost: "127.0.0.1",
		ListenPort: 39519,
	}
}

var wantBaked = ServeArgs{
	ConfigPath: "/srv/mcrelay/config.yaml",
	DataDir:    "/srv/mcrelay/data",
	ListenHost: "127.0.0.1",
	ListenPort: 39519,
}

// TestReadServeArgsFromEachDefinition: the probe reads what setup baked into
// the installed definition, in each OS's format, with the refresh's parsers
// (MADR 0054 amendment S8, S1).
func TestReadServeArgsFromEachDefinition(t *testing.T) {
	opts := bakedOptions("mcrelay")

	t.Run("systemd", func(t *testing.T) {
		t.Cleanup(OverrideInstallOS("linux"))
		withUnitRecord(t, "mcrelay", "")
		dir := withDefinitionDir(t, &systemdUserDir)
		body, err := renderWith(opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcrelay.service"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, why, err := readServeArgs("mcrelay", "")
		if err != nil || why != "" || got != wantBaked {
			t.Fatalf("readServeArgs = %+v, %q, %v; want %+v", got, why, err, wantBaked)
		}
	})

	t.Run("launchd", func(t *testing.T) {
		t.Cleanup(OverrideInstallOS("darwin"))
		withUnitRecord(t, "mcrelay", "")
		dir := withDefinitionDir(t, &launchAgentsDir)
		body, err := renderPlistWith(opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "com.magiccliremote.mcrelay.plist"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, why, err := readServeArgs("mcrelay", "")
		if err != nil || why != "" || got != wantBaked {
			t.Fatalf("readServeArgs = %+v, %q, %v; want %+v", got, why, err, wantBaked)
		}
	})

	t.Run("task", func(t *testing.T) {
		t.Cleanup(OverrideInstallOS("windows"))
		withUnitRecord(t, "mcrelay", "")
		body, err := renderTaskXML(opts, `HOST\user`)
		if err != nil {
			t.Fatal(err)
		}
		// The definition comes from Task Scheduler through the library, not a
		// schtasks export (MADR 0207 D17, U4 Deviation 1).
		s := withTaskScheduler(t)
		s.xml["mcrelay"] = body
		withSchtasks(t, func(args ...string) (string, error) {
			t.Errorf("schtasks %q", args)
			return "", nil
		})
		got, why, err := readServeArgs("mcrelay", "")
		if err != nil || why != "" || got != wantBaked {
			t.Fatalf("readServeArgs = %+v, %q, %v; want %+v", got, why, err, wantBaked)
		}
		if !slices.Equal(s.calls, []string{"definition mcrelay"}) {
			t.Errorf("Task Scheduler calls %q, want one definition read of mcrelay", s.calls)
		}
	})
}

// withDefinitionDir points a definition folder at a scratch directory for one
// test, and returns it.
func withDefinitionDir(t *testing.T, dirFn *func() (string, error)) string {
	t.Helper()
	dir := t.TempDir()
	prev := *dirFn
	*dirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { *dirFn = prev })
	return dir
}

// withServeArgs replaces the definition reader for one test.
func withServeArgs(t *testing.T, fn func(product, unitName string) (ServeArgs, string, error)) {
	t.Helper()
	prev := installedServeArgs
	installedServeArgs = fn
	t.Cleanup(func() { installedServeArgs = prev })
}

// TestBackendProbePassesTheDefinitionsArgs: the product's check gets the
// definition's arguments on each call.
func TestBackendProbePassesTheDefinitionsArgs(t *testing.T) {
	withServeArgs(t, func(product, unitName string) (ServeArgs, string, error) {
		return ServeArgs{DataDir: "/srv/" + product}, "", nil
	})
	var seen ServeArgs
	h := HealthOptions{Probe: func(_ context.Context, args ServeArgs) (libservice.Health, error) {
		seen = args
		return libservice.Health{Ready: true}, nil
	}}
	got, err := h.backendProbe("mcremote", "")(context.Background())
	if err != nil || !got.Ready || seen.DataDir != "/srv/mcremote" {
		t.Fatalf("probe = %+v, %v; the check saw %+v", got, err, seen)
	}
}

// TestBackendProbeSkipsAnUnreadableDefinition: a definition whose arguments
// cannot be read back, such as one edited by hand, gets no application check,
// so its update is not rolled back for a socket nobody serves.
func TestBackendProbeSkipsAnUnreadableDefinition(t *testing.T) {
	withServeArgs(t, func(string, string) (ServeArgs, string, error) {
		return ServeArgs{}, "has an unrecognised ExecStart= argument \"--verbose\"", nil
	})
	h := HealthOptions{Probe: func(context.Context, ServeArgs) (libservice.Health, error) {
		t.Error("the check ran on a definition that could not be read back")
		return libservice.Health{}, nil
	}}
	got, err := h.backendProbe("mcremote", "")(context.Background())
	if err != nil || !got.Ready || !strings.Contains(got.Detail, "no application check") {
		t.Fatalf("probe = %+v, %v; want ready, with the reason", got, err)
	}
}

// TestBackendProbeErrorsWithNoDefinition: a definition that cannot be read is
// an error, which the backend keeps and reports if the wait times out.
func TestBackendProbeErrorsWithNoDefinition(t *testing.T) {
	missing := errors.New("read the mcremote unit: no such file")
	withServeArgs(t, func(string, string) (ServeArgs, string, error) { return ServeArgs{}, "", missing })
	h := HealthOptions{Probe: func(context.Context, ServeArgs) (libservice.Health, error) {
		return libservice.Health{Ready: true}, nil
	}}
	if _, err := h.backendProbe("mcremote", "")(context.Background()); !errors.Is(err, missing) {
		t.Fatalf("probe error = %v, want %v", err, missing)
	}
}

// TestNoProbeWithoutTheProducts: NewBackend, which doctor and setup use,
// wires no probe, as before.
func TestNoProbeWithoutTheProducts(t *testing.T) {
	var h HealthOptions
	if h.backendProbe("mcremote", "") != nil || h.backendRequestStop("mcremote", "") != nil {
		t.Fatal("empty HealthOptions built a probe or a stop request")
	}
}

// TestBackendRequestStopPassesTheDefinitionsArgs: the Task Scheduler
// backend's request goes to the daemon the definition runs, not to a PID.
func TestBackendRequestStopPassesTheDefinitionsArgs(t *testing.T) {
	withServeArgs(t, func(string, string) (ServeArgs, string, error) {
		return ServeArgs{DataDir: `C:\srv\data`}, "", nil
	})
	var seen ServeArgs
	h := HealthOptions{RequestStop: func(_ context.Context, args ServeArgs) error {
		seen = args
		return nil
	}}
	if err := h.backendRequestStop("mcremote", "")(context.Background(), 4242); err != nil || seen.DataDir != `C:\srv\data` {
		t.Fatalf("request = %v; it saw %+v", err, seen)
	}
}
