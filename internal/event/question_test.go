package event

import (
	"encoding/json"
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
