package kilo

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func syncDialect() (*httpDialect, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &httpDialect{
		log: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, buf
}

func syncFrame(agg string, seq int64) []byte {
	return []byte(`{"directory":"/x","project":"global","payload":{"type":"sync",` +
		`"syncEvent":{"id":"evt_1","type":"message.updated.1","seq":` +
		json.Number(itoa(seq)).String() + `,"aggregateID":"` + agg + `"}}}`)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// TestSyncGapIsReported is MADR 0137 F7.
//
// Before this, a reconnect that dropped events and one that dropped none were
// indistinguishable: kilo publishes a per-aggregate sequence on its `sync`
// stream — 18 of 56 frames in a one-turn capture — and mcremote decoded them
// and threw them away.
func TestSyncGapIsReported(t *testing.T) {
	d, buf := syncDialect()
	for _, seq := range []int64{0, 1, 2} {
		d.DecodeFrame(syncFrame("ses_a", seq))
	}
	if strings.Contains(buf.String(), "sync stream gap") {
		t.Fatalf("a contiguous sequence reported a gap:\n%s", buf.String())
	}

	// 3 and 4 never arrived.
	d.DecodeFrame(syncFrame("ses_a", 5))
	out := buf.String()
	if !strings.Contains(out, "sync stream gap") {
		t.Fatalf("a jump from seq 2 to 5 was not reported:\n%s", out)
	}
	if !strings.Contains(out, "missed=2") {
		t.Fatalf("gap size wrong; want missed=2:\n%s", out)
	}
}

// TestFirstSyncSightingIsNotAGap covers the reconnect case that would
// otherwise cry wolf on every engine restart.
//
// The first sequence seen for an aggregate establishes a baseline. A session
// that has been running before the daemon attached starts at whatever seq it
// has reached, and calling that a gap would report a loss on every start.
func TestFirstSyncSightingIsNotAGap(t *testing.T) {
	d, buf := syncDialect()
	d.DecodeFrame(syncFrame("ses_b", 4096))
	if strings.Contains(buf.String(), "sync stream gap") {
		t.Fatalf("a first sighting was reported as a gap:\n%s", buf.String())
	}
}

// TestSyncSequenceGoingBackwardsIsNotAGap: kilo re-sends a sequence on
// reconnect, and an id reused by a new aggregate starts over. Neither is a
// loss, and reporting them would make the signal useless.
func TestSyncSequenceGoingBackwardsIsNotAGap(t *testing.T) {
	d, buf := syncDialect()
	d.DecodeFrame(syncFrame("ses_c", 10))
	d.DecodeFrame(syncFrame("ses_c", 10)) // duplicate
	d.DecodeFrame(syncFrame("ses_c", 3))  // restart
	if strings.Contains(buf.String(), "sync stream gap") {
		t.Fatalf("a repeat or restart was reported as a gap:\n%s", buf.String())
	}
}

// TestSyncFramesAreNotRoutedToASession is the property that makes consuming
// `sync` safe at all.
//
// Each sync event is the event-sourced twin of a plain frame already on the
// stream. Routing it as well would deliver every message twice — which is why
// F7 says "do not double-deliver", and why this reads the sequence without
// giving the frame a session id.
func TestSyncFramesAreNotRoutedToASession(t *testing.T) {
	d, _ := syncDialect()
	typ, _, sid, ok := d.DecodeFrame(syncFrame("ses_d", 1))
	if !ok {
		t.Fatal("sync frame failed to decode")
	}
	if sid != "" {
		t.Fatalf("sync frame decoded with session id %q: it would be delivered "+
			"alongside the plain frame it duplicates", sid)
	}
	if typ != "sync" {
		t.Fatalf("type = %q, want sync", typ)
	}
}

// TestFixtureSyncFramesAdvanceContiguously reads the real capture: kilo's
// sequences are dense and monotonic within an aggregate, which is what makes a
// jump meaningful evidence rather than normal behaviour.
func TestFixtureSyncFramesAdvanceContiguously(t *testing.T) {
	data, err := os.ReadFile("testdata/wire/" + KnownGoodVersion + "/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	seqs := map[string][]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, `"sync"`) {
			continue
		}
		var frame struct {
			Payload struct {
				Type      string         `json:"type"`
				SyncEvent *kiloSyncEvent `json:"syncEvent"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil || frame.Payload.SyncEvent == nil {
			continue
		}
		ev := frame.Payload.SyncEvent
		seqs[ev.AggregateID] = append(seqs[ev.AggregateID], ev.Seq)
	}
	if len(seqs) == 0 {
		t.Fatal("no sync frames in the fixture: F7's premise is unverified")
	}
	for agg, list := range seqs {
		for i := 1; i < len(list); i++ {
			if list[i] != list[i-1]+1 {
				t.Errorf("aggregate %s: seq jumped %d -> %d in a clean capture; "+
					"if kilo's sequences are sparse, a gap is not evidence of loss",
					agg, list[i-1], list[i])
			}
		}
	}
}
