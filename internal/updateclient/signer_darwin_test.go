//go:build darwin

package updateclient

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/codesign"
	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// TestSignerSignsUnderTheFixedIdentifier runs the library signer with this
// product's options. It signs with /usr/bin/codesign, never one found on
// PATH, under com.magiccliremote.<product>, and then requires the signature
// to name that identifier: the two defects of the old transformer (MADR 0054).
func TestSignerSignsUnderTheFixedIdentifier(t *testing.T) {
	var runs []libservice.Command
	o := signOptions("mcremote", "Developer ID")
	o.Runner = libservice.RunnerFunc(func(_ context.Context, c libservice.Command) (libservice.Output, error) {
		runs = append(runs, c)
		return libservice.Output{}, nil
	})
	tr, err := codesign.NewSigner(o)
	if err != nil {
		t.Fatal(err)
	}
	req := selfupdate.TransformRequest{Product: "mcremote", Platform: selfupdate.Platform{OS: "darwin", Arch: "arm64"}, Path: "/tmp/staged"}
	if err := tr.Transform(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(runs) < 2 {
		t.Fatalf("ran %d commands, want a sign and a verify: %+v", len(runs), runs)
	}
	for _, r := range runs {
		if r.Path != "/usr/bin/codesign" {
			t.Errorf("ran %q, want /usr/bin/codesign", r.Path)
		}
	}
	sign := strings.Join(runs[0].Args, " ")
	if !strings.Contains(sign, "--sign Developer ID --identifier com.magiccliremote.mcremote") {
		t.Errorf("sign = %q", sign)
	}
	verified := false
	for _, r := range runs[1:] {
		verified = verified || slices.Contains(r.Args, `-R=identifier "com.magiccliremote.mcremote"`)
	}
	if !verified {
		t.Errorf("no verify required the identifier: %+v", runs[1:])
	}
}
