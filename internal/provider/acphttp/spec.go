package acphttp

import (
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Spec describes static provider metadata for an ACP-over-HTTP engine.
type Spec struct {
	ID            provider.ID
	DefaultBin    string
	ServeArgs     func(port int) []string
	HealthPath    string
	StaticModels  []picker.Option
	StaticModes   []event.SessionMode
	DefaultModeID string
	Commands      command.Table
}
