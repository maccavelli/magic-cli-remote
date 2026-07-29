package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestErrorSentinels(t *testing.T) {
	if ErrNotImplemented == nil {
		t.Fatal("ErrNotImplemented must not be nil")
	}
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Fatal("ErrNotImplemented must wrap itself")
	}
	if ErrTurnBusy == nil {
		t.Fatal("ErrTurnBusy must not be nil")
	}
	if !errors.Is(ErrTurnBusy, ErrTurnBusy) {
		t.Fatal("ErrTurnBusy must wrap itself")
	}
}

func TestErrNotImplementedMessage(t *testing.T) {
	if ErrNotImplemented.Error() != "provider not implemented" {
		t.Fatalf("message = %q, want %q", ErrNotImplemented.Error(), "provider not implemented")
	}
}

func TestErrTurnBusyMessage(t *testing.T) {
	if ErrTurnBusy.Error() != "turn busy" {
		t.Fatalf("message = %q, want %q", ErrTurnBusy.Error(), "turn busy")
	}
}

func TestIDConstants(t *testing.T) {
	cases := []struct {
		id   ID
		want string
	}{
		{IDFake, "fake"},
		{IDGrok, "grok"},
		{IDOpencode, "opencode"},
		{IDGoose, "goose"},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			if string(tc.id) != tc.want {
				t.Fatalf("ID = %q, want %q", tc.id, tc.want)
			}
		})
	}
}

func TestStartOptionsZeroValues(t *testing.T) {
	var opts StartOptions
	if opts.CWD != "" {
		t.Fatalf("CWD = %q, want empty", opts.CWD)
	}
	if opts.Model != "" {
		t.Fatalf("Model = %q, want empty", opts.Model)
	}
	if opts.Name != "" {
		t.Fatalf("Name = %q, want empty", opts.Name)
	}
	if opts.Agent != "" {
		t.Fatalf("Agent = %q, want empty", opts.Agent)
	}
	if opts.AgentSessionID != "" {
		t.Fatalf("AgentSessionID = %q, want empty", opts.AgentSessionID)
	}
	if opts.LocalSessionID != "" {
		t.Fatalf("LocalSessionID = %q, want empty", opts.LocalSessionID)
	}
}

func TestContentZeroValues(t *testing.T) {
	var c Content
	if c.Type != "" {
		t.Fatalf("Type = %q, want empty", c.Type)
	}
	if c.Text != "" {
		t.Fatalf("Text = %q, want empty", c.Text)
	}
	if c.MimeType != "" {
		t.Fatalf("MimeType = %q, want empty", c.MimeType)
	}
	if c.Data != "" {
		t.Fatalf("Data = %q, want empty", c.Data)
	}
}

type mockSession struct{}

func (m mockSession) ID() string                              { return "ses_1" }
func (m mockSession) ProviderID() ID                          { return IDFake }
func (m mockSession) AgentSessionID() string                  { return "" }
func (m mockSession) Prompt(context.Context, []Content) error { return nil }
func (m mockSession) Cancel(context.Context) error            { return nil }
func (m mockSession) Events() <-chan event.Event              { return nil }
func (m mockSession) Close(context.Context) error             { return nil }

func TestSessionInterfaceCompiles(t *testing.T) {
	var s Session = mockSession{}
	_ = s
}

func TestProviderInterfaceCompiles(t *testing.T) {
	p := &mockProvider{id: IDFake, ready: true}
	var pr Provider = p
	if pr.ID() != IDFake {
		t.Fatalf("ID = %q, want %q", pr.ID(), IDFake)
	}
	if !pr.Ready() {
		t.Fatal("Ready should be true")
	}
}

func TestAgentSessionMetaOmitsUnknownUpdatedAt(t *testing.T) {
	// omitempty never applies to a struct, so the zero time used to reach the
	// client as "0001-01-01T00:00:00Z" and the session picker dated the row to
	// the year 1 (MADR 0046 L-13).
	b, err := json.Marshal(AgentSessionMeta{ID: "s1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("updated_at")) {
		t.Fatalf("an unknown timestamp must be omitted, got %s", b)
	}

	when := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	b, err = json.Marshal(AgentSessionMeta{ID: "s1", UpdatedAt: when})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte("2026-07-29T10:00:00Z")) {
		t.Fatalf("a known timestamp must still be sent, got %s", b)
	}
}
