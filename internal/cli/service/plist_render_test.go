package service_test

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

func TestRenderPlistMinimal(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		UnitName: "mcremote",
		Binary:   "/Users/mac/.local/bin/mcremote",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.magiccliremote.mcremote</string>",
		"<string>/Users/mac/.local/bin/mcremote</string>",
		"<string>serve</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<true/>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q\n%s", want, body)
		}
	}
}

func TestRenderPlistHardeningKeys(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		Binary: "/usr/local/bin/mcremote",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>ThrottleInterval</key>",
		"<integer>2</integer>",
		"<key>ExitTimeOut</key>",
		"<integer>45</integer>",
		"<key>ProcessType</key>",
		"<string>Standard</string>",
		"<key>NumberOfFiles</key>",
		"<integer>65536</integer>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plist missing %q\n%s", want, body)
		}
	}
}

func TestRenderPlistLogsNotTmp(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		Product: "mcremote",
		Binary:  "/usr/local/bin/mcremote",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "/tmp/") {
		t.Errorf("logs must not use /tmp:\n%s", body)
	}
	if !strings.Contains(body, "Library/Logs/mcremote") {
		t.Errorf("expected Library/Logs/mcremote:\n%s", body)
	}
	if !strings.Contains(body, "mcremote.out.log") || !strings.Contains(body, "mcremote.err.log") {
		t.Errorf("expected out/err log names:\n%s", body)
	}
}

func TestRenderPlistPATH(t *testing.T) {
	body, err := service.RenderPlist(service.Options{Binary: "/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, ".local/bin") {
		t.Errorf("PATH missing .local/bin:\n%s", body)
	}
	if !strings.Contains(body, "/opt/homebrew/bin") {
		t.Errorf("PATH missing /opt/homebrew/bin:\n%s", body)
	}
}

func TestRenderPlistConfigArg(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		Binary:     "/usr/local/bin/mcremote",
		ConfigPath: "/Users/mac/.config/mcremote/config.yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<string>--config</string>") {
		t.Errorf("missing --config arg:\n%s", body)
	}
	if !strings.Contains(body, "<string>/Users/mac/.config/mcremote/config.yaml</string>") {
		t.Errorf("missing config path:\n%s", body)
	}
}

func TestRenderPlistNoUserName(t *testing.T) {
	body, err := service.RenderPlist(service.Options{Binary: "/usr/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "UserName") || strings.Contains(body, "GroupName") {
		t.Errorf("agent plist must not set UserName/GroupName:\n%s", body)
	}
}

func TestRenderPlistEscaping(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		Binary:     `/Users/a&b/<bin>/mcremote`,
		ConfigPath: `/Users/a&b/config.yaml`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, `<bin>`) || strings.Contains(body, `a&b`) {
		// raw unescaped should not appear
		if strings.Contains(body, `a&b`) && !strings.Contains(body, `a&amp;b`) {
			t.Errorf("ampersand not escaped:\n%s", body)
		}
	}
	if !strings.Contains(body, `a&amp;b`) {
		t.Errorf("expected &amp; escape:\n%s", body)
	}
	if !strings.Contains(body, `&lt;bin&gt;`) {
		t.Errorf("expected &lt; &gt; escape:\n%s", body)
	}
}

func TestRenderPlistControlChars(t *testing.T) {
	inject := "127.0.0.1\nInjected"
	_, err := service.RenderPlist(service.Options{
		Binary:     "/usr/bin/true",
		ListenHost: inject,
	})
	if err == nil {
		t.Fatal("expected control-character rejection")
	}
	if !strings.Contains(err.Error(), "control characters") {
		t.Fatalf("error = %v, want control-characters", err)
	}
}

func TestLaunchdLabelMapping(t *testing.T) {
	cases := []struct {
		product, unit, want string
	}{
		{"mcremote", "", "com.magiccliremote.mcremote"},
		{"mcremote", "mcremote", "com.magiccliremote.mcremote"},
		{"mcrelay", "mcrelay", "com.magiccliremote.mcrelay"},
		{"mcremote", "com.example.custom", "com.example.custom"},
		{"mcremote", "ci-test", "com.magiccliremote.ci-test"},
	}
	for _, tc := range cases {
		got, err := service.LaunchdLabel(tc.product, tc.unit)
		if err != nil {
			t.Errorf("LaunchdLabel(%q,%q): %v", tc.product, tc.unit, err)
			continue
		}
		if got != tc.want {
			t.Errorf("LaunchdLabel(%q,%q)=%q, want %q", tc.product, tc.unit, got, tc.want)
		}
	}
	if _, err := service.LaunchdLabel("mcremote", "has space"); err == nil {
		t.Error("expected error for invalid unit name")
	}
}

func TestRenderPlistMcrelayLabel(t *testing.T) {
	body, err := service.RenderPlist(service.Options{
		Product: "mcrelay",
		Binary:  "/Users/mac/.local/bin/mcrelay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "com.magiccliremote.mcrelay") {
		t.Errorf("missing mcrelay label:\n%s", body)
	}
	if !strings.Contains(body, "Library/Logs/mcrelay") {
		t.Errorf("missing mcrelay logs:\n%s", body)
	}
}
