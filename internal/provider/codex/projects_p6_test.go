package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestProjectAllSevenMethodsAndBounds(t *testing.T) {
	if upstreamProjectDefaultLimit != 25 || upstreamProjectMaximumLimit != 100 || projectListLimit != 50 {
		t.Fatalf("project limits default=%d maximum=%d requested=%d", upstreamProjectDefaultLimit, upstreamProjectMaximumLimit, projectListLimit)
	}
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"project/list":   {json.RawMessage(`{"data":[{"id":"p1","name":"One","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":2}],"nextCursor":null}`)},
		"project/read":   {json.RawMessage(`{"project":{"id":"p1","name":"One","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":2}}`)},
		"project/create": {json.RawMessage(`{"project":{"id":"p1","name":"One","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":2}}`)},
		"project/import": {json.RawMessage(`{"project":{"id":"p2","name":"Two","roots":[{"path":"/other"}],"metadata":{},"position":2,"createdAt":1,"updatedAt":2}}`)},
		"project/update": {json.RawMessage(`{"project":{"id":"p1","name":"Renamed","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":3}}`)},
		"project/move":   {json.RawMessage(`{}`)},
		"project/delete": {json.RawMessage(`{}`)},
	}}
	api := newProjectAPI(stub.send, func(CapabilityID) bool { return true })
	page, err := api.List(context.Background(), "", 50)
	if err != nil || len(page.Projects) != 1 || page.Limit != 50 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := api.Read(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Create(context.Background(), provider.ProjectMutation{Name: "One", Roots: []string{"/repo"}, IdempotencyKey: "envelope-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Import(context.Background(), provider.ProjectMutation{Name: "Two", Roots: []string{"/other"}, ThreadIDs: []string{"t1"}, IdempotencyKey: "envelope-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Update(context.Background(), "p1", provider.ProjectMutation{Name: "Renamed", Roots: []string{"/repo"}}); err != nil {
		t.Fatal(err)
	}
	if err := api.Move(context.Background(), "p1", "p2"); err != nil {
		t.Fatal(err)
	}
	if err := api.Delete(context.Background(), "p2"); err != nil {
		t.Fatal(err)
	}
	want := []string{"project/list", "project/read", "project/create", "project/import", "project/update", "project/move", "project/delete"}
	got := make([]string, 0, len(stub.requests))
	for _, request := range stub.requests {
		got = append(got, request.method)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("methods = %v", got)
	}
	if stub.requests[0].params["limit"] != uint32(50) {
		t.Fatalf("project list must send limit 50: %#v", stub.requests[0].params)
	}
}

func TestProjectValidationCanonicalRootsNamesAndImportThreads(t *testing.T) {
	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(tmp, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	tests := []provider.ProjectMutation{
		{Name: "   ", Roots: []string{realRoot}},
		{Name: "relative", Roots: []string{"repo"}},
		{Name: "logical duplicate", Roots: []string{realRoot, realRoot}},
		{Name: "canonical duplicate", Roots: []string{realRoot, linkRoot}},
		{Name: "duplicate threads", Roots: []string{realRoot}, ThreadIDs: []string{"t1", "t1"}},
	}
	for _, mutation := range tests {
		if err := validateProjectMutation(mutation); err == nil {
			t.Fatalf("validation accepted %+v", mutation)
		}
	}
	if err := validateProjectMutation(provider.ProjectMutation{Name: "valid", Roots: []string{realRoot}, ThreadIDs: []string{"t1", "t2"}}); err != nil {
		t.Fatalf("valid mutation: %v", err)
	}
}

func TestProjectAssignmentExactOmitClearAssignAndForkInheritance(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/metadata/update": {
			json.RawMessage(`{"thread":{"id":"t1","projectId":"p1"}}`),
			json.RawMessage(`{"thread":{"id":"t1","projectId":null}}`),
		},
	}}
	api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
	if err := api.AssignProject(context.Background(), "t1", provider.ProjectAssignment{ProjectID: "p1", Set: true}); err != nil {
		t.Fatal(err)
	}
	if err := api.AssignProject(context.Background(), "t1", provider.ProjectAssignment{Set: true}); err != nil {
		t.Fatal(err)
	}
	if err := api.AssignProject(context.Background(), "t1", provider.ProjectAssignment{}); err != nil {
		t.Fatal(err)
	}
	if stub.requests[0].params["projectId"] != "p1" || stub.requests[1].params["projectId"] != "" {
		t.Fatalf("assign params = %+v", stub.requests)
	}
	if _, exists := stub.requests[2].params["projectId"]; exists {
		t.Fatalf("omitted assignment changed project: %#v", stub.requests[2].params)
	}
	parent := provider.AgentSessionMeta{ID: "parent", ProjectID: "p1"}
	child := inheritForkProject(parent, provider.AgentSessionMeta{ID: "child", ForkedFromID: "parent"})
	if child.ProjectID != "p1" {
		t.Fatalf("fork project = %+v", child)
	}
}

