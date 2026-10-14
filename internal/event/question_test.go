package event

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestQuestionFieldsAreAdditiveAndSecretValuesHaveNoEventSlot(t *testing.T) {
	event := Event{
		Type: TypeQuestion, QuestionID: "form",
		Questions: []QuestionItem{{
			ID: "token", Header: "Token", Text: "Enter token", Custom: true, Secret: true,
			Options: []PermissionOption{{OptionID: "saved", Name: "Saved", Description: "Use host token"}},
		}},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"token"`, `"secret":true`, `"description":"Use host token"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("event JSON %s lacks %s", raw, want)
		}
	}
	if strings.Contains(string(raw), "answer") || strings.Contains(string(raw), "value") {
		t.Fatalf("question event unexpectedly has a value slot: %s", raw)
	}
}

// TestQuestionFormFieldsAreOmittedWhenUnused is PLAN 0191 L1 step 1: the
// typed-form fields are additive, so a form from a provider that sends none of
// them encodes exactly as it did before MADR 0191.
func TestQuestionFormFieldsAreOmittedWhenUnused(t *testing.T) {
	ev := Event{Type: TypeQuestion, QuestionID: "q", Questions: []QuestionItem{{
		ID: "a", Header: "H", Text: "T", Multiple: true, Custom: true, Secret: true,
		Options: []PermissionOption{{OptionID: "x", Name: "X"}},
	}}}
	raw, err := json.Marshal(ev.Questions[0])
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"a","header":"H","text":"T","multiple":true,"custom":true,"secret":true,"options":[{"option_id":"x","name":"X"}]}`
	if string(raw) != want {
		t.Fatalf("question item changed shape:\n got: %s\nwant: %s", raw, want)
	}
	all, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(all), "allow_decline") {
		t.Fatalf("allow_decline present on a form that offers no decline: %s", all)
	}
}

// TestQuestionFormFieldsRoundTrip: every typed-form field survives the wire
// (MADR 0191 D4). Bounds are pointers, so a bound of zero is still sent.
func TestQuestionFormFieldsRoundTrip(t *testing.T) {
	zero, ten := 0.0, 10.0
	one, eight, two, four := 1, 8, 2, 4
	in := Event{Type: TypeQuestion, QuestionID: "q", AllowDecline: true, Questions: []QuestionItem{{
		ID: "age", Kind: QuestionKindInteger, Optional: true, Default: []string{"3"},
		Min: &zero, Max: &ten, MinLength: &one, MaxLength: &eight, MinItems: &two, MaxItems: &four,
		Format: "email", Pattern: "^a",
	}}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"integer"`, `"optional":true`, `"default":["3"]`, `"min":0`,
		`"max":10`, `"min_length":1`, `"max_length":8`, `"min_items":2`, `"max_items":4`,
		`"format":"email"`, `"pattern":"^a"`, `"allow_decline":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("event JSON lacks %s: %s", want, raw)
		}
	}
	var out Event
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Questions, in.Questions) || !out.AllowDecline {
		t.Fatalf("round trip lost fields:\n got %+v\nwant %+v", out.Questions, in.Questions)
	}
}
