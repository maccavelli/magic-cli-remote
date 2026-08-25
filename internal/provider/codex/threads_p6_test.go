package codex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

type p6RPCRequest struct {
	method string
	params map[string]any
}

type p6RPCStub struct {
	responses map[string][]json.RawMessage
	errors    map[string][]error
	requests  []p6RPCRequest
}

func (s *p6RPCStub) send(_ context.Context, method string, params any) (json.RawMessage, error) {
	m, _ := params.(map[string]any)
	s.requests = append(s.requests, p6RPCRequest{method: method, params: m})
	if queue := s.errors[method]; len(queue) > 0 {
		s.errors[method] = queue[1:]
		if queue[0] != nil {
			return nil, queue[0]
		}
	}
	queue := s.responses[method]
	if len(queue) == 0 {
		return json.RawMessage(`{}`), nil
	}
	s.responses[method] = queue[1:]
	return queue[0], nil
}

func TestThreadListReadLoadedAndPaginationFixtures(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/list": {
			json.RawMessage(`{"data":[{"id":"thread-1","cwd":"/repo","name":"First","preview":"hello","status":{"type":"idle"},"source":"vscode","createdAt":10,"updatedAt":20,"parentThreadId":"parent","forkedFromId":"fork","projectId":"project-1","section":{"id":"section-1","name":"Pinned","appearance":{"icon":"star","color":"blue"}}}],"nextCursor":"next","backwardsCursor":"back"}`),
		},
		"thread/loaded/list": {json.RawMessage(`{"data":["thread-1"],"nextCursor":null}`)},
	}}
	api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
	page, err := api.List(context.Background(), provider.ThreadListOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Threads) != 1 || page.NextCursor != "next" || page.BackwardsCursor != "back" || page.Source != provider.ThreadSourceNative {
		t.Fatalf("page = %+v", page)
	}
	got := page.Threads[0]
	if got.ID != "thread-1" || got.NativeStatus != "idle" || got.Source != "vscode" || !got.Loaded || !got.Pinned || got.SectionID != "section-1" || got.ParentThreadID != "parent" || got.ForkedFromID != "fork" || got.ProjectID != "project-1" {
		t.Fatalf("thread = %+v", got)
	}
	if len(stub.requests) != 2 || stub.requests[0].method != "thread/list" || stub.requests[0].params["limit"] != uint32(50) || stub.requests[1].method != "thread/loaded/list" {
		t.Fatalf("requests = %+v", stub.requests)
	}
}

func TestThreadReadRenameMetadataForkAndUnsubscribeFixtures(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/read":            {json.RawMessage(`{"thread":{"id":"thread-1","cwd":"/repo","name":"First","preview":"hello","status":{"type":"idle"},"source":"cli","createdAt":10,"updatedAt":20}}`)},
		"thread/name/set":        {json.RawMessage(`{}`)},
		"thread/metadata/update": {json.RawMessage(`{}`)},
		"thread/fork":            {json.RawMessage(`{"thread":{"id":"thread-2","cwd":"/repo","name":"Fork","preview":"","status":{"type":"idle"},"source":"mcremote","createdAt":21,"updatedAt":21,"forkedFromId":"thread-1","projectId":"project-1"}}`)},
		"thread/unsubscribe":     {json.RawMessage(`{}`)},
	}}
	api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
	thread, err := api.Read(context.Background(), "thread-1", false)
	if err != nil || thread.ID != "thread-1" {
		t.Fatalf("read=%+v err=%v", thread, err)
	}
	if err := api.Rename(context.Background(), "thread-1", " Renamed "); err != nil {
		t.Fatal(err)
	}
	if err := api.AssignProject(context.Background(), "thread-1", provider.ProjectAssignment{ProjectID: "project-1", Set: true}); err != nil {
		t.Fatal(err)
	}
	fork, err := api.Fork(context.Background(), "thread-1")
	if err != nil || fork.ID != "thread-2" || fork.ForkedFromID != "thread-1" || fork.ProjectID != "project-1" {
		t.Fatalf("fork=%+v err=%v", fork, err)
	}
	if err := api.Unsubscribe(context.Background(), "thread-1"); err != nil {
		t.Fatal(err)
	}
	wantMethods := []string{"thread/read", "thread/name/set", "thread/metadata/update", "thread/fork", "thread/unsubscribe"}
	gotMethods := make([]string, 0, len(stub.requests))
	for _, request := range stub.requests {
		gotMethods = append(gotMethods, request.method)
	}
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("methods = %v", gotMethods)
	}
	if stub.requests[0].params["includeTurns"] != false || stub.requests[1].params["name"] != "Renamed" || stub.requests[2].params["projectId"] != "project-1" {
		t.Fatalf("requests = %+v", stub.requests)
	}
}

