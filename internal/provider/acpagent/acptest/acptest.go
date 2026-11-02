// Package acptest is a fixture ACP agent, for testing a Spec on real sessions:
// the agent end of a connection on in-memory pipes, with
// [acpagent.Provider.StartOn] running Magic's end. A Spec's package tests its
// hooks through it without reaching into acpagent (MADR 0208, P6b deviation 1).
package acptest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Budget bounds every wait in this package.
const Budget = 5 * time.Second

// Options shapes the fixture agent.
type Options struct {
	// SessionID is the id session/new answers with; "agent-1" when empty.
	SessionID string
	// AgentCapabilities is initialize's agentCapabilities, JSON; {} when nil.
	AgentCapabilities json.RawMessage
	// InitializeMeta is initialize's top-level `_meta`, JSON; none when nil.
	InitializeMeta json.RawMessage
	// AuthMethods are the agent auth method ids initialize advertises.
	AuthMethods []string
	// Replies maps a method to the raw JSON result it is answered with.
	// initialize, authenticate, session/new and session/load have defaults;
	// any other request is answered -32601, as an agent without the method
	// would, unless it is held.
	Replies map[string]string
	// Hold names methods that are not answered: the test takes them with
	// Await and answers with Respond. session/prompt is always held unless
	// Replies names it.
	Hold []string
	// Start is passed to StartOn. Its CWD defaults to a new temporary
	// directory.
	Start provider.StartOptions
}

// Message is one JSON-RPC message the client wrote.
type Message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// Agent is the fixture agent's end of a connection.
type Agent struct {
	t      testing.TB
	opts   Options
	toConn io.Writer
	wmu    sync.Mutex

	mu       sync.Mutex
	replies  map[string]string
	requests []Message
	held     chan Message
	answers  chan Message
	nextID   int
}

