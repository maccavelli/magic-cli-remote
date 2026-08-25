package session

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

type p6AliasProvider struct {
	last   *p6AliasSession
	starts int
}

func (p *p6AliasProvider) ID() provider.ID { return provider.IDCodex }
func (p *p6AliasProvider) Ready() bool     { return true }
func (p *p6AliasProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.starts++
	s := &p6AliasSession{id: opts.LocalSessionID, agentID: opts.AgentSessionID, events: make(chan event.Event, 8)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, AgentSessionID: s.agentID, Status: "idle"}
	p.last = s
	return s, nil
}

type p6AliasSession struct {
	id      string
	agentID string
	events  chan event.Event
}

func (s *p6AliasSession) ID() string                 { return s.id }
func (s *p6AliasSession) ProviderID() provider.ID    { return provider.IDCodex }
func (s *p6AliasSession) AgentSessionID() string     { return s.agentID }
func (s *p6AliasSession) Events() <-chan event.Event { return s.events }
func (s *p6AliasSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *p6AliasSession) Cancel(context.Context) error { return nil }
func (s *p6AliasSession) Close(context.Context) error  { return nil }

func TestReconcileAgentSessionAliasPrefersExactProviderID(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{ID: "managed-1", Provider: provider.IDCodex, AgentSessionID: "native-1", AgentSessionAliases: []string{"older-native"}, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{ID: "managed-2", Provider: provider.IDCodex, AgentSessionID: "other", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, ok := store.ResolveAgentSession(provider.IDCodex, "native-1")
	if !ok || got.ID != "managed-1" {
		t.Fatalf("exact resolve = %+v/%v", got, ok)
	}
	got, ok = store.ResolveAgentSession(provider.IDCodex, "older-native")
	if !ok || got.ID != "managed-1" {
		t.Fatalf("alias resolve = %+v/%v", got, ok)
	}
}

func TestRecordWithoutAliasesRemainsCompatible(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := Record{ID: "legacy", Provider: provider.IDCodex, AgentSessionID: "native", CreatedAt: time.Now()}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("legacy")
	if err != nil || got.AgentSessionID != "native" || got.AgentSessionAliases != nil {
		t.Fatalf("legacy record = %+v err=%v", got, err)
	}
}

func TestLoadedThreadAdoptionSuppressesDuplicateManagedRecordsAndReconnectKeepsAlias(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Record{ID: "managed-1", Provider: provider.IDCodex, AgentSessionID: "native-1", AgentSessionAliases: []string{"older-native"}, OwnerDeviceID: "device-1", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	p := &p6AliasProvider{}
	registry := provider.NewRegistry()
	registry.Register(p)
	manager := NewManager(registry, store, nil, nil)

	first, err := manager.Create(context.Background(), provider.IDCodex, provider.StartOptions{AgentSessionID: "native-1", Name: "adopted"}, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(context.Background(), provider.IDCodex, provider.StartOptions{AgentSessionID: "native-1", Name: "adopted again"}, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "managed-1" || second.ID != first.ID || manager.LiveCount() != 1 || p.starts != 2 {
		t.Fatalf("first=%+v second=%+v live=%d starts=%d", first, second, manager.LiveCount(), p.starts)
	}
	if len(second.AgentSessionAliases) != 1 || second.AgentSessionAliases[0] != "older-native" {
		t.Fatalf("adoption lost durable aliases: %+v", second.AgentSessionAliases)
	}

	p.last.events <- event.Event{Type: event.TypeSessionStatus, SessionID: second.ID, AgentSessionID: "native-2", Status: "idle"}
	deadline := time.Now().Add(time.Second)
	for {
		meta, getErr := manager.Get(second.ID)
		if getErr == nil && meta.AgentSessionID == "native-2" {
			if len(meta.AgentSessionAliases) != 2 || meta.AgentSessionAliases[0] != "older-native" || meta.AgentSessionAliases[1] != "native-1" {
				t.Fatalf("reconnect aliases = %+v", meta.AgentSessionAliases)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reconnect was not reconciled: %+v err=%v", meta, getErr)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestColdReplayDuplicateSuppression(t *testing.T) {
	entry := &entry{}
	first := event.Event{Type: event.TypeAssistantChunk, Text: "same", AgentSessionID: "native-1", Replay: true}
	second := first
	entry.appendHistoryLocked(&first)
	entry.appendHistoryLocked(&second)
	if len(entry.history) != 1 || entry.seq != 1 {
		t.Fatalf("history=%+v seq=%d", entry.history, entry.seq)
	}
}
