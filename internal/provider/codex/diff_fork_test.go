package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestClipDiffUTF8AndCap(t *testing.T) {
	got, trunc := clipDiff("hello\n")
	if trunc || got != "hello\n" {
		t.Fatalf("small = %q trunc=%v", got, trunc)
	}
	wide := strings.Repeat("é", maxDiffBytes) + "\nextra\n"
	out, trunc := clipDiff(wide)
	if !trunc {
		t.Fatal("expected truncation")
	}
	if !utf8.ValidString(out) {
		t.Fatal("split a rune")
	}
	if !strings.Contains(out, "[diff truncated]") {
		t.Fatalf("missing truncation notice: %q", out[len(out)-40:])
	}
}

func TestDiffUsesCWDOnlyAndValidatesSHA(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.cwd = "/tmp/work"
	s.engineGeneration = 1

	done := make(chan provider.DiffResult, 1)
	errc := make(chan error, 1)
	go func() {
		res, err := s.Diff(context.Background(), "/etc/passwd")
		done <- res
		errc <- err
	}()
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "gitDiffToRemote" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Params["cwd"] != "/tmp/work" {
		t.Fatalf("cwd = %#v", req.Params["cwd"])
	}
	if _, ok := req.Params["path"]; ok {
		t.Fatal("must not send a caller path")
	}
	sha := strings.Repeat("a", 40)
	resp := map[string]any{"id": req.ID, "result": map[string]any{"sha": sha, "diff": "diff --git a/x b/x\n"}}
	b, _ := json.Marshal(resp)
	if _, err := engineW.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	res := <-done
	if res.BaseSHA != sha || res.Scope != diffScopeWorkingTree {
		t.Fatalf("%+v", res)
	}
}

func TestDiffInvalidSHA(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"result":{"sha":"nope","diff":""}}` + "\n"))
	}()
	if _, err := s.Diff(context.Background(), ""); err == nil {
		t.Fatal("invalid sha must fail")
	}
}

func TestTurnDiffUpdatedReplacesByTurn(t *testing.T) {
	s := seededCollabSession(t)
	s.rememberTurnDiff("t1", "first")
	s.rememberTurnDiff("t1", "second")
	s.rememberTurnDiff("t2", "other")
	if s.turnDiffs["t1"] != "second" || s.lastTurnDiffID != "t2" {
		t.Fatalf("cache = %+v last=%s", s.turnDiffs, s.lastTurnDiffID)
	}
}

func TestDiffFallbackOnMethodNotFound(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1
	s.rememberTurnDiff("t9", "cached patch")
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"error":{"code":-32601,"message":"Method not found"}}` + "\n"))
	}()
	res, err := s.Diff(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope != diffScopeLatestTurn || res.Summary != "cached patch" {
		t.Fatalf("%+v", res)
	}
}

func TestForkSendsLastTurnIDOnly(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1, experimental: true}
	s := seededCollabSession(t)
	s.p = p
	s.agentID = "thread-1"

	done := make(chan provider.ForkResult, 1)
	errc := make(chan error, 1)
	go func() {
		res, err := s.Fork(context.Background(), provider.ForkOptions{LastTurnID: "turn-9"})
		done <- res
		errc <- err
	}()
	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "thread/fork" {
		t.Fatalf("method = %q", req.Method)
	}
	if _, ok := req.Params["turnId"]; ok {
		t.Fatal("must not send turnId")
	}
	if req.Params["lastTurnId"] != "turn-9" {
		t.Fatalf("lastTurnId = %#v", req.Params["lastTurnId"])
	}
	b, _ := json.Marshal(map[string]any{
		"id": req.ID,
		"result": map[string]any{
			"thread":       map[string]any{"id": "child-1"},
			"forkedFromId": "thread-1",
		},
	})
	if _, err := engineW.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	res := <-done
	if res.AgentSessionID != "child-1" || res.ForkedFromID != "thread-1" {
		t.Fatalf("%+v", res)
	}
}

