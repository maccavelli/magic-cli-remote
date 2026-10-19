package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
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
	renderEngineDoctor(&b, reg, bins)
	out := b.String()
	lines := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
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
