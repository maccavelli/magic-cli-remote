// Package admin provides a local Unix-socket control plane for the running
// mcremote daemon (revoke kick, etc.). Auth is filesystem permissions only:
// the socket is restricted to the owning user under the instance RuntimeDir
// (MADR 0059) — mode 0600 on Unix, an owner-only DACL on Windows, where
// os.Chmod grants no access control at all (MADR 0116 D7).
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
	"runtime/debug"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

const (
	// SocketName is the basename under the instance runtime dir.
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

// SocketPath returns runtimeDir/admin.sock.
// Prefer appdirs.Paths.AdminSocket when a full layout is available.
func SocketPath(runtimeDir string) string {
	return filepath.Join(runtimeDir, SocketName)
}

// Serve listens on the admin Unix socket at socketPath until ctx is cancelled.
// It validates the parent runtime directory and removes only a verified stale
// socket before binding.
func Serve(ctx context.Context, socketPath string, d Disconnector, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	log = log.With(slog.String("component", "admin"))
	if d == nil {
		return fmt.Errorf("admin: nil disconnector")
	}
	if socketPath == "" {
		return fmt.Errorf("admin: empty socket path")
	}
	if err := appdirs.CheckSocketPathLength(socketPath); err != nil {
		return err
	}
	runtimeDir := filepath.Dir(socketPath)
	if err := appdirs.EnsurePrivateDir(runtimeDir); err != nil {
		return fmt.Errorf("admin: runtime dir: %w", err)
	}
	if err := appdirs.ValidateRuntimeDir(runtimeDir); err != nil {
		return fmt.Errorf("admin: runtime dir: %w", err)
	}

	// Only clear a *stale* socket: if a daemon answers a ping on it, binding
	// here would silently steal its control plane instead of failing loudly.
	if fi, err := os.Lstat(socketPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("admin: %s exists and is not a socket", socketPath)
		}
		// Fail closed: an un-inspectable owner used to skip this check
		// entirely (`if ok && ...`), which silently accepted a socket whose
		// ownership could not be established (MADR 0116 D7/C3).
		owned, oerr := ownedByCurrentUser(socketPath, fi)
		if oerr != nil {
			return fmt.Errorf("admin: %s: %w", socketPath, oerr)
		}
		if !owned {
			return fmt.Errorf("admin: %s not owned by current user", socketPath)
		}
		if resp, err := Call(socketPath, Request{Op: OpPing}); err == nil && resp.OK {
			return fmt.Errorf("admin: another daemon is already serving %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return fmt.Errorf("admin: remove stale socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("admin: lstat: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("admin: listen %s: %w", socketPath, err)
	}
	// Restrict the socket to the owning user, or do not serve one at all.
	if err := secureSocket(socketPath); err != nil {
		_ = ln.Close()
		return fmt.Errorf("admin: secure %s: %w", socketPath, err)
	}
	// Capture inode for safe shutdown removal.
	var sockInode uint64
	if fi, err := os.Lstat(socketPath); err == nil {
		if id, ok := socketIdentity(fi); ok {
			sockInode = id
		}
	}
	log.Info("admin socket listening", slog.String("path", socketPath))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("admin listener closer panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		<-ctx.Done()
		_ = ln.Close()
		// Remove only if path still names this listener's socket inode.
		if fi, err := os.Lstat(socketPath); err == nil {
			if id, ok := socketIdentity(fi); ok && sockInode != 0 && id == sockInode {
				_ = os.Remove(socketPath)
			}
		}
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
	defer func() {
		if r := recover(); r != nil {
			log.Error("admin handleConn panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
		}
	}()
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
func Call(socketPath string, req Request) (Response, error) {
	if socketPath == "" {
		return Response{}, ErrDaemonNotRunning
	}
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return Response{}, ErrDaemonNotRunning
		}
		return Response{}, err
	}
	dialer := net.Dialer{Timeout: DefaultDialTimeout}
	conn, err := dialer.Dial("unix", socketPath)
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
func NotifyDisconnect(socketPath string, deviceIDs ...string) (disconnected int, err error) {
	ids := make([]string, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	resp, err := Call(socketPath, Request{Op: OpDisconnectDevices, DeviceIDs: ids})
	if err != nil {
		return 0, err
	}
	return resp.Disconnected, nil
}
