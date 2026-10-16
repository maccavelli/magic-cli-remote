package acpagent

import (
	"context"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// CreateElicitation implements acp.Client for form mode (MADR 0191 D1–D3).
//
// A session-scoped form becomes a question_request on the phone, answered
// through the question flow. The answer is checked against the schema and
// returned as typed content; an explicit decline and a cancel come back as
// themselves. Everything Magic cannot show — URL mode, a mode it does not
// know, a form outside any session, a required field of an unknown type — is
// answered cancel and declined by name. Elicitation is advertised as form only
// (D6), so URL mode reaching here means an agent ignored the advertisement.
func (s *session) CreateElicitation(ctx context.Context, req acp.CreateElicitationRequest) (acp.CreateElicitationResponse, error) {
	switch {
	case req.Form != nil:
	case req.Url != nil:
		s.declineUpdate("elicitation.url")
		return acp.NewCreateElicitationResponseCancel(), nil
	case req.Other != nil:
		s.declineUpdate("elicitation.mode." + req.Other.Mode)
		return acp.NewCreateElicitationResponseCancel(), nil
	default:
		s.declineUpdate("elicitation.empty")
		return acp.NewCreateElicitationResponseCancel(), nil
	}
	f := req.Form
	if f.Session == nil {
		// Request-scoped: there is no session on the phone to show it in.
		s.declineUpdate("elicitation.request_scope")
		return acp.NewCreateElicitationResponseCancel(), nil
	}
	if len(f.Meta) > 0 {
		s.declineUpdate("elicitation._meta")
	}

	fields, missing := s.formFields(f.RequestedSchema)
	if missing != "" {
		s.emit(event.Event{
			Type:      event.TypeNotice,
			SessionID: s.localID,
			Timestamp: time.Now().UTC(),
			Text: fmt.Sprintf("The agent asked for %q in a form, as a kind of field Magic can't show, "+
				"so the form was cancelled.", missing),
		})
		return acp.NewCreateElicitationResponseCancel(), nil
	}

	items := make([]event.QuestionItem, len(fields))
	for i, fd := range fields {
		items[i] = fd.item
	}
	toolID := ""
	if f.Session.ToolCallId != nil {
		toolID = string(*f.Session.ToolCallId)
	}
	var head []string
	for _, p := range []*string{f.RequestedSchema.Title, &f.Message, f.RequestedSchema.Description} {
		if p != nil && strings.TrimSpace(*p) != "" {
			head = append(head, strings.TrimSpace(*p))
		}
	}

	qID := uuid.NewString()
	res := s.awaitQuestion(ctx, qID, event.Event{
		Type:         event.TypeQuestion,
		SessionID:    s.localID,
		Timestamp:    time.Now().UTC(),
		QuestionID:   qID,
		ToolID:       toolID,
		Status:       "pending",
		Text:         strings.Join(head, "\n\n"),
		Questions:    items,
		AllowDecline: true,
	}, &questionWaiter{
		form: func(a provider.QuestionAnswers) (map[string]acp.ElicitationContentValue, error) {
			return checkForm(fields, a)
		},
		allowDecline: true,
	})
	switch {
	case res.declined:
		return acp.NewCreateElicitationResponseDecline(), nil
	case res.cancelled || res.content == nil:
		return acp.NewCreateElicitationResponseCancel(), nil
	}
	resp := acp.NewCreateElicitationResponseAccept()
	resp.Accept.Content = res.content
	return resp, nil
}

// CompleteElicitation is part of acp.Client. It completes a URL-mode
// elicitation, which Magic does not advertise (MADR 0191 D1), so the SDK
// refuses it before this runs.
func (s *session) CompleteElicitation(_ context.Context, _ acp.CompleteElicitationNotification) error {
	return acp.NewMethodNotFound(acp.ClientMethodElicitationComplete)
}

// The answer kinds a form field is checked and typed as.
const (
	fieldText    = "text"
	fieldChoice  = "choice"
	fieldMulti   = "multi"
	fieldNumber  = "number"
	fieldInteger = "integer"
	fieldBool    = "boolean"
)

// formField is one schema property, as the phone draws it and as its answer
// is checked.
type formField struct {
	name     string
	kind     string
	required bool
	item     event.QuestionItem
	pattern  *regexp.Regexp
}

// knownFormats are the string formats the phone can say something about and
// the daemon can check (ACP StringFormat).
var knownFormats = map[string]bool{"email": true, "uri": true, "date": true, "date-time": true}

// formFields maps a schema's properties onto question items (MADR 0191 D2).
//
// Fields follow the agent's order, which the SDK records in PropertyOrder for
// every decoded form (MADR 0191, amendment of 2026-10-16): a repeated name is
// drawn once, a name with no property is skipped, and a property the order
// leaves out follows, by name. A schema with no order, as one built in Go is,
// keeps D2's original rule: required fields first, in the order the schema
// lists them, then the rest by name.
//
// It returns the name of a required field Magic cannot draw, if there is one,
// in which case the form cannot be completed.
func (s *session) formFields(schema acp.ElicitationSchema) ([]formField, string) {
	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; ok {
			required[name] = true
		}
	}
	first := schema.PropertyOrder
	if len(first) == 0 {
		first = schema.Required
	}
	placed := make(map[string]bool, len(schema.Properties))
	var order []string
	for _, name := range first {
		if _, ok := schema.Properties[name]; ok && !placed[name] {
			placed[name] = true
			order = append(order, name)
		}
	}
	var rest []string
	for name := range schema.Properties {
		if !placed[name] {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	order = append(order, rest...)

	fields := make([]formField, 0, len(order))
	for _, name := range order {
		fd, ok := s.formField(name, schema.Properties[name], required[name])
		if !ok {
			if required[name] {
				return nil, name
			}
			continue
		}
		fields = append(fields, fd)
	}
	return fields, ""
}

// formField maps one property; ok is false for a type Magic cannot draw.
func (s *session) formField(name string, p acp.ElicitationPropertySchema, required bool) (formField, bool) {
	fd := formField{name: name, required: required}
	it := event.QuestionItem{ID: name, Header: name, Optional: !required}
	setText := func(title, description *string) {
		if title != nil && strings.TrimSpace(*title) != "" {
			it.Header = strings.TrimSpace(*title)
		}
		if description != nil {
			it.Text = strings.TrimSpace(*description)
		}
	}
	switch {
	case p.String != nil:
		v := p.String
		setText(v.Title, v.Description)
		switch {
		case len(v.OneOf) > 0:
			fd.kind = fieldChoice
			for _, o := range v.OneOf {
				it.Options = append(it.Options, event.PermissionOption{OptionID: o.Const, Name: firstNonEmpty(o.Title, o.Const)})
			}
		case len(v.Enum) > 0:
			fd.kind = fieldChoice
			for _, e := range v.Enum {
				it.Options = append(it.Options, event.PermissionOption{OptionID: e, Name: e})
			}
		default:
			fd.kind = fieldText
			it.MinLength = intBound(v.MinLength)
			it.MaxLength = intBound(v.MaxLength)
			if v.Format != nil {
				if knownFormats[string(*v.Format)] {
					it.Format = string(*v.Format)
				} else {
					s.declineUpdate("elicitation.schema.format")
				}
			}
			if v.Pattern != nil && *v.Pattern != "" {
				// JSON Schema patterns are ECMA-262; Go's RE2 accepts most of
				// what forms use. One it cannot compile is left to the agent.
				if re, err := regexp.Compile(*v.Pattern); err == nil {
					fd.pattern, it.Pattern = re, *v.Pattern
				} else {
					s.declineUpdate("elicitation.schema.pattern")
				}
			}
		}
		if v.Default != nil {
			it.Default = []string{*v.Default}
		}
	case p.Number != nil:
		v := p.Number
		setText(v.Title, v.Description)
		fd.kind, it.Kind = fieldNumber, event.QuestionKindNumber
		it.Min, it.Max = v.Minimum, v.Maximum
		if v.Default != nil {
			it.Default = []string{strconv.FormatFloat(*v.Default, 'f', -1, 64)}
		}
	case p.Integer != nil:
		v := p.Integer
		setText(v.Title, v.Description)
		fd.kind, it.Kind = fieldInteger, event.QuestionKindInteger
		it.Min, it.Max = floatBound(v.Minimum), floatBound(v.Maximum)
		if v.Default != nil {
			it.Default = []string{strconv.FormatInt(*v.Default, 10)}
		}
	case p.Boolean != nil:
		v := p.Boolean
		setText(v.Title, v.Description)
		fd.kind = fieldBool
		it.Options = []event.PermissionOption{{OptionID: "true", Name: "Yes"}, {OptionID: "false", Name: "No"}}
		if v.Default != nil {
			it.Default = []string{strconv.FormatBool(*v.Default)}
		}
	case p.Array != nil:
		v := p.Array
		setText(v.Title, v.Description)
		switch {
		case v.Items.String != nil:
			for _, e := range v.Items.String.Enum {
				it.Options = append(it.Options, event.PermissionOption{OptionID: e, Name: e})
			}
		case v.Items.Titled != nil:
			for _, o := range v.Items.Titled.AnyOf {
				it.Options = append(it.Options, event.PermissionOption{OptionID: o.Const, Name: firstNonEmpty(o.Title, o.Const)})
			}
		default:
			s.declineUpdate("elicitation.schema.array")
			return fd, false
		}
		fd.kind, it.Multiple = fieldMulti, true
		it.MinItems, it.MaxItems = countBound(v.MinItems), countBound(v.MaxItems)
		it.Default = v.Default
	default:
		kind := "unknown"
		if p.Other != nil && p.Other.Type != "" {
			kind = p.Other.Type
		}
		s.declineUpdate("elicitation.schema." + kind)
		return fd, false
	}
	fd.item = it
	return fd, true
}

// checkForm checks every answer against its field and types it (MADR 0191
// D3). An empty optional field is left out; anything else that breaks a rule
// is refused with ErrQuestionInvalid, naming the field.
func checkForm(fields []formField, answers provider.QuestionAnswers) (map[string]acp.ElicitationContentValue, error) {
	content := make(map[string]acp.ElicitationContentValue, len(fields))
	for _, fd := range fields {
		var values []string
		for _, v := range answers[fd.name] {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			if fd.required {
				return nil, fieldError(fd, "is required")
			}
			continue
		}
		v, err := checkField(fd, values)
		if err != nil {
			return nil, err
		}
		content[fd.name] = v
	}
	return content, nil
}

func checkField(fd formField, values []string) (acp.ElicitationContentValue, error) {
	it := fd.item
	if fd.kind != fieldMulti && len(values) != 1 {
		return acp.ElicitationContentValue{}, fieldError(fd, "takes one answer")
	}
	v := values[0]
	switch fd.kind {
	case fieldText:
		n := utf8.RuneCountInString(v)
		if it.MinLength != nil && n < *it.MinLength {
			return acp.ElicitationContentValue{}, fieldError(fd, fmt.Sprintf("must be at least %d characters", *it.MinLength))
		}
		if it.MaxLength != nil && n > *it.MaxLength {
			return acp.ElicitationContentValue{}, fieldError(fd, fmt.Sprintf("must be at most %d characters", *it.MaxLength))
		}
		if !matchesFormat(it.Format, v) {
			return acp.ElicitationContentValue{}, fieldError(fd, "must be a valid "+it.Format)
		}
		if fd.pattern != nil && !fd.pattern.MatchString(v) {
			return acp.ElicitationContentValue{}, fieldError(fd, "does not match the required pattern")
		}
		return acp.NewElicitationContentValueString(v), nil
	case fieldChoice:
		if !hasOption(it, v) {
			return acp.ElicitationContentValue{}, fieldError(fd, "must be one of the options")
		}
		return acp.NewElicitationContentValueString(v), nil
	case fieldBool:
		b, err := strconv.ParseBool(v)
		if err != nil || !hasOption(it, v) {
			return acp.ElicitationContentValue{}, fieldError(fd, "must be yes or no")
		}
		return acp.NewElicitationContentValueBoolean(b), nil
	case fieldNumber:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return acp.ElicitationContentValue{}, fieldError(fd, "must be a number")
		}
		if err := inRange(fd, f); err != nil {
			return acp.ElicitationContentValue{}, err
		}
		return acp.NewElicitationContentValueNumber(f), nil
	case fieldInteger:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return acp.ElicitationContentValue{}, fieldError(fd, "must be a whole number")
		}
		if err := inRange(fd, float64(n)); err != nil {
			return acp.ElicitationContentValue{}, err
		}
		return acp.NewElicitationContentValueInteger(n), nil
	case fieldMulti:
		for _, x := range values {
			if !hasOption(it, x) {
				return acp.ElicitationContentValue{}, fieldError(fd, "must only use the options")
			}
		}
		if it.MinItems != nil && len(values) < *it.MinItems {
			return acp.ElicitationContentValue{}, fieldError(fd, fmt.Sprintf("needs at least %d choices", *it.MinItems))
		}
		if it.MaxItems != nil && len(values) > *it.MaxItems {
			return acp.ElicitationContentValue{}, fieldError(fd, fmt.Sprintf("allows at most %d choices", *it.MaxItems))
		}
		return acp.NewElicitationContentValueStringArray(slices.Clone(values)), nil
	}
	return acp.ElicitationContentValue{}, fieldError(fd, "cannot be answered")
}

