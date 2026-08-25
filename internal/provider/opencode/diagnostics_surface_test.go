package opencode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Diagnostics sanitization (MADR 0112 A6, PLAN P7 steps 2/8/9).
//
// The load-bearing property is negative: locations, contents, roots, executable
// paths, MCP errors, URLs and bearer tokens must not appear anywhere on the
// wire, whatever the engine sends.

// TestMCPStateMappingIsTotal pins the closed 1.18.21 vocabulary plus its
// degradation target. A pass-through default would be the path by which an
// upstream error string escaped.
func TestMCPStateMappingIsTotal(t *testing.T) {
	for raw, want := range map[string]string{
		"connected":                    provider.MCPStateConnected,
		"disabled":                     provider.MCPStateDisabled,
		"failed":                       provider.MCPStateFailed,
		"needs_auth":                   provider.MCPStateNeedsAuth,
		"needs_client_registration":    provider.MCPStateNeedsRegistration,
		"CONNECTED":                    provider.MCPStateConnected,
		"  failed  ":                   provider.MCPStateFailed,
		"":                             provider.MCPStateUnknown,
		"   ":                          provider.MCPStateUnknown,
		"some_future_state":            provider.MCPStateUnknown,
		"needs_client_registration_v2": provider.MCPStateUnknown,
	} {
		if got := normalizeMCPState(raw); got != want {
			t.Fatalf("normalizeMCPState(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestDiagnosticsDropsEverySensitiveField is the sanitization gate. The fixture
// deliberately contains an OAuth URL, a bearer token, absolute roots, a skill
// location and its full instruction text.
func TestDiagnosticsDropsEverySensitiveField(t *testing.T) {
	h := newRecorder(
		route{"/vcs/status", `[]`},
		route{"/vcs", `{"branch":"main","default_branch":"main"}`},
		route{"/mcp", `{
			"github":{"status":"failed",
			          "error":"oauth failed at https://evil.example/callback?token=SECRETTOKEN",
			          "url":"https://mcp.example/sse",
			          "headers":{"Authorization":"Bearer BEARERSECRET"}},
			"local":{"status":"needs_client_registration",
			         "error":"register at https://register.example with Bearer OTHERSECRET"}
		}`},
		route{"/skill", `[
			{"name":"customize-opencode","description":"Author skills",
			 "location":"/Users/secret/.opencode/skills/customize-opencode/SKILL.md",
			 "content":"SECRET INSTRUCTION BODY"}
		]`},
		route{"/lsp", `[{"name":"gopls","status":"running",
			"root":"/Users/secret/project","id":"lsp-1"}]`},
		route{"/formatter", `[{"name":"gofmt","enabled":true,
			"extensions":[".go",".mod"],"command":["/usr/local/bin/gofmt"]}]`},
	)
	s := newOpsSession(h)

	got, err := s.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(got)
	for _, forbidden := range []string{
		"SECRETTOKEN", "BEARERSECRET", "OTHERSECRET",
		"oauth", "Bearer", "Authorization",
		"https://evil.example", "https://mcp.example", "https://register.example",
		"/Users/secret", "SECRET INSTRUCTION BODY",
		"/usr/local/bin/gofmt", ".mod", "lsp-1",
	} {
		if strings.Contains(string(blob), forbidden) {
			t.Fatalf("diagnostics leaked %q:\n%s", forbidden, blob)
		}
	}

	// And the useful parts survive.
	if len(got.Skills) != 1 || got.Skills[0].Name != "customize-opencode" {
		t.Fatalf("skills = %+v", got.Skills)
	}
	if got.Skills[0].Description != "Author skills" {
		t.Fatalf("skill description lost: %+v", got.Skills[0])
	}
	if len(got.LSP) != 1 || got.LSP[0].Name != "gopls" || got.LSP[0].Status != "running" {
		t.Fatalf("lsp = %+v", got.LSP)
	}
	if len(got.Formatters) != 1 || !got.Formatters[0].Enabled {
		t.Fatalf("formatters = %+v", got.Formatters)
	}
	if got.Formatters[0].Extensions != 2 {
		t.Fatalf("extensions must be a count, got %d", got.Formatters[0].Extensions)
	}
	// Both MCP rows normalize; neither keeps its raw status.
	states := map[string]string{}
	for _, m := range got.MCP {
		states[m.Name] = m.State
	}
	if states["github"] != provider.MCPStateFailed {
		t.Fatalf("github state = %q", states["github"])
	}
	if states["local"] != provider.MCPStateNeedsRegistration {
		t.Fatalf("local state = %q", states["local"])
	}
}

// TestDiagnosticsSectionsAreBounded proves each section is capped and sorted.
func TestDiagnosticsSectionsAreBounded(t *testing.T) {
	var skills, lsp, formatters []string
	for i := 0; i < 200; i++ {
		skills = append(skills, `{"name":"skill-`+pad(i)+`"}`)
		lsp = append(lsp, `{"name":"lsp-`+pad(i)+`"}`)
		formatters = append(formatters, `{"name":"fmt-`+pad(i)+`"}`)
	}
	h := newRecorder(
		route{"/skill", "[" + strings.Join(skills, ",") + "]"},
		route{"/lsp", "[" + strings.Join(lsp, ",") + "]"},
		route{"/formatter", "[" + strings.Join(formatters, ",") + "]"},
	)
	s := newOpsSession(h)
	got, err := s.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != maxDiagnosticSkills {
		t.Fatalf("skills = %d, want %d", len(got.Skills), maxDiagnosticSkills)
	}
	if len(got.LSP) != maxDiagnosticLSP {
		t.Fatalf("lsp = %d, want %d", len(got.LSP), maxDiagnosticLSP)
	}
	if len(got.Formatters) != maxDiagnosticFormatters {
		t.Fatalf("formatters = %d, want %d", len(got.Formatters), maxDiagnosticFormatters)
	}
	// Sorted, so two reports of the same state render identically.
	for i := 1; i < len(got.Skills); i++ {
		if got.Skills[i-1].Name > got.Skills[i].Name {
			t.Fatalf("skills unsorted at %d", i)
		}
	}
}

func pad(i int) string {
	s := "000" + itoa(i)
	return s[len(s)-3:]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestDiagnosticsSurvivesPartialFailure proves one failed route leaves its
// section absent rather than failing the whole report — a degraded engine must
// still be able to explain itself.
func TestDiagnosticsSurvivesPartialFailure(t *testing.T) {
	h := newRecorder(route{"/skill", `[{"name":"only"}]`})
	s := newOpsSession(h)
	got, err := s.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("a partial report failed outright: %v", err)
	}
	if len(got.Skills) != 1 {
		t.Fatalf("skills = %+v", got.Skills)
	}
}

// TestSkillRouteIsReadOnly proves nothing in this surface mutates skills.
func TestSkillRouteIsReadOnly(t *testing.T) {
	h := newRecorder(
		route{"/skill", `[{"name":"a"}]`},
		route{"/lsp", `[]`},
		route{"/formatter", `[]`},
		route{"/mcp", `{}`},
		route{"/vcs", `{}`},
	)
	s := newOpsSession(h)
	if _, err := s.Diagnostics(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.calls {
		if strings.Contains(c.path, "/skill") && c.method != "GET" {
			t.Fatalf("the skill route was called with %s", c.method)
		}
	}
}

// TestEngineEventNeedsDiagnostics pins exactly which global events invalidate
// the report. The 1.18.21 union has no formatter, config or skill event, so
// inventing one here would be inventing a contract.
func TestEngineEventNeedsDiagnostics(t *testing.T) {
	d := &httpDialect{}
	for _, yes := range []string{"mcp.tools.changed", "lsp.updated"} {
		if !d.EngineEventNeedsDiagnostics(yes) {
			t.Fatalf("%q should invalidate diagnostics", yes)
		}
	}
	for _, no := range []string{
		"session.idle", "message.updated", "message.part.delta",
		"formatter.updated", "skill.updated", "config.changed", "",
	} {
		if d.EngineEventNeedsDiagnostics(no) {
			t.Fatalf("%q must not invalidate diagnostics", no)
		}
	}
}

// TestDisposeCallsTheDocumentedRouteOnce proves recycling is one POST to the
// documented endpoint, scoped to the session's own directory.
func TestDisposeCallsTheDocumentedRouteOnce(t *testing.T) {
	h := newRecorder()
	s := newOpsSession(h)
	if err := s.DisposeInstance(context.Background()); err != nil {
		t.Fatal(err)
	}
	var disposals int
	for _, c := range h.calls {
		if strings.Contains(c.path, "/instance/dispose") {
			disposals++
			if c.method != "POST" {
				t.Fatalf("dispose used %s", c.method)
			}
			if !strings.Contains(c.path, "directory=") {
				t.Fatalf("dispose carried no directory scope: %q", c.path)
			}
		}
	}
	if disposals != 1 {
		t.Fatalf("dispose called %d times, want exactly 1", disposals)
	}
}

// TestReloadReadsBothCatalogs proves a skill becomes visible in diagnostics and
// in the composer, not just one of them.
func TestReloadReadsBothCatalogs(t *testing.T) {
	h := newRecorder(
		route{"/skill", `[{"name":"new-skill"}]`},
		route{"/command", `[]`},
	)
	s := newOpsSession(h)
	if err := s.ReloadSkillCatalogs(context.Background()); err != nil {
		t.Fatal(err)
	}
	var sawSkill, sawCommand bool
	for _, c := range h.calls {
		if strings.Contains(c.path, "/skill") {
			sawSkill = true
		}
		if strings.Contains(c.path, "/command") {
			sawCommand = true
		}
	}
	if !sawSkill || !sawCommand {
		t.Fatalf("reload read skill=%v command=%v; both are needed", sawSkill, sawCommand)
	}
}
