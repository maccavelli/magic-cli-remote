package ws

import "testing"

// The pre-auth slot-exhaustion defense evicts the oldest UNauthenticated client
// when the pool is full, so idle pre-auth sockets can't shut out new
// connections — but it must never evict an authenticated client, and must
// report "nothing evictable" when every slot is held by an authed client.
func TestOldestUnauthedLocked(t *testing.T) {
	s := &Server{clients: map[*client]struct{}{}}
	authed := &client{authed: true, seq: 1}
	oldest := &client{authed: false, seq: 2}
	newer := &client{authed: false, seq: 5}
	for _, c := range []*client{authed, oldest, newer} {
		s.clients[c] = struct{}{}
	}
	if got := s.oldestUnauthedLocked(); got != oldest {
		t.Fatalf("evicted wrong client: got seq=%d, want the oldest unauthed (seq=2)", got.seq)
	}

	allAuthed := &Server{clients: map[*client]struct{}{
		{authed: true, seq: 1}: {},
		{authed: true, seq: 2}: {},
	}}
	if got := allAuthed.oldestUnauthedLocked(); got != nil {
		t.Fatalf("all-authed pool must have nothing evictable, got seq=%d", got.seq)
	}
}
