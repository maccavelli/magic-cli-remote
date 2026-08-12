package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

var errConnLost = errors.New("engine connection lost")

type rpcErrorBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcErrorBody) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type conn struct {
	stdin  io.Writer
	stdout io.Reader
	log    *slog.Logger

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan rpcResponse

	closed chan struct{}
}

func newConn(stdin io.Writer, stdout io.Reader, log *slog.Logger) *conn {
	return &conn{
		stdin:   stdin,
		stdout:  stdout,
		log:     log,
		pending: make(map[int64]chan rpcResponse),
		closed:  make(chan struct{}),
	}
}

func (c *conn) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	msg := map[string]any{
		"method": method,
		"id":     id,
		"params": params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if err := c.writeLine(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errConnLost
	}
}

func (c *conn) sendNotification(ctx context.Context, method string, params any) error {
	msg := map[string]any{
		"method": method,
		"params": params,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	return c.writeLine(data)
}

func (c *conn) sendResponse(ctx context.Context, id json.RawMessage, result any, rpcErr *rpcErrorBody) error {
	msg := map[string]any{
		"id": id,
	}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	return c.writeLine(data)
}

func (c *conn) deliverResponse(id int64, result json.RawMessage, rpcErr *rpcErrorBody) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	c.pendingMu.Unlock()
	if !ok {
		return
	}
	var err error
	if rpcErr != nil {
		err = rpcErr
	}
	select {
	case ch <- rpcResponse{result: result, err: err}:
	default:
	}
}

func (c *conn) failAll() {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan rpcResponse)
	c.pendingMu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- rpcResponse{err: errConnLost}:
		default:
		}
	}
}

func (c *conn) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(append(data, '\n'))
	return err
}

type wireMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcErrorBody   `json:"error,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

type notificationHandler func(method string, params json.RawMessage)
type serverRequestHandler func(method string, id json.RawMessage, params json.RawMessage)

func (c *conn) readPump(onNotif notificationHandler, onRequest serverRequestHandler) {
	defer c.failAll()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg wireMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.log.Debug("codex: unparseable frame", slog.String("err", err.Error()))
			continue
		}
		hasID := len(msg.ID) > 0 && string(msg.ID) != "null"
		if hasID && msg.Method == "" && (msg.Result != nil || msg.Error != nil) {
			var id int64
			if err := json.Unmarshal(msg.ID, &id); err == nil {
				c.deliverResponse(id, msg.Result, msg.Error)
			}
			continue
		}
		if msg.Method != "" {
			if hasID {
				onRequest(msg.Method, msg.ID, msg.Params)
			} else {
				onNotif(msg.Method, msg.Params)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		c.log.Debug("codex: read pump error", slog.String("err", err.Error()))
	}
}
