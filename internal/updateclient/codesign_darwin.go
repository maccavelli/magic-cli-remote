//go:build darwin

package updateclient

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/maccavelli/mcplib/selfupdate"
)

// codesignTransformer re-signs a verified staged binary on macOS so the
// installed image keeps its TCC grants across an update (MADR 0069).
//
// This deliberately runs as a Transformer and not as part of verification. It
// modifies bytes that were already checksum-verified against the release, so
// the shared coordinator recomputes the installed digest afterwards and
// records that it intentionally differs from the release digest (MADR 0005 F8).
type codesignTransformer struct {
	identity string
	runner   func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var _ selfupdate.Transformer = (*codesignTransformer)(nil)

// newCodesignTransformer returns nil when no identity is configured, which
// leaves the shared no-op transform in place.
func newCodesignTransformer(identity string) selfupdate.Transformer {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	return &codesignTransformer{identity: identity}
}

// Transform signs the staged file and then verifies the signature it just
// made. An unverified signature is a failure, not a warning: the alternative
// is installing a binary that macOS will refuse to run.
func (t *codesignTransformer) Transform(ctx context.Context, req selfupdate.TransformRequest) error {
	if req.Platform.OS != "" && req.Platform.OS != "darwin" {
		return fmt.Errorf("codesign: refusing to sign a %s binary", req.Platform.OS)
	}
	if out, err := t.run(ctx, "codesign", "--force", "--sign", t.identity, req.Path); err != nil {
		return fmt.Errorf("codesign --sign %s: %w%s", t.identity, err, detail(out))
	}
	if out, err := t.run(ctx, "codesign", "--verify", "--strict", req.Path); err != nil {
		return fmt.Errorf("codesign --verify --strict: %w%s", err, detail(out))
	}
	return nil
}

func (t *codesignTransformer) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if t.runner != nil {
		return t.runner(ctx, name, args...)
	}
	// #nosec G204 — name is a constant and the path is the staging file this
	// process created and verified.
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func detail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