func TestThreadSearchNativeAndStableBoundedFallback(t *testing.T) {
	t.Run("native", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{
			"thread/search":      {json.RawMessage(`{"data":[{"thread":{"id":"native","cwd":"/repo","preview":"needle","status":{"type":"idle"},"source":"cli","createdAt":1,"updatedAt":2},"score":0.9}],"nextCursor":null}`)},
			"thread/loaded/list": {json.RawMessage(`{"data":[]}`)},
		}}
		api := newNativeThreadAPI(stub.send, func(id CapabilityID) bool { return id == CapabilityThreadSearch })
		page, err := api.Search(context.Background(), provider.ThreadSearchOptions{Term: "needle", Limit: 25})
		if err != nil || page.Source != provider.ThreadSourceNativeSearch || len(page.Threads) != 1 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		if stub.requests[0].method != "thread/search" || stub.requests[0].params["limit"] != uint32(25) {
			t.Fatalf("requests = %+v", stub.requests)
		}
	})

	t.Run("fallback", func(t *testing.T) {
		stub := &p6RPCStub{responses: map[string][]json.RawMessage{
			"thread/list": {
				json.RawMessage(`{"data":[{"id":"a","cwd":"/repo","name":"Needle one","preview":"","status":{"type":"idle"},"source":"cli","createdAt":1,"updatedAt":1}],"nextCursor":"c2"}`),
				json.RawMessage(`{"data":[{"id":"b","cwd":"/repo","name":"other","preview":"contains needle","status":{"type":"idle"},"source":"cli","createdAt":1,"updatedAt":2}],"nextCursor":null}`),
			},
			"thread/loaded/list": {json.RawMessage(`{"data":[]}`), json.RawMessage(`{"data":[]}`)},
		}}
		api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return false })
		page, err := api.Search(context.Background(), provider.ThreadSearchOptions{Term: "needle", Limit: 25})
		if err != nil || page.Source != provider.ThreadSourceStableFallback || len(page.Threads) != 2 || !page.Truncated {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		if len(stub.requests) > maxThreadFallbackPages*2 {
			t.Fatalf("fallback was not bounded: %d requests", len(stub.requests))
		}
	})
}

func TestThreadTurnAndItemPaginationAreIndependentlyNegotiated(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"thread/turns/list": {json.RawMessage(`{"data":[{"id":"turn-1","items":[]}],"nextCursor":"turn-next","backwardsCursor":"turn-back"}`)},
		"thread/items/list": {json.RawMessage(`{"data":[{"turnId":"turn-1","item":{"type":"agentMessage","id":"item-1","text":"hello"}}],"nextCursor":"item-next","backwardsCursor":"item-back"}`)},
	}}
	turns := newNativeThreadAPI(stub.send, func(id CapabilityID) bool {
		return id == CapabilityThreadTurnsList
	})
	turnPage, err := turns.ListTurns(context.Background(), provider.NativeThreadHistoryOptions{
		ThreadID: "thread-1", Cursor: "turn-cursor", Limit: 250, SortDirection: "asc", ItemsView: "full",
	})
	if err != nil || len(turnPage.Data) != 1 || turnPage.Source != provider.ThreadSourceNativeTurns || turnPage.Limit != maxNativeThreadHistoryPage || turnPage.NextCursor != "turn-next" || turnPage.BackwardsCursor != "turn-back" {
		t.Fatalf("turn page=%+v err=%v", turnPage, err)
	}
	if _, err := turns.ListItems(context.Background(), provider.NativeThreadHistoryOptions{ThreadID: "thread-1"}); err == nil {
		t.Fatal("item pagination was available without its independent capability")
	}

	items := newNativeThreadAPI(stub.send, func(id CapabilityID) bool {
		return id == CapabilityThreadItemsList
	})
	itemPage, err := items.ListItems(context.Background(), provider.NativeThreadHistoryOptions{
		ThreadID: "thread-1", TurnID: "turn-1", Cursor: "item-cursor", Limit: 25, SortDirection: "desc",
	})
	if err != nil || len(itemPage.Data) != 1 || itemPage.Source != provider.ThreadSourceNativeItems || itemPage.Limit != 25 || itemPage.NextCursor != "item-next" || itemPage.BackwardsCursor != "item-back" {
		t.Fatalf("item page=%+v err=%v", itemPage, err)
	}
	if _, err := items.ListTurns(context.Background(), provider.NativeThreadHistoryOptions{ThreadID: "thread-1"}); err == nil {
		t.Fatal("turn pagination was available without its independent capability")
	}

	if got := stub.requests[0]; got.method != "thread/turns/list" || got.params["threadId"] != "thread-1" || got.params["cursor"] != "turn-cursor" || got.params["limit"] != uint32(maxNativeThreadHistoryPage) || got.params["sortDirection"] != "asc" || got.params["itemsView"] != "full" {
		t.Fatalf("turn request = %#v", got)
	}
	if got := stub.requests[1]; got.method != "thread/items/list" || got.params["threadId"] != "thread-1" || got.params["turnId"] != "turn-1" || got.params["cursor"] != "item-cursor" || got.params["limit"] != uint32(25) || got.params["sortDirection"] != "desc" {
		t.Fatalf("item request = %#v", got)
	}
}

