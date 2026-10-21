package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// PLAN 0191 L2: an ACP form elicitation becomes a question form, and the
// answer comes back checked and typed (MADR 0191 D1–D3).
//
// The requests below are built from the SDK's own types. They are the wire
// fixture for MADR 0191 D7 until a live probe replaces them, and they are not a
// capture: no agent Magic runs sends elicitation yet.

func newFormSession(timeout time.Duration) (*session, *recordingHandler) {
	h := &recordingHandler{}
	s := newExtSession(timeout)
	s.log = slog.New(h)
	return s, h
}

type elicitResult struct {
	resp acp.CreateElicitationResponse
	err  error
}

func elicit(s *session, req acp.CreateElicitationRequest) <-chan elicitResult {
	out := make(chan elicitResult, 1)
	go func() {
		resp, err := s.CreateElicitation(context.Background(), req)
		out <- elicitResult{resp, err}
	}()
	return out
}

func waitElicit(t *testing.T, out <-chan elicitResult) acp.CreateElicitationResponse {
	t.Helper()
	select {
	case r := <-out:
		if r.err != nil {
			t.Fatalf("CreateElicitation: %v", r.err)
		}
		return r.resp
	case <-time.After(3 * time.Second):
		t.Fatal("CreateElicitation did not return")
	}
	return acp.CreateElicitationResponse{}
}

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

func strProp(p acp.ElicitationPropertySchemaString) acp.ElicitationPropertySchema {
	p.Type = "string"
	return acp.ElicitationPropertySchema{String: &p}
}

// formRequest is a session-scoped form, tied to a tool call.
func formRequest(props map[string]acp.ElicitationPropertySchema, required ...string) acp.CreateElicitationRequest {
	return acp.CreateElicitationRequest{Form: &acp.CreateElicitationForm{
		Message: "Tell me about the release.",
		Mode:    "form",
		RequestedSchema: acp.ElicitationSchema{
			Title: new("Release details"), Properties: props, Required: required, Type: "object",
		},
		Session: &acp.ElicitationSessionScope{SessionId: "a1", ToolCallId: ptr(acp.ToolCallId("call-7"))},
	}}
}

// releaseForm has one property of every kind Magic draws.
func releaseForm() acp.CreateElicitationRequest {
	return formRequest(map[string]acp.ElicitationPropertySchema{
		"name": strProp(acp.ElicitationPropertySchemaString{Title: new("Name"),
			MinLength: new(uint32(1)), MaxLength: new(uint32(12)), Default: new("Ada")}),
		"email": strProp(acp.ElicitationPropertySchemaString{Format: ptr(acp.StringFormatEmail)}),
		"color": strProp(acp.ElicitationPropertySchemaString{Enum: []string{"red", "green"}}),
		"size": strProp(acp.ElicitationPropertySchemaString{OneOf: []acp.EnumOption{
			{Const: "s", Title: "Small"}, {Const: "l", Title: "Large"}}}),
		"code": strProp(acp.ElicitationPropertySchemaString{Pattern: new("^[A-Z]{3}$")}),
		"age": {Integer: &acp.ElicitationPropertySchemaInteger{Type: "integer",
			Minimum: new(int64(0)), Maximum: new(int64(130))}},
		"ratio": {Number: &acp.ElicitationPropertySchemaNumber{Type: "number", Maximum: new(1.0)}},
		"ok":    {Boolean: &acp.ElicitationPropertySchemaBoolean{Type: "boolean", Default: new(true)}},
		"tags": {Array: &acp.ElicitationPropertySchemaArray{Type: "array",
			Items:    acp.MultiSelectItems{String: &acp.MultiSelectItemsString{Type: "string", Enum: []string{"a", "b", "c"}}},
			MinItems: new(uint64(1)), MaxItems: new(uint64(2))}},
	}, "age", "name")
}

func itemByID(t *testing.T, items []event.QuestionItem, id string) event.QuestionItem {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", id, items)
	return event.QuestionItem{}
}