func fieldError(fd formField, rule string) error {
	return fmt.Errorf("%w: %s (%s) %s", provider.ErrQuestionInvalid, fd.item.Header, fd.name, rule)
}

func inRange(fd formField, f float64) error {
	it := fd.item
	if it.Min != nil && f < *it.Min {
		return fieldError(fd, "must be at least "+strconv.FormatFloat(*it.Min, 'f', -1, 64))
	}
	if it.Max != nil && f > *it.Max {
		return fieldError(fd, "must be at most "+strconv.FormatFloat(*it.Max, 'f', -1, 64))
	}
	return nil
}

func hasOption(it event.QuestionItem, v string) bool {
	return slices.ContainsFunc(it.Options, func(o event.PermissionOption) bool { return o.OptionID == v })
}

// matchesFormat checks the formats Magic forwards; an empty format matches.
func matchesFormat(format, v string) bool {
	switch format {
	case "email":
		a, err := mail.ParseAddress(v)
		return err == nil && a.Address == v
	case "uri":
		u, err := url.Parse(v)
		return err == nil && u.Scheme != ""
	case "date":
		_, err := time.Parse(time.DateOnly, v)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, v)
		return err == nil
	}
	return true
}

func intBound(v *uint32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func countBound(v *uint64) *int {
	if v == nil {
		return nil
	}
	n := saturatingInt(*v)
	return &n
}

func floatBound(v *int64) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}
