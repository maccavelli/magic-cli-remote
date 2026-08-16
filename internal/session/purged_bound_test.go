package session

import (
	"fmt"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// The purge tombstone guards a persistDebounce-sized window (2s), not the
// process lifetime; ids are UUIDs so clearPurged-on-Create never fires for
// them, and without a bound the set grew for the life of the daemon
// (MADR 0095 F7).
func TestPurgedSetIsBounded(t *testing.T) {
	m := NewManager(provider.NewRegistry(), nil, nil, func(event.Event) {})
	for i := 0; i < maxPurgedIDs+64; i++ {
		m.markPurged(fmt.Sprintf("sess-%d", i))
	}
	m.persistMu.Lock()
	n := len(m.purged)
	order := len(m.purgedOrder)
	m.persistMu.Unlock()
	if n > maxPurgedIDs {
		t.Errorf("purged = %d, want <= %d", n, maxPurgedIDs)
	}
	if order != n {
		t.Errorf("purgedOrder = %d entries, purged map = %d; they must not drift", order, n)
	}
	// The newest tombstone is the one whose window is still open.
	if !m.isPurged(fmt.Sprintf("sess-%d", maxPurgedIDs+63)) {
		t.Error("the newest tombstone must survive the trim")
	}
	// clearPurged must keep both structures in step.
	newest := fmt.Sprintf("sess-%d", maxPurgedIDs+63)
	m.clearPurged(newest)
	m.persistMu.Lock()
	n2, order2 := len(m.purged), len(m.purgedOrder)
	m.persistMu.Unlock()
	if m.isPurged(newest) {
		t.Error("clearPurged did not remove the id")
	}
	if n2 != order2 {
		t.Errorf("after clearPurged: map=%d order=%d; they must not drift", n2, order2)
	}
}

// Re-marking an id must not grow the order slice (MADR 0095 F7).
func TestMarkPurgedIsIdempotent(t *testing.T) {
	m := NewManager(provider.NewRegistry(), nil, nil, func(event.Event) {})
	for i := 0; i < 10; i++ {
		m.markPurged("same-id")
	}
	m.persistMu.Lock()
	n, order := len(m.purged), len(m.purgedOrder)
	m.persistMu.Unlock()
	if n != 1 || order != 1 {
		t.Fatalf("map=%d order=%d, want 1 and 1", n, order)
	}
}