func TestElicitationFormBecomesAQuestion(t *testing.T) {
	s, _ := newFormSession(0)
	out := elicit(s, releaseForm())
	req := waitEvent(t, s, event.TypeQuestion)

	if !req.AllowDecline || req.ToolID != "call-7" {
		t.Errorf("allow_decline %t, tool %q; want true and call-7", req.AllowDecline, req.ToolID)
	}
	if !strings.Contains(req.Text, "Release details") || !strings.Contains(req.Text, "Tell me about the release.") {
		t.Errorf("text = %q, want the schema title and the message", req.Text)
	}
	var order []string
	for _, it := range req.Questions {
		order = append(order, it.ID)
	}
	// Required first, in the required list's order, then the rest by name.
	// This form is built in Go, so it carries no PropertyOrder and takes the
	// fallback; a form from the wire keeps its agent's order
	// (TestFormFieldsFollowTheAgentsOrder).
	want := []string{"age", "name", "code", "color", "email", "ok", "ratio", "size", "tags"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}

	if it := itemByID(t, req.Questions, "age"); it.Kind != event.QuestionKindInteger || it.Optional ||
		it.Min == nil || *it.Min != 0 || it.Max == nil || *it.Max != 130 {
		t.Errorf("age = %+v", it)
	}
	if it := itemByID(t, req.Questions, "name"); it.Header != "Name" || it.Optional || it.Kind != "" ||
		*it.MinLength != 1 || *it.MaxLength != 12 || !reflect.DeepEqual(it.Default, []string{"Ada"}) {
		t.Errorf("name = %+v", it)
	}
	if it := itemByID(t, req.Questions, "email"); it.Format != "email" || !it.Optional {
		t.Errorf("email = %+v", it)
	}
	if it := itemByID(t, req.Questions, "color"); len(it.Options) != 2 || it.Options[0].OptionID != "red" {
		t.Errorf("color = %+v", it)
	}
	if it := itemByID(t, req.Questions, "size"); len(it.Options) != 2 ||
		it.Options[1].OptionID != "l" || it.Options[1].Name != "Large" {
		t.Errorf("size = %+v, want option l titled Large", it)
	}
	if it := itemByID(t, req.Questions, "code"); it.Pattern != "^[A-Z]{3}$" {
		t.Errorf("code = %+v", it)
	}
	if it := itemByID(t, req.Questions, "ratio"); it.Kind != event.QuestionKindNumber || *it.Max != 1 || it.Min != nil {
		t.Errorf("ratio = %+v", it)
	}
	if it := itemByID(t, req.Questions, "ok"); len(it.Options) != 2 || it.Options[0].OptionID != "true" ||
		it.Options[0].Name != "Yes" || !reflect.DeepEqual(it.Default, []string{"true"}) {
		t.Errorf("ok = %+v", it)
	}
	if it := itemByID(t, req.Questions, "tags"); !it.Multiple || len(it.Options) != 3 ||
		*it.MinItems != 1 || *it.MaxItems != 2 {
		t.Errorf("tags = %+v", it)
	}

	if err := s.RespondQuestion(context.Background(), req.QuestionID, nil, true); err != nil {
		t.Fatal(err)
	}
	if resp := waitElicit(t, out); resp.Cancel == nil {
		t.Fatalf("response = %+v, want cancel", resp)
	}
}

func TestElicitationAnswerIsTypedContent(t *testing.T) {
	s, _ := newFormSession(0)
	out := elicit(s, releaseForm())
	req := waitEvent(t, s, event.TypeQuestion)

	answers := provider.QuestionAnswers{
		"age": {"42"}, "name": {"Grace"}, "email": {"grace@example.com"}, "color": {"green"},
		"size": {"l"}, "code": {"ABC"}, "ratio": {"0.5"}, "ok": {"false"}, "tags": {"a", "c"},
	}
	if err := s.RespondQuestion(context.Background(), req.QuestionID, answers, false); err != nil {
		t.Fatal(err)
	}
	resp := waitElicit(t, out)
	if resp.Accept == nil {
		t.Fatalf("response = %+v, want accept", resp)
	}
	want := map[string]acp.ElicitationContentValue{
		"age":   acp.NewElicitationContentValueInteger(42),
		"name":  acp.NewElicitationContentValueString("Grace"),
		"email": acp.NewElicitationContentValueString("grace@example.com"),
		"color": acp.NewElicitationContentValueString("green"),
		"size":  acp.NewElicitationContentValueString("l"),
		"code":  acp.NewElicitationContentValueString("ABC"),
		"ratio": acp.NewElicitationContentValueNumber(0.5),
		"ok":    acp.NewElicitationContentValueBoolean(false),
		"tags":  acp.NewElicitationContentValueStringArray([]string{"a", "c"}),
	}
	if !reflect.DeepEqual(resp.Accept.Content, want) {
		t.Fatalf("content = %+v\nwant      %+v", resp.Accept.Content, want)
	}
	if ev := waitEvent(t, s, event.TypeQuestionResolved); ev.Status != event.PermissionStatusResolved {
		t.Fatalf("resolved status = %q", ev.Status)
	}
}

