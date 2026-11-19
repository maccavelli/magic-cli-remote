package service

import (
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/cliout"
)

// PrintRemoveResult writes what --remove did, for mcremote ("daemon") or
// mcrelay ("relay"): the definition, the backup it deleted when there was one
// (MADR 0210 D9), the logs it kept (MADR 0212 PLAN P4b), and whether the
// service stopped. A stop that failed is said so: the definition is gone, but
// the process may still run (MADR 0207 PLAN U6b).
func PrintRemoveResult(out io.Writer, res Result, what string) {
	cliout.Fprintf(out, "Removed:           %s\n", res.UnitPath)
	if res.RemovedBackup != "" {
		cliout.Fprintf(out, "Removed backup:    %s\n", res.RemovedBackup)
	}
	if res.KeptLogs != "" {
		cliout.Fprintf(out, "Logs kept:         %s (delete them yourself when no longer needed)\n", res.KeptLogs)
	}
	if res.StopFailure != "" {
		cliout.Fprintf(out, "The %s could not be stopped: %s. Its definition is removed; end the process yourself if it still runs.\n",
			what, res.StopFailure)
		return
	}
	cliout.Fprintf(out, "The %s was stopped and the service disabled.\n", what)
}

// PrintRefreshResult writes the outcome of a --refresh. Every verdict is a
// success: the caller decides nothing from the exit code except whether the
// refresh itself errored.
func PrintRefreshResult(out io.Writer, res RefreshResult, product string, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	switch res.Verdict {
	case VerdictNone:
		cliout.Fprintf(out, "no service definition installed for %s — nothing to refresh\n", product)
	case VerdictUnchanged:
		cliout.Fprintf(out, "service definition unchanged: %s\n", res.Path)
	case VerdictRefreshed:
		if res.BackupPath != "" {
			cliout.Fprintf(out, "service definition refreshed: %s (previous kept at %s)\n", res.Path, res.BackupPath)
		} else {
			cliout.Fprintf(out, "service definition would be refreshed: %s\n", res.Path)
		}
	case VerdictKept:
		cliout.Fprintf(out, "service definition kept: %s — %s; refresh it with: %s setup-service --force\n",
			res.Path, res.Reason, product)
	}
	for _, w := range res.Warnings {
		cliout.Fprintf(out, "warning: %s\n", w)
	}
	return nil
}

