package codex

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file holds the inputs and derivations for the contract capture
// (PLAN 0163 P4). It exists so re-pinning to a new Codex release is a command
// with arguments, not an edit to a test body.
//
// Before D2, four values were hardcoded literals inside
// TestGenerateContractManifest — the version, the binary SHA-256, the upstream
// commit and the output directory — and the "implemented" classification was a
// hand-maintained switch. That is why the pin sat six releases behind while the
// drift gate it feeds could not pass on any host (MADR 0163 F1/F2/F5).

// generatorInputs is every value the capture needs.
type generatorInputs struct {
	version          string
	binarySHA256     string
	sourceCommit     string
	outDir           string
	installedStable  string
	installedExpUnfl string
	sourceStable     string
	sourceExp        string
	sourceTree       string
}

func readGeneratorInputs(t *testing.T) generatorInputs {
	t.Helper()
	in := generatorInputs{
		version:          requiredEnv(t, "CODEX_CONTRACT_VERSION"),
		sourceCommit:     requiredEnv(t, "CODEX_SOURCE_COMMIT"),
		outDir:           requiredEnv(t, "CODEX_CONTRACT_OUT_DIR"),
		installedStable:  requiredEnv(t, "CODEX_CONTRACT_STABLE_SCHEMA"),
		installedExpUnfl: requiredEnv(t, "CODEX_CONTRACT_EXPERIMENTAL_SCHEMA"),
		sourceStable:     requiredEnv(t, "CODEX_SOURCE_STABLE_SCHEMA"),
		sourceExp:        requiredEnv(t, "CODEX_SOURCE_EXPERIMENTAL_SCHEMA"),
		sourceTree:       requiredEnv(t, "CODEX_SOURCE_TREE"),
	}
	in.binarySHA256 = resolveGeneratorSHA(t, in.version)
	return in
}

// resolveGeneratorSHA computes the SHA-256 of the codex binary on PATH rather
// than trusting an operator to paste one, and cross-checks the version while it
// is there.
//
// CODEX_CONTRACT_BINARY_SHA256 overrides for a cross-host capture, but a
// disagreement is fatal: a manifest carrying one binary's hash and another
// binary's surface is worse than no manifest, because EvidenceMatched would be
// false on the very machine that generated it. The version check exists because
// two Codex installs at different versions coexisted on the reference host —
// %APPDATA%\npm at 0.155.1 and the managed tree at 0.154.0-alpha.6.2 — and only
// the resolved path says which one answered (MADR 0163 D18).
func resolveGeneratorSHA(t *testing.T, wantVersion string) string {
	t.Helper()
	override := os.Getenv("CODEX_CONTRACT_BINARY_SHA256")
	identity, err := resolveBinaryIdentity(context.Background(), "codex", "")
	if err != nil {
		if override == "" {
			t.Fatalf("cannot resolve the codex binary, and CODEX_CONTRACT_BINARY_SHA256 is unset: %v", err)
		}
		t.Logf("codex not resolvable (%v); trusting CODEX_CONTRACT_BINARY_SHA256", err)
		return override
	}
	t.Logf("codex resolved: path=%s version=%s sha256=%s", identity.Path, identity.Version, identity.SHA256)
	// On a Windows npm install, PATH resolves to a ~341-byte .cmd shim, so the
	// recorded hash identifies the SHIM and not the 300 MB engine behind it.
	// npm regenerates that shim identically across codex releases, which means
	// BinarySHA256 contributes almost nothing to EvidenceMatched there and the
	// version comparison is carrying the check. Say so at capture time rather
	// than letting a reader trust the hash. Resolving a shim to its real target
	// is Deferred in PLAN 0163 (it is the same work as wiring launch.Command's
	// batch handling).
	if ext := strings.ToLower(filepath.Ext(identity.Path)); ext == ".cmd" || ext == ".bat" {
		t.Logf("NOTE %s is a %s shim: BinarySHA256 identifies the shim, not the engine, "+
			"so version equality is the real evidence on this host", identity.Path, ext)
	}
	if identity.Version != "" && identity.Version != wantVersion {
		t.Fatalf("CODEX_CONTRACT_VERSION=%s but the codex on PATH reports %s (%s): capture against "+
			"the binary being pinned, or the manifest describes a surface no install has",
			wantVersion, identity.Version, identity.Path)
	}
	if override != "" && override != identity.SHA256 {
		t.Fatalf("CODEX_CONTRACT_BINARY_SHA256=%s but %s hashes to %s", override, identity.Path, identity.SHA256)
	}
	return identity.SHA256
}

