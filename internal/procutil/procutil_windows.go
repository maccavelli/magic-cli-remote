//go:build windows

// Package procutil helps manage child process trees (job objects on Windows).
package procutil

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// SetProcessGroup configures cmd so it starts in a new process group.
//
// On Windows that means CREATE_NEW_PROCESS_GROUP, which is what makes
// GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid) deliverable to the child
// alone rather than to this process's whole console group. Note the flag also
// disables Ctrl+C in the child, which is why termination uses CTRL_BREAK.
//
// This does NOT create the job object: os/exec exposes no hook between
// CreateProcess and the caller regaining control, so the tree-kill guarantee
// comes from [SuperviseStarted], which the caller invokes right after Start.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

// KillProcessGroup terminates p.
func KillProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		// Already gone, or not ours to kill; fall back to the runtime's own
		// handle, which is what the caller would have used anyway.
		return p.Kill()
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return p.Kill()
	}
	return nil
}

// TerminateProcessGroup stops p politely, then kills it if it has not exited
// within timeout. It reports whether the polite signal alone was enough.
//
// The polite signal is CTRL_BREAK_EVENT, which requires the child to have been
// started with CREATE_NEW_PROCESS_GROUP ([SetProcessGroup]); without that the
// event would be delivered to this process's group too. Semantics match the
// Unix implementation: true iff the graceful phase sufficed.
func TerminateProcessGroup(p *os.Process, exited <-chan struct{}, timeout time.Duration) bool {
	if p == nil {
		return true
	}
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid)); err != nil {
		// No console, or the child is not in its own group: there is nothing
		// gentler left to try, so escalate immediately and report that the
		// polite phase did not do it.
		_ = KillProcessGroup(p)
		return false
	}

	deadline := time.Now().Add(timeout)
	for {
		if exited != nil {
			select {
			case <-exited:
				return true
			case <-time.After(20 * time.Millisecond):
			}
		} else {
			if !processAlive(p.Pid) {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			break
		}
	}

	_ = KillProcessGroup(p)
	return false
}

// SuperviseStarted attaches an already-started process to a kill-on-close job
// object so its whole descendant tree dies with it. Call it immediately after
// cmd.Start(). The returned func closes the job, killing the tree.
//
// This is the Windows answer to the Unix negative-pid signal: a process group
// there is signalled as a unit, whereas on Windows only a job object gives the
// same guarantee for grandchildren (node, python, git spawned by an agent
// CLI). It is a no-op on Unix (MADR 0116 D8).
func SuperviseStarted(p *os.Process) (release func(), err error) {
	if p == nil {
		return func() {}, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("procutil: create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafePointer(&info)), uint32(unsafeSizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("procutil: set job limits: %w", err)
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("procutil: open process %d: %w", p.Pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("procutil: assign process %d to job: %w", p.Pid, err)
	}
	return func() { windows.CloseHandle(job) }, nil
}
