package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// doctorProvider is a provider that doctor must never start.
type doctorProvider struct {
	t     *testing.T
	id    provider.ID
	ready bool
}

func (p doctorProvider) ID() provider.ID { return p.id }
func (p doctorProvider) Ready() bool     { return p.ready }
func (p doctorProvider) Start(context.Context, provider.StartOptions) (provider.Session, error) {
	p.t.Errorf("doctor started %s; it must only look up", p.id)
	return nil, nil
}

// TestDoctorReportsEachEnginesBinaryAndReadiness is MADR 0201 D32 (0073
// §6.11). doctor reported credential files, but not whether each enabled
// engine's binary was present and ready.
func TestDoctorReportsEachEnginesBinaryAndReadiness(t *testing.T) {
	present, err := os.Executable() // any binary that resolves
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	reg.Register(doctorProvider{t: t, id: "alpha", ready: true})
	reg.Register(doctorProvider{t: t, id: "beta", ready: false})
	bins := map[provider.ID]string{"alpha": present, "beta": "mcremote-no-such-engine"}

	var b bytes.Buffer
	renderEngineDoctor(&b, reg, bins, nil)
	out := b.String()
	lines := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if f := strings.Fields(line); len(f) > 1 {
			lines[strings.TrimSuffix(f[0], ":")] = line
		}
	}
	if l := lines["alpha"]; !strings.Contains(l, "ready") || !strings.Contains(l, present) {
		t.Errorf("alpha: want ready and its path %s; report:\n%s", present, out)
	}
	if l := lines["beta"]; !strings.Contains(l, "missing: mcremote-no-such-engine") {
		t.Errorf("beta: want missing: mcremote-no-such-engine; report:\n%s", out)
	}
}

// stubEngineVersion makes `<bin> --version` answer out and err, and counts
// the calls, so no test runs a real engine.
func stubEngineVersion(t *testing.T, out string, err error) *int {
	t.Helper()
	calls := 0
	prev := engineVersion
	engineVersion = func(launch.Resolved) (string, error) {
		calls++
		return out, err
	}
	t.Cleanup(func() { engineVersion = prev })
	return &calls
}

// TestDoctorChecksTheOpenCodeVersion: doctor cannot start OpenCode to read its
// version at initialize, so it asks the binary, and reports one below the
// floor as too old, with where to upgrade (0214 PLAN P8 Deviation 1). An
// engine with no floor is not asked.
func TestDoctorChecksTheOpenCodeVersion(t *testing.T) {
	present, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, out string
		err       error
		want      []string
	}{
		{"current", "v2.2.3\n", nil, []string{"ready", present, "(v2.2.3)"}},
		{"1.x", "1.18.21\n", nil, []string{"too old:", "is 1.18.21", opencode.MinVersion + " or later", opencode.UpgradeURL}},
		{"no version", "opencode\n", nil, []string{"ready", "`--version` named no version"}},
		{"unreadable", "", errors.New("signal: killed"), []string{"ready", "could not be read: signal: killed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubEngineVersion(t, tc.out, tc.err)
			reg := provider.NewRegistry()
			reg.Register(doctorProvider{t: t, id: provider.IDOpencode, ready: true})
			reg.Register(doctorProvider{t: t, id: "alpha", ready: true})
			bins := map[provider.ID]string{provider.IDOpencode: present, "alpha": present}

			var b bytes.Buffer
			renderEngineDoctor(&b, reg, bins, engineFloors())
			out := b.String()
			var line string
			for l := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(strings.TrimSpace(l), "opencode:") {
					line = l
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("opencode's line lacks %q:\n%s", want, out)
				}
			}
			if *calls != 1 {
				t.Errorf("--version ran %d times, want once, for opencode alone:\n%s", *calls, out)
			}
		})
	}
}

// TestVersionInReadsWhatTheBinaryPrints: the version is the first SemVer word,
// with or without a "v".
func TestVersionInReadsWhatTheBinaryPrints(t *testing.T) {
	for out, want := range map[string]string{
		"v2.2.3\n": "v2.2.3", "1.18.21": "1.18.21", "opencode 2.1.0\n": "2.1.0", "": "", "opencode\n": "",
	} {
		if got, ok := versionIn(out); got != want || ok != (want != "") {
			t.Errorf("versionIn(%q) = %q, %v; want %q", out, got, ok, want)
		}
	}
}
