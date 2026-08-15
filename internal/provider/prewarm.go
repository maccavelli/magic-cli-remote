package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// Engine states returned by Controller.Set (MADR 0089 D7). Same strings as
// protocol.Engine* — kept here so this package does not import protocol.
const (
	EngineRunning          = "running"
	EngineStopped          = "stopped"
	EngineStoppingWhenIdle = "stopping_when_idle"
)

// ErrEngineNotStartable means the flag was persisted but no engine could be
// pre-warmed — the agent is not enabled on this daemon, so there is nothing to
// boot. Wrapped by Set so callers classify the failure without re-probing the
// registry themselves.
var ErrEngineNotStartable = errors.New("no engine to pre-warm")

// Controller applies a prewarm flag to a running engine (MADR 0089 D7).
type Controller struct {
	mu           sync.Mutex
	Live         *config.Live
	Registry     *Registry
	LiveCount    func(ID) int
	stopWhenIdle map[ID]bool
}

// NewController builds a controller. LiveCount may be nil (treated as 0).
func NewController(live *config.Live, reg *Registry, liveCount func(ID) int) *Controller {
	if liveCount == nil {
		liveCount = func(ID) int { return 0 }
	}
	return &Controller{
		Live:         live,
		Registry:     reg,
		LiveCount:    liveCount,
		stopWhenIdle: make(map[ID]bool),
	}
}

// Set persists (via Live) and starts or latches-stops the engine.
func (c *Controller) Set(_ context.Context, id ID, on bool) (engine string, err error) {
	if c == nil || c.Live == nil {
		return "", fmt.Errorf("prewarm controller not configured")
	}
	sid := string(id)
	if !config.KnownProvider(sid) {
		return "", fmt.Errorf("%w: %s", config.ErrUnknownProvider, sid)
	}
	if err := c.Live.SetPrewarm(sid, on); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		delete(c.stopWhenIdle, id)
		if err := startEngine(c.Registry, id); err != nil {
			return "", err
		}
		return EngineRunning, nil
	}
	n := c.LiveCount(id)
	if n > 0 {
		c.stopWhenIdle[id] = true
		return EngineStoppingWhenIdle, nil
	}
	delete(c.stopWhenIdle, id)
	stopEngine(c.Registry, id)
	return EngineStopped, nil
}

// OnIdle honours a stop-when-idle latch after the last session of id closes.
func (c *Controller) OnIdle(id ID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopWhenIdle[id] {
		return
	}
	if c.LiveCount(id) > 0 {
		return
	}
	delete(c.stopWhenIdle, id)
	stopEngine(c.Registry, id)
}

// Current is the live in-memory prewarm flag.
func (c *Controller) Current(id ID) (prewarm bool, ok bool) {
	if c == nil || c.Live == nil {
		return false, false
	}
	return c.Live.GetPrewarm(string(id))
}

func startEngine(reg *Registry, id ID) error {
	if reg == nil {
		return fmt.Errorf("%w: no registry", ErrEngineNotStartable)
	}
	p, err := reg.Get(id)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrEngineNotStartable, id, err)
	}
	switch e := p.(type) {
	case interface{ EnsureServer() }:
		e.EnsureServer()
	case interface{ EnsureWarm() }:
		e.EnsureWarm()
	}
	return nil
}

func stopEngine(reg *Registry, id ID) {
	if reg == nil {
		return
	}
	p, err := reg.Get(id)
	if err != nil {
		return
	}
	if e, ok := p.(interface{ Shutdown() }); ok {
		e.Shutdown()
	}
}
