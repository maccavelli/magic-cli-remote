//go:build unix

package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// probeRoots points the XDG roots at scratch directories and the default
// configuration at one whose data_dir is defaultData. The runtime root is
// short: an admin socket's path must fit 104 bytes on macOS.
func probeRoots(t *testing.T) (defaultData string) {
	t.Helper()
	for _, env := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		dir, err := os.MkdirTemp("", "xdg")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(env, dir)
	}
	t.Setenv("MCREMOTE_DATA_DIR", "")
	defaultData = filepath.Join(t.TempDir(), "default-data")
	t.Setenv("MCREMOTE_CONFIG", writeProbeConfig(t, defaultData))
	return defaultData
}

// writeProbeConfig writes a private config.yaml naming dataDir.
func writeProbeConfig(t *testing.T, dataDir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("data_dir: "+dataDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// socketFor is the admin socket `serve --config <ConfigPath> --data-dir
// <DataDir>` serves, resolved here without the probe's own loader.
func socketFor(t *testing.T, args service.ServeArgs) string {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{ConfigFile: args.ConfigPath})
	if err != nil {
		t.Fatal(err)
	}
	if args.DataDir != "" {
		cfg.DataDir = args.DataDir
		if err := cfg.RecomputePaths(); err != nil {
			t.Fatal(err)
		}
	}
	return cfg.Paths.AdminSocket
}

// serveAdmin runs a real admin socket at sock until the test ends.
func serveAdmin(t *testing.T, sock string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- admin.Serve(ctx, sock, noDisconnector{}, nil, nil) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := admin.Call(sock, admin.Request{Op: admin.OpPing}); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the admin socket never answered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAdminPingProbeFollowsTheBakedDataDir: a service set up with its own
// --data-dir serves another socket than the default configuration names, and
// the probe pings that one (MADR 0054 amendment S8, S1).
func TestAdminPingProbeFollowsTheBakedDataDir(t *testing.T) {
	probeRoots(t)
	args := service.ServeArgs{DataDir: filepath.Join(t.TempDir(), "service-data")}
	if socketFor(t, args) == socketFor(t, service.ServeArgs{}) {
		t.Fatal("precondition: the baked data dir has the default's socket")
	}
	serveAdmin(t, socketFor(t, args))

	h, err := adminPingProbe(context.Background(), args)
	if err != nil || !h.Ready {
		t.Fatalf("probe = %+v, %v; want ready on the baked data dir's socket", h, err)
	}
	if h, err := adminPingProbe(context.Background(), service.ServeArgs{}); err == nil && h.Ready {
		t.Fatal("the default configuration's socket answered, but no daemon serves it")
	}
}

// TestAdminPingProbeFollowsTheBakedConfig: a service set up with its own
// --config, whose data_dir differs from the default's.
func TestAdminPingProbeFollowsTheBakedConfig(t *testing.T) {
	probeRoots(t)
	args := service.ServeArgs{ConfigPath: writeProbeConfig(t, filepath.Join(t.TempDir(), "config-data"))}
	if socketFor(t, args) == socketFor(t, service.ServeArgs{}) {
		t.Fatal("precondition: the baked config has the default's socket")
	}
	serveAdmin(t, socketFor(t, args))

	h, err := adminPingProbe(context.Background(), args)
	if err != nil || !h.Ready {
		t.Fatalf("probe = %+v, %v; want ready on the baked config's socket", h, err)
	}
}

// TestAdminPingProbeNotReadyWithoutADaemon: nothing on the socket is an
// error, which the backend keeps until the wait times out.
func TestAdminPingProbeNotReadyWithoutADaemon(t *testing.T) {
	probeRoots(t)
	h, err := adminPingProbe(context.Background(), service.ServeArgs{})
	if err == nil || h.Ready {
		t.Fatalf("probe = %+v, %v; want an error and not ready", h, err)
	}
}

// TestAdminPingProbeNotReadyOnANotOKAnswer: a socket that answers ping with
// OK false is not ready, though the call itself went through.
func TestAdminPingProbeNotReadyOnANotOKAnswer(t *testing.T) {
	probeRoots(t)
	sock := socketFor(t, service.ServeArgs{})
	if err := appdirs.EnsurePrivateDir(filepath.Dir(sock)); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req admin.Request
			_ = json.NewDecoder(conn).Decode(&req)
			_ = json.NewEncoder(conn).Encode(admin.Response{OK: false, Error: "draining"})
			_ = conn.Close()
		}
	}()

	h, err := adminPingProbe(context.Background(), service.ServeArgs{})
	if err != nil {
		t.Fatalf("probe error = %v, want an observation", err)
	}
	if h.Ready {
		t.Fatalf("probe = %+v, want not ready on OK: false", h)
	}
}
