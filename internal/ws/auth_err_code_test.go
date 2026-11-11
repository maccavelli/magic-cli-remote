package ws_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

// TestCoordinatorSentinelsHaveTheirOwnCodes (MADR 0074 D36): each outcome of
// the credential coordinator reaches the phone as a code of its own, wrapped
// or not, never as credential_failed.
func TestCoordinatorSentinelsHaveTheirOwnCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"transaction busy", providerauth.ErrTransactionBusy, protocol.ErrProviderAuthBusy},
		{"conflict", providerauth.ErrConflict, protocol.ErrCredentialConflict},
		{"unsupported backend", providerauth.ErrUnsupportedBackend, protocol.ErrUnsupportedBackend},
		{"recovery required", providerauth.ErrRecoveryRequired, protocol.ErrRecoveryRequired},
		{"too many flows", providerauth.ErrTooManyFlows, protocol.ErrTooManySignIns},
	}
	for _, tc := range cases {
		for _, wrapped := range []bool{false, true} {
			err := tc.err
			if wrapped {
				err = fmt.Errorf("codex: %w", tc.err)
			}
			code, msg := ws.AuthErrCode(err)
			if code != tc.code {
				t.Errorf("%s (wrapped=%v): code = %q, want %q", tc.name, wrapped, code, tc.code)
			}
			if code == protocol.ErrCredentialFailed {
				t.Errorf("%s (wrapped=%v) fell through to credential_failed", tc.name, wrapped)
			}
			if msg == "" {
				t.Errorf("%s (wrapped=%v): empty message", tc.name, wrapped)
			}
		}
	}
}

// TestAuthErrCodeTable pins the MADR 0083 D5 taxonomy: each known failure
// class gets its own wire code so the phone can say what to do next, and only
// the true residual falls through to credential_failed.
func TestAuthErrCodeTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"busy", provider.ErrAuthBusy, protocol.ErrProviderBusy},
		{"unsupported provider", provider.ErrAuthUnsupported, "unsupported"},
		{"confirm required", provider.ErrAuthConfirmRequired, protocol.ErrConfirmRequired},
		{"method unsupported", provider.ErrAuthMethodUnsupported, protocol.ErrMethodUnsupported},
		{"empty secret", credstore.ErrEmptySecret, protocol.ErrInvalidKey},
		{"oversized secret", credstore.ErrSecretTooLarge, protocol.ErrInvalidKey},
		{"engine timeout", fmt.Errorf("PUT /auth: %w", context.DeadlineExceeded), protocol.ErrEngineUnavailable},
		{"not accepted", provider.ErrCredentialNotAccepted, protocol.ErrCredentialNotAccepted},
		{"wrapped not accepted", fmt.Errorf("togetherai: %w", provider.ErrCredentialNotAccepted), protocol.ErrCredentialNotAccepted},
		{"residual", errors.New("engine said 500"), protocol.ErrCredentialFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, msg := ws.AuthErrCode(tc.err)
			if code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
			if msg == "" {
				t.Fatal("empty message")
			}
		})
	}
}
