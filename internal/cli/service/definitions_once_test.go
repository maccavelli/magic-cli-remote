package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// G2 of MADR 0207 amendment D (D35). The update replaces the binary while the
// old daemon still runs, then stops it (D33). That is safe only while no
// definition runs the product binary anywhere but its start command: a stop
// or reload hook that ran it would run the NEW binary against the OLD daemon.
// These tests hold that premise for every definition setup-service writes, so
// a later hook fails here before it can make the order unsafe.

// g2Binary is a path no other field of the test options contains, so every
// occurrence in a rendered definition is the binary.
func g2Binary(product string) string { return "/opt/g2probe/bin/" + product }

// g2Opts are the options of a definition that sets every field the
// definitions render.
func g2Opts(product string) Options {
	return Options{
		Product: product, UnitName: product, Description: product + " daemon",
		Binary: g2Binary(product), ConfigPath: "/home/u/.config/" + product + "/config.yaml",
		DataDir: "/home/u/data", ListenHost: "127.0.0.1", ListenPort: 7531,
		LogLevel: "info", LogFormat: "json", WorkingDirectory: "/home/u/work",
		ExtraEnviron: []string{"EXTRA=1"},
	}
}

// g2Env pins the render environment, so nothing of this host reaches the
// definitions.
func g2Env() renderEnv {
	return renderEnv{
		Home: "/home/u", User: "u", Path: "/home/u/.local/bin:/usr/bin:/bin",
		XDGConfigHome: "/home/u/.config", XDGDataHome: "/home/u/.local/share",
		XDGStateHome: "/home/u/.local/state", XDGCacheHome: "/home/u/.cache",
		XDGRuntimeDir: "/run/user/1000",
	}
}

// g2Definition is one rendered definition, and how to tell its start command.
type g2Definition struct {
	name string
	body string
	// isStart reports whether line, which holds the binary, is the start
	// command, given the lines before it.
	isStart func(before []string, line string) bool
}

func g2Definitions(t *testing.T, product string) []g2Definition {
	t.Helper()
	env := g2Env()
	opts := g2Opts(product)
	unit, err := renderWith(opts, &env)
	if err != nil {
		t.Fatal(err)
	}
	plist, err := renderPlistWith(opts, &env)
	if err != nil {
		t.Fatal(err)
	}
	task, err := renderTaskXML(opts, `CORP\dev`)
	if err != nil {
		t.Fatal(err)
	}
	defs := []g2Definition{
		{"systemd unit", unit, func(_ []string, line string) bool {
			return strings.HasPrefix(line, "ExecStart=")
		}},
		{"launchd plist", plist, func(before []string, _ string) bool {
			// The first string of ProgramArguments.
			return len(before) >= 2 &&
				strings.TrimSpace(before[len(before)-1]) == "<array>" &&
				strings.TrimSpace(before[len(before)-2]) == "<key>ProgramArguments</key>"
		}},
		{"task XML", task, func(_ []string, line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), "<Command>")
		}},
	}
	for _, kind := range []Kind{KindRunit, KindS6, KindOpenRCUser} {
		body, err := renderScript(kind, opts, &env)
		if err != nil {
			t.Fatal(err)
		}
		isStart := func(_ []string, line string) bool { return strings.HasPrefix(line, "exec ") }
		if kind == KindOpenRCUser {
			// supervise-daemon runs command (MADR 0212 D10-A).
			isStart = func(_ []string, line string) bool { return strings.HasPrefix(line, "command=") }
		}
		defs = append(defs, g2Definition{string(kind) + " script", body, isStart})
	}
	return defs
}

// TestDefinitionsRunTheBinaryOnlyToStart is D35's G2: in every definition
// setup-service renders, for both products, the binary's path appears once,
// as the start command, and no systemd unit has a stop or reload hook.
func TestDefinitionsRunTheBinaryOnlyToStart(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		for _, def := range g2Definitions(t, product) {
			t.Run(product+"/"+def.name, func(t *testing.T) {
				bin := g2Binary(product)
				if n := strings.Count(def.body, bin); n != 1 {
					t.Fatalf("the binary appears %d times, want once, as the start command:\n%s", n, def.body)
				}
				lines := strings.Split(def.body, "\n")
				for i, line := range lines {
					if strings.Contains(line, bin) && !def.isStart(lines[:i], line) {
						t.Errorf("line %d names the binary but is not the start command: %q", i+1, line)
					}
				}
				for _, hook := range []string{"ExecStop=", "ExecStopPost=", "ExecReload="} {
					for i, line := range lines {
						if strings.HasPrefix(strings.TrimSpace(line), hook) {
							t.Errorf("line %d is a %s hook: %q", i+1, strings.TrimSuffix(hook, "="), line)
						}
					}
				}
			})
		}
	}
}

// TestFilesBesideTheScriptsNeverRunTheBinary is G2 for the files runit's
// and s6's definitions gain beside the run script (MADR 0212 D9-A): the log
// service's run script and s6's timeout-kill. None names the binary.
func TestFilesBesideTheScriptsNeverRunTheBinary(t *testing.T) {
	for _, product := range []string{"mcremote", "mcrelay"} {
		for _, kind := range []Kind{KindRunit, KindS6} {
			aux := scriptAuxFor(kind, scriptDefFor(kind, product, "/svc"), product, g2Env())
			if len(aux) == 0 {
				t.Fatalf("%s %s: no files beside the run script", product, kind)
			}
			for _, a := range aux {
				if strings.Contains(a.body, g2Binary(product)) {
					t.Errorf("%s %s: %s names the binary:\n%s", product, kind, a.path, a.body)
				}
			}
		}
	}
}

// TestNoSupervisorFinishScript is the rest of G2: runit and s6 run a
// service's finish script after every exit of its run script, so one that
// ran the binary would run the new binary while the old one stops. No
// non-test file of this package names one.
func TestNoSupervisorFinishScript(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if s == "finish" || strings.HasSuffix(s, "/finish") {
				t.Errorf("%s names a supervisor finish script (%q)", fset.Position(lit.Pos()), s)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no non-test Go file was checked")
	}
}
