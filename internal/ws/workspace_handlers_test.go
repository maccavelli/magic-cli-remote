package ws_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// Workspace inspection over the wire (MADR 0112 A5, PLAN P6 step 4).
//
// The properties that matter here are refusal properties: an unowned session,
// a provider without the surface, and a malformed request must each fail with
// a specific code and leak nothing about the daemon host.

// errorFor sends env and returns the error payload it produced.
func errorFor(t *testing.T, ws *wsSession, env protocol.Envelope) protocol.ErrorPayload {
	t.Helper()
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != protocol.TypeError {
		t.Fatalf("want an error frame, got %s payload=%s", got.Type, string(got.Payload))
	}
	var e protocol.ErrorPayload
	if err := json.Unmarshal(got.Payload, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// TestWSWorkspaceRejectsMissingSession proves every workspace op requires a
// session id before anything else happens.
func TestWSWorkspaceRejectsMissingSession(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	for _, c := range []struct {
		name string
		typ  string
		p    any
	}{
		{"list", protocol.TypeWorkspaceList, protocol.WorkspaceListPayload{}},
		{"read", protocol.TypeWorkspaceRead, protocol.WorkspaceReadPayload{Path: "a.txt"}},
		{"search", protocol.TypeWorkspaceSearch, protocol.WorkspaceSearchPayload{Kind: "text", Query: "q"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, _ := protocol.NewEnvelope(c.typ, "ws-nosess-"+c.name, c.p)
			if e := errorFor(t, ws, env); e.Code != "bad_payload" {
				t.Fatalf("code = %q, want bad_payload", e.Code)
			}
		})
	}
}

// resultFor sends env and returns the successful reply payload.
func resultFor(t *testing.T, ws *wsSession, env protocol.Envelope, wantType string) map[string]any {
	t.Helper()
	ws.send(t, env)
	got := ws.recvSkipEvents(t)
	if got.Type != wantType {
		t.Fatalf("type = %s, want %s (payload=%s)", got.Type, wantType, string(got.Payload))
	}
	var out map[string]any
	if err := json.Unmarshal(got.Payload, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestWSWorkspaceRoundTrip proves the three operations reach the provider and
// their results come back intact over the wire.
func TestWSWorkspaceRoundTrip(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	list, _ := protocol.NewEnvelope(protocol.TypeWorkspaceList, "ws-list",
		protocol.WorkspaceListPayload{SessionID: ws.meta.ID, Path: "lib"})
	got := resultFor(t, ws, list, protocol.TypeWorkspaceListResult)
	entries, _ := got["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %v", got["entries"])
	}
	if got["path"] != "lib" {
		t.Fatalf("path echo = %v", got["path"])
	}

	read, _ := protocol.NewEnvelope(protocol.TypeWorkspaceRead, "ws-read",
		protocol.WorkspaceReadPayload{SessionID: ws.meta.ID, Path: "go.mod"})
	gotRead := resultFor(t, ws, read, protocol.TypeWorkspaceReadResult)
	if gotRead["text"] != "fixture body" {
		t.Fatalf("text = %v", gotRead["text"])
	}
	if gotRead["path"] != "go.mod" {
		t.Fatalf("path = %v", gotRead["path"])
	}

	// The cap that actually applied differs by kind and must survive the wire.
	for kind, wantCap := range map[string]float64{"text": 10, "file": 100} {
		search, _ := protocol.NewEnvelope(protocol.TypeWorkspaceSearch, "ws-search-"+kind,
			protocol.WorkspaceSearchPayload{SessionID: ws.meta.ID, Kind: kind, Query: "needle"})
		gotSearch := resultFor(t, ws, search, protocol.TypeWorkspaceSearchResult)
		if gotSearch["kind"] != kind {
			t.Fatalf("kind = %v, want %q", gotSearch["kind"], kind)
		}
		if gotSearch["cap"] != wantCap {
			t.Fatalf("%s cap = %v, want %v", kind, gotSearch["cap"], wantCap)
		}
	}
}

// TestWSWorkspaceRefusalsKeepTheirCode proves each validation failure reaches
// the phone as its own code, so a client can explain what went wrong instead of
// showing one generic error.
func TestWSWorkspaceRefusalsKeepTheirCode(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	for _, c := range []struct {
		name, typ string
		payload   any
		wantCode  string
	}{
		{"escape", protocol.TypeWorkspaceList,
			protocol.WorkspaceListPayload{SessionID: ws.meta.ID, Path: "escape"},
			protocol.ErrPathEscape},
		{"symlink", protocol.TypeWorkspaceList,
			protocol.WorkspaceListPayload{SessionID: ws.meta.ID, Path: "symlink"},
			protocol.ErrPathSymlink},
		{"upstream failure", protocol.TypeWorkspaceList,
			protocol.WorkspaceListPayload{SessionID: ws.meta.ID, Path: "boom"},
			protocol.ErrWorkspaceFailed},
		{"binary", protocol.TypeWorkspaceRead,
			protocol.WorkspaceReadPayload{SessionID: ws.meta.ID, Path: "binary.bin"},
			protocol.ErrBinaryContent},
		{"too large", protocol.TypeWorkspaceRead,
			protocol.WorkspaceReadPayload{SessionID: ws.meta.ID, Path: "huge.txt"},
			protocol.ErrResultTooLarge},
		{"invalid path", protocol.TypeWorkspaceRead,
			protocol.WorkspaceReadPayload{SessionID: ws.meta.ID, Path: ""},
			protocol.ErrInvalidPath},
		{"unknown kind", protocol.TypeWorkspaceSearch,
			protocol.WorkspaceSearchPayload{SessionID: ws.meta.ID, Kind: "symbol", Query: "x"},
			protocol.ErrInvalidQuery},
		{"empty query", protocol.TypeWorkspaceSearch,
			protocol.WorkspaceSearchPayload{SessionID: ws.meta.ID, Kind: "text", Query: ""},
			protocol.ErrInvalidQuery},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, _ := protocol.NewEnvelope(c.typ, "ws-refuse-"+c.name, c.payload)
			e := errorFor(t, ws, env)
			if e.Code != c.wantCode {
				t.Fatalf("code = %q, want %q", e.Code, c.wantCode)
			}
			if strings.Contains(e.Message, ws.dir) {
				t.Fatalf("the refusal leaked a host path: %q", e.Message)
			}
		})
	}
}

// TestWSWorkspaceRejectsUnknownSession proves a session id the caller does not
// own cannot be probed for existence through this surface.
func TestWSWorkspaceRejectsUnknownSession(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	for _, c := range []struct {
		name string
		typ  string
		p    any
	}{
		{"list", protocol.TypeWorkspaceList, protocol.WorkspaceListPayload{SessionID: "not-mine"}},
		{"read", protocol.TypeWorkspaceRead, protocol.WorkspaceReadPayload{SessionID: "not-mine", Path: "a.txt"}},
		{"search", protocol.TypeWorkspaceSearch, protocol.WorkspaceSearchPayload{SessionID: "not-mine", Kind: "text", Query: "q"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, _ := protocol.NewEnvelope(c.typ, "ws-unknown-"+c.name, c.p)
			e := errorFor(t, ws, env)
			if e.Code == "" {
				t.Fatal("an unknown session produced no error code")
			}
			if strings.Contains(e.Message, ws.dir) {
				t.Fatalf("the refusal leaked a host path: %q", e.Message)
			}
		})
	}
}

// TestWorkspaceErrorCodesAreRegistered proves every code this surface can
// return is a declared protocol code, so a client can branch on it.
func TestWorkspaceErrorCodesAreRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range protocol.ErrorCodes() {
		registered[c] = true
	}
	for _, c := range []string{
		protocol.ErrInvalidPath,
		protocol.ErrPathEscape,
		protocol.ErrPathSymlink,
		protocol.ErrBinaryContent,
		protocol.ErrResultTooLarge,
		protocol.ErrInvalidQuery,
		protocol.ErrWorkspaceFailed,
	} {
		if !registered[c] {
			t.Errorf("workspace code %q is not registered", c)
		}
	}
}

// TestWSWorkspaceRejectsMalformedPayloads proves a payload that cannot decode
// is refused as bad_payload rather than reaching a handler with zero values —
// an empty session id would otherwise look like a valid request for the root.
func TestWSWorkspaceRejectsMalformedPayloads(t *testing.T) {
	ws := setupWSSession(t, "test")
	defer ws.close(t)

	for _, typ := range []string{
		protocol.TypeWorkspaceList,
		protocol.TypeWorkspaceRead,
		protocol.TypeWorkspaceSearch,
	} {
		t.Run(typ, func(t *testing.T) {
			// session_id typed as a number cannot decode into the string field.
			env, _ := protocol.NewEnvelope(typ, "ws-malformed-"+typ,
				map[string]any{"session_id": 42})
			if e := errorFor(t, ws, env); e.Code != "bad_payload" {
				t.Fatalf("code = %q, want bad_payload", e.Code)
			}
		})
	}
}
