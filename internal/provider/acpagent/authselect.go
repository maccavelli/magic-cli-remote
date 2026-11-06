package acpagent

import (
	"errors"
	"fmt"
	"slices"
)

// ErrACPAuthRequired is returned by Start when initialize advertised no
// headless-safe auth method and session/new would hang or fail
// (MADR 0085 D7).
var ErrACPAuthRequired = errors.New("acp: authentication required")

var errUnsafeOrMissing = errors.New("acp auth method is not advertised or not headless-safe")

// selectACPAuthMethod picks a headless-safe ACP authenticate id (MADR 0085 D2):
// the pinned method, when it is advertised and safe; else the first method in
// the Spec's preference that is advertised and safe; else the Spec's key
// method, when a key exists and the method is safe and advertised. A key
// method the agent did not advertise is never sent: the protocol forbids it
// and the SDK refuses it (MADR 0234 D2, superseding 0085 D2 step 4). An
// empty methodID with a nil error means the caller must apply D7 (do not
// authenticate, do not call session/new).
func selectACPAuthMethod(advertised []string, pin string, safe, preference []string, keyMethod string, hasKey bool) (methodID string, err error) {
	if pin != "" {
		if slices.Contains(advertised, pin) && slices.Contains(safe, pin) {
			return pin, nil
		}
		return "", fmt.Errorf("acp auth method %q: %w", pin, errUnsafeOrMissing)
	}
	for _, id := range preference {
		if slices.Contains(advertised, id) && slices.Contains(safe, id) {
			return id, nil
		}
	}
	if hasKey && keyMethod != "" && slices.Contains(safe, keyMethod) && slices.Contains(advertised, keyMethod) {
		return keyMethod, nil
	}
	return "", nil
}

// rejectUnauthenticated is D7's failure. When a key was present but its
// method was not advertised, the text says so, and adds the Spec's remedy
// (MADR 0234 D3). It never carries the key.
func rejectUnauthenticated(s *session) error {
	if s == nil || !s.authRequired {
		return nil
	}
	if s.keyNotAdvertised == "" {
		return fmt.Errorf("acp: authentication required (advertised %v): %w", s.advertisedAuth, ErrACPAuthRequired)
	}
	why := fmt.Sprintf("a key is present, but the agent did not advertise %q", s.keyNotAdvertised)
	if s.keyNotAdvertisedHint != "" {
		why += ": " + s.keyNotAdvertisedHint
	}
	return fmt.Errorf("acp: authentication required (advertised %v; %s): %w", s.advertisedAuth, why, ErrACPAuthRequired)
}

func shouldKeepWarm(s *session) bool {
	return s != nil && !s.authRequired
}
