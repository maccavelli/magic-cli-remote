//go:build live_grok

package acpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Run with: go test -tags live_grok ./internal/provider/acpagent/ -run LiveGrokRewindPoints -count=1
//
// MADR 0138 Phase 9, acceptance 6. The shapes in rewind.go were transcribed
// from grok's source; this is the only thing that can say whether the installed
// binary agrees with it.
//
// **It spends no model tokens.** It starts a session and asks for the rewind
// points — a read of local snapshot state — and never prompts. It still needs a
// live grok, so it is tagged rather than run by default.
//
// The decode is strict on purpose: an ordinary decode of a renamed or restructured
// response yields zero values and no error, which is exactly the failure mode
// this acceptance exists to rule out. DisallowUnknownFields turns a shape drift
// into a named failure.
func TestLiveGrokRewindPointsDecodeStrictly(t *testing.T) {
	p := New(Spec{
		ID:         provider.IDGrok,
		DefaultBin: "grok",
		DefaultArgs: func(Config) []string {
			return []string{"--no-auto-update", "agent", "--no-leader", "stdio"}
		},
	}, Config{})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sess, err := p.Start(ctx, provider.StartOptions{Name: "rewind-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start grok: %v", err)
	}
	defer sess.Close(context.Background())

	s, ok := sess.(*session)
	if !ok {
		t.Fatalf("grok session is %T, not *session", sess)
	}
	if _, ok := sess.(provider.UndoSession); !ok {
		t.Fatal("the live grok session does not satisfy provider.UndoSession")
	}

	var raw json.RawMessage
	if err := callAgentExtension(ctx, s, "x.ai/rewind/points",
		rewindPointsRequest{SessionID: s.AgentSessionID()}, &raw); err != nil {
		if errors.Is(err, provider.ErrNotImplemented) {
			t.Fatalf("this grok build has no _x.ai/rewind/points; Phase 9's undo cannot work on it: %v", err)
		}
		t.Fatalf("_x.ai/rewind/points: %v", err)
	}
	t.Logf("_x.ai/rewind/points returned: %s", raw)

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var points rewindPointsResponse
	if err := dec.Decode(&points); err != nil {
		t.Fatalf("the response does not match rewindPointsResponse — the transcribed shape has "+
			"drifted from the installed binary: %v", err)
	}

	// A fresh session has no points yet, so an empty array is the expected
	// result and is not a failure. What matters is that the field decoded and
	// the entries, if any, are the shape rewind.go expects.
	for i, pt := range points.RewindPoints {
		if pt.CreatedAt == "" {
			t.Errorf("rewind point %d decoded with an empty created_at; check the casing", i)
		}
	}
	t.Logf("decoded %d rewind points strictly", len(points.RewindPoints))
}
