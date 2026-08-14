package ws_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

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
		{"keyring managed", credstore.ErrGooseKeyringManaged, protocol.ErrKeyringManaged},
		{"wrapped keyring managed", fmt.Errorf("goose: %w", credstore.ErrGooseKeyringManaged), protocol.ErrKeyringManaged},
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
