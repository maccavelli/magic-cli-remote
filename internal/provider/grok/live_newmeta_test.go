//go:build live_grok

package grok_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// T-M1: argv -m is an ACP no-op; _meta.modelId selects; top-level modelId is
// ignored (MADR 0106 Phase A). Grok 1.0.5 (5115b46bc909).
func TestLiveGrokSessionNewMetaModelID(t *testing.T) {
	t.Run("argv-m-noop", func(t *testing.T) {
		p := startACPInitArgs(t, []string{
			"-m", "grok-4.5",
			"--no-auto-update", "--permission-mode", "default",
			"agent", "--no-leader", "stdio",
		})
		cwd := t.TempDir()
		p.cwd = cwd
		p.send(t, 2, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
		if err := p.waitID(t, 2, 25*time.Second); err != nil {
			t.Fatalf("session/new: %v", err)
		}
		got := currentModelID(rpcResult(t, p, 2))
		if got != "grok-4.6" {
			t.Fatalf("argv -m grok-4.5 applied model %q, want grok-4.6 (ACP no-op)", got)
		}
	})

	t.Run("meta-modelId", func(t *testing.T) {
		p := startACP(t, map[string]any{"modelId": "grok-4.5"})
		got := currentModelID(rpcResult(t, p, 2))
		if got != "grok-4.5" {
			t.Fatalf("_meta.modelId grok-4.5 applied %q, want grok-4.5", got)
		}
	})

	t.Run("toplevel-modelId-ignored", func(t *testing.T) {
		p := startACPInit(t)
		cwd := t.TempDir()
		p.cwd = cwd
		p.send(t, 2, "session/new", map[string]any{
			"cwd": cwd, "mcpServers": []any{}, "modelId": "grok-4.5",
		})
		if err := p.waitID(t, 2, 25*time.Second); err != nil {
			t.Fatalf("session/new: %v", err)
		}
		got := currentModelID(rpcResult(t, p, 2))
		if got != "grok-4.6" {
			t.Fatalf("top-level modelId grok-4.5 applied %q, want grok-4.6 (ignored)", got)
		}
	})
}

// T-E1: session/new _meta.reasoningEffort is applied (MADR 0106 Phase A).
func TestLiveGrokSessionNewMetaReasoningEffort(t *testing.T) {
	rows := []struct {
		name string
		meta map[string]any
		want string
	}{
		{name: "omitted", meta: nil, want: "high"},
		{name: "xhigh", meta: map[string]any{"reasoningEffort": "xhigh"}, want: "xhigh"},
		{name: "low", meta: map[string]any{"reasoningEffort": "low"}, want: "low"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			p := startACP(t, row.meta)
			got := appliedEffort(rpcResult(t, p, 2))
			if got != row.want {
				t.Fatalf("_meta.reasoningEffort %s: applied %q, want %q", row.name, got, row.want)
			}
		})
	}
}

// T-E2: session/load _meta.reasoningEffort changes the loaded session
// (MADR 0106 Phase A).
func TestLiveGrokSessionLoadMetaReasoningEffort(t *testing.T) {
	src := startACP(t, map[string]any{"reasoningEffort": "low"})
	sid := src.sessionID()
	if sid == "" {
		t.Fatal("session/new returned no sessionId")
	}
	cwd := src.cwd
	if got := appliedEffort(rpcResult(t, src, 2)); got != "low" {
		t.Fatalf("setup: created session effort %q, want low", got)
	}
	_ = src.stdin.Close()
	_ = src.cmd.Process.Kill()
	_ = src.cmd.Wait()

	dst := startACPInit(t)
	dst.send(t, 2, "session/load", map[string]any{
		"sessionId":  sid,
		"cwd":        cwd,
		"mcpServers": []any{},
		"_meta":      map[string]any{"reasoningEffort": "xhigh"},
	})
	if err := dst.waitID(t, 2, 45*time.Second); err != nil {
		t.Fatalf("session/load: %v", err)
	}
	got := appliedEffort(rpcResult(t, dst, 2))
	if got != "xhigh" {
		t.Fatalf("session/load _meta.reasoningEffort xhigh: applied %q, want xhigh", got)
	}
}

// T-M2: Provider.Start with Model grok-4.5 applies `_meta.modelId` (MADR 0106 Phase B).
func TestLiveGrokStartAppliesMetaModel(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "meta-model", CWD: t.TempDir(), Model: "grok-4.5"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	got := promptText(t, s, "/session-info", 40*time.Second)
	if !strings.Contains(got, "**Model:** grok-4.5") {
		t.Fatalf("session-info after Start(Model=grok-4.5): %q", got)
	}
}

// T-E3: Provider.Start with ThinkingLevel low reports low (MADR 0106 Phase B).
func TestLiveGrokStartAppliesMetaEffort(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "meta-effort", CWD: t.TempDir(), ThinkingLevel: "low"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	ts, ok := s.(provider.ThinkingSession)
	if !ok {
		t.Fatal("session is not ThinkingSession")
	}
	if got := ts.ThinkingLevel(); got != "low" {
		t.Fatalf("ThinkingLevel() = %q, want low", got)
	}
}

func rpcResult(t *testing.T, p *acpProc, id int) map[string]any {
	t.Helper()
	msg := p.result(id)
	if msg == nil {
		t.Fatalf("no rpc id %d", id)
	}
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		t.Fatalf("rpc %d: no result: %v", id, msg)
	}
	return res
}

func currentModelID(result map[string]any) string {
	if models, ok := result["models"].(map[string]any); ok {
		if id, _ := models["currentModelId"].(string); id != "" {
			return id
		}
	}
	meta, _ := result["_meta"].(map[string]any)
	if det, ok := nestedMap(meta, "x.ai/sessionDetail"); ok {
		if id, _ := det["currentModelId"].(string); id != "" {
			return id
		}
	}
	return ""
}

func appliedEffort(result map[string]any) string {
	if models, ok := result["models"].(map[string]any); ok {
		cur, _ := models["currentModelId"].(string)
		if cur == "" {
			cur = "grok-4.6"
		}
		if ams, ok := models["availableModels"].([]any); ok {
			for _, raw := range ams {
				m, _ := raw.(map[string]any)
				if m == nil {
					continue
				}
				id, _ := m["modelId"].(string)
				if id != cur {
					continue
				}
				if meta, _ := m["_meta"].(map[string]any); meta != nil {
					if e, _ := meta["reasoningEffort"].(string); e != "" {
						return e
					}
				}
			}
		}
	}
	meta, _ := result["_meta"].(map[string]any)
	cfg, ok := nestedMap(meta, "x.ai/sessionConfig")
	if !ok {
		return ""
	}
	opts, _ := cfg["options"].([]any)
	for _, raw := range opts {
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		cat, _ := o["category"].(string)
		sel, _ := o["selected"].(bool)
		if cat == "mode" && sel {
			id, _ := o["id"].(string)
			return id
		}
	}
	return ""
}

func nestedMap(parent map[string]any, key string) (map[string]any, bool) {
	if parent == nil {
		return nil, false
	}
	m, ok := parent[key].(map[string]any)
	return m, ok
}
