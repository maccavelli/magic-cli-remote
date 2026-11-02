package acpagent

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var _ provider.UndoSession = (*session)(nil)

// UndoLast implements [provider.UndoSession] through the Spec's Ops.UndoLast.
func (s *session) UndoLast(ctx context.Context) (string, error) {
	if s.ops.UndoLast == nil {
		return "", errNotOffered("undo")
	}
	return s.ops.UndoLast(ctx, s)
}
