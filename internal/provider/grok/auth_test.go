package grok

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0083 D2: the key write serves exactly one method; anything else
// refuses rather than writing the config key for a device sign-in.
func TestSetCredentialGuardsMethodID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, m := range []string{"xai:device", "openai:api"} {
		if err := setCredential(context.Background(), "xai", m, "sk-x", nil); !errors.Is(err, provider.ErrAuthMethodUnsupported) {
			t.Fatalf("method %q: err = %v, want ErrAuthMethodUnsupported", m, err)
		}
	}
}
