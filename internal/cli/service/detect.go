package service

import (
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// Kind names the service manager setup-service drives on this host (MADR 0054
// PLAN S5a). The names are install.sh's detect_init values, plus the Windows
// task, so the shell installer's records and this one read alike.
type Kind string

// The kinds detectInit reports.
const (
	// KindSystemdUser is a reachable systemd user manager.
	KindSystemdUser Kind = "systemd-user"
	// KindSystemdBroken is systemctl present with its user manager unreachable.
	KindSystemdBroken Kind = "systemd-broken"
	// KindRunit is runit: runsvdir and sv.
	KindRunit Kind = "runit"
	// KindS6 is s6: s6-svscan, s6-svc and s6-svstat.
	KindS6 Kind = "s6"
	// KindOpenRCUser is OpenRC with user services (OpenRC 0.60 or later).
	KindOpenRCUser Kind = "openrc-user"
	// KindOpenRCSystem is OpenRC with system services only.
	KindOpenRCSystem Kind = "openrc-system"
	// KindLaunchdAgent is a launchd user LaunchAgent.
	KindLaunchdAgent Kind = "launchd-agent"
	// KindWindowsTask is a per-user Task Scheduler task.
	KindWindowsTask Kind = "windows-task"
	// KindNone is no service manager setup-service knows.
	KindNone Kind = "none"
)

// installable reports whether setup-service writes a definition for k. Every
// other kind gets advice and exit 0 (MADR 0054 amendment 4). runit, s6 and
// OpenRC user services join this list in S5b.
func (k Kind) installable() bool {
	switch k {
	case KindSystemdUser, KindLaunchdAgent, KindWindowsTask:
		return true
	}
	return false
}

// osDefaultKind is the kind setup-service has always used on osName.
func osDefaultKind(osName string) Kind {
	switch osName {
	case "linux":
		return KindSystemdUser
	case "darwin":
		return KindLaunchdAgent
	case "windows":
		return KindWindowsTask
	}
	return KindNone
}

// detectKind is how Setup learns the host's kind. OverrideInstallOS pins it
// to that OS's default, so a test that pins the OS is not probing the host.
var detectKind = detectInit

// OverrideDetectedKind pins the kind Setup detects, for tests. Restore with the
// returned func.
func OverrideDetectedKind(k Kind) (restore func()) {
	prev := detectKind
	detectKind = func() Kind { return k }
	return func() { detectKind = prev }
}

// lookPath reports whether name resolves on PATH. It is a variable for tests.
var lookPath = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// detectInit ports install.sh's detect_init (scripts/install.sh:132-174).
// Darwin is launchd-first, so a Homebrew runit, s6 or systemctl never wins.
func detectInit() Kind {
	switch runtime.GOOS {
	case "darwin":
		if lookPath("launchctl") {
			return KindLaunchdAgent
		}
		return KindNone
	case "windows":
		return KindWindowsTask
	case "linux":
		return detectLinux()
	}
	return KindNone
}

// detectLinux is detect_init's Linux order: systemctl decides alone when it
// is present, then runit, s6 and OpenRC. Like install.sh it never falls
// through from a broken systemd to another supervisor.
func detectLinux() Kind {
	if lookPath("systemctl") {
		if userSystemdReachable() {
			return KindSystemdUser
		}
		return KindSystemdBroken
	}
	if lookPath("runsvdir") && lookPath("sv") {
		return KindRunit
	}
	// s6-svstat is required, not optional: the running probe needs it, so a
	// partial s6 install is none rather than a backend that cannot tell
	// whether its service is up (MADR 0099 F1).
	if lookPath("s6-svscan") && lookPath("s6-svc") && lookPath("s6-svstat") {
		return KindS6
	}
	if lookPath("rc-service") {
		if _, err := runCmdOutput("rc-service", "--user", "--help"); err == nil {
			return KindOpenRCUser
		}
		return KindOpenRCSystem
	}
	return KindNone
}

// userSystemdReachable is detect_init's systemd-user test: a runtime directory
// that exists, and a user manager that answers is-system-running (which exits
// non-zero on a degraded manager) or show-environment.
//
// One deliberate difference: install.sh requires XDG_RUNTIME_DIR to be set,
// and this derives it when it is not, as every systemctl call here already
// does (withUserRuntimeEnv). Otherwise a cron or `su -` session that
// setup-service has always served would now be refused.
func userSystemdReachable() bool {
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		var ok bool
		if rd, ok = userRuntimeDir(); !ok {
			return false
		}
	}
	if st, err := os.Stat(rd); err != nil || !st.IsDir() {
		return false
	}
	if _, err := runCmdOutputEnv([]string{"XDG_RUNTIME_DIR=" + rd}, "systemctl", "--user", "is-system-running"); err == nil {
		return true
	}
	_, err := runCmdOutputEnv([]string{"XDG_RUNTIME_DIR=" + rd}, "systemctl", "--user", "show-environment")
	return err == nil
}

