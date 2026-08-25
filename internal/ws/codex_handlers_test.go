package ws

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
)

func TestCodexPhoneOperationRegistryIsCompleteAndTyped(t *testing.T) {
	list := codexPhoneOperationList()
	if len(list) != 1 {
		t.Fatalf("Codex-specific phone operations = %+v", list)
	}
	operation := list[0]
	if operation.Type != protocol.TypeSessionSetCollaboration ||
		operation.Capability != codex.CapabilityThreadSettings ||
		operation.TimeoutKey == "" || !operation.Mutable {
		t.Fatalf("operation metadata = %+v", operation)
	}
	registered := codexPhoneOperations[operation.Type]
	if registered.decode == nil || registered.authorize == nil || registered.handle == nil {
		t.Fatalf("operation lacks decoder/authorization/handler: %#v", registered)
	}
}
