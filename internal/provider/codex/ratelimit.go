package codex

import (
	"fmt"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// codexRateWindow is one window of account/rateLimits/updated: codex's
// primary (a short rolling window, 5 hours) or secondary (a long one, a
// week).
type codexRateWindow struct {
	UsedPercent        int   `json:"usedPercent"`
	WindowDurationMins int   `json:"windowDurationMins"`
	ResetsAt           int64 `json:"resetsAt"`
}

// The window lengths that have a name of their own, in minutes.
const (
	minutesPerDay  = 24 * 60
	minutesPerWeek = 7 * minutesPerDay
)

// hitWindow is the window at 100%, or nil: when both are, the one whose
// reset is later, since that is when work can go on.
func hitWindow(primary, secondary *codexRateWindow) *codexRateWindow {
	var hit *codexRateWindow
	for _, w := range []*codexRateWindow{primary, secondary} {
		if w == nil || w.UsedPercent < 100 {
			continue
		}
		if hit == nil || w.ResetsAt > hit.ResetsAt {
			hit = w
		}
	}
	return hit
}

func (w *codexRateWindow) resetAt() time.Time {
	if w.ResetsAt <= 0 {
		return time.Time{}
	}
	return time.Unix(w.ResetsAt, 0).UTC()
}

// scope is the window's limit scope and stated length (MADR 0243 D6): a week
// is weekly, a day daily, any other stated length a rolling window.
func (w *codexRateWindow) scope() (string, int) {
	switch w.WindowDurationMins {
	case 0:
		return agenterr.ScopeRate, 0
	case minutesPerWeek:
		return agenterr.ScopeWeekly, 0
	case minutesPerDay:
		return agenterr.ScopeDaily, 0
	}
	return agenterr.ScopeWindow, w.WindowDurationMins
}

// name is the window as a person says it: "5-hour", "weekly", "daily",
// "90-minute", or "rate" when codex gave no length.
func (w *codexRateWindow) name() string {
	switch m := w.WindowDurationMins; {
	case m == 0:
		return "rate"
	case m == minutesPerWeek:
		return "weekly"
	case m == minutesPerDay:
		return "daily"
	case m%60 == 0:
		return fmt.Sprintf("%d-hour", m/60)
	default:
		return fmt.Sprintf("%d-minute", m)
	}
}

// limitError is the error event for a window at 100%.
func (w *codexRateWindow) limitError(sessionID string, now time.Time) event.Event {
	scope, minutes := w.scope()
	return event.Event{
		Type:               event.TypeError,
		SessionID:          sessionID,
		Timestamp:          now,
		ErrorKind:          string(agenterr.KindRateLimit),
		RetryAt:            w.resetAt(),
		LimitScope:         scope,
		LimitWindowMinutes: minutes,
		LimitProvider:      "codex",
		Error: fmt.Sprintf("Codex %s limit reached (%d%% used). Wait for it to reset, or try again later.",
			w.name(), w.UsedPercent),
	}
}
