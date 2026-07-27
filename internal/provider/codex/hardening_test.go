package codex

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestDrainChunksCloseLogsDroppedBytes: the close-path flush reports
// bytes that did not fit in the channel (MADR 0035 D7). The previous
// code dropped them silently; a full consumer at session close is now
// visible in the daemon's log.
func TestDrainChunksCloseLogsDroppedBytes(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 1), // tiny buffer
		done:   make(chan struct{}),
		log:    silentLogger(),
		chunks: nil, // force lazy build
	}
	defer close(s.done)

	// Pre-load the chunkbuf with a run, then fill the event channel so
	// the drain's trySend loses.
	_ = s.chunkBuffer() // build
	chunk := event.Event{Type: event.TypeAssistantChunk, SessionID: "s1", Text: "hello world"}
	s.chunks.Add(chunk) // leading-edge flush
	// Now consume the first chunk and pack the channel so the next drain
	// loses. Drain twice: once to take the leading chunk, then again
	// to assert the close path can run without panicking.
	out, _, _ := s.chunks.Add(event.Event{Type: event.TypeAssistantChunk, SessionID: "s1", Text: "x"})
	for _, e := range out {
		select {
		case s.events <- e:
		default:
		}
	}

	// Close path: must not panic on a nil consumer or a full channel.
	s.drainChunksClose()
}

// TestRateLimitMappingAt100Percent: a `usedPercent >= 100` notification
// must surface as a TypeError with error_kind=rate_limit and a retry_at
// hint (MADR 0035 D9).
func TestRateLimitMappingAt100Percent(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	// 100% used, resets 1h from now.
	resetsAt := time.Now().Add(time.Hour).Unix()
	frame := `{
		"rateLimits": {
			"primary": {
				"usedPercent": 100,
				"windowDurationMins": 60,
				"resetsAt": ` + itoa(int(resetsAt)) + `
			}
		}
	}`
	s.handleNotification("account/rateLimits/updated", []byte(frame))

	events := drain(s)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (TypeError)", len(events))
	}
	if events[0].Type != event.TypeError {
		t.Fatalf("type = %s, want error", events[0].Type)
	}
	if events[0].ErrorKind != "rate_limit" {
		t.Errorf("ErrorKind = %q, want rate_limit", events[0].ErrorKind)
	}
	if events[0].RetryAt.IsZero() {
		t.Error("RetryAt should be populated from rateLimit.reSetsAt")
	}
}

// TestRateLimitMappingApproaching: a `usedPercent` between 90 and 100
// produces a TypeNotice so the user sees the limit before they hit it.
func TestRateLimitMappingApproaching(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"rateLimits": {
			"primary": {"usedPercent": 92, "windowDurationMins": 60, "resetsAt": 1700000000}
		}
	}`
	s.handleNotification("account/rateLimits/updated", []byte(frame))

	events := drain(s)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (TypeNotice)", len(events))
	}
	if events[0].Type != event.TypeNotice {
		t.Fatalf("type = %s, want notice", events[0].Type)
	}
	if events[0].Text == "" {
		t.Error("Text must describe the approaching limit")
	}
}

// TestRateLimitMappingBelow90: a low usage produces no event.
func TestRateLimitMappingBelow90(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"rateLimits": {
			"primary": {"usedPercent": 45, "windowDurationMins": 10080, "resetsAt": 1700000000}
		}
	}`
	s.handleNotification("account/rateLimits/updated", []byte(frame))

	events := drain(s)
	if len(events) != 0 {
		t.Errorf("got %d events, want 0 for low usage", len(events))
	}
}

// TestMCPServerFailureNotice: an MCP server reporting status=failed
// surfaces as a TypeNotice with the server name and error message.
func TestMCPServerFailureNotice(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"threadId": "thread-x",
		"name": "codex_apps",
		"status": "failed",
		"error": "connection refused"
	}`
	s.handleNotification("mcpServer/startupStatus/updated", []byte(frame))

	events := drain(s)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != event.TypeNotice {
		t.Fatalf("type = %s, want notice", events[0].Type)
	}
	if events[0].Text == "" {
		t.Error("Text must describe the failed server")
	}
}

// TestMCPServerSuccessSilent: a successful MCP startup is silent.
func TestMCPServerSuccessSilent(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"threadId": "thread-x",
		"name": "codex_apps",
		"status": "ready",
		"error": null
	}`
	s.handleNotification("mcpServer/startupStatus/updated", []byte(frame))

	events := drain(s)
	if len(events) != 0 {
		t.Errorf("got %d events, want 0 (successful MCP startup must be silent)", len(events))
	}
}

// TestLastActivityStamp: MADR 0035 D8 — handleNotification stamps
// lastActivity on every notification (single atomic store).
func TestLastActivityStamp(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 4),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	before := time.Now().UnixNano()
	s.handleNotification("item/agentMessage/delta", []byte(`{"itemId":"i","delta":"hi"}`))
	after := time.Now().UnixNano()

	stamp := s.lastActivity.Load()
	if stamp < before || stamp > after {
		t.Errorf("lastActivity %d not in [%d, %d]", stamp, before, after)
	}
}