func TestForkNoRolloutFound(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"error":{"code":-32600,"message":"no rollout found"}}` + "\n"))
	}()
	_, err := s.Fork(context.Background(), provider.ForkOptions{})
	if !errors.Is(err, provider.ErrForkNothing) {
		t.Fatalf("err = %v", err)
	}
}

func TestForkBusyRejected(t *testing.T) {
	s := seededCollabSession(t)
	s.turnBusy = true
	if _, err := s.Fork(context.Background(), provider.ForkOptions{}); !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiffAcceptsSHA64AndEmptyPatch(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		sha := strings.Repeat("ab", 32)
		b, _ := json.Marshal(map[string]any{"id": req.ID, "result": map[string]any{"sha": sha, "diff": ""}})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	res, err := s.Diff(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "" || res.Truncated || res.Scope != diffScopeWorkingTree {
		t.Fatalf("%+v", res)
	}
	if len(res.BaseSHA) != 64 {
		t.Fatalf("sha len = %d", len(res.BaseSHA))
	}
}

func TestDiffFallbackUnavailableAndLatch(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.engineGeneration = 1
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(engineR).Decode(&req); err != nil {
			return
		}
		_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"error":{"code":-32601,"message":"Method not found"}}` + "\n"))
	}()
	if _, err := s.Diff(context.Background(), ""); !errors.Is(err, provider.ErrDiffUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if !p.eng.diffUnavailable {
		t.Fatal("must latch unavailable for this generation")
	}
	// Second call must not send another RPC.
	res, err := s.Diff(context.Background(), "")
	if !errors.Is(err, provider.ErrDiffUnavailable) {
		t.Fatalf("second err = %v res=%+v", err, res)
	}
}

func TestClipDiffNewlineBoundaryAndEnvelope(t *testing.T) {
	// A multi-byte rune straddling the cap must not be split; clip back to a
	// newline so the truncation notice sits on its own line.
	prefix := strings.Repeat("a", maxDiffBytes-3) + "\néxtra\n"
	out, trunc := clipDiff(prefix)
	if !trunc {
		t.Fatal("expected truncation")
	}
	if !utf8.ValidString(out) {
		t.Fatal("invalid utf8")
	}
	if !strings.HasSuffix(out, diffTruncationNotice) {
		t.Fatalf("suffix = %q", out[len(out)-20:])
	}
	if strings.Contains(out, "extra") {
		t.Fatal("clipped past the last full line")
	}
}

func TestForkWholeThreadOmitsBoundary(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	s.agentID = "thread-1"
	got := make(chan map[string]any, 1)
	go func() {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(engineR).Decode(&req); err != nil {
			return
		}
		got <- map[string]any{"method": req.Method, "params": req.Params}
		b, _ := json.Marshal(map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"thread":       map[string]any{"id": "child-1"},
				"forkedFromId": "thread-1",
			},
		})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	res, err := s.Fork(context.Background(), provider.ForkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.AgentSessionID != "child-1" {
		t.Fatalf("%+v", res)
	}
	req := <-got
	if req["method"] != "thread/fork" {
		t.Fatalf("method = %q", req["method"])
	}
	params := req["params"].(map[string]any)
	if _, ok := params["lastTurnId"]; ok {
		t.Fatal("whole-thread fork must omit lastTurnId")
	}
	if _, ok := params["turnId"]; ok {
		t.Fatal("must not send turnId")
	}
	if _, ok := params["deferGoalContinuation"]; ok {
		t.Fatal("defer must be omitted when false")
	}
}

func TestForkUnknownLastTurnIDDoesNotSilentFork(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1}
	s := seededCollabSession(t)
	s.p = p
	go func() {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		_, _ = engineW.Write([]byte(`{"id":` + itoa64(req.ID) + `,"error":{"code":-32602,"message":"unknown lastTurnId"}}` + "\n"))
	}()
	_, err := s.Fork(context.Background(), provider.ForkOptions{LastTurnID: "missing"})
	if err == nil {
		t.Fatal("unknown boundary must not silently fork")
	}
	if errors.Is(err, provider.ErrForkNothing) {
		t.Fatal("unknown boundary is not the never-materialized case")
	}
}

func TestForkDeferGoalSerializedOnlyWhenExperimental(t *testing.T) {
	s := seededCollabSession(t)
	s.p = &Provider{log: testLogger(t), eng: &engine{generation: 1}}
	if _, err := s.Fork(context.Background(), provider.ForkOptions{DeferGoalContinuation: true}); err == nil {
		t.Fatal("defer without experimental must fail before RPC")
	}

	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, generation: 1, experimental: true}
	s = seededCollabSession(t)
	s.p = p
	s.agentID = "thread-1"
	got := make(chan map[string]any, 1)
	go func() {
		var req struct {
			ID     int64          `json:"id"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(engineR).Decode(&req)
		got <- req.Params
		b, _ := json.Marshal(map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"thread":       map[string]any{"id": "child-2"},
				"forkedFromId": "thread-1",
			},
		})
		_, _ = engineW.Write(append(b, '\n'))
	}()
	if _, err := s.Fork(context.Background(), provider.ForkOptions{DeferGoalContinuation: true}); err != nil {
		t.Fatal(err)
	}
	params := <-got
	if params["deferGoalContinuation"] != true {
		t.Fatalf("defer = %#v", params["deferGoalContinuation"])
	}
	if _, ok := params["turnId"]; ok {
		t.Fatal("must not send turnId")
	}
}
