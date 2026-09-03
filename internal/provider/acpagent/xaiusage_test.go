package acpagent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestXAITurnCompletedCarriesGroksCacheSplit is the regression for the one
// provider whose cold/warm signal had nowhere to live (MADR 0137, second
// correction).
//
// `acpagent` maps ACP's SessionUsageUpdate, whose schema is {used, size, cost}
// and carries no cache fields at all (acp-go-sdk@v0.13.5 types_gen.go:5243).
// grok's token accounting travels on the `turn_completed` variant of
// `_x.ai/session_notification`, which this handler previously ignored — so
// every grok turn was recorded as having paid a full uncached prefill,
// including the ones that read 11776 cached tokens.
func TestXAITurnCompletedCarriesGroksCacheSplit(t *testing.T) {
	s := newQueueTestSession()
	params := json.RawMessage(`{"sessionId":"agent-q","update":{` +
		`"sessionUpdate":"turn_completed","stop_reason":"end_turn",` +
		`"usage":{"inputTokens":19042,"outputTokens":48,"totalTokens":19090,` +
		`"cachedReadTokens":5888,"cacheCreationTokens":0,"reasoningTokens":34}}}`)

	HandleXAISessionNotification(context.Background(), s, params)

	u := drainUsage(t, s)
	if u.CacheRead != 5888 {
		t.Fatalf("CacheRead = %d, want 5888: grok's warm turns are invisible", u.CacheRead)
	}
	if u.Used != 19090 || u.Input != 19042 || u.Output != 48 || u.Reasoning != 34 {
		t.Fatalf("token split wrong: %+v", u)
	}
}

// TestXAIUsageIsNotPublishedForAnotherSession keeps the origin rule that the
// rest of this handler already enforces: a sub-agent's frames arrive on the
// same channel, and its token accounting is not this conversation's.
func TestXAIUsageIsNotPublishedForAnotherSession(t *testing.T) {
	s := newQueueTestSession()
	params := json.RawMessage(`{"sessionId":"some-child","update":{` +
		`"sessionUpdate":"turn_completed",` +
		`"usage":{"totalTokens":19090,"cachedReadTokens":5888}}}`)

	HandleXAISessionNotification(context.Background(), s, params)

	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("got %d usage events for a different session's turn, want 0", n)
	}
}

// TestXAIUsageIgnoresAnEmptyReport proves a turn_completed without usage is
// not turned into a zero report, which would read as a real measurement.
func TestXAIUsageIgnoresAnEmptyReport(t *testing.T) {
	s := newQueueTestSession()
	HandleXAISessionNotification(context.Background(), s,
		json.RawMessage(`{"sessionId":"agent-q","update":{"sessionUpdate":"turn_completed"}}`))
	if n := countUsageEvents(s); n != 0 {
		t.Fatalf("got %d usage events for a turn reporting none, want 0", n)
	}
}

// TestFixtureConfirmsGrokReportsACacheRead reads the claim from grok's own
// wire rather than assuming it.
func TestFixtureConfirmsGrokReportsACacheRead(t *testing.T) {
	data, err := os.ReadFile("../grok/testdata/wire/1.0.13/frames.jsonl")
	if err != nil {
		t.Skipf("grok fixture unavailable: %v", err)
	}
	if !strings.Contains(string(data), `"cachedReadTokens"`) {
		t.Fatal("no cachedReadTokens in grok's fixture: the premise of this " +
			"mapping is that grok reports one")
	}
}

func drainUsage(t *testing.T, s *session) *event.Usage {
	t.Helper()
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeUsage && ev.Usage != nil {
				return ev.Usage
			}
		default:
			t.Fatal("no usage event emitted")
			return nil
		}
	}
}

func countUsageEvents(s *session) int {
	n := 0
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeUsage {
				n++
			}
		default:
			return n
		}
	}
}
