package cli

import (
	"testing"
)

func TestNewServeCmdHasExpectedFlags(t *testing.T) {
	cmd := newServeCmd()
	if cmd.Use != "serve" {
		t.Fatalf("Use = %q, want serve", cmd.Use)
	}
	f := cmd.Flags()
	cases := []struct {
		name string
		def  string
		typ  string // "string", "int", "bool"
	}{
		{"listen-host", "", "string"},
		{"listen-port", "0", "int"},
		{"data-dir", "", "string"},
		{"tls", "true", "bool"},
		{"tls-mode", "", "string"},
		{"tls-domain", "[]", "stringSlice"},
		{"tls-email", "", "string"},
		{"tls-acme-directory", "", "string"},
		{"tls-acme-staging", "false", "bool"},
		{"tls-route53-zone-id", "", "string"},
		{"tls-route53-region", "", "string"},
		{"tls-route53-profile", "", "string"},
		{"relay-url", "", "string"},
		{"relay-host-id", "", "string"},
		{"relay-secret", "", "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fl := f.Lookup(tc.name)
			if fl == nil {
				t.Fatalf("missing --%s flag", tc.name)
			}
			if fl.DefValue != tc.def {
				t.Fatalf("--%s default = %q, want %q", tc.name, fl.DefValue, tc.def)
			}
		})
	}
}

func TestNewServeCmdNoArgs(t *testing.T) {
	cmd := newServeCmd()
	if cmd.Args == nil {
		t.Fatal("Args must not be nil")
	}
}

func TestNewServeCmdIsSubcommand(t *testing.T) {
	// Verify the serve command is added to the root tree.
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c == newServeCmd() || c.Name() == "serve" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("serve is not a subcommand of root")
	}
}