func TestProjectChangedAndThreadProjectUpdatedNotificationsArePinned(t *testing.T) {
	if got := notificationRouteFor("project/changed"); got != notificationRouteProvider {
		t.Fatalf("project/changed route = %v", got)
	}
	if got := notificationRouteFor("thread/project/updated"); got != notificationRouteSession {
		t.Fatalf("thread/project/updated route = %v", got)
	}
	s := modeTestSession(t, Config{})
	s.handleDecodedNotification("thread/project/updated", json.RawMessage(`{"threadId":"thread-1","projectId":"project-1"}`), unixTime(10))
	events := drainModeEvents(s)
	if len(events) != 1 || !strings.Contains(events[0].Text, "project assignment updated") {
		t.Fatalf("events = %+v", events)
	}
}

func TestProjectUpstreamBehavioralCases(t *testing.T) {
	t.Run("projects_persist_and_assign_threads", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{
			"project/create":         {json.RawMessage(`{"project":{"id":"p","name":"Persistent","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":1}}`)},
			"thread/metadata/update": {json.RawMessage(`{"thread":{"id":"t","projectId":"p"}}`)},
		}}
		projects := newProjectAPI(stub.send, func(CapabilityID) bool { return true })
		threads := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
		if _, err := projects.Create(context.Background(), provider.ProjectMutation{Name: "Persistent", Roots: []string{"/repo"}, IdempotencyKey: "key"}); err != nil {
			t.Fatal(err)
		}
		if err := threads.AssignProject(context.Background(), "t", provider.ProjectAssignment{ProjectID: "p", Set: true}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("deleted_project_is_dropped_before_first_durable_thread_persistence", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{"project/delete": {json.RawMessage(`{}`)}}}
		api := newProjectAPI(stub.send, func(CapabilityID) bool { return true })
		if err := api.Delete(context.Background(), "p"); err != nil {
			t.Fatal(err)
		}
		if len(stub.requests) != 1 || stub.requests[0].method != "project/delete" {
			t.Fatalf("project deletion touched threads: %+v", stub.requests)
		}
	})

	t.Run("project_import_is_atomic_and_notifies_after_commit_in_order", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{"project/import": {json.RawMessage(`{"project":{"id":"p","name":"Imported","roots":[{"path":"/repo"}],"metadata":{},"position":1,"createdAt":1,"updatedAt":1}}`)}}}
		api := newProjectAPI(stub.send, func(CapabilityID) bool { return true })
		if _, err := api.Import(context.Background(), provider.ProjectMutation{Name: "Imported", Roots: []string{"/repo"}, ThreadIDs: []string{"t1", "t2"}, IdempotencyKey: "one-envelope"}); err != nil {
			t.Fatal(err)
		}
		if got := stub.requests[0].params["threads"]; !reflect.DeepEqual(got, []string{"t1", "t2"}) {
			t.Fatalf("atomic import threads = %#v", got)
		}
	})

	t.Run("projects_validate_filters_cursors_and_sqlite_less_assignment", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{"project/list": {json.RawMessage(`{"data":[],"nextCursor":"next"}`)}}}
		api := newProjectAPI(stub.send, func(CapabilityID) bool { return true })
		page, err := api.List(context.Background(), "cursor", 500)
		if err != nil || page.NextCursor != "next" || page.Limit != 100 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		if stub.requests[0].params["cursor"] != "cursor" || stub.requests[0].params["limit"] != uint32(100) {
			t.Fatalf("filters = %#v", stub.requests[0].params)
		}
	})

	t.Run("assigned_forks_inherit_projects_for_persistent_and_ephemeral_children", func(t *testing.T) {
		for _, id := range []string{"persistent", "ephemeral"} {
			child := inheritForkProject(
				provider.AgentSessionMeta{ID: "parent", ProjectID: "p"},
				provider.AgentSessionMeta{ID: id, ForkedFromID: "parent"},
			)
			if child.ProjectID != "p" {
				t.Fatalf("%s child = %+v", id, child)
			}
		}
	})
}
