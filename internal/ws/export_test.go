package ws

import "time"

// AuthErrCode exposes the provider-auth error mapping to the external test
// package (MADR 0083 D5).
var (
	AuthErrCode         = authErrCode
	UpstreamAuthPayload = upstreamAuthPayload
)

// SeedIdemCompleted marks (deviceID, requestID) as finished in the
// idempotency ledger with no captured response frame — the state a handler
// leaves when it succeeds without writing anything. A later request reusing
// that id then takes dispatchAsync's empty-replay path (MADR 0095 D6).
func (s *Server) SeedIdemCompleted(deviceID, requestID string) {
	s.idem.complete(deviceID, requestID, nil)
}

// SetCancelBoundForTest shortens how long session.cancel may hold the read
// loop (MADR 0201 D10) and returns a func that restores it.
func SetCancelBoundForTest(d time.Duration) func() {
	prev := cancelBound
	cancelBound = d
	return func() { cancelBound = prev }
}
