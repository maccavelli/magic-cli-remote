package kilo

import "log/slog"

// kiloSyncEvent is the event-sourced twin kilo publishes alongside each plain
// SSE frame: a monotonic `seq` per `aggregateID` (the session id).
//
// 18 of the 56 frames in a one-turn capture are these (MADR 0137 Phase 1).
type kiloSyncEvent struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Seq         int64  `json:"seq"`
	AggregateID string `json:"aggregateID"`
}

// firstSync returns the first non-nil sync event; kilo puts it at the top level
// on the per-directory stream and under `payload` on /global/event.
func firstSync(vals ...*kiloSyncEvent) *kiloSyncEvent {
	for _, v := range vals {
		if v != nil && v.AggregateID != "" {
			return v
		}
	}
	return nil
}

// noteSyncSeq records the latest sequence seen for an aggregate and reports a
// gap when one appears.
//
// This is the half of MADR 0137 F7 that is worth having. The other half —
// resuming from the recorded seq via `POST /sync/history` — was measured and
// declined: that endpoint returns the FULL history of every aggregate the
// caller does not list, and mcremote can only list its own sessions. On the
// development host that is a 28 MB response covering 153 aggregates and 13410
// events, on every reconnect, replacing a targeted per-session REST resync.
// There is no allow-list form of the request.
//
// So the gap is detected and reported here, and filled by the existing resync.
// Before this, a reconnect that dropped events and one that dropped none were
// indistinguishable — which is the real weakness F7 identified, and it is
// closed without taking on an experimental API whose default is unbounded.
func (d *httpDialect) noteSyncSeq(aggregateID string, seq int64) {
	d.mu.Lock()
	if d.syncSeq == nil {
		d.syncSeq = make(map[string]int64)
	}
	last, seen := d.syncSeq[aggregateID]
	d.syncSeq[aggregateID] = seq
	d.mu.Unlock()

	// A first sighting establishes a baseline and proves nothing. Only a
	// forward jump of more than one is a gap: kilo re-sends a seq on
	// reconnect, and going backwards means a new aggregate reusing an id, not
	// a loss.
	if !seen || seq <= last+1 {
		return
	}
	d.log.Warn("kilo sync stream gap",
		slog.String("session", aggregateID),
		slog.Int64("last_seq", last),
		slog.Int64("seq", seq),
		slog.Int64("missed", seq-last-1),
		slog.String("hint", "events were dropped between these sequences; the "+
			"per-session resync fills them"),
	)
}
