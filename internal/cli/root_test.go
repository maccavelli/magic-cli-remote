package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewRootCmdHasExpectedSubcommands(t *testing.T) {
	cmd := newRootCmd()
	if cmd.Use != "mcremote" {
		t.Fatalf("Use = %q, want mcremote", cmd.Use)
	}
	subs := map[string]bool{}
	for _, c := range cmd.Commands() {
		subs[c.Name()] = true
	}
	for _, want := range []string{"serve", "version", "pair", "setup-service", "engines"} {
		if !subs[want] {
			t.Fatalf("missing subcommand %q", want)
		}
	}
}

func TestNewRootCmdHasPersistentFlags(t *testing.T) {
	cmd := newRootCmd()
	f := cmd.PersistentFlags()
	if f.Lookup("config") == nil {
		t.Fatal("missing --config persistent flag")
	}
	if f.Lookup("log-level") == nil {
		t.Fatal("missing --log-level persistent flag")
	}
	if f.Lookup("log-format") == nil {
		t.Fatal("missing --log-format persistent flag")
	}
}

func TestNewRootCmdHasSetupServiceFlag(t *testing.T) {
	cmd := newRootCmd()
	f := cmd.Flags()
	if f.Lookup("setup-service") == nil {
		t.Fatal("missing --setup-service flag")
	}
}

func TestNewRootCmdVersionTemplate(t *testing.T) {
	cmd := newRootCmd()
	if cmd.Version == "" {
		t.Fatal("version template should not be empty")
	}
}

func TestRootHelpIsDefault(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "Available Commands:") {
		t.Fatalf("root without args should print help, got:\n%s", out)
	}
}