func TestElicitationOptionalFieldsMayBeEmpty(t *testing.T) {
	s, _ := newFormSession(0)
	out := elicit(s, releaseForm())
	req := waitEvent(t, s, event.TypeQuestion)
	if err := s.RespondQuestion(context.Background(), req.QuestionID,
		provider.QuestionAnswers{"age": {"7"}, "name": {"Ada"}, "email": {"  "}, "tags": {}}, false); err != nil {
		t.Fatal(err)
	}
	resp := waitElicit(t, out)
	if resp.Accept == nil || len(resp.Accept.Content) != 2 {
		t.Fatalf("response = %+v, want accept with only age and name", resp)
	}
}

// TestElicitationRefusesABadAnswerAndKeepsTheForm: each broken rule is
// refused with ErrQuestionInvalid naming the field, and the form stays
// pending until a valid answer arrives.
func TestElicitationRefusesABadAnswerAndKeepsTheForm(t *testing.T) {
	s, _ := newFormSession(0)
	out := elicit(s, releaseForm())
	req := waitEvent(t, s, event.TypeQuestion)
	good := func() provider.QuestionAnswers {
		return provider.QuestionAnswers{"age": {"42"}, "name": {"Grace"}}
	}
	for _, tc := range []struct {
		name, field string
		change      func(provider.QuestionAnswers)
	}{
		{"required left empty", "age", func(a provider.QuestionAnswers) { delete(a, "age") }},
		{"not a whole number", "age", func(a provider.QuestionAnswers) { a["age"] = []string{"4.5"} }},
		{"above the maximum", "age", func(a provider.QuestionAnswers) { a["age"] = []string{"131"} }},
		{"too long", "name", func(a provider.QuestionAnswers) { a["name"] = []string{"Grace Hopper Jr"} }},
		{"not an email", "email", func(a provider.QuestionAnswers) { a["email"] = []string{"nope"} }},
		{"not an option", "color", func(a provider.QuestionAnswers) { a["color"] = []string{"blue"} }},
		{"two answers to one choice", "color", func(a provider.QuestionAnswers) { a["color"] = []string{"red", "green"} }},
		{"pattern", "code", func(a provider.QuestionAnswers) { a["code"] = []string{"abc"} }},
		{"not a number", "ratio", func(a provider.QuestionAnswers) { a["ratio"] = []string{"half"} }},
		{"not yes or no", "ok", func(a provider.QuestionAnswers) { a["ok"] = []string{"maybe"} }},
		{"too many choices", "tags", func(a provider.QuestionAnswers) { a["tags"] = []string{"a", "b", "c"} }},
		{"an unknown choice", "tags", func(a provider.QuestionAnswers) { a["tags"] = []string{"z"} }},
	} {
		a := good()
		tc.change(a)
		err := s.RespondQuestion(context.Background(), req.QuestionID, a, false)
		if !errors.Is(err, provider.ErrQuestionInvalid) || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: err = %v, want ErrQuestionInvalid naming %q", tc.name, err, tc.field)
		}
	}
	// Still pending: the valid answer is accepted.
	if err := s.RespondQuestion(context.Background(), req.QuestionID, good(), false); err != nil {
		t.Fatalf("a valid answer after refusals: %v", err)
	}
	if resp := waitElicit(t, out); resp.Accept == nil {
		t.Fatalf("response = %+v, want accept", resp)
	}
}

