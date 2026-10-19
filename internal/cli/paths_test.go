package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRelativeDataDirFlagResolvesAgainstCWD is MADR 0201 D24 (0073 §6.5).
// config.Load already makes --data-dir absolute against the working
// directory, but serve and paths then assigned the raw flag value again, so
// a relative --data-dir failed in appdirs.WithDataDir ("must be absolute").
func TestRelativeDataDirFlagResolvesAgainstCWD(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("log:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_CONFIG", cfgPath)
	t.Setenv("MCREMOTE_DATA_DIR", "")
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("rel", "x")
	want := filepath.Join(cwd, rel)

	t.Run("paths", func(t *testing.T) {
		cmd := newPathsCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--data-dir", rel})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("paths --data-dir %s: %v\n%s", rel, err, out.String())
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if v, ok := strings.CutPrefix(line, "data_dir:"); ok {
				if got := strings.TrimSpace(v); got != want {
					t.Fatalf("paths reports data_dir %q, want %q", got, want)
				}
				return
			}
		}
		t.Fatalf("no data_dir line in:\n%s", out.String())
	})
	t.Run("serve", func(t *testing.T) {
		cmd := newServeCmd()
		if err := cmd.ParseFlags([]string{"--data-dir", rel}); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadServeConfig(cmd)
		if err != nil {
			t.Fatalf("serve --data-dir %s: %v", rel, err)
		}
		if cfg.DataDir != want {
			t.Fatalf("serve would run with data_dir %q, want %q", cfg.DataDir, want)
		}
	})
}
