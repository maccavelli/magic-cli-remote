package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

type noDisconnector struct{}

func (noDisconnector) DisconnectDevice(string) int    { return 0 }
func (noDisconnector) DisconnectDevices([]string) int { return 0 }

// pairCodeStderr runs `pair code` (the config comes from MCREMOTE_CONFIG,
// which names cfgPath) and returns what it wrote to stderr.
func pairCodeStderr(t *testing.T, cfgPath string) string {
	t.Helper()
	cmd := newPairCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"code", "--qr=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pair code: %v\n%s%s", err, out.String(), errOut.String())
	}
	return errOut.String()
}

// TestPairCodeWarnsWhenNoDaemonServesItsDataDir is MADR 0201 D2 (0073
// F3(a)). A code written to one data directory while the daemon on the
// listen port serves another is rejected on the phone with nothing on the
// host to say why. The CLI now says so when it writes the code.
func TestPairCodeWarnsWhenNoDaemonServesItsDataDir(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := fmt.Sprintf("data_dir: %s\nlisten:\n  host: 127.0.0.1\n  port: %d\n", filepath.Join(dir, "data"), port)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)
	t.Setenv("MCREMOTE_DATA_DIR", "")
	t.Setenv("MCREMOTE_PAIR_HOST", "100.64.0.1:7531")

	const warning = "no daemon serves this data directory"
	t.Run("a daemon on the port, none on this data dir", func(t *testing.T) {
		if got := pairCodeStderr(t, cfgPath); !strings.Contains(got, warning) {
			t.Fatalf("no warning; stderr:\n%s", got)
		}
	})
	t.Run("a daemon serving this data dir", func(t *testing.T) {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: cfgPath})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		served := make(chan error, 1)
		go func() { served <- admin.Serve(ctx, cfg.Paths.AdminSocket, noDisconnector{}, nil, nil) }()
		t.Cleanup(func() {
			cancel()
			<-served
		})
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := admin.Call(cfg.Paths.AdminSocket, admin.Request{Op: admin.OpPing}); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the fake admin socket never answered")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := pairCodeStderr(t, cfgPath); strings.Contains(got, warning) {
			t.Fatalf("warned although a daemon serves this data dir; stderr:\n%s", got)
		}
	})
}
