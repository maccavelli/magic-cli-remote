package ws

import (
	"encoding/json"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

func TestCodexSurfaceNegotiationIsAdditiveAndSorted(t *testing.T) {
	c := &client{negotiated: protocol.V2, codexSurfaceVersion: 1}
	s := &Server{}
	caps := s.capsFor(c)
	if caps.CodexSurface == nil || caps.CodexSurface.Version != 1 {
		t.Fatalf("codex surface = %+v", caps.CodexSurface)
	}
	if !sortedStrings(caps.CodexSurface.Operations) || !sortedStrings(caps.CodexSurface.Experimental) {
		t.Fatalf("surface lists are not sorted: %+v", caps.CodexSurface)
	}
	if caps.CodexSurface.MaxPageSize != 100 || caps.CodexSurface.MaxTextBytes != 262144 || caps.CodexSurface.MaxBinaryChunkBytes != 262144 {
		t.Fatalf("surface bounds = %+v", caps.CodexSurface)
	}
	old := &client{negotiated: protocol.V2}
	if got := s.capsFor(old).CodexSurface; got != nil {
		t.Fatalf("old client received codex surface: %+v", got)
	}
}

func TestCodexSurfaceOfferIsOptionalOnAuthAndPairing(t *testing.T) {
	var oldAuth protocol.AuthPayload
	if err := json.Unmarshal([]byte(`{"token":"x","protocols":[1,2]}`), &oldAuth); err != nil || oldAuth.CodexSurfaceVersion != 0 {
		t.Fatalf("old auth = %+v err=%v", oldAuth, err)
	}
	var auth protocol.AuthPayload
	if err := json.Unmarshal([]byte(`{"token":"x","protocols":[1,2],"codex_surface_version":1}`), &auth); err != nil || auth.CodexSurfaceVersion != 1 {
		t.Fatalf("surface auth = %+v err=%v", auth, err)
	}
	var claim protocol.PairClaimPayload
	if err := json.Unmarshal([]byte(`{"code":"12345678","protocols":[1,2],"codex_surface_version":1}`), &claim); err != nil || claim.CodexSurfaceVersion != 1 {
		t.Fatalf("surface claim = %+v err=%v", claim, err)
	}
}

func sortedStrings(in []string) bool {
	for i := 1; i < len(in); i++ {
		if in[i-1] > in[i] {
			return false
		}
	}
	return true
}
