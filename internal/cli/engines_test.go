package cli

import (
	"testing"
)

func TestNewEnginesCmdProperties(t *testing.T) {
	cmd := newEnginesCmd()
	if cmd.Use != "engines" {
		t.Fatalf("Use = %q, want engines", cmd.Use)
	}
	if cmd.Example == "" {
		t.Fatal("Example should not be empty")
	}
}

func TestNewEnginesCmdHasReapFlag(t *testing.T) {
	cmd := newEnginesCmd()
	f := cmd.Flags().Lookup("reap")
	if f == nil {
		t.Fatal("missing --reap flag")
	}
	if f.DefValue != "false" {
		t.Fatalf("--reap default = %q, want false", f.DefValue)
	}
}
