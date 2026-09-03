//go:build darwin

package updateclient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/selfupdate"
)

func TestNewCodesignTransformerNilWithoutIdentity(t *testing.T) {
	for _, id := range []string{"", "   ", "\t"} {
		if got := newCodesignTransformer(id); got != nil {
			t.Errorf("newCodesignTransformer(%q) = %v, want nil", id, got)
		}
	}
}

func TestCodesignSignsThenVerifies(t *testing.T) {
	var calls [][]string
	tr := &codesignTransformer{
		identity: "Developer ID",
		runner: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}
	req := selfupdate.TransformRequest{Product: "mcremote", Platform: selfupdate.Platform{OS: "darwin", Arch: "arm64"}, Path: "/tmp/staged"}
	if err := tr.Transform(context.Background(), req); err != nil {
		t.Fatalf("Transform = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("ran %d commands, want sign then verify: %v", len(calls), calls)
	}
	if got := strings.Join(calls[0], " "); got != "codesign --force --sign Developer ID /tmp/staged" {
		t.Errorf("sign call = %q", got)
	}
	if got := strings.Join(calls[1], " "); got != "codesign --verify --strict /tmp/staged" {
		t.Errorf("verify call = %q", got)
	}
}

// TestCodesignVerifyFailureIsFatal is the point of running verify at all: an
// unverifiable signature must fail the update rather than install a binary
// macOS will refuse to run.
func TestCodesignVerifyFailureIsFatal(t *testing.T) {
	boom := errors.New("code object is not signed at all")
	tr := &codesignTransformer{
		identity: "Developer ID",
		runner: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "--verify" {
				return []byte("bad signature"), boom
			}
			return nil, nil
		},
	}
	err := tr.Transform(context.Background(), selfupdate.TransformRequest{Path: "/tmp/staged"})
	if err == nil {
		t.Fatal("expected verify failure to be fatal")
	}
	if !strings.Contains(err.Error(), "bad signature") {
		t.Errorf("error %q should carry the tool diagnostic", err)
	}
}

func TestCodesignSignFailureIsFatal(t *testing.T) {
	tr := &codesignTransformer{
		identity: "Developer ID",
		runner: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, errors.New("no identity found")
		},
	}
	if err := tr.Transform(context.Background(), selfupdate.TransformRequest{Path: "/tmp/x"}); err == nil {
		t.Fatal("expected sign failure to be fatal")
	}
}

// TestCodesignRefusesForeignPlatform fixes the old behaviour of attempting
// codesign on any operating system whenever the variable was set.
func TestCodesignRefusesForeignPlatform(t *testing.T) {
	tr := &codesignTransformer{
		identity: "Developer ID",
		runner: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("ran codesign against a non-darwin binary")
			return nil, nil
		},
	}
	req := selfupdate.TransformRequest{Platform: selfupdate.Platform{OS: "linux", Arch: "amd64"}, Path: "/tmp/x"}
	if err := tr.Transform(context.Background(), req); err == nil {
		t.Fatal("expected a refusal for a linux binary")
	}
}
