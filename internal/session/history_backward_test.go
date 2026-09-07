package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func ringOf(n int) []event.Event {
	out := make([]event.Event, 0, n)
	for i := range n {
		out = append(out, event.Event{
			Type:      event.TypeAssistantChunk,
			SessionID: "s1",
			Seq:       uint64(i + 1),
			Text:      strings.Repeat("b", 512),
		})
	}
	return out
}

func managerWith(hist []event.Event) *Manager {
	return &Manager{sessions: map[string]*entry{"s1": {history: hist}}}
}

func TestNewestPageReturnsTheTail(t *testing.T) {
	m := managerWith(ringOf(5000))

	page, truncated, prev := m.HistoryPageBefore("s1", 0, 200)

	if len(page) != 200 {
		t.Fatalf("want a 200-event page, got %d", len(page))
	}
	// A chat screen opens at the bottom. This is the whole point of the phase:
	// the newest page in one round trip, not a walk from seq 1 (MADR 0138 F17).
	if page[len(page)-1].Seq != 5000 {
		t.Fatalf("newest event in the page is seq %d, want 5000 — this page is not the tail",
			page[len(page)-1].Seq)
	}
	if page[0].Seq != 4801 {
		t.Fatalf("oldest event in the page is seq %d, want 4801", page[0].Seq)
	}
	// Oldest-first within the page, so the client's reducer is unchanged.
	for i := 1; i < len(page); i++ {
		if page[i].Seq <= page[i-1].Seq {
			t.Fatalf("page is not oldest-first at %d: %d then %d", i, page[i-1].Seq, page[i].Seq)
		}
	}
	if !truncated {
		t.Fatal("4,800 older events remain; truncated must be true")
	}
	if prev != 4801 {
		t.Fatalf("prev_before_seq = %d, want 4801 (exclusive upper bound for the next older page)", prev)
	}
}

func TestBackwardPagingTerminatesAtFirstSeq(t *testing.T) {
	const total = 1000
	m := managerWith(ringOf(total))

	seen := make(map[uint64]int)
	before := uint64(0)
	pages := 0
	for {
		page, truncated, prev := m.HistoryPageBefore("s1", before, 200)
		pages++
		if len(page) == 0 {
			t.Fatalf("page %d is empty before reaching the oldest event", pages)
		}
		for _, ev := range page {
			seen[ev.Seq]++
		}
		if !truncated {
			break
		}
		if prev == 0 || prev >= before && before != 0 {
			t.Fatalf("prev_before_seq did not advance: %d then %d", before, prev)
		}
		before = prev
		if pages > 20 {
			t.Fatal("backward paging did not terminate")
		}
	}

	if pages != total/200 {
		t.Fatalf("walked the ring in %d pages, want %d", pages, total/200)
	}
	if len(seen) != total {
		t.Fatalf("saw %d distinct events, want %d", len(seen), total)
	}
	for seq, n := range seen {
		if n != 1 {
			t.Fatalf("seq %d returned %d times; backward paging must not overlap", seq, n)
		}
	}
}

func TestNewestPageEncodesEachEventOnce(t *testing.T) {
	// Same budget discipline as the forward page: one encode per candidate.
	hist := make([]event.Event, 0, 800)
	for i := range 800 {
		hist = append(hist, bigEvent(i))
	}
	m := managerWith(hist)

	calls := 0
	orig := historyMarshal
	historyMarshal = func(ev *event.Event) ([]byte, error) { calls++; return json.Marshal(ev) }
	t.Cleanup(func() { historyMarshal = orig })

	page, truncated, prev := m.HistoryPageBefore("s1", 0, historyMaxPage)
	if calls > len(hist) {
		t.Fatalf("encoded %d times over an %d-event window; want at most one pass", calls, len(hist))
	}
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(b) > historyMaxResponseBytes {
		t.Fatalf("page is %d bytes, over the %d budget", len(b), historyMaxResponseBytes)
	}
	// The budget must trim the *old* end of a backward page: the newest events
	// are the ones the screen is opening on.
	if page[len(page)-1].Seq != 800 {
		t.Fatalf("the byte budget dropped the newest event: page ends at seq %d, want 800", page[len(page)-1].Seq)
	}
	if !truncated || prev == 0 {
		t.Fatalf("a shortened page must report truncated with a cursor: truncated=%v prev=%d", truncated, prev)
	}
}

func TestBackwardPageAlwaysMakesProgress(t *testing.T) {
	huge := event.Event{
		Type: event.TypeAssistantChunk, SessionID: "s1", Seq: 2,
		Text: strings.Repeat("z", historyMaxResponseBytes+4096),
	}
	m := managerWith([]event.Event{bigEvent(0), huge})

	page, _, _ := m.HistoryPageBefore("s1", 0, historyMaxPage)
	if len(page) != 1 || page[0].Seq != 2 {
		t.Fatalf("want the single oversized newest event, got %d events", len(page))
	}
}

func TestBackwardPagingOnAnEmptyOrUnknownSession(t *testing.T) {
	m := managerWith(nil)
	page, truncated, prev := m.HistoryPageBefore("nope", 0, 200)
	if len(page) != 0 || truncated || prev != 0 {
		t.Fatalf("unknown session must yield an empty page: %d events truncated=%v prev=%d", len(page), truncated, prev)
	}
}

func TestBeforeSeqBelowFirstSeqYieldsNothing(t *testing.T) {
	// Seqs 500..1000 retained; asking for anything older than 500 is servable
	// only as "there is nothing there", not as an error.
	hist := ringOf(1000)[499:]
	m := managerWith(hist)

	page, truncated, prev := m.HistoryPageBefore("s1", 400, 200)
	if len(page) != 0 {
		t.Fatalf("want an empty page below first_seq, got %d events starting at %d", len(page), page[0].Seq)
	}
	if truncated || prev != 0 {
		t.Fatalf("an empty page must not claim more remains: truncated=%v prev=%d", truncated, prev)
	}
}
