package acphttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/coder/websocket"
)

type acpConn struct {
	connID  string
	baseURL string
	httpc   *http.Client
	cfg     Config
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

func newACPConn(baseURL string, cfg Config) *acpConn {
	return &acpConn{
		httpc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:    2,
				IdleConnTimeout: 30 * time.Second,
			},
		},
		baseURL: baseURL,
		cfg:     cfg,
	}
}

func (c *acpConn) postJSON(ctx context.Context, method string, params any) (json.RawMessage, error) {
	body, _ := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/acp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.connID != "" {
		req.Header.Set("Acp-Connection-Id", c.connID)
	}
	res, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /acp %s: %w", method, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	// Extract connection-id from initialize response header
	if method == "initialize" {
		if id := res.Header.Get("Acp-Connection-Id"); id != "" {
			c.connID = id
		}
	}

	var rpc jsonRPCResponse
	if err := json.Unmarshal(data, &rpc); err != nil {
		return nil, fmt.Errorf("POST /acp %s: decode: %w", method, err)
	}
	if rpc.Error != nil {
		return nil, rpc.Error
	}
	return rpc.Result, nil
}

func (c *acpConn) initialize(ctx context.Context) (*acp.AgentCapabilities, error) {
	params := acp.InitializeRequest{
		ProtocolVersion: 1,
		ClientInfo: &acp.Implementation{
			Name:    "mcremote",
			Version: "dev",
		},
	}
	raw, err := c.postJSON(ctx, "initialize", params)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var resp acp.InitializeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("initialize: decode: %w", err)
	}
	if c.connID == "" {
		return nil, fmt.Errorf("initialize: no Acp-Connection-Id in response")
	}
	if len(resp.AuthMethods) > 0 && strings.TrimSpace(c.cfg.AuthMethodID) != "" {
		if err := c.authenticate(ctx); err != nil {
			return nil, err
		}
	}
	return &resp.AgentCapabilities, nil
}

func (c *acpConn) authenticate(ctx context.Context) error {
	params := map[string]any{
		"methodId": c.cfg.AuthMethodID,
	}
	_, err := c.postJSON(ctx, "authenticate", params)
	return err
}

// maxEngineFrameBytes bounds one inbound engine WebSocket frame.
// coder/websocket's default read limit is 32 KiB (read.go defaultReadLimit =
// 32768) and exceeding it closes the connection with StatusMessageTooBig.
// Goose tool_call/tool_call_update updates routinely carry file reads and
// command output well past 32 KiB, so the default turned one large tool
// result into a dropped engine connection ("engine lost") for every session.
// 8 MiB is comfortably above the largest payload surfaced in chat while still
// bounding a misbehaving peer.
const maxEngineFrameBytes = 8 << 20

func (c *acpConn) dialWS(ctx context.Context) (*websocket.Conn, error) {
	wsURL := "ws://" + strings.TrimPrefix(c.baseURL, "http://") + "/acp"
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Acp-Connection-Id": {c.connID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	ws.SetReadLimit(maxEngineFrameBytes)
	return ws, nil
}
