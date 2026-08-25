package relay

import "errors"

// Join-plane failure sentinels (0115 F8). The wire code for each is the
// string in errCode — frozen; changing one is a protocol change.
var (
	errLimit          = errors.New("limit")
	errHostOffline    = errors.New("host_offline")
	errUnknownSession = errors.New("unknown_session")
	errUnauthorized   = errors.New("unauthorized")
	errInternal       = errors.New("internal")
)

// errCode maps a hub error to its frozen wire code.
func errCode(err error) string {
	switch {
	case errors.Is(err, errLimit):
		return "limit"
	case errors.Is(err, errHostOffline):
		return "host_offline"
	case errors.Is(err, errUnknownSession):
		return "unknown_session"
	case errors.Is(err, errUnauthorized):
		return "unauthorized"
	default:
		return "internal"
	}
}
