package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/maccavelli/magic-cli-remote/internal/auth"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
	"github.com/maccavelli/magic-cli-remote/internal/session"
	"github.com/maccavelli/magic-cli-remote/internal/ws"
)

type wsSession struct {
	dir      string
	store    *auth.Store
	token    string
	conn     *websocket.Conn
	ts       *httptest.Server
	meta     session.Meta
	mgr      *session.Manager
	provider *fake.Provider
}

func setupWSSession(t *testing.T, deviceName string) *wsSession {
	t.Helper()
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.Create(deviceName)
	if err != nil {
		t.Fatal(err)
	}

	fp := fake.New()
	reg := provider.NewRegistry()
	reg.Register(fp)

	var srv *ws.Server
	mgr := session.NewManager(reg, nil, nil, func(ev event.Event) {
		if srv != nil {
			srv.BroadcastEvent(ev)
		}
	})
	srv = ws.New(ws.Options{
		Store:              store,
		Sessions:           mgr,
		Registry:           reg,
		RequireDeviceToken: true,
		Version:            "test",
		ListenAddr:         "127.0.0.1:0",
	})
	ts := httptest.NewServer(srv.Handler())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/v1/ws", nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}

	authEnv, _ := protocol.NewEnvelope(protocol.TypeAuth, "auth", protocol.AuthPayload{Token: token})
	writeEnv(ctx, t, conn, authEnv)
	got := readEnv(ctx, t, conn)
	if got.Type != protocol.TypeAuthOK {
		conn.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		t.Fatalf("auth: %s", got.Type)
	}

	createEnv, _ := protocol.NewEnvelope(protocol.TypeSessionCreate, "create", protocol.SessionCreatePayload{
		Provider: "fake",
		Name:     "test-session",
	})
	writeEnv(ctx, t, conn, createEnv)
	for {
		got = readEnv(ctx, t, conn)
		if got.Type == protocol.TypeEvent {
			continue
		}
		if got.Type != protocol.TypeSessionCreated {
			conn.Close(websocket.StatusNormalClosure, "")
			ts.Close()
			t.Fatalf("create session: %s payload=%s", got.Type, string(got.Payload))
		}
		break
	}
	var meta session.Meta
	if err := json.Unmarshal(got.Payload, &meta); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		ts.Close()
		t.Fatal(err)
	}

	return &wsSession{
		dir:      dir,
		store:    store,
		token:    token,
		conn:     conn,
		ts:       ts,
		meta:     meta,
		mgr:      mgr,
		provider: fp,
	}
}

func (s *wsSession) close(t *testing.T) {
	t.Helper()
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
	s.ts.Close()
}

func (s *wsSession) send(t *testing.T, env protocol.Envelope) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	writeEnv(ctx, t, s.conn, env)
}

func (s *wsSession) recv(t *testing.T) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return readEnv(ctx, t, s.conn)
}

func (s *wsSession) recvSkipEvents(t *testing.T) protocol.Envelope {
	t.Helper()
	for {
		env := s.recv(t)
		if env.Type != protocol.TypeEvent {
			return env
		}
	}
}

func TestWSSessionRename(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("rename unsupported by provider", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionRename, "r1", protocol.SessionRenamePayload{
			SessionID: ws.meta.ID,
			Name:      "renamed-session",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeOK {
			t.Fatalf("expected error for rename-unsupported provider, got ok")
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionRename, "r2", protocol.SessionRenamePayload{
			SessionID: ws.meta.ID,
			Name:      "",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s payload=%s", got.Type, string(got.Payload))
		}
	})
}

func TestWSSessionRevertUnrevertDiff(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("revert valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionRevert, "rev", protocol.SessionRevertPayload{
			SessionID: ws.meta.ID,
			MessageID: "msg-1",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("revert failed: %s", string(got.Payload))
		}
	})

	t.Run("revert unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionRevert, "rev-bad", protocol.SessionRevertPayload{
			SessionID: "no-such-session",
			MessageID: "msg-1",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})

	t.Run("unrevert valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionUnrevert, "unrev", protocol.SessionUnrevertPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("unrevert failed: %s", string(got.Payload))
		}
	})

	t.Run("diff valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionDiff, "diff", protocol.SessionDiffPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeSessionDiffResult {
			t.Fatalf("want diff_result, got %s payload=%s", got.Type, string(got.Payload))
		}
	})
}

func TestWSSessionClose(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("close valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionClose, "close", protocol.SessionIDPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("close failed: %s", string(got.Payload))
		}
	})

	t.Run("close unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionClose, "close-bad", protocol.SessionIDPayload{
			SessionID: "no-such-session",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})
}

func TestWSSessionDelete(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("delete valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionDelete, "del", protocol.SessionIDPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("delete failed: %s", string(got.Payload))
		}
	})

	t.Run("delete already deleted", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionDelete, "del-again", protocol.SessionIDPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeOK {
			t.Fatalf("expected error for double delete, got ok")
		}
	})
}

