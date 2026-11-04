package opencode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// Undo, redo and the `!` shell of an ACP session, through the control plane
// (0214 MADR D11, as amended at B7 and at P6e):
//   - /undo stages a revert at the newest user message not already undone,
//     which restores the files the turns after it changed, in a git
//     repository; a second /undo reaches one turn further;
//   - /redo clears the staged revert, and the files come back;
//   - the next prompt makes the undo permanent: before it, Magic commits the
//     revert itself and removes the undone turns from its transcript, as a
//     count of turns, since their rows share no native id;
//   - the shell runs one command and shows it as a bash tool call, with the
//     output its message records.
//
// The control plane's busy check is its own process's, which never runs the
// engine's turns, so each operation is refused while a turn runs here.

// acpRevertTimeout bounds a revert operation or a message read.
const acpRevertTimeout = 30 * time.Second

// acpShellOutputMax is how much of a shell command's output is shown; the
// session manager cuts it further for the phone and keeps the rest for
// session.tool_output (MADR 0208 D11).
const acpShellOutputMax = 1 << 20

// errACPBusy refuses an operation while a turn runs.
var errACPBusy = fmt.Errorf("%w: OpenCode can undo, redo or run a command only between turns", provider.ErrTurnBusy)

// errACPAcrossCompaction refuses an undo that would reach past a compaction:
// the engine keeps a compacted turn without a user message, so its count of
// turns and Magic's would differ.
var errACPAcrossCompaction = errors.New("OpenCode can't undo past a compaction of this session")

// acpUndo is a session's staged undo: how many turns it spans.
type acpUndo struct {
	mu    sync.Mutex
	turns int
}

// newACPUndo is the Spec's NewSessionState.
func newACPUndo() any { return &acpUndo{} }

func undoOf(h acpagent.Host) (*acpUndo, error) {
	st, ok := h.SpecState().(*acpUndo)
	if !ok {
		return nil, errors.New("opencode: the session has no undo state")
	}
	return st, nil
}

// sessionMessage is a message as the list route gives it, as far as undo
// needs it.
type sessionMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
}

// do sends one request in the session's directory and decodes a 2xx answer.
func (s *acpSurfaces) do(ctx context.Context, h acpagent.Host, req control.Request, out any) error {
	req.Directory = h.CWD()
	res, err := s.plane.Do(ctx, req)
	if err != nil {
		return err
	}
	if res.Status < 200 || res.Status > 299 {
		var b struct {
			Tag string `json:"_tag"`
		}
		_ = json.Unmarshal(res.Body, &b)
		return &engineError{method: req.Method, path: req.Path, status: res.Status, tag: b.Tag}
	}
	if out == nil || len(res.Body) == 0 {
		return nil
	}
	if err := json.Unmarshal(res.Body, out); err != nil {
		return fmt.Errorf("opencode %s %s: the answer could not be decoded", req.Method, req.Path)
	}
	return nil
}

// sessionPath is a route under the session.
func sessionPath(sid, rest string) string { return "/api/session/" + url.PathEscape(sid) + rest }

// newestMessages lists up to limit of the session's messages of one type,
// newest first.
func (s *acpSurfaces) newestMessages(ctx context.Context, h acpagent.Host, typ string, limit int) ([]sessionMessage, error) {
	var out struct {
		Data []sessionMessage `json:"data"`
	}
	q := url.Values{"type": {typ}, "order": {"desc"}, "limit": {strconv.Itoa(limit)}}
	err := s.do(ctx, h, control.Request{Method: http.MethodGet, Path: sessionPath(h.AgentSessionID(), "/message"), Query: q}, &out)
	return out.Data, err
}

// stage stages a revert at target, refusing one that reaches past a
// compaction, and returns how many files it restored. OpenCode records a
// turn's files only in a git repository (packages/core/src/snapshot.ts:114 at
// b44b7164ba), so elsewhere that is none.
func (s *acpSurfaces) stage(ctx context.Context, h acpagent.Host, target sessionMessage) (int, error) {
	compactions, err := s.newestMessages(ctx, h, "compaction", 1)
	if err != nil {
		return 0, err
	}
	if len(compactions) > 0 && compactions[0].Time.Created >= target.Time.Created {
		return 0, errACPAcrossCompaction
	}
	var out struct {
		Data struct {
			Files []json.RawMessage `json:"files"`
		} `json:"data"`
	}
	err = s.do(ctx, h, control.Request{Method: http.MethodPost, Path: sessionPath(h.AgentSessionID(), "/revert/stage"),
		Body: map[string]any{"messageID": target.ID, "files": true}}, &out)
	return len(out.Data.Files), err
}

// undoText says what an undo of turns did to the files.
func undoText(turns, files int) string {
	what := "the last turn"
	if turns > 1 {
		what = fmt.Sprintf("the last %d turns", turns)
	}
	restored := "no file changes were recorded to restore (OpenCode records them only in a git repository)"
	switch {
	case files == 1:
		restored = "restored 1 file"
	case files > 1:
		restored = fmt.Sprintf("restored %d files", files)
	}
	return fmt.Sprintf("Undid %s and %s. /redo brings it back; your next prompt makes the undo permanent.", what, restored)
}

// ready refuses an operation that cannot run now.
func ready(h acpagent.Host) error {
	if h.Busy() {
		return errACPBusy
	}
	if h.AgentSessionID() == "" {
		return errors.New("opencode: the session has not started")
	}
	return nil
}

