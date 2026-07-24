package opencode

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestNormalizePermissionAskDualShape(t *testing.T) {
	asked, ok := normalizePermissionAsk(json.RawMessage(`{
		"id":"p1","sessionID":"s1","permission":"edit","patterns":["a.go"],"always":["edit"]
	}`))
	if !ok || asked.Name != "edit" || len(asked.Patterns) != 1 {
		t.Fatalf("asked=%+v ok=%v", asked, ok)
	}

	updated, ok := normalizePermissionAsk(json.RawMessage(`{
		"id":"p2","sessionID":"s1","type":"bash","pattern":"rm *","title":"Run rm"
	}`))
	if !ok {
		t.Fatal("updated normalize failed")
	}
	if updated.Name != "bash" {
		t.Fatalf("name=%q want bash (from type)", updated.Name)
	}
	if len(updated.Patterns) != 1 || updated.Patterns[0] != "rm *" {
		t.Fatalf("patterns=%v", updated.Patterns)
	}
	if updated.Detail != "rm *" && !strings.Contains(updated.Detail, "Run rm") {
		// detail is join(patterns) first
		if updated.Detail != "rm *" {
			t.Fatalf("detail=%q", updated.Detail)
		}
	}

	// pattern as array
	u2, ok := normalizePermissionAsk(json.RawMessage(`{
		"id":"p3","type":"edit","pattern":["a","b"],"title":"t"
	}`))
	if !ok || len(u2.Patterns) != 2 {
		t.Fatalf("array pattern=%+v", u2)
	}
}

func TestNormalizePermissionV2(t *testing.T) {
	p, ok := normalizePermissionV2Ask(json.RawMessage(`{
		"id":"v1","sessionID":"child1","action":"read","resources":["/tmp/x"],"save":["read"]
	}`))
	if !ok || p.Name != "read" || p.SessionID != "child1" || len(p.Always) != 1 {
		t.Fatalf("%+v ok=%v", p, ok)
	}
}

func TestPermissionUpdatedEmitsRequest(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("permission.updated", json.RawMessage(`{
		"id":"perm-u","sessionID":"ses_test","type":"edit","pattern":"f.go","title":"Edit f.go"
	}`))
	var found bool
	for _, ev := range h.events {
		if ev.Type == event.TypePermission && ev.PermissionID == "perm-u" {
			found = true
			if ev.ToolName != "edit" {
				t.Fatalf("tool_name=%q", ev.ToolName)
			}
		}
	}
	if !found {
		t.Fatal("expected permission_request from permission.updated")
	}
}

func TestPermissionV2AskedEmitsRequest(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("permission.v2.asked", json.RawMessage(`{
		"id":"pv2","sessionID":"child1","action":"bash","resources":["ls"],"save":[]
	}`))
	found := false
	for _, ev := range h.events {
		if ev.Type == event.TypePermission && ev.PermissionID == "pv2" && ev.ToolName == "bash" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected v2 permission_request")
	}
}

func TestRespondPermissionPrefersGlobalThenSession(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "/permission/") && strings.Contains(r.URL.Path, "/reply") {
			http.Error(w, "not found", 404)
			return
		}
		if strings.Contains(r.URL.Path, "/permissions/") {
			if !strings.Contains(string(body), `"response"`) {
				t.Errorf("session path body=%s want response key", body)
			}
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	h := &originHost{
		captureHost: captureHost{
			api: func(ctx context.Context, method, path string, body, out any) error {
				var rd io.Reader
				if body != nil {
					b, _ := json.Marshal(body)
					rd = strings.NewReader(string(b))
				}
				req, err := http.NewRequestWithContext(ctx, method, ts.URL+path, rd)
				if err != nil {
					return err
				}
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					return err
				}
				defer res.Body.Close()
				if res.StatusCode >= 300 {
					b, _ := io.ReadAll(res.Body)
					return errHTTP(res.StatusCode, string(b))
				}
				return nil
			},
		},
		origin: "child1",
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	if err := s.RespondPermission(context.Background(), "perm-x", "once", false); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) < 2 {
		t.Fatalf("paths=%v want global then session", paths)
	}
	if !strings.Contains(paths[0], "/permission/perm-x/reply") {
		t.Fatalf("first path=%q want global reply", paths[0])
	}
	if !strings.Contains(paths[1], "/session/child1/permissions/perm-x") {
		t.Fatalf("second path=%q want origin session path", paths[1])
	}
}

type originHost struct {
	captureHost
	origin string
}

func (h *originHost) PermissionOrigin(string) string { return h.origin }
func (h *originHost) AgentSessionID() string         { return "parent1" }

type httpStatusError struct {
	code int
	msg  string
}

func (e httpStatusError) Error() string { return e.msg }

func errHTTP(code int, msg string) error {
	return httpStatusError{code: code, msg: msg}
}
