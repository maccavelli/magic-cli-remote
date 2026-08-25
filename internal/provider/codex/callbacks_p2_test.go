package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

const questionSecretSentinel = "P2-SECRET-MUST-NEVER-PERSIST"

func TestQuestionRequestMatchesCodex01491(t *testing.T) {
	s, _ := permSession(t)
	s.pendingQuestions = make(map[string]pendingQuestion)
	params := json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","itemId":"item-1",
		"isBlocking":true,"autoResolutionMs":null,
		"questions":[
			{"id":"language","header":"Language","question":"Choose one",
			 "options":[{"label":"Go","description":"Fast and typed"}],
			 "isOther":true,"isSecret":false},
			{"id":"token","header":"Token","question":"Enter token",
			 "options":null,"isOther":false,"isSecret":true}
		]
	}`)

	s.handleUserInputRequest("item/tool/requestUserInput", json.RawMessage(`41`), params)
	events := drainEvents(s)
	if len(events) != 1 || events[0].Type != event.TypeQuestion {
		t.Fatalf("events = %+v, want one question_request", events)
	}
	got := events[0].Questions
	if len(got) != 2 {
		t.Fatalf("questions = %+v", got)
	}
	if got[0].ID != "language" || !got[0].Custom || got[0].Secret {
		t.Fatalf("first question metadata lost: %+v", got[0])
	}
	if len(got[0].Options) != 1 || got[0].Options[0].Description != "Fast and typed" {
		t.Fatalf("option description lost: %+v", got[0].Options)
	}
	if got[1].ID != "token" || !got[1].Secret {
		t.Fatalf("secret question metadata lost: %+v", got[1])
	}

	raw, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), questionSecretSentinel) {
		t.Fatalf("secret value reached event JSON: %s", raw)
	}
}

func TestQuestionResponseIsKeyedAndExactlyOnce(t *testing.T) {
	s, f := permSession(t)
	s.pendingQuestions = make(map[string]pendingQuestion)
	s.handleUserInputRequest("item/tool/requestUserInput", json.RawMessage(`42`), json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,
		"questions":[
			{"id":"one","header":"One","question":"Pick","options":[{"label":"A","description":"first"}]},
			{"id":"many","header":"Many","question":"Pick many","options":[{"label":"B","description":"second"}],"isOther":true},
			{"id":"secret","header":"Secret","question":"Enter","options":null,"isSecret":true}
		]
	}`))
	request := drainEvents(s)[0]

	answers := map[string][]string{
		"one":    {"A"},
		"many":   {"B", "Other value"},
		"secret": {questionSecretSentinel},
	}
	if err := s.RespondQuestion(context.Background(), request.QuestionID, answers, false); err != nil {
		t.Fatal(err)
	}
	if len(f.responses) != 1 {
		t.Fatalf("responses = %+v", f.responses)
	}
	want := map[string]any{"answers": map[string]any{
		"one":    map[string]any{"answers": []string{"A"}},
		"many":   map[string]any{"answers": []string{"B", "Other value"}},
		"secret": map[string]any{"answers": []string{questionSecretSentinel}},
	}}
	if !jsonValueEqual(f.responses[0]["result"], want) {
		t.Fatalf("result = %#v, want %#v", f.responses[0]["result"], want)
	}
	if answers["secret"] != nil {
		t.Fatalf("secret answer was not cleared after dispatch: %#v", answers["secret"])
	}
	if err := s.RespondQuestion(context.Background(), request.QuestionID, map[string][]string{"one": {"A"}}, false); err != nil {
		t.Fatalf("duplicate response should be an exactly-once no-op: %v", err)
	}
	if len(f.responses) != 1 {
		t.Fatalf("duplicate reached Codex: %+v", f.responses)
	}
}

func TestQuestionResponseRejectsMismatchedIDsAndCancels(t *testing.T) {
	s, f := permSession(t)
	s.pendingQuestions = make(map[string]pendingQuestion)
	params := json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","isBlocking":true,"questions":[{"id":"known","header":"H","question":"Q","options":null}]}`)
	s.handleUserInputRequest("item/tool/requestUserInput", json.RawMessage(`43`), params)
	request := drainEvents(s)[0]
	if err := s.RespondQuestion(context.Background(), request.QuestionID, map[string][]string{"unknown": {"x"}}, false); err == nil {
		t.Fatal("mismatched upstream id accepted")
	}
	if len(f.responses) != 0 {
		t.Fatalf("invalid answer reached Codex: %+v", f.responses)
	}
	if err := s.RespondQuestion(context.Background(), request.QuestionID, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(f.responses) != 1 || f.responses[0]["error"] == nil {
		t.Fatalf("cancel response = %+v", f.responses)
	}
}

func TestGranularPermissionResponseNeverUsesDecision(t *testing.T) {
	s, f := permSession(t)
	s.handleApprovalRequest("item/permissions/requestApproval", json.RawMessage(`51`), json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","cwd":"/work","startedAtMs":1,
		"permissions":{"network":{"enabled":true},"fileSystem":{"read":["/work/a"],"write":["/work/b"]}},
		"reason":"needs access"
	}`))
	permID := firstPermissionID(t, drainEvents(s))
	if err := s.RespondPermission(context.Background(), permID, "accept", false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	result, _ := f.responses[0]["result"].(map[string]any)
	if _, exists := result["decision"]; exists {
		t.Fatalf("granular response contains forbidden decision: %#v", result)
	}
	if result["scope"] != "turn" || result["permissions"] == nil {
		t.Fatalf("granular response = %#v", result)
	}

	s2, f2 := permSession(t)
	s2.handleApprovalRequest("item/permissions/requestApproval", json.RawMessage(`52`), json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","itemId":"item-2","cwd":"/work","startedAtMs":1,
		"permissions":{"network":{"enabled":true}},"reason":"network"
	}`))
	deniedID := firstPermissionID(t, drainEvents(s2))
	if err := s2.RespondPermission(context.Background(), deniedID, "decline", false, "dev-1"); err != nil {
		t.Fatal(err)
	}
	denied, _ := f2.responses[0]["result"].(map[string]any)
	permissions, ok := denied["permissions"].(map[string]any)
	if !ok || len(permissions) != 0 {
		t.Fatalf("denial must be an explicit empty grant: %#v", denied)
	}
}

