package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// capture0166 returns the frames of codex 0.166.0's capture (0208-PLAN P5
// Deviations 3 and 4): the turn's thread/tokenUsage/updated notification and
// the account/rateLimits/read answer.
func capture0166(t *testing.T) (usageParams, limitsResult string) {
	t.Helper()
	f, err := os.Open("testdata/wire/0.166.0/frames.jsonl")
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var frame struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(sc.Bytes(), &frame); err != nil {
			t.Fatalf("capture frame: %v", err)
		}
		switch {
		case frame.Method == "thread/tokenUsage/updated":
			usageParams = string(frame.Params)
		case len(frame.Result) > 0:
			limitsResult = string(frame.Result)
		}
	}
	if usageParams == "" || limitsResult == "" {
		t.Fatal("the 0.166.0 capture lacks the usage notification or the rate-limits answer")
	}
	return usageParams, limitsResult
}

// TestUsagePassesCodexTokenUsageThrough is 0208-PLAN P5 step 3 for codex
// `/usage`: the latest thread/tokenUsage/updated, byte for byte.
func TestUsagePassesCodexTokenUsageThrough(t *testing.T) {
	params, _ := capture0166(t)
	s := p4Session()

	before, err := s.RuntimeUsage(context.Background())
	if err != nil || len(before.Body) != 0 || before.Text == "" {
		t.Fatalf("before any report: %+v, %v; want only a line saying there is none", before, err)
	}

	s.handleDecodedNotification("thread/tokenUsage/updated", json.RawMessage(params), time.Now())
	res, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	if string(res.Body) != params {
		t.Errorf("body = %s\nwant codex's notification unchanged: %s", res.Body, params)
	}
	if res.Method != "thread/tokenUsage/updated" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the notification's method and no text", res.Method, res.Text)
	}
}

// fakeEngine connects p to a pipe whose far end answers each request with
// result, and reports each request's method on the returned channel.
func fakeEngine(t *testing.T, p *Provider, result string) <-chan string {
	t.Helper()
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})
	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})
	p.eng.conn = c
	asked := make(chan string, 4)
	go func() {
		for {
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if readFrame(engineR, &req) != nil {
				return
			}
			asked <- req.Method
			_, _ = io.WriteString(engineW, `{"id":`+jsonInt(req.ID)+`,"result":`+result+"}\n")
		}
	}()
	return asked
}

// TestStatusPassesCodexRateLimitsThrough is 0208-PLAN P5 step 3 for codex
// `/status`: account/rateLimits/read is sent, and its answer comes back byte
// for byte.
func TestStatusPassesCodexRateLimitsThrough(t *testing.T) {
	_, result := capture0166(t)
	p, _ := negotiatedProvider(t, false)
	asked := fakeEngine(t, p, result)
	s := p4Session()
	s.p = p

	res, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	}
	select {
	case m := <-asked:
		if m != "account/rateLimits/read" {
			t.Fatalf("asked codex for %q, want account/rateLimits/read", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("codex was never asked for account/rateLimits/read")
	}
	if string(res.Body) != result {
		t.Errorf("body = %s\nwant codex's answer unchanged: %s", res.Body, result)
	}
	if res.Method != "account/rateLimits/read" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the read's method and no text", res.Method, res.Text)
	}
}

// TestStatusWithoutTheCapabilityIsALine: a running engine that does not
// advertise the read is never asked, and `/status` says why.
func TestStatusWithoutTheCapabilityIsALine(t *testing.T) {
	_, result := capture0166(t)
	p := New(Config{})
	p.eng = &engine{ready: true, capabilities: newCapabilityState(CapabilitySnapshot{
		Supported: map[CapabilityID]bool{},
	})}
	asked := fakeEngine(t, p, result)
	s := p4Session()
	s.p = p

	res, err := s.RuntimeStatus(context.Background())
	if err != nil || len(res.Body) != 0 || !strings.Contains(res.Text, "does not advertise") {
		t.Fatalf("status = %+v, %v; want only a line saying codex does not advertise the read", res, err)
	}
	select {
	case m := <-asked:
		t.Fatalf("codex was asked for %q though it does not advertise it", m)
	case <-time.After(200 * time.Millisecond):
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
