package httpagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Connected-set cache and verify ladder (MADR 0086 D13).
//
// Layer 0 is this in-process cache plus a 32-slot mutation ring.
// Layer 1 is GET /config/providers (ids only).
// Layer 3 is GET /provider streamed for `connected` only, and only when
// Layer 1 and the write disagree.

const (
	connectedTTL    = 20 * time.Second
	negativeTTL     = 10 * time.Second
	mutationRingCap = 32
	layer1RetryWait = 150 * time.Millisecond
)

type connectedCache struct {
	ids       map[string]struct{}
	gen       uint64
	source    string
	fetchedAt time.Time
	negUntil  map[string]time.Time
}

type credMutation struct {
	seq      uint64
	op       string
	upstream string
	at       time.Time
}

// ConnectedSnapshot is a point-in-time view of Layer 0 (MADR 0086 D5/D13).
type ConnectedSnapshot struct {
	IDs   map[string]struct{}
	Gen   uint64
	Seq   uint64
	Fresh bool
}

func cloneIDSet(in map[string]struct{}) map[string]struct{} {
	if in == nil {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func (p *Provider) connectedLock() {
	p.connectedMu.Lock()
}

func (p *Provider) connectedUnlock() {
	p.connectedMu.Unlock()
}

// Snapshot returns Layer 0 without I/O.
func (p *Provider) Snapshot() ConnectedSnapshot {
	p.connectedLock()
	defer p.connectedUnlock()
	return ConnectedSnapshot{
		IDs:   cloneIDSet(p.connected.ids),
		Gen:   p.connected.gen,
		Seq:   p.mutSeq,
		Fresh: !p.connected.fetchedAt.IsZero() && time.Since(p.connected.fetchedAt) < connectedTTL,
	}
}

// InvalidateConnected expires the connected-set cache so the next read
// refreshes Layer 1. Writes call this together with Note.
func (p *Provider) InvalidateConnected() {
	p.connectedLock()
	defer p.connectedUnlock()
	p.connected.fetchedAt = time.Time{}
}

// Note records a credential mutation on the ring and applies an optimistic
// membership change. It is not D1 success by itself.
func (p *Provider) Note(op, upstream string) {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return
	}
	p.connectedLock()
	defer p.connectedUnlock()
	p.mutSeq++
	p.mutations[p.mutHead] = credMutation{
		seq: p.mutSeq, op: op, upstream: upstream, at: time.Now(),
	}
	p.mutHead = (p.mutHead + 1) % mutationRingCap
	if p.connected.ids == nil {
		p.connected.ids = map[string]struct{}{}
	}
	switch op {
	case "set", "device":
		p.connected.ids[upstream] = struct{}{}
		delete(p.connected.negUntil, upstream)
	case "clear":
		delete(p.connected.ids, upstream)
	}
	p.connected.gen++
}

// Remember replaces the cached connected set from a Layer 1 or Layer 3 fetch.
func (p *Provider) Remember(ids map[string]struct{}, source string) {
	p.connectedLock()
	defer p.connectedUnlock()
	p.connected.ids = cloneIDSet(ids)
	p.connected.source = source
	p.connected.fetchedAt = time.Now()
	p.connected.gen++
}

func (p *Provider) rememberNegative(id string) {
	p.connectedLock()
	defer p.connectedUnlock()
	if p.connected.negUntil == nil {
		p.connected.negUntil = map[string]time.Time{}
	}
	p.connected.negUntil[id] = time.Now().Add(negativeTTL)
}

func (p *Provider) negativeFresh(id string) bool {
	p.connectedLock()
	defer p.connectedUnlock()
	until, ok := p.connected.negUntil[id]
	return ok && time.Now().Before(until)
}

// MutationRingLen reports how many ring slots are occupied. Tests use it
// to pin wrap-around.
func (p *Provider) MutationRingLen() int {
	p.connectedLock()
	defer p.connectedUnlock()
	n := 0
	for _, m := range p.mutations {
		if m.seq != 0 {
			n++
		}
	}
	return n
}

// FetchConfigProviderIDs is GET /config/providers decoded to ids only
// (MADR 0086 D13 Layer 1). The struct must not declare Key (0043 D4).
func FetchConfigProviderIDs(ctx context.Context, api API) (map[string]struct{}, error) {
	var resp struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := api(ctx, "GET", "/config/providers", nil, &resp); err != nil {
		return nil, fmt.Errorf("config providers: %w", err)
	}
	out := make(map[string]struct{}, len(resp.Providers))
	for _, p := range resp.Providers {
		if id := strings.TrimSpace(p.ID); id != "" {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

// FetchProviderConnectedIDs is GET /provider decoded for the `connected`
// field only (MADR 0086 D13 Layer 3). encoding/json discards `all` and any
// `key` field. This still transfers ~4.7 MB; call it only on a dispute.
func FetchProviderConnectedIDs(ctx context.Context, api API) (map[string]struct{}, error) {
	var resp struct {
		Connected []string          `json:"connected"`
		Default   map[string]string `json:"default"`
	}
	if err := api(ctx, "GET", "/provider", nil, &resp); err != nil {
		return nil, fmt.Errorf("provider connected: %w", err)
	}
	out := make(map[string]struct{}, len(resp.Connected))
	for _, id := range resp.Connected {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

// VerifyUpstreamConnected runs the D13 ladder: Layer 1 first, Layer 3 only
// when Layer 1 does not contain the id. A fresh negative cache skips Layer 3.
func (p *Provider) VerifyUpstreamConnected(ctx context.Context, api API, upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return fmt.Errorf("verify credential: upstream id required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	ids, err := FetchConfigProviderIDs(ctx, api)
	if err != nil {
		return fmt.Errorf("verify %s: %w", upstreamID, err)
	}
	if _, ok := ids[upstreamID]; !ok {
		// One short retry: some engines lag one tick after PUT.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(layer1RetryWait):
		}
		ids, err = FetchConfigProviderIDs(ctx, api)
		if err != nil {
			return fmt.Errorf("verify %s: %w", upstreamID, err)
		}
	}
	p.Remember(ids, "config")
	if _, ok := ids[upstreamID]; ok {
		return nil
	}

	if p.negativeFresh(upstreamID) {
		return fmt.Errorf("upstream %s: %w", upstreamID, provider.ErrCredentialNotAccepted)
	}

	full, err := p.fetchProviderConnectedSingleFlight(ctx, api)
	if err != nil {
		return err
	}
	p.Remember(full, "provider")
	if _, ok := full[upstreamID]; ok {
		return nil
	}
	p.rememberNegative(upstreamID)
	return fmt.Errorf("upstream %s: %w", upstreamID, provider.ErrCredentialNotAccepted)
}

func (p *Provider) fetchProviderConnectedSingleFlight(ctx context.Context, api API) (map[string]struct{}, error) {
	p.sfMu.Lock()
	if p.sfCh != nil {
		ch := p.sfCh
		p.sfMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ch:
			p.sfMu.Lock()
			ids, err := cloneIDSet(p.sfIDs), p.sfErr
			p.sfMu.Unlock()
			return ids, err
		}
	}
	ch := make(chan struct{})
	p.sfCh = ch
	p.sfMu.Unlock()

	ids, err := FetchProviderConnectedIDs(ctx, api)

	p.sfMu.Lock()
	p.sfIDs, p.sfErr = ids, err
	close(ch)
	p.sfCh = nil
	p.sfMu.Unlock()
	return cloneIDSet(ids), err
}

func (p *Provider) afterCredentialWrite(ctx context.Context, api API, upstreamID string, compensate func(context.Context) error) error {
	p.Note("set", upstreamID)
	p.InvalidateAuthCatalog()
	p.InvalidateConnected()
	if err := p.VerifyUpstreamConnected(ctx, api, upstreamID); err != nil {
		if compensate != nil && errors.Is(err, provider.ErrCredentialNotAccepted) {
			_ = compensate(ctx)
			p.Note("clear", upstreamID)
		}
		return err
	}
	return nil
}
