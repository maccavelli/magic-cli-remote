package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// latencyScript says what a fake turn emits and when, as offsets from the
// moment the provider is handed the prompt.
//
// Offsets rather than sleeps: the manager starts its clock immediately before
// calling Prompt, so a provider that stamps its own events off a base captured
// inside Prompt produces the same arithmetic a real turn would, without the
// test depending on the scheduler. Real providers stamp their own timestamps
// too (codex/session.go:2305 and the four others), so this is the shape the
// production path actually sees.
type latencyScript struct {
	events []scriptEvent
}

type scriptEvent struct {
	at    time.Duration
	typ   event.Type
	usage *event.Usage
}

type latencyProvider struct {
	script latencyScript
	// reportsModel makes Start return a session implementing
	// provider.ModelReporter, standing in for kilo and opencode. Empty returns
	// a plain session, standing in for grok, goose and codex.
	reportsModel string
}

func (p *latencyProvider) ID() provider.ID { return "lat" }
func (p *latencyProvider) Ready() bool     { return true }

func (p *latencyProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "lat-1"
	}
	s := &latencySession{id: id, script: p.script, events: make(chan event.Event, 32)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle",
		Timestamp: time.Now().UTC()}
	if p.reportsModel != "" {
		return &latencyModelSession{latencySession: s, model: p.reportsModel}, nil
	}
	return s, nil
}

// latencyModelSession is a session that knows its own model, the shape
// provider.ModelReporter exists for.
type latencyModelSession struct {
	*latencySession
	model string
}

func (s *latencyModelSession) CurrentModel() string { return s.model }

type latencySession struct {
	id     string
	script latencyScript
	events chan event.Event

	mu     sync.Mutex
	closed bool
}

func (s *latencySession) ID() string                   { return s.id }
func (s *latencySession) ProviderID() provider.ID      { return "lat" }
func (s *latencySession) AgentSessionID() string       { return s.id }
func (s *latencySession) Cancel(context.Context) error { return nil }
func (s *latencySession) Events() <-chan event.Event   { return s.events }

func (s *latencySession) Prompt(_ context.Context, _ []provider.Content) error {
	base := time.Now().UTC()
	for _, se := range s.script.events {
		s.events <- event.Event{
			Type:      se.typ,
			SessionID: s.id,
			Timestamp: base.Add(se.at),
			Usage:     se.usage,
		}
	}
	return nil
}

func (s *latencySession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

// syncBuf is a writer the pump goroutine and the test can both touch.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// turnRecords returns every "turn latency" record logged so far, decoded.
func (b *syncBuf) turnRecords(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(b.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		if rec["msg"] == "turn latency" {
			out = append(out, rec)
		}
	}
	return out
}

func newLatencyManager(t *testing.T, script latencyScript) (*session.Manager, *syncBuf, session.Meta) {
	t.Helper()
	return newLatencyManagerModel(t, script, "")
}

func newLatencyManagerModel(
	t *testing.T, script latencyScript, model string,
) (*session.Manager, *syncBuf, session.Meta) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(&latencyProvider{script: script, reportsModel: model})
	buf := &syncBuf{}
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mgr := session.NewManager(reg, nil, log, nil)
	meta, err := mgr.Create(context.Background(), "lat", provider.StartOptions{
		Name: "s", LocalSessionID: "lat-1",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close(context.Background(), meta.ID, "dev-a") })
	return mgr, buf, meta
}