// hostEnvironment is install.sh's detect_environment: native, wsl1, wsl2 or
// container (scripts/install.sh:102-123).
type hostEnvironment string

const (
	envNative    hostEnvironment = "native"
	envWSL1      hostEnvironment = "wsl1"
	envWSL2      hostEnvironment = "wsl2"
	envContainer hostEnvironment = "container"
)

// The files detectEnvironment and pid1Comm read. They are variables for
// tests, as install.sh's MC_TEST_OSRELEASE and MC_TEST_PID1COMM are.
var (
	osReleasePath    = "/proc/sys/kernel/osrelease"
	pid1CommPath     = "/proc/1/comm"
	pid1CgroupPath   = "/proc/1/cgroup"
	containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}
)

func detectEnvironment() hostEnvironment {
	if b, err := os.ReadFile(osReleasePath); err == nil {
		rel := string(b)
		switch {
		case strings.Contains(rel, "WSL2"), strings.Contains(rel, "wsl2"):
			return envWSL2
		case strings.Contains(rel, "Microsoft"):
			return envWSL1
		case strings.Contains(rel, "microsoft"):
			return envWSL2
		}
	}
	for _, m := range containerMarkers {
		if _, err := os.Stat(m); err == nil {
			return envContainer
		}
	}
	if b, err := os.ReadFile(pid1CgroupPath); err == nil {
		cg := string(b)
		if strings.Contains(cg, "docker") || strings.Contains(cg, "lxc") || strings.Contains(cg, "kubepods") {
			return envContainer
		}
	}
	return envNative
}

// pid1Comm is the name of PID 1, or "unknown".
func pid1Comm() string {
	b, err := os.ReadFile(pid1CommPath)
	if err != nil {
		return "unknown"
	}
	if s := strings.TrimSpace(string(b)); s != "" {
		return s
	}
	return "unknown"
}

// unsupervisedAdvice is what setup-service prints on a host where it writes no
// definition. It ports install.sh's advisories (:854-893) and its "not
// configured" summary (:952-956), and adds the root steps for a system-only
// OpenRC, which setup-service, running as the user, does not perform (MADR
// 0054 amendment 2026-10-19 (S5)).
func unsupervisedAdvice(kind Kind, env hostEnvironment, pid1, product, binary, name string) []string {
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }

	add("service:  not configured (no supported service manager detected: " + string(kind) + ")")
	switch kind {
	case KindRunit, KindS6, KindOpenRCUser:
		add("          setup-service does not manage " + string(kind) + " services yet")
	}
	add("run it:   "+binary+" serve",
		"background:",
		"          nohup "+binary+" serve >$HOME/"+product+".log 2>&1 &")

	// Not on WSL: there the cause is the distro's systemd boot setting, and
	// the su/pam_systemd story is wrong on every count (MADR 0099 F6).
	if kind == KindSystemdBroken && env != envWSL1 && env != envWSL2 {
		add("", "systemctl is present but the user bus is unreachable.")
		if pid1 != "systemd" && pid1 != "unknown" {
			// MADR 0099 F8: with no systemd at PID 1 there is no user manager
			// to reach, so the su story is false here.
			add("PID 1 on this host is '"+pid1+"', not systemd, so there is no",
				"user manager to connect to. systemd user services are not",
				"available here — run the daemon under this host's own",
				"supervisor, or in the foreground.")
		} else {
			add("This usually means the session was entered with 'su', which skips",
				"pam_systemd and leaves XDG_RUNTIME_DIR unset. Reconnect with ssh,",
				"or use: machinectl shell "+currentUsername()+"@")
		}
	}
	if env == envWSL2 && kind != KindSystemdUser {
		add("", "WSL2 without systemd. Enable it by adding to /etc/wsl.conf:",
			"    [boot]",
			"    systemd=true",
			"then run 'wsl.exe --shutdown' from Windows and reopen the distro.")
	}
	if env == envWSL1 {
		add("", "WSL1 has no service manager. Upgrade the distro to WSL2 for",
			"background operation: wsl.exe --set-version <distro> 2")
	}
	if kind == KindOpenRCSystem {
		add("",
			"OpenRC here runs system services only; user services need OpenRC 0.60",
			"or later. setup-service runs as you and does not install system",
			"services. To run "+product+" as one, as root, write /etc/init.d/"+name+":",
			"    #!/sbin/openrc-run",
			"    command=\""+binary+"\"",
			"    command_args=\"serve\"",
			"    command_user=\""+currentUsername()+"\"",
			"    command_background=true",
			"    pidfile=\"/run/"+name+".pid\"",
			"then: chmod 0755 /etc/init.d/"+name+" && rc-update add "+name+" default && rc-service "+name+" start")
	}
	return lines
}

func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "$USER"
}
