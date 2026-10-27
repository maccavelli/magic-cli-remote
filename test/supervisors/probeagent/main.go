// Command probeagent is the stand-in ACP agent of `make live-supervisors`
// (MADR 0212 D13-A; the 0210 PLAN's kit). The daemon under test starts it as
// its grok provider, over stdio. Each prompt "RUN <command>" runs the command
// through the client's terminal (terminal/create), as an agent such as grok
// does when the client serves the terminal, and answers with its exit status
// and output. So a test can make the daemon's own agent run `mcremote update`
// from inside the service it updates.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/maccavelli/acp-go-sdk2"
)

// agent answers the methods acp.Agent requires; everything but a session and
// a prompt is refused as not supported.
type agent struct {
	conn *acp.AgentSideConnection
}

var _ acp.Agent = (*agent)(nil)

// SetAgentConnection implements acp.AgentConnAware: NewAgentSideConnection
// hands the agent its connection before reading starts.
func (a *agent) SetAgentConnection(conn *acp.AgentSideConnection) { a.conn = conn }

// Initialize advertises the one headless-safe method Magic's grok spec
// authenticates with, so the service needs no credential (MADR 0085).
func (a *agent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AuthMethods:     []acp.AuthMethod{{Agent: &acp.AuthMethodAgent{Id: "cached_token", Name: "probe"}}},
	}, nil
}

// Authenticate accepts the advertised method.
func (a *agent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// NewSession opens a session; the agent keeps nothing per session.
func (a *agent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return acp.NewSessionResponse{}, err
	}
	return acp.NewSessionResponse{SessionId: acp.SessionId("sess_" + hex.EncodeToString(b[:]))}, nil
}

// Prompt runs "RUN <command>" through the client's terminal, waits for it to
// exit, and reports "probeagent: exit N" and its output. Any other prompt is
// answered with a usage line.
func (a *agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	sid := params.SessionId
	var text strings.Builder
	for _, b := range params.Prompt {
		if b.Text != nil {
			text.WriteString(b.Text.Text)
		}
	}
	say := func(s string) error {
		return a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: sid, Update: acp.UpdateAgentMessageText(s)})
	}
	end := acp.PromptResponse{StopReason: acp.StopReasonEndTurn}
	command, ok := strings.CutPrefix(strings.TrimSpace(text.String()), "RUN ")
	if !ok {
		return end, say("probeagent: send RUN <command>")
	}
	created, err := a.conn.CreateTerminal(ctx, acp.CreateTerminalRequest{SessionId: sid, Command: "/bin/sh", Args: []string{"-c", command}})
	if err != nil {
		return end, say("probeagent: terminal/create failed: " + err.Error())
	}
	exit, werr := a.conn.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{SessionId: sid, TerminalId: created.TerminalId})
	out, oerr := a.conn.TerminalOutput(ctx, acp.TerminalOutputRequest{SessionId: sid, TerminalId: created.TerminalId})
	_, _ = a.conn.ReleaseTerminal(ctx, acp.ReleaseTerminalRequest{SessionId: sid, TerminalId: created.TerminalId})
	status := "unknown"
	switch {
	case werr != nil:
		status = "wait failed: " + werr.Error()
	case exit.ExitCode != nil:
		status = fmt.Sprintf("exit %d", *exit.ExitCode)
	case exit.Signal != nil:
		status = "signal " + *exit.Signal
	}
	body := out.Output
	if oerr != nil {
		body = "output failed: " + oerr.Error()
	}
	return end, say(fmt.Sprintf("probeagent: %s\n%s", status, body))
}

// Cancel implements acp.Agent. The SDK cancels the running turn's context
// itself.
func (a *agent) Cancel(context.Context, acp.CancelNotification) error { return nil }

// Logout implements acp.Agent: not supported.
func (a *agent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}

// CloseSession implements acp.Agent: not supported.
func (a *agent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}

// DeleteSession implements acp.Agent: not supported.
func (a *agent) DeleteSession(context.Context, acp.DeleteSessionRequest) (acp.DeleteSessionResponse, error) {
	return acp.DeleteSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionDelete)
}

// ListSessions implements acp.Agent: not supported.
func (a *agent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}

// ResumeSession implements acp.Agent: not supported.
func (a *agent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}

// SetSessionConfigOption implements acp.Agent: not supported.
func (a *agent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}

// SetSessionMode implements acp.Agent: accepted, and ignored.
func (a *agent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func main() {
	// SIGINT ends the agent; the daemon otherwise ends it by closing stdio.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	conn := acp.NewAgentSideConnection(&agent{}, os.Stdout, os.Stdin)
	conn.SetLogger(slog.Default())
	select {
	case <-conn.Done():
	case <-ctx.Done():
	}
}