// waitForTurnRecords blocks until n records have been logged, or fails.
func waitForTurnRecords(t *testing.T, buf *syncBuf, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		recs := buf.turnRecords(t)
		if len(recs) >= n {
			return recs
		}
		if time.Now().After(deadline) {
			t.Fatalf("wanted %d \"turn latency\" records, got %d; log was:\n%s",
				n, len(recs), buf.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func num(t *testing.T, rec map[string]any, key string) float64 {
	t.Helper()
	v, ok := rec[key]
	if !ok {
		t.Fatalf("record has no %q: %v", key, rec)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("%q is %T, want a number: %v", key, v, rec)
	}
	return f
}

// TestTurnEmitsExactlyOneLatencyRecord is the base claim: one prompt, one
// record, carrying the identity a reader needs to attribute it (MADR 0137
// Phase 2).
//
// "Exactly one" is the load-bearing half. A turn emits dozens of chunks and
// several terminal-adjacent events, and a record per chunk would be noise
// rather than instrumentation.
func TestTurnEmitsExactlyOneLatencyRecord(t *testing.T) {
	mgr, buf, meta := newLatencyManager(t, latencyScript{events: []scriptEvent{
		{at: 200 * time.Millisecond, typ: event.TypeThoughtChunk},
		{at: 260 * time.Millisecond, typ: event.TypeAssistantChunk},
		{at: 300 * time.Millisecond, typ: event.TypeAssistantChunk},
		{at: 480 * time.Millisecond, typ: event.TypeUsage,
			usage: &event.Usage{Used: 14435, Input: 99, Output: 12, CacheRead: 14336}},
		{at: 500 * time.Millisecond, typ: event.TypeTurnComplete},
	}})

	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	recs := waitForTurnRecords(t, buf, 1)

	// Give any spurious extra record time to land before counting.
	time.Sleep(100 * time.Millisecond)
	if got := len(buf.turnRecords(t)); got != 1 {
		t.Fatalf("got %d turn latency records for one turn, want exactly 1:\n%s",
			got, buf.String())
	}

	rec := recs[0]
	if rec["session_id"] != meta.ID {
		t.Errorf("session_id = %v, want %s", rec["session_id"], meta.ID)
	}
	if rec["provider"] != "lat" {
		t.Errorf("provider = %v, want lat", rec["provider"])
	}
	// The turn's own usage must ride along: a latency number without the
	// cold/warm bit cannot distinguish the regression from a cache miss.
	if rec["cold"] != false {
		t.Errorf("cold = %v, want false on a turn reporting cache_read", rec["cold"])
	}
	if got := num(t, rec, "cache_read"); got != 14336 {
		t.Errorf("cache_read = %v, want 14336", got)
	}
	if got := num(t, rec, "context_used"); got != 14435 {
		t.Errorf("context_used = %v, want 14435", got)
	}
}

// TestTTFTIsMeasuredToFirstOutputNotToCompletion is the assertion the whole
// phase exists for.
//
// A record that timed the whole turn twice would look correct — one number,
// plausible magnitude — and would be useless: the regression under
// investigation is time spent BEFORE the first token, and a turn_ms that
// already includes generation cannot separate the two.
func TestTTFTIsMeasuredToFirstOutputNotToCompletion(t *testing.T) {
	mgr, buf, meta := newLatencyManager(t, latencyScript{events: []scriptEvent{
		{at: 200 * time.Millisecond, typ: event.TypeThoughtChunk},
		{at: 900 * time.Millisecond, typ: event.TypeAssistantChunk},
		{at: 1000 * time.Millisecond, typ: event.TypeTurnComplete},
	}})

	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	rec := waitForTurnRecords(t, buf, 1)[0]

	ttft := num(t, rec, "ttft_ms")
	turn := num(t, rec, "turn_ms")
	// Bounds are wide because the manager's clock starts a few microseconds
	// before the provider's base; they are still far too tight to pass if
	// ttft were measured to turn_complete (1000ms) instead.
	if ttft < 180 || ttft > 260 {
		t.Errorf("ttft_ms = %v, want ~200 (the first thought chunk); "+
			"a value near turn_ms means it was measured to completion", ttft)
	}
	if turn < 980 || turn > 1080 {
		t.Errorf("turn_ms = %v, want ~1000", turn)
	}
	if turn-ttft < 500 {
		t.Errorf("turn_ms (%v) and ttft_ms (%v) are the same measurement; "+
			"the wait before the first token is not separable", turn, ttft)
	}
}

// TestTurnWithNoOutputOmitsTTFT pins the distinction a zero would erase.
//
// A turn that answered instantly and a turn that never answered at all are
// opposite outcomes. Reporting ttft_ms: 0 for the second makes the second look
// like the best turn in the log, which is exactly backwards for a latency
// investigation.
func TestTurnWithNoOutputOmitsTTFT(t *testing.T) {
	mgr, buf, meta := newLatencyManager(t, latencyScript{events: []scriptEvent{
		{at: 700 * time.Millisecond, typ: event.TypeError},
	}})

	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	rec := waitForTurnRecords(t, buf, 1)[0]

	if v, ok := rec["ttft_ms"]; ok {
		t.Errorf("ttft_ms = %v on a turn that produced no output; it must be "+
			"absent, because zero would read as an instant answer", v)
	}
	if turn := num(t, rec, "turn_ms"); turn < 680 {
		t.Errorf("turn_ms = %v, want ~700: the wait still happened", turn)
	}
	if rec["failed"] != true {
		t.Errorf("failed = %v, want true on a turn ending in an error", rec["failed"])
	}
}

// TestRecordNamesTheModelTheSessionIsActuallyRunningOn covers the MADR 0137
// eighth amendment.
//
// The client asked for no model, so Meta.Model is empty — the normal case
// under the accepted "providers run on their own default" constraint. Before
// ModelReporter the record simply had no model in that case, which is the one
// case that matters.
func TestRecordNamesTheModelTheSessionIsActuallyRunningOn(t *testing.T) {
	mgr, buf, meta := newLatencyManagerModel(t, latencyScript{events: []scriptEvent{
		{at: 100 * time.Millisecond, typ: event.TypeAssistantChunk},
		{at: 200 * time.Millisecond, typ: event.TypeTurnComplete},
	}}, "kilo/kilo-auto/balanced")

	if meta.Model != "" {
		t.Fatalf("Meta.Model = %q; this test is only meaningful when the client "+
			"named no model", meta.Model)
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	rec := waitForTurnRecords(t, buf, 1)[0]

	if rec["model"] != "kilo/kilo-auto/balanced" {
		t.Errorf("model = %v, want kilo/kilo-auto/balanced: a default-model "+
			"session must still name what it ran on", rec["model"])
	}
}

// TestRecordOmitsTheModelWhenTheSessionCannotReportOne is the other half.
//
// grok, goose and codex track no default, and a record that filled the gap
// with the provider id, the string "default", or the client's empty request
// would be a fabrication a reader could not tell from a real answer.
func TestRecordOmitsTheModelWhenTheSessionCannotReportOne(t *testing.T) {
	mgr, buf, meta := newLatencyManager(t, latencyScript{events: []scriptEvent{
		{at: 100 * time.Millisecond, typ: event.TypeAssistantChunk},
		{at: 200 * time.Millisecond, typ: event.TypeTurnComplete},
	}})

	if err := mgr.Prompt(context.Background(), meta.ID, "hi", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	rec := waitForTurnRecords(t, buf, 1)[0]

	if v, ok := rec["model"]; ok {
		t.Errorf("model = %v on a session that reports none; it must be absent", v)
	}
}
