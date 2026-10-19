package ws

import (
	"context"
	"log/slog"
	"sync"
)

// unownedLogMax bounds how many session ids the drop log remembers.
const unownedLogMax = 256

// unownedLog records events BroadcastEvent drops because their session has no
// known owner — once per session id, so a stream of them is one line (MADR
// 0201 D3, 0073 F-T1). The remembered set is bounded; the oldest id goes
// first.
type unownedLog struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	order []string
}

// note logs the first drop for id: at debug when the session was purged on
// purpose, at info otherwise.
func (u *unownedLog) note(log *slog.Logger, id, eventType string, purged bool) {
	u.mu.Lock()
	if u.seen == nil {
		u.seen = make(map[string]struct{})
	}
	if _, dup := u.seen[id]; dup {
		u.mu.Unlock()
		return
	}
	u.seen[id] = struct{}{}
	u.order = append(u.order, id)
	for len(u.order) > unownedLogMax {
		delete(u.seen, u.order[0])
		u.order = u.order[1:]
	}
	u.mu.Unlock()

	level := slog.LevelInfo
	if purged {
		level = slog.LevelDebug
	}
	log.Log(context.Background(), level, "event dropped: its session has no known owner",
		slog.String("session_id", id),
		slog.String("type", eventType),
		slog.Bool("purged", purged),
	)
}
