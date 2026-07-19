package grok

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestAutoAllowPrefersAllowKind(t *testing.T) {
	resp := autoAllow(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "deny", Name: "Deny", Kind: "reject_once"},
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
	})
	if resp.Outcome.Selected == nil {
		t.Fatal("expected selected")
	}
	if string(resp.Outcome.Selected.OptionId) != "allow" {
		t.Fatalf("got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestContentText(t *testing.T) {
	if contentText(acp.TextBlock("hi")) != "hi" {
		t.Fatal("expected hi")
	}
	if contentText(acp.ContentBlock{}) != "" {
		t.Fatal("expected empty")
	}
}
