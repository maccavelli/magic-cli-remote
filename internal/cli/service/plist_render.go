package service

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RenderPlist returns a launchd user LaunchAgent property list for opts
// (no side effects). Binary path is not required to exist (print/preview only).
// Always produces an agent plist (no UserName/GroupName, no system daemon).
func RenderPlist(opts Options) (string, error) {
	opts.PrintOnly = true
	opts, err := normalize(opts)
	if err != nil {
		return "", err
	}
	return renderPlist(opts)
}

func renderPlist(opts Options) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	u, _ := user.Current()
	username := ""
	if u != nil {
		username = u.Username
	}

	label, err := LaunchdLabel(opts.Product, opts.UnitName)
	if err != nil {
		return "", err
	}

	paths, perr := resolveProductPaths(opts.Product, opts.DataDir)
	if perr != nil {
		return "", perr
	}
	logDir := paths.LogDir
	if logDir == "" {
		logDir = filepath.Join(home, "Library", "Logs", opts.Product)
	}
	outLog := filepath.Join(logDir, opts.Product+".out.log")
	errLog := filepath.Join(logDir, opts.Product+".err.log")

	args := []string{opts.Binary, "serve"}
	if opts.ConfigPath != "" {
		args = append(args, "--config", opts.ConfigPath)
	}
	if opts.DataDir != "" {
		args = append(args, "--data-dir", opts.DataDir)
	}
	if opts.ListenHost != "" {
		args = append(args, "--listen-host", opts.ListenHost)
	}
	if opts.ListenPort > 0 {
		args = append(args, "--listen-port", strconv.Itoa(opts.ListenPort))
	}
	if opts.LogLevel != "" {
		args = append(args, "--log-level", opts.LogLevel)
	}
	if opts.LogFormat != "" {
		args = append(args, "--log-format", opts.LogFormat)
	}

	wd := opts.WorkingDirectory
	if wd == "" {
		wd = home
	}

	// Export resolved absolute XDG roots so LaunchAgent matches shell/appdirs.
	env := map[string]string{
		"HOME":            home,
		"USER":            username,
		"LOGNAME":         username,
		"PATH":            servicePathEnv(home),
		"XDG_CONFIG_HOME": filepath.Dir(paths.ConfigDir),
		"XDG_DATA_HOME":   filepath.Dir(paths.DataDir),
		"XDG_STATE_HOME":  filepath.Dir(paths.StateDir),
		"XDG_CACHE_HOME":  filepath.Dir(paths.CacheDir),
	}
	if paths.RuntimeBase != "" {
		// Prefer parent of product runtime base when it is under a true runtime home.
		rt := filepath.Dir(paths.RuntimeBase)
		if filepath.Base(paths.RuntimeBase) == opts.Product || strings.Contains(filepath.Base(paths.RuntimeBase), "-runtime-") {
			if strings.Contains(filepath.Base(paths.RuntimeBase), "-runtime-") {
				env["XDG_RUNTIME_DIR"] = paths.RuntimeBase
			} else if rt != "." && filepath.IsAbs(rt) {
				env["XDG_RUNTIME_DIR"] = rt
			}
		}
	}
	for _, kv := range opts.ExtraEnviron {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString(`<dict>` + "\n")
	writePlistString(&b, "Label", label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range args {
		b.WriteString("    <string>")
		b.WriteString(xmlEscape(a))
		b.WriteString("</string>\n")
	}
	b.WriteString("  </array>\n")
	writePlistString(&b, "WorkingDirectory", wd)
	b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	// Stable key order for goldens and diffs.
	for _, k := range []string{"HOME", "USER", "LOGNAME", "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		if v, ok := env[k]; ok {
			b.WriteString("    <key>")
			b.WriteString(xmlEscape(k))
			b.WriteString("</key>\n    <string>")
			b.WriteString(xmlEscape(v))
			b.WriteString("</string>\n")
			delete(env, k)
		}
	}
	// Remaining ExtraEnviron keys (sorted for stability).
	if len(env) > 0 {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("    <key>")
			b.WriteString(xmlEscape(k))
			b.WriteString("</key>\n    <string>")
			b.WriteString(xmlEscape(env[k]))
			b.WriteString("</string>\n")
		}
	}
	b.WriteString("  </dict>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	b.WriteString("  <key>ThrottleInterval</key>\n  <integer>2</integer>\n")
	b.WriteString("  <key>ExitTimeOut</key>\n  <integer>45</integer>\n")
	writePlistString(&b, "ProcessType", "Standard")
	b.WriteString("  <key>SoftResourceLimits</key>\n  <dict>\n")
	b.WriteString("    <key>NumberOfFiles</key>\n    <integer>65536</integer>\n")
	b.WriteString("  </dict>\n")
	writePlistString(&b, "StandardOutPath", outLog)
	writePlistString(&b, "StandardErrorPath", errLog)
	// Decimal 63 == octal 077 (MADR 0059 D4).
	b.WriteString("  <key>Umask</key>\n  <integer>63</integer>\n")
	b.WriteString(`</dict>` + "\n")
	b.WriteString(`</plist>` + "\n")
	return b.String(), nil
}

func writePlistString(b *strings.Builder, key, value string) {
	b.WriteString("  <key>")
	b.WriteString(xmlEscape(key))
	b.WriteString("</key>\n  <string>")
	b.WriteString(xmlEscape(value))
	b.WriteString("</string>\n")
}

func xmlEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
