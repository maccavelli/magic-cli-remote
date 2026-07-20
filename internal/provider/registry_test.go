package provider

import (
	"context"
	"testing"
)

type mockProvider struct {
	id    ID
	ready bool
}

func (m *mockProvider) ID() ID { return m.id }
func (m *mockProvider) Ready() bool { return m.ready }
func (m *mockProvider) Start(ctx context.Context, opts StartOptions) (Session, error) {
	return nil, nil
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	// 1. Get unknown provider
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Error("expected error when getting unregistered provider")
	}

	// 2. Register mock
	p1 := &mockProvider{id: "mock1", ready: true}
	reg.Register(p1)

	got, err := reg.Get("mock1")
	if err != nil {
		t.Fatalf("failed to get registered provider: %v", err)
	}
	if got != p1 {
		t.Errorf("got %v; want %v", got, p1)
	}

	// 3. Overwrite registration
	p2 := &mockProvider{id: "mock1", ready: false}
	reg.Register(p2)
	got2, err := reg.Get("mock1")
	if err != nil {
		t.Fatalf("failed to get registered provider: %v", err)
	}
	if got2 != p2 {
		t.Errorf("got %v; want %v", got2, p2)
	}

	// 4. List providers
	list := reg.List()
	if len(list) != 1 {
		t.Errorf("list len = %d; want 1", len(list))
	}
	if list[0].ID != "mock1" {
		t.Errorf("list[0].ID = %q; want mock1", list[0].ID)
	}
	if list[0].Ready != false {
		t.Error("list[0].Ready should be false")
	}
}
