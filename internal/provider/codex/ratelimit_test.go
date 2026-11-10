package codex

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func windowsFrame(primary, secondary string) []byte {
	return []byte(`{"rateLimits":{"primary":` + primary + `,"secondary":` + secondary + `}}`)
}

func window(used, mins int, resetsAt int64) string {
	return `{"usedPercent":` + strconv.Itoa(used) + `,"windowDurationMins":` + strconv.Itoa(mins) +
		`,"resetsAt":` + strconv.FormatInt(resetsAt, 10) + `}`
}

func rateSession() *session {
	return &session{events: make(chan event.Event, 8), done: make(chan struct{}), log: silentLogger()}
}

// TestRateLimitWindowsAreNamed is MADR 0243 D6: the window at 100% sets the
// limit's scope and length, a week is weekly, both at 100% take the later
// reset, and the 90% notice names its window.
func TestRateLimitWindowsAreNamed(t *testing.T) {
	soon, later := time.Now().Add(time.Hour).Unix(), time.Now().Add(72*time.Hour).Unix()
	for _, tc := range []struct {
		name               string
		primary, secondary string
		scope              string
		minutes            int
		reset              int64
		text               string
	}{
		{"the 5-hour window", window(100, 300, soon), window(40, 10080, later), "window", 300, soon, "Codex 5-hour limit reached"},
		{"the weekly window", window(60, 300, soon), window(100, 10080, later), "weekly", 0, later, "Codex weekly limit reached"},
		{"both, the later reset", window(100, 300, soon), window(100, 10080, later), "weekly", 0, later, "Codex weekly limit reached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := rateSession()
			defer close(s.done)
			s.handleNotification("account/rateLimits/updated", windowsFrame(tc.primary, tc.secondary))
			events := drain(s)
			if len(events) != 1 || events[0].Type != event.TypeError {
				t.Fatalf("events = %+v, want one error", events)
			}
			e := events[0]
			if e.LimitScope != tc.scope || e.LimitWindowMinutes != tc.minutes || e.LimitProvider != "codex" {
				t.Errorf("limit = %q/%d/%q, want %q/%d/codex", e.LimitScope, e.LimitWindowMinutes, e.LimitProvider, tc.scope, tc.minutes)
			}
			if e.RetryAt.Unix() != tc.reset {
				t.Errorf("RetryAt = %v, want %v", e.RetryAt, time.Unix(tc.reset, 0))
			}
			if !strings.HasPrefix(e.Error, tc.text) {
				t.Errorf("Error = %q, want it to start %q", e.Error, tc.text)
			}
		})
	}

	s := rateSession()
	defer close(s.done)
	s.handleNotification("account/rateLimits/updated", windowsFrame(window(10, 300, soon), window(93, 10080, later)))
	events := drain(s)
	if len(events) != 1 || events[0].Type != event.TypeNotice || !strings.Contains(events[0].Text, "weekly limit (93% used)") {
		t.Fatalf("events = %+v, want one notice naming the weekly window", events)
	}
}
