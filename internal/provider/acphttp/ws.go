package acphttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// ErrEngineDown reports that no live engine WebSocket is available for an RPC:
// the engine died (or was shut down) and has not been restarted. Callers must
// surface this instead of dereferencing a nil framer.
var ErrEngineDown = errors.New("agent engine not running")

// rpcFramer is the session-facing half of the engine WebSocket: JSON-RPC
// requests outbound, responses to agent-initiated requests inbound. *wsFramer
// is the production implementation; tests substitute a fake so turn lifecycle
// behaviour can be exercised without an engine.
type rpcFramer interface {
	sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error)
	sendNotification(ctx context.Context, method string, params any) error
	sendResponse(ctx context.Context, id json.RawMessage, result any, rpcErr *rpcErrorBody) error
}

// rpcResponse carries one inbound RPC result. err is a *rpcErrorBody when it
// came off the wire, or a local sentinel (errConnLost) when the connection
// died before the peer answered.
type rpcResponse struct {
	result json.RawMessage
	err    error
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcErrorBody) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

type wsFramer struct {
	ws      *websocket.Conn
	log     *slog.Logger
	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	nextID  atomic.Int64
}

func newWSFramer(ws *websocket.Conn, log *slog.Logger) *wsFramer {
	return &wsFramer{
		ws:      ws,
		log:     log,
		pending: make(map[int64]chan rpcResponse),
	}
}

func (f *wsFramer) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := f.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	f.mu.Lock()
	f.pending[id] = ch
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.pending, id)
		f.mu.Unlock()
	}()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if err := f.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sendNotification writes a JSON-RPC notification. ACP session/cancel is a
// notification, not a request: an agent that implements the protocol need not
// (and Goose does not) register a request handler or return a response.
func (f *wsFramer) sendNotification(ctx context.Context, method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	if err := f.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

// sendResponse writes a JSON-RPC 2.0 response for an agent-initiated request
// (e.g. session/request_permission). id is the original request id (number or
// string; goose uses UUID strings). Preserved as RawMessage so the wire form
// of the id is unchanged.
func (f *wsFramer) sendResponse(ctx context.Context, id json.RawMessage, result any, rpcErr *rpcErrorBody) error {
	envelope := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcErrorBody   `json:"error,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if err := f.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

func (f *wsFramer) deliverResponse(id int64, result json.RawMessage, rpcErr *rpcErrorBody) {
	f.mu.Lock()
	ch, ok := f.pending[id]
	f.mu.Unlock()
	if !ok {
		return
	}
	// A typed-nil *rpcErrorBody must not become a non-nil error interface.
	var err error
	if rpcErr != nil {
		err = rpcErr
	}
	select {
	case ch <- rpcResponse{result: result, err: err}:
	default:
	}
}

// failAll unblocks every in-flight RPC with errConnLost. Called when the read
// pump exits (connection death, orderly close): without it a prompt turn
// would wait on a reply that can never arrive — its context is deliberately
// cancel-proof — leaking the goroutine and pinning the session turn-busy
// forever.
func (f *wsFramer) failAll() {
	f.mu.Lock()
	pending := f.pending
	f.pending = make(map[int64]chan rpcResponse)
	f.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- rpcResponse{err: errConnLost}:
		default:
		}
	}
}

// wsMessage is one inbound JSON-RPC frame. ID is RawMessage because goose
// uses UUID strings for agent→client requests (session/request_permission)
// while client→agent requests use integer ids.
type wsMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcErrorBody   `json:"error,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// readPump demultiplexes the engine socket until it fails or is closed. fr is
// the framer of THIS connection generation: on exit it fails that
// generation's in-flight RPCs (a later generation's are untouched).
func (p *Provider) readPump(fr *wsFramer) {
	defer func() {
		fr.failAll()
		fr.ws.Close(websocket.StatusNormalClosure, "read pump done")
	}()
	for {
		_, data, err := fr.ws.Read(context.Background())
		if err != nil {
			p.handleWSError(err)
			return
		}
		p.wire.Frame(data)
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			p.log.Debug("ws: unparseable frame", slog.String("err", err.Error()))
			continue
		}
		hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
		// Response to one of our outbound RPCs: id + result/error, no method.
		if hasID && msg.Method == "" && (msg.Result != nil || msg.Error != nil) {
			var id int64
			if err := json.Unmarshal(msg.ID, &id); err == nil {
				fr.deliverResponse(id, msg.Result, msg.Error)
			}
			continue
		}
		if msg.Method != "" {
			if hasID {
				// Agent-initiated JSON-RPC request (must reply with same id).
				p.routeAgentRequest(msg.Method, msg.ID, msg.Params)
			} else {
				p.routeNotification(msg.Method, msg.Params, data)
			}
		}
	}
}
