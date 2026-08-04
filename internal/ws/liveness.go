package ws

import (
	"time"

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
	// ReadDeadline. False until 0068 P1 wires the server pinger — the
	// capability block must never advertise behaviour ahead of it existing.
	WSPingResetsDeadline bool
}

// livenessSpec derives the spec from the server's live configuration.
func (s *Server) livenessSpec() LivenessSpec {
	return LivenessSpec{
		ReadDeadline:         s.readDeadline,
		PingInterval:         expectedPingInterval,
		WSPingResetsDeadline: false,
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
