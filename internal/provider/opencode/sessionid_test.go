package opencode

import (
	"encoding/json"
	"testing"
)

func TestSessionIDOfInfoID(t *testing.T) {
	// session.created shape: info.id only (MADR 0020).
	sid := sessionIDOf(json.RawMessage(`{"info":{"id":"child1","parentID":"parent1"}}`))
	if sid != "child1" {
		t.Fatalf("sessionIDOf info.id = %q want child1", sid)
	}
}

func TestSessionIDOfPrefersTopLevel(t *testing.T) {
	sid := sessionIDOf(json.RawMessage(`{"sessionID":"top","info":{"id":"info-id"}}`))
	if sid != "top" {
		t.Fatalf("got %q want top", sid)
	}
}

func TestParentIDOf(t *testing.T) {
	pid := parentIDOf(json.RawMessage(`{"info":{"id":"c","parentID":"p"}}`))
	if pid != "p" {
		t.Fatalf("parentIDOf = %q want p", pid)
	}
	d := &httpDialect{}
	if d.ParentIDFromProps(json.RawMessage(`{"info":{"parentID":"p2"}}`)) != "p2" {
		t.Fatal("ChildFrame ParentIDFromProps failed")
	}
}
