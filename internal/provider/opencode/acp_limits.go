package opencode

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/output"
)

// The bounds and guards the workspace, diagnostics and shell surfaces keep.
// They came from the 1.x dialect when 0214 PLAN P7 retired it, unchanged.

// Workspace bounds (MADR 0112 A5, PLAN P6 cross-cutting limits).
const (
	maxWorkspacePathBytes = 4096
	maxWorkspaceEntries   = 200
	maxWorkspaceQueryLen  = 256
	maxWorkspaceMatches   = 100
	maxWorkspaceFileBytes = 262144
	// maxWorkspaceEnvelope is the largest raw JSON body this surface accepts.
	// One extra byte is read to detect overflow *before* decoding, so an
	// oversized response fails outright instead of decoding to partial JSON.
	maxWorkspaceEnvelope = 2097152
	// fileSearchLimit is what a file search asks for.
	fileSearchLimit = 100
)

// Workspace errors. These map onto stable protocol codes; the wire layer
// translates them rather than copying an upstream body.
var (
	errWorkspaceInvalidPath  = errors.New("invalid_path")
	errWorkspacePathEscape   = errors.New("path_escape")
	errWorkspacePathSymlink  = errors.New("path_symlink")
	errWorkspaceBinary       = errors.New("binary_content")
	errWorkspaceTooLarge     = errors.New("result_too_large")
	errWorkspaceInvalidQuery = errors.New("invalid_query")
)

// Diagnostics bounds (MADR 0112 A6).
const (
	maxDiagnosticSkills    = 64
	maxSkillNameLen        = 128
	maxSkillDescriptionLen = 512
	maxDiagnosticTextLen   = 160
)

// Shell command bounds (MADR 0112 A9, PLAN P10 step 2).
const (
	maxShellCommandBytes = 8192
	// shellTimeout bounds one blocking submission. It is generous because a
	// real command can legitimately take minutes; it is not a containment
	// mechanism, and the surface says so.
	shellTimeout = 30 * time.Minute
)

// normalizeWorkspacePath validates a caller-supplied relative path and returns
// its cleaned slash form ("" for the root).
//
// Absolute paths, NUL, traversal beyond the root and over-long input are
// rejected outright. This runs before any filesystem or HTTP work so a bad
// path never reaches either.
func normalizeWorkspacePath(in string) (string, error) {
	if len(in) > maxWorkspacePathBytes {
		return "", fmt.Errorf("%w: path exceeds %d bytes", errWorkspaceInvalidPath, maxWorkspacePathBytes)
	}
	if strings.ContainsRune(in, 0) {
		return "", fmt.Errorf("%w: path contains NUL", errWorkspaceInvalidPath)
	}
	if !utf8.ValidString(in) {
		return "", fmt.Errorf("%w: path is not valid UTF-8", errWorkspaceInvalidPath)
	}
	// Accept either separator from a client, then work in slash form.
	s := strings.ReplaceAll(in, "\\", "/")
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == "./" {
		return "", nil
	}
	if strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("%w: absolute paths are not accepted", errWorkspaceInvalidPath)
	}
	// A scheme-ish prefix is a URI, not a workspace path.
	if strings.Contains(s, "://") {
		return "", fmt.Errorf("%w: URIs are not accepted", errWorkspaceInvalidPath)
	}
	cleaned := path.Clean(s)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: path escapes the session root", errWorkspacePathEscape)
	}
	if cleaned == "." {
		return "", nil
	}
	return cleaned, nil
}

// checkNoSymlinkComponents rejects any *existing* component beneath root that
// is a symlink.
//
// Rejecting rather than resolving is the point: a resolved link is trusted, and
// the thing it points at can change between the check and the engine's open.
// OpenCode — not this daemon — performs that open, so this cannot eliminate a
// race with a concurrent local filesystem actor. The supported threat boundary
// assumes the engine's own workspace is not concurrently hostile; what this
// does eliminate is a link that already exists at validation time
// (MADR 0112 A5).
func checkNoSymlinkComponents(root, rel string) error {
	if rel == "" {
		return nil
	}
	cur := root
	for seg := range strings.SplitSeq(rel, "/") {
		if seg == "" {
			continue
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				// A component that does not exist cannot be a symlink; the
				// engine will report the miss.
				return nil
			}
			return fmt.Errorf("%w: cannot inspect path", errWorkspaceInvalidPath)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %q is a symlink", errWorkspacePathSymlink, seg)
		}
	}
	return nil
}

// validateReturnedPath re-checks a path the engine returned.
//
// Upstream is not trusted to stay inside the root either: a listing that
// escaped would otherwise become a navigable rung out of the workspace.
func validateReturnedPath(in string) (string, bool) {
	rel, err := normalizeWorkspacePath(strings.TrimSuffix(in, "/"))
	if err != nil || rel == "" {
		return "", false
	}
	return rel, true
}

// sortMatches orders by path, then line, then column, so two runs of the same
// search render identically regardless of engine order.
func sortMatches(m []provider.WorkspaceMatch) {
	sort.SliceStable(m, func(i, j int) bool {
		if m[i].Path != m[j].Path {
			return m[i].Path < m[j].Path
		}
		if m[i].Line != m[j].Line {
			return m[i].Line < m[j].Line
		}
		return m[i].Column < m[j].Column
	})
}

// validShellCommand checks what can be checked before submission.
//
// Deliberately no attempt to parse or constrain shell semantics: a command can
// create persistent filesystem and network effects, spawn descendants that
// outlive it, and none of that is knowable from the string. Pretending to
// sanitize it would be worse than admitting the surface is remote execution
// (MADR 0112 A9).
func validShellCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("%w: empty command", provider.ErrShellInvalid)
	}
	if len(cmd) > maxShellCommandBytes {
		return fmt.Errorf("%w: command exceeds %d bytes", provider.ErrShellInvalid, maxShellCommandBytes)
	}
	if strings.ContainsRune(cmd, 0) {
		return fmt.Errorf("%w: command contains NUL", provider.ErrShellInvalid)
	}
	if !utf8.ValidString(cmd) {
		return fmt.Errorf("%w: command is not valid UTF-8", provider.ErrShellInvalid)
	}
	return nil
}

// clip is the one-line form of s, for an id, a title, a branch or a header
// (output.Line).
func clip(s string, max int) string { return output.Line(s, max) }

// firstNonEmpty is the first of vals that is not blank, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
