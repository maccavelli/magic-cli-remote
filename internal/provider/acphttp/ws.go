package acphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type rpcPending struct {
	ch    chan rpcResponse
	timer *time.Timer
}

type rpcResponse struct {
	result json.RawMessage
	rpcErr *rpcErrorBody
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
		if res.rpcErr != nil {
			return nil, res.rpcErr
		}
		return res.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	if ok {
		select {
		case ch <- rpcResponse{result: result, rpcErr: rpcErr}:
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

func (p *Provider) readPump() {
	defer func() {
		if p.ws != nil {
			p.ws.Close(websocket.StatusNormalClosure, "read pump done")
		}
	}()
	for {
		_, data, err := p.ws.Read(context.Background())
		if err != nil {
			p.handleWSError(err)
			return
		}
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
				p.wsFramer.deliverResponse(id, msg.Result, msg.Error)
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
