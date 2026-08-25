package ws

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
)

func TestCodexPhoneOperationRegistryIsCompleteAndTyped(t *testing.T) {
	list := codexPhoneOperationList()
	if len(list) != 4 {
		t.Fatalf("Codex-specific phone operations = %+v", list)
	}
	want := map[string]codex.CapabilityID{
		protocol.TypeCodexDoctorRun:          codex.CapabilityServerDiagnostics,
		protocol.TypeCodexRuntimeRead:        codex.CapabilityAccountRead,
		protocol.TypeCodexPermissionsWrite:   codex.CapabilityConfigBatchWrite,
		protocol.TypeSessionSetCollaboration: codex.CapabilityThreadSettings,
	}
	for _, operation := range list {
		if want[operation.Type] != operation.Capability || operation.TimeoutKey == "" {
			t.Fatalf("operation metadata = %+v", operation)
		}
		if operation.Type == protocol.TypeSessionSetCollaboration && !operation.Mutable {
			t.Fatalf("collaboration mutation marked read-only: %+v", operation)
		}
		registered := codexPhoneOperations[operation.Type]
		if registered.decode == nil || registered.authorize == nil || registered.handle == nil {
			t.Fatalf("operation lacks decoder/authorization/handler: %#v", registered)
		}
	}
}
