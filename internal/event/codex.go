package event

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// MaxCodexTextBytes bounds each human-readable string in a Codex projection.
const MaxCodexTextBytes = 4096

// MaxCodexListItems bounds repeated values in a Codex projection.
const MaxCodexListItems = 32

// CodexPayload is the provider-independent wire body for bounded Codex
// activity cards. Key is the stable reducer upsert key.
type CodexPayload struct {
	Key       string   `json:"key"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status,omitempty"`
	Title     string   `json:"title,omitempty"`
	Text      string   `json:"text,omitempty"`
	FromModel string   `json:"from_model,omitempty"`
	ToModel   string   `json:"to_model,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Values    []string `json:"values,omitempty"`
	Resolved  bool     `json:"resolved,omitempty"`
}

// MarshalJSON enforces the wire bound even if a provider constructs the
// payload directly. This is defense in depth at the shared event boundary.
func (p CodexPayload) MarshalJSON() ([]byte, error) {
	type plain CodexPayload
	q := plain(p)
	q.Key = clipCodex(q.Key)
	q.Kind = clipCodex(q.Kind)
	q.Status = clipCodex(q.Status)
	q.Title = clipCodex(q.Title)
	q.Text = clipCodex(q.Text)
	q.FromModel = clipCodex(q.FromModel)
	q.ToModel = clipCodex(q.ToModel)
	q.Reason = clipCodex(q.Reason)
	if len(q.Values) > MaxCodexListItems {
		q.Values = q.Values[:MaxCodexListItems]
	}
	for i := range q.Values {
		q.Values[i] = clipCodex(q.Values[i])
	}
	return json.Marshal(q)
}

func clipCodex(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
	if len(s) <= MaxCodexTextBytes {
		return s
	}
	s = s[:MaxCodexTextBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
