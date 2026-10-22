package acpagent

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// wireMsg is one JSON-RPC message the client wrote, as the agent reads it.
type wireMsg struct {
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

// wireAgent plays the agent end of a real ACP connection at the byte level:
// it answers initialize with the agent capabilities it was given, and hands
// every other line the client writes to the test. It is for tests of what
// the SDK, configured as production configures it, answers at Magic's
// boundary (PLAN 0184 A2).
type wireAgent struct {
	t       *testing.T
	toConn  *io.PipeWriter
	lines   chan wireMsg
	stall   chan struct{} // closed to make the agent stop reading
	stalled sync.Once
	done    chan struct{} // closed at cleanup, ending a stalled reader
}

// newWireSession builds a session on a real connection whose peer is a
// wireAgent, and completes initialize with Magic's own initialize request, so
// the SDK's gates hold what production negotiates. agentCaps is the agent's
// capabilities block as JSON ("{}" for none).
func newWireSession(t *testing.T, agentCaps string) (*session, *wireAgent) {
	t.Helper()
	return newWireSessionFor(t, agentCaps, &Provider{})
}

// newWireSessionFor is newWireSession for a given provider, whose Spec decides
// what the client advertises at initialize.
func newWireSessionFor(t *testing.T, agentCaps string, p *Provider) (*session, *wireAgent) {
	t.Helper()
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	a := &wireAgent{t: t, toConn: agentWrites, lines: make(chan wireMsg, 64), stall: make(chan struct{}),
		done: make(chan struct{})}

	s := &session{
		localID:   "local-1",
		agentID:   "agent-1",
		log:       slog.New(slog.DiscardHandler),
		events:    make(chan event.Event, 256),
		done:      make(chan struct{}),
		attached:  true,
		pending:   make(map[string]*permWaiter),
		questions: make(map[string]*questionWaiter),
		terms:     newTerminalHost(),
	}
	go a.read(agentReads, agentCaps)
	s.conn = acp.NewClientSideConnection(s, &stallingWriter{w: clientWrites, a: a}, clientReads, clientConnOptions(nil, s)...)
	t.Cleanup(func() {
		close(a.done)
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	initializeForTestWith(t, s.conn, p.initializeRequest())
	return s, a
}

func (a *wireAgent) read(r io.Reader, agentCaps string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 32<<20)
	for sc.Scan() {
		var m wireMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "initialize" {
			a.sendRaw(`{"jsonrpc":"2.0","id":` + string(m.ID) +
				`,"result":{"protocolVersion":1,"agentCapabilities":` + agentCaps + `}}`)
			continue
		}
		select {
		case a.lines <- m:
		case <-a.stall:
			// Stop reading: the client's next write blocks on the pipe, as it
			// would against an agent that has stopped reading its stdin.
			<-a.done
			return
		}
	}
}

// stopReading makes the agent stop reading what the client writes: from now
// on every client write blocks, as a write to an agent that has stopped
// reading its stdin does once the pipe is full.
func (a *wireAgent) stopReading() { a.stalled.Do(func() { close(a.stall) }) }

// stallingWriter is the client's end of the pipe. Once its agent has stopped
// reading, a Write blocks until the test ends.
type stallingWriter struct {
	w io.Writer
	a *wireAgent
}

func (s *stallingWriter) Write(p []byte) (int, error) {
	select {
	case <-s.a.stall:
		<-s.a.done
		return 0, io.ErrClosedPipe
	default:
	}
	return s.w.Write(p)
}

// quiet reports the next message the client writes within d, or false when
// it writes nothing.
func (a *wireAgent) quiet(d time.Duration) (wireMsg, bool) {
	select {
	case m := <-a.lines:
		return m, true
	case <-time.After(d):
		return wireMsg{}, false
	}
}

// sendRaw writes one line to the client, exactly as given.
func (a *wireAgent) sendRaw(line string) {
	if _, err := io.WriteString(a.toConn, line+"\n"); err != nil {
		a.t.Logf("wireAgent: write: %v", err)
	}
}

// send writes one JSON-RPC message to the client.
func (a *wireAgent) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		a.t.Fatalf("wireAgent: marshal: %v", err)
	}
	a.sendRaw(string(b))
}

// next returns the next message the client wrote, or fails after budget.
func (a *wireAgent) next(budget time.Duration) wireMsg {
	a.t.Helper()
	select {
	case m := <-a.lines:
		return m
	case <-time.After(budget):
		a.t.Fatalf("wireAgent: nothing from the client within %s", budget)
		return wireMsg{}
	}
}

// answerTo returns the client's answer to the request with id, skipping
// anything else it writes first.
func (a *wireAgent) answerTo(id string, budget time.Duration) wireMsg {
	a.t.Helper()
	deadline := time.Now().Add(budget)
	for {
		m := a.next(time.Until(deadline))
		if strings.TrimSpace(string(m.ID)) == id && m.Method == "" {
			return m
		}
	}
}

// request reads the next request the client sent for method, skipping
// anything else.
func (a *wireAgent) request(method string, budget time.Duration) wireMsg {
	a.t.Helper()
	deadline := time.Now().Add(budget)
	for {
		m := a.next(time.Until(deadline))
		if m.Method == method {
			return m
		}
	}
}

// respond answers the client's request m with result.
func (a *wireAgent) respond(m wireMsg, result any) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
}