func (s *acpSurfaces) undoLast(ctx context.Context, h acpagent.Host) (string, error) {
	if err := ready(h); err != nil {
		return "", err
	}
	st, err := undoOf(h)
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, acpRevertTimeout)
	defer cancel()
	users, err := s.newestMessages(ctx, h, "user", st.turns+1)
	if err != nil {
		return "", err
	}
	if len(users) <= st.turns {
		return "", errors.New("there is no earlier turn to undo")
	}
	files, err := s.stage(ctx, h, users[st.turns])
	if err != nil {
		return "", err
	}
	st.turns++
	return undoText(st.turns, files), nil
}

func (s *acpSurfaces) revert(ctx context.Context, h acpagent.Host, messageID, partID string) error {
	if partID != "" {
		return errors.New("OpenCode reverts a whole turn, not a part of one")
	}
	if err := ready(h); err != nil {
		return err
	}
	st, err := undoOf(h)
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, acpRevertTimeout)
	defer cancel()
	users, err := s.newestMessages(ctx, h, "user", 200)
	if err != nil {
		return err
	}
	for i, m := range users {
		if m.ID != messageID {
			continue
		}
		if _, err := s.stage(ctx, h, m); err != nil {
			return err
		}
		st.turns = i + 1
		return nil
	}
	return errors.New("only the turn of one of the last 200 prompts can be reverted")
}

func (s *acpSurfaces) unrevert(ctx context.Context, h acpagent.Host) error {
	if err := ready(h); err != nil {
		return err
	}
	st, err := undoOf(h)
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, acpRevertTimeout)
	defer cancel()
	if err := s.do(ctx, h, control.Request{Method: http.MethodDelete, Path: sessionPath(h.AgentSessionID(), "/revert")}, nil); err != nil {
		return err
	}
	st.turns = 0
	return nil
}

// beforePrompt makes a staged undo permanent before the next prompt: the
// revert is committed, then the undone turns leave the transcript. If the
// commit fails the prompt is refused and the undo stays staged.
func (s *acpSurfaces) beforePrompt(ctx context.Context, h acpagent.Host) error {
	st, err := undoOf(h)
	if err != nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.turns == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, acpRevertTimeout)
	defer cancel()
	if err := s.do(ctx, h, control.Request{Method: http.MethodPost, Path: sessionPath(h.AgentSessionID(), "/revert/commit")}, nil); err != nil {
		return fmt.Errorf("the undo could not be made permanent, so the prompt was not sent; /redo restores the turn: %w", err)
	}
	h.Emit(event.Event{Type: event.TypeTranscriptRemove, RemoveTurns: st.turns})
	st.turns = 0
	return nil
}

// newShellMessageID is a message id of the form the shell route takes.
func newShellMessageID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "msg_" + hex.EncodeToString(b), nil
}

func (s *acpSurfaces) shell(ctx context.Context, h acpagent.Host, command string) error {
	if err := validShellCommand(command); err != nil {
		return err
	}
	if err := ready(h); err != nil {
		return err
	}
	id, err := newShellMessageID()
	if err != nil {
		return err
	}
	// The command is never logged (as on 1.x).
	h.Log().Info("opencode shell submitted", slog.String("agent_session_id", h.AgentSessionID()))
	h.Emit(event.Event{Type: event.TypeToolCall, ToolID: id, ToolName: "bash", ToolKind: "execute",
		Status: event.ToolStatusRunning, Text: command})
	finished := func(status, text string) {
		h.Emit(event.Event{Type: event.TypeToolUpdate, ToolID: id, ToolName: "bash", ToolKind: "execute",
			Status: status, Text: text})
	}
	// No retry: a command that timed out may still be running.
	err = s.do(ctx, h, control.Request{Method: http.MethodPost, Path: sessionPath(h.AgentSessionID(), "/shell"),
		Body: map[string]any{"id": id, "command": command}, Timeout: shellTimeout}, nil)
	if err != nil {
		finished(event.ToolStatusFailed, "The command could not be run.")
		return err
	}
	readCtx, cancel := context.WithTimeout(ctx, acpRevertTimeout)
	defer cancel()
	var msg struct {
		Data struct {
			Status string `json:"status"`
			Exit   *int   `json:"exit"`
			Output struct {
				Output    string `json:"output"`
				Truncated bool   `json:"truncated"`
			} `json:"output"`
		} `json:"data"`
	}
	if err := s.do(readCtx, h, control.Request{Method: http.MethodGet, Path: sessionPath(h.AgentSessionID(), "/message/"+url.PathEscape(id))}, &msg); err != nil {
		finished(event.ToolStatusCompleted, "The command ran; its output could not be read.")
		return nil
	}
	finished(shellStatus(msg.Data.Exit), shellText(msg.Data.Output.Output, msg.Data.Output.Truncated, msg.Data.Exit))
	return nil
}

// shellStatus is the tool status for an exit code: failed when it is known
// and not zero.
func shellStatus(exit *int) string {
	if exit != nil && *exit != 0 {
		return event.ToolStatusFailed
	}
	return event.ToolStatusCompleted
}

// shellText is the output as shown: cut at acpShellOutputMax, with a line
// saying so, and the exit code when it is not zero.
func shellText(out string, engineCut bool, exit *int) string {
	if len(out) > acpShellOutputMax {
		out, engineCut = out[:acpShellOutputMax], true
	}
	if engineCut {
		out += "\n… output cut"
	}
	if exit != nil && *exit != 0 {
		out += fmt.Sprintf("\n(exit %d)", *exit)
	}
	return out
}
