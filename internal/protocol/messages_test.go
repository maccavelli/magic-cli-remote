package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := protocol.NewEnvelope(protocol.TypeAuth, "1", protocol.AuthPayload{Token: "mcr_x"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Envelope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != protocol.TypeAuth || got.ID != "1" || got.V != protocol.Version {
		t.Fatalf("got=%+v", got)
	}
	var p protocol.AuthPayload
	if err := protocol.DecodePayload(got, &p); err != nil {
		t.Fatal(err)
	}
	if p.Token != "mcr_x" {
		t.Fatalf("token=%q", p.Token)
	}
}

func TestSessionForkPayloadLastTurnID(t *testing.T) {
	raw := []byte(`{"session_id":"s1","last_turn_id":"turn-9"}`)
	var p protocol.SessionForkPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != "s1" || p.LastTurnID != "turn-9" || p.MessageID != "" {
		t.Fatalf("%+v", p)
	}
}

func TestSessionDiffResultEnvelopeUnder1MiB(t *testing.T) {
	// 256 KiB clipped patch plus metadata must stay well under the 1 MiB
	// outbound WebSocket cap (MADR 0080 D15).
	const maxOutbound = 1 << 20
	body := strings.Repeat("diff --git a/x b/x\n", 20000)
	env, err := protocol.NewEnvelope(protocol.TypeSessionDiffResult, "d1", protocol.SessionDiffResultPayload{
		SessionID: "s1",
		Summary:   body,
		BaseSHA:   strings.Repeat("a", 64),
		Scope:     "working_tree",
		Truncated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= maxOutbound {
		t.Fatalf("envelope %d bytes exceeds %d", len(b), maxOutbound)
	}
}

// ---------------------------------------------------------------------------
// MADR 0112 A1 — additive discovery fields and projects.list
// ---------------------------------------------------------------------------

// The new AgentSessionMeta fields are additive: an old daemon's payload must
// still decode, and a payload with none of them must not serialize empty keys
// that an old client would have to ignore.
func TestAgentSessionMetaAdditiveFieldsAreBackwardCompatible(t *testing.T) {
	t.Run("legacy payload decodes", func(t *testing.T) {
		const legacy = `{"id":"ses_1","cwd":"/w","title":"t","updated_at":"2026-07-26T20:52:14Z"}`
		var got provider.AgentSessionMeta
		if err := json.Unmarshal([]byte(legacy), &got); err != nil {
			t.Fatalf("legacy payload must still decode: %v", err)
		}
		if got.ID != "ses_1" || got.CWD != "/w" || got.Title != "t" {
			t.Errorf("legacy fields lost: %+v", got)
		}
		if got.ModelID != "" || got.Agent != "" || got.ThinkingLevel != "" || got.Aggregate != nil {
			t.Errorf("absent additive fields must stay zero: %+v", got)
		}
	})

	t.Run("empty additive fields are omitted", func(t *testing.T) {
		b, err := json.Marshal(provider.AgentSessionMeta{ID: "ses_1"})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"model_id", "thinking_level", "agent", "aggregate"} {
			if strings.Contains(string(b), key) {
				t.Errorf("payload %s should omit empty %q", b, key)
			}
		}
	})

	t.Run("populated fields round-trip", func(t *testing.T) {
		cost := 1.25
		want := provider.AgentSessionMeta{
			ID: "ses_1", CWD: "/w", Title: "t",
			UpdatedAt:     time.Date(2026, 7, 26, 20, 52, 14, 0, time.UTC),
			ModelID:       "opencode/big-pickle",
			ThinkingLevel: "high",
			Agent:         "build",
			Aggregate: &provider.AgentSessionUsage{
				Input: 10, Output: 20, Reasoning: 1,
				CacheRead: 5, CacheWrite: 2, CostUSD: &cost,
			},
		}
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var got provider.AgentSessionMeta
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.ModelID != want.ModelID || got.ThinkingLevel != want.ThinkingLevel || got.Agent != want.Agent {
			t.Errorf("round-trip lost fields: %+v", got)
		}
		if got.Aggregate == nil || *got.Aggregate.CostUSD != cost {
			t.Errorf("aggregate round-trip failed: %+v", got.Aggregate)
		}
	})
}

// A free session and a session with unknown cost must stay distinguishable
// across the wire: nil aggregate means "not reported", a present zero means
// "known free".
func TestAgentSessionUsageDistinguishesFreeFromUnknown(t *testing.T) {
	zero := 0.0
	free, err := json.Marshal(provider.AgentSessionMeta{
		ID: "a", Aggregate: &provider.AgentSessionUsage{CostUSD: &zero},
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := json.Marshal(provider.AgentSessionMeta{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if string(free) == string(unknown) {
		t.Fatal("a known-free session and an unreported one must not serialize identically")
	}

	var decodedFree provider.AgentSessionMeta
	if err := json.Unmarshal(free, &decodedFree); err != nil {
		t.Fatal(err)
	}
	if decodedFree.Aggregate == nil {
		t.Fatal("a present zero-cost aggregate must survive the round trip")
	}
	if decodedFree.Aggregate.CostUSD == nil || *decodedFree.Aggregate.CostUSD != 0 {
		t.Errorf("zero cost = %v, want a present 0", decodedFree.Aggregate.CostUSD)
	}
}

func TestProjectsListPayloadsRoundTrip(t *testing.T) {
	req := protocol.ProjectsListPayload{Provider: "opencode"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var gotReq protocol.ProjectsListPayload
	if err := json.Unmarshal(b, &gotReq); err != nil {
		t.Fatal(err)
	}
	if gotReq.Provider != "opencode" {
		t.Errorf("provider = %q", gotReq.Provider)
	}

	res := protocol.ProjectsResultPayload{
		Provider: "opencode",
		Projects: []provider.ProjectMeta{{ID: "p1", Name: "repo", Worktree: "/work/repo"}},
	}
	b, err = json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var gotRes protocol.ProjectsResultPayload
	if err := json.Unmarshal(b, &gotRes); err != nil {
		t.Fatal(err)
	}
	if len(gotRes.Projects) != 1 || gotRes.Projects[0].Worktree != "/work/repo" {
		t.Fatalf("projects round-trip failed: %+v", gotRes.Projects)
	}
	// A nameless project omits the key rather than sending "".
	b, err = json.Marshal(provider.ProjectMeta{ID: "p", Worktree: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "name") {
		t.Errorf("payload %s should omit an empty name", b)
	}
}

// Both discovery message types must be registered, or the daemon answers
// unknown_type and the phone sees a hang rather than a clear failure.
func TestProjectsListTypeConstantsAreDistinct(t *testing.T) {
	if protocol.TypeProjectsList == protocol.TypeProjectsResult {
		t.Fatal("request and result types must differ")
	}
	for _, want := range []string{"projects.list", "projects.list_result"} {
		if protocol.TypeProjectsList != want && protocol.TypeProjectsResult != want {
			continue
		}
	}
	if protocol.TypeProjectsList != "projects.list" {
		t.Errorf("request type = %q", protocol.TypeProjectsList)
	}
	if protocol.TypeProjectsResult != "projects.list_result" {
		t.Errorf("result type = %q", protocol.TypeProjectsResult)
	}
}
