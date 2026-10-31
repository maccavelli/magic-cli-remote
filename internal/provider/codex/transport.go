package codex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// maxTransportFrameBytes bounds one Codex message, in either direction, on
// every transport. It is acp-go-sdk2's defaultMaxMessageSize, the bound
// Magic's ACP providers already read under (MADR 0223 D1).
const maxTransportFrameBytes = 10 << 20

// jsonlReadBufferSize is the JSONL reader's buffer, acp-go-sdk2's
// readBufferSize: a line longer than it grows in Read, up to
// maxTransportFrameBytes, so memory follows the line (MADR 0223 D2).
const jsonlReadBufferSize = 128 << 10

type transport interface {
	Send(context.Context, []byte) error
	Read(context.Context) ([]byte, error)
	Close() error
}

type jsonlTransport struct {
	reader  *bufio.Reader
	writer  io.Writer
	readMu  sync.Mutex
	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool
	closers []io.Closer
}

func newJSONLTransport(writer io.Writer, reader io.Reader) transport {
	t := &jsonlTransport{
		reader: bufio.NewReaderSize(reader, jsonlReadBufferSize),
		writer: writer,
	}
	seen := make(map[io.Closer]struct{})
	for _, value := range []any{writer, reader} {
		if closer, ok := value.(io.Closer); ok {
			if _, duplicate := seen[closer]; !duplicate {
				t.closers = append(t.closers, closer)
				seen[closer] = struct{}{}
			}
		}
	}
	return t
}

func (t *jsonlTransport) Send(_ context.Context, frame []byte) error {
	if len(frame) > maxTransportFrameBytes {
		return fmt.Errorf("codex transport frame exceeds %d bytes", maxTransportFrameBytes)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.closeMu.Lock()
	closed := t.closed
	t.closeMu.Unlock()
	if closed {
		return net.ErrClosed
	}
	_, err := t.writer.Write(append(append([]byte(nil), frame...), '\n'))
	return err
}

func (t *jsonlTransport) Read(_ context.Context) ([]byte, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	// As acp-go-sdk2's readLine: append chunks to the newline, and stop
	// keeping them once the line passes the bound, still reading through its
	// newline so the next Read starts on the next line.
	var line []byte
	tooLong := false
	for {
		chunk, err := t.reader.ReadSlice('\n')
		if !tooLong {
			line = append(line, chunk...)
			if len(bytes.TrimSuffix(line, []byte("\n"))) > maxTransportFrameBytes {
				line, tooLong = nil, true
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if tooLong {
			return nil, fmt.Errorf("codex transport frame exceeds %d bytes", maxTransportFrameBytes)
		}
		if err != nil {
			return nil, err
		}
		line = line[:len(line)-1]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return line, nil
	}
}

func (t *jsonlTransport) Close() error {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return nil
	}
	t.closed = true
	closers := append([]io.Closer(nil), t.closers...)
	t.closeMu.Unlock()
	var result error
	for _, closer := range closers {
		result = errors.Join(result, closer.Close())
	}
	return result
}

type websocketTransport struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func dialWebSocketTransport(ctx context.Context, url string, client *http.Client) (transport, error) {
	return dialWebSocketTransportWithHeaders(ctx, url, client, nil)
}

func dialWebSocketTransportWithHeaders(ctx context.Context, url string, client *http.Client, headers http.Header) (transport, error) {
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(maxTransportFrameBytes)
	return &websocketTransport{conn: conn}, nil
}

// pipeConn lets coder/websocket perform its normal HTTP Upgrade over the raw
// byte stream exposed by `codex app-server proxy`. The proxy owns framing only
// at the WebSocket layer; JSON-RPC remains in conn.
type pipeConn struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func (c *pipeConn) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c *pipeConn) Write(p []byte) (int, error)      { return c.writer.Write(p) }
func (c *pipeConn) LocalAddr() net.Addr              { return pipeAddr("mcremote") }
func (c *pipeConn) RemoteAddr() net.Addr             { return pipeAddr("codex-daemon") }
func (c *pipeConn) SetDeadline(time.Time) error      { return nil }
func (c *pipeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *pipeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *pipeConn) Close() error                     { return errors.Join(c.writer.Close(), c.reader.Close()) }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func dialPipeWebSocketTransport(ctx context.Context, reader io.ReadCloser, writer io.WriteCloser) (transport, error) {
	stream := &pipeConn{reader: reader, writer: writer}
	var once sync.Once
	httpTransport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		var conn net.Conn
		once.Do(func() { conn = stream })
		if conn == nil {
			return nil, fmt.Errorf("codex daemon proxy accepts one connection")
		}
		return conn, nil
	}}
	client := &http.Client{Transport: httpTransport}
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	conn.SetReadLimit(maxTransportFrameBytes)
	return &websocketTransport{conn: conn}, nil
}

func dialUnixWebSocketTransport(ctx context.Context, socket string, headers http.Header) (transport, error) {
	dialer := &net.Dialer{}
	httpTransport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	client := &http.Client{Transport: httpTransport}
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	if err != nil {
		httpTransport.CloseIdleConnections()
		return nil, err
	}
	conn.SetReadLimit(maxTransportFrameBytes)
	return &websocketTransport{conn: conn}, nil
}

func (t *websocketTransport) Send(ctx context.Context, frame []byte) error {
	if len(frame) > maxTransportFrameBytes {
		return fmt.Errorf("codex transport frame exceeds %d bytes", maxTransportFrameBytes)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	return t.conn.Write(ctx, websocket.MessageText, frame)
}

func (t *websocketTransport) Read(ctx context.Context) ([]byte, error) {
	typ, frame, err := t.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, fmt.Errorf("codex transport requires text frames")
	}
	return frame, nil
}

func (t *websocketTransport) Close() error {
	return t.conn.CloseNow()
}
