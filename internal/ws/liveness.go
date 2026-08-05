package ws

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// expectedPingInterval is the app-level ping cadence expected of clients —
// the number 0063 locked (10 s resets the 60 s read deadline with margin
// for two misses). Not enforced server-side; advertised so v2 clients stop
// hardcoding it.
const expectedPingInterval = 10 * time.Second

// LivenessSpec is the single source of truth for the connection-liveness
// numbers (MADR 0068 D1/D2): the enforcement paths and the advertised v2
// capability block are both built from it, so advertised == enforced by
// construction.
type LivenessSpec struct {
	// ReadDeadline is the rolling inbound-data deadline (0063): only
	// inbound data frames reset it in v1.
	ReadDeadline time.Duration
	// PingInterval is the app-level ping cadence expected of clients.
	PingInterval time.Duration
	// WSPingResetsDeadline reports whether transport-level pongs also reset
	// ReadDeadline (0068 P1: they do, for v2 connections). The capability
	// block must never advertise behaviour ahead of it existing.
	WSPingResetsDeadline bool
}

// livenessSpec derives the spec from the server's live configuration.
func (s *Server) livenessSpec() LivenessSpec {
	return LivenessSpec{
		ReadDeadline: s.readDeadline,
		PingInterval: expectedPingInterval,
		// True since 0068 P1: pingLoop + readHorizon make transport pongs
		// extend the deadline for v2 connections.
		WSPingResetsDeadline: true,
	}
}

// horizon is the moment a v2 connection dies without further liveness:
// max(lastData, lastPong) + ReadDeadline (0068 P1). Both marks are
// atomics — the read loop and pingLoop write them from different
// goroutines.
func (s *Server) horizon(c *client) time.Time {
	last := c.lastData.Load()
	if lp := c.lastPong.Load(); lp > last {
		last = lp
	}
	return time.Unix(0, last).Add(s.readDeadline)
}

// startV2Liveness begins the pinger + deadline watchdog pair for a
// connection that negotiated v2. Idempotent via pingerOnce (auth and
// pair.claim can both negotiate on one connection).
func (s *Server) startV2Liveness(c *client) {
	c.pingerOnce.Do(func() {
		c.lastData.Store(time.Now().UnixNano())
		go s.pingLoop(c)
		go s.deadlineWatchdog(c)
	})
}

// pingLoop keeps a v2 connection's horizon honest from the server side
// (0068 P1): a completed transport pong proves the peer's WS stack is
// reachable even when the app is quiet. Exits with the connection; a
// failed ping is left for the watchdog to reap — closing here would race
// the read loop.
func (s *Server) pingLoop(c *client) {
	interval := s.readDeadline / 3
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-c.lifeCtx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(c.lifeCtx, interval)
		err := c.conn.Ping(pctx)
		cancel()
		if err != nil {
			return
		}
		c.lastPong.Store(time.Now().UnixNano())
	}
}

// deadlineWatchdog owns the v2 reap (0068 P1): it closes the connection
// when the horizon passes with neither data nor pongs. It, not the read
// context, must make this call — coder/websocket closes the connection on
// read-ctx expiry, which would ignore a pong that landed while a read was
// blocked.
func (s *Server) deadlineWatchdog(c *client) {
	for {
		now := time.Now()
		h := s.horizon(c)
		if !h.After(now) {
			c.deadlineReaped.Store(true)
			_ = c.conn.Close(websocket.StatusPolicyViolation, "read_deadline")
			return
		}
		select {
		case <-c.closed:
			return
		case <-c.lifeCtx.Done():
			return
		case <-time.After(h.Sub(now)):
		}
	}
}

// caps renders the spec as the wire capability block for one connection.
// Resume stays nil until 0068 P4 implements it.
func (l LivenessSpec) caps(tlsResumed bool) *protocol.Caps {
	return &protocol.Caps{
		Protocol:             protocol.V2,
		ReadDeadlineMS:       l.ReadDeadline.Milliseconds(),
		PingIntervalMS:       l.PingInterval.Milliseconds(),
		WSPingResetsDeadline: l.WSPingResetsDeadline,
		HistoryRing:          session.HistoryRingCap,
		MaxFrameBytes:        maxOutboundFrameBytes,
		TLSResumed:           tlsResumed,
	}
}

// capsFor is the per-connection capability block: the liveness spec plus
// the seq-lineage epoch when a session store exists (MADR 0068 P3).
func (s *Server) capsFor(c *client) *protocol.Caps {
	caps := s.livenessSpec().caps(c.tlsResumed)
	if s.sessions != nil {
		caps.Epoch = s.sessions.Epoch()
	}
	return caps
}
