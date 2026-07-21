// Package admin provides a local Unix-socket control plane for the running
// mcremote daemon (revoke kick, etc.). Auth is filesystem permissions only:
// the socket is created mode 0600 under the daemon data dir.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SocketName is the basename under the daemon data dir.
	SocketName = "admin.sock"

	// DefaultDialTimeout bounds CLI → daemon connect/request.
	DefaultDialTimeout = 2 * time.Second
)

// ErrDaemonNotRunning is returned when the admin socket is missing or unreachable.
var ErrDaemonNotRunning = errors.New("mcremote daemon not running (admin socket unavailable)")

// Op names on the JSON request line.
const (
	OpPing              = "ping"
	OpDisconnectDevice  = "disconnect_device"
	OpDisconnectDevices = "disconnect_devices"
)

// Request is a single admin command.
type Request struct {
	Op        string   `json:"op"`
	DeviceID  string   `json:"device_id,omitempty"`
	DeviceIDs []string `json:"device_ids,omitempty"`
}

// Response is the reply to a Request.
type Response struct {
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	Disconnected int    `json:"disconnected,omitempty"`
}

// Disconnector is implemented by the WS server (kick live sockets after revoke).
type Disconnector interface {
	DisconnectDevice(deviceID string) int
	DisconnectDevices(deviceIDs []string) int
}

// SocketPath returns dataDir/admin.sock.
func SocketPath(dataDir string) string {
	return filepath.Join(dataDir, SocketName)
}

// Serve listens on the admin Unix socket until ctx is cancelled.
// It removes any stale socket file before binding.
func Serve(ctx context.Context, dataDir string, d Disconnector, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "admin"))
	if d == nil {
		return fmt.Errorf("admin: nil disconnector")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("admin: data dir: %w", err)
	}
	path := SocketPath(dataDir)
	// Only clear a *stale* socket: if a daemon answers a ping on it, binding
	// here would silently steal its control plane instead of failing loudly.
	if _, err := os.Stat(path); err == nil {
		if resp, err := Call(dataDir, Request{Op: OpPing}); err == nil && resp.OK {
			return fmt.Errorf("admin: another daemon is already serving %s", path)
		}
		_ = os.Remove(path)
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("admin: listen %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("admin: chmod %s: %w", path, err)
	}
	log.Info("admin socket listening", slog.String("path", path))

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go handleConn(conn, d, log)
	}
}

func handleConn(conn net.Conn, d Disconnector, log *slog.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(DefaultDialTimeout))

	var req Request
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "bad_json: " + err.Error()})
		return
	}
	resp := dispatch(req, d)
	if !resp.OK {
		log.Debug("admin request failed",
			slog.String("op", req.Op),
			slog.String("error", resp.Error),
		)
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func dispatch(req Request, d Disconnector) Response {
	switch strings.ToLower(strings.TrimSpace(req.Op)) {
	case OpPing:
		return Response{OK: true}
	case OpDisconnectDevice:
		id := strings.TrimSpace(req.DeviceID)
		if id == "" {
			return Response{OK: false, Error: "device_id required"}
		}
		n := d.DisconnectDevice(id)
		return Response{OK: true, Disconnected: n}
	case OpDisconnectDevices:
		ids := req.DeviceIDs
		if len(ids) == 0 {
			if id := strings.TrimSpace(req.DeviceID); id != "" {
				ids = []string{id}
			}
		}
		if len(ids) == 0 {
			return Response{OK: false, Error: "device_ids required"}
		}
		n := d.DisconnectDevices(ids)
		return Response{OK: true, Disconnected: n}
	default:
		return Response{OK: false, Error: "unknown op: " + req.Op}
	}
}

// Call dials the admin socket and runs one request.
func Call(dataDir string, req Request) (Response, error) {
	path := SocketPath(dataDir)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return Response{}, ErrDaemonNotRunning
		}
		return Response{}, err
	}
	dialer := net.Dialer{Timeout: DefaultDialTimeout}
	conn, err := dialer.Dial("unix", path)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrDaemonNotRunning, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(DefaultDialTimeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("admin: %s", resp.Error)
	}
	return resp, nil
}

// NotifyDisconnect asks a running daemon to kick the given device ids.
// Missing daemon is not an error; returns (0, ErrDaemonNotRunning).
func NotifyDisconnect(dataDir string, deviceIDs ...string) (disconnected int, err error) {
	ids := make([]string, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	resp, err := Call(dataDir, Request{Op: OpDisconnectDevices, DeviceIDs: ids})
	if err != nil {
		return 0, err
	}
	return resp.Disconnected, nil
}
