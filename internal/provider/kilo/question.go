package kilo

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// engineQuestion is the Kilo question.asked / list payload shape.
type engineQuestion struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		Multiple bool `json:"multiple"`
		Custom   bool `json:"custom"`
	} `json:"questions"`
}

// RespondQuestion implements dialect reply/reject for Kilo questions.
func (o *httpSession) RespondQuestion(ctx context.Context, questionID string, answers [][]string, cancelled bool) error {
	if questionID == "" {
		return nil
	}
	if cancelled {
		return o.h.API()(ctx, "POST", "/question/"+questionID+"/reject"+o.dir(), nil, nil)
	}
	// answers: array of label arrays in question order (SDK QuestionAnswer).
	body := map[string]any{"answers": answers}
	if answers == nil {
		body["answers"] = [][]string{}
	}
	return o.h.API()(ctx, "POST", "/question/"+questionID+"/reply"+o.dir(), body, nil)
}

// handleQuestionAsked maps question.asked / question.v2.asked to question_request.
// AlwaysApprove does NOT auto-answer questions (Owner Q6).
func (o *httpSession) handleQuestionAsked(props json.RawMessage) {
	var q engineQuestion
	if json.Unmarshal(props, &q) != nil || q.ID == "" {
		return
	}
	items := make([]event.QuestionItem, 0, len(q.Questions))
	var headers []string
	for _, qi := range q.Questions {
		opts := make([]event.PermissionOption, 0, len(qi.Options))
		for _, opt := range qi.Options {
			label := opt.Label
			if label == "" {
				continue
			}
			// option_id == label (Kilo reply wire). Name is the short display.
			_ = opt.Description // reserved for future UI field
			opts = append(opts, event.PermissionOption{
				OptionID: label,
				Name:     label,
			})
		}
		items = append(items, event.QuestionItem{
			Header:   qi.Header,
			Text:     qi.Question,
			Multiple: qi.Multiple,
			Custom:   qi.Custom,
			Options:  opts,
		})
		if qi.Header != "" {
			headers = append(headers, qi.Header)
		} else if qi.Question != "" {
			headers = append(headers, clip(qi.Question, 40))
		}
	}
	summary := strings.Join(headers, " · ")
	if summary == "" {
		summary = "Agent question"
	}
	o.h.TrackQuestion(q.ID)
	o.h.Emit(event.Event{
		Type:       event.TypeQuestion,
		QuestionID: q.ID,
		Status:     "pending",
		Text:       summary,
		Questions:  items,
	})
}

func (o *httpSession) handleQuestionResolved(props json.RawMessage, cancelled bool) {
	var p struct {
		RequestID string `json:"requestID"`
		ID        string `json:"id"`
	}
	_ = json.Unmarshal(props, &p)
	id := firstNonEmpty(p.RequestID, p.ID)
	if id == "" {
		return
	}
	if !o.h.TakeQuestionPending(id) {
		return
	}
	status := event.PermissionStatusResolved
	if cancelled {
		status = event.PermissionStatusCancelled
	}
	o.h.Emit(event.Event{
		Type:       event.TypeQuestionResolved,
		QuestionID: id,
		Status:     status,
	})
}

// resyncPendingQuestions re-emits open questions for this tree (PR5 extension).
func (o *httpSession) resyncPendingQuestions(ctx context.Context, parent string, treeIDs map[string]struct{}) {
	var list []engineQuestion
	if err := o.h.API()(ctx, "GET", "/question"+o.dir(), nil, &list); err != nil {
		return
	}
	for _, q := range list {
		sid := q.SessionID
		if sid != "" {
			if _, ok := treeIDs[sid]; !ok {
				continue
			}
		}
		b, err := json.Marshal(q)
		if err != nil {
			continue
		}
		o.handleQuestionAsked(b)
	}
}
