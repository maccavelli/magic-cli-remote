package acpagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestAutoApprovalPicksByKindOnly is MADR 0207 amendment H, D46 and question
// 14: auto-approval reads an option's kind, never its name, prefers allow_once
// to allow_always, and answers cancelled when no option allows. The sweep's
// picker agrees with it on every set.
func TestAutoApprovalPicksByKindOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []acp.PermissionOption
		want string // the option selected; "" for cancelled
	}{
		{"a reject named \"Don't allow\" listed first", []acp.PermissionOption{
			{OptionId: "no", Name: "Don't allow", Kind: acp.PermissionOptionKindRejectOnce},
			{OptionId: "yes", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce}}, "yes"},
		{"allow_once before allow_always, whatever the order", []acp.PermissionOption{
			{OptionId: "always", Name: "Allow always", Kind: acp.PermissionOptionKindAllowAlways},
			{OptionId: "once", Name: "Allow once", Kind: acp.PermissionOptionKindAllowOnce}}, "once"},
		{"allow_always when it is the only allow", []acp.PermissionOption{
			{OptionId: "no", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
			{OptionId: "always", Name: "Always", Kind: acp.PermissionOptionKindAllowAlways}}, "always"},
		{"only a reject named \"Don't allow\"", []acp.PermissionOption{
			{OptionId: "no", Name: "Don't allow", Kind: acp.PermissionOptionKindRejectOnce}}, ""},
		{"only a reject named \"Approve\"", []acp.PermissionOption{
			{OptionId: "no", Name: "Approve", Kind: acp.PermissionOptionKindRejectAlways}}, ""},
		{"no options", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := autoAllow(acp.RequestPermissionRequest{Options: tc.opts})
			got := ""
			if id, ok := resp.Chosen(); ok {
				got = string(id)
			} else if resp.Outcome.Cancelled == nil {
				t.Fatalf("outcome is neither selected nor cancelled: %+v", resp.Outcome)
			}
			if got != tc.want {
				t.Errorf("auto-approval chose %q, want %q", got, tc.want)
			}
			evOpts := make([]event.PermissionOption, 0, len(tc.opts))
			for _, o := range tc.opts {
				evOpts = append(evOpts, event.PermissionOption{OptionID: string(o.OptionId), Name: o.Name, Kind: string(o.Kind)})
			}
			if sweep := pickAllowOptionID(evOpts); sweep != tc.want {
				t.Errorf("the sweep chose %q, want %q, as auto-approval does", sweep, tc.want)
			}
		})
	}
}

// TestBlockKindNamesAnUnknownVariant: a compaction summary block of a kind no
// SDK knows is declined by its own discriminator now, not as "unknown" (H-6).
func TestBlockKindNamesAnUnknownVariant(t *testing.T) {
	var b acp.ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"video","uri":"file:///x"}`), &b); err != nil {
		t.Fatal(err)
	}
	if got := blockKind(b); got != "video" {
		t.Errorf("blockKind = %q, want \"video\"", got)
	}
	if got := blockKind(acp.ContentBlock{}); got != "unknown" {
		t.Errorf("blockKind of an empty block = %q, want \"unknown\"", got)
	}
}

// TestPromptCancelledIsBenign: the SDK's own cancellation is not a turn error
// (acp.IsCancelled, H-6), as context.Canceled already was.
func TestPromptCancelledIsBenign(t *testing.T) {
	if !isBenignPromptErr(acp.ErrPromptCancelled) {
		t.Fatal("acp.ErrPromptCancelled must be benign")
	}
	if !isBenignPromptErr(&acp.RequestError{Code: int(acp.ErrorCodeRequestCancelled), Message: "cancelled"}) {
		t.Fatal("a -32800 answer must stay benign")
	}
	if !isBenignPromptErr(context.DeadlineExceeded) {
		t.Fatal("a deadline must stay benign, which acp.IsCancelled does not cover")
	}
}

// TestListAgentSessionsStopsAtAnyRepeatedCursor: an agent that sends a cursor
// it sent before, not only the last one, is looping. The listing stops there,
// with what it had, and lists nothing twice (acp.AllSessions, H-6).
func TestListAgentSessionsStopsAtAnyRepeatedCursor(t *testing.T) {
	s, a := newWireSession(t, `{"sessionCapabilities":{"list":{}}}`)
	pages := map[string]string{
		"":   `{"sessions":[{"sessionId":"s1","cwd":"/w"}],"nextCursor":"c1"}`,
		"c1": `{"sessions":[{"sessionId":"s2","cwd":"/w"}],"nextCursor":"c2"}`,
		"c2": `{"sessions":[{"sessionId":"s3","cwd":"/w"}],"nextCursor":"c1"}`,
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case m := <-a.lines:
				if m.Method != "session/list" {
					continue
				}
				var p struct {
					Cursor string `json:"cursor"`
				}
				_ = json.Unmarshal(m.Params, &p)
				a.sendRaw(`{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":` + pages[p.Cursor] + `}`)
			case <-stop:
				return
			}
		}
	}()
	got, err := s.ListAgentSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "s1,s2,s3" {
		t.Fatalf("sessions = %v, want s1, s2 and s3, each once", ids)
	}
}

// TestAgentSessionMetaReadsTheTimeOrNone: a session's updatedAt is read by the
// SDK and kept in UTC; one that is absent or not RFC 3339 is the zero time,
// never a guess (MADR 0046 L-13). Surrounding spaces make it not RFC 3339,
// where Magic's own reader trimmed them (H-6).
func TestAgentSessionMetaReadsTheTimeOrNone(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, tc := range []struct {
		name string
		at   *string
		zero bool
	}{
		{"absent", nil, true},
		{"not a time", str("not a time"), true},
		{"spaces around it", str(" 2026-09-04T04:44:41Z "), true},
		{"RFC 3339, with an offset", str("2026-09-04T06:44:41+02:00"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := agentSessionMeta(acp.SessionInfo{SessionId: "s", Cwd: "/w", UpdatedAt: tc.at}).UpdatedAt
			if got.IsZero() != tc.zero {
				t.Fatalf("updated = %v, zero %v; want zero %v", got, got.IsZero(), tc.zero)
			}
			if !tc.zero && (got.Location() != time.UTC || got.Hour() != 4) {
				t.Fatalf("updated = %v, want 04:44:41 UTC", got)
			}
		})
	}
}