func TestGranularPermissionScopeAndStrictReviewStates(t *testing.T) {
	for _, tc := range []struct {
		option string
		scope  string
		strict bool
	}{
		{"accept", "turn", false},
		{"acceptStrict", "turn", true},
		{"acceptForSession", "session", false},
		{"acceptStrictForSession", "session", true},
	} {
		t.Run(tc.option, func(t *testing.T) {
			s, f := permSession(t)
			s.handleApprovalRequest("item/permissions/requestApproval", json.RawMessage(`53`), json.RawMessage(`{
				"threadId":"thread-1","turnId":"turn-1","itemId":"item","cwd":"/work","startedAtMs":1,
				"permissions":{"network":{"enabled":true},"fileSystem":{"read":["/work/a"]}}
			}`))
			id := firstPermissionID(t, drainEvents(s))
			if err := s.RespondPermission(context.Background(), id, tc.option, false, "dev"); err != nil {
				t.Fatal(err)
			}
			result := f.responses[0]["result"].(map[string]any)
			if result["scope"] != tc.scope || result["strictAutoReview"] != tc.strict {
				t.Fatalf("result = %#v, want scope=%s strict=%v", result, tc.scope, tc.strict)
			}
			grant := result["permissions"].(map[string]any)
			if _, ok := grant["network"]; !ok {
				t.Fatalf("accepted grant is not the requested subset: %#v", grant)
			}
		})
	}
}

func TestServerRequestResolvedIsAuthoritative(t *testing.T) {
	s, f := permSession(t)
	s.pendingQuestions = make(map[string]pendingQuestion)
	s.handleUserInputRequest("item/tool/requestUserInput", json.RawMessage(`71`), json.RawMessage(`{
		"threadId":"thread-1","turnId":"turn-1","itemId":"item","isBlocking":true,
		"questions":[{"id":"field","header":"H","question":"Q","options":null}]
	}`))
	request := drainEvents(s)[0]
	s.handleNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"thread-1","requestId":71}`))
	events := drainEvents(s)
	if len(events) != 1 || events[0].Type != event.TypeQuestionResolved || events[0].QuestionID != request.QuestionID {
		t.Fatalf("resolution events = %+v", events)
	}
	if err := s.RespondQuestion(context.Background(), request.QuestionID, map[string][]string{"field": {"late"}}, false); err == nil {
		t.Fatal("late phone response must return not_found")
	}
	if len(f.responses) != 0 {
		t.Fatalf("authoritative resolution caused a second Codex reply: %+v", f.responses)
	}
}

func TestApprovalResponseKindsAreMethodSpecific(t *testing.T) {
	tests := []struct {
		method     string
		params     string
		allow      string
		wantResult map[string]any
	}{
		{"item/commandExecution/requestApproval", `{"threadId":"thread-1","turnId":"turn-1","itemId":"c","startedAtMs":1,"command":"git status"}`, "accept", map[string]any{"decision": "accept"}},
		{"item/fileChange/requestApproval", `{"threadId":"thread-1","turnId":"turn-1","itemId":"f","startedAtMs":1,"reason":"edit"}`, "accept", map[string]any{"decision": "accept"}},
		{"mcpServer/elicitation/request", `{"threadId":"thread-1","turnId":null,"serverName":"docs"}`, "accept", map[string]any{"action": "accept"}},
		{"applyPatchApproval", `{"conversationId":"thread-1","callId":"p","fileChanges":{"a":{"type":"add","content":"x"}}}`, "approved", map[string]any{"decision": "approved"}},
		{"execCommandApproval", `{"conversationId":"thread-1","callId":"e","command":["pwd"],"cwd":"/work","parsedCmd":[]}`, "approved", map[string]any{"decision": "approved"}},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			s, f := permSession(t)
			s.handleApprovalRequest(tc.method, json.RawMessage(`61`), json.RawMessage(tc.params))
			id := firstPermissionID(t, drainEvents(s))
			if err := s.RespondPermission(context.Background(), id, tc.allow, false, "dev"); err != nil {
				t.Fatal(err)
			}
			if !jsonValueEqual(f.responses[0]["result"], tc.wantResult) {
				t.Fatalf("result = %#v, want %#v", f.responses[0]["result"], tc.wantResult)
			}
		})
	}
}

func jsonValueEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	var av, bv any
	_ = json.Unmarshal(ab, &av)
	_ = json.Unmarshal(bb, &bv)
	return jsonSemanticallyEqualForTest(av, bv)
}

func jsonSemanticallyEqualForTest(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
