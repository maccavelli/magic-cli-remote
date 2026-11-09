package gobble

import (
	"context"
	"errors"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// The phone's Rename and Fork, on gobble's own requests (MADR 0179 D9). Both
// hold gobble's turn, so gobble refuses each with -32600 during a prompt,
// and that refusal is the operation's error.

// rename names the session. gobble answers {} and sends session_info_update,
// which acpagent maps to the title.
func rename(ctx context.Context, h acpagent.Host, title string) error {
	return h.CallExtension(ctx, acpext.MethodSetSessionName,
		acpext.SetSessionNameParams{SessionID: h.AgentSessionID(), Name: title}, nil)
}

// fork copies the whole session into a new one, which the daemon then loads.
// gobble's clone takes no point to fork at, so LastTurnID is not used, as
// grok's fork does not use it; a fork before an earlier message is
// `/fork [entry]`, passed through.
func fork(ctx context.Context, h acpagent.Host, _ provider.ForkOptions) (provider.ForkResult, error) {
	source := h.AgentSessionID()
	var res acpext.CloneResult
	if err := h.CallExtension(ctx, acpext.MethodClone, acpext.SessionParams{SessionID: source}, &res); err != nil {
		return provider.ForkResult{}, err
	}
	if res.SessionID == "" {
		return provider.ForkResult{}, errors.New("gobble's clone named no session")
	}
	return provider.ForkResult{AgentSessionID: res.SessionID, ForkedFromID: source}, nil
}
