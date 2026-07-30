package service

import (
	"fmt"
	"io"
)

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
	fmt.Fprintln(out, "      Install/update it with: make install")
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
	default:
		fmt.Fprintf(out, "Status:  systemctl --user status %s\n", res.UnitName)
		fmt.Fprintf(out, "Logs:    journalctl --user -u %s -f\n", res.UnitName)
		fmt.Fprintf(out, "Stop:    systemctl --user stop %s\n", res.UnitName)
		fmt.Fprintf(out, "Disable: systemctl --user disable --now %s\n", res.UnitName)
	}
}
