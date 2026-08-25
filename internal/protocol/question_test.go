package protocol

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestQuestionRespondPayloadKeyedAndLegacyCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"keyed", `{"session_id":"s","question_id":"q","answers":{"field":["yes"]}}`},
		{"legacy ordered", `{"session_id":"s","question_id":"q","answers":[["yes"],["no"]]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload QuestionRespondPayload
			if err := json.Unmarshal([]byte(tc.raw), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.SessionID != "s" || payload.QuestionID != "q" || len(payload.Answers) == 0 {
				t.Fatalf("payload = %+v", payload)
			}
		})
	}
}

func TestQuestionRespondPayloadRedactsAndClearsAnswers(t *testing.T) {
	const secret = "P2-PROTOCOL-SECRET-SENTINEL"
	payload := QuestionRespondPayload{
		SessionID: "s", QuestionID: "q",
		Answers: map[string][]string{"secret": {secret}},
	}
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("question", slog.Any("payload", payload), slog.Any("answers", payload.Answers))
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("secret reached structured logs: %s", logs.String())
	}
	payload.ClearAnswers()
	if payload.Answers["secret"] != nil {
		t.Fatalf("answer was not cleared: %#v", payload.Answers)
	}
}
