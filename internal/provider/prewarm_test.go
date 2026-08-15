package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

type fakeEngine struct {
	id        ID
	ensures   int
	shutdowns int
}

func (p *fakeEngine) ID() ID      { return p.id }
func (p *fakeEngine) Ready() bool { return true }
func (p *fakeEngine) Start(context.Context, StartOptions) (Session, error) {
	return nil, ErrNotImplemented
}
func (p *fakeEngine) EnsureServer() { p.ensures++ }
func (p *fakeEngine) Shutdown()     { p.shutdowns++ }

func TestControllerSetTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  kilo:\n    prewarm: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	live := &config.Live{Path: path, Cfg: &cfg}
	eng := &fakeEngine{id: IDKilo}
	reg := NewRegistry()
	reg.Register(eng)
	liveCount := 0
	c := NewController(live, reg, func(ID) int { return liveCount })

	engine, err := c.Set(context.Background(), IDKilo, true)
	if err != nil {
		t.Fatal(err)
	}
	if engine != EngineRunning || eng.ensures != 1 {
		t.Fatalf("on: engine=%s ensures=%d", engine, eng.ensures)
	}

	liveCount = 2
	engine, err = c.Set(context.Background(), IDKilo, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine != EngineStoppingWhenIdle || eng.shutdowns != 0 {
		t.Fatalf("busy off: engine=%s shutdowns=%d", engine, eng.shutdowns)
	}

	liveCount = 0
	c.OnIdle(IDKilo)
	if eng.shutdowns != 1 {
		t.Fatalf("idle latch: shutdowns=%d", eng.shutdowns)
	}

	engine, err = c.Set(context.Background(), IDKilo, false)
	if err != nil {
		t.Fatal(err)
	}
	if engine != EngineStopped || eng.shutdowns != 2 {
		t.Fatalf("idle off: engine=%s shutdowns=%d", engine, eng.shutdowns)
	}

	// Latch cleared by a subsequent true.
	liveCount = 1
	if _, err := c.Set(context.Background(), IDKilo, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Set(context.Background(), IDKilo, true); err != nil {
		t.Fatal(err)
	}
	before := eng.shutdowns
	liveCount = 0
	c.OnIdle(IDKilo)
	if eng.shutdowns != before {
		t.Fatal("latched stop should have been cleared by set true")
	}
}