// PrintSetupResult writes the post-install summary for mcremote or mcrelay.
func PrintSetupResult(out io.Writer, res Result, noLinger bool, product string) {
	if product == "" {
		product = res.UnitName
		if product == "" {
			product = appdirs.ProductMcremote.Name
		}
	}
	if res.NotConfigured {
		for _, line := range res.Advice {
			cliout.Fprintln(out, line)
		}
		return
	}
	defer printHealth(out, res)
	if Kind(res.Scope).isScriptKind() {
		printScriptResult(out, res, product)
		return
	}
	cliout.Fprintf(out, "ExecStart binary:  %s\n", res.Binary)
	switch res.Scope {
	case string(KindLaunchdAgent):
		cliout.Fprintf(out, "Plist:             %s\n", res.UnitPath)
		cliout.Fprintf(out, "Label:             %s\n", res.Label)
		cliout.Fprintln(out, "Scope:             launchd-agent (session — stops on logout)")
	case string(KindWindowsTask):
		cliout.Fprintf(out, "Task:              %s\n", res.UnitPath)
		cliout.Fprintln(out, "Scope:             per-user Task Scheduler task (at logon, no elevation)")
	default:
		cliout.Fprintf(out, "Unit file:         %s\n", res.UnitPath)
		cliout.Fprintf(out, "Unit name:         %s.service\n", res.UnitName)
		cliout.Fprintln(out, "Scope:             systemd-user")
	}
	if res.ConfigPath != "" {
		if res.ConfigCreated {
			cliout.Fprintf(out, "Config:            %s (default written)\n", res.ConfigPath)
			switch res.Scope {
			case string(KindLaunchdAgent):
				svc := res.Domain + "/" + res.Label
				if res.Domain == "" {
					svc = "gui/$(id -u)/" + res.Label
				}
				cliout.Fprintf(out, "                   Edit this file, then: launchctl kickstart -k %s\n", svc)
			case string(KindWindowsTask):
				cliout.Fprintf(out, "                   Edit this file, then: schtasks /end /tn %s and schtasks /run /tn %s\n", res.Label, res.Label)
			default:
				cliout.Fprintln(out, "                   Edit this file, then: systemctl --user restart "+res.UnitName)
			}
		} else {
			cliout.Fprintf(out, "Config:            %s\n", res.ConfigPath)
		}
	}
	if res.Enabled {
		switch res.Scope {
		case string(KindLaunchdAgent):
			cliout.Fprintln(out, "Enabled:           yes (launchctl enable)")
		case string(KindWindowsTask):
			cliout.Fprintln(out, "Enabled:           yes (registered in Task Scheduler)")
		default:
			cliout.Fprintln(out, "Enabled:           yes (systemctl --user enable)")
		}
	} else {
		cliout.Fprintln(out, "Enabled:           skipped")
	}
	if res.Started {
		// What setup did, through the backend the update uses (MADR 0207 D4,
		// D43; PLAN U6b).
		switch res.Scope {
		case string(KindLaunchdAgent):
			cliout.Fprintln(out, "Started:           yes (booted out, then bootstrapped and kickstarted)")
		case string(KindWindowsTask):
			cliout.Fprintln(out, "Started:           yes (enabled, then started; --force stops it first)")
		default:
			cliout.Fprintln(out, "Started:           yes (stopped, then started)")
		}
	} else {
		cliout.Fprintln(out, "Started:           skipped")
	}
	switch res.Scope {
	case string(KindLaunchdAgent):
		cliout.Fprintln(out, "Linger:            n/a on macOS (LaunchAgent is session-bound; no sudo system daemon)")
		if res.LogDir != "" {
			cliout.Fprintf(out, "Logs dir:          %s\n", res.LogDir)
		}
	case string(KindWindowsTask):
		// No linger concept: the task starts at logon and stops at logoff.
	default:
		if res.LingerEnabled {
			cliout.Fprintln(out, "Linger:            yes (survives logout)")
		} else if !noLinger {
			cliout.Fprintln(out, "Linger:            not enabled (run: loginctl enable-linger $USER)")
		} else {
			cliout.Fprintln(out, "Linger:            skipped")
		}
	}
	cliout.Fprintln(out)
	cliout.Fprintln(out, "Note: setup-service does not install the binary.")
	if res.Scope == string(KindWindowsTask) {
		cliout.Fprintln(out, "      Install/update it with: install.ps1")
	} else {
		cliout.Fprintln(out, "      Install/update it with: make install")
	}
	if res.Scope == string(KindLaunchdAgent) {
		cliout.Fprintln(out, "Note: macOS 13+ may show a Background Items notification;")
		cliout.Fprintln(out, "      System Settings → General → Login Items can disable the agent.")
	}
	cliout.Fprintln(out)
	switch res.Scope {
	case string(KindLaunchdAgent):
		svc := res.Domain + "/" + res.Label
		if res.Domain == "" {
			svc = "gui/$(id -u)/" + res.Label
		}
		cliout.Fprintf(out, "Status:  launchctl print %s\n", svc)
		if res.LogDir != "" {
			cliout.Fprintf(out, "Logs:    tail -f %s/%s.err.log\n", res.LogDir, product)
		}
		cliout.Fprintf(out, "Stop:    launchctl bootout %s\n", svc)
		cliout.Fprintf(out, "Remove:  %s setup-service --remove\n", product)
	case string(KindWindowsTask):
		// MADR 0159 D2. Stop is written for D5's semantics: once the task
		// carries a repeating trigger, /end alone is undone within a minute, so
		// stopping means disabling first. Until then /disable is harmless.
		cliout.Fprintf(out, "Status:  schtasks /query /tn %s\n", res.Label)
		cliout.Fprintf(out, "Start:   schtasks /run /tn %s\n", res.Label)
		cliout.Fprintf(out, "Stop:    schtasks /change /tn %s /disable  then  schtasks /end /tn %s\n", res.Label, res.Label)
		cliout.Fprintln(out, "Logs:    not yet written to a file on Windows (MADR 0157)")
		cliout.Fprintf(out, "Remove:  %s setup-service --remove\n", product)
	default:
		cliout.Fprintf(out, "Status:  systemctl --user status %s\n", res.UnitName)
		cliout.Fprintf(out, "Logs:    journalctl --user -u %s -f\n", res.UnitName)
		cliout.Fprintf(out, "Stop:    systemctl --user stop %s\n", res.UnitName)
		cliout.Fprintf(out, "Disable: systemctl --user disable --now %s\n", res.UnitName)
	}
}

