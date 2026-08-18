package service

// White-box: recoverOptions, render and the pinned renderEnv are the contract
// this file guards, and the round-trip between them is what stops the recovery
// parser drifting away from the template (MADR 0100).

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordSystemctl swaps the systemctl runner for one that logs its arguments.
func recordSystemctl(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	restore := OverrideRunSystemctl(func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})
	t.Cleanup(restore)
	return &calls
}

func linuxRefresh(t *testing.T) {
	t.Helper()
	restore := OverrideInstallOS("linux")
	t.Cleanup(restore)
}

// writeUnit renders opts and drops the result on disk as the installed unit.
func writeUnit(t *testing.T, unitDir string, opts Options, penv *renderEnv) string {
	t.Helper()
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := renderWith(opts, penv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(unitDir, opts.UnitName+".service")
	mode := os.FileMode(0o644)
	if len(opts.ExtraEnviron) > 0 {
		mode = 0o600
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// staleify injects a directive an older template emitted and the current one
// does not — the 0099 F4a shape, which is exactly what a refresh must undo.
func staleify(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(b), "[Service]\n", "[Service]\nPrivateDevices=true\nRestrictNamespaces=true\n", 1)
	if out == string(b) {
		t.Fatal("could not inject stale directives")
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshRoundTripsRenderedUnit(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name string
		opts Options
	}{
		{"minimal", Options{Product: "mcremote", UnitName: "mcremote", Description: "d", WorkingDirectory: home}},
		{"every flag", Options{
			Product: "mcrelay", UnitName: "mcrelay", Description: "relay d",
			Binary: filepath.Join(home, "bin", "mcrelay"), ConfigPath: filepath.Join(home, "c.yaml"),
			DataDir: filepath.Join(home, "data"), ListenHost: "127.0.0.1", ListenPort: 9099,
			LogLevel: "debug", LogFormat: "json", WorkingDirectory: home,
		}},
		{"spaces and percent", Options{
			Product: "mcremote", UnitName: "mcremote", Description: "d",
			Binary:           filepath.Join(home, "my bin", "mcremote"),
			ConfigPath:       filepath.Join(home, "a b", "100%-config.yaml"),
			WorkingDirectory: filepath.Join(home, "work dir"),
		}},
		{"extra environ", Options{
			Product: "mcremote", UnitName: "mcremote-alt", Description: "d",
			WorkingDirectory: home,
			ExtraEnviron:     []string{"TOKEN=s3cr3t", "OTHER=a b c", "PCT=50%"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.opts.Binary == "" {
				tc.opts.Binary = filepath.Join(home, tc.opts.Product)
			}
			body, err := renderWith(tc.opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			rec, why, ok := recoverOptions(tc.opts.Product, tc.opts.UnitName, body)
			if !ok {
				t.Fatalf("recoverOptions refused a unit we rendered: %s", why)
			}
			if !reflect.DeepEqual(rec.Opts, tc.opts) {
				t.Fatalf("round trip lost data:\n got %+v\nwant %+v", rec.Opts, tc.opts)
			}
			// Re-rendering with the recovered options and pinned env must be
			// byte-identical, or a refresh would churn on every run.
			again, err := renderWith(rec.Opts, &rec.Env)
			if err != nil {
				t.Fatal(err)
			}
			if again != body {
				t.Fatalf("re-render differs from the original:\n%s", diffLines(body, again))
			}
		})
	}
}

func TestRefreshUnchangedOnFreshSetup(t *testing.T) {
	linuxRefresh(t)
	calls := recordSystemctl(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(dir, "units")
	res, err := Setup(Options{
		Product: "mcremote", UnitName: "mcremote", Binary: bin, UnitDir: unitDir,
		ConfigPath: filepath.Join(dir, "config.yaml"),
		Force:      true, NoEnable: true, NoStart: true, NoLinger: true,
	})
	if err != nil {
		if strings.Contains(err.Error(), "systemd user bus") {
			t.Skip("no systemd user bus on this host")
		}
		t.Fatal(err)
	}
	before, err := os.ReadFile(res.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	*calls = nil

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictUnchanged {
		t.Fatalf("verdict = %q (%s), want unchanged", got.Verdict, got.Reason)
	}
	if got.Changed {
		t.Fatal("Changed must be false")
	}
	after, _ := os.ReadFile(res.UnitPath)
	if string(after) != string(before) {
		t.Fatal("unit was rewritten")
	}
	if !got.Reloaded || len(*calls) != 1 || (*calls)[0] != "--user daemon-reload" {
		t.Fatalf("want exactly one daemon-reload, got %v (reloaded=%v)", *calls, got.Reloaded)
	}
}

func TestRefreshRewritesStaleUnit(t *testing.T) {
	linuxRefresh(t)
	calls := recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{
		Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir,
	}
	path := writeUnit(t, unitDir, opts, nil)
	canonical, _ := os.ReadFile(path)
	staleify(t, path)
	stale, _ := os.ReadFile(path)

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictRefreshed || !got.Changed {
		t.Fatalf("verdict = %q (%s), want refreshed", got.Verdict, got.Reason)
	}
	now, _ := os.ReadFile(path)
	if string(now) != string(canonical) {
		t.Fatalf("unit not restored to the current template:\n%s", diffLines(string(canonical), string(now)))
	}
	if strings.Contains(string(now), "PrivateDevices") {
		t.Fatal("stale directive survived the refresh")
	}
	backup, err := os.ReadFile(got.BackupPath)
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(backup) != string(stale) {
		t.Fatal("backup does not hold the previous unit")
	}
	if !got.Reloaded || len(*calls) != 1 {
		t.Fatalf("want one daemon-reload after a rewrite, got %v", *calls)
	}
}

func TestRefreshPreservesBakedOptions(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{
		Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), ConfigPath: filepath.Join(dir, "c.yaml"),
		ListenHost: "127.0.0.1", ListenPort: 9099, WorkingDirectory: dir,
		ExtraEnviron: []string{"TOKEN=s3cr3t"},
	}
	path := writeUnit(t, unitDir, opts, nil)
	staleify(t, path)

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictRefreshed {
		t.Fatalf("verdict = %q (%s)", got.Verdict, got.Reason)
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{
		"--listen-port 9099", "--listen-host 127.0.0.1",
		"--config " + opts.ConfigPath, "Environment=TOKEN=s3cr3t",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("refresh dropped %q:\n%s", want, body)
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600 for a unit carrying --env", fi.Mode().Perm())
	}
}

func TestRefreshPinsRenderedEnvironment(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	pinned := renderEnv{
		Home:          dir,
		User:          "someone",
		Path:          "/pinned/one:/pinned/two",
		XDGConfigHome: filepath.Join(dir, ".config"),
		XDGDataHome:   filepath.Join(dir, ".local", "share"),
		XDGStateHome:  filepath.Join(dir, ".local", "state"),
		XDGCacheHome:  filepath.Join(dir, ".cache"),
		XDGRuntimeDir: "/run/user/4242",
	}
	opts := Options{
		Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir,
	}
	path := writeUnit(t, unitDir, opts, &pinned)

	// The caller's environment is deliberately nothing like the pinned one.
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("XDG_RUNTIME_DIR", "")

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictUnchanged {
		t.Fatalf("verdict = %q (%s), want unchanged — the environment block was recomputed", got.Verdict, got.Reason)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "Environment=PATH=/pinned/one:/pinned/two") {
		t.Fatalf("PATH was not pinned:\n%s", body)
	}
	if !strings.Contains(string(body), "Environment=XDG_RUNTIME_DIR=/run/user/4242") {
		t.Fatalf("XDG_RUNTIME_DIR was dropped:\n%s", body)
	}
	// The pinned PATH lacks every prefix this release wants, so say so.
	joined := strings.Join(got.Warnings, "\n")
	if !strings.Contains(joined, "PATH entries this release adds are not applied") {
		t.Fatalf("expected a PATH drift warning, got %v", got.Warnings)
	}
	if !strings.Contains(joined, "setup-service --force") {
		t.Fatalf("warning must name the remedy, got %v", got.Warnings)
	}
}

func TestRefreshKeepsHandEditedUnit(t *testing.T) {
	linuxRefresh(t)
	calls := recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir}
	path := writeUnit(t, unitDir, opts, nil)
	b, _ := os.ReadFile(path)
	edited := strings.Replace(string(b), "[Service]\n", "[Service]\nExecStartPre=/bin/true\n", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictKept {
		t.Fatalf("verdict = %q, want kept", got.Verdict)
	}
	if !strings.Contains(got.Reason, "ExecStartPre") {
		t.Fatalf("reason must name the directive, got %q", got.Reason)
	}
	now, _ := os.ReadFile(path)
	if string(now) != edited {
		t.Fatal("a hand-edited unit was rewritten")
	}
	// Kept still reloads: the file may have changed on disk since the manager
	// last read it (MADR 0100 F2).
	if !got.Reloaded || len(*calls) != 1 {
		t.Fatalf("want a reload even when keeping, got %v", *calls)
	}
}

func TestRefreshKeepsForeignUnit(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(unitDir, "mcremote.service")
	body := "# existing unit with different content\n[Service]\nExecStart=/bin/true\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictKept || !strings.Contains(got.Reason, "managed-by") {
		t.Fatalf("verdict = %q reason = %q, want kept/no managed-by header", got.Verdict, got.Reason)
	}
	now, _ := os.ReadFile(path)
	if string(now) != body {
		t.Fatal("a foreign unit was rewritten")
	}
}

func TestRefreshKeepsUnparseableExecStart(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: filepath.Join(dir, "mcremote"), WorkingDirectory: dir}
	path := writeUnit(t, unitDir, opts, nil)
	b, _ := os.ReadFile(path)

	for _, tc := range []struct{ name, exec, want string }{
		{"unknown flag", "ExecStart=/bin/mcremote serve --unknown-flag x", "unrecognised"},
		{"not serve", "ExecStart=/bin/mcremote daemon", "serve"},
		{"exec prefix", "ExecStart=-/bin/mcremote serve", "prefix"},
		{"flag without value", "ExecStart=/bin/mcremote serve --config", "no value"},
		{"bad port", "ExecStart=/bin/mcremote serve --listen-port zero", "not a port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := replaceExecStart(string(b), tc.exec)
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Verdict != VerdictKept || !strings.Contains(got.Reason, tc.want) {
				t.Fatalf("verdict = %q reason = %q, want kept mentioning %q", got.Verdict, got.Reason, tc.want)
			}
			now, _ := os.ReadFile(path)
			if string(now) != out {
				t.Fatal("unit was rewritten despite an unparseable ExecStart")
			}
		})
	}

	// Two ExecStart lines are equally disqualifying.
	out := string(b) + "\nExecStart=/bin/mcremote serve\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictKept || !strings.Contains(got.Reason, "more than one ExecStart") {
		t.Fatalf("verdict = %q reason = %q", got.Verdict, got.Reason)
	}
}

func TestRefreshNoUnitInstalled(t *testing.T) {
	linuxRefresh(t)
	calls := recordSystemctl(t)
	got, err := RefreshUnit(Options{Product: "mcrelay", UnitDir: t.TempDir()}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictNone || got.Changed || got.Reloaded {
		t.Fatalf("got %+v, want a none verdict with no side effects", got)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing installed must not touch systemctl, got %v", *calls)
	}
}

func TestRefreshDarwinRewritesPlist(t *testing.T) {
	restore := OverrideInstallOS("darwin")
	defer restore()
	calls := recordSystemctl(t)
	dir := t.TempDir()
	opts := Options{
		Product: "mcremote", UnitName: "mcremote",
		Binary: filepath.Join(dir, "mcremote"), ConfigPath: filepath.Join(dir, "c.yaml"),
		ListenPort: 9099, WorkingDirectory: dir,
	}
	body, err := renderPlistWith(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "com.magiccliremote.mcremote.plist")
	stale := strings.Replace(body, "  <key>ThrottleInterval</key>\n  <integer>2</integer>\n", "", 1)
	if stale == body {
		t.Fatal("could not staleify the plist")
	}
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: dir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != VerdictRefreshed || !got.Changed {
		t.Fatalf("verdict = %q (%s), want refreshed", got.Verdict, got.Reason)
	}
	now, _ := os.ReadFile(path)
	if string(now) != body {
		t.Fatalf("plist not restored to the current template:\n%s", diffLines(body, string(now)))
	}
	if !strings.Contains(string(now), "--listen-port") {
		t.Fatal("baked flags lost from ProgramArguments")
	}
	if got.Reloaded || len(*calls) != 0 {
		t.Fatalf("launchd has no daemon-reload; got %v", *calls)
	}
}

func TestRefreshWarnsOnForeignExecStartBinary(t *testing.T) {
	linuxRefresh(t)
	recordSystemctl(t)
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "units")
	opts := Options{Product: "mcremote", UnitName: "mcremote", Description: "d",
		Binary: "/somewhere/else/mcremote", WorkingDirectory: dir}
	writeUnit(t, unitDir, opts, nil)

	got, err := RefreshUnit(Options{Product: "mcremote", UnitDir: unitDir}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), "/somewhere/else/mcremote") {
		t.Fatalf("expected a foreign-binary warning, got %v", got.Warnings)
	}
}

func TestRestoreUnitBackup(t *testing.T) {
	linuxRefresh(t)
	calls := recordSystemctl(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcremote.service")
	backup := path + ".prev"
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreUnitBackup(path, backup); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "old\n" {
		t.Fatalf("restored content = %q", b)
	}
	if _, err := os.Stat(backup); err == nil {
		t.Fatal("backup should be consumed by the restore")
	}
	if len(*calls) != 1 || (*calls)[0] != "--user daemon-reload" {
		t.Fatalf("restore must reload, got %v", *calls)
	}
}

// replaceExecStart swaps the ExecStart line of a rendered unit.
func replaceExecStart(body, line string) string {
	out := make([]string, 0, 80)
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "ExecStart=") {
			out = append(out, line)
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// diffLines is a minimal first-difference report for failure messages.
func diffLines(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return "line " + itoa(i+1) + ":\n  want: " + a + "\n   got: " + b
		}
	}
	return "(no line differs)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
