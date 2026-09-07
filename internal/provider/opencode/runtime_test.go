package opencode

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// TestOpencodeDeliberatelyHasNoRuntimeDialect.
//
// opencode publishes no account usage: its 162 documented paths carry no
// provider-usage, billing or quota endpoint. The only thing resembling engine
// status is the engine's experimental capabilities route, and A11
// (surface_contract_test.go) forbids this package from calling an experimental
// endpoint — including naming one here, since the guard is a substring scan
// over the package's own source.
//
// So it implements no RuntimeDialect on purpose, and httpagent's generic path
// answers /status with the session's own model and agent plus an explicit
// statement that this engine publishes no plan usage. That is a different thing
// from /status being broken, and only one of them tells the operator to stop
// looking.
//
// This test exists so the absence reads as a decision rather than an omission
// (MADR 0138 Phase 11 deviation).
func TestOpencodeDeliberatelyHasNoRuntimeDialect(t *testing.T) {
	var d any = &httpDialect{}
	if _, ok := d.(httpagent.RuntimeDialect); ok {
		t.Fatal("opencode's dialect implements httpagent.RuntimeDialect. If that is now " +
			"intended, check it reaches no experimental endpoint (A11 forbids it) and not " +
			"/config, which carries plaintext provider apiKey values")
	}
}
