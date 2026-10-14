package session

import (
	"hash/fnv"
	"strconv"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// maxCompactionStates caps how many compaction IDs one session remembers for
// deduplication. The agent chooses the IDs, so the set must be bounded.
const maxCompactionStates = 64

// compactionChangedLocked reports whether ev differs from the last state kept
// for its compaction ID, and records it when it does (MADR 0190 D2).
//
// A hash is kept rather than the state: a summary can be 32 KiB. Past the cap
// the memory starts over, which can only let a repeat through, never drop a
// change.
//
// Caller holds m.mu.
func (e *entry) compactionChangedLocked(ev *event.Event) bool {
	h := fnv.New64a()
	for _, s := range []string{ev.CompactionStatus, ev.CompactionSummary, ev.CompactionError,
		strconv.FormatBool(ev.CompactionTruncated)} {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum64()
	if last, ok := e.compactionSeen[ev.CompactionID]; ok && last == sum {
		return false
	}
	if e.compactionSeen == nil || len(e.compactionSeen) >= maxCompactionStates {
		e.compactionSeen = make(map[string]uint64)
	}
	e.compactionSeen[ev.CompactionID] = sum
	return true
}
