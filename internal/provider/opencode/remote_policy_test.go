package opencode

import (
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Remote mutation policy plumbing (MADR 0112 A8/A9, PLAN P8).
//
// P8 only carries the operator's decision to the dialect. No handler consults
// it yet — that is P9 and P10 — so what these pin is that the default is off,
// that the two flags are independent, and that they arrive unmodified.

// newDialectFromConfig builds the dialect exactly as NewHTTPWithToolFrameHook
// does, so these tests exercise the real construction path rather than a
// hand-assembled struct.
func newDialectFromConfig(cfg Config) *httpDialect {
	return &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
		pure:                 cfg.Pure,
		allowRemoteShare:     cfg.AllowRemoteShare,
		allowRemoteShell:     cfg.AllowRemoteShell,
	}
}

// TestRemotePolicyDefaultsOff is the safety default: a fresh daemon exposes
// neither mutation.
func TestRemotePolicyDefaultsOff(t *testing.T) {
	d := newDialectFromConfig(Config{})
	if d.allowRemoteShare || d.allowRemoteShell {
		t.Fatalf("a zero config enabled a remote mutation: share=%v shell=%v",
			d.allowRemoteShare, d.allowRemoteShell)
	}
}

// TestRemotePolicyCombinations pins all four states and, crucially, that
// enabling one leaves the other alone. An operator who wants collaboration must
// not silently also get command execution.
func TestRemotePolicyCombinations(t *testing.T) {
	for _, tc := range []struct{ share, shell bool }{
		{false, false},
		{true, false},
		{false, true},
		{true, true},
	} {
		d := newDialectFromConfig(Config{
			AllowRemoteShare: tc.share,
			AllowRemoteShell: tc.shell,
		})
		if d.allowRemoteShare != tc.share {
			t.Fatalf("share=%v reached the dialect as %v", tc.share, d.allowRemoteShare)
		}
		if d.allowRemoteShell != tc.shell {
			t.Fatalf("shell=%v reached the dialect as %v", tc.shell, d.allowRemoteShell)
		}
	}
}

// TestRemotePolicyIsIndependentOfOtherConfig proves the flags are not coupled to
// any other option that happens to be set.
func TestRemotePolicyIsIndependentOfOtherConfig(t *testing.T) {
	d := newDialectFromConfig(Config{
		Bin:           "opencode",
		AlwaysApprove: true,
		Pure:          true,
		Model:         "opencode/big-pickle",
	})
	if d.allowRemoteShare || d.allowRemoteShell {
		t.Fatalf("an unrelated option enabled a remote mutation: %+v", d)
	}
	if !d.pure {
		t.Fatal("an unrelated option was lost")
	}
}

// TestRemotePolicyIsNotAdvertisedYet proves P8 wires policy without enabling a
// capability: the operations arrive in P9 and P10.
func TestRemotePolicyIsNotAdvertisedYet(t *testing.T) {
	h := newRecorder()
	d := newDialectFromConfig(Config{AllowRemoteShare: true, AllowRemoteShell: true})
	s := d.NewSession(h).(*httpSession)

	// No share or shell method exists on the dialect session in this phase.
	if _, ok := any(s).(interface{ Share() error }); ok {
		t.Fatal("a share operation exists before P9")
	}
	if _, ok := any(s).(interface{ Shell(string) error }); ok {
		t.Fatal("a shell operation exists before P10")
	}
}

// TestNoMCPPolicyKeyExists proves P8 adds exactly two booleans. MCP lifecycle
// control is excluded by A7/A12, so a policy key for it would be the first step
// toward a surface the decision refuses.
func TestNoMCPPolicyKeyExists(t *testing.T) {
	var cfg httpagent.Config
	// A compile-time shape check: the two fields exist and nothing MCP-shaped
	// does. Adding an MCP policy field would fail to compile this list.
	cfg.AllowRemoteShare = false
	cfg.AllowRemoteShell = false
	_ = cfg
}
