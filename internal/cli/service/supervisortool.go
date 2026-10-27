package service

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// runit's, s6's and OpenRC's tools are found in fixed directories, never on
// PATH (MADR 0212 D2-A). An update run from cron, an agent's shell or a
// stripped service environment has a PATH of its own, so a lookup on it
// differs by host: on Void, a user's PATH has neither /sbin nor /usr/sbin.
// The 0212 PLAN's P1 found every tool in these four directories, on Void,
// Alpine and Ubuntu.

// supervisorTools are the tools the resolver finds, the union P1 probed.
var supervisorTools = []string{
	"sv", "runsvdir", "svlogd",
	"s6-svc", "s6-svstat", "s6-svscan", "s6-svscanctl", "s6-log",
	"rc-service", "rc-update", "openrc", "start-stop-daemon", "supervise-daemon",
}

// supervisorToolDirs are searched in order: toolPath's order. It is a
// variable for tests, through OverrideSupervisorToolDirs.
var supervisorToolDirs = []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// toolPath is the PATH the tools run with. OpenRC's openrc-run finds its own
// helpers through it.
const toolPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// OverrideSupervisorToolDirs points the resolver at dirs, for tests outside
// this package that fake the tools. Restore with the returned func.
func OverrideSupervisorToolDirs(dirs ...string) (restore func()) {
	prev := supervisorToolDirs
	supervisorToolDirs = dirs
	return func() { supervisorToolDirs = prev }
}

// resolveTool returns the absolute path of a supervisor tool: the first of
// supervisorToolDirs that holds an executable file of that name. It never
// consults PATH. Its error names every directory searched.
func resolveTool(name string) (string, error) {
	if !slices.Contains(supervisorTools, name) {
		return "", fmt.Errorf("%s is not a supervisor tool", name)
	}
	for _, dir := range supervisorToolDirs {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s is in none of %s", name, strings.Join(supervisorToolDirs, ", "))
}

// haveTools reports whether every named tool resolves.
func haveTools(names ...string) bool {
	for _, n := range names {
		if _, err := resolveTool(n); err != nil {
			return false
		}
	}
	return true
}

// toolEnv is the environment the tools run with, built and never inherited:
// PATH, LC_ALL=C, the user's runtime directory, HOME and USER, and
// XDG_CONFIG_HOME when it is set, since OpenRC's user mode reads its init.d
// from there, where setupScript writes it (0212 PLAN P2 Deviation 2). extra
// entries come last, and win.
func toolEnv(extra ...string) []string {
	env := []string{"PATH=" + toolPath, "LC_ALL=C"}
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		env = append(env, "XDG_RUNTIME_DIR="+rd)
	} else if rd, ok := userRuntimeDir(); ok {
		env = append(env, "XDG_RUNTIME_DIR="+rd)
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		env = append(env, "HOME="+home)
	}
	name := os.Getenv("USER")
	if name == "" {
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
	}
	if name != "" {
		env = append(env, "USER="+name)
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		env = append(env, "XDG_CONFIG_HOME="+v)
	}
	return append(env, extra...)
}

// toolRunner runs the supervisor tools. Nil is the library's ExecRunner,
// which kills a tool when its context ends and captures both streams. It is
// a variable for tests.
var toolRunner libservice.Runner

// maxToolMessage bounds the output a CommandError quotes, as the library's
// exec reconciler does.
const maxToolMessage = 4096

// toolCall is one run of a supervisor tool for kind's service.
type toolCall struct {
	kind Kind
	// tool is the tool's name, resolved by resolveTool.
	tool string
	// args are its arguments.
	args []string
	// subject is what the call acts on, for the error: the service
	// directory, or the OpenRC service's name.
	subject string
	// env are extra environment entries.
	env []string
}

// toolFor is a call of kind's tool name on subject, with args.
func toolFor(kind Kind, subject, name string, args ...string) toolCall {
	return toolCall{kind: kind, tool: name, args: args, subject: subject}
}

// withEnv returns c with extra environment entries.
func (c toolCall) withEnv(env ...string) toolCall {
	c.env = env
	return c
}

// run runs the call under ctx, capturing its output: nothing reaches this
// process's stdout or stderr (0212-MADR M9). A tool that ran and exited
// non-zero is a *libservice.CommandError, wrapped with the class P1's table
// gives it, when there is one (classifyTool); its output is returned too.
func (c toolCall) run(ctx context.Context) (libservice.Output, error) {
	path, err := resolveTool(c.tool)
	if err != nil {
		return libservice.Output{}, err
	}
	r := toolRunner
	if r == nil {
		r = libservice.ExecRunner()
	}
	out, err := r.Run(ctx, libservice.Command{Path: path, Args: c.args, Env: toolEnv(c.env...)})
	if err != nil {
		return out, fmt.Errorf("%s %s: %w", c.tool, strings.Join(c.args, " "), err)
	}
	if out.ExitCode == 0 {
		return out, nil
	}
	msg := strings.TrimSpace(string(out.Stderr))
	if msg == "" {
		msg = strings.TrimSpace(string(out.Stdout))
	}
	if len(msg) > maxToolMessage {
		msg = msg[:maxToolMessage]
	}
	ce := &libservice.CommandError{
		Backend:  string(c.kind),
		Step:     c.step(),
		Name:     c.subject,
		ExitCode: out.ExitCode,
		Message:  msg,
	}
	if class := classifyTool(c.kind, out.ExitCode, string(out.Stdout)+"\n"+string(out.Stderr)); class != nil {
		return out, fmt.Errorf("%w: %w", ce, class)
	}
	return out, ce
}

// step is the call's tool and arguments without its subject and the
// option terminator: "sv down", "rc-service --user stop".
func (c toolCall) step() string {
	parts := []string{c.tool}
	for _, a := range c.args {
		if a != c.subject && a != "--" {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// classifyTool is the 0212 PLAN P1 table of what a failed run means. runit's
// sv exits 1 for every failure and says which on stdout, so runit is read by
// message. s6 cannot tell a missing definition from a missing supervisor;
// Installed reads the definition for that. OpenRC's permission case was not
// produced in P1, and is read by message if it comes. nil is unclassified.
func classifyTool(kind Kind, code int, out string) error {
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(out, x) {
				return true
			}
		}
		return false
	}
	switch kind {
	case KindRunit:
		switch {
		case has("unable to change to service directory: file does not exist"):
			return libservice.ErrNotInstalled
		case has("unable to open supervise/ok", "unable to open supervise/control"):
			return libservice.ErrNoSupervisor
		case has("access denied"):
			return libservice.ErrPermission
		case has("timeout: "):
			return libservice.ErrTimeout
		}
	case KindS6:
		switch {
		case has("s6-supervise not running", "supervisor not listening"):
			return libservice.ErrNoSupervisor
		case code == 111 && has("Permission denied"):
			return libservice.ErrPermission
		}
	case KindOpenRCUser:
		switch {
		case has("' does not exist"):
			return libservice.ErrNotInstalled
		case has("openrc did not boot", "XDG_RUNTIME_DIR unset"):
			return libservice.ErrNoSupervisor
		case has("Permission denied", "must be root"):
			return libservice.ErrPermission
		}
	}
	return nil
}

// runBounded runs c for a caller with no context, setup-service and the
// settle's samplers, bounded by cmdTimeout as runCmd was.
func runBounded(c toolCall) (libservice.Output, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return c.run(ctx)
}
