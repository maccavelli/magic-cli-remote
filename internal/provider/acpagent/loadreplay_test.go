package acpagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// scriptRequest is one request the client made of a scripted agent.
type scriptRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	write  func(string)
}

// Answer answers the request with result, JSON.
func (r scriptRequest) Answer(result string) {
	r.write(`{"jsonrpc":"2.0","id":` + string(r.ID) + `,"result":` + result + `}`)
}

// Fail answers the request with a JSON-RPC error.
func (r scriptRequest) Fail(code int, message string) {
	r.write(`{"jsonrpc":"2.0","id":` + string(r.ID) + `,"error":{"code":` + strconv.Itoa(code) +
		`,"message":` + strconv.Quote(message) + `}}`)
}

// startOnScript runs p.StartOn against an agent scripted by handle, which is
// given each request and a way to write a line to the client. Unlike acptest,
// the start's events stay on the session.
func startOnScript(t *testing.T, p *Provider, opts provider.StartOptions,
	handle func(r scriptRequest, write func(string)),
) (provider.Session, error) {
	t.Helper()
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	t.Cleanup(func() {
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	// One message per line, as the transport frames them.
	write := func(line string) {
		var b bytes.Buffer
		if err := json.Compact(&b, []byte(line)); err != nil {
			t.Errorf("scripted agent: %v: %s", err, line)
			return
		}
		b.WriteByte('\n')
		_, _ = agentWrites.Write(b.Bytes())
	}
	go func() {
		sc := bufio.NewScanner(agentReads)
		sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
		for sc.Scan() {
			var r scriptRequest
			if json.Unmarshal(sc.Bytes(), &r) != nil || len(r.ID) == 0 || r.Method == "" {
				continue
			}
			r.write = write
			handle(r, write)
		}
	}()
	if opts.CWD == "" {
		opts.CWD = t.TempDir()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := p.StartOn(ctx, opts, clientReads, clientWrites)
	if err == nil {
		t.Cleanup(func() { _ = s.Close(context.Background()) })
	}
	return s, err
}

// loadingSession is a session in the middle of session/load.
func loadingSession() *session {
	return &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 32),
		done:    make(chan struct{}),
		log:     slog.New(slog.DiscardHandler),
		loading: true,
	}
}

// replay sends one session/update frame, as the agent writes it.
func replay(t *testing.T, s *session, update string) {
	t.Helper()
	var n acp.SessionNotification
	if err := json.Unmarshal([]byte(`{"sessionId":"agent-1","update":`+update+`}`), &n); err != nil {
		t.Fatalf("frame: %v", err)
	}
	if err := s.SessionUpdate(context.Background(), n); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
}

// drainEvents returns what s emitted, waiting briefly for the last of it.
func drainEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

// TestLoadReplaysThePrompts pins 0214 MADR amendment B, D1 (B2): while a
// session loads, a replayed user_message_chunk is the conversation's prompt and
// is emitted as a replayed user message, for every Spec. Chunks of one message
// make one message. The replay carries no usage, and none is reported (D6, B3).
func TestLoadReplaysThePrompts(t *testing.T) {
	s := loadingSession()
	// G2's shape: each prompt as user_message_chunk with its message id, each
	// answer as agent_message_chunk, and no usage_update.
	replay(t, s, `{"sessionUpdate":"user_message_chunk","messageId":"msg-u1","content":{"type":"text","text":"write note"}}`)
	replay(t, s, `{"sessionUpdate":"user_message_chunk","messageId":"msg-u1","content":{"type":"text","text":".txt"}}`)
	replay(t, s, `{"sessionUpdate":"agent_message_chunk","messageId":"msg-a1","content":{"type":"text","text":"Done."}}`)
	replay(t, s, `{"sessionUpdate":"user_message_chunk","messageId":"msg-u2","content":{"type":"text","text":"and again"}}`)
	s.flushReplayedUser()

	var users []string
	for _, ev := range drainEvents(s) {
		switch ev.Type {
		case event.TypeUserMessage:
			if !ev.Replay {
				t.Errorf("replayed prompt %q not marked replay", ev.Text)
			}
			users = append(users, ev.Text)
		case event.TypeUsage:
			t.Errorf("a load reported usage: %+v", ev)
		}
	}
	if len(users) != 2 || users[0] != "write note.txt" || users[1] != "and again" {
		t.Errorf("replayed prompts = %q, want [\"write note.txt\" \"and again\"]", users)
	}
}

// TestSessionLoadEmitsTheLastReplayedPrompt runs session/load end to end: the
// agent replays one prompt and then answers the load, and the prompt is
// emitted as replay before the session stops loading.
func TestSessionLoadEmitsTheLastReplayedPrompt(t *testing.T) {
	s, err := startOnScript(t, New(Spec{ID: "vendor"}, Config{}), provider.StartOptions{AgentSessionID: "agent-1"},
		func(r scriptRequest, write func(string)) {
			switch r.Method {
			case "initialize":
				r.Answer(`{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}`)
			case "session/load":
				write(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"agent-1",` +
					`"update":{"sessionUpdate":"user_message_chunk","messageId":"msg-u1","content":{"type":"text","text":"the prompt"}}}}`)
				r.Answer(`{}`)
			}
		})
	if err != nil {
		t.Fatalf("StartOn: %v", err)
	}
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeUserMessage {
				if ev.Text != "the prompt" || !ev.Replay {
					t.Fatalf("replayed prompt = %q, replay %v; want \"the prompt\" as replay", ev.Text, ev.Replay)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("the replayed prompt was not emitted by the load")
		}
	}
}

// TestLiveUserMessageChunkIsStillSkipped: outside a load, the chunk is the
// agent's echo of a prompt Magic already emitted.
func TestLiveUserMessageChunkIsStillSkipped(t *testing.T) {
	s := loadingSession()
	s.loading = false
	replay(t, s, `{"sessionUpdate":"user_message_chunk","messageId":"msg-u1","content":{"type":"text","text":"hi"}}`)
	replay(t, s, `{"sessionUpdate":"agent_message_chunk","messageId":"msg-a1","content":{"type":"text","text":"hello"}}`)
	s.flushReplayedUser()
	for _, ev := range drainEvents(s) {
		if ev.Type == event.TypeUserMessage {
			t.Errorf("a live echo became a user message: %+v", ev)
		}
	}
}
