package ws_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

func modelsResult(t *testing.T, env protocol.Envelope) protocol.ModelsResultPayload {
	t.Helper()
	if env.Type != protocol.TypeModelsResult {
		t.Fatalf("want models.list_result, got %s payload=%s", env.Type, string(env.Payload))
	}
	var res protocol.ModelsResultPayload
	if err := json.Unmarshal(env.Payload, &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func optionIDs(res protocol.ModelsResultPayload) []string {
	out := make([]string, 0, len(res.Options))
	for _, o := range res.Options {
		out = append(out, o.ID)
	}
	return out
}

func hasID(res protocol.ModelsResultPayload, id string) bool {
	for _, o := range res.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// TestWSModelsListScopes covers the four dispatch routes of MADR 0043 D1. The
// distinction that matters is default-vs-everything: the default reply must be
// the connected set only, or the whole point of the scoping is lost.
func TestWSModelsListScopes(t *testing.T) {
	s := setupWSSession(t, "test")
	defer s.close(t)

	t.Run("default scope is the connected set", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m1",
			protocol.ModelsListPayload{Provider: "fake"})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		if !hasID(res, "fake-echo") {
			t.Fatalf("connected models missing: %v", optionIDs(res))
		}
		if hasID(res, "fake-big") {
			t.Fatalf("unconnected model provider leaked into the default set: %v", optionIDs(res))
		}
		if res.Truncated {
			t.Error("small catalog reported truncated")
		}
	})

	t.Run("providers scope enumerates model providers", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m2",
			protocol.ModelsListPayload{Provider: "fake", Scope: "providers"})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		if !hasID(res, "fake") || !hasID(res, "fake-remote") {
			t.Fatalf("want both model providers, got %v", optionIDs(res))
		}
		for _, o := range res.Options {
			if o.ID == "fake" && o.Meta["connected"] != "true" {
				t.Errorf("fake should be marked connected: %+v", o.Meta)
			}
			if o.ID == "fake-remote" && o.Meta["connected"] != "false" {
				t.Errorf("fake-remote should be marked unconnected: %+v", o.Meta)
			}
		}
	})

	t.Run("model_provider narrows to one provider", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m3",
			protocol.ModelsListPayload{Provider: "fake", ModelProvider: "fake-remote"})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		if res.ModelProvider != "fake-remote" {
			t.Errorf("reply did not echo the applied scope: %q", res.ModelProvider)
		}
		if !hasID(res, "fake-big") || hasID(res, "fake-echo") {
			t.Fatalf("want only fake-remote models, got %v", optionIDs(res))
		}
	})

	t.Run("unknown model_provider is empty, not an error", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m4",
			protocol.ModelsListPayload{Provider: "fake", ModelProvider: "nope"})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		if len(res.Options) != 0 {
			t.Fatalf("want empty, got %v", optionIDs(res))
		}
	})

	t.Run("session scope pre-selects the session model", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m5",
			protocol.ModelsListPayload{Provider: "fake", SessionID: s.meta.ID})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		if !hasID(res, "fake-echo") {
			t.Fatalf("session catalog missing models: %v", optionIDs(res))
		}
		if len(res.DefaultIDs) == 0 {
			t.Fatal("session-scoped catalog carries no default; the picker cannot pre-select")
		}
		if res.Source != "live" {
			t.Errorf("session catalog source = %q, want live", res.Source)
		}
	})

	t.Run("ordering puts deprecated last", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m6",
			protocol.ModelsListPayload{Provider: "fake"})
		s.send(t, env)
		res := modelsResult(t, s.recvSkipEvents(t))
		ids := optionIDs(res)
		if len(ids) == 0 || ids[len(ids)-1] != "fake-legacy" {
			t.Fatalf("deprecated model not ranked last: %v", ids)
		}
	})

	t.Run("unknown scope is a payload error", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "m7",
			protocol.ModelsListPayload{Provider: "fake", Scope: "modelz"})
		s.send(t, env)
		got := s.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error for an unknown scope, got %s payload=%s", got.Type, string(got.Payload))
		}
	})
}

// TestWSModelsListSessionForbidden: a session's model is not public
// information, so a second device on the same daemon must be refused rather
// than quietly served the owner's session catalog.
func TestWSModelsListSessionForbidden(t *testing.T) {
	owner := setupWSSession(t, "owner")
	defer owner.close(t)

	_, otherToken, err := owner.store.Create("intruder")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+owner.ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "auth", protocol.AuthPayload{Token: otherToken})
	writeEnv(ctx, t, conn, authEnv)
	if got := readEnv(ctx, t, conn); got.Type != protocol.TypeAuthOK {
		t.Fatalf("auth: %s", got.Type)
	}

	env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "f1",
		protocol.ModelsListPayload{Provider: "fake", SessionID: owner.meta.ID})
	writeEnv(ctx, t, conn, env)
	for {
		got := readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeError {
			t.Fatalf("a foreign device was served a session catalog: %s payload=%s",
				got.Type, string(got.Payload))
		}
		var e protocol.ErrorPayload
		if err := json.Unmarshal(got.Payload, &e); err != nil {
			t.Fatal(err)
		}
		if e.Code != "session_forbidden" {
			t.Fatalf("error code = %q, want session_forbidden", e.Code)
		}
		return
	}
}

// TestWSModelsListUnknownSessionFallsBack: an id that does not exist is not an
// authorization failure the user can act on (a stale picker request after the
// session closed), so it degrades to the provider catalog rather than an error
// — but it must not be answered as if it were session state.
func TestWSModelsListUnknownSessionFallsBack(t *testing.T) {
	s := setupWSSession(t, "test")
	defer s.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeModelsList, "u1",
		protocol.ModelsListPayload{Provider: "fake", SessionID: "no-such-session"})
	s.send(t, env)
	res := modelsResult(t, s.recvSkipEvents(t))
	if res.Source == "live" {
		t.Fatalf("unknown session answered with a live session catalog: %+v", res)
	}
	if !hasID(res, "fake-echo") {
		t.Fatalf("fallback catalog is empty: %v", optionIDs(res))
	}
}
