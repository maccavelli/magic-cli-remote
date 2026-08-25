package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type lifecycleProvider struct {
	session *lifecycleSession
}

func (p *lifecycleProvider) ID() provider.ID { return "lifecycle" }
func (p *lifecycleProvider) Ready() bool     { return true }
func (p *lifecycleProvider) CommandTable() command.Table {
	return command.Table{
		"archive": {Kind: command.KindOp, Op: command.OpArchive},
		"delete":  {Kind: command.KindOp, Op: command.OpDelete},
	}
}
func (p *lifecycleProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &lifecycleSession{id: opts.LocalSessionID, events: make(chan event.Event, 4)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Status: "idle"}
	p.session = s
	return s, nil
}

type lifecycleSession struct {
	id           string
	events       chan event.Event
	archiveCalls []bool
	previewCalls int
	deleteCalls  int
}

func (s *lifecycleSession) ID() string                 { return s.id }
func (s *lifecycleSession) ProviderID() provider.ID    { return "lifecycle" }
func (s *lifecycleSession) AgentSessionID() string     { return "native-1" }
func (s *lifecycleSession) Events() <-chan event.Event { return s.events }
func (s *lifecycleSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *lifecycleSession) Cancel(context.Context) error { return nil }
func (s *lifecycleSession) Close(context.Context) error  { return nil }
func (s *lifecycleSession) ArchiveNativeThread(_ context.Context, archived bool) error {
	s.archiveCalls = append(s.archiveCalls, archived)
	return nil
}
func (s *lifecycleSession) PreviewNativeDelete(context.Context) (provider.ThreadDeletePreview, error) {
	s.previewCalls++
	return provider.ThreadDeletePreview{DescendantIDs: []string{"child-1", "child-2"}, HasLoadedDescendants: true}, nil
}
func (s *lifecycleSession) DeleteNativeThread(context.Context) (provider.ThreadDeleteResult, error) {
	s.deleteCalls++
	return provider.ThreadDeleteResult{
		Deleted: true, DescendantIDs: []string{"child-1", "child-2"}, FailedDescendantIDs: []string{"child-2"}, Partial: true,
	}, nil
}

func TestArchiveAndDeleteRequireFixedConfirmationAndReportDescendants(t *testing.T) {
	p := &lifecycleProvider{}
	registry := provider.NewRegistry()
	registry.Register(p)
	sink := &eventSink{}
	manager := session.NewManager(registry, nil, nil, sink.handle)
	meta, err := manager.Create(context.Background(), "lifecycle", provider.StartOptions{LocalSessionID: "local-1", Name: "native"}, "device-1")
	if err != nil {
		t.Fatal(err)
	}

	for _, prompt := range []string{"/archive", "/archive archive", "/archive unarchive", "/delete", "/delete delete permanently"} {
		if err := manager.Prompt(context.Background(), meta.ID, prompt, nil, "device-1"); err != nil {
			t.Fatalf("%s: %v", prompt, err)
		}
	}
	if len(p.session.archiveCalls) != 2 || !p.session.archiveCalls[0] || p.session.archiveCalls[1] {
		t.Fatalf("archive calls = %v", p.session.archiveCalls)
	}
	if p.session.previewCalls != 2 || p.session.deleteCalls != 1 {
		t.Fatalf("preview=%d delete=%d", p.session.previewCalls, p.session.deleteCalls)
	}
	joined := strings.Join(sink.notices(), "\n")
	for _, want := range []string{
		"Confirmation required: /archive archive (or /archive unarchive).",
		"Confirm exactly: /delete delete permanently",
		"2 descendant(s)",
		"1 descendant(s) failed",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notices missing %q: %s", want, joined)
		}
	}
}
