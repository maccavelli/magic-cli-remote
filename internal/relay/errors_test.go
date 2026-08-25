package relay

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrCode pins the frozen sentinel→wire-code mapping (0115 P1). These
// strings are protocol surface: a phone or host shipped today matches on
// them, so a change here is a wire change, not a refactor.
func TestErrCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errLimit, "limit"},
		{errHostOffline, "host_offline"},
		{errUnknownSession, "unknown_session"},
		{errUnauthorized, "unauthorized"},
		{errInternal, "internal"},
		// Wrapped sentinels still map (errors.Is, not equality).
		{fmt.Errorf("beginJoin: %w", errLimit), "limit"},
		// Anything unrecognized degrades to internal, never leaks its text.
		{errors.New("crypto/rand: entropy pool sad"), "internal"},
		{nil, "internal"},
	}
	for _, c := range cases {
		if got := errCode(c.err); got != c.want {
			t.Errorf("errCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