func TestElicitationDeclineCancelAndTimeout(t *testing.T) {
	t.Run("decline", func(t *testing.T) {
		s, _ := newFormSession(0)
		out := elicit(s, releaseForm())
		req := waitEvent(t, s, event.TypeQuestion)
		if err := s.DeclineQuestion(context.Background(), req.QuestionID); err != nil {
			t.Fatal(err)
		}
		if resp := waitElicit(t, out); resp.Decline == nil {
			t.Fatalf("response = %+v, want decline", resp)
		}
		if ev := waitEvent(t, s, event.TypeQuestionResolved); ev.Status != event.PermissionStatusResolved {
			t.Fatalf("resolved status = %q, want resolved", ev.Status)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		s, _ := newFormSession(0)
		out := elicit(s, releaseForm())
		req := waitEvent(t, s, event.TypeQuestion)
		if err := s.RespondQuestion(context.Background(), req.QuestionID, nil, true); err != nil {
			t.Fatal(err)
		}
		if resp := waitElicit(t, out); resp.Cancel == nil {
			t.Fatalf("response = %+v, want cancel", resp)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		s, _ := newFormSession(30 * time.Millisecond)
		out := elicit(s, releaseForm())
		_ = waitEvent(t, s, event.TypeQuestion)
		if resp := waitElicit(t, out); resp.Cancel == nil {
			t.Fatalf("response = %+v, want cancel", resp)
		}
		if ev := waitEvent(t, s, event.TypeQuestionResolved); !ev.TimedOut {
			t.Fatal("a timed-out form must say so")
		}
	})
	t.Run("a grok question cannot be declined", func(t *testing.T) {
		s, _ := newFormSession(0)
		out := callExtension(context.Background(), s, methodAskUserQuestion, askParams(t, false))
		req := waitEvent(t, s, event.TypeQuestion)
		if req.AllowDecline {
			t.Fatal("grok's ask_user_question offered a decline it cannot return")
		}
		if err := s.DeclineQuestion(context.Background(), req.QuestionID); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-out:
			if res, ok := got.(askUserQuestionResult); !ok || res.Outcome != askOutcomeCancelled {
				t.Fatalf("result = %+v, want a cancelled outcome", got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a decline left grok's question unanswered")
		}
	})
}

// TestElicitationFormsMagicCannotShow: each is answered cancel without a form
// on the phone, and declined by name (MADR 0191 D1, D2).
func TestElicitationFormsMagicCannotShow(t *testing.T) {
	color := acp.ElicitationPropertySchema{Other: &acp.ElicitationPropertySchemaOther{Type: "color"}}
	name := strProp(acp.ElicitationPropertySchemaString{})
	requestScoped := formRequest(map[string]acp.ElicitationPropertySchema{"name": name})
	requestScoped.Form.Session = nil
	requestScoped.Form.Request = &acp.ElicitationRequestScope{RequestId: acp.RequestId{}}

	for _, tc := range []struct {
		name    string
		req     acp.CreateElicitationRequest
		decline string
		notice  bool
	}{
		{"request scope", requestScoped, "elicitation.request_scope", false},
		{"other mode", acp.CreateElicitationRequest{Other: &acp.CreateElicitationOther{Mode: "_voice", Message: "m"}},
			"elicitation.mode._voice", false},
		{"url mode", acp.CreateElicitationRequest{Url: &acp.CreateElicitationUrl{Mode: "url", Message: "m",
			ElicitationId: "e1", Url: "https://example.com", Session: &acp.ElicitationSessionScope{SessionId: "a1"}}},
			"elicitation.url", false},
		{"required unknown property", formRequest(map[string]acp.ElicitationPropertySchema{
			"name": name, "color": color}, "color"), "elicitation.schema.color", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h := newFormSession(0)
			resp := waitElicit(t, elicit(s, tc.req))
			if resp.Cancel == nil {
				t.Fatalf("response = %+v, want cancel", resp)
			}
			sawNotice := false
			for len(s.events) > 0 {
				ev := <-s.events
				if ev.Type == event.TypeQuestion {
					t.Fatalf("a form Magic cannot show reached the phone: %+v", ev)
				}
				if ev.Type == event.TypeNotice {
					sawNotice = true
				}
			}
			if sawNotice != tc.notice {
				t.Errorf("notice = %t, want %t", sawNotice, tc.notice)
			}
			if variants, _ := h.declined(); !reflect.DeepEqual(variants, []string{tc.decline}) {
				t.Errorf("declined %v, want [%s]", variants, tc.decline)
			}
		})
	}
}

// TestElicitationDropsWhatItCannotDraw: an optional unknown property is left
// out, a pattern RE2 cannot compile is not enforced, an unknown format is
// dropped, and _meta is declined; the rest of the form still reaches the phone.
func TestElicitationDropsWhatItCannotDraw(t *testing.T) {
	s, h := newFormSession(0)
	req := formRequest(map[string]acp.ElicitationPropertySchema{
		"name":  strProp(acp.ElicitationPropertySchemaString{Pattern: new(`(?<=x)y`), Format: ptr(acp.StringFormat("hostname"))}),
		"color": {Other: &acp.ElicitationPropertySchemaOther{Type: "color"}},
	}, "name")
	req.Form.Meta = map[string]any{"x": 1}
	out := elicit(s, req)
	q := waitEvent(t, s, event.TypeQuestion)
	if len(q.Questions) != 1 || q.Questions[0].ID != "name" || q.Questions[0].Pattern != "" || q.Questions[0].Format != "" {
		t.Fatalf("questions = %+v, want name alone, without the pattern or the format", q.Questions)
	}
	if err := s.RespondQuestion(context.Background(), q.QuestionID, provider.QuestionAnswers{"name": {"anything"}}, false); err != nil {
		t.Fatalf("an unenforced pattern refused an answer: %v", err)
	}
	if resp := waitElicit(t, out); resp.Accept == nil {
		t.Fatalf("response = %+v, want accept", resp)
	}
	variants, _ := h.declined()
	want := []string{"elicitation._meta", "elicitation.schema.color", "elicitation.schema.format", "elicitation.schema.pattern"}
	if !reflect.DeepEqual(sortedCopy(variants), want) {
		t.Fatalf("declined %v, want %v", variants, want)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// questionOrder asks s with req, returns the question items' IDs in the order
// the phone would draw them, and cancels the form.
func questionOrder(t *testing.T, s *session, req acp.CreateElicitationRequest) []string {
	t.Helper()
	out := elicit(s, req)
	q := waitEvent(t, s, event.TypeQuestion)
	var order []string
	for _, it := range q.Questions {
		order = append(order, it.ID)
	}
	if err := s.RespondQuestion(context.Background(), q.QuestionID, nil, true); err != nil {
		t.Fatal(err)
	}
	waitElicit(t, out)
	return order
}

// TestFormFieldsFollowTheAgentsOrder is MADR 0191's amendment of 2026-10-16
// (G1): a form decoded from the wire keeps the order its agent wrote, which
// acp-go-sdk2 v1.15.0 records in PropertyOrder. Required fields are not moved
// to the top; each item already says whether it is optional.
func TestFormFieldsFollowTheAgentsOrder(t *testing.T) {
	var schema acp.ElicitationSchema
	if err := json.Unmarshal([]byte(`{"type":"object","required":["mid"],"properties":{`+
		`"zeta":{"type":"string"},"alpha":{"type":"string"},"mid":{"type":"string"}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	req := formRequest(nil)
	req.Form.RequestedSchema = schema

	s, _ := newFormSession(0)
	if got, want := questionOrder(t, s, req), []string{"zeta", "alpha", "mid"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want the agent's order %v", got, want)
	}
}

// TestFormFieldOrderSkipsRepeatsAndUnknownNames: PropertyOrder is the agent's
// word, so a name it repeats is drawn once, a name with no property is
// skipped, and a property it leaves out still appears, after the ones it names.
func TestFormFieldOrderSkipsRepeatsAndUnknownNames(t *testing.T) {
	req := formRequest(map[string]acp.ElicitationPropertySchema{
		"a": strProp(acp.ElicitationPropertySchemaString{}),
		"b": strProp(acp.ElicitationPropertySchemaString{}),
		"c": strProp(acp.ElicitationPropertySchemaString{}),
	})
	req.Form.RequestedSchema.PropertyOrder = []string{"b", "ghost", "b", "a"}

	s, _ := newFormSession(0)
	if got, want := questionOrder(t, s, req), []string{"b", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
