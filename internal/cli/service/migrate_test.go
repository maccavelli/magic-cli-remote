package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRewriteUserPath: the per-product folders leave the User PATH, the new
// folder joins it once, and every other entry, empty ones included, stays in
// its place (MADR 0054 amendment S5-4).
func TestRewriteUserPath(t *testing.T) {
	old := []string{`C:\L\Programs\mcremote`, `C:\L\Programs\mcrelay`}
	add := `C:\L\Programs\magic-cli-remote`
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{"both moved", `C:\Windows;C:\L\Programs\mcremote;C:\L\Programs\mcrelay;C:\Tools`, `C:\Windows;C:\Tools;` + add, true},
		{"case and a trailing backslash", `c:\l\programs\MCREMOTE\;C:\Tools`, `C:\Tools;` + add, true},
		{"already moved", `C:\Windows;` + add, `C:\Windows;` + add, false},
		{"moved, with an old entry left", add + `;C:\L\Programs\mcrelay`, add, true},
		{"empty entries kept", `C:\A;;C:\L\Programs\mcremote;C:\B`, `C:\A;;C:\B;` + add, true},
		{"empty value", ``, add, true},
		{"%VAR% entries kept", `%USERPROFILE%\bin;C:\L\Programs\mcremote`, `%USERPROFILE%\bin;` + add, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rewriteUserPath(tc.in, old, add)
			if got != tc.want || changed != tc.changed {
				t.Fatalf("rewriteUserPath(%q) = %q, %v; want %q, %v", tc.in, got, changed, tc.want, tc.changed)
			}
		})
	}
}

// memPath is a User PATH in memory.
type memPath struct {
	value      string
	typ        uint32
	broadcasts int
}

func (m *memPath) get() (string, uint32, error) { return m.value, m.typ, nil }
func (m *memPath) set(v string, typ uint32) error {
	m.value, m.typ = v, typ
	return nil
}
func (m *memPath) broadcast() { m.broadcasts++ }

// fakeTasks is Task Scheduler for the folder move: each task's registered
// XML by name, replaced on /create.
type fakeTasks struct {
	xml     map[string]string
	creates int
}