func TestThreadSectionCRUDAndOrderingWireShapes(t *testing.T) {
	stub := &p6RPCStub{responses: map[string][]json.RawMessage{
		"threadSection/list":   {json.RawMessage(`{"data":[{"id":"s1","name":"Work","appearance":{"icon":"briefcase","color":"blue"}}],"nextCursor":"more"}`)},
		"threadSection/create": {json.RawMessage(`{"section":{"id":"s2","name":"Later","appearance":null}}`)},
		"threadSection/update": {json.RawMessage(`{"section":{"id":"s2","name":"Next","appearance":{"icon":"star","color":"gold"}}}`)},
		"thread/section/move":  {json.RawMessage(`{}`), json.RawMessage(`{}`)},
		"threadSection/delete": {json.RawMessage(`{}`)},
	}}
	api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
	page, err := api.ListSections(context.Background(), "", 50)
	if err != nil || len(page.Sections) != 1 || page.NextCursor != "more" {
		t.Fatalf("sections=%+v err=%v", page, err)
	}
	created, err := api.CreateSection(context.Background(), provider.ThreadSectionMutation{Name: "Later"})
	if err != nil || created.ID != "s2" {
		t.Fatalf("create=%+v err=%v", created, err)
	}
	if _, err := api.UpdateSection(context.Background(), "s2", provider.ThreadSectionMutation{Name: "Next", Icon: "star", Color: "gold", AppearanceSet: true}); err != nil {
		t.Fatal(err)
	}
	if err := api.MoveThread(context.Background(), "thread-1", "s2", "thread-2"); err != nil {
		t.Fatal(err)
	}
	if err := api.MoveThread(context.Background(), "thread-1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteSection(context.Background(), "s2"); err != nil {
		t.Fatal(err)
	}
	wantMethods := []string{"threadSection/list", "threadSection/create", "threadSection/update", "thread/section/move", "thread/section/move", "threadSection/delete"}
	gotMethods := make([]string, 0, len(stub.requests))
	for _, request := range stub.requests {
		gotMethods = append(gotMethods, request.method)
	}
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("methods = %v", gotMethods)
	}
	if section, ok := stub.requests[4].params["sectionId"]; !ok || section != nil {
		t.Fatalf("unsection params = %#v", stub.requests[4].params)
	}
}

func TestThreadArchiveDeleteDescendantsAndUnknownOutcomeReconcile(t *testing.T) {
	unknown := errors.New("connection lost after write")
	stub := &p6RPCStub{
		responses: map[string][]json.RawMessage{
			"thread/list": {
				json.RawMessage(`{"data":[{"id":"child-a","cwd":"/repo","preview":"","status":{"type":"idle"},"source":"cli","createdAt":1,"updatedAt":1},{"id":"child-b","cwd":"/repo","preview":"","status":{"type":"idle"},"source":"cli","createdAt":1,"updatedAt":1}],"nextCursor":null}`),
				json.RawMessage(`{"data":[],"nextCursor":null}`),
			},
			"thread/loaded/list": {json.RawMessage(`{"data":["child-b"]}`)},
		},
		errors: map[string][]error{
			"thread/delete": {unknown},
			"thread/read":   {errors.New("thread not found")},
		},
	}
	api := newNativeThreadAPI(stub.send, func(CapabilityID) bool { return true })
	preview, err := api.DeletePreview(context.Background(), "root")
	if err != nil || len(preview.DescendantIDs) != 2 || !preview.HasLoadedDescendants {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	result, err := api.Delete(context.Background(), "root")
	if err != nil || !result.Reconciled || !result.Deleted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err := api.Archive(context.Background(), "root", true); err != nil {
		t.Fatal(err)
	}
	if err := api.Archive(context.Background(), "root", false); err != nil {
		t.Fatal(err)
	}
	if stub.requests[len(stub.requests)-2].method != "thread/archive" || stub.requests[len(stub.requests)-1].method != "thread/unarchive" {
		t.Fatalf("archive methods = %+v", stub.requests)
	}
}

func TestThreadReplayTurnsAreColdReplayOnly(t *testing.T) {
	s := modeTestSession(t, Config{})
	raw := json.RawMessage(`{"thread":{"id":"thread-1","turns":[{"id":"turn-1","items":[{"type":"userMessage","id":"u1","content":[{"type":"text","text":"hello"}]},{"type":"agentMessage","id":"a1","text":"world"}]}]}}`)
	if err := s.emitThreadReplay(raw); err != nil {
		t.Fatal(err)
	}
	first := <-s.events
	second := <-s.events
	if !first.Replay || !second.Replay || first.Text != "hello" || second.Text != "world" {
		t.Fatalf("replay events = %+v %+v", first, second)
	}
}
