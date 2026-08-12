package provider

import (
	"context"
	"errors"
	"strings"
)

// ReviewTarget is one of the four app-server review targets (MADR 0080 D19).
type ReviewTarget struct {
	// Kind is uncommittedChanges, baseBranch, commit, or custom.
	Kind         string
	Branch       string
	SHA          string
	Instructions string
}

const (
	// ReviewUncommitted reviews working-tree changes.
	ReviewUncommitted = "uncommittedChanges"
	// ReviewBaseBranch reviews against a base branch.
	ReviewBaseBranch = "baseBranch"
	// ReviewCommit reviews a commit SHA.
	ReviewCommit = "commit"
	// ReviewCustom reviews with custom instructions.
	ReviewCustom = "custom"
)

// ErrReviewInvalid means the /review arguments were missing or empty.
var ErrReviewInvalid = errors.New("review target invalid")

// ReviewSession starts an inline review turn.
type ReviewSession interface {
	Session
	StartReview(ctx context.Context, target ReviewTarget) error
}

// ParseReviewArg maps /review grammar onto a typed target.
// Bare and "uncommitted" are uncommittedChanges. Empty values after
// base/commit/custom are rejected locally.
func ParseReviewArg(arg string) (ReviewTarget, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.EqualFold(arg, "uncommitted") {
		return ReviewTarget{Kind: ReviewUncommitted}, nil
	}
	fields := strings.Fields(arg)
	verb := strings.ToLower(fields[0])
	rest := strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
	switch verb {
	case "base":
		if rest == "" {
			return ReviewTarget{}, ErrReviewInvalid
		}
		return ReviewTarget{Kind: ReviewBaseBranch, Branch: rest}, nil
	case "commit":
		if rest == "" {
			return ReviewTarget{}, ErrReviewInvalid
		}
		return ReviewTarget{Kind: ReviewCommit, SHA: rest}, nil
	case "custom":
		if rest == "" {
			return ReviewTarget{}, ErrReviewInvalid
		}
		return ReviewTarget{Kind: ReviewCustom, Instructions: rest}, nil
	default:
		return ReviewTarget{}, ErrReviewInvalid
	}
}