// experimentalNotifications derives the experimental notification set from the
// Rust source, because the schema bundles cannot answer the question.
//
// Codex's exporter prunes experimental client methods, server methods and
// fields, but never experimental NOTIFICATIONS (filter_experimental_schema,
// app-server-protocol/src/export.rs), so both bundles list every notification
// while the runtime does suppress the experimental ones
// (app-server/src/transport.rs). Trusting the bundle is why the pinned manifest
// marked notifications such as thread/queue/changed and thread/realtime/sdp as
// stable when they never arrive without the opt-in (MADR 0163 F4).
//
// The scan is narrow on purpose: inside the server_notification_definitions!
// invocation, an entry whose preceding attributes include #[experimental is
// experimental. An empty result is treated as failure, because empty is the
// signature of a macro-syntax change rather than a legitimate answer.
func experimentalNotifications(t *testing.T, sourceTree string) map[string]struct{} {
	t.Helper()
	path := filepath.Join(sourceTree, "codex-rs", "app-server-protocol", "src", "protocol", "common.rs")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (CODEX_SOURCE_TREE must be a codex checkout at CODEX_SOURCE_COMMIT)", path, err)
	}
	const marker = "server_notification_definitions!"
	text := string(raw)
	start := strings.LastIndex(text, marker)
	if start < 0 {
		t.Fatalf("%s has no %s invocation: the macro was renamed and this scan needs updating", path, marker)
	}
	block := text[start:]
	if end := strings.Index(block, "\n}\n"); end > 0 {
		block = block[:end]
	}

	out := map[string]struct{}{}
	experimental, renamed := false, ""
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#[experimental"):
			experimental = true
			continue
		case strings.HasPrefix(trimmed, "#[serde(rename"):
			// One notification spells its wire name in an attribute rather than
			// with the => "wire/method" form (account/login/completed).
			if v, ok := firstQuoted(trimmed); ok {
				renamed = v
			}
			continue
		case strings.HasPrefix(trimmed, "#["), strings.HasPrefix(trimmed, "//"), trimmed == "":
			continue
		}
		method := renamed
		if i := strings.Index(trimmed, "=> \""); i >= 0 {
			if v, ok := firstQuoted(trimmed[i+3:]); ok {
				method = v
			}
		}
		if method != "" {
			if experimental {
				out[method] = struct{}{}
			}
			experimental, renamed = false, ""
		}
	}
	if len(out) == 0 {
		t.Fatalf("no #[experimental] notifications found in %s: an empty result is the failure "+
			"signature of a macro-syntax change, not a valid answer (MADR 0163 D4)", path)
	}
	t.Logf("source scan: %d experimental notifications", len(out))
	return out
}

func firstQuoted(s string) (string, bool) {
	i := strings.Index(s, "\"")
	if i < 0 {
		return "", false
	}
	rest := s[i+1:]
	j := strings.Index(rest, "\"")
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// implementedMethods reports which wire methods this package actually uses,
// derived from the package's own code rather than from the hand-maintained
// switch it replaces. That switch named 32 client requests and no server request
// at all, which is why all 21 server requests sat in typed_deferred while
// session.go answered nine of them deliberately (MADR 0163 F5).
//
// Two exact sources, plus the route table read by the caller:
//   - callArgs: a string literal passed as an argument to a call in a non-test
//     file. Restricting to call arguments, rather than any literal, keeps a
//     method merely named in a lookup table from counting as sent.
//   - caseLabels: a string literal in a case clause, which is how
//     handleServerRequest declares the requests it answers on purpose.
func implementedMethods(t *testing.T) (callArgs, caseLabels map[string]struct{}) {
	t.Helper()
	callArgs, caseLabels = map[string]struct{}{}, map[string]struct{}{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				for _, arg := range node.Args {
					if v, ok := stringLiteral(arg); ok {
						callArgs[v] = struct{}{}
					}
				}
			case *ast.CaseClause:
				for _, expr := range node.List {
					if v, ok := stringLiteral(expr); ok {
						caseLabels[v] = struct{}{}
					}
				}
			}
			return true
		})
	}
	if len(callArgs) == 0 {
		t.Fatal("no string literals found in call arguments: the AST scan is broken")
	}
	return callArgs, caseLabels
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// stableNotificationsOnly drops the experimental notifications from a captured
// surface. It must be applied to BOTH the installed and the source surface: the
// exporter over-reports identically on each side, so filtering one and not the
// other reports every experimental notification as a source-only delta that
// never clears (MADR 0163 D4).
func stableNotificationsOnly(entries []WireContract, experimentalOnly map[string]struct{}) []WireContract {
	kept := make([]WireContract, 0, len(entries))
	for _, entry := range entries {
		if _, isExperimental := experimentalOnly[entry.Method]; !isExperimental {
			kept = append(kept, entry)
		}
	}
	return kept
}
