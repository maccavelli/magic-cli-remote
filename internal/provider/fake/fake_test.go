package fake_test

import (
	"context"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

func TestStartHonorsLocalSessionID(t *testing.T) {
	p := fake.New()
	s, err := p.Start(context.Background(), provider.StartOptions{LocalSessionID: "fixed-id"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != "fixed-id" {
		t.Fatalf("id=%q want fixed-id", s.ID())
	}
	_ = s.Close(context.Background())
}