// printScriptResult is PrintSetupResult for a runit, s6 or OpenRC user service
// (MADR 0054 PLAN S5b). Its boot and session wording is install.sh's summary
// (scripts/install.sh:916-958).
func printScriptResult(out io.Writer, res Result, product string) {
	kind := Kind(res.Scope)
	def := scriptDefFor(kind, res.UnitName, filepath.Dir(filepath.Dir(res.UnitPath)))
	if kind == KindOpenRCUser {
		def = scriptDefFor(kind, res.UnitName, filepath.Dir(res.UnitPath))
	}
	cliout.Fprintf(out, "Serve binary:      %s\n", res.Binary)
	cliout.Fprintf(out, "Definition:        %s\n", res.UnitPath)
	cliout.Fprintf(out, "Scope:             %s (user service)\n", kind)
	if res.ConfigPath != "" {
		if res.ConfigCreated {
			cliout.Fprintf(out, "Config:            %s (default written)\n", res.ConfigPath)
		} else {
			cliout.Fprintf(out, "Config:            %s\n", res.ConfigPath)
		}
	}
	if res.Enabled {
		cliout.Fprintln(out, "Enabled:           yes")
	} else {
		cliout.Fprintln(out, "Enabled:           skipped")
	}
	if res.Started {
		cliout.Fprintln(out, "Started:           yes")
	} else {
		cliout.Fprintln(out, "Started:           skipped")
	}
	switch {
	case res.SupervisorStarted && kind == KindOpenRCUser:
		cliout.Fprintln(out, "Supervisor:        started your OpenRC user session for this login")
		cliout.Fprintln(out, "At boot:           NOT configured unless pam_openrc starts user sessions at login")
	case res.SupervisorStarted:
		scanner := toolRunsvdir
		if kind == KindS6 {
			scanner = toolS6Svscan
		}
		cliout.Fprintf(out, "Supervisor:        started %s for this login session (pid %d)\n", scanner, res.SupervisorPID)
		cliout.Fprintln(out, "At boot:           NOT configured — arrange for your supervisor to start at boot")
	case kind == KindOpenRCUser:
		cliout.Fprintln(out, "At boot:           in the user default runlevel (rc-update --user)")
	default:
		cliout.Fprintln(out, "Supervisor:        your own, already watching "+def.ScanDir)
		cliout.Fprintln(out, "At boot:           as your supervisor is started at boot")
	}
	cliout.Fprintln(out)
	cliout.Fprintln(out, "Note: setup-service does not install the binary.")
	cliout.Fprintln(out, "      Install/update it with: make install")
	cliout.Fprintln(out)
	switch kind {
	case KindRunit:
		cliout.Fprintf(out, "Status:  sv status %s\n", def.Dir)
		cliout.Fprintf(out, "Stop:    sv down %s\n", def.Dir)
	case KindS6:
		cliout.Fprintf(out, "Status:  s6-svstat %s\n", def.Dir)
		cliout.Fprintf(out, "Stop:    s6-svc -d %s\n", def.Dir)
	case KindOpenRCUser:
		cliout.Fprintf(out, "Status:  rc-service --user %s status\n", res.UnitName)
		cliout.Fprintf(out, "Stop:    rc-service --user %s stop\n", res.UnitName)
	}
	cliout.Fprintf(out, "Remove:  %s setup-service --remove\n", product)
}

// printHealth closes a setup result with the verified start's outcome and any
// warnings (MADR 0054 PLAN S5c).
func printHealth(out io.Writer, res Result) {
	switch res.Health {
	case HealthRunning:
		cliout.Fprintln(out)
		cliout.Fprintln(out, "Running:           yes, confirmed after the start")
	case HealthStarting:
		cliout.Fprintln(out)
		cliout.Fprintln(out, "Running:           not yet confirmed — still starting when the wait ended")
	}
	for _, w := range res.Warnings {
		cliout.Fprintf(out, "warning: %s\n", w)
	}
}
