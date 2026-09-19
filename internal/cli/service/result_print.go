package service

import (
	"encoding/json"
	"fmt"
	"io"
)

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
		fmt.Fprintf(out, "no service definition installed for %s — nothing to refresh\n", product)
	case VerdictUnchanged:
		fmt.Fprintf(out, "service definition unchanged: %s\n", res.Path)
	case VerdictRefreshed:
		if res.BackupPath != "" {
			fmt.Fprintf(out, "service definition refreshed: %s (previous kept at %s)\n", res.Path, res.BackupPath)
		} else {
			fmt.Fprintf(out, "service definition would be refreshed: %s\n", res.Path)
		}
	case VerdictKept:
		fmt.Fprintf(out, "service definition kept: %s — %s; refresh it with: %s setup-service --force\n",
			res.Path, res.Reason, product)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	return nil
}

// PrintSetupResult writes the post-install summary for mcremote or mcrelay.
func PrintSetupResult(out io.Writer, res Result, noLinger bool, product string) {
	if product == "" {
		product = res.UnitName
		if product == "" {
			product = "mcremote"
		}
	}
	fmt.Fprintf(out, "ExecStart binary:  %s\n", res.Binary)
	switch res.Scope {
	case "launchd-agent":
		fmt.Fprintf(out, "Plist:             %s\n", res.UnitPath)
		fmt.Fprintf(out, "Label:             %s\n", res.Label)
		fmt.Fprintln(out, "Scope:             launchd-agent (session — stops on logout)")
	case "windows-task":
		fmt.Fprintf(out, "Task:              %s\n", res.UnitPath)
		fmt.Fprintln(out, "Scope:             per-user Task Scheduler task (at logon, no elevation)")
	default:
		fmt.Fprintf(out, "Unit file:         %s\n", res.UnitPath)
		fmt.Fprintf(out, "Unit name:         %s.service\n", res.UnitName)
		fmt.Fprintln(out, "Scope:             systemd-user")
	}
	if res.ConfigPath != "" {
		if res.ConfigCreated {
			fmt.Fprintf(out, "Config:            %s (default written)\n", res.ConfigPath)
			switch res.Scope {
			case "launchd-agent":
				svc := res.Domain + "/" + res.Label
				if res.Domain == "" {
					svc = "gui/$(id -u)/" + res.Label
				}
				fmt.Fprintf(out, "                   Edit this file, then: launchctl kickstart -k %s\n", svc)
			case "windows-task":
				fmt.Fprintf(out, "                   Edit this file, then: schtasks /end /tn %s and schtasks /run /tn %s\n", res.Label, res.Label)
			default:
				fmt.Fprintln(out, "                   Edit this file, then: systemctl --user restart "+res.UnitName)
			}
		} else {
			fmt.Fprintf(out, "Config:            %s\n", res.ConfigPath)
		}
	}
	if res.Enabled {
		switch res.Scope {
		case "launchd-agent":
			fmt.Fprintln(out, "Enabled:           yes (launchctl enable)")
		case "windows-task":
			fmt.Fprintln(out, "Enabled:           yes (registered in Task Scheduler)")
		default:
			fmt.Fprintln(out, "Enabled:           yes (systemctl --user enable)")
		}
	} else {
		fmt.Fprintln(out, "Enabled:           skipped")
	}
	if res.Started {
		switch res.Scope {
		case "launchd-agent":
			fmt.Fprintln(out, "Started:           yes (bootstrap + kickstart)")
		case "windows-task":
			fmt.Fprintln(out, "Started:           yes (schtasks /run)")
		default:
			fmt.Fprintln(out, "Started:           yes (systemctl --user restart/start)")
		}
	} else {
		fmt.Fprintln(out, "Started:           skipped")
	}
	switch res.Scope {
	case "launchd-agent":
		fmt.Fprintln(out, "Linger:            n/a on macOS (LaunchAgent is session-bound; no sudo system daemon)")
		if res.LogDir != "" {
			fmt.Fprintf(out, "Logs dir:          %s\n", res.LogDir)
		}
	case "windows-task":
		// No linger concept: the task starts at logon and stops at logoff.
	default:
		if res.LingerEnabled {
			fmt.Fprintln(out, "Linger:            yes (survives logout)")
		} else if !noLinger {
			fmt.Fprintln(out, "Linger:            not enabled (run: loginctl enable-linger $USER)")
		} else {
			fmt.Fprintln(out, "Linger:            skipped")
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Note: setup-service does not install the binary.")
	if res.Scope == "windows-task" {
		fmt.Fprintln(out, "      Install/update it with: install.ps1")
	} else {
		fmt.Fprintln(out, "      Install/update it with: make install")
	}
	if res.Scope == "launchd-agent" {
		fmt.Fprintln(out, "Note: macOS 13+ may show a Background Items notification;")
		fmt.Fprintln(out, "      System Settings → General → Login Items can disable the agent.")
	}
	fmt.Fprintln(out)
	switch res.Scope {
	case "launchd-agent":
		svc := res.Domain + "/" + res.Label
		if res.Domain == "" {
			svc = "gui/$(id -u)/" + res.Label
		}
		fmt.Fprintf(out, "Status:  launchctl print %s\n", svc)
		if res.LogDir != "" {
			fmt.Fprintf(out, "Logs:    tail -f %s/%s.err.log\n", res.LogDir, product)
		}
		fmt.Fprintf(out, "Stop:    launchctl bootout %s\n", svc)
		fmt.Fprintf(out, "Remove:  %s setup-service --remove\n", product)
	case "windows-task":
		// MADR 0159 D2. Stop is written for D5's semantics: once the task
		// carries a repeating trigger, /end alone is undone within a minute, so
		// stopping means disabling first. Until then /disable is harmless.
		fmt.Fprintf(out, "Status:  schtasks /query /tn %s\n", res.Label)
		fmt.Fprintf(out, "Start:   schtasks /run /tn %s\n", res.Label)
		fmt.Fprintf(out, "Stop:    schtasks /change /tn %s /disable  then  schtasks /end /tn %s\n", res.Label, res.Label)
		fmt.Fprintln(out, "Logs:    not yet written to a file on Windows (MADR 0157)")
		fmt.Fprintf(out, "Remove:  %s setup-service --remove\n", product)
	default:
		fmt.Fprintf(out, "Status:  systemctl --user status %s\n", res.UnitName)
		fmt.Fprintf(out, "Logs:    journalctl --user -u %s -f\n", res.UnitName)
		fmt.Fprintf(out, "Stop:    systemctl --user stop %s\n", res.UnitName)
		fmt.Fprintf(out, "Disable: systemctl --user disable --now %s\n", res.UnitName)
	}
}
