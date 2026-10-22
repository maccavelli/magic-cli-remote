package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Silent-command detection (MADR 0208 D10). When the daemon forwards a slash
// command to the agent and the turn it starts completes with nothing to show,
// the command is marked silent for that provider, agent version and host. A
// mark makes a canonical command that would forward to it unavailable, and
// `mcremote doctor` lists every mark. A mark recorded under another version or
// on another host is ignored, so an upgrade, or a copied data directory, starts
// clean.

// SilentMark is one command the daemon saw produce nothing over the remote
// connection.
type SilentMark struct {
	Provider string    `json:"provider"`
	Version  string    `json:"version"`
	Host     string    `json:"host"`
	Command  string    `json:"command"`
	MarkedAt time.Time `json:"marked_at"`
}

// silentMarksFile sits beside the session store, in the daemon's data
// directory.
const silentMarksFile = "silent-commands.json"

// SilentMarksPath is where the marks of the daemon using dataDir are kept.
func SilentMarksPath(dataDir string) string { return filepath.Join(dataDir, silentMarksFile) }

// LoadSilentMarks reads every mark under dataDir, for any version and host. No
// file means no marks.
func LoadSilentMarks(dataDir string) ([]SilentMark, error) {
	b, err := os.ReadFile(SilentMarksPath(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marks []SilentMark
	if err := json.Unmarshal(b, &marks); err != nil {
		return nil, fmt.Errorf("%s: %w", SilentMarksPath(dataDir), err)
	}
	return marks, nil
}

// hostname is os.Hostname, replaced in tests.
var hostname = os.Hostname

// silentMarks is the daemon's live set of marks, kept on disk as JSON.
type silentMarks struct {
	path string
	host string
	log  *slog.Logger

	mu    sync.Mutex
	marks []SilentMark
}

// openSilentMarks loads the marks under dataDir. A file that cannot be read is
// logged and treated as empty: losing a mark only means a silent command is
// tried once more.
func openSilentMarks(dataDir string, log *slog.Logger) *silentMarks {
	host, err := hostname()
	if err != nil {
		log.Warn("silent-command marks: no host name; marks are kept but never match", slog.String("err", err.Error()))
	}
	marks, err := LoadSilentMarks(dataDir)
	if err != nil {
		log.Warn("silent-command marks unreadable; starting empty", slog.String("err", err.Error()))
		marks = nil
	}
	return &silentMarks{path: SilentMarksPath(dataDir), host: host, log: log, marks: marks}
}

// commands are the names marked silent for prov at version on this host.
func (s *silentMarks) commands(prov provider.ID, version string) []string {
	if s == nil || s.host == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.marks {
		if m.Provider == string(prov) && m.Version == version && m.Host == s.host {
			out = append(out, m.Command)
		}
	}
	return out
}

// add marks command silent for prov at version on this host and writes the
// file. It reports whether the mark is new.
func (s *silentMarks) add(prov provider.ID, version, command string, at time.Time) (bool, error) {
	if s == nil || s.host == "" {
		return false, nil
	}
	command = strings.ToLower(strings.TrimPrefix(command, "/"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.marks, func(m SilentMark) bool {
		return m.Provider == string(prov) && m.Version == version && m.Host == s.host && m.Command == command
	}) {
		return false, nil
	}
	next := append(slices.Clone(s.marks), SilentMark{
		Provider: string(prov), Version: version, Host: s.host, Command: command, MarkedAt: at.UTC(),
	})
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return false, err
	}
	if err := fsutil.WriteFileAtomic(s.path, append(b, '\n'), fsutil.AtomicOptions{
		Perm: 0o600, SyncFile: true, SyncDir: true,
	}); err != nil {
		return false, err
	}
	s.marks = next
	return true, nil
}

// engineVersion is the version the provider's agent last reported, or "".
func engineVersion(p provider.Provider) string {
	if v, ok := p.(interface{ EngineVersion() string }); ok {
		return v.EngineVersion()
	}
	return ""
}

// armSilentWatch starts following the turn a forwarded command is about to
// start. Only from an idle session: a prompt that queues behind a running turn
// cannot be told from that turn's output, so it is not judged at all.
func (m *Manager) armSilentWatch(id, command string) {
	if m.silent == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.sessions[id]; e != nil && e.promptAt.IsZero() {
		e.silentWatch = &silentWatch{command: strings.ToLower(command)}
	}
}

// noteSilent marks command silent for the session's provider at its current
// version on this host, tells the phone, and re-advertises the commands so a
// canonical command that forwards to it shows as unavailable.
func (m *Manager) noteSilent(id string, prov provider.ID, command string) {
	version := ""
	if p, err := m.provider(prov); err == nil {
		version = engineVersion(p)
	}
	added, err := m.silent.add(prov, version, command, time.Now())
	if err != nil {
		m.log.Warn("silent-command mark not saved", slog.String("provider", string(prov)),
			slog.String("command", command), slog.String("err", err.Error()))
	}
	m.emitNotice(id, fmt.Sprintf("“/%s” produced nothing over the remote connection. "+
		"It is marked unavailable here for this agent version.", command))
	if added {
		m.advertiseCommands(id)
	}
}

// silentWatch follows the turn a forwarded slash command started.
type silentWatch struct {
	command string
	output  bool
}

// breaksSilence reports whether an event of type t is something the command
// produced. Everything is, except a turn's own bookkeeping: its status, the
// echoed prompt, usage, and lists of commands and capabilities. That is broader
// than "a reply": a command whose only effect is a mode switch, a title or a
// plan is not silent (MADR 0208 PLAN, P4).
func breaksSilence(t event.Type) bool {
	switch t {
	case event.TypeSessionStatus, event.TypeUserMessage, event.TypeTurnComplete,
		event.TypeAvailableCommands, event.TypeRemoteCommands, event.TypeUsage,
		event.TypeSessionCapabilities, event.TypeDiagnosticsChanged:
		return false
	}
	return true
}