// Start starts a session for p on a fixture agent, and drains the events the
// start emitted. The session is closed, and the pipes with it, when the test
// ends.
func Start(t testing.TB, p *acpagent.Provider, opts Options) (provider.Session, *Agent) {
	t.Helper()
	if opts.SessionID == "" {
		opts.SessionID = "agent-1"
	}
	if opts.Start.CWD == "" {
		opts.Start.CWD = t.TempDir()
	}
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	a := &Agent{
		t:       t,
		opts:    opts,
		toConn:  agentWrites,
		replies: map[string]string{},
		held:    make(chan Message, 64),
		answers: make(chan Message, 64),
		nextID:  1000,
	}
	for m, r := range opts.Replies {
		a.replies[m] = r
	}
	go a.read(agentReads)

	ctx, cancel := context.WithTimeout(context.Background(), Budget)
	defer cancel()
	s, err := p.StartOn(ctx, opts.Start, clientReads, clientWrites)
	if err != nil {
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		t.Fatalf("acptest: StartOn: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close(context.Background())
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	Drain(s)
	return s, a
}

func (a *Agent) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
	for sc.Scan() {
		var m Message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" {
			a.answers <- m
			continue
		}
		a.mu.Lock()
		a.requests = append(a.requests, m)
		result, known := a.replies[m.Method]
		a.mu.Unlock()
		if len(m.ID) == 0 {
			continue // a notification: nothing to answer
		}
		if !known {
			result, known = a.defaultReply(m.Method)
		}
		switch {
		case known:
			a.sendRaw(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, m.ID, result))
		case m.Method == "session/prompt" || slices.Contains(a.opts.Hold, m.Method):
			a.held <- m
		default:
			a.sendRaw(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}`, m.ID))
		}
	}
}

// defaultReply answers the handshake's methods.
func (a *Agent) defaultReply(method string) (string, bool) {
	switch method {
	case "initialize":
		caps := a.opts.AgentCapabilities
		if caps == nil {
			caps = json.RawMessage(`{}`)
		}
		methods := make([]string, 0, len(a.opts.AuthMethods))
		for _, id := range a.opts.AuthMethods {
			methods = append(methods, `{"id":`+strconv.Quote(id)+`,"name":`+strconv.Quote(id)+`}`)
		}
		meta := ""
		if a.opts.InitializeMeta != nil {
			meta = `,"_meta":` + string(a.opts.InitializeMeta)
		}
		return `{"protocolVersion":1,"agentCapabilities":` + string(caps) +
			`,"authMethods":[` + strings.Join(methods, ",") + `]` + meta + `}`, true
	case "authenticate":
		return `{}`, true
	case "session/new":
		return `{"sessionId":` + strconv.Quote(a.opts.SessionID) + `}`, true
	case "session/load":
		return `{}`, true
	}
	return "", false
}

// sendRaw writes one message to the client, compacted onto one line.
func (a *Agent) sendRaw(line string) {
	var framed bytes.Buffer
	if err := json.Compact(&framed, []byte(line)); err != nil {
		a.t.Errorf("acptest: message is not JSON: %v: %s", err, line)
		return
	}
	framed.WriteByte('\n')
	a.wmu.Lock()
	defer a.wmu.Unlock()
	if _, err := a.toConn.Write(framed.Bytes()); err != nil {
		a.t.Logf("acptest: write: %v", err)
	}
}

// Send writes one JSON-RPC message to the client, exactly as given.
func (a *Agent) Send(line string) { a.sendRaw(line) }

// Notify sends a notification.
func (a *Agent) Notify(method string, params any) {
	a.sendRaw(a.marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}))
}

// Call sends a request and returns its id, for AnswerTo.
func (a *Agent) Call(method string, params any) string {
	a.mu.Lock()
	a.nextID++
	id := strconv.Itoa(a.nextID)
	a.mu.Unlock()
	a.sendRaw(a.marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}))
	return id
}

// AnswerTo returns the client's answer to the request with id, skipping the
// answers to others.
func (a *Agent) AnswerTo(id string) Message {
	a.t.Helper()
	deadline := time.After(Budget)
	for {
		select {
		case m := <-a.answers:
			if strings.TrimSpace(string(m.ID)) == id {
				return m
			}
		case <-deadline:
			a.t.Fatalf("acptest: no answer to request %s within %s", id, Budget)
			return Message{}
		}
	}
}

// Await returns the next held request for method, skipping other held ones.
func (a *Agent) Await(method string) Message {
	a.t.Helper()
	deadline := time.After(Budget)
	for {
		select {
		case m := <-a.held:
			if m.Method == method {
				return m
			}
		case <-deadline:
			a.t.Fatalf("acptest: no %s within %s", method, Budget)
			return Message{}
		}
	}
}

// Respond answers a held request with result.
func (a *Agent) Respond(m Message, result any) {
	a.sendRaw(a.marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}))
}

// RespondError answers a held request with an error.
func (a *Agent) RespondError(m Message, code int, message string) {
	a.sendRaw(a.marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID,
		"error": map[string]any{"code": code, "message": message}}))
}

// SetReply sets the raw JSON result a method is answered with from now on.
func (a *Agent) SetReply(method, result string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.replies[method] = result
}

// Requests returns every request and notification the client sent, in order.
func (a *Agent) Requests() []Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.requests)
}

// Methods returns the method of every message the client sent after the
// handshake: initialize, authenticate and session/new or session/load are
// left out.
func (a *Agent) Methods() []string {
	out := []string{}
	for _, m := range a.Requests() {
		switch m.Method {
		case "initialize", "authenticate", "session/new", "session/load":
			continue
		}
		out = append(out, m.Method)
	}
	return out
}

// Sent reports whether the client sent method.
func (a *Agent) Sent(method string) bool {
	return slices.ContainsFunc(a.Requests(), func(m Message) bool { return m.Method == method })
}

// ParamsFor returns the decoded params of the first message for method.
func (a *Agent) ParamsFor(method string) map[string]any {
	a.t.Helper()
	for _, m := range a.Requests() {
		if m.Method != method {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(m.Params, &p); err != nil {
			a.t.Fatalf("acptest: decode params of %s: %v", method, err)
		}
		return p
	}
	a.t.Fatalf("acptest: nothing was sent for %s; the wire carried %v", method, a.Methods())
	return nil
}

func (a *Agent) marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		a.t.Fatalf("acptest: marshal: %v", err)
	}
	return string(b)
}

// Drain returns every event s has already queued, without waiting.
func Drain(s provider.Session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.Events():
			out = append(out, ev)
		default:
			return out
		}
	}
}

// Next returns the next event of type want, skipping others, or fails.
func Next(t testing.TB, s provider.Session, want event.Type) event.Event {
	t.Helper()
	deadline := time.After(Budget)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("acptest: no %s event within %s", want, Budget)
			return event.Event{}
		}
	}
}

// Host returns s as the Host a Spec's hooks receive.
func Host(t testing.TB, s provider.Session) acpagent.Host {
	t.Helper()
	h, ok := s.(acpagent.Host)
	if !ok {
		t.Fatalf("acptest: %T is not an acpagent.Host", s)
	}
	return h
}
