package acpagent

import (
	"encoding/json"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// rawToolOutputs are tool results whose meaning is in their layout: columns,
// indentation and colour. grok's committed captures hold no tool frames, so
// these are written by hand (PLAN 0208 P1 step 1).
var rawToolOutputs = map[string]string{
	"directory listing": "total 16\n" +
		"drwxr-xr-x  4 user  staff   128 Oct 21 10:00 .\n" +
		"drwxr-xr-x  9 user  staff   288 Oct 21 09:58 ..\n" +
		"-rw-r--r--  1 user  staff  1024 Oct 21 10:00 main.go",
	"aligned table": "NAME      STATUS    AGE\n" +
		"api       Running   3d\n" +
		"worker    Pending   12m",
	"stack trace": "panic: runtime error: index out of range [3] with length 3\n\n" +
		"goroutine 1 [running]:\n" +
		"main.main()\n" +
		"\t/src/main.go:12 +0x1d",
	"indented first line": "    indented by four\n  then two",
	"ansi colour":         "\x1b[31mFAIL\x1b[0m\tpkg/a\t0.012s\n\x1b[32mok\x1b[0m  \tpkg/b\t0.003s",
}

// toolTextFrame is a tool_call or tool_call_update carrying text as one content block.
func toolTextFrame(t *testing.T, kind, text string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"sessionUpdate": kind,
		"toolCallId":    "t1",
		"title":         "Run",
		"status":        "completed",
		"content": []any{map[string]any{
			"type":    "content",
			"content": map[string]any{"type": "text", "text": text},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestToolTextKeepsItsLayout is PLAN 0208 P1 step 1 (MADR 0208 D11): a tool
// event carries the agent's text as it came, line breaks, indentation and
// escape bytes included. Collapsing it made a listing one line of words.
func TestToolTextKeepsItsLayout(t *testing.T) {
	for name, want := range rawToolOutputs {
		for _, kind := range []string{"tool_call", "tool_call_update"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				s, _ := newUpdateSession()
				deliver(t, s, toolTextFrame(t, kind, want))
				var got []event.Event
				for len(s.events) > 0 {
					got = append(got, <-s.events)
				}
				if len(got) == 0 {
					t.Fatal("no event")
				}
				if got[0].Text != want {
					t.Errorf("Text = %q\nwant  %q", got[0].Text, want)
				}
			})
		}
	}
}