func TestWSSessionSetMode(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("set mode valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionSetMode, "mode", protocol.SessionSetModePayload{
			SessionID: ws.meta.ID,
			ModeID:    "default",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("set mode failed: %s", string(got.Payload))
		}
	})

	t.Run("set mode unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionSetMode, "mode-bad", protocol.SessionSetModePayload{
			SessionID: "no-such-session",
			ModeID:    "default",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})
}

func TestWSSessionSetConfig(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("set config valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionSetConfig, "cfg", protocol.SessionSetConfigPayload{
			SessionID: ws.meta.ID,
			OptionID:  "model",
			Kind:      "select",
			Value:     "fake-echo",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("set config failed: %s", string(got.Payload))
		}
	})

	t.Run("set config unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionSetConfig, "cfg-bad", protocol.SessionSetConfigPayload{
			SessionID: "no-such-session",
			OptionID:  "model",
			Kind:      "select",
			Value:     "fake-echo",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})
}

func TestWSSessionCancel(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("cancel valid", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionCancel, "cancel", protocol.SessionIDPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeError {
			t.Fatalf("cancel failed: %s", string(got.Payload))
		}
	})
}

func TestWSSessionFork(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("fork unsupported by provider", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionFork, "fork", protocol.SessionForkPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeSessionCreated {
			t.Fatalf("expected error for fork-unsupported provider, got session.created")
		}
	})

	t.Run("fork unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionFork, "fork-bad", protocol.SessionForkPayload{
			SessionID: "no-such-session",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})
}

func TestWSSessionDiagnostics(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("diagnostics unsupported by provider", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionDiagnostics, "diag", protocol.SessionIDPayload{
			SessionID: ws.meta.ID,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeOK {
			t.Fatalf("expected error or other response for diagnostics-unsupported provider, got ok")
		}
	})

	t.Run("diagnostics unknown session", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeSessionDiagnostics, "diag-bad", protocol.SessionIDPayload{
			SessionID: "no-such-session",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s", got.Type)
		}
	})
}

func TestWSProvidersList(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeProvidersList, "prov", nil)
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != protocol.TypeProvidersResult {
		t.Fatalf("want providers.list_result, got %s payload=%s", got.Type, string(got.Payload))
	}
	var res protocol.ProvidersResultPayload
	if err := json.Unmarshal(got.Payload, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Providers) == 0 {
		t.Fatal("expected at least one provider")
	}
	foundFake := false
	for _, p := range res.Providers {
		if p.ID == "fake" {
			foundFake = true
		}
	}
	if !foundFake {
		t.Fatalf("fake provider not in list: %+v", res.Providers)
	}
}

func TestWSAgentsList(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeAgentsList, "agents", protocol.AgentsListPayload{
		Provider: "fake",
	})
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != protocol.TypeAgentsResult {
		t.Fatalf("want agents.list_result, got %s payload=%s", got.Type, string(got.Payload))
	}
}

func TestWSPermissionRespond(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("session without permission support returns error", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypePermissionRespond, "perm", protocol.PermissionRespondPayload{
			SessionID:    ws.meta.ID,
			PermissionID: "no-such-perm",
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type == protocol.TypeOK {
			t.Fatalf("expected error for non-permission provider, got ok")
		}
	})
}

func TestWSQuestionRespond(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	t.Run("respond no pending question", func(t *testing.T) {
		env, _ := protocol.NewEnvelope(protocol.TypeQuestionRespond, "q", protocol.QuestionRespondPayload{
			SessionID:  ws.meta.ID,
			QuestionID: "no-such-q",
			Cancelled:  true,
		})
		ws.send(t, env)
		got := ws.recvSkipEvents(t)
		if got.Type != protocol.TypeError {
			t.Fatalf("want error, got %s payload=%s", got.Type, string(got.Payload))
		}
	})
}

func TestWSSessionHistoryNonExistentReturnsEmpty(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeSessionHistory, "hist", protocol.SessionHistoryPayload{
		SessionID: "no-such-session",
	})
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type == protocol.TypeError {
		t.Fatalf("expected empty history for unknown session, got error: %s", string(got.Payload))
	}
	if got.Type != protocol.TypeSessionHistoryResult {
		t.Fatalf("want session.history_result, got %s", got.Type)
	}
}

func TestWSSessionPendingAsksReturnsAuthenticatedSnapshot(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	env, _ := protocol.NewEnvelope(protocol.TypeSessionPendingAsks, "asks", map[string]any{})
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != protocol.TypeSessionPendingAsksResult {
		t.Fatalf("want session.pending_asks_result, got %s payload=%s", got.Type, string(got.Payload))
	}
	var payload protocol.SessionPendingAsksResultPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Events == nil || len(payload.Events) != 0 {
		t.Fatalf("new session pending asks = %+v, want empty non-nil slice", payload.Events)
	}
}
