package goose

import (
	"strconv"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
)

func TestStaticModes(t *testing.T) {
	if len(staticModes) == 0 {
		t.Fatal("staticModes must not be empty")
	}
	for i, m := range staticModes {
		if m.ID == "" {
			t.Errorf("staticModes[%d]: empty ID", i)
		}
		if m.Name == "" {
			t.Errorf("staticModes[%d] (%q): empty Name", i, m.ID)
		}
	}
}

func TestCommandTableEntries(t *testing.T) {
	if len(commandTable) == 0 {
		t.Fatal("commandTable must not be empty")
	}
	validKinds := map[command.Kind]bool{
		command.KindDaemon: true,
		command.KindNative: true,
		command.KindNone:   true,
		command.KindMode:   true,
		command.KindOp:     true,
	}
	for name, entry := range commandTable {
		if name == "" {
			t.Error("commandTable entry with empty name")
		}
		if !validKinds[entry.Kind] {
			t.Errorf("commandTable[%q]: invalid Kind %q", name, entry.Kind)
		}
	}
}

func TestTerminalOnlyCommandsAreNeverForwardedOverACP(t *testing.T) {
	for _, name := range []string{"compact", "goal"} {
		resolution, ok := command.Resolve(name, commandTable, command.SessionState{
			AgentCommands: []string{name},
		})
		if !ok || resolution.Available || resolution.Mapping.Kind != command.KindNone || resolution.Reason() == "" {
			t.Fatalf("/%s resolution = %#v, want unavailable explicit mapping", name, resolution)
		}
	}
}

func TestSpecFields(t *testing.T) {
	if spec.ID != provider.IDGoose {
		t.Errorf("spec.ID = %q, want %q", spec.ID, provider.IDGoose)
	}
	if spec.DefaultBin == "" {
		t.Error("spec.DefaultBin must not be empty")
	}
	if spec.HealthPath == "" {
		t.Error("spec.HealthPath must not be empty")
	}
	if spec.ServeArgs == nil {
		t.Fatal("spec.ServeArgs must not be nil")
	}
	if spec.DefaultModeID == "" {
		t.Error("spec.DefaultModeID must not be empty")
	}
	if spec.Commands == nil {
		t.Error("spec.Commands must not be nil")
	}
}

func TestServeArgs(t *testing.T) {
	args := spec.ServeArgs(8080)
	if len(args) == 0 {
		t.Fatal("ServeArgs(8080) returned empty")
	}
	host := false
	port := false
	for i, a := range args {
		if a == "--host" && i+1 < len(args) && args[i+1] == "127.0.0.1" {
			host = true
		}
		if a == "--port" && i+1 < len(args) && args[i+1] == "8080" {
			port = true
		}
	}
	if !host {
		t.Error("ServeArgs(8080): expected --host 127.0.0.1")
	}
	if !port {
		t.Error("ServeArgs(8080): expected --port 8080")
	}
}

func TestWithBuiltinsBecomeOnlyGooseBuiltinFlags(t *testing.T) {
	args := newSpec([]string{"developer", "computercontroller"}).ServeArgs(8080)
	want := []string{
		"serve", "--host", "127.0.0.1", "--port", "8080", "--dangerously-unauthenticated",
		"--with-builtin", "developer", "--with-builtin", "computercontroller",
	}
	if len(args) != len(want) {
		t.Fatalf("ServeArgs = %#v, want %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("ServeArgs[%d] = %q, want %q (all args %#v)", i, args[i], want[i], args)
		}
	}
}

func TestTypeAliasesCompile(t *testing.T) {
	var _ Config = Config{Config: acphttp.Config{}}
	var _ McpServer = McpServer{}
	var _ *Provider = &Provider{}
}

func TestNew(t *testing.T) {
	p := New(Config{Config: acphttp.Config{}})
	if p == nil {
		t.Fatal("New returned nil")
	}
	if p.ID() != provider.IDGoose {
		t.Errorf("p.ID() = %q, want %q", p.ID(), provider.IDGoose)
	}
}

func TestNewWithLogger(t *testing.T) {
	p := NewWithLogger(Config{Config: acphttp.Config{}}, nil)
	if p == nil {
		t.Fatal("NewWithLogger returned nil")
	}
}

func TestListModels(t *testing.T) {
	ctx := t.Context()
	cat, err := New(Config{Config: acphttp.Config{}}).ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if !cat.AllowCustom {
		t.Fatal("Goose's pre-session catalog must allow a configured model id")
	}
	if len(cat.Options) != 0 {
		t.Fatalf("pre-session catalog = %#v, want no stale static models", cat.Options)
	}
}

func TestServeArgsPortRoundTrip(t *testing.T) {
	for _, port := range []int{0, 1024, 65535, 9999} {
		args := spec.ServeArgs(port)
		found := false
		for i, a := range args {
			if a == "--port" && i+1 < len(args) {
				if args[i+1] == strconv.Itoa(port) {
					found = true
				}
			}
		}
		if port > 0 && !found {
			t.Errorf("ServeArgs(%d): expected --port %d", port, port)
		}
	}
}

func TestStaticModesDefault(t *testing.T) {
	if spec.DefaultModeID == "" {
		t.Fatal("spec.DefaultModeID must not be empty")
	}
	found := false
	for _, m := range staticModes {
		if m.ID == spec.DefaultModeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("spec.DefaultModeID %q not found in staticModes", spec.DefaultModeID)
	}
}
