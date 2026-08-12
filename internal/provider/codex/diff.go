package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	maxDiffBytes         = 256 << 10
	diffScopeWorkingTree = "working_tree"
	diffScopeLatestTurn  = "latest_codex_turn"
	diffTruncationNotice = "\n[diff truncated]\n"
)

var hexSHA = regexp.MustCompile(`(?i)^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

func clipDiff(s string) (string, bool) {
	if len(s) <= maxDiffBytes {
		return s, false
	}
	cut := s[:maxDiffBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		cut = cut[:i+1]
	}
	return strings.TrimRight(cut, "\n") + diffTruncationNotice, true
}

func (s *session) Diff(ctx context.Context, _ string) (provider.DiffResult, error) {
	s.mu.Lock()
	cwd := s.cwd
	gen := s.engineGeneration
	s.mu.Unlock()

	if s.p != nil {
		s.p.mu.Lock()
		unavailable := s.p.eng != nil && s.p.eng.diffUnavailable
		s.p.mu.Unlock()
		if unavailable {
			return s.diffFallback()
		}
	}

	fr := s.p.framer()
	if fr == nil {
		return provider.DiffResult{}, fmt.Errorf("engine not running")
	}
	raw, err := fr.sendRequest(ctx, "gitDiffToRemote", map[string]any{"cwd": cwd})
	if err != nil {
		var rpc *rpcErrorBody
		if errors.As(err, &rpc) && rpc != nil && rpc.Code == -32601 {
			s.latchDiffUnavailable(gen)
			return s.diffFallback()
		}
		return provider.DiffResult{}, err
	}
	var resp struct {
		SHA  string `json:"sha"`
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return provider.DiffResult{}, fmt.Errorf("gitDiffToRemote: decode: %w", err)
	}
	if resp.SHA != "" && !hexSHA.MatchString(resp.SHA) {
		return provider.DiffResult{}, fmt.Errorf("gitDiffToRemote: invalid sha")
	}
	body, truncated := clipDiff(resp.Diff)
	return provider.DiffResult{
		Summary:   body,
		BaseSHA:   resp.SHA,
		Scope:     diffScopeWorkingTree,
		Truncated: truncated,
	}, nil
}

func (s *session) latchDiffUnavailable(gen int) {
	if s.p == nil {
		return
	}
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	if s.p.eng != nil && s.p.eng.generation == gen {
		s.p.eng.diffUnavailable = true
	}
}

func (s *session) rememberTurnDiff(turnID, diff string) {
	if turnID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnDiffs == nil {
		s.turnDiffs = make(map[string]string)
	}
	s.turnDiffs[turnID] = diff
	s.lastTurnDiffID = turnID
}

func (s *session) diffFallback() (provider.DiffResult, error) {
	s.mu.Lock()
	patch := s.turnDiffs[s.lastTurnDiffID]
	s.mu.Unlock()
	if patch == "" {
		return provider.DiffResult{}, provider.ErrDiffUnavailable
	}
	body, truncated := clipDiff(patch)
	return provider.DiffResult{
		Summary:   body,
		Scope:     diffScopeLatestTurn,
		Truncated: truncated,
	}, nil
}
