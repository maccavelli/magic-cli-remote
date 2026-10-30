package session

import (
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// errProviderClosed stands for a provider's own error once its session is
// closed: a plain error, which is what every provider returns (MADR 0218 F4).
var errProviderClosed = errors.New("session closed")

// numberedSession is a provider.Session whose pointers are distinct, so an
// entry can hold a different session from the one a call went to.
type numberedSession struct {
	stubSession
	n int
}

// TestProviderErr pins MADR 0218 D1: a provider's error becomes ErrNotLive
// exactly when the session the call went to is no longer the id's live
// session, and the provider's error stays reachable through errors.Is.
func TestProviderErr(t *testing.T) {
	called := &numberedSession{n: 1}
	other := &numberedSession{n: 2}
	cases := []struct {
		name    string
		entry   *entry // nil: the id has no entry
		err     error
		notLive bool
	}{
		{name: "removed", err: errProviderClosed, notLive: true},
		{name: "replaced", entry: &entry{sess: other}, err: errProviderClosed, notLive: true},
		{name: "dead", entry: &entry{sess: called, dead: true}, err: errProviderClosed, notLive: true},
		{name: "live", entry: &entry{sess: called}, err: provider.ErrTurnBusy},
		{name: "nil error", entry: &entry{sess: called}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manager{sessions: map[string]*entry{}}
			if tc.entry != nil {
				m.sessions["s1"] = tc.entry
			}
			got := m.providerErr("s1", called, tc.err)
			switch {
			case tc.err == nil:
				if got != nil {
					t.Fatalf("providerErr(nil) = %v, want nil", got)
				}
			case tc.notLive:
				if !errors.Is(got, ErrNotLive) {
					t.Fatalf("got %v, want ErrNotLive", got)
				}
				if !errors.Is(got, tc.err) {
					t.Fatalf("got %v, which no longer wraps the provider's error %v", got, tc.err)
				}
			default:
				if got != tc.err {
					t.Fatalf("got %v, want the provider's error unchanged (%v)", got, tc.err)
				}
			}
		})
	}
}
