package acphttp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Runtime status and usage for the ACP-over-websocket transport, which today
// means goose (MADR 0138 Phase 11).
//
// goose publishes no HTTP status surface at all: /status, /health, /metrics,
// /doc and /openapi.json every one returns 404 on the running engine. So unlike
// kilo, opencode and grok there is no endpoint to read, and everything reported
// here comes from what the agent has already sent over ACP.
//
// That constrains what can honestly be said, and the constraint is the design:
// context occupancy is measured and reported; cost is not reported because
// goose never sends one, and a zero would be a claim rather than a gap.

var _ provider.RuntimeSession = (*session)(nil)

// lastUsage is the most recent ACP SessionUsageUpdate, kept so /usage can
// answer without asking the agent for something it does not serve.
//
// Two atomics rather than a mutex: they are written from the notification
// handler on every turn and read only by a slash command, and this must not
// add a lock to that path (MADR 0138 F5 — that goroutine must never block).
// The pair can be read mid-update, which at worst reports a used count from one
// turn against a window size from the next; the window size does not change
// within a session, so in practice they cannot disagree.
type lastUsage struct {
	used atomic.Int64
	size atomic.Int64
	// seen distinguishes "no usage reported yet" from "zero tokens used".
	seen atomic.Bool
}

func (l *lastUsage) record(used, size int) {
	l.used.Store(int64(used))
	l.size.Store(int64(size))
	l.seen.Store(true)
}

// RuntimeUsage implements [provider.RuntimeSession].
//
// Context occupancy, not spend. goose reports `used` and `size` on the standard
// ACP usage update and nothing else — no cost, no per-model split, no turn
// count — so this reports exactly that and says what it does not know.
func (s *session) RuntimeUsage(context.Context) (string, error) {
	if !s.usage.seen.Load() {
		return "Usage: goose has not reported any yet — it sends context usage " +
			"after a turn, and this session has not completed one.", nil
	}
	used, size := s.usage.used.Load(), s.usage.size.Load()

	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %d tokens in context", used)
	if size > 0 {
		fmt.Fprintf(&b, " of %d (%.0f%%)", size, float64(used)/float64(size)*100)
	}
	// Stated rather than omitted: a usage line with no money on it otherwise
	// reads as "this session was free".
	b.WriteString(" · goose reports no cost")
	return b.String(), nil
}

// RuntimeStatus implements [provider.RuntimeSession].
func (s *session) RuntimeStatus(context.Context) (string, error) {
	parts := []string{"Goose"}
	if m := s.currentModelLabel(); m != "" {
		parts = append(parts, "model "+m)
	}
	if id := s.AgentSessionID(); id != "" {
		parts = append(parts, "session "+id)
	}
	parts = append(parts, "this engine publishes no plan usage")
	return strings.Join(parts, " · "), nil
}

// currentModelLabel reads the model out of the config options goose reports,
// which for this agent *are* the model catalog (MADR 0043 D6). Empty when the
// agent has not sent them, which is the honest answer before the first update.
func (s *session) currentModelLabel() string {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	for _, opt := range s.configOpts {
		if opt.ID == "model" && strings.TrimSpace(opt.CurrentValue) != "" {
			return opt.CurrentValue
		}
	}
	return ""
}
