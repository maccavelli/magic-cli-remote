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

const (
	acpAuthCachedToken = "cached_token"
	acpAuthAPIKey      = "xai.api_key"
)

// selectACPAuthMethod picks a headless-safe ACP authenticate id (MADR 0085 D2).
// An empty methodID with a nil error means the caller must apply D7 (do not
// authenticate, do not call session/new).
func selectACPAuthMethod(advertised []string, pin string, safe []string, hasKey bool) (methodID string, err error) {
	if pin != "" {
		if slices.Contains(advertised, pin) && slices.Contains(safe, pin) {
			return pin, nil
		}
		return "", fmt.Errorf("acp auth method %q: %w", pin, errUnsafeOrMissing)
	}
	if slices.Contains(advertised, acpAuthCachedToken) && slices.Contains(safe, acpAuthCachedToken) {
		return acpAuthCachedToken, nil
	}
	if slices.Contains(advertised, acpAuthAPIKey) && slices.Contains(safe, acpAuthAPIKey) {
		return acpAuthAPIKey, nil
	}
	if hasKey && slices.Contains(safe, acpAuthAPIKey) {
		return acpAuthAPIKey, nil
	}
	return "", nil
}

func rejectUnauthenticated(s *session) error {
	if s == nil || !s.authRequired {
		return nil
	}
	return fmt.Errorf("acp: authentication required (advertised %v): %w", s.advertisedAuth, ErrACPAuthRequired)
}

func shouldKeepWarm(s *session) bool {
	return s != nil && !s.authRequired
}