// migrationHost is a Windows host with install.ps1's layout under a scratch
// %LOCALAPPDATA%: each product in products installed in its own folder, and
// a User PATH with both folders, as REG_EXPAND_SZ. tasks names the products
// whose task is registered, running its old-layout binary.
func migrationHost(t *testing.T, products []string, tasks ...string) (*fakeTasks, *memPath) {
	t.Helper()
	t.Cleanup(OverrideInstallOS("windows"))
	withUnitRecord(t, "mcremote", "")
	t.Setenv("LOCALAPPDATA", t.TempDir())
	for _, p := range products {
		if err := os.MkdirAll(oldWindowsDir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		exe := filepath.Join(oldWindowsDir(p), p+".exe")
		if err := os.WriteFile(exe, []byte(p+" v0.2.0 (release)\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe+".prev", []byte(p+" v0.1.0 (release)\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		// The library's own files, as the Windows test host's old folders held.
		for _, f := range libraryLeftovers(p) {
			if err := os.WriteFile(filepath.Join(oldWindowsDir(p), f), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	prevID := binaryIdentity
	binaryIdentity = func(bin string) (string, error) {
		b, err := os.ReadFile(bin)
		line, _, _ := strings.Cut(string(b), "\n")
		return line, err
	}
	t.Cleanup(func() { binaryIdentity = prevID })
	store := &memPath{value: `C:\Windows;` + oldWindowsDir("mcremote") + `;` + oldWindowsDir("mcrelay") + `;C:\Tools`, typ: 2}
	prevPath := userPath
	userPath = store
	t.Cleanup(func() { userPath = prevPath })

	withTaskPrincipal(t, fixtureSID, nil, fixtureAccount, nil)
	ft := &fakeTasks{xml: map[string]string{}}
	for _, p := range tasks {
		ft.xml[p] = taskXMLRunning(t, p, filepath.Join(oldWindowsDir(p), p+".exe"))
	}
	withTaskState(t, func(name string) (int, bool, error) {
		_, ok := ft.xml[name]
		return 3, ok, nil
	})
	withSchtasks(t, func(args ...string) (string, error) {
		switch {
		case args[0] == "/query" && len(args) > 3 && args[3] == "/xml":
			return ft.xml[args[2]], nil
		case args[0] == "/create":
			b, err := os.ReadFile(args[4])
			if err != nil {
				t.Fatalf("read staged task file: %v", err)
			}
			ft.xml[args[2]] = decodeUTF16LEWithBOM(t, b)
			ft.creates++
		}
		return "", nil
	})
	prevBackup := taskBackupPath
	backup := filepath.Join(t.TempDir(), "task.prev.xml")
	taskBackupPath = func(string, string) (string, error) { return backup, nil }
	t.Cleanup(func() { taskBackupPath = prevBackup })
	return ft, store
}

// libraryLeftovers are the files go-selfupdate-lib keeps beside a binary, as
// seen in the Windows test host's old folders on 2026-10-19.
func libraryLeftovers(product string) []string {
	return []string{"." + product + ".exe.selfupdate.lock", "." + product + ".exe.selfupdate.cleanup",
		"." + product + ".exe.selfupdate-bak-1078993367"}
}

// taskXMLRunning is product's task as setup-service registers it, running bin.
func taskXMLRunning(t *testing.T, product, bin string) string {
	t.Helper()
	body, err := renderTaskXML(Options{Product: product, UnitName: product, Binary: bin,
		WorkingDirectory: filepath.Dir(bin)}, currentTaskUser())
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func taskCommand(t *testing.T, xml string) string {
	t.Helper()
	f, err := taskFieldsFromXML(xml)
	if err != nil {
		t.Fatal(err)
	}
	return f.Command
}

// TestRefreshMovesAnOldWindowsInstall is D9's move, end to end: both
// binaries are copied and checked, the task runs the new folder's copy, the
// two PATH entries become one with the value's type kept, the change is
// announced, and the old copies and their folders are gone.
func TestRefreshMovesAnOldWindowsInstall(t *testing.T) {
	ft, store := migrationHost(t, []string{"mcremote", "mcrelay"}, "mcremote")
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newExe := filepath.Join(newWindowsDir(), "mcremote.exe")
	if !res.Changed || res.Migrated != newWindowsDir() || !strings.Contains(res.Reason, newExe) {
		t.Fatalf("res = %+v, want a change naming the move to %s", res, newExe)
	}
	if got := taskCommand(t, ft.xml["mcremote"]); !samePath(got, newExe) {
		t.Fatalf("the task runs %s, want %s", got, newExe)
	}
	for _, p := range []string{"mcremote", "mcrelay"} {
		if b, err := os.ReadFile(filepath.Join(newWindowsDir(), p+".exe")); err != nil || string(b) != p+" v0.2.0 (release)\n" {
			t.Errorf("%s in the new folder: %q, %v", p, b, err)
		}
		if _, err := os.Stat(oldWindowsDir(p)); !os.IsNotExist(err) {
			t.Errorf("the old %s folder survived: %v", p, err)
		}
	}
	if want := `C:\Windows;C:\Tools;` + newWindowsDir(); store.value != want || store.typ != 2 || store.broadcasts != 1 {
		t.Fatalf("User PATH %q (type %d, %d broadcasts), want %q, type 2, one broadcast", store.value, store.typ, store.broadcasts, want)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "open a new terminal") {
		t.Errorf("warnings %v do not tell the operator about the PATH", res.Warnings)
	}
}

// TestRefreshKeepsACopyAnotherTaskRuns: mcrelay's own task still runs its old
// copy, so that copy stays until mcrelay's refresh moves it.
func TestRefreshKeepsACopyAnotherTaskRuns(t *testing.T) {
	_, _ = migrationHost(t, []string{"mcremote", "mcrelay"}, "mcremote", "mcrelay")
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	relay := filepath.Join(oldWindowsDir("mcrelay"), "mcrelay.exe")
	if _, err := os.Stat(relay); err != nil {
		t.Fatalf("mcrelay's old copy, which its task runs, was removed: %v", err)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), relay+" is kept") {
		t.Errorf("warnings %v do not say why the copy was kept", res.Warnings)
	}
}

// TestRefreshDoesNotMoveWhenACopyFails: a copy that does not report its
// source's identity stops the move, and nothing is switched.
func TestRefreshDoesNotMoveWhenACopyFails(t *testing.T) {
	ft, store := migrationHost(t, []string{"mcremote", "mcrelay"}, "mcremote")
	before := store.value
	binaryIdentity = func(bin string) (string, error) {
		if strings.Contains(bin, installerName) {
			return "something else", nil
		}
		return "mcremote v0.2.0 (release)", nil
	}
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated != "" || !strings.Contains(strings.Join(res.Warnings, "\n"), "was not made") {
		t.Fatalf("res = %+v, want no move and a warning", res)
	}
	if got := taskCommand(t, ft.xml["mcremote"]); !samePath(got, filepath.Join(oldWindowsDir("mcremote"), "mcremote.exe")) {
		t.Fatalf("the task was switched to %s after a failed copy", got)
	}
	if store.value != before {
		t.Fatalf("the User PATH changed after a failed copy: %q", store.value)
	}
	if _, err := os.Stat(filepath.Join(oldWindowsDir("mcremote"), "mcremote.exe")); err != nil {
		t.Fatalf("the old copy is gone after a failed move: %v", err)
	}
}

// TestRefreshAfterTheMoveOnlyCleansUp: a task already in the new folder is
// unchanged, and the refresh removes what a previous move left.
func TestRefreshAfterTheMoveOnlyCleansUp(t *testing.T) {
	ft, store := migrationHost(t, []string{"mcremote"})
	if err := copyToNewFolder(); err != nil {
		t.Fatal(err)
	}
	ft.xml["mcremote"] = taskXMLRunning(t, "mcremote", filepath.Join(newWindowsDir(), "mcremote.exe"))
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictUnchanged || ft.creates != 0 {
		t.Fatalf("res = %+v with %d registrations, want unchanged and none", res, ft.creates)
	}
	if _, err := os.Stat(oldWindowsDir("mcremote")); !os.IsNotExist(err) {
		t.Fatalf("the old folder a previous move left survived: %v", err)
	}
	if !strings.HasSuffix(store.value, ";"+newWindowsDir()) {
		t.Fatalf("User PATH %q, want the new folder", store.value)
	}
}

// TestRefreshPreviewDoesNotMove: --print-only writes nothing.
func TestRefreshPreviewDoesNotMove(t *testing.T) {
	_, store := migrationHost(t, []string{"mcremote"}, "mcremote")
	before := store.value
	res, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{PrintOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newWindowsDir()); !os.IsNotExist(err) || store.value != before {
		t.Fatalf("a preview moved something: %v, PATH %q", err, store.value)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "would move") {
		t.Errorf("warnings %v do not say the task would move", res.Warnings)
	}
}

// TestRefreshKeepsFilesItDidNotWrite: the old folder's other files, and the
// folder with them, stay; only the binary, its .prev and the library's files
// for it go (PLAN S5g Deviation 1).
func TestRefreshKeepsFilesItDidNotWrite(t *testing.T) {
	_, _ = migrationHost(t, []string{"mcremote"}, "mcremote")
	mine := filepath.Join(oldWindowsDir("mcremote"), "notes.txt")
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(oldWindowsDir("mcremote"))
	if err != nil {
		t.Fatalf("the old folder holding another file was removed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.txt" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the old folder holds %v, want only notes.txt", names)
	}
}

// TestRefreshCleansLeftoversAfterTheCopyIsGone is the Windows test host's state
// after its first move: the old binary already deleted, the library's files
// left. A later refresh removes them and the folder.
func TestRefreshCleansLeftoversAfterTheCopyIsGone(t *testing.T) {
	ft, _ := migrationHost(t, []string{"mcremote"})
	if err := copyToNewFolder(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"mcremote.exe", "mcremote.exe.prev"} {
		if err := os.Remove(filepath.Join(oldWindowsDir("mcremote"), f)); err != nil {
			t.Fatal(err)
		}
	}
	ft.xml["mcremote"] = taskXMLRunning(t, "mcremote", filepath.Join(newWindowsDir(), "mcremote.exe"))
	if _, err := refreshSchtasks(Options{Product: "mcremote"}, RefreshOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldWindowsDir("mcremote")); !os.IsNotExist(err) {
		t.Fatalf("the old folder with only the library's files survived: %v", err)
	}
}
