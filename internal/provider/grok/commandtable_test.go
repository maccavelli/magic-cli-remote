package grok

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
)

// T-F1: grok /review forwards the advertised skill (MADR 0081 P3.12).
func TestGrokReviewIsNativeWhenAdvertised(t *testing.T) {
	m := commandTable["review"]
	if m.Kind != command.KindNative || m.Native != "review" {
		t.Fatalf("review=%+v, want KindNative native=review", m)
	}
}

func TestGrokForkIsOpFork(t *testing.T) {
	m := commandTable["fork"]
	if m.Kind != command.KindOp || m.Op != command.OpFork {
		t.Fatalf("fork=%+v, want KindOp OpFork", m)
	}
}
